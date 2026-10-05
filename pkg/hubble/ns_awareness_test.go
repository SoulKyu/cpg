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

// TestFirstSeenZeroTime verifies the fail-safe timestamp edges (Pitfall 3):
// flows with (a) nil Time, (b) a zero-value (Go year-1) timestamp, and (c) an
// invalid timestamppb record NOTHING into the first-seen map; a capture
// consisting ONLY of such flows yields an empty report — computeNewNamespaces
// returns nil AND PrintNewNamespacesSummary writes zero bytes (fail-safe
// empty, mirroring the summary no-op — never a crash, never a bogus report).
func TestFirstSeenZeroTime(t *testing.T) {
	later := nsawTime.Add(10 * time.Minute)

	t.Run("nil_time_records_nothing", func(t *testing.T) {
		m := map[string]time.Time{}
		noteFirstSeen(m, nsawFlow(time.Time{}, "default", "production"))
		assert.Empty(t, m, "nil Flow.Time must not be recorded")
	})

	t.Run("zero_value_timestamp_records_nothing", func(t *testing.T) {
		m := map[string]time.Time{}
		f := nsawFlow(later, "default", "production")
		f.Time = timestamppb.New(time.Time{}) // Go zero time (year 1), IsZero via AsTime
		noteFirstSeen(m, f)
		assert.Empty(t, m, "zero-value Flow.Time must not be recorded")
	})

	t.Run("invalid_timestamp_records_nothing", func(t *testing.T) {
		m := map[string]time.Time{}
		f := nsawFlow(later, "default", "production")
		f.Time = &timestamppb.Timestamp{Seconds: 1, Nanos: -1} // IsValid() == false
		noteFirstSeen(m, f)
		assert.Empty(t, m, "invalid Flow.Time must not be recorded")
	})

	t.Run("all_untimestamped_capture_reports_nothing", func(t *testing.T) {
		m := map[string]time.Time{}
		noteFirstSeen(m, nsawFlow(time.Time{}, "default", "production"))
		noteFirstSeen(m, nsawFlow(time.Time{}, "latecomer", "production"))

		assert.Nil(t, computeNewNamespaces(m), "no valid timestamps ⇒ nil report")

		var buf bytes.Buffer
		PrintNewNamespacesSummary(&buf, computeNewNamespaces(m), NewNamespacesFromReplay)
		assert.Equal(t, "", buf.String(), "all-untimestamped capture must write zero bytes")
	})
}

// TestFirstSeenNamespaces_CaptureStartEquality verifies the strict-After
// adjacency edge: namespaces sharing the MINIMUM first-seen timestamp are all
// treated as pre-existing (flagged assumption — the heuristic cannot
// distinguish a namespace that existed at capture start from one created in
// the same instant as the first flow). Namespaces sharing a LATER first-seen
// (strictly after capture start) are ALL reported, sorted.
func TestFirstSeenNamespaces_CaptureStartEquality(t *testing.T) {
	t0 := nsawTime
	mid := t0.Add(5 * time.Minute)
	later := t0.Add(10 * time.Minute)

	t.Run("equal_to_capture_start_neither_reported", func(t *testing.T) {
		m := map[string]time.Time{}
		noteFirstSeen(m, nsawFlow(t0, "default", "production"))
		assert.Nil(t, computeNewNamespaces(m),
			"namespaces first-seen exactly at capture start must NOT be reported")
	})

	t.Run("equal_later_first_seen_both_reported_sorted", func(t *testing.T) {
		m := map[string]time.Time{}
		noteFirstSeen(m, nsawFlow(t0, "default", "production"))
		noteFirstSeen(m, nsawFlow(mid, "alpha", "beta"))
		noteFirstSeen(m, nsawFlow(later, "latecomer", "x"))

		got := computeNewNamespaces(m)
		assert.Equal(t, []string{"alpha", "beta", "latecomer", "x"}, got,
			"alpha/beta share a mid-capture first-seen strictly after capture start — both reported, sorted")
	})
}

// TestFirstSeenNamespaces_SortedDeterministic verifies the ordering edge:
// recording namespaces in non-alphabetical arrival order still yields a
// lexicographically sorted report, and PrintNewNamespacesSummary output is
// byte-identical across calls with differently-ordered copies of the same set
// (deterministic output regardless of map iteration / flow arrival order).
func TestFirstSeenNamespaces_SortedDeterministic(t *testing.T) {
	t0 := nsawTime
	later := t0.Add(10 * time.Minute)

	t.Run("arrival_order_does_not_affect_sorting", func(t *testing.T) {
		m := map[string]time.Time{}
		noteFirstSeen(m, nsawFlow(t0, "default", "production"))
		noteFirstSeen(m, nsawFlow(later, "zeta", ""))
		noteFirstSeen(m, nsawFlow(later.Add(time.Minute), "alpha", ""))

		assert.Equal(t, []string{"alpha", "zeta"}, computeNewNamespaces(m))
	})

	t.Run("print_output_byte_identical_across_input_orderings", func(t *testing.T) {
		var buf1, buf2 bytes.Buffer
		PrintNewNamespacesSummary(&buf1, []string{"zeta", "alpha"}, NewNamespacesFromReplay)
		PrintNewNamespacesSummary(&buf2, []string{"alpha", "zeta"}, NewNamespacesFromReplay)

		assert.Equal(t, buf1.String(), buf2.String(), "differently-ordered copies of the same set must print identically")
	})
}

// TestFirstSeenNamespaces_EmptyNamespaceName verifies the empty-name guard:
// endpoints carrying namespace "" (or absent endpoints) record nothing under
// the empty key — "" is never treated as a reportable namespace.
func TestFirstSeenNamespaces_EmptyNamespaceName(t *testing.T) {
	later := nsawTime.Add(10 * time.Minute)

	m := map[string]time.Time{}
	// Explicit endpoints present but with empty namespace strings.
	f1 := nsawFlow(nsawTime, "", "")
	f1.Source = &flowpb.Endpoint{}
	f1.Destination = &flowpb.Endpoint{}
	noteFirstSeen(m, f1)
	// Nil endpoints entirely.
	noteFirstSeen(m, &flowpb.Flow{Time: timestamppb.New(later)})
	// Mixed: one empty-name endpoint alongside a real one.
	f3 := nsawFlow(later, "default", "")
	noteFirstSeen(m, f3)

	_, hasEmpty := m[""]
	assert.False(t, hasEmpty, "empty-string namespace key must never be recorded")
	assert.Equal(t, map[string]time.Time{"default": later}, m,
		"only the real namespace (min of its observations) is recorded")
}
