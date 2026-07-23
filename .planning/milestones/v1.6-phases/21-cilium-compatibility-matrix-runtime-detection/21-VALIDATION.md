---
phase: 21
slug: cilium-compatibility-matrix-runtime-detection
status: planned
nyquist_compliant: true
wave_0_complete: true
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
| 21-01-T1 | 21-01 | 1 | COMPAT-02 | T-21-01-01/02/03 | privilege-neutral parse, no-panic, no ServerStatus/exec | build | `rtk proxy go build ./pkg/k8s/ && rtk proxy go vet ./pkg/k8s/` | new (`pkg/k8s/version.go`) | ⬜ pending |
| 21-01-T2 | 21-01 | 1 | COMPAT-02 | T-21-01-01/03 | min-reduction, RBAC-forbidden warn, bounded unreachable dial | unit | `rtk proxy go test ./pkg/k8s/... -run "TestParseImageTag\|TestDetectCiliumVersion" -count=1 -race` | new (`pkg/k8s/version_test.go`) | ⬜ pending |
| 21-02-T1 | 21-02 | 1 | COMPAT-01, COMPAT-03 | T-21-02-01/02 | PR-verified floors, no proxy-visibility ≤1.19 bug | doc | `rg -n "## Supported Cilium versions" README.md && ! rg -n "proxy-visibility" README.md \| rg "1\.19"` | modified (`README.md`) | ⬜ pending |
| 21-02-T2 | 21-02 | 1 | COMPAT-01, COMPAT-03 | T-21-02-02 | golden consistency tripwire | unit | `rtk proxy go test ./cmd/cpg/... -run TestReadmeCompatSection -count=1` | new (`cmd/cpg/readme_compat_test.go`) | ⬜ pending |
| 21-03-T1 | 21-03 | 2 | COMPAT-02 | T-21-03-01/03 | warn-never-abort, advisory | unit | `rtk proxy go test ./cmd/cpg/... -run "TestMaybeRunVersionPreflight\|TestMaybeRunL7Preflight" -count=1 -race` | modified (`generate.go`, `generate_test.go`) | ⬜ pending |
| 21-03-T2 | 21-03 | 2 | COMPAT-02 | T-21-03-02 | replay stays offline (structural absence) | unit | `rtk proxy go test ./cmd/cpg/... -run TestReplay_NoVersionDetection -count=1` | modified (`replay_test.go`) | ⬜ pending |
| 21-04-T1 | 21-04 | 2 | COMPAT-02 | T-21-04-01/02/03 | additive fields + seam, zero new write reachability | build | `rtk proxy go build ./... && rtk proxy go vet ./pkg/session/ ./cmd/cpg/` | modified (`session.go`, `manager.go`) | ⬜ pending |
| 21-04-T2 | 21-04 | 2 | COMPAT-02 | T-21-04-03/04 | seam prevents bypass dial, wire round-trip, SEC-01 green | unit | `rtk proxy go test ./pkg/session/... -run "TestManager_Start\|TestManager_Status" -count=1 -race && rtk proxy go test ./cmd/cpg/... -run "TestMCPSession\|TestMCPAuditReadonlyReachability" -count=1 -race` | modified (`manager_test.go`, `mcp_session_test.go`) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

No blocking Wave 0 infrastructure gap. The framework (go test + testify), the fake clientset harness (`k8s.io/client-go/kubernetes/fake`), the observed-logger helpers (`pkg/k8s/preflight_test.go`), the `withFakeL7ClientFactory` seam (`cmd/cpg/generate_test.go`), and the in-memory MCP transport + `decodeStructured` (`cmd/cpg/mcp_session_test.go`) all already exist and are reused. The new test files (`pkg/k8s/version_test.go`, `cmd/cpg/readme_compat_test.go`) are authored inside their plans' own tasks (Task 2 alongside the code), not as a separate Wave 0 prerequisite.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live rolling-upgrade min-version selection | COMPAT-02 | Requires a live cluster mid-upgrade (observed once during research) | Point cpg at a mid-upgrade cluster; verify min-of-versions verdict + warning names lagging nodes. Unit coverage (21-01-T2 mixed-version case) proves the reduction logic without a cluster. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (none — existing infra reused)
- [x] No watch-mode flags
- [x] Feedback latency < 120s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planned (2026-07-22)
