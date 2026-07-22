---
phase: 21-cilium-compatibility-matrix-runtime-detection
plan: 01
subsystem: infra
tags: [kubernetes, cilium, hubble, grpc, client-go, apimachinery, semver, compatibility]

# Dependency graph
requires: []
provides:
  - "pkg/k8s.CompatInfo — the shared version-detection result shape (ClusterVersion, VersionsSeen, Source, BelowFloorFeatures)"
  - "pkg/k8s.DetectCiliumVersion — privilege-neutral pod-image-based primary detection (CLI + MCP)"
  - "pkg/k8s.DetectCiliumVersionViaGetNodes — bounded Hubble GetNodes MCP-only secondary detection"
  - "pkg/k8s.parseImageTag — registry host:port / digest-tolerant image tag parser"
  - "featureFloors table (1.14/1.15/1.16) driving BelowFloorFeatures naming"
affects: [21-02, 21-03, 21-04, 22-audit-onboarding-bootstrap]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Warn-and-proceed three-way branch (err==nil / IsForbidden / default) reused verbatim from pkg/k8s/preflight.go for the new pods/list probe"
    - "Per-feature floor table + apiversion.AtLeast comparison (belowFloorFeatures)"
    - "Local bounded gRPC dial + ready-wait reimplementation for a single unary RPC (waitForVersionConnReady), distinct from pkg/hubble.Client's streaming-shaped dial"

key-files:
  created:
    - pkg/k8s/version.go
    - pkg/k8s/version_test.go
  modified: []

key-decisions:
  - "parseImageTag requires a '/' to be present before treating the last ':' as a tag separator — rejects bare 'sha256:<hex>' digest IDs (the pod .status.containerStatuses[].image shape) that the plan's literal split-index description alone would have misparsed as a tag"
  - "GetNodes' per-node Version string is formatted '<component> v<version>' by cilium's own build.Version.String() (e.g. 'cilium v1.19.2+g3977f6a1') — parseAgentVersionString strips the component prefix before ParseGeneric, which otherwise fails on every real value"
  - "All CompatInfo return paths route through finalizeCompat uniformly (including forbidden/error branches) — behaviorally identical to a bespoke per-branch treatment since finalizeCompat no-ops on an empty ClusterVersion, but keeps a single below-floor choke point"

requirements-completed: [COMPAT-02]

# Metrics
duration: 19min
completed: 2026-07-22
---

# Phase 21 Plan 01: Cilium Version Detection Library Summary

**New `pkg/k8s/version.go`: privilege-neutral Cilium version detection via cilium-agent pod-image enumeration (primary) and a bounded Hubble `GetNodes()` probe (MCP-only secondary), reducing mixed-version clusters to a minimum and naming below-floor features — zero new RBAC, zero new go.mod dependencies.**

## Performance

- **Duration:** ~19 min
- **Started:** 2026-07-22T12:12:00Z
- **Completed:** 2026-07-22T12:30:00Z
- **Tasks:** 2/2 completed
- **Files modified:** 2 (both new)

## Accomplishments

- `CompatInfo` result type + `DetectCiliumVersion` (pod-list primary, warn-and-proceed, never errors) reusing the exact `pods/list`-in-`kube-system` RBAC tier `findRelayPod` already requires
- `DetectCiliumVersionViaGetNodes` (Hubble observer gRPC secondary, MCP-only), with a hard-capped 3s dial+ready-wait bound that a caller may only tighten, never loosen — verified to return `undetermined` well within 3s against an unreachable relay
- `parseImageTag` correctly handles registry `host:port` prefixes, `@sha256` digest suffixes, digest-only refs, and bare digest IDs (all four forms table-tested)
- `featureFloors` table (`1.14.0`/`1.15.0`/`1.16.0`) driving `BelowFloorFeatures` naming, verified against the mixed v1.19.2(53)/v1.19.3(30) live-cluster split documented in 21-RESEARCH.md
- Full `pkg/k8s` suite (existing preflight tests + new version tests) green under `-race`; zero new lint findings; zero `go.mod`/`go.sum` changes

## Task Commits

Each task was committed atomically:

1. **Task 1: Create pkg/k8s/version.go — CompatInfo + detection + floor table** - `cfa36ea` (feat)
2. **Task 2: Create pkg/k8s/version_test.go — fake-clientset + parsing table tests** - `d7691c5` (test)
3. **Task 2 follow-up: direct-coverage test for the parseAgentVersionString fix** - `3e7e490` (test)

**Plan metadata:** SUMMARY commit (this file)

## Files Created/Modified

- `pkg/k8s/version.go` (365 lines) - `CompatInfo`, `DetectCiliumVersion`, `DetectCiliumVersionViaGetNodes`, `parseImageTag`, `featureFloors`, and supporting unexported helpers (`tallyPodImageVersions`, `parseAgentVersionString`, `waitForVersionConnReady`, `finalizeCompat`, `belowFloorFeatures`)
- `pkg/k8s/version_test.go` (326 lines) - `ciliumAgentPod` fixture, `forbiddenListReactor`, `TestParseImageTag`, `TestDetectCiliumVersion` (5 subtests), `TestDetectCiliumVersion_Forbidden`, `TestDetectCiliumVersionViaGetNodes_UnreachableIsBoundedUndetermined`, `TestParseAgentVersionString` (4 subtests)

## Decisions Made

- Kept `DetectCiliumVersion`'s three return branches (success/forbidden/default) all routing through `finalizeCompat` rather than short-circuiting the error branches — simpler single choke point, behaviorally identical since `finalizeCompat` is a no-op on an empty `ClusterVersion`.
- Named the local bounded-ready-wait helper `waitForVersionConnReady` (distinct from `pkg/hubble/client.go`'s unexported `waitForConnReady`) for clarity when navigating a single package, even though Go would not have collided on the same name across packages.
- Used `atomic.Int64` for the test fixture's pod-name counter (not a plain package-level int) for defensive `-race` safety, even though no subtests currently run in parallel.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `parseImageTag`'s literal split-index description would have misparsed a bare digest ID as a tag**
- **Found during:** Task 1 (writing `parseImageTag`)
- **Issue:** The plan's `<action>` text (and 21-RESEARCH.md's own Code Examples section it was drawn from) describes the tag-separator check as "last colon exists AND occurs after the last slash." Taken literally, `"sha256:5051a679"` has no slash at all (`lastSlash == -1`), so `lastColon (6) > lastSlash (-1)` is trivially true — the described algorithm would have returned `("5051a679", true)`, directly contradicting the plan's own `<behavior>` contract: `parseImageTag("sha256:5051a679") == ("", false)  // bare digest ID (pod .status shape) rejected`.
- **Fix:** Added an explicit `lastSlash == -1` rejection branch — a colon is only ever treated as a tag separator when the reference also contains at least one path slash. Verified this doesn't regress any of the other four documented cases (registry `host:port`, digest suffix, digest-only, no-tag) — all still resolve correctly since none of them relies on a slash-free image having a tag.
- **Files modified:** `pkg/k8s/version.go` (`parseImageTag`)
- **Verification:** `TestParseImageTag` covers all 5 documented cases including `bare_digest_id_rejected`; `go test ./pkg/k8s/... -run TestParseImageTag -race` passes.
- **Committed in:** `cfa36ea` (Task 1 commit)

**2. [Rule 1 - Bug] `GetNodes()`'s per-node `Version` string is not a bare version — direct `ParseGeneric` would have failed on every real value**
- **Found during:** Task 1 (writing `DetectCiliumVersionViaGetNodes`)
- **Issue:** The plan's `<action>` text says to "Tally per-node `Node.Version` with the same ParseGeneric+min reduction," implying a direct `apiversion.ParseGeneric(node.Version)` call. Reading cpg's own vendored source (`github.com/cilium/cilium@v1.19.4/pkg/hubble/build/version.go`, `Version.String()`) shows the field is actually populated as `"<component> v<version>"` (e.g. `"cilium v1.19.2+g3977f6a1"`, matching 21-RESEARCH.md's own live-cluster-observed example verbatim) — a string `ParseGeneric` cannot parse at all (its regex requires the string to start with an optional `v` then digits, not a component name like `"cilium"`). Implemented literally, `DetectCiliumVersionViaGetNodes` would have silently returned `Source: "undetermined"` for every reachable relay with real nodes, permanently breaking the entire secondary detection source.
- **Fix:** Added `parseAgentVersionString`, which strips the `"<component> "` prefix (splits on the last space) before calling `ParseGeneric`, falling back to parsing the raw string unchanged if no space is present.
- **Files modified:** `pkg/k8s/version.go` (`DetectCiliumVersionViaGetNodes`, new `parseAgentVersionString` helper)
- **Verification:** Verified against vendored source directly (`pkg/hubble/build/version.go`'s `Version.String()` method body) rather than assumption; `go build`/`go vet` clean. Directly unit-tested via `TestParseAgentVersionString` (added as a Task 2 follow-up, `3e7e490`) covering both a build-metadata-suffixed value (`"cilium v1.19.2+g3977f6a1"`) and a plain one (`"cilium v1.19.3"`), plus unparseable/empty inputs. No fake-gRPC-server integration test exists for the full `DetectCiliumVersionViaGetNodes` successful-parse path (out of scope for this plan — Task 2's test map calls only for the bounded-unreachable case); the pure-function fix itself is now directly covered.
- **Committed in:** `cfa36ea` (Task 1 commit), test coverage in `3e7e490`

**3. [Rule 3 - Blocking] Added `crypto/tls` import, not listed in the plan's explicit import enumeration**
- **Found during:** Task 1 (writing the TLS-enabled dial branch of `DetectCiliumVersionViaGetNodes`)
- **Issue:** The plan's import list for Task 1 omits `crypto/tls`, but the specified behavior ("Build a grpc client... TLS or insecure like client.go") requires `credentials.NewTLS(&tls.Config{})`, matching `pkg/hubble/client.go`'s own TLS branch — which cannot compile without importing `crypto/tls`.
- **Fix:** Added the stdlib `crypto/tls` import.
- **Files modified:** `pkg/k8s/version.go`
- **Verification:** `rtk proxy go build ./pkg/k8s/` compiles clean.
- **Committed in:** `cfa36ea` (Task 1 commit)

---

**Total deviations:** 3 auto-fixed (2 Rule 1 bug fixes, 1 Rule 3 blocking-import fix)
**Impact on plan:** All three are correctness-preserving refinements of literal prose in the plan/research that would otherwise have silently broken specific documented behaviors (a bare-digest false-positive, and the entire GetNodes secondary source) or failed to compile. No scope creep, no architectural change, no new dependency.

## Issues Encountered

None beyond the deviations above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `pkg/k8s.DetectCiliumVersion` / `DetectCiliumVersionViaGetNodes` / `CompatInfo` are ready for 21-02 (README COMPAT-01/03 authoring, sourced from the same `featureFloors` table), 21-03 (CLI `maybeRunVersionPreflight` wiring in `cmd/cpg/generate.go`), and 21-04 (MCP `resolveSetup`/`StartResult`/`StatusResult` wiring in `pkg/session`).
- `parseAgentVersionString`'s component-prefix-stripping logic is now directly unit-tested (`TestParseAgentVersionString`); the only remaining untested surface is the full `DetectCiliumVersionViaGetNodes` successful-parse path end-to-end against a real/fake gRPC relay (would need a fake `ObserverServer`), which no plan task calls for — 21-04 (the only caller of this secondary path) may want an integration-level check if it stands up any fake relay harness for its own wiring tests, but this is a nice-to-have, not a blocker.
- No blockers for 21-02/21-03/21-04.

---
*Phase: 21-cilium-compatibility-matrix-runtime-detection*
*Completed: 2026-07-22*

## Self-Check: PASSED

- FOUND: `pkg/k8s/version.go`
- FOUND: `pkg/k8s/version_test.go`
- FOUND: `.planning/phases/21-cilium-compatibility-matrix-runtime-detection/21-01-SUMMARY.md`
- FOUND commit: `cfa36ea`
- FOUND commit: `d7691c5`
- FOUND commit: `3e7e490`
