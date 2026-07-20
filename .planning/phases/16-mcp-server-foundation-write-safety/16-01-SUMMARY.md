---
phase: 16-mcp-server-foundation-write-safety
plan: 01
subsystem: infra
tags: [go, filesystem, atomic-write, concurrency, testing]

# Dependency graph
requires: []
provides:
  - Atomic (temp+rename) policy YAML writer in pkg/output/writer.go, matching pkg/evidence/writer.go and pkg/hubble/health_writer.go
  - Regression-proof that generated policy files stay 0644 (existing TestWriter_FilePermissions still green)
  - TestWriter_ConcurrentReaderNeverSeesPartialFile: race-clean proof that a concurrent reader never observes a torn/partial policy file
affects: [16-mcp-server-foundation-write-safety, 18-query-tools]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Same-dir atomic write: os.CreateTemp -> Write -> Close -> Chmod(0644) -> os.Rename, mirrored from pkg/evidence/writer.go with an explicit chmod addition"

key-files:
  created: []
  modified:
    - pkg/output/writer.go
    - pkg/output/writer_test.go

key-decisions:
  - "Explicit os.Chmod(tmpPath, 0644) between tmp.Close() and os.Rename() — os.CreateTemp defaults to 0600 and neither existing analog (pkg/evidence/writer.go, pkg/hubble/health_writer.go) chmods; pkg/output's 0644 is an observed GitOps contract pinned by TestWriter_FilePermissions, so mirroring the analogs verbatim would have silently regressed permissions (this was directed explicitly by the plan's Task 1 action steps, not an executor deviation)"
  - "Concurrent-reader test varies the destination port every iteration (8000+i) instead of repeating an identical event, so every w.Write call produces genuinely different policy content and forces a real temp+rename cycle each time — a fixed/repeated event would hit the writer's equivalent-policy skip fast-path after the first write, starving the test of the concurrent-rename pressure it needs to actually catch a non-atomic writer"

requirements-completed: [SEC-02]

# Metrics
duration: 9min
completed: 2026-07-20
---

# Phase 16 Plan 01: Atomic Policy Writer Summary

**Same-dir temp+rename atomic write for `pkg/output/writer.go` with explicit 0644 chmod, proven torn-read-safe by a race-clean concurrent writer/reader test.**

## Performance

- **Duration:** 9 min
- **Started:** 2026-07-20T16:20:42Z
- **Completed:** 2026-07-20T16:29:18Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- `(*Writer).Write` in `pkg/output/writer.go` no longer calls `os.WriteFile` for the policy file; it now does `os.CreateTemp(filepath.Dir(path), ...) -> tmp.Write(data) -> tmp.Close() -> os.Chmod(tmpPath, 0644) -> os.Rename(tmpPath, path)`, matching the pattern already proven in `pkg/evidence/writer.go` and `pkg/hubble/health_writer.go`, with unprefixed error strings matching evidence/writer.go's convention.
- Added `TestWriter_AtomicNoLeftoverTempFiles`, proving no `.tmp-*` file survives a successful write and the resulting file is valid CNP YAML.
- Added `TestWriter_ConcurrentReaderNeverSeesPartialFile`, a `-race`-clean test that drives a writer goroutine (100 real rewrites, varying the port each time to force a genuine rename every iteration) concurrently with a reader goroutine polling the same path — every successful read unmarshals cleanly, proving the reader can never observe a torn file.
- All 8 pre-existing `pkg/output` tests remain green, including `TestWriter_FilePermissions` (0644 regression gate) and `TestWriter_NewFileCreation`.
- Full repo suite (`go test ./... -race`) — 10 packages — stays green; `golangci-lint run ./pkg/output/...` shows only the expected errcheck parity (bare `tmp.Close()`/`os.Remove()` on error paths), identical to the pattern already present in both analog writers — confirmed pre-existing debt (LINT-01), not new.

## Task Commits

Each task was committed atomically:

1. **Task 1: Replace os.WriteFile with atomic temp+rename in pkg/output/writer.go** - `55f7cc2` (feat)
2. **Task 2: Add atomicity + concurrent-reader tests to pkg/output/writer_test.go** - `1e354a2` (test)

_Worktree mode: no separate plan-metadata commit here — SUMMARY.md and REQUIREMENTS.md are committed together below per the parallel-executor protocol._

## Files Created/Modified
- `pkg/output/writer.go` - `(*Writer).Write`'s file-write step replaced with same-dir atomic temp+rename, explicit 0644 chmod before rename
- `pkg/output/writer_test.go` - two new tests: no-leftover-temp-file assertion, and a `-race`-clean concurrent writer/reader property test

## Decisions Made
- Added explicit `os.Chmod(tmpPath, 0644)` between `tmp.Close()` and `os.Rename()` — required deviation from both analogs (neither chmods) because this file's 0644 output is an observed contract for GitOps readers, pinned by the pre-existing `TestWriter_FilePermissions` test. This was the plan's own directive (Task 1, "DELIBERATE DEVIATION from the analog"), not an executor-initiated change.
- Concurrent-reader test varies the destination port per iteration (`8000+i`) rather than repeating an identical event, so every `w.Write` call is a genuine content change forcing a real rename cycle — a fixed/repeated event would fall into the writer's "policy unchanged, skip write" fast path after the first iteration and starve the test of concurrent-rename pressure.

## Deviations from Plan

None - plan executed exactly as written (Task 1's chmod addition and the error-message text for the new chmod step were explicitly within the plan's own instructions/discretion; no Rule 1-4 auto-fixes were needed — build and both new tests passed on the first attempt).

## Issues Encountered
None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- SEC-02 is fully satisfied: `pkg/output/writer.go` is torn-read-safe ahead of Phase 18's query tools, which will read `policies/<ns>/<workload>.yaml` concurrently with the writing pipeline.
- No blockers for the rest of Phase 16 (SRV-02/SRV-03, `cpg mcp` stdout hardening — separate plan/wave, unrelated files).

---
*Phase: 16-mcp-server-foundation-write-safety*
*Completed: 2026-07-20*
