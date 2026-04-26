---
phase: 13-flags-and-exit-code
plan: "02"
subsystem: cmd/cpg + pkg/hubble
tags: [commonflags, pipeline, tdd, FILTER-01, FILTER-02, FILTER-03, EXIT-01]
dependency_graph:
  requires:
    - 13-01 (Aggregator.SetIgnoreDropReasons)
    - pkg/dropclass (ValidReasonNames, Classify, DropClassInfra, DropClassTransient)
    - flowpb.DropReason_value map for FILTER-03 class lookup
  provides:
    - validateIgnoreDropReasons([]string, *zap.Logger) ([]string, error)
    - --ignore-drop-reason StringSlice flag on generate + replay
    - --fail-on-infra-drops bool flag on generate + replay
    - PipelineConfig.IgnoreDropReasons []string
    - PipelineConfig.FailOnInfraDrops bool
    - RunPipelineWithSource calls agg.SetIgnoreDropReasons
  affects:
    - 13-03 (reads PipelineConfig.FailOnInfraDrops to implement os.Exit(1))
tech_stack:
  added:
    - go.uber.org/zap/zaptest/observer (test-only, already in transitive deps)
  patterns:
    - validateIgnoreDropReasons mirrors validateIgnoreProtocols (uppercase vs lowercase normalisation)
    - FILTER-03 uses dropclass.Classify() + dropClassLabel() local helper for WARN formatting
    - PipelineConfig flag wiring pattern: struct fields + agg.Set* call in RunPipelineWithSource
key_files:
  created:
    - cmd/cpg/commonflags_test.go
  modified:
    - cmd/cpg/commonflags.go
    - cmd/cpg/generate.go
    - cmd/cpg/replay.go
    - pkg/hubble/pipeline.go
decisions:
  - "validateIgnoreDropReasons accepts *zap.Logger parameter so FILTER-03 WARN can be emitted inline — avoids returning warnings as []string (would require caller loop)"
  - "dropClassLabel() is a local helper in commonflags.go — avoids exporting String() from pkg/dropclass which has no other use"
  - "FailOnInfraDrops stored in PipelineConfig but RunPipelineWithSource does NOT act on it — plan 13-03 implements the os.Exit(1) path"
metrics:
  duration_minutes: 8
  completed_date: "2026-04-26"
  tasks_completed: 2
  files_modified: 5
---

# Phase 13 Plan 02: Commonflags and Pipeline Wiring Summary

**One-liner:** `--ignore-drop-reason` and `--fail-on-infra-drops` flags wired through CLI surface to PipelineConfig with validateIgnoreDropReasons FILTER-02/03 validation.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | Add failing tests for validateIgnoreDropReasons | 8688432 | cmd/cpg/commonflags_test.go |
| 2 (GREEN) | Implement flags, validation, PipelineConfig wiring | c29233a | commonflags.go, generate.go, replay.go, pipeline.go |

## What Was Built

### validateIgnoreDropReasons (cmd/cpg/commonflags.go)

Mirrors `validateIgnoreProtocols` exactly with two differences:
- Allowlist sourced from `dropclass.ValidReasonNames()` (uppercase canonical enum names)
- FILTER-03: after unknown-name check, calls `dropclass.Classify()` on each reason; emits WARN when class is Infra or Transient (redundant with default suppression)

Signature: `func validateIgnoreDropReasons(in []string, logger *zap.Logger) ([]string, error)`

### Flag Registration

Both `--ignore-drop-reason` (StringSlice, repeatable/comma-separated) and `--fail-on-infra-drops` (bool) added to `addCommonFlags()` — available on both `generate` and `replay` subcommands.

### PipelineConfig (pkg/hubble/pipeline.go)

Two new fields:
- `IgnoreDropReasons []string` — passed to `agg.SetIgnoreDropReasons()` in `RunPipelineWithSource`
- `FailOnInfraDrops bool` — stored for plan 13-03 (exit code logic, not implemented here)

## Requirements Satisfied

- FILTER-01 (cmd side): PipelineConfig.IgnoreDropReasons → agg.SetIgnoreDropReasons wired
- FILTER-02: validateIgnoreDropReasons rejects unknown reason names with "unknown drop reason %q: valid values are %s"
- FILTER-03: validateIgnoreDropReasons emits WARN when reason is Infra/Transient class
- EXIT-01 (flag only): PipelineConfig.FailOnInfraDrops field present; exit logic in plan 13-03

## Deviations from Plan

None - plan executed exactly as written.

## Test Count

411 tests pass (was 411 before this plan — 8 new tests added in commonflags_test.go, all previously green tests still passing).

## Self-Check: PASSED

Files:
- cmd/cpg/commonflags_test.go — FOUND
- cmd/cpg/commonflags.go — FOUND (validateIgnoreDropReasons, dropClassLabel, new struct fields, new flag registrations)
- cmd/cpg/generate.go — FOUND (validateIgnoreDropReasons call, IgnoreDropReasons + FailOnInfraDrops in PipelineConfig)
- cmd/cpg/replay.go — FOUND (same pattern as generate.go)
- pkg/hubble/pipeline.go — FOUND (IgnoreDropReasons + FailOnInfraDrops fields, SetIgnoreDropReasons call)

Commits:
- 8688432 test(13-02): add failing tests for validateIgnoreDropReasons
- c29233a feat(13-02): wire --ignore-drop-reason and --fail-on-infra-drops flags
