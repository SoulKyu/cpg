---
phase: 13-flags-and-exit-code
plan: "01"
subsystem: pkg/hubble
tags: [aggregator, filter, tdd, FILTER-01]
dependency_graph:
  requires:
    - pkg/dropclass (ValidReasonNames + flowpb.DropReason_name enum)
    - pkg/hubble aggregator (SetIgnoreProtocols pattern)
  provides:
    - Aggregator.SetIgnoreDropReasons([]string)
    - Aggregator.IgnoredByDropReason() map[string]uint64
    - Run() filter precedence: ignoreDropReasons > ignoreProtocols > classification gate
  affects:
    - 13-02 (commonflags wiring will call SetIgnoreDropReasons)
tech_stack:
  added: []
  patterns:
    - mirror of SetIgnoreProtocols/IgnoredByProtocol with UPPERCASE normalisation instead of lowercase
    - flowpb.DropReason_name[int32(f.GetDropReasonDesc())] for canonical key lookup
key_files:
  created: []
  modified:
    - pkg/hubble/aggregator.go
    - pkg/hubble/aggregator_test.go
decisions:
  - "SetIgnoreDropReasons normalises to UPPERCASE (canonical flowpb enum name form) — mirrors SetIgnoreProtocols exactly but uppercase vs lowercase"
  - "FILTER-01 block placed BEFORE ignoreProtocols check in Run(): user explicit exclusion always takes precedence"
  - "ignoredByDropReason keys are uppercase canonical names (CT_MAP_INSERTION_FAILED not int32)"
metrics:
  duration_minutes: 8
  completed_date: "2026-04-26"
  tasks_completed: 2
  files_changed: 2
requirements: [FILTER-01]
---

# Phase 13 Plan 01: Aggregator ignore-drop-reason Filter Summary

**One-liner:** Map-based pre-classification filter on Aggregator.Run() that silently drops flows by uppercase DropReason name before flowsSeen/infraDrops/healthCh are touched.

## What Was Built

`SetIgnoreDropReasons` + `IgnoredByDropReason` on Aggregator, with a FILTER-01 gate in `Run()` inserted at the top of the flow-processing loop, before the existing `--ignore-protocol` check and before the classification gate.

Filter precedence (enforced by position in Run()):
1. `ignoreDropReasons` (new, FILTER-01) — user explicit exclusion
2. `ignoreProtocols` (PA5) — user protocol exclusion
3. classification gate (HEALTH-01/05) — auto-suppression

## Deviations from Plan

None — plan executed exactly as written.

## Tasks

| Task | Type | Commit | Description |
|------|------|--------|-------------|
| 1 | TDD RED | 44b5e2f | 6 failing tests for SetIgnoreDropReasons / IgnoredByDropReason |
| 2 | TDD GREEN | 5c96669 | Implementation: fields, init, methods, Run() filter block |

## Test Coverage Added

6 new tests (total pkg/hubble: 86 tests, all pass with -race):
- `TestAggregatorIgnoreDropReasonFilter` — skip flow, no flowsSeen/infraDrops/healthCh
- `TestAggregatorIgnoreDropReasonCounter` — per-reason counter accumulates (x2)
- `TestAggregatorIgnoreDropReasonPrecedence` — reason filter fires before protocol filter
- `TestAggregatorIgnoreDropReasonCaseInsensitive` — lowercase input → uppercase key
- `TestAggregatorIgnoreDropReasonNonMatching` — non-matching reason passes through normally
- `TestIgnoredByDropReasonCopy` — accessor returns independent copy

## Known Stubs

None.

## Self-Check: PASSED

- [x] `pkg/hubble/aggregator.go` modified — FOUND
- [x] `pkg/hubble/aggregator_test.go` modified — FOUND
- [x] Commit 44b5e2f exists (RED tests)
- [x] Commit 5c96669 exists (GREEN implementation)
- [x] `go test ./pkg/hubble/... -race -count=1` — 86 passed
- [x] `go vet ./pkg/hubble/...` — clean
- [x] `go build ./...` — clean
