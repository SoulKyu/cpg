---
phase: 18-query-tools
fixed_at: 2026-07-21T15:56:51Z
review_path: .planning/phases/18-query-tools/18-REVIEW.md
iteration: 1
findings_in_scope: 4
fixed: 4
skipped: 0
status: all_fixed
---

# Phase 18: Code Review Fix Report

**Fixed at:** 2026-07-21T15:56:51Z
**Source review:** .planning/phases/18-query-tools/18-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 4 (fix_scope: critical_warning — WR-01..WR-04; IN-01/IN-02 out of scope, not attempted)
- Fixed: 4
- Skipped: 0

## Fixed Issues

### WR-01: `list_dropped_flows` misclassifies `DROP_REASON_UNKNOWN` samples as "transient", breaking its own dropclass contract

**Files modified:** `cmd/cpg/mcp_query_flows.go`, `cmd/cpg/mcp_query_flows_test.go`
**Commit:** `8ea5a42`
**Status:** fixed: requires human verification (classification/logic bug — see note below)

**Applied fix:** `classifyDropReasonName` now special-cases `DROP_REASON_UNKNOWN` (value 0) to
return `dropclass.DropClassUnknown` before falling through to `dropclass.Classify`, mirroring
`pkg/hubble/aggregator.go`'s classification gate exactly: reason==0 on a DROPPED flow is excluded
from the infra/transient-suppression branch and falls through to the policy/evidence path, so a
sample carrying it is policy-actionable-by-construction and must never classify as "transient"
(`dropclass.Classify(0)`'s own bucket, written for Cilium's reason-code taxonomy, not this
aggregator's routing behavior). Kept the fix at the call site in `cmd/cpg` per the task's explicit
guidance — `pkg/dropclass` is untouched and remains the single source of truth for the
reason-code-to-class taxonomy itself.

Added a new sub-test, `dropclass_unknown_reason_sample_classifies_unknown_not_transient`, seeding a
`DROP_REASON_UNKNOWN` evidence sample and asserting it is excluded from `dropclass=transient` and
present under `dropclass=unknown` — pinning the corrected contract as REVIEW.md's Fix section
requested.

**Verification:** `go build ./...` clean; `go vet` clean; targeted test
(`TestMCPQueryListDroppedFlows`, including the new sub-test) passes; full `cmd/cpg` and
`pkg/hubble` suites pass.

**Note on "requires human verification":** this finding is a classification/logic bug (an incorrect
condition inside `classifyDropReasonName`), not a syntax or structural issue — Tier 1/2 verification
(re-read + build/vet) cannot prove semantic correctness on its own. A dedicated regression test was
added and passes, directly encoding the reviewer's exact scenario (a `DROP_REASON_UNKNOWN` sample
must appear under `dropclass=unknown`, never `dropclass=transient`), which is strong affirmative
evidence — but per this workflow's logic-bug policy, please manually confirm the classification
reasoning (aggregator gate vs. `dropclass.Classify`'s taxonomy) before this phase proceeds to the
verifier.

### WR-02: `list_policies` and `get_cluster_health` return unbounded responses, bypassing the MCP output cap

**Files modified:** `cmd/cpg/mcp_query.go`, `cmd/cpg/mcp_query_pagination.go`, `cmd/cpg/mcp_query_tools_test.go`
**Commit:** `afab493`
**Status:** fixed

**Applied fix:** Two independent mitigations, one per tool, per REVIEW.md's "either/or" guidance:

- `list_policies` now takes the same `limit`/`cursor` treatment as `get_evidence`/
  `list_dropped_flows`: a new `listPoliciesArgs{SessionID, Limit, Cursor}` and a
  `listPoliciesResult` carrying `total_count`/`has_more`/`next_cursor`, reusing the existing
  `paginate` + `paginateBoundaryKey` mechanism unchanged (rows already sort naturally by
  namespace/workload — `os.ReadDir`'s own sorted-by-filename order — and each (namespace,
  workload) pair yields exactly one row, so `Index` is always 0). Reuses the existing
  `defaultFlowLimit`/`maxFlowLimit` consts (updated their doc comment to name `list_policies` as a
  second consumer) since its rows are the same compact scale as `list_dropped_flows`'. Cursor
  decoding was deliberately moved before any filesystem access, so an invalid cursor is an
  actionable error even when the session has zero policies yet — proven by a new
  `TestMCPQueryToolsNotFoundAndCursorErrorTexts` case.
- `get_cluster_health` now caps each drop reason's `by_node`/`by_workload` breakdown map at a new
  `maxHealthMapEntries` (100) via `capClusterHealthReport`/`capHealthCountMap`, keeping the
  highest-count entries (ties broken alphabetically for determinism); a new `Truncated bool
  json:"truncated,omitempty"` field on `getClusterHealthResult` signals when capping occurred. The
  reason's own `Count` total is never touched — only the breakdown's cardinality — so a capped
  response never misrepresents totals. `pkg/hubble.ClusterHealthReport`/`HealthDropJSON` (the
  "passthrough discipline... zero new/derived fields" D-12 JSON structs) were deliberately left
  untouched; truncation happens on the already-decoded, call-private struct instance inside the
  MCP handler, never in `pkg/hubble` itself.

Added `TestMCPQueryListPoliciesPagination` (3-policy, 2-namespace page-by-page walk),
`TestMCPQueryGetClusterHealth`'s new `stopped_present_truncates_large_workload_breakdown` sub-test,
and a pure-function `TestCapClusterHealthReport` (under-cap/over-cap/tie-break-boundary cases).

**Verification:** `go build ./...` clean; `go vet` clean; all new + existing tests in
`cmd/cpg` pass (`TestMCPQueryListPolicies`, `TestMCPQueryListPoliciesPagination`,
`TestMCPQueryGetClusterHealth`, `TestClusterHealthBranch`, `TestCapClusterHealthReport`,
`TestMCPQueryToolsListed`, `TestMCPQueryToolsQRY05Contract`, `TestMCPQueryToolsErrorTexts`,
`TestMCPQueryToolsNotFoundAndCursorErrorTexts`); full `cmd/cpg` suite passes.

### WR-03: `handleGetPolicy` reads the policy file twice, risking metadata/YAML skew during an active capture

**Files modified:** `cmd/cpg/mcp_query.go`, `pkg/output/writer.go`, `pkg/output/writer_test.go`
**Commit:** `794175a`
**Status:** fixed

**Applied fix:** Added `output.UnmarshalPolicy(data []byte) (*ciliumv2.CiliumNetworkPolicy, error)`,
factored out of `output.ReadPolicyFile` (which now delegates to it after its own `os.ReadFile`).
`handleGetPolicy` now reads the file exactly once via `os.ReadFile`, then derives both the parsed
CNP (`Name`/rule counts) and the raw YAML string from that same byte slice via
`output.UnmarshalPolicy` — eliminating the read-twice race against `Writer.Write`'s atomic
temp+rename by construction (there is no longer a second read that could observe a different
on-disk version). The not-found error text/behavior on `os.ReadFile`'s `fs.ErrNotExist` is
unchanged.

Added `TestUnmarshalPolicy_RoundTrips` and `TestUnmarshalPolicy_MalformedYAML` in `pkg/output`,
mirroring `TestReadPolicyFile_RoundTrips`/`_MalformedYAML` at the parse-only level.

**Verification:** `go build ./...` clean; `go vet` clean; `pkg/output` (`TestReadPolicyFile_*`,
new `TestUnmarshalPolicy_*`) and `cmd/cpg` (`TestMCPQueryGetPolicy`) targeted tests pass; full
`pkg/output`, `cmd/cpg`, `pkg/hubble` suites pass.

### WR-04: The evidence/health path-derivation formula is duplicated across 5 sites with no single source of truth

**Files modified:** `pkg/session/paths.go` (new), `pkg/session/pipeline_config.go`,
`pkg/session/manager.go`, `cmd/cpg/mcp_query.go`, `cmd/cpg/mcp_query_evidence.go`,
`cmd/cpg/mcp_query_flows.go`
**Commit:** `d733cbb`
**Status:** fixed

**Applied fix:** Added `session.SessionPaths{OutputDir, EvidenceDir, OutputHash,
ClusterHealthPath}` and `session.DeriveSessionPaths(tmpDir) SessionPaths` (pure, no filesystem
access) exactly as REVIEW.md's Fix section suggested, and rewired all 5 cited sites to call it
instead of hand-copying `outputHash = HashOutputDir(join(tmpDir,"policies"))` /
`evidenceDir = join(tmpDir,"evidence")` / `healthPath = join(evidenceDir, outputHash,
"cluster-health.json")`: `buildPipelineConfig`, `Manager.Stop`, `handleGetClusterHealth`,
`handleGetEvidence`, and `handleListDroppedFlows` (the last of which also had a second, local
re-join of the same `cluster-health.json` path further down the function — replaced with
`paths.ClusterHealthPath` too, for full consistency within that file). Removed now-unused
`path/filepath` imports from `pkg/session/pipeline_config.go` and `cmd/cpg/mcp_query_evidence.go`,
and the now-unused `pkg/evidence` import from `pkg/session/manager.go`.

This is a behavior-preserving refactor: every call site now computes byte-identical values to
before (verified by the full existing test suite passing unchanged — no test assertions needed
updating).

**Verification:** `go build ./...` clean; `go vet ./...` clean; full `cmd/cpg`, `pkg/session`,
`pkg/hubble` suites pass (see note below on one `pkg/session` test needing isolation); full
repo suite `go test ./... -count=1 -race` passes clean (all 12 packages, including `pkg/session`
in the same combined run).

**Note on a `pkg/session` flake observed during iteration:** one combined run of
`cmd/cpg`+`pkg/session`+`pkg/hubble` together showed `TestManager_Start_ShutdownRacesSetup` fail
on an unscoped `/tmp/cpg-session-*` glob count (5 items vs. expected 4). This is a pre-documented,
pre-existing flake unrelated to this fix — see
`.planning/phases/18-query-tools/deferred-items.md` ("Recurrence: 3 `pkg/session` tmpdir-count
tests failed under `go test ./... -race -count=1`", root-caused there to a shared-`/tmp`
cross-package concurrency artifact, not a regression). Confirmed unrelated: `pkg/session` in
isolation passed cleanly 6/6 runs, and this fix touches only pure path-string derivation
(`DeriveSessionPaths`), never tmpdir creation/`Shutdown` timing. The final full-suite run above
passed clean with no recurrence.

## Skipped Issues

None — all 4 in-scope findings were fixed.

---

_Fixed: 2026-07-21T15:56:51Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
