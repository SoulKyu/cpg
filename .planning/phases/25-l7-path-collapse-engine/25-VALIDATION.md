---
phase: "25"
slug: "l7-path-collapse-engine"
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: "2026-10-04"
---

# Phase 25 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test + testify (race detector enabled, per project convention) |
| **Config file** | `Makefile` (test target) |
| **Quick run command** | `go test -race -run 'TestL7Collapse|TestPathTemplate' ./pkg/policy/...` |
| **Full suite command** | `make test` (go test -race ./...) |
| **Estimated runtime** | ~30 seconds (full suite; policy package ~10s) |

---

## Sampling Rate

- **After every task commit:** Run `go test -race ./pkg/policy/...`
- **After every plan wave:** Run `make test`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 25-01-01 | 01 | 1 | L7GEN-02 | T-25-01 | Variance-only generalization; mixed classes stay literal | unit | `go test -race -run TestPathTemplateClassifier ./pkg/policy/...` | ❌ W0 | ⬜ pending |
| 25-01-02 | 01 | 1 | L7GEN-04 | — | Below-gate templates emit literal rules, never silent widening | unit | `go test -race -run TestMinFlowsGate ./pkg/policy/...` | ❌ W0 | ⬜ pending |
| 25-01-03 | 01 | 1 | L7GEN-01 | — | 10k distinct paths collapse to one anchored template | golden/replay | `go test -race -run TestL7CollapseGolden ./pkg/policy/...` | ❌ W0 | ⬜ pending |
| 25-02-01 | 02 | 2 | L7GEN-01 (criterion 2) | — | Byte-identical output without the flag | golden/regression | `go test -race -run TestL7NoCollapseByteIdentity ./pkg/policy/...` | ❌ W0 | ⬜ pending |
| 25-02-02 | 02 | 2 | L7GEN-03 | T-25-02 | MCP arg validation fail-closed (reject invalid min_flows) | integration | `go test -race -run TestMCPCollapseArgs ./` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Wave 0 (test scaffolding) must exist before implementation tasks:

- Fixture files `testdata/flows/l7-collapse-*.jsonl` (users-10k, mixed-classes, below-threshold, unicode-encoded) following existing jsonpb fixture conventions
- Golden-file harness for generated CNP YAML (byte-identity regression for criterion 2)

---

## Success-Criteria Mapping (from ROADMAP Phase 25)

| SC | Proof |
|----|-------|
| 1 — 10k paths → 1 template | `TestL7CollapseGolden` on `users-jsonl` fixture (replay, deterministic) |
| 2 — byte-identical without flag | `TestL7NoCollapseByteIdentity` golden vs v1.6 output |
| 3 — gate literal fallback | `TestMinFlowsGate` on `below-threshold.jsonl` |
| 4 — variance-only classes | table-driven `TestPathTemplateClassifier` (numeric/hex/UUID/mixed/version/encoded) |
| 5 — MCP parity | CLI-vs-MCP golden equivalence on one shared fixture (`TestMCPCollapseArgs` + parity test) |
