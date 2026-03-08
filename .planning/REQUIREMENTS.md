# Requirements: CPG (Cilium Policy Generator)

**Defined:** 2026-03-08
**Core Value:** Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.

## v1 Requirements

Requirements for initial release. Each maps to roadmap phases.

### Connectivity

- [x] **CONN-01**: Tool connects to Hubble Relay via gRPC using cilium/cilium observer proto
- [ ] **CONN-02**: Tool auto port-forwards to hubble-relay service in kube-system
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
- [ ] **DEDP-02**: Tool deduplicates against live CiliumNetworkPolicies in cluster via client-go
- [ ] **DEDP-03**: Tool aggregates similar flows before generating policies (avoid one policy per packet)

## v2 Requirements

### L7 Policy Generation

- **L7-01**: Tool generates L7 policies (HTTP path/method, DNS names) from L7 flows
- **L7-02**: Tool supports two-step workflow (deploy L4, observe L7, then generate L7 policies)

### Advanced Features

- **ADV-01**: Tool merges/consolidates multiple granular policies into fewer broader ones
- **ADV-02**: Tool integrates with Cilium policy audit mode for automated audit-then-enforce workflow
- **ADV-03**: Tool exposes Prometheus metrics for long-running instances
- **ADV-04**: Tool shows policy diff against existing cluster state

## Out of Scope

| Feature | Reason |
|---------|--------|
| JSON file/stdin input mode | gRPC only -- eliminates custom JSON parsing, uses native proto types |
| Named port resolution | Exact port numbers are unambiguous and match datapath directly |
| CiliumClusterwideNetworkPolicy | Namespace-scoped only -- cluster-wide policies are hand-crafted by platform teams |
| Web UI / dashboard | CLI tool only -- editor.networkpolicy.io exists for visualization |
| Policy simulation / dry-run | Cilium audit mode already provides this -- don't reimplement |
| Auto kubectl apply | Dangerous in production -- users apply via their GitOps pipeline |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| CONN-01 | Phase 2 | Complete |
| CONN-02 | Phase 3 | Pending |
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
| DEDP-02 | Phase 3 | Pending |
| DEDP-03 | Phase 3 | Pending |

**Coverage:**
- v1 requirements: 17 total
- Mapped to phases: 17
- Unmapped: 0

---
*Requirements defined: 2026-03-08*
*Last updated: 2026-03-08 after roadmap creation*
