---
phase: 17-session-lifecycle
reviewed: 2026-07-21T05:44:34Z
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
  warning: 4
  info: 1
  total: 5
status: issues_found
---

# Phase 17: Code Review Report

**Reviewed:** 2026-07-21T05:44:34Z
**Depth:** standard
**Files Reviewed:** 12
**Status:** issues_found

## Summary

Reviewed the Phase 17 session-lifecycle implementation: the MCP composition root (`cmd/cpg/mcp.go`, `mcp_tools.go`), the `pkg/session` state machine (`Manager`, `Session`, `buildPipelineConfig`), and the `OnFinal` hook added to `pkg/hubble/pipeline.go`. `go build ./...`, `go vet`, and `golangci-lint` (govet/errcheck/staticcheck/unused/errorlint) are all clean on every file in scope, and the full test suite passes under `-race`, including the concurrency-focused suites in `manager_test.go`.

The concurrency design (single-slot claim-before-setup, `sync.Once`-guarded idempotent stop, bounded-wait shutdown fan-out) is sound and well tested for the scenarios its own test suite exercises. Adversarial review focused on scenarios *outside* that suite: what happens to a pipeline that fails on its own (rather than being cancelled), and what happens when shutdown races the synchronous setup phase rather than the already-running pipeline. Both turned up real gaps, detailed below. WR-02's claim about the MCP SDK's request-context lifetime was verified directly against the pinned dependency source (`go-sdk@v1.6.1`) rather than assumed.

No security vulnerabilities, hardcoded secrets, injection vectors, or crashes were found. All findings below are Warning/Info tier — logic gaps and unhandled edge cases, not exploitable defects.

## Warnings

### WR-01: Pipeline's terminal error is read off the done channel and silently discarded — get_status can report "capturing" forever for a dead session

**File:** `pkg/session/manager.go:195-199, 339-350, 381-388`
**Issue:**
The background goroutine started in `Start()` sends the pipeline's terminal error onto `s.done`:
```go
go func() {
    err := m.runPipeline(sessionCtx, cfg)
    portForwardCleanup()
    s.done <- err        // <-- err is never inspected again
}()
```
Both consumers of that channel discard the value — they only use the receive to unblock a `select`:
```go
// Stop(), manager.go:341-345
select {
case <-s.done:
case <-time.After(m.stopWait):
    m.logger.Warn("session did not exit within deadline; proceeding", ...)
}
// Shutdown(), manager.go:383-387
select {
case <-done:
case <-time.After(m.stopWait):
    m.logger.Warn("shutdown: session did not exit within deadline; removing tmpdir anyway", ...)
}
```
Neither branch logs, stores, or surfaces `err`. `pkg/hubble/pipeline.go` goes out of its way to surface genuine stream failures instead of draining to a clean exit 0 (Stage 0's comment, and `TestRunPipeline_SurfacesStreamError` in `pipeline_test.go`) — that work is thrown away at this layer.

Two concrete, user-visible consequences:
1. `StopResult` (`pkg/session/session.go:155-179`) has no error/failure field at all, so a session that crashed mid-capture (relay connection reset, auth expiry, unreachable D-07 bypass address) produces a `stop_session` response that is structurally indistinguishable from a clean stop — same shape, just smaller counters.
2. `Session.State` is *only* ever set to `StateStopped` inside `Stop()`'s `stopOnce.Do`. Nothing updates it when the pipeline goroutine exits on its own. So if the pipeline dies immediately (e.g. connection refused against the D-07 bypass address) and the caller hasn't yet called `stop_session`, `get_status` keeps reporting `"state": "capturing"` indefinitely — directly contradicting the tool's own description, "Poll get_status to check progress" (`mcp_tools.go:82`), since there is no progress and no way to detect that from get_status.

This path is untested: no test in `manager_test.go` uses a `runPipeline` stand-in that returns a real (non-context-cancellation) error, so this gap has never been exercised.

**Fix:**
```go
// manager.go — capture err instead of discarding it
go func() {
    err := m.runPipeline(sessionCtx, cfg)
    portForwardCleanup()
    if err != nil {
        m.logger.Warn("session pipeline exited with error", zap.String("session_id", s.ID), zap.Error(err))
    }
    s.done <- err
}()
```
Thread the received value through `Stop()`/`Shutdown()` into a stored field on `Session` (e.g. `atomic.Pointer[error]` alongside `final`), and surface it as an `error`/`failed` field on `StopResult` and/or a third state so `get_status` stops reporting "capturing" for a session whose pipeline has already exited.

---

### WR-02: Shutdown() does not actually cancel an in-flight Start()'s synchronous setup phase — can orphan an empty session tmpdir past process exit

**File:** `pkg/session/manager.go:130, 160`
**Issue:**
`sessionCtx` (what `Shutdown()` cancels via `s.cancel()`) and `setupCtx` (what bounds `resolveSetupFn` — kubeconfig load + port-forward + cluster-dedup) are derived from two different parents:
```go
// Start(), manager.go:130
sessionCtx, sessionCancel := context.WithCancel(m.rootCtx)
...
// Start(), manager.go:159-161
timeout := defaultDuration(args.Timeout, 10*time.Second)
setupCtx, setupCancel := context.WithTimeout(reqCtx, timeout) // Pitfall H
defer setupCancel()
```
`setupCtx` is a child of `reqCtx` (the tool-handler's per-call context), not of `sessionCtx`/`m.rootCtx`. `Shutdown()` only ever calls `s.cancel()` (== `sessionCancel`), which has no effect on `setupCtx`.

I checked whether `reqCtx` itself gets cancelled when the server-root `ctx` is cancelled (SIGTERM) or the transport dies, since that would make this moot. It does not: the pinned go-sdk (`github.com/modelcontextprotocol/go-sdk@v1.6.1`) deliberately insulates every in-flight request context from the connection context. `internal/jsonrpc2/conn.go:199` wraps the root ctx in a `notDone{}` (`Done()` returns `nil`, `Err()` returns `nil`) before any request is dispatched, and `acceptRequest` (`conn.go:524`) derives every request's `ctx` via `context.WithCancel(notDone{...})`. Cancelling the outer ctx therefore never reaches an in-flight tool call's context — only an explicit per-request cancel (`$/cancelRequest`) or the call completing does.

The implementation's own test suite corroborates this: `TestManager_Start_ShutdownRacesSetup` (`manager_test.go:563-625`) injects a `resolveSetupFn` that blocks on a plain, ctx-independent channel and can *only* be unblocked by the test manually calling `closeRelease()` — if `Shutdown()`'s cancellation actually reached `setupCtx`, that manual release wouldn't be necessary for the fake to unblock.

Concrete consequence: `Start()` creates the session's tmpdir via `os.MkdirTemp` *before* calling `resolveSetupFn` (manager.go:154-157), i.e. before `s.TmpDir` is ever assigned. If SIGTERM arrives while `resolveSetupFn` is genuinely stuck (the code's own error message anticipates this exact scenario: "kubeconfig auth did not complete within the setup timeout... re-authenticate outside the MCP session", `manager.go:226-227`), then:
- `Shutdown()` reads `tmpDir := s.TmpDir` while it's still `""`, and its own `os.RemoveAll("")` is a documented no-op (manager.go:390-394).
- `Start()`'s own cleanup branch (the `m.session != s` check, manager.go:181-191) is the only code that would remove the *real* tmpdir — but it only runs after `resolveSetupFn` returns, which may never happen before the process exits (once `runMCPServer` returns, `main()` exits and the OS kills the still-blocked goroutine without it ever reaching that cleanup code).
- Net effect: an orphaned, empty `cpg-session-*` directory left under `os.TempDir()` for every SIGTERM-during-setup occurrence — not reaped by `Shutdown()`, not reaped by `Start()`, not reaped by process exit.

This contradicts the SESS-05 comment at the `mgr.Shutdown()` call site (`cmd/cpg/mcp.go:98-104`), which asserts synchronous, bounded cleanup for "BOTH return paths." `Shutdown()` *does* return bounded (that part is correctly tested), but it does not actually reach/cancel a mid-setup `Start()` — only a session whose pipeline has already launched.

**Fix:** Derive `setupCtx` from `sessionCtx` (or merge both) so `Shutdown()`'s cancellation actually reaches it:
```go
setupCtx, setupCancel := context.WithTimeout(sessionCtx, timeout)
defer setupCancel()
```
If per-call (`reqCtx`) cancellation must also still be honored, merge explicitly, e.g.:
```go
setupCtx, setupCancel := context.WithTimeout(reqCtx, timeout)
defer setupCancel()
context.AfterFunc(sessionCtx, setupCancel) // also abort setup if the session/manager is torn down
```

---

### WR-03: No upper-bound validation on MCP-supplied timeout/flush_interval

**File:** `cmd/cpg/mcp_tools.go:50-62`
**Issue:**
```go
func parseOptionalDuration(raw, field string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q: %w", field, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive, got %q", field, raw)
	}
	return d, nil
}
```
Only `d <= 0` is rejected; there is no upper bound. An MCP client can pass e.g. `"timeout": "876000h"`, which flows straight into `setupCtx`'s deadline (`manager.go:160`). Combined with WR-02, this removes the only bound on how long a stuck `resolveSetupFn` call can run — the setup phase's own timeout is the sole safety net once ctx-based cancellation is shown not to reach it. An oversized `flush_interval` has a similar, lower-severity effect: `get_status`'s `policy_file_count` can stay 0 for the entire session even while flows are being observed, since nothing flushes to disk until the interval ticks.

**Fix:**
```go
const maxSessionDuration = 24 * time.Hour // pick a product-appropriate ceiling

if d > maxSessionDuration {
    return 0, fmt.Errorf("%s must be <= %s, got %q", field, maxSessionDuration, raw)
}
```

---

### WR-04: Unrecognized DropReason values collapse into a single map key, silently undercounting

**File:** `pkg/session/session.go:214-216`
**Issue:**
```go
for reason, count := range stats.InfraDropsByReason {
    result.InfraDropsByReason[flowpb.DropReason_name[int32(reason)]] = count
}
```
`flowpb.DropReason_name` is `map[int32]string` (confirmed in `github.com/cilium/cilium@v1.19.x/api/v1/flow/flow.pb.go:598`). A lookup miss returns Go's zero value, `""` — not an error, not a panic. If the observed cluster runs a Cilium version newer than the one `cpg` is compiled against (a realistic operational scenario, since `DropReason` enum values are added upstream over time), *every* unrecognized reason maps to the same `""` key. Because this is a plain map assignment (`=`, not `+=`), multiple distinct unrecognized reasons observed in the same session don't sum — each overwrites the previous one, and which one "wins" depends on Go's randomized map iteration order. The `stop_session` summary would then silently under-report `infra_drops_by_reason` with no indication anything was collapsed.

**Fix:**
```go
for reason, count := range stats.InfraDropsByReason {
    name, ok := flowpb.DropReason_name[int32(reason)]
    if !ok {
        name = fmt.Sprintf("UNKNOWN(%d)", reason)
    }
    result.InfraDropsByReason[name] = count
}
```

## Info

### IN-01: outputHash/healthPath formula duplicated between manager.go and pipeline_config.go

**File:** `pkg/session/manager.go:329-333`, `pkg/session/pipeline_config.go:69-71`
**Issue:** The same formula — `evidence.HashOutputDir(filepath.Join(tmpDir, "policies"))` followed by `filepath.Join(tmpDir, "evidence", outputHash, "cluster-health.json")` — is computed independently in two places. `manager.go`'s own comment acknowledges this is a deliberate inline recompute rather than a stored field ("Session carries no outputHash field... This is the exact formula buildPipelineConfig uses"). `HashOutputDir` is a pure function of the (deterministic) tmpDir path, so the two currently agree — but nothing enforces they stay in sync if either formula changes independently in the future (e.g. a path-layout change made at only one call site), which would silently produce a wrong `cluster_health_path` in `StopResult`.
**Fix:** Store `OutputHash` (or the fully built `healthPath`) on `Session` once, at the point `s.TmpDir` is assigned in `Start()` (manager.go:192), and have `Stop()` read it back instead of recomputing it.

---

_Reviewed: 2026-07-21T05:44:34Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
