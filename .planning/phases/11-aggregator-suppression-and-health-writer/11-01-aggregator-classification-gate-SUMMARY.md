---
phase: 11-aggregator-suppression-and-health-writer
plan: "01"
subsystem: hubble
tags: [aggregator, dropclass, classification, health, counter]

requires:
  - phase: 10-classifier-core
    provides: "dropclass.Classify(), DropClass enum, SetWarnLogger(), ClassifierVersion"

provides:
  - "DropEvent struct in pkg/hubble (Reason/Class/Namespace/Workload/NodeName)"
  - "Aggregator.Run() 4-arg signature with healthCh chan<- DropEvent"
  - "Aggregator.InfraDrops() / InfraDropTotal() accessors"
  - "Classification gate in Run(): Infra/Transient suppressed, Noise discarded, Policy/Unknown pass through"
  - "infraDrops map[DropReason]uint64 counter on Aggregator struct"
  - "SessionStats.InfraDropTotal + InfraDropsByReason fields + Log() wiring"

affects:
  - 11-02-health-writer
  - 12-session-summary-block
  - 13-flags-exit-code

tech-stack:
  added: []
  patterns:
    - "Classification gate pattern: Verdict==DROPPED guard + dropclass.Classify() + switch on DropClass"
    - "healthCh chan<- DropEvent: nil-safe pass-through to health writer (plan 11-02 wires real channel)"
    - "flowsSeen includes Infra/Transient flows (Pitfall 6 invariant: total observed, not policy-eligible)"

key-files:
  created: []
  modified:
    - pkg/hubble/aggregator.go
    - pkg/hubble/aggregator_test.go
    - pkg/hubble/pipeline.go

key-decisions:
  - "Guard classification gate on Verdict==DROPPED && reason!=zero: prevents false suppression of test/forwarded flows where DropReasonDesc has zero value"
  - "pipeline.go passes nil as healthCh until plan 11-02 creates the real health writer channel"
  - "Noise flows silently discarded (no flowsSeen, no infraDrops, no healthCh send)"
  - "Unknown class falls through to keyFromFlow (errs on side of CNP generation)"

patterns-established:
  - "DropEvent: minimal struct for suppressed flows — consumed by healthWriter (plan 11-02)"
  - "infraDrops map copy semantics: InfraDrops() returns independent copy (mirrors IgnoredByProtocol())"

requirements-completed: [HEALTH-01, HEALTH-05]

duration: 8min
completed: 2026-04-26
---

# Phase 11 Plan 01: Aggregator Classification Gate Summary

**Classification gate wired into Aggregator.Run() with DropEvent channel: Infra/Transient flows suppressed from CNP generation but counted in flowsSeen, with per-reason infraDrops counter and nil-safe healthCh dispatch**

## Performance

- **Duration:** ~8 min
- **Started:** 2026-04-26T20:26:00Z
- **Completed:** 2026-04-26T20:34:21Z
- **Tasks:** 2 (TDD: RED + GREEN)
- **Files modified:** 3

## Accomplishments

- Classification gate inserted after --ignore-protocol filter, before keyFromFlow: Infra/Transient flows increment flowsSeen and infraDrops, then continue (no bucket); Noise flows silently discarded; Policy/Unknown fall through
- Pitfall 6 invariant verified: 5 policy + 3 infra flows → flowsSeen=8, infraDrops=3, exactly 5 CNP buckets created
- DropEvent struct + buildDropEvent/effectiveEndpoint helpers added; healthCh nil-safe throughout
- SessionStats extended with InfraDropTotal/InfraDropsByReason; Log() updated; pipeline.go call site patched with nil healthCh (plan 11-02 replaces)

## Task Commits

1. **Task 1: Write failing tests (RED)** - `845a8ec` (test)
2. **Task 2: Implement classification gate (GREEN)** - `3def91e` (feat)

**Plan metadata:** (docs commit follows)

## Files Created/Modified

- `/home/gule/Workspace/team-infrastructure/cpg/pkg/hubble/aggregator.go` - DropEvent struct, infraDrops field, Run() signature change, classification gate, buildDropEvent/effectiveEndpoint, InfraDrops()/InfraDropTotal()
- `/home/gule/Workspace/team-infrastructure/cpg/pkg/hubble/aggregator_test.go` - 10 new tests (TestAggregatorClassificationSuppression, TestAggregatorPolicyFlowPassthrough, TestAggregatorFlowsSeenInvariant, TestAggregatorNoiseDiscarded, TestAggregatorTransientCounted, TestAggregatorHealthChReceivesDropEvent, TestAggregatorHealthChNilSafe, TestAggregatorFilterPrecedence, TestInfraDropTotal, TestInfraDropsCopy); all existing tests updated to 4-arg Run()
- `/home/gule/Workspace/team-infrastructure/cpg/pkg/hubble/pipeline.go` - flowpb import, SessionStats fields, Log() wiring, Run() call site → nil healthCh

## Decisions Made

- Guarded classification gate on `Verdict==DROPPED && reason!=DROP_REASON_UNKNOWN`: existing testdata flows have zero Verdict (Verdict_VERDICT_UNKNOWN), so they pass through classification unaffected. Only explicitly dropped flows with a non-zero reason are classified. Per PITFALLS Integration Gotchas: "always check Verdict == DROPPED first".
- pipeline.go passes `nil` as healthCh until plan 11-02 creates the real channel. This is explicit and safe — Run() nil-checks before sending.
- `dropclass.SetWarnLogger(logger)` called once in `NewAggregator()` (not in Run()) to wire the warn logger before any classify calls.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Added Verdict==DROPPED guard to classification gate**
- **Found during:** Task 2 (GREEN implementation)
- **Issue:** Existing test flows (testdata helpers) set zero-value Verdict (Verdict_VERDICT_UNKNOWN=0) and zero-value DropReasonDesc. Without the guard, `Classify(DROP_REASON_UNKNOWN)` returns `DropClassTransient` (per phase 10 taxonomy), silently suppressing all previously-passing flows and causing 5 existing tests to fail.
- **Fix:** Gate classification on `f.Verdict == flowpb.Verdict_DROPPED && f.GetDropReasonDesc() != flowpb.DropReason_DROP_REASON_UNKNOWN`. This matches the PITFALLS recommendation ("always check Verdict == DROPPED first") and preserves all existing test behavior.
- **Files modified:** pkg/hubble/aggregator.go
- **Verification:** All 60 hubble tests pass (was 50 before new tests); `go build ./...` clean
- **Committed in:** 3def91e (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 — bug)
**Impact on plan:** Essential correctness fix. The plan specified the gate but not the Verdict guard — required by the protobuf semantics documented in PITFALLS.

## Issues Encountered

None beyond the auto-fixed Verdict guard.

## Known Stubs

- `pipeline.go` passes `nil` as `healthCh` — intentional temporary stub until plan 11-02 creates the real `healthWriter` and wires the buffered channel.

## Next Phase Readiness

- `DropEvent` struct exported and ready for healthWriter consumption (plan 11-02)
- `Aggregator.Run(ctx, in, out, healthCh)` signature stable
- `InfraDrops()` / `InfraDropTotal()` accessors ready for SessionStats and --fail-on-infra-drops (phase 13)
- plan 11-02 must: create `healthWriter`, wire `healthCh` channel in pipeline.go, implement `cluster-health.json` atomic write

## Self-Check: PASSED

- aggregator.go: FOUND
- aggregator_test.go: FOUND
- pipeline.go: FOUND
- Commit 845a8ec (RED tests): FOUND
- Commit 3def91e (GREEN implementation): FOUND

---
*Phase: 11-aggregator-suppression-and-health-writer*
*Completed: 2026-04-26*
