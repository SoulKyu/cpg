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
		flowpb.DropReason_POLICY_DENIED: makeEntry(
			flowpb.DropReason_POLICY_DENIED,
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
	PrintClusterHealthSummary(&buf, snaps, stats, healthPath, false)

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
	// Transient entry present
	assert.Contains(t, out, "POLICY_DENIED")
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
	PrintClusterHealthSummary(&buf, snaps, makeStats(0, 0), "/some/path.json", false)
	assert.Equal(t, "", buf.String(), "no output when zero drops")
}

// TestPrintClusterHealthSummaryNilSnapshots verifies no output when snapshots is nil.
func TestPrintClusterHealthSummaryNilSnapshots(t *testing.T) {
	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, nil, makeStats(0, 0), "/some/path.json", false)
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
	PrintClusterHealthSummary(&buf, snaps, makeStats(10, 10), "/path.json", false)
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
	PrintClusterHealthSummary(&buf, snaps, makeStats(50, 50), "/path.json", false)
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
	PrintClusterHealthSummary(&buf, snaps, makeStats(5, 5), "/evidence/abc/cluster-health.json", true)
	out := buf.String()
	assert.Contains(t, out, "(dry-run, not written)", "dry-run appends suffix to path line")
}

// TestPrintClusterHealthSummaryNoRemediationHint verifies no "Hint:" for reasons with no hint.
func TestPrintClusterHealthSummaryNoRemediationHint(t *testing.T) {
	// POLICY_DENIED is a transient/policy reason — no hint in RemediationHint map.
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		flowpb.DropReason_POLICY_DENIED: makeEntry(
			flowpb.DropReason_POLICY_DENIED,
			dropclass.DropClassTransient,
			3,
			map[string]uint64{"node-1": 3},
			map[string]uint64{"prod/svc": 3},
		),
	})
	snaps := hw.Snapshot()

	var buf bytes.Buffer
	PrintClusterHealthSummary(&buf, snaps, makeStats(3, 0), "/path.json", false)
	out := buf.String()
	assert.NotContains(t, out, "Hint:", "no Hint: line when RemediationHint is empty")
}

// TestPrintClusterHealthSummarySeveritySort verifies infra printed before transient
// even when infra count is lower than transient count.
func TestPrintClusterHealthSummarySeveritySort(t *testing.T) {
	hw := makeHealthWriter(t, map[flowpb.DropReason]*healthDropEntry{
		flowpb.DropReason_POLICY_DENIED: makeEntry(
			flowpb.DropReason_POLICY_DENIED,
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
	PrintClusterHealthSummary(&buf, snaps, makeStats(105, 5), "/path.json", false)
	out := buf.String()

	infraIdx := strings.Index(out, "CT_MAP_INSERTION_FAILED")
	transientIdx := strings.Index(out, "POLICY_DENIED")
	require.True(t, infraIdx >= 0, "CT_MAP_INSERTION_FAILED must appear in output")
	require.True(t, transientIdx >= 0, "POLICY_DENIED must appear in output")
	assert.Less(t, infraIdx, transientIdx, "infra entry must be printed before transient entry")
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
	PrintClusterHealthSummary(&buf, snaps, makeStats(53, 53), "/path.json", false)
	out := buf.String()

	ctIdx := strings.Index(out, "CT_MAP_INSERTION_FAILED")
	svcIdx := strings.Index(out, "SERVICE_BACKEND_NOT_FOUND")
	require.True(t, ctIdx >= 0)
	require.True(t, svcIdx >= 0)
	assert.Less(t, ctIdx, svcIdx, "higher-count infra entry must be printed first within same class")
}
