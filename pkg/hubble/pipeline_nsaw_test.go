package hubble

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/flowsource"
)

// TestPipelineNewNamespaces is the NSAW-02 tracer's end-to-end proof: a
// FileSource replay of the staggered-timestamp fixture flows through the
// aggregator's first-seen recording, the post-g.Wait() accessor, the pure
// computation, and the PrintNewNamespacesSummary block on the SAME resolved
// cfg.Stdout writer PrintClusterHealthSummary uses. Only 'latecomer' — whose
// first observed flow is strictly after the capture's minimum flow timestamp —
// may appear in the report; the capture-start namespaces (default, production)
// must never be flagged.
func TestPipelineNewNamespaces(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := newObservedLogger()

	src, err := flowsource.NewFileSource("../../testdata/flows/ns_staggered.jsonl", logger)
	require.NoError(t, err)

	var buf bytes.Buffer
	cfg := PipelineConfig{
		FlushInterval: 10 * time.Millisecond,
		OutputDir:     tmpDir,
		Logger:        logger,
		Stdout:        &buf,
	}

	err = RunPipelineWithSource(context.Background(), cfg, src)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "New namespaces detected during this run:")
	assert.Contains(t, out, "  - namespace latecomer first observed after capture start — re-run to cover it")
	assert.NotContains(t, out, "  - namespace default ", "capture-start namespace must NOT be reported")
	assert.NotContains(t, out, "  - namespace production ", "capture-start namespace must NOT be reported")
}
