---
phase: 17-session-lifecycle
reviewed: 2026-07-21T11:22:35Z
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
  warning: 3
  info: 6
  total: 9
status: issues_found
---

# Phase 17: Code Review Report

**Reviewed:** 2026-07-21T11:22:35Z
**Depth:** standard
**Files Reviewed:** 12
**Status:** issues_found

## Summary

Full fresh re-review after plan 17-09 (commits a185b9f..35504f5), which broadened the launch goroutine's autonomous-exit guard in `pkg/session/manager.go` from `err != nil && sessionCtx.Err() == nil` to `sessionCtx.Err() == nil` alone, so a clean `nil` pipeline drain now also transitions the session to `Stopped` and releases the single-active slot. Verified first: `go build`, `go vet`, and `rtk proxy golangci-lint run` are clean on every file in scope (the 11 errcheck issues on `./pkg/hubble/...` all live in `summary.go`/`health_writer.go`/`client.go` — pre-existing, out of this diff range, confirmed by line number). `rtk proxy go test ./pkg/session/... ./pkg/hubble/... ./cmd/cpg/... -count=1 -race` passes in full (all green, no data races reported).

**Prior-review findings — resolution status:**

- **Prior WR-01 (clean nil-exit leaves session capturing forever) — RESOLVED.** The 17-09 guard broadening (`manager.go:238`, `if sessionCtx.Err() == nil {`) now fires on both `err != nil` and `err == nil`, transitioning `State` and releasing the slot either way, with error-surfacing (`s.pipelineErr.Store`, Warn log) staying conditional on `err != nil` and an Info log on the clean path. Traced against `TestManager_CleanDrainAutonomouslyStopsSession` and `TestManager_FirstStopAfterCleanAutonomousExitIsNotAlreadyStopped` (`manager_test.go:831-886`) — both correctly fail against the pre-fix guard and pass against the current one.
- **Prior IN-01 (17-08's `s.cancel()` comment asserted a false happens-before ordering) — RESOLVED.** The comment at `manager.go:253-260` was rewritten in the same 17-09 commit to the corrected justification ("safe even if it fires Start's still-registered `context.AfterFunc`... setupCancel is itself idempotent, and setupCtx has no remaining consumers once resolveSetupFn already returned") — accurate this time; verified against the actual call order in `Start` (`manager.go:104-268`).
- **Prior WR-02 (cross-package `os.TempDir()` glob race) — STILL UNRESOLVED**, carried forward below as WR-03.
- **Prior IN-02 through IN-06 — STILL UNRESOLVED**, carried forward below as IN-01 through IN-05 (renumbered; content and line ranges re-verified against the current file state).

**Fresh findings this pass:** the 17-09 broadening reopens a narrow but real race between `Stop()`'s own teardown and the now-much-more-reachable autonomous "clean drain" transition, silently distorting the reported session `Duration` (WR-01 below — this is a *new* WR-01 for this pass, distinct from the resolved prior one). Separately, tracing the actual call chain into `github.com/modelcontextprotocol/go-sdk` confirmed the three session tool handlers have no panic recovery, and neither does the SDK's own request-dispatch goroutine (WR-02, new). One new minor duplication (IN-06).

No hardcoded secrets, `eval`/`exec`/shell-out patterns, insecure randomness, empty catch-equivalents, or debug artifacts were found via pattern scan across all 12 files. Session/evidence tmpdir paths are all server-generated (`os.MkdirTemp`), never derived from unsanitized client input, so no path-traversal vector was found in this diff.

## Warnings

### WR-01: `Stop()` can overwrite the autonomously-recorded `StoppedAt` with a later timestamp when it races the pipeline's own natural completion — surface widened by 17-09

**File:** `pkg/session/manager.go:401-434` (`Stop`), racing `pkg/session/manager.go:238-262` (launch goroutine's autonomous-exit guard)
**Issue:**
`Stop()` reads `state := s.State` once, under `m.mu` (`manager.go:401`), releases the lock, computes `outputHash`/`healthPath`, and — only if that snapshot read was `StateCapturing` — enters `s.stopOnce.Do`:
```go
s.stopOnce.Do(func() { // Pitfall F — only the first concurrent caller performs the real teardown
    s.cancel()
    select {
    case <-s.done:
    case <-time.After(m.stopWait):
        m.logger.Warn("session did not exit within deadline; proceeding", zap.String("session_id", id))
    }
    m.mu.Lock()
    s.State = StateStopped
    s.StoppedAt = time.Now()   // manager.go:432 — unconditional overwrite
    m.mu.Unlock()
})
```
This write is unconditional: it does not check whether `State` already became `StateStopped` in the interim. If the pipeline finishes **on its own** (natural EOF/drain, `err == nil`, `sessionCtx` still healthy at the moment it checks) in the window between `Stop()`'s initial `state := s.State` read and this goroutine reaching `s.stopOnce.Do`, the launch goroutine's autonomous-exit branch (`manager.go:238-251`) races to set `s.State = StateStopped; s.StoppedAt = time.Now()` first, with the *accurate* stop timestamp. `Stop()`'s `stopOnce.Do` then still runs to completion (it already committed to entering the closure based on its stale `state == StateCapturing` read), calls the now-redundant `s.cancel()`, receives the pipeline's `nil` from `<-s.done`, and **overwrites `StoppedAt` a second time** with a strictly later timestamp — inflating the `Duration`/`Elapsed` value the client sees in the `stop_session`/`get_status` response relative to when the pipeline actually stopped.

This exact class of race existed before 17-09 too (a crashing pipeline racing `Stop()`), but only for a genuine failure — a comparatively rare event. Because 17-09 makes the autonomous transition fire on **every** clean drain as well (short/bounded captures, a relay closing the stream normally), the reachable window for this race is now the common case, not just the crash path, which is exactly the "interaction with the existing stop path" this review pass was scoped to check. Impact is limited to a stale duration figure (no crash, no data loss, `AlreadyStopped`/`pipelineErr`/`FlowsSeen` etc. are all unaffected since they use independent atomics/`final` snapshot) — not reachable via a single malicious input, only via timing coincidence — hence Warning, not Blocker. No existing test exercises this: every test that calls `Stop()` on a session backed by a fast-completing source (`closedFlowSource`) either waits for `PolicyFileCount > 0` first (`TestManager_Stop`) or only asserts internal consistency between two post-stop reads (`TestManager_Status`), never a bound on `Duration` relative to actual completion time.

**Fix:** Guard the overwrite so whichever transition (autonomous or explicit) reaches `StateStopped` first wins the timestamp:
```go
m.mu.Lock()
if s.State != StateStopped {
    s.State = StateStopped
    s.StoppedAt = time.Now()
}
m.mu.Unlock()
```

---

### WR-02: No panic recovery in the three session-lifecycle MCP tool handlers — an unrecovered panic anywhere in the call chain crashes the entire long-lived MCP server process

**File:** `cmd/cpg/mcp_tools.go:106` (`start_session`), `:150` (`get_status`), `:162` (`stop_session`)
**Issue:** None of the three `mcp.AddTool` handler closures wrap their body in `recover()`. I traced the actual dispatch path in `github.com/modelcontextprotocol/go-sdk@v1.6.1` to confirm the SDK doesn't provide this safety net either: `internal/jsonrpc2/conn.go:640-644` invokes each handler on its own per-request goroutine —
```go
go func() {
    defer releaser.release(true)
    result, err := c.handler.Handle(ctx, req.Request)
    c.processResult(c.handler, req, result, err)
}()
```
— with no `recover()` anywhere in that call stack (verified: zero non-test occurrences of `recover()` in the whole `mcp` and `internal/jsonrpc2` packages). A Go panic that is never recovered terminates the *entire process*, not just the panicking goroutine, regardless of what other goroutines (including the one running `runMCPServer`/`server.Run`) are doing. Concretely: any future nil-pointer dereference, index-out-of-range, or similar defect reached from `mgr.Start`/`mgr.Status`/`mgr.Stop` — or anything they call transitively (`hubble.RunPipeline`, `pkg/k8s` port-forward/kubeconfig helpers, `pkg/evidence`) — during a single tool call takes down the whole `cpg mcp` server for every session and every other in-flight/future call, not just the one triggering the bug. This directly undercuts the SESS-05/D-01 bounded-cleanup design this phase invests heavily in (Shutdown's bounded fan-out never runs on this path either, since the crash bypasses `runMCPServer`'s own return entirely). Today's reviewed code has no known reachable panic path (nil-safety was traced explicitly for `pipelineErr`/`final`/`cancel`/`done`), so this is a defense-in-depth gap, not a proven live bug — Warning, not Blocker.
**Fix:** Recover at the handler boundary and convert a panic into a returned error, consistent with the existing Pattern 0 (a returned `error` auto-converts to a tool-error result):
```go
// recoverToolError converts a handler panic into a returned error so a bug
// in one call can never crash the whole long-lived MCP server process —
// go-sdk's own per-request dispatch goroutine has no recover() of its own.
func recoverToolError(toolName string, err *error) {
    if r := recover(); r != nil {
        *err = fmt.Errorf("%s: internal error: %v", toolName, r)
    }
}
```
applied via named returns in each handler, e.g.:
```go
}, func(ctx context.Context, _ *mcp.CallToolRequest, args startSessionArgs) (_ *mcp.CallToolResult, out session.StartResult, err error) {
    defer recoverToolError("start_session", &err)
    ...
})
```

---

### WR-03: `os.TempDir()`-wide `cpg-session-*` glob counting in `manager_test.go` races concurrently-running `cmd/cpg` package tests — cross-package CI flake vector (carried forward, unresolved across two review passes)

**File:** `pkg/session/manager_test.go:602/642` (`TestManager_Start_ShutdownRacesSetup`), `:661/668` (`TestManager_Start_SetupFailureRollsBackSlot`), `:915/952` (`TestManager_Start_ShutdownCancelsSetupCtx`)
**Issue:** Three tests assert "no orphaned tmpdir" by comparing `filepath.Glob(filepath.Join(os.TempDir(), "cpg-session-*"))` before/after an exercised sequence. That glob is process- and machine-global, but `cmd/cpg`'s `TestMCPSessionLifecycleWiringAndStdoutPurity` (`mcp_session_test.go`) also drives a real `Manager.Start`/`Shutdown` in the same `os.TempDir()` via the same `cpg-session-*` prefix (`manager.go:154`). `go test ./...` runs package test binaries in parallel by default, so a `cmd/cpg` session tmpdir appearing or disappearing inside one of these before/after windows makes the length comparison fail spuriously — a latent flake that will read as a real orphaned-tmpdir regression when it fires. This was flagged in the prior review (as WR-02) and remains completely unaddressed; all six glob call sites are unchanged in substance from the prior pass (only line numbers shifted from the 17-09 test additions).
**Fix:** Scope the assertion to a test-private root instead of the shared global tmp dir: add an unexported `tmpRoot string` field to `Manager` (empty preserves production behavior via `os.MkdirTemp`'s own default), have `Start` call `os.MkdirTemp(m.tmpRoot, "cpg-session-*")`, and set `m.tmpRoot = t.TempDir()` in `newTestManager` (`manager_test.go:132-141`). The three tests then glob `filepath.Join(m.tmpRoot, "cpg-session-*")`, which no other package can touch.

## Info

### IN-01: `Stop()` racing a concurrent `Start()`'s D-04 purge returns a full summary for a purged session, referencing an already-removed tmpdir (carried forward, unresolved)

**File:** `pkg/session/manager.go:394-421` (`Stop`'s slot capture and early-return path), interacting with the purge at `manager.go:114-121`
**Issue:** `Stop` captures `s := m.session` under `m.mu`, releases the lock at `manager.go:403` ("Pitfall G — release before the (potentially slow) bounded wait"), then builds the summary. A concurrent `Start` can acquire the lock in that window, take the D-04 purge branch for the same stopped session (`os.RemoveAll(m.session.TmpDir)`, slot replaced), after which the still-running `Stop` returns a well-formed `StopResult` whose `TmpDir`/`ClusterHealthPath` point at just-deleted paths — for a session that D-04/D-02 semantics say should now be "not found" (a purged session "simply has no Manager slot," `session.go:32-33`). Benign staleness (no crash; single-client sequential usage never hits it), a small semantic contradiction with the documented purge model.
**Fix:** Re-validate the slot before the early-return summary: after computing `healthPath`, re-lock and confirm `m.session == s`; if not, return the SESS-06 "not found or expired" error.

---

### IN-02: Dead assertion branch in `TestManager_Start_ConcurrentStartRejectsSecond` — `loserID` is always empty, so the loser-not-queryable check never runs (carried forward, unresolved)

**File:** `pkg/session/manager_test.go:217-242`
**Issue:** The loser goroutine's `Start` returns a zero `StartResult` alongside its error (`manager.go:108-112` returns `StartResult{}, fmt.Errorf(...)`), so `loserID = o.res.SessionID` (line 225) always assigns `""`, and the guarded block `if loserID != "" { ... }` (lines 238-242) is unreachable — the intended "the rejected Start left nothing queryable behind" assertion provides zero coverage while reading as if it does.
**Fix:** Delete the dead branch, or replace it with an assertion that's actually reachable at the point the loser is identified (inside the `else` branch at lines 223-227, where the loser's `startOutcome` is already in hand): `assert.Equal(t, StartResult{}, o.res, "a rejected Start must return a zero result")`.

---

### IN-03: `outputHash`/`healthPath` formula still duplicated between `Stop()` and `buildPipelineConfig` (carried forward, unresolved)

**File:** `pkg/session/manager.go:408-409`, `pkg/session/pipeline_config.go:69-71`
**Issue:** `evidence.HashOutputDir(filepath.Join(tmpDir, "policies"))` + `filepath.Join(tmpDir, "evidence", outputHash, "cluster-health.json")` is computed independently in both files. `HashOutputDir` is pure and both call sites feed it the same absolute path today, so they agree — but nothing enforces they stay in sync if either formula changes independently.
**Fix:** Store `OutputHash` (or the fully-built `healthPath`) on `Session` at the point `s.TmpDir` is assigned (`manager.go:212`) and have `Stop()` read it back instead of recomputing.

---

### IN-04: Stale `//nolint:unused` directives on `Session.cancel`/`done`/`stopOnce` (carried forward, unresolved)

**File:** `pkg/session/session.go:89, 93, 99`
**Issue:** All three fields are read/written by `manager.go` in the same package (`s.cancel()` field-access reads — `manager.go:261,424`, plus the initial write via the struct literal; `<-s.done`/`s.done <-` — `manager.go:264,426,474`; `s.stopOnce.Do` — `manager.go:423`), so the suppressions are no-ops that actively mislead a reader into thinking the fields are unused and the suppression load-bearing.
**Fix:** Remove the three `//nolint:unused` directives; keep the doc comments.

---

### IN-05: Tautological `EvidenceFileCount >= 0` assertion provides no coverage (carried forward, unresolved)

**File:** `pkg/session/manager_test.go:298`
**Issue:** `assert.GreaterOrEqual(t, status.EvidenceFileCount, 0)` is unconditionally true for a `len()`-derived `int`; no test in scope positively confirms evidence files are counted.
**Fix:**
```go
require.Eventually(t, func() bool {
    status, err := m.Status(res.SessionID)
    return err == nil && status.EvidenceFileCount > 0
}, 5*time.Second, 5*time.Millisecond, "evidence file should eventually land under the session tmpdir")
```

---

### IN-06: `start_session`'s default timeout/flush_interval values are duplicated between the jsonschema doc strings and the actual implementation, with no single source of truth

**File:** `cmd/cpg/mcp_tools.go:29,31` (`"default: 10s; max 24h"` / `"default: 5s; max 24h"`), `pkg/session/pipeline_config.go:76,80` (`defaultDuration(args.Timeout, 10*time.Second)` / `defaultDuration(args.FlushInterval, 5*time.Second)`)
**Issue:** The LLM-facing schema description and the actual fallback literals agree today (verified), but they live in two different packages with nothing tying them together — changing one default without the other silently makes the tool's advertised contract wrong.
**Fix:** Define the two defaults once as exported constants in `pkg/session` (e.g. `DefaultTimeout`, `DefaultFlushInterval`) and reference them from both `pipeline_config.go`'s `defaultDuration` calls and `mcp_tools.go`'s schema doc strings via `fmt.Sprintf`, or at minimum add a comment on each side pointing at the other so a future edit can't silently diverge unnoticed.

---

_Reviewed: 2026-07-21T11:22:35Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
