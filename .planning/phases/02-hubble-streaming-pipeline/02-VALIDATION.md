---
phase: 02
slug: hubble-streaming-pipeline
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-03-08
---

# Phase 02 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing (stdlib) + `testify/assert` |
| **Config file** | None — Go test infrastructure is zero-config |
| **Quick run command** | `go test ./pkg/hubble/... -short -count=1` |
| **Full suite command** | `go test ./... -count=1 -race` |
| **Estimated runtime** | ~5 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/hubble/... -short -count=1`
- **After every plan wave:** Run `go test ./... -count=1 -race`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 10 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 02-01-01 | 01 | 1 | CONN-01 | integration | `go test ./pkg/hubble/ -run TestClient -count=1` | ❌ W0 | ⬜ pending |
| 02-01-02 | 01 | 1 | CONN-04 | unit | `go test ./pkg/hubble/ -run TestBuildFilters -count=1` | ❌ W0 | ⬜ pending |
| 02-01-03 | 01 | 1 | CONN-05 | unit | `go test ./pkg/hubble/ -run TestLostEvents -count=1` | ❌ W0 | ⬜ pending |
| 02-02-01 | 02 | 2 | OUTP-02 | integration | `go test ./pkg/hubble/ -run TestPipeline -count=1` | ❌ W0 | ⬜ pending |
| 02-02-02 | 02 | 2 | CONN-03 | unit | `go test ./cmd/... -run TestServerFlag -count=1` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/hubble/client_test.go` — gRPC client connection tests (CONN-01) with mock server
- [ ] `pkg/hubble/client.go` — gRPC client wrapper (new file)
- [ ] `pkg/hubble/aggregator_test.go` — aggregation + flush behavior tests
- [ ] `pkg/hubble/aggregator.go` — temporal aggregation (new file)
- [ ] `pkg/hubble/pipeline_test.go` — end-to-end pipeline tests (OUTP-02) with mocks
- [ ] `pkg/hubble/pipeline.go` — errgroup orchestration (new file)
- [ ] Test helper: mock `observer.ObserverClient` using interface-based mock

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live Hubble Relay connection | CONN-01 | Requires running Kubernetes cluster with Cilium | Deploy to test cluster, run `cpg generate --server <relay-addr>`, verify policy files created |
| Ring buffer overflow warning | CONN-05 | LostEvents only triggered under real load | Generate high flow volume, verify warning logged |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 10s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
