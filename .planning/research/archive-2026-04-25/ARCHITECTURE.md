# Architecture Patterns

**Domain:** L7 policy generation and cluster apply for existing Cilium Policy Generator
**Researched:** 2026-03-09

## Recommended Architecture

### Design Principle: Extend, Don't Restructure

The existing architecture is well-factored. L7 support and `cpg apply` integrate as extensions to existing components, not rewrites. The key insight: L7 rules attach to existing `PortRule.Rules` (the `L7Rules` field), so L7 data enriches the same ingress/egress rules the builder already produces.

### Component Map (existing + new)

```
                    Hubble gRPC Stream
                          |
                          v
              +--------------------------+
              |  pkg/hubble/client       |  (existing, modify: request L7 flows)
              |  StreamDroppedFlows      |
              +------------+-------------+
                           |
                           v
              +--------------------------+
              |  pkg/hubble/aggregator   |  (existing, no changes needed)
              |  AggKey bucketing        |
              +------------+-------------+
                           |
                           v
              +--------------------------+
              |  pkg/policy/builder      |  (existing, MODIFY: add L7 rule building)
              |  BuildPolicy             |
              |  + buildL7HTTP()         |  <- NEW logic in existing file
              |  + buildL7DNS()          |  <- NEW logic in existing file
              +------------+-------------+
                           |
                           v
              +--------------------------+
              |  pkg/policy/dedup        |  (existing, MODIFY: normalize L7Rules)
              |  PoliciesEquivalent      |
              +------------+-------------+
                           |
                           v
              +--------------------------+
              |  pkg/policy/merge        |  (existing, MODIFY: merge L7Rules)
              |  MergePolicy             |
              +------------+-------------+
                           |
                           v
              +--------------------------+
              |  pkg/output/writer       |  (existing, no changes needed)
              +--------------------------+


              +--------------------------+
              |  cmd/cpg/apply.go        |  <- NEW command file
              |  newApplyCmd()           |
              +------------+-------------+
                           |
                           v
              +--------------------------+
              |  pkg/k8s/apply.go        |  <- NEW file in existing package
              |  ApplyPolicies()         |
              |  DryRunPolicies()        |
              +--------------------------+
```

### Component Boundaries

| Component | Responsibility | Status | Communicates With |
|-----------|---------------|--------|-------------------|
| `pkg/hubble/client` | gRPC stream with L7 flow types | MODIFY | aggregator (via flow channel) |
| `pkg/hubble/aggregator` | Bucket flows by namespace+workload | NO CHANGE | builder (via flush) |
| `pkg/hubble/pipeline` | Orchestrate stream-to-disk | NO CHANGE | all stages |
| `pkg/policy/builder` | Convert flows to CNP with L4+L7 rules | MODIFY | labels pkg |
| `pkg/policy/dedup` | Normalize and compare policies | MODIFY | builder output |
| `pkg/policy/merge` | Merge rules into existing policies | MODIFY | writer |
| `pkg/output/writer` | Write YAML to disk | NO CHANGE | merge, dedup |
| `pkg/k8s/apply` | Apply/dry-run policies to cluster | NEW | Cilium clientset |
| `cmd/cpg/apply.go` | CLI command for apply | NEW | k8s/apply, output/reader |

## Integration Details

### 1. L7 HTTP Flow Detection and Rule Building

**Where:** `pkg/policy/builder.go` -- modify `buildIngressRules()` and `buildEgressRules()`

**How it works:** Hubble flows with `f.L7 != nil && f.L7.Type == L7FlowType_HTTP` carry HTTP metadata. The builder must detect these flows and attach `L7Rules` to the corresponding `PortRule`.

**Flow data mapping (Hubble -> Cilium policy API):**

| Hubble `flow.L7.Http` field | Cilium `api.PortRuleHTTP` field | Notes |
|---|---|---|
| `Method` (string) | `Method` (string) | Direct mapping, e.g. "GET" |
| `Url` (string) | `Path` (string) | Extract path from URL, regex-escape |
| `Headers` (`[]*HTTPHeader`) | `HeaderMatches` (`[]HeaderMatch`) | Selective: only common auth/routing headers |

**Key design decision:** The `PortRule` struct already has a `Rules *L7Rules` field. Currently cpg creates `PortRule{Ports: []PortProtocol{...}}` with `Rules` left nil. For L7 flows, populate `Rules`:

```go
portRule := api.PortRule{
    Ports: []api.PortProtocol{{Port: port, Protocol: proto}},
    Rules: &api.L7Rules{
        HTTP: []api.PortRuleHTTP{
            {Method: http.Method, Path: extractPath(http.Url)},
        },
    },
}
```

**Integration point in existing code:** The `peerPorts` struct (used inside `buildIngressRules` and `buildEgressRules`) currently tracks `ports []api.PortProtocol` and dedup via `seen map[string]struct{}`. This needs extension:

- Change `peerPorts` to track `portRules map[string]*portAccumulator` keyed by `port/proto`
- Each accumulator collects L7 HTTP rules for that port
- Dedup key for L7 becomes `port/proto/method/path`

**Refactoring approach:** Extract a `portAccumulator` type that accumulates both L4-only and L7-enriched port rules per peer. This keeps `buildIngressRules`/`buildEgressRules` clean while adding L7 support.

### 2. L7 DNS Flow Detection and FQDN Rule Building

**Where:** `pkg/policy/builder.go` -- modify `buildEgressRules()` only (DNS visibility is egress-only in Cilium)

**How it works:** DNS flows have `f.L7 != nil && f.L7.Type == L7FlowType_DNS`. The DNS query name maps to `ToFQDNs` on the egress rule.

**Flow data mapping:**

| Hubble `flow.L7.Dns` field | Cilium policy API | Notes |
|---|---|---|
| `Query` (string) | `EgressRule.ToFQDNs` with `FQDNSelector{MatchName: query}` | Strip trailing dot |
| `Query` (string) | `PortRule.Rules.DNS` with `PortRuleDNS{MatchPattern: query}` | L7 DNS visibility rule on port 53 |

**Critical constraint from Cilium:** `ToFQDNs` cannot coexist with `ToEndpoints`, `ToCIDR`, or other `To*` fields in the same `EgressRule`. This means DNS-based egress rules must be separate `EgressRule` entries, not merged with existing peer-based rules.

**Design:** Create a separate code path in `buildEgressRules()` for DNS flows:

```go
// DNS flows produce two things:
// 1. An EgressRule with ToFQDNs (the actual FQDN allow rule)
// 2. An EgressRule allowing DNS traffic to kube-dns on port 53/UDP
//    with L7 DNS rules for the specific query
```

**Important:** The FQDN egress rule also needs a corresponding DNS allow rule (port 53/UDP to kube-dns) with L7 DNS rules. Without this, Cilium cannot resolve the FQDN to learn IP-to-FQDN mappings. The builder should auto-generate both.

### 3. Hubble Client Modifications

**Where:** `pkg/hubble/client.go` -- modify `StreamDroppedFlows()`

**What changes:** The current Hubble observe request filters for dropped flows. For L7 visibility:

- L7 flows appear as `DROPPED` or `REDIRECTED` verdict depending on Cilium config
- The flow filter may need to include `flowpb.Verdict_REDIRECTED` or adjust type filters
- L7 data is already present in the `Flow` protobuf; no gRPC API changes needed
- Consider adding a `--l7` flag to explicitly opt-in to L7 flow processing (L7 visibility requires Cilium pod annotations or CiliumNetworkPolicy with L7 rules already in place)

**No aggregator changes:** The aggregator buckets by `AggKey{Namespace, Workload}` extracted from flow endpoints. L7 flows carry the same endpoint information, so they naturally bucket with their L4 counterparts. This is correct behavior: a single workload's policy should contain both L4 and L7 rules.

### 4. Dedup and Merge Extensions

**Where:** `pkg/policy/dedup.go` and `pkg/policy/merge.go`

**Dedup (`PoliciesEquivalent`):**
- Currently normalizes and compares `Ingress`/`Egress` rules via `reflect.DeepEqual`
- L7Rules are nested inside `PortRule.Rules`, so `reflect.DeepEqual` already captures them
- However, `normalizeRule()` must sort L7 rules within `PortRule.Rules.HTTP` and `PortRule.Rules.DNS` for deterministic comparison
- Add sorting of `L7Rules.HTTP` entries by method+path and `L7Rules.DNS` entries by matchPattern
- Also normalize `ToFQDNs` entries (sort by matchName/matchPattern)

**Merge (`MergePolicy`):**
- Currently matches peers by endpoints, then merges `ToPorts` via `mergePortRules()`
- `mergePortRules()` deduplicates by `port/protocol` string key but ignores `Rules`
- Must extend to merge `L7Rules` when port+protocol match: append new HTTP rules (dedup by method+path), append new DNS rules (dedup by matchPattern)
- For FQDN rules: match egress rules by `ToFQDNs` equality, merge associated port rules
- Add `matchFQDNs()` alongside existing `matchEndpoints()` for FQDN-based rule matching

### 5. `cpg apply` Command

**Where:** `cmd/cpg/apply.go` (new) + `pkg/k8s/apply.go` (new)

**Design:**

```
cpg apply [--dir ./policies] [--namespace production] [--force] [--kubeconfig path]
```

- Default behavior: **dry-run** -- read YAML files from output dir, diff against cluster state, show what would change
- `--force`: actually apply (create or update) policies to the cluster
- Uses the existing `pkg/k8s` package's kubeconfig loading and Cilium clientset

**`pkg/k8s/apply.go` responsibilities:**
1. Walk the output directory, read all YAML files, unmarshal to `CiliumNetworkPolicy`
2. For each policy, check if it exists in cluster (by namespace+name)
3. If exists: compare specs (reuse `PoliciesEquivalent`), update if different
4. If not exists: create
5. Return a summary of actions (created, updated, unchanged, errors)

**Dry-run implementation:** Use Kubernetes server-side dry-run (`metav1.CreateOptions{DryRun: []string{"All"}}`) rather than client-side simulation. This validates against admission webhooks and CRD validation. Fall back to client-side diff display if server-side dry-run is not available.

**Apply implementation:** Use the Cilium clientset's `CiliumNetworkPolicies(namespace).Create()` and `.Update()` methods. These already exist in the `ciliumclient` package that `cluster_dedup.go` imports.

**Safety features:**
- Require `managed-by: cpg` label on all policies (already set by builder)
- Refuse to update policies without `managed-by: cpg` label (prevent overwriting manually-created policies)
- Show diff before apply with `--force`
- Count and report: created/updated/skipped/errors

### Data Flow Changes

**Current flow (L4 only):**

```
gRPC stream -> Flow{L4: TCP/UDP} -> Aggregator -> BuildPolicy(L4 ports) -> Write YAML
```

**New flow (L4 + L7):**

```
gRPC stream -> Flow{L4 + L7: HTTP/DNS} -> Aggregator -> BuildPolicy(L4 ports + L7 rules) -> Write YAML
                                                                                                  |
                                                                                                  v
                                                                              cpg apply -> Read YAML -> Apply to cluster
```

The pipeline stays unchanged. L7 data flows through the same channel, same aggregator, same builder. The builder produces richer `PortRule` entries with `Rules` populated when L7 data is present.

## Patterns to Follow

### Pattern 1: L7 Rule Extraction Functions

**What:** Separate functions to extract L7 rules from flows, parallel to existing `extractPort()`

**When:** Processing flows with L7 data in the builder

**Example:**

```go
// extractHTTPRule extracts an L7 HTTP rule from a flow's Layer7 data.
// Returns nil if the flow has no HTTP L7 information.
func extractHTTPRule(f *flowpb.Flow) *api.PortRuleHTTP {
    if f.L7 == nil || f.L7.Http == nil {
        return nil
    }
    h := f.L7.Http
    rule := &api.PortRuleHTTP{}
    if h.Method != "" {
        rule.Method = h.Method
    }
    if h.Url != "" {
        rule.Path = extractPathFromURL(h.Url)
    }
    return rule
}

// extractDNSQuery extracts the DNS query name from a flow's Layer7 data.
// Returns empty string if the flow has no DNS information or is a response.
func extractDNSQuery(f *flowpb.Flow) string {
    if f.L7 == nil || f.L7.Dns == nil {
        return ""
    }
    // Only process DNS requests, not responses
    if f.L7.Type != flowpb.L7FlowType_REQUEST {
        return ""
    }
    return strings.TrimSuffix(f.L7.Dns.Query, ".")
}
```

### Pattern 2: Port Rule Accumulator

**What:** Replace bare `[]PortProtocol` tracking with a structure that accumulates both L4 and L7 rules per port

**When:** Building ingress/egress rules in the builder

**Example:**

```go
// portAccumulator groups L7 rules under a single port+protocol combination.
type portAccumulator struct {
    port      string
    protocol  api.L4Proto
    httpRules []api.PortRuleHTTP
    httpSeen  map[string]struct{} // dedup by method+path
}

// toPortRule produces the final PortRule with optional L7Rules attached.
func (pa *portAccumulator) toPortRule() api.PortRule {
    pr := api.PortRule{
        Ports: []api.PortProtocol{{Port: pa.port, Protocol: pa.protocol}},
    }
    if len(pa.httpRules) > 0 {
        pr.Rules = &api.L7Rules{HTTP: pa.httpRules}
    }
    return pr
}
```

### Pattern 3: Separate FQDN Egress Rules

**What:** DNS flows produce isolated `EgressRule` entries with `ToFQDNs` since these cannot be combined with other `To*` fields

**When:** Building egress rules from DNS L7 flows

**Example:**

```go
// buildFQDNEgressRules produces EgressRules for DNS-observed FQDNs.
// Each FQDN gets its own rule because ToFQDNs cannot coexist with
// ToEndpoints or ToCIDR in the same EgressRule.
func buildFQDNEgressRules(dnsFlows []*flowpb.Flow) []api.EgressRule {
    fqdns := make(map[string]struct{})
    for _, f := range dnsFlows {
        query := extractDNSQuery(f)
        if query == "" {
            continue
        }
        fqdns[query] = struct{}{}
    }

    var rules []api.EgressRule
    for fqdn := range fqdns {
        rules = append(rules, api.EgressRule{
            EgressCommonRule: api.EgressCommonRule{
                ToFQDNs: api.FQDNSelectorSlice{
                    {MatchName: fqdn},
                },
            },
            ToPorts: api.PortRules{{
                Ports: []api.PortProtocol{{Port: "443", Protocol: api.ProtoTCP}},
            }},
        })
    }
    return rules
}
```

### Pattern 4: Apply with Safety Guards

**What:** Policy application checks `managed-by` label before mutations

**When:** `cpg apply` creates/updates cluster policies

```go
func applyPolicy(ctx context.Context, client ciliumclient.Interface,
    policy *ciliumv2.CiliumNetworkPolicy, force bool) (action string, err error) {

    existing, err := client.CiliumV2().CiliumNetworkPolicies(policy.Namespace).
        Get(ctx, policy.Name, metav1.GetOptions{})
    if err != nil {
        if apierrors.IsNotFound(err) {
            if !force {
                return "would-create", nil
            }
            _, err = client.CiliumV2().CiliumNetworkPolicies(policy.Namespace).
                Create(ctx, policy, metav1.CreateOptions{})
            return "created", err
        }
        return "", err
    }

    // Safety: refuse to overwrite non-cpg policies
    if existing.Labels["app.kubernetes.io/managed-by"] != "cpg" {
        return "skipped-not-managed", nil
    }

    if PoliciesEquivalent(existing, policy) {
        return "unchanged", nil
    }

    if !force {
        return "would-update", nil
    }
    policy.ResourceVersion = existing.ResourceVersion
    _, err = client.CiliumV2().CiliumNetworkPolicies(policy.Namespace).
        Update(ctx, policy, metav1.UpdateOptions{})
    return "updated", err
}
```

## Anti-Patterns to Avoid

### Anti-Pattern 1: Mixing L4-only and L7 PortRules for Same Port

**What:** Creating separate `PortRule` entries for the same port (one L4-only, one with L7 rules)

**Why bad:** Cilium treats each `PortRule` independently. Two rules for port 80 -- one allowing all traffic and one restricting to GET /api -- means the L4-only rule allows everything, making the L7 rule useless.

**Instead:** When L7 data exists for a port, always attach L7 rules to the same `PortRule`. If some flows for a port have L7 data and others don't, only generate L7 rules (the restrictive case) or fall back to L4-only (the permissive case). Choose L4-only as default since L7 visibility may not be enabled for all pods.

### Anti-Pattern 2: Generating FQDN Rules Without DNS Allow Rules

**What:** Creating `ToFQDNs` rules without corresponding DNS port 53 allow rules

**Why bad:** Cilium requires DNS traffic to be allowed so it can intercept DNS responses to learn IP-to-FQDN mappings. Without DNS allow rules, FQDN-based policies silently fail.

**Instead:** Always pair FQDN rules with a DNS allow rule (port 53/UDP to `kube-dns` endpoint).

### Anti-Pattern 3: Applying Without Ownership Checks

**What:** Blindly updating any CiliumNetworkPolicy found in the cluster

**Why bad:** Overwrites manually-crafted or operator-managed policies, causing outages.

**Instead:** Only touch policies with `app.kubernetes.io/managed-by: cpg` label. Skip anything without it.

### Anti-Pattern 4: Client-Side Dry-Run Only

**What:** Implementing dry-run as pure diff display without server validation

**Why bad:** Misses CRD validation errors, admission webhook rejections, RBAC denials. User applies with `--force` and gets unexpected failures.

**Instead:** Use server-side dry-run (`DryRun: []string{"All"}` in create/update options) to validate the policy would be accepted, then display the diff.

### Anti-Pattern 5: Path Literal Explosion

**What:** Generating separate HTTP rules for `/api/users/1`, `/api/users/2`, `/api/users/3`...

**Why bad:** Produces policies with hundreds of path entries. Cilium compiles these to envoy config, bloating proxy memory.

**Instead:** Consider path generalization -- replace numeric path segments with regex patterns like `[0-9]+`. Implement as an optional `--generalize-paths` flag.

## Suggested Build Order

The build order follows dependency chains and enables incremental testing:

### Phase 1: L7 HTTP Rules (builder core)

1. **Add `extractHTTPRule()` and `extractPathFromURL()`** in `builder.go`
   - Pure functions, easily unit tested
   - No changes to existing code yet

2. **Refactor `peerPorts` to support L7 rules**
   - Extend the internal structs in `buildIngressRules`/`buildEgressRules`
   - Change from `ports []api.PortProtocol` to `portRules map[string]*portAccumulator`
   - Existing L4-only behavior must remain identical (regression tests)

3. **Wire L7 HTTP detection into flow processing loop**
   - Check `f.L7 != nil && f.L7.Http != nil` in the flow iteration
   - Attach `L7Rules` to matching `PortRule`

4. **Extend dedup normalization** for L7 rules
   - Sort HTTP rules in `normalizeRule()` by method+path

5. **Extend merge** for L7-enriched port rules
   - `mergePortRules()` must handle `Rules` field merging

### Phase 2: L7 DNS / FQDN Rules

6. **Add `extractDNSQuery()`** in `builder.go`

7. **Add `buildFQDNEgressRules()`** as separate builder function
   - Produces isolated `EgressRule` with `ToFQDNs`
   - Auto-generates companion DNS allow rule for kube-dns

8. **Wire into `BuildPolicy()`** -- separate DNS flows from L4 flows in the egress path

9. **Extend dedup/merge** for FQDN rules
   - Normalize `ToFQDNs` sorting
   - Add `matchFQDNs()` for merge matching
   - Merge FQDN selectors

### Phase 3: `cpg apply` Command

10. **Add `pkg/k8s/apply.go`** -- core apply/dry-run logic
    - Read YAML files from directory
    - Compare with cluster state via `PoliciesEquivalent`
    - Create/Update with safety guards and ownership checks

11. **Add `cmd/cpg/apply.go`** -- cobra command wiring
    - Flags: `--dir`, `--namespace`, `--force`
    - Default dry-run behavior
    - Wire to `main.go`

12. **Register apply command** in `cmd/cpg/main.go`

### Phase 4: Hubble Client Verification

13. **Verify/modify `StreamDroppedFlows()`** filter
    - Ensure L7 flows are not filtered out by verdict filter
    - May need to add `flowpb.Verdict_REDIRECTED` or adjust flow type filters
    - This is last because it requires a live cluster to validate

## Scalability Considerations

| Concern | At 100 policies | At 1K policies | At 10K policies |
|---------|-----------------|----------------|-----------------|
| Apply speed | Sequential fine | Batch with concurrency (10 workers) | Batch + rate limiting |
| YAML reading | `os.ReadDir` fine | Still fine | Consider streaming |
| L7 rule cardinality | Low | HTTP paths can explode | Need path generalization (regex patterns) |
| FQDN count | Low | Moderate | May hit Cilium DNS proxy limits |

**L7 rule explosion risk:** HTTP paths like `/api/users/123` and `/api/users/456` should not produce separate rules. Consider path generalization (replace numeric segments with regex `[0-9]+`). This is a post-MVP enhancement, not blocking for initial implementation. Flag for validation during development.

## Sources

- [Cilium policy API - pkg.go.dev](https://pkg.go.dev/github.com/cilium/cilium/pkg/policy/api) - HIGH confidence (official Go docs, v1.19.1)
- [Hubble Flow protobuf - Cilium docs](https://docs.cilium.io/en/stable/_api/v1/flow/README/) - HIGH confidence (official protocol docs)
- [Cilium L7 HTTP policy example](https://github.com/cilium/cilium/blob/main/examples/policies/l7/http/http.yaml) - HIGH confidence (official examples)
- [Cilium L7 visibility docs](https://docs.cilium.io/en/stable/observability/visibility/) - HIGH confidence (official docs)
- Existing codebase analysis (`pkg/policy/builder.go`, `pkg/policy/merge.go`, `pkg/policy/dedup.go`, `pkg/hubble/pipeline.go`, `pkg/k8s/cluster_dedup.go`) - HIGH confidence (direct code reading)
