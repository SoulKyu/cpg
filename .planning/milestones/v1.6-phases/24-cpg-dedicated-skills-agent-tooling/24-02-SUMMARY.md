---
phase: 24-cpg-dedicated-skills-agent-tooling
plan: 02
subsystem: testing
tags: [go, mcp, testify, in-memory-transport, consistency-tripwire, skills, agent-tooling]

# Dependency graph
requires:
  - phase: 24-cpg-dedicated-skills-agent-tooling
    provides: "plan 24-01's six markdown artifacts (5 SKILL.md files + cpg-operator.md) and README '## Agent tooling' section"
provides:
  - "cmd/cpg/skills_test.go: TestSkillsConsistencyTripwire — compiled, CI-run mitigation for skill/agent/README drift against the live MCP tool registry"
  - "liveToolRegistry(t) helper: live tools/list enumeration reusable by future tripwire-style tests"
affects: [future phases adding/removing MCP tools, .claude/skills/*, .claude/agents/cpg-operator.md, README.md '## Agent tooling']

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Consistency tripwire pattern: compiled Go test cross-checks developer-authored markdown prose against a live, in-process enumerated registry (never a hardcoded literal list) so drift fails a build, not just a review"
    - "Backtick-quoted, verb-prefixed regex token convention ((?:start|get|stop|list)_[a-z_]+) as the load-bearing contract skill authors must follow for phantom/coverage checks to work"

key-files:
  created: [cmd/cpg/skills_test.go]
  modified: []

key-decisions:
  - "Reused startInMemoryMCPSession + initLoggerForTesting verbatim from mcp_harness_test.go/mcp_session_test.go — zero new test infrastructure, zero new dependencies"
  - "Kept README pin assertions in skills_test.go rather than readme_compat_test.go, per RESEARCH guidance, to keep that file scoped to COMPAT-01/03"

patterns-established:
  - "Pattern: liveToolRegistry(t) map[string]bool as the canonical way any future test enumerates the exact MCP tool surface without hardcoding names"

requirements-completed: [SKL-01, SKL-02, SKL-03, SKL-04, SKL-05, SKL-06]

# Metrics
duration: 12min
completed: 2026-07-22
---

# Phase 24 Plan 02: Skills Consistency Tripwire Summary

**Compiled Go test (`TestSkillsConsistencyTripwire`) that live-enumerates the 9-tool MCP registry via the in-memory harness and fails the build on any phantom tool name, missing smoke coverage, tool-count drift, or missing README pin — the only artifact in Phase 24 that can fail CI on skill/agent/README drift.**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-07-22T18:07:00Z (approx, after worktree sync)
- **Completed:** 2026-07-22T18:19:38Z
- **Tasks:** 2 (1 code task + 1 gate task, no code change)
- **Files modified:** 1 (`cmd/cpg/skills_test.go`, new)

## Accomplishments
- `TestSkillsConsistencyTripwire` enumerates the live tool registry (9 tools) via `startInMemoryMCPSession` — never a hardcoded literal list.
- Phantom-check: zero backtick-quoted `start_/get_/stop_/list_` tokens in any `cpg-*/SKILL.md` or `cpg-operator.md` reference a tool not in the live registry.
- Coverage floor: `cpg-mcp-smoke/SKILL.md` mentions all 9 registered tools.
- Count pin: `wantToolCount = 9`, with a sweep-before-bump failure message.
- README pins: `## Agent tooling` section present, all 5 skill names (`cpg-triage`, `cpg-audit-onboard`, `cpg-policy-review`, `cpg-health-report`, `cpg-mcp-smoke`) present.
- Full `cmd/cpg` package green under `-race` (no `-short`), including `TestMCPE2EGracefulLifecycle` — the SKL-05 smoke routing target proven to actually run (not silently skipped).
- Full module (`go test ./...`) green under `-race`.

## Task Commits

Each task was committed atomically:

1. **Task 1: TestSkillsConsistencyTripwire (phantom-check + coverage-floor + count-pin + README pins)** - `8d52f4a` (test)
2. **Task 2: Full race-suite gate incl. the SKL-05 e2e smoke target (no -short)** - no commit (gate-only task, all green on first run; no fix needed)

**Plan metadata:** pending (this summary's commit)

## Files Created/Modified
- `cmd/cpg/skills_test.go` - `TestSkillsConsistencyTripwire`, `liveToolRegistry(t)`, `toolNameToken` regex, `wantToolCount` const

## Decisions Made
- Reused `startInMemoryMCPSession` verbatim (same package `main`) and added the required `initLoggerForTesting(t)` call inside `liveToolRegistry` — every existing caller of `startInMemoryMCPSession` in this package calls it first (confirmed via `mcp_session_test.go`), and omitting it causes a nil-pointer panic in `bridgedSlogLogger` (`mcp.go:120`) because the package-level zap logger is uninitialized outside `cpg`'s normal startup path. This was not explicit in the plan's task sketch but is required for the helper to work; documented here as a minor gap-fill, not a deviation requiring a rule citation (mechanical prerequisite for calling an existing, documented helper).
- Kept the README `## Agent tooling` + skill-name assertions inside `skills_test.go` (not `readme_compat_test.go`), matching RESEARCH's explicit "keep that file scoped to COMPAT" instruction.

## Deviations from Plan

None beyond the `initLoggerForTesting` gap-fill noted above (mechanical, not a Rule 1-4 event — required by the pre-existing test-harness contract, verified by grepping all three existing `startInMemoryMCPSession` call sites). Wave-1 artifacts (skills, agent doc, README section) needed zero fixes — the tripwire passed against them on the first run, confirming plan 24-01 was already fully consistent with the live 9-tool registry.

## Issues Encountered
- `rtk proxy go test ./cmd/cpg/... -count=1 -race -timeout 300s` exceeds the Bash tool's default 120s timeout (takes ~211s); reran with an explicit longer tool timeout. No code issue — purely an invocation note for future executors running the full `cmd/cpg` race suite.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- SKL success criterion 5 fully satisfied: an automated, CI-enforced consistency tripwire now ties skill/agent/README prose to the compiled MCP tool registry.
- Phase 24 (`cpg-dedicated-skills-agent-tooling`) has no further plans in its wave structure per this plan's scope; ready for phase completion / verification workflow.

---
*Phase: 24-cpg-dedicated-skills-agent-tooling*
*Completed: 2026-07-22*
