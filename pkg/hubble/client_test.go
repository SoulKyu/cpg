package hubble

import (
	"context"
	"io"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	observerpb "github.com/cilium/cilium/api/v1/observer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// mockStream implements the flowStream interface for testing.
type mockStream struct {
	responses []*observerpb.GetFlowsResponse
	index     int
	ctx       context.Context
}

func (m *mockStream) Recv() (*observerpb.GetFlowsResponse, error) {
	if m.index >= len(m.responses) {
		return nil, io.EOF
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *mockStream) Context() context.Context {
	return m.ctx
}

func TestBuildFilters_AllNamespaces(t *testing.T) {
	filters := buildFilters(nil, true)

	require.Len(t, filters, 1, "all-namespaces should produce a single filter")
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Empty(t, filters[0].SourcePod, "should not filter by source pod")
	assert.Empty(t, filters[0].DestinationPod, "should not filter by destination pod")
}

func TestBuildFilters_SingleNamespace(t *testing.T) {
	filters := buildFilters([]string{"production"}, false)

	require.Len(t, filters, 2, "single namespace should produce two OR-ed filters")

	// First filter: source pod with namespace prefix
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Equal(t, []string{"production/"}, filters[0].SourcePod)
	assert.Empty(t, filters[0].DestinationPod)

	// Second filter: destination pod with namespace prefix
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[1].Verdict)
	assert.Empty(t, filters[1].SourcePod)
	assert.Equal(t, []string{"production/"}, filters[1].DestinationPod)
}

func TestBuildFilters_MultipleNamespaces(t *testing.T) {
	filters := buildFilters([]string{"prod", "staging"}, false)

	require.Len(t, filters, 2, "multiple namespaces should produce two OR-ed filters")

	expectedPrefixes := []string{"prod/", "staging/"}

	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Equal(t, expectedPrefixes, filters[0].SourcePod)
	assert.Empty(t, filters[0].DestinationPod)

	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[1].Verdict)
	assert.Empty(t, filters[1].SourcePod)
	assert.Equal(t, expectedPrefixes, filters[1].DestinationPod)
}

func TestBuildFilters_EmptyNamespaces(t *testing.T) {
	filters := buildFilters(nil, false)

	require.Len(t, filters, 1, "empty namespaces should behave like all-namespaces")
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Empty(t, filters[0].SourcePod)
	assert.Empty(t, filters[0].DestinationPod)
}

func TestClient_StreamDroppedFlows(t *testing.T) {
	logger := zaptest.NewLogger(t)

	testFlow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Namespace: "default"},
	}
	testLostEvent := &flowpb.LostEvent{
		NumEventsLost: 42,
	}

	stream := &mockStream{
		ctx: context.Background(),
		responses: []*observerpb.GetFlowsResponse{
			{ResponseTypes: &observerpb.GetFlowsResponse_Flow{Flow: testFlow}},
			{ResponseTypes: &observerpb.GetFlowsResponse_LostEvents{LostEvents: testLostEvent}},
		},
	}

	flows, lostEvents := streamFromSource(stream, logger, nil)

	var receivedFlows []*flowpb.Flow
	var receivedLost []*flowpb.LostEvent

	done := make(chan struct{})
	go func() {
		defer close(done)
		for f := range flows {
			receivedFlows = append(receivedFlows, f)
		}
	}()

	for le := range lostEvents {
		receivedLost = append(receivedLost, le)
	}
	<-done

	require.Len(t, receivedFlows, 1)
	assert.Equal(t, "default", receivedFlows[0].Source.Namespace)

	require.Len(t, receivedLost, 1)
	assert.Equal(t, uint64(42), receivedLost[0].NumEventsLost)
}

func TestClient_StreamContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	logger := zaptest.NewLogger(t)

	// Stream that blocks until context is cancelled
	stream := &blockingStream{ctx: ctx}

	flows, lostEvents := streamFromSource(stream, logger, nil)

	// Cancel context to trigger shutdown
	cancel()

	// Both channels should close
	timeout := time.After(2 * time.Second)
	select {
	case _, ok := <-flows:
		assert.False(t, ok, "flows channel should be closed")
	case <-timeout:
		t.Fatal("timed out waiting for flows channel to close")
	}
	select {
	case _, ok := <-lostEvents:
		assert.False(t, ok, "lostEvents channel should be closed")
	case <-timeout:
		t.Fatal("timed out waiting for lostEvents channel to close")
	}
}

func TestClient_StreamError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	stream := &errorStream{
		ctx: context.Background(),
		err: io.ErrUnexpectedEOF,
	}

	flows, lostEvents := streamFromSource(stream, logger, nil)

	// Both channels should close without panic
	timeout := time.After(2 * time.Second)
	select {
	case _, ok := <-flows:
		assert.False(t, ok, "flows channel should be closed on error")
	case <-timeout:
		t.Fatal("timed out waiting for flows channel to close")
	}
	select {
	case _, ok := <-lostEvents:
		assert.False(t, ok, "lostEvents channel should be closed on error")
	case <-timeout:
		t.Fatal("timed out waiting for lostEvents channel to close")
	}
}

func TestClient_SkipsNonDroppedFlowResponses(t *testing.T) {
	logger := zaptest.NewLogger(t)

	testFlow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Namespace: "default"},
	}

	stream := &mockStream{
		ctx: context.Background(),
		responses: []*observerpb.GetFlowsResponse{
			// NodeStatus response (no flow, no lost event)
			{ResponseTypes: nil},
			// Flow response
			{ResponseTypes: &observerpb.GetFlowsResponse_Flow{Flow: testFlow}},
			// Another nil response
			{ResponseTypes: nil},
		},
	}

	flows, lostEvents := streamFromSource(stream, logger, nil)

	var receivedFlows []*flowpb.Flow
	done := make(chan struct{})
	go func() {
		defer close(done)
		for f := range flows {
			receivedFlows = append(receivedFlows, f)
		}
	}()

	// Drain lost events
	for range lostEvents {
	}
	<-done

	require.Len(t, receivedFlows, 1, "should only receive the actual flow, skipping nil responses")
}

// blockingStream blocks on Recv until context is cancelled.
type blockingStream struct {
	ctx context.Context
}

func (b *blockingStream) Recv() (*observerpb.GetFlowsResponse, error) {
	<-b.ctx.Done()
	return nil, b.ctx.Err()
}

func (b *blockingStream) Context() context.Context {
	return b.ctx
}

// errorStream returns an error on the first Recv call.
type errorStream struct {
	ctx context.Context
	err error
}

func (e *errorStream) Recv() (*observerpb.GetFlowsResponse, error) {
	return nil, e.err
}

func (e *errorStream) Context() context.Context {
	return e.ctx
}
