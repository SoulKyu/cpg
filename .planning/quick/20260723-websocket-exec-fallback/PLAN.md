---
quick_id: 260723-b26
slug: websocket-exec-fallback
created: 2026-07-23
type: quick
requirements: [AUD-FUT-01]
files_modified:
  - pkg/k8s/exec.go
  - pkg/k8s/exec_test.go
  - cmd/cpg/mcp_audit_test.go
  - README.md
autonomous: true
---

# Quick Task: WebSocket exec with SPDY fallback (AUD-FUT-01)

## Why

`kubectl` defaults to WebSocket exec since 1.30+; SPDY is on its way out and already breaks through some proxies/ingresses. `cpg audit-window`'s exec path (`pkg/k8s/exec.go`) is SPDY-only today — it will silently start failing on clusters/paths that drop SPDY. Mirror kubectl's own fallback construction.

## Scoping (verified against vendored deps — zero new go.mod entries)

- `k8s.io/client-go@v0.35.4/tools/remotecommand`: `NewWebSocketExecutor(config, method string, url string)` (NOTE: url is a **string**, unlike SPDY's `*url.URL`) and `NewFallbackExecutor(primary, secondary Executor, shouldFallback func(error) bool)`.
- `k8s.io/apimachinery@v0.35.4/pkg/util/httpstream`: `IsUpgradeFailure(err)`, `IsHTTPSProxyError(err)`.
- kubectl's exact pattern (kubectl@v0.33.0 pkg/cmd/exec/exec.go:158): WebSocket primary, SPDY secondary, fallback when `httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)`.
- SEC-01 impact: `cmd/cpg/mcp_audit_test.go:502` `execConstructorSymbol` is a single-symbol const watching `remotecommand.NewSPDYExecutor`; the negative half (unreachable from runMCPServer) and positive half (reachable from runAuditWindow) both key on it.

## Tasks

### Task 1: fallback executor in pkg/k8s/exec.go

In the exec construction site (`exec.go:73`), replace the bare SPDY executor with kubectl's construction:

```go
spdyExec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
// handle err
wsExec, err := remotecommand.NewWebSocketExecutor(config, "POST", req.URL().String())
// handle err
executor, err := remotecommand.NewFallbackExecutor(wsExec, spdyExec, func(err error) bool {
    return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
})
// handle err
```

Add the `k8s.io/apimachinery/pkg/util/httpstream` import. Keep everything else (StreamOptions, CodeExitError handling) unchanged. Follow the file's existing error-wrapping style.

Tests (`pkg/k8s/exec_test.go`): the existing tests stub via the `execFn` seam so they keep passing untouched; add ONE focused test `TestExecFallbackPredicate` proving the predicate returns true for an upgrade-failure error and false for a plain error (use `httpstream.NewUpgradeFailureError` if exported, else construct via the package's error type; check the vendored source for the exact constructor). If the predicate is written inline, extract it as `shouldFallbackToSPDY(err error) bool` so it's testable — small named function, no interface.

Verify: `rtk proxy go build ./...` && `rtk proxy go test ./pkg/k8s/... -count=1 -race`.
Commit: `feat(quick): use WebSocket exec with SPDY fallback in audit-window exec path`

### Task 2: extend the SEC-01 tripwire to the full exec-constructor set

In `cmd/cpg/mcp_audit_test.go`: replace the single `execConstructorSymbol` const with a set:

```go
var execConstructorSymbols = map[string]bool{
    "k8s.io/client-go/tools/remotecommand.NewSPDYExecutor":      true,
    "k8s.io/client-go/tools/remotecommand.NewWebSocketExecutor": true,
    "k8s.io/client-go/tools/remotecommand.NewFallbackExecutor":  true,
}
```

- Negative half: NO cpg-owned function genuinely reachable from `runMCPServer` may statically call ANY of the three.
- Positive half (non-vacuity): at least `NewFallbackExecutor` AND `NewSPDYExecutor` must be genuinely reachable from `runAuditWindow` (the fallback wraps both).
- Do NOT touch `TestMCPAuditReadonlyReachability`'s body (byte-identical invariant stands).

Verify: `rtk proxy go test ./cmd/cpg/... -run "TestAuditWindowNotReachableFromMCP|TestMCPAuditReadonlyReachability" -count=1 -race -timeout 600s` (~2 min).
Commit: `test(quick): extend SEC-01 exec tripwire to WebSocket + fallback constructors`

### Task 3: README note + full-suite gate

README's audit-window / RBAC prose: if it names the transport (search for "SPDY"), update to "WebSocket with SPDY fallback (same as kubectl)"; if it doesn't name it, add half a sentence in the audit-window paragraph. Keep all golden pins green (`readme_compat_test.go`, `audit_docs_test.go` — run them).

Full gate: `rtk proxy go test ./... -count=1 -race -timeout 900s` (all 13 packages).
Commit: `docs(quick): note WebSocket-first exec transport in README` (skip commit if README needs no change — then fold the gate into Task 2's verification).

## Done criteria

- Exec path constructs FallbackExecutor(WebSocket → SPDY) with kubectl's exact predicate.
- Predicate unit-tested; existing exec/manager/CLI tests untouched and green.
- SEC-01: negative half covers all three constructors; positive half non-vacuous; `TestMCPAuditReadonlyReachability` body untouched.
- Full suite green; `git diff go.mod go.sum` empty.
