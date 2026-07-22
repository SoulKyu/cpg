---
phase: 19-security-hardening-end-to-end-validation
reviewed: 2026-07-21T19:18:56Z
depth: standard
files_reviewed: 4
files_reviewed_list:
  - cmd/cpg/mcp_audit_test.go
  - cmd/cpg/mcp_e2e_test.go
  - go.mod
  - README.md
findings:
  critical: 0
  warning: 4
  info: 3
  total: 7
status: issues_found
---

# Phase 19: Code Review Report

**Reviewed:** 2026-07-21T19:18:56Z
**Depth:** standard
**Files Reviewed:** 4
**Status:** issues_found

## Summary

Phase 19 ships a structural readonly audit (`mcp_audit_test.go`), a real-subprocess stdio e2e suite (`mcp_e2e_test.go`), a README `## MCP Server` section, and a one-line go.mod promotion of `golang.org/x/tools` to a direct (test) dependency.

Cross-checked the two test files against the code they claim to protect (`cmd/cpg/mcp.go`, `mcp_tools.go`, `pkg/session/manager.go`, `pkg/session/paths.go`, `pkg/output/writer.go`) rather than reading them in isolation.

**Overall assessment:** The e2e concurrency plumbing is genuinely careful (mutex-guarded buffers, buffered exit channel, `<-stream.Context().Done()` fan-out proof, two-stage synchronization against the async-relay race) and the allowlist keys in the audit match the current SSA symbols exactly. **But the security audit is weaker than its own docstring and the README claim it backs.** The three concrete concerns the phase brief raised — "could a write sneak past?", subprocess/pipe robustness, README-vs-code accuracy — each surfaced a real defect:

- The audit's headline self-check (`require.True(bfsRes.visited[root])`) is **tautological** and does not actually verify the audit is scanning anything (WR-01).
- Both watchlists are **incomplete**: `disallowedFSWrite` omits filesystem-mutating `os.*` functions that are *already used in the repo* (`os.Chmod`), and `k8sWriteVerbs` omits real destructive verbs (`DeleteCollection`, `UpdateStatus`) — so the "no write reaches outside the tmpdir / no K8s write verb is reachable" guarantee has holes (WR-02, WR-03).
- The e2e spawns a `-race` subprocess with **no cleanup guarantee**, so any mid-test failure orphans it (WR-04).

None is a live vulnerability today (verified: no unwatched write and no K8s write verb is currently reachable from `runMCPServer`), so all are WARNING/INFO rather than BLOCKER. But because this phase's entire deliverable is a *proof* of a security property, shipping the proof with known holes is exactly the kind of soft pass to avoid.

go.mod is clean: the sole change (`x/tools` indirect→direct) is correct — it is imported by `mcp_audit_test.go`, its transitive deps (`x/mod`, `x/sync`) are already direct, and `go.sum` needed no change because the module was already in the graph.

## Warnings

### WR-01: Audit's root-reachability self-check is tautological — a future refactor can silently make the whole audit vacuous

**File:** `cmd/cpg/mcp_audit_test.go:234-235` (mechanism in `bfsFromRoot`, lines 117-141)

**Issue:**
The audit's soundness hinges on `runMCPServer` actually being a node in the RTA callgraph. The guard that is supposed to prove this does not:

```go
bfsRes := bfsFromRoot(rtaRes.CallGraph, root)
require.True(t, bfsRes.visited[root], "runMCPServer must be reachable from itself (BFS root)")
```

`bfsFromRoot` seeds its result with `visited: map[*ssa.Function]bool{root: true}` (line 119) *before* it ever consults the graph, and returns early with exactly that seed when `cg.Nodes[root] == nil` (lines 122-124). Therefore `bfsRes.visited[root]` is **always true regardless of whether `root` is in the graph** — the assertion can never fail and proves nothing.

The failure mode this masks: if a future change drops `runMCPServer` from the RTA graph (e.g. command wiring is restructured so cobra's `RunE` func-value dispatch no longer connects to it, or the composition root is renamed/moved), `bfsFromRoot` returns `{root: true}` only, `cpgOwned` collapses to `{runMCPServer}`, Stage 3 scans just `runMCPServer`'s own (near-empty) body, and **the audit goes green while auditing nothing** — the precise false-negative the D-04 "re-runnable, catches future leaks" contract exists to prevent. The audit is non-vacuous *today* (the empirically-found 5 allowlist entries prove the BFS currently descends into `pkg/session`/`pkg/output`/`pkg/evidence`/`pkg/hubble`), so this is a latent robustness gap, not a current break.

**Fix:** Assert the root is genuinely in the graph, and add a floor proving the BFS actually descended into the deep writer subsystem:

```go
require.NotNil(t, rtaRes.CallGraph.Nodes[root],
    "runMCPServer must be a node in the RTA callgraph — otherwise the BFS scans nothing and this audit passes vacuously")

bfsRes := bfsFromRoot(rtaRes.CallGraph, root)
// ... after computing cpgOwned ...
require.Greater(t, len(cpgOwned), 1,
    "expected runMCPServer to transitively reach cpg-owned functions; a size of 1 means the audit is vacuous")
// Strongest form: pin a known-reachable deep writer so a vacuous audit is impossible.
require.Contains(t, symbolSet(cpgOwned), "(*github.com/SoulKyu/cpg/pkg/session.Manager).Start",
    "the session writer subsystem must be reachable from runMCPServer for this audit to be meaningful")
```

### WR-02: `disallowedFSWrite` watchlist omits filesystem-mutating `os.*` functions — one is already used in the repo

**File:** `cmd/cpg/mcp_audit_test.go:29-40`

**Issue:**
The set is documented as "the watched set of write-capable `os.*` package-level functions … Deliberately an over-approximation." It is actually an **under**-approximation: it omits several `os` functions that mutate the filesystem without going through a watched constructor:

- `os.Chmod`, `os.Truncate`, `os.Symlink`, `os.Link`, `os.Chown`, `os.Lchown`, `os.Chtimes`

`os.Truncate`/`os.Symlink`/`os.Link` create or destroy filesystem state on a caller-chosen path with no watched constructor in the chain; `os.Chmod`/`os.Chown` mutate metadata. A function reachable from `runMCPServer` doing `os.Truncate("/etc/whatever", 0)` or `os.Symlink(evil, linkInTmp)` would pass this audit undetected — directly contradicting the docstring's "no filesystem write outside the 5 allowlisted functions is reachable" and the README's "cpg … never writes outside its own session tmpdir" (README:508).

This is not hypothetical: `os.Chmod` is **already called in the write path** at `pkg/output/writer.go:95` (`os.Chmod(tmpPath, 0644)`). It escapes a finding today only because its caller `(*output.Writer).Write` happens to be allowlisted — but the watchlist would not catch the same call from a *new*, non-allowlisted function, defeating the "brand-new writer function still fails until reviewed" guarantee the allowlist doc (lines 61-72) sells.

**Fix:** Extend the set to cover the metadata/link/truncate mutators:

```go
var disallowedFSWrite = map[string]bool{
    "os.WriteFile": true, "os.Create": true, "os.CreateTemp": true,
    "os.OpenFile": true, "os.Mkdir": true, "os.MkdirAll": true,
    "os.MkdirTemp": true, "os.Rename": true, "os.Remove": true, "os.RemoveAll": true,
    // added: filesystem-mutating calls that bypass a watched constructor
    "os.Chmod": true, "os.Truncate": true, "os.Symlink": true, "os.Link": true,
    "os.Chown": true, "os.Lchown": true, "os.Chtimes": true,
}
```

(If any addition trips on an existing allowlisted writer — e.g. `os.Chmod` in `(*output.Writer).Write` — that caller is already in `fsWriteAllowlist`, so the audit stays green while the watchlist gets sound.)

### WR-03: `k8sWriteVerbs` omits real destructive verbs (`DeleteCollection`, `UpdateStatus`, `ApplyStatus`)

**File:** `cmd/cpg/mcp_audit_test.go:53-59`

**Issue:**
Property 1 matches interface-dispatch method names against `{Create, Update, Patch, Delete, Apply}`, on the stated premise that "client-go's typed clients and the dynamic client expose every K8s write verb exclusively as interface methods, so a bare-name match … IS the K8s-write signal." The premise is right; the **name set is incomplete**. client-go's typed `*Interface` and `dynamic.ResourceInterface` expose these additional write verbs as distinct method names, none of which are in the set:

- `DeleteCollection` — bulk delete (highly destructive)
- `UpdateStatus` — subresource write
- `ApplyStatus` — subresource server-side apply

A future handler reaching `clientset.…(ns).DeleteCollection(ctx, …)` or `…UpdateStatus(ctx, …)` would **not** be flagged, even though the docstring promises "the repo baseline is zero reachable K8s write verbs, and any hit is new." Verified latent, not live: `pkg/k8s` currently issues zero write verbs of any name.

**Fix:**

```go
var k8sWriteVerbs = map[string]bool{
    "Create": true, "Update": true, "Patch": true, "Delete": true, "Apply": true,
    // added: verbs client-go also exposes as interface methods
    "DeleteCollection": true, "UpdateStatus": true, "ApplyStatus": true,
}
```

### WR-04: e2e subprocess has no `t.Cleanup` — a mid-test failure orphans a `-race` `cpg mcp` process and leaks its `cmd.Wait` goroutine

**File:** `cmd/cpg/mcp_e2e_test.go:301-334` (`startE2ESubprocess`)

**Issue:**
The subprocess is started with `exec.Command(binPath, "mcp")` (line 305) — **not** `exec.CommandContext(ctx, …)` — and the harness registers no cleanup to terminate it. Termination is entirely implicit: it relies on the test reaching `e2e.stdinW.Close()` on the happy path so the subprocess sees stdin EOF and self-exits.

On any early-exit path this breaks:
- If `client.Connect` fails (line 320-321 `require.NoError`), `cmd.Start()` already succeeded (line 313) but the `go func(){ exitedCh <- cmd.Wait() }()` reaper (line 324) has not been set up yet → the subprocess is fully orphaned with *nobody* calling `Wait`, becoming a running orphan (then a zombie once it exits).
- If any `require.*` between start and `stdinW.Close()` fails (start_session, `require.Eventually` timeouts, schema assertions), the test goroutine unwinds via `runtime.Goexit` without closing stdin → the `-race`-instrumented subprocess keeps running, blocked on stdin, and the `cmd.Wait` goroutine stays blocked, for the **remainder of the test-binary run** (they only clear when the parent `go test` process exits and the OS closes the pipe).

Since `ctx` never kills the process (no `CommandContext`), the 120s/60s deadlines do not bound the subprocess either. This is a test-reliability defect: leaked `-race` subprocesses consume real memory/CPU and muddy CI diagnostics (and the fake relay's `t.Cleanup` `grpcServer.Stop()` will make the orphan's pipeline log spurious stream errors after the test already failed).

**Fix:** Register a guaranteed kill immediately after a successful `Start`:

```go
require.NoError(t, cmd.Start())
t.Cleanup(func() {
    if cmd.Process != nil {
        _ = cmd.Process.Kill() // idempotent w.r.t. a process that already self-exited
    }
})
```

(Placing it right after `cmd.Start()` also covers the `client.Connect`-failure path, which the current code leaves completely unmanaged.)

## Info

### IN-01: Stage 3 scan inspects only static and interface-dispatch calls — func-value (indirect) calls are never checked

**File:** `cmd/cpg/mcp_audit_test.go:251-278`

**Issue:** The per-instruction scan handles exactly two shapes: `common.StaticCallee() != nil` (Property 2) and `common.IsInvoke()` (Property 1). A `CallInstruction` that dispatches through a **func value** — e.g. `w := os.WriteFile; w(path, data, 0o644)` — has a nil `StaticCallee()` and is not an invoke, so it falls through both branches and is scanned by neither property. No cpg code currently dispatches a filesystem/K8s write through a func value (the atomic writers all call `os.*` directly), so this is a soundness completeness gap rather than a live miss, and it compounds WR-02/WR-03. Worth a one-line acknowledgement in the docstring even if left unhandled; a full fix would resolve func-value callees against the RTA graph's `Out` edges for the call site.

### IN-02: README overstates the setup-timeout's coverage vs. the code's own documented unbounded path

**File:** `README.md:551`

**Issue:** The exec-credential caveat states "`start_session`'s bounded setup timeout turns **any** residual hang into an actionable error rather than a silent wait." `pkg/session/manager.go:172-178` documents an explicit exception in its own words: "`k8s.LoadKubeConfig()` … takes no ctx parameter at all, so a hang specifically inside kubeconfig load is reachable by neither the timeout nor this cancellation — a pre-existing upstream helper limitation." So "any residual hang" is broader than the implementation guarantees. The practical exec-plugin scenario the paragraph describes (re-auth during the first API call) does run under `setupCtx` via `PortForwardToRelay`, so the reassurance is mostly sound — but the absolute "any" is inaccurate. Suggest softening to "the bounded setup timeout turns the common exec-plugin re-auth hang (during dial/port-forward) into an actionable error" and, if desired, noting the kubeconfig-load edge as a known gap.

### IN-03: `cmd.Wait()` is called concurrently with reads from `StdoutPipe` — the documented ordering footgun

**File:** `cmd/cpg/mcp_e2e_test.go:308-334, 659`

**Issue:** `os/exec`'s `StdoutPipe` doc: "It is thus incorrect to call `Wait` before all reads from the pipe have completed." Here `cmd.Wait()` runs in a background goroutine (line 324) concurrently with the MCP client's read loop draining `stdoutR` via the `io.TeeReader` (line 316). On process exit, `Wait` closes the read end of the pipe, which can race the client's final read and turn a clean `io.EOF` into a "file already closed" error, and can leave the last bytes untee'd before `assertStdoutPurity` snapshots `rawTee.Bytes()` (line 659). It is benign in practice — every JSON-RPC response the test asserts on is read synchronously via `CallTool` *before* stdin is closed, so no complete frame is lost and the purity check only ever sees whole frames — but it is a latent flake vector under load. A robust alternative is to signal completion from the reader (or read stdout to EOF in a dedicated goroutine whose done-channel is awaited) before consulting `rawTee`.

---

_Reviewed: 2026-07-21T19:18:56Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
