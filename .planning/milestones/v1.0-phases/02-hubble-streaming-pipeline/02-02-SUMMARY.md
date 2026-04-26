---
phase: 02-hubble-streaming-pipeline
plan: 02
subsystem: streaming
tags: [errgroup, aggregation, pipeline, channels, graceful-shutdown, session-stats]

requires:
  - phase: 02-hubble-streaming-pipeline
    provides: Hubble gRPC client with StreamDroppedFlows, typed channels
  - phase: 01-core-policy-engine
    provides: BuildPolicy, MergePolicy, output.Writer, labels.WorkloadName
provides:
  - Temporal flow aggregation with configurable flush interval
  - Pipeline orchestration via errgroup (aggregator, writer, lost events monitor)
  - LostEvents aggregated warning (30s ticker)
  - Session summary on shutdown
  - Fully wired cpg generate command
affects: [03-testing-validation]

tech-stack:
  added: [golang.org/x/sync/errgroup (promoted to direct)]
  patterns: [errgroup 3-stage pipeline, FlowSource interface for testability, AggKey bucketing]

key-files:
  created: [pkg/hubble/aggregator.go, pkg/hubble/aggregator_test.go, pkg/hubble/pipeline.go, pkg/hubble/pipeline_test.go]
  modified: [cmd/cpg/generate.go, go.mod, go.sum]

key-decisions:
  - "FlowSource interface enables pipeline testing without gRPC (RunPipelineWithSource)"
  - "AggKey uses (namespace, workload) only -- direction handled inside BuildPolicy"
  - "Flows with empty namespace skipped at aggregator level to prevent empty directory names"
  - "Session stats updated in single writer goroutine (no atomic needed)"

patterns-established:
  - "FlowSource interface: injectable streaming source for pipeline tests"
  - "errgroup 3-stage pipeline: aggregator -> writer -> lost events monitor"
  - "AggKey bucketing with flush-on-ticker/close/cancel pattern"

requirements-completed: [CONN-05, OUTP-02]

duration: 4min
completed: 2026-03-08
---

# Phase 2 Plan 2: Pipeline Orchestration Summary

**Temporal flow aggregation with errgroup pipeline, LostEvents monitoring, session summary, and fully wired CLI generate command**

## Performance

- **Duration:** 4 min
- **Started:** 2026-03-08T19:37:32Z
- **Completed:** 2026-03-08T19:41:32Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments
- Aggregator accumulates flows by (namespace, workload) and flushes on ticker, channel close, or context cancel
- LostEvents monitor aggregates warnings every 30s with period and total counts
- Pipeline orchestrates 3 goroutines via errgroup with graceful shutdown
- Session summary logged on exit with duration, flows seen, policies written, lost events
- `cpg generate --server <addr>` now calls RunPipeline (no more "not yet implemented")
- 19 total hubble package tests pass with race detector

## Task Commits

Each task was committed atomically:

1. **Task 1: Aggregator + LostEvents monitor (TDD)**
   - `f129e69` (test) - failing tests for aggregator and lost events monitor
   - `9ba9654` (feat) - aggregator implementation with temporal flush

2. **Task 2: Pipeline orchestration + CLI wiring (TDD)**
   - `ec02c95` (test) - failing tests for pipeline and session stats
   - `74d289f` (feat) - pipeline implementation and CLI generate command wiring

## Files Created/Modified
- `pkg/hubble/aggregator.go` - Aggregator with AggKey bucketing, flush, monitorLostEvents
- `pkg/hubble/aggregator_test.go` - 8 tests: flush on ticker/close/cancel, key derivation, skip empty ns, lost events
- `pkg/hubble/pipeline.go` - PipelineConfig, SessionStats, RunPipeline, FlowSource interface
- `pkg/hubble/pipeline_test.go` - 3 tests: end-to-end, graceful shutdown, session stats logging
- `cmd/cpg/generate.go` - Wired RunPipeline with signal-aware context replacing stub
- `go.mod` - Promoted golang.org/x/sync to direct dependency
- `go.sum` - Updated checksums

## Decisions Made
- FlowSource interface enables full pipeline testing without gRPC -- RunPipelineWithSource accepts any FlowSource
- AggKey uses (namespace, workload) only, direction is handled inside BuildPolicy
- Flows with empty namespace are skipped at the aggregator level to prevent empty directory names
- Session stats updated by the single writer goroutine, no atomic counters needed

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Phase 2 complete: full streaming pipeline from Hubble Relay to policy files on disk
- `cpg generate --server localhost:4245` attempts gRPC connection and streams
- All existing Phase 1 and Phase 2 tests pass (full suite green)
- Ready for Phase 3: testing and validation

---
*Phase: 02-hubble-streaming-pipeline*
*Completed: 2026-03-08*

## Self-Check: PASSED
