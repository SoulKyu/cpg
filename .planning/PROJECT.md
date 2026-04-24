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
- ✓ Offline replay from Hubble jsonpb captures (`cpg replay <file>`, stdin, gzip) — v1.1
- ✓ Per-rule flow evidence persisted to `$XDG_CACHE_HOME/cpg/evidence` with FIFO caps — v1.1
- ✓ `cpg explain <NS/workload | policy.yaml>` with filters + text/JSON/YAML renderers — v1.1
- ✓ `--dry-run` with unified YAML diff on `generate` and `replay` (ANSI on TTY) — v1.1

### Active

<!-- Awaiting v1.2 scoping via /gsd:new-milestone. Candidates: -->

- [ ] Generate L7 HTTP policies (method, path, headers) from Hubble L7 flows — v1.2 candidate
- [ ] Generate L7 DNS policies (FQDN matchPattern) from Hubble DNS flows — v1.2 candidate
- [ ] `cpg apply` command: apply generated policies to cluster (dry-run by default, `--force` to apply) — v1.2 candidate
- [ ] Policy consolidation / merging into broader rules — v1.2 candidate
- [ ] Prometheus metrics for long-running instances — v1.2 candidate

### Planned

<!-- Empty — next milestone will redefine v1.2 scope. -->

- _(defined during `/gsd:new-milestone`)_

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
| gRPC only initially | Simplified v1 architecture — offline jsonpb ingestion added in v1.1 for iteration workflow | ✓ Good — v1.1 added `FlowSource` abstraction cleanly |
| Auto port-forward to Hubble Relay | UX parity with hubble CLI, zero manual setup | ✓ Good — shipped v1.0 via SPDY |
| One file per policy output | Easier to review, git-diff friendly, selective apply | ✓ Good |
| Smart label defaults over configurable | Reduces config burden, app.kubernetes.io labels are standard | ✓ Good |
| zap over slog | Team preference, widely used in K8s/Cilium ecosystem | ✓ Good |
| Domain-driven pkg/ structure | Clear separation of concerns, testable packages | ✓ Good — stable through v1.1 (only `flowsource` promoted) |
| Exact ports over named ports | Simpler, no ambiguity, matches flow data directly | ✓ Good |
| Both local + cluster dedup | Comprehensive dedup prevents duplicate policies in all scenarios | ✓ Good — three-layer dedup shipped v1.0 |
| `FlowSource` interface in `pkg/flowsource` | Decouples replay (file) from live (gRPC); testable without Hubble | ✓ Good — shipped v1.1 |
| Evidence under `$XDG_CACHE_HOME/cpg/evidence`, hashed by output dir | Multiple projects don't collide; not committed; XDG-compliant | ✓ Good — shipped v1.1 |
| Evidence JSON schema v1 pinned | Reader rejects unknown versions — safe forward-compat | ✓ Good — shipped v1.1 |
| FIFO caps on samples / sessions | Bounded disk use; `--evidence-samples` / `--evidence-sessions` tunable | ✓ Good — shipped v1.1 |
| Channel fan-out (tee) for policy + evidence writers | Writers independent; neither blocks the other | ✓ Good — shipped v1.1 |
| `--dry-run` covers policies AND evidence | Pure preview semantics — no filesystem side effects | ✓ Good — shipped v1.1 |
| `cpg explain` rejects non-`cpg-` YAML names | Guards against explaining hand-crafted/non-cpg policies | ✓ Good — shipped v1.1 |

## Current State

**Shipped:** v1.0 (2026-03-08) and v1.1 (2026-04-24).

**Codebase:** 9 packages (`pkg/{labels,policy,output,hubble,k8s,dedup,flowsource,evidence,diff}` + `cmd/`). 180 tests passing. Release-please tagged `v1.6.0` (latest product release).

**Not yet started:** v1.2 scope locked via `/gsd:new-milestone`.

## Next Milestone: v1.2 L7 Policies & Auto-Apply (to be scoped)

Candidate themes — subject to revision during `/gsd:new-milestone`:
- L7 HTTP policy generation from Hubble L7 flows
- L7 DNS policy generation (FQDN matchPattern)
- `cpg apply` command with dry-run-by-default + `--force`
- Policy consolidation across overlapping rules
- Prometheus metrics for long-running deployments

---
*Last updated: 2026-04-24 — archived v1.0 and v1.1 milestones; v1.2 awaiting scoping.*
