---
phase: 20-include-audit-verdict-ingestion
plan: 03
subsystem: testing
tags: [go, testify, zaptest-observer, hubble, audit-verdict, flowsource, replay-pipeline]

# Dependency graph
requires:
  - phase: 20-include-audit-verdict-ingestion (plan 01-02)
    provides: "Aggregator.SetIncludeAudit/AuditVerdictCount, PipelineConfig.IncludeAudit, RunPipelineWithSource threading, and the AUD-01 warning block in pipeline.go (all already landed at this plan's base commit)"
provides:
  - "End-to-end regression proof for --include-audit: testdata/flows/with_audit.jsonl fixture (1 DROPPED + 1 AUDIT, same production/api-server workload)"
  - "pkg/hubble/pipeline_audit_test.go: runReplayPipelineAudit helper + 5 tests proving AC-1 (ingested like DROPPED), AC-2 (byte-identical when flag off), AC-3 (warning fires exactly once)"
affects: [20-04, any future phase extending pipeline_l7_test.go-style E2E fixture/warning tests]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Reused the pipeline_l7_test.go template (runReplayPipeline + zaptest/observer log assertions + fixture-driven E2E) verbatim for a 4th boolean-flag feature, confirming the pattern generalizes cleanly across L7Enabled/IgnoreProtocols/IgnoreDropReasons/IncludeAudit"

key-files:
  created:
    - testdata/flows/with_audit.jsonl
    - pkg/hubble/pipeline_audit_test.go
  modified: []

key-decisions:
  - "Declared only the new withAuditFixture const in pipeline_audit_test.go, referencing the existing package-level l4OnlyFixture/emptyFixture consts (already declared in pipeline_l7_test.go) by name — PATTERNS.md Group 6's illustrative code block showed all three consts declared together, which would have redeclared two already-existing package-level identifiers and failed to compile"
  - "Dropped the encoding/json, pkg/evidence, go.uber.org/zap, and go.uber.org/zap/zapcore imports from the new test file — none of the 5 AUDIT tests assert evidence-file content or reference zap/zapcore types directly (the helper's returned *observer.ObservedLogs only requires the zaptest/observer import); PATTERNS.md's task instruction only explicitly called out dropping evidence/encoding-json, but keeping the two zap imports unused would also fail to compile"

requirements-completed: [AUD-01]

# Metrics
duration: ~5min
completed: 2026-07-22
---

# Phase 20 Plan 03: `--include-audit` End-to-End Regression Proof Summary

**New `with_audit.jsonl` fixture + `pipeline_audit_test.go` (helper + 5 tests) proving AUDIT flows generate policies like DROPPED, flag-off output is byte-identical, and the AUD-01 zero-signal warning fires exactly once — all 5 tests green under `-race` on the first run.**

## Performance

- **Duration:** ~5 min
- **Completed:** 2026-07-22T10:23:16Z
- **Tasks:** 2/2 completed
- **Files modified:** 2 (both new files; zero production code touched)

## Accomplishments
- Added `testdata/flows/with_audit.jsonl`: 2-line fixture (1 DROPPED port 8080 + 1 AUDIT port 9090, same `production/api-server` workload) enabling a single test to assert "port 8080 always present, port 9090 present iff flag is set."
- Added `pkg/hubble/pipeline_audit_test.go`: `runReplayPipelineAudit` helper (mirrors `runReplayPipeline`) + 5 tests covering all three AUD-01 acceptance criteria end-to-end through the real `RunPipelineWithSource` pipeline, reusing existing `small.jsonl`/`empty.jsonl` fixtures for 2 of the 5 tests.
- Confirmed via `git diff --stat` against the plan's base commit that the diff touches exactly the two files the plan specified — no production code was modified by this plan.

## Task Commits

Each task was committed atomically:

1. **Task 1: Create the with_audit.jsonl fixture (1 DROPPED + 1 AUDIT, same workload)** - `42abd7a` (test)
2. **Task 2: Create pipeline_audit_test.go — helper + 5 E2E tests mirroring pipeline_l7_test.go** - `c262257` (test)

_Note: this is a pure-test/fixture plan — no `feat` commits were expected or made; both tasks are `test`-typed and directly deliver AC-1/AC-2/AC-3 regression proof, not new production behavior (which landed in plans 01-02)._

## Files Created/Modified
- `testdata/flows/with_audit.jsonl` - 2-line fixture: 1 DROPPED (port 8080) + 1 AUDIT (port 9090), same `production`/`api-server` workload
- `pkg/hubble/pipeline_audit_test.go` - `runReplayPipelineAudit` helper + `TestPipeline_AuditEmpty_FiresWarning`, `TestPipeline_AuditDisabled_NoWarning`, `TestPipeline_AuditDisabled_AuditFlowsIgnored`, `TestPipeline_AuditEnabled_NoFlows_NoWarning`, `TestPipeline_AuditIngested_GeneratedLikeDropped`

## Decisions Made
- Reused the two existing package-level fixture consts (`l4OnlyFixture`, `emptyFixture` from `pipeline_l7_test.go`) by reference instead of redeclaring them in the new file, since Go forbids duplicate top-level identifiers in the same package — only `withAuditFixture` is newly declared.
- Imported only what the new file actually references (`context`, `os`, `path/filepath`, `strings`, `testing`, `time`, `testify`'s `assert`/`require`, `zaptest/observer`, `pkg/flowsource`) rather than copying the L7 template's full import block verbatim, since `encoding/json`/`pkg/evidence`/`go.uber.org/zap`/`go.uber.org/zap/zapcore` are unused in this file and Go treats unused imports as compile errors.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Corrected PATTERNS.md Group 6's fixture-const code block to avoid a compile-time redeclaration**
- **Found during:** Task 2 (writing `pipeline_audit_test.go`)
- **Issue:** PATTERNS.md's "Fixture constants" illustrative snippet shows `l4OnlyFixture` and `emptyFixture` declared alongside the new `withAuditFixture` in the new file. Both `l4OnlyFixture` and `emptyFixture` are already declared as package-level `const`s in `pipeline_l7_test.go` (same `hubble` package) — copying the snippet verbatim would produce a `redeclared in this block` compile error.
- **Fix:** Declared only `const withAuditFixture = "../../testdata/flows/with_audit.jsonl"` in the new file; referenced the two existing consts by name in test bodies without redeclaring them.
- **Files modified:** `pkg/hubble/pipeline_audit_test.go`
- **Verification:** `rtk proxy go build ./...` and `rtk proxy go test ./pkg/hubble/ -count=1 -race` both green.
- **Committed in:** `c262257` (Task 2 commit)

**2. [Rule 1 - Bug] Trimmed unused imports from the L7 template's import block**
- **Found during:** Task 2 (writing `pipeline_audit_test.go`)
- **Issue:** PATTERNS.md's "Imports" section shows the full `pipeline_l7_test.go` import block (including `encoding/json`, `pkg/evidence`, `go.uber.org/zap`, `go.uber.org/zap/zapcore`) as the literal template to reuse. None of the 5 AUDIT tests assert evidence-file content (task instructions explicitly called out dropping `encoding/json`/`pkg/evidence`) or reference `zap`/`zapcore` types directly by name — only `go.uber.org/zap/zaptest/observer` is needed for the helper's `*observer.ObservedLogs` return type. Keeping the unreferenced imports would fail to compile ("imported and not used").
- **Fix:** Imported only `context`, `os`, `path/filepath`, `strings`, `testing`, `time`, `github.com/stretchr/testify/{assert,require}`, `go.uber.org/zap/zaptest/observer`, and `github.com/SoulKyu/cpg/pkg/flowsource`.
- **Files modified:** `pkg/hubble/pipeline_audit_test.go`
- **Verification:** `rtk proxy go build ./...` and `rtk proxy go vet ./pkg/hubble/...` both clean.
- **Committed in:** `c262257` (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (both Rule 1 — compile-correctness fixes against PATTERNS.md's illustrative-but-not-literally-pasteable code blocks)
**Impact on plan:** Both fixes are mechanical Go compile-correctness corrections with zero behavioral impact — the resulting test file matches every test body, assertion, and helper signature PATTERNS.md/the plan's `<action>` specified verbatim. No scope creep.

## Issues Encountered
None — all 5 tests passed on the first run under `-race`, no debugging cycles needed.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- AUD-01's three numbered success criteria (AC-1 ingested-like-DROPPED, AC-2 byte-identical-when-off, AC-3 exactly-once-warning) are now proven end-to-end via automated regression tests, not just unit-level plumbing checks from plans 01-02.
- `git diff --stat` against this plan's base commit confirms zero production code was touched — plan 20-04 (CLI/MCP surface, running in parallel per this plan's frontmatter `wave: 3`) has no risk of merge conflict with this plan's changes.
- No blockers for the orchestrator's post-wave merge or for Phase 21+.

---
*Phase: 20-include-audit-verdict-ingestion*
*Completed: 2026-07-22*
