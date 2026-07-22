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
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	filters := buildFilters(nil, true, false)

	require.Len(t, filters, 1, "all-namespaces should produce a single filter")
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Empty(t, filters[0].SourcePod, "should not filter by source pod")
	assert.Empty(t, filters[0].DestinationPod, "should not filter by destination pod")
}

// TestBuildFilters_AllNamespaces_WithAudit proves AC-4: includeAudit=true
// widens the verdict slice to {DROPPED, AUDIT} (order matters — DROPPED
// first, AUDIT appended).
func TestBuildFilters_AllNamespaces_WithAudit(t *testing.T) {
	filters := buildFilters(nil, true, true)

	require.Len(t, filters, 1, "all-namespaces should produce a single filter")
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED, flowpb.Verdict_AUDIT}, filters[0].Verdict)
}

func TestBuildFilters_SingleNamespace(t *testing.T) {
	filters := buildFilters([]string{"production"}, false, false)

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

// TestBuildFilters_SingleNamespace_WithAudit proves AC-4 across both OR-ed
// filters when a single namespace is set.
func TestBuildFilters_SingleNamespace_WithAudit(t *testing.T) {
	filters := buildFilters([]string{"production"}, false, true)

	require.Len(t, filters, 2, "single namespace should produce two OR-ed filters")
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED, flowpb.Verdict_AUDIT}, filters[0].Verdict)
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED, flowpb.Verdict_AUDIT}, filters[1].Verdict)
}

func TestBuildFilters_MultipleNamespaces(t *testing.T) {
	filters := buildFilters([]string{"prod", "staging"}, false, false)

	require.Len(t, filters, 2, "multiple namespaces should produce two OR-ed filters")

	expectedPrefixes := []string{"prod/", "staging/"}

	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Equal(t, expectedPrefixes, filters[0].SourcePod)
	assert.Empty(t, filters[0].DestinationPod)

	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[1].Verdict)
	assert.Empty(t, filters[1].SourcePod)
	assert.Equal(t, expectedPrefixes, filters[1].DestinationPod)
}

// TestBuildFilters_MultipleNamespaces_WithAudit proves AC-4 across both OR-ed
// filters when multiple namespaces are set.
func TestBuildFilters_MultipleNamespaces_WithAudit(t *testing.T) {
	filters := buildFilters([]string{"prod", "staging"}, false, true)

	require.Len(t, filters, 2, "multiple namespaces should produce two OR-ed filters")
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED, flowpb.Verdict_AUDIT}, filters[0].Verdict)
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED, flowpb.Verdict_AUDIT}, filters[1].Verdict)
}

func TestBuildFilters_EmptyNamespaces(t *testing.T) {
	filters := buildFilters(nil, false, false)

	require.Len(t, filters, 1, "empty namespaces should behave like all-namespaces")
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Empty(t, filters[0].SourcePod)
	assert.Empty(t, filters[0].DestinationPod)
}

// TestBuildFilters_EmptyNamespaces_WithAudit proves AC-4 for the
// empty-namespaces-behaves-like-all-namespaces case.
func TestBuildFilters_EmptyNamespaces_WithAudit(t *testing.T) {
	filters := buildFilters(nil, false, true)

	require.Len(t, filters, 1, "empty namespaces should behave like all-namespaces")
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED, flowpb.Verdict_AUDIT}, filters[0].Verdict)
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

	flows, lostEvents := streamFromSource(stream, logger, nil, nil)

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

	flows, lostEvents := streamFromSource(stream, logger, nil, nil)

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

	flows, lostEvents := streamFromSource(stream, logger, nil, nil)

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

	flows, lostEvents := streamFromSource(stream, logger, nil, nil)

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

// TestStreamFromSource_SurfacesTransportError verifies that a genuine transport
// failure (non-EOF, context not cancelled) is logged at Warn and sent on errCh,
// while a clean io.EOF neither logs at Warn nor emits an error — so a real
// mid-capture failure is distinguishable from a completed stream.
func TestStreamFromSource_SurfacesTransportError(t *testing.T) {
	tests := []struct {
		name        string
		recvErr     error
		wantErrSent bool
		wantWarn    bool
	}{
		{
			name:        "transport failure surfaces error at warn",
			recvErr:     io.ErrUnexpectedEOF,
			wantErrSent: true,
			wantWarn:    true,
		},
		{
			name:        "clean EOF is silent",
			recvErr:     io.EOF,
			wantErrSent: false,
			wantWarn:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			logger := zap.New(core)

			stream := &errorStream{ctx: context.Background(), err: tt.recvErr}
			errCh := make(chan error, 1)

			flows, lostEvents := streamFromSource(stream, logger, nil, errCh)

			// Drain both data channels so the goroutine runs to completion.
			for range flows {
			}
			for range lostEvents {
			}

			var gotErr error
			var errOpen bool
			select {
			case gotErr, errOpen = <-errCh:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for errCh to resolve")
			}

			if tt.wantErrSent {
				require.True(t, errOpen, "errCh must yield an error before closing")
				require.Error(t, gotErr)
				assert.ErrorIs(t, gotErr, tt.recvErr)
			} else {
				assert.False(t, errOpen, "errCh must be closed with no error on clean EOF")
			}

			warns := logs.FilterLevelExact(zapcore.WarnLevel).All()
			if tt.wantWarn {
				assert.NotEmpty(t, warns, "transport failure must log at Warn")
			} else {
				assert.Empty(t, warns, "clean EOF must not log at Warn")
			}
		})
	}
}

// TestWaitForConnReady_TimesOutOnUnreachableRelay verifies the --timeout knob is
// actually applied to connection establishment: an unreachable relay must fail
// fast within the configured timeout instead of blocking indefinitely.
func TestWaitForConnReady_TimesOutOnUnreachableRelay(t *testing.T) {
	// TEST-NET-1 (RFC 5737) is guaranteed unroutable, so the TCP connect hangs
	// and the connection never reaches Ready.
	conn, err := grpc.NewClient("192.0.2.1:9999", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	start := time.Now()
	err = waitForConnReady(context.Background(), conn, 200*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err, "unreachable relay must return a connection error")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 2*time.Second, "must fail fast, close to the configured timeout")
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
