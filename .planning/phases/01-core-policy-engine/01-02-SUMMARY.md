---
phase: 01-core-policy-engine
plan: 02
subsystem: policy
tags: [cilium, go, tdd, cnp, policy-generation, hubble]

# Dependency graph
requires:
  - "01-01: Label selector with BuildEndpointSelector, BuildPeerSelector, WorkloadName"
provides:
  - "BuildPolicy: Flow-to-CiliumNetworkPolicy transformation with direction-aware rule generation"
  - "MergePolicy: Read-modify-write policy merging with port dedup and peer matching"
  - "PolicyEvent type for downstream pipeline use"
  - "Test fixtures for building Hubble flow structs"
affects: [01-03]

# Tech tracking
tech-stack:
  added: [sigs.k8s.io/yaml]
  patterns: [peer grouping by label key, port deduplication, direction-aware rule building]

key-files:
  created:
    - pkg/policy/builder.go
    - pkg/policy/builder_test.go
    - pkg/policy/merge.go
    - pkg/policy/merge_test.go
    - pkg/policy/testdata/ingress_flow.go
  modified:
    - go.mod
    - go.sum

key-decisions:
  - "EndpointSelector comparison via LabelSelector.MatchLabels for peer matching in merge"
  - "Single PortRule per IngressRule/EgressRule containing all ports for that peer"
  - "Ordered peer grouping using insertion-order tracking for deterministic rule output"

patterns-established:
  - "TDD workflow: RED (failing tests + stub) -> GREEN (implementation) -> commit each phase separately"
  - "Flow grouping: peer key from labels.SelectLabels, port dedup via seen map"
  - "Test fixtures in testdata/ package for building proto structs"

requirements-completed: [PGEN-01, PGEN-02, PGEN-05, PGEN-06]

# Metrics
duration: 5min
completed: 2026-03-08
---

# Phase 1 Plan 2: Policy Builder + Merge Summary

**Flow-to-CiliumNetworkPolicy builder with direction-aware ingress/egress rules, port extraction, peer grouping, and read-modify-write merge logic**

## Performance

- **Duration:** 5 min
- **Started:** 2026-03-08T09:00:15Z
- **Completed:** 2026-03-08T09:04:46Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments
- BuildPolicy correctly transforms Hubble dropped flows into CiliumNetworkPolicy with direction semantics (ingress -> IngressRule, egress -> EgressRule)
- Port extraction from L4 TCP/UDP with nil-safety (nil L4 flows silently skipped)
- Peer grouping: same peer gets merged ports in single rule, different peers get separate rules
- MergePolicy performs read-modify-write: adds ports to existing peer rules, appends new peers, deduplicates port+protocol
- 14 unit tests (9 builder + 5 merge) all passing via TDD
- YAML roundtrip produces valid CiliumNetworkPolicy with correct apiVersion, kind, metadata, spec

## Task Commits

Each task was committed atomically:

1. **Task 1 RED: Failing tests for policy builder** - `a538d4d` (test)
2. **Task 1 GREEN: Implement policy builder** - `6eb3c24` (feat)
3. **Task 2 RED: Failing tests for policy merge** - `cdb664f` (test)
4. **Task 2 GREEN: Implement policy merge** - `8367508` (feat)

## Files Created/Modified
- `pkg/policy/builder.go` - BuildPolicy: flow-to-CNP transformation with direction-aware rule generation
- `pkg/policy/builder_test.go` - 9 unit tests covering all BuildPolicy behaviors
- `pkg/policy/merge.go` - MergePolicy: read-modify-write merging with port dedup
- `pkg/policy/merge_test.go` - 5 unit tests covering merge scenarios
- `pkg/policy/testdata/ingress_flow.go` - Test helpers for building flow.Flow proto structs
- `go.mod` - Added sigs.k8s.io/yaml + transitive deps for ciliumv2 CRD types
- `go.sum` - Updated checksums

## Decisions Made
- Used `LabelSelector.MatchLabels` for peer comparison in merge (EndpointSelector has no GetMatchLabels method)
- Single PortRule per rule containing all ports (not one PortRule per port) for cleaner YAML output
- Insertion-order tracking via `[]string` for deterministic rule ordering in output

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Resolved missing transitive dependencies for ciliumv2 CRD types**
- **Found during:** Task 1 RED (test compilation)
- **Issue:** Importing `ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"` pulled in prometheus, statedb, and protobuf transitive deps not in go.sum
- **Fix:** Ran `go mod tidy` to resolve all transitive dependencies
- **Files modified:** go.mod, go.sum
- **Verification:** Tests compile and run successfully
- **Committed in:** a538d4d (Task 1 RED commit)

**2. [Rule 1 - Bug] Fixed EndpointSelector API usage in merge peer matching**
- **Found during:** Task 2 GREEN (merge implementation)
- **Issue:** `EndpointSelector.GetMatchLabels()` does not exist; the type wraps `*slim_metav1.LabelSelector`
- **Fix:** Accessed `LabelSelector.MatchLabels` field directly with nil check
- **Files modified:** pkg/policy/merge.go
- **Verification:** All 5 merge tests pass
- **Committed in:** 8367508 (Task 2 GREEN commit)

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 bug)
**Impact on plan:** Both fixes necessary for correct compilation and runtime behavior. No scope creep.

## Issues Encountered
None beyond the auto-fixed deviations above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Policy builder and merge logic ready for use by output writer (Plan 03)
- BuildPolicy and MergePolicy exported and tested
- PolicyEvent type available for pipeline integration
- Test fixtures in testdata/ reusable by downstream tests

## Self-Check: PASSED

All 5 files verified present. All 4 commits verified in git log.

---
*Phase: 01-core-policy-engine*
*Completed: 2026-03-08*
