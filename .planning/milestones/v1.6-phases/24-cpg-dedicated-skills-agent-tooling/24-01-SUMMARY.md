---
phase: 24-cpg-dedicated-skills-agent-tooling
plan: 01
subsystem: infra
tags: [claude-code, skills, agent, mcp, markdown, docs, cilium, cpg]

# Dependency graph
requires: []
provides:
  - "Six repo-local markdown router artifacts: .claude/agents/cpg-operator.md + five .claude/skills/cpg-*/SKILL.md"
  - "README '## Agent tooling' section listing all 5 skills + cpg-operator"
  - "Every backtick-quoted tool name across the six artifacts limited to the real 9-tool MCP registry (zero phantom names)"
affects: [24-02]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Router Principle: skills/agent prose name workflow steps + backtick-quoted tool NAMES only, never argument schemas or result shapes; semantics discovered live via tools/list"
    - "Single shared cpg-operator agent (Bash+Read only) driving MCP session lifecycle on behalf of cpg-triage and cpg-audit-onboard, instead of per-skill agents"

key-files:
  created:
    - .claude/agents/cpg-operator.md
    - .claude/skills/cpg-triage/SKILL.md
    - .claude/skills/cpg-audit-onboard/SKILL.md
    - .claude/skills/cpg-policy-review/SKILL.md
    - .claude/skills/cpg-health-report/SKILL.md
    - .claude/skills/cpg-mcp-smoke/SKILL.md
  modified:
    - README.md

key-decisions:
  - "cpg-operator tools allowlist pinned to Bash, Read only (no Write/Edit) — agent drives and reports, never authors repo files"
  - "cpg-audit-onboard guides cpg bootstrap / cpg audit-window as human-run CLI steps and never invokes them itself; enforce checklist ends with the human running kubectl apply, no apply tool referenced or invented"
  - "cpg-audit-onboard avoids the bare hyphenated daemon-wide policy-audit-mode token entirely (defense-in-depth beyond the existing path-scoped runbook pin)"
  - "cpg-mcp-smoke asserts the current real 9-tool handshake (not the stale '8-tool' figure) and backtick-mentions all 9 tool names for the wave-2 coverage-floor tripwire"
  - "README '## Agent tooling' section inserted between '## MCP Server (cpg mcp)' and '## Label selection', mirroring the existing MCP tool table's terse two-column style"

patterns-established:
  - "Pattern: SKILL.md two-field frontmatter (name, description with positive + negative triggers) — no tools: field on skills, only on agents"
  - "Pattern: agent frontmatter (name, description, tools: comma-separated allowlist) with description stating who spawns it and that it is never invoked standalone"

requirements-completed: [SKL-01, SKL-02, SKL-03, SKL-04, SKL-05, SKL-06]

# Metrics
duration: ~20min
completed: 2026-07-22
---

# Phase 24 Plan 01: cpg-Dedicated Skills & Agent Tooling Summary

**Six markdown workflow routers (cpg-operator agent + 5 cpg-* skills) plus a README Agent tooling section, all backtick-referencing only the real 9-tool MCP registry with zero schema restatement**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-22T18:04:00Z (approx, first commit 2026-07-22T18:04:51Z)
- **Completed:** 2026-07-22T18:07:07Z
- **Tasks:** 3
- **Files modified:** 7 (6 created, 1 modified)

## Accomplishments
- Authored `cpg-operator` agent (Bash+Read only) and `cpg-triage` skill routing the tested live-session tool sequence (`start_session` → `get_status` → `list_dropped_flows` → per-workload `list_policies`/`get_policy`/`get_evidence` → `stop_session` → `get_cluster_health`)
- Authored `cpg-audit-onboard` (Variant B: guides CLI, drives capture via MCP, human applies) and `cpg-policy-review` (routes `cpg explain` + `get_evidence` to README's L7 Prerequisites / Explain policies sections)
- Authored `cpg-health-report` (cluster-health.json → escaped, self-contained HTML) and `cpg-mcp-smoke` (routes to `TestMCPE2EGracefulLifecycle` without `-short`, backtick-mentions all 9 tools)
- Added README `## Agent tooling` section listing all 5 skills + `cpg-operator`
- Verified, across all six files together, that every backtick-quoted `start_/get_/stop_/list_` token is one of the real 9 MCP tools — no phantom names

## Task Commits

Each task was committed atomically:

1. **Task 1: cpg-operator agent + cpg-triage skill** - `817db1f` (feat)
2. **Task 2: cpg-audit-onboard + cpg-policy-review skills** - `de97afa` (feat)
3. **Task 3: cpg-health-report + cpg-mcp-smoke skills + README agent tooling** - `26d06a5` (feat)

**Plan metadata:** pending (this commit)

## Files Created/Modified
- `.claude/agents/cpg-operator.md` - single repo-local agent driving live MCP session lifecycle, Bash+Read only
- `.claude/skills/cpg-triage/SKILL.md` - live-session triage router, delegates driving to cpg-operator
- `.claude/skills/cpg-audit-onboard/SKILL.md` - onboarding router: guides bootstrap/audit-window CLI, drives capture, ends with human kubectl-apply
- `.claude/skills/cpg-policy-review/SKILL.md` - offline CNP audit router via `cpg explain` + `get_evidence`
- `.claude/skills/cpg-health-report/SKILL.md` - cluster-health.json → escaped self-contained HTML report router
- `.claude/skills/cpg-mcp-smoke/SKILL.md` - post-release smoke router, all 9 tools + e2e lifecycle
- `README.md` - new "## Agent tooling" section between MCP Server and Label selection sections

## Decisions Made
- `cpg-operator`'s tools allowlist is `Bash, Read` — no `Write`/`Edit`, matching the readonly discipline of the underlying `cpg mcp` server itself (T-24-02 mitigation).
- `cpg-audit-onboard` never runs `cpg bootstrap`/`cpg audit-window` itself (human-run CLI, Variant B) and never mentions an apply tool for the enforce checklist — the human always runs the final `kubectl apply` (T-24-04 mitigation).
- `cpg-audit-onboard` avoids the bare hyphenated `policy-audit-mode` token entirely in its own prose, even though the existing runbook pin (`TestRunbookNeverSuggestsDaemonWideAudit`) only scopes `docs/bootstrap-runbook.md` — defense-in-depth per the plan's explicit instruction.
- `cpg-health-report` instructs HTML-escaping every `cluster-health.json` string field before embedding (T-24-03 mitigation, self-XSS guardrail).
- `cpg-mcp-smoke` asserts the tool count is 9 (current reality) rather than the stale "8-tool" ROADMAP text.

## Deviations from Plan

None - plan executed exactly as written. All six artifacts and the README section match the plan's `must_haves` truths/artifacts/key_links verbatim; every per-task automated verify command passed.

## Issues Encountered

None. The `rtk` shell hook rejected a couple of compound `&&`-chained multi-file grep commands as "too complex to verify worktree containment" — worked around by running each grep check as a separate single-purpose `rtk proxy grep` invocation per file/token, matching the pattern documented in this project's own memory notes (RTK grep/rg rewrite quirks).

## User Setup Required

None - no external service configuration required. All output is repo-local markdown; no new Go dependency, no `go.mod`/`go.sum` change (`git diff --exit-code go.mod go.sum` confirmed clean).

## Next Phase Readiness
- All six artifacts exist with correctly backtick-quoted tool-name references limited to the real 9-tool registry — plan 24-02 (wave 2) can now build `cmd/cpg/skills_test.go`'s `TestSkillsConsistencyTripwire` against these files without any phantom-tool or coverage-floor failures expected.
- No blockers. The tripwire test itself (mechanical enforcement) is out of this plan's scope by design — it belongs to 24-02.

---
*Phase: 24-cpg-dedicated-skills-agent-tooling*
*Completed: 2026-07-22*

## Self-Check: PASSED

- FOUND: .claude/agents/cpg-operator.md
- FOUND: .claude/skills/cpg-triage/SKILL.md
- FOUND: .claude/skills/cpg-audit-onboard/SKILL.md
- FOUND: .claude/skills/cpg-policy-review/SKILL.md
- FOUND: .claude/skills/cpg-health-report/SKILL.md
- FOUND: .claude/skills/cpg-mcp-smoke/SKILL.md
- FOUND: README.md (## Agent tooling section present)
- FOUND commit: 817db1f
- FOUND commit: de97afa
- FOUND commit: 26d06a5
