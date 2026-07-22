# Phase 23: Managed Audit Window + SEC-01 Evolution - Context

**Gathered:** 2026-07-22
**Status:** Ready for planning
**Mode:** Smart discuss (autonomous) — ROADMAP decision gate resolved by the user via AskUserQuestion (2026-07-22): **AUD-03 surface = CLI-only (Variant B)**; SEC-01 two-mode mechanism decision (build-tag vs path-scoped) is therefore moot. Recorded in PROJECT.md Key Decisions.

<domain>
## Phase Boundary

Operators can open a supervised, lifecycle-bound audit window on a namespace via a new CLI command that cannot structurally be left open by accident: per-endpoint `PolicyAuditMode` flips over `pods/exec`, new-endpoint watcher, revert-only-ours bookkeeping keyed on `CiliumEndpoint` UID, revert riding the SESS-05 bounded cleanup fan-out on every exit path. The MCP server is untouched and stays pure-readonly — SEC-01's existing zero-tolerance proof remains literally unchanged, extended only by a tripwire proving the mutation stays unreachable from `runMCPServer`. Requirements: AUD-03, AUD-04.

</domain>

<decisions>
## Implementation Decisions

### Surface (USER-DECIDED at the ROADMAP gate — locked)
- CLI-only command (Variant B). No MCP tool, no MCP flag, no session property — zero diffs under `cmd/cpg/mcp*.go` except the SEC-01 tripwire test. The `pods/exec` privileged mutation stays a human act.
- Foreground, supervised command: `cpg audit-window -n <ns>` opens the window, watches, and reverts on EVERY exit (explicit Ctrl+C/SIGTERM, TTL expiry, transport death). A foreground process is the structural can't-be-left-open guarantee — no detached/daemon mode.
- `--ttl <duration>` with a sane default (e.g. 30m): the window is ALWAYS time-bounded; TTL expiry reverts and exits. Exact default at planner's discretion.

### Mutation Mechanism
- Per-endpoint `PolicyAuditMode` flips via `pods/exec` on the Cilium agent pod of each endpoint's node (`cilium-dbg endpoint config <id> PolicyAuditMode=Enabled` — binary name `cilium-dbg` vs `cilium` gated on the Phase 21 detected version, ≥1.15 = `cilium-dbg`).
- SPDY executor (`remotecommand.NewSPDYExecutor`, `StreamWithContext`) — matches current kubectl exec; WebSocket fallback is deferred (AUD-FUT-01).
- Never touch an endpoint already in audit before cpg started (revert-only-ours); bookkeeping keyed on `CiliumEndpoint` UID, never the raw per-node integer endpoint ID (ID reuse hazard — research pitfall).
- New-endpoint watcher: namespace-scoped `CiliumEndpoint` watch; flips new endpoints as they appear; the race window for brand-new endpoints (enforced before the flip lands) is documented honestly in the runbook, not papered over.

### Lifecycle & Cleanup
- Revert rides the exact SESS-05 bounded cleanup fan-out pattern (same shape as `pkg/session` Manager shutdown) — no parallel construct, no independent `time.AfterFunc` racing an explicit stop, per-endpoint revert success/failure tracked and reported (a revert sweep that can't say which endpoint failed is a non-starter).
- Precondition check: refuse to open a window if daemon-wide `policy-audit-mode` is already active — hard refusal naming the reason, not a silent proceed. Detection source at planner's discretion (cilium-config ConfigMap read preferred over exec if RBAC-clean).

### SEC-01 Evolution (Variant B interpretation of criterion 4)
- The existing `TestMCPAuditReadonlyReachability` stays byte-identical in what it proves: zero write verbs / zero unallowlisted fs writes reachable from `runMCPServer`. The mutating subtree lives in a sibling cobra command never reachable from that root.
- Add a tripwire assertion (same audit file or sibling): the exec-executor constructor (`remotecommand.NewSPDYExecutor` / the audit-window entry function) is NOT reachable from `runMCPServer`, and (positive path-scope) is reachable ONLY from the audit-window command entry point among cpg-owned roots — failing loudly if any other path grows one.
- RBAC step-up (`pods/exec` create, `ciliumendpoints` list/watch) documented in README + runbook as exclusive to `cpg audit-window` — readonly commands need none of it.

### README Honesty (criterion 5, Variant B form)
- MCP server section: "readonly, period" claim STAYS (it remains literally true — stronger than the ROADMAP's pre-decision wording anticipated).
- CLI section: states plainly that `cpg audit-window` is the one mutating command (scoped, lifecycle-bound, per-endpoint audit flips with guaranteed revert), everything else is readonly. Wording per ROADMAP intent: "readonly by default; scoped, lifecycle-bound mutations behind an explicit command".

### Claude's Discretion
- Exact TTL default/bounds, flag names beyond `-n`/`--ttl`, package layout (`pkg/auditwindow` vs under `pkg/k8s`), watcher implementation details, error wording, runbook integration (extend docs/bootstrap-runbook.md audit-window step vs separate doc).

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/k8s/portforward.go` — the existing SPDY `RESTClient().Post()...SubResource()` shape (port-forward) is the exec pattern's proven analog.
- `pkg/session/manager.go` — SESS-05 bounded shutdown fan-out (the cleanup pattern to ride), signal handling, idempotent stop.
- `pkg/k8s/version.go` — Phase 21 `DetectCiliumVersion` for the `cilium-dbg` vs `cilium` binary-name gate.
- `cmd/cpg/bootstrap.go` — newest single-purpose command constructor + `bootstrapDetectVersion` seam pattern for tests.
- `cmd/cpg/mcp_audit_test.go` — SSA/RTA audit harness + `callPathFrom` helper for the new tripwire assertion.

### Established Patterns
- Package-level function-var seams for cluster-touching calls so unit tests run with no live cluster.
- Hard refusal on determined dangerous state, warn-and-proceed only on undetermined (Phases 21/22).
- Conventional Commits, atomic per-task commits, table-driven tests with testify.

### Integration Points
- `cmd/cpg/main.go` command registration.
- `docs/bootstrap-runbook.md` — the runbook's audit-window step currently references the concept; wire the real command in.
- README RBAC + readonly-guarantee sections.

</code_context>

<specifics>
## Specific Ideas

- Success criterion 2 needs a test per exit path (explicit stop, SIGTERM, TTL expiry, transport death) proving revert fires — mirroring how Phase 17 tested SESS-03/05 exit paths with a fake/stubbed layer.
- The new-endpoint race (endpoint enforced before flip lands) is documented, not "solved" — no busy-loop hacks.
- Zero new go.mod deps expected: client-go `remotecommand` and apimachinery watch are already vendored (verify in research).

</specifics>

<deferred>
## Deferred Ideas

- MCP flag-gated audit window (Variant A) — future milestone if LLM-driven onboarding proves needed; requires the SEC-01 two-mode mechanism decision then.
- WebSocket exec with SPDY fallback (AUD-FUT-01).
- cpg-managed CNP apply/delete (AUD-FUT-02).

</deferred>
