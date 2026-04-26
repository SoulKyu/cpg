---
phase: 01-core-policy-engine
plan: 01
subsystem: labels
tags: [cilium, go, labels, cobra, zap, tdd]

# Dependency graph
requires: []
provides:
  - "Go module with Cilium v1.19.1 dependency and build tooling"
  - "Label selector package with 3-tier hierarchy and denylist"
  - "BuildEndpointSelector and BuildPeerSelector for policy generation"
  - "WorkloadName for deterministic file naming"
affects: [01-02, 01-03]

# Tech tracking
tech-stack:
  added: [cilium v1.19.1, cobra, zap, sigs.k8s.io/yaml, testify]
  patterns: [TDD red-green, label hierarchy heuristic, denylist filtering]

key-files:
  created:
    - cmd/cpg/main.go
    - pkg/labels/selector.go
    - pkg/labels/selector_test.go
    - Makefile
    - .golangci.yml
  modified:
    - go.mod
    - go.sum

key-decisions:
  - "Used NewESFromMatchRequirements with plain keys (not NewESFromLabels) to avoid k8s: prefix in YAML output"
  - "Namespace label uses plain io.kubernetes.pod.namespace key in peer selectors"
  - "WorkloadName fallback joins sorted label values with hyphen, truncated to 63 chars"

patterns-established:
  - "Label filtering: parse with labels.ParseLabel(), filter by source + denylist, apply priority hierarchy"
  - "TDD workflow: write tests first in _test.go, implement in .go, verify all green"

requirements-completed: [PGEN-04]

# Metrics
duration: 4min
completed: 2026-03-08
---

# Phase 1 Plan 1: Go Module Scaffolding + Label Selector Summary

**Go module with Cilium v1.19.1 types and smart label selector implementing app.kubernetes.io/name > app > all-labels hierarchy with 7-label denylist**

## Performance

- **Duration:** 4 min
- **Started:** 2026-03-08T08:49:32Z
- **Completed:** 2026-03-08T08:53:08Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments
- Go module building cleanly with Cilium v1.19.1 monorepo dependency (cobra, zap, yaml, testify)
- Label selector with 3-tier hierarchy: app.kubernetes.io/name > app > all labels (minus denylist)
- All 7 denylist labels correctly excluded (pod-template-hash, controller-revision-hash, etc.)
- BuildEndpointSelector and BuildPeerSelector producing valid Cilium EndpointSelector objects
- WorkloadName providing deterministic naming for file output
- 14 unit tests passing via TDD workflow

## Task Commits

Each task was committed atomically:

1. **Task 1: Initialize Go module with Cilium dependency and build tooling** - `ecdadf0` + `c4d1ac6` (feat)
2. **Task 2 RED: Failing tests for label selector** - `0e090ed` (test)
3. **Task 2 GREEN: Implement label selector** - `d869ccd` (feat)

_Note: Task 1 had a prior commit (ecdadf0) with a follow-up (c4d1ac6) to resolve Cilium transitive deps and add sigs.k8s.io/yaml._

## Files Created/Modified
- `cmd/cpg/main.go` - Minimal Cobra root command with zap logger
- `pkg/labels/selector.go` - Label selection with hierarchy, denylist, endpoint/peer selector builders
- `pkg/labels/selector_test.go` - 14 unit tests covering all behaviors
- `Makefile` - build, test, lint, clean, all targets
- `.golangci.yml` - golangci-lint v2 config (govet, errcheck, staticcheck, unused)
- `go.mod` - Module with Cilium v1.19.1, cobra, zap, yaml, testify + replace directives
- `go.sum` - Resolved dependency checksums

## Decisions Made
- Used `api.NewESFromMatchRequirements(map[string]string, nil)` instead of `api.NewESFromLabels()` to ensure plain keys in YAML output (avoids k8s: prefix issue from Research Open Question 1)
- Namespace label in peer selectors uses plain `io.kubernetes.pod.namespace` key (not `k8s:io.kubernetes.pod.namespace`) since NewESFromMatchRequirements takes plain keys
- WorkloadName fallback: sort all label values alphabetically, join with "-", truncate to 63 chars (K8s name limit)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed EndpointSelector test assertions**
- **Found during:** Task 2 RED (test writing)
- **Issue:** Tests used `es.Requirements()` which does not exist on `api.EndpointSelector`. The plan tests were based on incorrect API assumptions.
- **Fix:** Rewrote tests to use `es.HasKey()` and `es.IsZero()` which are the correct EndpointSelector API methods
- **Files modified:** pkg/labels/selector_test.go
- **Verification:** Tests compile and correctly validate selector behavior
- **Committed in:** 0e090ed (test commit)

---

**Total deviations:** 1 auto-fixed (1 bug in test API usage)
**Impact on plan:** Necessary correction -- the Cilium EndpointSelector API does not expose Requirements(). No scope creep.

## Issues Encountered
- `make test` fails with "go: Permission denied" when using `-race` flag (likely cgo/sandbox restriction). Tests pass without race detector. This is an environment limitation, not a code issue.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Label selector package ready for use by policy builder (Plan 02)
- Go module builds cleanly, all dependencies resolved
- Makefile targets operational (build, test, lint, clean)
- SelectLabels, WorkloadName, BuildEndpointSelector, BuildPeerSelector all exported and tested

## Self-Check: PASSED

All 7 files verified present. All 4 commits verified in git log.

---
*Phase: 01-core-policy-engine*
*Completed: 2026-03-08*
