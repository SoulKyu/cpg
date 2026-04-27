package hubble

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/SoulKyu/cpg/pkg/dropclass"
)

// makeStats returns a minimal SessionStats for finalize() tests.
func makeStats(flowsSeen, infraDropTotal uint64) *SessionStats {
	return &SessionStats{
		StartTime:      time.Now().Add(-1 * time.Minute),
		FlowsSeen:      flowsSeen,
		InfraDropTotal: infraDropTotal,
	}
}

// makeDropEvent builds a DropEvent for tests.
func makeDropEvent(reason flowpb.DropReason, class dropclass.DropClass, node, workload string) DropEvent {
	return DropEvent{
		Reason:    reason,
		Class:     class,
		Namespace: "prod",
		Workload:  workload,
		NodeName:  node,
	}
}

// TestHealthWriterSchemaVersion: finalize writes schema_version=1.
func TestHealthWriterSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	hw := newHealthWriter(dir, "testhash", zaptest.NewLogger(t), time.Now())
	hw.accumulate(makeDropEvent(
		flowpb.DropReason_CT_MAP_INSERTION_FAILED,
		dropclass.DropClassInfra,
		"node-1", "adserver",
	))
	stats := makeStats(5, 1)
	require.NoError(t, hw.finalize(stats))

	data, err := os.ReadFile(filepath.Join(dir, "testhash", "cluster-health.json"))
	require.NoError(t, err)

	var report map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &report))
	assert.Equal(t, float64(1), report["schema_version"])
}

// TestHealthWriterClassifierVersion: finalize embeds dropclass.ClassifierVersion.
func TestHealthWriterClassifierVersion(t *testing.T) {
	dir := t.TempDir()
	hw := newHealthWriter(dir, "testhash", zaptest.NewLogger(t), time.Now())
	hw.accumulate(makeDropEvent(
		flowpb.DropReason_CT_MAP_INSERTION_FAILED,
		dropclass.DropClassInfra,
		"node-1", "adserver",
	))
	require.NoError(t, hw.finalize(makeStats(1, 1)))

	data, err := os.ReadFile(filepath.Join(dir, "testhash", "cluster-health.json"))
	require.NoError(t, err)

	var report map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &report))
	assert.Equal(t, dropclass.ClassifierVersion, report["classifier_version"])
}

// TestHealthWriterCounterAccumulation: 3 DropEvents with same reason → drops[0].count=3.
func TestHealthWriterCounterAccumulation(t *testing.T) {
	dir := t.TempDir()
	hw := newHealthWriter(dir, "testhash", zaptest.NewLogger(t), time.Now())

	for i := 0; i < 3; i++ {
		hw.accumulate(makeDropEvent(
			flowpb.DropReason_CT_MAP_INSERTION_FAILED,
			dropclass.DropClassInfra,
			"node-1", "adserver",
		))
	}
	require.NoError(t, hw.finalize(makeStats(3, 3)))

	data, err := os.ReadFile(filepath.Join(dir, "testhash", "cluster-health.json"))
	require.NoError(t, err)

	var report clusterHealthReport
	require.NoError(t, json.Unmarshal(data, &report))
	require.Len(t, report.Drops, 1)
	assert.Equal(t, uint64(3), report.Drops[0].Count)
}

// TestHealthWriterByNodeCounter: 2 events from "node-1", 1 from "node-2" → by_node correct.
func TestHealthWriterByNodeCounter(t *testing.T) {
	dir := t.TempDir()
	hw := newHealthWriter(dir, "testhash", zaptest.NewLogger(t), time.Now())

	hw.accumulate(makeDropEvent(flowpb.DropReason_CT_MAP_INSERTION_FAILED, dropclass.DropClassInfra, "node-1", "adserver"))
	hw.accumulate(makeDropEvent(flowpb.DropReason_CT_MAP_INSERTION_FAILED, dropclass.DropClassInfra, "node-1", "adserver"))
	hw.accumulate(makeDropEvent(flowpb.DropReason_CT_MAP_INSERTION_FAILED, dropclass.DropClassInfra, "node-2", "adserver"))
	require.NoError(t, hw.finalize(makeStats(3, 3)))

	data, err := os.ReadFile(filepath.Join(dir, "testhash", "cluster-health.json"))
	require.NoError(t, err)

	var report clusterHealthReport
	require.NoError(t, json.Unmarshal(data, &report))
	require.Len(t, report.Drops, 1)
	assert.Equal(t, uint64(2), report.Drops[0].ByNode["node-1"])
	assert.Equal(t, uint64(1), report.Drops[0].ByNode["node-2"])
}

// TestHealthWriterByWorkloadCounter: events from different workloads → by_workload correct.
func TestHealthWriterByWorkloadCounter(t *testing.T) {
	dir := t.TempDir()
	hw := newHealthWriter(dir, "testhash", zaptest.NewLogger(t), time.Now())

	hw.accumulate(makeDropEvent(flowpb.DropReason_CT_MAP_INSERTION_FAILED, dropclass.DropClassInfra, "node-1", "adserver"))
	hw.accumulate(makeDropEvent(flowpb.DropReason_CT_MAP_INSERTION_FAILED, dropclass.DropClassInfra, "node-1", "frontend"))
	hw.accumulate(makeDropEvent(flowpb.DropReason_CT_MAP_INSERTION_FAILED, dropclass.DropClassInfra, "node-1", "adserver"))
	require.NoError(t, hw.finalize(makeStats(3, 3)))

	data, err := os.ReadFile(filepath.Join(dir, "testhash", "cluster-health.json"))
	require.NoError(t, err)

	var report clusterHealthReport
	require.NoError(t, json.Unmarshal(data, &report))
	require.Len(t, report.Drops, 1)
	// workload key: "prod/adserver" and "prod/frontend" (namespace from DropEvent + workload)
	assert.Equal(t, uint64(2), report.Drops[0].ByWorkload["prod/adserver"])
	assert.Equal(t, uint64(1), report.Drops[0].ByWorkload["prod/frontend"])
}

// TestHealthWriterAtomicWrite: finalize writes to correct path and file is valid JSON.
func TestHealthWriterAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	hw := newHealthWriter(dir, "abc123", zaptest.NewLogger(t), time.Now())
	hw.accumulate(makeDropEvent(flowpb.DropReason_CT_MAP_INSERTION_FAILED, dropclass.DropClassInfra, "node-1", "adserver"))
	require.NoError(t, hw.finalize(makeStats(1, 1)))

	expectedPath := filepath.Join(dir, "abc123", "cluster-health.json")
	data, err := os.ReadFile(expectedPath)
	require.NoError(t, err, "cluster-health.json must exist at evidence dir + hash + filename")

	var report clusterHealthReport
	require.NoError(t, json.Unmarshal(data, &report), "file must be valid JSON")
}

// TestHealthWriterNoWriteOnZeroDrops: accumulate nothing → finalize() returns nil, no file.
func TestHealthWriterNoWriteOnZeroDrops(t *testing.T) {
	dir := t.TempDir()
	hw := newHealthWriter(dir, "testhash", zaptest.NewLogger(t), time.Now())
	require.NoError(t, hw.finalize(makeStats(0, 0)))

	_, err := os.Stat(filepath.Join(dir, "testhash", "cluster-health.json"))
	assert.True(t, os.IsNotExist(err), "cluster-health.json must NOT be written when zero drops")
}

// TestHealthWriterNilSafe: nil *healthWriter → finalize(stats) returns nil (no panic).
func TestHealthWriterNilSafe(t *testing.T) {
	var hw *healthWriter
	require.NoError(t, hw.finalize(makeStats(0, 0)))
}

// TestHealthWriterDryRun: hw=nil (dry-run simulation) → finalize is no-op, no file written.
func TestHealthWriterDryRun(t *testing.T) {
	dir := t.TempDir()
	// Simulate dry-run: hw is nil (not constructed)
	var hw *healthWriter
	require.NoError(t, hw.finalize(makeStats(5, 2)))

	entries, _ := os.ReadDir(dir)
	assert.Empty(t, entries, "no files should be written when hw is nil (dry-run)")
}

// TestHealthWriterSessionBlock: session.flows_seen and infra_drops_total populated from stats.
func TestHealthWriterSessionBlock(t *testing.T) {
	dir := t.TempDir()
	hw := newHealthWriter(dir, "testhash", zaptest.NewLogger(t), time.Now())
	hw.accumulate(makeDropEvent(flowpb.DropReason_CT_MAP_INSERTION_FAILED, dropclass.DropClassInfra, "node-1", "adserver"))
	stats := makeStats(42, 7)
	require.NoError(t, hw.finalize(stats))

	data, err := os.ReadFile(filepath.Join(dir, "testhash", "cluster-health.json"))
	require.NoError(t, err)

	var report clusterHealthReport
	require.NoError(t, json.Unmarshal(data, &report))
	assert.Equal(t, uint64(42), report.Session.FlowsSeen)
	assert.Equal(t, uint64(7), report.Session.InfraDropTotal)
	assert.False(t, report.Session.Started.IsZero(), "session.started must be set")
	assert.False(t, report.Session.Ended.IsZero(), "session.ended must be set")
}

// TestHealthWriterSnapshotIdempotent verifies I4: Snapshot() returns the same
// (deep-equal) result on subsequent calls; 8 concurrent callers all see identical
// content after accumulation is complete.
func TestHealthWriterSnapshotIdempotent(t *testing.T) {
	dir := t.TempDir()
	hw := newHealthWriter(dir, "testhash", zaptest.NewLogger(t), time.Now())

	// Accumulate 100 events sequentially (simulating Stage 2c goroutine).
	for i := 0; i < 100; i++ {
		hw.accumulate(makeDropEvent(flowpb.DropReason_CT_MAP_INSERTION_FAILED, dropclass.DropClassInfra, "node-1", "svc"))
	}

	// All 8 goroutines call Snapshot() "after" g.Wait() (sequentially here, but
	// the idempotency contract must hold regardless).
	const goroutines = 8
	results := make([][]HealthDropSnapshot, goroutines)
	done := make(chan int, goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			results[i] = hw.Snapshot()
			done <- i
		}()
	}
	for range results {
		<-done
	}

	// All results must be non-nil and equal.
	require.NotNil(t, results[0], "Snapshot() must return non-nil after accumulation")
	for i := 1; i < goroutines; i++ {
		require.Equal(t, len(results[0]), len(results[i]), "goroutine %d snapshot length differs", i)
		if len(results[0]) > 0 {
			assert.Equal(t, results[0][0].Count, results[i][0].Count, "goroutine %d count differs", i)
		}
	}
}

// TestHealthWriterSnapshotNilSafe verifies Snapshot() on nil hw returns nil.
func TestHealthWriterSnapshotNilSafe(t *testing.T) {
	var hw *healthWriter
	assert.Nil(t, hw.Snapshot(), "nil hw must return nil from Snapshot()")
}

// TestHealthWriterDropsSorted: multiple reasons → drops array sorted by reason name.
func TestHealthWriterDropsSorted(t *testing.T) {
	dir := t.TempDir()
	hw := newHealthWriter(dir, "testhash", zaptest.NewLogger(t), time.Now())

	// Add in reverse alphabetical order to verify sorting
	hw.accumulate(makeDropEvent(flowpb.DropReason_SERVICE_BACKEND_NOT_FOUND, dropclass.DropClassInfra, "node-1", "svc"))
	hw.accumulate(makeDropEvent(flowpb.DropReason_CT_MAP_INSERTION_FAILED, dropclass.DropClassInfra, "node-1", "adserver"))
	require.NoError(t, hw.finalize(makeStats(2, 2)))

	data, err := os.ReadFile(filepath.Join(dir, "testhash", "cluster-health.json"))
	require.NoError(t, err)

	var report clusterHealthReport
	require.NoError(t, json.Unmarshal(data, &report))
	require.Len(t, report.Drops, 2)
	// Verify sorted by reason name (CT_MAP_INSERTION_FAILED < SERVICE_BACKEND_NOT_FOUND)
	assert.Less(t, report.Drops[0].Reason, report.Drops[1].Reason,
		"drops must be sorted by reason name: got %s, %s", report.Drops[0].Reason, report.Drops[1].Reason)
}
