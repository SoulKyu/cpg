---
phase: 24-cpg-dedicated-skills-agent-tooling
fixed_at: 2026-07-22T00:00:00Z
review_path: .planning/phases/24-cpg-dedicated-skills-agent-tooling/24-REVIEW.md
iteration: 1
findings_in_scope: 1
fixed: 1
skipped: 0
status: all_fixed
---

# Phase 24: Code Review Fix Report

| ID | Severity | Fix | Commit |
|----|----------|-----|--------|
| WR-01 | warning | Non-existent `cpg explain --output json` corrected to the real `--json` flag in `cpg-policy-review/SKILL.md` AND at the drift source in `cmd/cpg/mcp_query_evidence.go` (doc comment + tool Description string) — both places now match `explain.go`'s actual flag surface | `d011d16` |

Skipped (info, accepted-by-design, documented in REVIEW.md): IN-01 (regex catches only backtick-quoted tool mentions — deliberate router-principle trade-off), IN-02 (coverage floor asserts named-not-exercised for `get_bootstrap_policy` in the smoke — skill prose honestly hedges this).

Verification: tripwire + evidence-tool + README/runbook pins green (`rtk proxy go test ./cmd/cpg/... -run "TestMCPQuery|TestGetEvidence|TestReadme|TestRunbook|TestSkillsConsistencyTripwire"`).
