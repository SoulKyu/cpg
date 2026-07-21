---
phase: 19-security-hardening-end-to-end-validation
plan: 03
subsystem: docs
tags: [readme, mcp, documentation, secrets-posture, kubeconfig]

# Dependency graph
requires:
  - phase: 16-mcp-server-foundation-write-safety
    provides: "cpg mcp stdio server skeleton, readonly composition root (runMCPServer)"
  - phase: 17-session-lifecycle
    provides: "start_session/get_status/stop_session tool semantics, stopped-session retention (D-01/D-02)"
  - phase: 18-query-tools
    provides: "all 5 query tools (list_dropped_flows, list_policies, get_policy, get_evidence, get_cluster_health) with final registered descriptions/annotations"
provides:
  - "New `## MCP Server (cpg mcp)` README section (SEC-03) — the only operator-facing documentation for `cpg mcp`"
  - "8-tool reference table matching the real registered tool descriptions"
  - "Documented harness `env` contract (KUBECONFIG/PATH/TMPDIR + WHY per key)"
  - "Documented secrets posture (what reaches the LLM, what never does, no v1.5 redaction) with L7 cross-link"
  - "Documented exec-credential-plugin non-interactive-hang caveat + credential-persistence note"
affects: [phase-transition, milestone-close, future-mcp-doc-updates]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "README section structure: intro paragraph -> table -> ### sub-headings per concern (mirrors ## L7 Prerequisites)"

key-files:
  created: []
  modified:
    - "README.md - new ## MCP Server (cpg mcp) section (51 lines) between ## Explain policies and ## Label selection"

key-decisions:
  - "Followed D-12/D-13/D-14 exactly as locked in 19-CONTEXT.md - no new decisions required"
  - "Tool one-liners derived from the real registered Description strings in cmd/cpg/mcp_tools.go, cmd/cpg/mcp_query.go, cmd/cpg/mcp_query_evidence.go, cmd/cpg/mcp_query_flows.go (not invented) to keep the table honest against D-14's scope guard"
  - "Exec-credential-plugin credential-persistence note resolves 19-RESEARCH.md Open Question 1 as a one-line documentation caveat, not a code change (third-party client-go behavior, out of SEC-01's audit scope)"

patterns-established:
  - "Doc-only D-13 mandatory-content-block pattern: any future MCP tool doc addition should extend the existing table + relevant ### sub-heading rather than creating a new top-level section"

requirements-completed: [SEC-03]

# Metrics
duration: 20min
completed: 2026-07-21
---

# Phase 19 Plan 03: MCP Server README Section Summary

**New `## MCP Server (cpg mcp)` README section covering the 8-tool table, harness `env` (KUBECONFIG/PATH/TMPDIR) contract, LLM secrets posture, and the exec-credential-plugin non-interactive-hang caveat — written from scratch since README had zero prior MCP mentions.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-21T18:21:57Z
- **Tasks:** 1/1 completed
- **Files modified:** 1

## Accomplishments
- Authored all 6 D-13 mandatory content blocks in the required order: what-it-is, 8-tool table, harness configuration, secrets posture, exec-credential-plugin caveat, session model
- Cross-linked the secrets-posture paragraph to the existing `#l7-prerequisites` anchor using the file's own established link syntax
- Verified every tool one-liner against the real registered `Description` strings in `cmd/cpg/mcp_tools.go` / `mcp_query.go` / `mcp_query_evidence.go` / `mcp_query_flows.go` rather than inventing prose
- Resolved 19-RESEARCH.md's Open Question 1 (OIDC/exec credential-cache persistence) as the one-line caveat note the research explicitly recommended

## Task Commits

Each task was committed atomically:

1. **Task 1: Author the `## MCP Server (cpg mcp)` README section (6 mandatory blocks)** - `b2562df` (docs)

**Plan metadata:** committed together with this SUMMARY.md (worktree mode — STATE.md/ROADMAP.md excluded; orchestrator updates those after merge)

## Files Created/Modified
- `README.md` - New `## MCP Server (cpg mcp)` section (lines 506-556): intro paragraph, 8-row tool table, `### Harness configuration` (JSON `mcpServers` example + KUBECONFIG/PATH/TMPDIR WHY bullets), `### Secrets posture` (L7 cross-link, Authorization/Cookie never captured, no v1.5 redaction), `### Exec-credential-plugin caveat` (aws eks get-token / gke-gcloud-auth-plugin / azure kubelogin, headless-auth verification, bounded-timeout actionable error, credential-persistence note), `### Session model` (one session at a time, stopped-session retention)

## Decisions Made
None new — followed 19-CONTEXT.md's D-12/D-13/D-14 exactly as locked. The only judgment call (A1 in 19-RESEARCH.md's Assumptions Log — exact insertion point) was already resolved by the plan's `<read_first>` line-number verification (between line ~504 and line 506).

## Deviations from Plan

None - plan executed exactly as written. All acceptance criteria satisfied on first pass; no auto-fixes, no blocking issues, no architectural questions.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required. This plan is documentation-only with no runtime behavior.

## Verification Results

Automated structural gate (from the plan's `<verify>` block) passed:
```
rtk proxy rg -q "^## MCP Server \(cpg mcp\)" README.md && rtk proxy rg -q "KUBECONFIG" README.md && rtk proxy rg -q "TMPDIR" README.md && rtk proxy rg -q "### Harness configuration" README.md && rtk proxy rg -q "### Secrets posture" README.md && rtk proxy rg -q "### Exec-credential-plugin caveat" README.md && rtk proxy rg -q "### Session model" README.md && rtk proxy rg -q "l7-prerequisites" README.md && echo README_STRUCTURE_OK
=> README_STRUCTURE_OK
```

Additional acceptance-criteria checks, all passed:
- Exactly one `## MCP Server (cpg mcp)` heading, positioned after `## Explain policies` (line 438) and before `## Label selection` (now line 557)
- All 8 tool names (`start_session`, `get_status`, `stop_session`, `list_dropped_flows`, `list_policies`, `get_policy`, `get_evidence`, `get_cluster_health`) present in the Markdown table
- Harness JSON block contains `KUBECONFIG`, `PATH`, `TMPDIR` and the phrase "do not inherit"
- All four sub-headings present
- Secrets-posture block contains `Authorization`, the never-captured claim, and the `#l7-prerequisites` cross-link
- Exec-credential block names all three example plugins (`get-token`, `gke-gcloud-auth-plugin`, `kubelogin`) and includes the credential-persistence one-liner
- No aspirational content: `rtk proxy rg -q "HTTP transport|multi-session|SSE transport" README.md` returns no match (verified: exit 1, no hits)

Prose accuracy (per 19-RESEARCH.md's Validation Architecture, this is reviewed downstream by the phase verifier — no automated test for prose is possible) is expected to hold: every factual claim (KUBECONFIG resolution order, TMPDIR honoring, exec-plugin non-interactive hang, credential-persistence side effect) was cross-checked against `.planning/research/PITFALLS.md` Pitfall 8 and `pkg/k8s/client.go`'s documented behavior rather than invented.

## Known Stubs

None - this plan adds documentation only; no code, no data-flow, no rendering components.

## Next Phase Readiness

- SEC-03 fully satisfied; this closes one of Phase 19's four deliverables (SEC-01, SRV-01, SRV-04, SEC-03)
- No blockers for the phase's other plans (structural readonly audit test, e2e stdio lifecycle test) — this plan is independent (wave 1, `depends_on: []`) and touches only `README.md`
- No follow-up work identified; the section documents only what shipped through Phase 18 (D-14 scope guard honored)

## Self-Check: PASSED

- FOUND: README.md
- FOUND: .planning/phases/19-security-hardening-end-to-end-validation/19-03-SUMMARY.md
- FOUND commit: b2562df (Task 1: MCP Server README section)
- FOUND commit: d922eb7 (SUMMARY.md metadata commit)
- Working tree clean after final commit

---
*Phase: 19-security-hardening-end-to-end-validation*
*Completed: 2026-07-21*
