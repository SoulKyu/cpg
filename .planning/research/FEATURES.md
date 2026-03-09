# Feature Landscape: v1.1 L7 Policies & Auto-Apply

**Domain:** Cilium L7 network policy generation + safe apply workflow
**Researched:** 2026-03-09
**Confidence:** MEDIUM -- L7 policy structure is well-documented in Cilium; Hubble L7 flow availability has important prerequisites that introduce uncertainty.

## Context: What Already Exists in v1.0

The existing pipeline handles L3/L4 CiliumNetworkPolicy generation: Hubble gRPC streaming, ingress/egress rules with endpoint selectors and CIDR, smart label selection, policy merge, file/cluster deduplication, auto port-forward. All table stakes and P2 features from the original research are shipped.

This research focuses ONLY on the three new v1.1 features:
1. L7 HTTP policy generation (method, path, headers)
2. L7 DNS/FQDN policy generation (matchPattern/matchName)
3. `cpg apply` command with safe dry-run workflow

---

## Table Stakes

Features that users expect from any tool claiming L7 policy generation or policy apply.

| Feature | Why Expected | Complexity | Dependencies |
|---------|--------------|------------|--------------|
| HTTP method + path rules in toPorts | Minimum viable L7 HTTP policy. Without method/path, L7 adds no value over L4. | MEDIUM | Hubble L7 flow data (`flow.L7.Http.Method`, `flow.L7.Http.Url`) |
| DNS FQDN egress rules (toFQDNs) | Standard pattern for allowing external DNS-resolved destinations. Every Cilium tutorial covers this. | MEDIUM | Hubble DNS flow data (`flow.L7.Dns.Query`), or `flow.DestinationNames` for L3/L4 flows |
| DNS proxy companion rule | toFQDNs policies REQUIRE an accompanying egress rule allowing DNS traffic to kube-dns on port 53. Without it, the FQDN policy silently fails. | LOW | Must auto-generate alongside every toFQDNs rule |
| Dry-run by default for apply | Applying network policies without preview is dangerous. Every serious K8s tool defaults to dry-run. | LOW | `kubectl apply --dry-run=server` or client-go equivalent |
| Show diff before apply | Users must see what will change before committing. Standard UX for any apply workflow. | MEDIUM | `kubectl diff` equivalent via client-go or exec |
| Force flag to actually apply | Explicit opt-in to real apply. Must never apply without user confirmation. | LOW | `--force` flag gating actual API server write |
| Server-side validation on apply | CiliumNetworkPolicy CRDs have validation rules. Client-only validation misses webhook checks. | LOW | `--dry-run=server` sends to API server without persistence |

## Differentiators

Features that set cpg apart from alternatives in the L7/apply space.

| Feature | Value Proposition | Complexity | Dependencies |
|---------|-------------------|------------|--------------|
| Automatic L7 rule extraction from Hubble flows | No other OSS CLI generates L7 CiliumNetworkPolicy rules from live Hubble streams. The Python competitor requires manual two-step L7 observation. | HIGH | Hubble must be configured for L7 visibility (see Pitfalls) |
| Combined L4+L7 policy in single CNP | Generate policies that have both L4 port rules AND L7 HTTP rules under the same toPorts entry. Mirrors how Cilium actually structures policies. | MEDIUM | Existing L4 builder must be extended, not replaced |
| FQDN pattern inference from DNS queries | Automatically derive `matchPattern` wildcards from observed DNS queries (e.g., multiple `*.s3.amazonaws.com` queries become a single pattern). | HIGH | Heuristic design for pattern grouping. Risk of being too broad or too narrow. |
| Apply with audit mode recommendation | After apply, suggest enabling Cilium audit mode to validate policies before enforcement. Print the `cilium endpoint config` command. | LOW | Documentation/UX only |
| Batch apply with per-policy dry-run | Apply multiple generated policies with individual dry-run results, not all-or-nothing. | MEDIUM | Iterate over output directory, dry-run each independently |
| Policy diff against live cluster state | Show exact diff between generated policy and what exists in cluster. More useful than generic `kubectl diff`. | MEDIUM | client-go list + structured comparison |

## Anti-Features

Features to explicitly NOT build for v1.1.

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Auto-apply without any confirmation | Even with `--force`, silently applying network policies in production is reckless. A bad L7 policy can break HTTP traffic for an entire service. | Always show dry-run output first. Require `--force` for real apply. Print warnings about audit mode. |
| L7 header matching in generated policies | HTTP header rules (`headerMatches`) are rarely needed for allow-listing and massively increase policy complexity. Headers are also high-cardinality (auth tokens, request IDs). | Generate method + path rules only. Document how to manually add header rules if needed. |
| Kafka L7 policy generation | Hubble supports Kafka flows but Kafka L7 policies are niche. Adding Kafka protocol handling doubles the L7 surface area for minimal user base. | Out of scope. Document as future extension point. |
| gRPC L7 policy generation | While gRPC is HTTP/2, Cilium's gRPC-specific L7 rules are rarely used in practice. Standard HTTP method+path covers most gRPC use cases. | Treat gRPC as HTTP. The POST method + path covers gRPC endpoints. |
| Automatic DNS wildcard broadening | Auto-converting `api.us-east-1.amazonaws.com` to `*.amazonaws.com` is too aggressive. Can accidentally allow traffic to unintended AWS endpoints. | Generate exact matchName by default. Offer `--fqdn-wildcard-depth N` flag for explicit opt-in to pattern broadening. |
| Apply to remote clusters | Supporting multi-cluster apply via kubeconfig context switching adds complexity for an edge case. | Apply to current kubeconfig context only. Users switch contexts themselves. |
| Rollback / undo apply | Implementing policy rollback requires state tracking, versioning, and the ability to restore previous policy state. This is what GitOps tools do. | Generate policies to files. Users manage rollback through git revert + their GitOps pipeline. |

## Feature Dependencies

```
[Existing L4 Pipeline] (v1.0, already built)
    |
    +-- [L7 HTTP Rules]
    |       |-- requires: Hubble L7 flow data (flow.Type == L7, flow.L7.Http != nil)
    |       |-- requires: L7 visibility enabled in cluster (existing L7 policy or annotation)
    |       |-- extends: builder.go toPorts with api.PortRule.Rules.HTTP
    |       |-- extends: aggregator.go to handle L7 flow grouping
    |       +-- produces: CiliumNetworkPolicy with toPorts[].rules.http[]
    |
    +-- [L7 DNS/FQDN Rules]
    |       |-- requires: Hubble DNS flow data (flow.L7.Dns.Query) OR flow.DestinationNames
    |       |-- requires: DNS proxy enabled (Cilium --enable-l7-proxy=true)
    |       |-- extends: builder.go egress rules with api.EgressRule.ToFQDNs
    |       |-- MUST also generate: DNS proxy companion rule (port 53 to kube-dns)
    |       +-- produces: CiliumNetworkPolicy with toFQDNs[] + DNS allow rule
    |
    +-- [cpg apply]
            |-- requires: client-go (already in pkg/k8s)
            |-- requires: generated policy files on disk (output of generate)
            |-- independent of: L7 features (works with any valid CNP)
            +-- produces: dry-run output, diff, actual apply with --force

[L7 HTTP Rules] --independent of--> [L7 DNS/FQDN Rules]
[L7 HTTP Rules] --independent of--> [cpg apply]
[L7 DNS/FQDN Rules] --independent of--> [cpg apply]
```

### Critical Dependency: Hubble L7 Flow Availability

**This is the single most important architectural constraint for v1.1.**

Hubble only emits L7 flow data (HTTP method/path, DNS queries) when one of these conditions is met:

1. **An existing L7 policy is in place** -- Cilium routes traffic through the Envoy proxy when an L7 rule exists, which enables L7 flow observation.
2. **L7 visibility policy exists** -- A CiliumNetworkPolicy with `toPorts[].rules.http` or `toPorts[].rules.dns` that explicitly enables L7 visibility for monitoring purposes.

This creates a **chicken-and-egg problem**: to generate L7 policies from flows, you need L7 flows, but to get L7 flows, you need L7 policies or visibility configuration.

**Practical impact on cpg:**
- L7 HTTP generation will only work in clusters that already have some L7 visibility configured.
- DNS flows are more commonly available because DNS proxy is often enabled cluster-wide (`--enable-l7-proxy=true`).
- The tool should detect when L7 data is absent and provide clear guidance.

### DNS Flow Data Sources

DNS information appears in Hubble flows in TWO places:

1. **`flow.L7.Dns`** -- Full DNS query/response data (query name, IPs, TTL, rcode). Only available when DNS proxy is active and L7 flow type.
2. **`flow.DestinationNames`** -- FQDN labels attached to L3/L4 flows after DNS resolution. Available on regular dropped flows when Cilium has seen the DNS resolution. More widely available than L7 DNS flows.

**Recommendation:** Use `flow.DestinationNames` as the PRIMARY source for FQDN policy generation (works with existing L4 dropped flows). Use `flow.L7.Dns` as a SECONDARY enrichment source when available.

## Cilium L7 Policy Structure Reference

### HTTP Rules (under toPorts)

```yaml
spec:
  endpointSelector:
    matchLabels:
      app: myservice
  ingress:
  - fromEndpoints:
    - matchLabels:
        app: client
    toPorts:
    - ports:
      - port: "80"
        protocol: TCP
      rules:
        http:
        - method: "GET"
          path: "/api/v1/.*"
        - method: "POST"
          path: "/api/v1/submit"
```

**Key fields in `api.PortRuleHTTP`:**
- `Method` -- Extended POSIX regex matched against request method
- `Path` -- Extended POSIX regex matched against request path (must start with `/`)
- `Host` -- Extended POSIX regex matched against Host header
- `Headers` -- List of required HTTP headers (key: value strings)
- `HeaderMatches` -- Advanced header matching with mismatch handling

**For cpg v1.1:** Generate `Method` + `Path` only. Skip `Headers` and `HeaderMatches` (anti-feature).

### DNS/FQDN Rules (toFQDNs)

```yaml
spec:
  endpointSelector:
    matchLabels:
      app: myservice
  egress:
  # Rule 1: Allow DNS resolution through kube-dns
  - toEndpoints:
    - matchLabels:
        k8s:io.kubernetes.pod.namespace: kube-system
        k8s:k8s-app: kube-dns
    toPorts:
    - ports:
      - port: "53"
        protocol: ANY
      rules:
        dns:
        - matchPattern: "*"
  # Rule 2: Allow traffic to resolved FQDNs
  - toFQDNs:
    - matchName: "api.github.com"
    - matchPattern: "*.s3.amazonaws.com"
```

**Key fields in `api.FQDNSelector`:**
- `matchName` -- Exact domain match (e.g., `api.github.com`)
- `matchPattern` -- Wildcard match (`*` matches DNS chars except `.`)

**Critical:** The DNS proxy companion rule (port 53 to kube-dns) is MANDATORY. Without it, toFQDNs silently fails because Cilium cannot intercept DNS responses to learn IP mappings.

### Hubble Flow Proto Fields for L7

```
flow.Type == FlowType_L7  // Identifies L7 flows

flow.L7.Http.Method       // "GET", "POST", etc.
flow.L7.Http.Url          // "/api/v1/users" (path)
flow.L7.Http.Code         // HTTP status code (response)
flow.L7.Http.Protocol     // "HTTP/1.1", "HTTP/2.0"
flow.L7.Http.Headers      // []HTTPHeader (key-value pairs)

flow.L7.Dns.Query         // "api.github.com."
flow.L7.Dns.Ips           // ["1.2.3.4", "5.6.7.8"]
flow.L7.Dns.Ttl           // 300
flow.L7.Dns.Rcode         // 0 (NOERROR)

flow.DestinationNames     // ["api.github.com"] (on L3/L4 flows)
```

## Implementation Plan Recommendation

### Phase 1: DNS/FQDN Rules (lower risk, higher availability)

**Rationale:** DNS data is more widely available than HTTP L7 data. `flow.DestinationNames` exists on regular L4 dropped flows, so this works without any L7 visibility prerequisite.

1. Detect `flow.DestinationNames` on egress dropped flows
2. Generate `toFQDNs` with `matchName` for each observed FQDN
3. Auto-generate DNS proxy companion rule (port 53 to kube-dns)
4. Optional: `--fqdn-wildcard-depth N` for pattern broadening

### Phase 2: HTTP L7 Rules (higher value, requires L7 visibility)

**Rationale:** Requires L7 visibility to be enabled in the cluster. Must handle the chicken-and-egg problem gracefully.

1. Detect `flow.Type == L7` and `flow.L7.Http != nil`
2. Extract method + path (normalize URL to path only)
3. Extend existing `toPorts` builder to add `rules.http` entries
4. Warn when no L7 flows are observed (suggest enabling L7 visibility)

### Phase 3: cpg apply (independent, parallel track)

**Rationale:** Works with any generated policy (L4 or L7). Can be built in parallel.

1. Read generated policies from output directory
2. Dry-run each against API server (`--dry-run=server`)
3. Show diff against cluster state
4. Apply with `--force` flag

## Feature Prioritization Matrix (v1.1)

| Feature | User Value | Implementation Cost | Risk | Priority |
|---------|------------|---------------------|------|----------|
| DNS/FQDN egress rules (toFQDNs) | HIGH | MEDIUM | LOW | P1 |
| DNS proxy companion rule auto-generation | HIGH | LOW | LOW | P1 |
| cpg apply with dry-run default | HIGH | MEDIUM | LOW | P1 |
| cpg apply --force for real apply | HIGH | LOW | LOW | P1 |
| HTTP method + path L7 rules | HIGH | MEDIUM | MEDIUM | P1 |
| Policy diff on apply | MEDIUM | MEDIUM | LOW | P2 |
| L7 flow absence detection + user guidance | MEDIUM | LOW | LOW | P2 |
| FQDN wildcard pattern inference | LOW | HIGH | HIGH | P3 |
| Batch apply with per-policy status | LOW | MEDIUM | LOW | P3 |
| Audit mode recommendation after apply | LOW | LOW | LOW | P3 |

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Cilium L7 HTTP policy structure | HIGH | Official docs, well-documented API types in `pkg/policy/api` |
| Cilium DNS/FQDN policy structure | HIGH | Official docs, multiple verified examples |
| DNS proxy companion rule requirement | HIGH | Documented in every FQDN tutorial, confirmed across multiple Cilium versions |
| Hubble L7 flow proto fields | HIGH | Official proto documentation at `api/v1/flow/flow.proto` |
| L7 visibility prerequisite (chicken-and-egg) | MEDIUM | Confirmed in docs but exact behavior with latest Cilium versions needs cluster testing |
| `flow.DestinationNames` availability | MEDIUM | Referenced in Hubble docs and issues, but depends on DNS proxy being enabled |
| kubectl dry-run=server for CiliumNetworkPolicy | HIGH | Standard K8s API, works with any CRD |

## Sources

- [Cilium L7 Policy Language](https://docs.cilium.io/en/stable/security/policy/language/) -- Official L7 rule syntax reference
- [Cilium DNS-Based Policies](https://docs.cilium.io/en/stable/security/dns/) -- FQDN policy deep dive with DNS proxy requirement
- [Cilium L7 Protocol Visibility](https://docs.cilium.io/en/stable/observability/visibility/) -- L7 visibility prerequisites
- [Cilium L7 HTTP Example](https://github.com/cilium/cilium/blob/main/examples/policies/l7/http/http.yaml) -- Official HTTP L7 policy example
- [Hubble Flow Proto](https://docs.cilium.io/en/stable/_api/v1/flow/README/) -- L7 flow message structure
- [Cilium Policy API Go Types](https://pkg.go.dev/github.com/cilium/cilium/pkg/policy/api) -- PortRuleHTTP, FQDNSelector, L7Rules structs
- [Hubble Flow Package](https://pkg.go.dev/github.com/cilium/cilium/api/v1/flow) -- HTTP, DNS proto message types
- [Safe Cilium Policy Management (CNCF)](https://www.cncf.io/blog/2025/11/06/safely-managing-cilium-network-policies-in-kubernetes-testing-and-simulation-techniques/) -- Audit mode, dry-run, safe apply patterns
- [K8s Server-Side Dry Run](https://kubernetes.io/blog/2019/01/14/apiserver-dry-run-and-kubectl-diff/) -- kubectl dry-run=server and diff
- [Cilium FQDN Deep Dive](https://hackmd.io/@Echo-Live/B1UOe_yr5) -- DNS proxy internals and matchPattern behavior
- [Cilium FQDN Wildcard Issue #22081](https://github.com/cilium/cilium/issues/22081) -- Wildcard matching limitations for subdomains

---
*Feature research for: CPG v1.1 L7 Policies & Auto-Apply*
*Researched: 2026-03-09*
