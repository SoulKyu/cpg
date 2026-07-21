---
phase: 17-session-lifecycle
plan: 01
subsystem: pipeline
tags: [go, hubble, pipeline, sessionstats, mcp-session-hook]

# Dependency graph
requires: []
provides:
  - "PipelineConfig.OnFinal func(SessionStats) nil-safe end-of-run stats hook (D-08)"
  - "Fires-once + nil-safe test coverage (TestRunPipeline_OnFinalFiresOnce, TestRunPipeline_OnFinalNilSafe)"
affects: [17-02-session-lifecycle, pkg/session]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Nil-safe optional func field on a config struct, invoked with a defensive value-copy dereference (cfg.OnFinal(*stats)) to avoid sharing a mutable pointer across a goroutine boundary"

key-files:
  created: []
  modified:
    - pkg/hubble/pipeline.go
    - pkg/hubble/pipeline_test.go

key-decisions:
  - "Call site placed immediately after the last stats.* population line (InfraDropsByReason) and before the VIS-01 gate, per plan's exact anchor — no reordering of surrounding logic"
  - "Interface-first sequencing followed literally: Task 1 defines the field+call site (feat commit), Task 2 adds coverage (test commit) — this is NOT classic test-first RED/GREEN; the plan's own objective states this explicitly (\"define and test the hook now\")"
  - "requirements mark-complete SESS-04 was deliberately skipped this plan (see Deviations) — the literal requirement text (stop_session tool + final summary returned to an LLM caller) is not satisfied until plan 17-04 lands"

patterns-established:
  - "hw/ew nil-safety convention extended to func-field hooks: `if cfg.OnFinal != nil { cfg.OnFinal(*stats) }`"

requirements-completed: []  # SESS-04 intentionally NOT marked complete by this plan — see Deviations

# Metrics
duration: ~8min
completed: 2026-07-21
---

# Phase 17 Plan 01: OnFinal Session Stats Hook Summary

**Nil-safe `PipelineConfig.OnFinal func(SessionStats)` hook fired exactly once at end-of-run with fully populated stats, landed and tested as the interface-first foundation for 17-02's session manager.**

## Performance

- **Duration:** ~8 min
- **Completed:** 2026-07-21T04:16:46Z
- **Tasks:** 2 completed
- **Files modified:** 2

## Accomplishments
- `PipelineConfig.OnFinal func(SessionStats)` field added, doc-commented per D-08, positioned after `Stdout`
- Nil-checked call site (`if cfg.OnFinal != nil { cfg.OnFinal(*stats) }`) fires exactly once, after all seven `stats.*` fields are populated and before the VIS-01 gate / `ew.finalize` / `hw.finalize`
- Value-copy dereference (`*stats`, never the live pointer) makes the cross-goroutine handoff to the future MCP-mode session manager race-free by construction
- Two new tests prove the contract: fires-once with fully populated stats, and no-op/no-panic when left nil (every existing CLI path)
- Zero behavior change to any existing CLI path — full `pkg/hubble` suite green under `-race`: 119 tests (117 pre-existing + 2 new)

## Task Commits

Each task was committed atomically:

1. **Task 1: Add nil-safe OnFinal hook field + call site to pipeline.go** - `507d0b3` (feat)
2. **Task 2: Add fires-once + nil-safe OnFinal tests to pipeline_test.go** - `d1e43a4` (test)

**Plan metadata:** committed separately by the orchestrator after worktree merge (worktree-mode executor scope excludes STATE.md/ROADMAP.md).

_Note: tasks were tagged `tdd="true"` in the plan but executed in the plan's explicit interface-first order (define, then test) rather than literal RED-then-GREEN — see Decisions above._

## Files Created/Modified
- `pkg/hubble/pipeline.go` - Added `OnFinal func(SessionStats)` field to `PipelineConfig` (after `Stdout`) and the nil-guarded `cfg.OnFinal(*stats)` call site in `RunPipelineWithSource`, between the stats-population block and the VIS-01 gate
- `pkg/hubble/pipeline_test.go` - Added `TestRunPipeline_OnFinalFiresOnce` (2-flow source, closure counter + captured stats, asserts `called == 1` and `captured.FlowsSeen == 2`) and `TestRunPipeline_OnFinalNilSafe` (OnFinal left nil, asserts no panic / no error)

## Decisions Made
- Followed the plan's exact insertion anchors verbatim (no reordering of `ew.finalize`/`hw.finalize`/the VIS-01 gate/the stdout summary path)
- Split commits as `feat` (Task 1, production code) then `test` (Task 2, coverage) to match the plan's stated interface-first rationale rather than forcing a literal TDD RED-first commit ordering that the plan itself does not ask for
- Skipped `requirements mark-complete SESS-04` — see Deviations

## Deviations from Plan

### Documentation-accuracy deviation (not a Rule 1-4 code deviation)

**1. Skipped `requirements mark-complete SESS-04`**
- **Found during:** Pre-SUMMARY requirements review
- **Issue:** This plan's frontmatter lists `requirements: [SESS-04]`, and the standard workflow instructs marking all frontmatter requirement IDs complete after the plan finishes. However, SESS-04's literal text (`.planning/REQUIREMENTS.md` line 23) is *"LLM can `stop_session(session_id)`: pipeline context cancelled, artifacts finalized (cluster-health.json, session stats), final summary returned"* — an end-to-end MCP tool behavior. Plan 17-01 only adds the internal `pkg/hubble` hook that a future session manager will consume; no `stop_session` tool exists yet. Cross-checked `17-03-PLAN.md` and `17-04-PLAN.md` frontmatter: both *also* list `requirements: [..., SESS-04, ...]`, confirming SESS-04 is intentionally tracked across three contributing plans, with the actual LLM-facing behavior landing in 17-04 (where `mcp_tools.go` registers `stop_session`).
- **Action:** Verified `requirementsMarkComplete` (gsd-sdk `sdk/src/query/roadmap.ts`) is a pure mechanical checkbox/table text substitution with no cross-plan awareness — calling it now would flip `.planning/REQUIREMENTS.md` to "Complete" for SESS-04 while the described behavior does not exist, misrepresenting project state to anyone (or automation) reading the traceability table.
- **Fix:** Did not call `requirements mark-complete` for SESS-04 in this plan. Left `requirements-completed: []` in this SUMMARY's frontmatter. Recommend the plan that actually lands the `stop_session` tool (17-04, or 17-03 if the manager-level "final summary" shape is judged sufficient) perform the mark-complete call.
- **Files modified:** None (documentation-accuracy judgment call, not a code change)
- **Verification:** Confirmed via direct read of `.planning/phases/17-session-lifecycle/{17-02,17-03,17-04}-PLAN.md` frontmatter and `.planning/REQUIREMENTS.md` lines 21-26, 82-92
- **Committed in:** N/A (no REQUIREMENTS.md change made)

---

**Total deviations:** 1 (documentation-accuracy judgment call, no code impact)
**Impact on plan:** None on code/tests — plan executed exactly as written for both tasks. The only departure from the generic executor workflow is withholding a premature "Complete" status on a requirement this plan only partially fulfills.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Known Stubs
None - no stub patterns, placeholder values, or unwired data introduced by this plan.

## Threat Flags
None - this plan's only new surface (the `OnFinal` hook) is exactly what the plan's own `<threat_model>` already covers (T-17-01-01 data race, mitigated via value-copy dereference; T-17-01-02 nil-func panic, mitigated via the nil guard). No additional undocumented surface was introduced.

## Next Phase Readiness
- `PipelineConfig.OnFinal func(SessionStats)` is landed, doc-commented, and proven (fires-once + nil-safe) — 17-02's `pkg/session/pipeline_config.go` can now wire `cfg.OnFinal = func(s hubble.SessionStats){ session.final.Store(&s) }` against a tested contract, exactly as this plan's `<output>` instruction specified
- No blockers for 17-02 (wave 2, `depends_on: [17-01]`)
- SESS-04 traceability remains `Pending` in `.planning/REQUIREMENTS.md` — intentional, see Deviations; will need `requirements mark-complete SESS-04` once the full `stop_session` behavior lands (17-04)

---
*Phase: 17-session-lifecycle*
*Completed: 2026-07-21*

## Self-Check: PASSED

- FOUND: pkg/hubble/pipeline.go
- FOUND: pkg/hubble/pipeline_test.go
- FOUND: .planning/phases/17-session-lifecycle/17-01-SUMMARY.md
- FOUND: commit 507d0b3 (Task 1)
- FOUND: commit d1e43a4 (Task 2)
- FOUND: commit 7f7f64c (SUMMARY.md)
- FOUND: `OnFinal func(SessionStats)` field declaration in pipeline.go
- FOUND: `TestRunPipeline_OnFinalFiresOnce` in pipeline_test.go
- FOUND: `TestRunPipeline_OnFinalNilSafe` in pipeline_test.go
