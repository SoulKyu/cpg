# Requirements: CPG (Cilium Policy Generator)

**Defined:** 2026-03-08
**Core Value:** Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.

## v1 Requirements

Requirements for initial release. Each maps to roadmap phases.

### Connectivity

- [x] **CONN-01**: Tool connects to Hubble Relay via gRPC using cilium/cilium observer proto
- [x] **CONN-02**: Tool auto port-forwards to hubble-relay service in kube-system
- [x] **CONN-03**: User can override relay address with `--server` flag
- [x] **CONN-04**: User can filter observed flows by namespace (`--namespace`) or all namespaces (`--all-namespaces`)
- [x] **CONN-05**: Tool detects and warns about LostEvents from Hubble ring buffer overflow

### Policy Generation

- [x] **PGEN-01**: Tool generates ingress CiliumNetworkPolicy from dropped flows
- [x] **PGEN-02**: Tool generates egress CiliumNetworkPolicy from dropped flows
- [x] **PGEN-03**: Tool generates CIDR-based rules (toCIDR/fromCIDR) for external traffic (world identity)
- [x] **PGEN-04**: Tool uses smart label selection for endpoint selectors (app.kubernetes.io/*, workload name)
- [x] **PGEN-05**: Generated policies use exact port number + protocol (TCP/UDP)
- [x] **PGEN-06**: Generated YAML is valid CiliumNetworkPolicy that applies cleanly with kubectl

### Output

- [x] **OUTP-01**: Tool outputs one YAML file per policy in organized directory structure
- [x] **OUTP-02**: Tool generates policies continuously in real-time as flows arrive (streaming)
- [x] **OUTP-03**: Tool uses structured logging via zap with configurable log levels

### Deduplication

- [x] **DEDP-01**: Tool deduplicates against existing files in output directory
- [x] **DEDP-02**: Tool deduplicates against live CiliumNetworkPolicies in cluster via client-go
- [x] **DEDP-03**: Tool aggregates similar flows before generating policies (avoid one policy per packet)

## v1.1 Requirements (Offline Replay & Policy Analysis)

Spec: `docs/superpowers/specs/2026-04-24-offline-replay-and-analysis-design.md`
Plan: `docs/superpowers/plans/2026-04-24-offline-replay-and-analysis.md`

### Offline Replay

- [ ] **OFFL-01**: Tool ingests Hubble jsonpb dumps (`hubble observe --output jsonpb`)
      through a new `cpg replay <file>` subcommand
- [ ] **OFFL-02**: Replay supports stdin (`-`) and transparent gzip (`.gz`)
- [ ] **OFFL-03**: Non-DROPPED verdicts and malformed lines are skipped with counters
      surfaced in the session summary

### Per-rule Evidence

- [ ] **EVID-01**: Every rule emitted by `generate` / `replay` is attributed to its
      contributing flows (samples + counters + first/last seen) on disk
- [ ] **EVID-02**: Evidence lives under `$XDG_CACHE_HOME/cpg/evidence`, keyed by a
      hash of the output directory; `--evidence-dir` overrides the location
- [ ] **EVID-03**: Evidence is bounded by `--evidence-samples` (per rule) and
      `--evidence-sessions` (per policy); FIFO by time
- [ ] **EVID-04**: Evidence merges across sessions mirror the merge semantics of
      the policy YAML (preserve rules not re-emitted in the current session)

### Explain

- [ ] **EXPL-01**: `cpg explain <NAMESPACE/WORKLOAD | path/to/policy.yaml>` reads the
      evidence file and renders per-rule flow attribution
- [ ] **EXPL-02**: Filters: `--ingress` / `--egress`, `--port`, `--peer KEY=VAL`,
      `--peer-cidr`, `--since`
- [ ] **EXPL-03**: Output formats: text (default, ANSI on TTY), `--json`, and
      `--format yaml`

### Dry-run

- [ ] **DRYR-01**: `--dry-run` on `generate` and `replay` suppresses filesystem
      writes (policies and evidence) while running the full pipeline
- [ ] **DRYR-02**: `--dry-run` prints a unified YAML diff against existing files on
      disk; `--no-diff` disables just the diff

## v1.2 Requirements (L7 Policies & Auto-Apply)

### L7 Policy Generation

- **L7-01**: Tool generates L7 policies (HTTP path/method, DNS names) from L7 flows
- **L7-02**: Tool supports two-step workflow (deploy L4, observe L7, then generate L7 policies)

### Auto-Apply

- **APLY-01**: `cpg apply` command applies generated policies to the cluster
- **APLY-02**: `cpg apply` defaults to dry-run semantics; `--force` performs the real apply

### Advanced Features

- **ADV-01**: Tool merges/consolidates multiple granular policies into fewer broader ones
- **ADV-02**: Tool integrates with Cilium policy audit mode for automated audit-then-enforce workflow
- **ADV-03**: Tool exposes Prometheus metrics for long-running instances
- **ADV-04**: Tool shows policy diff against existing cluster state (subsumed by DRYR-02 once v1.1 ships)

## Out of Scope

| Feature | Reason |
|---------|--------|
| Named port resolution | Exact port numbers are unambiguous and match datapath directly |
| CiliumClusterwideNetworkPolicy | Namespace-scoped only — cluster-wide policies are hand-crafted by platform teams |
| Web UI / dashboard | CLI tool only — editor.networkpolicy.io exists for visualization |
| Auto kubectl apply without safeguards | Dangerous in production — v1.2 apply defaults to dry-run |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| CONN-01 | Phase 2 | Complete |
| CONN-02 | Phase 3 | Complete |
| CONN-03 | Phase 2 | Complete |
| CONN-04 | Phase 2 | Complete |
| CONN-05 | Phase 2 | Complete |
| PGEN-01 | Phase 1 | Complete |
| PGEN-02 | Phase 1 | Complete |
| PGEN-03 | Phase 3 | Complete |
| PGEN-04 | Phase 1 | Complete |
| PGEN-05 | Phase 1 | Complete |
| PGEN-06 | Phase 1 | Complete |
| OUTP-01 | Phase 1 | Complete |
| OUTP-02 | Phase 2 | Complete |
| OUTP-03 | Phase 1 | Complete |
| DEDP-01 | Phase 3 | Complete |
| DEDP-02 | Phase 3 | Complete |
| DEDP-03 | Phase 3 | Complete |

| OFFL-01 | Phase 4 | Planned |
| OFFL-02 | Phase 4 | Planned |
| OFFL-03 | Phase 4 | Planned |
| EVID-01 | Phase 4 | Planned |
| EVID-02 | Phase 4 | Planned |
| EVID-03 | Phase 4 | Planned |
| EVID-04 | Phase 4 | Planned |
| EXPL-01 | Phase 6 | Planned |
| EXPL-02 | Phase 6 | Planned |
| EXPL-03 | Phase 6 | Planned |
| DRYR-01 | Phase 5 | Planned |
| DRYR-02 | Phase 5 | Planned |

**Coverage:**
- v1.0 requirements: 17 total, all complete
- v1.1 requirements: 12 total, mapped to phases 4–6

---
*Requirements defined: 2026-03-08*
*Last updated: 2026-04-24 — added v1.1 offline replay, evidence, explain, dry-run; pushed L7 + auto-apply to v1.2.*
