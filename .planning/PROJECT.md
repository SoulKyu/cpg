# CPG — Cilium Policy Generator

## What This Is

A Go CLI tool that connects directly to Hubble Relay via gRPC, observes dropped/denied network flows in real-time, and automatically generates CiliumNetworkPolicy YAML files. Built for SRE/DevOps teams operating Cilium clusters with default-deny network policies, it eliminates the tedious manual process of writing allow rules by turning observed denials into ready-to-apply policies.

## Core Value

Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.

## Requirements

### Validated

<!-- Shipped and confirmed valuable in v1.0. -->

- ✓ Connect to Hubble Relay via gRPC with auto port-forward — v1.0
- ✓ Override relay address with `--server` flag — v1.0
- ✓ Observe dropped flows filtered by namespace or all-namespaces — v1.0
- ✓ Generate CiliumNetworkPolicy for ingress/egress traffic — v1.0
- ✓ Generate CIDR-based policies for external traffic — v1.0
- ✓ Exact ports (port number + protocol) in generated policies — v1.0
- ✓ Smart label selection (app.kubernetes.io/*, workload name) — v1.0
- ✓ One YAML file per policy in organized directory structure — v1.0
- ✓ Stream policy generation continuously as flows arrive — v1.0
- ✓ Deduplicate against files and live cluster policies — v1.0
- ✓ Structured logging with zap — v1.0

### Active

<!-- Current scope for v1.1. -->

- [ ] Offline replay from Hubble jsonpb captures (`cpg replay <file>`)
- [ ] Per-rule flow evidence persisted to XDG cache
- [ ] `cpg explain <target>` to surface flows behind any generated rule
- [ ] `--dry-run` with unified YAML diff on generate and replay

### Planned

<!-- Scope for v1.2 — L7 policies and auto-apply. Moved from v1.1 to make room for
     the offline-workflow milestone first, which is foundational for iterating
     on L7 policies once we have them. -->

- [ ] Generate L7 HTTP policies (method, path, headers) from Hubble L7 flows — v1.2
- [ ] Generate L7 DNS policies (FQDN matchPattern) from Hubble DNS flows — v1.2
- [ ] `cpg apply` command: apply generated policies to cluster (dry-run by default, --force to apply) — v1.2

### Out of Scope

- Named port resolution — use exact port numbers only
- CiliumClusterwideNetworkPolicy generation — namespace-scoped only
- Web UI or dashboard — CLI tool only

## Context

- Target environment: Kubernetes clusters running Cilium with default-deny policies
- Hubble Relay exposes a gRPC API (protobuf) on port 4245, typically behind a ClusterIP service in kube-system
- Cilium's Go module (`github.com/cilium/cilium`) provides both the observer proto types and the CiliumNetworkPolicy CRD types
- Hubble flow JSON structure includes source/destination identity, labels, namespace, pod name, workload info, traffic direction, ports, and verdict
- The tool replaces the workflow: `hubble observe --verdict DROPPED` → manually read flows → manually write CNP YAML

## Constraints

- **Language**: Go 1.23+ — latest stable, leveraging iterators and rangefunc
- **CLI framework**: cobra — consistent with Cilium/Kubernetes ecosystem tooling
- **Logging**: zap — structured, performant, widely used in K8s ecosystem
- **K8s client**: client-go — direct access to CiliumNetworkPolicy CRDs
- **Hubble integration**: gRPC via cilium/cilium observer proto — no JSON parsing, native types
- **Architecture**: Domain-driven packages — `pkg/{hubble,policy,dedup,k8s,labels}` + `cmd/`

## Key Decisions

<!-- Decisions that constrain future work. Add throughout project lifecycle. -->

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| gRPC only initially | Simplified v1 architecture — offline jsonpb ingestion added in v1.1 for iteration workflow | Revised v1.1 |
| Auto port-forward to Hubble Relay | UX parity with hubble CLI, zero manual setup | — Pending |
| One file per policy output | Easier to review, git-diff friendly, selective apply | — Pending |
| Smart label defaults over configurable | Reduces config burden, app.kubernetes.io labels are standard | — Pending |
| zap over slog | Team preference, widely used in K8s/Cilium ecosystem | — Pending |
| Domain-driven pkg/ structure | Clear separation of concerns, testable packages | — Pending |
| Exact ports over named ports | Simpler, no ambiguity, matches flow data directly | — Pending |
| Both local + cluster dedup | Comprehensive dedup prevents duplicate policies in all scenarios | — Pending |

## Current Milestone: v1.1 Offline Replay & Policy Analysis

**Goal:** Add an offline iteration workflow (capture once, replay many), per-rule
flow evidence (`cpg explain`), and a dry-run mode with unified diff, so users can
iterate on policy generation without reproducing traffic and audit why each rule
was generated.

**Target features:**
- `cpg replay <file.jsonl>` — ingest Hubble jsonpb dumps through the same pipeline
- Per-rule evidence persisted under `$XDG_CACHE_HOME/cpg/evidence`
- `cpg explain <NAMESPACE/WORKLOAD>` with filters and multi-format output
- `--dry-run` with unified YAML diff on both `generate` and `replay`

## Next Milestone: v1.2 L7 Policies & Auto-Apply

Pushed from v1.1 to v1.2. See the "Planned" section above.

---
*Last updated: 2026-04-24 — v1.1 scope changed from L7+Auto-Apply to Offline Workflow; L7+Auto-Apply moved to v1.2.*
