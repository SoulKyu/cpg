---
phase: 19-security-hardening-end-to-end-validation
fixed_at: 2026-07-21T19:34:15Z
review_path: .planning/phases/19-security-hardening-end-to-end-validation/19-REVIEW.md
iteration: 1
findings_in_scope: 7
fixed: 6
skipped: 1
status: partial
---

# Phase 19: Code Review Fix Report

**Fixed at:** 2026-07-21T19:34:15Z
**Source review:** .planning/phases/19-security-hardening-end-to-end-validation/19-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 7 (WR-01..WR-04 mandatory; IN-02 mandatory; IN-01 applied as small/safe; IN-03 evaluated and skipped)
- Fixed: 6
- Skipped: 1

| ID | Status | Commit | What changed |
| --- | --- | --- | --- |
| WR-01 | fixed | `7665120` | Replaced tautological BFS self-check with `CallGraph.Nodes[root]` + reachability-floor assertions |
| WR-02 | fixed | `37dbea4` | Extended `disallowedFSWrite` with Chmod/Truncate/Symlink/Link/Chown/Lchown/Chtimes |
| WR-03 | fixed | `e92669f` | Extended `k8sWriteVerbs` with DeleteCollection/UpdateStatus/ApplyStatus |
| WR-04 | fixed | `5b5d5c7` | Added `t.Cleanup` kill-guard to `startE2ESubprocess` |
| IN-01 | fixed | `8c9e6f5` | Documented the func-value call-dispatch scan gap (comment only) |
| IN-02 | fixed | `f0d706e` | Softened README exec-plugin timeout claim, noted kubeconfig-load exception |
| IN-03 | skipped | — | Proposed fix is a concurrency restructure, not small/safe; documented below |

## Fixed Issues

### WR-01: Audit's root-reachability self-check is tautological — a future refactor can silently make the whole audit vacuous

**Files modified:** `cmd/cpg/mcp_audit_test.go`
**Commit:** `7665120`
**Applied fix:** `bfsRes.visited[root]` is always `true` by construction (`bfsFromRoot` seeds `{root: true}` before consulting the graph), so the old self-check proved nothing. Replaced it with `require.NotNil(t, rtaRes.CallGraph.Nodes[root], ...)`, checked before the BFS runs. After `cpgOwned` is computed, replaced the weak `require.NotEmpty` with `require.Greater(t, len(cpgOwned), 1, ...)` and added `require.Contains(t, symbolSet(cpgOwned), "(*github.com/SoulKyu/cpg/pkg/session.Manager).Start", ...)` — pinning a known-reachable deep writer so the audit cannot silently pass while scanning nothing. Added the `symbolSet` helper the assertion needs. Verified: `TestMCPAuditReadonlyReachability` passes (13.8s, non-race) — the pinned symbol is already one of the 5 hand-audited `fsWriteAllowlist` entries, confirming it is genuinely reachable today.

### WR-02: `disallowedFSWrite` watchlist omits filesystem-mutating `os.*` functions — one is already used in the repo

**Files modified:** `cmd/cpg/mcp_audit_test.go`
**Commit:** `37dbea4`
**Applied fix:** Added `os.Chmod`, `os.Truncate`, `os.Symlink`, `os.Link`, `os.Chown`, `os.Lchown`, `os.Chtimes` to `disallowedFSWrite`. Verified green exactly as the review predicted: `os.Chmod` at `pkg/output/writer.go:95` is called from `(*output.Writer).Write`, which is already in `fsWriteAllowlist`, so the audit stays green while the watchlist is now sound against a *new*, non-allowlisted caller. `TestMCPAuditReadonlyReachability` passes (13.3s).

### WR-03: `k8sWriteVerbs` omits real destructive verbs (`DeleteCollection`, `UpdateStatus`, `ApplyStatus`)

**Files modified:** `cmd/cpg/mcp_audit_test.go`
**Commit:** `e92669f`
**Applied fix:** Added the three verbs to `k8sWriteVerbs`. Verified green: `pkg/k8s` issues zero reachable write verbs of any name today, so the audit's baseline holds. `TestMCPAuditReadonlyReachability` passes (13.7s).

### WR-04: e2e subprocess has no `t.Cleanup` — a mid-test failure orphans a `-race` `cpg mcp` process

**Files modified:** `cmd/cpg/mcp_e2e_test.go`
**Commit:** `5b5d5c7`
**Applied fix:** Registered `t.Cleanup(func() { if cmd.Process != nil { _ = cmd.Process.Kill() } })` immediately after `require.NoError(t, cmd.Start())` in `startE2ESubprocess` — covers both the `client.Connect`-failure path (reaper goroutine not yet started) and any `require.*` failure before `stdinW.Close()`. `Kill()` is idempotent against an already self-exited process: Go's `os.Process` tracks `Wait()` completion internally and turns a later `Kill()` into a no-op (`ErrProcessDone`) instead of signaling a potentially-recycled PID, so the existing graceful-exit path is unaffected. Verified under `-race`: `TestMCPE2EGracefulLifecycle` (8.3s) and `TestMCPE2EUngracefulDisconnect` (2.4s) both pass.

### IN-01: Stage 3 scan never checks func-value (indirect) calls

**Files modified:** `cmd/cpg/mcp_audit_test.go`
**Commit:** `8c9e6f5`
**Applied fix:** Documentation-only, per the review's own suggested minimal remediation ("worth a one-line acknowledgement in the docstring even if left unhandled"). Added a comment immediately before the Stage 3 loop stating the func-value-dispatch gap explicitly (no cpg code currently dispatches a write this way, so it's a soundness-completeness gap, not a live miss). No behavior change. Verified: `TestMCPAuditReadonlyReachability` passes (13.6s).

### IN-02: README overstates the setup-timeout's coverage vs. the code's own documented unbounded path

**Files modified:** `README.md`
**Commit:** `f0d706e`
**Applied fix:** Replaced "turns **any** residual hang into an actionable error" with "turns the common exec-plugin re-auth hang (during dial/port-forward) into an actionable error … — the one known exception is a hang specifically inside kubeconfig load itself (`k8s.LoadKubeConfig()` takes no `ctx`), which neither this timeout nor cancellation can bound," matching the exception `pkg/session/manager.go:172-178` already documents in its own comments. Not a Go source file — Tier 1 re-read only (no applicable syntax checker), per the 3-tier verification's fallback tier.

## Skipped Issues

### IN-03: `cmd.Wait()` is called concurrently with reads from `StdoutPipe` — the documented ordering footgun

**File:** `cmd/cpg/mcp_e2e_test.go:308-334, 659`
**Reason:** Skipped per task scope (apply only if small/obviously safe). The review's own proposed remedy is a genuine concurrency restructure — "signal completion from the reader (or read stdout to EOF in a dedicated goroutine whose done-channel is awaited) before consulting `rawTee`" — which changes when `Wait()` observably completes relative to the stdout tee. That is a nontrivial rework of already carefully-reasoned `-race` test-harness synchronization (the file's own `syncBuffer` doc comment documents a previously-found race in this exact neighborhood) and risks introducing a new subtle bug (e.g. a deadlock on an undrained done-channel) if applied mechanically rather than deliberately designed and reviewed. The review itself classifies the current behavior as "benign in practice": every JSON-RPC response the test asserts on is read synchronously via `CallTool` before stdin is closed, so no complete frame is lost and `assertStdoutPurity` only ever sees whole frames — a latent flake vector under load, not a live failure.
**Original issue:** `os/exec`'s `StdoutPipe` doc states it is incorrect to call `Wait` before all reads from the pipe complete. Here `cmd.Wait()` runs in a background goroutine (line 324 pre-fix / see `startE2ESubprocess`) concurrently with the MCP client's read loop draining `stdoutR` via `io.TeeReader`, which can race the client's final read and, under load, could leave the last bytes untee'd before `assertStdoutPurity` snapshots `rawTee.Bytes()`.

---

## Final verification (full package, all fixes applied)

```
rtk proxy go build ./...                                   # clean, no output
rtk proxy go test ./cmd/cpg/ -count=1 -race                 # PASS, ok  github.com/SoulKyu/cpg/cmd/cpg  81.023s
```

All tests in `cmd/cpg` (including `TestMCPAuditReadonlyReachability`, `TestMCPE2EGracefulLifecycle`, `TestMCPE2EUngracefulDisconnect`, and every pre-existing in-memory MCP/replay test) pass under `-race` with all six fixes applied.

---

_Fixed: 2026-07-21T19:34:15Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
