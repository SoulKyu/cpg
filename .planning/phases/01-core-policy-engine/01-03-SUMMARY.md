---
phase: 01-core-policy-engine
plan: 03
subsystem: output
tags: [cilium, go, tdd, yaml, cli, cobra, zap, output-writer]

# Dependency graph
requires:
  - "01-02: BuildPolicy and MergePolicy for flow-to-CNP transformation and merge-on-write"
provides:
  - "Writer: Directory-organized YAML file writer with merge-on-write via MergePolicy"
  - "cpg generate CLI subcommand with all flags for Hubble streaming (Phase 2)"
  - "Configurable zap logging (--debug, --log-level, --json)"
affects: [02-hubble-streaming]

# Tech tracking
tech-stack:
  added: [go.uber.org/zap/zapcore]
  patterns: [merge-on-write file output, cobra PersistentPreRunE for logger init, flag validation in RunE]

key-files:
  created:
    - pkg/output/writer.go
    - pkg/output/writer_test.go
    - cmd/cpg/generate.go
  modified:
    - cmd/cpg/main.go

key-decisions:
  - "Logger initialized in root PersistentPreRunE, stored as package-level var for subcommand access"
  - "Console encoder (colored) as default log format, JSON only via --json flag"
  - "--server marked required via Cobra MarkFlagRequired, mutual exclusion validated in RunE"

patterns-established:
  - "Output writer pattern: namespace subdirectory -> workload.yaml with merge-on-write"
  - "CLI flag organization: connection flags, namespace filtering, output config, aggregation tuning"
  - "Logger configuration: --debug shortcut overrides --log-level, --json switches encoder"

requirements-completed: [OUTP-01, OUTP-03]

# Metrics
duration: 4min
completed: 2026-03-08
---

# Phase 1 Plan 3: Output Writer + CLI Summary

**Directory-organized YAML file writer with merge-on-write and complete CLI skeleton with zap structured logging**

## Performance

- **Duration:** 4 min
- **Started:** 2026-03-08T09:07:22Z
- **Completed:** 2026-03-08T09:11:00Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments
- Output writer creates outputDir/namespace/workload.yaml with valid CiliumNetworkPolicy YAML
- Merge-on-write: reads existing file, calls MergePolicy, writes merged result with port deduplication
- cpg generate subcommand with all 10 flags (--server, --namespace, --all-namespaces, --output-dir, --debug, --log-level, --json, --tls, --flush-interval, --timeout)
- zap logger configurable: info default, debug with --debug, JSON output with --json
- 5 unit tests for output writer all passing with race detector

## Task Commits

Each task was committed atomically:

1. **Task 1 RED: Failing tests for output writer** - `a956cc4` (test)
2. **Task 1 GREEN: Implement output writer** - `d8224ce` (feat)
3. **Task 2: Wire CLI generate command with flags and logging** - `cdc2694` (feat)

## Files Created/Modified
- `pkg/output/writer.go` - Writer with NewWriter(), Write(PolicyEvent) for directory-organized YAML output with merge-on-write
- `pkg/output/writer_test.go` - 5 tests: file creation, directory creation, merge-on-write, permissions, multiple namespaces
- `cmd/cpg/generate.go` - cpg generate subcommand with all CLI flags and validation
- `cmd/cpg/main.go` - Root command with PersistentPreRunE logger initialization, global logging flags

## Decisions Made
- Logger initialized in root PersistentPreRunE and stored as package-level var (simple, subcommands access directly)
- Console encoder with colors as default format, JSON only via explicit --json flag
- --server required via Cobra MarkFlagRequired; --namespace/--all-namespaces mutual exclusion validated in RunE
- RunE returns "not yet implemented" error for Phase 2 Hubble streaming placeholder

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Phase 1 vertical slice complete: labels -> policy builder -> merge -> output writer -> CLI
- cpg generate accepts all flags, ready for Phase 2 Hubble streaming pipeline wiring
- Output writer ready to receive PolicyEvents from the aggregation pipeline
- All 3 packages (labels, policy, output) tested and passing with race detector

## Self-Check: PASSED

All 5 files verified present. All 3 commits verified in git log. Line counts meet minimums.

---
*Phase: 01-core-policy-engine*
*Completed: 2026-03-08*
