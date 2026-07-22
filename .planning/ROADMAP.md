# Roadmap: CPG (Cilium Policy Generator)

## Overview

CPG delivers a Go CLI tool that turns Hubble dropped flows into ready-to-apply CiliumNetworkPolicies. v1.0 shipped the core live-streaming generator. v1.1 added an offline iteration workflow (`cpg replay`), per-rule flow evidence, `cpg explain`, and `--dry-run` with unified YAML diff. v1.2 extended generation to L7 (HTTP + DNS) with two-step workflow guidance. v1.3 closed the class of bug where infra-level Hubble drops generated bogus CNPs via a static classifier taxonomy. v1.4 landed audit-driven hardening from a Fable 5 full-code review — 29 confirmed findings fixed, two reachable vulnerabilities patched, and the CI pipeline running (green) for the first time. v1.5 exposes cpg as a readonly MCP stdio server so an LLM harness can run a live Hubble capture session, query dropped flows and generated policies, and review cluster health — cpg stays deterministic while the LLM brings the intelligence. v1.6 closes cpg's remaining onboarding gap — ingesting Cilium's AUDIT verdicts, generating a namespaced default-deny bootstrap artifact and runbook, and (pending an explicit surface decision) a lifecycle-bound managed audit window — while cpg-dedicated repo-local skills and an optional subagent put the v1.5 MCP surface to work for real LLM-driven workflows.

## Milestones

- ✅ **v1.0 MVP (Core Policy Generator)** — Phases 1-3 (shipped 2026-03-08) — [archive](milestones/v1.0-ROADMAP.md)
- ✅ **v1.1 Offline Replay & Policy Analysis** — Phases 4-6 (shipped 2026-04-24) — [archive](milestones/v1.1-ROADMAP.md)
- ✅ **v1.2 L7 Policies (HTTP + DNS)** — Phases 7-9 (shipped 2026-04-25) — [archive](milestones/v1.2-ROADMAP.md)
- ✅ **v1.3 Cluster Health Surfacing** — Phases 10-13 (shipped 2026-04-26) — [archive](milestones/v1.3-ROADMAP.md)
- ✅ **v1.4 Audit Fable5** — Phases 14-15 (shipped 2026-07-20) — [archive](milestones/v1.4-ROADMAP.md)
- ✅ **v1.5 MCP Integration** — Phases 16-19 (shipped 2026-07-22) — [archive](milestones/v1.5-ROADMAP.md)
- ✅ **v1.6 Audit-Mode Onboarding & cpg-Dedicated Agent Tooling** — Phases 20-24 (shipped 2026-07-22) — [archive](milestones/v1.6-ROADMAP.md)

## Phases

<details>
<summary>✅ v1.0 MVP (Phases 1-3) — SHIPPED 2026-03-08</summary>

- [x] Phase 1: Core Policy Engine (3/3 plans) — completed 2026-03-08
- [x] Phase 2: Hubble Streaming Pipeline (2/2 plans) — completed 2026-03-08
- [x] Phase 3: Production Hardening (2/2 plans) — completed 2026-03-08

Full details: [milestones/v1.0-ROADMAP.md](milestones/v1.0-ROADMAP.md)

</details>

<details>
<summary>✅ v1.1 Offline Replay & Policy Analysis (Phases 4-6) — SHIPPED 2026-04-24</summary>

- [x] Phase 4: Offline Replay Core (1/1 plan) — completed 2026-04-24
- [x] Phase 5: Dry-Run & Pipeline Integration (1/1 plan) — completed 2026-04-24
- [x] Phase 6: Explain Command (1/1 plan) — completed 2026-04-24

Full details: [milestones/v1.1-ROADMAP.md](milestones/v1.1-ROADMAP.md)

</details>

<details>
<summary>✅ v1.2 L7 Policies — HTTP + DNS (Phases 7-9) — SHIPPED 2026-04-25</summary>

- [x] Phase 7: L7 Infrastructure Prep (4/4 plans) — completed 2026-04-25
- [x] Phase 8: HTTP L7 Generation (4/4 plans) — completed 2026-04-25
- [x] Phase 9: DNS L7 Generation + explain L7 + Docs (4/4 plans) — completed 2026-04-25

Full details: [milestones/v1.2-ROADMAP.md](milestones/v1.2-ROADMAP.md)

</details>

<details>
<summary>✅ v1.3 Cluster Health Surfacing (Phases 10-13) — SHIPPED 2026-04-26</summary>

- [x] Phase 10: Classifier Core (2/2 plans) — completed 2026-04-26
- [x] Phase 11: Aggregator Suppression + Health Writer (2/2 plans) — completed 2026-04-26
- [x] Phase 12: Session Summary Block (1/1 plan) — completed 2026-04-26
- [x] Phase 13: Flags + Exit Code (3/3 plans) — completed 2026-04-26

Full details: [milestones/v1.3-ROADMAP.md](milestones/v1.3-ROADMAP.md)

</details>

<details>
<summary>✅ v1.4 Audit Fable5 (Phases 14-15) — SHIPPED 2026-07-20</summary>

- [x] Phase 14: Fix Verification + Quality Gates (direct multi-agent workflow) — completed 2026-07-20
- [x] Phase 15: CI Trigger Fix + PR Delivery (PR #16, merged) — completed 2026-07-20

Full details: [milestones/v1.4-ROADMAP.md](milestones/v1.4-ROADMAP.md)

</details>

<details>
<summary>✅ v1.5 MCP Integration (Phases 16-19) — SHIPPED 2026-07-22</summary>

- [x] Phase 16: MCP Server Foundation & Write Safety (3/3 plans) — completed 2026-07-20
- [x] Phase 17: Session Lifecycle (9/9 plans) — completed 2026-07-21
- [x] Phase 18: Query Tools (5/5 plans) — completed 2026-07-21
- [x] Phase 19: Security Hardening & End-to-End Validation (4/4 plans) — completed 2026-07-21

Full details: [milestones/v1.5-ROADMAP.md](milestones/v1.5-ROADMAP.md)

</details>

<details>
<summary>✅ v1.6 Audit-Mode Onboarding & cpg-Dedicated Agent Tooling (Phases 20-24) — SHIPPED 2026-07-22</summary>

- [x] Phase 20: `--include-audit` Verdict Ingestion (4/4 plans) — completed 2026-07-22
- [x] Phase 21: Cilium Compatibility Matrix + Runtime Detection (4/4 plans) — completed 2026-07-22
- [x] Phase 22: Bootstrap Artifact Generation (3/3 plans) — completed 2026-07-22
- [x] Phase 23: Managed Audit Window + SEC-01 Evolution (4/4 plans, CLI-only per gate decision) — completed 2026-07-22
- [x] Phase 24: cpg-Dedicated Skills & Agent Tooling (2/2 plans) — completed 2026-07-22

Full details: [milestones/v1.6-ROADMAP.md](milestones/v1.6-ROADMAP.md)

</details>

## Phase Details

### Phase 16: MCP Server Foundation & Write Safety

**Goal**: `cpg mcp` runs as a protocol-safe stdio process — pure JSON-RPC on stdout, unified stderr logging — and the on-disk policy writer is torn-read safe, before any session or query tool is built on top of it
**Depends on**: Nothing (first phase of v1.5, builds on the v1.4 codebase)
**Requirements**: SRV-02, SRV-03, SEC-02
**Success Criteria** (what must be TRUE):

  1. Across a full simulated session on an in-memory transport, every byte written to stdout parses as a valid JSON-RPC frame — verified by an automated stdout-purity test that exercises every stdout-defaulting seam (`PipelineConfig.Stdout`, dry-run `diffOut`, cobra `SilenceUsage`/`SilenceErrors`)
  2. All server-side log output — cpg's own zap logs and the go-sdk's internal logs bridged via `zap/exp/zapslog` — appears on stderr only, as one unified stream
  3. `pkg/output/writer.go` writes policy YAML via temp+rename (matching the evidence and health writers' existing pattern), so a concurrent reader can never observe a partial or corrupt file

**Plans**: 3 plans in 2 waves

**Wave 1**

- [x] 16-01-PLAN.md — SEC-02 atomic policy writer (temp+rename, chmod 0644)
- [x] 16-02-PLAN.md — go-sdk v1.6.1 dependency legitimacy gate + install

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 16-03-PLAN.md — cpg mcp stdio server skeleton + stdout-purity & logging tests

### Phase 17: Session Lifecycle

**Goal**: An LLM can start, monitor, and stop a live Hubble capture session through MCP tools, with the process robustly cleaning up on every exit path
**Depends on**: Phase 16
**Requirements**: SESS-01, SESS-02, SESS-03, SESS-04, SESS-05, SESS-06
**Success Criteria** (what must be TRUE):

  1. LLM calls `start_session` with namespace/filter arguments and receives an opaque `session_id`; the capture pipeline runs in a background goroutine on a detached cancellable context, writing artifacts to an ephemeral `os.MkdirTemp` tmpdir — and a second `start_session` while one is active is rejected with an actionable error naming the active session, never queued or silently replaced
  2. LLM calls `get_status(session_id)` at any point and receives coarse state — capturing/stopped, elapsed time, artifact file counts on disk
  3. LLM calls `stop_session(session_id)` and the pipeline context is cancelled, artifacts are finalized (`cluster-health.json`, session stats), and a final summary is returned
  4. Killing the transport for any reason (stdin EOF, harness crash) during an active session cancels the session context, closes the port-forward, and removes the tmpdir — each step bounded by its own deadline so one wedged cleanup cannot block process exit
  5. Any session-scoped tool called with an unknown, purged, or replaced `session_id` returns a crisp "session not found or expired" error, never a generic failure — a retained STOPPED session stays queryable (`get_status`/`stop_session` succeed against it, per D-02) and does not trigger this error

**Plans**: 4 plans in 4 waves (linear — each layer consumes the prior)

**Wave 1**

- [x] 17-01-PLAN.md — pkg/hubble OnFinal end-of-run stats hook (D-08); the one additive pipeline change

**Wave 2** *(blocked on Wave 1)*

- [x] 17-02-PLAN.md — pkg/session data model + buildPipelineConfig recipe (state/results, tmpdir-scoped config, zero-duration crash guard)

**Wave 3** *(blocked on Wave 2)*

- [x] 17-03-PLAN.md — pkg/session Manager: single-active state machine, retention, idempotent stop, bounded shutdown fan-out (SESS-01..06, D-01..04)

**Wave 4** *(blocked on Wave 3)*

- [x] 17-04-PLAN.md — cmd/cpg session tools (start_session/get_status/stop_session) + composition wiring + in-memory integration tests

**Gap Closure (post-verification):** 17-05..17-07 closed the prior round (WR-01 old class, WR-02/03 old, D-02 docs). 17-VERIFICATION.md (2026-07-21, 4/5) reopened SESS-03 narrowly and flagged an SESS-04/D-03 contract regression:

- [x] 17-08-PLAN.md — WR-01 (scoped dial-timeout crash classification, SESS-03) + WR-02 (already_stopped reflects explicit stop, D-03/SESS-04)

The re-verification after 17-08 (2026-07-21, 4/5) closed both of those but reopened SESS-03 a third time, narrower still — the landed guard only fires for a non-nil error, so a CLEAN nil pipeline exit (relay io.EOF / the closedFlowSource fixture) still wedges the session at "capturing" forever and blocks the single slot:

- [x] 17-09-PLAN.md — WR-01 this round (clean/nil-exit autonomous stop, SESS-03 / Truth 2): broaden the guard to fire on any exit while sessionCtx.Err()==nil, keeping error-surfacing conditional on err!=nil

### Phase 18: Query Tools

**Goal**: An LLM can read a session's dropped flows, generated policies, per-rule evidence, and cluster health as safe, well-described, paginated MCP tool results
**Depends on**: Phase 17
**Requirements**: QRY-01, QRY-02, QRY-03, QRY-04, QRY-05
**Success Criteria** (what must be TRUE):

  1. LLM calls `list_dropped_flows(session_id, …filters)` and receives a paginated (`limit`/`cursor`/`total_count`/`has_more`) composed view over the capped evidence samples and aggregate health counts, with the tool description explicit that it is a sampled/aggregated view, not a raw flow log
  2. LLM calls `list_policies(session_id)` for policy metadata (name, workload, direction, rule counts) and `get_policy(session_id, name)` for full CNP YAML plus its absolute tmpdir path, both returning consistent data even while the pipeline is actively writing
  3. LLM calls `get_evidence(session_id, …filters)` and receives paginated per-rule flow attribution identical to `cpg explain --output json`, via the promoted `pkg/explain` renderer
  4. LLM calls `get_cluster_health(session_id)` and receives the finalized report (including per-reason Cilium remediation URLs) once stopped, or an explicit non-error "available after stop_session" result while still capturing
  5. Every one of these tools ships `structuredContent` + `outputSchema`, truthful annotations (`readOnlyHint` etc.), a description that teaches the dropclass taxonomy (policy-actionable vs infra/transient), and `isError` errors with specific, actionable text on failure

**Plans**: 5 plans in 4 waves

**Wave 1** *(read-side foundations — parallel, zero file overlap)*

- [x] 18-01-PLAN.md — pkg/explain promotion out of cmd/cpg (QRY-03 shared-renderer foundation)
- [x] 18-02-PLAN.md — reader exports: output.ReadPolicyFile + hubble.ReadClusterHealth/types + finalize-on-error test (QRY-02/QRY-04 foundations)

**Wave 2** *(blocked on Wave 1)*

- [x] 18-03-PLAN.md — query composition root + list_policies/get_policy (QRY-02) + get_cluster_health 3-way branch (QRY-04)

**Wave 3** *(blocked on Wave 2)*

- [x] 18-04-PLAN.md — pagination + enum-schema infra (mustQuerySchema, opaque cursor) + get_evidence (QRY-03)

**Wave 4** *(blocked on Wave 3)*

- [x] 18-05-PLAN.md — list_dropped_flows composed view (QRY-01) + all-8-tools integration test (QRY-05)

### Phase 19: Security Hardening & End-to-End Validation

**Goal**: The readonly guarantee is structurally proven and documented, and the complete session lifecycle is verified end-to-end under race detection
**Depends on**: Phase 18
**Requirements**: SRV-01, SRV-04, SEC-01, SEC-03
**Success Criteria** (what must be TRUE):

  1. An SRE registers `cpg mcp` (stdio transport) in an MCP harness and the initialize handshake succeeds, listing all 8 tools (`start_session`, `get_status`, `stop_session`, `list_dropped_flows`, `list_policies`, `get_policy`, `get_evidence`, `get_cluster_health`) with correct schemas
  2. An audit test proves no K8s write verb and no filesystem write outside the session tmpdir is reachable from the MCP composition root — re-runnable for every future tool addition
  3. An end-to-end stdio integration test drives `initialize → start_session → get_status → each query tool → stop_session → exit` under `-race`, plus an ungraceful-disconnect variant proving the port-forward and tmpdir are cleaned up within a bounded deadline
  4. README's MCP section documents harness `env` configuration (`KUBECONFIG`/`PATH`/`TMPDIR`), the secrets posture (HTTP paths/labels reach the LLM context, headers never captured), and the exec-credential-plugin non-interactive hang caveat, so an SRE can configure a harness correctly on first try

**Plans**: 4 plans in 2 waves

**Wave 1** *(parallel — zero file overlap)*

- [x] 19-01-PLAN.md — SEC-01 structural readonly audit (RTA reachability + BFS cpg-owned filter + direct-call scan + 5-function allowlist) + `golang.org/x/tools` promotion
- [x] 19-02-PLAN.md — SRV-04/SRV-01 e2e infra (fake Hubble relay + `-race` subprocess harness) + graceful lifecycle + handshake/schema proof (byte-pure stdout)
- [x] 19-03-PLAN.md — SEC-03 README `## MCP Server (cpg mcp)` section (harness env, secrets posture, exec-credential caveat)

**Wave 2** *(blocked on 19-02 — same test file)*

- [x] 19-04-PLAN.md — SRV-04 ungraceful-disconnect variant (bounded self-exit + tmpdir removal + relay stream cancel)

Archived per-phase details for shipped milestones live in [milestones/](milestones/).

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Core Policy Engine | v1.0 | 3/3 | Complete | 2026-03-08 |
| 2. Hubble Streaming Pipeline | v1.0 | 2/2 | Complete | 2026-03-08 |
| 3. Production Hardening | v1.0 | 2/2 | Complete | 2026-03-08 |
| 4. Offline Replay Core | v1.1 | 1/1 | Complete | 2026-04-24 |
| 5. Dry-Run & Pipeline Integration | v1.1 | 1/1 | Complete | 2026-04-24 |
| 6. Explain Command | v1.1 | 1/1 | Complete | 2026-04-24 |
| 7. L7 Infrastructure Prep | v1.2 | 4/4 | Complete | 2026-04-25 |
| 8. HTTP L7 Generation | v1.2 | 4/4 | Complete | 2026-04-25 |
| 9. DNS L7 Generation + explain L7 + Docs | v1.2 | 4/4 | Complete | 2026-04-25 |
| 10. Classifier Core | v1.3 | 2/2 | Complete | 2026-04-26 |
| 11. Aggregator Suppression + Health Writer | v1.3 | 2/2 | Complete | 2026-04-26 |
| 12. Session Summary Block | v1.3 | 1/1 | Complete | 2026-04-26 |
| 13. Flags + Exit Code | v1.3 | 3/3 | Complete | 2026-04-26 |
| 14. Fix Verification + Quality Gates | v1.4 | n/a (direct workflow) | Complete | 2026-07-20 |
| 15. CI Trigger Fix + PR Delivery | v1.4 | n/a (direct workflow) | Complete | 2026-07-20 |
| 16. MCP Server Foundation & Write Safety | v1.5 | 3/3 | Complete    | 2026-07-20 |
| 17. Session Lifecycle | v1.5 | 9/9 | Complete    | 2026-07-21 |
| 18. Query Tools | v1.5 | 5/5 | Complete    | 2026-07-21 |
| 19. Security Hardening & End-to-End Validation | v1.5 | 4/4 | Complete    | 2026-07-21 |
| 20. `--include-audit` Verdict Ingestion | v1.6 | 4/4 | Complete    | 2026-07-22 |
| 21. Cilium Compatibility Matrix + Runtime Detection | v1.6 | 4/4 | Complete   | 2026-07-22 |
| 22. Bootstrap Artifact Generation | v1.6 | 1/3 | In Progress | - |
| 23. Managed Audit Window + SEC-01 Evolution | v1.6 | 2/4 | In Progress|  |
| 24. cpg-Dedicated Skills & Agent Tooling | v1.6 | 0/TBD | Not started | - |

**Milestone status:** v1.0 ✅ shipped · v1.1 ✅ shipped · v1.2 ✅ shipped · v1.3 ✅ shipped · v1.4 ✅ shipped · v1.5 ✅ shipped · v1.6 📋 in progress
