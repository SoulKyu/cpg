---
phase: 17-session-lifecycle
plan: 09
subsystem: session-lifecycle
tags: [go, concurrency, state-machine, mcp, hubble, race-testing]

# Dependency graph
requires:
  - phase: 17-session-lifecycle (plans 17-01..17-08)
    provides: pkg/session Manager state machine, launch-goroutine autonomous-exit classification for non-nil errors (17-05/17-08), explicitStopSeen D-03 mechanism (17-08)
provides:
  - Autonomous-exit guard broadened to classify on sessionCtx.Err()==nil alone (not err != nil && sessionCtx.Err()==nil), so a clean/nil pipeline drain now autonomously transitions the session to stopped exactly like a crash does, minus the error string
  - Single-slot un-wedge after a clean autonomous exit: a subsequent start_session succeeds via the D-04 silent purge instead of being rejected "already running"
  - D-03 already_stopped contract independently pinned for the clean-exit path (first explicit stop_session reports false, second reports true)
affects: [18-query-tools, 19-security-hardening-e2e-validation, phase-17-verification]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Autonomous-exit classification is session-ctx-scoped, not error-shape-scoped: `if sessionCtx.Err() == nil` is the sole gate, with error-surfacing (pipelineErr store + Warn log vs Info log) nested and conditional inside it — extends the 17-05/17-08 pattern to cover the nil-error case symmetrically"

key-files:
  created: []
  modified:
    - pkg/session/manager.go
    - pkg/session/manager_test.go

key-decisions:
  - "Broadened the launch-goroutine's autonomous-exit guard from `err != nil && sessionCtx.Err() == nil` to `sessionCtx.Err() == nil` alone — sessionCtx.Err() being non-nil is the only true 'this was cancelled on purpose' signal (Stop/Shutdown/rootCtx), so a clean nil drain now transitions to stopped exactly like a genuine failure, with error-surfacing (pipelineErr + Warn) staying conditional on err != nil inside the broadened guard (Info log on a clean drain instead)"
  - "Relaxed TestManager_Start_PurgesStoppedSession's capturing-after-drain assertion to accept either 'capturing' or 'stopped' rather than adding test synchronization — the broadened guard means a fast-draining closedFlowSource session may already be autonomously stopped by the time Status() is polled right after Start(); both states equally prove the new session was created and is queryable, which is what D-04 requires there"
  - "Task 2 added zero production code — the D-03 already_stopped contract for the clean-exit path is satisfied by 17-08's existing explicitStopSeen mechanism unmodified, since it keys off 'was Stop() called', not off how State reached Stopped; the new test is a pure regression pin proving that composition holds"

patterns-established:
  - "Pattern (extends 17-05/17-08): autonomous pipeline-exit classification never inspects the returned error's kind/identity — it inspects sessionCtx.Err() only, then treats err's nilness purely as a logging/surfacing decision, not a transition-gating one"

requirements-completed: [SESS-03]

# Metrics
duration: ~10min
completed: 2026-07-21
---

# Phase 17 Plan 09: Clean-Nil-Exit Autonomous Stop Gap Closure Summary

**Broadened `pkg/session/manager.go`'s autonomous-exit guard from `err != nil && sessionCtx.Err() == nil` to `sessionCtx.Err() == nil` alone, so a pipeline that drains cleanly (nil error, e.g. Hubble Relay closing its gRPC stream on io.EOF) now autonomously transitions the session to stopped instead of wedging at "capturing" forever.**

## Performance

- **Duration:** ~10 min (commits span 2026-07-21T10:55:47Z–2026-07-21T10:58:25Z; includes prior context loading and three full-suite `-race` verification passes, including one whole-repo run across 11 packages)
- **Started:** 2026-07-21T10:55:47Z
- **Completed:** 2026-07-21T10:59:25Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments

- Closed the sole remaining Phase 17 verification gap (17-VERIFICATION.md, `gaps_found`, 4/5): `get_status` on a session whose pipeline drained to a CLEAN nil exit now truthfully reports `state: "stopped"` with an empty `error`, with zero `stop_session` calls required (Truth 2 / SESS-03)
- The single-slot wedge this gap caused is fixed as a direct consequence: a subsequent `start_session` after a clean autonomous exit now succeeds via the D-04 silent purge instead of being rejected "already running"
- Independently pinned that the D-03 `already_stopped` idempotency contract (landed in 17-08 for the crash path) also holds for the new clean-exit transition path, with zero additional production code — confirming `explicitStopSeen` already generalizes correctly
- Every pre-existing `pkg/session` test passes unchanged except one now-accepted-as-racy assertion, explicitly relaxed per the plan's scope; whole-repo `-race` suite (539 tests, 11 packages) stays green, including `cmd/cpg`'s MCP lifecycle/stdout-purity test

## Task Commits

Each task was committed atomically (TDD: RED → GREEN, plus a pure regression-pin commit for Task 2):

1. **Task 1 (RED): add failing clean-nil-exit regression** - `a185b9f` (test)
2. **Task 1 (GREEN): broaden the autonomous-exit guard + relax the racy assertion** - `d4fdbb1` (feat)
3. **Task 2: pin D-03 already_stopped contract for the clean-exit path** - `eb40580` (test)

**Plan metadata:** commit pending (this SUMMARY + REQUIREMENTS.md, made immediately after this document)

_TDD gate sequence confirmed in git log: `test(...)` (RED) before `feat(...)` (GREEN), consistent with the plan-level TDD requirement._

## Files Created/Modified

- `pkg/session/manager.go` - Launch goroutine's autonomous-exit guard broadened to `if sessionCtx.Err() == nil { ... }`; `s.pipelineErr.Store(&err)` + Warn log now nested inside `if err != nil`, with a new `else` branch emitting an Info log (`"session pipeline drained to a clean exit; transitioning to stopped"`) on a clean drain; the `m.mu`-guarded `State -> Stopped` transition and the trailing `s.cancel()` now run unconditionally inside the outer guard for both outcomes. `resolveSetup`'s unrelated `errors.Is(pfErr, context.DeadlineExceeded)` setup-timeout classification is untouched.
- `pkg/session/manager_test.go` - Added `TestManager_CleanDrainAutonomouslyStopsSession` (RED-then-GREEN regression: clean-drain autonomous stop + single-slot un-wedge via D-04) and `TestManager_FirstStopAfterCleanAutonomousExitIsNotAlreadyStopped` (D-03 contract pin for the clean-exit path); relaxed `TestManager_Start_PurgesStoppedSession`'s `assert.Equal(t, "capturing", status.State)` to accept either `"capturing"` or `"stopped"`.

## Decisions Made

- Guard broadening keeps error-surfacing (`pipelineErr` store + Warn log) strictly conditional on `err != nil`, nested inside the now-unconditional `sessionCtx.Err() == nil` outer guard — preserves the exact error semantics of the crash path (17-05/17-08) while adding the clean-drain case symmetrically, with no new state field and no new synchronization primitive.
- Chose to relax the one existing assertion the broadening made racy (`TestManager_Start_PurgesStoppedSession`) rather than add artificial synchronization (e.g. forcing the second session to stay capturing) — the plan explicitly scoped this as the fix's only test-suite fallout, and accepting either terminal-adjacent state is the more honest assertion given the new legitimate race.
- Verified (rather than assumed) that Task 2 requires zero production changes: traced `Stop()`'s early-return path and confirmed `explicitStopSeen.Swap(true)` is keyed on "was `Stop()` called," independent of *how* `State` reached `StateStopped` — so the new clean-exit transition path automatically inherits correct `AlreadyStopped` semantics with no code change, only a regression test proving it.

## Deviations from Plan

None — plan executed exactly as written. Both tasks' `<action>` and `<acceptance_criteria>` blocks were followed literally; all source gates (`rg` checks for the old/new guard patterns, the Info-log string, the preserved `errors.Is(pfErr,` line, the relaxed/unrelaxed `"capturing"` assertions) match exactly as specified in the plan.

One minor incidental improvement made while rewriting the guard's block comment (not a functional deviation): the prior comment's justification for `s.cancel()`'s safety asserted a happens-before ordering relative to `Start`'s deferred `stopSetupOnShutdown()` that 17-REVIEW.md's IN-01 finding correctly flagged as not actually holding (the launch goroutine can run concurrently with `Start`'s own defers on an instantly-failing pipeline). Since this exact comment block was being rewritten anyway to describe the broadened guard, the corrected justification (`setupCancel`'s idempotence + setup having already completed, not un-registration ordering) was used instead of carrying the inaccurate claim forward. No behavioral change; IN-01 itself was Info-tier and not in this plan's scope, so no separate fix-tracking was warranted.

## Issues Encountered

None. The RED test failed against pre-fix code exactly as predicted (5s `require.Eventually` timeout, `Condition never satisfied`), the GREEN fix made it pass immediately, `-count=10` on the relaxed assertion showed zero flakes, and Task 2's test passed on first run since Task 1's fix was already present in the tree within this same execution (exactly as the plan's own text anticipated: "after Task 1 it passes with NO further production change").

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Phase 17's sole remaining verification gap (Truth 2 / SESS-03, `gaps_found` in 17-VERIFICATION.md) is closed: `get_status` now truthfully reflects every autonomous exit path (clean or crashing), and the single-slot invariant no longer wedges on a session that died cleanly.
- Recommend re-running `/gsd-verify-phase 17` before proceeding to Phase 18 (Query Tools) planning — this plan does not itself re-run the verifier.
- The pre-existing, already-deferred cross-package `os.TempDir()` glob flake (WR-02 in 17-REVIEW.md, tracked in `deferred-items.md` since 17-05) is unchanged and intentionally out of this plan's scope — it was not one of this plan's two tasks.
- No blockers for Phase 18/19.

---

## Self-Check: PASSED

- FOUND: `pkg/session/manager.go` (broadened guard `if sessionCtx.Err() == nil {` confirmed present)
- FOUND: `pkg/session/manager_test.go` (`TestManager_CleanDrainAutonomouslyStopsSession` and `TestManager_FirstStopAfterCleanAutonomousExitIsNotAlreadyStopped` confirmed present)
- FOUND: `.planning/phases/17-session-lifecycle/17-09-SUMMARY.md`
- FOUND commit `a185b9f` (test: RED)
- FOUND commit `d4fdbb1` (feat: GREEN)
- FOUND commit `eb40580` (test: D-03 pin)
- FOUND commit `d37ced3` (docs: this SUMMARY)

---

_Phase: 17-session-lifecycle_
_Completed: 2026-07-21_
