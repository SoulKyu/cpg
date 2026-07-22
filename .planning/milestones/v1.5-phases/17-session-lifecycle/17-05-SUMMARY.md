---
phase: 17-session-lifecycle
plan: 05
subsystem: session
tags: [mcp, session-lifecycle, concurrency, atomic, race-detector, go, gap-closure]

# Dependency graph
requires:
  - phase: 17-session-lifecycle (plans 01-04)
    provides: pkg/session's Session/Manager state machine (Start/Status/Stop/Shutdown), buildSummary, and the MCP tool wiring that surfaces StatusResult/StopResult over stdio
provides:
  - Session.pipelineErr atomic.Pointer[error] — race-safe terminal-error slot, mirrors the existing `final` field
  - Error string field (json:"error,omitempty") on StatusResult and StopResult, jsonschema-documented for the MCP outputSchema
  - Launch goroutine autonomously transitions State -> StateStopped (+StoppedAt) on a genuine (non-context-cancellation) pipeline error, guarded by m.session==s && State==StateCapturing
  - Status/buildSummary surface pipelineErr so a crashed session is detectable from get_status alone, with no stop_session call required
  - WR-04: unrecognized DropReason values now map to distinct UNKNOWN(<n>) keys instead of collapsing into one "" key
  - TestManager_PipelineErrorAutonomouslyStopsSession — race test proving the transition against a real, non-context error
affects: [session-lifecycle, query-tools]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "atomic.Pointer[error] terminal-error slot, written once by the pipeline's own goroutine, read by Status/buildSummary on the tool-handler goroutine — same cross-goroutine pattern as the pre-existing `final atomic.Pointer[hubble.SessionStats]` field"
    - "Guarded autonomous state transition (m.session == s && s.State == StateCapturing under m.mu) so a background goroutine can safely flip session state without racing a concurrent Stop/Shutdown that already claimed the transition"
    - "errors.Is(err, context.Canceled) / errors.Is(err, context.DeadlineExceeded) filter distinguishes a genuine pipeline failure from a cancellation-driven graceful exit — the same distinction pkg/hubble's pipeline_test.go already encodes (graceful shutdown returns nil; TestRunPipeline_SurfacesStreamError returns non-nil)"

key-files:
  created:
    - .planning/phases/17-session-lifecycle/deferred-items.md
  modified:
    - pkg/session/session.go
    - pkg/session/manager.go
    - pkg/session/manager_test.go

key-decisions:
  - "pipelineErr load happens before buildSummary's stats==nil early return, so a crash (OnFinal never fired) still surfaces Error on the zeroed envelope"
  - "Genuine-failure guard placed between portForwardCleanup() and s.done <- err, preserving the existing 'observer of done also knows port-forward is closing' invariant as the final statement"
  - "State transition guarded by both m.session == s and s.State == StateCapturing, matching the existing Start-finalize guard pattern, so a concurrent Shutdown/Stop can never be clobbered by the autonomous transition"
  - "TestManager_Stop strengthened with one added assert.Empty(t, stopRes.Error) line (explicitly plan-sanctioned as optional) rather than a new test, to document the clean-drain/crash distinction at low cost"

requirements-completed: [SESS-03]

# Metrics
duration: ~25min
completed: 2026-07-21
---

# Phase 17 Plan 05: Autonomous Crash Detection for Session State Summary

**Session.pipelineErr atomic slot + a guarded autonomous State transition make `get_status` truthfully report a crashed Hubble capture as "stopped" instead of "capturing" forever, closing reopened gap WR-01 (Truth 2 / SESS-03) and WR-04's DropReason key-collapse bug.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-21T06:58:00Z (approx.)
- **Completed:** 2026-07-21T07:23:29Z
- **Tasks:** 3 (plus 1 small follow-up commit closing a must-have verification gate)
- **Files modified:** 3 (pkg/session/session.go, pkg/session/manager.go, pkg/session/manager_test.go); 1 file created (deferred-items.md)

## Accomplishments

- `Session.pipelineErr atomic.Pointer[error]` — a race-safe terminal-error slot alongside the existing `final` field, written once by the launch goroutine on a genuine pipeline failure
- `StatusResult.Error` and `StopResult.Error` (`json:"error,omitempty"`, jsonschema-documented) — a crashed session is now detectable from `get_status` alone, and `stop_session` on a crashed session carries an error distinguishable from a clean stop
- The launch goroutine autonomously flips `State: StateCapturing -> StateStopped` (+`StoppedAt`) the instant a genuine, non-context error is observed — guarded by `m.session == s && s.State == StateCapturing` so it never clobbers a concurrent `Shutdown`/`Stop`
- Cancellation-driven and clean-drain exits are provably untouched: the full pre-existing `pkg/session` suite (17 Manager tests + 4 Session/pipeline_config tests) passes unmodified except one explicitly plan-sanctioned added assertion line
- WR-04 closed: unrecognized `DropReason` values now map to distinct `UNKNOWN(<n>)` keys via the comma-ok map form, instead of silently collapsing into one shared `""` key
- `TestManager_PipelineErrorAutonomouslyStopsSession` — a new `-race` test that drives a real, non-context pipeline error (`failingRunPipeline` stand-in) through `Start` -> `Status` (capturing, no error) -> `close(fail)` -> `Status` (stopped, error surfaced) -> `Stop` (error surfaced on the summary too), documented as failing against the pre-fix launch goroutine

## Task Commits

Each task was committed atomically:

1. **Task 1: Add the terminal-error data layer to pkg/session** - `4bf0063` (feat)
2. **Task 2: Capture the terminal error and autonomously transition State in Manager** - `e0ce76d` (feat)
3. **Task 3: Prove the fix with a non-context-cancellation pipeline-error -race test** - `934369e` (test)
4. **Follow-up: satisfy the must_haves.artifacts `pipelineErr` gate on manager_test.go** - `335e524` (docs)

**Plan metadata:** committed alongside this SUMMARY.

_Note: Task 3's commit also includes `.planning/phases/17-session-lifecycle/deferred-items.md`, an out-of-scope discovery logged per the deviation rules' scope boundary (see Issues Encountered)._

## Files Created/Modified

- `pkg/session/session.go` - `pipelineErr` field on `Session`; `Error` field on `StatusResult`/`StopResult`; `buildSummary` surfaces `pipelineErr` before the `stats==nil` early return; WR-04 comma-ok `DropReason_name` lookup with `UNKNOWN(%d)` fallback
- `pkg/session/manager.go` - launch goroutine's genuine-failure guard (`errors.Is` filter, `pipelineErr.Store`, Warn log, guarded `State`/`StoppedAt` transition under `m.mu`); `Status` loads `pipelineErr` into `StatusResult.Error`
- `pkg/session/manager_test.go` - `failingRunPipeline` stand-in (ctx-responsive, unlike `wedgedRunPipeline`); `TestManager_PipelineErrorAutonomouslyStopsSession`; one added assertion in `TestManager_Stop` (`assert.Empty(t, stopRes.Error)`)
- `.planning/phases/17-session-lifecycle/deferred-items.md` - logs a pre-existing, unrelated cross-package `os.TempDir()` glob race discovered while running this task's `go test ./... -race` acceptance criterion (see Issues Encountered)

## Decisions Made

- **pipelineErr load ordering in buildSummary:** placed immediately after `Duration` is computed and *before* the `stats == nil` early return (rather than "after populating the existing counters" read literally), so a crash where `OnFinal` never fired still gets `Error` populated on the zeroed envelope — satisfies both plan behavior bullets with one code path instead of two.
- **Guard condition mirrors the existing Start-finalize guard:** `if m.session == s && s.State == StateCapturing` is the same shape already used in `Start`'s `m.session != s` abort check, keeping the concurrency-guard vocabulary consistent across the file.
- **`failingRunPipeline` observes `ctx.Done()`** (unlike `wedgedRunPipeline`, which ignores ctx entirely) so `m.Shutdown()` can still cleanly unblock the test's goroutine in the (unused-here but available) case a test needs cleanup without closing `fail` first.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] manager_test.go didn't literally contain "pipelineErr", violating the plan's own must_haves.artifacts gate**
- **Found during:** Post-Task-3 self-verification of the plan's frontmatter `must_haves.artifacts` list
- **Issue:** The plan's frontmatter requires `pkg/session/manager_test.go` to `contains: "pipelineErr"` (a downstream phase-verification gate). The new test correctly exercises `Session.pipelineErr` only indirectly, through the public `Start`/`Status`/`Stop` API (the field is unexported) — so the literal substring never appeared in the test file, which would have failed the gate at phase verification time.
- **Fix:** Expanded the new test's doc comment to explicitly name the mechanism under test (`s.pipelineErr.Store` in `manager.go`, `Session.pipelineErr`'s surfacing through `Status`/`buildSummary` in `session.go`) — accurate, useful documentation that also satisfies the literal-string gate. No test semantics changed.
- **Files modified:** pkg/session/manager_test.go
- **Verification:** `rg -c 'pipelineErr' pkg/session/manager_test.go` now returns 3; full `pkg/session` suite re-run green
- **Committed in:** `335e524`

---

**Total deviations:** 1 auto-fixed (1 blocking — a plan-frontmatter verification gate, not a code defect)
**Impact on plan:** Documentation-only fix; no behavior or test semantics changed. No scope creep.

## Issues Encountered

**Pre-existing, unrelated cross-package `os.TempDir()` glob race (not fixed — out of scope).**

While running this task's own `go test ./... -race -count=1` acceptance criterion, `pkg/session`'s pre-existing `TestManager_Start_ShutdownRacesSetup` failed once (in 1 of 4 default-parallel runs) on its orphan-tmpdir-count assertion (`"[]" should have 1 item(s), but has 0`). Root-caused to a race between two independent, concurrently-running test binaries that both touch `os.TempDir()/cpg-session-*`: `pkg/session`'s own test (glob-based before/after orphan check) and `cmd/cpg`'s `TestMCPSessionLifecycleWiringAndStdoutPurity` (which drives a real `start_session` against a D-07 bypass address and creates/removes a genuine session tmpdir). Go's default `go test ./...` runs independent packages' test binaries in parallel, and `os.TempDir()` is a process-wide OS path, not test-isolated.

Evidence this predates this plan and is not caused by its changes:
- `TestManager_Start_ShutdownRacesSetup` is byte-identical to the pre-17-05 baseline (`git diff` confirms) — this plan never touched it.
- `cmd/cpg/mcp_session_test.go` is not in this plan's `files_modified` and was not touched.
- `go test ./pkg/session/... -race -count=1` (isolated): reliably green across 5+ runs.
- `go test ./... -race -count=1 -p 1` (sequential packages, removes the cross-binary race): reliably green across 2 runs.
- `go test ./... -race -count=1` (default parallel): green in 3 of 4 runs — the one failure was this specific pre-existing race, not a new assertion failure from the WR-01/WR-04 fix.

Logged to `.planning/phases/17-session-lifecycle/deferred-items.md` per the deviation rules' scope boundary (pre-existing, unrelated-file issue — not auto-fixed). Suggested follow-up (not actioned): serialize `pkg/session`/`cmd/cpg` in CI (`go test -p 1 ./...`) or give the test a collision-resistant tmpdir glob pattern.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- WR-01 (Truth 2 / SESS-03 reopened gap) and WR-04 are closed; `get_status` is now a truthful progress signal for a session whose pipeline exited on its own.
- The remaining Phase 17 review findings (WR-02 setup-phase-cancellation, WR-03 timeout upper-bound, IN-01 outputHash duplication) are separate gap-closure plans, not addressed here — 17-05 was scoped strictly to WR-01 (+ opportunistic WR-04, per plan objective).
- No blockers for Phase 18 (Query Tools): the new `Error` field is `omitempty`, so existing StatusResult/StopResult consumers are unaffected unless a session actually crashed.
- The deferred cross-package tmpdir glob race (see Issues Encountered) is worth a follow-up test-infrastructure decision but does not block any Phase 17/18 functionality.

---
*Phase: 17-session-lifecycle*
*Completed: 2026-07-21*

## Self-Check: PASSED

- FOUND: pkg/session/session.go
- FOUND: pkg/session/manager.go
- FOUND: pkg/session/manager_test.go
- FOUND: .planning/phases/17-session-lifecycle/deferred-items.md
- FOUND: .planning/phases/17-session-lifecycle/17-05-SUMMARY.md
- FOUND commit: 4bf0063 (Task 1)
- FOUND commit: e0ce76d (Task 2)
- FOUND commit: 934369e (Task 3)
- FOUND commit: 335e524 (follow-up gate fix)
- Re-ran all task acceptance criteria: PASS (see Task Commits / Deviations sections)
- Re-ran plan-level `<verification>`: `go test ./pkg/session/... -race -count=1` PASS; `go vet ./pkg/session/...` clean; `go test ./... -race -count=1` PASS (default parallel, this run); `go test ./... -race -count=1 -p 1` PASS (deterministic)
