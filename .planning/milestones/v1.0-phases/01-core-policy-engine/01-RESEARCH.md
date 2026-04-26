# Phase 1: Core Policy Engine - Research

**Researched:** 2026-03-08
**Domain:** CiliumNetworkPolicy generation from Hubble gRPC flow streams (Go)
**Confidence:** HIGH

## Summary

This phase delivers an end-to-end CLI tool (`cpg generate`) that connects to Hubble Relay via gRPC, streams dropped flows, transforms them into valid CiliumNetworkPolicy YAML files with smart label selection, and writes organized output. The domain is well-understood: Cilium's Go types are stable and well-documented, the Hubble Observer gRPC API is straightforward, and the policy CRD structure is mature.

**Critical finding:** The user's CONTEXT.md specifies "Go 1.23 minimum" but Cilium v1.19.1 requires Go 1.25.0 in its `go.mod`. The project must use Go >= 1.25 to import Cilium v1.19. Only 2 `replace` directives are needed (controller-tools and gobgp), which is manageable. The local system has Go 1.22.2 -- upgrading is required before build.

**Primary recommendation:** Use Cilium v1.19.1 types directly (`pkg/k8s/apis/cilium.io/v2`, `pkg/policy/api`, `api/v1/flow`, `api/v1/observer`). Build CiliumNetworkPolicy structs programmatically and serialize with `sigs.k8s.io/yaml`. Parse Hubble flow labels with `labels.ParseLabel()` which natively handles the `k8s:key=value` format.

<user_constraints>

## User Constraints (from CONTEXT.md)

### Locked Decisions
- Import full `github.com/cilium/cilium` monorepo for type-safe CRD and proto types, target Cilium 1.19
- Label selection hierarchy: `app.kubernetes.io/name` > `app` > all labels with denylist filtering
- Denylist labels: `pod-template-hash`, `controller-revision-hash`, `statefulset.kubernetes.io/pod-name`, `job-name`, `batch.kubernetes.io/job-name`, `batch.kubernetes.io/controller-uid`, `apps.kubernetes.io/pod-index`
- When no priority label found: use ALL labels after denylist filtering
- Same label logic for endpointSelector and peer selectors
- Always include `k8s:io.kubernetes.pod.namespace` in peer selectors for cross-namespace traffic
- Output directory: `<output-dir>/<namespace>/<workload>.yaml`, default `./policies`
- One file per workload with both ingress and egress rules
- K8s resource name: `cpg-<workload>` with label `app.kubernetes.io/managed-by: cpg`
- Merge intelligent: read existing file, add new ports/peers, rewrite
- CLI: `cpg generate`, `--server`/`-s` required, insecure by default, `--tls` to enable
- Namespace: default from kubeconfig, `--namespace`/`-n` repeatable, `--all-namespaces`/`-A`
- Streaming only, flush interval 5s default (`--flush-interval`), aggregation key `(namespace, workload, direction)`
- Graceful shutdown on SIGINT/SIGTERM: flush + summary
- gRPC reconnect: exponential backoff 1s..60s
- Malformed flows: skip + debug log
- File write errors: log, continue, retain in memory
- Logging: zap, info default, `--debug`/`--log-level debug`, `--json` for structured output
- Session summary on shutdown: duration, flows seen, policies generated/updated/skipped, lost events, output dir
- LostEvents: aggregated warning every 30s
- Package layout: `cmd/`, `pkg/hubble/`, `pkg/policy/`, `pkg/labels/`, `pkg/output/`
- Makefile: build (ldflags version), test, lint, clean, all
- Cobra CLI, zap logging, golangci-lint v2

### Claude's Discretion
- Channel buffer sizes for streaming pipeline
- Exact backoff parameters for gRPC reconnection
- Internal data structures for flow aggregation
- Compression/optimization of generated YAML
- Test fixture design and coverage strategy
- golangci-lint exact configuration

### Deferred Ideas (OUT OF SCOPE)
- Auto port-forward to hubble-relay
- File-based deduplication
- Cluster-based deduplication via client-go
- CIDR-based rules for external traffic / reserved:world identity
- CI gate on binary size
- TLS certificate configuration flags (--tls-cert, --tls-key, --tls-ca)
- One-shot/duration mode for CI pipelines

</user_constraints>

<phase_requirements>

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| PGEN-01 | Generate ingress CiliumNetworkPolicy from dropped flows | Cilium `api.IngressRule` with `FromEndpoints` + `FromPorts`; flow `TrafficDirection == INGRESS` + `Verdict == DROPPED` |
| PGEN-02 | Generate egress CiliumNetworkPolicy from dropped flows | Cilium `api.EgressRule` with `ToEndpoints` + `ToPorts`; flow `TrafficDirection == EGRESS` + `Verdict == DROPPED` |
| PGEN-04 | Smart label selection for endpoint selectors | Hubble flow labels are `k8s:key=value` format; use `labels.ParseLabel()` then apply hierarchy + denylist |
| PGEN-05 | Exact port number + protocol (TCP/UDP) | Flow `L4.TCP.DestinationPort` / `L4.UDP.DestinationPort` maps to `api.PortProtocol{Port, Protocol}` |
| PGEN-06 | Valid CiliumNetworkPolicy YAML for kubectl apply | Use `ciliumv2.CiliumNetworkPolicy` struct with proper TypeMeta/ObjectMeta; serialize with `sigs.k8s.io/yaml` |
| OUTP-01 | One YAML file per policy in organized directory | `<output-dir>/<namespace>/<workload>.yaml` with merge-on-write logic |
| OUTP-03 | Structured logging via zap with configurable levels | `go.uber.org/zap` with Cobra flag integration for level + format |
| CONN-01 | Connect to Hubble Relay via gRPC | `observer.NewObserverClient(conn)` with `GetFlows` streaming RPC |
| CONN-03 | Override relay address with --server flag | Cobra string flag, required |
| CONN-04 | Filter by namespace or all namespaces | `FlowFilter` whitelist with namespace filtering |
| CONN-05 | Detect and warn about LostEvents | `GetFlowsResponse.LostEvents` field, aggregated 30s warning |
| OUTP-02 | Continuous real-time policy generation (streaming) | `Follow: true` in `GetFlowsRequest`, channel pipeline, temporal aggregation |

</phase_requirements>

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/cilium/cilium` | v1.19.1 | CRD types, flow proto, label parsing | Locked decision -- type-safe, native proto types |
| `github.com/spf13/cobra` | latest | CLI framework | Locked decision -- de facto Go CLI standard |
| `go.uber.org/zap` | latest | Structured logging | Locked decision -- 4-20x faster than alternatives |
| `sigs.k8s.io/yaml` | latest | YAML serialization | K8s ecosystem standard, JSON-tag compatible with CRD structs |
| `google.golang.org/grpc` | v1.78.0 (via cilium) | gRPC client | Required by Hubble Observer API |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `k8s.io/apimachinery` | v0.35.0 (via cilium) | `metav1.TypeMeta`, `metav1.ObjectMeta` | Every CiliumNetworkPolicy struct |
| `github.com/cilium/cilium/pkg/labels` | via cilium | `ParseLabel()`, `Label`, `LabelSourceK8s` | Parsing Hubble flow endpoint labels |
| `github.com/cilium/cilium/pkg/policy/api` | via cilium | `Rule`, `IngressRule`, `EgressRule`, `EndpointSelector` | Building policy rules programmatically |
| `github.com/cilium/cilium/api/v1/flow` | via cilium | `Flow`, `Endpoint`, `Layer4`, `Verdict`, `TrafficDirection` | Processing Hubble flow events |
| `github.com/cilium/cilium/api/v1/observer` | via cilium | `ObserverClient`, `GetFlowsRequest`, `FlowFilter` | gRPC streaming from Hubble Relay |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Full cilium monorepo | Standalone proto compilation | Lighter binary but lose type safety, label utilities, CRD types |
| `sigs.k8s.io/yaml` | `gopkg.in/yaml.v3` | Direct YAML but no JSON-tag compatibility with K8s structs |
| `zap` | `slog` (stdlib) | slog is simpler but lacks zap's performance and field-level control |

**Installation:**
```bash
# Requires Go >= 1.25.0 (Cilium v1.19.1 dependency)
go mod init github.com/your-org/cpg
go get github.com/cilium/cilium@v1.19.1
go get github.com/spf13/cobra@latest
go get go.uber.org/zap@latest
go get sigs.k8s.io/yaml@latest
```

**Required `replace` directives (copy from Cilium v1.19.1 go.mod):**
```
replace sigs.k8s.io/controller-tools => github.com/cilium/controller-tools v0.16.5-1
replace github.com/osrg/gobgp/v3 => github.com/cilium/gobgp/v3 v3.0.0-20260130142103-27e5da2a39e6
```

## Architecture Patterns

### Recommended Project Structure
```
cpg/
├── cmd/
│   └── cpg/
│       ├── main.go              # Entry point, root command
│       └── generate.go          # `cpg generate` subcommand
├── pkg/
│   ├── hubble/
│   │   ├── client.go            # gRPC connection, GetFlows streaming
│   │   ├── client_test.go
│   │   └── filter.go            # FlowFilter construction (verdict=DROPPED)
│   ├── labels/
│   │   ├── selector.go          # Label hierarchy, denylist, EndpointSelector building
│   │   └── selector_test.go
│   ├── policy/
│   │   ├── builder.go           # Flow -> CiliumNetworkPolicy transformation
│   │   ├── builder_test.go
│   │   ├── merge.go             # Read-modify-write policy merging
│   │   └── merge_test.go
│   └── output/
│       ├── writer.go            # Directory structure, file I/O
│       └── writer_test.go
├── Makefile
├── go.mod
├── go.sum
└── .golangci.yml
```

### Pattern 1: Channel Pipeline (Source -> Transform -> Sink)
**What:** Streaming architecture where each stage communicates via Go channels.
**When to use:** Real-time flow processing with temporal aggregation.
```go
// Simplified pipeline
func Run(ctx context.Context) error {
    flows := make(chan *flow.Flow, 256)       // source -> transform
    policies := make(chan *PolicyEvent, 64)    // transform -> sink

    g, ctx := errgroup.WithContext(ctx)
    g.Go(func() error { return streamFlows(ctx, client, flows) })
    g.Go(func() error { return aggregateAndBuild(ctx, flows, policies, flushInterval) })
    g.Go(func() error { return writeOutput(ctx, policies, outputDir) })
    return g.Wait()
}
```

### Pattern 2: Flow-to-Policy Transformation
**What:** Pure function that converts aggregated flows into a CiliumNetworkPolicy struct.
**When to use:** Core domain logic, highly testable without gRPC.
```go
func BuildPolicy(namespace, workload string, flows []*flow.Flow) *ciliumv2.CiliumNetworkPolicy {
    cnp := &ciliumv2.CiliumNetworkPolicy{
        TypeMeta: metav1.TypeMeta{
            APIVersion: "cilium.io/v2",
            Kind:       "CiliumNetworkPolicy",
        },
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("cpg-%s", workload),
            Namespace: namespace,
            Labels:    map[string]string{"app.kubernetes.io/managed-by": "cpg"},
        },
        Spec: &api.Rule{
            EndpointSelector: buildEndpointSelector(flows[0].Source), // or Destination
            Ingress:          buildIngressRules(ingressFlows),
            Egress:           buildEgressRules(egressFlows),
        },
    }
    return cnp
}
```

### Pattern 3: Label Parsing from Hubble Flows
**What:** Extract and filter labels from Hubble flow endpoints to build selectors.
**When to use:** Every policy generation -- the label heuristic is central to correctness.
```go
// Hubble flow endpoint labels arrive as []string: ["k8s:app=nginx", "k8s:pod-template-hash=abc123", ...]
func BuildSelector(endpointLabels []string) api.EndpointSelector {
    var filtered []labels.Label
    for _, raw := range endpointLabels {
        l := labels.ParseLabel(raw)
        if l.Source != labels.LabelSourceK8s {
            continue
        }
        if isDenylisted(l.Key) {
            continue
        }
        filtered = append(filtered, l)
    }

    // Priority: app.kubernetes.io/name > app > all remaining
    if lbl, ok := findLabel(filtered, "app.kubernetes.io/name"); ok {
        return api.NewESFromLabels(lbl)
    }
    if lbl, ok := findLabel(filtered, "app"); ok {
        return api.NewESFromLabels(lbl)
    }
    return api.NewESFromLabels(filtered...)
}
```

### Pattern 4: Temporal Aggregation with Flush
**What:** Accumulate flows by key `(namespace, workload, direction)` and flush periodically.
**When to use:** Avoiding one-policy-per-packet generation.
```go
type Aggregator struct {
    mu       sync.Mutex
    buckets  map[AggKey][]*flow.Flow
    interval time.Duration
}

type AggKey struct {
    Namespace string
    Workload  string
    Direction flow.TrafficDirection
}

func (a *Aggregator) Run(ctx context.Context, in <-chan *flow.Flow, out chan<- AggregatedBatch) {
    ticker := time.NewTicker(a.interval)
    defer ticker.Stop()
    for {
        select {
        case f := <-in:
            a.add(f)
        case <-ticker.C:
            a.flush(out)
        case <-ctx.Done():
            a.flush(out) // graceful shutdown: flush remaining
            return
        }
    }
}
```

### Pattern 5: Policy Merge (Read-Modify-Write)
**What:** When output file exists, read it, merge new ports/peers into existing rules, rewrite.
**When to use:** Every flush cycle to prevent duplicate rules.
```go
func MergePolicy(existing, incoming *ciliumv2.CiliumNetworkPolicy) *ciliumv2.CiliumNetworkPolicy {
    // Merge ingress: for each incoming rule, find matching peer selector in existing
    // If found: add new ports that don't already exist
    // If not found: append entire rule
    // Same logic for egress
    return merged
}
```

### Anti-Patterns to Avoid
- **One policy per flow:** Generates thousands of identical policies. Always aggregate first.
- **Using `Specs` (plural) instead of `Spec`:** `Spec` (singular `*api.Rule`) is cleaner for per-workload policies with one endpointSelector. Use `Specs` only if you need multiple different endpointSelectors in one resource (we do not).
- **Hardcoding `k8s:` prefix in string manipulation:** Use `labels.ParseLabel()` which handles the prefix natively.
- **Ignoring TrafficDirection:** A dropped flow can be INGRESS on the destination or EGRESS on the source. The policy must be applied to the correct endpoint.
- **Using pod IP as selector:** IPs are ephemeral. Always use label-based selectors.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| CiliumNetworkPolicy struct | Custom YAML templating | `ciliumv2.CiliumNetworkPolicy` | Type-safe, schema-validated, handles all edge cases |
| Label parsing | String splitting on `:` and `=` | `labels.ParseLabel()` | Handles reserved labels, escaping, source detection |
| EndpointSelector construction | Manual `matchLabels` map | `api.NewESFromLabels()` | Handles sanitization, internal label format |
| YAML serialization | `fmt.Sprintf` YAML templates | `sigs.k8s.io/yaml.Marshal()` | Correct field ordering, omitempty handling, JSON-tag support |
| gRPC client setup | Raw net/http | `grpc.NewClient()` + `observer.NewObserverClient()` | Handles HTTP/2, streaming, keepalive, backpressure |
| CLI flag parsing | `flag` stdlib | `cobra.Command` | Subcommands, help text, shell completion, kubectl-style UX |
| Exponential backoff | Manual sleep loops | Simple helper or `time.Duration` math | Edge cases: jitter, max cap, context cancellation |

**Key insight:** The Cilium monorepo provides every type and utility needed for this tool. The only custom logic is the label heuristic (priority + denylist), the aggregation strategy, and the merge algorithm.

## Common Pitfalls

### Pitfall 1: Cilium Go Module Replace Directives
**What goes wrong:** `go get github.com/cilium/cilium@v1.19.1` fails or builds fail with version conflicts.
**Why it happens:** Cilium's go.mod has `replace` directives that are only applied in the main module.
**How to avoid:** Copy both replace directives from Cilium v1.19.1 go.mod into your project's go.mod immediately after `go get`.
**Warning signs:** Build errors mentioning `sigs.k8s.io/controller-tools` or `github.com/osrg/gobgp`.

### Pitfall 2: Go Version Mismatch
**What goes wrong:** `go mod tidy` fails or compilation errors.
**Why it happens:** Cilium v1.19.1 requires `go 1.25.0` in its go.mod. Using an older Go version will fail.
**How to avoid:** Ensure Go >= 1.25 is installed. The local system currently has Go 1.22.2 -- must upgrade.
**Warning signs:** `go: go.mod requires go >= 1.25.0 (running go 1.22.2)`.

### Pitfall 3: TrafficDirection Semantics
**What goes wrong:** Policy allows wrong direction or is applied to wrong endpoint.
**Why it happens:** A dropped ingress flow means the *destination* endpoint needs an ingress allow rule. A dropped egress flow means the *source* endpoint needs an egress allow rule.
**How to avoid:** For INGRESS flows: endpointSelector = destination labels, fromEndpoints = source labels. For EGRESS flows: endpointSelector = source labels, toEndpoints = destination labels.
**Warning signs:** Policies that don't resolve the drop when applied.

### Pitfall 4: Label Source Prefix in Selectors
**What goes wrong:** CiliumNetworkPolicy matchLabels don't match any endpoints.
**Why it happens:** Cilium endpointSelector requires plain keys (`app: nginx`) while Hubble labels include source prefix (`k8s:app=nginx`). If you include `k8s:` in matchLabels, it won't match.
**How to avoid:** Use `labels.ParseLabel()` to strip the source prefix, then use the `Key` and `Value` fields for matchLabels. Or use `api.NewESFromLabels()` which handles this correctly.
**Warning signs:** `cilium policy get` shows the policy but no endpoints match.

### Pitfall 5: Missing Namespace in Cross-Namespace Peer Selectors
**What goes wrong:** Policy allows traffic from/to all pods with matching labels across all namespaces.
**Why it happens:** CiliumNetworkPolicy `fromEndpoints`/`toEndpoints` without namespace label matches all namespaces.
**How to avoid:** Always include `k8s:io.kubernetes.pod.namespace: <ns>` in peer selectors when source and destination are in different namespaces.
**Warning signs:** Overly permissive policies that allow cross-namespace traffic unintentionally.

### Pitfall 6: Port String Type in API
**What goes wrong:** Port number silently ignored or compilation error.
**Why it happens:** `api.PortProtocol.Port` is a `string`, not an integer. The flow proto's `TCP.DestinationPort` is `uint32`.
**How to avoid:** Convert: `Port: strconv.FormatUint(uint64(tcp.DestinationPort), 10)`.
**Warning signs:** Policies with empty port fields.

### Pitfall 7: Nil L4 in Flows
**What goes wrong:** Nil pointer panic when accessing `flow.L4.TCP.DestinationPort`.
**Why it happens:** Some flows may have nil L4 (ARP, ICMP without L4 struct, etc.).
**How to avoid:** Always nil-check `flow.L4`, then `flow.L4.GetTcp()` / `flow.L4.GetUdp()`. Skip flows with no L4 info.
**Warning signs:** Runtime panics on specific flow types.

## Code Examples

### Building a Complete CiliumNetworkPolicy
```go
import (
    ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
    "github.com/cilium/cilium/pkg/policy/api"
    slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/yaml"
)

func Example() {
    cnp := &ciliumv2.CiliumNetworkPolicy{
        TypeMeta: metav1.TypeMeta{
            APIVersion: "cilium.io/v2",
            Kind:       "CiliumNetworkPolicy",
        },
        ObjectMeta: metav1.ObjectMeta{
            Name:      "cpg-nginx",
            Namespace: "default",
            Labels: map[string]string{
                "app.kubernetes.io/managed-by": "cpg",
            },
        },
        Spec: &api.Rule{
            EndpointSelector: api.NewESFromMatchRequirements(
                map[string]string{"app": "nginx"},
                nil,
            ),
            Ingress: []api.IngressRule{
                {
                    IngressCommonRule: api.IngressCommonRule{
                        FromEndpoints: []api.EndpointSelector{
                            api.NewESFromMatchRequirements(
                                map[string]string{
                                    "app":                              "frontend",
                                    "k8s:io.kubernetes.pod.namespace":  "web",
                                },
                                nil,
                            ),
                        },
                    },
                    ToPorts: api.PortRules{
                        {
                            Ports: []api.PortProtocol{
                                {Port: "8080", Protocol: api.ProtoTCP},
                            },
                        },
                    },
                },
            },
        },
    }

    data, _ := yaml.Marshal(cnp)
    // Produces valid kubectl-apply-able YAML
}
```

### Streaming Flows from Hubble Relay
```go
import (
    "context"
    observerpb "github.com/cilium/cilium/api/v1/observer"
    flowpb "github.com/cilium/cilium/api/v1/flow"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func StreamDroppedFlows(ctx context.Context, server string, namespaces []string) (<-chan *flowpb.Flow, error) {
    conn, err := grpc.NewClient(server, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        return nil, err
    }

    client := observerpb.NewObserverClient(conn)

    // Build whitelist filters for dropped verdict
    filters := []*flowpb.FlowFilter{
        {
            Verdict:  []flowpb.Verdict{flowpb.Verdict_DROPPED},
            SourcePod: namespaces, // filter by namespace prefix if needed
        },
    }

    req := &observerpb.GetFlowsRequest{
        Follow:    true,
        Whitelist: filters,
    }

    stream, err := client.GetFlows(ctx, req)
    if err != nil {
        return nil, err
    }

    ch := make(chan *flowpb.Flow, 256)
    go func() {
        defer close(ch)
        for {
            resp, err := stream.Recv()
            if err != nil {
                return
            }
            if resp.GetFlow() != nil {
                ch <- resp.GetFlow()
            }
            if le := resp.GetLostEvents(); le != nil {
                // Handle lost events
            }
        }
    }()
    return ch, nil
}
```

### Parsing Flow Labels with Hierarchy
```go
import "github.com/cilium/cilium/pkg/labels"

var denylist = map[string]bool{
    "pod-template-hash":                    true,
    "controller-revision-hash":             true,
    "statefulset.kubernetes.io/pod-name":   true,
    "job-name":                             true,
    "batch.kubernetes.io/job-name":         true,
    "batch.kubernetes.io/controller-uid":   true,
    "apps.kubernetes.io/pod-index":         true,
}

func SelectLabels(endpointLabels []string) map[string]string {
    var k8sLabels []labels.Label
    for _, raw := range endpointLabels {
        l := labels.ParseLabel(raw)
        if l.Source != labels.LabelSourceK8s {
            continue
        }
        if denylist[l.Key] {
            continue
        }
        k8sLabels = append(k8sLabels, l)
    }

    // Priority hierarchy
    for _, l := range k8sLabels {
        if l.Key == "app.kubernetes.io/name" {
            return map[string]string{l.Key: l.Value}
        }
    }
    for _, l := range k8sLabels {
        if l.Key == "app" {
            return map[string]string{l.Key: l.Value}
        }
    }

    // Fallback: all labels after denylist
    result := make(map[string]string, len(k8sLabels))
    for _, l := range k8sLabels {
        result[l.Key] = l.Value
    }
    return result
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `grpc.Dial()` | `grpc.NewClient()` | grpc-go v1.63 (2024) | `Dial` is deprecated; `NewClient` is non-blocking by default |
| Separate hubble-relay proto | `api/v1/observer` in cilium monorepo | Cilium 1.12+ | Single import path for all Hubble types |
| `gopkg.in/yaml.v2` | `sigs.k8s.io/yaml` | K8s ecosystem 2023+ | JSON-tag-first marshaling, better K8s struct compat |
| golangci-lint v1 config | golangci-lint v2 config | 2025 | `enable-all`/`disable-all` replaced by `linters.default` |

**Deprecated/outdated:**
- `grpc.Dial()` / `grpc.DialContext()`: use `grpc.NewClient()` instead
- `grpc.WithInsecure()`: use `grpc.WithTransportCredentials(insecure.NewCredentials())`
- golangci-lint v1 config format: v2 uses different YAML structure (`linters.default`, `linters.settings`)

## Open Questions

1. **EndpointSelector internal label format**
   - What we know: `api.NewESFromLabels()` takes `labels.Label` with `Source` field. `api.NewESFromMatchRequirements()` takes plain `map[string]string`.
   - What's unclear: Whether the YAML output from `NewESFromLabels` includes `k8s:` prefix in `matchLabels` keys (which Cilium expects) vs plain keys. Need to verify at build time.
   - Recommendation: Write a unit test early that serializes a CiliumNetworkPolicy to YAML and validates the matchLabels keys match what `kubectl apply` expects. If `NewESFromLabels` adds internal prefixes, use `NewESFromMatchRequirements` with plain key-value maps instead.

2. **Workload name derivation from labels**
   - What we know: File naming uses workload name from the same label used in endpointSelector.
   - What's unclear: When falling back to "all labels", what is the workload name? Could be very long or contain special characters.
   - Recommendation: Use the value of the priority label as workload name. For fallback (all labels), use the pod name from `Endpoint.PodName` minus any hash suffix, or the first label value. Define a deterministic fallback in the label selector package.

3. **Cilium v1.19 binary size impact**
   - What we know: User acknowledged potential 40+ MiB binary from monorepo import.
   - What's unclear: Exact size with only the packages we import.
   - Recommendation: Validate at build time. Not a blocker -- explicitly deferred from CI gating.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing (stdlib) + `testify/assert` for readability |
| Config file | None -- Go test infrastructure is zero-config |
| Quick run command | `go test ./pkg/... -short -count=1` |
| Full suite command | `go test ./... -count=1 -race` |

### Phase Requirements -> Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PGEN-01 | Ingress policy from dropped flow | unit | `go test ./pkg/policy/ -run TestBuildIngressPolicy -count=1` | Wave 0 |
| PGEN-02 | Egress policy from dropped flow | unit | `go test ./pkg/policy/ -run TestBuildEgressPolicy -count=1` | Wave 0 |
| PGEN-04 | Smart label selection hierarchy | unit | `go test ./pkg/labels/ -run TestSelectLabels -count=1` | Wave 0 |
| PGEN-05 | Exact port + protocol | unit | `go test ./pkg/policy/ -run TestPortProtocol -count=1` | Wave 0 |
| PGEN-06 | Valid CiliumNetworkPolicy YAML | unit | `go test ./pkg/policy/ -run TestYAMLOutput -count=1` | Wave 0 |
| OUTP-01 | Organized directory output | unit | `go test ./pkg/output/ -run TestDirectoryStructure -count=1` | Wave 0 |
| OUTP-03 | Zap logging with levels | integration | `go test ./cmd/... -run TestLogLevel -count=1` | Wave 0 |
| CONN-01 | gRPC connection to Hubble | integration | `go test ./pkg/hubble/ -run TestConnect -count=1` | Wave 0 |
| OUTP-02 | Streaming policy generation | integration | `go test ./pkg/hubble/ -run TestStreamPipeline -count=1` | Wave 0 |
| CONN-05 | LostEvents warning | unit | `go test ./pkg/hubble/ -run TestLostEvents -count=1` | Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./pkg/... -short -count=1`
- **Per wave merge:** `go test ./... -count=1 -race`
- **Phase gate:** Full suite green before `/gsd:verify-work`

### Wave 0 Gaps
- [ ] `go.mod` / `go.sum` -- project initialization with Cilium dependency
- [ ] `pkg/labels/selector_test.go` -- covers PGEN-04 (label hierarchy + denylist)
- [ ] `pkg/policy/builder_test.go` -- covers PGEN-01, PGEN-02, PGEN-05, PGEN-06
- [ ] `pkg/policy/merge_test.go` -- covers merge-on-write behavior
- [ ] `pkg/output/writer_test.go` -- covers OUTP-01
- [ ] `pkg/hubble/client_test.go` -- covers CONN-01 (mock gRPC server)
- [ ] Test fixtures: sample `flow.Flow` proto structs for ingress/egress/various label scenarios
- [ ] `testify/assert` dependency: `go get github.com/stretchr/testify`

## Sources

### Primary (HIGH confidence)
- [pkg.go.dev/github.com/cilium/cilium/pkg/policy/api](https://pkg.go.dev/github.com/cilium/cilium/pkg/policy/api) -- Rule, IngressRule, EgressRule, EndpointSelector types
- [pkg.go.dev/github.com/cilium/cilium/api/v1/flow](https://pkg.go.dev/github.com/cilium/cilium/api/v1/flow) -- Flow, Endpoint, Layer4, Verdict, TrafficDirection types
- [pkg.go.dev/github.com/cilium/cilium/api/v1/observer](https://pkg.go.dev/github.com/cilium/cilium/api/v1/observer) -- ObserverClient, GetFlowsRequest, FlowFilter
- [pkg.go.dev/github.com/cilium/cilium/pkg/labels](https://pkg.go.dev/github.com/cilium/cilium/pkg/labels) -- ParseLabel, Label, LabelSourceK8s
- [pkg.go.dev/github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2](https://pkg.go.dev/github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2) -- CiliumNetworkPolicy struct
- Cilium v1.19.1 go.mod (raw GitHub) -- replace directives, Go version requirement
- Cilium v1.17.12 go.mod (raw GitHub) -- alternative Go version comparison

### Secondary (MEDIUM confidence)
- [Cilium Network Policy docs](https://docs.cilium.io/en/stable/network/kubernetes/policy/) -- Policy YAML structure, endpointSelector behavior
- [Hubble internals docs](https://docs.cilium.io/en/stable/internals/hubble/) -- GetFlows ring buffer, streaming architecture
- [golangci-lint v2 configuration](https://golangci-lint.run/docs/configuration/) -- v2 config format changes

### Tertiary (LOW confidence)
- WebSearch results on Cilium monorepo import challenges -- replace directive sync requirement (verified with go.mod)
- WebSearch results on zap performance claims -- 4-20x faster (multiple sources agree)

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- all libraries verified via pkg.go.dev and go.mod
- Architecture: HIGH -- channel pipeline is idiomatic Go, Cilium types well-documented
- Pitfalls: HIGH -- verified via official docs and Go package API signatures
- Label parsing: MEDIUM -- `ParseLabel` format verified, but EndpointSelector YAML output needs build-time validation (Open Question 1)

**Research date:** 2026-03-08
**Valid until:** 2026-04-08 (stable domain, Cilium releases monthly patches but API is stable)
