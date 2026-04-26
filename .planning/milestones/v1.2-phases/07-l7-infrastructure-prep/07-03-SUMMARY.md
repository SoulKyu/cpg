---
phase: 07-l7-infrastructure-prep
plan: 03
subsystem: infra
tags: [k8s, cilium, preflight, rbac, l7, client-go, fake-client]

requires:
  - phase: 03-production-hardening
    provides: pkg/k8s clientset bootstrapping, kubeconfig loader, fake-client test pattern
provides:
  - "pkg/k8s.RunL7Preflight: callable VIS-04 + VIS-05 cluster prerequisite check with warn-and-proceed semantics"
  - "Literal warning copy operators can grep during incident response"
  - "Cilium 1.14-1.15 embedded-envoy fallback via enable-envoy-config flag"
  - "RBAC-403 graceful degradation (warn, return nil — never abort)"
affects: [07-04-PLAN.md, cmd/generate.go (Plan 07-04 wiring)]

tech-stack:
  added: []
  patterns:
    - "Pre-flight checks in pkg/k8s accept kubernetes.Interface (not the concrete clientset) so tests inject fake.NewSimpleClientset"
    - "Advisory cluster checks: emit zap.Warn with explicit remediation, never return error"
    - "RBAC denial detection via apierrors.IsForbidden — name the missing permission in the warning so operators can patch the Role/ClusterRole"

key-files:
  created:
    - pkg/k8s/preflight.go
    - pkg/k8s/preflight_test.go
  modified: []

key-decisions:
  - "Single-warning-per-check via per-call locality (no global sync.Once). Caller contract documented in godoc: invoke once per cpg run. Avoids global state, matches Plan 07-04's expected single-startup-call pattern."
  - "Unexpected (non-403, non-404) errors on either Get treated like NotFound — warn with the same remediation message but include err via zap.Error. Operators get actionable text plus debug detail."
  - "RunL7Preflight returns void (no error). Pre-flight is purely advisory; making it errorable would tempt callers to abort, violating the warn-and-proceed invariant."
  - "VIS-05 fallback re-uses cilium-config Data already fetched by VIS-04 (passed as ciliumConfigData struct). Avoids a second Get and stays correct when ConfigMap was forbidden/missing (struct flags signal that → no fallback consulted, warning fires)."

patterns-established:
  - "ciliumConfigData struct carries ConfigMap fetch outcome (Data | NotFound | Forbidden) between checks so VIS-05 fallback only triggers when VIS-04 actually saw the ConfigMap."
  - "Warning copy stored as package-level constants (warnL7ProxyDisabled, warnConfigMapNotFound, warnConfigMapForbidden, warnEnvoyMissing, warnEnvoyForbidden) so future tests assert on identifiers, not duplicated literals."

requirements-completed: [VIS-04, VIS-05]

duration: ~10min
completed: 2026-04-25
---

# Phase 07 Plan 03: L7 Preflight Cluster Checks Summary

**`pkg/k8s.RunL7Preflight` — VIS-04 (cilium-config `enable-l7-proxy`) and VIS-05 (cilium-envoy DaemonSet + 1.14-1.15 embedded-envoy fallback) with warn-and-proceed RBAC semantics, exercised across a 10-case fake-client test matrix.**

## Performance

- **Duration:** ~10 min
- **Started:** 2026-04-25T07:17:00Z (approx)
- **Completed:** 2026-04-25T07:27:08Z
- **Tasks:** 1 (TDD: RED commit + GREEN commit)
- **Files modified:** 2 created (`pkg/k8s/preflight.go`, `pkg/k8s/preflight_test.go`)

## Accomplishments

- `RunL7Preflight(ctx, kubernetes.Interface, *zap.Logger)` exposed in `pkg/k8s` — single entry point for Plan 07-04 to invoke from `cmd/generate.go`.
- VIS-04 implemented: reads `kube-system/cilium-config`, warns if `enable-l7-proxy != "true"` or ConfigMap is missing, with remediation hint naming the ConfigMap and instructing to roll the cilium-agent DaemonSet.
- VIS-05 implemented: looks for `kube-system/cilium-envoy` DaemonSet, falls back to `enable-envoy-config="true"` in cilium-config for Cilium 1.14-1.15 (embedded envoy in cilium-agent — silent pass).
- RBAC 403 handled per the warn-and-proceed invariant: each forbidden Get warns with the literal required permission (`configmaps/get`, `daemonsets/get` in `kube-system`) and returns; the function never aborts, regardless of how many checks fail.
- Single-warning-per-check invariant: each of the two checks emits at most one warning per invocation (max 2 warnings total, one per check). Documented in godoc with a "call once per cpg run" contract — caller (Plan 07-04) honors it by invoking exactly once at startup.
- VIS-06 wiring deliberately deferred to Plan 07-04 — godoc tells the caller to gate on `--no-l7-preflight` by simply skipping the call.

## Task Commits

1. **Task 1 RED — failing tests for L7 preflight** — `306b36e` (test)
2. **Task 1 GREEN — RunL7Preflight implementation** — `bdde8b8` (feat)

_Note: TDD pair lands as two commits per CPG convention (test files in failing commits before implementation, mirroring v1.1 phases)._

## Files Created/Modified

- `pkg/k8s/preflight.go` — `RunL7Preflight` + private helpers `getCiliumConfig` and `checkCiliumEnvoy`; package-level constants for warning copy.
- `pkg/k8s/preflight_test.go` — table-driven `TestRunL7Preflight` (10 matrix subtests) plus `TestRunL7Preflight_SingleWarningPerInvocation`. Uses `k8s.io/client-go/kubernetes/fake` and `client.PrependReactor` to simulate 403s.

## Function Signature & Contract

```go
// VIS-04 + VIS-05. Advisory: warns with remediation, never returns error.
// RBAC 403 → warn-and-proceed. Each warning fires at most once per call.
// Caller contract: invoke ONCE per cpg run. VIS-06 (--no-l7-preflight)
// satisfied by skipping the call entirely (Plan 07-04 owns the flag).
func RunL7Preflight(ctx context.Context, client kubernetes.Interface, logger *zap.Logger)
```

## Exact Warning Copy (operator grep reference)

| Constant | Trigger | Message |
|----------|---------|---------|
| `warnL7ProxyDisabled` | VIS-04 fails | `L7 preflight: enable-l7-proxy is not set to 'true' in ConfigMap kube-system/cilium-config. L7 policy generation requires the Cilium L7 proxy. Remediation: set 'enable-l7-proxy: "true"' in the ConfigMap and roll the cilium-agent DaemonSet.` |
| `warnConfigMapNotFound` | cilium-config ConfigMap NotFound (or other unexpected error) | `L7 preflight: ConfigMap kube-system/cilium-config not found. L7 policy generation requires Cilium with L7 proxy enabled. Verify Cilium is installed in this cluster.` |
| `warnConfigMapForbidden` | RBAC 403 on get configmaps | `L7 preflight: RBAC denied for get configmaps in kube-system (cilium-config). Skipping enable-l7-proxy check; proceeding. Required permission: configmaps/get in kube-system.` |
| `warnEnvoyMissing` | DaemonSet `cilium-envoy` NotFound AND no `enable-envoy-config="true"` | `L7 preflight: DaemonSet kube-system/cilium-envoy not found and enable-envoy-config is not 'true' in cilium-config. On Cilium >= 1.16 the envoy DaemonSet is required for L7 visibility; on Cilium 1.14-1.15 set enable-envoy-config: "true" in cilium-config.` |
| `warnEnvoyForbidden` | RBAC 403 on get daemonsets | `L7 preflight: RBAC denied for get daemonsets in kube-system (cilium-envoy). Skipping cilium-envoy check; proceeding. Required permission: daemonsets/get in kube-system.` |

## Test Matrix Coverage

| # | Scenario | Expected |
|---|----------|----------|
| 1 | `enable-l7-proxy="true"` + cilium-envoy DS present | 0 warnings (silent pass) |
| 2 | `enable-l7-proxy="false"` | 1 warning: `warnL7ProxyDisabled` |
| 3 | `enable-l7-proxy` key missing | 1 warning: `warnL7ProxyDisabled` |
| 4 | cilium-config ConfigMap NotFound | 1 warning: `warnConfigMapNotFound` |
| 5 | cilium-envoy DS absent + `enable-envoy-config="true"` | 0 warnings (Cilium 1.14-1.15 fallback) |
| 6 | cilium-envoy DS absent + no `enable-envoy-config` | 1 warning: `warnEnvoyMissing` |
| 7 | 403 on get configmaps | 1 warning: `warnConfigMapForbidden` |
| 8 | 403 on get daemonsets | 1 warning: `warnEnvoyForbidden` |
| 9 | 403 on both gets | 2 warnings (one per check) |
| 10 | Single-warning-per-invocation: empty cluster (both checks fail) | 2 warnings total (one per check, never duplicated within a check) |

All 11 subtests pass under `go test ./pkg/k8s/... -count=1 -v`.

## Decisions Made

- **No global `sync.Once`**: per-call helpers warn at most once each by virtue of being one-shot per Get; the "single warning per RUN" invariant is established by the caller-side contract ("invoke once per cpg run") rather than package-level state. Cleaner, no init-order surprises, easier to test.
- **`ciliumConfigData` struct carries fetch outcome between checks**: so VIS-05's fallback to `enable-envoy-config` is only consulted when the ConfigMap was actually fetched; on Forbidden/NotFound the fallback is correctly skipped and the envoy warning fires unaltered.
- **Unexpected errors → warn-like-NotFound**: any non-403, non-404 error (network, decode, etc.) emits the same remediation warning as NotFound but with `zap.Error(err)` for debugging. Operator sees actionable text plus the underlying cause.

## Deviations from Plan

None - plan executed exactly as written.

The plan's "implementation hint" allowed a choice between `sync.Once` and a documented "call once per run" contract — the latter was selected as cleanest, matching the plan's stated preference ("Prefer this path — it avoids global state and matches CONTEXT's single-startup-call expectation").

## Issues Encountered

- Pre-existing failures in `pkg/policy/merge_l7_test.go` (TestMergePortRules_PreservesRules) showed up under `go test ./...`. These are the deliberate TDD-RED tests for **Plan 07-01** (EVID2-03 merge bug) — Wave 1 parallel execution: 07-01 implementation lands separately. Out of scope for 07-03; not introduced by this plan. `go test ./pkg/k8s/... -count=1` is fully green.

## Self-Check: PASSED

Verified post-write:

- `pkg/k8s/preflight.go` exists at expected path.
- `pkg/k8s/preflight_test.go` exists at expected path.
- Commit `306b36e` (test, RED) present in `git log`.
- Commit `bdde8b8` (feat, GREEN) present in `git log`.
- `go vet ./pkg/k8s/...` clean; `go build ./...` clean; `go test ./pkg/k8s/... -count=1` green (19 tests including the 11 RunL7Preflight subtests).
- `grep -n "RunL7Preflight\|enable-l7-proxy\|cilium-envoy\|enable-envoy-config\|IsForbidden" pkg/k8s/preflight.go` returns 8+ matches as required by plan verification block.

## Next Phase Readiness

- `RunL7Preflight` ready for Plan 07-04 to invoke from `cmd/generate.go`. Plan 07-04 also adds the `--no-l7-preflight` flag (VIS-06) and the `--l7` flag (L7CLI-01) and gates the call on both.
- No blockers. No scope creep.

---
*Phase: 07-l7-infrastructure-prep*
*Plan: 03*
*Completed: 2026-04-25*
