---
phase: 18-query-tools
reviewed: 2026-07-21T00:00:00Z
depth: standard
files_reviewed: 24
files_reviewed_list:
  - cmd/cpg/explain.go
  - cmd/cpg/explain_test.go
  - cmd/cpg/mcp.go
  - cmd/cpg/mcp_query.go
  - cmd/cpg/mcp_query_evidence.go
  - cmd/cpg/mcp_query_evidence_test.go
  - cmd/cpg/mcp_query_flows.go
  - cmd/cpg/mcp_query_flows_test.go
  - cmd/cpg/mcp_query_pagination.go
  - cmd/cpg/mcp_query_pagination_test.go
  - cmd/cpg/mcp_query_tools_test.go
  - cmd/cpg/mcp_session_test.go
  - pkg/explain/doc.go
  - pkg/explain/filter.go
  - pkg/explain/filter_test.go
  - pkg/explain/render.go
  - pkg/explain/render_test.go
  - pkg/hubble/health_reader.go
  - pkg/hubble/health_reader_test.go
  - pkg/hubble/health_writer.go
  - pkg/hubble/health_writer_test.go
  - pkg/hubble/pipeline_test.go
  - pkg/output/writer.go
  - pkg/output/writer_test.go
findings:
  critical: 0
  warning: 4
  info: 2
  total: 6
status: issues_found
---

# Phase 18: Code Review Report

**Reviewed:** 2026-07-21T00:00:00Z
**Depth:** standard
**Files Reviewed:** 24
**Status:** issues_found

## Summary

Phase 18 adds 5 readonly MCP query tools (`list_policies`, `get_policy`,
`get_cluster_health`, `get_evidence`, `list_dropped_flows`) over a session
tmpdir, plus an opaque boundary-key pagination/cursor mechanism and an
enum-patching schema helper. It also promotes `pkg/explain` (moved verbatim
from `cmd/cpg/explain_filter.go`/`explain_render.go`) and exports the
`cluster-health.json` structs in `pkg/hubble` so the new `ReadClusterHealth`
reader and the MCP outputSchema can share them.

I reviewed every changed source file, traced the query handlers against their
upstream dependencies (`evidence.ValidatePolicyRef`, `evidence.HashOutputDir`,
`session.Manager.Status`, `dropclass.Classify`, the aggregator's drop-routing
gate), built the affected packages, and ran the affected tests — all green.

**The security-sensitive surfaces the phase flagged are sound:**

- **Path traversal** — `get_policy`/`get_evidence` gate namespace/workload
  through `evidence.ValidatePolicyRef` before any `filepath.Join`; it rejects
  empty, `.`/`..`, and `/`. `list_dropped_flows` never uses filter values for
  path construction at all (it walks the tree and filters in-memory).
- **Cursor tampering** — `decodeCursor` fails closed (error, never panic) on
  bad base64/JSON; a well-formed-but-adversarial cursor only shifts the
  pagination scan position and is never used as a filesystem path component.
- **Session-id confusion** — `resolveSession` delegates to `Manager.Status`,
  which requires an exact single-slot ID match and returns the verbatim
  SESS-06 "not found or expired" text.
- **Readonly** — every handler only reads; no K8s write verb is reachable.
- **Pagination caps** — `clampLimit` bounds page size; `paginate`'s slice math
  is panic-safe across all boundary cases I traced.
- **Path-derivation invariant** — the `HashOutputDir(join(tmpDir,"policies"))`
  + evidenceDir + healthPath formula the readers re-derive matches the writer
  side (`pkg/session/pipeline_config.go`) exactly, so health/evidence reads
  land where the pipeline wrote them.

No Critical issues. The findings below are 1 real filter-classification bug,
2 robustness gaps, 1 maintainability risk, and 2 minor Info items.

## Warnings

### WR-01: `list_dropped_flows` misclassifies `DROP_REASON_UNKNOWN` samples as "transient", breaking its own dropclass contract

**File:** `cmd/cpg/mcp_query_flows.go:430-438` (used at `:213`); root cause cross-references `pkg/hubble/aggregator.go:417-441`

**Issue:** `classifyDropReasonName` re-derives a sample's drop class from its
stored reason string via `dropclass.Classify`. For a sample whose
`DropReason == "DROP_REASON_UNKNOWN"` it computes
`Classify(flowpb.DropReason(0))`, which the taxonomy maps to
`DropClassTransient` → `"transient"`.

But such samples reach evidence **only via the policy-actionable path**. The
aggregator's classification gate is:

```go
if f.Verdict == flowpb.Verdict_DROPPED && f.GetDropReasonDesc() != flowpb.DropReason_DROP_REASON_UNKNOWN {
    // Infra/Transient -> healthCh + continue (never written to evidence)
    // Noise           -> continue (discarded)
}
// reason == DROP_REASON_UNKNOWN falls through to keyFromFlow -> evidence
```

A DROPPED flow with reason `DROP_REASON_UNKNOWN` (value 0) is **excluded** from
the infra/transient suppression branch and falls through to the policy/evidence
path — so it is, by construction, policy-actionable, never transient-suppressed.
A genuinely transient flow can never appear in evidence at all. The query tool
then re-labels that same sample "transient", which is inconsistent with how the
pipeline actually routed it, and directly contradicts the tool's emphatic
description:

> "dropclass=policy/unknown flows only ever populate samples[], dropclass=infra/transient flows only ever populate aggregates[]"

Concrete consequences for an LLM caller relying on that contract:
- `dropclass=policy` **drops** these genuinely policy-actionable samples (false negative).
- `dropclass=transient` **surfaces** sample rows (false positive) even though the
  description promises transient flows only ever appear in `aggregates[]`.

The author's `name == ""` → `Unknown` guard (the comment at `:427-429` about
"no reason recorded" vs "explicitly DROP_REASON_UNKNOWN") does not catch this:
`evidence_writer.go:131` stores `f.GetDropReasonDesc().String()`, which for the
zero value is the non-empty string `"DROP_REASON_UNKNOWN"`, not `""` — so the
sample takes the map-lookup branch, not the empty-string branch.

Reachability depends on flows carrying reason `DROP_REASON_UNKNOWN` on a DROPPED
verdict, which Cilium emits when the datapath sets no specific code — not exotic.

**Fix:** Classify samples consistently with the aggregator's routing gate.
Because every evidence sample is policy-actionable by construction, the zero
reason should map to `Unknown` (the fall-through bucket), not `Transient`:

```go
func classifyDropReasonName(name string) dropclass.DropClass {
	if name == "" {
		return dropclass.DropClassUnknown
	}
	if val, ok := flowpb.DropReason_value[name]; ok {
		// Mirror aggregator.go's gate: reason == DROP_REASON_UNKNOWN(0) is NOT
		// transient-suppressed — it falls through to the policy/evidence path,
		// so a sample carrying it is Unknown-class, never Transient.
		if flowpb.DropReason(val) == flowpb.DropReason_DROP_REASON_UNKNOWN {
			return dropclass.DropClassUnknown
		}
		return dropclass.Classify(flowpb.DropReason(val))
	}
	return dropclass.DropClassUnknown
}
```

Add a `dropclass=transient` sub-test seeded with a `DROP_REASON_UNKNOWN` sample
asserting `samples[]` is empty, pinning the corrected contract.

### WR-02: `list_policies` and `get_cluster_health` return unbounded responses, bypassing the MCP output cap the paginated tools deliberately respect

**File:** `cmd/cpg/mcp_query.go:142-195` (`handleListPolicies`), `cmd/cpg/mcp_query.go:302-360` (`get_cluster_health`); contrast `cmd/cpg/mcp_query_pagination.go:11-31`

**Issue:** The pagination consts are explicitly justified as staying "bounded
well under the ~25k-token MCP output cap" (`mcp_query_pagination.go:11-13`), and
`get_evidence`/`list_dropped_flows` cap every page via `clampLimit`. But
`list_policies` returns one row per policy file with no pagination or cap, and
`get_cluster_health` passes through the entire `ClusterHealthReport` including
per-reason `by_node`/`by_workload` maps. A session capturing policy-actionable
drops across many namespaces/workloads (or a report spanning many nodes ×
workloads × reasons) can produce a response that exceeds the same output cap the
rest of the phase carefully engineers around — truncating or breaking the tool
result for an LLM host. The tool description acknowledges `list_policies` is
"Unpaginated," but the design intent elsewhere makes the omission an
inconsistency, not merely a documented choice.

**Fix:** Either give `list_policies` the same `limit`/`cursor`/`total_count`/
`has_more` treatment the other list tools use (its rows already sort naturally
by namespace/workload, so the existing `paginate` + `paginateBoundaryKey`
mechanism applies directly), or add an explicit server-side row cap with a
`has_more`-style truncation marker so a large capture degrades predictably
instead of silently overflowing the cap.

### WR-03: `handleGetPolicy` reads the policy file twice, risking metadata/YAML skew during an active capture

**File:** `cmd/cpg/mcp_query.go:255-269`

**Issue:** The handler reads the same path twice:

```go
cnp, err := output.ReadPolicyFile(path) // read #1: parse for Name + rule counts
...
yamlBytes, err := os.ReadFile(path)     // read #2: raw YAML string
```

The writer rewrites policy files with an atomic temp+rename during active
capture (`pkg/output/writer.go:81-102`). If a rewrite lands between read #1 and
read #2, the returned `getPolicyResult` mixes `Name`/`IngressRuleCount`/
`EgressRuleCount` from one version with `YAML` from another — internally
inconsistent output (rule counts that don't match the YAML document). Atomic
writes guarantee each individual read is complete, but not that the two reads
observe the same version.

**Fix:** Read the bytes once and derive both from them, eliminating the skew and
halving the I/O:

```go
yamlBytes, err := os.ReadFile(path)
if err != nil {
	if errors.Is(err, fs.ErrNotExist) { /* actionable not-found */ }
	return nil, getPolicyResult{}, err
}
cnp, err := output.UnmarshalPolicy(yamlBytes) // or yaml.Unmarshal into a CNP here
```

### WR-04: The evidence/health path-derivation formula is duplicated across 5 sites with no single source of truth

**File:** `cmd/cpg/mcp_query.go:311-312`, `cmd/cpg/mcp_query_evidence.go:124-126`, `cmd/cpg/mcp_query_flows.go:170-171`, `pkg/session/pipeline_config.go:69-71`, `pkg/session/manager.go:408-409`

**Issue:** The correctness-critical layout contract
`outputHash = HashOutputDir(join(tmpDir,"policies"))`,
`evidenceDir = join(tmpDir,"evidence")`,
`healthPath = join(evidenceDir, outputHash, "cluster-health.json")` is
hand-copied into all three new query handlers plus the writer-side config and
`Manager.Stop`. It is currently consistent (verified), but it is exactly the
kind of invariant that breaks silently: change the "policies" subdir name or the
hash input in one place and the readers keep looking where the writer no longer
writes, with no compile error and only a subtle "no data" symptom. The repeated
`// D-08: re-derive ... exact formula` comments confirm the duplication was a
conscious "zero new pkg/session API" trade-off, but a single shared helper is
cheap insurance for a filesystem contract this load-bearing.

**Fix:** Introduce one small helper (e.g. `session.SessionPaths(tmpDir)` returning
`{OutputDir, EvidenceDir, OutputHash, ClusterHealthPath}`, or an
`evidence`-package equivalent) and call it from all 5 sites so the layout is
defined exactly once.

## Info

### IN-01: `absOutDir` is misleadingly named — no absolute-path conversion happens

**File:** `cmd/cpg/explain.go:68-69`

**Issue:** `absOutDir := outputDir` implies an absolute-path normalization that
never occurs; the raw flag value is passed straight to `evidence.HashOutputDir`,
which does its own `filepath.Abs`+`Clean` internally. The result is correct, but
the variable name invites a reader to assume a conversion that isn't there.
(Pre-existing; not modified by this phase, but present in a reviewed file.)

**Fix:** Drop the alias and call `evidence.HashOutputDir(outputDir)` directly, or
rename to `outputDir`/document that normalization is delegated to `HashOutputDir`.

### IN-02: `validateFilterComponent` guards values that never reach a filesystem path

**File:** `cmd/cpg/mcp_query_flows.go:281-289` (comment at `:158-166`)

**Issue:** `list_dropped_flows` resolves samples/aggregates by walking the
evidence tree and filtering namespace/workload **in memory**
(`matchesNamespaceWorkload`); the filter args are never joined into a path. The
traversal guard is therefore harmless defense-in-depth, but the surrounding
comment ("before any path use ... symmetrically with the write side") overstates
its role and could mislead a future maintainer into thinking these values reach
`filepath.Join` (they don't, unlike `get_policy`/`get_evidence` where the guard
is load-bearing).

**Fix:** Keep the guard (defense-in-depth is fine) but soften the comment to note
these filter values are never used as path components in this tool; the guard is
purely belt-and-suspenders symmetry with the required-arg tools.

---

_Reviewed: 2026-07-21T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
