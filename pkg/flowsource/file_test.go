package flowsource

import (
	"context"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func drain(t *testing.T, flows <-chan *flowpb.Flow) []*flowpb.Flow {
	t.Helper()
	var out []*flowpb.Flow
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case f, ok := <-flows:
			if !ok {
				return out
			}
			out = append(out, f)
		case <-deadline.C:
			t.Fatalf("timed out waiting for channel close")
			return out
		}
	}
}

func TestFileSourceHappyPath(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/small.jsonl", zap.NewNop())
	require.NoError(t, err)

	flows, lost, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	got := drain(t, flows)
	assert.Len(t, got, 3)

	_, ok := <-lost
	assert.False(t, ok, "lost channel must be closed")

	assert.Equal(t, int64(3), src.Stats().FlowsEmitted)
	assert.Equal(t, int64(0), src.Stats().NonDroppedSkipped)
	assert.Equal(t, int64(0), src.Stats().Malformed)
}

func TestFileSourceFiltersNonDropped(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/with_non_dropped.jsonl", zap.NewNop())
	require.NoError(t, err)
	flows, _, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	got := drain(t, flows)
	assert.Len(t, got, 3)
	assert.Equal(t, int64(2), src.Stats().NonDroppedSkipped)
}

func TestFileSourceSkipsMalformed(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/malformed.jsonl", zap.NewNop())
	require.NoError(t, err)
	flows, _, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	got := drain(t, flows)
	assert.Len(t, got, 2)
	assert.Equal(t, int64(1), src.Stats().Malformed)
}

func TestFileSourceEmptyFile(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/empty.jsonl", zap.NewNop())
	require.NoError(t, err)
	flows, _, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	got := drain(t, flows)
	assert.Empty(t, got)
}

func TestFileSourceMissingFile(t *testing.T) {
	_, err := NewFileSource("/nonexistent/file.jsonl", zap.NewNop())
	require.Error(t, err)
}

func TestFileSourceContextCancellation(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/small.jsonl", zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	flows, _, err := src.StreamDroppedFlows(ctx, nil, false)
	require.NoError(t, err)

	_ = drain(t, flows)
}

func TestFileSourceGzip(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/small.jsonl.gz", zap.NewNop())
	require.NoError(t, err)

	flows, _, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	got := drain(t, flows)
	assert.Len(t, got, 3)
}
