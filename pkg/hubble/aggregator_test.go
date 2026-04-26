package hubble

import (
	"context"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/SoulKyu/cpg/pkg/dropclass"
	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)

func TestAggregator_FlushOnTicker(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(10*time.Millisecond, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	// Send a flow then close input after a short delay to let ticker fire
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"production",
		8080,
	)
	in <- f

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- agg.Run(ctx, in, out, nil)
	}()

	// Wait for ticker to flush
	select {
	case ev := <-out:
		assert.Equal(t, "production", ev.Namespace)
		assert.Equal(t, "server", ev.Workload)
		assert.NotNil(t, ev.Policy)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ticker flush")
	}

	// Close input to end Run
	close(in)

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to return")
	}
}

func TestAggregator_KeyFromFlow_Ingress(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker) // long interval, won't tick

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	// INGRESS: destination is the policy target
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"production",
		8080,
	)
	in <- f
	close(in) // triggers flush of remaining

	err := agg.Run(context.Background(), in, out, nil)
	require.NoError(t, err)

	events := drainEvents(out)
	require.Len(t, events, 1)
	assert.Equal(t, "production", events[0].Namespace)
	assert.Equal(t, "server", events[0].Workload)
}

func TestAggregator_KeyFromFlow_Egress(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	// EGRESS: source is the policy target
	f := testdata.EgressUDPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=dns"},
		"staging",
		53,
	)
	in <- f
	close(in)

	err := agg.Run(context.Background(), in, out, nil)
	require.NoError(t, err)

	events := drainEvents(out)
	require.Len(t, events, 1)
	assert.Equal(t, "staging", events[0].Namespace)
	assert.Equal(t, "client", events[0].Workload)
}

func TestAggregator_FlushOnChannelClose(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker) // long interval, won't tick

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	// Send flows then close input
	in <- testdata.IngressTCPFlow([]string{"k8s:app=a"}, []string{"k8s:app=s1"}, "ns1", 80)
	in <- testdata.IngressTCPFlow([]string{"k8s:app=b"}, []string{"k8s:app=s2"}, "ns2", 443)
	close(in)

	err := agg.Run(context.Background(), in, out, nil)
	require.NoError(t, err)

	events := drainEvents(out)
	assert.Len(t, events, 2, "should flush all remaining buckets on channel close")
}

func TestAggregator_FlushOnContextCancel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- testdata.IngressTCPFlow([]string{"k8s:app=a"}, []string{"k8s:app=srv"}, "default", 80)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- agg.Run(ctx, in, out, nil)
	}()

	// Deterministic: wait until the aggregator has consumed the buffered flow.
	require.Eventually(t, func() bool { return len(in) == 0 }, time.Second, time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to return after cancel")
	}

	events := drainEvents(out)
	assert.Len(t, events, 1, "should flush remaining on context cancel")
}

func TestAggregator_SkipEmptyNamespace(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	// Flow with empty namespace on destination (ingress target)
	f := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "", // empty namespace
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 80},
			},
		},
	}
	in <- f
	close(in)

	err := agg.Run(context.Background(), in, out, nil)
	require.NoError(t, err)

	events := drainEvents(out)
	assert.Empty(t, events, "should skip flows with empty namespace")

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 1, "empty namespace should be tracked")
	assert.Equal(t, "empty_namespace", fieldString(debugLogs[0], "reason"))
}

func TestMonitorLostEvents_AggregatesWarnings(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	logger := zap.New(core)

	ch := make(chan *flowpb.LostEvent, 10)
	ctx, cancel := context.WithCancel(context.Background())

	// Send lost events
	ch <- &flowpb.LostEvent{NumEventsLost: 10}
	ch <- &flowpb.LostEvent{NumEventsLost: 5}

	done := make(chan error, 1)
	go func() {
		done <- monitorLostEvents(ctx, ch, logger)
	}()

	// Deterministic: wait until both events are drained, then cancel.
	require.Eventually(t, func() bool { return len(ch) == 0 }, time.Second, time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	// Should have at least the final summary warn
	var found bool
	for _, entry := range logs.All() {
		if entry.Level == zapcore.WarnLevel {
			found = true
		}
	}
	assert.True(t, found, "should log warning about lost events")
}

func TestMonitorLostEvents_FinalSummary(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	logger := zap.New(core)

	ch := make(chan *flowpb.LostEvent, 10)
	ctx, cancel := context.WithCancel(context.Background())

	ch <- &flowpb.LostEvent{NumEventsLost: 42}

	done := make(chan error, 1)
	go func() {
		done <- monitorLostEvents(ctx, ch, logger)
	}()

	require.Eventually(t, func() bool { return len(ch) == 0 }, time.Second, time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	// Check final summary was logged
	var totalLogged bool
	for _, entry := range logs.All() {
		for _, field := range entry.Context {
			if field.Key == "total_lost" && field.Integer == 42 {
				totalLogged = true
			}
		}
	}
	assert.True(t, totalLogged, "should log total lost events in final summary")
}

// TestAggregator_L7DNSCount_Increments asserts that observing a flow carrying
// Flow.L7.Dns increments the diagnostic L7DNSCount counter, regardless of
// whether L7Enabled is set (the counter powers the VIS-01 empty-records gate
// and must remain accurate even when codegen is disabled). HTTP and DNS
// counters move independently when the flow carries both records.
func TestAggregator_L7DNSCount_Increments(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	dnsOnly := testdata.EgressUDPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:k8s-app=kube-dns"},
		"production", 53,
	)
	dnsOnly.L7 = &flowpb.Layer7{
		Record: &flowpb.Layer7_Dns{Dns: &flowpb.DNS{Query: "api.example.com."}},
	}

	// Layer7.Record is a oneof — a single flow cannot carry both Http and Dns.
	// Use two consecutive flows: one HTTP, one DNS, and assert both counters
	// move independently.
	makeBare := func() *flowpb.Flow {
		return &flowpb.Flow{
			TrafficDirection: flowpb.TrafficDirection_INGRESS,
			Source:           &flowpb.Endpoint{Labels: []string{"k8s:app=a"}, Namespace: "production"},
			Destination:      &flowpb.Endpoint{Labels: []string{"k8s:app=b"}, Namespace: "production"},
			L4: &flowpb.Layer4{
				Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{DestinationPort: 80}},
			},
		}
	}
	httpFlow := makeBare()
	httpFlow.L7 = &flowpb.Layer7{Record: &flowpb.Layer7_Http{Http: &flowpb.HTTP{Method: "GET", Url: "/"}}}
	dnsFlow := makeBare()
	dnsFlow.L7 = &flowpb.Layer7{Record: &flowpb.Layer7_Dns{Dns: &flowpb.DNS{Query: "x.example.com."}}}

	in <- dnsOnly
	in <- httpFlow
	in <- dnsFlow
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))
	_ = drainEvents(out)

	assert.Equal(t, uint64(2), agg.L7DNSCount(), "two DNS-bearing flows → L7DNSCount==2")
	assert.Equal(t, uint64(1), agg.L7HTTPCount(), "one HTTP-bearing flow → L7HTTPCount==1")
}

// TestAggregator_L7DNSCount_IndependentOfL7Enabled mirrors the HTTP counter
// contract: the diagnostic counter increments regardless of SetL7Enabled.
func TestAggregator_L7DNSCount_IndependentOfL7Enabled(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)
	agg.SetL7Enabled(false) // explicit: counter must still move

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	f := testdata.EgressUDPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:k8s-app=kube-dns"},
		"production", 53,
	)
	f.L7 = &flowpb.Layer7{Record: &flowpb.Layer7_Dns{Dns: &flowpb.DNS{Query: "api.example.com."}}}
	in <- f
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))
	_ = drainEvents(out)

	assert.Equal(t, uint64(1), agg.L7DNSCount(), "counter is diagnostic, not gated by L7Enabled")
}

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

	err := agg.Run(context.Background(), in, out, nil)
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

	err := agg.Run(context.Background(), in, out, nil)
	require.NoError(t, err)

	events := drainEvents(out)
	assert.Empty(t, events)

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 1, "empty namespace should be tracked")
	assert.Equal(t, "empty_namespace", fieldString(debugLogs[0], "reason"))
}

// TestAggregator_IgnoreProtocols_DropsICMPv4 asserts that a single ICMPv4 flow
// is dropped before bucketing when icmpv4 is in the ignore set: no PolicyEvent
// is emitted, FlowsSeen stays at 0, and the per-protocol counter records the
// drop.
func TestAggregator_IgnoreProtocols_DropsICMPv4(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)
	agg.SetIgnoreProtocols([]string{"icmpv4"})

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- testdata.EgressICMPv4Flow(
		[]string{"k8s:app=client"}, "production",
		[]string{"k8s:app=server"}, "10.0.0.1", 8,
	)
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	events := drainEvents(out)
	assert.Empty(t, events, "ignored protocol must not produce PolicyEvent")
	assert.Equal(t, uint64(0), agg.FlowsSeen(), "ignored flow must not count toward FlowsSeen")
	assert.Equal(t, uint64(1), agg.IgnoredByProtocol()["icmpv4"], "icmpv4 counter must register the drop")
}

// TestAggregator_IgnoreProtocols_TCPPassthrough asserts that TCP flows pass
// through untouched while ICMPv4 flows are filtered when only ICMP is excluded.
func TestAggregator_IgnoreProtocols_TCPPassthrough(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)
	agg.SetIgnoreProtocols([]string{"icmpv4", "icmpv6"})

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- testdata.IngressTCPFlow([]string{"k8s:app=c"}, []string{"k8s:app=s"}, "production", 80)
	in <- testdata.EgressICMPv4Flow([]string{"k8s:app=c"}, "production", []string{"k8s:app=x"}, "10.0.0.1", 8)
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	events := drainEvents(out)
	assert.Len(t, events, 1, "TCP flow must produce one PolicyEvent")
	assert.Equal(t, uint64(1), agg.FlowsSeen())
	assert.Equal(t, uint64(1), agg.IgnoredByProtocol()["icmpv4"])
}

// TestAggregator_IgnoreProtocols_MultipleProtocols asserts that the ignore set
// honours multiple entries simultaneously.
func TestAggregator_IgnoreProtocols_MultipleProtocols(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)
	agg.SetIgnoreProtocols([]string{"tcp", "udp"})

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- testdata.IngressTCPFlow([]string{"k8s:app=c"}, []string{"k8s:app=s"}, "production", 80)
	in <- testdata.EgressUDPFlow([]string{"k8s:app=c"}, []string{"k8s:app=dns"}, "production", 53)
	in <- testdata.EgressICMPv4Flow([]string{"k8s:app=c"}, "production", []string{"k8s:app=x"}, "10.0.0.1", 8)
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	events := drainEvents(out)
	assert.Len(t, events, 1, "only the ICMPv4 flow survives bucketing")
	counters := agg.IgnoredByProtocol()
	assert.Equal(t, uint64(1), counters["tcp"])
	assert.Equal(t, uint64(1), counters["udp"])
	assert.Zero(t, counters["icmpv4"], "icmpv4 was not in the ignore set")
}

// TestAggregator_IgnoreProtocols_EmptyIsNoOp asserts that calling
// SetIgnoreProtocols with nil is a no-op: flows are processed as usual and the
// per-protocol counter is empty.
func TestAggregator_IgnoreProtocols_EmptyIsNoOp(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)
	agg.SetIgnoreProtocols(nil)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- testdata.IngressTCPFlow([]string{"k8s:app=c"}, []string{"k8s:app=s"}, "production", 80)
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	events := drainEvents(out)
	assert.Len(t, events, 1)
	assert.Empty(t, agg.IgnoredByProtocol(), "no entries when ignore set is empty")
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

// drainEvents reads all events from a closed channel.
func drainEvents(ch <-chan policy.PolicyEvent) []policy.PolicyEvent {
	var events []policy.PolicyEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

// makeInfraFlow builds a dropped EGRESS flow with the given DropReasonDesc.
// Source endpoint is in namespace "test" with k8s:app=worker label.
// NodeName is set to "node-1" for healthCh assertions.
func makeInfraFlow(reason flowpb.DropReason) *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Verdict:          flowpb.Verdict_DROPPED,
		DropReasonDesc:   reason,
		NodeName:         "node-1",
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=worker"},
			Namespace: "test",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=backend"},
			Namespace: "test",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 8080},
			},
		},
	}
}

// makePolicyFlow builds a dropped EGRESS POLICY_DENIED flow with namespace "test".
func makePolicyFlow() *flowpb.Flow {
	f := testdata.EgressUDPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"test",
		8080,
	)
	f.Verdict = flowpb.Verdict_DROPPED
	f.DropReasonDesc = flowpb.DropReason_POLICY_DENIED
	return f
}

// TestAggregatorClassificationSuppression verifies that a CT_MAP_INSERTION_FAILED
// flow is suppressed from CNP generation but counted in flowsSeen and infraDrops.
func TestAggregatorClassificationSuppression(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- makeInfraFlow(flowpb.DropReason_CT_MAP_INSERTION_FAILED)
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	events := drainEvents(out)
	assert.Empty(t, events, "infra flow must not produce PolicyEvent")
	assert.Equal(t, uint64(1), agg.FlowsSeen(), "infra flow must count toward flowsSeen")
	assert.Equal(t, uint64(1), agg.InfraDropTotal(), "infraDrops must be 1")
	drops := agg.InfraDrops()
	assert.Equal(t, uint64(1), drops[flowpb.DropReason_CT_MAP_INSERTION_FAILED])
}

// TestAggregatorPolicyFlowPassthrough verifies that a POLICY_DENIED flow is bucketed
// and produces a PolicyEvent.
func TestAggregatorPolicyFlowPassthrough(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- makePolicyFlow()
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	events := drainEvents(out)
	assert.Len(t, events, 1, "policy flow must produce exactly one PolicyEvent")
	assert.Equal(t, uint64(1), agg.FlowsSeen())
	assert.Equal(t, uint64(0), agg.InfraDropTotal())
}

// TestAggregatorFlowsSeenInvariant verifies Pitfall 6: 5 policy + 3 infra flows
// yields flowsSeen=8, infraDrops=3, and exactly 5 CNP buckets.
func TestAggregatorFlowsSeenInvariant(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 20)
	out := make(chan policy.PolicyEvent, 20)

	for i := 0; i < 5; i++ {
		in <- makePolicyFlow()
	}
	for i := 0; i < 3; i++ {
		in <- makeInfraFlow(flowpb.DropReason_CT_MAP_INSERTION_FAILED)
	}
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	drainEvents(out) // consume so the channel drains
	assert.Equal(t, uint64(8), agg.FlowsSeen(), "flowsSeen must include infra flows (Pitfall 6)")
	assert.Equal(t, uint64(3), agg.InfraDropTotal(), "infraDrops must be 3")
}

// TestAggregatorNoiseDiscarded verifies that NAT_NOT_NEEDED flows are silently
// discarded — no bucket, flowsSeen=0, infraDrops=0.
func TestAggregatorNoiseDiscarded(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- makeInfraFlow(flowpb.DropReason_NAT_NOT_NEEDED)
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	events := drainEvents(out)
	assert.Empty(t, events)
	assert.Equal(t, uint64(0), agg.FlowsSeen(), "noise flow must NOT count toward flowsSeen")
	assert.Equal(t, uint64(0), agg.InfraDropTotal(), "noise flow must NOT count toward infraDrops")
}

// TestAggregatorTransientCounted verifies that a STALE_OR_UNROUTABLE_IP flow
// is counted in flowsSeen and infraDrops but produces no CNP bucket.
func TestAggregatorTransientCounted(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- makeInfraFlow(flowpb.DropReason_STALE_OR_UNROUTABLE_IP)
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	events := drainEvents(out)
	assert.Empty(t, events)
	assert.Equal(t, uint64(1), agg.FlowsSeen())
	assert.Equal(t, uint64(1), agg.InfraDropTotal())
}

// TestAggregatorHealthChReceivesDropEvent verifies that an infra flow sends a
// DropEvent on a non-nil healthCh with correct fields.
func TestAggregatorHealthChReceivesDropEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)
	healthCh := make(chan DropEvent, 10)

	in <- makeInfraFlow(flowpb.DropReason_CT_MAP_INSERTION_FAILED)
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, healthCh))

	close(healthCh)
	var events []DropEvent
	for ev := range healthCh {
		events = append(events, ev)
	}
	require.Len(t, events, 1)
	assert.Equal(t, flowpb.DropReason_CT_MAP_INSERTION_FAILED, events[0].Reason)
	assert.Equal(t, dropclass.DropClassInfra, events[0].Class)
	assert.Equal(t, "node-1", events[0].NodeName)
	assert.Equal(t, "test", events[0].Namespace)
}

// TestAggregatorHealthChNilSafe verifies that passing nil as healthCh does not
// panic when infra flows are processed.
func TestAggregatorHealthChNilSafe(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- makeInfraFlow(flowpb.DropReason_CT_MAP_INSERTION_FAILED)
	close(in)

	// Must not panic
	require.NoError(t, agg.Run(context.Background(), in, out, nil))
}

// TestAggregatorFilterPrecedence verifies that --ignore-protocol fires before the
// classification gate: an ICMPv4 flow that would be classified as Infra/Transient
// is counted in ignoredByProtocol, NOT in infraDrops.
func TestAggregatorFilterPrecedence(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)
	agg.SetIgnoreProtocols([]string{"icmpv4"})

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	// ICMPv4 flow with an infra drop reason — proto filter must fire first
	f := makeInfraFlow(flowpb.DropReason_CT_MAP_INSERTION_FAILED)
	f.L4 = &flowpb.Layer4{
		Protocol: &flowpb.Layer4_ICMPv4{
			ICMPv4: &flowpb.ICMPv4{Type: 8},
		},
	}
	in <- f
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	assert.Equal(t, uint64(1), agg.IgnoredByProtocol()["icmpv4"], "proto filter must fire")
	assert.Equal(t, uint64(0), agg.InfraDropTotal(), "classification gate must NOT fire for proto-filtered flows")
	assert.Equal(t, uint64(0), agg.FlowsSeen(), "proto-filtered flows must not count toward flowsSeen")
}

// TestInfraDropTotal verifies the convenience sum accessor.
func TestInfraDropTotal(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- makeInfraFlow(flowpb.DropReason_CT_MAP_INSERTION_FAILED)
	in <- makeInfraFlow(flowpb.DropReason_CT_MAP_INSERTION_FAILED)
	in <- makeInfraFlow(flowpb.DropReason_STALE_OR_UNROUTABLE_IP)
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	drainEvents(out)
	assert.Equal(t, uint64(3), agg.InfraDropTotal())
}

// TestInfraDropsCopy verifies that InfraDrops() returns an independent copy.
func TestInfraDropsCopy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(time.Hour, logger, tracker)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- makeInfraFlow(flowpb.DropReason_CT_MAP_INSERTION_FAILED)
	close(in)

	require.NoError(t, agg.Run(context.Background(), in, out, nil))

	drainEvents(out)
	copy1 := agg.InfraDrops()
	copy1[flowpb.DropReason_CT_MAP_INSERTION_FAILED] = 999

	copy2 := agg.InfraDrops()
	assert.Equal(t, uint64(1), copy2[flowpb.DropReason_CT_MAP_INSERTION_FAILED],
		"modifying the returned map must not affect the internal counter")
}
