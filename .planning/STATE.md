---
gsd_state_version: 1.0
milestone: v1.2
milestone_name: L7 Policies (HTTP + DNS)
status: in_progress
stopped_at: Completed 08-01-PLAN.md
last_updated: "2026-04-25T08:01:14.511Z"
last_activity: "2026-04-25 -- Plan 08-01 complete: extractHTTPRules + normalizeHTTPMethod helpers (HTTP-02/03/05), 262 tests pass"
progress:
  total_phases: 3
  completed_phases: 1
  total_plans: 8
  completed_plans: 5
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-04-25)

**Core value:** Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.
**Current focus:** Phase 7 — L7 Infrastructure Prep (next, planning required)

## Current Position

Phase: 8 (in progress, 1/4 plans) → next: Plan 08-02 (BuildPolicy --l7 integration)
Plan: 08-01 ✅ complete
Status: Phase 8 in progress (1/4 plans). v1.2 milestone 1/3 phases done; phase 8 underway.
Last activity: 2026-04-25 -- Plan 08-01 complete: extractHTTPRules + normalizeHTTPMethod helpers (HTTP-02/03/05), 262 tests pass

Progress: v1.0 ✅ · v1.1 ✅ · v1.2 🚧 phases 7 ✅ · 8-9 pending (1/3 complete)

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

### Pending Todos

- Run `/gsd:execute-phase 8` to continue Phase 8 with Plan 08-02 (BuildPolicy --l7 integration).

### Blockers/Concerns

None open. Research-flagged items (deferred to phase planning):

- Phase 8 — DROPPED vs REDIRECTED verdict needs live-cluster validation; one-line filter expansion if needed.
- Phase 9 — DNS REFUSED via FORWARDED verdict documented as known v1.2 limitation; `--include-l7-forwarded` deferred to v1.3 (L7-FUT-01).

## Session Continuity

Last session: 2026-04-25T08:01:14.506Z
Stopped at: Completed 08-01-PLAN.md
Resume: Run `/gsd:execute-phase 8` to continue Phase 8 with Plan 08-02.
