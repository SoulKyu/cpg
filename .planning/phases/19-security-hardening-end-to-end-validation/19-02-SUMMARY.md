---
phase: 19-security-hardening-end-to-end-validation
plan: 02
subsystem: testing
tags: [mcp, grpc, e2e, stdio, race-detector, hubble]

requires:
  - phase: 18-query-tools
    provides: all 8 MCP tools (3 session + 5 query) registered with final schemas/annotations
provides:
  - Real-subprocess MCP stdio e2e test infrastructure (fake Hubble gRPC relay, -race build-once binary helper, subprocess+tee client harness, drop-flow fixtures) shared with Plan 04's ungraceful variant
  - TestMCPE2EGracefulLifecycle -- full D-07 graceful lifecycle proof over real stdio under -race
  - SRV-01 handshake/schema/annotation proof on the real transport (D-10)
  - Byte-level stdout-purity re-validation on real stdio, redeeming 16-CONTEXT D-06
affects: [19-04-ungraceful-disconnect-variant]

tech-stack:
  added: []
  patterns:
    - "In-process fake gRPC service test double (observerpb.UnimplementedObserverServer embedded by value + net.Listen 127.0.0.1:0) for MCP e2e tests needing an external gRPC dependency without a real cluster"
    - "syncBuffer: mutex-guarded byte buffer for reading exec.Cmd-fed buffers (Stderr, an io.TeeReader target) concurrently with their background writer goroutines"
    - "Real-subprocess MCP e2e harness: exec.Cmd + StdinPipe/StdoutPipe + mcp.IOTransport (never mcp.CommandTransport) for explicit byte-level stdout-purity assertions"

key-files:
  created:
    - cmd/cpg/mcp_e2e_test.go
  modified: []

key-decisions:
  - "syncBuffer (mutex-guarded) instead of plain bytes.Buffer for exec.Cmd's Stderr and the stdout io.TeeReader target -- go test -race caught a genuine data race on the unguarded version during Task 2 verification"
  - "fakeRelay's started/cancelled signaling + snapshot() built in Task 1 even though only Plan 04's ungraceful variant asserts on cancelled -- interface-first per the plan, so Plan 04 is a pure consumer with zero infra edits"
  - "Mid-capture list_policies/list_dropped_flows/get_evidence assertions poll via require.Eventually against the aggregator's flush_interval tick, not a fixed sleep, to avoid a flaky race against the flush"

requirements-completed: [SRV-01, SRV-04]

duration: 23min
completed: 2026-07-21
---

# Phase 19 Plan 02: E2E Stdio Infrastructure + Graceful Lifecycle Summary

**Real `-race`-built `cpg mcp` subprocess driven over OS stdin/stdout pipes against an in-process fake Hubble gRPC relay, proving the full session lifecycle and the all-8-tool SRV-01 handshake on real stdio (not the in-memory transport).**

## Performance

- **Duration:** 23 min
- **Started:** 2026-07-21T18:16:54Z
- **Completed:** 2026-07-21T18:38:51Z
- **Tasks:** 2
- **Files modified:** 1 (created)

## Accomplishments

- Built the shared e2e infrastructure both this plan and Plan 04 (ungraceful variant) consume: an in-process fake Hubble relay (`observerpb.ObserverServer`, `GetFlows`-only, started/cancelled signaling via `snapshot()`), a `sync.Once`-guarded `go build -race` binary helper, a subprocess+tee `mcp.IOTransport` client harness, and drop-flow fixtures that actually flow through the real classifier (Pitfall 4: `Verdict`/`DropReasonDesc` set explicitly)
- `TestMCPE2EGracefulLifecycle`: drives `initialize -> tools/list -> start_session -> get_status -> 5 query tools mid-capture -> stop_session -> get_cluster_health post-stop -> close stdin -> exit 0` over the real subprocess under `-race`, with a real policy file landing on disk and a real cluster-health report with remediation URLs
- SRV-01 fully proven on real stdio: exactly 8 tools, non-empty description/inputSchema per tool, non-empty outputSchema per data-returning tool, D-10 annotation truth (5 query tools assert `OpenWorldHint=false`; the 3 session tools assert their own truth and never assert `OpenWorldHint`), and the `dropclass` enum scoped to `list_dropped_flows` only
- Byte-level stdout-purity re-validated line by line on real OS pipes -- the redemption of 16-CONTEXT D-06's deliberately deferred assertion
- Found and fixed a genuine data race (via `go test -race` itself, not by inspection) in the harness's own diagnostic buffers

## Task Commits

Each task was committed atomically:

1. **Task 1: Build the e2e infrastructure -- fake relay, -race build helper, subprocess/tee client harness, drop-flow fixtures** - `2648732` (feat)
2. **Task 2: Graceful lifecycle test + SRV-01 handshake/schema/annotation assertions** - `cade360` (feat, includes the Rule 1 race fix)

## Files Created/Modified

- `cmd/cpg/mcp_e2e_test.go` - Fake Hubble relay (`fakeRelay`/`newFakeRelay`/`snapshot`/`waitStarted`), `buildE2EBinary` (`-race` build-once helper), `e2eSession`/`startE2ESubprocess`/`waitExit` (subprocess+tee MCP client harness), `syncBuffer` (mutex-guarded buffer), `buildPolicyDeniedFlow`/`buildInfraClassDropFlow` (drop-flow fixtures), `assertStdoutPurity`, and `TestMCPE2EGracefulLifecycle`

## Decisions Made

- **syncBuffer over bytes.Buffer:** `exec.Cmd`'s internal stderr-copy goroutine and the MCP client's internal stdout-read loop (fed via `io.TeeReader`) both write into their target buffers for the lifetime of the subprocess/connection. A diagnostic read (a failed `require.NoError`'s message argument, or the final byte-purity check) can race a still-in-flight write. `go test -race` caught this on the first run against the unguarded version; fixed with a small mutex-guarded `syncBuffer` type (`Write`/`String`/`Bytes`, the latter returning a defensive copy).
- **Interface-first infra:** `fakeRelay.waitStarted`/`snapshot()` expose both the `started` and `cancelled` flags now, even though this plan's graceful test only needs to synchronize on `started` (to defeat Pitfall 3's async relay-dial race before trusting mid-capture query results). `cancelled` exists so Plan 04's ungraceful variant can assert on it directly with zero infrastructure edits -- confirmed by reading `19-04-PLAN.md`'s own `must_haves` before finishing this plan.
- **require.Eventually for flush-dependent assertions:** `list_policies`, `list_dropped_flows`, and `get_evidence` all depend on the aggregator's `flush_interval` (1s) ticker having fired at least once. Asserting on the very first call after `start_session` would be flaky; polling with `require.Eventually` (15s cap, 200ms tick) proves the fixture flowed through mid-capture without a fixed sleep. `get_cluster_health`'s mid-capture check needed no such polling -- it branches purely on session state (`capturing`), never on file existence.
- **Infra-class fixture reason:** `flowpb.DropReason_CT_MAP_INSERTION_FAILED` (confirmed in `pkg/dropclass/classifier.go` as `DropClassInfra`) was chosen for the second fixture flow, matching the existing `mcp_query_tools_test.go` cluster-health fixture's own reason name for consistency across the test suite.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Data race on exec.Cmd-fed diagnostic buffers**
- **Found during:** Task 2 (first `-race` run of `TestMCPE2EGracefulLifecycle`)
- **Issue:** `startE2ESubprocess` used plain `bytes.Buffer` values for both `cmd.Stderr` and the `io.TeeReader` target (`rawTee`). `exec.Cmd` spawns a background goroutine that continuously copies subprocess stderr into that buffer for the lifetime of the process; the MCP client's internal stdout-read loop does the same for `rawTee`. Reading either buffer (e.g. `stderrBuf.String()` as a `require.NoError` diagnostic message argument, evaluated eagerly regardless of whether the assertion fails) from the test goroutine while those background goroutines are still writing is a genuine, `-race`-detected data race.
- **Fix:** Added a `syncBuffer` type (mutex-guarded `Write`/`String`/`Bytes`, with `Bytes()` returning a defensive copy rather than an alias into the internal buffer) and switched `e2eSession.stderr`, `e2eSession.rawTee`, and their corresponding locals in `startE2ESubprocess` to use it.
- **Files modified:** `cmd/cpg/mcp_e2e_test.go` (same file, part of Task 2's commit)
- **Verification:** `rtk proxy go test ./cmd/cpg/... -run TestMCPE2EGracefulLifecycle -race -count=3 -v` passed 3/3 after the fix (previously failed on run 1 with two reported data races); full package (`rtk proxy go test ./cmd/cpg/... -race -count=1`) and full module (`rtk proxy go test ./... -count=1 -race`) both green afterward.
- **Committed in:** `cade360` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug)
**Impact on plan:** Necessary for correctness under `-race` -- exactly the guarantee SRV-04 requires ("the whole test file runs under the suite's `-race`"). No scope creep: the fix is confined to the harness's own internal buffers, does not change any assertion, tool call, or fixture.

## Issues Encountered

None beyond the data race documented above, which was found and fixed within the normal Rule 1 auto-fix budget (one fix attempt, verified immediately).

## User Setup Required

None -- no external service configuration required.

## Verification Evidence

- `rtk proxy go test ./cmd/cpg/... -run '^$' -count=1` -- compiles (Task 1 gate): PASS
- `rtk proxy go test ./cmd/cpg/... -run TestMCPE2EGracefulLifecycle -race -count=1` (and `-count=3` for flakiness): PASS, ~2.3-7s per run (first run pays the one-time `-race` binary build)
- `rtk proxy go vet ./cmd/cpg/...`: clean
- `rtk proxy golangci-lint run ./cmd/cpg/...`: 0 issues (the transient `unused` warnings after Task 1 alone resolved once Task 2 called every helper)
- `rtk proxy go test ./cmd/cpg/... -race -count=1` (full package, D-11 regression check): PASS, all existing in-memory tests (`mcp_harness_test.go`, `mcp_session_test.go`, `mcp_query_tools_test.go`, etc.) stay green
- `rtk proxy go test ./... -count=1 -race` (full module regression): PASS across all 12 packages
- `go.mod`/`go.sum`: unchanged (every import -- `google.golang.org/grpc`, `github.com/cilium/cilium/api/v1/{flow,observer}`, `github.com/SoulKyu/cpg/pkg/{policy/testdata,session}` -- was already a direct project dependency; no `credentials/insecure` import needed since `grpc.NewServer()` serves plaintext by default with no explicit credential option)

## Next Phase Readiness

- Shared e2e infrastructure (`fakeRelay`, `buildE2EBinary`, `e2eSession`/`startE2ESubprocess`/`waitExit`, `syncBuffer`, the drop-flow fixtures) is in place in `cmd/cpg/mcp_e2e_test.go` for Plan 04 (Wave 2, `depends_on: ["19-02"]`) to add `TestMCPE2EUngracefulDisconnect` as a pure consumer -- no infra edits expected.
- SRV-01 is now fully satisfied (real-stdio handshake/schema/annotation proof). SRV-04's graceful half is satisfied; the ungraceful half is Plan 04's remaining scope.
- No blockers for Plan 04 or for the phase's remaining plans (19-01 SEC-01 audit, 19-03 README).

---
*Phase: 19-security-hardening-end-to-end-validation*
*Completed: 2026-07-21*

## Self-Check: PASSED

- FOUND: `cmd/cpg/mcp_e2e_test.go`
- FOUND: `.planning/phases/19-security-hardening-end-to-end-validation/19-02-SUMMARY.md`
- FOUND commit: `2648732` (Task 1)
- FOUND commit: `cade360` (Task 2)
- Grep-verified: `flow.Verdict = flowpb.Verdict_DROPPED` appears 2x (both fixture builders)
- Grep-verified: `func TestMCPE2EGracefulLifecycle` defined exactly once
- Grep-verified: zero actual uses of `InMemoryTransport`, `startInMemoryMCPSession`, `connectQueryTestClient`, `initLoggerForTesting` (the two `mcp.CommandTransport` matches are prose in doc comments explaining why it is NOT used)
