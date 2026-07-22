---
phase: 21-cilium-compatibility-matrix-runtime-detection
plan: 03
subsystem: cli
tags: [cilium, kubernetes, version-detection, cobra, zap, client-go]

# Dependency graph
requires:
  - phase: 21-01
    provides: "pkg/k8s/version.go — CompatInfo, DetectCiliumVersion(ctx, client, logger), DetectCiliumVersionViaGetNodes(ctx, server, tlsEnabled, timeout, logger)"
provides:
  - "cmd/cpg/generate.go: maybeRunVersionPreflight — always-on, advisory Cilium version preflight mirroring maybeRunL7Preflight's shape"
  - "Call site wired immediately before hubble.RunPipeline, alongside the existing maybeRunL7Preflight call"
  - "TestMaybeRunVersionPreflight_BelowFloorWarns: below-floor (v1.14.2) warns naming cilium-dbg + enableDefaultDeny; at-floor (v1.16.0) emits zero warnings"
  - "TestReplay_NoVersionDetection: structural source-scan regression proving cpg replay never invokes version detection"
affects: [21-04, 22-audit-onboarding]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Always-on preflight variant of the existing gated-preflight pattern (maybeRunL7Preflight): same nil-kubeConfig fallback + l7ClientFactory reuse, but no opt-in flag gate"
    - "Structural source-scan regression test (os.ReadFile + assert.NotContains on symbol names) for proving an offline command stays free of a forbidden call, used where no runtime side effect exists to assert on"

key-files:
  created: []
  modified:
    - cmd/cpg/generate.go
    - cmd/cpg/generate_test.go
    - cmd/cpg/replay_test.go

key-decisions:
  - "maybeRunVersionPreflight has no l7Enabled/noPreflight-style gate — version detection is always-on per COMPAT-02, unlike opt-in L7 pre-flight"
  - "CompatInfo returned by k8s.DetectCiliumVersion is discarded at the CLI call site (no further CLI consumer this plan; detection warns internally) — matches the plan's explicit instruction, superseding 21-RESEARCH.md's stale draft signature that also threaded an observer connection"
  - "TestReplay_NoVersionDetection asserts on symbol-name absence via a source-file scan rather than a runtime behavioral test, since replay.go has no kubeconfig/K8s-client code path at all to exercise"

patterns-established:
  - "Version preflight is invoked from cpg generate only; cpg replay's offline guarantee is preserved by the same doc-comment contract already used for L7 pre-flight (maybeRunL7Preflight), now duplicated onto maybeRunVersionPreflight and structurally enforced by a dedicated regression test"

requirements-completed: [COMPAT-02]

# Metrics
duration: ~15min
completed: 2026-07-22
---

# Phase 21 Plan 03: CLI Version Preflight Wiring Summary

**`cpg generate` now runs an always-on, advisory Cilium version preflight (`maybeRunVersionPreflight`) before the pipeline starts, mirroring `maybeRunL7Preflight`'s shape exactly, while `cpg replay` is structurally proven to stay version-detection-free.**

## Performance

- **Duration:** ~15 min
- **Completed:** 2026-07-22
- **Tasks:** 2/2 completed
- **Files modified:** 3

## Accomplishments

- `cmd/cpg/generate.go` gained `maybeRunVersionPreflight(ctx, kubeConfig, logger)`: nil-kubeConfig falls back to `k8s.LoadKubeConfig()`, client construction reuses the existing test-substitutable `l7ClientFactory` package var, and every failure path is `logger.Warn(...)` + `return` — never an error, never a block.
- Call site wired immediately after the existing `maybeRunL7Preflight(...)` call and before `hubble.RunPipeline(...)`, reusing the same already-resolved `kubeConfig` local.
- `TestMaybeRunVersionPreflight_BelowFloorWarns` proves the below-floor case (fake cilium-agent pod at `v1.14.2`) emits a warning naming both `cilium-dbg` and `enableDefaultDeny`, and the at-floor case (`v1.16.0`) emits zero below-floor warnings.
- `TestReplay_NoVersionDetection` source-scans `replay.go` for `maybeRunVersionPreflight`/`DetectCiliumVersion` and asserts their absence — manually verified (temporary, reverted edit) that the guard actually fails when `DetectCiliumVersion` is present, then confirmed clean pass again after reverting.
- Full `cmd/cpg` suite green under `-race` (153.9s), including all pre-existing tests plus the two new ones.

## Task Commits

Each task was committed atomically:

1. **Task 1: Add maybeRunVersionPreflight + call site in generate.go and its warn test** - `6362e89` (feat)
2. **Task 2: Regression-guard replay.go against version detection** - `9d090dd` (test)

_No plan-metadata commit: STATE.md/ROADMAP.md/REQUIREMENTS.md are owned by the orchestrator, not this parallel worktree agent (per execution instructions)._

## Files Created/Modified

- `cmd/cpg/generate.go` - Added `maybeRunVersionPreflight` (always-on advisory version preflight) + its call site before `hubble.RunPipeline`
- `cmd/cpg/generate_test.go` - Added `TestMaybeRunVersionPreflight_BelowFloorWarns` (below-floor warn assertion + at-floor zero-warning assertion) and a local `ciliumAgentPodForTest` fixture builder
- `cmd/cpg/replay_test.go` - Added `TestReplay_NoVersionDetection` (structural source-scan regression guard)

## Decisions Made

- Followed the plan's explicit instruction to call `k8s.DetectCiliumVersion(ctx, client, logger)` and discard the returned `CompatInfo` (three-argument signature, matching the already-merged Wave 1 `pkg/k8s/version.go`), rather than 21-RESEARCH.md's earlier four-argument draft signature that also threaded an observer connection — the plan itself is the authoritative source here since it reflects the actual implemented library.
- Kept the below-floor warning entirely inside `k8s.DetectCiliumVersion` (already the case from Wave 1); `maybeRunVersionPreflight` does not re-warn, matching the plan's acceptance criteria.

## Deviations from Plan

None — plan executed exactly as written. Both tasks' acceptance criteria were met without requiring Rule 1-4 auto-fixes:

- Task 1: function + call site present, doc comment carries the "cpg replay ... must never call this function" contract, `maybeRunVersionPreflight` never returns and the call site doesn't gate on a result — all verified.
- Task 2: guard passes against current `replay.go`, and was manually confirmed to fail when `DetectCiliumVersion` is temporarily present (reverted before commit, never landed in a commit).

## Known Stubs

None — this plan adds CLI wiring and tests only; no UI/data-rendering surface, no placeholder values.

## Threat Flags

None — this plan's `<threat_model>` (T-21-03-01..03, T-21-03-SC) already covers the only surface touched (an always-on advisory cluster read reusing the existing `pods/list`-in-`kube-system` RBAC tier); no new endpoint, auth path, or schema change was introduced.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- COMPAT-02's CLI surface is complete: `cpg generate` warns-and-proceeds on below-floor clusters; `cpg replay`'s offline guarantee is both documented and structurally regression-tested.
- Plan 21-04 (MCP surfacing of `CompatInfo` via `StartResult`/`StatusResult`) can proceed independently — it does not depend on this plan's CLI-only call site.
- No blockers.

---
*Phase: 21-cilium-compatibility-matrix-runtime-detection*
*Completed: 2026-07-22*

## Self-Check: PASSED

- FOUND: cmd/cpg/generate.go (maybeRunVersionPreflight function at line 68, call site at line 256)
- FOUND: cmd/cpg/generate_test.go (TestMaybeRunVersionPreflight_BelowFloorWarns at line 271)
- FOUND: cmd/cpg/replay_test.go (TestReplay_NoVersionDetection at line 599)
- FOUND: commit 6362e89 (Task 1)
- FOUND: commit 9d090dd (Task 2)
- Full `cmd/cpg` suite green under `-race` (153.9s), confirmed via `rtk proxy go test ./cmd/cpg/... -count=1 -race`.
