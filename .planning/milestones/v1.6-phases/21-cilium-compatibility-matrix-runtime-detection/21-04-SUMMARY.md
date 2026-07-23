---
phase: 21-cilium-compatibility-matrix-runtime-detection
plan: 04
subsystem: session
tags: [mcp, cilium, version-detection, session, compat, go-sdk]

# Dependency graph
requires:
  - phase: 21-cilium-compatibility-matrix-runtime-detection (plan 21-01, merged into wave 2 base)
    provides: "pkg/k8s.CompatInfo, DetectCiliumVersion(ctx, client, logger), DetectCiliumVersionViaGetNodes(ctx, server, tls, timeout, logger)"
provides:
  - "StartResult/StatusResult carry cilium_version, cilium_versions_seen, below_floor_features (COMPAT-02)"
  - "Manager.detectVersionFn test-only seam (mirrors resolveSetupFn) defaulting to Manager.detectVersion"
  - "resolveSetup computes the compat verdict once (pod-list primary via kubeConfig, bounded GetNodes secondary on the pure --server bypass), cached on Session.compat, re-surfaced on every get_status"
affects: [22-audit-mode-onboarding (bootstrap CNP emission needs this version gate per PROJECT.md's phase-sequencing decision)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "detectVersionFn seam: test-only injectable field mirroring resolveSetupFn exactly (declared unexported on Manager, bound as a method value in NewManager AFTER construction, overridable in same-package tests)"
    - "compat cached in the same m.mu finalize critical section as TmpDir (write-once, mutex-protected, no atomic needed) — reused for a value with the identical 'set once at setup, read thereafter' lifecycle"

key-files:
  created: []
  modified:
    - pkg/session/session.go
    - pkg/session/manager.go
    - pkg/session/manager_test.go
    - cmd/cpg/mcp_session_test.go

key-decisions:
  - "compat inserted into resolveSetup's return tuple immediately before the trailing err (not after) — keeps Go's error-last convention intact while still letting every early error return thread a zero k8s.CompatInfo{} alongside its error"
  - "detectVersion passes args.Timeout (possibly zero/omitted) straight through to DetectCiliumVersionViaGetNodes rather than the Start-computed defaulted timeout — DetectCiliumVersionViaGetNodes's own versionDetectTimeout floor already handles zero correctly, and setupCtx's deadline double-bounds the call regardless"
  - "StopResult intentionally left untouched — no requirement or research finding calls for compat data on the stop summary"

requirements-completed: [COMPAT-02]

# Metrics
duration: ~18min
completed: 2026-07-22
---

# Phase 21 Plan 04: MCP Session Compat Surfacing Summary

**`start_session`/`get_status` now carry `cilium_version`/`cilium_versions_seen`/`below_floor_features` via a `detectVersionFn`-seamed, bounded call inside `resolveSetup`, cached once on the Session and re-surfaced on every poll.**

## Performance

- **Duration:** ~18 min
- **Completed:** 2026-07-22T12:54:17Z
- **Tasks:** 2/2
- **Files modified:** 4

## Accomplishments

- `StartResult` and `StatusResult` (pkg/session/session.go) both gained `CiliumVersion`, `CiliumVersionsSeen`, `BelowFloorFeatures` (`json:",omitempty"`, jsonschema-documented); `StopResult` deliberately untouched (no requirement calls for it).
- `Session` gained an unexported `compat k8s.CompatInfo` field, set once in the same `m.mu` finalize block that already sets `TmpDir`, and copied out in `Status`'s existing copy-under-lock block — no new synchronization primitive needed.
- `Manager` gained a `detectVersionFn` test seam (byte-for-byte mirroring `resolveSetupFn`'s existing shape: unexported field, bound as a method value in `NewManager` right after construction) defaulting to a new `detectVersion` method: pod-list primary (`kubernetes.NewForConfig` + `k8s.DetectCiliumVersion`) when a `kubeConfig` is in scope, bounded `k8s.DetectCiliumVersionViaGetNodes` secondary otherwise (the pure D-07 `--server` bypass).
- `resolveSetup`'s return arity grew a trailing `compat k8s.CompatInfo` (inserted before `err`, preserving Go's error-last convention); every existing error return now threads a zero `k8s.CompatInfo{}` alongside its error, and the real detection call sits at the very end of the success path.
- All 4 existing `resolveSetupFn` test overrides in `manager_test.go` updated for the new arity; `newTestManager` now stubs `detectVersionFn` to a fixed `1.19.4`/`test-stub` `CompatInfo` by default, so none of the ~20 `Start`-family unit tests newly dials the fake `bypass:1` address.
- `cmd/cpg/mcp_session_test.go`'s `TestMCPSessionLifecycleWiringAndStdoutPurity` decodes `cilium_version`/`below_floor_features` from both `start_session` and `get_status` `structuredContent` over the real in-memory MCP transport, proving wire-level presence; against the fake `127.0.0.1:1` address (production `Manager`, no kubeconfig) both decode to their empty zero values within the test's existing 2s timeout (test wall time 2.08s — the bounded secondary dial stayed inside its budget).
- `TestMCPAuditReadonlyReachability` (SEC-01) re-run unmodified and green — zero new K8s write-verb reachability, zero new fs-write allowlist entries.

## Task Commits

Each task was committed atomically:

1. **Task 1: Add compat fields + detectVersionFn seam + resolveSetup/Start/Status wiring** - `3ca6465` (feat)
2. **Task 2: Test the seam (no bypass dial), field surfacing, wire round-trip, and SEC-01 no-op** - `46b885a` (test)

_Note: Task 1's own `<verify>` block includes `go vet ./pkg/session/ ./cmd/cpg/`, which — because `go vet` type-checks a package's `_test.go` files together with its non-test sources — necessarily fails at the Task-1-only midpoint (the 4 `resolveSetupFn` test-override closures still have the pre-change arity). This is the plan's own designed split, not a defect: Task 1's actual acceptance criteria only requires `go build ./...` to compile (confirmed clean), and Task 2 (the very next, non-checkpointed task in this same execution) restores full `go vet`/`go test -race` green before the plan is considered done. Both commits landed in the same uninterrupted run, so the package was never left in that intermediate state across a checkpoint boundary._

## Files Created/Modified

- `pkg/session/session.go` - `CiliumVersion`/`CiliumVersionsSeen`/`BelowFloorFeatures` on `StartResult` + `StatusResult`; unexported `compat k8s.CompatInfo` field on `Session`
- `pkg/session/manager.go` - `detectVersionFn` seam + `detectVersion` production impl; `resolveSetup` computes and returns `compat`; `Start`/`Status` cache/read/surface it
- `pkg/session/manager_test.go` - `newTestManager` stubs `detectVersionFn`; 4 `resolveSetupFn` overrides updated for new arity; `TestManager_Start`/`TestManager_Status` assert the stubbed `1.19.4` version round-trips
- `cmd/cpg/mcp_session_test.go` - wire-level decode of `cilium_version`/`below_floor_features` on `start_session` and `get_status`, asserting the undetermined (empty) verdict against the fake bypass address

## Decisions Made

- `compat` placed immediately before `err` in `resolveSetup`'s return tuple (not after) — keeps the idiomatic Go "error last" position while still letting every early-return error path carry a zero `k8s.CompatInfo{}` in the same statement.
- `detectVersion` forwards `args.TLS`/`args.Timeout` (the raw, possibly-zero StartArgs values) rather than `Start`'s already-defaulted `timeout` local — intentional per plan: `DetectCiliumVersionViaGetNodes`'s own `versionDetectTimeout` floor already turns an omitted/zero timeout into a safe 3s ceiling, and `setupCtx`'s own deadline independently bounds the whole call either way.
- `StopResult` left untouched — compat data has no stop-summary requirement; adding it would have been scope creep per the plan's own explicit note.

## Deviations from Plan

None - plan executed exactly as written. Both tasks' `<action>` steps were followed literally; no Rule 1/2/3 auto-fixes were needed, no architectural questions arose, and no auth gates were hit (all detection paths in the test suite are either seam-stubbed or bounded against a syntactically-valid-but-unreachable address, never a real cluster).

## Issues Encountered

- `go vet`'s test-file inclusion made Task 1's own literal `<verify>` command fail at the Task-1-only midpoint (see Task Commits note above) — resolved by the very next task in the same run, per the plan's own two-commit design. Not a bug, not scope creep; documented here for commit-history clarity.
- The full `./pkg/session/... ./cmd/cpg/...` `-race` suite exceeds the 2-minute foreground command budget (`TestMCPAuditReadonlyReachability` alone runs ~96s as an SSA/RTA callgraph build); run via `run_in_background` and confirmed green (exit 0) before committing Task 2.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- COMPAT-02 (runtime detection surfaced through MCP session tools) is now fully shipped: the LLM harness gets `cilium_version`/`cilium_versions_seen`/`below_floor_features` on both `start_session` and every `get_status` poll.
- Phase 22 (AUD-02, bootstrap default-deny CNP generation) can now read `Session.compat`/`StatusResult.BelowFloorFeatures` if it needs a version gate for `enableDefaultDeny` (Cilium >= 1.16) — no new plumbing required, the verdict is already cached per-session.
- No blockers for this phase's remaining plans (if any) or for Phase 22.

---
*Phase: 21-cilium-compatibility-matrix-runtime-detection*
*Completed: 2026-07-22*

## Self-Check: PASSED

- FOUND: pkg/session/session.go
- FOUND: pkg/session/manager.go
- FOUND: pkg/session/manager_test.go
- FOUND: cmd/cpg/mcp_session_test.go
- FOUND: commit 3ca6465 (Task 1)
- FOUND: commit 46b885a (Task 2)
