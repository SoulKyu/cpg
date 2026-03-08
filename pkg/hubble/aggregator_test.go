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

	"github.com/gule/cpg/pkg/policy"
	"github.com/gule/cpg/pkg/policy/testdata"
)

func TestAggregator_FlushOnTicker(t *testing.T) {
	logger := zaptest.NewLogger(t)
	agg := NewAggregator(10*time.Millisecond, logger)

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
		done <- agg.Run(ctx, in, out)
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
	agg := NewAggregator(time.Hour, logger) // long interval, won't tick

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

	err := agg.Run(context.Background(), in, out)
	require.NoError(t, err)

	events := drainEvents(out)
	require.Len(t, events, 1)
	assert.Equal(t, "production", events[0].Namespace)
	assert.Equal(t, "server", events[0].Workload)
}

func TestAggregator_KeyFromFlow_Egress(t *testing.T) {
	logger := zaptest.NewLogger(t)
	agg := NewAggregator(time.Hour, logger)

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

	err := agg.Run(context.Background(), in, out)
	require.NoError(t, err)

	events := drainEvents(out)
	require.Len(t, events, 1)
	assert.Equal(t, "staging", events[0].Namespace)
	assert.Equal(t, "client", events[0].Workload)
}

func TestAggregator_FlushOnChannelClose(t *testing.T) {
	logger := zaptest.NewLogger(t)
	agg := NewAggregator(time.Hour, logger) // long interval, won't tick

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	// Send flows then close input
	in <- testdata.IngressTCPFlow([]string{"k8s:app=a"}, []string{"k8s:app=s1"}, "ns1", 80)
	in <- testdata.IngressTCPFlow([]string{"k8s:app=b"}, []string{"k8s:app=s2"}, "ns2", 443)
	close(in)

	err := agg.Run(context.Background(), in, out)
	require.NoError(t, err)

	events := drainEvents(out)
	assert.Len(t, events, 2, "should flush all remaining buckets on channel close")
}

func TestAggregator_FlushOnContextCancel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	agg := NewAggregator(time.Hour, logger)

	in := make(chan *flowpb.Flow, 10)
	out := make(chan policy.PolicyEvent, 10)

	in <- testdata.IngressTCPFlow([]string{"k8s:app=a"}, []string{"k8s:app=srv"}, "default", 80)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- agg.Run(ctx, in, out)
	}()

	// Give time for the flow to be received
	time.Sleep(20 * time.Millisecond)
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
	agg := NewAggregator(time.Hour, logger)

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

	err := agg.Run(context.Background(), in, out)
	require.NoError(t, err)

	events := drainEvents(out)
	assert.Empty(t, events, "should skip flows with empty namespace")
	assert.GreaterOrEqual(t, logs.Len(), 1, "should log debug message for skipped flow")
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

	// Wait a bit then cancel -- with a very short ticker we'd see a warn log
	// We close channel to trigger return
	time.Sleep(50 * time.Millisecond)
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

	time.Sleep(20 * time.Millisecond)
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

// drainEvents reads all events from a closed channel.
func drainEvents(ch <-chan policy.PolicyEvent) []policy.PolicyEvent {
	var events []policy.PolicyEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}
