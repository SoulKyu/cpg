---
phase: 20-include-audit-verdict-ingestion
plan: 02
subsystem: hubble-flowsource
tags: [audit-verdict, hubble, flowsource, grpc-filter, cilium, interface-widening]

# Dependency graph
requires:
  - phase: 20-include-audit-verdict-ingestion (plan 01)
    provides: "Aggregator.SetIncludeAudit(bool) / Aggregator.AuditVerdictCount() uint64 (site 5 machinery, already merged into this plan's base)"
provides:
  - "FlowSource interface widened with includeAudit bool 4th param, all 19 compiler-ripple locations landed atomically"
  - "buildFilters (pkg/hubble/client.go, sites 1-3) rebuilt around a single verdicts slice, widened when includeAudit=true"
  - "file.go replay gate (site 4) widened to (DROPPED || (includeAudit && AUDIT)); nonDroppedSkipped counter kept inside the same skip branch"
  - "PipelineConfig.IncludeAudit field threads to source.StreamDroppedFlows and agg.SetIncludeAudit"
  - "AUD-01 warning: bare single post-g.Wait() check on agg.AuditVerdictCount(), mirrors VIS-01 exactly (no dedup map)"
  - "8 TestBuildFilters_* tests (4 byte-identical regression + 4 _WithAudit widened) value-pin buildFilters output"
affects: [20-03, 20-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single-build verdict/filter-value slice reused across FlowFilter literals (avoids the 3x-independent-conditional footgun)"
    - "Compiler-enforced interface ripple landed atomically across 19 locations in one plan (tree compiles at every task-commit boundary)"
    - "VIS-01-style bare single post-run warning (no dedup map) — 2nd instance, AUD-01"

key-files:
  created: []
  modified:
    - pkg/flowsource/source.go
    - pkg/hubble/client.go
    - pkg/flowsource/file.go
    - pkg/hubble/pipeline.go
    - pkg/hubble/pipeline_test.go
    - pkg/session/manager_test.go
    - pkg/flowsource/source_test.go
    - pkg/flowsource/file_test.go
    - pkg/hubble/client_test.go

key-decisions:
  - "Pulled forward client_test.go's 4 buildFilters regression-arg edits (3rd arg `false`) into Task 2's commit instead of Task 3's — Task 1's buildFilters arity change and the FlowSource interface change both land in pkg/hubble, so Task 2's own `go vet`/`go test` verification would fail on a file nominally in Task 3's scope without this fix."

patterns-established:
  - "4th instance of the L7Enabled-shaped diagnostic/threading pattern now fully covers the FlowSource interface layer (sites 1-4), complementing plan 01's aggregator-side site 5"

requirements-completed: [AUD-01]

# Metrics
duration: ~12min
completed: 2026-07-22
---

# Phase 20 Plan 02: FlowSource Interface Widening + Filter Sites 1-4 Summary

**Widened the `FlowSource` interface and gRPC/replay verdict-filter sites 1-4 to accept `Verdict_AUDIT` alongside `Verdict_DROPPED` behind an opt-in `includeAudit` bool, landed the 19-location compiler ripple atomically, threaded `PipelineConfig.IncludeAudit` end-to-end (source + aggregator + AUD-01 warning), and value-pinned `buildFilters`' output with an 8-test byte-identical/widened regression suite.**

## Performance

- **Duration:** ~12 min
- **Completed:** 2026-07-22T10:14:33Z
- **Tasks:** 3 completed
- **Files modified:** 9

## Accomplishments
- `FlowSource.StreamDroppedFlows` gained a 4th `includeAudit bool` parameter; both production implementations (`pkg/hubble/client.go`'s `Client`, `pkg/flowsource/file.go`'s `FileSource`) and all 15 test-double locations (7 implementations + 8 direct call sites) were updated in the same plan so `go build ./...` and every package's test suite compile and pass at each task boundary — no broken intermediate state.
- `buildFilters` (sites 1-3, gRPC whitelist) rebuilt around a single `verdicts := []flowpb.Verdict{flowpb.Verdict_DROPPED}` slice, conditionally `append`-ing `Verdict_AUDIT`, reused across all 3 `FlowFilter` literals — avoids the 3x-independent-conditional footgun the plan explicitly flagged as an anti-pattern.
- `pkg/flowsource/file.go`'s replay gate (site 4) widened to `!(DROPPED || (includeAudit && AUDIT))`, keeping `nonDroppedSkipped.Add(1)` inside the same widened skip branch (existing `with_non_dropped.jsonl` fixture's asserted count of `2` is unaffected — zero AUDIT flows in that fixture).
- `PipelineConfig.IncludeAudit` threads to `source.StreamDroppedFlows(..., cfg.IncludeAudit)` and `agg.SetIncludeAudit(cfg.IncludeAudit)`; the new AUD-01 warning is a bare single check (`cfg.IncludeAudit && stats.FlowsSeen > 0 && agg.AuditVerdictCount() == 0`) placed immediately after the existing VIS-01 block — no dedup map, matching Pattern C exactly.
- `pkg/hubble/client_test.go`'s `TestBuildFilters_*` suite extended from 4 to 8 tests: the original 4 now call `buildFilters(..., false)` and still assert the exact `{DROPPED}` slice (byte-identical proof), and 4 new `_WithAudit` siblings call `buildFilters(..., true)` asserting the exact `{DROPPED, AUDIT}` slice (order pinned) across every returned filter.
- Full repo-wide `go test ./... -race -count=1` (all 12 packages, not just the 3 the plan named) passes clean — confirms no FlowSource/StreamDroppedFlows caller was missed outside the plan's listed 19 locations.

## Task Commits

Each task was committed atomically:

1. **Task 1: Widen FlowSource interface, filter sites 1-4, and thread PipelineConfig.IncludeAudit + AUD-01 warning** - `dceb3ad` (feat)
2. **Task 2: Update all 15 test-double implementations and call sites for the widened interface (compile ripple)** - `4586f97` (test)
3. **Task 3: Pin buildFilters output — byte-identical (flag off) + widened (flag on) tests** - `98d1fd3` (test)

**Plan metadata:** SUMMARY.md commit (this file) — committed immediately after this document is written (worktree mode; STATE.md/ROADMAP.md excluded per orchestrator ownership).

_No REFACTOR commits — all three tasks' implementations matched the target pattern exactly with no cleanup needed._

## Files Created/Modified
- `pkg/flowsource/source.go` — `FlowSource` interface widened with 4th `includeAudit bool` param
- `pkg/hubble/client.go` — `Client.StreamDroppedFlows` widened; `buildFilters` rebuilt around a single reused `verdicts` slice
- `pkg/flowsource/file.go` — `FileSource.StreamDroppedFlows` widened; replay gate (site 4) widened, counter semantics preserved
- `pkg/hubble/pipeline.go` — `PipelineConfig.IncludeAudit` field; call site passes `cfg.IncludeAudit`; `agg.SetIncludeAudit` wiring; AUD-01 warning block
- `pkg/hubble/pipeline_test.go` — 4 test-double `StreamDroppedFlows` implementations gained ignored 4th `_ bool` param
- `pkg/session/manager_test.go` — 2 test-double `StreamDroppedFlows` implementations gained ignored 4th `_ bool` param
- `pkg/flowsource/source_test.go` — `stubSource.StreamDroppedFlows` gained ignored 4th `_ bool` param
- `pkg/flowsource/file_test.go` — 8 direct `StreamDroppedFlows` call sites gained a trailing `false` 4th arg
- `pkg/hubble/client_test.go` — 4 existing `TestBuildFilters_*` extended with `false` 3rd arg (byte-identical proof); 4 new `_WithAudit` siblings added asserting the widened `{DROPPED, AUDIT}` slice

## Decisions Made
- Rebuilt `buildFilters` around one local `verdicts` slice reused in all 3 `FlowFilter` literals rather than 3 independent `if includeAudit` blocks, per the plan's explicit anti-pattern warning (an empty/mismatched conditional per-literal risks "match everything" filters).
- Pulled forward the 4 `client_test.go` regression-arg edits (adding `false` as `buildFilters`' 3rd arg) into Task 2's commit rather than waiting for Task 3 — see Deviations below.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Task 2's own verification failed on a file nominally owned by Task 3**
- **Found during:** Task 2 (test-double compile ripple)
- **Issue:** Task 1 changed two independent signatures in the same commit: the `FlowSource` interface (Task 2's ripple to fix) and `buildFilters`' arity (Task 3's ripple to fix). Both live in package `pkg/hubble`, so Task 2's own stated verification (`go vet ./pkg/hubble/ ...` and `go test ./pkg/hubble/... -race`) failed on `client_test.go`'s 4 `TestBuildFilters_*` calls (`buildFilters(nil, true)` → wants 3 args, got 2) — a file the plan assigns entirely to Task 3.
- **Fix:** Added the `false` 3rd argument to the 4 existing `TestBuildFilters_*` call sites (`AllNamespaces`, `SingleNamespace`, `MultipleNamespaces`, `EmptyNamespaces`) as part of Task 2's commit — this is exactly the "regression" half of Task 3's own planned action, pulled forward only far enough to keep the package compiling and green at the Task 2 boundary. Every existing assertion (`[]flowpb.Verdict{flowpb.Verdict_DROPPED}`) was left unchanged.
- **Files modified:** `pkg/hubble/client_test.go` (4 call sites only; the 4 new `_WithAudit` sibling tests were still added in Task 3, as planned)
- **Verification:** `rtk proxy go build ./...` and `rtk proxy go vet ./pkg/hubble/ ./pkg/flowsource/ ./pkg/session/` both clean; `rtk proxy go test ./pkg/hubble/... ./pkg/flowsource/... ./pkg/session/... -race -count=1` green
- **Committed in:** `4586f97` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Necessary to keep every task-commit boundary compiling/testing green, matching the plan's own stated invariant ("go build ./... never breaks"). No scope creep — Task 3 still delivered its full planned scope (the 4 `_WithAudit` tests); only the order of two already-planned edits to the same file shifted by one task.

## Issues Encountered

**`go build ./...` alone does not exercise the compiler ripple.** The plan's Task 1 acceptance criteria states "`go build ./...` (whole tree) will still FAIL here because test doubles are not yet updated" — in practice, plain `go build` excludes `_test.go` files, so it succeeded immediately after Task 1 (confirmed via a scoped `go vet ./...` check, which did show the expected 3 failures: `file_test.go`, `client_test.go`, `manager_test.go`). This did not block anything — Task 1's own verify command was correctly scoped to non-test packages (`go build ./pkg/flowsource/ ./pkg/hubble/`), and Task 2/3's verify commands correctly use `go vet`/`go test`, which do compile test files. No action needed; noting for future plan-writing calibration.

## User Setup Required

None - no external service configuration required. Pure in-process Go interface/pipeline change with zero new dependencies (`go.mod`/`go.sum` diff confirmed empty; threat model T-20-SC not triggered).

## Next Phase Readiness

- `PipelineConfig.IncludeAudit` now exists and is fully wired (source + aggregator + AUD-01 warning), unblocking plan 20-04's CLI/MCP flag-threading work (`--include-audit` / `include_audit`), which depends on this plan per its own frontmatter (`depends_on: [20-02]`).
- All 5 verdict-filter sites AUD-01 (AC-4) requires are now widened: site 5 (aggregator classification gate) shipped in plan 01; sites 1-4 (gRPC `buildFilters` x3 + replay gate) ship in this plan.
- No blockers. Full repo-wide verification:
  - `rtk proxy go build ./...` — clean
  - `rtk proxy go test ./... -race -count=1` — all 12 packages green (`cmd/cpg`, `pkg/diff`, `pkg/dropclass`, `pkg/evidence`, `pkg/explain`, `pkg/flowsource`, `pkg/hubble`, `pkg/k8s`, `pkg/labels`, `pkg/output`, `pkg/policy`, `pkg/session`)
  - `rtk proxy git diff --stat cmd/cpg/mcp_audit_test.go` — no change (SEC-01 proof untouched)
  - `rtk proxy git diff --stat go.mod go.sum` — no change (zero new deps)
  - `rtk proxy rg -c "func TestBuildFilters" pkg/hubble/client_test.go` — 8 (4 original + 4 `_WithAudit`)

---
*Phase: 20-include-audit-verdict-ingestion*
*Completed: 2026-07-22*
