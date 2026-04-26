---
phase: 03-production-hardening
plan: 02
subsystem: k8s
tags: [cilium, kubernetes, port-forward, dedup, cluster-dedup, spdy, clientset]

requires:
  - phase: 03-production-hardening
    plan: 01
    provides: "PoliciesEquivalent semantic comparison for cluster and cross-flush dedup"
  - phase: 02-hubble-streaming-pipeline
    provides: "RunPipeline, PipelineConfig, SessionStats, FlowSource interface"
provides:
  - "Auto port-forward to hubble-relay when --server is omitted"
  - "Cluster-based CiliumNetworkPolicy dedup via --cluster-dedup flag"
  - "Cross-flush dedup preventing redundant writes across flush cycles"
  - "pkg/k8s package: LoadKubeConfig, PortForwardToRelay, LoadClusterPolicies"
affects: []

tech-stack:
  added: [k8s.io/client-go, k8s.io/api, spdy, cilium-clientset]
  patterns: [auto-port-forward, cluster-snapshot-dedup, cross-flush-dedup, interface-based-k8s-mocking]

key-files:
  created:
    - pkg/k8s/client.go
    - pkg/k8s/client_test.go
    - pkg/k8s/portforward.go
    - pkg/k8s/portforward_test.go
    - pkg/k8s/cluster_dedup.go
    - pkg/k8s/cluster_dedup_test.go
  modified:
    - pkg/hubble/pipeline.go
    - pkg/hubble/pipeline_test.go
    - cmd/cpg/generate.go
    - go.mod

key-decisions:
  - "Cross-flush dedup uses PoliciesEquivalent in-memory comparison (not YAML bytes) since policies stay in-memory"
  - "Cluster dedup is opt-in via --cluster-dedup flag due to RBAC requirements"
  - "Cluster dedup uses startup snapshot (no periodic refresh) -- acceptable v1 limitation"
  - "findRelayPod selects only Running pods to avoid forwarding to pending/crashed pods"
  - "--server is now optional: auto port-forward when omitted, explicit address when provided"
  - "kubeConfig reused between port-forward and cluster-dedup when both active"

patterns-established:
  - "Auto port-forward pattern: findRelayPod -> SPDY dialer -> portforward.New -> readyCh"
  - "Cluster snapshot dedup: load once at startup, compare in writer goroutine"
  - "Cross-flush dedup: WrittenPolicies map keyed by namespace/workload in writer goroutine"
  - "Interface-based k8s testing: fake.NewSimpleClientset for pod resolution tests"

requirements-completed: [CONN-02, DEDP-02, DEDP-03]

duration: 6min
completed: 2026-03-08
---

# Phase 03 Plan 02: Auto Port-Forward, Cluster Dedup, and Cross-Flush Dedup Summary

**Auto port-forward to hubble-relay via SPDY, cluster-based CNP dedup via Cilium clientset, and cross-flush dedup via in-memory PoliciesEquivalent comparison**

## Performance

- **Duration:** 6 min
- **Started:** 2026-03-08T20:17:09Z
- **Completed:** 2026-03-08T20:24:01Z
- **Tasks:** 2
- **Files modified:** 10

## Accomplishments
- pkg/k8s package with LoadKubeConfig, PortForwardToRelay, and LoadClusterPolicies
- --server flag is now optional: auto port-forwards to hubble-relay in kube-system when omitted
- --cluster-dedup flag loads existing CNPs with managed-by=cpg label and skips equivalent policies
- Cross-flush dedup prevents redundant writes when the same policy is generated across flush cycles
- Session summary now reports PoliciesSkipped alongside PoliciesWritten
- 9 new tests in pkg/k8s, 4 new tests in pkg/hubble, all passing with race detector

## Task Commits

Each task was committed atomically (TDD: test then feat):

1. **Task 1: Kubernetes package (port-forward + kubeconfig + cluster dedup)**
   - `bcddb73` (test: failing tests for k8s package)
   - `51efea4` (feat: implement k8s package)
2. **Task 2: Pipeline cross-flush dedup + CLI wiring**
   - `0b9a898` (test: failing tests for cross-flush dedup, cluster dedup, skipped counter)
   - `2b6b542` (feat: cross-flush dedup, cluster dedup, and auto port-forward CLI wiring)

## Files Created/Modified
- `pkg/k8s/client.go` - LoadKubeConfig using clientcmd loading rules
- `pkg/k8s/client_test.go` - Tests with temp kubeconfig via KUBECONFIG env var
- `pkg/k8s/portforward.go` - PortForwardToRelay via SPDY dialer + findRelayPod
- `pkg/k8s/portforward_test.go` - Tests with fake.NewSimpleClientset for pod resolution
- `pkg/k8s/cluster_dedup.go` - LoadClusterPolicies + buildClusterPolicyMap with label selector
- `pkg/k8s/cluster_dedup_test.go` - Tests for policy map building and label selector constant
- `pkg/hubble/pipeline.go` - ClusterPolicies field, PoliciesSkipped counter, cross-flush dedup logic
- `pkg/hubble/pipeline_test.go` - Cross-flush dedup (same/changed), cluster dedup, skipped counter tests
- `cmd/cpg/generate.go` - Optional --server, --cluster-dedup flag, auto port-forward wiring
- `go.mod` - Promoted k8s.io/client-go, k8s.io/api to direct dependencies

## Decisions Made
- Cross-flush dedup uses in-memory PoliciesEquivalent (not YAML bytes) since policies remain in-memory between flush cycles, avoiding the any: prefix roundtrip issue
- Cluster dedup is opt-in (--cluster-dedup) because it requires RBAC permissions to list CiliumNetworkPolicies
- Cluster policies loaded once at startup as a snapshot -- no periodic refresh. Acceptable for v1 since cpg sessions are typically short-lived
- findRelayPod filters for PodRunning phase to avoid port-forwarding to pending or crashed pods
- kubeConfig is reused between port-forward and cluster-dedup initialization to avoid redundant kubeconfig loads

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- This is the final plan in the project (Phase 3, Plan 2 of 2)
- All core features complete: policy generation, CIDR rules, file dedup, cluster dedup, cross-flush dedup, auto port-forward
- Full test suite: all tests pass with race detector across all packages

---
*Phase: 03-production-hardening*
*Completed: 2026-03-08*
