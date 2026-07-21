---
phase: 17-session-lifecycle
reviewed: 2026-07-21T09:40:25Z
depth: standard
files_reviewed: 12
files_reviewed_list:
  - cmd/cpg/mcp.go
  - cmd/cpg/mcp_harness_test.go
  - cmd/cpg/mcp_session_test.go
  - cmd/cpg/mcp_tools.go
  - pkg/hubble/pipeline.go
  - pkg/hubble/pipeline_test.go
  - pkg/session/manager.go
  - pkg/session/manager_test.go
  - pkg/session/pipeline_config.go
  - pkg/session/pipeline_config_test.go
  - pkg/session/session.go
  - pkg/session/session_test.go
findings:
  critical: 0
  warning: 2
  info: 6
  total: 8
status: issues_found
---

# Phase 17: Code Review Report

**Reviewed:** 2026-07-21T09:40:25Z
**Depth:** standard
**Files Reviewed:** 12
**Status:** issues_found

## Summary

Re-review after gap-closure plan 17-08 landed. Verified first: `go build ./...`, `go vet` and `golangci-lint run` are clean on every in-scope file (the 11 lint issues reported for `./pkg/hubble/...` all live in `summary.go`/`health_writer.go`/`client.go` — pre-existing, out of this diff range), and `go test ./pkg/session/... ./pkg/hubble/... ./cmd/cpg/... -count=1 -race` passes in full.

**Prior-review findings — resolution status:**

- **WR-01 (prior, blocker-tier) — RESOLVED.** The launch goroutine now classifies a genuine failure on `sessionCtx.Err() == nil` (`pkg/session/manager.go:234`), not on the returned error's identity. I traced this against the actual producer of the previously-misclassified error (`pkg/hubble/client.go:109-121`, `waitForConnReady`'s scoped `context.WithTimeout` child): a scoped `DeadlineExceeded` from a healthy `sessionCtx` now correctly triggers the autonomous stopped-transition, while Stop/Shutdown/rootCtx cancellations remain untouched. The regression test `TestManager_ScopedDialTimeoutAutonomouslyStopsSession` (`manager_test.go:783-807`) mirrors `waitForConnReady`'s exact child-ctx shape, and `TestManager_PipelineErrorAutonomouslyStopsSession` covers the plain-error case. The follow-on `s.cancel()` on the autonomous-exit path (`manager.go:252`) correctly releases the session ctx's registration on `rootCtx` (the CancelFunc is idempotent; a concurrent later Stop's own `s.cancel()` is a no-op). One inaccuracy in the justifying comment — see IN-01 below; the code itself is safe.
- **WR-02 (prior, warning) — RESOLVED.** `Session.explicitStopSeen atomic.Bool` (`pkg/session/session.go:124`) with `Swap(true)` at both `buildSummary` call sites in `Stop()` (`manager.go:411` early-return branch, `manager.go:439` teardown branch) decouples `AlreadyStopped` from `State`. Traced every interleaving: first Stop after an autonomous crash → `Swap` returns false → `AlreadyStopped==false`; genuine second Stop → true; two concurrent first Stops → exactly one false (documented as acceptable in `TestManager_ConcurrentStop:426-429`). `TestManager_FirstStopAfterAutonomousCrashIsNotAlreadyStopped` (`manager_test.go:737-763`) pins the D-03 contract.

**Fresh findings this pass:** the fixed crash-classification guard is asymmetric — it handles every non-nil autonomous exit, but a **nil** autonomous exit (clean `io.EOF` stream end, a path `client.go` itself implements) still leaves the session in `capturing` forever AND blocks all future `start_session` calls (WR-01 below). Plus a cross-package flake vector in `manager_test.go`'s `os.TempDir()` glob counting (WR-02 below), and six Info items (three new, three carried unaddressed from the prior review).

No security vulnerabilities, hardcoded secrets, injection vectors, or crash paths were found. Evidence-path construction remains guarded by `evidence.ValidatePolicyRef`; the MCP argument surface is validated before reaching `pkg/session`; no debug artifacts or dangerous calls in scope.

## Warnings

### WR-01: A clean autonomous pipeline exit (`err == nil`, healthy `sessionCtx`) leaves the session `capturing` forever and blocks every future `start_session` — the nil-error sibling of the gap 17-05/17-08 fixed

**File:** `pkg/session/manager.go:234` (classification guard), interacting with `pkg/hubble/client.go:157-159` and `manager.go:106-111`
**Issue:**
The autonomous-transition guard fires only for non-nil errors:
```go
if err != nil && sessionCtx.Err() == nil {
```
But `RunPipelineWithSource` can return **nil** while `sessionCtx` is still healthy: `pkg/hubble/client.go`'s stream goroutine treats a clean `io.EOF` as a non-error end-of-stream (`client.go:157-159` — "Clean end-of-stream (not expected under Follow:true, but harmless)"), closes both flow channels without signaling `StreamErr`, the pipeline drains all stages, and `g.Wait()` returns nil. On that path the launch goroutine takes no action at all: no `State` transition, no `pipelineErr`, no `s.cancel()` (the 17-08 ctx-release also lives inside the `err != nil` branch — the session ctx stays registered on `rootCtx` until an explicit Stop). Consequences for a long-lived MCP server:

1. `get_status` reports `"state": "capturing"` with a still-ticking `elapsed` for a pipeline that has fully exited — the exact "session cannot self-report its own death" symptom 17-05/17-08 were written to eliminate, minus the error string.
2. `start_session` is rejected with "session ... already running; call stop_session first" (`manager.go:106-111`) because `State` is still `StateCapturing` — the dead session wedges the single slot until the client explicitly stops it.

The in-code comment (`manager.go:230-233`) declares the clean-drain path "deliberately left untouched ... so every pre-existing test keeps passing unchanged" — a test-suite-preservation rationale, not a product one. `client.go`'s own "harmless" verdict on EOF is only true for the one-shot CLI (the process exits right after the drain); for the MCP state machine the drained pipeline outlives its `capturing` label indefinitely. Reachability is low under `Follow:true` (a relay teardown normally surfaces as a non-EOF status error, which the fixed guard now handles), but the EOF branch exists in this codebase's own client precisely because it has been observed shapes-wise; when it fires, the failure mode is a silently wedged slot. Recovery exists (`stop_session` works normally and returns a correct summary), so this is a robustness gap, not data loss.

**Fix:** Treat any autonomous exit — nil or not — as terminal, keeping the error-surfacing bits conditional:
```go
if sessionCtx.Err() == nil { // autonomous exit: nobody asked this pipeline to stop
    if err != nil {
        s.pipelineErr.Store(&err)
        m.logger.Warn("session pipeline exited with error", zap.String("session_id", s.ID), zap.Error(err))
    } else {
        m.logger.Info("session pipeline drained to a clean exit; transitioning to stopped", zap.String("session_id", s.ID))
    }
    m.mu.Lock()
    if m.session == s && s.State == StateCapturing {
        s.State = StateStopped
        s.StoppedAt = time.Now()
    }
    m.mu.Unlock()
    s.cancel()
}
```
Note the test-suite impact this was scoped around: `closedFlowSource`-based tests that observe `"capturing"` after the drain completes (e.g. `TestManager_Start_PurgesStoppedSession:271`, which asserts the second session is `"capturing"` and can race the drain) must be updated to accept or await `"stopped"` — that churn is the fix's real cost, not the production logic.

---

### WR-02: `os.TempDir()`-wide `cpg-session-*` glob counting in `manager_test.go` races concurrently-running `cmd/cpg` package tests — cross-package CI flake vector

**File:** `pkg/session/manager_test.go:597-598/637-639` (`TestManager_Start_ShutdownRacesSetup`), `656-657/663-665` (`TestManager_Start_SetupFailureRollsBackSlot`), `836-837/873-875` (`TestManager_Start_ShutdownCancelsSetupCtx`)
**Issue:** Three tests assert "no orphaned tmpdir" by counting `filepath.Glob(filepath.Join(os.TempDir(), "cpg-session-*"))` before and after the exercised sequence and requiring equal lengths. That glob is process-global and machine-global, but `cpg-session-*` dirs are also created in the same `os.TempDir()` by another package's test binary: `cmd/cpg`'s `TestMCPSessionLifecycleWiringAndStdoutPurity` drives a real `Manager.Start` (via `os.MkdirTemp("", "cpg-session-*")`, `manager.go:154`) and later removes it at Shutdown. `go test ./...` runs package test binaries in parallel by default (`-p` = GOMAXPROCS), so a `cmd/cpg` session tmpdir created between one of these before/after glob pairs makes `after == before+1` (or `before-1` if removed in the window), failing `assert.Len(after, len(before))` spuriously. The windows are non-trivial — each spans a full Start+Shutdown sequence bounded at hundreds of milliseconds. This is a latent CI flake that will be misread as a real orphaned-tmpdir regression when it fires.
**Fix:** Scope the assertion to a test-private root instead of the shared global tmp: add an unexported `tmpRoot string` field to `Manager` (empty means `os.MkdirTemp("", ...)`'s default, preserving production behavior), have `Start` call `os.MkdirTemp(m.tmpRoot, "cpg-session-*")`, and set `m.tmpRoot = t.TempDir()` in `newTestManager`. The three tests then glob `filepath.Join(m.tmpRoot, "cpg-session-*")`, which no other package can touch. (Set-difference on the shared dir is not sufficient — the concurrent creator/remover races both directions.)

## Info

### IN-01: 17-08's `s.cancel()` safety comment asserts a false happens-before ordering (conclusion is right, justification is wrong)

**File:** `pkg/session/manager.go:245-252`
**Issue:** The comment justifying the autonomous-path `s.cancel()` states it is "safe w.r.t. Start's `context.AfterFunc(sessionCtx, setupCancel)`" because "Start's own deferred `stopSetupOnShutdown()` already un-registered that AfterFunc before this goroutine's `runPipeline` call returned." That ordering claim is false: Start's deferred calls run when `Start` returns (`manager.go:258`), while the launch goroutine starts at `manager.go:215` — with an instantly-failing `runPipeline` (e.g. an immediate dial error), the goroutine's `s.cancel()` can execute *before* Start's defers, firing the still-registered AfterFunc and invoking `setupCancel()`. The code is nevertheless safe — `setupCancel` is an idempotent `CancelFunc` for a `setupCtx` whose only consumer (`resolveSetupFn`) has already returned — but the comment pins the wrong invariant. A future maintainer who trusts it (e.g. registering a non-idempotent or effectful AfterFunc on `sessionCtx`) inherits a real ordering bug with a comment telling them it cannot happen.
**Fix:** Correct the comment: safety comes from `setupCancel`'s idempotence and setup having completed before the goroutine launches, not from any un-registration ordering — e.g. "safe even if this fires the still-registered AfterFunc: setupCancel is idempotent and setupCtx has no remaining consumers once resolveSetupFn returned."

---

### IN-02: `Stop()` racing a concurrent `Start()`'s D-04 purge returns a full summary for a purged session, referencing an already-removed tmpdir

**File:** `pkg/session/manager.go:385-411`, interacting with the purge at `manager.go:114-121`
**Issue:** `Stop` captures `s := m.session` under `m.mu`, releases the lock (`manager.go:394`), then builds the summary. A concurrent `Start` can acquire the lock in that window, take the D-04 purge branch for the same stopped session (`os.RemoveAll(m.session.TmpDir)`, slot replaced), after which the still-running `Stop` returns a well-formed `StopResult` whose `TmpDir`/`ClusterHealthPath` point at just-deleted paths — for a session that D-04/D-02 semantics say should now be "not found" (a purged session "simply has no Manager slot", `session.go:32-33`). A client issuing `stop_session` and `start_session` concurrently gets a summary whose artifact paths dangle. Benign staleness (no crash, no corruption; single-client sequential usage never hits it), but it is a small semantic contradiction with the documented purge model.
**Fix:** Re-validate the slot before the early-return summary: after computing `healthPath`, re-lock and confirm `m.session == s`; if not, return the SESS-06 "not found or expired" error. (The teardown branch is self-protecting: a capturing session is never purged.)

---

### IN-03: Dead assertion branch in `TestManager_Start_ConcurrentStartRejectsSecond` — `loserID` is always empty, so the loser-not-queryable check never runs

**File:** `pkg/session/manager_test.go:225, 238-242`
**Issue:** The loser goroutine's `Start` returns a zero `StartResult` alongside its error, so `loserID = o.res.SessionID` (line 225) always assigns `""`, and the guarded block `if loserID != "" { m.Status(loserID) ... }` (lines 238-242) is unreachable — the intended "the rejected Start left nothing queryable behind" assertion provides zero coverage while reading as if it does. (The winner-side assertions and the Shutdown tmpdir check remain meaningful.)
**Fix:** Delete the dead branch, or make it meaningful: assert `o.res == StartResult{}` for the loser (a rejected Start must return a zero result), which is the actually-testable property here.

---

### IN-04: `outputHash`/`healthPath` formula still duplicated between `Stop()` and `buildPipelineConfig` (carried from prior review IN-01 — unaddressed)

**File:** `pkg/session/manager.go:399-400`, `pkg/session/pipeline_config.go:69-71`
**Issue:** `evidence.HashOutputDir(filepath.Join(tmpDir, "policies"))` + `filepath.Join(tmpDir, "evidence", outputHash, "cluster-health.json")` is computed independently in both files. `HashOutputDir` is pure and both call sites feed it the same absolute path today, so they agree — but nothing enforces they stay in sync if either formula changes independently.
**Fix:** Store `OutputHash` (or the fully-built `healthPath`) on `Session` at the point `s.TmpDir` is assigned (`manager.go:212`) and have `Stop()` read it back.

---

### IN-05: Stale `//nolint:unused` directives on `Session.cancel`/`done`/`stopOnce` (carried from prior review IN-02 — unaddressed)

**File:** `pkg/session/session.go:89, 93, 99`
**Issue:** All three fields are now read/written by `manager.go` in the same package (`s.cancel()`, `<-s.done`, `s.stopOnce.Do`), so the suppressions are no-ops that actively mislead a reader into thinking the fields are unused and the suppression load-bearing. Lint remains clean with them removed (verified in the prior review pass).
**Fix:** Remove the three `//nolint:unused` directives; keep the doc comments.

---

### IN-06: Tautological `EvidenceFileCount >= 0` assertion provides no coverage (carried from prior review IN-03 — unaddressed)

**File:** `pkg/session/manager_test.go:293`
**Issue:** `assert.GreaterOrEqual(t, status.EvidenceFileCount, 0)` is unconditionally true for a `len()`-derived int; no test in scope positively confirms evidence files are counted (the glob `evidence/*/*/*.json` does match the real `evidence/<hash>/<ns>/<workload>.json` layout — verified against `pkg/evidence/paths.go:42` — so a positive assertion is achievable with the existing `twoFlows()` fixture).
**Fix:**
```go
require.Eventually(t, func() bool {
    status, err := m.Status(res.SessionID)
    return err == nil && status.EvidenceFileCount > 0
}, 5*time.Second, 5*time.Millisecond, "evidence file should eventually land under the session tmpdir")
```

---

_Reviewed: 2026-07-21T09:40:25Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
