---
phase: 02-hubble-streaming-pipeline
plan: 01
subsystem: streaming
tags: [grpc, hubble, cilium, channels, streaming]

requires:
  - phase: 01-core-policy-engine
    provides: flowpb types, policy builder, labels package
provides:
  - Hubble gRPC client with StreamDroppedFlows
  - FlowFilter construction for namespace-aware dropped flow filtering
  - Typed channels for flow and lost event dispatch
affects: [02-02, pipeline-orchestration]

tech-stack:
  added: [google.golang.org/grpc (direct), golang.org/x/sync (direct)]
  patterns: [interface-based mock for gRPC streams, variadic closer pattern]

key-files:
  created: [pkg/hubble/client.go, pkg/hubble/client_test.go]
  modified: [go.mod, go.sum]

key-decisions:
  - "Interface-based flowStream abstraction for testability (avoids bufconn complexity)"
  - "Variadic closer pattern to pass grpc.ClientConn cleanup into streaming goroutine"
  - "Buffered channels: flows=256, lostEvents=16 to absorb burst traffic"

patterns-established:
  - "flowStream interface: Recv() + Context() for testable gRPC stream consumers"
  - "streamFromSource as reusable dispatcher from any flowStream to typed channels"

requirements-completed: [CONN-01, CONN-03, CONN-04]

duration: 3min
completed: 2026-03-08
---

# Phase 2 Plan 1: Hubble gRPC Client Summary

**gRPC streaming client connecting to Hubble Relay with namespace-aware FlowFilter construction and typed channel dispatch**

## Performance

- **Duration:** 3 min
- **Started:** 2026-03-08T19:29:56Z
- **Completed:** 2026-03-08T19:33:00Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments
- buildFilters constructs OR-ed FlowFilter entries handling all-namespaces, single/multiple namespace, and empty namespace cases
- StreamDroppedFlows connects to Hubble Relay via gRPC with TLS support and dispatches to typed channels
- 8 unit tests pass with race detector covering filter construction and streaming behavior

## Task Commits

Each task was committed atomically:

1. **Task 1: Implement buildFilters and unit tests** - `b88a5ff` (feat)
2. **Task 2: Implement StreamDroppedFlows with gRPC streaming** - `4611504` (feat)

## Files Created/Modified
- `pkg/hubble/client.go` - Hubble gRPC client with Client, NewClient, StreamDroppedFlows, buildFilters, flowStream interface
- `pkg/hubble/client_test.go` - 8 tests: 4 for filter construction, 4 for streaming behavior
- `go.mod` - Promoted grpc and x/sync to direct dependencies
- `go.sum` - Updated checksums

## Decisions Made
- Used interface-based `flowStream` abstraction instead of bufconn for gRPC mock testing -- simpler, faster, no test server lifecycle
- Variadic `closer` parameter on `streamFromSource` to avoid goroutine draining channels for connection cleanup
- Context-aware `select` on all channel sends to prevent goroutine leaks on shutdown

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed goroutine draining flows channel for connection cleanup**
- **Found during:** Task 2 (StreamDroppedFlows implementation)
- **Issue:** Initial implementation started a goroutine that drained the flows channel to detect stream end and close grpc.ClientConn -- this would consume messages meant for the caller
- **Fix:** Introduced variadic `closer` parameter on `streamFromSource` so the streaming goroutine itself handles connection cleanup via deferred Close()
- **Files modified:** pkg/hubble/client.go
- **Verification:** All tests pass with race detector
- **Committed in:** 4611504 (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug)
**Impact on plan:** Essential correctness fix. No scope creep.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Client ready for integration with pipeline orchestration (plan 02-02)
- StreamDroppedFlows returns channels compatible with aggregator stage pattern from research
- Existing Phase 1 tests unaffected (full suite green)

---
*Phase: 02-hubble-streaming-pipeline*
*Completed: 2026-03-08*

## Self-Check: PASSED
