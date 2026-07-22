---
phase: 18-query-tools
verified: 2026-07-21T16:09:05Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
---

# Phase 18: Query Tools Verification Report

**Phase Goal:** An LLM can read a session's dropped flows, generated policies, per-rule evidence, and cluster health as safe, well-described, paginated MCP tool results
**Verified:** 2026-07-21T16:09:05Z
**Status:** passed
**Re-verification:** No — initial verification

## Method

Goal-backward, adversarial stance: build/vet/lint run independently by the verifier (not trusted from SUMMARY.md), all `cmd/cpg`/`pkg/explain`/`pkg/output`/`pkg/hubble`/`pkg/session` tests re-executed under `-race` with `-v` to inspect actual sub-test names (not just top-level `ok`), git diff-stat taken against the pre-phase-18 base commit (`2c57c07`) to catch undisclosed scope creep, and the WR-01 code-review finding's classification logic independently traced through `pkg/hubble/aggregator.go` / `pkg/dropclass/classifier.go` rather than accepted on the fixer's word.

## Goal Achievement

### Observable Truths

Primary truths are ROADMAP.md's 5 Success Criteria (the non-negotiable contract). Each plan's frontmatter `must_haves` restate/detail these same 5 — folded into the Evidence column rather than duplicated as separate rows, per the merge rule (a PLAN truth that restates a roadmap SC keeps the roadmap wording).

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | LLM calls `list_dropped_flows(session_id, …filters)` and receives a paginated composed view (samples[] + aggregates[]), description explicit it is "a sampled/aggregated view, not a raw flow log" | ✓ VERIFIED | `cmd/cpg/mcp_query_flows.go:44-90` defines `DroppedFlowSample`/`droppedFlowAggregateRow`/`listDroppedFlowsResult` as two distinct never-merged sections; verbatim phrase confirmed at line 117 (`grep -q 'a sampled/aggregated view, not a raw flow log'` matches); `paginate()` wraps the combined view (line 247) with `total_count`/`has_more`/`next_cursor`. `TestMCPQueryListDroppedFlows` (7 sub-tests: composed_view_stopped_session, capturing_marker_samples_still_live, direction_filters_samples_only, dropclass_noise_yields_empty_aggregates, dropclass_unknown_reason_sample_classifies_unknown_not_transient, namespace_filter_narrows_both_halves, pagination_over_combined_view, description_and_schema_contract) — all PASS, run directly by this verifier, not read from SUMMARY. |
| 2 | LLM calls `list_policies(session_id)` for metadata and `get_policy(session_id, namespace, workload)` for full CNP YAML + absolute path, both consistent even while the pipeline is actively writing | ✓ VERIFIED | `handleListPolicies`/`handleGetPolicy` in `cmd/cpg/mcp_query.go:166-342`. Consistency under concurrent writes: `pkg/output/writer.go`'s atomic temp+rename (`TestWriter_ConcurrentReaderNeverSeesPartialFile` PASS — a reader never observes a torn file) plus the WR-03 single-read fix (`handleGetPolicy` reads the file exactly once via `os.ReadFile` then derives both YAML string and parsed metadata from the same byte slice via the new `output.UnmarshalPolicy`, eliminating the prior read-twice skew risk — confirmed at `mcp_query.go:308-328`). `TestMCPQueryListPolicies`, `TestMCPQueryListPoliciesPagination`, `TestMCPQueryGetPolicy`, `TestUnmarshalPolicy_RoundTrips`, `TestUnmarshalPolicy_MalformedYAML` all PASS. |
| 3 | LLM calls `get_evidence(session_id, …filters)` and receives paginated per-rule flow attribution identical to `cpg explain --output json`, via the promoted `pkg/explain` renderer | ✓ VERIFIED | `pkg/explain/render.go:24-28`'s `Output{Policy, Sessions, MatchedRules}` and `cmd/cpg/mcp_query_evidence.go:50-57`'s `getEvidenceResult` use the exact same `evidence.PolicyRef`/`evidence.SessionInfo`/`evidence.RuleEvidence` types — per-record shape identity is structural (same Go type), not a re-implementation. `cmd/cpg/explain.go` confirmed thinned and importing `pkg/explain` (`explain.RenderJSON`/`RenderYAML`/`RenderText`, `explain.Filter`, `explain.ParsePeerLabel`, `filter.Match` — all present; zero stale `explainFilter`/`explainOutput`/`renderJSON(`/`parsePeerLabel(` references, confirmed via grep count = 0). `buildEvidenceFilter` (`mcp_query_evidence.go:190-216`) mirrors `cmd/cpg/explain.go`'s `buildFilter` field-for-field, including the deliberate absence of a `protocol` field (none exists on `explain.Filter`). `go test ./pkg/explain/... -race -count=1` (19 sub-tests) and `TestMCPQueryGetEvidence` (11 sub-tests: shape_and_unfiltered, pagination, direction/port/peer/peer_cidr/http_method/http_path/dns_pattern filters, malformed_peer_isError, unknown_target_isError, invalid_cursor_isError) all PASS. |
| 4 | LLM calls `get_cluster_health(session_id)` and receives the finalized report (with remediation URLs) once stopped, or a non-error "available after stop_session" result while capturing | ✓ VERIFIED | `clusterHealthBranch` (`cmd/cpg/mcp_query.go:449-489`) implements the exact 4-state branch (capturing / stopped+present / stopped+absent+no-error / stopped+absent+error), factored as a pure function and unit-tested independent of a real session. `TestMCPQueryGetClusterHealth` (5 sub-tests: capturing, stopped_present, stopped_present_truncates_large_workload_breakdown, stopped_absent_no_error, stopped_absent_with_error) PASS. `pkg/hubble.ReadClusterHealth` (`health_reader.go`) reads+schema-gates the file; `TestRunPipeline_FinalizesHealthOnStreamError` (pkg/hubble) independently re-run and PASS, proving `finalize()` writes `cluster-health.json` unconditionally even after a mid-capture pipeline error — the evidence D-13's "absent ≠ crashed" branch depends on. |
| 5 | Every data-returning tool ships `structuredContent`+`outputSchema`, truthful annotations, a taxonomy-teaching description, and `isError` with specific actionable text | ✓ VERIFIED | `TestMCPQueryToolsListed` asserts exactly 8 tools (`require.Len(t, toolsResult.Tools, 8, ...)`, `mcp_query_tools_test.go:686`) — 3 session + 5 query. `TestMCPQueryToolsQRY05Contract` asserts, for all 5 query tools: `ReadOnlyHint==true`, `OpenWorldHint` explicitly set to `false` (not omitted), non-empty `OutputSchema`, and enum constraints on `direction` (get_evidence, list_dropped_flows) and `dropclass` (list_dropped_flows, 5 values) — all read directly off the wire InputSchema, not source code. `TestMCPQueryToolsErrorTexts`/`TestMCPQueryToolsNotFoundAndCursorErrorTexts` assert the verbatim SESS-06 "not found or expired" text on all 5 tools, "list_policies"-suggesting not-found text on get_policy/get_evidence, and the D-05 "invalid cursor" text on get_evidence/list_dropped_flows/list_policies (WR-02) — confirmed never a panic, always a tool-level `isError`. Descriptions read directly in `mcp_query.go`/`mcp_query_evidence.go`/`mcp_query_flows.go` all carry the policy-actionable-vs-infra/transient taxonomy lesson. |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `cmd/cpg/mcp_query.go` | `registerQueryTools` + resolveSession + list_policies/get_policy/get_cluster_health handlers | ✓ VERIFIED | 490 lines; exists, substantive (full handler logic, WR-02/WR-03/WR-04 fixes present), wired (called from `mcp.go:96`) |
| `cmd/cpg/mcp_query_evidence.go` | get_evidence handler + args struct | ✓ VERIFIED | 217 lines; `registerGetEvidenceTool` wired into `registerQueryTools` (`mcp_query.go:39`) |
| `cmd/cpg/mcp_query_flows.go` | list_dropped_flows handler + composed-view assembly | ✓ VERIFIED | 498 lines; `registerListDroppedFlowsTool` wired into `registerQueryTools` (`mcp_query.go:40`); WR-01 fix (`classifyDropReasonName`'s `DROP_REASON_UNKNOWN` special case) present and independently traced against `pkg/hubble/aggregator.go`'s classification gate (see Anti-Patterns/Notes) |
| `cmd/cpg/mcp_query_pagination.go` | mustQuerySchema[T], cursor codec, paginate helper | ✓ VERIFIED | 199 lines; consumed by all 3 files above; `defaultFlowLimit`/`maxFlowLimit` consumed by both `list_dropped_flows` and `list_policies` (WR-02), no longer `//nolint:unused` |
| `pkg/explain/{doc,filter,render}.go` | exported Filter.Match, Output, Render*, ParsePeerLabel | ✓ VERIFIED | `cmd/cpg/explain.go` imports and calls all of them; `cmd/cpg/explain_filter.go`/`explain_render.go`/`explain_filter_test.go` confirmed deleted; 0 stale unexported references |
| `pkg/output/writer.go` | ReadPolicyFile + UnmarshalPolicy (WR-03) | ✓ VERIFIED | Both exported, `ReadPolicyFile` delegates to `UnmarshalPolicy`; `readExistingPolicy`/`ReadExisting` untouched |
| `pkg/hubble/health_reader.go` + exported types in `health_writer.go` | ReadClusterHealth + ClusterHealthReport/HealthSession/HealthDropJSON | ✓ VERIFIED | All present, schema-version-gated, JSON tags byte-identical (including plural `infra_drops_total`) |
| `pkg/session/paths.go` | SessionPaths + DeriveSessionPaths (WR-04) | ✓ VERIFIED | Pure function; all 5 originally-duplicated call sites (`buildPipelineConfig`, `Manager.Stop`, `handleGetClusterHealth`, `handleGetEvidence`, `handleListDroppedFlows`) now call it — confirmed via grep, zero hand-copied formula remains |
| `cmd/cpg/mcp_query_tools_test.go` | final 8-tool integration test | ✓ VERIFIED | `Len(t, toolsResult.Tools, 8` present and passing |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `cmd/cpg/mcp.go` | `registerQueryTools` | composition-root call | ✓ WIRED | `mcp.go:96`, immediately after `registerSessionTools` (line 95), called exactly once |
| `cmd/cpg/mcp_query*.go` | `pkg/session.Manager.Status` | `resolveSession(mgr, sessionID)` | ✓ WIRED | Every handler's first line resolves session via this helper; SESS-06 text flows through verbatim (tested) |
| `cmd/cpg/mcp_query_evidence.go` | `pkg/explain` | `explain.Filter{}`, `.Match(r)`, `explain.ParsePeerLabel` | ✓ WIRED | `mcp_query_evidence.go:191-216`; grep confirms `explain\.(Filter\|Output)` and `explain.ParsePeerLabel` present |
| `cmd/cpg/mcp_query.go` + `mcp_query_flows.go` | `pkg/hubble.ReadClusterHealth` | direct call, gated by session state | ✓ WIRED | `mcp_query.go:459`, `mcp_query_flows.go:190`; capturing-state branch confirmed to skip the call entirely (grep + `TestMCPQueryListDroppedFlows/capturing_marker_samples_still_live` PASS) |
| `cmd/cpg/mcp_query*.go` | `pkg/session.DeriveSessionPaths` | WR-04 single-source-of-truth path derivation | ✓ WIRED | All 3 query-tool readers plus `buildPipelineConfig`/`Manager.Stop` call it; zero remaining hand-copied `HashOutputDir(join(...))` duplication |
| `cmd/cpg/mcp_query_pagination.go` | `github.com/google/jsonschema-go/jsonschema` | `jsonschema.For[T]`, `.Ptr(false)`, Enum patch | ✓ WIRED | `go.mod` confirms `github.com/google/jsonschema-go v0.4.3` is now a direct (non-indirect) require line |

### Data-Flow Trace (Level 4)

Not a rendering-layer phase (no React/UI components) — the equivalent check is "do the typed result structs carry real filesystem/session data, or hardcoded stand-ins." Traced for each tool:

| Tool | Result field | Source | Produces Real Data | Status |
|------|-------------|--------|---------------------|--------|
| list_policies | `Policies[]` | `os.ReadDir` + `output.ReadPolicyFile` walk of `<tmpDir>/policies/<ns>/<wl>.yaml` | Yes — round-tripped in `TestMCPQueryListPolicies` against real on-disk YAML fixtures | ✓ FLOWING |
| get_policy | `YAML`, rule counts | `os.ReadFile` + `output.UnmarshalPolicy` of the exact tmpdir path | Yes — `TestMCPQueryGetPolicy` asserts full YAML string round-trip | ✓ FLOWING |
| get_evidence | `MatchedRules[]` | `evidence.NewReader(...).Read(ns, wl)` against real seeded evidence JSON | Yes — per-filter-field assertions in `TestMCPQueryGetEvidence` narrow a real matched set (not a static stub) | ✓ FLOWING |
| get_cluster_health | `Report` | `hubble.ReadClusterHealth` against real seeded `cluster-health.json` | Yes — `stopped_present` and `stopped_present_truncates_large_workload_breakdown` sub-tests read real fixture data, including the truncation cap over a large synthetic map | ✓ FLOWING |
| list_dropped_flows | `Samples[]`/`Aggregates.Rows[]` | evidence-dir walk + `ReadClusterHealth`, both against real seeded fixtures | Yes — `composed_view_stopped_session` sub-test asserts `len(samples)==2` and `len(aggregates)==1` against seeded data, not synthesized | ✓ FLOWING |

### Behavioral Spot-Checks

Actual `go test -v` execution performed by this verifier (not sourced from SUMMARY.md), inspecting sub-test names directly:

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| All 5 query tools + 3 session tools listed, exactly 8 | `go test ./cmd/cpg/... -race -count=1 -run TestMCPQueryToolsListed -v` | `--- PASS: TestMCPQueryToolsListed (0.09s)` | ✓ PASS |
| QRY-05 annotation/schema contract holds over the wire | `go test ./cmd/cpg/... -race -count=1 -run TestMCPQueryToolsQRY05Contract -v` | `--- PASS (0.08s)` | ✓ PASS |
| WR-01 regression: DROP_REASON_UNKNOWN classifies unknown, not transient | `go test ./cmd/cpg/... -race -count=1 -run TestMCPQueryListDroppedFlows -v` | sub-test `dropclass_unknown_reason_sample_classifies_unknown_not_transient` PASS | ✓ PASS |
| finalize() writes cluster-health.json despite a mid-capture pipeline error | `go test ./pkg/hubble/... -race -count=1 -run TestRunPipeline_FinalizesHealthOnStreamError -v` | `--- PASS (0.15s)` | ✓ PASS |
| Full phase-18-touched package suite, no skips/failures | `go test ./cmd/cpg/... ./pkg/explain/... ./pkg/output/... ./pkg/hubble/... -race -count=1 -v` | 235 `--- PASS`, 0 `--- FAIL`, 0 `--- SKIP` | ✓ PASS |
| Repo-wide build | `go build ./...` | exit 0, no output | ✓ PASS |
| `pkg/session` in isolation (documented pre-existing combined-run flake) | `go test ./pkg/session/... -race -count=1` (then 3x stress on the 3 named tests) | all green in isolation; combined `go test ./...` reproduces the exact documented flake (`TestManager_Start_ShutdownRacesSetup`, glob-count race) | ✓ PASS (flake confirmed pre-existing, not phase-18-caused) |

### Probe Execution

SKIPPED — no `scripts/*/tests/probe-*.sh` found and none referenced by any Phase 18 PLAN/SUMMARY. Not a migration/tooling phase.

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|----------------|--------------|--------|----------|
| QRY-01 | 18-05 | `list_dropped_flows` composed view, paginated, sampled/aggregated description | ✓ SATISFIED | Truth #1 above |
| QRY-02 | 18-02, 18-03 | `list_policies`/`get_policy` metadata + YAML, torn-read safe | ✓ SATISFIED | Truth #2 above |
| QRY-03 | 18-01, 18-04 | `get_evidence` identical-shape to `cpg explain --output json`, paginated | ✓ SATISFIED | Truth #3 above |
| QRY-04 | 18-02, 18-03 | `get_cluster_health` passthrough / available_after_stop | ✓ SATISFIED | Truth #4 above |
| QRY-05 | 18-03, 18-04, 18-05 | structuredContent+outputSchema, truthful annotations, taxonomy description, actionable isError | ✓ SATISFIED | Truth #5 above |

**Union of plan-declared requirement IDs** (`grep -A5 requirements: 18-0*-PLAN.md`): `{QRY-01, QRY-02, QRY-03, QRY-04, QRY-05}` — matches the phase's declared requirement set exactly. `.planning/REQUIREMENTS.md`'s traceability table maps only these 5 IDs to Phase 18, and all 5 are marked `[x]` Complete / "Complete" in the table. **No orphaned requirements** — nothing in REQUIREMENTS.md assigns an additional Phase-18 ID that no plan claimed.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `pkg/explain/render.go` | 34,37,46-50 | Unchecked `fmt.Fprintf`/`Fprintln` return values (errcheck) | ℹ️ INFO | Pre-existing — confirmed present byte-for-byte in `cmd/cpg/explain_render.go` at the pre-phase-18 base commit (`git show 2c57c07:...`); relocated verbatim by the 18-01 mechanical promotion, not introduced. Tracked under REQUIREMENTS.md's v2 `LINT-01..03` backlog; CI gates on `only-new-issues: true` so this does not fail the build. |
| `pkg/hubble/health_writer.go` | 151,152,156,160 | Unchecked `tmp.Close()`/`os.Remove()` (errcheck) | ℹ️ INFO | Confirmed pre-existing at the same base commit; the 18-02 rename-only change (unexported→exported type names) did not touch these lines. |
| `pkg/hubble/client.go` | 146 | Unchecked `onClose.Close()` (errcheck) | ℹ️ INFO | Confirmed pre-existing; file untouched by any Phase 18 plan. |
| `cmd/cpg/commonflags.go` | — | gofmt formatting debt | ℹ️ INFO | Confirmed pre-existing at the same base commit (predates Phase 18 by an unrelated commit); already logged in `deferred-items.md` by the 18-05 executor. File not touched by Phase 18. |
| `pkg/session/manager_test.go` | — | 3 tests assert an unscoped `/tmp/cpg-session-*` glob count, racy under concurrent test-suite execution | ℹ️ INFO | Reproduced during this verification's full-suite run exactly as documented in `deferred-items.md` (logged independently by both 18-01 and 18-04 executors); confirmed via 1x + 3x-stress isolated re-run that `pkg/session` is clean on its own. Pre-existing Phase-17 test-design issue, not a Phase 18 regression — this plan's `cmd/cpg`/`pkg/explain`/`pkg/output`/`pkg/hubble`/`pkg/session/paths.go` changes are unrelated to the tmpdir-count assertion's raciness. |

No 🛑 BLOCKER anti-patterns. No `TODO`/`FIXME`/`XXX`/`HACK`/`PLACEHOLDER` debt markers found in any file this phase touched (grep across all `cmd/cpg/mcp_query*.go`, `pkg/explain/*.go`, `pkg/hubble/health_reader.go`, `pkg/output/writer.go`, `pkg/session/paths.go` returned zero matches; the two "placeholder" grep hits were doc-comment prose describing a marker struct and a validation stand-in value, not debt markers).

**Readonly-discipline spot-check (SEC-01 is formally Phase 19's audit, but "safe" is part of this phase's goal wording):** grep for `os.WriteFile`/`os.Create`/`os.MkdirAll`/`os.Remove`/K8s write verbs across all 4 `cmd/cpg/mcp_query*.go` files returned zero matches — every handler reaches only `mgr.Status` plus filesystem readers, consistent with the phase's stated readonly composition-root discipline.

**WR-01 domain-logic note (independently traced, not taken on faith):** `18-REVIEW-FIX.md` flagged WR-01's fix as needing manual domain-semantics confirmation ("classification reasoning: aggregator gate vs. dropclass.Classify's taxonomy"). This verifier independently traced it: `pkg/hubble/aggregator.go:417` gates classification with `if f.Verdict == flowpb.Verdict_DROPPED && f.GetDropReasonDesc() != flowpb.DropReason_DROP_REASON_UNKNOWN` — i.e., reason-code 0 is explicitly excluded from ever reaching `dropclass.Classify` inside the infra/transient-suppression branch, meaning any DROPPED flow with reason 0 that produces an evidence sample got there via the policy-actionable fall-through, never the infra/transient path. `pkg/dropclass/classifier.go:28` separately maps `DROP_REASON_UNKNOWN → DropClassTransient` for direct `Classify(0)` calls elsewhere (Cilium reason-code taxonomy, a different call context than the aggregator's routing gate). `classifyDropReasonName` in `mcp_query_flows.go` special-cases this exact mismatch at the call site, and the dedicated regression test (`dropclass_unknown_reason_sample_classifies_unknown_not_transient`) passes. This trace is now recorded here for a Cilium-domain owner to double-check if desired — it is not blocking, since the code-level reasoning is self-consistent and test-pinned, which is the strongest verification available short of live-cluster behavior (out of scope for this milestone's readonly, offline-fixture testing model).

### Human Verification Required

None. Every observable truth in this phase resolves to `structuredContent`/`isError` behavior over an in-memory MCP transport, testable and independently re-executed by this verifier. No visual, real-time, or external-service-dependent behavior is introduced by Phase 18 (the live-cluster stdio integration test is explicitly SRV-04/Phase 19 scope, not this phase's).

### Gaps Summary

None. All 5 roadmap Success Criteria are verified with direct code inspection plus independently re-executed tests (not SUMMARY.md claims). The 4 code-review findings (WR-01..WR-04) are all present in the code, covered by dedicated regression tests, and match the "expected, not deviations" framing given for this verification. Diff-stat against the pre-phase-18 base commit shows no undisclosed file changes beyond what the 5 SUMMARYs and the review-fix report claim. The one repo-wide test failure encountered (`pkg/session`'s `TestManager_Start_ShutdownRacesSetup` under the combined `go test ./...` run) is a reproduced, pre-existing, already-documented flake unrelated to Phase 18's own changes — confirmed via isolated re-run (1x clean + 3x-stress clean).

---

_Verified: 2026-07-21T16:09:05Z_
_Verifier: Claude (gsd-verifier)_
