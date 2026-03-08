# Phase 2: Hubble Streaming Pipeline - Research

**Researched:** 2026-03-08
**Domain:** Hubble Relay gRPC streaming, flow filtering, channel pipeline architecture (Go)
**Confidence:** HIGH

## Summary

Phase 2 implements the streaming pipeline that connects `cpg generate` to a live Hubble Relay instance. Phase 1 delivered all domain logic (policy builder, label selector, merge, output writer) and the CLI skeleton with a stub `runGenerate` returning "not yet implemented." This phase wires the real gRPC client, flow filtering, temporal aggregation, graceful shutdown, and LostEvents detection.

The scope is well-defined: create `pkg/hubble/` with a gRPC client, build a channel pipeline (source -> aggregator -> sink), handle SIGINT/SIGTERM for graceful flush, and implement LostEvents aggregated warnings. All downstream components (policy builder, merge, writer) already exist and are tested.

**Primary recommendation:** Build `pkg/hubble/client.go` as a thin wrapper around `observer.NewObserverClient` with `GetFlows(Follow: true)`. Use an `errgroup` to run three goroutines (streamer, aggregator, writer) connected by typed channels. Reuse existing `policy.BuildPolicy()` and `output.Writer` unchanged.

<phase_requirements>

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| CONN-01 | Connect to Hubble Relay via gRPC using cilium/cilium observer proto | `grpc.NewClient()` + `observer.NewObserverClient()` + `GetFlows()` streaming RPC; gRPC already a transitive dep via Cilium |
| CONN-03 | User can override relay address with `--server` flag | Already implemented in Phase 1 `generate.go` as required Cobra flag |
| CONN-04 | User can filter by namespace or all namespaces | `FlowFilter.SourcePod` and `FlowFilter.DestinationPod` accept namespace prefixes like `"production/"`. Two whitelist filters needed (one for source, one for destination) |
| CONN-05 | Detect and warn about LostEvents from ring buffer overflow | `GetFlowsResponse.GetLostEvents()` returns `flow.LostEvent` with `NumEventsLost` and `Source` fields; aggregate and warn every 30s |
| OUTP-02 | Generate policies continuously as new dropped flows arrive (streaming) | `Follow: true` in `GetFlowsRequest`, channel pipeline with temporal aggregation via ticker, flush to existing `output.Writer` |

</phase_requirements>

## Standard Stack

### Core (already in go.mod)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/cilium/cilium` | v1.19.1 | Observer gRPC client, flow proto, policy types | Already a dependency from Phase 1 |
| `github.com/cilium/cilium/api/v1/observer` | via cilium | `ObserverClient`, `GetFlowsRequest`, `FlowFilter` | Native Hubble streaming API |
| `github.com/cilium/cilium/api/v1/flow` | via cilium | `Flow`, `FlowFilter`, `Verdict`, `LostEvent` | Flow and filter proto types |
| `go.uber.org/zap` | v1.27.1 | Structured logging | Already a dependency from Phase 1 |
| `github.com/spf13/cobra` | v1.10.2 | CLI framework | Already a dependency from Phase 1 |

### New Direct Dependencies
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `google.golang.org/grpc` | v1.78+ (via cilium) | gRPC client dial, transport credentials | Hubble Relay connection; promote from indirect to direct |
| `google.golang.org/grpc/credentials/insecure` | via grpc | Insecure transport credentials | Default connection mode (no TLS) |
| `google.golang.org/grpc/credentials` | via grpc | TLS transport credentials | When `--tls` flag is set |
| `golang.org/x/sync/errgroup` | v0.19.0 (already indirect) | Goroutine lifecycle management | Pipeline goroutine coordination; promote from indirect to direct |

### Existing Internal Packages (reuse unchanged)
| Package | Purpose | Phase 2 Role |
|---------|---------|-------------|
| `pkg/policy` | `BuildPolicy()`, `MergePolicy()`, `PolicyEvent` | Aggregator calls `BuildPolicy()`, writer uses `PolicyEvent` |
| `pkg/labels` | `WorkloadName()`, `SelectLabels()` | Aggregator derives workload name from flow labels |
| `pkg/output` | `Writer.Write()` | Sink stage writes policies to disk |

**Installation:**
```bash
# Promote transitive deps to direct (needed for import)
go get google.golang.org/grpc
go get golang.org/x/sync
```

## Architecture Patterns

### New Package Structure
```
pkg/
├── hubble/
│   ├── client.go            # gRPC connection + GetFlows streaming
│   ├── client_test.go       # Tests with mock gRPC server
│   ├── aggregator.go        # Temporal flow aggregation with flush
│   ├── aggregator_test.go
│   ├── pipeline.go          # Orchestrates streamer -> aggregator -> writer
│   └── pipeline_test.go
├── labels/                  # (existing, unchanged)
├── policy/                  # (existing, unchanged)
└── output/                  # (existing, unchanged)
```

### Pattern 1: Hubble gRPC Client with Reconnection
**What:** Thin wrapper around `observer.ObserverClient` that handles connection setup, flow streaming, and automatic reconnection with exponential backoff.
**When to use:** Single entry point for the streaming source stage.
```go
// Source: Cilium Observer API (api/v1/observer)
type Client struct {
    server    string
    tlsEnabled bool
    timeout   time.Duration
    logger    *zap.Logger
}

func (c *Client) StreamDroppedFlows(ctx context.Context, namespaces []string, allNS bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
    conn, err := grpc.NewClient(c.server,
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        return nil, nil, fmt.Errorf("creating gRPC client: %w", err)
    }

    client := observerpb.NewObserverClient(conn)

    req := &observerpb.GetFlowsRequest{
        Follow: true,
        Whitelist: buildFilters(namespaces, allNS),
    }

    stream, err := client.GetFlows(ctx, req)
    if err != nil {
        conn.Close()
        return nil, nil, fmt.Errorf("starting flow stream: %w", err)
    }

    flows := make(chan *flowpb.Flow, 256)
    lostEvents := make(chan *flowpb.LostEvent, 16)

    go func() {
        defer close(flows)
        defer close(lostEvents)
        defer conn.Close()
        for {
            resp, err := stream.Recv()
            if err != nil {
                return // context cancelled or stream error
            }
            if f := resp.GetFlow(); f != nil {
                flows <- f
            }
            if le := resp.GetLostEvents(); le != nil {
                lostEvents <- le
            }
        }
    }()

    return flows, lostEvents, nil
}
```

### Pattern 2: FlowFilter Construction for Namespace Filtering
**What:** Build `FlowFilter` whitelist entries to filter dropped flows by namespace.
**When to use:** When user specifies `--namespace` or `--all-namespaces`.
**Key insight:** `FlowFilter.SourcePod` and `FlowFilter.DestinationPod` accept namespace prefixes (e.g., `"kube-system/"` matches all pods in `kube-system`). For namespace-only filtering, use `"<namespace>/"` as a prefix. For all namespaces, omit these fields entirely.
```go
// Source: Cilium flow proto docs (FlowFilter.source_pod, FlowFilter.destination_pod)
func buildFilters(namespaces []string, allNS bool) []*flowpb.FlowFilter {
    if allNS || len(namespaces) == 0 {
        // No namespace filtering -- only filter by verdict
        return []*flowpb.FlowFilter{
            {Verdict: []flowpb.Verdict{flowpb.Verdict_DROPPED}},
        }
    }

    // Build namespace prefixes (e.g., "production/" matches all pods in "production")
    prefixes := make([]string, len(namespaces))
    for i, ns := range namespaces {
        prefixes[i] = ns + "/"
    }

    // Need two filters: one for source, one for destination
    // FlowFilter fields are AND-ed within a filter, but multiple whitelist
    // filters are OR-ed. We want flows where EITHER source OR destination
    // is in the target namespace(s).
    return []*flowpb.FlowFilter{
        {
            Verdict:   []flowpb.Verdict{flowpb.Verdict_DROPPED},
            SourcePod: prefixes,
        },
        {
            Verdict:        []flowpb.Verdict{flowpb.Verdict_DROPPED},
            DestinationPod: prefixes,
        },
    }
}
```

### Pattern 3: errgroup Pipeline Orchestration
**What:** Three-stage pipeline using `errgroup.WithContext` for goroutine lifecycle management.
**When to use:** The main `runGenerate` function.
```go
func runPipeline(ctx context.Context, cfg Config, logger *zap.Logger) error {
    // Setup signal handling for graceful shutdown
    ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
    defer cancel()

    client := hubble.NewClient(cfg.Server, cfg.TLS, cfg.Timeout, logger)
    writer := output.NewWriter(cfg.OutputDir, logger)
    agg := hubble.NewAggregator(cfg.FlushInterval, logger)

    flows, lostEvents, err := client.StreamDroppedFlows(ctx, cfg.Namespaces, cfg.AllNamespaces)
    if err != nil {
        return err
    }

    policies := make(chan policy.PolicyEvent, 64)

    g, ctx := errgroup.WithContext(ctx)

    // Stage 1: Aggregate flows and build policies
    g.Go(func() error {
        return agg.Run(ctx, flows, policies)
    })

    // Stage 2: Write policies to disk
    g.Go(func() error {
        return writePolicies(ctx, policies, writer, logger)
    })

    // Stage 3: Monitor lost events
    g.Go(func() error {
        return monitorLostEvents(ctx, lostEvents, logger)
    })

    return g.Wait()
}
```

### Pattern 4: Temporal Aggregation with Graceful Flush
**What:** Accumulate flows by `(namespace, workload, direction)` key, flush on ticker or context cancellation.
**When to use:** Aggregator stage between flow stream and policy writer.
```go
type Aggregator struct {
    interval time.Duration
    logger   *zap.Logger
}

type AggKey struct {
    Namespace string
    Workload  string
}

func (a *Aggregator) Run(ctx context.Context, in <-chan *flowpb.Flow, out chan<- policy.PolicyEvent) error {
    defer close(out)

    buckets := make(map[AggKey][]*flowpb.Flow)
    ticker := time.NewTicker(a.interval)
    defer ticker.Stop()

    for {
        select {
        case f, ok := <-in:
            if !ok {
                // Stream ended -- flush remaining
                a.flush(buckets, out)
                return nil
            }
            key := a.keyFromFlow(f)
            buckets[key] = append(buckets[key], f)

        case <-ticker.C:
            a.flush(buckets, out)

        case <-ctx.Done():
            // Graceful shutdown: flush all remaining flows
            a.flush(buckets, out)
            return nil
        }
    }
}

func (a *Aggregator) keyFromFlow(f *flowpb.Flow) AggKey {
    // For INGRESS: destination is the policy target
    // For EGRESS: source is the policy target
    var ep *flowpb.Endpoint
    switch f.TrafficDirection {
    case flowpb.TrafficDirection_INGRESS:
        ep = f.Destination
    case flowpb.TrafficDirection_EGRESS:
        ep = f.Source
    }
    if ep == nil {
        return AggKey{Namespace: "unknown", Workload: "unknown"}
    }
    return AggKey{
        Namespace: ep.Namespace,
        Workload:  labels.WorkloadName(ep.Labels),
    }
}

func (a *Aggregator) flush(buckets map[AggKey][]*flowpb.Flow, out chan<- policy.PolicyEvent) {
    for key, flows := range buckets {
        cnp := policy.BuildPolicy(key.Namespace, key.Workload, flows)
        out <- policy.PolicyEvent{
            Namespace: key.Namespace,
            Workload:  key.Workload,
            Policy:    cnp,
        }
    }
    // Clear buckets (reuse map)
    for k := range buckets {
        delete(buckets, k)
    }
}
```

### Pattern 5: LostEvents Aggregated Warning
**What:** Collect LostEvents and log an aggregated warning every 30 seconds to avoid log spam.
**When to use:** Monitoring goroutine alongside the main pipeline.
```go
func monitorLostEvents(ctx context.Context, ch <-chan *flowpb.LostEvent, logger *zap.Logger) error {
    var totalLost uint64
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    var periodLost uint64
    for {
        select {
        case le, ok := <-ch:
            if !ok {
                return nil
            }
            periodLost += le.NumEventsLost
            totalLost += le.NumEventsLost

        case <-ticker.C:
            if periodLost > 0 {
                logger.Warn("hubble events lost — consider increasing ring buffer size",
                    zap.Uint64("lost_this_period", periodLost),
                    zap.Uint64("total_lost", totalLost),
                )
                periodLost = 0
            }

        case <-ctx.Done():
            if totalLost > 0 {
                logger.Warn("total hubble events lost during session",
                    zap.Uint64("total_lost", totalLost),
                )
            }
            return nil
        }
    }
}
```

### Pattern 6: Session Summary on Shutdown
**What:** Print a summary of the session on graceful shutdown (SIGINT/SIGTERM).
**When to use:** After `errgroup.Wait()` returns in `runGenerate`.
```go
type SessionStats struct {
    StartTime       time.Time
    FlowsSeen       uint64
    PoliciesCreated uint64
    PoliciesUpdated uint64
    FlowsSkipped    uint64
    LostEvents      uint64
    OutputDir       string
}

func (s *SessionStats) Log(logger *zap.Logger) {
    logger.Info("session summary",
        zap.Duration("duration", time.Since(s.StartTime)),
        zap.Uint64("flows_seen", s.FlowsSeen),
        zap.Uint64("policies_created", s.PoliciesCreated),
        zap.Uint64("policies_updated", s.PoliciesUpdated),
        zap.Uint64("flows_skipped", s.FlowsSkipped),
        zap.Uint64("lost_events", s.LostEvents),
        zap.String("output_dir", s.OutputDir),
    )
}
```

### Anti-Patterns to Avoid
- **Blocking on channel send without context check:** Always use `select` with `ctx.Done()` alongside channel sends to prevent goroutine leaks.
- **Not closing channels:** The producer goroutine MUST close its output channel; the consumer detects stream end via `ok` check on receive.
- **Reconnection in the aggregator:** Keep reconnection logic in the client layer only. The aggregator should not know about gRPC.
- **Unbounded flow accumulation:** Always flush on ticker. Without a ticker, memory grows unbounded if no flows arrive for a long time (edge case: flows arrive continuously but never trigger flush).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Goroutine lifecycle | Manual WaitGroup + panic recovery | `errgroup.WithContext` | Automatic context cancellation on first error, cleaner API |
| Signal handling | Manual `os.Signal` channel | `signal.NotifyContext` | Returns a context that cancels on signal; composes with errgroup |
| gRPC connection | Raw TCP + HTTP/2 | `grpc.NewClient()` | Handles HTTP/2, keepalive, backpressure, reconnection |
| Exponential backoff | Manual sleep loop | Simple helper with `time.Duration` math | Must handle jitter, max cap, context cancellation |
| Flow-to-policy transform | New code | Existing `policy.BuildPolicy()` | Already implemented and tested in Phase 1 |
| Policy merge on write | New code | Existing `output.Writer.Write()` | Already handles read-modify-write with merge |

## Common Pitfalls

### Pitfall 1: FlowFilter AND vs OR Semantics
**What goes wrong:** Setting both `SourcePod` and `DestinationPod` in the same `FlowFilter` requires BOTH to match (AND semantics within a filter). This means flows where only the source OR destination is in the target namespace are missed.
**Why it happens:** Multiple fields within a single `FlowFilter` are AND-ed together. Multiple `FlowFilter` entries in the whitelist are OR-ed.
**How to avoid:** Use two separate `FlowFilter` entries in the whitelist: one filtering `SourcePod` by namespace prefix, another filtering `DestinationPod` by namespace prefix. Both include `Verdict: DROPPED`.
**Warning signs:** Missing flows when filtering by namespace.

### Pitfall 2: Namespace Prefix Format in FlowFilter
**What goes wrong:** Namespace filter matches nothing or matches wrong pods.
**Why it happens:** `FlowFilter.SourcePod` uses `"namespace/podname"` format. For namespace-only filtering, use `"namespace/"` (trailing slash). Without the slash, `"prod"` would match `"production/"` too.
**How to avoid:** Always append `/` to namespace names when building pod prefixes: `ns + "/"`.
**Warning signs:** Unexpected flows from other namespaces or no flows at all.

### Pitfall 3: grpc.NewClient is Non-Blocking
**What goes wrong:** `grpc.NewClient()` succeeds even when the server is unreachable. Connection errors only surface on the first RPC call (`GetFlows`).
**Why it happens:** `grpc.NewClient()` (unlike deprecated `grpc.Dial`) is lazy by default. It does not establish a connection until needed.
**How to avoid:** Handle connection errors at `client.GetFlows()` call time. Implement retry with backoff at this level. Consider a pre-flight connectivity check with a short timeout using `conn.Connect()` or by calling `GetFlows` immediately.
**Warning signs:** Silent startup with no connection error, then failure when streaming starts.

### Pitfall 4: Channel Deadlock on Graceful Shutdown
**What goes wrong:** Pipeline hangs on shutdown because a goroutine is blocked writing to a full channel while the consumer has already exited.
**Why it happens:** Context cancellation stops the consumer, but the producer still has data to flush.
**How to avoid:** Use buffered channels. Ensure flush logic uses `select` with `ctx.Done()` on channel sends. Close channels from the producer side only.
**Warning signs:** `cpg generate` hangs on Ctrl+C instead of printing summary and exiting.

### Pitfall 5: Missing gRPC Direct Dependency
**What goes wrong:** Compilation error when importing `google.golang.org/grpc` because it's only an indirect dependency.
**Why it happens:** Phase 1 never imported gRPC directly; it came transitively via Cilium.
**How to avoid:** Run `go get google.golang.org/grpc` to promote to direct dependency before building.
**Warning signs:** `cannot find module providing package google.golang.org/grpc`.

### Pitfall 6: LostEvent.NumEventsLost is uint64, Not int
**What goes wrong:** Potential overflow or sign issues if cast to int.
**Why it happens:** The proto field is `uint64`.
**How to avoid:** Use `uint64` consistently for lost event counters. Zap has `zap.Uint64()` for logging.
**Warning signs:** Negative numbers in lost event logs.

### Pitfall 7: Flow Without Namespace
**What goes wrong:** Empty namespace in `AggKey` causes policies written to root of output dir or empty directory name.
**Why it happens:** Some flows (e.g., host-level traffic, health probes) may have empty `Endpoint.Namespace`.
**How to avoid:** Skip flows where the target endpoint has an empty namespace. Log at debug level.
**Warning signs:** Policy files created at `<output-dir>/.yaml` or `<output-dir>//<workload>.yaml`.

## Code Examples

### Complete runGenerate Implementation Pattern
```go
// Source: Phase 1 generate.go stub + Phase 2 pipeline pattern
func runGenerate(cmd *cobra.Command, _ []string) error {
    server, _ := cmd.Flags().GetString("server")
    namespaces, _ := cmd.Flags().GetStringSlice("namespace")
    allNamespaces, _ := cmd.Flags().GetBool("all-namespaces")
    outputDir, _ := cmd.Flags().GetString("output-dir")
    tlsEnabled, _ := cmd.Flags().GetBool("tls")
    flushInterval, _ := cmd.Flags().GetDuration("flush-interval")
    timeout, _ := cmd.Flags().GetDuration("timeout")

    if len(namespaces) > 0 && allNamespaces {
        return fmt.Errorf("--namespace and --all-namespaces are mutually exclusive")
    }

    // Setup signal-aware context
    ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
    defer cancel()

    stats := &SessionStats{StartTime: time.Now(), OutputDir: outputDir}

    client := hubble.NewClient(server, tlsEnabled, timeout, logger)
    writer := output.NewWriter(outputDir, logger)
    agg := hubble.NewAggregator(flushInterval, logger)

    flows, lostEvents, err := client.StreamDroppedFlows(ctx, namespaces, allNamespaces)
    if err != nil {
        return fmt.Errorf("connecting to Hubble Relay: %w", err)
    }

    logger.Info("connected to Hubble Relay, streaming dropped flows",
        zap.String("server", server),
    )

    policies := make(chan policy.PolicyEvent, 64)

    g, ctx := errgroup.WithContext(ctx)
    g.Go(func() error { return agg.Run(ctx, flows, policies, stats) })
    g.Go(func() error { return writePolicies(ctx, policies, writer, stats, logger) })
    g.Go(func() error { return monitorLostEvents(ctx, lostEvents, stats, logger) })

    err = g.Wait()
    stats.Log(logger)
    return err
}
```

### Exponential Backoff for Reconnection
```go
func (c *Client) StreamWithRetry(ctx context.Context, namespaces []string, allNS bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
    flows := make(chan *flowpb.Flow, 256)
    lostEvents := make(chan *flowpb.LostEvent, 16)

    go func() {
        defer close(flows)
        defer close(lostEvents)

        backoff := time.Second
        const maxBackoff = 60 * time.Second

        for {
            err := c.streamOnce(ctx, namespaces, allNS, flows, lostEvents)
            if ctx.Err() != nil {
                return // context cancelled, clean shutdown
            }

            c.logger.Warn("hubble stream disconnected, reconnecting",
                zap.Error(err),
                zap.Duration("backoff", backoff),
            )

            select {
            case <-time.After(backoff):
                backoff = min(backoff*2, maxBackoff)
            case <-ctx.Done():
                return
            }
        }
    }()

    return flows, lostEvents, nil
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `grpc.Dial()` | `grpc.NewClient()` | grpc-go v1.63 (2024) | Non-blocking by default, `Dial` is deprecated |
| `grpc.WithInsecure()` | `grpc.WithTransportCredentials(insecure.NewCredentials())` | grpc-go v1.40 (2021) | Old API is deprecated |
| `os.Signal` channel + `signal.Notify` | `signal.NotifyContext` (Go 1.16+) | 2021 | Returns context directly, composes with errgroup |
| `sync.WaitGroup` for goroutine mgmt | `errgroup.WithContext` | Mature since 2020+ | Error propagation + context cancellation built in |

## Open Questions

1. **TLS credential configuration**
   - What we know: `--tls` flag enables TLS but no cert flags exist (deferred to future phases)
   - What's unclear: Whether system cert pool is sufficient for most Hubble Relay deployments (it should be for clusters with proper CA)
   - Recommendation: Use `credentials.NewTLS(&tls.Config{})` which loads system cert pool by default. Sufficient for now; cert flags are deferred.

2. **Connection timeout behavior**
   - What we know: `--timeout` flag exists (10s default). `grpc.NewClient` is non-blocking.
   - What's unclear: Best way to implement a connection timeout when the client is lazy.
   - Recommendation: Use `context.WithTimeout` around the first `GetFlows` call. If it times out, return a clear error. Alternative: use `grpc.WithBlock()` dial option (deprecated but functional) -- avoid this.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing (stdlib) + `testify/assert` |
| Config file | None -- Go test infrastructure is zero-config |
| Quick run command | `go test ./pkg/hubble/... -short -count=1` |
| Full suite command | `go test ./... -count=1 -race` |

### Phase Requirements -> Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CONN-01 | gRPC connection to Hubble Relay | integration | `go test ./pkg/hubble/ -run TestClient -count=1` | Wave 0 |
| CONN-03 | --server flag override | unit | `go test ./cmd/... -run TestServerFlag -count=1` | Existing (Phase 1 CLI) |
| CONN-04 | Namespace filtering via FlowFilter | unit | `go test ./pkg/hubble/ -run TestBuildFilters -count=1` | Wave 0 |
| CONN-05 | LostEvents aggregated warning | unit | `go test ./pkg/hubble/ -run TestLostEvents -count=1` | Wave 0 |
| OUTP-02 | Continuous streaming policy generation | integration | `go test ./pkg/hubble/ -run TestPipeline -count=1` | Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./pkg/hubble/... -short -count=1`
- **Per wave merge:** `go test ./... -count=1 -race`
- **Phase gate:** Full suite green before `/gsd:verify-work`

### Wave 0 Gaps
- [ ] `pkg/hubble/client.go` -- gRPC client wrapper (new file)
- [ ] `pkg/hubble/client_test.go` -- covers CONN-01 with mock gRPC server
- [ ] `pkg/hubble/aggregator.go` -- temporal aggregation (new file)
- [ ] `pkg/hubble/aggregator_test.go` -- covers aggregation + flush behavior
- [ ] `pkg/hubble/pipeline.go` -- errgroup orchestration (new file)
- [ ] `pkg/hubble/pipeline_test.go` -- covers OUTP-02 end-to-end with mocks
- [ ] Test helper: mock `observer.ObserverClient` using gRPC `bufconn` or interface-based mock

## Sources

### Primary (HIGH confidence)
- [Cilium Observer Proto](https://docs.cilium.io/en/stable/_api/v1/observer/README/) -- GetFlowsRequest, GetFlowsResponse, FlowFilter fields
- [Cilium Flow Proto](https://docs.cilium.io/en/stable/_api/v1/flow/README/) -- FlowFilter.source_pod/destination_pod namespace prefix format, LostEvent struct
- [Hubble Internals](https://docs.cilium.io/en/stable/internals/hubble/) -- Ring buffer, GetFlows streaming, allow/deny filter semantics
- [pkg.go.dev/github.com/cilium/cilium/api/v1/observer](https://pkg.go.dev/github.com/cilium/cilium/api/v1/observer) -- Go API surface
- Phase 1 codebase (existing `pkg/policy/`, `pkg/labels/`, `pkg/output/`, `cmd/cpg/`) -- verified by reading source

### Secondary (MEDIUM confidence)
- [gRPC Go NewClient migration](https://pkg.go.dev/google.golang.org/grpc) -- NewClient vs deprecated Dial behavior

### Tertiary (LOW confidence)
- None -- all findings verified against official sources

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- all dependencies already in go.mod or verified transitive
- Architecture: HIGH -- channel pipeline is idiomatic Go, errgroup is standard pattern
- FlowFilter semantics: HIGH -- verified via official proto docs (AND within filter, OR across filters)
- Pitfalls: HIGH -- verified via official docs and gRPC migration guides
- LostEvents: HIGH -- verified proto field structure via official flow proto docs

**Research date:** 2026-03-08
**Valid until:** 2026-04-08 (stable domain, Hubble gRPC API is stable v1.0 for Cilium 1.x lifecycle)
