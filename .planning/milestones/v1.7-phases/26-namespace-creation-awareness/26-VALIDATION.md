---
phase: "26"
slug: "namespace-creation-awareness"
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: "2026-10-05"
---

# Phase 26 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from 26-RESEARCH.md `## Validation Architecture` (plan-phase §5.5); refine during validate-phase.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` + `stretchr/testify` v1.11.1 (assert/require), race detector |
| **Config file** | none — conventional Go layout; commands via Makefile |
| **Quick run command** | `go test ./pkg/hubble/ ./pkg/k8s/ -run 'NewNamespace|NamespaceWatch|FirstSeen' -count=1 -race` |
| **Full suite command** | `go test ./... -count=1 -race` (= `make test`) |
| **Estimated runtime** | ~seconds (quick) / ~90 seconds (full suite, 610+ existing tests) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/hubble/ ./pkg/k8s/ -run 'NewNamespace|NamespaceWatch|FirstSeen' -count=1 -race`
- **After every plan wave:** Run `go test ./... -count=1 -race`
- **Before `/gsd:verify-work`:** Full suite must be green (phase gate: full suite + byte-identity test green)
- **Max feedback latency:** 90 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD (planner assigns) | TBD | 0 | NSAW-01 | — | Watch failure (RBAC, connection, channel close) warns once and proceeds — readonly posture intact | unit (fake clientset + `PrependWatchReactor` + `watch.NewFake()`) | `go test ./pkg/k8s/ -run TestNamespaceWatch -count=1 -race` | ❌ W0 (`pkg/k8s/ns_watch_test.go`) | ⬜ pending |
| TBD (planner assigns) | TBD | 0 | NSAW-01 | — | List/Watch error / RBAC 403 ⇒ one warning, empty collector, no error propagated | unit | `go test ./pkg/k8s/ -run TestNamespaceWatchFailSafe -count=1 -race` | ❌ W0 | ⬜ pending |
| TBD (planner assigns) | TBD | 0 | NSAW-01 | — | End-of-run summary lists new namespaces with re-run hint, threaded through `RunPipelineWithSource` | integration (in-process pipeline, `flowsource.FlowSource` fake — precedent `pkg/hubble/pipeline_test.go`) | `go test ./pkg/hubble/ -run TestPipelineNewNamespaces -count=1 -race` | ❌ W0 | ⬜ pending |
| TBD (planner assigns) | TBD | 0 | NSAW-01 | — | Zero new namespaces ⇒ summary output byte-identical to output without the feature | unit (dual-buffer comparison; precedent `pipeline_l7_golden_test.go`) | `go test ./pkg/hubble/ -run TestSummaryByteIdentity -count=1` | ❌ W0 | ⬜ pending |
| TBD (planner assigns) | TBD | 0 | NSAW-02 | — | First flow after capture start per namespace from staggered-timestamp fixtures | unit (new fixture `testdata/flows/ns_staggered.jsonl`, mirroring `small.jsonl` shape) | `go test ./pkg/hubble/ -run TestFirstSeenNamespaces -count=1 -race` | ❌ W0 | ⬜ pending |
| TBD (planner assigns) | TBD | 0 | NSAW-02 | — | Flows with nil/zero `Flow.Time` skipped; all-zero capture ⇒ empty report | unit | `go test ./pkg/hubble/ -run TestFirstSeenZeroTime -count=1 -race` | ❌ W0 | ⬜ pending |
| TBD (planner assigns) | TBD | 0 | SC-4 | — | MCP stdout purity unaffected by the new summary block | integration (extend `TestMCPSessionLifecycleWiringAndStdoutPurity`, `cmd/cpg/mcp_session_test.go`) | `go test ./cmd/cpg/ -run TestMCPSessionLifecycleWiringAndStdoutPurity -count=1 -race` | ✅ exists — extend | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/k8s/ns_watch_test.go` — watch/fail-safe coverage for NSAW-01
- [ ] `pkg/hubble/ns_awareness_test.go` — pure-helper + print-block tests incl. byte-identity
- [ ] `testdata/flows/ns_staggered.jsonl` — replay fixture with staggered namespace first-seen timestamps
- [ ] Go toolchain present in executor environment (research caveat A7 — Go ≥ 1.25.1 needed for `make test`)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| None — all phase behaviors have automated verification. | — | — | — |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
