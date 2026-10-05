package hubble

import (
	"bytes"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// nsawFlow builds a minimal flowpb.Flow with the given endpoint namespaces and
// timestamp for first-seen unit tests. A zero t0 leaves Flow.Time nil (the
// untimestamped-flow edge).
func nsawFlow(t0 time.Time, srcNS, dstNS string) *flowpb.Flow {
	f := &flowpb.Flow{Verdict: flowpb.Verdict_DROPPED}
	if srcNS != "" {
		f.Source = &flowpb.Endpoint{Namespace: srcNS}
	}
	if dstNS != "" {
		f.Destination = &flowpb.Endpoint{Namespace: dstNS}
	}
	if !t0.IsZero() {
		f.Time = timestamppb.New(t0)
	}
	return f
}

// nsawTime is the capture-start timestamp shared by the first-seen unit tests.
var nsawTime = time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)

// TestFirstSeenNamespaces verifies the NSAW-02 replay heuristic end to end at
// the helper level: staggered first-seen timestamps yield exactly the
// mid-capture namespaces, capture start = min observed Flow.Time, strict
// strictly-after comparison, and BOTH endpoint namespaces attributed (A5).
func TestFirstSeenNamespaces(t *testing.T) {
	t0 := nsawTime
	later := t0.Add(10 * time.Minute)

	t.Run("staggered_first_seen_reports_only_latecomer", func(t *testing.T) {
		m := map[string]time.Time{}
		noteFirstSeen(m, nsawFlow(t0, "default", "production"))
		noteFirstSeen(m, nsawFlow(later, "latecomer", "production"))

		got := computeNewNamespaces(m)
		assert.Equal(t, []string{"latecomer"}, got)
	})

	t.Run("destination_only_first_seen_at_capture_start_not_reported", func(t *testing.T) {
		// dstonly exists purely as a DESTINATION endpoint on the capture-start
		// flow — its first-seen EQUALS capture start, so strict After must
		// exclude it.
		m := map[string]time.Time{}
		noteFirstSeen(m, nsawFlow(t0, "", "dstonly"))
		noteFirstSeen(m, nsawFlow(later, "default", "production"))

		got := computeNewNamespaces(m)
		assert.NotContains(t, got, "dstonly")
	})

	t.Run("source_only_first_seen_mid_capture_is_reported", func(t *testing.T) {
		// srconly exists purely as a SOURCE endpoint on a mid-capture flow —
		// the both-endpoints attribution (A5) must report it.
		m := map[string]time.Time{}
		noteFirstSeen(m, nsawFlow(t0, "default", "production"))
		noteFirstSeen(m, nsawFlow(later, "srconly", ""))

		got := computeNewNamespaces(m)
		assert.Contains(t, got, "srconly")
	})
}

// TestFirstSeenNamespaces_EmptyAndSingle verifies the degenerate inputs of the
// pure computation: an empty map and a single-namespace map both yield nil. A
// single namespace's first-seen IS the capture start, and strictly-after is
// impossible — the first-observed namespace can never be reported. That is
// correct heuristic behavior, not a gap: a namespace observed on the very
// first flow existed at capture start as far as the flow evidence can know.
func TestFirstSeenNamespaces_EmptyAndSingle(t *testing.T) {
	assert.Nil(t, computeNewNamespaces(nil))
	assert.Nil(t, computeNewNamespaces(map[string]time.Time{}))
	assert.Nil(t, computeNewNamespaces(map[string]time.Time{
		"default": nsawTime,
	}))
}

// TestPrintNewNamespacesSummary_ReplayCopy pins the exact replay-path copy of
// the new-namespace report block (planner-pinned in 26-01 <interfaces>).
func TestPrintNewNamespacesSummary_ReplayCopy(t *testing.T) {
	var buf bytes.Buffer
	PrintNewNamespacesSummary(&buf, []string{"latecomer"}, NewNamespacesFromReplay)

	out := buf.String()
	assert.Contains(t, out, "New namespaces detected during this run:")
	assert.Contains(t, out, "  - namespace latecomer first observed after capture start — re-run to cover it")
}

// TestPrintNewNamespacesSummary_EmptyAndNil verifies the byte-identity
// mechanism: an empty or nil namespace list writes ZERO bytes, mirroring
// PrintClusterHealthSummary's zero-drop guard (summary_test.go shape).
func TestPrintNewNamespacesSummary_EmptyAndNil(t *testing.T) {
	var empty, nilList bytes.Buffer
	PrintNewNamespacesSummary(&empty, []string{}, NewNamespacesFromReplay)
	PrintNewNamespacesSummary(&nilList, nil, NewNamespacesFromReplay)

	assert.Equal(t, "", empty.String(), "no output when the list is empty")
	assert.Equal(t, "", nilList.String(), "no output when the list is nil")
}
