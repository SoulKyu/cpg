# Design: Unhandled Flows Tracker

**Date:** 2026-03-11
**Status:** Accepted

## Problem

CPG silently drops Hubble flows it cannot convert to CiliumNetworkPolicy rules. Users have no visibility into what is being ignored, making it hard to understand coverage gaps.

Two filtering layers drop flows without reporting:
- **Aggregator** (`pkg/hubble/aggregator.go`): nil endpoint, empty namespace — logged at DEBUG but inconsistently
- **Policy builder** (`pkg/policy/builder.go`): no L4, nil source/dest, unknown protocol, world traffic without IP — completely silent

## Solution

A centralized `UnhandledTracker` component in `pkg/hubble/unhandled.go` that:
- Receives skipped flows from both the aggregator and policy builder
- Deduplicates per unique flow (src + dst + port + proto + reason)
- Emits a structured INFO summary at each pipeline flush cycle and at shutdown
- Emits individual DEBUG logs for each new unique flow (first occurrence only)

## Architecture

### UnhandledTracker struct

```go
// pkg/hubble/unhandled.go

type UnhandledTracker struct {
    mu       sync.Mutex
    seen     map[string]struct{}    // dedup key = src+dst+port+proto+reason
    counters map[string]int64       // counters by reason
    logger   *zap.Logger
}

func (t *UnhandledTracker) Track(flow *flowpb.Flow, reason string)
func (t *UnhandledTracker) Flush()
```

### Track() behavior

1. Compute dedup key from flow identity + reason
2. If key not in `seen`:
   - Add to `seen`
   - Emit DEBUG log with: src, dst, port, proto, reason, dst_labels
3. Increment counter for the reason (always, even if already seen)

### Flush() behavior

1. Emit one INFO log with all reason counters > 0
2. Reset counters to zero
3. Do NOT reset `seen` map — each unique flow logs DEBUG only once for the entire process lifetime

### Dedup key

`source_identity + dest_identity + port/protocol + reason` — most granular, allows per-flow investigation in DEBUG while the INFO summary aggregates by reason.

### Memory management

No reset of the `seen` map. Memory is bounded by the number of unique flow combinations in the cluster, which is finite and typically small.

Thread-safe via mutex for future concurrency.

## Skip reasons

### Policy builder (`pkg/policy/builder.go`)

| Reason | Condition |
|--------|-----------|
| `no_l4` | `f.L4 == nil` — no protocol info |
| `nil_source` | Source endpoint nil in `buildIngressRule` |
| `nil_destination` | Dest endpoint nil in `buildEgressRule` |
| `unknown_protocol` | `extractProto()` returns nil |
| `world_no_ip` | World traffic without exploitable IP |

### Aggregator (`pkg/hubble/aggregator.go`)

| Reason | Condition |
|--------|-----------|
| `nil_endpoint` | Source/dest endpoint is nil |
| `empty_namespace` | Namespace empty, non-reserved identity |

Existing reserved identity warnings (WARN level) remain unchanged — they are actionable and already well-deduplicated.

## Pipeline integration

```
Flow entrant
    |
    +-- Aggregator: endpoint nil / namespace vide ?
    |     YES -> tracker.Track(flow, reason)
    |     NO  -> continue to bucketing
    |
    +-- Policy Builder: L4 nil / endpoint nil / proto unknown / world no IP ?
    |     YES -> tracker.Track(flow, reason)
    |     NO  -> generate rule
    |
    +-- End of flush cycle
          +-- Write policy YAML files
          +-- tracker.Flush() -> INFO structured summary
```

**Flush points:**
- After each policy write cycle in the pipeline
- On program shutdown (defer or SIGTERM handler)

**Injection:** `UnhandledTracker` created once in `pipeline.go`, passed to aggregator and policy builder via constructors.

## Example output

### Normal usage (INFO)

```
INFO  Policies written         {"namespace": "default", "count": 3}
INFO  Unhandled flows summary  {"no_l4": 42, "nil_endpoint": 8, "world_no_ip": 3, "period": "flush"}
```

### Debug mode (DEBUG + INFO)

```
DEBUG Unhandled flow  {"src": "default/nginx", "dst": "kube-system/coredns", "port": 53, "proto": "UDP", "reason": "no_l4", "dst_labels": ["k8s:app=coredns"]}
DEBUG Unhandled flow  {"src": "default/nginx", "dst": "reserved:world", "port": 443, "proto": "TCP", "reason": "world_no_ip", "dst_labels": ["reserved:world"]}
...
INFO  Unhandled flows summary  {"no_l4": 42, "nil_endpoint": 8, "world_no_ip": 3, "period": "flush"}
```

### Shutdown (SIGTERM)

```
INFO  Shutting down
INFO  Unhandled flows summary  {"no_l4": 12, "world_no_ip": 1, "period": "final"}
```

## Testing strategy

File: `pkg/hubble/unhandled_test.go`

| Test | Validates |
|------|-----------|
| `TestTrack_Dedup` | Same flow tracked twice, only one DEBUG log emitted |
| `TestTrack_DifferentFlows` | Two distinct flows, two DEBUG logs |
| `TestFlush_EmitsSummary` | INFO log with correct structured counters |
| `TestFlush_ResetsCountersNotSeen` | After flush, counters are 0 but `seen` map persists |
| `TestFlush_SkipsZeroCounters` | No field in log for reasons with zero count |

Tests use `zap/zaptest/observer` to capture and assert emitted logs.

## README documentation

Add a "Unhandled Flows" section to README covering:
- What unhandled flows are and why they occur
- List of skip reasons with short descriptions
- How to enable debug mode (`--debug` / `--log-level debug`) for per-flow detail
- Example INFO and DEBUG output

## What does NOT change

- Reserved identity warnings (WARN) in aggregator — separate, well-targeted mechanism
- Lost Hubble events reporting in aggregator
- Policy generation flow
