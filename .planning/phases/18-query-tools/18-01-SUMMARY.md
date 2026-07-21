---
phase: 18-query-tools
plan: 01
subsystem: api
tags: [go, refactor, package-promotion, cli, explain, mcp-prep]

# Dependency graph
requires:
  - phase: v1.1 (pkg/evidence + cpg explain)
    provides: evidence.PolicyEvidence/RuleEvidence/PeerRef/L7Ref schema and the original cmd/cpg explain_filter.go/explain_render.go logic this plan promotes verbatim
provides:
  - "pkg/explain package: exported Filter (+ Match), Output, RenderJSON/RenderText/RenderYAML, ParsePeerLabel"
  - "thinned cmd/cpg/explain.go calling into pkg/explain (buildFilter returns explain.Filter)"
  - "shared-renderer foundation for QRY-03's byte-identical get_evidence MCP tool contract"
affects: [18-04 (get_evidence MCP tool handler — imports pkg/explain directly)]

# Tech tracking
tech-stack:
  added: []
  patterns: ["pkg/flowsource-style code promotion: unexported cmd/cpg type -> exported pkg/* type, mechanical rename only, tests relocated verbatim (commit 0aba62d precedent)"]

key-files:
  created:
    - pkg/explain/doc.go
    - pkg/explain/filter.go
    - pkg/explain/render.go
    - pkg/explain/filter_test.go
    - pkg/explain/render_test.go
  modified:
    - cmd/cpg/explain.go
    - cmd/cpg/explain_test.go

key-decisions:
  - "Mechanical move-and-rename mirroring the pkg/flowsource v1.1 promotion precedent (commit 0aba62d) — zero logic changes, only explainFilter->Filter / match->Match / explainOutput->Output / renderJSON/Text/YAML exported renames"
  - "ParsePeerLabel exported (not left unexported as originally worded in some planning notes) since both cmd/cpg's buildFilter and the future get_evidence handler (18-04) call it across the package boundary"
  - "writeRule/peerSummary/fmtEndpoint/colorizer + ansi consts moved along but stay unexported inside pkg/explain — nothing outside the package calls them"

patterns-established:
  - "Promoted-package pattern: pkg/explain package doc comment states the shared-core purpose ('shared core behind cpg explain and the MCP get_evidence tool'); caller thins to import + call; tests move with only type/method renames, assertions unchanged"

requirements-completed: [QRY-03]

# Metrics
duration: ~10min
completed: 2026-07-21
---

# Phase 18 Plan 01: Promote pkg/explain Summary

**Moved `cpg explain`'s filter/render logic into an importable `pkg/explain` package (exported `Filter.Match`, `Output`, `RenderJSON/RenderText/RenderYAML`, `ParsePeerLabel`), thinning `cmd/cpg/explain.go` to call it — byte-identical CLI output, ready for the get_evidence MCP tool to reuse directly.**

## Performance

- **Duration:** ~10 min
- **Started:** 2026-07-21T13:32:30Z (phase execution start)
- **Completed:** 2026-07-21T13:43:44Z
- **Tasks:** 2 completed
- **Files modified:** 9 (5 created, 2 modified, 3 deleted)

## Accomplishments
- `pkg/explain` package created with exported `Filter`/`Filter.Match`, `Output`, `RenderJSON`/`RenderText`/`RenderYAML`, and `ParsePeerLabel` — a 1:1 mechanical promotion of `cmd/cpg/explain_filter.go` + `explain_render.go`, zero logic changes
- `cmd/cpg/explain.go` thinned: `buildFilter` now returns `explain.Filter` and calls `explain.ParsePeerLabel`; `runExplain` calls `filter.Match(r)` and `explain.RenderJSON/RenderText/RenderYAML`; cobra wiring, target resolution, and the `errors.Is(err, fs.ErrNotExist)` not-found branch are untouched
- All 9 filter unit tests + 11 pure render tests relocated verbatim (only type/method names changed) into `pkg/explain/filter_test.go` / `render_test.go`; the 5 cobra/`newExplainCmd()` end-to-end tests stay in `cmd/cpg/explain_test.go`
- Full TDD RED→GREEN cycle for both tasks, verified with a whole-module `go test ./... -race -count=1` pass across all 12 packages (no collateral regressions)

## Task Commits

Each task was committed atomically, following the plan's TDD gate for both tasks:

1. **Task 1 RED: relocated filter/render tests** - `3215c25` (test) — `pkg/explain/filter_test.go` + `render_test.go` added referencing not-yet-existing `Filter`/`Output`/`Render*`; confirmed build failure (`undefined: Filter`, etc.)
2. **Task 1 GREEN: pkg/explain implementation** - `667f5ba` (feat) — `pkg/explain/doc.go`, `filter.go`, `render.go` added; `go test ./pkg/explain/... -race -count=1` → 19/19 pass
3. **Task 2 RED: delete promoted-out files, align explain_test.go** - `9dde6e5` (test) — deleted `cmd/cpg/explain_filter.go`, `explain_render.go`, `explain_filter_test.go`; trimmed `explain_test.go` to the cobra e2e tests only, updated `explainOutput`→`explain.Output`; confirmed `cmd/cpg` build failure (`undefined: renderJSON`, `explainFilter`, `parsePeerLabel`, etc.)
4. **Task 2 GREEN: thin cmd/cpg/explain.go** - `ca5b920` (feat) — imported `pkg/explain`, updated all call sites; `go build ./...` and `go test ./cmd/cpg/... ./pkg/explain/... -race -count=1` pass

_No REFACTOR commits: this was a direct mechanical move (`gofmt -l` and `go vet` both clean at every GREEN step), nothing left to clean up._

## Files Created/Modified
- `pkg/explain/doc.go` - package doc comment (shared core behind `cpg explain` and the future `get_evidence` MCP tool)
- `pkg/explain/filter.go` - exported `Filter` struct + `Match` method + `ParsePeerLabel` (was `explainFilter`/`match`/`parsePeerLabel` in `cmd/cpg/explain_filter.go`)
- `pkg/explain/render.go` - exported `Output` type + `RenderJSON`/`RenderText`/`RenderYAML`; unexported `writeRule`/`peerSummary`/`fmtEndpoint`/`colorizer`/ansi consts moved along (was `cmd/cpg/explain_render.go`)
- `pkg/explain/filter_test.go` - the 9 relocated filter unit tests (was `cmd/cpg/explain_filter_test.go`, deleted)
- `pkg/explain/render_test.go` - the 11 relocated pure render tests + `sampleEvidence`/`httpRuleEvidence`/`dnsRuleEvidence` fixtures (relocated from `cmd/cpg/explain_test.go`)
- `cmd/cpg/explain.go` - thinned: imports `pkg/explain`; `buildFilter` returns `explain.Filter`; call sites use `explain.RenderJSON/RenderText/RenderYAML`, `explain.ParsePeerLabel`, `filter.Match`
- `cmd/cpg/explain_test.go` - trimmed to the 5 cobra/`newExplainCmd()` end-to-end tests; `sampleEvidence`/`seedEvidence` kept (still needed by the e2e tests); `TestExplainJSONOutput`'s `explainOutput` reference updated to `explain.Output`
- `cmd/cpg/explain_filter.go` - deleted (logic promoted to `pkg/explain/filter.go`)
- `cmd/cpg/explain_render.go` - deleted (logic promoted to `pkg/explain/render.go`)
- `cmd/cpg/explain_filter_test.go` - deleted (relocated to `pkg/explain/filter_test.go`)

## Decisions Made
- Followed the plan's mechanical-move instructions exactly — no design decisions required beyond the two already locked by the plan (`ParsePeerLabel` exported; `writeRule`/`peerSummary`/`fmtEndpoint`/`colorizer` stay unexported within `pkg/explain`).
- Kept `httpRule`/`dnsRule` fixture-helper names in `pkg/explain/filter_test.go` unchanged from the original (caught and reverted an unnecessary rename to `httpFilterRule`/`dnsFilterRule` I introduced during drafting — there was no actual name collision with `render_test.go`'s `httpRuleEvidence`/`dnsRuleEvidence`, so the plan's "assertions unchanged" instruction applies to helper names too).

## Deviations from Plan

None in the code itself - plan executed exactly as written. The one self-correction (fixture-helper naming, noted above) was caught and fixed before committing, so it never reached a commit.

### Process correction (not a code deviation)

**REQUIREMENTS.md QRY-03 checkbox left as `[ ] Pending`, not marked complete.**
- **Found during:** state-update step (after SUMMARY self-check)
- **Issue:** This plan's frontmatter declares `requirements: [QRY-03]`, and the generic state-update instructions say to mark all of a plan's declared requirements complete in `.planning/REQUIREMENTS.md`. I ran `gsd-sdk query requirements.mark-complete QRY-03`, which flipped the checkbox to `[x]` and the traceability table to `Complete`. Re-reading REQUIREMENTS.md's actual QRY-03 text — "LLM can `get_evidence(session_id, …filters)` for per-rule flow attribution... paginated" — that describes the MCP tool handler itself, which does not exist yet: no `get_evidence`/`mcp_query_evidence.go` file exists in `cmd/cpg` at this commit. `18-04-PLAN.md` (not yet executed) also declares `requirements: [QRY-03, QRY-05]` and is the plan that actually implements the handler. This plan's own `<success_criteria>` says it explicitly: "QRY-03's shared-renderer **foundation** is in place for the get_evidence tool (plan 18-04)" — foundation, not completion.
- **Fix:** Reverted `.planning/REQUIREMENTS.md` via `git checkout -- .planning/REQUIREMENTS.md` (targeted single-file revert, no blanket reset) before it was committed. QRY-03 stays `[ ] Pending` / `Phase 18 | Pending` until 18-04 lands and its executor runs the mark-complete step for real.
- **Files affected:** `.planning/REQUIREMENTS.md` (reverted, never committed — working tree is clean on this file)
- **Note for 18-04's executor:** when 18-04 completes, run `gsd-sdk query requirements.mark-complete QRY-03` then (and QRY-05, shared with 18-03).

## Issues Encountered
None.

## User Setup Required

None - no external service configuration required. Pure intra-repo code promotion, no new dependencies added (confirmed via the plan's own T-18-SC threat-register entry: "No new external packages added in this plan").

## Next Phase Readiness
- `pkg/explain` is ready for 18-04's `get_evidence` MCP tool handler to import directly — `explain.Filter.Match`, `explain.RenderJSON`, and `explain.ParsePeerLabel` are all exported and byte-identical to the pre-promotion CLI behavior (QRY-03's foundation is in place).
- Full `go test ./... -race -count=1` green across all 12 packages; no blockers for 18-02/18-03/18-04/18-05.

## Self-Check: PASSED

All created files verified present, all deleted files verified absent, all 5 commit hashes verified present in `git log`:
- `pkg/explain/{doc,filter,render,filter_test,render_test}.go` — FOUND
- `cmd/cpg/{explain.go,explain_test.go}` — FOUND (modified)
- `cmd/cpg/{explain_filter.go,explain_render.go,explain_filter_test.go}` — CONFIRMED DELETED
- Commits `3215c25`, `667f5ba`, `9dde6e5`, `ca5b920`, `8281adc` — all FOUND

---
*Phase: 18-query-tools*
*Completed: 2026-07-21*
