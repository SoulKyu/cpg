---
phase: 23-managed-audit-window-sec-01-evolution
verified: 2026-07-22T17:44:38Z
status: passed
score: 6/6 must-haves verified
overrides_applied: 0
---

# Phase 23: Managed Audit Window + SEC-01 Evolution — Verification Report

**Phase Goal:** Operators can open a supervised, lifecycle-bound audit window on a namespace that cannot structurally be left open by accident, with cpg's readonly guarantee evolved honestly. Requirements AUD-03, AUD-04. USER-LOCKED surface: CLI-only, zero MCP changes.

**Verified:** 2026-07-22T17:44:38Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `cpg audit-window -n <ns> --ttl` flips per-endpoint audit via pods/exec, watches new endpoints, never touches pre-audited endpoints, UID-keyed revert-only-ours bookkeeping (re-reads current ID at revert) | ✓ VERIFIED | `pkg/k8s/exec.go` (`ExecCiliumDbg`/SPDY pods/exec), `pkg/auditwindow/manager.go` `flipIfNeeded` skips endpoints already `enabled` (never-touch-already-audited), `ours` map keyed on `types.UID`, `Close`/`defaultResolveCurrentID` re-lists and re-resolves current ID by UID before reverting. `TestManager_Open_SkipsAlreadyAuditedEndpoint` and `TestManager_Close_UsesUIDNotReusedEndpointID` directly assert both properties; both pass under `-race`. Compiled binary `cpg audit-window --help` confirms `-n`/`--namespace` (required) and `--ttl` (default `30m0s`) flags exist exactly as documented. |
| 2 | EVERY exit path reverts boundedly: explicit stop, ctx cancel/SIGTERM (fresh non-cancelled revert ctx), TTL expiry, wedged transport (bounded + TimedOut/PossiblyStuck reporting, CLI exits non-zero naming stuck UIDs). No path leaves the cluster in audit silently | ✓ VERIFIED | `Manager.Shutdown()` (`pkg/auditwindow/manager.go:429`) cancels+drains the watcher, then runs `Close` under `context.Background()` (never the caller's cancelled ctx — CR-01) with an independent bounded deadline (CR-02); on timeout it returns `RevertResult{TimedOut:true, PossiblyStuck:[...]}` naming every endpoint the window owned. `runAuditWindow` (`cmd/cpg/audit_window.go:143,170`) `defer`s `wm.Shutdown()` immediately after construction (covers early-return/Open-failure exit) and routes both the signal and TTL branches through the same `wm.Shutdown()` call — never a bare `Close`. On `TimedOut`, `runAuditWindow` returns a non-nil error naming the stuck-endpoint count (`len(result.PossiblyStuck)`) after logging each UID individually via `logger.Warn`, so the process exits non-zero. Directly pinned by `TestManager_Shutdown_RevertsUnderNonCancelledContext` (asserts `ctx.Err()==nil` inside `setFn` after `rootCtx` cancellation — CR-01), `TestManager_Shutdown_RevertsWatcherFlippedEndpoint` (WR-01: a watcher-flipped endpoint is present in `Shutdown`'s `EndpointResults`), `TestManager_Shutdown_WedgedExecDoesNotBlock` (bounded return + `TimedOut`/`PossiblyStuck` naming), `TestAuditWindow_TTLExpiryTriggersRevert` and `TestAuditWindow_SignalPathRevertsViaBoundedShutdown` (assert `shutdownWasCalled==true`, `closeWasCalled==false` on both CLI exit paths). All pass under `-race`. See "CR-01/CR-02/WR-01 Human-Verification Evaluation" below for why this is scored VERIFIED rather than routed to human review. |
| 3 | Daemon-wide audit-mode active → hard refusal; undetermined → warn-and-proceed | ✓ VERIFIED | `pkg/k8s/exec.go` `CheckDaemonAuditMode` collapses RBAC-forbidden to `(false, nil)`; `Manager.Open` (`pkg/auditwindow/manager.go:173`) hard-refuses with a named error when `active==true`, logs a warning and proceeds when `preconditionFn` itself errors. `TestManager_Open_RefusesWhenDaemonAuditActive` and `TestManager_Open_ProceedsWhenPreconditionUndetermined` directly assert both branches (refusal message content + `setFn` never called on refusal; flip proceeds on undetermined). Both pass. |
| 4 | SEC-01: `TestMCPAuditReadonlyReachability` byte-identical to pre-phase (git diff 4a9149a..HEAD -- cmd/cpg/mcp_audit_test.go shows zero deletions); new `TestAuditWindowNotReachableFromMCP` has sound negative half (synthetic-edge filter + withAnonFuncs closure scan + non-vacuity floor) and non-vacuous positive half; zero MCP surface diffs (mcp.go/mcp_tools.go/mcp_bootstrap.go/mcp_query*.go unchanged vs 4a9149a) | ✓ VERIFIED | `git diff --stat 4a9149a..HEAD -- cmd/cpg/mcp_audit_test.go` → `305 insertions(+)`, 0 deletions; `TestMCPAuditReadonlyReachability`'s body (lines 381-495) is unmodified. `git diff 4a9149a..HEAD -- cmd/cpg/mcp.go cmd/cpg/mcp_tools.go cmd/cpg/mcp_bootstrap.go` and `'cmd/cpg/mcp_query*.go'` both empty. `TestAuditWindowNotReachableFromMCP` (line 525) negative half uses `bfsFromRootGenuine` (Edge.Site==nil filter + `isBareFuncValueDispatch` second-sweep filter, with documented soundness rationale) + `withAnonFuncs` lexical-closure compensation scan + a WR-03 non-vacuity floor (`require.Greater(len, 1)` + `require.Contains(..., session.NewManager)`); positive half asserts `remotecommand.NewSPDYExecutor` genuinely reachable from `runAuditWindow` via `require.True(foundExecCaller)`. Ran standalone: `PASS (55.78s)`, consistent with documented budget. |
| 5 | README: MCP readonly claim intact; audit-window documented as sole mutating command + exclusive RBAC. Runbook: real command wired, new-endpoint race honestly documented, hyphenated policy-audit-mode token confined to warning block. Golden tests pin all of it | ✓ VERIFIED | README.md:559 unchanged claim "cpg never mutates your cluster and never writes outside its own session tmpdir"; README.md:86-110 new "Readonly by default" section names `cpg audit-window` as the one mutating command, discloses exclusive RBAC (`pods/exec` create in kube-system, `ciliumendpoints` list/watch) and the Pitfall-4 scoping limitation. `docs/bootstrap-runbook.md`:69-110 drives the real `cpg audit-window -n <namespace> --ttl 30m` command and documents the new-endpoint race window ("documented, not solved") plainly. `grep -n "policy-audit-mode"` across both files shows 0 hits in README.md and exactly 2 hits in the runbook, both inside the leading warning block (lines 3, 10) — `TestRunbookNeverSuggestsDaemonWideAudit`'s pin stays satisfied. `TestReadmeAuditWindowSection` and `TestRunbookAuditWindowStep` both pass standalone. |
| 6 | Zero new go.mod deps; all 14 VALIDATION.md test names exist and pass | ✓ VERIFIED | `git diff --exit-code go.mod go.sum` → exit 0 (clean). All 14 names from 23-VALIDATION.md's per-requirement map (`TestManager_Open_RefusesWhenDaemonAuditActive`, `TestManager_Open_ProceedsWhenPreconditionUndetermined`, `TestManager_Open_SkipsAlreadyAuditedEndpoint`, `TestManager_Close_UsesUIDNotReusedEndpointID`, `TestManager_Close`, `TestManager_Shutdown_OnCtxCancel`, `TestAuditWindow_TTLExpiryTriggersRevert`, `TestManager_Shutdown_WedgedExecDoesNotBlock`, `TestManager_Close_ReportsPerEndpointResult`, `TestManager_Watcher_FlipsNewEndpoint`, `TestManager_Watcher_ReconnectsOnChannelClose`, `TestAuditWindowNotReachableFromMCP`, `TestReadmeAuditWindowSection`, `TestRunbookAuditWindowStep`) confirmed present via `grep -n "^func Test"` and run explicitly with `-run '<all 14>' -race` — all `--- PASS`. Full-suite `go build ./...` clean; `go test ./... -count=1 -race -timeout 900s` → all 13 packages `ok`, zero failures, ~190s for `cmd/cpg` (dominated by the two whole-program SSA/RTA tests). |

**Score:** 6/6 truths verified

### CR-01/CR-02/WR-01 Human-Verification Evaluation

23-REVIEW-FIX.md flagged the CR-01/CR-02/WR-01 fix (commit `4d62126`) as "requires human verification (concurrency/state semantics)" because it touches exit-path context/ordering logic. Re-examined against the actual regression tests added for this fix:

- **CR-01** (revert must run under a fresh, non-cancelled context): `TestManager_Shutdown_RevertsUnderNonCancelledContext` directly captures `ctx.Err()` inside `setFn` during the revert call after `rootCtx` has been cancelled and asserts it is `nil` — this is a direct assertion on the exact property in question, not an indirect proxy.
- **WR-01** (a watcher-flipped endpoint must not escape the sweep): `TestManager_Shutdown_RevertsWatcherFlippedEndpoint` pushes a watch event, waits for the watcher to record the UID, then calls `Shutdown` and asserts the UID is present in `EndpointResults` — directly proves the cancel-before-snapshot ordering closes the leak.
- **CR-02** (wedged transport cannot block process exit, and the timeout path names what it could not confirm): `TestManager_Shutdown_WedgedExecDoesNotBlock` blocks `setFn` indefinitely and asserts `Shutdown` still returns within a bounded deadline with `TimedOut=true` and the correct `PossiblyStuck` UID.
- **CLI routing** (both exit paths must go through the bounded `Shutdown`, never a bare `Close`): `TestAuditWindow_TTLExpiryTriggersRevert` and `TestAuditWindow_SignalPathRevertsViaBoundedShutdown` assert `shutdownWasCalled==true` and `closeWasCalled==false` on both paths.

All four tests are deterministic (no live cluster, no timing-flake-prone sleeps beyond bounded `require.Eventually`/timeout guards), run under `-race`, and pass. Each test asserts the exact concurrency/ordering property the review flagged, not a weaker proxy. On this evidence, these three findings are scored **VERIFIED**, not routed to `human_verification` — the original review's caution was reasonable at review time (before the regression tests existed to pin the semantics), but the gap it identified is now closed by direct, automated assertions.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `pkg/k8s/exec.go` | SPDY pods/exec, node→agent-pod mapping, read-before-flip primitives, daemon precondition | ✓ VERIFIED | 207 lines, substantive; `ExecCiliumDbg`, `FindAgentPodForNode`, `CiliumBinaryName`, `ReadPolicyAuditMode`, `SetPolicyAuditMode`, `CheckDaemonAuditMode` all present, wired, exercised by 7 tests in `exec_test.go` |
| `pkg/auditwindow/manager.go` | Open/Close/Shutdown state machine | ✓ VERIFIED | 485 lines; matches SESS-05 shape, one `sync.Once`, zero `time.AfterFunc`; wired into `cmd/cpg/audit_window.go` via `auditWindowNewManager` seam |
| `cmd/cpg/audit_window.go` | `cpg audit-window` cobra command | ✓ VERIFIED | 187 lines; registered in `main.go:62`; compiled binary `--help` output matches documented flags exactly |
| `cmd/cpg/mcp_audit_test.go` | SEC-01 tripwire additions, existing audit test untouched | ✓ VERIFIED | 650 lines; `TestMCPAuditReadonlyReachability` byte-identical (0 deletions in diff); `TestAuditWindowNotReachableFromMCP` new, both pass |
| `README.md` / `docs/bootstrap-runbook.md` | Readonly claim + mutating-command disclosure + honest race documentation | ✓ VERIFIED | Golden-pinned by `cmd/cpg/audit_docs_test.go`; both pins pass; hand-verified via grep that content matches claims |
| `cmd/cpg/audit_docs_test.go` | Golden pins for README/runbook | ✓ VERIFIED | 72 lines; `TestReadmeAuditWindowSection`, `TestRunbookAuditWindowStep` both pass |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `cmd/cpg/main.go` | `newAuditWindowCmd()` | `rootCmd.AddCommand` | WIRED | `main.go:62` |
| `cmd/cpg/audit_window.go` `runAuditWindow` | `pkg/auditwindow.Manager` | `auditWindowNewManager` seam → `auditwindow.NewManager` | WIRED | Production seam loads kubeconfig, constructs real `Manager`; test seam substitutes stub, exercised by 5 CLI tests |
| `pkg/auditwindow.Manager` | `pkg/k8s` exec primitives | `readFn`/`setFn`/`preconditionFn` seams bound in `NewManager` to `k8s.ReadPolicyAuditMode`/`SetPolicyAuditMode`/`CheckDaemonAuditMode` | WIRED | `manager.go:134-160`; positive-half SEC-01 test traces the real call chain `runAuditWindow -> Manager.Open -> flipIfNeeded -> k8s.ReadPolicyAuditMode -> k8s.ExecCiliumDbg` |
| `runMCPServer` | `remotecommand.NewSPDYExecutor` | (must be absent) | NOT_WIRED (intentional) | Confirmed by `TestAuditWindowNotReachableFromMCP` negative half — zero genuine call-path hits |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| `cpg audit-window` flags match documentation | `go build -o /tmp/cpg-verify-bin ./cmd/cpg && /tmp/cpg-verify-bin audit-window --help` | `-n/--namespace` required, `--ttl` default `30m0s`, help text matches README/runbook prose verbatim | ✓ PASS |
| `cpg audit-window` registered as subcommand | `/tmp/cpg-verify-bin --help \| grep audit` | `audit-window  Open a managed, TTL-bounded per-endpoint policy-audit-mode window` | ✓ PASS |
| Zero new dependencies | `git diff --exit-code go.mod go.sum` | exit 0 | ✓ PASS |
| SEC-01 negative/positive halves independently green | `go test ./cmd/cpg/... -run TestAuditWindowNotReachableFromMCP -race -v` | `--- PASS (55.78s)` | ✓ PASS |

### Probe Execution

No `scripts/*/tests/probe-*.sh` convention or PLAN-declared probes found for this phase — SKIPPED (no probe-based verification for this phase; verification uses the Go test suite directly, per 23-VALIDATION.md's own test infrastructure contract).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| AUD-03 | 23-01, 23-02, 23-03, 23-04 | Managed audit window with per-endpoint flip, revert-only-ours, watcher, precondition, docs | ✓ SATISFIED | Truths 1, 2, 3, 5 above |
| AUD-04 | 23-03 | SEC-01 structural proof extended to cover the audit-window mutation | ✓ SATISFIED | Truth 4 above |

**Note (tracking, not a code gap):** `.planning/REQUIREMENTS.md` still lists AUD-04 as `Pending` (line 70), and `.planning/ROADMAP.md`'s Phase 23 plan checklist still shows `23-03-PLAN.md`/`23-04-PLAN.md` as unchecked (`[ ]`) despite both being complete (commits `6b7157a`..`bbe18f4`, SUMMARY files present for all 4 plans). 23-04-SUMMARY.md documents this explicitly: "No plan-metadata commit requested for this plan ... no STATE.md/ROADMAP.md/REQUIREMENTS.md update." This is expected pre-phase-completion state (these files are updated by the phase-completion step after verification passes), not a functional gap — flagged here for the phase-completion step to close, not as a VERIFICATION blocker.

### Anti-Patterns Found

None. Scanned all phase-created/modified files (`pkg/k8s/exec.go`, `pkg/k8s/exec_test.go`, `pkg/auditwindow/manager.go`, `pkg/auditwindow/manager_test.go`, `cmd/cpg/audit_window.go`, `cmd/cpg/audit_window_test.go`, `cmd/cpg/audit_docs_test.go`, `cmd/cpg/main.go`, `cmd/cpg/mcp_audit_test.go`, `README.md`, `docs/bootstrap-runbook.md`) for `TBD|FIXME|XXX|TODO|HACK|PLACEHOLDER` (case-insensitive) — zero hits (the one README.md grep hit, "Replace the two placeholders", is pre-existing unrelated YAML-example prose, not phase-23 content). `go vet ./cmd/cpg/... ./pkg/auditwindow/... ./pkg/k8s/...` clean.

### Human Verification Required

None. All success criteria are verified against the actual codebase via build, `go vet`, the full `-race` test suite, standalone re-runs of all 14 VALIDATION.md-named tests, a compiled-binary behavioral spot-check of the CLI surface, and direct diff inspection of the SEC-01 byte-identity and zero-MCP-surface-diff claims. The one item the code review flagged for human review (CR-01/CR-02/WR-01 concurrency semantics) is downgraded to VERIFIED — see the dedicated evaluation section above — because deterministic, race-enabled regression tests directly assert the exact properties in question.

### Gaps Summary

No gaps. All 6 roadmap success criteria are observably true in the codebase: the CLI command exists, compiles, and behaves as documented; the Manager correctly implements revert-only-ours UID-keyed bookkeeping with fresh-ID re-resolution at revert; every exit path (explicit stop, signal, TTL, wedged transport) reverts through one bounded, well-tested path with per-endpoint and stuck-endpoint reporting; the daemon-wide precondition hard-refuses/warns correctly; SEC-01's existing audit is byte-identical and the new tripwire is sound on both halves; README/runbook honestly document the new mutating command and its RBAC/race caveats with golden-pin protection; zero new dependencies were introduced. The only non-code item worth flagging is the pending REQUIREMENTS.md/ROADMAP.md tracking-sync noted above, which belongs to the phase-completion step, not to this verification.

---

_Verified: 2026-07-22T17:44:38Z_
_Verifier: Claude (gsd-verifier)_
