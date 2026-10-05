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

// TestSummaryByteIdentity is the ROADMAP SC1 byte-identity regression for the
// v1.6 session summary (zero-new-namespace half). Run over small.jsonl — a
// fixture where EVERY namespace (default, production) is first observed on the
// very first flow at 14:00:00 = capture start — the pipeline's summary output
// must contain NEITHER the NSAW header NOR the re-run hint.
//
// Byte-identity argument (provenance discipline mirroring
// pipeline_l7_golden_test.go — a reviewer must be able to re-derive the
// claim; NEVER weaken an assertion here to make this test pass):
//
//  1. The NSAW print call is the ONLY new stdout write introduced on this
//     path (grep-gated: exactly one PrintNewNamespacesSummary(stdout call in
//     pipeline.go, immediately after PrintClusterHealthSummary).
//  2. PrintNewNamespacesSummary is unit-proven to write ZERO bytes for empty
//     and nil lists (TestPrintNewNamespacesSummary_EmptyAndNil) — its FIRST
//     statement is the empty guard, so no header can leak.
//  3. small.jsonl is unit-proven below (companion assertion) to yield an
//     EMPTY list via computeNewNamespaces — every namespace's first-seen
//     equals the capture's minimum flow timestamp, and strict After excludes
//     them all.
//
// Therefore the summary output for zero-new-namespace runs is byte-identical
// to the v1.6 build without the NSAW feature.
func TestSummaryByteIdentity(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := newObservedLogger()

	src, err := flowsource.NewFileSource("../../testdata/flows/small.jsonl", logger)
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
	assert.NotContains(t, out, "New namespaces detected during this run:",
		"zero new namespaces ⇒ no NSAW header (small.jsonl byte-identity)")
	assert.NotContains(t, out, "re-run to cover it",
		"zero new namespaces ⇒ no re-run hint (small.jsonl byte-identity)")

	// Companion unit assertion: firstSeen built from small.jsonl's three
	// flows (default/production all first seen at 14:00:00–14:00:02, source
	// and destination) yields an EMPTY new-namespace list.
	m := map[string]time.Time{}
	noteFirstSeen(m, nsawFlow(nsawTime, "default", "production"))
	noteFirstSeen(m, nsawFlow(nsawTime.Add(1*time.Second), "default", "production"))
	noteFirstSeen(m, nsawFlow(nsawTime.Add(2*time.Second), "production", "production"))
	assert.Empty(t, computeNewNamespaces(m),
		"small.jsonl namespaces are all first-seen at capture start ⇒ empty report")
}

// fakeNewNamespacesSource is a NewNamespacesSource returning a fixed list —
// the test double for pkg/k8s.NamespaceWatcher (structural typing: no import
// of pkg/k8s needed here).
type fakeNewNamespacesSource struct{ names []string }

func (f fakeNewNamespacesSource) NewNamespaces() []string { return f.names }

// TestPipelineNewNamespacesWatch is the NSAW-01 watcher-precedence proof: when
// PipelineConfig.NewNamespaces is set (cpg generate started a watcher), the
// watcher's list is AUTHORITATIVE for the run and the report uses the WATCH
// phrasing ("appeared after run start") — never the replay phrasing.
func TestPipelineNewNamespacesWatch(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := newObservedLogger()

	src, err := flowsource.NewFileSource("../../testdata/flows/small.jsonl", logger)
	require.NoError(t, err)

	var buf bytes.Buffer
	cfg := PipelineConfig{
		FlushInterval: 10 * time.Millisecond,
		OutputDir:     tmpDir,
		Logger:        logger,
		Stdout:        &buf,
		NewNamespaces: fakeNewNamespacesSource{names: []string{"tenant-b"}},
	}

	err = RunPipelineWithSource(context.Background(), cfg, src)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "New namespaces detected during this run:")
	assert.Contains(t, out, "  - namespace tenant-b appeared after run start — re-run to cover it",
		"watcher-source run must use the watch phrasing")
	assert.NotContains(t, out, "first observed after capture start",
		"watcher-source run must NOT use the replay phrasing (watcher precedence)")
}

// TestPipelineNewNamespacesHeuristicFallback proves the precedence branch did
// not perturb 26-01's fallback: with cfg.NewNamespaces nil (replay, MCP mode,
// unreachable kubeconfig), the flow-timestamp heuristic stays in effect —
// small.jsonl (all namespaces first-seen at capture start) yields zero NSAW
// bytes (byte-identity with the v1.6 summary).
func TestPipelineNewNamespacesHeuristicFallback(t *testing.T) {
	tmpDir := t.TempDir()
	logger, _ := newObservedLogger()

	src, err := flowsource.NewFileSource("../../testdata/flows/small.jsonl", logger)
	require.NoError(t, err)

	var buf bytes.Buffer
	cfg := PipelineConfig{
		FlushInterval: 10 * time.Millisecond,
		OutputDir:     tmpDir,
		Logger:        logger,
		Stdout:        &buf,
		// NewNamespaces deliberately nil — heuristic fallback.
	}

	err = RunPipelineWithSource(context.Background(), cfg, src)
	require.NoError(t, err)

	out := buf.String()
	assert.NotContains(t, out, "New namespaces detected during this run:",
		"nil NewNamespaces ⇒ heuristic fallback; small.jsonl has zero new namespaces")
	assert.NotContains(t, out, "re-run to cover it",
		"nil NewNamespaces ⇒ zero NSAW bytes (byte-identity preserved)")
}
