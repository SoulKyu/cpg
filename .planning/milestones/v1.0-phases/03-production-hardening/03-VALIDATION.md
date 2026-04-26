---
phase: 3
slug: production-hardening
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-03-08
---

# Phase 3 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | none — standard Go testing |
| **Quick run command** | `go test ./pkg/... ./cmd/...` |
| **Full suite command** | `go test -race -count=1 ./...` |
| **Estimated runtime** | ~5 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/... ./cmd/...`
- **After every plan wave:** Run `go test -race -count=1 ./...`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 10 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 03-01-01 | 01 | 1 | PGEN-03, DEDP-01 | unit (TDD) | `go test ./pkg/policy/... ./pkg/output/... -count=1 -v` | ❌ W0 (dedup.go, dedup_test.go new; builder_test.go extend) | ⬜ pending |
| 03-01-02 | 01 | 1 | DEDP-01 | unit (TDD) | `go test ./pkg/output/... -count=1 -v` | ❌ W0 (writer_test.go new) | ⬜ pending |
| 03-02-01 | 02 | 2 | CONN-02, DEDP-02 | unit (TDD) | `go test ./pkg/k8s/... -count=1 -v` | ❌ W0 (all pkg/k8s/ files new) | ⬜ pending |
| 03-02-02 | 02 | 2 | DEDP-03, CONN-02, DEDP-02 | unit (TDD) + build | `go build ./cmd/cpg/ && go test -race -count=1 ./pkg/hubble/... ./cmd/...` | ❌ W0 (pipeline_test.go cross-flush tests new) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/policy/dedup.go` + `pkg/policy/dedup_test.go` — new: semantic policy comparison for DEDP-01
- [ ] `pkg/policy/builder_test.go` — extend with CIDR world identity tests for PGEN-03
- [ ] `pkg/policy/testdata/ingress_flow.go` — extend with world identity flow helpers
- [ ] `pkg/output/writer_test.go` — new: file-based dedup tests for DEDP-01
- [ ] `pkg/k8s/portforward.go` + `pkg/k8s/portforward_test.go` — new: port-forward for CONN-02
- [ ] `pkg/k8s/client.go` + `pkg/k8s/client_test.go` — new: kubeconfig loading
- [ ] `pkg/k8s/cluster_dedup.go` + `pkg/k8s/cluster_dedup_test.go` — new: cluster dedup for DEDP-02
- [ ] `pkg/hubble/pipeline_test.go` — extend with cross-flush dedup tests for DEDP-03

*All Wave 0 files are created as part of TDD tasks (tests written first).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Auto port-forward to live cluster | CONN-02 | Requires real Kubernetes cluster | Run `cpg generate` without `--server`, verify connection to hubble-relay |
| Cluster dedup against live CiliumNetworkPolicies | DEDP-02 | Requires Cilium CRDs in cluster | Apply a CNP, run generate with `--cluster-dedup`, verify skip |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 10s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
