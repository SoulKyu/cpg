# Unhandled Flows Tracker Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Centralized reporting of Hubble flows that CPG cannot convert to CiliumNetworkPolicy, with dedup and periodic structured summaries.

**Architecture:** A new `UnhandledTracker` in `pkg/hubble/unhandled.go` receives skipped flows from both the aggregator and policy builder. It deduplicates by flow identity+reason, emits DEBUG logs per unique flow, and emits structured INFO summaries at each flush cycle. The tracker is injected into aggregator and builder via the pipeline.

**Tech Stack:** Go, zap logging, Cilium flow protobuf, testify, zap/zaptest/observer

---

### Task 1: UnhandledTracker core — Track() with dedup

**Files:**
- Create: `pkg/hubble/unhandled.go`
- Create: `pkg/hubble/unhandled_test.go`

**Step 1: Write the failing tests**

In `pkg/hubble/unhandled_test.go`:

```go
package hubble

import (
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestTrack_Dedup(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "production",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 8080},
			},
		},
	}

	tracker.Track(flow, "no_l4")
	tracker.Track(flow, "no_l4") // duplicate

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 1, "duplicate flow should only produce one DEBUG log")
}

func TestTrack_DifferentFlows(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow1 := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "production",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 8080},
			},
		},
	}

	flow2 := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=api"},
			Namespace: "staging",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"reserved:world"},
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 443},
			},
		},
	}

	tracker.Track(flow1, "no_l4")
	tracker.Track(flow2, "world_no_ip")

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 2, "different flows should produce separate DEBUG logs")
}

func TestTrack_SameFlowDifferentReason(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "production",
		},
	}

	tracker.Track(flow, "no_l4")
	tracker.Track(flow, "unknown_protocol")

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 2, "same flow with different reasons should produce separate DEBUG logs")
}

// filterLogs returns log entries matching the given level and message substring.
func filterLogs(logs *observer.ObservedLogs, level zapcore.Level, msgSubstring string) []observer.LoggedEntry {
	var result []observer.LoggedEntry
	for _, entry := range logs.All() {
		if entry.Level == level && contains(entry.Message, msgSubstring) {
			result = append(result, entry)
		}
	}
	return result
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/ -run "TestTrack" -v`
Expected: FAIL — `NewUnhandledTracker` not defined

**Step 3: Write minimal implementation**

In `pkg/hubble/unhandled.go`:

```go
package hubble

import (
	"fmt"
	"sync"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"go.uber.org/zap"
)

// UnhandledTracker tracks flows that CPG cannot convert to policy rules.
// It deduplicates by flow identity + reason, emits DEBUG logs for first
// occurrence of each unique flow, and emits periodic INFO summaries.
type UnhandledTracker struct {
	mu       sync.Mutex
	seen     map[string]struct{}
	counters map[string]int64
	logger   *zap.Logger
}

// NewUnhandledTracker creates a new tracker with the given logger.
func NewUnhandledTracker(logger *zap.Logger) *UnhandledTracker {
	return &UnhandledTracker{
		seen:     make(map[string]struct{}),
		counters: make(map[string]int64),
		logger:   logger,
	}
}

// Track records an unhandled flow. On first occurrence (unique src+dst+port+proto+reason),
// it emits a DEBUG log with flow details and destination labels. It always increments the
// reason counter for the periodic summary.
func (t *UnhandledTracker) Track(f *flowpb.Flow, reason string) {
	key := t.dedupKey(f, reason)

	t.mu.Lock()
	defer t.mu.Unlock()

	t.counters[reason]++

	if _, seen := t.seen[key]; seen {
		return
	}
	t.seen[key] = struct{}{}

	src, dst, port, proto, dstLabels := t.extractFields(f)
	t.logger.Debug("unhandled flow",
		zap.String("src", src),
		zap.String("dst", dst),
		zap.String("port", port),
		zap.String("proto", proto),
		zap.String("reason", reason),
		zap.Strings("dst_labels", dstLabels),
	)
}

// dedupKey builds a unique key from the flow's identity fields and reason.
func (t *UnhandledTracker) dedupKey(f *flowpb.Flow, reason string) string {
	src := endpointID(f.Source)
	dst := endpointID(f.Destination)
	port, proto := protoFields(f)
	return fmt.Sprintf("%s|%s|%s|%s|%s", src, dst, port, proto, reason)
}

// extractFields pulls human-readable fields from a flow for DEBUG logging.
func (t *UnhandledTracker) extractFields(f *flowpb.Flow) (src, dst, port, proto string, dstLabels []string) {
	src = endpointID(f.Source)
	dst = endpointID(f.Destination)
	port, proto = protoFields(f)
	if f.Destination != nil {
		dstLabels = f.Destination.Labels
	}
	return
}

// endpointID returns "namespace/workload" or a label-based fallback for an endpoint.
func endpointID(ep *flowpb.Endpoint) string {
	if ep == nil {
		return "<nil>"
	}
	if ep.Namespace != "" {
		name := ep.Namespace
		for _, l := range ep.Labels {
			if len(l) > 6 && l[:6] == "k8s:app=" {
				// won't match, check properly below
			}
		}
		// Use workload name from labels if available
		workload := workloadFromLabels(ep.Labels)
		if workload != "" {
			return name + "/" + workload
		}
		return name + "/<unknown>"
	}
	// No namespace — use first label as identifier
	if len(ep.Labels) > 0 {
		return ep.Labels[0]
	}
	return "<unknown>"
}

// workloadFromLabels extracts a workload name from endpoint labels.
// Mirrors the logic in pkg/labels.WorkloadName but avoids a cross-package dependency.
func workloadFromLabels(lbls []string) string {
	for _, l := range lbls {
		if len(l) > 24 && l[:24] == "k8s:app.kubernetes.io/name=" {
			return l[24:]
		}
	}
	for _, l := range lbls {
		if len(l) > 8 && l[:8] == "k8s:app=" {
			return l[8:]
		}
	}
	return ""
}

// protoFields extracts port and protocol from a flow's L4 layer.
func protoFields(f *flowpb.Flow) (port, proto string) {
	if f.L4 == nil {
		return "0", "unknown"
	}
	if tcp := f.L4.GetTCP(); tcp != nil {
		return fmt.Sprintf("%d", tcp.DestinationPort), "TCP"
	}
	if udp := f.L4.GetUDP(); udp != nil {
		return fmt.Sprintf("%d", udp.DestinationPort), "UDP"
	}
	if icmp4 := f.L4.GetICMPv4(); icmp4 != nil {
		return fmt.Sprintf("%d", icmp4.Type), "ICMPv4"
	}
	if icmp6 := f.L4.GetICMPv6(); icmp6 != nil {
		return fmt.Sprintf("%d", icmp6.Type), "ICMPv6"
	}
	return "0", "unknown"
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/ -run "TestTrack" -v`
Expected: PASS — all 3 tests green

**Step 5: Commit**

```bash
git add pkg/hubble/unhandled.go pkg/hubble/unhandled_test.go
git commit -m "feat: add UnhandledTracker with Track() and dedup logic"
```

---

### Task 2: Flush() with structured INFO summary

**Files:**
- Modify: `pkg/hubble/unhandled.go`
- Modify: `pkg/hubble/unhandled_test.go`

**Step 1: Write the failing tests**

Append to `pkg/hubble/unhandled_test.go`:

```go
func TestFlush_EmitsSummary(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	// Track some flows
	flow1 := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Labels: []string{"k8s:app=a"}, Namespace: "default"},
		Destination:      &flowpb.Endpoint{Labels: []string{"k8s:app=b"}, Namespace: "prod"},
	}
	flow2 := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Source:           &flowpb.Endpoint{Labels: []string{"k8s:app=c"}, Namespace: "staging"},
		Destination:      &flowpb.Endpoint{Labels: []string{"reserved:world"}},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{DestinationPort: 443}},
		},
	}

	tracker.Track(flow1, "no_l4")
	tracker.Track(flow1, "no_l4") // dup — counter still increments
	tracker.Track(flow2, "world_no_ip")

	tracker.Flush()

	infoLogs := filterLogs(logs, zapcore.InfoLevel, "unhandled flows summary")
	require.Len(t, infoLogs, 1)

	// Check structured fields
	fields := fieldMap(infoLogs[0])
	assert.Equal(t, int64(2), fields["no_l4"], "no_l4 counter should be 2 (tracked twice)")
	assert.Equal(t, int64(1), fields["world_no_ip"])
}

func TestFlush_ResetsCountersNotSeen(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Labels: []string{"k8s:app=a"}, Namespace: "default"},
		Destination:      &flowpb.Endpoint{Labels: []string{"k8s:app=b"}, Namespace: "prod"},
	}

	tracker.Track(flow, "no_l4")
	tracker.Flush()

	// Track same flow again — counter increments but no new DEBUG log
	tracker.Track(flow, "no_l4")
	tracker.Flush()

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 1, "seen map should persist — no second DEBUG log")

	infoLogs := filterLogs(logs, zapcore.InfoLevel, "unhandled flows summary")
	require.Len(t, infoLogs, 2, "should have two flush summaries")

	// Both flushes should show count of 1
	assert.Equal(t, int64(1), fieldMap(infoLogs[0])["no_l4"])
	assert.Equal(t, int64(1), fieldMap(infoLogs[1])["no_l4"])
}

func TestFlush_SkipsZeroCounters(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Labels: []string{"k8s:app=a"}, Namespace: "default"},
		Destination:      &flowpb.Endpoint{Labels: []string{"k8s:app=b"}, Namespace: "prod"},
	}

	tracker.Track(flow, "no_l4")
	tracker.Flush()

	// No new tracks — flush should emit nothing
	tracker.Flush()

	infoLogs := filterLogs(logs, zapcore.InfoLevel, "unhandled flows summary")
	assert.Len(t, infoLogs, 1, "flush with zero counters should not emit INFO log")
}

func TestFlush_NoTracksNoLog(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	tracker.Flush()

	infoLogs := filterLogs(logs, zapcore.InfoLevel, "unhandled flows summary")
	assert.Empty(t, infoLogs, "flush with no tracks should not emit INFO log")
}

// fieldMap extracts int64 fields from a log entry into a map.
func fieldMap(entry observer.LoggedEntry) map[string]int64 {
	m := make(map[string]int64)
	for _, f := range entry.Context {
		if f.Type == zapcore.Int64Type {
			m[f.Key] = f.Integer
		}
	}
	return m
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/ -run "TestFlush" -v`
Expected: FAIL — `Flush` method not defined

**Step 3: Write Flush() implementation**

Add to `pkg/hubble/unhandled.go`:

```go
// Flush emits a structured INFO log summarizing unhandled flow counts by reason,
// then resets the counters. The seen map is NOT reset — each unique flow only
// produces one DEBUG log for the entire process lifetime.
// If all counters are zero (no new tracks since last flush), no log is emitted.
func (t *UnhandledTracker) Flush() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Collect non-zero counters
	var fields []zap.Field
	var total int64
	for reason, count := range t.counters {
		if count > 0 {
			fields = append(fields, zap.Int64(reason, count))
			total += count
		}
	}

	if total == 0 {
		return
	}

	t.logger.Info("unhandled flows summary", fields...)

	// Reset counters but keep seen map
	for k := range t.counters {
		t.counters[k] = 0
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/ -run "TestFlush" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/hubble/unhandled.go pkg/hubble/unhandled_test.go
git commit -m "feat: add Flush() with structured INFO summary and counter reset"
```

---

### Task 3: Integrate tracker into aggregator

**Files:**
- Modify: `pkg/hubble/aggregator.go:25-38` (struct + constructor)
- Modify: `pkg/hubble/aggregator.go:88-108` (keyFromFlow skip paths)
- Modify: `pkg/hubble/aggregator_test.go`

**Step 1: Write the failing test**

Add to `pkg/hubble/aggregator_test.go`:

```go
func TestAggregator_TracksNilEndpoint(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	// Flow with nil destination (ingress)
	f := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: nil,
	}
	in <- f
	close(in)

	err := agg.Run(context.Background(), in, out)
	require.NoError(t, err)

	events := drainEvents(out)
	assert.Empty(t, events)

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 1, "nil endpoint should be tracked")
	assert.Equal(t, "nil_endpoint", fieldString(debugLogs[0], "reason"))
}

func TestAggregator_TracksEmptyNamespace(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	f := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 80},
			},
		},
	}
	in <- f
	close(in)

	err := agg.Run(context.Background(), in, out)
	require.NoError(t, err)

	events := drainEvents(out)
	assert.Empty(t, events)

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 1, "empty namespace should be tracked")
	assert.Equal(t, "empty_namespace", fieldString(debugLogs[0], "reason"))
}

// fieldString extracts a string field value from a log entry.
func fieldString(entry observer.LoggedEntry, key string) string {
	for _, f := range entry.Context {
		if f.Key == key {
			return f.String
		}
	}
	return ""
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/ -run "TestAggregator_Tracks" -v`
Expected: FAIL — `NewAggregator` signature mismatch (missing tracker parameter)

**Step 3: Modify aggregator to accept and use tracker**

In `pkg/hubble/aggregator.go`, update the struct and constructor:

```go
// Aggregator struct — add tracker field
type Aggregator struct {
	interval       time.Duration
	logger         *zap.Logger
	warnedReserved map[string]struct{}
	tracker        *UnhandledTracker
}

// NewAggregator — add tracker parameter
func NewAggregator(interval time.Duration, logger *zap.Logger, tracker *UnhandledTracker) *Aggregator {
	return &Aggregator{
		interval:       interval,
		logger:         logger,
		warnedReserved: make(map[string]struct{}),
		tracker:        tracker,
	}
}
```

In `keyFromFlow`, replace the two skip paths (lines 88-108):

```go
if ep == nil {
    t.tracker.Track(f, "nil_endpoint")
    return AggKey{}, true
}

if ep.Namespace == "" {
    if isActionableReserved(ep.Labels) {
        warnKey := reservedWarnKey(ep.Labels, f)
        if _, seen := a.warnedReserved[warnKey]; !seen {
            a.warnedReserved[warnKey] = struct{}{}
            a.logger.Warn("dropped flow targets a reserved identity — cpg generates namespace-scoped CiliumNetworkPolicy and cannot handle reserved endpoints; use a CiliumClusterwideNetworkPolicy instead",
                zap.Strings("labels", ep.Labels),
                zap.String("summary", flowSummary(f)),
            )
        }
    } else {
        a.tracker.Track(f, "empty_namespace")
    }
    return AggKey{}, true
}
```

**Step 4: Fix existing tests**

Update all existing `NewAggregator` calls in `pkg/hubble/aggregator_test.go` and `pkg/hubble/pipeline_test.go` to pass the tracker:

For each test in `aggregator_test.go` that uses `zaptest.NewLogger(t)`:
```go
// Before:
agg := NewAggregator(time.Hour, logger)
// After:
tracker := NewUnhandledTracker(logger)
agg := NewAggregator(time.Hour, logger, tracker)
```

For `TestAggregator_SkipEmptyNamespace` — update to check the tracker DEBUG log instead of the old direct DEBUG log.

Update `pipeline.go:85` to pass the tracker:
```go
tracker := NewUnhandledTracker(cfg.Logger)
agg := NewAggregator(cfg.FlushInterval, cfg.Logger, tracker)
```

**Step 5: Run all hubble tests**

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add pkg/hubble/aggregator.go pkg/hubble/aggregator_test.go pkg/hubble/pipeline.go
git commit -m "feat: integrate UnhandledTracker into aggregator for nil_endpoint and empty_namespace"
```

---

### Task 4: Integrate tracker into policy builder

**Files:**
- Modify: `pkg/policy/builder.go:1-15` (imports)
- Modify: `pkg/policy/builder.go:80-138` (BuildPolicy signature + no_l4)
- Modify: `pkg/policy/builder.go:239-258` (buildIngressRules — nil_source, unknown_protocol, world_no_ip)
- Modify: `pkg/policy/builder.go:370-408` (buildEgressRules — nil_destination, unknown_protocol, world_no_ip)
- Modify: `pkg/hubble/aggregator.go:119-131` (flush calls BuildPolicy with tracker)

**Step 1: Define the tracker interface in policy package**

To avoid a circular dependency (`policy` cannot import `hubble`), define an interface in `pkg/policy/builder.go`:

```go
// FlowTracker receives flows that cannot be converted to policy rules.
type FlowTracker interface {
	Track(f *flowpb.Flow, reason string)
}
```

**Step 2: Update BuildPolicy signature**

```go
func BuildPolicy(namespace, workload string, flows []*flowpb.Flow, tracker FlowTracker) *ciliumv2.CiliumNetworkPolicy {
```

At line 102 (the `f.L4 == nil` skip):
```go
if f.L4 == nil {
    if tracker != nil {
        tracker.Track(f, "no_l4")
    }
    continue
}
```

Update `buildIngressRules` and `buildEgressRules` to also accept the tracker and track `nil_source`/`nil_destination`, `unknown_protocol`, and `world_no_ip`.

**Step 3: Update aggregator flush to pass tracker**

In `pkg/hubble/aggregator.go` flush method (line 121):
```go
cnp := policy.BuildPolicy(key.Namespace, key.Workload, flows, a.tracker)
```

`UnhandledTracker` already satisfies `policy.FlowTracker` (it has `Track(*flowpb.Flow, string)`).

**Step 4: Fix all existing callers**

Search for `BuildPolicy(` across the codebase and add tracker parameter (pass `nil` in existing tests that don't need tracking).

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && grep -rn "BuildPolicy(" --include="*.go"`

Update each call site.

**Step 5: Run full test suite**

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && go test ./... -v`
Expected: PASS

**Step 6: Commit**

```bash
git add pkg/policy/builder.go pkg/hubble/aggregator.go
git commit -m "feat: integrate UnhandledTracker into policy builder for all skip reasons"
```

---

### Task 5: Pipeline flush integration

**Files:**
- Modify: `pkg/hubble/pipeline.go:85-157`

**Step 1: Write the failing test**

Add to `pkg/hubble/pipeline_test.go` a test that verifies the tracker is flushed when policies are written. This depends on the existing pipeline test structure — adapt to use `observer` logger and check for INFO "unhandled flows summary" after a flow with nil L4 is processed.

**Step 2: Implement pipeline integration**

In `pkg/hubble/pipeline.go`, in `RunPipelineWithSource`:

```go
// After line 85 (agg := NewAggregator...)
tracker := NewUnhandledTracker(cfg.Logger)
agg := NewAggregator(cfg.FlushInterval, cfg.Logger, tracker)
```

In the writer goroutine (Stage 2), after the `for pe := range policies` loop ends:
```go
// Final flush when pipeline shuts down
tracker.Flush()
```

Also add a flush after each policy write batch. Since policies come one at a time from the channel, we need a way to know when a flush cycle ends. The simplest approach: flush the tracker in the aggregator's `flush()` method after sending all policy events:

In `pkg/hubble/aggregator.go` flush method:
```go
func (a *Aggregator) flush(buckets map[AggKey][]*flowpb.Flow, out chan<- policy.PolicyEvent) {
	for key, flows := range buckets {
		cnp := policy.BuildPolicy(key.Namespace, key.Workload, flows, a.tracker)
		out <- policy.PolicyEvent{
			Namespace: key.Namespace,
			Workload:  key.Workload,
			Policy:    cnp,
		}
	}
	for k := range buckets {
		delete(buckets, k)
	}
	// Flush unhandled flow summary after each aggregation cycle
	a.tracker.Flush()
}
```

**Step 3: Run full test suite**

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && go test ./... -v`
Expected: PASS

**Step 4: Commit**

```bash
git add pkg/hubble/pipeline.go pkg/hubble/aggregator.go
git commit -m "feat: flush UnhandledTracker at each aggregation cycle and shutdown"
```

---

### Task 6: README documentation

**Files:**
- Modify: `README.md` (after "Deduplication" section, line ~139)

**Step 1: Add the section**

Insert after the "Deduplication" section (line 139) in `README.md`:

```markdown
## Unhandled flows

Not every dropped flow can become a policy rule. cpg reports what it skips so you can investigate:

- **INFO summary** at each flush cycle — structured counters by reason
- **DEBUG detail** per unique flow — logged once, with source, destination, port, protocol, and labels

Enable debug logging to see individual flows:

```bash
cpg --debug generate -n production
# or
cpg --log-level debug generate -n production
```

### Skip reasons

| Reason | What it means |
|--------|---------------|
| `no_l4` | Flow has no L4 layer (no port/protocol info) |
| `nil_endpoint` | Source or destination endpoint is nil |
| `empty_namespace` | Target endpoint has no namespace (non-reserved) |
| `nil_source` | Ingress flow with nil source endpoint |
| `nil_destination` | Egress flow with nil destination endpoint |
| `unknown_protocol` | L4 layer present but protocol not TCP/UDP/ICMP |
| `world_no_ip` | World (external) traffic without an IP address |

### Example output

At INFO level (default):
```
INFO  Unhandled flows summary  {"no_l4": 42, "nil_endpoint": 8, "world_no_ip": 3}
```

At DEBUG level:
```
DEBUG Unhandled flow  {"src": "default/nginx", "dst": "kube-system/coredns", "port": "53", "proto": "UDP", "reason": "no_l4", "dst_labels": ["k8s:app=coredns"]}
```

Reserved identity flows (like `reserved:host` or `reserved:kube-apiserver`) are reported separately as WARN logs with guidance to use CiliumClusterwideNetworkPolicy instead.
```

**Step 2: Review the change**

Read the modified README and verify formatting.

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add unhandled flows section to README"
```

---

### Task 7: Final validation

**Step 1: Run full test suite with race detector**

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && make test`
Expected: PASS

**Step 2: Run linter**

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && make lint`
Expected: PASS (or fix any issues)

**Step 3: Build binary**

Run: `cd /home/gule/Workspace/team-infrastructure/cpg && make build`
Expected: Binary builds successfully
