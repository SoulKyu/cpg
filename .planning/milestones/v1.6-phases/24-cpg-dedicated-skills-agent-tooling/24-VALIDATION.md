---
phase: 24
slug: cpg-dedicated-skills-agent-tooling
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-22
---

# Phase 24 — Validation Strategy

> Per-phase validation contract. Derived from 24-RESEARCH.md `## Validation Architecture` (full 12-row map there).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + testify |
| **Config file** | none |
| **Quick run command** | `rtk proxy go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` (sub-second) |
| **Full suite command** | `rtk proxy go test ./... -count=1 -race` |
| **Estimated runtime** | quick <1s; full ~4 min |

Note: sandbox denies `go` via `make`; invoke `go test` directly (via `rtk proxy`), never `make test`. The e2e smoke test skips under `-short` — run without it.

---

## Sampling Rate

- **After every task commit:** `rtk proxy go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v`
- **After every wave:** `rtk proxy go test ./cmd/cpg/... -count=1 -race`
- **Phase gate:** full suite green + one manual read-through per skill (prose quality is not mechanically testable — inherent to a routing/documentation phase, not a design gap)

---

## Per-Requirement Verification Map (mechanical half — prose quality is manual)

| Req | Mechanical check (all via `TestSkillsConsistencyTripwire`) | File Exists? |
|-----|-------------------------------------------------------------|--------------|
| SKL-01..06 | No phantom tool names in any `.claude/skills/cpg-*/SKILL.md` or `.claude/agents/cpg-operator.md` (registry enumerated live via `startInMemoryMCPSession`) | ❌ Wave 0 |
| SKL-05 | `cpg-mcp-smoke` mentions every registered tool (coverage floor) + routes to `TestMCPE2EGracefulLifecycle` | ❌ Wave 0 (tripwire) / ✓ (e2e test pre-existing) |
| SKL-06 | `cpg-operator` referenced by `cpg-triage` and `cpg-audit-onboard` | ❌ Wave 0 |
| (all) | Tool count pinned at 9 (a 10th tool forces a skill sweep) | ❌ Wave 0 |
| (all) | README `## Agent tooling` section lists all 5 skills | ❌ Wave 0 |

Manual-only (human read-through at phase gate): triage workflow sensibility (SKL-01), audit-onboard guides-not-runs CLI + no apply tool (SKL-02), health-report HTML vs real `ClusterHealthReport` fields (SKL-04).

---

## Wave 0 Gaps

- [ ] `cmd/cpg/skills_test.go` — `TestSkillsConsistencyTripwire` (reuses `startInMemoryMCPSession` from mcp_harness_test.go:28; `../../` relative paths per readme_compat_test.go convention)
- [ ] The five `SKILL.md` files + `cpg-operator.md` (the artifacts under test)
- [ ] README `## Agent tooling` section
