---
phase: 21
slug: cilium-compatibility-matrix-runtime-detection
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-22
---

# Phase 21 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + testify (require/assert) |
| **Config file** | none — go.mod at repo root |
| **Quick run command** | `rtk proxy go test ./pkg/k8s/... ./pkg/session/... ./cmd/cpg/... -count=1 -race` |
| **Full suite command** | `rtk proxy go test ./... -count=1 -race` |
| **Estimated runtime** | ~90 seconds |

Note: sandbox denies `go` via `make`; invoke `go test` directly (via `rtk proxy`), never `make test`.

---

## Sampling Rate

- **After every task commit:** Run the task's `<automated>` command
- **After every plan wave:** Run `rtk proxy go test ./... -count=1 -race`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled at planning) | — | — | COMPAT-01..03 | — | privilege-neutral detection, warn-never-abort | unit | see plan `<automated>` commands | — | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements (go test + testify; fake clientset pattern already used in `pkg/k8s/preflight_test.go` for RBAC-free unit tests).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live rolling-upgrade min-version selection | COMPAT-02 | Requires a live cluster mid-upgrade (observed once during research) | Point cpg at a mid-upgrade cluster; verify min-of-versions verdict + warning names lagging nodes |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
