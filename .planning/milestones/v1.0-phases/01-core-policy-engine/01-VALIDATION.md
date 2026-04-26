---
phase: 1
slug: core-policy-engine
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-03-08
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing (stdlib) + `testify/assert` for readability |
| **Config file** | None — Go test infrastructure is zero-config |
| **Quick run command** | `go test ./pkg/... -short -count=1` |
| **Full suite command** | `go test ./... -count=1 -race` |
| **Estimated runtime** | ~10 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/... -short -count=1`
- **After every plan wave:** Run `go test ./... -count=1 -race`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 15 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 01-01-T1 | 01 | 1 | — | setup | `go build ./... && make build && test -f bin/cpg` | ❌ W0 | ⬜ pending |
| 01-01-T2 | 01 | 1 | PGEN-04 | unit (TDD) | `go test ./pkg/labels/ -v -count=1` | ❌ W0 | ⬜ pending |
| 01-02-T1 | 02 | 2 | PGEN-01, PGEN-02, PGEN-05, PGEN-06 | unit (TDD) | `go test ./pkg/policy/ -run TestBuildPolicy -v -count=1` | ❌ W0 | ⬜ pending |
| 01-02-T2 | 02 | 2 | PGEN-06 | unit (TDD) | `go test ./pkg/policy/ -run TestMergePolicy -v -count=1` | ❌ W0 | ⬜ pending |
| 01-03-T1 | 03 | 3 | OUTP-01 | unit (TDD) | `go test ./pkg/output/ -v -count=1` | ❌ W0 | ⬜ pending |
| 01-03-T2 | 03 | 3 | OUTP-03 | integration | `make build && bin/cpg generate --help` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

*No Wave 0 needed — all tasks have automated verify commands. Test files are created as part of TDD tasks in each plan.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
*All phase behaviors have automated verification.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 15s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
