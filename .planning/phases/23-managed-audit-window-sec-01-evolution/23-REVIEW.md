---
phase: 23-managed-audit-window-sec-01-evolution
reviewed: 2026-07-22T17:12:26Z
depth: standard
files_reviewed: 13
files_reviewed_list:
  - pkg/k8s/exec.go
  - pkg/k8s/exec_test.go
  - pkg/auditwindow/manager.go
  - pkg/auditwindow/manager_test.go
  - cmd/cpg/audit_window.go
  - cmd/cpg/audit_window_test.go
  - cmd/cpg/main.go
  - cmd/cpg/mcp_audit_test.go
  - cmd/cpg/audit_docs_test.go
  - README.md
  - docs/bootstrap-runbook.md
findings:
  critical: 2
  warning: 3
  info: 2
  total: 7
status: issues_found
---

# Phase 23: Code Review Report

**Reviewed:** 2026-07-22T17:12:26Z
**Depth:** standard
**Files Reviewed:** 13
**Status:** issues_found

## Summary

Phase 23 introduces `cpg audit-window`, cpg's first and only mutating command:
a foreground, TTL-bounded per-endpoint `PolicyAuditMode` window backed by a
`pkg/auditwindow.Manager` state machine and a new `pkg/k8s` SPDY-exec surface.
The SEC-01 structural tripwire is evolved with `TestAuditWindowNotReachableFromMCP`
(Property 3, exec constructor reachability).

The phase invariants verified clean:

- **Invariant 1 (byte-identical body):** `git diff` of `mcp_audit_test.go` shows
  **zero deletions** — `TestMCPAuditReadonlyReachability` is untouched. PASS.
- **Invariant 2 (zero MCP surface):** no diffs to `mcp.go` / `mcp_tools.go` /
  `mcp_bootstrap.go` / `mcp_query*.go`; no new MCP tool. PASS.
- **Invariant 6 (exec safety):** commands built as `[]string` argv into
  `PodExecOptions.Command` (no shell, no injection surface); endpoint IDs via
  `strconv.FormatInt`; container pinned to `ciliumAgentContainerName`; JSON array
  parse of `endpoint get`; `CodeExitError` distinguished from transport error. PASS.
- **Invariant 7 (TTL/ns):** `--ttl <= 0` clamped to default (never unbounded);
  `validateBootstrapNamespace` (DNS-1123) reused. PASS.
- **Invariant 8 (docs):** hyphenated `policy-audit-mode` confined to the runbook
  warning block (lines 3, 10); README readonly claim pinned by
  `TestReadmeAuditWindowSection`; RBAC step-up documented as exclusive to
  audit-window. PASS.
- **Invariant 9 (deps):** `go.mod` / `go.sum` unchanged. PASS.
- Build clean (`go build ./...`).

However, the command-level revert path (Invariant 3) has two BLOCKER-class
defects, both rooted in `runAuditWindow` calling `wm.Close(ctx)` directly with
the signal-bound context instead of routing the revert through the bounded,
background-context `Shutdown`. The SEC-01 tripwire evolution (Invariant 5) has a
soundness hole in its new edge filter plus a missing non-vacuity floor on the
negative half.

## Critical Issues

### CR-01: Signal-path revert runs under a cancelled context and fails for every endpoint

**File:** `cmd/cpg/audit_window.go:156-163`
**Issue:** `runAuditWindow` reverts by calling `wm.Close(ctx)` where `ctx` is the
`signal.NotifyContext` context. On the primary exit path — Ctrl+C / SIGTERM — the
`select` fires precisely *because* `ctx` is already Done, and that same cancelled
`ctx` is then passed straight into `Close`:

```go
select {
case <-ctx.Done():
    logger.Info("audit window: signal received, reverting")
case <-ttlTimer.C:
    ...
}
result, closeErr := wm.Close(ctx) // ctx is already cancelled on the signal branch
```

Inside `Manager.Close`, every revert calls `setFn(ctx, ...)` →
`FindAgentPodForNode(ctx, ...)` → `clientset.Pods().List(ctx)`, which returns
`context.Canceled` immediately for a cancelled context. So on SIGINT/SIGTERM the
revert fails for **every** endpoint, leaving them all stuck in
`PolicyAuditMode=Enabled` (not enforcing).

The `Open`-spawned `<-rootCtx.Done() → Shutdown()` goroutine *does* use
`context.Background()` (correct), but it races the direct `Close` through the
shared `closeOnce`, and it is strictly slower to reach `Close` (it first cancels
the watcher and bounded-waits for `watchDone`). The direct `Close(cancelledCtx)`
therefore wins `closeOnce` in practice, so the background-context revert never
runs. This breaks the headline "Ctrl+C reverts every flip" guarantee the README
and runbook advertise. The unit tests miss it: `stubAuditWindowManager.Close`
ignores its `ctx`, and `TestManager_*` only ever calls `Close(context.Background())`.

**Fix:** Never revert under the signal context. Use a fresh, bounded context for
the graceful revert (or route the revert through `Shutdown`, which already uses
`context.Background()`):
```go
revertCtx, revertCancel := context.WithTimeout(context.Background(), auditWindowRevertBound)
defer revertCancel()
result, closeErr := wm.Close(revertCtx)
```
Add a command-level test that drives the signal path (cancel `cmd.Context()`) with
a real Manager whose `setFn` asserts `ctx.Err() == nil` at revert time.

### CR-02: Command-path revert via direct `Close` is unbounded — a wedged exec hangs the command forever

**File:** `cmd/cpg/audit_window.go:163` (and `pkg/auditwindow/manager.go:355-400`)
**Issue:** `Manager.Close` has no internal deadline: its fan-out ends in an
unconditional `wg.Wait()` and it never `select`s on `ctx.Done()`. The only bounded
protection lives in `Shutdown` (which wraps `Close` in a goroutine + `select`-on-
`time.After(deadline)`). But `runAuditWindow` calls `wm.Close(ctx)` **directly**,
bypassing that protection. On the TTL-expiry branch `ctx` is still live, so a
wedged SPDY transport (`setFn` that never observes cancellation) makes
`wm.Close(ctx)` block on `wg.Wait()` indefinitely — the deferred `wm.Shutdown()`
never runs because control never returns from the direct `Close`. This is exactly
the "wedged exec can never block process exit" failure mode the SESS-05 shape was
built to prevent, reintroduced on the command's real code path.
`TestManager_Shutdown_WedgedExecDoesNotBlock` only proves `Shutdown` is bounded —
it never exercises the direct `Close` call `runAuditWindow` actually uses.

**Fix:** Perform the command-level revert through the bounded `Shutdown` path
(rather than a bare `Close`), or give `Close` its own internal bounded wait
(`select { case <-doneFanOut: case <-time.After(bound): }`) so every caller — not
just `Shutdown` — inherits the bound. Fixing CR-01 by routing through `Shutdown`
resolves both CR-01 and CR-02 at once.

## Warnings

### WR-01: TTL-path revert leak — endpoints flipped by the watcher after `Close` snapshots are never reverted

**File:** `cmd/cpg/audit_window.go:163`, `pkg/auditwindow/manager.go:356-363,410-418`
**Issue:** On the TTL branch the watcher is still running when `wm.Close(ctx)` is
called (only `Shutdown` cancels the watcher, and it runs later, via `defer`).
`Close` snapshots `m.ours` under the mutex, then the deferred `Shutdown` cancels
the watcher. Any endpoint the watcher flips in the window between the snapshot and
the watcher-cancel is recorded in `m.ours` but is **never reverted**: `closeOnce`
has already fired, so `Shutdown`'s second `Close` returns the cached summary
without a second sweep. The leaked endpoint is left permanently in audit mode
(not enforcing). The window is narrow, but the guarantee this package exists to
encode is "every flip cpg made is reverted."
**Fix:** Cancel/drain the watcher *before* snapshotting `m.ours` for the revert
(i.e. stop the watcher, then Close). Routing the command revert through `Shutdown`
(which cancels the watcher first) closes this race as a side effect.

### WR-02: SEC-01 tripwire — the bare-`func()` edge filter can sever a genuine exec-carrying call chain (false-negative hole)

**File:** `cmd/cpg/mcp_audit_test.go:210-253` (`bfsFromRootGenuine` /
`isBareFuncValueDispatch`)
**Issue:** `bfsFromRootGenuine` skips *any* BFS edge whose call site is an indirect
dispatch through a zero-param/zero-result `func()` value. That shape is not unique
to RTA's spurious `context.CancelFunc` sweep — it is also the exact signature of
`sync.Once.Do(f func())`, `defer func(){...}()`, `go func(){...}()`, and any
`context.CancelFunc`-typed field. Because the filter cuts the BFS at the **first**
bare-`func()` edge, every function *downstream* of such an edge is dropped from the
reachable set. The audit-window code itself routes its exec through exactly this
shape: `Close` runs the `setFn`-bearing revert closure inside `closeOnce.Do(func(){...})`.
Consequently, a future MCP path that reached `remotecommand.NewSPDYExecutor` only
through a `sync.Once.Do`/`defer func(){}` cleanup closure would be silently excluded
from the negative assertion — the precise class of leak this security-critical
tripwire exists to catch. There is no live vulnerability today (MCP has no exec
path at all), but the tripwire's soundness in its own guarded dimension is weakened.
The positive half survives only because `readFn`'s non-bare signature
(`func(context.Context, string, int64) (bool, error)`) happens to expose the exec
chain — that is a fragile accident, not a guarantee.
**Fix:** Do not blanket-skip bare-`func()` edges. Prefer filtering only the
reflect-specific synthetic edges (nil `Site`, already handled) and, for the
bare-`func()` case, additionally scan the callee closures' own bodies for the exec
constructor (defense in depth) rather than pruning them from reachability. At
minimum, document the accepted false-negative class explicitly and add a
compensating direct-body scan of `sync.Once.Do` / `defer` closures reachable from
`runMCPServer`.

### WR-03: `TestAuditWindowNotReachableFromMCP` negative half has no non-vacuity floor

**File:** `cmd/cpg/mcp_audit_test.go:540-554`
**Issue:** The negative (security) half iterates `genuineCpgOwnedFromMCP` and asserts
no member statically calls `NewSPDYExecutor` — but there is no assertion that this
set is non-trivially populated. The positive half is floor-guarded
(`require.True(foundExecCaller)`), and the sibling `TestMCPAuditReadonlyReachability`
floor-checks its set (`require.Greater(len(cpgOwned), 1)` + `require.Contains(...Start)`).
The new negative half — which uses the *filtered* `bfsFromRootGenuine` (see WR-02) —
has neither. If the edge filter (or a future refactor) ever over-prunes MCP
reachability to near-empty, this assertion passes vacuously and nobody notices,
since Property 3 (exec constructor) is not covered by the unfiltered sibling test.
**Fix:** Add a floor to the negative half, e.g.
`require.Greater(t, len(genuineCpgOwnedFromMCP), 1)` plus a
`require.Contains(symbolSet(...), <known-MCP-reachable cpg function>)` so an
over-pruned/vacuous negative half fails loudly.

## Info

### IN-01: `Shutdown` revert deadline scales with endpoint count (`removeWait * N`)

**File:** `pkg/auditwindow/manager.go:427-433`
**Issue:** `deadline := m.removeWait * time.Duration(n)` where `n = len(m.ours)`.
The revert fan-out is fully concurrent (one goroutine per endpoint), so all reverts
complete within ~one `removeWait` regardless of `n`. Multiplying by `n` makes the
"bounded, can never block process exit" deadline grow linearly with namespace size:
a namespace with hundreds/thousands of endpoints plus a single wedged transport
blocks process exit for `removeWait * N` (minutes), contradicting the constant-bound
intent the doc comment states.
**Fix:** Use a constant (or small fixed multiple of) `removeWait` for the fan-out
deadline; the concurrency already makes per-endpoint cost non-additive.

### IN-02: `CheckDaemonAuditMode` doc comment contradicts the caller's actual handling

**File:** `pkg/k8s/exec.go:190-207`, `pkg/auditwindow/manager.go:163-172`
**Issue:** `CheckDaemonAuditMode`'s doc states non-forbidden errors are returned "so
the caller can hard-refuse on a genuine read failure rather than silently proceeding."
The sole caller, `Manager.Open`, does the opposite: on *any* `preconditionFn` error
it warns and proceeds. This matches the phase's warn-and-proceed invariant (so the
behavior is intended), but the stale comment could mislead a future maintainer into
believing a genuine ConfigMap read failure blocks the window — it does not, so a
transient read failure that masks an actually-active daemon-wide audit mode results
in cpg opening a scoped window on top of it.
**Fix:** Correct the comment to state that the sole caller treats all read errors
as undetermined/warn-and-proceed, or make `Open` distinguish forbidden/undetermined
(proceed) from a genuine read error (refuse) if hard-refusal is actually desired.

---

_Reviewed: 2026-07-22T17:12:26Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
