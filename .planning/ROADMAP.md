# Roadmap: CPG (Cilium Policy Generator)

## Overview

CPG delivers a Go CLI tool that turns Hubble dropped flows into ready-to-apply CiliumNetworkPolicies. The roadmap moves from pure domain logic (policy building, label selection, YAML output) through Hubble gRPC integration (streaming pipeline, real-time generation) to production features (auto port-forward, deduplication, CIDR rules). Each phase delivers a testable, coherent capability -- Phase 1 produces correct policies from test data, Phase 2 produces policies from a live Hubble stream, Phase 3 adds the UX and dedup features that make it production-ready.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

**v1.0 — shipped:**
- [x] **Phase 1: Core Policy Engine** - Go module scaffolding, flow-to-CiliumNetworkPolicy transformation, smart label selection, YAML output
- [x] **Phase 2: Hubble Streaming Pipeline** - gRPC connection to Hubble Relay, flow filtering, real-time streaming generation, CLI wiring (completed 2026-03-08)
- [x] **Phase 3: Production Hardening** - Auto port-forward, file and cluster deduplication, CIDR policies for external traffic, flow aggregation (completed 2026-03-08)

**v1.1 — Offline Replay & Policy Analysis (active):**
- [ ] **Phase 4: Offline Replay Core** - FlowSource abstraction, jsonpb file ingestion, evidence schema + writer, per-rule attribution from builder
- [ ] **Phase 5: Dry-Run & Pipeline Integration** - YAML unified diff, policyWriter dry-run branch, pipeline fan-out for evidence
- [ ] **Phase 6: Explain Command** - Shared flag helper, `cpg replay` and `cpg explain` commands, text/json/yaml renderers, README updates

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
**Plans:** 2/2 plans complete

Plans:
- [x] 03-01-PLAN.md — CIDR rules for world identity + file-based deduplication (PGEN-03, DEDP-01)
- [ ] 03-02-PLAN.md — Auto port-forward, cluster dedup, cross-flush dedup, CLI wiring (CONN-02, DEDP-02, DEDP-03)

### Phase 4: Offline Replay Core
**Goal**: Users can ingest a Hubble jsonpb capture through the existing pipeline and receive policies + per-rule evidence on disk
**Depends on**: Phase 3
**Requirements**: OFFL-01, OFFL-02, OFFL-03, EVID-01, EVID-02, EVID-03, EVID-04
**Success Criteria** (what must be TRUE):
  1. `cpg replay <file.jsonl>` produces the same policy YAMLs as `cpg generate` would for the same flows
  2. Per-rule evidence files exist under `$XDG_CACHE_HOME/cpg/evidence/<hash>/<ns>/<workload>.json` with schema_version=1
  3. `.gz` extension triggers transparent gzip decompression; `-` reads from stdin
  4. Malformed lines and non-DROPPED verdicts are counted and surfaced in the session summary
  5. Evidence merges across sessions preserve first_seen and cap samples/sessions FIFO
**Plans:** 0/0 plans (to be created from the master plan Phase 1-4)

### Phase 5: Dry-Run & Pipeline Integration
**Goal**: Users can preview generation changes with a unified diff and the pipeline writes evidence alongside policies
**Depends on**: Phase 4
**Requirements**: DRYR-01, DRYR-02
**Success Criteria** (what must be TRUE):
  1. `--dry-run` on `generate` and `replay` writes no files (policy or evidence) and logs "would write" per policy
  2. With `--dry-run` and an existing policy on disk, a unified YAML diff prints to stdout (colored on a TTY)
  3. `--no-diff` disables the diff output; `--dry-run` log lines still fire
  4. `policyWriter` and `evidenceWriter` consume a fanned-out `PolicyEvent` channel; neither blocks the other
**Plans:** 0/0 plans (to be created from the master plan Phase 5-6)

### Phase 6: Explain Command
**Goal**: Users can inspect which flows produced each generated rule
**Depends on**: Phase 4 (needs evidence files)
**Requirements**: EXPL-01, EXPL-02, EXPL-03
**Success Criteria** (what must be TRUE):
  1. `cpg explain NAMESPACE/WORKLOAD` reads evidence and renders per-rule attribution in text
  2. `cpg explain path/to/policy.yaml` resolves NS + workload from the YAML and rejects non-`cpg-` names
  3. Filters (`--ingress`, `--egress`, `--port`, `--peer`, `--peer-cidr`, `--since`) compose correctly
  4. `--json` and `--format yaml` emit structured output carrying the full matched-rule set
  5. Missing evidence produces an actionable error naming the path that was checked
**Plans:** 0/0 plans (to be created from the master plan Phase 7-8)

## Progress

**Execution Order:**
Phases execute in numeric order: 1 -> 2 -> 3 -> 4 -> 5 -> 6

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Core Policy Engine | 3/3 | Complete | 2026-03-08 |
| 2. Hubble Streaming Pipeline | 2/2 | Complete | 2026-03-08 |
| 3. Production Hardening | 2/2 | Complete | 2026-03-08 |
| 4. Offline Replay Core | 0/0 | Planned | — |
| 5. Dry-Run & Pipeline Integration | 0/0 | Planned | — |
| 6. Explain Command | 0/0 | Planned | — |
