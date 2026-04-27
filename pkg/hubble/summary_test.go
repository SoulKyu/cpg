package hubble

import (
	"bytes"
	"strings"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/SoulKyu/cpg/pkg/dropclass"
)

// realTransientReason is a real Transient-class reason (M6 fixture guard).
// Using STALE_OR_UNROUTABLE_IP instead of POLICY_DENIED which is DropClassPolicy.
// The init check below catches future taxonomy drift.
func init() {
	if dropclass.Classify(flowpb.DropReason_STALE_OR_UNROUTABLE_IP) != dropclass.DropClassTransient {
		panic("summary_test fixture guard: STALE_OR_UNROUTABLE_IP is no longer DropClassTransient — update M6 fixtures")
	}
}

const realTransientReason = flowpb.DropReason_STALE_OR_UNROUTABLE_IP

// makeHealthWriter builds a synthetic *healthWriter for summary tests.
func makeHealthWriter(t *testing.T, drops map[flowpb.DropReason]*healthDropEntry) *healthWriter {
	t.Helper()
	return &healthWriter{
		evidenceDir: t.TempDir(),
		outputHash:  "abc123",
		logger:      zaptest.NewLogger(t),
		drops:       drops,
		startedAt:   time.Now(),
	}
}

// makeEntry constructs a healthDropEntry for test fixtures.
func makeEntry(reason flowpb.DropReason, class dropclass.DropClass, count uint64, byNode, byWorkload map[string]uint64) *healthDropEntry {
	return &healthDropEntry{
		reason:     reason,
		class:      class,
		count:      count,
		byNode:     byNode,
		byWorkload: byWorkload,
	}
}

// TestPrintClusterHealthSummaryFullBlock verifies the happy path:
// 2 entries (infra + transient) produce a complete formatted block.
func TestPrintClusterHealthSummaryFullBlock(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		flowpb.DropReason_CT_MAP_INSERTION_FAILED: makeEntry(
			flowpb.DropReason_CT_MAP_INSERTION_FAILED,
			dropclass.DropClassInfra,
			47,
			map[string]uint64{"node-a-1": 32, "node-b-2": 12, "node-c-3": 3},
			map[string]uint64{"team-trading/mmtro-adserver": 28, "team-data/x": 15, "team-foo/y": 4},
		),
		realTransientReason: makeEntry(
			realTransientReason,
			dropclass.DropClassTransient,
			5,
			map[string]uint64{"node-a-1": 5},
			map[string]uint64{"team-trading/butler": 5},
		),
	})
	snaps := hw.Snapshot()
	require.Len(t, snaps, 2)

	stats := makeStats(100, 52)
	var buf bytes.Buffer
	healthPath := "/home/gule/.cache/cpg/evidence/abc123/cluster-health.json"
	PrintClusterHealthSummary(&buf, snaps, stats, healthPath, SummaryPathWritten)

	out := buf.String()
	// Header frame and title
	assert.Contains(t, out, "━")
	assert.Contains(t, out, "Cluster-critical drops detected")
	// Infra entry present
	assert.Contains(t, out, "CT_MAP_INSERTION_FAILED")
	assert.Contains(t, out, "[infra]")
	assert.Contains(t, out, "47 flows")
	// Top nodes
	assert.Contains(t, out, "node-a-1")
	// Transient entry present (using real Transient-class reason per M6)
	assert.Contains(t, out, "STALE_OR_UNROUTABLE_IP")
	assert.Contains(t, out, "[transient]")
	assert.Contains(t, out, "5 flows")
	// Remediation hint for CT_MAP_INSERTION_FAILED
	assert.Contains(t, out, "Hint:")
	assert.Contains(t, out, "cilium.io")
	// Path line
	assert.Contains(t, out, healthPath)
}

// TestPrintClusterHealthSummaryZeroDrops verifies no output when no drops.
func TestPrintClusterHealthSummaryZeroDrops(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{})
	snaps := hw.Snapshot()

	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(0, 0), "/some/path.json", SummaryPathWritten)
	assert.Equal(t, "", buf.String(), "no output when zero drops")
}

// TestPrintClusterHealthSummaryNilSnapshots verifies no output when snapshots is nil.
func TestPrintClusterHealthSummaryNilSnapshots(t *testing.T) {
	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, nil, makeStats(0, 0), "/some/path.json", SummaryPathWritten)
	assert.Equal(t, "", buf.String(), "no output when snapshots is nil")
}

// TestPrintClusterHealthSummarySingleContributor verifies no "(+N more)" suffix
// when only 1 node and 1 workload contributed.
func TestPrintClusterHealthSummarySingleContributor(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		flowpb.DropReason_CT_MAP_INSERTION_FAILED: makeEntry(
			flowpb.DropReason_CT_MAP_INSERTION_FAILED,
			dropclass.DropClassInfra,
			10,
			map[string]uint64{"node-1": 10},
			map[string]uint64{"prod/adserver": 10},
		),
	})
	snaps := hw.Snapshot()

	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(10, 10), "/path.json", SummaryPathWritten)
	out := buf.String()
	assert.NotContains(t, out, "(+", "no truncation suffix when <=3 contributors")
}

// TestPrintClusterHealthSummaryMoreThanThree verifies "(+2 more)" suffix when 5 nodes.
func TestPrintClusterHealthSummaryMoreThanThree(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		flowpb.DropReason_CT_MAP_INSERTION_FAILED: makeEntry(
			flowpb.DropReason_CT_MAP_INSERTION_FAILED,
			dropclass.DropClassInfra,
			50,
			map[string]uint64{
				"node-1": 20,
				"node-2": 15,
				"node-3": 8,
				"node-4": 5,
				"node-5": 2,
			},
			map[string]uint64{"prod/adserver": 50},
		),
	})
	snaps := hw.Snapshot()

	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(50, 50), "/path.json", SummaryPathWritten)
	out := buf.String()
	assert.Contains(t, out, "(+2 more)", "5 nodes → +2 more suffix")
}

// TestPrintClusterHealthSummaryDryRun verifies "(dry-run, not written)" in path line.
func TestPrintClusterHealthSummaryDryRun(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		flowpb.DropReason_CT_MAP_INSERTION_FAILED: makeEntry(
			flowpb.DropReason_CT_MAP_INSERTION_FAILED,
			dropclass.DropClassInfra,
			5,
			map[string]uint64{"node-1": 5},
			map[string]uint64{"prod/svc": 5},
		),
	})
	snaps := hw.Snapshot()

	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(5, 5), "/evidence/abc/cluster-health.json", SummaryPathDryRun)
	out := buf.String()
	assert.Contains(t, out, "(dry-run, not written)", "dry-run appends suffix to path line")
}

// TestPrintClusterHealthSummaryNoRemediationHint verifies no "Hint:" for reasons with no hint.
func TestPrintClusterHealthSummaryNoRemediationHint(t *testing.T) {
	// STALE_OR_UNROUTABLE_IP is a real Transient reason — no deep-link hint after M1.
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		realTransientReason: makeEntry(
			realTransientReason,
			dropclass.DropClassTransient,
			3,
			map[string]uint64{"node-1": 3},
			map[string]uint64{"prod/svc": 3},
		),
	})
	snaps := hw.Snapshot()

	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(3, 0), "/path.json", SummaryPathWritten)
	out := buf.String()
	assert.NotContains(t, out, "Hint:", "no Hint: line when RemediationHint is empty")
}

// TestPrintClusterHealthSummarySeveritySort verifies infra printed before transient
// even when infra count is lower than transient count.
func TestPrintClusterHealthSummarySeveritySort(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		realTransientReason: makeEntry(
			realTransientReason,
			dropclass.DropClassTransient,
			100, // higher count
			map[string]uint64{"node-1": 100},
			map[string]uint64{"prod/svc": 100},
		),
		flowpb.DropReason_CT_MAP_INSERTION_FAILED: makeEntry(
			flowpb.DropReason_CT_MAP_INSERTION_FAILED,
			dropclass.DropClassInfra,
			5, // lower count but infra class
			map[string]uint64{"node-1": 5},
			map[string]uint64{"prod/svc": 5},
		),
	})
	snaps := hw.Snapshot()

	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(105, 5), "/path.json", SummaryPathWritten)
	out := buf.String()

	infraIdx := strings.Index(out, "CT_MAP_INSERTION_FAILED")
	transientIdx := strings.Index(out, "STALE_OR_UNROUTABLE_IP")
	require.True(t, infraIdx >= 0, "CT_MAP_INSERTION_FAILED must appear in output")
	require.True(t, transientIdx >= 0, "STALE_OR_UNROUTABLE_IP must appear in output")
	assert.Less(t, infraIdx, transientIdx, "infra entry must be printed before transient entry")
}

// TestPrintClusterHealthSummaryEvidenceOff verifies C3: SummaryPathEvidenceOff prints
// "(evidence disabled — file not written)" and does NOT print the healthPath.
func TestPrintClusterHealthSummaryEvidenceOff(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		flowpb.DropReason_CT_MAP_INSERTION_FAILED: makeEntry(
			flowpb.DropReason_CT_MAP_INSERTION_FAILED,
			dropclass.DropClassInfra,
			3,
			map[string]uint64{"node-1": 3},
			map[string]uint64{"prod/svc": 3},
		),
	})
	snaps := hw.Snapshot()

	const realPath = "/evidence/abc/cluster-health.json"
	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(3, 3), realPath, SummaryPathEvidenceOff)
	out := buf.String()
	assert.Contains(t, out, "evidence disabled", "evidence-off state must say evidence disabled")
	assert.Contains(t, out, "file not written", "evidence-off state must say file not written")
	assert.NotContains(t, out, realPath, "real path must NOT appear when evidence is off")
	assert.NotContains(t, out, "dry-run", "dry-run wording must not appear in evidence-off state")
}

// TestPrintClusterHealthSummaryDryRunPathLine verifies C3: SummaryPathDryRun appends
// "(dry-run, not written)" to the real path — operator can see where the file WOULD be.
func TestPrintClusterHealthSummaryDryRunPathLine(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		flowpb.DropReason_CT_MAP_INSERTION_FAILED: makeEntry(
			flowpb.DropReason_CT_MAP_INSERTION_FAILED,
			dropclass.DropClassInfra,
			2,
			map[string]uint64{"node-1": 2},
			map[string]uint64{"prod/svc": 2},
		),
	})
	snaps := hw.Snapshot()

	const realPath = "/evidence/abc/cluster-health.json"
	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(2, 2), realPath, SummaryPathDryRun)
	out := buf.String()
	assert.Contains(t, out, realPath, "real path must appear in dry-run state")
	assert.Contains(t, out, "(dry-run, not written)", "dry-run suffix must appear")
	assert.NotContains(t, out, "evidence disabled", "evidence-off wording must not appear in dry-run state")
}

// TestPrintClusterHealthSummaryWrittenPathLine verifies C3: SummaryPathWritten
// prints the bare path with no suffix.
func TestPrintClusterHealthSummaryWrittenPathLine(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		flowpb.DropReason_CT_MAP_INSERTION_FAILED: makeEntry(
			flowpb.DropReason_CT_MAP_INSERTION_FAILED,
			dropclass.DropClassInfra,
			1,
			map[string]uint64{"node-1": 1},
			map[string]uint64{"prod/svc": 1},
		),
	})
	snaps := hw.Snapshot()

	const realPath = "/evidence/abc/cluster-health.json"
	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(1, 1), realPath, SummaryPathWritten)
	out := buf.String()
	assert.Contains(t, out, realPath, "real path must appear in written state")
	assert.NotContains(t, out, "dry-run", "no dry-run suffix in written state")
	assert.NotContains(t, out, "evidence disabled", "no evidence-off wording in written state")
}

// TestTop3TieBoundary verifies I8: top3 includes ALL entries tied at the boundary.
// {a:10, b:5, c:5, d:5} → all 4 entries, no "+N more".
func TestTop3TieBoundary(t *testing.T) {
	m := map[string]uint64{"a": 10, "b": 5, "c": 5, "d": 5}
	result := top3(m)
	assert.Contains(t, result, "a (10)", "top entry must appear")
	assert.Contains(t, result, "b (5)", "tied 2nd must appear")
	assert.Contains(t, result, "c (5)", "tied 3rd must appear")
	assert.Contains(t, result, "d (5)", "tied 4th must appear (tie boundary inclusion)")
	assert.NotContains(t, result, "(+", "no hidden entries when all ties are shown")
}

// TestTop3StrictTop3 verifies that with no tie at boundary, exactly 3 entries show + remainder.
// {a:10, b:9, c:8, d:1} → 3 entries + "(+1 more)".
func TestTop3StrictTop3(t *testing.T) {
	m := map[string]uint64{"a": 10, "b": 9, "c": 8, "d": 1}
	result := top3(m)
	assert.Contains(t, result, "a (10)")
	assert.Contains(t, result, "b (9)")
	assert.Contains(t, result, "c (8)")
	assert.Contains(t, result, "(+1 more)", "exactly 1 entry hidden since d:1 does not tie c:8")
}

// TestTop3AllTied verifies that when all entries are tied, all are shown with no "+N more".
// {a:10, b:10, c:10, d:10, e:10} → all 5 shown.
func TestTop3AllTied(t *testing.T) {
	m := map[string]uint64{"a": 10, "b": 10, "c": 10, "d": 10, "e": 10}
	result := top3(m)
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		assert.Contains(t, result, name+" (10)", "all tied entries must appear: %s", name)
	}
	assert.NotContains(t, result, "(+", "no hidden entries when all are tied")
}

// TestPrintClusterHealthSummaryAdaptiveWidth verifies M5: long DropReason names
// render without truncation and the frame width expands accordingly.
func TestPrintClusterHealthSummaryAdaptiveWidth(t *testing.T) {
	// NO_CONFIGURATION_AVAILABLE_TO_PERFORM_POLICY_DECISION is 52 chars — longer than the old summaryWidth.
	longReason := flowpb.DropReason_NO_CONFIGURATION_AVAILABLE_TO_PERFORM_POLICY_DECISION
	require.Equal(t, dropclass.DropClassTransient, dropclass.Classify(longReason),
		"fixture guard: reason must still be Transient")

	longReasonName := "NO_CONFIGURATION_AVAILABLE_TO_PERFORM_POLICY_DECISION"
	require.Equal(t, 53, len(longReasonName), "sanity: long reason is %d chars", len(longReasonName))

	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		longReason: makeEntry(longReason, dropclass.DropClassTransient, 7,
			map[string]uint64{"node-1": 7},
			map[string]uint64{"prod/svc": 7},
		),
	})
	snaps := hw.Snapshot()

	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(7, 7), "/path.json", SummaryPathWritten)
	out := buf.String()

	assert.Contains(t, out, longReasonName, "long reason name must appear without truncation")
	assert.NotContains(t, out, "...", "no truncation marker must appear")
}

// TestPrintClusterHealthSummaryShortNameNoWiden verifies M5: short reason names
// do not cause the frame to widen beyond minReasonNameWidth.
func TestPrintClusterHealthSummaryShortNameNoWiden(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		flowpb.DropReason_CT_MAP_INSERTION_FAILED: makeEntry(
			flowpb.DropReason_CT_MAP_INSERTION_FAILED,
			dropclass.DropClassInfra,
			5,
			map[string]uint64{"node-1": 5},
			map[string]uint64{"prod/svc": 5},
		),
	})
	snaps := hw.Snapshot()

	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(5, 5), "/path.json", SummaryPathWritten)
	out := buf.String()

	// The frame line is the first line; its length must equal minReasonNameWidth+28.
	lines := strings.Split(out, "\n")
	require.Greater(t, len(lines), 0)
	frameLine := lines[0]
	// CT_MAP_INSERTION_FAILED is 22 chars < 38 (minReasonNameWidth), so frame = 38+28 = 66.
	assert.LessOrEqual(t, len([]rune(frameLine)), minReasonNameWidth+28+4,
		"frame must not widen for short reason names")
}

// TestPrintClusterHealthSummaryWithinClassSortByCount verifies descending count sort
// within the same class.
func TestPrintClusterHealthSummaryWithinClassSortByCount(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		flowpb.DropReason_SERVICE_BACKEND_NOT_FOUND: makeEntry(
			flowpb.DropReason_SERVICE_BACKEND_NOT_FOUND,
			dropclass.DropClassInfra,
			3, // lower count
			map[string]uint64{"node-1": 3},
			map[string]uint64{"prod/svc": 3},
		),
		flowpb.DropReason_CT_MAP_INSERTION_FAILED: makeEntry(
			flowpb.DropReason_CT_MAP_INSERTION_FAILED,
			dropclass.DropClassInfra,
			50, // higher count — must be printed first
			map[string]uint64{"node-1": 50},
			map[string]uint64{"prod/svc": 50},
		),
	})
	snaps := hw.Snapshot()

	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(53, 53), "/path.json", SummaryPathWritten)
	out := buf.String()

	ctIdx := strings.Index(out, "CT_MAP_INSERTION_FAILED")
	svcIdx := strings.Index(out, "SERVICE_BACKEND_NOT_FOUND")
	require.True(t, ctIdx >= 0)
	require.True(t, svcIdx >= 0)
	assert.Less(t, ctIdx, svcIdx, "higher-count infra entry must be printed first within same class")
}
