---
phase: 24-cpg-dedicated-skills-agent-tooling
reviewed: 2026-07-22T00:00:00Z
depth: standard
files_reviewed: 8
files_reviewed_list:
  - .claude/agents/cpg-operator.md
  - .claude/skills/cpg-triage/SKILL.md
  - .claude/skills/cpg-audit-onboard/SKILL.md
  - .claude/skills/cpg-policy-review/SKILL.md
  - .claude/skills/cpg-health-report/SKILL.md
  - .claude/skills/cpg-mcp-smoke/SKILL.md
  - README.md
  - cmd/cpg/skills_test.go
findings:
  critical: 0
  warning: 1
  info: 2
  total: 3
status: issues_found
---

# Phase 24: Code Review Report

**Reviewed:** 2026-07-22
**Depth:** standard
**Files Reviewed:** 8
**Status:** issues_found

## Summary

Documentation/routing phase: five repo-local skills + one driver agent + README section + a consistency tripwire test. Reviewed against ground-truth surfaces (9-tool MCP registry, `cpg explain` flag set, `pkg/hubble/health_writer.go` schema, `docs/bootstrap-runbook.md` phase order, `cmd/cpg/mcp_e2e_test.go`).

Router principle holds well: skills name tool names only and defer schemas/result shapes to live `tools/list`. Verified facts that check out:
- All 9 tool names referenced across skills+agent map to the live registry (`start_session`, `get_status`, `stop_session`, `list_dropped_flows`, `list_policies`, `get_policy`, `get_evidence`, `get_cluster_health`, `get_bootstrap_policy`); no phantoms.
- `cpg-audit-onboard` mirrors the runbook phase order, GUIDES human-run `cpg bootstrap`/`cpg audit-window`, drives capture via MCP audit-inclusion, ends with human `kubectl apply`, invokes no apply tool. Safety prose is correct: explicitly forbids daemon-wide audit mode, routes RBAC to the runbook, and the "no separate disable step" claim matches the runbook's Disable section.
- `cpg-health-report` field names (`flows_seen`, `infra_drops_total`, `reason`/`class`/`count`, `by_node`/`by_workload`, `remediation`) all match the health writer struct; the "server-capped at 100 entries" claim matches `maxHealthMapEntries = 100`; `remediation` omitempty is handled ("omit link when empty"); XSS-escape step present.
- `cpg-mcp-smoke` correctly targets `TestMCPE2EGracefulLifecycle` WITHOUT `-short` (both e2e tests skip under short mode), asserts 9 tools, and honestly hedges the coverage floor ("exercised OR at minimum named") — accurate because the graceful e2e exercises 8 tools but not `get_bootstrap_policy`.
- Agent tools allowlist is minimal (`Bash, Read`), no `Write`/`Edit`.
- Zero `go.mod`/`go.sum` changes. `TestSkillsConsistencyTripwire`, README, and runbook tests pass.

One factual defect (invalid `cpg explain` flag) plus two informational notes on the tripwire's coverage semantics.

## Warnings

### WR-01: cpg-policy-review references a non-existent `cpg explain --output json` flag

**File:** `.claude/skills/cpg-policy-review/SKILL.md:37`
**Issue:** The note "it returns evidence in the same shape `cpg explain --output json` does" cites a flag that does not exist. `explain.go` registers `--json` (bool), `--format` (text|json|yaml), and `-o`/`--output-dir` (a directory, not a format). There is no `--output` flag anywhere (root only has a persistent `--json` for log format). `cpg explain --output json` errors with `unknown flag: --output`. This contradicts the phase directive "policy-review: real `cpg explain` flags only" and undermines the skill's own Step 2 flag list, which correctly names `--json`/`--format`. Root cause: the skill echoes the same wrong string carried by the MCP evidence tool's own description in `cmd/cpg/mcp_query_evidence.go:49,72` (out of phase-24 diff scope), so the inaccuracy now exists in two places and will drift.
**Fix:**
```markdown
it returns evidence in the same shape `cpg explain --json` (or `--format json`) does.
```
Consider also correcting the source string in `mcp_query_evidence.go` in a follow-up so the skill and tool description stop reinforcing each other.

## Info

### IN-01: Phantom-tool detection only catches backtick-quoted mentions

**File:** `cmd/cpg/skills_test.go:28`
**Issue:** `toolNameToken` requires backticks around a `start_/get_/stop_/list_`-prefixed token. An unquoted phantom tool name in any non-smoke skill would evade the phantom assertion. This is acceptable per design — the comment documents the mandatory backtick convention, and the coverage-floor assertion forces `cpg-mcp-smoke` to backtick all 9 tools (an unquoted name there would fail coverage) — but phantom detection in the other four skills still relies on author discipline rather than the test.
**Fix:** No change required; if stronger enforcement is wanted later, add a lint that rejects unquoted tool-name tokens, or broaden the regex to catch bare mentions and allowlist the known non-tool prefixes.

### IN-02: Coverage floor asserts tools are named, not exercised

**File:** `cmd/cpg/skills_test.go:63-65`
**Issue:** The coverage floor requires every registered tool to appear (backtick-quoted) in `cpg-mcp-smoke/SKILL.md`; it does not (and cannot, being a doc test) assert the e2e harness actually exercises each. `get_bootstrap_policy` is counted in the tools/list assertion (9) but is not exercised by `TestMCPE2EGracefulLifecycle`. The skill is honest about this ("exercised OR at minimum named"), so there is no contradiction — flagging only so downstream consumers understand the tripwire guarantees name-consistency, not runtime coverage.
**Fix:** No change required; consideration for a future phase if true runtime coverage of `get_bootstrap_policy` in the smoke path is desired.

---

_Reviewed: 2026-07-22_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
