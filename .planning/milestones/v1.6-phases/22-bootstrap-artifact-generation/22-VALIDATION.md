---
phase: 22
slug: bootstrap-artifact-generation
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-22
---

# Phase 22 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Derived from 22-RESEARCH.md `## Validation Architecture`.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + testify (require/assert) |
| **Config file** | none — go.mod at repo root |
| **Quick run command** | `rtk proxy go test ./pkg/policy/... ./cmd/cpg/... -run Bootstrap -count=1` |
| **Full suite command** | `rtk proxy go test ./... -count=1 -race` |
| **Estimated runtime** | quick ~10s; full ~2 min (SEC-01 SSA audit alone is ~45-76s under `-race`) |

Note: sandbox denies `go` via `make`; invoke `go test` directly (via `rtk proxy`), never `make test`.

---

## Sampling Rate

- **After every task commit:** Run the task's `<verify>` command
- **After every plan wave:** Run `rtk proxy go test ./... -count=1 -race`
- **Before `/gsd-verify-work`:** Full suite green, including `TestMCPAuditReadonlyReachability` and `TestReadmeCompatSection`
- **Max feedback latency:** 120 seconds

---

## Per-Requirement Verification Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|--------------|
| AUD-02 c1 | `BuildBootstrapPolicy` output carries `enableDefaultDeny` AND one-element empty-rule `ingress`/`egress`; `Spec.Sanitize()` returns nil (the #35558 regression guard — assert on Sanitize(), not just substring match) | unit | `rtk proxy go test ./pkg/policy/... -run TestBuildBootstrapPolicy -v` | ❌ Wave 0 (22-01) |
| AUD-02 c2 (determined below floor) | `cpg bootstrap -n ns` with injected `CompatInfo{ClusterVersion:"1.15.0",...}` exits nonzero naming detected version + 1.16 floor | unit/CLI | `rtk proxy go test ./cmd/cpg/... -run TestBootstrapVersionGate -v` | ❌ Wave 0 (22-02) |
| AUD-02 c2 (undetermined) | Same command with `CompatInfo{Source:"undetermined"}` proceeds, warns on stderr, still emits artifact | unit/CLI | `rtk proxy go test ./cmd/cpg/... -run TestBootstrapUndeterminedVersion -v` | ❌ Wave 0 (22-02) |
| AUD-02 c3 | `docs/bootstrap-runbook.md` never mentions daemon-wide `policy-audit-mode` outside the first-lines warning block; capture step references `cpg generate --include-audit` | golden/text | `rtk proxy go test ./cmd/cpg/... -run TestRunbookNeverSuggestsDaemonWideAudit -v` | ❌ Wave 0 (22-03) |
| AUD-02 c4 | Existing SEC-01 SSA audit still passes with zero new allowlist entries after `get_bootstrap_policy` wiring | integration | `rtk proxy go test ./cmd/cpg/... -run TestMCPAuditReadonlyReachability -v` | ✅ `cmd/cpg/mcp_audit_test.go` (reused unmodified) |
| AUD-02 c5 | README compat row extended (no duplicate), all existing pins kept | golden/text | `rtk proxy go test ./cmd/cpg/... -run TestReadmeCompat -v` | ✅ `cmd/cpg/readme_compat_test.go` (extended in 22-03) |

---

## Wave 0 Gaps (tests created by the plans themselves)

- [ ] `pkg/policy/bootstrap_builder_test.go` — AUD-02 c1 named #35558 regression test (22-01 Task 2)
- [ ] `cmd/cpg/bootstrap_test.go` — AUD-02 c2 both branches, with a `DetectCiliumVersion` test seam mirroring `detectVersionFn` (22-02 Task 1)
- [ ] `cmd/cpg/mcp_bootstrap_test.go` — MCP tool success/missing-namespace paths (22-02 Task 2)
- [ ] `cmd/cpg/runbook_test.go` + `docs/bootstrap-runbook.md` — AUD-02 c3 golden pinning (22-03 Task 1)
