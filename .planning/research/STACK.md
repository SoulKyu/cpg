# Stack Research — v1.1 L7 Policies & Apply Command

**Domain:** Go CLI tool — Extending CPG with L7 CiliumNetworkPolicy generation and safe cluster apply
**Researched:** 2026-03-09
**Confidence:** HIGH

## Scope

This research covers ONLY the stack additions/changes needed for v1.1 features:
1. L7 HTTP policy generation (method, path, headers)
2. L7 DNS policy generation (FQDN matchPattern)
3. `cpg apply` command (dry-run by default, `--force` to apply)

The existing stack (Go 1.25, Cilium v1.19.1, cobra, zap, client-go v0.35, grpc v1.79) is validated and NOT re-researched.

## Key Finding: No New Dependencies Required

All three features are achievable with types and APIs **already present** in the existing `go.mod`. No new `go get` needed.

---

## L7 Policy Generation: Cilium API Types Already Available

### Types in `github.com/cilium/cilium/pkg/policy/api` (v1.19.1)

The existing `api.PortRule` struct already has a `Rules *L7Rules` field that the current codebase does not use. This is the integration point.

**Current code** builds `api.PortRule{Ports: []api.PortProtocol{...}}` without setting `Rules`.
**v1.1** will set `Rules: &api.L7Rules{HTTP: [...]}` or `Rules: &api.L7Rules{DNS: [...]}` when L7 flow data is present.

#### PortRule (existing, partially used)

```go
// Already imported as: "github.com/cilium/cilium/pkg/policy/api"
type PortRule struct {
    Ports []PortProtocol `json:"ports,omitempty"`      // <-- v1.0 uses this
    Rules *L7Rules       `json:"rules,omitempty"`       // <-- v1.1 will use this
}
```

#### L7Rules (new usage, same import)

```go
type L7Rules struct {
    HTTP  PortRulesHTTP  `json:"http,omitempty"`   // For HTTP method/path/header rules
    Kafka PortRulesL7    `json:"kafka,omitempty"`  // Out of scope for v1.1
    GRPC  PortRulesL7    `json:"grpc,omitempty"`   // Out of scope for v1.1
    DNS   PortRulesDNS   `json:"dns,omitempty"`    // For DNS FQDN matching
}
```

#### PortRuleHTTP (new usage, same import)

```go
type PortRuleHTTP struct {
    Path    string        `json:"path,omitempty"`     // Extended POSIX regex
    Method  string        `json:"method,omitempty"`   // Extended POSIX regex
    Host    string        `json:"host,omitempty"`     // Extended POSIX regex
    Headers []HeaderMatch `json:"headers,omitempty"`  // Key-value header matching
}
```

#### PortRuleDNS (new usage, same import)

```go
type PortRuleDNS struct {
    MatchName    string `json:"matchName,omitempty"`    // Literal DNS name
    MatchPattern string `json:"matchPattern,omitempty"` // Wildcard pattern (e.g. "*.example.com")
}
```

#### FQDNSelector / ToFQDNs (new usage for egress DNS, same import)

```go
type EgressRule struct {
    // ... existing fields ...
    ToFQDNs FQDNSelectorSlice `json:"toFQDNs,omitempty"`  // DNS-based egress allow
}

type FQDNSelector struct {
    MatchName    string `json:"matchName,omitempty"`    // Literal: "api.example.com"
    MatchPattern string `json:"matchPattern,omitempty"` // Wildcard: "*.example.com"
}
```

**CRITICAL constraint:** `ToFQDNs` cannot coexist with other `To*` fields (`ToEndpoints`, `ToCIDR`, etc.) in the same `EgressRule`. DNS egress rules must be separate `EgressRule` entries.

**Confidence:** HIGH -- Verified against [pkg.go.dev/github.com/cilium/cilium@v1.19.1/pkg/policy/api](https://pkg.go.dev/github.com/cilium/cilium@v1.19.1/pkg/policy/api)

---

## L7 Flow Data: Hubble Proto Types Already Available

### Types in `github.com/cilium/cilium/api/v1/flow` (v1.19.1)

The existing `flowpb.Flow` has a `.L7` field that the current codebase ignores. This is the data source.

#### Flow.L7 field (existing, not yet consumed)

```go
// Already imported as: flowpb "github.com/cilium/cilium/api/v1/flow"
type Flow struct {
    // ... existing fields used by v1.0 (Source, Destination, L4, TrafficDirection, Verdict) ...
    L7        *Layer7       // <-- v1.1 will read this
    EventType *CiliumEventType
}
```

#### Layer7 (new usage, same import)

```go
type Layer7 struct {
    Type      L7FlowType  // L7_UNKNOWN=0, L7_HTTP=1, L7_DNS=2, L7_KAFKA=3
    LatencyNs uint64
    // oneof record:
    //   GetDns()   *DNS
    //   GetHttp()  *HTTP
    //   GetKafka() *Kafka
}
```

#### HTTP (for L7 HTTP flows)

```go
type HTTP struct {
    Code     uint32       // Response status code (0 for requests)
    Method   string       // GET, POST, PUT, DELETE, etc.
    Url      string       // Full URL path
    Protocol string       // HTTP/1.1, HTTP/2
    Headers  []*HTTPHeader
}

type HTTPHeader struct {
    Key   string
    Value string
}
```

#### DNS (for L7 DNS flows)

```go
type DNS struct {
    Query             string   // DNS name queried (e.g. "api.example.com.")
    Ips               []string // Resolved IPs
    Ttl               uint32
    Cnames            []string
    Rcode             uint32   // DNS return code (0=NOERROR)
    Qtypes            []string // Query types (A, AAAA, CNAME, etc.)
    Rrtypes           []string // Resource record types
    ObservationSource string
}
```

#### L7FlowType enum

```go
const (
    L7FlowType_UNKNOWN  L7FlowType = 0
    L7FlowType_REQUEST  L7FlowType = 1
    L7FlowType_RESPONSE L7FlowType = 2
    L7FlowType_SAMPLE   L7FlowType = 3
)
```

**Key distinction:** `L7FlowType` (REQUEST/RESPONSE/SAMPLE) is NOT the same as the protocol type. The protocol type is determined by which getter returns non-nil: `GetHttp()`, `GetDns()`, `GetKafka()`.

**Confidence:** HIGH -- Verified against [pkg.go.dev/github.com/cilium/cilium@v1.19.1/api/v1/flow](https://pkg.go.dev/github.com/cilium/cilium@v1.19.1/api/v1/flow)

---

## Hubble Filter Changes for L7

### Current filter (v1.0) -- DROPPED verdict only

```go
// pkg/hubble/client.go buildFilters()
{Verdict: []flowpb.Verdict{flowpb.Verdict_DROPPED}}
```

### Required change for L7

L7 flows have `Verdict_FORWARDED` (not DROPPED) because they pass L3/L4 and are observed by the proxy. L7 **denials** show as `Verdict_DROPPED` but with an L7 field populated. The current DROPPED filter will catch L7 denials.

However, for DNS FQDN policy generation, we need DNS **responses** (FORWARDED verdict) to extract the queried domain names. Two approaches:

1. **Add a second filter for DNS flows** with `Verdict_FORWARDED` + `EventType` for L7/DNS -- this pulls allowed DNS queries to learn which FQDNs are being accessed
2. **Stick with DROPPED only** -- only generate DNS policies for denied DNS queries

**Recommendation:** Option 2 (DROPPED only) for v1.1. It matches the tool's core value proposition ("generate policies from denials"). Option 1 requires filtering a much larger flow volume and changes the tool's semantics. Can be added in v1.2 as an `--include-forwarded` flag.

For HTTP, DROPPED verdict already works because HTTP denials from L7 policy violations produce dropped flows with L7.HTTP populated.

**Confidence:** MEDIUM -- Based on Cilium L7 visibility documentation and Hubble flow semantics. Verify with a real cluster that L7 DROPPED flows contain the `.L7` field.

---

## Apply Command: client-go Dynamic Client + SSA

### Approach: Dynamic Client with Server-Side Apply

Use `k8s.io/client-go/dynamic` (already a transitive dependency) for applying CiliumNetworkPolicy CRDs. The dynamic client works with `unstructured.Unstructured` objects and supports any CRD without generated typed clients.

**Why dynamic client over typed client:**
- Cilium does NOT ship a generated clientset in `cilium/cilium` v1.19.1 that is importable as a library
- Building a typed client from the CRD would require code-gen tooling and maintenance
- Dynamic client with `unstructured.Unstructured` is the standard approach for CRD operations from external tools
- kubectl itself uses dynamic client for CRD apply

### Key types and functions (all in existing dependencies)

```go
import (
    "k8s.io/client-go/dynamic"                           // dynamic.NewForConfig()
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"   // unstructured.Unstructured
    "k8s.io/apimachinery/pkg/runtime"                     // runtime.DefaultUnstructuredConverter
    "k8s.io/apimachinery/pkg/runtime/schema"              // schema.GroupVersionResource
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"         // metav1.ApplyOptions
)
```

### GVR for CiliumNetworkPolicy

```go
var ciliumNetworkPolicyGVR = schema.GroupVersionResource{
    Group:    "cilium.io",
    Version:  "v2",
    Resource: "ciliumnetworkpolicies",
}
```

### Apply with dry-run pattern

```go
// Convert typed CNP to unstructured
unstructuredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cnp)

// Apply with server-side apply
result, err := dynamicClient.
    Resource(ciliumNetworkPolicyGVR).
    Namespace(namespace).
    Apply(ctx, name, &unstructured.Unstructured{Object: unstructuredObj},
        metav1.ApplyOptions{
            FieldManager: "cpg",
            DryRun:       []string{metav1.DryRunAll},  // "All" triggers server-side dry-run
        },
    )
```

### Dry-run semantics

- `metav1.DryRunAll` = `"All"` -- tells the API server to execute admission, defaulting, and validation but NOT persist
- `metav1.ApplyOptions.DryRun: []string{metav1.DryRunAll}` for dry-run mode
- `metav1.ApplyOptions.DryRun: nil` for real apply (`--force` flag)
- `FieldManager: "cpg"` identifies this tool as the field owner for SSA conflict resolution

### Alternative: Patch with ApplyPatchType

If `Apply()` method is not available on the dynamic client version, fall back to `Patch()`:

```go
data, _ := json.Marshal(unstructuredObj)
result, err := dynamicClient.
    Resource(ciliumNetworkPolicyGVR).
    Namespace(namespace).
    Patch(ctx, name, types.ApplyPatchType, data,
        metav1.PatchOptions{
            FieldManager: "cpg",
            DryRun:       []string{metav1.DryRunAll},
        },
    )
```

`types.ApplyPatchType` = `"application/apply-patch+yaml"` -- the SSA patch type.

**Recommendation:** Use the `Apply()` method directly. It is available on `dynamic.ResourceInterface` in client-go v0.35.x. Fall back to `Patch()` only if testing reveals issues.

**Confidence:** HIGH -- `dynamic.ResourceInterface.Apply()` verified on [pkg.go.dev/k8s.io/client-go@v0.35.0/dynamic](https://pkg.go.dev/k8s.io/client-go@v0.35.0/dynamic)

---

## Integration Points with Existing Codebase

### pkg/policy/builder.go

**Change:** Add L7 rule extraction alongside existing L4 port extraction.

```
Current flow: extractPort(f) -> PortProtocol{Port, Protocol}
New flow:     extractPort(f) -> PortProtocol + extractL7Rules(f) -> *L7Rules
```

When `f.L7 != nil`:
- If `f.L7.GetHttp() != nil`: build `PortRuleHTTP{Method, Path}` from HTTP fields
- If `f.L7.GetDns() != nil`: build `PortRuleDNS{MatchName}` from DNS query field
- Attach to `PortRule.Rules` field

### pkg/policy/merge.go

**Change:** Extend `mergePortRules()` to handle L7 rules dedup.

Current merge deduplicates by `Port/Protocol`. Must also deduplicate L7 rules within the same `PortRule.Rules`. HTTP rules dedup by `Method+Path`, DNS rules dedup by `MatchName+MatchPattern`.

### pkg/hubble/aggregator.go

**Change:** No structural changes needed. L7 data is embedded in `flowpb.Flow` objects that already flow through the aggregator. The `BuildPolicy` call in `flush()` will automatically pick up L7 data.

### pkg/k8s/client.go

**Change:** Add `NewDynamicClient()` factory function using existing `LoadKubeConfig()`.

```go
func NewDynamicClient() (dynamic.Interface, error) {
    cfg, err := LoadKubeConfig()
    if err != nil {
        return nil, err
    }
    return dynamic.NewForConfig(cfg)
}
```

### cmd/cpg/ (new apply subcommand)

**Change:** Add `applyCmd` cobra command. Reads YAML files from output directory, converts to unstructured, applies via dynamic client.

---

## What NOT to Add

| Avoid | Why | What to Do Instead |
|-------|-----|---------------------|
| `github.com/cilium/cilium/pkg/k8s/client` (Cilium's own K8s clientset) | Internal to cilium-agent, not designed for external consumers. Pulls hive DI framework and massive dependency tree. | `k8s.io/client-go/dynamic` with `unstructured.Unstructured` |
| `controller-runtime` (`sigs.k8s.io/controller-runtime`) | Designed for operators/controllers, not CLI tools. Adds reconciliation loop, manager, caches -- all unnecessary overhead. | Direct `dynamic.Interface` calls |
| `k8s.io/cli-runtime` | kubectl-specific abstractions (resource builders, printers). Overkill for a single CRD type. | Direct dynamic client + `sigs.k8s.io/yaml` for reading files |
| Kafka L7 rules | `PortRuleKafka` is deprecated in Hubble proto (`Kafka kafka = 5; // Deprecated`). Out of scope per PROJECT.md. | Skip for now, revisit if user demand arises |
| `--server-dry-run` vs `--client-dry-run` flag | Overcomplicating UX. Server-side dry-run is strictly better (validates admission webhooks). | Single `--dry-run` (default: true), `--force` to disable |
| Custom CRD informer/lister | Not needed for one-shot apply. Informers are for watch-based reconciliation loops. | Single `Apply()` call per policy file |

---

## Recommended Stack Additions (v1.1)

### New Imports (from existing dependencies, no `go get` needed)

| Import Path | Purpose | From Module |
|-------------|---------|-------------|
| `k8s.io/client-go/dynamic` | Dynamic client for CRD apply | `k8s.io/client-go` v0.35.0 |
| `k8s.io/apimachinery/pkg/apis/meta/v1/unstructured` | Unstructured object wrapper | `k8s.io/apimachinery` v0.35.0 |
| `k8s.io/apimachinery/pkg/runtime` | Type conversion (typed -> unstructured) | `k8s.io/apimachinery` v0.35.0 |
| `k8s.io/apimachinery/pkg/runtime/schema` | GVR definition | `k8s.io/apimachinery` v0.35.0 |
| `encoding/json` | JSON marshaling for SSA patch (stdlib) | Go stdlib |
| `path/filepath` | Walking policy output directory for apply | Go stdlib |
| `os` | Reading YAML files from disk | Go stdlib |

### New Cilium Types Used (from existing `cilium/cilium` v1.19.1)

| Type | Package | Purpose |
|------|---------|---------|
| `api.L7Rules` | `pkg/policy/api` | L7 rule container on PortRule |
| `api.PortRuleHTTP` | `pkg/policy/api` | HTTP method/path/host/header matching |
| `api.PortRuleDNS` | `pkg/policy/api` | DNS name/pattern matching |
| `api.FQDNSelector` | `pkg/policy/api` | FQDN-based egress selectors (ToFQDNs) |
| `flowpb.Layer7` | `api/v1/flow` | L7 flow data container |
| `flowpb.HTTP` | `api/v1/flow` | HTTP request/response fields |
| `flowpb.DNS` | `api/v1/flow` | DNS query/response fields |
| `flowpb.L7FlowType` | `api/v1/flow` | REQUEST/RESPONSE/SAMPLE discriminator |

---

## Version Compatibility (no changes from v1.0)

All new functionality uses types already present in the existing dependency versions. No version bumps needed.

| Existing Module | Version | New Feature Support |
|-----------------|---------|---------------------|
| `cilium/cilium` | v1.19.1 | L7Rules, PortRuleHTTP, PortRuleDNS, FQDNSelector, Layer7, HTTP, DNS -- all present |
| `k8s.io/client-go` | v0.35.0 | `dynamic.Interface.Apply()` with `metav1.ApplyOptions` -- present |
| `k8s.io/apimachinery` | v0.35.0 | `unstructured.Unstructured`, `runtime.DefaultUnstructuredConverter`, `metav1.DryRunAll` -- present |

---

## Sources

- [Cilium policy API types (pkg.go.dev)](https://pkg.go.dev/github.com/cilium/cilium@v1.19.1/pkg/policy/api) -- L7Rules, PortRuleHTTP, PortRuleDNS, FQDNSelector (HIGH confidence)
- [Cilium flow proto types (pkg.go.dev)](https://pkg.go.dev/github.com/cilium/cilium@v1.19.1/api/v1/flow) -- Layer7, HTTP, DNS, L7FlowType (HIGH confidence)
- [client-go dynamic package (pkg.go.dev)](https://pkg.go.dev/k8s.io/client-go@v0.35.0/dynamic) -- ResourceInterface.Apply() signature (HIGH confidence)
- [Kubernetes Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/) -- SSA semantics and FieldManager (HIGH confidence)
- [Cilium DNS-based policies](https://docs.cilium.io/en/stable/security/dns/) -- ToFQDNs constraints, FQDN selector behavior (HIGH confidence)
- [Cilium L7 visibility docs](https://docs.cilium.io/en/latest/observability/visibility/) -- L7 flow requirements and proxy behavior (HIGH confidence)
- [Cilium FQDN source code](https://github.com/cilium/cilium/blob/main/pkg/policy/api/fqdn.go) -- FQDNSelector type definition (HIGH confidence)
- [CNCF: Safely managing Cilium network policies](https://www.cncf.io/blog/2025/11/06/safely-managing-cilium-network-policies-in-kubernetes-testing-and-simulation-techniques/) -- Dry-run best practices (MEDIUM confidence)

---
*Stack research for: CPG v1.1 -- L7 Policies & Apply Command*
*Researched: 2026-03-09*
