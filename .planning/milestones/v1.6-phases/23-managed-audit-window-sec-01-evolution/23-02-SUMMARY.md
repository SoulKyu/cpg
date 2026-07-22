---
phase: 23-managed-audit-window-sec-01-evolution
plan: 02
subsystem: infra
tags: [kubernetes, cilium-endpoint, audit-window, state-machine, watch]
status: complete

# Dependency graph
requires:
  - phase: 23-01
    provides: "pkg/k8s.ExecCiliumDbg/ReadPolicyAuditMode/SetPolicyAuditMode/CheckDaemonAuditMode/FindAgentPodForNode primitives this Manager binds seams to"
provides:
  - "pkg/auditwindow.Manager — Open/Close/Shutdown state machine driving the per-endpoint audit window lifecycle"
  - "pkg/auditwindow.RevertResult — per-endpoint revert success/failure reporting"
affects: [23-03, 23-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Manager mirrors pkg/session.Manager's SESS-05 shape exactly: seam function vars bound in NewManager to real pkg/k8s primitives, sync.Once-guarded Close, unconditionally-bounded Shutdown"
    - "ours map keyed on CiliumEndpoint ObjectMeta.UID (types.UID), never the reusable per-node integer Status.ID; current ID re-resolved fresh at revert time via a listCEFn-based resolveCurrentIDFn seam"
    - "rootCtx.Done()-triggered background goroutine spawned from Open calls Shutdown automatically — ctx cancellation alone reverts every flip, even if the CLI caller forgets to call Shutdown explicitly"
    - "Close's revert sweep fans out one goroutine per endpoint (not a sequential loop), so one wedged setFn transport can never block the others from completing and reporting a result"
    - "New-endpoint watcher is a deliberate one-line re-Watch()-with-bounded-backoff reconnect loop, not a cache.Reflector/informer (locked no-parallel-construct decision)"

key-files:
  created:
    - pkg/auditwindow/manager.go
    - pkg/auditwindow/manager_test.go
  modified: []

key-decisions:
  - "Daemon-wide precondition: preconditionFn error is treated as undetermined (warn-and-proceed); active==true with no error hard-refuses; active==false with no error proceeds silently — matches k8s.CheckDaemonAuditMode's own RBAC-forbidden-collapses-to-(false,nil) convention"
  - "resolveCurrentIDFn's default implementation re-lists the namespace via the existing listCEFn seam and filters by UID, rather than adding a Get-by-name client path — zero new client-construction idioms"
  - "The rootCtx-cancellation-triggers-Shutdown goroutine is spawned from Open (not NewManager), since nothing exists to revert before Open runs"
  - "Shutdown reuses Close's own sync.Once internally (calls m.Close in a goroutine wrapped by an independent bounded deadline) rather than a second teardown construct — grep confirms exactly one sync.Once in the file"

requirements-completed: [AUD-03]

# Metrics
duration: ~45min
completed: 2026-07-22
---

# Phase 23 Plan 02: pkg/auditwindow Manager Summary

**`pkg/auditwindow.Manager`: SESS-05-shaped Open/Close/Shutdown state machine that flips per-endpoint PolicyAuditMode via injected pkg/k8s seams, keyed on CiliumEndpoint UID, with a concurrent per-endpoint bounded revert fan-out and a bounded-backoff new-endpoint watcher.**

## Performance

- **Duration:** ~45 min
- **Tasks:** 3 completed
- **Files modified:** 2 (both created)

## Accomplishments

- `Manager.Open` calls the daemon-wide precondition seam first: hard-refuses when policy-audit-mode is determined active cluster-wide, warns and proceeds when the check itself is undetermined (an error), then lists the namespace's `CiliumEndpoints`, applies read-then-flip-if-needed per endpoint, and records only cpg-flipped endpoints in the `ours` map keyed on `ObjectMeta.UID`.
- `Manager.Close` is guarded by a single `sync.Once`: it fans out one goroutine per recorded endpoint, re-resolving each endpoint's current integer ID fresh from its UID (via `resolveCurrentIDFn`, defaulting to a `listCEFn`-based re-list-and-filter) before reverting, and returns a `RevertResult` naming every endpoint's individual success/failure — a second call returns the identical cached summary with no double-flip.
- `Manager.Shutdown` unconditionally cancels the watcher's derived ctx, bounded-waits for `watchDone`, then runs the same `closeOnce`-guarded `Close` sweep inside an independent bounded deadline (scaled by endpoint count) — a wedged `setFn` transport logs a warning but can never block process exit.
- `Open` spawns a background goroutine watching `rootCtx.Done()` that calls `Shutdown` automatically, so cancelling the root context alone (SIGINT/SIGTERM/parent death) reverts every flip even if the CLI layer never explicitly calls `Shutdown`.
- `startWatcher` watches for newly-created/-modified `CiliumEndpoints` and applies the same `flipIfNeeded` helper; on `ResultChan()` closing while ctx is live it re-`Watch()`s with a bounded backoff (scaled off `stopWait`) rather than standing up a `cache.Reflector`/informer — the residual race (a brand-new endpoint may be enforced for the reconnect interval before its flip lands) is documented in a code comment, not solved.

## Task Commits

1. **Task 1: Manager scaffold + Open (precondition, list, read-then-flip, UID bookkeeping)** - `f2de4da` (feat)
2. **Task 2: Close/Shutdown bounded revert fan-out + per-endpoint reporting** - `4c9c66e` (feat)
3. **Task 3: New-endpoint watcher with bounded-backoff reconnect** - `42b3b8f` (feat)

**Plan metadata:** (this commit, pending)

## Files Created/Modified

- `pkg/auditwindow/manager.go` (446 lines) - `Manager`, `endpointRecord`, `RevertResult`, `NewManager`, `Open`, `flipIfNeeded`, `defaultResolveCurrentID`, `Close`, `Shutdown`, `startWatcher`, `consumeWatch`, `sleepOrDone`
- `pkg/auditwindow/manager_test.go` (453 lines) - `TestManager_Open_RefusesWhenDaemonAuditActive`, `TestManager_Open_ProceedsWhenPreconditionUndetermined`, `TestManager_Open_SkipsAlreadyAuditedEndpoint`, `TestManager_Close`, `TestManager_Close_UsesUIDNotReusedEndpointID`, `TestManager_Close_ReportsPerEndpointResult`, `TestManager_Shutdown_OnCtxCancel`, `TestManager_Shutdown_WedgedExecDoesNotBlock`, `TestManager_Watcher_FlipsNewEndpoint`, `TestManager_Watcher_ReconnectsOnChannelClose` — all 10 names match 23-VALIDATION.md's per-requirement map exactly

## Decisions Made

- Combined each task's TDD behavior/action split into one atomic commit per task (mirrors 23-01's own documented deviation), verified green before each commit.
- Added a default `watchCEFn` stub (`fakeWatch`, an unbuffered-channel `watch.Interface`) to the shared `newTestManagerCtx` test helper — without it, every Task 1/2 test's `Open` call would spawn a watcher goroutine calling through to `NewManager`'s real production `watchCEFn` (bound against a never-dialed fake `rest.Config`), repeatedly dialing `127.0.0.1:6443` on a bounded-backoff loop and logging via `zaptest.NewLogger(t)` after individual tests complete. This is a Rule 3 blocking-issue auto-fix, not a plan deviation in scope — the fix lives entirely in test infrastructure.
- `resolveCurrentIDFn`'s default implementation reuses the existing `listCEFn` seam (list-and-filter-by-UID) rather than introducing a `Get`-by-name client path, since the typed `CiliumEndpointInterface.Get` requires a name, not a UID, and the plan's "zero new client-construction idioms" constraint rules out adding a second lookup path.

## Deviations from Plan

None beyond the two additive, in-scope items noted above (Rule 3 test-infrastructure fix; a design clarification for `resolveCurrentIDFn`'s default that stays within the plan's explicit "seam defaulting to a fresh Get... by UID" instruction while respecting the "no new client-construction idioms" constraint stated elsewhere in the same plan). Plan executed as written, task-by-task.

## Issues Encountered

None. `go build ./...` green throughout; `git diff --exit-code go.mod go.sum` clean (zero new dependencies); grep confirms exactly one `sync.Once` construct and zero `time.AfterFunc` calls in `manager.go`.

## User Setup Required

None — no external service configuration required; no live cluster used anywhere in this plan's tests (fake `watch.Interface`, stubbed exec/read/set/precondition seams throughout).

## Verification

- `rtk proxy go build ./...` — clean.
- `rtk proxy go test ./pkg/auditwindow/... -count=1 -race` — all 10 named tests pass.
- `rtk proxy go test ./pkg/auditwindow/... ./pkg/k8s/... ./cmd/cpg/... -count=1 -race` (wave-level regression per 23-VALIDATION.md) — all three packages `ok` (cmd/cpg ~148s, matching the documented SSA/RTA structural-test cost; no regression introduced by this plan).
- `git diff --exit-code go.mod go.sum` — clean.
- `grep -c "sync.Once" pkg/auditwindow/manager.go` — 2 matches (one doc comment, one field declaration) = exactly one teardown construct; `time.AfterFunc` — 0 matches.

## Next Phase Readiness

- `pkg/auditwindow.Manager` (`Open`/`Close`/`Shutdown`, `RevertResult`) is ready for Wave 3's `cmd/cpg/audit_window.go` cobra command to drive directly per 23-RESEARCH.md's architecture diagram: construct via `NewManager(rootCtx, logger, config, binary)`, call `Open(ctx, ns)`, and let the ctx-cancellation-triggered `Shutdown` handle every exit path (explicit Ctrl+C/SIGTERM, TTL expiry via `context.WithTimeout` on `rootCtx`, or an explicit `Close`/`Shutdown` call).
- No blockers.

---
*Phase: 23-managed-audit-window-sec-01-evolution*
*Completed: 2026-07-22*

## Self-Check: PASSED
- FOUND: pkg/auditwindow/manager.go
- FOUND: pkg/auditwindow/manager_test.go
- FOUND: f2de4da (Task 1 commit)
- FOUND: 4c9c66e (Task 2 commit)
- FOUND: 42b3b8f (Task 3 commit)
