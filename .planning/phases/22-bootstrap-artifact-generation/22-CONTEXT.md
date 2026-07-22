# Phase 22: Bootstrap Artifact Generation - Context

**Gathered:** 2026-07-22
**Status:** Ready for planning
**Mode:** Smart discuss (autonomous) — recommended answers auto-accepted per continuous-autonomy directive

<domain>
## Phase Boundary

Operators can generate a namespaced default-deny bootstrap artifact (`cpg bootstrap -n <ns>` + readonly MCP tool) that actually enforces default-deny once applied (cilium/cilium#35558 semantics), plus a runbook modeled on Cilium's "Creating Policies from Verdicts" that never suggests daemon-wide `policy-audit-mode`. Requirement: AUD-02. No cluster mutation, no new filesystem write call sites on the MCP path (SEC-01 stays intact).

</domain>

<decisions>
## Implementation Decisions

### CLI Command Surface
- New top-level command `cpg bootstrap` via `newBootstrapCmd()` registered in `cmd/cpg/main.go` alongside generate/replay/explain/mcp (matches roadmap literal `cpg bootstrap -n <ns>`).
- Output to stdout by default (pipe-friendly: `cpg bootstrap -n ns | kubectl apply -f -`); optional `-o/--output <file>` reusing the existing `pkg/output` atomic writer — same UX as generate.
- `-n/--namespace` is required with no default; refuse to run without it. The artifact is namespaced by definition — no cluster-wide variant exists on any code path.
- Single namespace per invocation. Multi-namespace loops are the operator's shell's job (YAGNI).

### Version Gating Behavior
- Below the Cilium 1.16 floor with a **determined** version: hard refusal with an actionable error naming the detected version and the floor. No legacy-form emission (simpler and safer of the two roadmap-sanctioned options).
- Reuse Phase 21 detection (`DetectCiliumVersion`, `featureFloors`) — add an `enableDefaultDeny` floor entry (1.16) to the existing table rather than a parallel mechanism.
- **Undetermined** version (no cluster reachable, detection failed): warn-and-proceed and emit the artifact with an explicit stderr warning — consistent with Phase 21's warn-and-proceed philosophy; offline/CI generation must keep working.
- The MCP tool applies the same gate and includes detected version/compat info in its result so the LLM can reason about applicability.

### Artifact Content
- CNP carries `spec.enableDefaultDeny: {ingress: true, egress: true}` AND explicit empty `ingress: []` / `egress: []` stanzas — both present, tested as a named acceptance criterion (cilium/cilium#35558 silent-no-op guard).
- `metadata.name: default-deny-<ns>`, `metadata.namespace: <ns>`; empty `endpointSelector: {}` (selects all endpoints in the namespace).
- Reuse `pkg/policy` types/marshaling if they can express `enableDefaultDeny`; otherwise a minimal dedicated builder — planner's discretion, but YAML output must go through the same marshal path as generate for byte-consistent style.

### MCP Tool + Runbook
- Tool name `get_bootstrap_policy`, `ReadOnlyHint: true`, returns the YAML as tool result content directly — zero new filesystem write call sites, zero SEC-01 allowlist entries (success criterion 4).
- Runbook as a repo markdown doc (docs/), phase order modeled 1:1 on Cilium's "Creating Policies from Verdicts", with a first-lines warning against daemon-wide `policy-audit-mode`; linked from README.
- Runbook's capture step references `cpg generate --include-audit` (Phase 20 surface).
- README compatibility table gains the bootstrap/`enableDefaultDeny` floor row, pinned by the existing `readme_compat_test.go` pattern.

### Claude's Discretion
- Exact error wording, flag help text, runbook file name/location, and internal package layout for the bootstrap builder.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/k8s/version.go` — `DetectCiliumVersion`, `featureFloors` table + `belowFloorFeatures` (Phase 21) for the 1.16 gate.
- `pkg/output/writer.go` — atomic file writer (SEC-02) for `-o` mode.
- `pkg/policy` — CNP builder/marshal conventions.
- `cmd/cpg/mcp_tools.go` — `mcp.AddTool` registration pattern with honest `ReadOnlyHint` annotations.
- `cmd/cpg/readme_compat_test.go` — README compat-table pinning pattern.

### Established Patterns
- One file per command in `cmd/cpg` (`generate.go`, `replay.go`, `explain.go`) with `newXCmd()` constructors registered in `main.go:57-60`.
- Warn-and-proceed on version-detection failure; hard behavior only on determined facts (Phase 21).
- MCP tools return content directly; write paths are structurally confined (SEC-01 proven in Phase 19).

### Integration Points
- `cmd/cpg/main.go` root command registration.
- `cmd/cpg/mcp_tools.go` tool registration (readonly section).
- README compatibility matrix + docs/ runbook.

</code_context>

<specifics>
## Specific Ideas

- Success criterion 1 must exist as a named test asserting BOTH `enableDefaultDeny` and the explicit empty stanzas survive marshaling (the cilium#35558 regression class).
- The runbook must never mention daemon-wide `policy-audit-mode` except in the warning against it.

</specifics>

<deferred>
## Deferred Ideas

- Managed audit window / per-endpoint audit flips — Phase 23 (AUD-03/AUD-04).
- Multi-namespace batch bootstrap — not requested; operator shell loops cover it.

</deferred>
