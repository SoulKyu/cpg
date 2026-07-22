---
phase: 23-managed-audit-window-sec-01-evolution
plan: 03
subsystem: cli-security
tags: [cobra, ssa, rta, callgraph, sec-01, remotecommand, cilium]

requires:
  - phase: 23-02
    provides: pkg/auditwindow.Manager (Open/Close/Shutdown, sync.Once-guarded revert, RevertResult)
provides:
  - cpg audit-window CLI command (foreground, signal-bound, TTL-bounded)
  - SEC-01 structural evolution proving the exec path stays unreachable from runMCPServer
affects: [23-04, future-mcp-surface-changes]

tech-stack:
  added: []
  patterns:
    - "bfsFromRootGenuine + isBareFuncValueDispatch: genuine-edge callgraph BFS filtering both RTA's reflect.Value.Call sweep (Edge.Site == nil) AND its second, undocumented bare-func() indirect-dispatch sweep (Edge.Site != nil but resolved via the same whole-program address-taken heuristic)"
    - "auditWindowDetectVersion / auditWindowNewManager seams mirroring bootstrapDetectVersion / l7ClientFactory for hermetic CLI testing"

key-files:
  created:
    - cmd/cpg/audit_window.go
    - cmd/cpg/audit_window_test.go
  modified:
    - cmd/cpg/main.go
    - cmd/cpg/mcp_audit_test.go

key-decisions:
  - "auditWindowNewManager seam owns kubeconfig loading internally (not a separate CLI-level step) so tests substitute the whole Manager construction with zero kubeconfig involvement"
  - "Manager.Open already performs the daemon-wide precondition hard-refusal internally (wave 2); runAuditWindow does not duplicate this check, it only propagates Open's error"
  - "bfsFromRootGenuine extended with isBareFuncValueDispatch beyond the RESEARCH-specified Edge.Site == nil filter, after empirically discovering a second RTA sweep via a diagnostic edge dump (see Deviations)"

requirements-completed: [AUD-03, AUD-04]

duration: ~25min
completed: 2026-07-22
---

# Phase 23 Plan 03: audit-window CLI + SEC-01 Structural Evolution Summary

**`cpg audit-window -n <ns> --ttl <d>` foreground signal-bound command wired to pkg/auditwindow.Manager, plus a two-layer genuine-edge SSA/RTA tripwire (`Edge.Site == nil` + a second, execution-discovered bare-`func()`-dispatch filter) proving `remotecommand.NewSPDYExecutor` is genuinely unreachable from `runMCPServer` and genuinely reachable from `runAuditWindow`.**

## Performance

- **Duration:** ~25 min
- **Completed:** 2026-07-22
- **Tasks:** 2
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments
- `cpg audit-window` is the one sanctioned mutating CLI command: `-n` required, `--ttl` always-bounded (30m default), foreground/signal-bound (`signal.NotifyContext`), `defer wm.Shutdown()` on every exit path (explicit Ctrl+C/SIGTERM, TTL expiry, or an Open failure).
- Registered in `main.go` next to the other subcommands.
- SEC-01 tripwire evolved (AUD-04): `TestAuditWindowNotReachableFromMCP` proves both halves — NewSPDYExecutor is NOT genuinely reachable from `runMCPServer`, and IS genuinely (non-vacuously) reachable from `runAuditWindow` via the real `readFn` seam path.
- `TestMCPAuditReadonlyReachability` (the pre-existing SEC-01 audit) stays byte-identical: `git diff master -- cmd/cpg/mcp_audit_test.go` shows 233 insertions, 0 deletions across the whole plan.

## Task Commits

1. **Task 1: audit-window cobra command + registration + TTL-expiry revert** - `6b7157a` (feat)
2. **Task 2: SEC-01 tripwire — genuine-edge reachability (Edge.Site == nil filter + bare-func() dispatch filter)** - `1c1077d` (test)

**Plan metadata:** (this commit)

## Files Created/Modified
- `cmd/cpg/audit_window.go` - `newAuditWindowCmd`/`runAuditWindow`: flags, signal ctx, `auditWindowDetectVersion`/`auditWindowNewManager` seams, TTL timer, deferred `Shutdown`
- `cmd/cpg/audit_window_test.go` - `stubAuditWindowManager` (sync.Once-guarded revert counter mirroring the real Manager's contract) + the four `TestAuditWindow_*` tests
- `cmd/cpg/main.go` - `rootCmd.AddCommand(newAuditWindowCmd())`
- `cmd/cpg/mcp_audit_test.go` - `bfsFromRootGenuine`, `isBareFuncValueDispatch`, `restrictToCpgOwned`, `TestAuditWindowNotReachableFromMCP` (all additions; `TestMCPAuditReadonlyReachability` untouched)

## Decisions Made
- `auditWindowNewManager`'s seam signature takes `(ctx, logger, binary)` and loads the kubeconfig internally (production default), rather than having `runAuditWindow` load a kubeconfig itself before calling the seam — keeps the CLI hermetically testable with zero kubeconfig/live-cluster involvement, mirroring `bootstrapDetectVersion`'s own internal-load convention.
- `runAuditWindow` does not re-implement the daemon-wide `policy-audit-mode` precondition check — `pkg/auditwindow.Manager.Open` (wave 2) already performs it and hard-refuses; the CLI only propagates `Open`'s error, avoiding drift between the two check sites.
- `stubAuditWindowManager` in `audit_window_test.go` deliberately mirrors the real `Manager`'s `sync.Once`-guarded revert contract (a shared `revertOnce` behind both `Close` and `Shutdown`) so the TTL-expiry test asserts "the revert path ran exactly once" against the same idempotency guarantee production code provides, rather than a raw call-count that would double-count the explicit `Close` + deferred `Shutdown` pair.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `bfsFromRootGenuine`'s `Edge.Site == nil` filter alone was insufficient — a second RTA sweep produced a genuine SEC-01 false positive**
- **Found during:** Task 2, first run of `TestAuditWindowNotReachableFromMCP`
- **Issue:** The RESEARCH-specified filter (skip edges where `Edge.Site == nil`, i.e. RTA's `reflect.Value.Call` sweep) was implemented exactly as designed, but the negative-half assertion still failed: `k8s.ExecCiliumDbg` was flagged as genuinely reachable from `runMCPServer` via the chain `runMCPServer -> (*pkg/session.Manager).Shutdown -> (*pkg/auditwindow.Manager).Close$1 -> (*pkg/auditwindow.Manager).Close$1$1 -> pkg/auditwindow.NewManager$5 -> pkg/k8s.SetPolicyAuditMode -> pkg/k8s.ExecCiliumDbg`. `pkg/session` has zero references to `pkg/auditwindow` (confirmed via grep) — this call chain has no basis in real program semantics.
- **Root cause (confirmed via a throwaway diagnostic test dumping `cg.Nodes[shutdownFn].Out`, deleted before commit — never part of any commit):** `(*session.Manager).Shutdown`'s single indirect call through a bare `func()`-typed value (`cancel()`, a `context.CancelFunc`) is resolved by RTA via a whole-program "any address-taken function of matching signature" sweep — the identical mechanism `reflect.Value.Call` uses — but the resulting edges carry the real call instruction as `Site` (non-nil), because the call site itself is genuine even though the target resolution is a coarse, unsound approximation. The diagnostic dump showed this single call site fanning out to several hundred unrelated whole-program closures (runtime internals, gRPC internals, and — load-bearing — every `pkg/auditwindow.Manager` exit-path closure), none of which `Shutdown` can actually invoke.
- **Fix:** Added `isBareFuncValueDispatch` and wired it into `bfsFromRootGenuine`: an edge is also excluded from genuine-reachability propagation when its call site is an indirect (non-static, non-invoke) dispatch through a value whose signature is the bare, zero-parameter, zero-result `func()` type. Verified this does not affect the positive-path proof: every real seam field this codebase dispatches through (`readFn`, `setFn`, `listCEFn`, `watchCEFn`, `preconditionFn`, `resolveCurrentIDFn`) carries a non-trivial signature (at minimum a `context.Context` parameter), so none are bare `func()` and all remain traceable.
- **Files modified:** `cmd/cpg/mcp_audit_test.go` (only file touched by this fix)
- **Verification:** `TestAuditWindowNotReachableFromMCP` passes; negative half reports zero hits; positive half's `t.Logf` shows the real path `runAuditWindow -> (*auditwindow.Manager).Open -> (*auditwindow.Manager).flipIfNeeded -> auditwindow.NewManager$4 -> k8s.ReadPolicyAuditMode -> k8s.ExecCiliumDbg` (via the `readFn` seam, non-bare signature) — non-vacuous. `TestMCPAuditReadonlyReachability` (unaffected by this file, but re-verified) still passes at 39.82s-55.33s across runs, consistent with its documented ~45-76s budget.
- **Committed in:** `1c1077d` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug fix, structural/security-critical)
**Impact on plan:** Necessary for AUD-04's actual correctness — without this fix, Property 3's negative-half assertion would have been permanently unsatisfiable for this codebase (a `context.CancelFunc` call exists on essentially every signal-bound code path, including `pkg/session.Manager.Shutdown`, itself reachable from `runMCPServer`), making the tripwire either always-red (blocking merge) or requiring the RESEARCH-anticipated `Edge.Site == nil` filter to be abandoned entirely. No scope creep — the fix is confined to `bfsFromRootGenuine`'s own filtering logic in the one file the plan already scoped for edits.

## Issues Encountered
- The diagnostic test used to confirm the exact RTA edge mechanism (`cmd/cpg/zzz_diag_test.go`) was written, run once, and deleted before any commit — it is not part of the repository history.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `cpg audit-window` and its SEC-01 evolution are complete; AUD-03 and AUD-04 requirements satisfied.
- Full wave regression green: `go test ./cmd/cpg/... ./pkg/auditwindow/... -count=1 -race -timeout 600s` — `ok github.com/SoulKyu/cpg/cmd/cpg 205.807s`, `ok github.com/SoulKyu/cpg/pkg/auditwindow 1.170s`.
- `git diff --exit-code go.mod go.sum` clean (no new dependencies).
- Ready for 23-04 (README/runbook documentation of the new command and its RBAC scope).

---
*Phase: 23-managed-audit-window-sec-01-evolution*
*Completed: 2026-07-22*

## Self-Check: PASSED

- FOUND: cmd/cpg/audit_window.go
- FOUND: cmd/cpg/audit_window_test.go
- FOUND: cmd/cpg/main.go
- FOUND: cmd/cpg/mcp_audit_test.go
- FOUND: .planning/phases/23-managed-audit-window-sec-01-evolution/23-03-SUMMARY.md
- FOUND commit: 6b7157a (feat(23): add cpg audit-window command)
- FOUND commit: 1c1077d (test(23): SEC-01 tripwire proving exec path unreachable from MCP)
