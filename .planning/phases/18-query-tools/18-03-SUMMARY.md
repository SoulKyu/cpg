---
phase: 18-query-tools
plan: 03
subsystem: api
tags: [mcp, go-sdk, jsonschema, cilium-network-policy, cluster-health]

# Dependency graph
requires:
  - phase: 18-query-tools (18-01)
    provides: pkg/explain promotion (not directly consumed by this plan, but establishes the promotion precedent)
  - phase: 18-query-tools (18-02)
    provides: pkg/output.ReadPolicyFile, pkg/hubble.ReadClusterHealth/ClusterHealthReport/HealthDropJSON/HealthSession (exported types)
  - phase: 17-session-lifecycle
    provides: pkg/session.Manager.Status/StatusResult, registerSessionTools registration pattern, the D-16 error-return convention
provides:
  - cmd/cpg/mcp_query.go composition root (registerQueryTools) wired into runMCPServer
  - resolveSession shared helper (D-08) and availableAfterStopMarker shared type (D-02), both reusable by 18-04/18-05
  - list_policies, get_policy (QRY-02), get_cluster_health (QRY-04) MCP tools, fully tested
affects: [18-query-tools (18-04), 18-query-tools (18-05), 19-security-hardening-e2e]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "MCP query-tool handlers factor pure branch logic (clusterHealthBranch) out of the mcp.AddTool closure so complex multi-state logic is unit-testable without a real session/pipeline"
    - "Shared arg-struct reuse (sessionRef) across tools needing only {session_id} avoids duplicate near-identical arg types"
    - "Anonymous struct embedding (availableAfterStopMarker) for a shared non-error marker shape — promotes JSON fields automatically via both encoding/json and jsonschema-go's reflection"

key-files:
  created:
    - cmd/cpg/mcp_query.go
    - cmd/cpg/mcp_query_tools_test.go
  modified:
    - cmd/cpg/mcp.go
    - cmd/cpg/mcp_session_test.go

key-decisions:
  - "Reused mcp_tools.go's existing sessionRef struct for list_policies/get_cluster_health args instead of declaring near-duplicate {session_id}-only structs"
  - "Factored get_cluster_health's D-13 4-state branch into a pure clusterHealthBranch(status, healthPath) function, separate from the mcp.AddTool closure, so 3 of 4 branches are fast/deterministic unit tests and the 4th (genuine crash) is a real but bounded-time integration test"
  - "availableAfterStopMarker is embedded (anonymous field) in getClusterHealthResult rather than referenced by pointer, so its fields promote to the top-level JSON/schema automatically — verified against jsonschema-go v0.4.3's infer.go anonymous-field handling"
  - "Verified empirically that get_cluster_health's capturing-state test is not racy: pkg/hubble/client.go's waitForConnReady loops on WaitForStateChange until Ready or its OWN timeout fires, so a refused D-07 bypass dial keeps state=='capturing' for the whole configured timeout, not a fraction of a millisecond"

patterns-established:
  - "Query-tool session resolution: resolveSession(mgr, sessionID) as the first line of every handler body, reused verbatim by 18-04/18-05"
  - "D-02/D-13 non-error markers share one embeddable type (availableAfterStopMarker) instead of each tool inventing its own capturing-state shape"

requirements-completed: [QRY-02, QRY-04, QRY-05]

duration: 30min
completed: 2026-07-21
---

# Phase 18 Plan 03: Query-Tool Composition Root + Policy/Cluster-Health Tools Summary

**registerQueryTools composition root plus list_policies/get_policy/get_cluster_health MCP tools, with get_cluster_health's D-13 crash-vs-healthy branch logic factored into a directly unit-tested pure function.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-07-21T16:04:00+02:00 (approx.)
- **Completed:** 2026-07-21T16:23:49+02:00
- **Tasks:** 3 completed
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments

- `cmd/cpg/mcp_query.go` composition root (`registerQueryTools`) wired into `runMCPServer`, extending Phase 17's exact registration pattern
- `list_policies` (QRY-02, D-07 unpaginated): metadata rows (namespace, workload, CNP name, directions, rule counts, absolute path) for every generated policy, best-effort tolerant of a mid-write file
- `get_policy` (QRY-02/D-11/D-17): full CNP YAML + metadata for one namespace/workload pair, path-traversal guarded via `evidence.ValidatePolicyRef`, actionable not-found error naming `list_policies`
- `get_cluster_health` (QRY-04/D-13): the corrected 4-state branch (capturing / stopped+present / stopped+absent+no-error / stopped+absent+error), proven both as a pure unit-tested function and end-to-end over the in-memory MCP transport
- Shared foundation for 18-04/18-05: `resolveSession` helper and `availableAfterStopMarker` type, both designed for direct reuse (not just similar shape)

## Task Commits

Each task was committed atomically (TDD RED/GREEN pairs):

1. **Task 1: Scaffold registerQueryTools + wire mcp.go + list_policies/get_policy**
   - `f9b3298` (test) - failing tests for list_policies and get_policy
   - `ee6a6c1` (feat) - implementation + mcp.go wiring + Rule-3 fix to mcp_session_test.go
2. **Task 2: get_cluster_health with the D-13 corrected 3-way branch**
   - `7037e07` (test) - failing 4-branch test for get_cluster_health
   - `1576330` (feat) - implementation (clusterHealthBranch + handler + registration)
3. **Task 3: In-memory-harness tests for the 3 non-paginated tools**
   - `889d133` (test) - tool-listing presence/required-fields + centralized error-text coverage

**Additional deviation commit:**
- `79163a5` (style) - staticcheck QF1008 lint fix (Rule 1)

**Plan metadata:** (this commit, docs)

_Note: Task 3 has no GREEN counterpart — it is pure test-coverage consolidation over Tasks 1-2's already-correct implementation; all its assertions passed on first run._

## Files Created/Modified

- `cmd/cpg/mcp_query.go` - `registerQueryTools` composition root; `resolveSession`; `availableAfterStopMarker`; `list_policies`/`get_policy`/`get_cluster_health` handlers and result types; `clusterHealthBranch` pure branch function
- `cmd/cpg/mcp.go` - one-line wiring: `registerQueryTools(server, mgr)` added to `runMCPServer`, immediately after `registerSessionTools`
- `cmd/cpg/mcp_query_tools_test.go` - new in-memory-harness test file: `TestMCPQueryListPolicies`, `TestMCPQueryGetPolicy`, `TestMCPQueryGetClusterHealth` (4 sub-tests), `TestClusterHealthBranch` (4 sub-tests, pure unit), `TestMCPQueryToolsListed`, `TestMCPQueryToolsErrorTexts`, plus shared test helpers (`startBypassSession`, `connectQueryTestClient`, `writeTestPolicy`, `writeClusterHealthFixture`, `findPolicyRow`)
- `cmd/cpg/mcp_session_test.go` - `TestMCPSessionToolsListed`'s hardcoded `Len(..., 3)` loosened to `GreaterOrEqual(..., 3)` (deviation, see below)

## Decisions Made

- Reused the existing `sessionRef{session_id}` struct (from `mcp_tools.go`) for `list_policies`/`get_cluster_health` args rather than declaring near-duplicate structs — both tools' argument surface is identical to `get_status`/`stop_session`'s.
- Factored `get_cluster_health`'s D-13 4-state branch into a pure `clusterHealthBranch(status session.StatusResult, healthPath string) (getClusterHealthResult, error)` function, called by the thin `handleGetClusterHealth` MCP handler. This let 3 of the 4 branches (capturing, stopped+present, stopped+absent+no-error) get fast, fully deterministic unit-test coverage (`TestClusterHealthBranch`) independent of the harder-to-control real-session timing, while the harness-level `TestMCPQueryGetClusterHealth` still separately proves the full wire-level behavior (registration, JSON round-trip, annotations) for all 4 cases.
- Verified (by reading `pkg/hubble/client.go`'s `waitForConnReady`) that the D-07 bypass address's dial failure does NOT race against the "still capturing" test window: `WaitForStateChange` loops through `CONNECTING`/`TRANSIENT_FAILURE`/backoff cycles and only returns once its OWN configured timeout context fires — never early on a mere connection refusal. This means using a generous `timeout` (e.g. `"10s"`) and asserting immediately after `start_session` is deterministic, not racy — confirmed empirically by 5 consecutive clean test runs.
- The one sub-test that genuinely needs the pipeline to fail on its own (`stopped_absent_with_error`) uses a short real timeout (`"1s"`) plus `require.Eventually` polling, mirroring `pkg/session/manager_test.go`'s own `TestManager_PipelineErrorAutonomouslyStopsSession` pattern at the black-box MCP-harness level — this is the one case where a small real-time wait (~1s) is unavoidable without white-box access to `Manager`'s unexported `runPipeline`/`resolveSetupFn` seams.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Loosened `TestMCPSessionToolsListed`'s exact tool-count assertion**
- **Found during:** Task 1 (GREEN — registering `list_policies`/`get_policy`)
- **Issue:** `cmd/cpg/mcp_session_test.go`'s pre-existing `TestMCPSessionToolsListed` asserted `require.Len(t, toolsResult.Tools, 3, ...)`. Since `registerQueryTools` registers additional tools on the same server, this hardcoded exact count broke the moment any query tool was registered — a required verification (`go test ./cmd/cpg/... -race -count=1`) blocker, not something this plan could defer, since 18-04/18-05 (the only plans that touch the *final* exact-8 assertion, per their own plan text targeting `mcp_query_tools_test.go`) never mention touching `mcp_session_test.go` either.
- **Fix:** Changed the assertion to `require.GreaterOrEqual(t, len(toolsResult.Tools), 3, ...)` and updated the doc comment to note the exact cross-phase total is asserted once, at the end of Phase 18, by `cmd/cpg/mcp_query_tools_test.go`'s final integration test (18-05's own stated job).
- **Files modified:** `cmd/cpg/mcp_session_test.go`
- **Verification:** `go test ./cmd/cpg/... -race -count=1` passes with 5 tools present after Task 1, 6 after Task 2.
- **Committed in:** `ee6a6c1` (Task 1 GREEN commit)

**2. [Rule 1 - Bug/code-quality] staticcheck QF1008 on `cnp.ObjectMeta.Name`**
- **Found during:** post-implementation lint pass (`golangci-lint run ./cmd/cpg/...`)
- **Issue:** `cnp.ObjectMeta.Name` is redundant since `ObjectMeta` is an embedded field on `ciliumv2.CiliumNetworkPolicy`; `cnp.Name` is equivalent and idiomatic. This repo's CI lint gate (`only-new-issues: true`) flags new issues in new code even though pre-existing debt is grandfathered.
- **Fix:** Simplified both occurrences (`policyRowFromCNP`, `handleGetPolicy`) to `cnp.Name`.
- **Files modified:** `cmd/cpg/mcp_query.go`
- **Verification:** `golangci-lint run ./cmd/cpg/... --tests` reports 0 issues; `go test ./cmd/cpg/... -race -count=1` still passes.
- **Committed in:** `79163a5`

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 code-quality)
**Impact on plan:** Both were necessary to keep the required verification gate (`go test ./cmd/cpg/... -race -count=1`) and this repo's lint discipline green. No scope creep — no new tools, no new fields beyond what the plan specified.

## Issues Encountered

None beyond the deviations above. The one design risk I investigated carefully — whether `get_cluster_health`'s "capturing" and "stopped+absent" sub-tests would be racy against the D-07 bypass address's background dial — turned out not to be a problem once I traced `pkg/hubble/client.go`'s `waitForConnReady` implementation; documented as a Decision above rather than an Issue since it resolved cleanly without requiring any workaround.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `registerQueryTools`, `resolveSession`, and `availableAfterStopMarker` are in place exactly as 18-04 (`get_evidence`) and 18-05 (`list_dropped_flows`) expect to extend them — both plans' own `<read_first>` sections point at this plan's file and these exact symbols.
- `pkg/hubble.ReadClusterHealth` (from 18-02) is now proven end-to-end through a real MCP tool call, not just at the reader-unit level — de-risks 18-05's reuse of the same reader for the aggregates half.
- No blockers. `go build ./...`, `go test ./cmd/cpg/... -race -count=1`, `go test ./... -race -count=1`, and `golangci-lint run ./cmd/cpg/... --tests` are all green; `govulncheck ./...` reports no vulnerabilities.
- Note for 18-04: `github.com/google/jsonschema-go v0.4.3` remains `// indirect` in `go.mod` despite this plan's direct `jsonschema.Ptr` import — this is intentional and matches the plan's own `read_first` note ("this becomes a direct import, resolved by `go mod tidy` in 18-04"); 18-04's own Task 1 acceptance criteria already expect to perform that `go mod tidy` reclassification.

---
*Phase: 18-query-tools*
*Completed: 2026-07-21*
