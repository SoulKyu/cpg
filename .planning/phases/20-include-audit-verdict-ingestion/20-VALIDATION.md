---
phase: 20
slug: include-audit-verdict-ingestion
status: approved
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-22
---

# Phase 20 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` + testify v1.11.1 + `zaptest/observer` |
| **Config file** | none — go.mod at repo root |
| **Quick run command** | `rtk proxy go test ./pkg/hubble/... ./pkg/flowsource/... ./pkg/session/... ./cmd/cpg/... -count=1 -race` |
| **Full suite command** | `rtk proxy go test ./... -count=1 -race` |
| **Estimated runtime** | ~60 seconds |

Note: sandbox denies `go` via `make`; invoke `go test` directly (via `rtk proxy`), never `make test`.

---

## Sampling Rate

- **After every task commit:** Run the task's `<automated>` command
- **After every plan wave:** Run `rtk proxy go test ./... -count=1 -race`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 90 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 20-01-01 | 01 | 1 | AUD-01 | T-20-01 | counter defaults off, no behavior change | unit (tdd) | `rtk proxy go test ./pkg/hubble/ -run 'TestAggregator_AuditVerdictCount' -race -count=1` | ✅ | ⬜ pending |
| 20-01-02 | 01 | 1 | AUD-01 | T-20-03 | classifier gate widened to exactly {DROPPED, AUDIT} | unit (tdd) | `rtk proxy go test ./pkg/hubble/ -run 'TestAggregator' -race -count=1` | ✅ | ⬜ pending |
| 20-02-01 | 02 | 2 | AUD-01 | T-20-01 | interface + filter sites widened, default-off preserved | build | `rtk proxy go build ./pkg/flowsource/ ./pkg/hubble/` | ✅ | ⬜ pending |
| 20-02-02 | 02 | 2 | AUD-01 | T-20-01 | 19-location compile ripple lands atomically | build+vet | `rtk proxy go build ./... && rtk proxy go vet ./pkg/hubble/ ./pkg/flowsource/ ./pkg/session/` | ✅ | ⬜ pending |
| 20-02-03 | 02 | 2 | AUD-01 | T-20-01 | buildFilters value-pinning proves byte-identical default | unit | `rtk proxy go test ./pkg/hubble/ -run 'TestBuildFilters' -race -count=1` | ✅ | ⬜ pending |
| 20-03-01 | 03 | 3 | AUD-01 | — | fixture contains AUDIT verdicts | CLI | `rtk proxy rg -c "verdict" testdata/flows/with_audit.jsonl \| rtk proxy rg -q "^2$" && rtk proxy rg -q "AUDIT" testdata/flows/with_audit.jsonl && echo OK` | ✅ | ⬜ pending |
| 20-03-02 | 03 | 3 | AUD-01 | T-20-01 | E2E: AC-1/2/3 behavioral proof incl. single-warning | e2e-unit | `rtk proxy go test ./pkg/hubble/ -run 'TestPipeline_Audit' -race -count=1 -v` | ✅ | ⬜ pending |
| 20-04-01 | 04 | 3 | AUD-01 | T-20-01 | `--include-audit` defaults false on generate/replay | unit | `rtk proxy go test ./cmd/cpg/ -run 'CommonFlags\|Flag' -race -count=1 && rtk proxy go build ./cmd/cpg/` | ✅ | ⬜ pending |
| 20-04-02 | 04 | 3 | AUD-01 | T-20-05 | MCP `include_audit` param, SEC-01 readonly untouched | unit | `rtk proxy go test ./pkg/session/ ./cmd/cpg/ -run 'PipelineConfig\|StartSession\|Session' -race -count=1 && rtk proxy go build ./...` | ✅ | ⬜ pending |
| 20-04-03 | 04 | 3 | AUD-01 | — | README documents flag (doc-drift guard) | CLI | `rtk proxy rg -n "include-audit\|include_audit" README.md` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements (go test with established golden/regression patterns in `pkg/hubble/pipeline_l7_test.go`; the one new fixture `testdata/flows/with_audit.jsonl` is created by task 20-03-01 itself).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| AUDIT `DropReasonDesc` fidelity on live cluster | AUD-01 | Requires live Cilium cluster with audit-mode policies | Deploy audit-mode CNP, run `cpg generate --include-audit`, inspect generated policy reasons |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (none — no MISSING markers)
- [x] No watch-mode flags
- [x] Feedback latency < 90s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-07-22
