---
phase: 10-classifier-core
plan: 02
subsystem: classifier
tags: [dropclass, zap, sync.Map, dedup, warn, tdd]

requires:
  - "10-01: pkg/dropclass package with Classify() and DropClassUnknown fallback"

provides:
  - "SetWarnLogger(*zap.Logger) — package-level logger injection; nil-safe"
  - "warnedUnknown sync.Map — deduplicated WARN once per unique unrecognized int32 value"
  - "Classify() extended: emits WARN on first-seen unknown reason, still returns DropClassUnknown"

affects:
  - "11-aggregator-suppression: phase 11 calls dropclass.SetWarnLogger(a.logger) before Run()"
  - "production ops: operators see exactly one WARN per unknown Cilium drop code seen in session"

tech-stack:
  added: []
  patterns:
    - "sync.Map.LoadOrStore for zero-alloc dedup on hot path (known reasons exit before reaching sync.Map)"
    - "sync.RWMutex around *zap.Logger pointer for concurrent logger injection"
    - "export_test.go pattern: internal test helper (ResetWarnStateForTest) visible to external test package"
    - "zaptest/observer + FilterMessageSnippet for substring WARN log assertion"

key-files:
  created:
    - pkg/dropclass/export_test.go
  modified:
    - pkg/dropclass/classifier.go
    - pkg/dropclass/classifier_test.go

key-decisions:
  - "sync.Map.LoadOrStore over mutex+map: zero-alloc on the already-seen fast path; all hot-path known reasons exit at the dropReasonClass map lookup before reaching warnedUnknown"
  - "RWMutex around *zap.Logger pointer (not sync/atomic.Pointer): logger injection is rare, read path is concurrent-safe with RLock"
  - "FilterMessageSnippet not FilterMessage for log assertion: FilterMessage is exact-match, not substring — zaptest observer API distinction"
  - "export_test.go in package dropclass (not dropclass_test): gives ResetWarnStateForTest direct access to unexported warnedUnknown without exporting it to production callers"

metrics:
  duration: 3min
  completed: 2026-04-26
  tasks: 2
  files_created: 1
  files_modified: 2
---

# Phase 10 Plan 02: Unknown Dedup WARN Summary

**SetWarnLogger + sync.Map dedup: exactly one WARN per unique unrecognized DropReason int32 across the process session — 33 tests pass -race, go vet clean**

## Performance

- **Duration:** 3 min
- **Started:** 2026-04-26T20:15:26Z
- **Completed:** 2026-04-26T20:18:xx Z
- **Tasks:** 2 (TDD: RED then GREEN)
- **Files modified:** 2, created: 1

## Accomplishments

- `SetWarnLogger(*zap.Logger)` — package-level logger injection with nil-safety guard
- `warnedUnknown sync.Map` — LoadOrStore dedup: first call per unique int32 emits WARN, subsequent calls are zero-cost (no lock, no alloc)
- `Classify()` hot path unchanged for known reasons: map lookup exits before touching `warnedUnknown`
- 5 new test functions covering: single-warn, dedup (100x same value → 1 WARN), per-value (2 distinct unknowns → 2 WARNs), known-no-warn, nil-logger-safe
- `export_test.go` with `ResetWarnStateForTest()` for per-test dedup isolation
- All 33 tests pass `-race`; `go vet ./pkg/dropclass/...` and `go build ./...` clean

## Task Commits

1. **Task 1: Write failing tests (RED)** — `78eced3` (test)
2. **Task 2: Implement SetWarnLogger + dedup WARN (GREEN)** — `b1ab014` (feat)

## Files Modified

- `/home/gule/Workspace/team-infrastructure/cpg/pkg/dropclass/classifier.go` — added `warnLogger`, `warnLoggerMu`, `warnedUnknown`, `SetWarnLogger()`, extended `Classify()` with dedup-WARN path
- `/home/gule/Workspace/team-infrastructure/cpg/pkg/dropclass/classifier_test.go` — 5 new test functions; import zaptest/observer
- `/home/gule/Workspace/team-infrastructure/cpg/pkg/dropclass/export_test.go` — `ResetWarnStateForTest()` (created)

## Decisions Made

- `sync.Map.LoadOrStore` over `map+mutex`: zero-alloc on already-seen fast path; no lock contention after first call per value
- `sync.RWMutex` around `*zap.Logger`: logger injection is rare write; `RLock` for concurrent `Classify()` read path
- `export_test.go` in `package dropclass` (not `dropclass_test`): accesses unexported `warnedUnknown` without polluting production API
- `FilterMessageSnippet` not `FilterMessage`: zap observer API — `FilterMessage` is exact string match, `FilterMessageSnippet` is substring match

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed test assertion using FilterMessageSnippet instead of FilterMessage**
- **Found during:** Task 2 GREEN verification
- **Issue:** `logs.FilterMessage("unrecognized")` does exact match — returned 0 entries even though the WARN was emitted with message `"unrecognized Cilium DropReason — classified as Unknown; consider upgrading cpg"`
- **Fix:** Changed assertion to `logs.FilterMessageSnippet("unrecognized")` which does substring match
- **Files modified:** `pkg/dropclass/classifier_test.go`
- **Commit:** `b1ab014` (included in GREEN commit, no separate fix commit needed)

## Known Stubs

None — full implementation with real dedup logic and logger injection.

## Self-Check: PASSED

Files exist:
- pkg/dropclass/classifier.go — FOUND
- pkg/dropclass/classifier_test.go — FOUND
- pkg/dropclass/export_test.go — FOUND
- .planning/phases/10-classifier-core/10-02-unknown-dedup-warn-SUMMARY.md — FOUND (this file)

Commits exist:
- 78eced3 (RED) — FOUND
- b1ab014 (GREEN) — FOUND

## Next Phase Readiness

Phase 11 (aggregator suppression) can now call `dropclass.SetWarnLogger(a.logger)` before `a.Run()` to wire the logger. The dedup map persists for the session lifetime — operators see exactly one WARN per novel Cilium drop code version bump.

---
*Phase: 10-classifier-core*
*Completed: 2026-04-26*
