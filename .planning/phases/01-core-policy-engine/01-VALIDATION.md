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
| 01-01-01 | 01 | 0 | — | setup | `go build ./...` | ❌ W0 | ⬜ pending |
| 01-01-02 | 01 | 1 | PGEN-04 | unit | `go test ./pkg/labels/ -run TestSelectLabels -count=1` | ❌ W0 | ⬜ pending |
| 01-01-03 | 01 | 1 | PGEN-01 | unit | `go test ./pkg/policy/ -run TestBuildIngressPolicy -count=1` | ❌ W0 | ⬜ pending |
| 01-01-04 | 01 | 1 | PGEN-02 | unit | `go test ./pkg/policy/ -run TestBuildEgressPolicy -count=1` | ❌ W0 | ⬜ pending |
| 01-01-05 | 01 | 1 | PGEN-05 | unit | `go test ./pkg/policy/ -run TestPortProtocol -count=1` | ❌ W0 | ⬜ pending |
| 01-01-06 | 01 | 1 | PGEN-06 | unit | `go test ./pkg/policy/ -run TestYAMLOutput -count=1` | ❌ W0 | ⬜ pending |
| 01-02-01 | 02 | 2 | OUTP-01 | unit | `go test ./pkg/output/ -run TestDirectoryStructure -count=1` | ❌ W0 | ⬜ pending |
| 01-02-02 | 02 | 2 | OUTP-03 | integration | `go test ./cmd/... -run TestLogLevel -count=1` | ❌ W0 | ⬜ pending |
| 01-02-03 | 02 | 2 | CONN-01 | integration | `go test ./pkg/hubble/ -run TestConnect -count=1` | ❌ W0 | ⬜ pending |
| 01-02-04 | 02 | 2 | CONN-05 | unit | `go test ./pkg/hubble/ -run TestLostEvents -count=1` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `go.mod` / `go.sum` — project initialization with Go 1.25, Cilium v1.19.1 dependency
- [ ] `go get github.com/stretchr/testify` — test assertion library
- [ ] `pkg/labels/selector_test.go` — stubs for PGEN-04 (label hierarchy + denylist)
- [ ] `pkg/policy/builder_test.go` — stubs for PGEN-01, PGEN-02, PGEN-05, PGEN-06
- [ ] `pkg/policy/merge_test.go` — stubs for merge-on-write behavior
- [ ] `pkg/output/writer_test.go` — stubs for OUTP-01
- [ ] `pkg/hubble/client_test.go` — stubs for CONN-01 (mock gRPC server)
- [ ] Test fixtures: sample `flow.Flow` proto structs for ingress/egress/various label scenarios

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Ctrl+C session summary | OUTP-03 | Signal handling requires process interaction | Run tool, press Ctrl+C, verify summary output |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 15s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
