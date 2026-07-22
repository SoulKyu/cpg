---
gsd_state_version: 1.0
milestone: v1.6
milestone_name: Audit-Mode Onboarding & cpg-Dedicated Agent Tooling
status: planning
last_updated: "2026-07-22T09:30:00.000Z"
last_activity: 2026-07-22
progress:
  total_phases: 5
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-22)

**Core value:** Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.
**Current focus:** Phase 20 — `--include-audit` Verdict Ingestion

## Current Position

Phase: 20 of 24 (`--include-audit` Verdict Ingestion)
Plan: — (not yet planned)
Status: Ready to plan
Last activity: 2026-07-22 — ROADMAP.md created for v1.6 (Phases 20-24), 13/13 requirements mapped

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity (cumulative):**

- Total plans completed: 51 (across 19 phases, 6 milestones; v1.4 executed via direct workflow, no plans)
- Total tests: 610 across 12 packages

**By Milestone:**

| Milestone | Phases | Plans | Tests at close |
|-----------|--------|-------|----------------|
| v1.0 | 1-3 | 7 | ~80 |
| v1.1 | 4-6 | 3 | 180 |
| v1.2 | 7-9 | 12 | 319 |
| v1.3 | 10-13 | 8 | 418 |
| v1.4 | 14-15 | 0 (direct workflow) | 484 |
| v1.5 | 16-19 | 21 | 610 |
| v1.6 | 20-24 | TBD (planning not started) | - |

*Updated after each plan completion.*
| Phase 10-classifier-core P01 | 4 | 2 tasks | 5 files |
| Phase 10-classifier-core P02 | 3 | 2 tasks | 3 files |
| Phase 11-aggregator-suppression-and-health-writer P01 | 8 | 2 tasks | 3 files |
| Phase 11-aggregator-suppression-and-health-writer P02 | 3 | 2 tasks | 3 files |
| Phase 12-session-summary-block P01 | 146 | 2 tasks | 4 files |
| Phase 13-flags-and-exit-code P01 | 8 | 2 tasks | 2 files |
| Phase 13-flags-and-exit-code P02 | 8 | 2 tasks | 5 files |
| Phase 13-flags-and-exit-code P03 | 146 | 2 tasks | 4 files |
| Phase 17 P08 | ~13min | 2 tasks | 3 files |

## Accumulated Context

### Decisions

Decisions logged in PROJECT.md Key Decisions table.

- [Phase 10-classifier-core]: O(1) map[flowpb.DropReason]DropClass lookup (not switch) for Classify(); fallback DropClassUnknown NEVER Policy
- [Phase 10-classifier-core]: sync.Map.LoadOrStore for dedup in Classify(): zero-alloc on hot path for already-seen unknown values
- [Phase 11-aggregator-suppression-and-health-writer]: Verdict==DROPPED guard in classification gate prevents false suppression of zero-Verdict test/forwarded flows (protobuf zero-value semantics)
- [Phase 11-aggregator-suppression-and-health-writer]: pipeline.go passes nil healthCh (temporary) until plan 11-02 creates real healthWriter channel
- [Phase 11-aggregator-suppression-and-health-writer]: hw nil-gate mirrors ew: cfg.EvidenceEnabled && !cfg.DryRun ensures health writer co-located with evidence writer
- [Phase 11-aggregator-suppression-and-health-writer]: drops sorted by flowpb.DropReason_name for deterministic cluster-health.json output
- [Phase 12-session-summary-block]: PrintClusterHealthSummary takes io.Writer for testability; nil Stdout defaults to os.Stdout at call site in pipeline.go
- [Phase 12-session-summary-block]: Snapshot() nil-safe method on healthWriter returns shallow copy of drops — avoids re-reading cluster-health.json
- [Phase 13-flags-and-exit-code]: SetIgnoreDropReasons normalises to UPPERCASE (canonical flowpb enum form); FILTER-01 inserted before ignoreProtocols in Run() for correct filter precedence
- [Phase 13-flags-and-exit-code]: validateIgnoreDropReasons accepts *zap.Logger for inline FILTER-03 WARN emission; dropClassLabel() local helper avoids exporting String() from pkg/dropclass
- [Phase 13-flags-and-exit-code]: FailOnInfraDrops stored in PipelineConfig but exit logic not yet implemented (plan 13-03)
- [Phase 13-flags-and-exit-code]: ExitCodeError defined in pkg/hubble to avoid import cycle; shouldExitForInfraDrops pure helper; errors.As in main.go; exit code 1 only (not 2)
- [v1.5 roadmap]: SEC-02 (atomic policy writer) pulled into Phase 16 (first phase) — must land before any query tool reads `pkg/output`'s directory (Phase 18)
- [v1.5 roadmap]: SRV-01 (full tool-list handshake) and SRV-04 (e2e lifecycle test) both close Phase 19 rather than SRV-01 sitting in the skeleton phase — "all tools listed" only becomes true once every tool from Phases 16-18 is registered
- [v1.5 roadmap]: Research's 6-phase proposal consolidated to 4 (coarse granularity) — Read-Side Foundations folded into Query Tools (Phase 18); Security Hardening + E2E Validation merged into one closing phase (Phase 19)
- [Phase 16]: Phase 17 handoff: MCP-mode PipelineConfig.Stdout MUST use mcpModeStdout()
- [Phase 17-session-lifecycle]: WR-01 crash classifier uses sessionCtx.Err() == nil (not errors.Is on the returned error) — the only true 'cancelled on purpose' signal, so it subsumes any future scoped-timeout shape a pipeline dependency introduces, not just client.go's dial timeout
- [Phase 17-session-lifecycle]: s.cancel() releases sessionCtx on the autonomous-exit path, placed inside the existing genuine-failure guard (not a separate step) — idempotent and safe w.r.t. Start's context.AfterFunc(sessionCtx, setupCancel), already un-registered by then
- [Phase 17-session-lifecycle]: WR-02: Session.explicitStopSeen atomic.Bool decouples 'was Stop() already called' from 'is State == StateStopped' — same per-session primitive placement as cancel/done/stopOnce, keeps Manager stateless across sessions
- [Phase 17-session-lifecycle]: explicitStopSeen.Swap(true) applied at BOTH of Stop's buildSummary call sites (the state==StateStopped early-return AND the post-stopOnce path) — the early-return is exactly the path a first-post-crash Stop() takes, so it needs the same already_stopped semantics
- [v1.6 roadmap]: Phase 21 (COMPAT-01/02/03) sequenced before Phase 22 (AUD-02) on ARCHITECTURE.md's technical-dependency read — AUD-02 needs COMPAT-02's version capability gate for correct `enableDefaultDeny` emission; overrides FEATURES.md's priority-tier grouping, which had no code-level blocker forcing a later placement
- [v1.6 roadmap]: Phase 23 (AUD-03/AUD-04) cannot be planned at file-level detail until `/gsd-discuss-phase` resolves (a) the surface decision — MCP flag-gated session property vs. CLI-only command — and (b), if MCP wins, the SEC-01 two-mode mechanism (build-tag split vs. path-scoped reachability assertion); both must land as recorded PROJECT.md Key Decisions before any audit-window mutation code is written
- [v1.6 roadmap]: Research's 5-phase proposal adopted as-is (coarse granularity, 3-5 typical) — Phases 20/21 kept independent/parallelizable per both ARCHITECTURE.md and FEATURES.md; Phase 24 (SKL-01..06) sequenced last though most skills have no technical dependency forcing that position (scheduling flexibility noted, not a fixed constraint)

### Pending Todos

None.

### Blockers/Concerns

Phase 23 (AUD-03/AUD-04) is blocked on an explicit `/gsd-discuss-phase` decision before it can be planned at file-level detail: AUD-03's surface (MCP flag-gated session property vs. CLI-only command, MCP staying pure-readonly) and, if the MCP variant wins, the SEC-01 two-mode mechanism (build-tag split vs. path-scoped reachability assertion) — see ROADMAP.md's decision-gate note and research/SUMMARY.md Tension 4. Does not block Phases 20, 21, 22, or 24.

v1.3 deferred items (L7-FUT-01, DNS-FUT-02, etc.) tracked in PROJECT.md Planned section. v1.4 lint debt (LINT-01..03) and release hardening (RELSEC-01..02) deliberately descoped from v1.5 — remain tracked in PROJECT.md Planned, not yet claimed by v1.6.

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260426-pa5 | ignore-protocol flag (cpg generate + replay) | 2026-04-26 | 8f33122 | [260426-pa5-ignore-protocol-flag-cpg-generate-replay](./quick/260426-pa5-ignore-protocol-flag-cpg-generate-replay/) |
| 260427-aml | v1.3 code-review fixes (16 fixes, C1-C3, I1-I8, M1-M7) | 2026-04-27 | e3b3e77 | [260427-aml-v1-3-code-review-fixes](./quick/260427-aml-v1-3-code-review-fixes/) |
| 260427-bp7 | v1.3 second-pass review fixes (12 fixes, C1-C2, I1-I9, M1+M3) | 2026-04-27 | 42f0f57 | [260427-bp7-v1-3-second-pass-review-fixes](./quick/260427-bp7-v1-3-second-pass-review-fixes/) |

## Deferred Items

Items acknowledged and deferred at milestone close on 2026-07-20 (v1.4); re-acknowledged unchanged at v1.5 close on 2026-07-22:

| Category | Item | Status |
|----------|------|--------|
| quick_task | 260426-pa5-ignore-protocol-flag-cpg-generate-replay | work completed 2026-04-26 (commit 8f33122); artifact lacks closure marker |
| quick_task | 260427-aml-v1-3-code-review-fixes | work completed 2026-04-27 (commit e3b3e77); artifact lacks closure marker |
| quick_task | 260427-bp7-v1-3-second-pass-review-fixes | work completed 2026-04-27 (commit 42f0f57); artifact lacks closure marker |

## Session Continuity

Last session: 2026-07-22T09:30:00.000Z
Stopped at: ROADMAP.md and STATE.md written for v1.6 Audit-Mode Onboarding & cpg-Dedicated Agent Tooling — Phases 20-24 created, 13/13 requirements mapped, REQUIREMENTS.md traceability updated
Resume: `/gsd-plan-phase 20` — plan `--include-audit` Verdict Ingestion (AUD-01)

## Operator Next Steps

- Review the roadmap draft in .planning/ROADMAP.md
- Start planning with /gsd-plan-phase 20
- Note: Phase 23 requires /gsd-discuss-phase (AUD-03 surface + SEC-01 mechanism decisions) before it can be planned
