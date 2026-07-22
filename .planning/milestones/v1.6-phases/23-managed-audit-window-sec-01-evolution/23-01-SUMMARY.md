---
phase: 23-managed-audit-window-sec-01-evolution
plan: 01
subsystem: infra
tags: [kubernetes, client-go, spdy, cilium, exec, pods-exec]

# Dependency graph
requires:
  - phase: 21-cilium-compatibility-matrix-runtime-detection
    provides: "pkg/k8s/version.go's CompatInfo, ciliumAgentLabelSelector, ciliumAgentContainerName, DetectCiliumVersion feature-floor table"
provides:
  - "pkg/k8s.ExecCiliumDbg — SPDY pods/exec into a cilium-agent pod, exec.CodeExitError vs transport-failure distinction"
  - "pkg/k8s.FindAgentPodForNode — node IP -> running cilium-agent pod mapping (no nodes/get RBAC)"
  - "pkg/k8s.ReadPolicyAuditMode / SetPolicyAuditMode — per-endpoint audit-mode read-before-flip primitives"
  - "pkg/k8s.CheckDaemonAuditMode — daemon-wide policy-audit-mode precondition read"
  - "pkg/k8s.CiliumBinaryName — cilium-dbg vs cilium binary name gate on detected version"
affects: [23-02, 23-03, 23-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "execCiliumDbgFn package-level seam (mirrors DetectCiliumVersion's own seam convention) — higher-level callers substitute a stub in tests, no fake SPDY server needed"
    - "SPDY exec request construction is a one-line SubResource variant of pkg/k8s/portforward.go's proven dialer shape"

key-files:
  created:
    - pkg/k8s/exec.go
    - pkg/k8s/exec_test.go
  modified: []

key-decisions:
  - "Canonical lowercase enable/disable argv literals hard-coded as package consts rather than relying on cilium-dbg's NormalizeBool tolerance (RESEARCH Pitfall 5)"
  - "ReadPolicyAuditMode standardizes on `endpoint get <id> -o json` (JSON array) exclusively, never the incompatible `endpoint config` object shape (RESEARCH Pitfall 2)"
  - "CheckDaemonAuditMode treats RBAC-forbidden as undetermined (false, nil) — warn-and-proceed, matching version.go's existing precondition convention; any other error (including NotFound) is returned so callers can hard-refuse on a genuine read failure"
  - "CiliumBinaryName defaults to cilium-dbg for both undetermined and unparseable versions, since >=1.15 is the overwhelmingly common floor"

patterns-established:
  - "Package-level function-var seam for exec calls (execCiliumDbgFn), so Wave-2 pkg/auditwindow.Manager and its tests never need a fake SPDY server"

requirements-completed: [AUD-03]

# Metrics
duration: 25min
completed: 2026-07-22
---

# Phase 23 Plan 01: SPDY Exec Plumbing + Audit-Mode K8s Primitives Summary

**pkg/k8s/exec.go: SPDY `pods/exec` into cilium-agent pods with exit-code-vs-transport-error distinction, node-to-agent-pod mapping via HostIP, read-before-flip PolicyAuditMode primitives, and a daemon-wide ConfigMap precondition read — zero new go.mod dependencies.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-22T16:00:00Z (approx, prior to first commit)
- **Completed:** 2026-07-22T16:19:52Z
- **Tasks:** 2 completed
- **Files modified:** 2 (both created)

## Accomplishments
- `ExecCiliumDbg` builds the `pods/exec` SPDY request (Post().Resource("pods").SubResource("exec") + `remotecommand.NewSPDYExecutor` + `StreamWithContext`), distinguishing `exec.CodeExitError` (remote non-zero exit) from transport failure via `errors.As`.
- `FindAgentPodForNode` maps a node's IP to its running cilium-agent pod using `Status.HostIP` — reuses the existing `pods/list` verb, requiring no `nodes/get` RBAC.
- `ReadPolicyAuditMode` / `SetPolicyAuditMode` implement the never-touch-already-audited read-before-flip contract: read parses the JSON *array* `cilium-dbg endpoint get -o json` returns (erroring on an empty array, never silently defaulting to false); flip emits the canonical lowercase `enable`/`disable` literal.
- `CheckDaemonAuditMode` reads the `cilium-config` ConfigMap's `policy-audit-mode` key, warn-and-proceeding (false, nil) on RBAC-forbidden, erroring on any other failure.
- `CiliumBinaryName` gates the `cilium-dbg` vs `cilium` binary name on the existing Phase 21 version-floor machinery, defaulting to `cilium-dbg` when undetermined.

## Task Commits

1. **Task 1: SPDY exec primitive + node->agent-pod mapping** - `c36e03e` (feat)
2. **Task 2: read-before-flip, canonical flip, binary-name gate, daemon precondition** - `f0ab85f` (feat)

**Plan metadata:** (this commit, pending)

## Files Created/Modified
- `pkg/k8s/exec.go` - `ExecCiliumDbg`, `execCiliumDbgFn` seam, `FindAgentPodForNode`, `CiliumBinaryName`, `ReadPolicyAuditMode`, `SetPolicyAuditMode`, `CheckDaemonAuditMode`
- `pkg/k8s/exec_test.go` - `TestFindAgentPodForNode_MatchesHostIP`, `TestFindAgentPodForNode_NoMatchErrors`, `TestFindAgentPodForNode_NoPodsAtAll`, `TestReadPolicyAuditMode_ParsesEnabledFromArray`, `TestSetPolicyAuditMode_UsesCanonicalLowercase`, `TestCheckDaemonAuditMode_ActiveWhenConfigTrue`, `TestCiliumBinaryName_Gate`

## Decisions Made
- Combined the plan's per-task TDD behavior/action split into one atomic commit per task (per orchestrator instruction: "one atomic commit per task"), rather than separate RED/GREEN commits — both tasks' tests and implementation landed together, verified green before each commit.
- Added `TestFindAgentPodForNode_NoPodsAtAll` and `TestCiliumBinaryName_Gate` beyond the plan's named tests: base-case/empty-clientset coverage and coverage for the newly-exported `CiliumBinaryName` gate, both low-risk additions matching Rule 2 (missing critical test coverage for a new exported function).
- Reused existing package test helpers (`ciliumAgentPod`, `configMap`, `forbiddenReactor`, `newObservedLogger`) from `version_test.go`/`preflight_test.go` rather than duplicating them.

## Deviations from Plan

None (beyond the two additive test-coverage items noted above, which are within Rule 2 scope) - plan executed as written. No redeclared constants (`ciliumAgentContainerName`, `ciliumAgentLabelSelector`, `ciliumNamespace`, `ciliumConfigMapName` all reused from `version.go`/`preflight.go` per the plan's explicit instruction).

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `pkg/k8s/exec.go` primitives are ready for Wave 2's `pkg/auditwindow.Manager` to consume directly (per 23-RESEARCH.md's architecture diagram): `ExecCiliumDbg`/`execCiliumDbgFn` seam, `FindAgentPodForNode`, `ReadPolicyAuditMode`, `SetPolicyAuditMode`, `CheckDaemonAuditMode`, `CiliumBinaryName`.
- No blockers. `go build ./...` green, `go test ./pkg/k8s/... -count=1 -race` green, `git diff --exit-code go.mod go.sum` clean (zero new dependencies).

---
*Phase: 23-managed-audit-window-sec-01-evolution*
*Completed: 2026-07-22*

## Self-Check: PASSED
- FOUND: pkg/k8s/exec.go
- FOUND: pkg/k8s/exec_test.go
- FOUND: c36e03e (Task 1 commit)
- FOUND: f0ab85f (Task 2 commit)
