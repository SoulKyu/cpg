---
gsd_state_version: 1.0
milestone: v1.2
milestone_name: l7-policies
status: roadmap_complete
stopped_at: v1.2 roadmap drafted -- phases 7-9 defined, awaiting plan-phase 7
last_updated: "2026-04-25T09:00:00.000Z"
last_activity: 2026-04-25 -- Roadmap for v1.2 L7 Policies created (phases 7, 8, 9)
progress:
  total_phases: 3
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-04-25)

**Core value:** Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.
**Current focus:** Phase 7 — L7 Infrastructure Prep (next, planning required)

## Current Position

Phase: 7 (next, planning required)
Plan: —
Status: Roadmap complete for v1.2; awaiting `/gsd:plan-phase 7`
Last activity: 2026-04-25 -- Roadmap for v1.2 L7 Policies created (phases 7, 8, 9)

Progress: v1.0 ✅ · v1.1 ✅ · v1.2 🚧 phases 7-9 defined (0/3 complete)

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

### Pending Todos

- Run `/gsd:plan-phase 7` to decompose Phase 7 (L7 Infrastructure Prep) into executable plans.

### Blockers/Concerns

None open. Research-flagged items (deferred to phase planning):

- Phase 8 — DROPPED vs REDIRECTED verdict needs live-cluster validation; one-line filter expansion if needed.
- Phase 9 — DNS REFUSED via FORWARDED verdict documented as known v1.2 limitation; `--include-l7-forwarded` deferred to v1.3 (L7-FUT-01).

## Session Continuity

Last session: 2026-04-25T09:00:00Z
Stopped at: Roadmap for v1.2 created — phases 7, 8, 9 defined with 22 requirements mapped, traceability filled.
Resume: Run `/gsd:plan-phase 7` to start Phase 7 planning.
