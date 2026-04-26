---
phase: 13-flags-and-exit-code
plan: "03"
subsystem: cmd/cpg + pkg/hubble + README
tags: [exit-code, infra-drops, readme, tdd]
dependency_graph:
  requires: [13-02]
  provides: [EXIT-01, EXIT-02]
  affects: [cmd/cpg/main.go, pkg/hubble/pipeline.go, README.md]
tech_stack:
  added: []
  patterns: [ExitCodeError sentinel, errors.As intercept, shouldExitForInfraDrops pure helper]
key_files:
  created:
    - pkg/hubble/pipeline_exit_test.go
  modified:
    - pkg/hubble/pipeline.go
    - cmd/cpg/main.go
    - README.md
decisions:
  - ExitCodeError defined in pkg/hubble (avoids import cycle; main.go already imports pkg/hubble)
  - shouldExitForInfraDrops pure helper avoids os.Exit in testable code path
  - Exit code 1 only (not 2); exit 0 preserved by default for P3 backward-compat invariant
  - errors.As pattern in main.go intercepts ExitCodeError before generic os.Exit(1)
metrics:
  duration: 146s
  completed: "2026-04-26"
  tasks: 2
  files_changed: 4
---

# Phase 13 Plan 03: Exit Code and README Summary

**One-liner:** ExitCodeError sentinel + shouldExitForInfraDrops helper wired into RunPipelineWithSource, with errors.As intercept in main.go and full README documentation.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | Failing tests for ExitCodeError + shouldExitForInfraDrops | test commit | pkg/hubble/pipeline_exit_test.go |
| 1 (GREEN) | ExitCodeError + shouldExitForInfraDrops + pipeline wiring | 296a851 | pkg/hubble/pipeline.go, cmd/cpg/main.go |
| 2 | README documentation (EXIT-02) | 2b7bf02 | README.md |

## What Was Built

### EXIT-01: Exit code logic

- `ExitCodeError{Code int, Msg string}` sentinel type added to `pkg/hubble/pipeline.go`
- `shouldExitForInfraDrops(failOnInfraDrops bool, infraDropTotal uint64) bool` pure helper (3 table-driven unit tests)
- `RunPipelineWithSource` returns `&ExitCodeError{Code:1}` when `FailOnInfraDrops=true && InfraDropTotal>0` (after `stats.Log`)
- `cmd/cpg/main.go` updated: `errors.As(err, &ec)` intercepts before generic `os.Exit(1)`
- Default behavior unchanged: exit 0 when `FailOnInfraDrops=false` regardless of drop count (P3 backward-compat)

### EXIT-02: README documentation

- `--ignore-drop-reason` added to Flags section (Filtering block)
- `--fail-on-infra-drops` added to Flags section (CI integration block)
- Offline replay shared-flags sentence updated to include both new flags
- New `## Exit codes` section with 2-row table (0 = success, 1 = fail-on-infra-drops + drops)
- CI/cron examples for both `cpg replay` and `cpg generate` with timeout

## Verification

```
go test ./... -race -count=1  →  418 passed in 10 packages
go vet ./...                   →  clean
go build ./...                 →  clean
cpg generate --help            →  --fail-on-infra-drops present
cpg replay --help              →  --fail-on-infra-drops present
README: fail-on-infra-drops    →  4 occurrences
README: ignore-drop-reason     →  2 occurrences
README: Exit codes section     →  present
```

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None.

## Self-Check: PASSED

- pkg/hubble/pipeline_exit_test.go: FOUND
- pkg/hubble/pipeline.go: FOUND
- cmd/cpg/main.go: FOUND
- README.md: FOUND
- Commit 296a851: FOUND
- Commit 2b7bf02: FOUND
