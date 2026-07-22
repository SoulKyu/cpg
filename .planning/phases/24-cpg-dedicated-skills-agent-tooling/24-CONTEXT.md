# Phase 24: cpg-Dedicated Skills & Agent Tooling - Context

**Gathered:** 2026-07-22
**Status:** Ready for planning
**Mode:** Smart discuss (autonomous) — recommended answers auto-accepted per continuous-autonomy directive

<domain>
## Phase Boundary

An LLM operator gets repo-local, cpg-specific skills (`.claude/skills/cpg-*/SKILL.md`) plus a single dedicated `cpg-operator` subagent (`.claude/agents/cpg-operator.md`) that drive real onboarding/triage/review workflows. Skills are workflow ROUTERS pointing at live `tools/list` discovery — never a third, drifting copy of tool semantics — with an automated Go consistency tripwire tying skill/README prose to the Go `Description:` strings. Requirements SKL-01..06. Everything repo-local; no global installs.

</domain>

<decisions>
## Implementation Decisions

### Deliverable Set (all six SKL items)
- Five skills: `cpg-triage` (SKL-01, live MCP session end-to-end), `cpg-audit-onboard` (SKL-02, full onboarding workflow), `cpg-policy-review` (SKL-03, offline CNP audit), `cpg-health-report` (SKL-04, cluster-health.json → HTML report), `cpg-mcp-smoke` (SKL-05, post-release smoke vs e2e fake relay).
- Build the `cpg-operator` subagent (SKL-06): single repo-local agent driving MCP sessions, referenced by `cpg-triage` and `cpg-audit-onboard` instead of per-skill agents.
- Layout: `.claude/skills/cpg-<name>/SKILL.md` with standard Claude Code skill frontmatter (`name`, `description`); `.claude/agents/cpg-operator.md` with agent frontmatter. Nothing outside the repo.

### Router Principle (anti-drift, locked by REQUIREMENTS)
- Skills NEVER restate tool argument schemas or result shapes. They name tools and route workflow steps; the harness discovers semantics live via `tools/list` (MCP) and `--help` (CLI). Tool names may appear; parameter-level detail may not.
- CLI-only surfaces from the gate decision are respected: `cpg-audit-onboard` GUIDES the operator through `cpg bootstrap` and `cpg audit-window` (human-run CLI commands, matching the Variant B decision) and DRIVES the capture via MCP `start_session` with `include_audit: true`.

### Consistency Tripwire (success criterion 5)
- One Go test (cmd/cpg, alongside the existing golden-pin tests) that: (1) enumerates the registered MCP tool names from the same registration path the server uses; (2) asserts every tool name referenced in any `.claude/skills/cpg-*/SKILL.md` and `.claude/agents/cpg-operator.md` exists in that registry (no phantom tools); (3) asserts every registered tool name is mentioned by at least the smoke skill (coverage floor); (4) pins the tool COUNT (now 9, including Phase 22's `get_bootstrap_policy`) so a future tool addition forces a skill sweep. Grep-based prose checks stay shallow (names only) per the router principle.
- README gets a short "Agent tooling" section listing the skills; pinned by the same test class.

### Smoke Scope (SKL-05)
- `cpg-mcp-smoke` asserts the real handshake tool count as it exists NOW (9 tools — the ROADMAP's "8-tool" text predates Phase 22; the criterion's intent is "the full real tool surface") plus a full session lifecycle against the existing e2e fake relay harness (reuse `mcp_e2e_test.go` infrastructure paths/commands, don't rebuild it).

### Claude's Discretion
- Exact skill prose, workflow step ordering inside each skill, HTML report structure for `cpg-health-report`, agent frontmatter details (tools list), test file naming.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `cmd/cpg/mcp.go` + `mcp_tools.go`/`mcp_query.go`/`mcp_bootstrap.go` — tool registration (names + Go `Description:` strings, the single source of truth).
- `cmd/cpg/mcp_e2e_test.go` — fake-relay e2e harness the smoke skill points at.
- `cmd/cpg/readme_compat_test.go`, `runbook_test.go`, `audit_docs_test.go` — golden-pin test conventions for prose.
- `docs/bootstrap-runbook.md` — the onboarding flow `cpg-audit-onboard` routes through.
- `pkg/hubble` cluster-health.json schema — input for `cpg-health-report`.

### Established Patterns
- Phase 22/23 golden tests: `strings.Contains`-based pins, region-scoped token checks.
- Repo has no `.claude/skills/` yet — greenfield layout, keep minimal.

### Integration Points
- `.claude/skills/cpg-*/SKILL.md`, `.claude/agents/cpg-operator.md` (new).
- `cmd/cpg/` new tripwire test file.
- README "Agent tooling" section.

</code_context>

<specifics>
## Specific Ideas

- The tripwire must fail if someone adds an MCP tool without updating the smoke skill — that's the drift the requirement targets.
- `cpg-audit-onboard`'s enforce checklist ends with the human applying policies (never an apply tool — out-of-scope table).

</specifics>

<deferred>
## Deferred Ideas

- Global/user-level skills — hard operator constraint, repo-local only.
- Per-skill dedicated agents — SKL-06's single `cpg-operator` replaces them.

</deferred>
