---
phase: 18
slug: query-tools
status: ready
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-21
---

# Phase 18 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + testify, all packages race-enabled |
| **Config file** | none — standard `go test` |
| **Quick run command** | `go test ./pkg/explain/... ./pkg/output/... ./pkg/hubble/... ./cmd/cpg/... -count=1 -race` |
| **Full suite command** | `go test ./... -count=1 -race` |
| **Estimated runtime** | ~60 seconds |

> Sandbox note: `make test` is denied under the sandbox — run the `go test … -race` commands directly (via `rtk proxy go test …` if the hook is active).

---

## Sampling Rate

- **After every task commit:** Run the quick run command (packages touched by the task)
- **After every plan wave:** Run `go test ./... -count=1 -race`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 90 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| T1 pkg/explain move | 18-01 | 1 | QRY-03 | T-18-01-R | Byte-identical renderer preserved | unit | `go test ./pkg/explain/... -race -count=1` | ❌ new | ⬜ pending |
| T2 thin explain.go | 18-01 | 1 | QRY-03 | — | not-found idiom preserved for reuse | unit | `go test ./cmd/cpg/... ./pkg/explain/... -race -count=1` | ✅/❌ | ⬜ pending |
| T1 ReadPolicyFile | 18-02 | 1 | QRY-02 | T-18-02-D | wrapped fs.ErrNotExist, no panic | unit | `go test ./pkg/output/... -race -count=1` | ❌ new | ⬜ pending |
| T2 ReadClusterHealth + type exports | 18-02 | 1 | QRY-04 | T-18-02-T | schema-version gate, passthrough | unit | `go test ./pkg/hubble/... -race -count=1` | ❌ new | ⬜ pending |
| T3 finalize-on-error | 18-02 | 1 | QRY-04 | T-18-02-D | health written despite pipeline error | unit | `go test ./pkg/hubble/... -run TestRunPipeline_FinalizesHealthOnStreamError -race` | ❌ new | ⬜ pending |
| T1 scaffold + list_policies/get_policy | 18-03 | 2 | QRY-02 | T-18-03-01/02/E | ValidatePolicyRef guard, SESS-06 resolve, readonly | in-memory MCP | `go test ./cmd/cpg/... -run 'TestMCPQuery(ListPolicies\|GetPolicy)' -race` | ❌ new | ⬜ pending |
| T2 get_cluster_health 3-way | 18-03 | 2 | QRY-04 | T-18-03-03 | absent≠error framing | in-memory MCP | `go test ./cmd/cpg/... -run TestMCPQueryGetClusterHealth -race` | ❌ new | ⬜ pending |
| T3 non-paginated tool tests | 18-03 | 2 | QRY-05 | T-18-03-01/02 | annotations truthful, error texts | in-memory MCP | `go test ./cmd/cpg/... -run TestMCPQuery -race` | ❌ new | ⬜ pending |
| T1 pagination + enum infra | 18-04 | 3 | QRY-03, QRY-05 | T-18-04-02 | cursor fails closed, limit clamp | unit | `go test ./cmd/cpg/... -run 'TestMustQuerySchema\|TestCursor\|TestPaginate' -race` | ❌ new | ⬜ pending |
| T2 get_evidence | 18-04 | 3 | QRY-03 | T-18-04-01/03 | path guard, pagination cap, IsNotExist | in-memory MCP | `go test ./cmd/cpg/... -run TestMCPQueryGetEvidence -race` | ❌ new | ⬜ pending |
| T1 list_dropped_flows | 18-05 | 4 | QRY-01 | T-18-05-01/04 | path guard, direction-samples-only, cap | in-memory MCP | `go test ./cmd/cpg/... -run TestMCPQueryListDroppedFlows -race` | ❌ new | ⬜ pending |
| T2 8-tool integration | 18-05 | 4 | QRY-05 | T-18-05-02/03 | truthful annotations, enum schemas, cursor | in-memory MCP | `go test ./... -race -count=1` | ❌ new | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [x] Existing infrastructure covers all phase requirements. The `-race` test framework, testify, and the in-memory MCP transport harness (`startInMemoryMCPSession` + `requiredFields`/`decodeStructured` in `cmd/cpg/mcp_harness_test.go` / `mcp_session_test.go`) are all present and working — no framework install needed.
- The one net-new pipeline-level test (`TestRunPipeline_FinalizesHealthOnStreamError`, 18-02 Task 3) is a regular additive test inside its plan, not a scaffolding gap: it closes the Pitfall-1 coverage hole so the QRY-04 3-way branch (18-03 Task 2) can rely on "absent file = zero drops, not crash".
- The D-07 server-bypass address (`"server":"127.0.0.1:1"`) lets every query-tool test obtain a real, empty session tmpdir and seed fixtures directly — no live cluster/kubeconfig required.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| — | — | — | — |

*All phase behaviors have automated verification via the in-memory MCP transport under `-race`.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (none — framework + harness pre-exist)
- [x] No watch-mode flags (all runs `-count=1`)
- [x] Feedback latency < 90s (~60s full suite)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** ready
