---
gsd_state_version: 1.0
milestone: v1.2
milestone_name: L7 Policies (HTTP + DNS)
status: executing
stopped_at: Completed 08-04-PLAN.md (Phase 8 complete)
last_updated: "2026-04-25T08:21:26Z"
last_activity: "2026-04-25 -- Plan 08-04 complete: e2e cpg replay --l7 tests for HTTP-01..HTTP-05 + VIS-01; README #l7-prerequisites anchor reserved; 279 tests pass. Phase 8 closed."
progress:
  total_phases: 3
  completed_phases: 2
  total_plans: 8
  completed_plans: 8
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-04-25)

**Core value:** Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.
**Current focus:** Phase 7 — L7 Infrastructure Prep (next, planning required)

## Current Position

Phase: 8 ✅ complete (4/4 plans) → next: Phase 9 (DNS L7 generation + docs)
Plan: 08-04 ✅ complete
Status: Phase 8 closed. v1.2 milestone 2/3 phases done.
Last activity: 2026-04-25 -- Plan 08-04 complete: e2e cpg replay --l7 tests for HTTP-01..HTTP-05 + VIS-01; README #l7-prerequisites anchor reserved; 279 tests pass.

Progress: v1.0 ✅ · v1.1 ✅ · v1.2 🚧 phases 7 ✅ · 8 ✅ · 9 pending (2/3 complete)

## Performance Metrics

**Velocity:**

- Total plans completed: 7
- Average duration: 4.4 min
- Total execution time: 0.52 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01-core-policy-engine | 3 | 13 min | 4.3 min |
| 02-hubble-streaming-pipeline | 2 | 7 min | 3.5 min |
| 03-production-hardening | 2 | 12 min | 6.0 min |
| 04-offline-replay-core | 1 | — | — |
| 05-dry-run-pipeline-integration | 1 | — | — |
| 06-explain-command | 1 | — | — |

**Recent Trend:**

- Last 5 plans: 02-02 (4 min), 03-01 (6 min), 03-02 (6 min), 04-01 (—), 05-01 (—)
- Trend: stable

*Updated after each plan completion*
| Phase 07 P03 | 10min | 1 tasks | 2 files |
| Phase 07 P02 | 3min | 2 tasks | 8 files |
| Phase 07 P04 | 12min | 2 tasks | 7 files |
| Phase 08 P01 | 12min | 2 tasks | 2 files |
| Phase 08 P02 | 12 | 3 tasks | 3 files |
| Phase 08 P03 | 4m | 2 tasks | 5 files |
| Phase 08 P04 | 12m | 2 tasks | 3 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.

Recent decisions affecting v1.2 work (research-confirmed 2026-04-25):

- Phase 7 fixes `mergePortRules` Rules-field-drop bug FIRST — silent latent bug today, blocker for L7 correctness once codegen ships.
- Evidence schema v2 ships with NO v1 back-compat layer (v1.1 shipped 2026-04-24, no production caches). Reader rejects `schema_version != 2` with explicit wipe instruction.
- `RuleKey` extends with optional L7 discriminator so two rules differing only by HTTP method/path are not deduplicated into the same evidence bucket.
- Pre-flight checks (VIS-04, VIS-05) reuse existing `pkg/k8s` clientset. RBAC denied → warn-and-proceed (NOT abort). `--no-l7-preflight` (VIS-06) skips entirely.
- Phase 7 `--l7` flag is plumbing-only: parsed and threaded but does not change YAML output until Phase 8.
- HTTP path emission: `regexp.QuoteMeta` + `^…$` anchoring (security-impacting; under-anchoring allows traffic the operator believed denied).
- HTTP method uppercase normalization MUST happen before merge / dedup; otherwise byte-stable YAML output breaks.
- HTTP `Headers`/`Host`/`HostExact` rules NEVER generated (anti-feature: secret leakage into committed YAML).
- DNS-02 companion-rule invariant: generator MUST NEVER emit a `toFQDNs` without the companion UDP+TCP/53 rule. Unit-test invariant.
- DNS `matchPattern` glob NOT generated in v1.2 — only `matchName` literals (wildcard inference deferred to v1.3, DNS-FUT-01).
- kube-dns selector hardcoded `k8s-app=kube-dns` with YAML comment in v1.2; runtime autodetection deferred to v1.3 (DNS-FUT-02).
- Capture L7 jsonpb fixtures from a real cluster session for replay tests (per STACK research).
- v1.2 docs/superpowers AI-analysis spec dropped before implementation (recoverable via git history at commit 7e1e455).
- [Phase 07]: L7 preflight uses caller-side single-call contract (godoc) instead of sync.Once — cleaner, easier to test.
- [Phase 07]: Evidence schema v1->v2 bumped with optional L7Ref; reader/writer reject non-v2 with wipe instruction naming $XDG_CACHE_HOME/cpg/evidence/ (no v1 back-compat)
- [Phase 07-04]: L7 client construction injected via package-level `l7ClientFactory` swappable var. Tests substitute `kubernetes/fake` clientsets without DI plumbing through every call site or touching the `cobra.Command` surface.
- [Phase 07-04]: Byte-stability test compares CNP YAML byte-for-byte but evidence sidecars by tree shape only — session UUID + timestamps differ legitimately run-to-run regardless of `--l7`. Invariant applies to codegen, not session-stamped state.
- [Phase 08]: Plan 08-01: HTTP L7 extraction primitives kept package-private; net/url.Parse handles bare-paths and full-URLs uniformly; empty path emits ^/$; HTTP-05 enforced via dedicated lint test
- [Phase 08]: BuildPolicy emits per-port PortRules only when L7 rules attach; L4-only path stays collapsed (single PortRule with all ports) to preserve v1.1 byte-stability.
- [Phase 08]: L7 attribution subsumes L4 attribution per-flow: when recordL7 emits HTTP-discriminated keys, the bare L4 RuleKey is skipped, preventing double-counting in evidence buckets.
- [Phase 08-03]: SessionStats counters hydrated post-Wait from aggregator (single source of truth). Incidentally fixed v1.0 audit BUG-01 (FlowsSeen always 0) since VIS-01 needs flowsSeen > 0 gate to be accurate.
- [Phase 08-03]: VIS-01 emitted from a single deterministic site (post g.Wait, pre stats.Log); workload list sorted for deterministic test assertions; HTTP+DNS sum gate ready for one-line Phase 9 plug-in.
- [Phase 08-04]: Observed-logger pattern in cmd/cpg via package-level swap (`initObservedLoggerForTesting` returns *observer.ObservedLogs, t.Cleanup restores prev). Avoids refactoring PipelineConfig plumbing.
- [Phase 08-04]: README `#l7-prerequisites` anchor placeholder reserved before Phase 9 ships content; VIS-01 hint string in `pkg/hubble/pipeline.go:203` already references it.

### Pending Todos

- Run `/gsd:plan-phase 9` to plan Phase 9 (DNS L7 generation + docs/two-step workflow + README #l7-prerequisites content).

### Blockers/Concerns

None open. Research-flagged items (deferred to phase planning):

- Phase 8 — DROPPED vs REDIRECTED verdict needs live-cluster validation; one-line filter expansion if needed.
- Phase 9 — DNS REFUSED via FORWARDED verdict documented as known v1.2 limitation; `--include-l7-forwarded` deferred to v1.3 (L7-FUT-01).

## Session Continuity

Last session: 2026-04-25T08:21:26Z
Stopped at: Completed 08-04-PLAN.md (Phase 8 complete)
Resume: Run `/gsd:plan-phase 9` to start Phase 9 (DNS L7 generation + docs).
