---
phase: 18-query-tools
plan: 05
subsystem: api
tags: [mcp, go-sdk, jsonschema, pagination, cluster-health, evidence, dropclass]

# Dependency graph
requires:
  - phase: 18-query-tools (18-02)
    provides: pkg/hubble.ReadClusterHealth/ClusterHealthReport/HealthDropJSON (exported types, ByWorkload key format)
  - phase: 18-query-tools (18-03)
    provides: cmd/cpg/mcp_query.go composition root (registerQueryTools), resolveSession helper, availableAfterStopMarker shared type, D-16 error-return convention
  - phase: 18-query-tools (18-04)
    provides: mustQuerySchema[T] enum-schema helper, opaque boundary-key cursor codec (encodeCursor/decodeCursor), generic paginate() helper, flow-scale defaultFlowLimit/maxFlowLimit consts
provides:
  - "list_dropped_flows (QRY-01): two-section composed view (samples[] from capped evidence FlowSamples, aggregates[] from cluster-health.json), never synthesized from each other"
  - "D-02 aggregates-half available_after_stop marker while capturing; samples half served live in both states"
  - "AND-combined namespace/workload/dropclass/direction filters, with direction applying to samples only (Pitfall 2) and dropclass computed via classifyDropReasonName (reverses flowpb.DropReason_value + dropclass.Classify)"
  - "Combined samples+aggregates pagination as one set via buildCombinedItems + per-(namespace,workload)-group boundary-key index, reusing 18-04's paginate/cursor primitives verbatim"
  - "Final Phase 18 integration test: exactly 8 tools (3 session + 5 query), QRY-05 annotation/schema contract centrally proven, D-16 error-text discipline extended to all 5 query tools"
  - "REQUIREMENTS.md: QRY-01 and QRY-05 marked complete — closes Phase 18's full QRY-01..05 requirement set"
affects: [19-security-hardening-e2e]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Combined-view pagination: two structurally different sources (evidence FlowSamples + cluster-health ByWorkload rows) merged into one sort.SliceStable-ordered slice, with a per-(namespace,workload)-group boundary-key index (never a global absolute offset) so list_dropped_flows' single pagination window degrades gracefully under D-06's best-effort-rescan model exactly like get_evidence's single-file index does"
    - "Reversing a persisted enum-name string back through its own generated _value map (flowpb.DropReason_value) to re-run the canonical classifier (dropclass.Classify) — the exact inverse of how the string was produced (f.GetDropReasonDesc().String()) — rather than hand-maintaining a second name-to-class table"
    - "Guarding an independently-optional filter pair through an existing both-required validator (evidence.ValidatePolicyRef) via a safe placeholder for the unset argument, instead of duplicating its traversal-check logic in a new function"

key-files:
  created:
    - cmd/cpg/mcp_query_flows.go
    - cmd/cpg/mcp_query_flows_test.go
  modified:
    - cmd/cpg/mcp_query.go
    - cmd/cpg/mcp_query_tools_test.go
    - cmd/cpg/mcp_query_pagination.go
    - .planning/REQUIREMENTS.md
    - .planning/phases/18-query-tools/deferred-items.md

key-decisions:
  - "Combined items assigned a per-(namespace,workload)-group index (not a global sequential index) so the shared paginateBoundaryKey scan-based resumption degrades gracefully if the file set shifts mid-capture, matching get_evidence's established single-file-index precedent"
  - "dropclass filtering on the samples half computed via classifyDropReasonName (flowpb.DropReason_value reverse lookup + dropclass.Classify), never a hand-rolled second classification table"
  - "validateFilterComponent reuses evidence.ValidatePolicyRef with a safe '_' placeholder for the unset argument, since namespace/workload are independently optional here (unlike get_policy/get_evidence's always-both-required contract) and pkg/evidence was out of this plan's file scope"
  - "Aggregates-half fs.ErrNotExist while stopped is treated as zero rows (not an error) — mirrors QRY-04's Pitfall-1 correction; any other ReadClusterHealth error (malformed/wrong schema_version) still propagates as isError"

patterns-established:
  - "buildCombinedItems: samples-before-aggregates within a matching (namespace,workload) pair, sort.SliceStable preserving each half's own already-deterministic internal order as the tiebreaker — the template for any future third composed-view source"

requirements-completed: [QRY-01, QRY-05]

duration: ~20min
completed: 2026-07-21
---

# Phase 18 Plan 05: list_dropped_flows Composed View + Phase Close Summary

**list_dropped_flows (QRY-01) ships the honest two-section samples[]/aggregates[] composed view with combined pagination, dropclass/direction enum schema, and closes Phase 18 with the final 8-tool integration test (QRY-05 fully satisfied).**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-21 (worktree reset to phase-18 base, then execution)
- **Completed:** 2026-07-21T17:16:46+02:00
- **Tasks:** 2 completed
- **Files modified:** 7 (2 created, 5 modified)

## Accomplishments

- `cmd/cpg/mcp_query_flows.go`: `list_dropped_flows` (QRY-01) — the composed view's samples half walks `<evidenceDir>/<hash>/<ns>/<wl>.json` via `evidence.Reader`, flattening every `RuleEvidence.Samples[]` into a `DroppedFlowSample` enriched with the parent rule's namespace/workload/direction/peer/port/protocol; the aggregates half flattens `pkg/hubble.ReadClusterHealth`'s `Drops[].ByWorkload` into per-(namespace,workload,reason,class,count) rows — the two halves are never synthesized from each other (D-01)
- D-02: aggregates carries the shared `availableAfterStopMarker` while `state=="capturing"`; samples are served live in both states (evidence files are atomic on disk)
- D-03 filters, AND-combined: namespace/workload match both halves via the shared `policyTargetEndpoint` identity; dropclass matches each half's own class (samples via a new `classifyDropReasonName` reversing `flowpb.DropReason_value` through `dropclass.Classify`, aggregates via `HealthDropJSON.Class` directly); direction narrows samples ONLY — aggregates are always returned unfiltered, documented explicitly (Pitfall 2/T-18-05-04)
- D-04/D-07: the combined, filtered samples+aggregates set is paginated as ONE window via a new `buildCombinedItems` assembly (deterministic sort + per-(namespace,workload)-group boundary-key index) plumbed straight into 18-04's `paginate`/cursor primitives and flow-scale limit consts; `total_count` counts filtered view items, never true flow totals
- D-14/D-15: `dropclass`/`direction` schema enums via `mustQuerySchema`; description carries the verbatim "a sampled/aggregated view, not a raw flow log" phrase plus the dropclass taxonomy lesson and the direction/drift caveats
- T-18-05-01: `validateFilterComponent` guards any SET namespace/workload filter via `evidence.ValidatePolicyRef` before any path use, without modifying `pkg/evidence` (out of this plan's file scope) — reuses the real validator via a safe placeholder for the independently-optional other argument
- Phase-closing integration test: `TestMCPQueryToolsListed` upgraded to the exact 8-tool assertion; new `TestMCPQueryToolsQRY05Contract` proves truthful annotations + non-empty outputSchema + enum constraints across all 5 query tools in one pass; `TestMCPQueryToolsErrorTexts` extended to all 5 tools; new `TestMCPQueryToolsNotFoundAndCursorErrorTexts` centralizes the not-found/invalid-cursor text discipline
- REQUIREMENTS.md: QRY-01 and QRY-05 marked complete — Phase 18's full QRY-01..05 set is now done

## Task Commits

Each task was committed atomically (TDD RED/GREEN pairs):

1. **Task 1: Implement list_dropped_flows composed view (samples + aggregates, filters, pagination) — QRY-01**
   - `5a7773a` (test) - failing tests for list_dropped_flows (compile-fail RED: `DroppedFlowSample` undefined)
   - `9836802` (feat) - implementation (mcp_query_flows.go + registerQueryTools wiring)
2. **Task 2: Final integration test — all 8 tools listed, QRY-05 annotations + schemas, error-text discipline**
   - `76983cc` (test) - exact 8-tool assertion, QRY-05 contract test, extended error-text tests (one self-caught test-authoring bug fixed before this commit — see Deviations)

**Additional deviation commits:**
- `18c5ec7` (docs) - logged pre-existing `commonflags.go` gofmt debt to deferred-items.md (Rule out-of-scope discovery, not fixed)
- `f33c8da` (style) - removed the now-consumed `//nolint:unused` on `defaultFlowLimit`/`maxFlowLimit` (orchestrator-directed cleanup)

**Plan metadata:** (this commit, docs)

_Note: Task 2 has no separate GREEN counterpart beyond the fixture-seeding fix below — every QRY-05/8-tool assertion passed against Task 1's already-correct implementation on first run, mirroring 18-03's own documented Task 3 precedent ("pure test-coverage consolidation")._

## Files Created/Modified

- `cmd/cpg/mcp_query_flows.go` - `listDroppedFlowsArgs`/`DroppedFlowSample`/`droppedFlowAggregateRow`/`listDroppedFlowsAggregates`/`listDroppedFlowsResult`, `registerListDroppedFlowsTool`, `handleListDroppedFlows`, `collectDroppedFlowSamples`, `collectDroppedFlowAggregates`, `buildCombinedItems`, `validateFilterComponent`, `matchesNamespaceWorkload`, `matchesDropClass`, `classifyDropReasonName`, `splitWorkloadKey`
- `cmd/cpg/mcp_query_flows_test.go` - `TestMCPQueryListDroppedFlows` (7 sub-tests covering every Task 1 behavior) + fixture helpers (`buildDroppedFlowsSampleEvidence`, `singleDropHealthReport`, `callListDroppedFlows`, `stopBypassSession`) + response-mirroring types
- `cmd/cpg/mcp_query.go` - `registerListDroppedFlowsTool(server, mgr)` wired into `registerQueryTools`; doc comment updated to reflect the final 5-tool set
- `cmd/cpg/mcp_query_tools_test.go` - `TestMCPQueryToolsListed` upgraded to exact 8; new `TestMCPQueryToolsQRY05Contract`; `TestMCPQueryToolsErrorTexts` extended to 5 tools; new `TestMCPQueryToolsNotFoundAndCursorErrorTexts`
- `cmd/cpg/mcp_query_pagination.go` - removed the `//nolint:unused` directives on `defaultFlowLimit`/`maxFlowLimit` now that this plan consumes them
- `.planning/REQUIREMENTS.md` - QRY-01 and QRY-05 checkboxes + traceability table rows marked Complete
- `.planning/phases/18-query-tools/deferred-items.md` - logged pre-existing `commonflags.go` gofmt debt (out of scope)

## Decisions Made

- Combined pagination items are assigned a per-(namespace,workload)-group index (0-based, reset at each group boundary in the already-sorted combined list), never a global sequential index — this is what lets the shared `paginateBoundaryKey` scan-based resumption degrade gracefully (occasional skip/dup at one boundary) rather than catastrophically (whole pages shifted) if the underlying file set changes size between calls during an active capture, exactly matching get_evidence's established single-file-index precedent extended to a multi-source, multi-workload view.
- `classifyDropReasonName` recovers a sample's `DropClass` by reversing its persisted `DropReason` string through `flowpb.DropReason_value` (the exact inverse of `evidence_writer.go`'s `f.GetDropReasonDesc().String()`) and running it through the single canonical `dropclass.Classify` — never a second hand-maintained name-to-class table, per D-14's single-source-of-truth instruction. An empty or unrecognized name classifies as `Unknown` rather than silently colliding with `DROP_REASON_UNKNOWN(0)`'s own "transient" bucket.
- `validateFilterComponent` reuses `evidence.ValidatePolicyRef(ns, wl)` — which requires both arguments non-empty (get_policy/get_evidence's contract) — via a safe `"_"` placeholder standing in for whichever of namespace/workload is unset, since list_dropped_flows' filters are each independently optional and `pkg/evidence` was outside this plan's declared file scope. This satisfies T-18-05-01's mitigation (reuse the exact existing traversal guard, never a second copy of its logic) without adding a new pkg/evidence function.
- Aggregates-half `fs.ErrNotExist` while stopped is treated as zero rows, not an error (mirrors QRY-04's Pitfall-1 correction: absence usually means "zero infra/transient drops this session," not a crash); any other `ReadClusterHealth` error (malformed file, wrong `schema_version`) still propagates as a genuine `isError`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug, self-caught during Task 2] Test-authoring bug: get_evidence's cursor sub-test needed a seeded fixture**
- **Found during:** Task 2, first run of `TestMCPQueryToolsNotFoundAndCursorErrorTexts`
- **Issue:** The centralized invalid-cursor assertion called `get_evidence` with `namespace=prod, workload=api, cursor=garbage` against a session with no evidence ever written for that pair. `handleGetEvidence` resolves the evidence file (`reader.Read`) *before* decoding the cursor, so the call surfaced "no evidence for prod/api — call list_policies" instead of the expected "invalid cursor" text — a genuine, if minor, RED result caught before commit.
- **Fix:** Seeded a real evidence fixture at `prod/api` via the existing `writeEvidenceFixture`/`buildEvidenceFixture` helpers (already defined in `mcp_query_evidence_test.go`, same package) before exercising the cursor cases, so the call reaches the cursor-decode path the sub-test is actually proving.
- **Files modified:** `cmd/cpg/mcp_query_tools_test.go`
- **Verification:** `go test ./cmd/cpg/... -race -count=1 -run TestMCPQueryToolsNotFoundAndCursorErrorTexts` passes; full `go test ./cmd/cpg/... -race -count=1` and `go test ./... -race -count=1` both green.
- **Committed in:** `76983cc` (fixed before commit, not a separate follow-up commit)

**2. [Rule 3 - Blocking, orchestrator-directed] Removed now-unneeded nolint:unused on flow-scale pagination consts**
- **Found during:** Post-Task-2 cleanup (explicitly directed by the orchestrator's parallel_execution instructions)
- **Issue:** 18-04 added `defaultFlowLimit`/`maxFlowLimit` with `//nolint:unused` suppressions naming 18-05 as the eventual consumer. Now that `mcp_query_flows.go` consumes both, the suppressions are stale/inaccurate.
- **Fix:** Removed both `//nolint:unused` directives; updated the surrounding doc comment to describe the consts as consumed, not pending.
- **Files modified:** `cmd/cpg/mcp_query_pagination.go`
- **Verification:** `go build ./...`, `go vet ./...`, `rtk proxy golangci-lint run ./cmd/cpg/... --tests` (0 issues), `go test ./cmd/cpg/... -race -count=1` all pass.
- **Committed in:** `f33c8da`

---

**Total deviations:** 2 auto-fixed (1 self-caught test bug, 1 orchestrator-directed cleanup)
**Impact on plan:** No scope creep — no new tools, no new fields beyond what the plan specified. Both fixes were necessary to keep the required verification gates green and to honor the explicit orchestrator directive.

## Issues Encountered

None beyond the deviations above. Worth noting for future executors: this worktree's HEAD was initially on a stale pre-Phase-18 commit (`1efe533`, from the v1.4 milestone) rather than the expected `8ac783a276499afd05b586b0d8513e09eed23f3b` base — the mandatory `<worktree_branch_check>` merge-base comparison caught this before any file was read or edited, and `git reset --hard` to the correct base (working tree was already clean) resolved it with no data loss. Documented here since it's a process note, not a plan deviation.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Phase 18 (query-tools) is now fully complete: all 5 query tools (`list_dropped_flows`, `list_policies`, `get_policy`, `get_evidence`, `get_cluster_health`) plus the 3 Phase 17 session tools (`start_session`, `get_status`, `stop_session`) are registered, tested, and proven over the in-memory MCP transport — exactly 8 tools, confirmed by `TestMCPQueryToolsListed`.
- QRY-01 through QRY-05 are all marked complete in `.planning/REQUIREMENTS.md`. Only SRV-01, SRV-04, SEC-01, SEC-03 remain open in the v1.5 milestone — all three assigned to Phase 19 (security-hardening-e2e) per the existing traceability table.
- `go build ./...`, `go test ./cmd/cpg/... -race -count=1`, `go test ./... -race -count=1` (all 12 packages), `go vet ./...`, `gofmt -l` (clean on every file this plan touched), `rtk proxy golangci-lint run ./cmd/cpg/... --tests` (0 issues), and `govulncheck ./...` (no vulnerabilities) are all green.
- No blockers for Phase 19. The one known pre-existing gap this plan did NOT touch: `cmd/cpg/commonflags.go`'s gofmt debt (logged in `deferred-items.md`, unrelated file, predates Phase 18).

---
*Phase: 18-query-tools*
*Completed: 2026-07-21*

## Self-Check: PASSED

- FOUND: cmd/cpg/mcp_query_flows.go
- FOUND: cmd/cpg/mcp_query_flows_test.go
- FOUND: cmd/cpg/mcp_query.go
- FOUND: cmd/cpg/mcp_query_tools_test.go
- FOUND: cmd/cpg/mcp_query_pagination.go
- FOUND: .planning/phases/18-query-tools/18-05-SUMMARY.md
- FOUND: .planning/phases/18-query-tools/deferred-items.md
- FOUND: .planning/REQUIREMENTS.md
- FOUND commit: 5a7773a (test: failing tests for list_dropped_flows)
- FOUND commit: 9836802 (feat: implement list_dropped_flows composed view)
- FOUND commit: 18c5ec7 (docs: log pre-existing commonflags.go gofmt debt)
- FOUND commit: 76983cc (test: final 8-tool integration test + QRY-05 contract)
- FOUND commit: f33c8da (style: remove now-unneeded nolint:unused)
