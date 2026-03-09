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

- [ ] Generate L7 HTTP policies (method, path, headers) from Hubble L7 flows
- [ ] Generate L7 DNS policies (FQDN matchPattern) from Hubble DNS flows
- [ ] `cpg apply` command: apply generated policies to cluster (dry-run by default, --force to apply)

### Out of Scope

- JSON file/stdin input mode — gRPC only, no offline mode
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
| gRPC only (no JSON stdin/file) | Simplifies architecture, uses native proto types, eliminates custom parsing | — Pending |
| Auto port-forward to Hubble Relay | UX parity with hubble CLI, zero manual setup | — Pending |
| One file per policy output | Easier to review, git-diff friendly, selective apply | — Pending |
| Smart label defaults over configurable | Reduces config burden, app.kubernetes.io labels are standard | — Pending |
| zap over slog | Team preference, widely used in K8s/Cilium ecosystem | — Pending |
| Domain-driven pkg/ structure | Clear separation of concerns, testable packages | — Pending |
| Exact ports over named ports | Simpler, no ambiguity, matches flow data directly | — Pending |
| Both local + cluster dedup | Comprehensive dedup prevents duplicate policies in all scenarios | — Pending |

## Current Milestone: v1.1 L7 Policies & Auto-Apply

**Goal:** Extend cpg with L7 policy generation (HTTP methods/paths/headers, DNS FQDN patterns) and a safe apply command with dry-run by default.

**Target features:**
- L7 HTTP policy rules from Hubble L7 flow data
- L7 DNS policy rules (FQDN matching) from Hubble DNS flow data
- `cpg apply` command with dry-run default and `--force` for real apply

---
*Last updated: 2026-03-09 after v1.1 milestone start*
