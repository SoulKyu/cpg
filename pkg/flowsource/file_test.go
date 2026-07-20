package flowsource

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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

// TestFileSourceOversizedLineTruncates verifies that a single line exceeding the
// scanner buffer cap terminates the stream and is reported as a truncated replay at
// Error level, NOT as a success-looking "replay complete" (which would hide the
// partial failure — every flow after the oversized line is dropped, not skipped).
func TestFileSourceOversizedLineTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oversized.jsonl")

	// One over-long line (> defaultScannerBufferBytes) triggers bufio.ErrTooLong.
	oversized := strings.Repeat("a", defaultScannerBufferBytes+1)
	require.NoError(t, os.WriteFile(path, []byte(oversized+"\n"), 0o600))

	core, logs := observer.New(zapcore.DebugLevel)
	src, err := NewFileSource(path, zap.New(core))
	require.NoError(t, err)

	flows, _, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	_ = drain(t, flows)

	assert.Empty(t, logs.FilterMessage("replay complete").All(),
		"truncated replay must not emit a success-looking 'replay complete'")

	truncated := logs.FilterMessageSnippet("replay truncated").All()
	require.Len(t, truncated, 1, "oversized line must surface a truncated-replay error")
	assert.Equal(t, zapcore.ErrorLevel, truncated[0].Level,
		"truncated replay must be logged at Error level")
}

// TestFileSourceContextCancellationNonDropped verifies the scan loop honors ctx
// cancellation directly (top of loop body), not only on the DROPPED send path. A
// file of exclusively non-DROPPED flows previously ran to EOF ignoring a cancelled
// ctx; with the guard in place the goroutine returns without processing any line.
func TestFileSourceContextCancellationNonDropped(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/with_non_dropped.jsonl", zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	flows, _, err := src.StreamDroppedFlows(ctx, nil, false)
	require.NoError(t, err)

	_ = drain(t, flows)
	assert.Equal(t, int64(0), src.Stats().LinesRead,
		"cancelled ctx must stop the loop before reading any line")
}

func TestFileSourceGzip(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/small.jsonl.gz", zap.NewNop())
	require.NoError(t, err)

	flows, _, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	got := drain(t, flows)
	assert.Len(t, got, 3)
}
