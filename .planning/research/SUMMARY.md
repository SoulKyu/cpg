# Project Research Summary

**Project:** CPG v1.1 -- L7 Policies & Apply Command
**Domain:** Go CLI tool extending Cilium Policy Generator with L7 HTTP/DNS policy generation and safe cluster apply
**Researched:** 2026-03-09
**Confidence:** HIGH

## Executive Summary

CPG v1.1 adds three capabilities to the existing L4 policy generator: L7 HTTP rules (method + path), L7 DNS/FQDN egress rules (toFQDNs with companion DNS allow), and a `cpg apply` command with dry-run-by-default semantics. The critical finding is that **no new Go dependencies are required** -- all needed types (`api.L7Rules`, `api.PortRuleHTTP`, `api.PortRuleDNS`, `api.FQDNSelector`, `flowpb.Layer7`) and APIs (`k8s.io/client-go/dynamic` with server-side apply) already exist in the current `go.mod`. The architecture extends cleanly: L7 rules attach to the existing `PortRule.Rules` field, and the apply command uses the dynamic client with `unstructured.Unstructured` for CRD operations.

The recommended approach is to build DNS/FQDN rules first (lower risk, data available via `flow.DestinationNames` on existing L4 dropped flows), then HTTP L7 rules (higher value but requires L7 visibility enabled in the cluster), then the apply command (fully independent). This order front-loads the feature with fewest prerequisites while the apply command can be developed in parallel.

The dominant risks are: (1) L7 rules trigger Envoy proxy redirect which RSTs existing connections -- mitigate by generating L7 policies as separate objects and warning on L4-to-L7 transitions; (2) the chicken-and-egg problem where L7 HTTP flows require pre-existing L7 visibility -- mitigate by using `flow.DestinationNames` for DNS and documenting the visibility prerequisite for HTTP; (3) HTTP path explosion from high-cardinality URLs -- mitigate with path normalization (numeric segments, UUIDs) from day one.

## Key Findings

### Recommended Stack

No new dependencies. All v1.1 features use types and APIs already present in `cilium/cilium` v1.19.1 and `k8s.io/client-go` v0.35.0. See [STACK.md](STACK.md) for full type inventory.

**Core additions (existing imports, new usage):**
- `api.L7Rules` / `api.PortRuleHTTP` / `api.PortRuleDNS`: L7 rule containers on `PortRule.Rules` -- already in `pkg/policy/api`
- `api.FQDNSelector` / `api.EgressRule.ToFQDNs`: DNS-based egress selectors -- already in `pkg/policy/api`
- `flowpb.Layer7` / `flowpb.HTTP` / `flowpb.DNS`: L7 flow data from Hubble stream -- already in `api/v1/flow`
- `k8s.io/client-go/dynamic`: Dynamic client for CRD apply with server-side apply -- already a transitive dependency
- `unstructured.Unstructured` + `runtime.DefaultUnstructuredConverter`: Type conversion for dynamic client -- already in `k8s.io/apimachinery`

### Expected Features

See [FEATURES.md](FEATURES.md) for full prioritization matrix.

**Must have (table stakes):**
- HTTP method + path rules in toPorts (`rules.http`)
- DNS FQDN egress rules (`toFQDNs` with `matchName`)
- DNS proxy companion rule auto-generation (port 53 to kube-dns) -- mandatory for toFQDNs to function
- Dry-run by default for apply, `--force` to actually apply
- Server-side validation on apply (not client-side only)

**Should have (differentiators):**
- Automatic L7 rule extraction from live Hubble streams (no other OSS CLI does this)
- Combined L4+L7 policy in single CNP
- Policy diff against live cluster state on apply
- L7 flow absence detection with user guidance

**Defer (v2+):**
- FQDN wildcard pattern inference (high risk of being too broad)
- Kafka/gRPC L7 rules
- Multi-cluster apply
- Rollback/undo (belongs in GitOps pipelines)
- HTTP header matching (high cardinality, rarely needed)

### Architecture Approach

The existing pipeline (Hubble gRPC -> aggregator -> builder -> dedup -> merge -> writer) stays intact. L7 data enriches the same flow objects, same aggregation keys, same builder output. The builder gets new extraction functions (`extractHTTPRule`, `extractDNSQuery`) and a `portAccumulator` pattern to group L7 rules per port. FQDN rules require a separate code path in `buildEgressRules()` because `ToFQDNs` cannot coexist with other `To*` fields. The apply command is a new `cmd/cpg/apply.go` + `pkg/k8s/apply.go` using the dynamic client with SSA. See [ARCHITECTURE.md](ARCHITECTURE.md) for component map and patterns.

**Major components modified/added:**
1. `pkg/policy/builder.go` -- add L7 HTTP/DNS rule extraction and port accumulator pattern
2. `pkg/policy/merge.go` -- extend `mergePortRules()` to handle `Rules` field with type-awareness
3. `pkg/policy/dedup.go` -- extend `normalizeRule()` to sort L7 rules for deterministic comparison
4. `pkg/k8s/apply.go` (NEW) -- dynamic client apply with dry-run, diff, ownership checks
5. `cmd/cpg/apply.go` (NEW) -- cobra command wiring with `--dir`, `--namespace`, `--force`

### Critical Pitfalls

See [PITFALLS.md](PITFALLS.md) for all 12 pitfalls with recovery strategies.

1. **Envoy proxy redirect on L4-to-L7 transition** -- Adding L7 rules RSTs existing connections. Generate L7 as separate policy objects; warn on transitions in apply diff.
2. **Missing DNS companion rule** -- toFQDNs silently fails without port 53 allow to kube-dns. Auto-generate companion rule alongside every toFQDNs rule.
3. **L7Rules union constraint** -- Cannot mix HTTP and DNS on same PortRule. Use L7 type as part of merge grouping key; validate before writing.
4. **HTTP path explosion** -- `/api/users/123` produces unbounded rules. Normalize paths from day one (numeric segments -> `[0-9]+`, UUIDs -> regex).
5. **MergePolicy blind to L7 rules** -- Current merge drops L7 rules silently. Must extend merge before any L7 generation ships.

## Implications for Roadmap

Based on research, suggested phase structure:

### Phase 1: L7 DNS/FQDN Egress Rules

**Rationale:** DNS data is available on existing L4 dropped flows via `flow.DestinationNames` -- no L7 visibility prerequisite. Lower risk entry point to L7 generation. Every Cilium tutorial covers FQDN policies, so users expect this.

**Delivers:** `toFQDNs` egress rules with `matchName`, auto-generated DNS proxy companion rule (port 53/ANY to kube-dns with `rules.dns`), extended dedup/merge for FQDN rules.

**Addresses:** DNS FQDN egress rules (P1), DNS proxy companion rule (P1), FQDN dedup/merge.

**Avoids:** Missing DNS companion rule (Pitfall 3), trailing dot mismatch (Pitfall 10), identity exhaustion warnings (Pitfall 12).

### Phase 2: L7 HTTP Rules

**Rationale:** Higher value but requires L7 visibility enabled in cluster. Depends on Phase 1 patterns (port accumulator, L7-aware merge) being established. The `portAccumulator` refactor and L7-aware merge are prerequisites.

**Delivers:** HTTP method + path rules in `toPorts[].rules.http`, path normalization (numeric/UUID segments), L7 flow absence detection with guidance, extended dedup normalization for HTTP rules.

**Addresses:** HTTP method + path rules (P1), automatic L7 extraction (differentiator), combined L4+L7 policy.

**Avoids:** Path explosion (Pitfall 7), response vs request flow confusion (Pitfall 9), L7 union constraint (Pitfall 2), merge blindness (Pitfall 6), dedup normalization gap (Pitfall 8).

### Phase 3: Apply Command

**Rationale:** Fully independent of L7 features -- works with any valid CNP on disk. Can be built in parallel with Phases 1-2 but ships after to allow testing with L7 policies.

**Delivers:** `cpg apply` with dry-run default, `--force` for real apply, server-side dry-run validation, policy diff against cluster state, ownership guard (`managed-by: cpg`), drift detection via spec-hash annotation.

**Addresses:** Dry-run by default (P1), force flag (P1), server-side validation (P1), policy diff (P2).

**Avoids:** Envoy redirect disruption on L4-to-L7 transition (Pitfall 1), drift overwrite (Pitfall 4), RBAC failures (Pitfall 11).

### Phase 4: Hubble Client Verification and Polish

**Rationale:** Last because it requires a live cluster. Verify that dropped L7 flows contain the `.L7` field, that `flow.DestinationNames` is populated, and that the filter configuration is correct.

**Delivers:** Verified Hubble filter for L7 flows, optional `--l7` flag for explicit L7 opt-in, documentation for L7 visibility prerequisites.

**Addresses:** L7 visibility chicken-and-egg (Pitfall 5), Hubble filter correctness.

### Phase Ordering Rationale

- DNS before HTTP: DNS data is available without L7 visibility prerequisites, establishing L7-aware patterns (merge, dedup) that HTTP reuses.
- Apply after L7 generation: Apply is independent but benefits from testing with L7 policies to validate the L4-to-L7 transition warning.
- Hubble verification last: Requires live cluster; earlier phases can use mocked flow data in tests.
- Phases 1 and 2 share infrastructure (port accumulator, L7-aware merge) so Phase 1 pays the refactoring cost.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 1 (DNS/FQDN):** Verify `flow.DestinationNames` availability with real cluster data. Confirm kube-dns label selectors across distributions (EKS, GKE, AKS use different labels).
- **Phase 2 (HTTP L7):** Verify L7 DROPPED flows contain `.L7` field in practice. Path normalization heuristics need validation against real traffic patterns.

Phases with standard patterns (skip research-phase):
- **Phase 3 (Apply):** Well-documented dynamic client + SSA pattern. Stack research already provides exact API signatures.
- **Phase 4 (Hubble verification):** Straightforward filter adjustment; testing-focused, not design-focused.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All types verified on pkg.go.dev for exact versions in go.mod. No new dependencies. |
| Features | MEDIUM | L7 policy structure is well-documented. `flow.DestinationNames` availability and L7 visibility chicken-and-egg need cluster validation. |
| Architecture | HIGH | Extends existing well-factored codebase. Integration points identified with line-number precision. |
| Pitfalls | HIGH | Critical pitfalls verified against Cilium GitHub issues with reproduction evidence. |

**Overall confidence:** HIGH

### Gaps to Address

- **`flow.DestinationNames` availability:** Confirmed in Hubble docs but depends on DNS proxy being enabled (`--enable-l7-proxy=true`). Needs live cluster validation during Phase 1 development.
- **L7 DROPPED flow content:** STACK.md notes MEDIUM confidence that L7 dropped flows contain the `.L7` field. Verify with real cluster before Phase 2.
- **Kube-dns label selectors:** The DNS companion rule targets `k8s:k8s-app: kube-dns` in `kube-system`. EKS/GKE may use different labels (`k8s-app: coredns`, etc.). Need to handle distribution variants or make configurable.
- **Path normalization quality:** Heuristics for numeric/UUID detection may produce overly broad or narrow patterns. Needs iterative tuning with real HTTP traffic.

## Sources

### Primary (HIGH confidence)
- [Cilium policy API types - pkg.go.dev](https://pkg.go.dev/github.com/cilium/cilium@v1.19.1/pkg/policy/api) -- L7Rules, PortRuleHTTP, PortRuleDNS, FQDNSelector
- [Cilium flow proto - pkg.go.dev](https://pkg.go.dev/github.com/cilium/cilium@v1.19.1/api/v1/flow) -- Layer7, HTTP, DNS message types
- [client-go dynamic - pkg.go.dev](https://pkg.go.dev/k8s.io/client-go@v0.35.0/dynamic) -- ResourceInterface.Apply() with SSA
- [Cilium DNS-Based Policies](https://docs.cilium.io/en/stable/security/dns/) -- toFQDNs constraints, DNS proxy requirement
- [Cilium L7 Policy Language](https://docs.cilium.io/en/stable/security/policy/language/) -- L7 rule syntax
- [Kubernetes Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/) -- SSA semantics

### Secondary (MEDIUM confidence)
- [CNCF: Safely Managing Cilium Network Policies](https://www.cncf.io/blog/2025/11/06/safely-managing-cilium-network-policies-in-kubernetes-testing-and-simulation-techniques/) -- Dry-run and audit mode patterns
- [Cilium L7 Visibility](https://docs.cilium.io/en/stable/observability/visibility/) -- L7 flow prerequisites
- [Cilium FQDN Deep Dive](https://hackmd.io/@Echo-Live/B1UOe_yr5) -- DNS proxy internals

### Tertiary (needs validation)
- Cilium GitHub issues (#35525, #43964, #30581) -- L7 proxy redirect connection reset behavior
- `flow.DestinationNames` field behavior -- referenced in docs but needs live cluster confirmation

---
*Research completed: 2026-03-09*
*Ready for roadmap: yes*
