---
phase: 17-session-lifecycle
plan: 03
subsystem: session
tags: [go, session, concurrency, mutex, sync-once, state-machine, race-testing, hubble-pipeline]

# Dependency graph
requires:
  - phase: 17-session-lifecycle plan 01
    provides: "PipelineConfig.OnFinal func(SessionStats) nil-safe end-of-run stats hook (D-08)"
  - phase: 17-session-lifecycle plan 02
    provides: "pkg/session data layer (State/Session/StartArgs/StartResult/StatusResult/StopResult/buildSummary/defaultDuration) + buildPipelineConfig session-tmpdir-scoped PipelineConfig builder"
provides:
  - "pkg/session.Manager: mutex-guarded single-active-session state machine (Start/Status/Stop/Shutdown)"
  - "pkg/session.NewManager(rootCtx, logger, stdout io.Writer, cpgVersion string) *Manager — the constructor signature plan 17-04's composition root calls"
  - "TOCTOU-safe slot claim: the placeholder Session is published under m.mu BEFORE the slow synchronous setup, with a finalize guard (m.session != s) that aborts cleanly if a concurrent Shutdown raced the setup window"
  - "resolveSetup/resolveSetupFn: production kubeconfig/port-forward/cluster-dedup recipe behind an unexported, injectable seam (test-only override, not a NewManager parameter)"
  - "16-test -race suite proving SESS-01..06, D-01..D-04, Pitfall F/G, and both concurrency Blockers (TOCTOU slot claim, copy-under-lock reads) closed"
affects: [17-04-session-lifecycle]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "TOCTOU-safe slot claim: publish a StateCapturing placeholder under the lock BEFORE the slow synchronous setup runs, so two concurrent Starts can never both pass the SESS-02 check; a rollback closure and a post-setup finalize guard (m.session != s) release the slot on setup failure or a Shutdown that raced the setup window"
    - "copy-under-lock reads: every read of Session.State/StoppedAt/TmpDir/cancel/done happens while holding m.mu, and only the local copy is used after unlocking — no unlocked read can race Stop's writer"
    - "injectable production-method seam: NewManager binds an unexported resolveSetupFn field to the method value m.resolveSetup after construction; same-package tests override the field directly to gate or fail the setup window deterministically, with zero change to the exported constructor signature"
    - "sync.Once-guarded idempotent teardown (Stop) plus an independently two-stage bounded fan-out (Shutdown): pipeline-exit wait and tmpdir removal each have their own deadline, so a wedged step in either stage can never block process exit"

key-files:
  created:
    - pkg/session/manager.go
    - pkg/session/manager_test.go
  modified: []

key-decisions:
  - "stopWait=5s / removeWait=2s adopted as NewManager defaults (RESEARCH.md Open Q2, resolved); same-package tests shrink both to 100ms"
  - "sync.Once adopted for idempotent Stop (RESEARCH.md Open Q3, resolved) — concurrent Stop callers all block on the same teardown, then all return the same summary"
  - "resolveSetup's DeadlineExceeded detection scoped to the port-forward call specifically (errors.Is on k8s.PortForwardToRelay's returned error) — the actionable re-authenticate message fires exactly when Pitfall H's bounded setupCtx actually expires waiting on the port-forward; k8s.LoadKubeConfig itself takes no ctx parameter (an existing, unchanged limitation of that helper, not a regression introduced here)"
  - "Test barrier pattern: close(chan struct{}) to release racing goroutines together, rather than a sync.WaitGroup-based barrier — a closed channel broadcasts to all waiters simultaneously, which is the more reliable way to maximize the odds of a genuine mutex race in the concurrent-Start/concurrent-Stop tests"
  - "Two test-design races were found and fixed during Task 2 (not manager.go bugs) — see Deviations: TestManager_Stop needed to wait for the pipeline to naturally finish before calling Stop, and TestManager_ConcurrentShutdownAndStop's assertion was relaxed to accept the legitimate SESS-06 outcome"
  - "Skipped requirements mark-complete for SESS-01..06 — see Deviations (same judgment call plans 17-01/17-02 already established: the literal requirement text describes LLM-facing MCP tool behavior that only exists once 17-04 registers the tools)"

patterns-established:
  - "A Manager's only test-only seam (resolveSetupFn) is an unexported struct field bound to a method value in the constructor, not a constructor parameter — keeps the exported API surface stable for callers while still giving same-package tests full control over the slow/networked part of Start"

requirements-completed: []  # SESS-01..06 intentionally NOT marked complete by this plan — see Deviations

# Metrics
duration: ~28min
completed: 2026-07-21
---

# Phase 17 Plan 03: Session Manager (Single-Slot State Machine) Summary

**Mutex-guarded `pkg/session.Manager` (Start/Status/Stop/Shutdown) implementing the capturing→stopped→gone state machine with a TOCTOU-safe slot claim, copy-under-lock reads, an injectable setup seam, and a 16-test `-race` suite closing both concurrency Blockers identified in research.**

## Performance

- **Duration:** ~28 min
- **Completed:** 2026-07-21T05:01Z
- **Tasks:** 2 completed
- **Files modified:** 2 (both new)

## Accomplishments
- `Manager.Start` claims the single slot under `m.mu` BEFORE the slow synchronous setup (kubeconfig/port-forward/cluster-dedup) — a `StateCapturing` placeholder is published while the lock is held, so two concurrent `start_session` dispatches can never both pass the SESS-02 check; a rollback closure and a post-setup finalize guard (`m.session != s`) release the slot with no orphaned goroutine/tmpdir/port-forward on either a setup failure or a `Shutdown` that races the setup window
- The background pipeline context forks from `m.rootCtx` (the server-lifetime ctx captured once at construction), never from the tool-call's request ctx — verified structurally (`rg` for `context.WithCancel(m.rootCtx)`, zero matches for `reqCtx`/`setupCtx` reaching the goroutine)
- `resolveSetup` ports `generate.go`'s kubeconfig/port-forward/cluster-dedup recipe under a single `setupCtx` bounding the entire synchronous setup (Pitfall H), reachable through an injectable, unexported `resolveSetupFn` seam that same-package tests override to gate or fail the setup window deterministically with no cluster
- `Status`/`Stop`/`Shutdown` all copy `State`/`StoppedAt`/`TmpDir`/`cancel`/`done` out while holding `m.mu`, before any unlocked use — proven data-race-free under `-race` against `Status`/`Shutdown` hammering a concurrently-stopping session
- `Stop` is idempotent via `sync.Once` (concurrent stops all block on the same teardown, then all return the same summary) and never removes the tmpdir (D-01 retention); it recomputes `cluster-health.json`'s path inline via `evidence.HashOutputDir`, matching `buildPipelineConfig`'s exact formula, since `Session` carries no `outputHash` field
- `Shutdown` bounds the pipeline-exit wait and the tmpdir removal with two independent deadlines, so a single wedged step (proven via a `wedgedRunPipeline` fixture that never observes ctx) cannot block process exit
- The D-10 correlation log line ties the opaque `sess_<uuid>` handle to the internal evidence `SessionID` on one `Info` call
- 16 new tests, all `-race` clean, verified stable across 10 consecutive full runs plus a 20x-repeated stress run (560/560 passed) with zero data-race reports; full repo suite: 521 tests passing across 11 packages (up from 505 at plan 17-02's close), zero regressions

## Task Commits

Each task was committed atomically:

1. **Task 1: Create pkg/session/manager.go (single-slot state machine + bounded shutdown)** - `6674d8b` (feat)
2. **Task 2: Create pkg/session/manager_test.go (-race suite: SESS-01..06 + D-01..04 + Pitfall F/G + concurrency races + Shutdown)** - `9e8168e` (test)

**Plan metadata:** committed separately by the orchestrator after worktree merge (worktree-mode executor scope excludes STATE.md/ROADMAP.md).

## Files Created/Modified
- `pkg/session/manager.go` (411 lines) - `Manager` struct, `NewManager`, `Start` (TOCTOU-safe slot claim + finalize guard), `resolveSetup` (kubeconfig/port-forward/cluster-dedup recipe) + `dedupNamespaces`, `Status` (copy-under-lock), `Stop` (`sync.Once` idempotent, D-01 retention, inline `outputHash` recompute), `Shutdown` (two-stage bounded fan-out), `countGlob`
- `pkg/session/manager_test.go` (657 lines) - 16 tests: `TestManager_Start`, `_RejectsConcurrent`, `_ConcurrentStartRejectsSecond`, `_PurgesStoppedSession`, `TestManager_Status`, `_StoppedSessionStaysQueryable`, `TestManager_Stop`, `_Idempotent`, `TestManager_UnknownSessionID`, `TestManager_ConcurrentStop`, `_ConcurrentStatusAndStop`, `_ConcurrentShutdownAndStop`, `TestManager_Shutdown`, `_WedgedStepDoesNotBlock`, `TestManager_Start_ShutdownRacesSetup`, `_SetupFailureRollsBackSlot`; plus local `closedFlowSource`/`blockingFlowSource` fakes (`flowsource.FlowSource` implementations), `wedgedRunPipeline`, and a `newTestManager` helper wiring 100ms bounded deadlines

## Decisions Made
- Followed the plan's exact lock-discipline ordering: slot claimed and session ctx forked EARLY (still holding `m.mu`), before `os.MkdirTemp`/`resolveSetupFn` — this is the plan's explicit correction over the milestone research's Code Example 2 (which released the lock before setup, a TOCTOU hazard the plan calls out by name)
- `stopWait`/`removeWait` defaulted to 5s/2s in `NewManager` (RESEARCH.md Open Q2, resolved), `sync.Once` adopted for idempotent Stop (Open Q3, resolved)
- Used `errors.Is(pfErr, context.DeadlineExceeded)` scoped to the port-forward call only, for the actionable re-authenticate error message (Pitfall H, Claude's Discretion) — `k8s.LoadKubeConfig` itself accepts no context parameter, an existing, unmodified limitation of that helper
- Skipped `requirements mark-complete` for SESS-01..06 — see Deviations

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed a test-design race causing non-deterministic `FlowsSeen` in `TestManager_Stop`**
- **Found during:** Task 2, first `-race` run
- **Issue:** `TestManager_Stop` called `m.Stop(id)` immediately after `m.Start(...)` returned, using a `closedFlowSource` whose two flows are already fully buffered in the channel by the time `StreamDroppedFlows` returns. `Stop`'s `s.cancel()` call and the aggregator's own channel-drain compete inside the aggregator's `select { case f, ok := <-in: ...; case <-ctx.Done(): ... }` loop — Go's `select` picks pseudo-randomly among simultaneously-ready cases, so if ctx cancellation becomes ready before (or during) the drain, the aggregator can exit having processed 0, 1, or 2 flows depending purely on scheduling. First run observed `flows_seen: 1` instead of the expected 2. This is a test-code defect, not a `manager.go` defect — `manager.go`'s `Stop` correctly cancels+waits exactly as specified.
- **Fix:** Added a `require.Eventually` wait (polling `Status().PolicyFileCount > 0`) before calling `Stop`, so the pipeline has already naturally drained and counted both flows (proven by the policy file landing on disk, which the aggregator only writes after draining the channel) before cancellation is ever introduced.
- **Files modified:** `pkg/session/manager_test.go`
- **Verification:** 10 consecutive `-race` runs plus a 20x-repeated single-invocation stress run (560/560 sub-test-runs passed), zero flakes after the fix
- **Committed in:** `9e8168e` (Task 2 commit — found and fixed before the first commit of this file)

**2. [Rule 1 - Bug] Relaxed an over-strict assertion in `TestManager_ConcurrentShutdownAndStop`**
- **Found during:** Task 2, first `-race` run
- **Issue:** The plan's action text states "Stop must not panic or error regardless of interleaving" when racing `Shutdown`. `Manager.Shutdown` (Task 1, faithfully implemented per its own explicit, unambiguous lock-discipline instructions) unconditionally sets `m.session = nil` immediately upon acquiring `m.mu` — this is a deliberate, correct part of the design (Shutdown is effectively an unconditional purge). If `Shutdown`'s lock acquisition wins the race against a concurrent `Stop`, `Stop` legitimately observes the slot already cleared and returns the well-formed SESS-06 "not found or expired" error rather than a summary. The original test asserted `Stop` must never error in this scenario, which is not an invariant the specified `Shutdown` design (or SESS-06/D-02's "purged session → not found" semantics elsewhere in the plan) actually guarantees, and the first `-race` run demonstrated the race firing in practice.
- **Fix:** Relaxed the assertion: `Stop`'s error, if any, must be exactly the well-formed SESS-06 shape (`"not found or expired"`) — never a panic or a malformed/different error. The stronger invariants (bounded completion time, zero `-race` reports, tmpdir removed) are unchanged and still asserted unconditionally.
- **Files modified:** `pkg/session/manager_test.go`
- **Verification:** 10 consecutive `-race` runs plus the 20x stress run, zero flakes; confirmed by direct code reading that `Shutdown`'s `m.session = nil` is the first statement inside its lock, matching Task 1's own acceptance criteria (already committed and verified in the prior task commit)
- **Committed in:** `9e8168e` (Task 2 commit — found and fixed before the first commit of this file)

---

**Total deviations:** 2 auto-fixed (both Rule 1 — test-code races/over-strict assertions found and corrected during Task 2's own `-race` verification pass, before either fix was committed). Zero deviations in `manager.go` itself: Task 1 was implemented and verified exactly as specified, and both fixes above confirm (rather than contradict) its correctness.
**Impact on plan:** No architectural or behavioral change to `manager.go`. Both fixes tightened the test suite's own correctness so it asserts exactly the invariants the specified concurrency design actually guarantees, rather than incidentally-true-most-of-the-time timing assumptions.

### Documentation-accuracy deviation (not a Rule 1-4 code deviation)

**3. Skipped `requirements mark-complete` for SESS-01, SESS-02, SESS-03, SESS-04, SESS-05, SESS-06**
- **Found during:** Pre-SUMMARY requirements review
- **Issue:** This plan's frontmatter lists `requirements: [SESS-01..06]`. Every one of those requirement texts in `.planning/REQUIREMENTS.md` (lines 20-25) describes end-to-end LLM-facing MCP tool behavior ("LLM can `start_session`...", "LLM can `get_status`...", "any session-scoped tool called with..."). This plan builds and exhaustively proves the `pkg/session.Manager` orchestration engine those tools will call, but registers zero MCP tools — no `start_session`/`get_status`/`stop_session` exists on the wire yet, and `cmd/cpg/mcp.go` does not yet call `mgr.Shutdown()` after `server.Run()` returns (this plan's own `<output>` section explicitly assigns both to 17-04). SESS-05 in particular ("transport termination... triggers full cleanup fan-out") is proven at the unit level here (`Shutdown()` is correct and bounded), but nothing yet *triggers* it from the actual MCP server lifecycle.
- **Action:** Cross-checked `.planning/phases/17-session-lifecycle/17-01-SUMMARY.md` and `17-02-SUMMARY.md`, both of which already established this identical judgment call for SESS-04 (17-01) and SESS-01/03/04 (17-02), for the identical reasoning. `.planning/REQUIREMENTS.md` traceability rows for SESS-01..06 (lines 81-86) are still "Pending" as of this plan's start.
- **Fix:** Did not call `requirements mark-complete` for SESS-01..06 in this plan. Left `requirements-completed: []` in this SUMMARY's frontmatter. Recommend 17-04 (where all three MCP tools actually register and `cmd/cpg/mcp.go` wires `Shutdown()` into the server lifecycle) perform the mark-complete calls, since that is where the literal requirement text becomes true end-to-end.
- **Files modified:** None (documentation-accuracy judgment call, not a code change)
- **Verification:** Confirmed via direct read of `.planning/REQUIREMENTS.md` lines 20-25, 81-86 and both prior plans' SUMMARY.md precedent
- **Committed in:** N/A (no REQUIREMENTS.md change made)

---

**Total deviations (including documentation-accuracy):** 3 (2 auto-fixed Rule 1 test corrections, 1 documentation-accuracy judgment call — no code impact from the third)

## Issues Encountered
None beyond the two test-design races documented above, which were found and resolved entirely within Task 2's own verification pass before that task was committed.

## User Setup Required
None - no external service configuration required. Every test runs against local fakes (`closedFlowSource`/`blockingFlowSource`); no real cluster, no MCP SDK.

## Known Stubs
None - `pkg/session/manager.go` is a pure backend orchestration layer with no UI rendering or unwired data paths. No stub patterns, placeholder values, or hardcoded empty responses were introduced.

## Threat Flags
None - every new surface this plan introduces (the `Manager` slot/state machine, `resolveSetup`'s kubeconfig/port-forward/cluster-dedup calls, the `resolveSetupFn` seam, the D-10 correlation log line) is exactly what this plan's own `<threat_model>` already covers (T-17-03-01..06, T-17-03-SC — DoS via wedged shutdown, DoS via hung setup, cross-goroutine tampering, session-handle spoofing, readonly-discipline elevation-of-privilege, DoS via concurrent Start, and supply-chain, respectively), each with a `mitigate` or `accept` disposition already proven by the test suite. No additional undocumented surface was introduced.

## Next Phase Readiness
- `pkg/session.Manager` is fully implemented, doc-commented, and proven correct under `-race`: `NewManager(rootCtx, logger, stdout io.Writer, cpgVersion string) *Manager` is the exact constructor signature 17-04's composition root calls; tool handlers call `mgr.Start(ctx, session.StartArgs{...pre-validated...})`, `mgr.Status(id)`, `mgr.Stop(id)`; `cmd/cpg/mcp.go` must call `mgr.Shutdown()` synchronously after `server.Run(...)` returns (both ctx-cancel and transport-close paths) — this plan's `<output>` instruction, carried forward unchanged
- The two concurrency Blockers (TOCTOU-safe slot claim, copy-under-lock State/StoppedAt reads) and the deep SESS-05 cleanup semantics are already fully proven at unit level under `-race` here — 17-04's own integration test only needs to prove the composition-root WIRING actually calls `Start`/`Status`/`Stop`/`Shutdown` correctly, not re-prove the concurrency safety underneath
- The `resolveSetupFn` seam is an unexported, test-only field — the exported `NewManager` signature is unchanged from what 17-02's SUMMARY already recorded for 17-04, so no downstream signature surprises
- No blockers for 17-04 (wave 4, depends on this plan)
- SESS-01..06 traceability remains `Pending` in `.planning/REQUIREMENTS.md` — intentional, see Deviations; 17-04 is the natural place to call `requirements mark-complete` once all three MCP tools are registered and `Shutdown()` is wired into the server lifecycle

---
*Phase: 17-session-lifecycle*
*Completed: 2026-07-21*

## Self-Check: PASSED

- FOUND: pkg/session/manager.go
- FOUND: pkg/session/manager_test.go
- FOUND: .planning/phases/17-session-lifecycle/17-03-SUMMARY.md (this file)
- FOUND: commit 6674d8b (Task 1)
- FOUND: commit 9e8168e (Task 2)
- FOUND: `func NewManager` in manager.go
- FOUND: `func (m *Manager) Start` in manager.go
- FOUND: `func (m *Manager) Status` in manager.go
- FOUND: `func (m *Manager) Stop` in manager.go
- FOUND: `func (m *Manager) Shutdown` in manager.go
- FOUND: `func (m *Manager) resolveSetup` in manager.go
- FOUND: `func countGlob` in manager.go
- FOUND: 16 top-level `func Test*` in manager_test.go (matches the plan's enumerated test list exactly)
- VERIFIED: `go build ./...` succeeds
- VERIFIED: `go vet ./pkg/session/...` succeeds
- VERIFIED: `go test ./pkg/session/... -race -count=1` green (28 tests: 12 pre-existing from 17-02 + 16 new)
- VERIFIED: `go test ./pkg/session/... -race -count=20` — 560/560 sub-runs green, zero data races (stress-tested stability of the concurrency-race tests)
- VERIFIED: `go test ./... -race -count=1` — 521 tests passing across 11 packages (up from 505 at 17-02 close), zero regressions
- VERIFIED: `golangci-lint run ./pkg/session/...` — 0 issues
- VERIFIED: `golangci-lint run ./...` — exactly the pre-existing 26 v1.4-debt findings (16 errcheck + 10 staticcheck), zero new issues
- VERIFIED: `rg -n 'm.session = s' pkg/session/manager.go` — placeholder claimed under m.mu before resolveSetupFn
- VERIFIED: `rg -n 'resolveSetupFn' pkg/session/manager.go` — field + NewManager default + Start call site all present
- VERIFIED: `rg -n 'context.WithCancel\(m.rootCtx' pkg/session/manager.go` — matches; zero matches for reqCtx/setupCtx reaching the goroutine
- VERIFIED: `rg -n 'evidence_session_id' pkg/session/manager.go` — matches, same Info call as session_id
- VERIFIED: `rg -n 'RemoveAll' pkg/session/manager.go` — 4 occurrences, all in Start's purge/rollback/finalize-guard branches or Shutdown; none in Stop, none inside the launch goroutine
- VERIFIED: `git diff b8bbbbd HEAD --name-only` — exactly `pkg/session/manager.go` + `pkg/session/manager_test.go` changed (files_modified constraint honored; session.go/pipeline_config.go from 17-02 untouched)
