---
phase: 19-security-hardening-end-to-end-validation
plan: 04
subsystem: testing
tags: [mcp, grpc, e2e, stdio, race-detector, hubble, session-lifecycle]

requires:
  - phase: 19-security-hardening-end-to-end-validation
    provides: "Plan 02's shared e2e infra -- fake Hubble relay (started/cancelled signaling + snapshot()), -race build-once helper, subprocess+tee client harness, drop-flow fixtures"
provides:
  - "TestMCPE2EUngracefulDisconnect -- the ungraceful-disconnect half of SRV-04 (D-08): proves the SESS-05 bounded cleanup fan-out fires on transport death with no stop_session call (process self-exit, tmpdir removal, relay stream cancellation)"
  - "D-09 honest port-forward-to-stream-cancel mapping, documented in-test so the phase verifier does not flag a phantom port-forward gap"
  - "An artifact-based synchronization pattern (wait for a real policy file on disk, not just a 'relay reached' signal) for tests that need the fake relay's fixture-send loop to have fully completed before tearing down the transport"
affects: []

tech-stack:
  added: []
  patterns:
    - "Artifact-based synchronization gate: before triggering a transport disconnect that a test's assertions depend on the relay having fully sent its fixtures, poll (require.Eventually) for the resulting on-disk artifact rather than relying solely on a 'handler was invoked' signal -- the invocation signal alone does not guarantee the handler's own send loop has finished"

key-files:
  created: []
  modified:
    - cmd/cpg/mcp_e2e_test.go

key-decisions:
  - "Added a second, stronger synchronization gate beyond relay.waitStarted(): wait for the POLICY_DENIED fixture to land as a real policy file (session.DeriveSessionPaths + require.Eventually) before closing stdin. waitStarted alone only proves GetFlows was invoked, not that its two-flow send loop finished -- disconnecting immediately after waitStarted races that loop and the relay never reaches (or sets) its cancelled flag."
  - "Set flush_interval: \"1s\" on start_session (matching the graceful test) so the aggregator's ticker flushes the fixture to disk quickly enough for the new synchronization gate to resolve fast."
  - "Asserted exitErr == nil for the ungraceful disconnect too (mirroring the graceful test): server.Run's jsonrpc2 peer-EOF-is-not-an-error semantics are unconditional on session state, so a clean exit 0 is the technically correct expectation here as well, not just for the graceful path. Verified empirically across 5 repeated -race runs."
  - "Kept the relay-cancelled assertion behind a short require.Eventually (5s) even after adding the stronger pre-disconnect sync: cross-process gRPC stream cancellation delivery is a genuine network event, so a bounded poll is the technically correct way to observe it rather than an instantaneous read."

requirements-completed: [SRV-04]

duration: 18min
completed: 2026-07-21
---

# Phase 19 Plan 04: Ungraceful-Disconnect E2E Variant Summary

**TestMCPE2EUngracefulDisconnect proves the SESS-05 bounded cleanup fan-out fires on transport death (no stop_session): bounded self-exit, tmpdir removal, and fake-relay stream cancellation -- stabilized against a real, empirically-reproduced async race by adding an artifact-based synchronization gate beyond the planned relay-reached signal.**

## Performance

- **Duration:** 18 min
- **Started:** 2026-07-21T18:45:38Z
- **Completed:** 2026-07-21T19:03:41Z
- **Tasks:** 1
- **Files modified:** 1

## Accomplishments

- `TestMCPE2EUngracefulDisconnect` added to `cmd/cpg/mcp_e2e_test.go`, reusing Plan 02's fake relay, `-race` build-once helper, subprocess+tee client harness, and drop-flow fixtures with zero infrastructure edits
- Drives `start_session -> get_status` against the fake relay, then abruptly closes stdin with **no** `stop_session` call, and asserts within bounded deadlines: the process self-exits (10s cap), the session tmpdir is removed (`require.NoDirExists`), and the fake relay's `GetFlows` stream context was cancelled (`snapshot().cancelled == true`)
- D-09's honest port-forward-to-stream-cancel mapping is documented directly in the test's doc comment and inline near the cancellation assertion, so the phase verifier does not flag a phantom port-forward gap
- Found and fixed a second, previously-undocumented async race (beyond Pitfall 3's relay-reached race) via actual empirical test execution: synchronizing only on "the relay was reached" is not sufficient to reliably observe "the relay's stream was cancelled" -- see Deviations below
- Verified stable: 5/5 passes under `-race -count=5` (the plan's own flake-resistance gate), both e2e variants together, full `cmd/cpg` package, and full module regression, all green; `go vet` and `golangci-lint` clean

## Task Commits

Each task was committed atomically:

1. **Task 1: Add the ungraceful-disconnect variant with relay-reached synchronization and bounded cleanup assertions** - `140d25f` (feat)

## Files Created/Modified

- `cmd/cpg/mcp_e2e_test.go` - Added `TestMCPE2EUngracefulDisconnect`: drives `start_session`/`get_status` against the fake relay, synchronizes on `relay.waitStarted` (Pitfall 3) plus a new artifact-based gate (waits for the POLICY_DENIED fixture's policy file to land on disk before disconnecting), closes stdin with no `stop_session`, then asserts bounded self-exit, `NoDirExists(tmp_dir)`, and `snapshot().cancelled == true`

## Decisions Made

- **Artifact-based second synchronization gate (the core fix):** `relay.waitStarted()` (Plan 02's shared signal) only proves `fakeRelay.GetFlows` was *invoked* -- it fires before the handler's own `for _, flow := range f.flows { stream.Send(...) }` loop runs. Disconnecting immediately after `waitStarted` returns races that loop: if the transport tears down mid-loop, `stream.Send` returns a non-nil error and `GetFlows` takes its early `return err` path, **never** reaching (or setting) `cancelled`. Fixed by waiting (`require.Eventually`, 15s bound) for the POLICY_DENIED fixture to actually appear as a real policy file under `session.DeriveSessionPaths(tmp_dir)` before closing stdin -- an artifact reaching disk requires far more wall-clock time (network receive, classify, aggregate, flush-ticker write) than the relay's two back-to-back in-memory `Send()` calls that precede its blocking wait, so by the time the artifact exists, the relay is reliably already parked on `<-stream.Context().Done()`.
- **`flush_interval: "1s"` on `start_session`:** needed so the aggregator's ticker flushes the fixture to disk quickly enough for the new synchronization gate to resolve within a reasonable bound (policy writes are ticker-gated, not per-flow -- confirmed by reading `pkg/hubble/aggregator.go`).
- **`assert.NoError(t, exitErr, ...)` for the ungraceful path too:** `cmd/cpg/mcp.go`'s `runMCPServer` returns whatever `server.Run(ctx, transport)` returns, and go-sdk's jsonrpc2 layer treats a peer-initiated clean `io.EOF` as not-an-error unconditionally on session state -- so a clean exit 0 is the technically correct expectation for the ungraceful disconnect too, not just the graceful one. Confirmed empirically across 5 repeated `-race` runs (never failed).
- **Kept a short `require.Eventually` (5s) around the final `cancelled` check** even after adding the stronger pre-disconnect artifact gate: delivering the client's cancellation to the relay's server-side stream context is still a genuine cross-process network event, so a bounded poll remains the technically correct way to observe it.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Async race: `relay.waitStarted()` alone is insufficient to reliably observe `cancelled == true`**
- **Found during:** Task 1, first `-race` run of `TestMCPE2EUngracefulDisconnect` (written per the plan's literal guidance: synchronize on `relay.waitStarted(t, 5*time.Second)`, then immediately close stdin)
- **Issue:** The first run failed with `Condition never satisfied` / `Should be true` on the `cancelled` assertion. Diagnostic logging of the subprocess's own stderr revealed the root cause: the subprocess's session summary reported `"flows_seen": 0` and a `"duration": "3.268214ms"` -- the whole session had been torn down before the fake relay's fixture-send loop (`for _, flow := range f.flows { stream.Send(...) }`, which runs *before* the relay blocks on `<-stream.Context().Done()`) could complete. `relay.waitStarted()` fires the instant `GetFlows` is invoked, which is *before* that send loop runs -- closing stdin immediately afterward raced the loop. When the transport tore down mid-loop, `stream.Send` returned a non-nil error, and `GetFlows` took its early `return err` path, which never reaches (and never sets) `cancelled`. A first fix attempt (wrapping the final assertion alone in `require.Eventually`) did not help, because the race is not "cancelled becomes true a little later" -- it is "cancelled is never going to become true because GetFlows already returned via a different code path."
- **Fix:** Added a second, stronger synchronization gate between `relay.waitStarted()` and the stdin close: `require.Eventually` (15s bound, 200ms tick) polling for the POLICY_DENIED fixture's policy file to exist under `session.DeriveSessionPaths(tmp_dir)`. This proves the relay's fixture-send loop already completed (an on-disk artifact takes far longer to appear than two in-memory `Send()` calls), so disconnecting afterward reliably lets `GetFlows` reach its blocking `<-stream.Context().Done()` line before any cancellation can race it. Also set `flush_interval: "1s"` on `start_session` so the aggregator's ticker (which gates policy writes, confirmed via `pkg/hubble/aggregator.go`) flushes quickly enough for this gate to resolve fast.
- **Files modified:** `cmd/cpg/mcp_e2e_test.go` (same file, part of the Task 1 commit)
- **Verification:** `rtk proxy go test ./cmd/cpg/... -run TestMCPE2EUngracefulDisconnect -race -count=5 -v` -- 5/5 pass after the fix (previously failed deterministically on every attempt before the fix, including a first-attempt fix using only a bounded poll with no stronger sync gate). Full `cmd/cpg` package (`-race -count=1`) and full module (`go test ./... -count=1 -race`) both green afterward. `go vet` and `golangci-lint run ./cmd/cpg/...` both clean.
- **Committed in:** `140d25f` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 bug, found and fixed via actual empirical test execution, not by inspection)
**Impact on plan:** Necessary for correctness and the exact flake-resistance guarantee SRV-04's own verify step demands (`-count=5`). The plan's specified `relay.waitStarted()` synchronization (Pitfall 3) remains in place and necessary -- the fix adds a second, complementary gate rather than replacing it. No scope creep: the fix is confined to this test's own pre-disconnect sequencing and does not touch Plan 02's shared infrastructure (fakeRelay, build helper, subprocess harness, fixtures) at all, matching the plan's "reuses ... with zero infra edits" constraint.

## Issues Encountered

None beyond the race documented above, which was root-caused (via subprocess stderr diagnostics, not guesswork) and fixed within the normal auto-fix budget.

## User Setup Required

None -- no external service configuration required.

## Verification Evidence

- `rtk proxy go build ./...` -- clean
- `rtk proxy go test ./cmd/cpg/ -run TestMCPE2EUngracefulDisconnect -count=1 -race -v` -- PASS after the fix (failed twice before it, with full diagnostic root-causing in between)
- `rtk proxy go test ./cmd/cpg/... -run TestMCPE2EUngracefulDisconnect -race -count=5 -v` (the plan's own verify command): PASS 5/5, ~2.3-6.5s per run (first run pays the one-time `-race` binary build)
- `rtk proxy go test ./cmd/cpg/... -run TestMCPE2E -race -count=1 -v` (both e2e variants together): PASS
- `rtk proxy go test ./cmd/cpg/... -race -count=1` (full package, D-11 regression check): PASS, 71.186s, all existing tests (including the SEC-01 audit test from a sibling plan) stay green
- `rtk proxy go test ./... -count=1 -race` (full module regression): PASS across all 12 packages
- `rtk proxy go vet ./cmd/cpg/...`: clean
- `rtk proxy golangci-lint run ./cmd/cpg/...`: 0 issues
- `go.mod`/`go.sum`: unchanged (no new imports beyond what Plan 02 already brought in; this plan's test uses only already-imported packages)

## Next Phase Readiness

- SRV-04 is now fully satisfied: Plan 02's graceful lifecycle plus this plan's ungraceful-disconnect variant together cover the full D-07/D-08 e2e contract, both stable under `-race -count=5`.
- D-09's port-forward-to-stream-cancel mapping is documented in-test; the phase verifier should not flag a phantom port-forward gap for this plan.
- The artifact-based synchronization pattern established here (wait for a real on-disk effect, not just an invocation signal, before tearing down a transport the test depends on) is available for any future e2e test in this package that needs similar pre-disconnect sequencing.
- No blockers for phase closure (19-01 SEC-01 audit and 19-03 README were parallel/independent plans in this phase).

---
*Phase: 19-security-hardening-end-to-end-validation*
*Completed: 2026-07-21*
