# Phase 17: Session Lifecycle - Context

**Gathered:** 2026-07-20
**Status:** Ready for planning

<domain>
## Phase Boundary

An LLM can start, monitor, and stop a live Hubble capture session through 3 MCP tools — `start_session` / `get_status` / `stop_session` — registered in `cmd/cpg/mcp.go`'s composition root. New `pkg/session` package (`SessionManager`) wraps `hubble.RunPipeline` launched in a background goroutine on a detached cancellable context, writing all artifacts to an ephemeral `os.MkdirTemp` session tmpdir, with full bounded-deadline cleanup on every exit path (stop, transport death, SIGTERM). Requirements: SESS-01..06.

</domain>

<decisions>
## Implementation Decisions

### Session state machine & post-stop retention (resolves the SESS-06 ↔ QRY-04 tension)
- **D-01:** State machine is `capturing → stopped → gone`. `stop_session` finalizes artifacts and **RETAINS** the tmpdir — removal happens at the **next `start_session`** or at **server shutdown**, never at stop. A stopped session stays queryable: `get_status` now, Phase 18 query tools read policies/evidence/cluster-health cold. This supersedes PROJECT.md's "tmpdir cleaned at stop_session" wording.
- **D-02:** SESS-06's "session not found or expired" applies to **unknown IDs and replaced/purged sessions only** — NOT to the retained stopped session. Binding interpretation for Phase 18: QRY-04's "available after stop_session" works naturally against the retained tmpdir.
- **D-03:** `stop_session` is **idempotent**: a second stop returns the same final summary with an "already stopped" marker, never `isError`. Harness retries after timeouts must not surface parasitic failures.
- **D-04:** `start_session` while a stopped session is retained: **silent purge + note** — the new start succeeds, removes the old tmpdir, and the response notes "previous session sess_X discarded". The old ID becomes "not found or expired". SESS-02's rejection applies only to an ACTIVE (capturing) session.

### start_session argument surface (resolves SESS-01's "existing generate filters")
- **D-05:** Exposed args: `namespace`/`all_namespaces` (SESS-01), `l7`, `ignore_drop_reasons`, `ignore_protocols`, `server`, `tls`, `timeout`, `cluster_dedup` (default false), `flush_interval`. Evidence caps stay at binary defaults. Excluded as nonsensical in MCP mode: `--dry-run`, `--no-evidence`, `--output-dir`, `--fail-on-infra-drops`.
- **D-06:** Reuse the existing CLI validations verbatim (`validateIgnoreDropReasons`, `validateIgnoreProtocols` in `cmd/cpg/commonflags.go`) — already tested, same normalization (UPPERCASE reasons, lowercase protocols).
- **D-07:** `server` bypasses the auto port-forward (same semantics as the CLI flag). Side benefit: Phase 19's SRV-04 e2e test can point `start_session` at a local fake gRPC server without kubeconfig. `timeout` (default 10s) bounds connection establishment so a bad relay makes `start_session` fail fast with an actionable `isError`.

### stop_session final summary
- **D-08:** Complete `SessionStats` reach the session manager via a **small additive nil-safe hook on `PipelineConfig`** (e.g. `OnFinal func(SessionStats)`), fired exactly once after `g.Wait()` when stats are fully populated; nil = no-op, CLI paths untouched. This is NOT LIVE-01 (zero mid-session counters — end-of-run only). Rationale: `cluster-health.json` only persists `flows_seen`/`infra_drops_total`/`started`/`ended` — `PoliciesWritten/Skipped/Failed`, `LostEvents`, and L7 counts are otherwise unrecoverable from disk.
- **D-09:** stop_session result = **typed `structuredContent` only** (SessionStats-derived struct + absolute `cluster-health.json` path + tmpdir path). The human `PrintClusterHealthSummary` block stays on **stderr** via `mcpModeStdout()` — Phase 16's handoff honored literally, no per-session buffer seam.

### session_id
- **D-10:** MCP `session_id` = opaque **`sess_<uuid>`**, mapped by the Manager to the session. The internal evidence `SessionID` keeps its existing `RFC3339-uuid4` format (evidence schema v2 untouched). The start log line carries both IDs for correlation.

### Claude's Discretion
- Exact SESS-05 per-step cleanup deadline values (bounded, one wedged step never blocks exit — pick sensible constants).
- `get_status` artifact file-count semantics (which files counted: policy YAML, evidence, health) and response field names.
- `pkg/session` file layout and Manager API shape (research Pattern 1 is the reference).
- Port-forward/kubeconfig failure error texts at `start_session` (distinct, actionable, per PITFALLS Pitfall 8).
- Field names of the stop-summary struct (`outputSchema` discipline arrives formally with QRY-05 in Phase 18; stay consistent with it).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Milestone research (v1.5 MCP)
- `.planning/research/SUMMARY.md` — Tension 1 (single session + opaque `session_id` compose), Tension 3 (no live counters — status/health shape), phase mapping
- `.planning/research/ARCHITECTURE.md` — Pattern 1 (session manager wraps `RunPipeline` unchanged; stop = ctx cancel + bounded wait on `done chan`), data flows 1–4, `pkg/session` component contract
- `.planning/research/PITFALLS.md` — Pitfall 2 (never block a tool handler on the pipeline), Pitfall 3 (orphaned sessions on harness death), Pitfall 8 (kubeconfig bounded-timeout + env)
- `.planning/research/STACK.md` — go-sdk v1.6.1 tool-registration API

### Planning
- `.planning/REQUIREMENTS.md` §Session Lifecycle — SESS-01..06 exact wording
- `.planning/ROADMAP.md` — Phase 17 goal + 5 success criteria
- `.planning/phases/16-mcp-server-foundation-write-safety/16-CONTEXT.md` — D-01..D-06 stdout discipline; the `mcpModeStdout()` handoff this phase completes

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `cmd/cpg/mcp.go:119` `mcpModeStdout()` — Phase 16 handoff: the first MCP-mode `PipelineConfig` MUST set `Stdout = mcpModeStdout()` (contract-pinned by `TestMCPModeStdoutNeverDefaultsToRealStdout`)
- `cmd/cpg/generate.go:145-256` `runGenerate` — the exact `PipelineConfig` construction recipe (port-forward, cluster-dedup snapshot load, evidence dir/hash/session fields) the session manager replicates
- `pkg/k8s.PortForwardToRelay` (used at `cmd/cpg/generate.go:174`) — per-session port-forward; returns `localAddr` + `cleanup` func, unchanged
- `cmd/cpg/commonflags.go` `validateIgnoreDropReasons` / `validateIgnoreProtocols` — arg validation reused verbatim (D-06)
- `cmd/cpg/mcp_harness_test.go` — in-memory-transport stdout-purity harness (Phase 16 D-04); extend with session-tool scenarios

### Established Patterns
- ctx-cancel shutdown already shipped and exercised (Ctrl+C on `cpg generate` via `signal.NotifyContext`) — `stop_session` reuses this path (research Pattern 1, near-zero new risk)
- All three writers (policy since Phase 16, evidence, health) are atomic temp+rename — tmpdir reads are torn-safe for the retained-session model
- 484 tests all run `-race` — `pkg/session` (highest-concurrency-risk piece of v1.5) follows suit, unit-tested with a fake `FlowSource`, no real cluster, no MCP SDK

### Integration Points
- `pkg/hubble/pipeline.go:93` `PipelineConfig.Stdout` + the new nil-safe final-stats hook (D-08) — the ONLY `pkg/hubble` change this phase
- `cmd/cpg/mcp.go` `runMCPServer` — where the 3 session tools register (composition root; readonly discipline from Phase 16 continues: session tools touch only the session tmpdir + K8s read/port-forward verbs)
- `RunPipeline`'s return error surfaces through the session's `done chan`; `hubble.ExitCodeError` is unreachable in MCP mode (`FailOnInfraDrops` not exposed, D-05) — plain error semantics
- Session ctx must be a child of the server's `signal.NotifyContext` ctx (SIGTERM cancels any active session), yet detached from the tool-call request ctx (SESS-01 "detached cancellable context")

</code_context>

<specifics>
## Specific Ideas

- start_session response wording when replacing a retained session: "previous session sess_X discarded" (D-04).
- Second stop_session response carries an explicit "already stopped" marker alongside the same summary (D-03).
- The start_session log line correlates `sess_<uuid>` ↔ internal evidence SessionID (D-10).

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope. (LIVE-01 live counters, FLOW-01 flow-sample writer, REDACT-01 redaction were already tracked as v2 requirements before this discussion.)

</deferred>

---

*Phase: 17-Session Lifecycle*
*Context gathered: 2026-07-20*
