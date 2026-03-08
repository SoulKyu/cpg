# Roadmap: CPG (Cilium Policy Generator)

## Overview

CPG delivers a Go CLI tool that turns Hubble dropped flows into ready-to-apply CiliumNetworkPolicies. The roadmap moves from pure domain logic (policy building, label selection, YAML output) through Hubble gRPC integration (streaming pipeline, real-time generation) to production features (auto port-forward, deduplication, CIDR rules). Each phase delivers a testable, coherent capability -- Phase 1 produces correct policies from test data, Phase 2 produces policies from a live Hubble stream, Phase 3 adds the UX and dedup features that make it production-ready.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [x] **Phase 1: Core Policy Engine** - Go module scaffolding, flow-to-CiliumNetworkPolicy transformation, smart label selection, YAML output
- [x] **Phase 2: Hubble Streaming Pipeline** - gRPC connection to Hubble Relay, flow filtering, real-time streaming generation, CLI wiring (completed 2026-03-08)
- [ ] **Phase 3: Production Hardening** - Auto port-forward, file and cluster deduplication, CIDR policies for external traffic, flow aggregation

## Phase Details

### Phase 1: Core Policy Engine
**Goal**: Users can transform Hubble flow data into valid, correctly-scoped CiliumNetworkPolicy YAML files
**Depends on**: Nothing (first phase)
**Requirements**: PGEN-01, PGEN-02, PGEN-04, PGEN-05, PGEN-06, OUTP-01, OUTP-03
**Success Criteria** (what must be TRUE):
  1. Given a dropped ingress flow, the tool produces a valid CiliumNetworkPolicy YAML that would allow that traffic
  2. Given a dropped egress flow, the tool produces a valid CiliumNetworkPolicy YAML that would allow that traffic
  3. Generated policies use smart label selectors (app.kubernetes.io/*, workload name) rather than raw pod names or IPs
  4. Generated policies specify exact port number and protocol (TCP/UDP) in rules
  5. Each policy is written as a separate YAML file in an organized directory structure with structured log output
**Plans:** 3 plans

Plans:
- [x] 01-01-PLAN.md — Go module scaffolding + label selector package (PGEN-04)
- [x] 01-02-PLAN.md — Policy builder and merge logic via TDD (PGEN-01, PGEN-02, PGEN-05, PGEN-06)
- [x] 01-03-PLAN.md — Output writer + CLI generate command + zap logging (OUTP-01, OUTP-03)

### Phase 2: Hubble Streaming Pipeline
**Goal**: Users can connect to a running Hubble Relay and generate policies in real-time from live dropped flows
**Depends on**: Phase 1
**Requirements**: CONN-01, CONN-03, CONN-04, CONN-05, OUTP-02
**Success Criteria** (what must be TRUE):
  1. Running `cpg generate --server <addr>` connects to Hubble Relay via gRPC and streams dropped flows
  2. User can filter observed flows by namespace (`--namespace`) or observe all namespaces (`--all-namespaces`)
  3. Policies are generated continuously as new dropped flows arrive (not batch)
  4. Tool warns the user when Hubble ring buffer overflow causes lost events
**Plans:** 2/2 plans complete

Plans:
- [ ] 02-01-PLAN.md — Hubble gRPC client + FlowFilter namespace filtering (CONN-01, CONN-03, CONN-04)
- [ ] 02-02-PLAN.md — Aggregator, pipeline orchestration, LostEvents monitor, CLI wiring (CONN-05, OUTP-02)

### Phase 3: Production Hardening
**Goal**: Users get zero-friction connectivity, no duplicate policies, and coverage for external traffic
**Depends on**: Phase 2
**Requirements**: CONN-02, DEDP-01, DEDP-02, DEDP-03, PGEN-03
**Success Criteria** (what must be TRUE):
  1. Running `cpg generate` without `--server` auto port-forwards to hubble-relay service in kube-system
  2. Tool skips generating a policy if an equivalent file already exists in the output directory
  3. Tool skips generating a policy if an equivalent CiliumNetworkPolicy already exists in the cluster
  4. Tool aggregates similar flows before generating policies (avoids one policy per packet)
  5. External traffic (world identity) produces CIDR-based rules (toCIDR/fromCIDR) instead of endpoint selectors
**Plans:** 2 plans

Plans:
- [ ] 03-01-PLAN.md — CIDR rules for world identity + file-based deduplication (PGEN-03, DEDP-01)
- [ ] 03-02-PLAN.md — Auto port-forward, cluster dedup, cross-flush dedup, CLI wiring (CONN-02, DEDP-02, DEDP-03)

## Progress

**Execution Order:**
Phases execute in numeric order: 1 -> 2 -> 3

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Core Policy Engine | 3/3 | Complete | 2026-03-08 |
| 2. Hubble Streaming Pipeline | 2/2 | Complete   | 2026-03-08 |
| 3. Production Hardening | 0/2 | Not started | - |
