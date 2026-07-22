---
phase: 20-include-audit-verdict-ingestion
plan: 01
subsystem: hubble-aggregator
tags: [audit-verdict, hubble, aggregator, classification-gate, cilium, dropclass]

# Dependency graph
requires:
  - phase: 20-include-audit-verdict-ingestion (pattern precedent, not a phase dependency)
    provides: existing l7Enabled/l7HTTPCount/l7DNSCount diagnostic-counter pattern and the site-5 classification gate in pkg/hubble/aggregator.go (shipped v1.2/v1.3), copied verbatim per 20-PATTERNS.md Group 3 / Pattern D
provides:
  - "Aggregator.includeAudit bool field + SetIncludeAudit(bool) setter"
  - "Aggregator.auditVerdictCount uint64 field + AuditVerdictCount() uint64 accessor, incremented unconditionally in Run() regardless of includeAudit"
  - "Widened site-5 classification gate: (DROPPED || (includeAudit && AUDIT)) && DropReasonDesc != UNKNOWN — switch class body unchanged"
  - "4 new unit tests proving: counter increments+flag-independence, and gate enable/disable behavior (byte-identical default)"
affects: [20-02-flowsource-verdict-filter-sites, 20-03, 20-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Diagnostic counter + setter/accessor pair incremented unconditionally in Run() (4th instance of the l7Enabled/l7HTTPCount/l7DNSCount shape, per 20-PATTERNS.md Pattern D)"
    - "Boolean-OR-widened classification gate condition, single verdicts check, no scope creep beyond {DROPPED, AUDIT}"

key-files:
  created: []
  modified:
    - pkg/hubble/aggregator.go
    - pkg/hubble/aggregator_test.go

key-decisions: []

patterns-established: []

requirements-completed: [AUD-01]

# Metrics
duration: ~6min
completed: 2026-07-22
---

# Phase 20 Plan 01: Aggregator includeAudit Gate + Diagnostic Counter Summary

**Widened the aggregator's site-5 classification gate to treat `Verdict_AUDIT` like `Verdict_DROPPED` when `includeAudit` is enabled, plus an unconditional `auditVerdictCount` diagnostic counter that will power the AUD-01 zero-signal warning.**

Note: this plan delivers only site 5 of the 5 verdict-filter sites AUD-01 (AC-4) requires widened — it is the aggregator-side machinery. `requirements-completed: [AUD-01]` above mirrors the plan's own frontmatter `requirements` field for traceability; full AUD-01 delivery (CLI/MCP flag threading, `FlowSource` interface widening at sites 1-4, and the pipeline.go AUD-01 warning) is completed by later phase-20 plans (20-02 confirmed to modify `pkg/hubble/pipeline.go`, `pkg/hubble/client.go`, `pkg/flowsource/*`).

## Performance

- **Duration:** ~6 min
- **Tasks:** 2 completed
- **Files modified:** 2

## Accomplishments
- `Aggregator` gained `SetIncludeAudit(bool)` / `AuditVerdictCount() uint64`, mirroring the existing `SetL7Enabled`/`L7DNSCount` shape exactly (struct field placement, doc comment style, unconditional increment rationale).
- Site-5 classification gate in `Run()` now reads `(f.Verdict == flowpb.Verdict_DROPPED || (a.includeAudit && f.Verdict == flowpb.Verdict_AUDIT)) && f.GetDropReasonDesc() != flowpb.DropReason_DROP_REASON_UNKNOWN` — the `switch class { ... }` body below is byte-for-byte unchanged (downstream logic keys off `DropReasonDesc`/`class`, never `Verdict`).
- Default (`includeAudit` unset) behavior is proven byte-identical to pre-v1.6: `TestAggregator_AuditNotClassifiedWhenDisabled` feeds an AUDIT-verdict Infra-class flow and asserts it still falls through to bucketing/policy generation exactly as before this plan.
- 4 new unit tests added, all passing under `-race`; all 26 pre-existing `TestAggregator*` tests unaffected (30 total in the package's Aggregator-focused suite).

## Task Commits

Each task followed TDD (RED test commit, then GREEN implementation commit):

1. **Task 1: Add includeAudit gate field, setter, AUDIT diagnostic counter + accessor, and unconditional Run() increment**
   - `a2a7710` (test) — RED: `TestAggregator_AuditVerdictCount_Increments` / `_IndependentOfIncludeAudit`, compile failure confirmed (`AuditVerdictCount` undefined)
   - `43adde7` (feat) — GREEN: `includeAudit`/`auditVerdictCount` fields, `SetIncludeAudit`/`AuditVerdictCount()` methods, unconditional `Run()` increment; both new tests pass
2. **Task 2: Widen the classification gate (site 5) to {DROPPED, AUDIT} with byte-identical default behavior**
   - `9209182` (test) — RED: `TestAggregator_ClassifiesAuditWhenEnabled` fails (gate not yet widened, AUDIT flow bucketed instead of suppressed); `TestAggregator_AuditNotClassifiedWhenDisabled` passes immediately (see Issues Encountered — this is expected, not a fail-fast violation)
   - `5b586ae` (feat) — GREEN: gate widened per 20-PATTERNS.md/20-RESEARCH.md exact recommended text; both new tests pass, all 30 Aggregator tests green under `-race`

**Plan metadata:** SUMMARY.md commit (this file) — pending, committed immediately after this document is written (worktree mode; STATE.md/ROADMAP.md excluded per orchestrator ownership).

_No REFACTOR commits — both tasks' GREEN implementations matched the target pattern exactly with no cleanup needed._

## Files Created/Modified
- `pkg/hubble/aggregator.go` — added `includeAudit`/`auditVerdictCount` struct fields, `SetIncludeAudit`/`AuditVerdictCount()` methods, unconditional `Run()` increment, widened site-5 classification gate condition + updated doc comment (HEALTH-01/05 + AUD-01, including the Pitfall-4 non-blocking note)
- `pkg/hubble/aggregator_test.go` — added `TestAggregator_AuditVerdictCount_Increments`, `TestAggregator_AuditVerdictCount_IndependentOfIncludeAudit`, `TestAggregator_ClassifiesAuditWhenEnabled`, `TestAggregator_AuditNotClassifiedWhenDisabled`

## Decisions Made
None - plan executed exactly as written, following 20-PATTERNS.md Group 3 / Pattern D verbatim (this is the 4th instance of an already-shipped pattern in the same file).

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

**RED-phase test that passed immediately (not a fail-fast violation):** During Task 2's RED phase, `TestAggregator_AuditNotClassifiedWhenDisabled` passed on first run, before the gate-widening code existed. This is expected: the test asserts the *disabled-path* behavior (`includeAudit` left at default `false`), which the widened gate condition `a.includeAudit && f.Verdict == flowpb.Verdict_AUDIT` structurally cannot affect when `includeAudit` is false — the disabled path is unchanged by design, both before and after this task's edit (this is precisely the "byte-identical when disabled" truth this plan must prove). The sibling test `TestAggregator_ClassifiesAuditWhenEnabled` (the actual feature-proving test) correctly failed during RED and turned green only after the implementation, confirming the gate-widening TDD cycle was genuine. No investigation or test correction was needed.

## User Setup Required

None - no external service configuration required. Pure in-process Go library change with zero new dependencies (threat model T-20-SC: package legitimacy audit not triggered).

## Next Phase Readiness

- `Aggregator.SetIncludeAudit(bool)` and `Aggregator.AuditVerdictCount()` are ready for plan 20-02 (and later plans) to wire from `PipelineConfig.IncludeAudit` and the AUD-01 warning in `pkg/hubble/pipeline.go` respectively (per 20-PATTERNS.md Group 2's `agg.SetIncludeAudit(cfg.IncludeAudit)` call site and the AUD-01 warning block referencing `agg.AuditVerdictCount()`).
- No blockers. `pkg/hubble` package (all files) and the full module (`go build ./...`) both compile clean; verification commands from the plan's `<verification>` block all pass:
  - `rtk proxy go build ./pkg/hubble/` — OK
  - `rtk proxy go test ./pkg/hubble/ -run TestAggregator -race -count=1` — PASS (30/30)
  - `rtk proxy git diff pkg/hubble/aggregator.go` — confirmed confined to 2 struct fields, 1 setter, 1 accessor, 1 unconditional increment, 1 gate-condition line + comment; `switch class` body untouched

---
*Phase: 20-include-audit-verdict-ingestion*
*Completed: 2026-07-22*
