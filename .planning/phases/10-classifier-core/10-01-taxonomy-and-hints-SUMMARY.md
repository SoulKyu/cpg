---
phase: 10-classifier-core
plan: 01
subsystem: classifier
tags: [dropclass, cilium, flowpb, taxonomy, classifier, hints, tdd]

requires: []

provides:
  - "pkg/dropclass package: DropClass enum (Unknown/Policy/Infra/Transient/Noise)"
  - "Classify(flowpb.DropReason) DropClass — O(1) map lookup, 11.62 ns/op, 0 allocs"
  - "RemediationHint(flowpb.DropReason) string — Cilium docs URL per infra reason"
  - "ValidReasonNames() []string — sorted proto enum names for flag validation"
  - "ClassifierVersion = '1.0.0-cilium1.19.1'"

affects:
  - "10-classifier-core plan 02 (aggregator integration)"
  - "11-aggregator-suppression (Classify gate before keyFromFlow)"
  - "12-session-summary (DropClass buckets for counts)"
  - "13-flags-exit-code (ValidReasonNames for --ignore-drop-reason validation)"

tech-stack:
  added: []
  patterns:
    - "pkg/dropclass: pure O(1) map lookup taxonomy pattern (no switch)"
    - "TDD: failing test commit (RED) then implementation commit (GREEN)"
    - "init() builds sorted validReasonNamesSorted slice from flowpb.DropReason_name"

key-files:
  created:
    - pkg/dropclass/classifier.go
    - pkg/dropclass/classifier_test.go
    - pkg/dropclass/hints.go
    - pkg/dropclass/hints_test.go
    - pkg/dropclass/version.go
  modified: []

key-decisions:
  - "O(1) map[flowpb.DropReason]DropClass instead of switch — switch on non-consecutive int32 is O(n)"
  - "Every flowpb.DropReason_name key has explicit bucket — fallback DropClassUnknown NEVER Policy (prevents CNP generation for unknown reasons)"
  - "DROP_REASON_UNKNOWN (code 0) classified Transient, not Unknown — matches proto enum semantics"
  - "AUTH_REQUIRED (189) classified Policy with code comment flagging SPIRE infra edge case for v1.4 review"
  - "RemediationHint only for Infra; all other classes return empty string"

patterns-established:
  - "dropReasonClass: map[flowpb.DropReason]DropClass initialised at package level — zero runtime alloc on hot path"
  - "TestClassifyAllKnownReasons: iterate flowpb.DropReason_name to catch new Cilium enum values on go.mod bump"
  - "TestRemediationHintAllInfraHaveURLs: coverage test that every infra reason has a URL — prevents silent gaps"

requirements-completed: [CLASSIFY-01, CLASSIFY-03]

duration: 4min
completed: 2026-04-26
---

# Phase 10 Plan 01: Taxonomy and Hints Summary

**Pure O(1) drop-reason classifier for all 76 Cilium v1.19.1 DropReason values with Cilium docs remediation URLs — 28 tests, 11.62 ns/op, 0 allocs/op**

## Performance

- **Duration:** 4 min
- **Started:** 2026-04-26T20:09:08Z
- **Completed:** 2026-04-26T20:13:16Z
- **Tasks:** 2 (TDD: RED then GREEN)
- **Files created:** 5

## Accomplishments

- Delivered `pkg/dropclass/` package: 5 files, all exported symbols per plan spec
- 28 tests pass including `TestClassifyAllKnownReasons` (full proto enum coverage) and `TestRemediationHintAllInfraHaveURLs` (hint completeness gate)
- BenchmarkClassifyReason: 11.62 ns/op, 0 allocs — O(1) map lookup confirmed, well under 50 ns/op gate
- 57 INFRA entries, 9 TRANSIENT, 6 NOISE, 4 POLICY — all 76 Cilium v1.19.1 reasons covered
- go vet passes, -race passes, `go build ./...` passes

## Task Commits

1. **Task 1: Write failing tests (RED)** — `efde875` (test)
2. **Task 2: Implement classifier, hints, version (GREEN)** — `10bf710` (feat)

**Plan metadata:** (docs commit — see below)

## Files Created

- `/home/gule/Workspace/team-infrastructure/cpg/pkg/dropclass/classifier.go` — DropClass enum, 76-entry taxonomy map, Classify(), ValidReasonNames()
- `/home/gule/Workspace/team-infrastructure/cpg/pkg/dropclass/classifier_test.go` — 28 tests including all-reasons coverage + benchmark
- `/home/gule/Workspace/team-infrastructure/cpg/pkg/dropclass/hints.go` — RemediationHint() + 57-entry infra URL table
- `/home/gule/Workspace/team-infrastructure/cpg/pkg/dropclass/hints_test.go` — URL format validation for all infra hints
- `/home/gule/Workspace/team-infrastructure/cpg/pkg/dropclass/version.go` — ClassifierVersion = "1.0.0-cilium1.19.1"

## Decisions Made

- O(1) `map[flowpb.DropReason]DropClass` (not switch): switch on non-consecutive int32 enum is O(n)
- `DROP_REASON_UNKNOWN` (code 0) → Transient, not Unknown: enum value 0 is known, just has no useful signal
- `AUTH_REQUIRED` (189) → Policy with comment: SPIRE infra misconfiguration edge case deferred to v1.4 per REQUIREMENTS.md
- Fallback `DropClassUnknown` for unrecognized values: NEVER Policy — prevents CNP generation for new/unknown Cilium codes

## Deviations from Plan

None — plan executed exactly as written.

## Issues Encountered

RTK proxy filtered benchmark output — used `rtk proxy --` to bypass filtering for ns/op verification. Not a code issue.

## Known Stubs

None — all five files are fully implemented with real data.

## Next Phase Readiness

Phase 10 Plan 02 (aggregator integration) can now import `pkg/dropclass` and call:
- `Classify(reason)` to gate flows before `keyFromFlow` in `pkg/hubble/aggregator.go`
- `ValidReasonNames()` for `--ignore-drop-reason` flag validation (phase 13)
- `ClassifierVersion` for embedding in `cluster-health.json` (phase 11)

No blockers.

---
*Phase: 10-classifier-core*
*Completed: 2026-04-26*
