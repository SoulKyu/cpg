---
phase: 17-session-lifecycle
plan: 06
subsystem: session
tags: [mcp, session-lifecycle, concurrency, context-cancellation, race-detector, go, gap-closure]

# Dependency graph
requires:
  - phase: 17-session-lifecycle (plans 01-05)
    provides: "pkg/session's Session/Manager state machine (Start/Status/Stop/Shutdown), the TOCTOU-safe slot claim, and 17-05's pipelineErr/autonomous-State-transition data layer this plan's setupCtx merge sits alongside"
provides:
  - "context.AfterFunc(sessionCtx, setupCancel) merge in Manager.Start — Shutdown's s.cancel() now reaches a mid-setup resolveSetupFn's setupCtx, closing WR-02 (Truth 4 / SESS-05 reopened gap)"
  - "TestManager_Start_ShutdownCancelsSetupCtx — a -race test whose fake resolveSetupFn blocks on <-setupCtx.Done() (not a manual release channel), independently confirmed to FAIL against the pre-fix code and PASS with the fix"
  - "maxSessionDuration = 24h ceiling in parseOptionalDuration, applied to both start_session's timeout and flush_interval, closing WR-03"
  - "TestParseOptionalDuration — table-driven test proving the new ceiling while regression-guarding the pre-existing empty/positive/non-positive branches"
affects: [session-lifecycle, query-tools]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "context.AfterFunc(parentCtx, childCancelFunc) merges two independently-scoped context trees post-hoc, without reparenting the child's own WithTimeout call — preserves both of the child's existing bounds (its own timeout + its own parent) while adding a third cancellation source"
    - "The stop func returned by context.AfterFunc is always captured and deferred, releasing the registration once the awaited operation (resolveSetupFn) completes normally instead of leaving a live registration for the remainder of the process"
    - "Table-driven parseXxx unit test asserting both accept and reject branches of a single-parameter validation function, mirroring cmd/cpg/commonflags_test.go's existing style"

key-files:
  created: []
  modified:
    - pkg/session/manager.go
    - pkg/session/manager_test.go
    - cmd/cpg/mcp_tools.go
    - cmd/cpg/mcp_session_test.go

key-decisions:
  - "AfterFunc merge chosen over reparenting setupCtx onto sessionCtx (17-REVIEW.md's alternative fix sketch) — preserves the per-call reqCtx bound and the existing Pitfall H WithTimeout(reqCtx, timeout) construction untouched, only adding the third cancellation signal"
  - "24h ceiling applied uniformly to both timeout and flush_interval via one maxSessionDuration const, since both flow through the same parseOptionalDuration parser — simpler than two separate constants"
  - "k8s.LoadKubeConfig's no-ctx residual (T-17-06-03) accepted as a documented, out-of-scope upstream limitation rather than refactored — matches the gap brief's explicit scope boundary; it does not block process exit since Shutdown's own fan-out is independently bounded"

requirements-completed: [SESS-05]

# Metrics
duration: ~14min
completed: 2026-07-21
---

# Phase 17 Plan 06: Setup-Phase Shutdown Cancellation + Duration Ceiling Summary

**context.AfterFunc(sessionCtx, setupCancel) merges Shutdown's cancellation into an in-flight Start()'s setup phase, and a 24h maxSessionDuration ceiling bounds MCP-supplied timeout/flush_interval — closing reopened gaps WR-02 (Truth 4 / SESS-05) and WR-03.**

## Performance

- **Duration:** ~14 min (approx.)
- **Started:** 2026-07-21T07:26:00Z (approx., worktree spawn + context reads)
- **Completed:** 2026-07-21T07:40:23Z
- **Tasks:** 3
- **Files modified:** 4

## Accomplishments

- `context.AfterFunc(sessionCtx, setupCancel)` registered immediately after `setupCtx`'s own `WithTimeout(reqCtx, timeout)`, stop func captured and deferred — `Shutdown()`'s `s.cancel()` now reaches a mid-setup `Start()` call, so a `resolveSetupFn` step that observes its ctx (`PortForwardToRelay`, `LoadClusterPoliciesForNamespaces`) aborts promptly instead of running to setupCtx's own timeout; the per-call `reqCtx` bound and the setup timeout are both retained unchanged
- `TestManager_Start_ShutdownCancelsSetupCtx` — a `-race` test whose fake `resolveSetupFn` blocks on `<-setupCtx.Done()` (the load-bearing difference from `TestManager_Start_ShutdownRacesSetup`'s manual release channel); uses `Timeout: 30*time.Second` + `context.Background()` reqCtx so setupCtx's own timeout is irrelevant to the test's ~800ms bounded window — independently confirmed to **FAIL** (0.90s, bounded-timeout `Fatal`) with Task 1's fix temporarily reverted, and **PASS** (0.11s) with it restored
- `maxSessionDuration = 24 * time.Hour` const in `cmd/cpg/mcp_tools.go`; `parseOptionalDuration` now rejects `d > maxSessionDuration` with `"%s must be <= %s, got %q"`, after the pre-existing `d <= 0` rejection — bounds both `timeout` and `flush_interval` since both flow through this one parser; `startSessionArgs` jsonschema descriptions updated to document "max 24h" to the calling LLM
- `TestParseOptionalDuration` (8 table-driven cases: empty, normal, negative, zero, malformed, exactly-at-ceiling, above-ceiling, one-second-above-ceiling) plus a field-name-threading assertion for `flush_interval`
- `k8s.LoadKubeConfig`'s no-ctx residual (T-17-06-03) documented in a code comment at the merge point, not silently left unaddressed
- Whole-repo `go test ./... -race -count=1` and `go vet ./pkg/session/... ./cmd/cpg/...` stay green; `golangci-lint run --new-from-rev=9e5c08920a5d31b2fd6c6700c3861238eb62c27e` on both changed packages returns 0 new issues (the 6 pre-existing errcheck findings in unrelated `cmd/cpg/explain_render.go` are untouched, already tracked as v1.5 LINT-01..03 debt in PROJECT.md)
- No new dependencies: `go.mod`/`go.sum` unchanged — uses only stdlib `context.AfterFunc` (Go 1.21+, module toolchain is go1.25.12), matching T-17-06-SC's accepted disposition

## Task Commits

Each task was committed atomically:

1. **Task 1: Make setupCtx cancellable by Shutdown — merge sessionCtx into setupCtx** - `f8f9bdd` (feat)
2. **Task 2: Prove Shutdown cancels mid-setup with a ctx-inspecting -race test** - `9147a2e` (test)
3. **Task 3: Bound MCP-supplied durations above (WR-03) in parseOptionalDuration + test** - `bf3012c` (feat)

**Plan metadata:** committed alongside this SUMMARY.md (worktree/parallel mode — STATE.md/ROADMAP.md are updated centrally by the orchestrator after merge, not here).

## Files Created/Modified

- `pkg/session/manager.go` - `context.AfterFunc(sessionCtx, setupCancel)` merge right after `setupCtx`'s `WithTimeout(reqCtx, timeout)`, deferred stop func, explanatory comment documenting the three-signal bound (own timeout + reqCtx + sessionCtx) and the `k8s.LoadKubeConfig` residual
- `pkg/session/manager_test.go` - `TestManager_Start_ShutdownCancelsSetupCtx`: ctx-inspecting fake `resolveSetupFn`, 30s `Timeout` + `Background()` reqCtx isolates the assertion to Shutdown's cancellation reaching setupCtx, bounded-return + no-orphaned-tmpdir assertions
- `cmd/cpg/mcp_tools.go` - `maxSessionDuration = 24 * time.Hour` const with doc comment; `parseOptionalDuration` rejects `d > maxSessionDuration`; `startSessionArgs.Timeout`/`FlushInterval` jsonschema descriptions mention the ceiling
- `cmd/cpg/mcp_session_test.go` - `TestParseOptionalDuration` table-driven test (8 cases + a field-name-threading check for `flush_interval`)

## Decisions Made

- **AfterFunc merge over reparenting:** kept `setupCtx, setupCancel := context.WithTimeout(reqCtx, timeout)` exactly as-is and merged `sessionCtx` in alongside it via `context.AfterFunc`, rather than switching the parent to `sessionCtx` (17-REVIEW.md's other fix sketch) — preserves the per-call `reqCtx`/`$/cancelRequest` bound with zero risk of silently dropping it.
- **One ceiling constant for both duration fields:** `maxSessionDuration` bounds `timeout` and `flush_interval` identically via the shared `parseOptionalDuration` parser, rather than two separate ceilings — simpler, and both fields have the same practical upper bound (no real capture session runs longer than 24h).
- **`k8s.LoadKubeConfig` residual left unaddressed:** per the plan's explicit scope boundary (T-17-06-03, accepted disposition) — refactoring that upstream helper's signature to accept a ctx is out of scope for this gap-closure plan; it is low-severity because it cannot block process exit (Shutdown's own fan-out is independently bounded).

## Deviations from Plan

None - plan executed exactly as written. All three tasks' acceptance criteria passed on first implementation; no bugs, missing functionality, blocking issues, or architectural questions arose during execution.

## Issues Encountered

None. `go build`, `go vet`, and `go test ./... -race -count=1` were green throughout; `golangci-lint --new-from-rev` confirmed zero new lint issues in the two changed packages.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- WR-02 (Truth 4 / SESS-05 reopened gap) and WR-03 are closed: `Shutdown()` now aborts a mid-setup `Start()` whose `resolveSetupFn` observes its context, and MCP-supplied `timeout`/`flush_interval` above 24h are rejected.
- Combined with 17-05 (WR-01/WR-04, SESS-03) and 17-07 (D-02 documentation reconciliation, SESS-06), all reopened findings from `17-VERIFICATION.md`'s `gaps_found` status that were assigned to gap-closure plans are now addressed at the code level. IN-01 (`outputHash`/`healthPath` formula duplication, Info-tier) remains unaddressed — it was not assigned a gap-closure plan and does not block correctness.
- No blockers for Phase 18 (Query Tools): this plan touches only `Manager.Start`'s setup-phase cancellation wiring and MCP argument validation, not any artifact-reading surface Phase 18 depends on.
- A future full re-verification of Phase 17 should re-check Truths 2 and 4 against the now-closed WR-01/WR-02/WR-03/WR-04 fixes.

---
*Phase: 17-session-lifecycle*
*Completed: 2026-07-21*

## Self-Check: PASSED

- FOUND: pkg/session/manager.go
- FOUND: pkg/session/manager_test.go
- FOUND: cmd/cpg/mcp_tools.go
- FOUND: cmd/cpg/mcp_session_test.go
- FOUND commit: f8f9bdd (Task 1)
- FOUND commit: 9147a2e (Task 2)
- FOUND commit: bf3012c (Task 3)
- Re-ran all task acceptance criteria: PASS (rg pattern checks for AfterFunc(sessionCtx, WithTimeout(reqCtx, timeout), maxSessionDuration, "must be <=", jsonschema "max 24h" mentions — all matched as specified)
- Re-ran plan-level `<verification>`: `go test ./pkg/session/... -race -count=1` PASS; `go test ./cmd/cpg/... -race -count=1` PASS; `go test ./... -race -count=1` PASS (11/11 packages); `go vet ./pkg/session/... ./cmd/cpg/...` clean
- Independently re-confirmed the setup-cancellation test's bounded-return assertion fails against the pre-fix code (0.90s FAIL) and passes with the fix restored (0.11s PASS)
- `git status --short` clean; no stray/untracked files; no unexpected deletions in any task commit
