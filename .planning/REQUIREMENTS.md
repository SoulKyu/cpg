# Requirements: CPG — Cilium Policy Generator

**Defined:** 2026-07-20
**Milestone:** v1.5 MCP Integration
**Core Value:** Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.

## v1 Requirements

Requirements for milestone v1.5. Each maps to roadmap phases.

### MCP Server Core

- [ ] **SRV-01**: SRE can register cpg in an MCP harness (`cpg mcp`, stdio transport) and the initialize handshake succeeds with all tools listed
- [ ] **SRV-02**: stdout carries only JSON-RPC frames across a full session lifecycle — enforced by explicit wiring of every stdout-defaulting seam (`PipelineConfig.Stdout`, dry-run `diffOut`, cobra `SilenceUsage`/`SilenceErrors`) and verified by an automated stdout-purity test on an in-memory transport
- [ ] **SRV-03**: All server logs go to stderr through the existing zap logger; go-sdk internal logging is bridged into it via `zap/exp/zapslog` (one unified log stream)
- [ ] **SRV-04**: An end-to-end stdio integration test drives `initialize → start_session → get_status → each query tool → stop_session → exit` under `-race`, plus an ungraceful-disconnect variant proving port-forward and tmpdir are cleaned up within a bounded deadline

### Session Lifecycle

- [ ] **SESS-01**: LLM can `start_session` (namespace / all-namespaces + existing generate filters) and receives an opaque `session_id`; the capture pipeline runs in a background goroutine on a detached cancellable context, writing all artifacts to an ephemeral session tmpdir (`os.MkdirTemp`)
- [ ] **SESS-02**: Exactly one concurrent session: a second `start_session` while one is active is rejected with an actionable error naming the active `session_id` — never queued, never silently replaced
- [ ] **SESS-03**: LLM can `get_status(session_id)` and gets coarse state — capturing/stopped, elapsed time, artifact file counts on disk (no live pipeline counters in v1.5; documented behavior)
- [ ] **SESS-04**: LLM can `stop_session(session_id)`: pipeline context cancelled, artifacts finalized (cluster-health.json, session stats), final summary returned
- [ ] **SESS-05**: Transport termination for any reason (stdin EOF, harness crash) triggers full cleanup fan-out — cancel session context, close port-forward, remove tmpdir — each step with a bounded deadline so one wedged cleanup cannot block process exit
- [ ] **SESS-06**: Any session-scoped tool called with an unknown or stopped `session_id` returns a crisp "session not found or expired" error (SEP-2567 handle semantics)

### Query Tools

- [ ] **QRY-01**: LLM can `list_dropped_flows(session_id, …filters)` as a composed view over the existing capped evidence samples + aggregate health counts, paginated (`limit`/`cursor`/`total_count`/`has_more`); the tool description explicitly states it is a sampled/aggregated view, not a raw flow log
- [ ] **QRY-02**: LLM can `list_policies(session_id)` (metadata: name, workload, direction, rule counts) and `get_policy(session_id, name)` (full CNP YAML + absolute tmpdir path); reads are torn-read safe against the writing pipeline
- [ ] **QRY-03**: LLM can `get_evidence(session_id, …filters)` for per-rule flow attribution, reusing the promoted `pkg/explain` JSON renderer verbatim, paginated
- [ ] **QRY-04**: LLM can `get_cluster_health(session_id)`: passthrough of cluster-health.json including per-reason Cilium remediation URLs; while the session is still capturing it returns an explicit "available after stop_session" result (not an error)
- [ ] **QRY-05**: Every data-returning tool ships `structuredContent` + `outputSchema` (typed Go structs), truthful annotations (`readOnlyHint` etc.), a description that teaches the dropclass taxonomy (policy-actionable vs infra/transient), and `isError` errors with specific actionable text

### Security & Hardening

- [ ] **SEC-01**: Readonly guarantee is structural: the MCP composition root registers only read-path handlers — no K8s write verb reachable from `cpg mcp`, no filesystem write outside the session tmpdir — verified by an audit test, re-runnable for every future tool
- [ ] **SEC-02**: `pkg/output/writer.go` writes policy YAML atomically (temp+rename, same pattern as the evidence and health writers) — landed as an early standalone change before any query tool reads that directory
- [ ] **SEC-03**: README MCP section documents harness configuration (explicit `env` block: `KUBECONFIG`/`PATH`/`TMPDIR` — MCP hosts don't inherit the shell env), the secrets posture (HTTP paths/labels reach the LLM context; headers are never captured), and the exec-credential-plugin non-interactive hang caveat

## v2 Requirements

Deferred to future milestones. Tracked but not in current roadmap.

### MCP Enhancements

- **FLOW-01**: Dedicated flow-sample writer (4th pipeline tee target with its own FIFO cap) for a full-fidelity `list_dropped_flows` — fast-follow if the composed view proves insufficient
- **LIVE-01**: Live mid-session counters (periodic health flush or `*SessionStats` hook on `PipelineConfig`) for `get_status`/`get_cluster_health`
- **REDACT-01**: Best-effort redaction of HTTPPath query strings / token-like values before tool results reach the LLM

### Debt (carried from v1.4 audit)

- **LINT-01..03**: Lint debt zero — 16 errcheck + 10 SA1019 + drop CI `only-new-issues` flag
- **RELSEC-01..02**: Release hardening — `release.yml` minimal permissions, govulncheck job pinning follow-through
- **REPLAY-01**: `cpg replay` exit-code parity on truncated input

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Mutating `apply_policy` tool | Breaks the readonly guarantee outright; no `cpg apply` CLI exists to wrap; structural exclusion is the SEC-01 mechanism |
| MCP resources for policy YAML | Real client-support gap (Claude Code surfaces resources only via manual @mention); plain absolute tmpdir paths are strictly better for stdio same-filesystem deployment |
| Elicitation & sampling | Sampling would be a new transport for the AI-plausibility feature explicitly shelved 2026-04-25; elicitation's statefulness is disproportionate for a single-user local process |
| MCP prompt templates | No validated use case; tool descriptions carry the guidance instead |
| Progress notifications | Correlate to a blocking request; start/poll/stop design has no blocking call to attach them to |
| HTTP/SSE transport + OAuth | Spec scopes authorization to HTTP transports; v1.5 is stdio-only local |
| Multi-session capacity | Single concurrent session by design (flat memory profile); `session_id` schema stays forward-compatible if this ever changes |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| SRV-01 | Phase 19 | Pending |
| SRV-02 | Phase 16 | Pending |
| SRV-03 | Phase 16 | Pending |
| SRV-04 | Phase 19 | Pending |
| SESS-01 | Phase 17 | Pending |
| SESS-02 | Phase 17 | Pending |
| SESS-03 | Phase 17 | Pending |
| SESS-04 | Phase 17 | Pending |
| SESS-05 | Phase 17 | Pending |
| SESS-06 | Phase 17 | Pending |
| QRY-01 | Phase 18 | Pending |
| QRY-02 | Phase 18 | Pending |
| QRY-03 | Phase 18 | Pending |
| QRY-04 | Phase 18 | Pending |
| QRY-05 | Phase 18 | Pending |
| SEC-01 | Phase 19 | Pending |
| SEC-02 | Phase 16 | Pending |
| SEC-03 | Phase 19 | Pending |

**Coverage:**
- v1 requirements: 18 total
- Mapped to phases: 18
- Unmapped: 0 ✓

---
*Requirements defined: 2026-07-20*
*Last updated: 2026-07-20 after roadmap creation (Phases 16-19, full coverage)*
