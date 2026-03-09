# Pitfalls Research

**Domain:** Adding L7 policy generation and auto-apply to existing L4 Cilium policy generator (cpg v1.1)
**Researched:** 2026-03-09
**Confidence:** HIGH (verified against Cilium docs, existing codebase, GitHub issues)

---

## Critical Pitfalls

### Pitfall 1: L7 Rules Trigger Envoy Proxy Redirect -- Breaks Existing Connections

**What goes wrong:**
Adding L7 HTTP rules (`rules.http`) to a `toPorts` entry causes Cilium to redirect traffic through the Envoy proxy. Existing L4-only connections on that port get RST'd because the datapath switches from eBPF-only forwarding to proxy-based forwarding. If `cpg apply` upgrades an existing L4-only policy to L4+L7, workloads experience connection drops during the transition.

**Why it happens:**
L4 rules use eBPF datapath only. L7 rules inject Envoy into the data path. The transition is not seamless -- TCP connections are reset when the redirect activates. This is documented in multiple Cilium issues (#35525, #43964, #30581).

**Consequences:**
Production outage for affected workloads during policy application. The blast radius is every active connection on the affected port.

**How to avoid:**
- Generate L7 policies as **separate policy objects** (e.g., `cpg-<workload>-l7`) rather than merging L7 rules into existing L4 policies. This prevents the merge logic from accidentally upgrading an L4 policy.
- The `cpg apply` command MUST detect L4-to-L7 transitions in diff output and warn explicitly: "L7 rules added -- will trigger Envoy proxy redirect, expect connection resets."
- Default to `--dry-run` (already planned). The diff output must highlight proxy redirect as a distinct warning.

**Warning signs:**
- `cilium monitor` shows `Redirect` events after apply.
- Hubble shows connection resets on previously-working flows.
- Application logs show sudden connection timeouts/resets.

**Phase to address:**
Both L7 generation (separate policy objects) and apply command (detect L4-to-L7 transitions in diff).

---

### Pitfall 2: L7 Rules Union Constraint -- Cannot Mix HTTP and DNS on Same PortRule

**What goes wrong:**
Cilium's `L7Rules` is a union: only one of `http`, `dns`, or `kafka` can be set per `PortRule`. If the merge logic combines an HTTP rule and a DNS rule on the same port, Cilium rejects the policy. But since CiliumNetworkPolicy CRDs lack admission webhook validation by default, `kubectl apply` succeeds and the Cilium agent silently fails to import the policy.

**Why it happens:**
The existing `mergePortRules()` in `pkg/policy/merge.go` (lines 101-131) only merges `PortProtocol` entries. It has zero awareness of the `Rules` field. Naively adding L7 rules to the merge path would combine rules without checking the L7 type constraint.

**How to avoid:**
- Merge logic must treat L7 rule type as part of the grouping key. Two port rules on the same port but different L7 types must remain separate `PortRule` entries.
- Add validation before writing: reject policies where the same port has conflicting L7 types.
- Practically: HTTP rules go on application ports (80, 8080, 443), DNS rules go on port 53. Keep the builder aware of this separation.

**Warning signs:**
- Cilium agent logs show policy import errors but kubectl reported success.
- Policies appear to have no effect despite being applied.

**Phase to address:**
L7 builder implementation. The `PortRule` builder must handle `Rules` field separately from `Ports` field.

---

### Pitfall 3: DNS FQDN Policies Require a Companion DNS Allow Rule

**What goes wrong:**
Generating a `toFQDNs` egress rule without also generating (or verifying existence of) an egress rule allowing DNS queries to kube-dns on port 53 with `rules.dns` inspection. Without the DNS allow rule, the pod cannot resolve the FQDN, and the `toFQDNs` rule never activates -- all egress traffic to that domain is silently dropped.

**Why it happens:**
`toFQDNs` works by intercepting DNS responses via Cilium's in-agent DNS proxy. If DNS traffic itself is blocked (because no rule allows it), the proxy never sees the resolution, and the FQDN-to-IP mapping is never populated. This is documented in Cilium's DNS policy docs but easy to miss.

**Consequences:**
Every `toFQDNs` policy silently fails. Traffic appears dropped with no obvious cause. Extremely confusing to debug.

**How to avoid:**
- When generating a `toFQDNs` egress rule, cpg MUST also generate a companion egress rule allowing DNS to kube-dns:
  ```yaml
  - toEndpoints:
    - matchLabels:
        k8s:io.kubernetes.pod.namespace: kube-system
        k8s:io.cilium.k8s.policy.serviceaccount: coredns
    toPorts:
    - ports:
      - port: "53"
        protocol: ANY
      rules:
        dns:
        - matchPattern: "*"
  ```
- Alternatively, check if a cluster-wide DNS allow policy already exists and skip generation.
- Log a warning if generating FQDN rules without a DNS companion rule.

**Warning signs:**
- Egress traffic to FQDN targets is dropped despite policy existing.
- `hubble observe` shows DNS queries being dropped.

**Phase to address:**
L7 DNS policy generation. Hard requirement, not optional.

---

### Pitfall 4: Auto-Apply Without Drift Detection -- Overwrites Manual Edits

**What goes wrong:**
`cpg apply --force` writes policies to the cluster. If a human has edited a cpg-generated policy in the cluster (added rules, tuned selectors), `cpg apply` would overwrite their changes with the generated version.

**Why it happens:**
The tool generates policies labeled `managed-by: cpg`. But there is no mechanism to detect that the cluster version has diverged from what cpg last applied.

**Consequences:**
Loss of manually-added rules. Potential production traffic disruption in a default-deny environment.

**How to avoid:**
- `cpg apply` MUST compare the cluster version against the local version and refuse to overwrite if the cluster version has changes not in the generated policy (drift detection).
- Store a generation hash annotation on applied policies (e.g., `cpg.io/spec-hash: sha256:...`) so drift detection is cheap: if cluster policy hash differs from what cpg last wrote, someone modified it.
- Never overwrite policies not labeled `managed-by: cpg`.
- Default `--dry-run` shows diff between local and cluster version.

**Warning signs:**
- Diff between local generated policy and cluster policy shows unexpected differences.
- Users report "my manual fixes keep disappearing."

**Phase to address:**
Apply command implementation. Drift detection is mandatory before `--force` works.

---

## Moderate Pitfalls

### Pitfall 5: L7 Flows Require Visibility Enablement -- Chicken-and-Egg Problem

**What goes wrong:**
Hubble only reports L7 flow details (HTTP method/path, DNS query) when L7 visibility is enabled via either (a) an existing L7 CiliumNetworkPolicy or (b) the `policy.cilium.io/proxy-visibility` annotation on the pod. Without either, Hubble flows contain only L3/L4 data -- the `L7` field is nil. Running `cpg generate` with L7 mode produces nothing.

**Why it happens:**
Cilium does not inject the Envoy proxy into the data path unless there is a reason to. No proxy = no L7 data.

**How to avoid:**
- Document clearly that L7 generation requires pre-existing visibility annotations or L7 policies.
- `cpg generate` should detect when flows lack L7 data and log a warning: "No L7 data in flows. Enable visibility via pod annotation `policy.cilium.io/proxy-visibility`."
- Consider a `--enable-visibility` flag that applies the proxy-visibility annotation to target pods before starting observation.
- The current code checks `f.L4 == nil` in `BuildPolicy` (builder.go line 81). L7 flows have L4 set but also have `f.L7` populated. The builder must check `f.L7 != nil` separately for L7 rule generation.

**Phase to address:**
L7 generation. This is a prerequisite -- without visibility, L7 generation produces nothing. Must document clearly.

---

### Pitfall 6: MergePolicy Is Blind to L7 Rules

**What goes wrong:**
The existing `MergePolicy` in `pkg/policy/merge.go` matches rules by `FromEndpoints`/`ToEndpoints` and merges `ToPorts` by deduplicating `PortProtocol` entries. It completely ignores the `Rules` field within `PortRule`. Merging an L7 policy into an existing L4 policy silently drops the L7 rules.

**Why it happens:**
`mergePortRules()` (merge.go lines 101-131) operates on `PortProtocol` slices and never reads `PortRule.Rules`. The code was written for L4 only.

**How to avoid:**
- Extend `mergePortRules` to also merge the `Rules` field when port+protocol match.
- When merging: if existing has no L7 rules but incoming does, add them. If both have L7 rules of the same type, union them. If types conflict, keep as separate `PortRule` entries (union constraint from Pitfall 2).
- Add test cases for: L4+L7 merge, L7+L7 merge (same type), L7+L7 merge (type conflict).

**Phase to address:**
L7 builder phase. Must be done before L7 generation can work with existing policy files on disk.

---

### Pitfall 7: HTTP Path Explosion -- Overly Specific Rules

**What goes wrong:**
HTTP flows contain full request URLs with path parameters (e.g., `/api/users/12345`, `/api/users/67890`). Generating one L7 HTTP rule per observed path creates policies with hundreds of rules that are unmaintainable and hit Envoy performance limits.

**Why it happens:**
Hubble reports the exact URL from each request via the `L7.Http.Url` field. Without normalization, every unique path is a separate rule.

**Consequences:**
Unreadable policies. Envoy rule matching degrades. New valid paths blocked until observed.

**How to avoid:**
- Implement path normalization from day one:
  - Detect numeric segments: `/api/users/12345` -> `/api/users/[0-9]+`
  - Detect UUIDs: `/api/orders/550e8400-e29b-41d4-a716-446655440000` -> `/api/orders/[a-f0-9-]+`
- Set a maximum HTTP rules per port (e.g., 50) with a warning when exceeded.
- Consider a `--l7-path-prefix` option generating prefix-based rules (`/api/.*`) as a simpler alternative.

**Phase to address:**
L7 HTTP builder. Path normalization is not optional -- it must be there from the start.

---

### Pitfall 8: PoliciesEquivalent Does Not Normalize L7 Rules

**What goes wrong:**
The dedup logic in `pkg/policy/dedup.go` uses `PoliciesEquivalent` with `normalizeRule` to sort rules before comparison. But `normalizeRule` (dedup.go lines 51-74) only sorts `PortProtocol` slices and rule-level ordering. It does not sort L7 rules within `PortRule.Rules.HTTP` or `PortRule.Rules.DNS`. Two semantically identical policies with HTTP rules in different order are considered different, causing unnecessary rewrites every flush cycle.

**How to avoid:**
- Extend `normalizeRule` to also sort L7 rules:
  - HTTP rules: sort by `(method, path)`
  - DNS rules: sort by `(matchName, matchPattern)`
- Add test cases with L7 rules in different orders.

**Phase to address:**
L7 builder phase. Required for dedup to work correctly with L7 policies.

---

### Pitfall 9: HTTP Response Flows vs Request Flows

**What goes wrong:**
Hubble reports both HTTP request and response flows. Response flows have `L7.Type = RESPONSE` and contain the status code but NOT the method/path (those are only on the request flow). Generating policies from response flows produces empty or nonsensical HTTP rules.

**How to avoid:**
- Filter L7 HTTP flows: only process flows where `L7.Type == L7FlowType_REQUEST`.
- Skip `RESPONSE` and `SAMPLE` flow types for policy generation.
- Response flows are useful for observability (latency, error rates) but not for policy generation.

**Phase to address:**
L7 HTTP builder. Simple filter but critical to get right.

---

### Pitfall 10: DNS Query Trailing Dot Mismatch

**What goes wrong:**
Hubble DNS flows include the trailing dot in the query field (e.g., `api.github.com.`). Cilium `toFQDNs.matchName` does NOT include the trailing dot. Copying the query directly produces a policy that never matches.

**How to avoid:**
- Strip trailing dot from DNS query before generating `matchName`/`matchPattern` values.
- One-liner: `strings.TrimSuffix(query, ".")`

**Phase to address:**
L7 DNS builder. Trivial fix but silent failure if missed.

---

### Pitfall 11: Apply Command RBAC and Server-Side Apply

**What goes wrong:**
Two sub-issues:
1. `cpg apply` needs create/update on CiliumNetworkPolicy CRDs. Current `--cluster-dedup` only needs list/get. Users with read-only RBAC get cryptic errors.
2. Using client-side apply (kubectl-style) causes field management conflicts with GitOps tools (ArgoCD, Flux, Helm). Server-side apply avoids this.

**How to avoid:**
- Pre-flight RBAC check: use SelfSubjectAccessReview or dry-run create to verify permissions before attempting apply.
- Use server-side apply (`client-go` `Patch` with `ApplyPatchType`) with field manager `cpg`.
- Provide clear error messages with required RBAC.

**Phase to address:**
Apply command implementation.

---

### Pitfall 12: toFQDNs Identity Exhaustion

**What goes wrong:**
When using `toFQDNs`, every IP observed by a matching DNS lookup gets a Cilium security identity. CDN domains (e.g., `*.amazonaws.com`) can resolve to hundreds of IPs, each getting a unique identity. This can exhaust Cilium's identity space and degrade agent performance.

**How to avoid:**
- Warn users when generating `toFQDNs` rules for wildcard patterns that match broad domains.
- Prefer `matchName` (exact) over `matchPattern` (wildcard) when possible.
- Document that FQDN policies for CDN/cloud provider domains should use CIDR rules instead.

**Phase to address:**
L7 DNS builder documentation and warnings.

---

## Phase-Specific Warnings

| Phase Topic | Likely Pitfall | Mitigation |
|-------------|---------------|------------|
| L7 HTTP builder | Path explosion (P7) | Path normalization from day one |
| L7 HTTP builder | Response vs request flows (P9) | Filter on `L7FlowType_REQUEST` only |
| L7 HTTP builder | MergePolicy blind to L7 (P6) | Extend merge before generating L7 |
| L7 HTTP builder | L7 union constraint (P2) | Type-aware merge and validation |
| L7 DNS builder | Missing DNS companion rule (P3) | Auto-generate or warn |
| L7 DNS builder | Trailing dot (P10) | Strip in builder |
| L7 DNS builder | Identity exhaustion (P12) | Warn on wildcard FQDN patterns |
| L7 visibility | Chicken-and-egg (P5) | Document prerequisite, add detection |
| Dedup | PoliciesEquivalent ignores L7 order (P8) | Extend `normalizeRule` |
| Apply command | Envoy redirect disruption (P1) | Warn on L4-to-L7 transitions in diff |
| Apply command | No rollback / drift (P4) | Drift detection with spec-hash annotation |
| Apply command | RBAC + SSA (P11) | Pre-flight check, server-side apply |

## Integration Gotchas Specific to L7 + Apply

| Integration Point | Mistake | Correct Approach |
|-------------------|---------|------------------|
| `BuildPolicy` + L7 flows | Checking only `f.L4 == nil` to skip flows | Also check `f.L7 != nil` to route to L7 builder |
| `mergePortRules` + L7 | Merging only `PortProtocol`, ignoring `Rules` | Merge `Rules` field with type-awareness |
| `PoliciesEquivalent` + L7 | Not sorting L7 rules in `normalizeRule` | Sort HTTP rules by (method, path), DNS by (matchName) |
| `peerKey` grouping + L7 | Same peer key for L4 and L7 rules on same port | L7 rules need port-level grouping within peer |
| Cluster dedup + L7 | Comparing L4-only cluster policy with L4+L7 generated policy | Separate L7 policies avoid this entirely |
| Writer + L7 | File naming assumes one policy per workload | L7 policies may need separate files (e.g., `<workload>-l7.yaml`) |
| Apply + existing L4 | Blindly applying L7 policy triggers Envoy redirect | Detect and warn on proxy redirect transitions |
| DNS builder + kube-dns | Generating `toFQDNs` without DNS companion rule | Always generate or verify DNS allow rule |

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| L7 rules trigger Envoy redirect (P1) | HIGH | Delete L7 policy to revert to L4-only. Existing connections recover. |
| L7 union conflict (P2) | LOW | Fix generated policy structure, re-apply. Old invalid policy had no effect. |
| Missing DNS companion (P3) | LOW | Add DNS allow rule. FQDN policy starts working immediately. |
| Drift overwrite (P4) | HIGH | Must manually reconstruct lost rules from cluster audit logs or git history. |
| Path explosion (P7) | MEDIUM | Regenerate with normalization. Delete old over-specific policies. |
| MergePolicy drops L7 (P6) | MEDIUM | Regenerate from scratch. Existing policies on disk may have lost L7 rules through merge cycles. |

## Sources

- [Cilium Envoy Proxy Documentation](https://docs.cilium.io/en/stable/security/network/proxy/envoy/)
- [Cilium DNS-Based Policies Documentation](https://docs.cilium.io/en/stable/security/dns/)
- [Cilium L7 Protocol Visibility](https://docs.cilium.io/en/stable/observability/visibility/)
- [Cilium Policy Language Reference](https://docs.cilium.io/en/stable/security/policy/language/)
- [Cilium Flow Protobuf API](https://docs.cilium.io/en/stable/_api/v1/flow/README/)
- [Cilium Policy API - L7Rules Union](https://pkg.go.dev/github.com/cilium/cilium/pkg/policy/api)
- [Envoy proxy stops working with L7 policy - Issue #35525](https://github.com/cilium/cilium/issues/35525)
- [Missing L7 policy state causes TCP resets - Issue #43964](https://github.com/cilium/cilium/issues/43964)
- [L7 rules cause gateway timeout - Issue #30581](https://github.com/cilium/cilium/issues/30581)
- [CNCF: Safely Managing Cilium Network Policies](https://www.cncf.io/blog/2025/11/06/safely-managing-cilium-network-policies-in-kubernetes-testing-and-simulation-techniques/)
- [Cilium FQDN DNS proxy truncated response - Issue #31197](https://github.com/cilium/cilium/issues/31197)
- [Debug Cilium toFQDN Network Policies (Medium)](https://mcvidanagama.medium.com/debug-cilium-tofqdn-network-policies-b5c4837e3fc4)

---
*Pitfalls research for: CPG v1.1 -- L7 Policy Generation + Auto-Apply*
*Researched: 2026-03-09*
