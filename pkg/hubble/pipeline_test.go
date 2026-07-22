package hubble

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)

// mockFlowSource implements FlowSource for testing.
type mockFlowSource struct {
	flows      []*flowpb.Flow
	lostEvents []*flowpb.LostEvent
}

func (m *mockFlowSource) StreamDroppedFlows(_ context.Context, _ []string, _ bool, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	flowCh := make(chan *flowpb.Flow, len(m.flows))
	lostCh := make(chan *flowpb.LostEvent, len(m.lostEvents))

	for _, f := range m.flows {
		flowCh <- f
	}
	close(flowCh)

	for _, le := range m.lostEvents {
		lostCh <- le
	}
	close(lostCh)

	return flowCh, lostCh, nil
}

func TestRunPipeline_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)

	source := &mockFlowSource{
		flows: []*flowpb.Flow{
			testdata.IngressTCPFlow(
				[]string{"k8s:app=client"},
				[]string{"k8s:app=server"},
				"production",
				8080,
			),
			testdata.EgressUDPFlow(
				[]string{"k8s:app=server"},
				[]string{"k8s:app=dns"},
				"production",
				53,
			),
		},
	}

	cfg := PipelineConfig{
		FlushInterval: 10 * time.Millisecond,
		OutputDir:     tmpDir,
		Logger:        logger,
	}

	err := RunPipelineWithSource(context.Background(), cfg, source)
	require.NoError(t, err)

	// Check policy files were written
	serverPolicy := filepath.Join(tmpDir, "production", "server.yaml")
	_, err = os.Stat(serverPolicy)
	assert.NoError(t, err, "server policy file should exist at %s", serverPolicy)

	// Check content is valid YAML
	data, err := os.ReadFile(serverPolicy)
	require.NoError(t, err)
	assert.Contains(t, string(data), "apiVersion: cilium.io/v2")
	assert.Contains(t, string(data), "kind: CiliumNetworkPolicy")
}

// TestRunPipeline_OnFinalFiresOnce proves the D-08 contract: cfg.OnFinal is
// called exactly once at end-of-run with the fully populated SessionStats.
// This is the tested foundation 17-02's session manager wires against
// (session.final.Store(&s) inside the closure).
func TestRunPipeline_OnFinalFiresOnce(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)

	source := &mockFlowSource{
		flows: []*flowpb.Flow{
			testdata.IngressTCPFlow(
				[]string{"k8s:app=client"},
				[]string{"k8s:app=server"},
				"production",
				8080,
			),
			testdata.EgressUDPFlow(
				[]string{"k8s:app=server"},
				[]string{"k8s:app=dns"},
				"production",
				53,
			),
		},
	}

	var called int
	var captured SessionStats

	cfg := PipelineConfig{
		FlushInterval: 10 * time.Millisecond,
		OutputDir:     tmpDir,
		Logger:        logger,
		OnFinal: func(s SessionStats) {
			called++
			captured = s
		},
	}

	// RunPipelineWithSource is synchronous: it returns only after g.Wait()
	// and therefore after OnFinal has already fired. Reading called/captured
	// after this call (not from another goroutine) is race-free with no
	// additional synchronization needed.
	err := RunPipelineWithSource(context.Background(), cfg, source)
	require.NoError(t, err)

	assert.Equal(t, 1, called, "OnFinal must fire exactly once")
	assert.Equal(t, uint64(2), captured.FlowsSeen, "captured stats must be fully populated (FlowsSeen from agg.FlowsSeen())")
}

// TestRunPipeline_OnFinalNilSafe proves a nil OnFinal (every existing CLI
// path) is a pure no-op: no panic, no error.
func TestRunPipeline_OnFinalNilSafe(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)

	source := &mockFlowSource{
		flows: []*flowpb.Flow{
			testdata.IngressTCPFlow(
				[]string{"k8s:app=client"},
				[]string{"k8s:app=server"},
				"production",
				8080,
			),
			testdata.EgressUDPFlow(
				[]string{"k8s:app=server"},
				[]string{"k8s:app=dns"},
				"production",
				53,
			),
		},
	}

	cfg := PipelineConfig{
		FlushInterval: 10 * time.Millisecond,
		OutputDir:     tmpDir,
		Logger:        logger,
		// OnFinal intentionally left unset (zero value / nil).
	}

	require.NotPanics(t, func() {
		err := RunPipelineWithSource(context.Background(), cfg, source)
		require.NoError(t, err)
	})
}

func TestRunPipeline_GracefulShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)

	// Source that provides flows but doesn't close channels immediately
	flowCh := make(chan *flowpb.Flow, 10)
	lostCh := make(chan *flowpb.LostEvent, 10)

	source := &channelFlowSource{flows: flowCh, lost: lostCh}

	cfg := PipelineConfig{
		FlushInterval: 10 * time.Millisecond,
		OutputDir:     tmpDir,
		Logger:        logger,
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Send a flow
	flowCh <- testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		80,
	)

	done := make(chan error, 1)
	go func() {
		done <- RunPipelineWithSource(ctx, cfg, source)
	}()

	// Deterministic: wait until the pipeline has written the policy, then cancel.
	serverPolicy := filepath.Join(tmpDir, "default", "server.yaml")
	require.Eventually(t, func() bool {
		_, err := os.Stat(serverPolicy)
		return err == nil
	}, 5*time.Second, 5*time.Millisecond)
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err, "graceful shutdown should not return error")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for graceful shutdown")
	}

	// Policy already verified above via require.Eventually.
}

func TestSessionStats_Log(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	stats := &SessionStats{
		StartTime:       time.Now().Add(-5 * time.Minute),
		FlowsSeen:       100,
		PoliciesWritten: 10,
		LostEvents:      5,
		OutputDir:       "/tmp/policies",
	}

	stats.Log(logger)

	require.GreaterOrEqual(t, logs.Len(), 1, "should log session summary")

	entry := logs.All()[0]
	assert.Equal(t, "session summary", entry.Message)

	fieldMap := make(map[string]interface{})
	for _, f := range entry.Context {
		fieldMap[f.Key] = f.Integer
	}
	assert.Contains(t, fieldMap, "flows_seen")
	assert.Contains(t, fieldMap, "policies_written")
}

// TestRunPipeline_PopulatesLostEvents is a regression guard for the BUG-01
// class defect on lost_events: monitorLostEvents accumulated the count locally
// but never wrote it back to SessionStats, so the session summary always
// reported lost_events=0 even when Hubble dropped events.
func TestRunPipeline_PopulatesLostEvents(t *testing.T) {
	tmpDir := t.TempDir()
	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	source := &mockFlowSource{
		flows: []*flowpb.Flow{
			testdata.IngressTCPFlow(
				[]string{"k8s:app=client"},
				[]string{"k8s:app=server"},
				"production",
				8080,
			),
		},
		lostEvents: []*flowpb.LostEvent{
			{NumEventsLost: 3},
			{NumEventsLost: 4},
		},
	}

	cfg := PipelineConfig{
		FlushInterval: 10 * time.Millisecond,
		OutputDir:     tmpDir,
		Logger:        logger,
	}

	err := RunPipelineWithSource(context.Background(), cfg, source)
	require.NoError(t, err)

	entries := logs.FilterMessage("session summary").All()
	require.Len(t, entries, 1, "session summary must be logged exactly once")

	var lost int64 = -1
	for _, f := range entries[0].Context {
		if f.Key == "lost_events" {
			lost = f.Integer
		}
	}
	assert.Equal(t, int64(7), lost, "lost_events must reflect the accumulated total, not 0")
}

// errStreamSource is a FlowSource whose stream fails mid-capture: both flow
// channels close cleanly but StreamErr yields a transport error, mirroring a
// live relay crash under Follow:true.
type errStreamSource struct {
	err error
}

func (e *errStreamSource) StreamDroppedFlows(_ context.Context, _ []string, _ bool, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	fc := make(chan *flowpb.Flow)
	close(fc)
	lc := make(chan *flowpb.LostEvent)
	close(lc)
	return fc, lc, nil
}

func (e *errStreamSource) StreamErr() <-chan error {
	ec := make(chan error, 1)
	ec <- e.err
	close(ec)
	return ec
}

// TestRunPipeline_SurfacesStreamError verifies a genuine transport failure is
// propagated out of the pipeline (non-nil error / non-zero exit) instead of
// draining to a clean exit 0.
func TestRunPipeline_SurfacesStreamError(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)

	sentinel := errors.New("hubble stream failed: connection reset")
	source := &errStreamSource{err: sentinel}

	cfg := PipelineConfig{
		FlushInterval: 10 * time.Millisecond,
		OutputDir:     tmpDir,
		Logger:        logger,
	}

	err := RunPipelineWithSource(context.Background(), cfg, source)
	require.Error(t, err, "a mid-capture stream failure must not be reported as a clean run")
	assert.ErrorIs(t, err, sentinel)
}

// errStreamSourceWithInfraDrop is a FlowSource whose flow channel carries one
// pre-classified infra DROPPED flow before closing cleanly (unlike
// errStreamSource above, which closes both channels immediately empty --
// exactly why TestRunPipeline_SurfacesStreamError never accumulates a drop).
//
// The stream error is delivered only after a short delay. The aggregator
// consumes an already-buffered channel item on the very first iteration of
// its select loop -- pure in-memory bookkeeping plus one buffered channel
// send, no I/O, no blocking -- which completes in low microseconds. Without
// the delay, gctx cancellation (triggered the instant the stream-error
// goroutine returns) could in principle become ready before the aggregator's
// select evaluates, and Go's select picks uniformly at random among ready
// cases: the accumulate-then-error ordering this test exists to prove would
// then be racy rather than deterministic. The delay's margin (orders of
// magnitude larger than the in-memory work it waits out) removes that race
// for practical purposes while still letting RunPipelineWithSource return
// the genuine stream error.
type errStreamSourceWithInfraDrop struct {
	err  error
	flow *flowpb.Flow
}

func (e *errStreamSourceWithInfraDrop) StreamDroppedFlows(_ context.Context, _ []string, _ bool, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	fc := make(chan *flowpb.Flow, 1)
	fc <- e.flow
	close(fc)
	lc := make(chan *flowpb.LostEvent)
	close(lc)
	return fc, lc, nil
}

func (e *errStreamSourceWithInfraDrop) StreamErr() <-chan error {
	ec := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		ec <- e.err
		close(ec)
	}()
	return ec
}

// TestRunPipeline_FinalizesHealthOnStreamError closes the Pitfall-1 gap and
// pins the evidence behind D-13's corrected 3-way branch: hw.finalize() runs
// unconditionally after g.Wait(), so a pipeline that accumulates at least one
// infra/transient drop before a genuine stream error still writes
// cluster-health.json -- "file absent" means "zero infra/transient drops",
// not "crashed". TestRunPipeline_SurfacesStreamError (above) leaves
// EvidenceEnabled unset (hw is nil) and its errStreamSource emits zero flows,
// so it never exercises this path.
func TestRunPipeline_FinalizesHealthOnStreamError(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)

	// EGRESS + Source carrying namespace/labels mirrors the established
	// synthetic-infra-flow shape already used in this file
	// (TestRunPipeline_DryRunWithoutEvidenceRendersEvidenceOff,
	// TestRunPipeline_FallbackSnapshotNoEvidence) -- policyTargetEndpoint
	// resolves the SOURCE endpoint for EGRESS flows, giving buildDropEvent a
	// resolvable namespace/workload for the by_workload key.
	infraFlow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Verdict:          flowpb.Verdict_DROPPED,
		DropReasonDesc:   flowpb.DropReason_CT_MAP_INSERTION_FAILED, // confirmed Infra-classified + remediation-linked, pkg/dropclass/hints_test.go:15
		NodeName:         "node-1",
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=worker"},
			Namespace: "production",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=backend"},
			Namespace: "production",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{DestinationPort: 8080}},
		},
	}

	sentinel := errors.New("hubble stream failed: connection reset mid-capture")
	source := &errStreamSourceWithInfraDrop{err: sentinel, flow: infraFlow}

	outputDir := filepath.Join(tmpDir, "policies")
	evidenceDir := filepath.Join(tmpDir, "evidence")
	outputHash := evidence.HashOutputDir(outputDir)

	cfg := PipelineConfig{
		FlushInterval:   10 * time.Millisecond,
		OutputDir:       outputDir,
		Logger:          logger,
		EvidenceEnabled: true,
		EvidenceDir:     evidenceDir,
		OutputHash:      outputHash,
	}

	err := RunPipelineWithSource(context.Background(), cfg, source)
	require.Error(t, err, "a mid-capture stream failure must still surface as a non-nil error")
	assert.ErrorIs(t, err, sentinel)

	healthPath := filepath.Join(cfg.EvidenceDir, cfg.OutputHash, "cluster-health.json")
	require.FileExists(t, healthPath, "finalize() must write cluster-health.json despite the pipeline error, given an accumulated infra drop")

	report, readErr := ReadClusterHealth(healthPath)
	require.NoError(t, readErr)
	require.Len(t, report.Drops, 1)
	assert.Equal(t, "CT_MAP_INSERTION_FAILED", report.Drops[0].Reason)
	assert.GreaterOrEqual(t, report.Drops[0].Count, uint64(1))
}

// channelFlowSource returns pre-made channels for testing.
type channelFlowSource struct {
	flows chan *flowpb.Flow
	lost  chan *flowpb.LostEvent
}

func (c *channelFlowSource) StreamDroppedFlows(_ context.Context, _ []string, _ bool, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	return c.flows, c.lost, nil
}

func TestCrossFlushDedup_SkipsSamePolicy(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)

	flow := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"production",
		8080,
	)

	// Two flushes with identical flows -- second should be skipped
	flowCh := make(chan *flowpb.Flow, 10)
	lostCh := make(chan *flowpb.LostEvent)

	source := &channelFlowSource{flows: flowCh, lost: lostCh}

	cfg := PipelineConfig{
		FlushInterval: 20 * time.Millisecond,
		OutputDir:     tmpDir,
		Logger:        logger,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunPipelineWithSource(ctx, cfg, source)
	}()

	// Send flow; wait deterministically for the file to appear.
	flowCh <- flow
	serverPolicy := filepath.Join(tmpDir, "production", "server.yaml")
	require.Eventually(t, func() bool {
		_, err := os.Stat(serverPolicy)
		return err == nil
	}, 5*time.Second, 5*time.Millisecond)
	info1, err := os.Stat(serverPolicy)
	require.NoError(t, err, "policy should be written after first flush")

	// Send same flow again; wait until the pipeline has drained the buffered send
	// so the second flush tick has observed it.
	flowCh <- flow
	require.Eventually(t, func() bool { return len(flowCh) == 0 }, 5*time.Second, 5*time.Millisecond)

	// Wait until two consecutive poll samples one flush-interval apart agree on
	// ModTime. `require.Eventually` polls on its own interval, so relying on
	// distinct poll samples (state across attempts) avoids sleeping inside the
	// callback — matches the Eventually style used elsewhere in this file.
	var prevModTime time.Time
	require.Eventually(t, func() bool {
		info, err := os.Stat(serverPolicy)
		if err != nil {
			return false
		}
		current := info.ModTime()
		if prevModTime.IsZero() {
			prevModTime = current
			return false
		}
		if !current.Equal(prevModTime) {
			prevModTime = current
			return false
		}
		return true
	}, 10*time.Second, cfg.FlushInterval)

	// File should not be rewritten (cross-flush dedup)
	info2, err := os.Stat(serverPolicy)
	require.NoError(t, err)
	assert.Equal(t, info1.ModTime(), info2.ModTime(), "file should not be rewritten for identical policy (cross-flush dedup)")

	cancel()
	<-done
}

func TestCrossFlushDedup_WritesChangedPolicy(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)

	flow1 := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"production",
		8080,
	)
	flow2 := testdata.IngressTCPFlow(
		[]string{"k8s:app=newclient"},
		[]string{"k8s:app=server"},
		"production",
		9090,
	)

	flowCh := make(chan *flowpb.Flow, 10)
	lostCh := make(chan *flowpb.LostEvent)

	source := &channelFlowSource{flows: flowCh, lost: lostCh}

	cfg := PipelineConfig{
		FlushInterval: 20 * time.Millisecond,
		OutputDir:     tmpDir,
		Logger:        logger,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunPipelineWithSource(ctx, cfg, source)
	}()

	// Send flow1 and wait for the first write to land.
	flowCh <- flow1
	serverPolicy := filepath.Join(tmpDir, "production", "server.yaml")
	require.Eventually(t, func() bool {
		_, err := os.Stat(serverPolicy)
		return err == nil
	}, 5*time.Second, 5*time.Millisecond)
	data1, err := os.ReadFile(serverPolicy)
	require.NoError(t, err)

	// Send flow2 with different rules and wait for the file content to change.
	flowCh <- flow2
	require.Eventually(t, func() bool {
		data2, err := os.ReadFile(serverPolicy)
		if err != nil {
			return false
		}
		return string(data2) != string(data1)
	}, 5*time.Second, 5*time.Millisecond)
	data2, err := os.ReadFile(serverPolicy)
	require.NoError(t, err)
	assert.NotEqual(t, string(data1), string(data2), "file should be updated when policy changes")

	cancel()
	<-done
}

func TestSessionStats_PoliciesSkipped(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	lgr := zap.New(core)

	stats := &SessionStats{
		StartTime:       time.Now().Add(-5 * time.Minute),
		FlowsSeen:       100,
		PoliciesWritten: 8,
		PoliciesSkipped: 2,
		LostEvents:      0,
		OutputDir:       "/tmp/policies",
	}

	stats.Log(lgr)

	require.GreaterOrEqual(t, logs.Len(), 1)
	entry := logs.All()[0]

	fieldMap := make(map[string]interface{})
	for _, f := range entry.Context {
		fieldMap[f.Key] = f.Integer
	}
	assert.Contains(t, fieldMap, "policies_skipped")
}

func TestClusterDedup_SkipsMatchingPolicy(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)

	flow := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"production",
		8080,
	)

	// Build the policy that would be generated from this flow
	generatedPolicy, _ := policy.BuildPolicy("production", "server", []*flowpb.Flow{flow}, nil, policy.AttributionOptions{})

	// Create cluster policies map with the same policy
	clusterPolicies := map[string]*ciliumv2.CiliumNetworkPolicy{
		"cpg-server": generatedPolicy,
	}

	source := &mockFlowSource{
		flows: []*flowpb.Flow{flow},
	}

	cfg := PipelineConfig{
		FlushInterval:   10 * time.Millisecond,
		OutputDir:       tmpDir,
		Logger:          logger,
		ClusterPolicies: clusterPolicies,
	}

	err := RunPipelineWithSource(context.Background(), cfg, source)
	require.NoError(t, err)

	// Policy should NOT be written because it matches cluster state
	serverPolicy := filepath.Join(tmpDir, "production", "server.yaml")
	_, err = os.Stat(serverPolicy)
	assert.True(t, os.IsNotExist(err), "policy should not be written when it matches cluster state")
}

// TestPipelineConfig_L7EnabledFieldExists is a compile-time + zero-value
// guardrail confirming PipelineConfig carries the L7Enabled bool field
// introduced in plan 07-04. The field is a no-op consumer in Phase 7;
// Phase 8 (HTTP) and Phase 9 (DNS) will wire actual codegen behavior.
func TestPipelineConfig_L7EnabledFieldExists(t *testing.T) {
	cfg := PipelineConfig{L7Enabled: true}
	assert.True(t, cfg.L7Enabled)
	zero := PipelineConfig{}
	assert.False(t, zero.L7Enabled, "zero value must default to false")
}

// TestRunPipeline_DryRunWithoutEvidenceRendersEvidenceOff is a regression guard
// for C-1 (operator-precedence cleanup). With EvidenceEnabled=false AND DryRun=true,
// the summary path line must read "evidence disabled" — NOT "(dry-run, not written)".
//
// The buggy compound expression `!cfg.EvidenceEnabled || cfg.DryRun && !cfg.EvidenceEnabled`
// evaluates identically to `!cfg.EvidenceEnabled` due to Go operator precedence
// (&& binds tighter than ||), so the test passes with the current code. The refactor
// to the explicit clean form preserves that intent and this test pins the documented
// behaviour so a future one-character edit cannot silently flip it.
func TestRunPipeline_DryRunWithoutEvidenceRendersEvidenceOff(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)

	// An infra drop flow so the summary block is triggered.
	infraFlow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Verdict:          flowpb.Verdict_DROPPED,
		DropReasonDesc:   flowpb.DropReason_CT_MAP_INSERTION_FAILED,
		NodeName:         "node-1",
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=worker"},
			Namespace: "production",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=backend"},
			Namespace: "production",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{DestinationPort: 8080}},
		},
	}

	source := &mockFlowSource{flows: []*flowpb.Flow{infraFlow}}

	var stdout bytes.Buffer
	cfg := PipelineConfig{
		FlushInterval:   10 * time.Millisecond,
		OutputDir:       tmpDir,
		Logger:          logger,
		EvidenceEnabled: false,
		DryRun:          true,
		Stdout:          &stdout,
	}

	err := RunPipelineWithSource(context.Background(), cfg, source)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "evidence disabled", "path line must say 'evidence disabled' when EvidenceEnabled=false, even if DryRun=true")
	assert.NotContains(t, out, "(dry-run, not written)", "DryRun path must NOT appear when EvidenceEnabled=false")
}

// TestRunPipeline_FallbackSnapshotNoEvidence verifies C2: when EvidenceEnabled=false
// and infra drops are observed, the cluster-health summary IS printed to
// PipelineConfig.Stdout with the per-reason counts visible.
// Top nodes/workloads show "(none)" since hw==nil never accumulated them.
func TestRunPipeline_FallbackSnapshotNoEvidence(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)

	// An infra drop flow: CT_MAP_INSERTION_FAILED, DROPPED verdict.
	infraFlow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Verdict:          flowpb.Verdict_DROPPED,
		DropReasonDesc:   flowpb.DropReason_CT_MAP_INSERTION_FAILED,
		NodeName:         "node-1",
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=worker"},
			Namespace: "production",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=backend"},
			Namespace: "production",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{DestinationPort: 8080}},
		},
	}

	source := &mockFlowSource{flows: []*flowpb.Flow{infraFlow}}

	var stdout bytes.Buffer
	cfg := PipelineConfig{
		FlushInterval:   10 * time.Millisecond,
		OutputDir:       tmpDir,
		Logger:          logger,
		EvidenceEnabled: false, // hw will be nil
		Stdout:          &stdout,
	}

	err := RunPipelineWithSource(context.Background(), cfg, source)
	require.NoError(t, err)

	out := stdout.String()
	// Summary block must be printed even though evidence is disabled.
	assert.Contains(t, out, "Cluster-critical drops detected", "summary header must appear")
	assert.Contains(t, out, "CT_MAP_INSERTION_FAILED", "infra reason must appear in summary")
	assert.Contains(t, out, "1 flows", "count must appear")
	// No node/workload attribution since hw==nil.
	assert.Contains(t, out, "(none)", "node/workload attribution is (none) when hw==nil")
}
