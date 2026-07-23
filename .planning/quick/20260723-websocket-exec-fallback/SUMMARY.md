---
quick_id: 260723-b26
slug: websocket-exec-fallback
status: complete
commits:
  - 2465740: "feat(quick): use WebSocket exec with SPDY fallback in audit-window exec path"
  - 4707c44: "test(quick): extend SEC-01 exec tripwire to WebSocket + fallback constructors"
  - 9afd645: "docs(quick): note WebSocket-first exec transport in README"
---

# Quick Task Summary: WebSocket exec with SPDY fallback (AUD-FUT-01)

## What changed

`pkg/k8s/exec.go`'s `ExecCiliumDbg` used a bare `remotecommand.NewSPDYExecutor` to reach
cilium-agent pods for `pods/exec`. SPDY is deprecated upstream; kubectl has defaulted to
WebSocket exec (falling back to SPDY) since 1.30+. Replaced the single-transport construction
with kubectl's exact pattern:

- `newFallbackExecutor(config, url *url.URL)` (new, `pkg/k8s/exec.go`): builds a WebSocket
  executor (`NewWebSocketExecutor(config, "POST", url.String())` — note the string URL, unlike
  SPDY's `*url.URL`), a SPDY executor (`NewSPDYExecutor(config, "POST", url)`), and wraps both in
  `NewFallbackExecutor(wsExec, spdyExec, shouldFallbackToSPDY)`.
- `shouldFallbackToSPDY(err error) bool` (new, extracted as a named func for testability):
  kubectl's exact predicate, `httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)`.
- `ExecCiliumDbg` now calls `newFallbackExecutor` instead of constructing SPDY directly; all
  downstream behavior (StreamWithContext, CodeExitError handling, error wrapping style) unchanged.
- SEC-01 tripwire (`cmd/cpg/mcp_audit_test.go`) extended from a single-symbol
  `execConstructorSymbol` const to a 3-symbol `execConstructorSymbols` set (`NewSPDYExecutor`,
  `NewWebSocketExecutor`, `NewFallbackExecutor`). Negative half: none of the three may be
  genuinely reachable from `runMCPServer`. Positive half (non-vacuity): both
  `NewFallbackExecutor` and `NewSPDYExecutor` must be genuinely reachable from `runAuditWindow`
  (the fallback wraps both, so seeing only one would be a weaker proof). The test's own `t.Logf`
  diagnostic output confirms the third symbol, `NewWebSocketExecutor`, is also genuinely
  reachable from `runAuditWindow`, though it isn't asserted with `require.True` (plan only
  specified the two).
- README: audit-window RBAC paragraph now names the transport ("WebSocket with SPDY fallback,
  same as kubectl since 1.30+").

## Verification results

- `rtk proxy go build ./...` — clean, no errors.
- `TestExecFallbackPredicate` (`pkg/k8s/exec_test.go`): PASS — true for
  `&httpstream.UpgradeFailureError{Cause: ...}` (exported struct + field, no exported
  constructor needed), false for a plain `errors.New(...)`.
- `rtk proxy go test ./pkg/k8s/... -count=1 -race`: PASS (2.1s), all existing tests untouched
  and green (existing tests stub via the `execCiliumDbgFn` seam, never touch the exec
  construction site directly).
- `TestMCPAuditReadonlyReachability`: PASS (36.8s) — body byte-identical, confirmed via `git
  diff` scoped entirely to lines 494+ (const/tripwire only).
- `TestAuditWindowNotReachableFromMCP`: PASS (39.9s) — negative half clean (none of the 3
  exec constructors reachable from `runMCPServer`); positive half found all three constructors
  genuinely reachable from `runAuditWindow` via `runAuditWindow -> Manager.Open ->
  flipIfNeeded -> NewManager$4 -> k8s.ReadPolicyAuditMode -> k8s.ExecCiliumDbg ->
  k8s.newFallbackExecutor`.
- `TestReadmeAuditWindowSection`, `TestReadmeCompatSection`, `TestRunbookAuditWindowStep`:
  PASS — golden pins intact after the README edit.
- Full suite: `rtk proxy go test ./... -count=1 -race -timeout 900s` — all 13 packages green
  (`cmd/cpg` 179.8s, rest 1-2s each).
- `git diff go.mod go.sum` — empty (zero new dependencies, as scoped: both `NewWebSocketExecutor`
  and `httpstream.IsHTTPSProxyError`/`IsUpgradeFailure` were already vendored at the pinned
  `k8s.io/client-go@v0.35.4` / `k8s.io/apimachinery@v0.35.4`).

## Deviations from Plan

None — plan executed exactly as written. One judgment call within Task 2's scope: the plan's
positive-half wording ("at least NewFallbackExecutor AND NewSPDYExecutor") was implemented
literally as two `require.True` assertions rather than a single combined check, for clearer
per-symbol failure diagnostics if either regresses independently.

## Self-Check

- `pkg/k8s/exec.go` — FOUND, contains `newFallbackExecutor` and `shouldFallbackToSPDY`.
- `pkg/k8s/exec_test.go` — FOUND, contains `TestExecFallbackPredicate`.
- `cmd/cpg/mcp_audit_test.go` — FOUND, contains `execConstructorSymbols` map (3 entries).
- `README.md` — FOUND, audit-window paragraph names WebSocket/SPDY fallback.
- Commit 2465740 — FOUND in `git log`.
- Commit 4707c44 — FOUND in `git log`.
- Commit 9afd645 — FOUND in `git log`.

## Self-Check: PASSED
