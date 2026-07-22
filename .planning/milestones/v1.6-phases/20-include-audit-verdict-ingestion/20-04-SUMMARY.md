---
phase: 20-include-audit-verdict-ingestion
plan: 04
subsystem: cli
tags: [cobra, mcp, hubble, go-sdk, jsonschema]

# Dependency graph
requires:
  - phase: 20-include-audit-verdict-ingestion (plans 20-01, 20-02)
    provides: hubble.PipelineConfig.IncludeAudit field, Aggregator.SetIncludeAudit/AuditVerdictCount, FlowSource interface's 4th includeAudit param, filter sites 1-5
provides:
  - "--include-audit cobra flag registered on cpg generate and cpg replay, reaching PipelineConfig.IncludeAudit"
  - "include_audit MCP arg on start_session, threaded StartArgs -> buildPipelineConfig -> PipelineConfig.IncludeAudit"
  - "README Flags table + MCP start_session row documenting both surfaces, wording synced with the mcp_tools.go jsonschema tag"
affects: [22-bootstrap-artifact-generation, 23-audit-window-management]

# Tech tracking
tech-stack:
  added: []
  patterns: ["Boolean-flag 3-hop threading (Pattern A): cobra.Bool -> commonFlags/generateFlags -> PipelineConfig literal, 4th instance of the L7Enabled/IgnoreProtocols/IgnoreDropReasons shape", "MCP arg 3-hop threading: startSessionArgs (json+jsonschema tags) -> session.StartArgs -> buildPipelineConfig -> PipelineConfig, mirrors L7/TLS/ClusterDedup exactly"]

key-files:
  created: []
  modified:
    - cmd/cpg/commonflags.go
    - cmd/cpg/commonflags_test.go
    - cmd/cpg/generate.go
    - cmd/cpg/replay.go
    - cmd/cpg/mcp_tools.go
    - pkg/session/session.go
    - pkg/session/pipeline_config.go
    - pkg/session/pipeline_config_test.go
    - README.md
    - .gitignore

key-decisions:
  - "Added /cpg to .gitignore — this plan's own Task 1 <verify> command (`go build ./cmd/cpg/`, a single-main-package target) writes an executable to the repo root that the existing bin/-only .gitignore does not cover; left uncommitted/untracked would violate the no-stray-generated-files rule"
  - "README's 'Offline replay' shared-flags sentence extended beyond the plan's literal Task 3 instructions (Flags table row + MCP row only) to also list --include-audit among generate-shared flags and caveat the 'non-DROPPED verdicts are skipped' sentence — the flag genuinely changes replay's skip behavior and the existing prose would otherwise silently misdescribe it once merged"

patterns-established: []

requirements-completed: [AUD-01]

# Metrics
duration: ~18min
completed: 2026-07-22
---

# Phase 20 Plan 04: `--include-audit` CLI/MCP Surface Summary

**`--include-audit` cobra flag (generate/replay) and `include_audit` MCP arg (start_session) both reach `hubble.PipelineConfig.IncludeAudit`, default false, documented in README, with `cmd/cpg/mcp_audit_test.go` verified byte-identical (SEC-01 untouched).**

## Performance

- **Duration:** ~18 min
- **Started:** ~2026-07-22T10:10:00Z (estimated)
- **Completed:** 2026-07-22T10:27:44Z
- **Tasks:** 3/3
- **Files modified:** 10 (9 plan-scoped + `.gitignore`)

## Accomplishments
- `--include-audit` registered once in `commonflags.go` (struct field, `f.Bool(...)`, `GetBool` parse) and consumed identically by both `generate` and `replay` — no preflight added, plain bool passthrough exactly like the plan specified.
- `include_audit` MCP arg added to `startSessionArgs` with a `jsonschema` description that is byte-identical to the README prose, threaded through `session.StartArgs` and `buildPipelineConfig` to `hubble.PipelineConfig.IncludeAudit` — zero new validation code (bool passthrough, same as `L7`/`TLS`/`ClusterDedup`).
- README's Flags table (Filtering block), the "Offline replay" shared-flags line, and the MCP `start_session` tool row all document the new surface; `cmd/cpg/mcp_audit_test.go` confirmed byte-for-byte unchanged (SEC-01 readonly proof intact, zero new write verbs).

## Task Commits

Each task was committed atomically:

1. **Task 1: CLI threading — register --include-audit and set it on generate + replay PipelineConfig** - `4e397d1` (feat)
2. **Task 2: MCP threading — include_audit arg through StartArgs -> buildPipelineConfig -> PipelineConfig** - `e49e93d` (feat)
3. **Task 3: Document --include-audit (README Flags table) and include_audit (README MCP section)** - `a849b27` (docs)

_No TDD tasks in this plan (autonomous, straight `type="auto"` tasks); each is a single commit._

## Files Created/Modified
- `cmd/cpg/commonflags.go` - `includeAudit bool` field, `f.Bool("include-audit", ...)` registration, `GetBool` parse
- `cmd/cpg/commonflags_test.go` - `TestIncludeAuditFlagParses` (generate/replay parse-true + default-false subtests)
- `cmd/cpg/generate.go` - `IncludeAudit: f.includeAudit,` in the `hubble.PipelineConfig{...}` literal (no preflight added)
- `cmd/cpg/replay.go` - `IncludeAudit: f.includeAudit,` in the `hubble.PipelineConfig{...}` literal
- `cmd/cpg/mcp_tools.go` - `startSessionArgs.IncludeAudit` (json+jsonschema tags) + `IncludeAudit: args.IncludeAudit,` in the `session.StartArgs{...}` literal passed to `mgr.Start`
- `pkg/session/session.go` - `StartArgs.IncludeAudit bool` field
- `pkg/session/pipeline_config.go` - `IncludeAudit: args.IncludeAudit,` in `buildPipelineConfig`'s `hubble.PipelineConfig{...}` literal
- `pkg/session/pipeline_config_test.go` - extended `TestBuildPipelineConfig` with `IncludeAudit: true` + assertion; new `TestBuildPipelineConfig_IncludeAuditDefaultsFalse`
- `README.md` - `--include-audit` Flags-table row, replay shared-flags list + skip-description caveat, `start_session` MCP row documenting `include_audit`
- `.gitignore` - `/cpg` entry (see Deviations)

## Decisions Made
- **Doc wording kept in lockstep (Pitfall 6):** the `jsonschema` string on `startSessionArgs.IncludeAudit` and the README `start_session` row prose use the identical phrase "ingest Verdict_AUDIT flows alongside DROPPED (opt-in; default preserves pre-v1.6 DROPPED-only behavior)" — written in the same plan to avoid a third drifting copy, per RESEARCH.md Pitfall 6.
- **No preflight for `--include-audit`:** unlike `--l7`'s `maybeRunL7Preflight`, `--include-audit` is a plain opt-in bool with no cluster pre-flight check — confirmed `maybeRunL7Preflight` call sites in `generate.go` are unchanged.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking/hygiene] Stray `cpg` binary from the plan's own Task 1 verify command**
- **Found during:** Task 1 verification (`rtk proxy go build ./cmd/cpg/`)
- **Issue:** `go build ./cmd/cpg/` is a single-main-package build target, so Go writes an executable named `cpg` to the current directory (the repo root) rather than discarding the build output. The repo's `.gitignore` only covers `bin/` (the `make build` output path via `-o bin/cpg`), so this stray binary showed up as an untracked file after running the plan's mandated `<verify>` command.
- **Fix:** Deleted the stray binary and added `/cpg` to `.gitignore` so any future run of this exact plan's verify command (by this executor, CI, or a human) doesn't leave an untracked/accidentally-committable binary at the repo root.
- **Files modified:** `.gitignore`
- **Verification:** `git status --short` clean after the fix; re-ran `go build ./cmd/cpg/` to confirm the binary is now gitignored.
- **Committed in:** `4e397d1` (Task 1 commit)

**2. [Rule 2 - Doc completeness] `--include-audit` added to replay's shared-flags list and skip-description**
- **Found during:** Task 3 (README documentation)
- **Issue:** The plan's Task 3 instructions scoped README changes to "Flags table row + MCP arg" only. While implementing, the "Offline replay" section's existing sentence — "Flags shared with `generate` (...) work identically. Non-DROPPED verdicts and malformed lines are skipped..." — would have become misleading once `--include-audit` shipped: it lists every other shared commonFlags field but omits the new one, and its "skipped" claim is no longer universally true once `--include-audit` is set (AUDIT verdicts are then admitted, not skipped).
- **Fix:** Added `--include-audit` to the shared-flags parenthetical and qualified the skip sentence with "(unless `--include-audit` also admits AUDIT)".
- **Files modified:** `README.md`
- **Verification:** Manual read — no contradiction between the Flags table, the replay section, and the MCP row; `rtk proxy rg -n "include-audit|include_audit" README.md` shows all three locations.
- **Committed in:** `a849b27` (Task 3 commit)

---

**Total deviations:** 2 auto-fixed (1 blocking/hygiene, 1 doc completeness)
**Impact on plan:** Both fixes are minor and directly scoped to what this plan touched (the plan's own verify command in the first case, the exact prose block Task 3 edits in the second). No architectural changes, no scope creep beyond documentation accuracy.

## Issues Encountered
None — all three tasks matched their `<acceptance_criteria>` on first implementation; no debugging required.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- AUD-01's full CLI+MCP surface is complete: `--include-audit` / `include_audit` both reach `PipelineConfig.IncludeAudit`, default false, tests green (`cmd/cpg` and `pkg/session` full package suites pass under `-race`), `go build ./...` clean, `cmd/cpg/mcp_audit_test.go` unmodified.
- Phase 20 (waves 1-3: pipeline behavior, FlowSource widening, CLI/MCP surface) is now feature-complete pending the orchestrator's cross-wave verification and STATE.md/ROADMAP.md/REQUIREMENTS.md updates.
- No blockers for Phase 21 (COMPAT-01/02/03) or Phase 22 (AUD-02 bootstrap artifacts) — this plan introduced no architectural changes that would affect their planning.

---
*Phase: 20-include-audit-verdict-ingestion*
*Completed: 2026-07-22*

## Self-Check: PASSED

- All 11 claimed files verified present on disk (10 code/doc files + this SUMMARY.md).
- All 4 commit hashes (`4e397d1`, `e49e93d`, `a849b27`, `a380c71`) verified present in `git log --oneline --all`.
- Content spot-checks confirmed: `includeAudit` field/flag/parse in `commonflags.go`; `IncludeAudit: f.includeAudit` in both `generate.go`/`replay.go`; `include_audit` jsonschema tag in `mcp_tools.go`; `StartArgs.IncludeAudit` in `session.go`; `IncludeAudit: args.IncludeAudit` in `pipeline_config.go`; both `include-audit` and `include_audit` present in `README.md`.
