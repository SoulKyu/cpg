# Phase 3: Production Hardening - Research

**Researched:** 2026-03-08
**Domain:** Kubernetes port-forwarding, CiliumNetworkPolicy deduplication, CIDR-based policy generation (Go)
**Confidence:** HIGH

## Summary

Phase 3 adds five capabilities to the existing streaming pipeline: (1) auto port-forward to hubble-relay, (2) file-based deduplication, (3) cluster-based deduplication via the Cilium typed clientset, (4) flow aggregation improvements (DEDP-03), and (5) CIDR-based rules for external (world identity) traffic. All five build on well-understood Go/Kubernetes patterns.

The port-forward feature uses `k8s.io/client-go/tools/portforward` with SPDY transport -- the same mechanism `kubectl port-forward` uses. The Cilium project provides a typed clientset at `pkg/k8s/client/clientset/versioned` with `NewForConfig(*rest.Config)` that gives typed access to `CiliumNetworkPolicies().List()`. For CIDR rules, the Cilium `api.EgressCommonRule` has `ToCIDR` (`CIDRSlice`) and `api.IngressCommonRule` has `FromCIDR` (`CIDRSlice`). World identity is detectable via the `reserved:world` label in flow endpoint labels or numeric identity `2` on the `Endpoint.Identity` field.

The main complexity is the port-forward lifecycle management (background goroutine, ready channel, reconnection on pod restart) and the semantic comparison for deduplication (comparing structured policies, not YAML strings).

**Primary recommendation:** Create a `pkg/k8s/` package for Kubernetes interactions (port-forward + CNP listing). Detect world identity by checking `Endpoint.Identity == 2` or `reserved:world` in labels. Use `api.CIDR` type for CIDR rules with `/32` for individual IPs from flows.

<phase_requirements>

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| CONN-02 | Auto port-forward to hubble-relay in kube-system | `k8s.io/client-go/tools/portforward` + SPDY dialer; resolve service to pod, forward port 4245, pass local address to gRPC client |
| DEDP-01 | Skip generating policy if equivalent file already exists | Load existing policy from disk path, compare with `reflect.DeepEqual` on Spec after normalization; existing `readExistingPolicy()` in `output/writer.go` provides the read logic |
| DEDP-02 | Skip generating policy if equivalent CiliumNetworkPolicy exists in cluster | Cilium typed clientset `CiliumV2().CiliumNetworkPolicies(ns).List()` with label selector `app.kubernetes.io/managed-by=cpg`; compare Spec fields |
| DEDP-03 | Aggregate similar flows before generating policies | Existing `Aggregator` already groups by `(namespace, workload)` and flushes periodically; `BuildPolicy` already deduplicates ports within a flush window; enhance by comparing against previous flush results |
| PGEN-03 | Generate CIDR-based rules for external traffic (world identity) | Detect `reserved:world` label or `Endpoint.Identity == 2`; use `api.EgressCommonRule.ToCIDR` / `api.IngressCommonRule.FromCIDR` with `api.CIDR("x.x.x.x/32")` |

</phase_requirements>

## Standard Stack

### Core (already in go.mod)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/cilium/cilium` | v1.19.1 | CRD types, policy API (CIDR types), typed clientset | Already a dependency; provides `CIDRSlice`, `CIDRRule`, typed CNP client |
| `k8s.io/client-go` | v0.35.0 | Port-forward, REST config, kubeconfig loading | Already indirect dep via Cilium; promote to direct |
| `k8s.io/apimachinery` | v0.35.0 | `metav1.ListOptions`, label selectors | Already a direct dependency |
| `go.uber.org/zap` | v1.27.1 | Structured logging | Already a direct dependency |

### New Direct Dependencies (promote from indirect)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `k8s.io/client-go/tools/portforward` | v0.35.0 | Programmatic kubectl port-forward | CONN-02: auto port-forward to hubble-relay |
| `k8s.io/client-go/transport/spdy` | v0.35.0 | SPDY dialer for port-forward tunnel | Required by portforward package |
| `k8s.io/client-go/tools/clientcmd` | v0.35.0 | Load kubeconfig, build `rest.Config` | Both port-forward and cluster dedup need it |
| `github.com/cilium/cilium/pkg/k8s/client/clientset/versioned` | v1.19.1 | Typed Cilium CRD clientset | DEDP-02: list CiliumNetworkPolicies from cluster |

### Already Available (no changes needed)
| Library | Purpose | Phase 3 Role |
|---------|---------|-------------|
| `github.com/cilium/cilium/pkg/policy/api` | `CIDRSlice`, `CIDR`, `CIDRRule`, `EgressCommonRule.ToCIDR`, `IngressCommonRule.FromCIDR` | PGEN-03: CIDR-based rules |
| `pkg/policy/builder.go` | `BuildPolicy()` | Modified to detect world identity and produce CIDR rules |
| `pkg/output/writer.go` | `readExistingPolicy()` + `Write()` | DEDP-01: file-based dedup check |
| `pkg/hubble/aggregator.go` | Flow aggregation by `(namespace, workload)` | DEDP-03: base for enhanced aggregation |

**Installation:**
```bash
# Promote transitive deps to direct
go get k8s.io/client-go@v0.35.0
```

## Architecture Patterns

### New Package Structure
```
pkg/
├── k8s/
│   ├── portforward.go       # Auto port-forward to hubble-relay service
│   ├── portforward_test.go
│   ├── client.go             # Kubeconfig loading, rest.Config, Cilium clientset
│   └── client_test.go
├── hubble/                   # (existing, minor changes)
│   ├── client.go             # Add auto port-forward integration point
│   ├── aggregator.go         # Existing (no changes needed for DEDP-03)
│   └── pipeline.go           # Add dedup filter stage
├── policy/
│   ├── builder.go            # Modified: detect world identity, produce CIDR rules
│   ├── dedup.go              # NEW: semantic policy comparison for dedup
│   └── dedup_test.go
├── output/
│   └── writer.go             # Modified: file dedup check before write
├── labels/
│   └── selector.go           # Existing (no changes)
```

### Pattern 1: Kubernetes Port-Forward Lifecycle
**What:** Background goroutine managing port-forward tunnel to hubble-relay service, with ready signaling and cleanup.
**When to use:** When `--server` flag is not provided (CONN-02).
```go
// Source: k8s.io/client-go/tools/portforward API
func PortForwardToRelay(ctx context.Context, config *rest.Config, logger *zap.Logger) (string, func(), error) {
    // 1. Resolve hubble-relay service to a pod
    k8sClient, _ := kubernetes.NewForConfig(config)
    pods, _ := k8sClient.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{
        LabelSelector: "k8s-app=hubble-relay",
    })
    pod := pods.Items[0]

    // 2. Build SPDY dialer
    transport, upgrader, _ := spdy.RoundTripperFor(config)
    url := k8sClient.CoreV1().RESTClient().Post().
        Resource("pods").
        Namespace(pod.Namespace).
        Name(pod.Name).
        SubResource("portforward").URL()
    dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport},
        http.MethodPost, url)

    // 3. Create port-forwarder with random local port
    stopCh := make(chan struct{})
    readyCh := make(chan struct{})
    fw, _ := portforward.New(dialer, []string{"0:4245"}, stopCh, readyCh,
        io.Discard, io.Discard)

    // 4. Run in background, wait for ready
    go fw.ForwardPorts()
    <-readyCh

    ports, _ := fw.GetPorts()
    localAddr := fmt.Sprintf("localhost:%d", ports[0].Local)

    cleanup := func() { close(stopCh) }
    return localAddr, cleanup, nil
}
```

### Pattern 2: World Identity Detection and CIDR Rule Generation
**What:** Detect `reserved:world` identity in flow endpoints and generate CIDR rules instead of endpoint selectors.
**When to use:** In `BuildPolicy()` when peer endpoint is external (PGEN-03).
```go
// Source: Cilium identity system + policy API
const ReservedWorldIdentity uint32 = 2

func isWorldIdentity(ep *flowpb.Endpoint) bool {
    if ep.Identity == ReservedWorldIdentity {
        return true
    }
    for _, l := range ep.Labels {
        if l == "reserved:world" {
            return true
        }
    }
    return false
}

// In buildEgressRules, when destination is world:
func buildCIDREgressRule(f *flowpb.Flow) api.EgressRule {
    ip := f.IP.Destination // flow's destination IP
    cidr := api.CIDR(ip + "/32")
    port, proto := extractPort(f)

    rule := api.EgressRule{
        EgressCommonRule: api.EgressCommonRule{
            ToCIDR: api.CIDRSlice{cidr},
        },
    }
    if port != "" {
        rule.ToPorts = api.PortRules{
            {Ports: []api.PortProtocol{{Port: port, Protocol: proto}}},
        }
    }
    return rule
}
```

### Pattern 3: Semantic Policy Deduplication
**What:** Compare two CiliumNetworkPolicy objects by their Spec content, ignoring metadata differences.
**When to use:** DEDP-01 (file dedup) and DEDP-02 (cluster dedup).
```go
// Compare policies by spec content, not metadata
func PoliciesEquivalent(a, b *ciliumv2.CiliumNetworkPolicy) bool {
    if a.Spec == nil || b.Spec == nil {
        return a.Spec == nil && b.Spec == nil
    }
    // Compare EndpointSelector
    if !reflect.DeepEqual(a.Spec.EndpointSelector, b.Spec.EndpointSelector) {
        return false
    }
    // Compare normalized ingress rules (sorted by peer key)
    if !rulesEquivalent(a.Spec.Ingress, b.Spec.Ingress) {
        return false
    }
    // Compare normalized egress rules
    if !egressRulesEquivalent(a.Spec.Egress, b.Spec.Egress) {
        return false
    }
    return true
}
```

### Pattern 4: Cluster Dedup via Cilium Clientset
**What:** List existing CiliumNetworkPolicies from cluster and check for equivalence before writing.
**When to use:** DEDP-02.
```go
import (
    ciliumclient "github.com/cilium/cilium/pkg/k8s/client/clientset/versioned"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func LoadClusterPolicies(ctx context.Context, config *rest.Config, namespace string) (map[string]*ciliumv2.CiliumNetworkPolicy, error) {
    cs, err := ciliumclient.NewForConfig(config)
    if err != nil {
        return nil, fmt.Errorf("creating Cilium clientset: %w", err)
    }

    list, err := cs.CiliumV2().CiliumNetworkPolicies(namespace).List(ctx, metav1.ListOptions{
        LabelSelector: "app.kubernetes.io/managed-by=cpg",
    })
    if err != nil {
        return nil, fmt.Errorf("listing CiliumNetworkPolicies: %w", err)
    }

    policies := make(map[string]*ciliumv2.CiliumNetworkPolicy, len(list.Items))
    for i := range list.Items {
        policies[list.Items[i].Name] = &list.Items[i]
    }
    return policies, nil
}
```

### Pattern 5: Kubeconfig Loading (Standard client-go)
**What:** Load `rest.Config` from kubeconfig for both port-forward and cluster dedup.
**When to use:** CLI initialization when `--server` is not provided or cluster dedup is enabled.
```go
import "k8s.io/client-go/tools/clientcmd"

func LoadKubeConfig() (*rest.Config, error) {
    rules := clientcmd.NewDefaultClientConfigLoadingRules()
    config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
        rules, &clientcmd.ConfigOverrides{},
    ).ClientConfig()
    if err != nil {
        return nil, fmt.Errorf("loading kubeconfig: %w", err)
    }
    return config, nil
}
```

### Anti-Patterns to Avoid
- **YAML string comparison for dedup:** Field ordering, whitespace, and comments make string comparison unreliable. Always compare structured objects.
- **CIDR rules for managed endpoints:** Cilium ignores CIDR rules when both sides are Cilium-managed. Only generate `toCIDR`/`fromCIDR` for `reserved:world` identity (numeric 2).
- **Synchronous cluster API calls per flow:** Cache the cluster policy list at startup and refresh periodically (e.g., every 30s), not per-flow.
- **Port-forward without reconnection handling:** `kubectl port-forward` connections drop on pod restart. Wrap in retry logic or detect and warn.
- **Global kubeconfig loading:** Only load kubeconfig when needed (auto port-forward or cluster dedup). When `--server` is provided without cluster dedup, no kubeconfig is required.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Port-forward tunnel | Raw TCP proxy | `k8s.io/client-go/tools/portforward` | Handles SPDY upgrade, multiplexing, pod lifecycle |
| Kubeconfig loading | Manual file parsing | `clientcmd.NewDefaultClientConfigLoadingRules()` | Handles `$KUBECONFIG`, `~/.kube/config`, in-cluster config |
| Cilium CRD client | Dynamic client + unstructured | `cilium/pkg/k8s/client/clientset/versioned` | Typed access, schema validation, proper serialization |
| CIDR type construction | String formatting | `api.CIDR("10.0.0.1/32")` | Type-safe, validated by Cilium |
| Policy comparison | YAML diff | `reflect.DeepEqual` on normalized Spec | Handles field ordering, nil vs empty, internal formats |
| Service-to-pod resolution | Manual endpoint lookup | `CoreV1().Pods().List(LabelSelector: "k8s-app=hubble-relay")` | Standard K8s API, handles pod selection |

**Key insight:** The Cilium monorepo already provides both the typed clientset for CNP listing and the policy API types for CIDR rules. No additional dependencies beyond promoting `client-go` to direct.

## Common Pitfalls

### Pitfall 1: CIDR Rules on Managed Endpoints (Silently Ignored)
**What goes wrong:** Generating `toCIDR` or `fromCIDR` rules using pod IPs produces policies that apply cleanly but have no effect.
**Why it happens:** Cilium resolves managed endpoints by security identity, not IP. CIDR rules only apply to traffic where at least one side is unmanaged (world identity).
**How to avoid:** Check `Endpoint.Identity == 2` or `reserved:world` label before generating CIDR rules. For managed endpoints, always use label-based selectors.
**Warning signs:** Policies with `/32` CIDR pointing at pod IPs within the cluster CIDR range.

### Pitfall 2: Port-Forward Connection Instability
**What goes wrong:** Long-running port-forward connections drop silently when the hubble-relay pod restarts or during cluster upgrades.
**Why it happens:** Port-forward uses SPDY over HTTP upgrade to the apiserver, then tunnels to the pod. If the pod goes away, the tunnel breaks.
**How to avoid:** Detect `ErrLostConnectionToPod` from the `ForwardPorts()` call. Either warn the user and exit, or implement reconnection with backoff. Log clearly when the port-forward drops.
**Warning signs:** gRPC stream errors after a period of successful operation; no new flows arriving.

### Pitfall 3: Dedup False Negatives from Field Ordering
**What goes wrong:** Two semantically identical policies are treated as different because ingress/egress rules or ports are in different order.
**Why it happens:** `reflect.DeepEqual` is order-sensitive for slices. Two policies with the same rules in different order will compare as unequal.
**How to avoid:** Normalize (sort) rules before comparison. Sort ingress/egress rules by a deterministic key (e.g., peer label hash). Sort ports within each rule.
**Warning signs:** Duplicate policy files appearing for the same workload across flush cycles.

### Pitfall 4: Cilium Clientset Import Weight
**What goes wrong:** Importing the Cilium versioned clientset pulls significant additional transitive dependencies.
**Why it happens:** The clientset package depends on `k8s.io/client-go` typed client infrastructure.
**How to avoid:** This is already mitigated since `client-go` is already an indirect dep at v0.35.0. Promote to direct and accept the cost. Binary size increase should be marginal.
**Warning signs:** Build time increase; verify with `go build -v`.

### Pitfall 5: Port 0 (Random) vs Fixed Port for Port-Forward
**What goes wrong:** Using a fixed local port (e.g., 4245) conflicts if another process uses it. Using port 0 requires retrieving the actual bound port from `GetPorts()`.
**Why it happens:** `portforward.New` accepts port specs like `"0:4245"` (random local, remote 4245).
**How to avoid:** Always use port 0 for the local side. Wait for `Ready` channel, then call `GetPorts()` to get the actual port. Pass this to the gRPC client.
**Warning signs:** `bind: address already in use` errors on startup.

### Pitfall 6: Flow IP Field Location
**What goes wrong:** Looking for the remote IP in the wrong flow field.
**Why it happens:** The flow has `IP.Source` and `IP.Destination` (layer 3), plus `Source` and `Destination` endpoints. The IP for CIDR rules must come from the `IP` field, not the endpoint.
**How to avoid:** For egress world traffic: use `flow.IP.Destination`. For ingress world traffic: use `flow.IP.Source`. Always nil-check `flow.IP`.
**Warning signs:** Nil pointer panics or empty CIDR rules.

### Pitfall 7: Cluster Dedup Requires RBAC
**What goes wrong:** The tool fails with 403 Forbidden when listing CiliumNetworkPolicies.
**Why it happens:** Listing CiliumNetworkPolicies requires a ClusterRole with `get`/`list` on `ciliumnetworkpolicies.cilium.io`.
**How to avoid:** Document RBAC requirements. Make cluster dedup opt-in (e.g., `--cluster-dedup` flag) so the tool works without extra permissions by default.
**Warning signs:** Forbidden errors in logs during startup.

## Code Examples

### Complete Port-Forward Integration with CLI
```go
// In cmd/cpg/generate.go runGenerate():
var server string
var cleanupFn func()

serverFlag, _ := cmd.Flags().GetString("server")
if serverFlag != "" {
    server = serverFlag
} else {
    // Auto port-forward (CONN-02)
    config, err := k8s.LoadKubeConfig()
    if err != nil {
        return fmt.Errorf("loading kubeconfig for auto port-forward: %w", err)
    }
    addr, cleanup, err := k8s.PortForwardToRelay(ctx, config, logger)
    if err != nil {
        return fmt.Errorf("auto port-forward to hubble-relay: %w", err)
    }
    defer cleanup()
    server = addr
    logger.Info("auto port-forwarded to hubble-relay", zap.String("local-address", addr))
}
```

### CIDR Rule in BuildPolicy
```go
// Modified buildEgressRules to handle world identity
func buildEgressRules(flows []*flowpb.Flow, policyNamespace string) []api.EgressRule {
    // ... existing peer grouping logic ...

    for _, f := range flows {
        if f.Destination == nil {
            continue
        }

        // Check if destination is world identity -> CIDR rule
        if isWorldIdentity(f.Destination) {
            ip := getDestinationIP(f)
            if ip == "" {
                continue
            }
            port, proto := extractPort(f)
            cidrKey := ip + "/32"

            // Group CIDR rules by IP (similar to peer grouping)
            // ... dedup ports per CIDR ...
            continue
        }

        // Existing endpoint selector logic for managed endpoints
        // ...
    }
}

func getDestinationIP(f *flowpb.Flow) string {
    if f.IP == nil {
        return ""
    }
    return f.IP.Destination
}
```

### File-Based Dedup in Writer
```go
// Enhanced Write method with dedup (DEDP-01)
func (w *Writer) Write(event policy.PolicyEvent) error {
    path := filepath.Join(w.outputDir, event.Namespace, event.Workload+".yaml")

    existing, err := readExistingPolicy(path)
    if err != nil {
        return fmt.Errorf("reading existing policy %s: %w", path, err)
    }

    if existing != nil {
        merged := policy.MergePolicy(existing, event.Policy)
        if policy.PoliciesEquivalent(existing, merged) {
            w.logger.Debug("policy unchanged, skipping write",
                zap.String("path", path),
            )
            return nil // DEDP-01: skip if equivalent
        }
        // Write merged result
        // ...
    }
    // ... existing write logic ...
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `spdy.RoundTripperFor` only | `portforward.NewSPDYOverWebsocketDialer` + fallback | client-go v0.30 (2024) | WebSocket transport for port-forward; SPDY still works |
| Manual CRD scheme registration | Cilium versioned clientset `NewForConfig` | Cilium 1.14+ | No manual scheme setup needed |
| `kubectl port-forward` as subprocess | `k8s.io/client-go/tools/portforward` programmatic | Always available | No subprocess management, proper lifecycle control |

**Deprecated/outdated:**
- Spawning `kubectl port-forward` as a subprocess: fragile, hard to lifecycle-manage, no programmatic port retrieval
- Using dynamic client for CiliumNetworkPolicy: loses type safety, requires manual scheme registration

## Open Questions

1. **Port-forward reconnection strategy**
   - What we know: `ForwardPorts()` returns `ErrLostConnectionToPod` when the relay pod dies. The gRPC stream will also error.
   - What's unclear: Whether to reconnect automatically (re-resolve pod, re-establish tunnel) or just exit with a clear message. Auto-reconnect adds complexity and may mask cluster issues.
   - Recommendation: For v1, log a clear error and exit. The user can re-run. Auto-reconnect is v2 scope.

2. **Cluster dedup refresh interval**
   - What we know: Listing CNPs on every flush (every 5s) adds API server load.
   - What's unclear: Optimal refresh interval for the cluster policy cache.
   - Recommendation: Load at startup, refresh every 60s in background. Cache is a `map[string]*CiliumNetworkPolicy` keyed by `namespace/name`.

3. **CIDR aggregation (multiple IPs to a broader CIDR)**
   - What we know: Each world-identity flow has a single `/32` IP. Many flows to different IPs in the same subnet could produce many `/32` CIDR rules.
   - What's unclear: Whether to aggregate to broader CIDRs (e.g., `/24`) automatically.
   - Recommendation: For v1, use `/32` per IP. CIDR aggregation is a v2 optimization. Users can manually broaden CIDRs in generated policies.

4. **hubble-relay service label selector**
   - What we know: Default Cilium install uses `k8s-app=hubble-relay` label on relay pods in `kube-system`.
   - What's unclear: Whether all Cilium distributions use the same label/namespace.
   - Recommendation: Default to `kube-system` namespace with `k8s-app=hubble-relay` label. Allow override via `--relay-namespace` and `--relay-selector` flags if needed (or defer to v2).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing (stdlib) + `testify/assert` |
| Config file | None -- Go test infrastructure is zero-config |
| Quick run command | `go test ./pkg/... -short -count=1` |
| Full suite command | `go test ./... -count=1 -race` |

### Phase Requirements -> Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CONN-02 | Auto port-forward to hubble-relay | unit (mock) | `go test ./pkg/k8s/ -run TestPortForward -count=1` | Wave 0 |
| DEDP-01 | Skip write if equivalent file exists | unit | `go test ./pkg/output/ -run TestWriteDedup -count=1` | Wave 0 |
| DEDP-02 | Skip if equivalent CNP in cluster | unit (mock) | `go test ./pkg/policy/ -run TestClusterDedup -count=1` | Wave 0 |
| DEDP-03 | Aggregate similar flows | unit | `go test ./pkg/hubble/ -run TestAggregation -count=1` | Existing (aggregator_test.go covers base case) |
| PGEN-03 | CIDR rules for world identity | unit | `go test ./pkg/policy/ -run TestCIDRWorld -count=1` | Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./pkg/... -short -count=1`
- **Per wave merge:** `go test ./... -count=1 -race`
- **Phase gate:** Full suite green before `/gsd:verify-work`

### Wave 0 Gaps
- [ ] `pkg/k8s/portforward.go` -- port-forward lifecycle (new file)
- [ ] `pkg/k8s/portforward_test.go` -- covers CONN-02 with mock K8s client
- [ ] `pkg/k8s/client.go` -- kubeconfig loading + Cilium clientset (new file)
- [ ] `pkg/k8s/client_test.go` -- covers client creation
- [ ] `pkg/policy/dedup.go` -- semantic policy comparison (new file)
- [ ] `pkg/policy/dedup_test.go` -- covers DEDP-01, DEDP-02 comparison logic
- [ ] `pkg/policy/builder_test.go` -- add TestCIDRWorld* test cases for PGEN-03
- [ ] `pkg/output/writer_test.go` -- add TestWriteDedup* test cases for DEDP-01

## Sources

### Primary (HIGH confidence)
- [k8s.io/client-go/tools/portforward](https://pkg.go.dev/k8s.io/client-go/tools/portforward) -- PortForwarder API, New(), GetPorts(), Ready channel
- [k8s.io/client-go/transport/spdy](https://pkg.go.dev/k8s.io/client-go/transport/spdy) -- RoundTripperFor, NewDialer for SPDY transport
- [Cilium policy API (pkg/policy/api)](https://pkg.go.dev/github.com/cilium/cilium/pkg/policy/api) -- CIDRSlice, CIDR, CIDRRule, EgressCommonRule.ToCIDR, IngressCommonRule.FromCIDR
- [Cilium versioned clientset](https://pkg.go.dev/github.com/cilium/cilium/pkg/k8s/client/clientset/versioned) -- NewForConfig, CiliumV2().CiliumNetworkPolicies().List()
- [Cilium identity package](https://pkg.go.dev/github.com/cilium/cilium/pkg/identity) -- ReservedIdentityWorld = 2
- [Cilium Layer 3 Policy docs](https://docs.cilium.io/en/stable/security/policy/language/) -- toCIDR/fromCIDR YAML syntax and semantics

### Secondary (MEDIUM confidence)
- [Programmatic port-forward in Go (gianarb.it)](https://gianarb.it/blog/programmatically-kube-port-forward-in-go) -- Complete code example for SPDY dialer + portforward
- [Cilium CiliumNetworkPolicy typed client source](https://github.com/cilium/cilium/blob/master/pkg/k8s/client/clientset/versioned/typed/cilium.io/v2/ciliumnetworkpolicy.go) -- Interface definition
- Project research: `.planning/research/PITFALLS.md` -- CIDR on managed endpoints, dedup by structure not strings

### Tertiary (LOW confidence)
- World identity numeric value (2) -- verified via official Cilium identity docs and `.planning/research/SUMMARY.md`

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- all libraries already in go.mod (direct or indirect), versions confirmed
- Port-forward: HIGH -- well-documented client-go API, multiple verified examples
- CIDR rules: HIGH -- Cilium policy API types verified on pkg.go.dev, YAML syntax verified in official docs
- Cluster dedup: MEDIUM -- Cilium clientset API verified, but potential import weight and RBAC implications need build-time validation
- Dedup semantics: MEDIUM -- `reflect.DeepEqual` approach is straightforward but normalization (sorting rules) needs careful implementation

**Research date:** 2026-03-08
**Valid until:** 2026-04-08 (stable domain, client-go and Cilium API are stable)
