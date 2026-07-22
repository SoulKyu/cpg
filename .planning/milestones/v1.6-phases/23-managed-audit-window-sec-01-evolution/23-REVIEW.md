---
phase: 23-managed-audit-window-sec-01-evolution
reviewed: 2026-07-22T19:40:00Z
depth: standard
iteration: 2
files_reviewed: 5
files_reviewed_list:
  - pkg/auditwindow/manager.go
  - cmd/cpg/audit_window.go
  - cmd/cpg/audit_window_test.go
  - cmd/cpg/mcp_audit_test.go
  - pkg/auditwindow/manager_test.go
findings:
  critical: 0
  warning: 1
  info: 3
  total: 4
status: issues_found
---

# Phase 23: Code Review Report (Iteration 2)

**Reviewed:** 2026-07-22T19:40:00Z
**Depth:** standard (focused fix-verification re-review)
**Files Reviewed:** 5
**Status:** issues_found (all 5 prior findings closed; 1 new warning + 3 info)

## Summary

Focused re-review of the Phase 23 fix commits `4d62126` (revert routing through
bounded `Shutdown`) and `dc7f21c` (SEC-01 tripwire hardening) against diff base
`4a9149a`. Build clean (`go build ./...`); full race suite green
(`pkg/auditwindow` 1.2s, `cmd/cpg` 206.5s under `-race`, both whole-program
SSA/RTA tripwire tests included).

**All five iteration-1 findings are verified closed:**

- **CR-01 (cancelled-context revert): CLOSED.** `runAuditWindow` no longer calls
  `wm.Close(ctx)`. Both exit branches (signal + TTL) and the deferred cleanup route
  through `wm.Shutdown()` (audit_window.go:143, 170), which runs the fan-out under
  `context.Background()` (manager.go:446) — never the signal-bound ctx. Traced
  `Shutdown → Close(context.Background()) → setFn(bg) → FindAgentPodForNode(bg)`: no
  cancelled context reaches the revert. Pinned by
  `TestManager_Shutdown_RevertsUnderNonCancelledContext` (asserts `ctx.Err()==nil`
  at every revert after `rootCtx` cancel).
- **CR-02 (unbounded wait): CLOSED.** The command path invokes only `Shutdown`,
  which wraps `Close` in a goroutine + `select` on `time.After(deadline)`
  (manager.go:444-462). No `wg.Wait()` is reachable from `runAuditWindow` without
  the bound. `TestAuditWindow_TTLExpiryTriggersRevert` /
  `TestAuditWindow_SignalPathRevertsViaBoundedShutdown` assert
  `shutdownWasCalled && !closeWasCalled` on both branches;
  `TestManager_Shutdown_WedgedExecDoesNotBlock` proves the bound holds under a
  wedged `setFn`.
- **WR-01 (watcher-flip snapshot leak): CLOSED.** `Shutdown` cancels the watcher
  and bounded-drains `watchDone` (manager.go:419-434) *before* launching the
  `Close` goroutine that snapshots `m.ours` (444-448). Cancel-before-snapshot
  ordering holds; `TestManager_Shutdown_RevertsWatcherFlippedEndpoint` confirms a
  watcher-recorded endpoint is swept. Residual leak is confined to the
  wedged-watcher timeout branch (documented, bounded — same SESS-05 tradeoff).
- **WR-02 (bare-`func()` prune hole): CLOSED for the flagged class.** `withAnonFuncs`
  (mcp_audit_test.go:337-353) transitively re-includes lexically-nested closures
  (`(*ssa.Function).AnonFuncs`) of every genuinely-reachable cpg-owned function, so
  an exec constructor inside a `sync.Once.Do` / `defer` / `go` closure of an
  MCP-reachable function is now scanned despite the pruned bare-`func()` edge. A
  narrower residual remains (IN-03).
- **WR-03 (no non-vacuity floor): CLOSED.** The negative half now floor-guards with
  `require.Greater(len(genuineCpgOwnedFromMCP), 1)` and
  `require.Contains(..., "…/pkg/session.NewManager")` (mcp_audit_test.go:597-600).
  Verified `session.NewManager` is a direct static callee of `runMCPServer`
  (cmd/cpg/mcp.go:94) — a genuine non-bare edge that survives `bfsFromRootGenuine`,
  so an over-pruned BFS fails loudly instead of passing vacuously.

**Invariant (byte-identical `TestMCPAuditReadonlyReachability`): CONFIRMED.**
`git diff 4a9149a -- cmd/cpg/mcp_audit_test.go` shows three hunks, all pure
insertions (`bfsFromRootGenuine`/`isBareFuncValueDispatch`, `withAnonFuncs`, and
`TestAuditWindowNotReachableFromMCP`) landing between existing functions — zero
deletions, nothing inside the protected function body changed.

One new honesty defect (WR-01 below) surfaced in the CR-02 fix's timeout branch,
plus two carried-forward info findings and one new defense-in-depth note.

## Warnings

### WR-01: `Shutdown` timeout branch returns an empty `RevertResult`, so the operator is never told which endpoints are stuck

**File:** `pkg/auditwindow/manager.go:456-461`, surfaced at `cmd/cpg/audit_window.go:170-178`
**Issue:** On the bounded-deadline timeout branch, `Shutdown` returns
`RevertResult{EndpointResults: map[types.UID]error{}}` — an empty map — to avoid
racing `closeResult` while the wedged `Close` fan-out is still inside `wg.Wait()`.
The CLI then ranges over that empty map (audit_window.go:171) and logs **nothing**
per-endpoint. On a genuinely wedged transport `setFn` never returns, so it also
never emits its own per-endpoint `Warn` (manager.go:386-388) — meaning on the exact
failure this bound exists to survive, the operator gets **zero** machine-readable
signal of which endpoints remain in `PolicyAuditMode=Enabled`. This contradicts the
package's own stated contract (`RevertResult` doc / T-23-06: "a sweep that cannot
name the endpoint it failed to revert is a non-starter") on the one path where
naming the stuck endpoints matters most — the operator cannot know which endpoints
to manually revert. The common (non-wedged) path is unaffected; this is a
recoverability/repudiation gap on the timeout branch introduced by the CR-02 fix's
empty-result choice.
**Fix:** On the timeout branch, report the known-but-unconfirmed set instead of an
empty map. `m.ours` is stable at that point (the watcher is already drained and
`Close` only reads it into `recs`, never mutates it), so reading it under `m.mu` is
race-free:
```go
case <-time.After(deadline):
    m.logger.Warn("audit window: revert fan-out did not complete within the bounded deadline; some endpoints may still be in audit mode")
    m.mu.Lock()
    stuck := make(map[types.UID]error, len(m.ours))
    for uid := range m.ours {
        stuck[uid] = fmt.Errorf("revert outcome unknown: fan-out exceeded bounded deadline; verify PolicyAuditMode manually")
    }
    m.mu.Unlock()
    return RevertResult{EndpointResults: stuck}
```
The CLI's existing per-endpoint loop then surfaces each UID an operator must check.

## Info

### IN-01: `Shutdown` revert deadline scales with endpoint count (`removeWait * N`)

**File:** `pkg/auditwindow/manager.go:437-442` (carried forward from iteration 1, unchanged)
**Issue:** `deadline := m.removeWait * time.Duration(n)` where `n = len(m.ours)`.
The revert fan-out is fully concurrent (one goroutine per endpoint), so all reverts
complete within ~one `removeWait` regardless of `n`. Multiplying by `n` lets the
"bounded, can never block process exit" deadline grow linearly with namespace size:
a namespace with hundreds of endpoints plus a single wedged transport can block
process exit for `removeWait * N` (minutes), contradicting the constant-bound intent
the doc comment states.
**Fix:** Use a constant (or small fixed multiple of) `removeWait` for the fan-out
deadline; concurrency already makes per-endpoint cost non-additive.

### IN-02: `CheckDaemonAuditMode` doc comment contradicts the caller's actual handling

**File:** `pkg/k8s/exec.go` (unchanged this iteration), `pkg/auditwindow/manager.go:163-172` (carried forward)
**Issue:** `CheckDaemonAuditMode`'s doc states non-forbidden errors are returned "so
the caller can hard-refuse on a genuine read failure rather than silently
proceeding." The sole caller, `Manager.Open`, does the opposite: on *any*
`preconditionFn` error it warns and proceeds (manager.go:166-168). This matches the
phase's warn-and-proceed invariant, but the stale comment could mislead a future
maintainer into believing a genuine ConfigMap read failure blocks the window — it
does not, so a transient read failure that masks an actually-active daemon-wide
audit mode results in cpg opening a scoped window on top of it.
**Fix:** Correct the comment to state the sole caller treats all read errors as
undetermined/warn-and-proceed, or make `Open` distinguish forbidden/undetermined
(proceed) from a genuine read error (refuse) if hard-refusal is actually desired.

### IN-03: WR-02 residual — a top-level (non-closure) bare-`func()` target still evades both the BFS and the `withAnonFuncs` scan

**File:** `cmd/cpg/mcp_audit_test.go:210-240` (`bfsFromRootGenuine`), `337-353` (`withAnonFuncs`)
**Issue:** `withAnonFuncs` compensates for the pruned bare-`func()` edge by scanning
the *lexically-nested* closures of reachable functions — which closes the exact
class WR-02 flagged (`once.Do`/`defer`/`go` closures). But it only walks
`AnonFuncs` (lexical children). A **package-level** `func()` (zero-param/zero-result)
that itself calls `remotecommand.NewSPDYExecutor` and is invoked *only* via a
bare-`func()` value dispatch would be dropped by `bfsFromRootGenuine` (edge pruned)
and is not an `AnonFunc` of any reachable function, so it is scanned by neither
half. The shape is highly contrived (a top-level bare `func()` cannot capture the
config/URL `NewSPDYExecutor` requires, so it would need package globals), and MCP
has no exec path today — this is a defense-in-depth completeness note, not a live
gap. The primary WR-02 hole (closures) is genuinely closed.
**Fix:** Optional — document the accepted residual on `isBareFuncValueDispatch`, or
additionally union in the RTA-resolved callees of pruned bare-`func()` sites that
are themselves cpg-owned top-level functions.

---

_Reviewed: 2026-07-22T19:40:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard — iteration 2 (fix verification)_
