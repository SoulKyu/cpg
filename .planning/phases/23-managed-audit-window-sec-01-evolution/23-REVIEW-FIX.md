---
phase: 23-managed-audit-window-sec-01-evolution
fixed_at: 2026-07-22T00:00:00Z
review_path: .planning/phases/23-managed-audit-window-sec-01-evolution/23-REVIEW.md
iteration: 2
findings_in_scope: 6
fixed: 6
skipped: 0
status: all_fixed
---

# Phase 23: Code Review Fix Report

**Fixed at:** 2026-07-22
**Source review:** .planning/phases/23-managed-audit-window-sec-01-evolution/23-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 5 (CR-01, CR-02, WR-01, WR-02, WR-03)
- Fixed: 5
- Skipped: 0
- Info findings (IN-01, IN-02): out of scope, not addressed

**Invariant preserved:** `TestMCPAuditReadonlyReachability`'s body is byte-identical — the only `mcp_audit_test.go` hunks land in `bfsFromRootGenuine`/`isBareFuncValueDispatch` docs, a new `withAnonFuncs` helper, and `TestAuditWindowNotReachableFromMCP`. No deletions inside the protected function.

**Verification:** `rtk proxy go build ./...` clean; `rtk proxy go vet ./cmd/cpg/... ./pkg/auditwindow/...` clean; `rtk proxy go test ./pkg/auditwindow/... -race` ok (1.2s); `rtk proxy go test ./cmd/cpg/... -race -timeout 600s` ok (208s, includes both whole-program SSA/RTA tripwire tests).

## Fixed Issues

### CR-01 / CR-02 / WR-01: Signal/TTL revert path — cancelled context, unbounded wait, watcher-flip leak

**Files modified:** `pkg/auditwindow/manager.go`, `pkg/auditwindow/manager_test.go`, `cmd/cpg/audit_window.go`, `cmd/cpg/audit_window_test.go`
**Commit:** 4d62126
**Status:** fixed: requires human verification (concurrency/state semantics)

These three findings share one root cause and one interlocking fix (splitting them would produce commits that do not build), so they are resolved together as the review itself recommended ("routing through `Shutdown` resolves both CR-01 and CR-02 at once" / "closes this race as a side effect").

**Applied fix:**
- `Manager.Shutdown()` now returns `RevertResult`. It remains the single bounded revert path: it cancels and drains the watcher *before* `Close` snapshots `m.ours`, runs the revert fan-out under `context.Background()`, and enforces its own internal deadline. The `revertDone` branch returns the published `closeResult` (race-free via the channel-close synchronisation); the timeout branch returns an empty summary without reading `closeResult` concurrently with the still-running `Close` (no data race).
- `runAuditWindow` now reverts via `wm.Shutdown()` for both the signal and TTL exit paths, instead of the bare `wm.Close(ctx)` with the already-cancelled signal ctx. This inherits the fresh context (CR-01: reverts no longer fail with `context.Canceled` on SIGINT/SIGTERM), the bound (CR-02: a wedged transport can no longer block process exit on the TTL branch), and the cancel-before-snapshot ordering (WR-01: no watcher-flipped endpoint escapes the sweep). The `auditWindowManager` interface and the test stub were updated to the new `Shutdown() auditwindow.RevertResult` signature.

**Regression tests added:**
- `TestManager_Shutdown_RevertsUnderNonCancelledContext` — asserts every `setFn` revert observes `ctx.Err() == nil` after `rootCtx` is cancelled (CR-01).
- `TestManager_Shutdown_RevertsWatcherFlippedEndpoint` — asserts a watcher-flipped endpoint is reverted by `Shutdown` (WR-01).
- `TestAuditWindow_SignalPathRevertsViaBoundedShutdown` (new) and an extension of `TestAuditWindow_TTLExpiryTriggersRevert` — assert both command exit paths revert via the bounded `Shutdown` and never via a bare `Close` (CR-01/CR-02).

**Why human verification is flagged:** the change touches concurrency and exit-path state semantics. The behaviour is pinned by race-enabled tests (all green), but a maintainer should confirm the intended exit-path routing is correct.

### WR-02: SEC-01 tripwire — bare-`func()` edge filter false-negative hole

**Files modified:** `cmd/cpg/mcp_audit_test.go`
**Commit:** dc7f21c
**Status:** fixed

**Applied fix:** `bfsFromRootGenuine` must keep pruning bare-`func()` indirect-dispatch edges (the only way to stay immune to RTA's spurious whole-program address-taken sweep — a callee-package filter cannot help, since the sweep also connects to genuinely cpg-owned closures such as `(*auditwindow.Manager).Close$1`). To compensate for the accepted prune, the negative half now scans not only the genuinely-reachable cpg-owned functions but also their **lexically-nested** closures via the new `withAnonFuncs` helper (transitive `(*ssa.Function).AnonFuncs`). Nesting is a lexical relationship — the enclosing function genuinely runs the `once.Do`/`defer`/`go` closure — not a callgraph edge, so a hypothetical exec constructor inside a cpg-owned once/defer/go closure reachable from `runMCPServer` is now caught, without reintroducing the spurious cross-package sweep. The soundness argument is documented on `isBareFuncValueDispatch`.

### WR-03: negative half had no non-vacuity floor

**Files modified:** `cmd/cpg/mcp_audit_test.go`
**Commit:** dc7f21c
**Status:** fixed

**Applied fix:** added a floor to the negative half of `TestAuditWindowNotReachableFromMCP`: `require.Greater(len(genuineCpgOwnedFromMCP), 1)` plus `require.Contains(..., "github.com/SoulKyu/cpg/pkg/session.NewManager")`. `session.NewManager` is a guaranteed static callee of `runMCPServer` (cmd/cpg/mcp.go), so it is a genuine (non-bare, non-synthetic) BFS edge that must always be present — an over-pruned or refactor-broken BFS now fails loudly instead of passing the exec-reachability assertion vacuously. This mirrors the floor guards the positive half and the sibling `TestMCPAuditReadonlyReachability` already carry.

## Out-of-Scope (not fixed)

- **IN-01** (`Shutdown` deadline scales `removeWait * N`) — Info, out of scope.
- **IN-02** (`CheckDaemonAuditMode` stale doc comment) — Info, out of scope.

---

_Fixed: 2026-07-22_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_

## Iteration 2 (--auto re-review + fix)

Re-review (Opus) confirmed all 5 iteration-1 findings CLOSED and raised one new warning:

| ID | Severity | Fix | Commit |
|----|----------|-----|--------|
| WR-01 (iter2) | warning | `Shutdown`'s timeout branch now returns `RevertResult{TimedOut: true, PossiblyStuck: [...]}` (race-free `m.ours` snapshot under mutex) and the CLI exits non-zero naming every possibly-stuck endpoint UID + a manual verification hint — the T-23-06 repudiation contract now holds on the timeout path too. Wedged-transport test extended to pin the reporting. | `acee4a9` |

Remaining (info, out of scope): IN-01 (revert deadline scales with N despite concurrent fan-out), IN-02 (stale CheckDaemonAuditMode doc comment), IN-03 (contrived top-level bare-func() tripwire evasion shape — defense-in-depth note).
