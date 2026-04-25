---
gsd_state_version: 1.0
milestone: v1.2
milestone_name: l7-policies
status: defining_requirements
stopped_at: v1.2 milestone started 2026-04-25 -- research phase
last_updated: "2026-04-25T08:00:00.000Z"
last_activity: 2026-04-25 -- Started v1.2 L7 Policies milestone
progress:
  total_phases: 0
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-03-09)

**Core value:** Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.
**Current focus:** Phase 04 — offline-replay-core

## Current Position

Phase: Not started (defining requirements)
Plan: —
Status: Defining requirements (v1.2 L7 Policies — research underway)
Last activity: 2026-04-25 -- Started v1.2 L7 Policies milestone

Progress: v1.0 ✅ · v1.1 ✅ · v1.2 🚧 (research → requirements → roadmap)

## Performance Metrics

**Velocity:**

- Total plans completed: 7
- Average duration: 4.4 min
- Total execution time: 0.52 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01-core-policy-engine | 3 | 13 min | 4.3 min |
| 02-hubble-streaming-pipeline | 2 | 7 min | 3.5 min |
| 03-production-hardening | 2 | 12 min | 6.0 min |

**Recent Trend:**

- Last 5 plans: 01-03 (4 min), 02-01 (3 min), 02-02 (4 min), 03-01 (6 min), 03-02 (6 min)
- Trend: stable

*Updated after each plan completion*

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- Used NewESFromMatchRequirements with plain keys (not NewESFromLabels) to avoid k8s: prefix in YAML output
- Namespace label in peer selectors uses plain io.kubernetes.pod.namespace key
- WorkloadName fallback: sorted label values joined with "-", truncated to 63 chars
- EndpointSelector peer comparison via LabelSelector.MatchLabels (no GetMatchLabels method)
- Single PortRule per rule with all ports for cleaner YAML output
- Insertion-order peer tracking for deterministic rule ordering
- FlowSource interface enables pipeline testing without gRPC (RunPipelineWithSource)
- AggKey uses (namespace, workload) only -- direction handled inside BuildPolicy
- Flows with empty namespace skipped at aggregator level to prevent empty directory names
- Logger initialized in root PersistentPreRunE, stored as package-level var
- Console encoder (colored) as default log format, JSON only via --json flag
- --server now optional: auto port-forward to hubble-relay when omitted
- Interface-based flowStream abstraction for testability (avoids bufconn complexity)
- Variadic closer pattern to pass grpc.ClientConn cleanup into streaming goroutine
- Buffered channels: flows=256, lostEvents=16 to absorb burst traffic
- YAML byte comparison for file dedup (not in-memory PoliciesEquivalent) due to any: prefix roundtrip mismatch
- matchEndpoints normalizes any: prefix for merge correctness after YAML roundtrip
- World identity detected by Identity==2 OR reserved:world label (defense in depth)
- CIDR rules placed before endpoint selector rules in output ordering
- Cross-flush dedup uses in-memory PoliciesEquivalent (not YAML bytes) since policies stay in-memory
- Cluster dedup is opt-in via --cluster-dedup due to RBAC requirements
- Cluster dedup uses startup snapshot (no periodic refresh) -- acceptable v1 limitation
- findRelayPod selects only Running pods to avoid forwarding to pending/crashed pods
- kubeConfig reused between port-forward and cluster-dedup when both active

### Pending Todos

None.

### Blockers/Concerns

None open at milestone close. Prior concerns resolved:
- ~~Cilium monorepo dependency inflates binary~~ — accepted (~40 MiB, acceptable for ops tool).
- ~~Flow label completeness for `app.kubernetes.io/*`~~ — smart label heuristics + workload-name fallback work in practice.

## Session Continuity

Last session: 2026-04-24T16:00:00Z
Stopped at: v1.0 and v1.1 milestones archived — ready for v1.2 scoping.
Resume: Run `/gsd:new-milestone` to scope v1.2 (L7 + auto-apply + metrics).
