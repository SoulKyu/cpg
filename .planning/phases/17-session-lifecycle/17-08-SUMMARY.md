---
phase: 17-session-lifecycle
plan: 08
subsystem: session
tags: [mcp, session-lifecycle, concurrency, atomic, race-detector, go, gap-closure]

# Dependency graph
requires:
  - phase: 17-session-lifecycle (plans 01-06)
    provides: pkg/session's Session/Manager state machine, the WR-01-in-17-05 autonomous crash transition, and the WR-02/WR-03-in-17-06 setup-cancellation + duration-ceiling fixes that this plan's fresh post-gap-closure review re-examined
provides:
  - "launch goroutine's genuine-failure guard reclassified on sessionCtx.Err() == nil instead of errors.Is(err, context.Canceled/DeadlineExceeded) — a SCOPED context.DeadlineExceeded from an unrelated timeout (e.g. pkg/hubble/client.go's dial timeout) derived from a still-healthy sessionCtx now correctly autonomously stops the session instead of being misclassified as an intentional teardown"
  - "s.cancel() now runs on the autonomous-exit path, releasing the sessionCtx registration on m.rootCtx (INFO ctx-registration-leak closed)"
  - "Session.explicitStopSeen atomic.Bool — tracks whether Stop() itself was ever called, independent of State, consumed via Swap(true) at both of Stop's buildSummary call sites"
  - "AlreadyStopped now means 'stop_session was already called' rather than 'State is StateStopped' — the first explicit stop_session after an autonomous crash correctly reports false (D-03 restored)"
  - "TestManager_ScopedDialTimeoutAutonomouslyStopsSession and TestManager_FirstStopAfterAutonomousCrashIsNotAlreadyStopped — -race regression tests, both proven to fail against pre-fix code"
affects: [session-lifecycle, query-tools]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Classify a background goroutine's exit as genuine-failure-vs-intentional-teardown on whether the goroutine's OWN ctx was cancelled (sessionCtx.Err() == nil), never on the KIND of error returned — errors.Is(err, context.DeadlineExceeded) is unsafe whenever the goroutine's own work can derive an unrelated SCOPED child context.WithTimeout from a healthy parent ctx (pkg/hubble/client.go's dial timeout is exactly this shape)"
    - "Decouple 'was this idempotent call already made' from 'has the underlying resource already reached its terminal state' via a dedicated atomic.Bool (explicitStopSeen) consumed only through Swap(true) — needed whenever a resource can reach its terminal state through more than one code path (here: Stop()'s own stopOnce.Do teardown, OR the launch goroutine's autonomous crash transition)"

key-files:
  created: []
  modified:
    - pkg/session/manager.go
    - pkg/session/session.go
    - pkg/session/manager_test.go

key-decisions:
  - "Guard rewritten to sessionCtx.Err() == nil rather than adding more errors.Is exclusions — sessionCtx.Err() is the only true 'this was on purpose' signal (only Stop/Shutdown/rootCtx cancellation sets it), so it correctly subsumes every future scoped-timeout shape, not just the one WR-01 found"
  - "s.cancel() placed after the m.mu-guarded transition block, inside the same genuine-failure guard, not restructured into a separate step — idempotent, a no-op for the already-exited pipeline, and safe w.r.t. Start's context.AfterFunc(sessionCtx, setupCancel) since stopSetupOnShutdown already un-registered that AfterFunc before runPipeline returned"
  - "explicitStopSeen placed on Session (not Manager) — same per-session concurrency-primitive placement as cancel/done/stopOnce, keeping Manager stateless across sessions"
  - "Swap(true) at BOTH buildSummary call sites in Stop (the state==StateStopped early-return AND the post-stopOnce path) — the early-return branch is exactly the path a first-post-crash Stop() takes, so it needed the same semantics as the normal teardown path"

requirements-completed: [SESS-03, SESS-04]

# Metrics
duration: ~13min
completed: 2026-07-21
---

# Phase 17 Plan 08: Crash Classification & already_stopped Semantics Summary

**Reclassified the launch goroutine's genuine-failure guard on `sessionCtx.Err() == nil` (not the returned error's identity) and added `Session.explicitStopSeen atomic.Bool` swapped at both `Stop()` call sites — closing WR-01 (a scoped dial-timeout `DeadlineExceeded` no longer wedges `get_status` at "capturing" forever) and WR-02 (the first `stop_session` after an autonomous crash no longer wrongly reports `already_stopped: true`).**

## Performance

- **Duration:** ~13 min
- **Started:** 2026-07-21T09:13:55Z (approx., orchestrator phase-execution-start timestamp)
- **Completed:** 2026-07-21T09:24:51Z
- **Tasks:** 2 completed (each TDD: RED test commit, then GREEN fix commit)
- **Files modified:** 3 (pkg/session/manager.go, pkg/session/session.go, pkg/session/manager_test.go)

## Accomplishments

- **WR-01 (BLOCKER) closed:** the launch goroutine's genuine-failure guard now reads `err != nil && sessionCtx.Err() == nil` instead of `err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)`. A scoped `context.DeadlineExceeded` produced by an unrelated child timeout (the exact shape `pkg/hubble/client.go`'s `waitForConnReady` returns on an unreachable/typo'd `--server` address) is no longer mistaken for an intentional Stop/Shutdown cancellation — `get_status` now truthfully reports `"stopped"` with the dial error surfaced, instead of `"capturing"` forever.
- **INFO fold-in closed:** `s.cancel()` now runs on the autonomous-exit path, releasing the `sessionCtx` registration on `m.rootCtx` that previously leaked for the process lifetime once a session crashed.
- **WR-02 (WARNING) closed:** `Session.explicitStopSeen atomic.Bool` tracks whether `Stop()` itself has ever returned a summary for a session, independent of *why* `State` reached `StateStopped`. Both of `Stop`'s `buildSummary` call sites now compute `AlreadyStopped` via `s.explicitStopSeen.Swap(true)` instead of a literal boolean — the first `stop_session` call after an autonomous crash transition (which never calls `Stop`) now correctly reports `already_stopped: false`, restoring the D-03 idempotency contract.
- Two new `-race` regression tests (`TestManager_ScopedDialTimeoutAutonomouslyStopsSession`, `TestManager_FirstStopAfterAutonomousCrashIsNotAlreadyStopped`), each verified failing against pre-fix code before the corresponding fix landed, plus a strengthened assertion in the pre-existing `TestManager_PipelineErrorAutonomouslyStopsSession`.
- Every pre-existing `pkg/session` test passes unmodified (except the one plan-sanctioned strengthening assertion): graceful stop, clean drain, genuine-cancellation, idempotent-second-stop, and concurrent-stop behavior are all unchanged.
- Whole-repo suite (`go test ./... -race -count=1`) green across all 11 packages.

## Task Commits

Each task was committed atomically via RED -> GREEN TDD cycles:

1. **Task 1 RED: failing test for scoped dial-timeout misclassification (WR-01)** - `f47b837` (test)
2. **Task 1 GREEN: classify genuine crash on sessionCtx cancellation, not error identity (WR-01)** - `cca60a1` (feat)
3. **Task 2 RED: failing test for first-stop-after-crash already_stopped (WR-02)** - `0690fd8` (test)
4. **Task 2 GREEN: track explicit-stop separately from State via atomic Swap (WR-02)** - `0159b66` (feat)

**Plan metadata:** committed alongside this SUMMARY.

## Files Created/Modified

- `pkg/session/manager.go` - launch goroutine's genuine-failure guard rewritten to `sessionCtx.Err() == nil`; `s.cancel()` added on the autonomous-exit path; both `Stop()` `buildSummary` call sites use `s.explicitStopSeen.Swap(true)`; `resolveSetup`'s unrelated `errors.Is(pfErr, context.DeadlineExceeded)` setup-timeout classification untouched
- `pkg/session/session.go` - `explicitStopSeen atomic.Bool` field added to `Session` (after `pipelineErr`); `StopResult.AlreadyStopped`'s doc comment updated to describe the new semantics
- `pkg/session/manager_test.go` - `TestManager_ScopedDialTimeoutAutonomouslyStopsSession` (WR-01), `TestManager_FirstStopAfterAutonomousCrashIsNotAlreadyStopped` (WR-02), and one added assertion in `TestManager_PipelineErrorAutonomouslyStopsSession` (`assert.False(t, stopRes.AlreadyStopped)`)

## Decisions Made

- `sessionCtx.Err() == nil` chosen over enumerating more `errors.Is` exclusions — it is the only signal that actually means "this exit was on purpose" (only Stop/Shutdown/rootCtx cancellation sets it), so it generalizes to any future scoped-timeout shape a pipeline dependency might introduce, not just the one WR-01 found.
- `s.cancel()` placed inside the existing genuine-failure guard, immediately after the `m.mu`-guarded transition block, rather than as a separate step — kept the goroutine's structure unchanged, per the plan's explicit "do not restructure the goroutine" instruction.
- `explicitStopSeen` modeled as a `Session`-scoped `atomic.Bool` (same placement pattern as `cancel`/`done`/`stopOnce`/`pipelineErr`) rather than a `Manager`-level map — keeps `Manager` stateless across sessions, consistent with the existing single-slot design.

## Deviations from Plan

None - plan executed exactly as written. One pre-commit self-correction is worth noting for transparency: while drafting Task 2's GREEN fix, an explanatory code comment near the second `buildSummary` call site initially restated the literal token `explicitStopSeen.Swap(true)`, which would have made the acceptance criterion's `rg -n 'explicitStopSeen\.Swap\(true\)'` gate return 3 matches instead of the required 2. Caught by running the acceptance-criteria `rg` checks before staging (same discipline the plan explicitly calls out for Task 1's guard-comment wording); reworded the comment to describe the mechanism without repeating the literal call expression, then re-verified the gate returns exactly 2 matches and re-ran the full test suite. This was resolved within the same edit-and-verify cycle and never reached a commit, so it is not a deviation from the shipped artifact — noted here only as a process detail.

## Issues Encountered

**Pre-existing, previously-documented cross-package `os.TempDir()` glob race reoccurred once (not fixed - out of scope, already tracked).**

During this plan's own `go test ./... -race -count=1` verification runs, `pkg/session`'s `TestManager_Start_ShutdownRacesSetup` failed once (in 1 of 3 whole-repo runs) on its orphan-tmpdir-count assertion — the identical race already root-caused and logged in `.planning/phases/17-session-lifecycle/deferred-items.md` during 17-05 (cross-binary interference on the shared `os.TempDir()/cpg-session-*` glob pattern between `pkg/session`'s own test and `cmd/cpg`'s `TestMCPSessionLifecycleWiringAndStdoutPurity`, which drives a real `start_session`). Confirmed unrelated to this plan's changes:
- The failing test is not in this plan's `files_modified` and asserts nothing about `sessionCtx.Err()`, `explicitStopSeen`, or `AlreadyStopped`.
- `go test ./pkg/session/... -race -count=1` (isolated, no cross-package interference): green across 4 consecutive runs during this plan's execution.
- `go test ./... -race -count=1` (whole-repo, default parallel): green on the run immediately before and the run immediately after the single failure.

Not re-logged to `deferred-items.md` (already fully documented there with root cause and suggested follow-up from 17-05); this note exists purely to record the reoccurrence for future traceability.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Both findings from the fresh post-gap-closure `17-REVIEW.md` (WR-01 blocker, WR-02 warning) are closed, plus the verifier's INFO ctx-registration-leak finding. `17-VERIFICATION.md`'s score-4/5 open items are resolved.
- `get_status` is now a truthful progress signal for the single most common connection-failure mode (unreachable/typo'd `--server`); `already_stopped` now means "was `stop_session` already called", honoring `stop_session`'s `IdempotentHint` contract (SESS-04) regardless of whether the session stopped via explicit teardown or autonomous crash.
- No blockers for Phase 18 (Query Tools): both changes are internal classification/tracking corrections with no new input surface, no new dependency, and no schema/API shape change — `StopResult.AlreadyStopped`'s JSON field and type are unchanged, only its computation.
- This closes the last tracked gap-closure plan for Phase 17 (17-05 WR-01/WR-04, 17-06 WR-02/WR-03, 17-07 D-02 docs, 17-08 this plan's fresh WR-01/WR-02/INFO). Phase 17 should be eligible for re-verification.

---
*Phase: 17-session-lifecycle*
*Completed: 2026-07-21*

## Self-Check: PASSED

- FOUND: pkg/session/manager.go
- FOUND: pkg/session/session.go
- FOUND: pkg/session/manager_test.go
- FOUND: .planning/phases/17-session-lifecycle/17-08-SUMMARY.md
- FOUND commit: f47b837 (Task 1 RED)
- FOUND commit: cca60a1 (Task 1 GREEN)
- FOUND commit: 0690fd8 (Task 2 RED)
- FOUND commit: 0159b66 (Task 2 GREEN)
- Re-verified source-assertion acceptance criteria via `rg`: `sessionCtx\.Err\(\) == nil` (1 match), `errors\.Is\(err,` (0 matches), `errors\.Is\(pfErr,` (1 match), `explicitStopSeen\s+atomic\.Bool` (1 match), `explicitStopSeen\.Swap\(true\)` (2 matches), `buildSummary\(true,` (0 matches), `buildSummary\(false,` (0 matches)
- Re-ran plan-level `<verification>`: `go test ./pkg/session/... -race -count=1` PASS (24 tests); `go vet ./pkg/session/...` clean; `go test ./... -race -count=1` PASS (11 packages, default parallel)
