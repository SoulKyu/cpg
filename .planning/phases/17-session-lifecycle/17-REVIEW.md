---
phase: 17-session-lifecycle
reviewed: 2026-07-21T08:03:58Z
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
  info: 3
  total: 5
status: issues_found
---

# Phase 17: Code Review Report

**Reviewed:** 2026-07-21T08:03:58Z
**Depth:** standard
**Files Reviewed:** 12
**Status:** issues_found

## Summary

Fresh re-review of the Phase 17 session-lifecycle implementation, performed against the current on-disk state — i.e. including the gap-closure waves (17-05/17-06/17-07) that landed since the prior `17-REVIEW.md`. This supersedes that file rather than amending it.

Verified first: `go build ./...`, `go vet` and `golangci-lint run --new-from-rev=<phase-17 base>` are clean (0 issues) on every file in scope, and `go test ./pkg/session/... ./pkg/hubble/... ./cmd/cpg/... -race -count=1` passes in full. I independently re-audited the three previously-reported gaps and confirm all are now fixed as claimed:
- **WR-01 (prior review)** — `pipelineErr`/autonomous `State` transition now exist (`pkg/session/manager.go:219-239`, `pkg/session/session.go:107-114`) and are exercised by `TestManager_PipelineErrorAutonomouslyStopsSession`.
- **WR-02 (prior review)** — `setupCtx` is now merged with `sessionCtx` via `context.AfterFunc(sessionCtx, setupCancel)` (`pkg/session/manager.go:180-181`), verified against the real go-sdk request-context behavior and covered by `TestManager_Start_ShutdownCancelsSetupCtx`.
- **WR-03 (prior review)** — `maxSessionDuration` (24h) ceiling added to `parseOptionalDuration` (`cmd/cpg/mcp_tools.go:52,76-78`), covered by `TestParseOptionalDuration`.
- **WR-04 (prior review)** — `DropReason_name` lookup misses now render `UNKNOWN(%d)` instead of colliding on `""` (`pkg/session/session.go:238-243`).

Adversarial focus for this pass was on the fixes themselves — a fix that closes one gap can open an adjacent one — plus a check of the one function `pkg/session` calls into but that isn't in scope (`pkg/hubble/client.go`), since a called function's behavior directly determines whether `manager.go`'s new error-classification logic is correct. That check found a real, reproducible gap in the WR-01 fix (below): it correctly handles a raw crash error, but not a *scoped* `context.DeadlineExceeded` produced by a healthy, uncancelled `sessionCtx` — which is exactly what the code's own dial-timeout path produces for an unreachable `--server` address, one of the three scenarios the WR-01 fix's own comment names as its target. Both warnings below were reproduced with standalone Go tests run against the actual `pkg/session` package (not included in the diff — verification only) before being written up; the repo was left clean (`git status` empty) after each.

No security vulnerabilities, hardcoded secrets, injection vectors, or crashes were found. All findings are Warning/Info tier.

## Warnings

### WR-01: A scoped `context.DeadlineExceeded` from a healthy `sessionCtx` is misclassified as "expected cancellation" — reopens the prior WR-01 gap for exactly the scenario it targeted (unreachable/typo'd `--server`)

**File:** `pkg/session/manager.go:215-242` (classification check at line 228), interacting with `pkg/hubble/client.go:105-123` and `pkg/session/pipeline_config.go:76`
**Issue:**
The launch goroutine's crash-detection guard treats *any* `context.Canceled`/`context.DeadlineExceeded` as "the session was torn down on purpose," and everything else as a genuine crash:
```go
// manager.go:215-239
go func() {
    err := m.runPipeline(sessionCtx, cfg)
    portForwardCleanup()

    if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
        s.pipelineErr.Store(&err)
        m.logger.Warn("session pipeline exited with error", ...)
        m.mu.Lock()
        if m.session == s && s.State == StateCapturing {
            s.State = StateStopped
            s.StoppedAt = time.Now()
        }
        m.mu.Unlock()
    }

    s.done <- err
}()
```
This assumes `context.DeadlineExceeded` can only originate from `sessionCtx`/`m.rootCtx` being cancelled. It doesn't: `pkg/session/pipeline_config.go:76` sets `PipelineConfig.Timeout` to the *same* `args.Timeout` used for `setupCtx`, and `pkg/hubble/client.go`'s `waitForConnReady` (called from `StreamDroppedFlows`, itself called from `RunPipelineWithSource`, i.e. *after* `resolveSetupFn` has already succeeded and the goroutine has launched) derives its own, independent, scoped timeout directly from that same healthy `sessionCtx`:
```go
// client.go:109-111
func waitForConnReady(ctx context.Context, conn *grpc.ClientConn, timeout time.Duration) error {
    dialCtx, cancel := context.WithTimeout(ctx, timeout)
    ...
    return fmt.Errorf("connecting to hubble relay %q: %w", conn.Target(), dialCtx.Err())
```
When the dial to an unreachable or misconfigured relay address doesn't complete before this scoped deadline, `dialCtx.Err()` is `context.DeadlineExceeded`, properly `%w`-wrapped all the way back up to `m.runPipeline`'s return — while `sessionCtx` itself is never cancelled (`sessionCtx.Err() == nil` throughout). `errors.Is(err, context.DeadlineExceeded)` is `true` for this error, so the classification guard treats it as "expected," and **never transitions `State`, never sets `pipelineErr`**. The pipeline goroutine has genuinely exited, but `get_status` reports `"state": "capturing"` indefinitely — a session cannot self-report its own death for this specific, realistic failure mode (a live/typo'd `--server` address, the exact scenario named in this same function's own comment two lines below at `manager.go:226-227` and in the tool description "Poll get_status to check progress," `cmd/cpg/mcp_tools.go:100`).

I reproduced this with a standalone test: a `runPipeline` stand-in that mirrors `waitForConnReady` exactly (derives `context.WithTimeout(sessionCtx, 20ms)` and returns `dialCtx.Err()` once it fires) leaves `Status().State == "capturing"` 300ms later, with `sessionCtx` never cancelled. No test in `manager_test.go` exercises this path — `TestManager_PipelineErrorAutonomouslyStopsSession` uses a plain `errors.New(...)` sentinel that is never mistaken for a context error, so it cannot catch this gap; `TestManager_Start_ShutdownCancelsSetupCtx` only exercises the pre-launch setup phase, not this post-launch dial phase.

Explicit `stop_session` still works correctly in this state (it doesn't depend on `State` being accurate to finalize), so this is not a hang or a stuck-forever session — but autonomous detection via `get_status`, the entire point of the original WR-01 fix, silently fails to fire for this scenario.

**Fix:** Classify based on whether `sessionCtx` itself was actually cancelled, not on the identity of the returned error — a scoped timeout unrelated to session teardown can produce the identical sentinel:
```go
go func() {
    err := m.runPipeline(sessionCtx, cfg)
    portForwardCleanup()

    // sessionCtx.Err() is nil unless Stop/Shutdown (or m.rootCtx) actually
    // cancelled it — the only two "this was on purpose" cases. Any other
    // non-nil err, INCLUDING a context.DeadlineExceeded from an unrelated
    // scoped timeout (e.g. client.go's dial timeout), is a genuine failure.
    if err != nil && sessionCtx.Err() == nil {
        s.pipelineErr.Store(&err)
        m.logger.Warn("session pipeline exited with error", zap.String("session_id", s.ID), zap.Error(err))
        m.mu.Lock()
        if m.session == s && s.State == StateCapturing {
            s.State = StateStopped
            s.StoppedAt = time.Now()
        }
        m.mu.Unlock()
    }

    s.done <- err
}()
```
Add a regression test alongside `TestManager_PipelineErrorAutonomouslyStopsSession` using a `runPipeline` stand-in that derives its own `context.WithTimeout` from the passed-in (healthy) `ctx` and returns that scoped `ctx.Err()`, asserting `get_status` still autonomously transitions to `"stopped"`.

---

### WR-02: The first-ever `stop_session` call after an autonomous crash reports `already_stopped: true`, contradicting the documented "second/idempotent call" contract

**File:** `pkg/session/manager.go:371-412` (specifically the early-return at 388-390), interacting with the autonomous transition at `manager.go:228-239`
**Issue:**
`Stop()` decides whether to report `AlreadyStopped: true` purely from `s.State`:
```go
// manager.go:371-403
func (m *Manager) Stop(id string) (StopResult, error) {
    ...
    state := s.State
    tmpDir := s.TmpDir
    m.mu.Unlock()
    ...
    if state == StateStopped {
        return s.buildSummary(true, healthPath), nil // D-03: idempotent, already-stopped marker, never isError
    }

    s.stopOnce.Do(func() { ... })
    return s.buildSummary(false, healthPath), nil
}
```
But `State` can reach `StateStopped` two ways: an explicit prior `Stop()` call (via `stopOnce.Do`), *or* the WR-01 autonomous crash-handler (`manager.go:234-237`), which never calls `stopOnce` at all. Both produce `state == StateStopped` here, so both take the `alreadyStopped: true` branch — including the very first `stop_session` call a client ever makes for a session that happened to crash before they got around to calling it. The tool's own description says the opposite: "Idempotent — **a second stop** returns the same summary with an already-stopped marker" (`cmd/cpg/mcp_tools.go:158-159`); `StopResult.AlreadyStopped`'s doc comment says the same ("marks a second/idempotent stop_session call," `pkg/session/session.go:175-178`).

Concretely, this makes the field's value **non-deterministic for the same client action**: whether a client's first-ever `stop_session` call reports `true` or `false` depends on whether it happens to race ahead of or behind the crash-handler's `m.mu` critical section — not on anything the client did differently. A client that keys behavior off this field (e.g. "only surface a stop notification when `!already_stopped`, since `true` means someone/something else already handled it") will incorrectly suppress the notification for a session that crashed on its own and was never actually reported as stopped to the caller before this call.

I reproduced this with a standalone test: start a session, trigger a genuine crash via a `runPipeline` stand-in, poll until `Status().State == "stopped"` (proving the autonomous transition ran), then call `Stop()` for the first time — `StopResult.AlreadyStopped` is `true`.

**Fix:** Decouple "was this call redundant" from "is the pipeline no longer running" — track whether an explicit `stop_session` call has previously completed, independent of why `State` is already `Stopped`:
```go
// session.go: new field alongside pipelineErr/final
explicitStopSeen atomic.Bool // true once any Stop() call has returned a summary

// manager.go Stop():
if state == StateStopped {
    return s.buildSummary(s.explicitStopSeen.Swap(true), healthPath), nil
}
s.stopOnce.Do(func() { ... })
return s.buildSummary(s.explicitStopSeen.Swap(true), healthPath), nil
```
`atomic.Bool.Swap(true)` returns the *previous* value, so the first `Stop()` call for a given session — crash-preceded or not — reports `false`, and every call after that reports `true`, matching the documented contract.

## Info

### IN-01: `outputHash`/`healthPath` formula still duplicated between `manager.go` and `pipeline_config.go`

**File:** `pkg/session/manager.go:385-386`, `pkg/session/pipeline_config.go:69-71`
**Issue:** Carried over from the prior review (unaddressed by the gap-closure waves). `evidence.HashOutputDir(filepath.Join(tmpDir, "policies"))` followed by `filepath.Join(tmpDir, "evidence", outputHash, "cluster-health.json")` is computed independently in both files. `manager.go`'s own comment acknowledges this is a deliberate inline recompute rather than a stored field. `HashOutputDir` is a pure function of the deterministic `tmpDir` path, so the two currently agree, but nothing enforces they stay in sync if either formula changes independently later.
**Fix:** Store `OutputHash` (or the fully-built `healthPath`) on `Session` once, at the point `s.TmpDir` is assigned in `Start()` (`manager.go:212`), and have `Stop()` read it back instead of recomputing it.

---

### IN-02: Stale `//nolint:unused` directives on `Session.cancel`/`done`/`stopOnce` — these fields are consumed within the same package

**File:** `pkg/session/session.go:89,93,99`
**Issue:**
```go
cancel context.CancelFunc //nolint:unused // consumed by plan 17-03's Manager
...
done chan error //nolint:unused // consumed by plan 17-03's Manager
...
stopOnce sync.Once //nolint:unused // consumed by plan 17-03's Manager
```
These comments date from when `session.go` (plan 17-02) was written standalone, before `manager.go` (plan 17-03) existed in the same package. `manager.go` now reads/writes all three fields directly (`s.cancel()`, `<-s.done`, `s.stopOnce.Do(...)`), so `staticcheck`'s `unused` check — which operates at the whole-package level, not per-file — no longer has any reason to flag them. I verified this directly: removing all three `//nolint:unused` comments and re-running `golangci-lint run ./pkg/session/...` still reports 0 issues. Left in place, the comments actively mislead a reader into thinking these fields are unused (they are the core of the Manager's concurrency model) and that the suppression is load-bearing, when it is not.
**Fix:** Remove the three `//nolint:unused` directives (keep the plain doc comments describing what drives each field).

---

### IN-03: Tautological assertion provides no real coverage for `EvidenceFileCount`

**File:** `pkg/session/manager_test.go:293`
**Issue:**
```go
assert.GreaterOrEqual(t, status.EvidenceFileCount, 0)
```
`EvidenceFileCount` is an `int` populated from `len(matches)` (`countGlob`, `manager.go:461-464`), which can never be negative — this assertion is true unconditionally and would pass even if evidence-file counting were completely broken (e.g. always returning 0, or globbing the wrong path). Unlike the adjacent `PolicyFileCount` check a few lines above, which meaningfully waits for `status.PolicyFileCount > 0`, there is no test anywhere in scope that positively confirms evidence files are actually being counted for a capturing session (the fixture already used, `twoFlows()`, drives real policy writes, and `buildPipelineConfig` always sets `EvidenceEnabled: true` in MCP mode, so a meaningful assertion is achievable here).
**Fix:**
```go
require.Eventually(t, func() bool {
    status, err := m.Status(res.SessionID)
    return err == nil && status.EvidenceFileCount > 0
}, 5*time.Second, 5*time.Millisecond, "evidence file should eventually land under the session tmpdir")
```

---

_Reviewed: 2026-07-21T08:03:58Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
