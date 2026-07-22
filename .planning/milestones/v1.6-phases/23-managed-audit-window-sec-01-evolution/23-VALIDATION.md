---
phase: 23
slug: managed-audit-window-sec-01-evolution
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-22
---

# Phase 23 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Derived from 23-RESEARCH.md `## Validation Architecture` (see it for the full per-requirement test map — 15 rows, AUD-03/AUD-04/criterion-5).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + testify (require/assert) |
| **Config file** | none — go.mod at repo root |
| **Quick run command** | `rtk proxy go test ./pkg/auditwindow/... ./pkg/k8s/... -count=1` |
| **Full suite command** | `rtk proxy go test ./... -count=1 -race` |
| **Estimated runtime** | quick ~10s; full ~4 min (two SSA/RTA structural tests at ~45-76s each under `-race`) |

Note: sandbox denies `go` via `make`; invoke `go test` directly (via `rtk proxy`), never `make test`.

---

## Sampling Rate

- **After every task commit:** the task's specific unit test(s)
- **After every plan wave:** `rtk proxy go test ./pkg/auditwindow/... ./pkg/k8s/... ./cmd/cpg/... -count=1 -race`
- **Before `/gsd-verify-work`:** full suite green, including the byte-identical-pass `TestMCPAuditReadonlyReachability` AND the new `TestAuditWindowNotReachableFromMCP`
- **Max feedback latency:** 300 seconds (SSA tests dominate)

---

## Per-Requirement Verification Map (summary — full 15-row map in 23-RESEARCH.md)

| Req | Behavior cluster | Tests | File Exists? |
|-----|------------------|-------|--------------|
| AUD-03 | Precondition (daemon-audit refuse / undetermined proceed) | `TestManager_Open_RefusesWhenDaemonAuditActive`, `TestManager_Open_ProceedsWhenPreconditionUndetermined` | ❌ Wave 0 |
| AUD-03 | Revert-only-ours + UID keying (skip pre-audited, ID-reuse safe) | `TestManager_Open_SkipsAlreadyAuditedEndpoint`, `TestManager_Close_UsesUIDNotReusedEndpointID` | ❌ Wave 0 |
| AUD-03 | Every exit path reverts (Close, ctx-cancel/SIGTERM, TTL, wedged transport) + per-endpoint reporting | `TestManager_Close`, `TestManager_Shutdown_OnCtxCancel`, `TestAuditWindow_TTLExpiryTriggersRevert`, `TestManager_Shutdown_WedgedExecDoesNotBlock`, `TestManager_Close_ReportsPerEndpointResult` | ❌ Wave 0 |
| AUD-03 | Watcher (flips new endpoints, bounded reconnect) | `TestManager_Watcher_FlipsNewEndpoint`, `TestManager_Watcher_ReconnectsOnChannelClose` | ❌ Wave 0 |
| AUD-04 | SEC-01 tripwire: exec executor NOT genuinely reachable from `runMCPServer` (synthetic reflect edges filtered via `Edge.Site == nil`), IS reachable from `runAuditWindow` (non-vacuous) | `TestAuditWindowNotReachableFromMCP` | ❌ Wave 0 |
| crit. 5 | README RBAC + honest wording; runbook real command + race documented | `TestReadmeAuditWindowSection`, `TestRunbookAuditWindowStep` | ❌ Wave 0 |

Existing guards that must stay green unmodified: `TestMCPAuditReadonlyReachability`, `TestRunbookNeverSuggestsDaemonWideAudit` (hyphenated `policy-audit-mode` token confined to the warning block — wording of the new precondition documentation must respect this pin).

---

## Wave 0 Gaps (created by the plans)

- [ ] `pkg/auditwindow/manager_test.go` — new package, fake watch.Interface + wedged exec stubs (mirrors `pkg/session/manager_test.go` patterns)
- [ ] `pkg/k8s/exec_test.go` — fake-clientset node→agent-pod mapping + injectable `execFn` seam (no fake SPDY server; seam approach per `detectVersionFn` precedent)
- [ ] `cmd/cpg/audit_window.go` + tests; SEC-01 tripwire additions live in `mcp_audit_test.go` (reuse `bfsResult`/`callPathFrom` helpers, add `bfsFromRootGenuine`)
- [ ] README/runbook golden pins (`TestReadmeAuditWindowSection`, `TestRunbookAuditWindowStep`)
