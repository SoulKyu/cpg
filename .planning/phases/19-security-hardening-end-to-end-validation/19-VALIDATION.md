---
phase: 19
slug: security-hardening-end-to-end-validation
status: approved
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-21
---

# Phase 19 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) — 607 existing tests, all `-race` |
| **Config file** | none — go.mod toolchain go1.25.12 |
| **Quick run command** | `rtk proxy go test ./cmd/cpg/ -count=1 -race` |
| **Full suite command** | `rtk proxy go test ./... -count=1 -race` |
| **Estimated runtime** | ~90 seconds full suite (e2e adds one `go build -race` ≈ +10s first run) |

---

## Sampling Rate

- **After every task commit:** Run `rtk proxy go test ./cmd/cpg/ -count=1 -race`
- **After every plan wave:** Run `rtk proxy go test ./... -count=1 -race`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 19-01-01 | 01 | 1 | SEC-01 | T-19-01 (audit unsoundness) | No K8s write verb / no fs write outside tmpdir reachable from `runMCPServer` | static-audit test | `rtk proxy go test ./cmd/cpg/ -run TestMCPAuditReadonlyReachability -count=1` | ✅ | ⬜ pending |
| 19-01-02 | 01 | 1 | SEC-01 | T-19-01 | x/tools direct test dep, audit green under `-race` | build+test | `rtk proxy go build ./... && rtk proxy go test ./cmd/cpg/ -run TestMCPAuditReadonlyReachability -race -count=1` | ✅ | ⬜ pending |
| 19-02-01 | 02 | 1 | SRV-04 | T-19-02 (subprocess/pipe DoS) | e2e infra compiles; fake relay + `-race` binary harness | build gate | `rtk proxy go test ./cmd/cpg/ -run '^$' -count=1` | ✅ | ⬜ pending |
| 19-02-02 | 02 | 1 | SRV-01, SRV-04 | T-19-02 (wire integrity) | Graceful lifecycle + 8-tool handshake + stdout byte-purity | e2e subprocess | `rtk proxy go test ./cmd/cpg/ -run TestMCPE2EGracefulLifecycle -race -count=1` | ✅ | ⬜ pending |
| 19-03-01 | 03 | 1 | SEC-03 | T-19-03 (doc misconfiguration) | README documents env block, secrets posture, exec-credential caveat | grep assertion | `rg -q '## MCP Server' README.md && rg -q 'KUBECONFIG' README.md` | ✅ | ⬜ pending |
| 19-04-01 | 04 | 2 | SRV-04 | T-19-02 | Ungraceful disconnect → bounded exit + tmpdir removed + stream cancelled | e2e subprocess | `rtk proxy go test ./cmd/cpg/ -run TestMCPE2EUngracefulDisconnect -race -count=5` | ✅ | ⬜ pending |

*Note: the audit test runs ~55-76s under `-race` (LoadAllSyntax over the Cilium dep graph) — budgeted within the 120s latency cap, not a regression (RESEARCH Pitfall 5).*

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — `go test -race` is already the suite-wide standard; the audit and e2e tests this phase ADDS are themselves the validation artifacts (SEC-01/SRV-04 deliverables are tests).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| README MCP section reads correctly for a first-time SRE | SEC-03 | Prose quality is human judgment | Read `README.md` §MCP Server; check env block, secrets posture, exec-plugin caveat present and accurate |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (6/6 tasks — plan-checker Dimension 8 pass)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (Wave 1: 5/5, Wave 2: 1/1)
- [x] Wave 0 covers all MISSING references (none used)
- [x] No watch-mode flags
- [x] Feedback latency < 120s (audit test ~55-76s budgeted, see note)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-07-21 (plan-checker Dimension 8: PASS)
