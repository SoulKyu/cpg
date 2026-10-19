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
| **Quick run command** | `go test -race -count=1 -run 'TestL7Collapse|TestPathTemplate' ./pkg/policy/... ./pkg/hubble/...` |
| **Full suite command** | `make test` (go test -race ./...) |
| **Estimated runtime** | ~30 seconds (full suite; policy package ~10s) |

> **Vacuous-pass guard:** per-row commands target specific packages with `-run`. If the output reports `no tests to run` (`testing: warning: no tests to run`), the pattern matched nothing in the targeted packages — treat the run as FAILED, never as a pass. Always pass `-count=1` (cached results are not evidence).

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
| 25-01-01 | 01 | 1 | L7GEN-02 | T-25-01 | Variance-only generalization; mixed classes stay literal | unit | `go test -race -count=1 -run TestPathTemplateClassifier ./pkg/policy/...` | ❌ W0 | ⬜ pending |
| 25-01-02 | 01 | 1 | L7GEN-04 | — | Below-gate templates emit literal rules, never silent widening | unit+replay | `go test -race -count=1 -run TestMinFlowsGate ./pkg/hubble/...` | ❌ W0 | ⬜ pending |
| 25-01-03 | 01 | 1 | L7GEN-01 | — | 10k distinct paths collapse to one anchored template | golden/replay | `go test -race -count=1 -run TestL7CollapseGolden ./pkg/hubble/...` | ❌ W0 | ⬜ pending |
| 25-01-04 | 01 | 1 | L7GEN-02 | T-25-01 | Segment-class matrix: hex/UUID templates; version/mixed/encoded literal fallback; pattern length cap (T-25-02) | unit+golden | `go test -race -count=1 -run 'TestPathTemplateClassifier\|TestCollapseHTTPPaths\|TestL7CollapseMixedClassesGolden' ./pkg/policy/... ./pkg/hubble/...` | ❌ W0 | ⬜ pending |
| 25-01-05 | 01 | 1 | L7GEN-04 | T-25-05 | Gate boundary N-1/N; CLI rejects --min-flows-per-l7-rule < 1 (fail-closed); explain keeps literal-path evidence | unit+integration | `go test -race -count=1 -run 'TestCollapseHTTPPaths_GateBoundary\|TestMinFlowsGate\|TestValidateMinFlowsPerL7Rule' ./pkg/policy/... ./pkg/hubble/... ./cmd/cpg/...` | ❌ W0 | ⬜ pending |
| 25-02-01 | 02 | 2 | L7GEN-01 (criterion 2) | — | Byte-identical output without the flag | golden/regression | `go test -race -count=1 -run TestL7NoCollapseByteIdentity ./pkg/hubble/...` | ❌ W0 | ⬜ pending |
| 25-02-02 | 02 | 2 | L7GEN-03 | T-25-05 | MCP arg validation fail-closed (reject invalid min_flows) | integration | `go test -race -count=1 -run TestMCPCollapseArgs ./cmd/cpg/...` | ❌ W0 | ⬜ pending |
| 25-02-03 | 02 | 2 | L7GEN-03 | — | MCP-vs-CLI parity: same flows, same collapsed rules (ROADMAP SC5) | integration | `go test -race -count=1 -run TestMCPCLICollapseParity ./cmd/cpg/...` | ❌ W0 | ⬜ pending |

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
| 5 — MCP parity | CLI-vs-MCP parity on one shared fixture (`TestMCPCLICollapseParity`); `TestMCPCollapseArgs` covers the fail-closed arg validation half |
