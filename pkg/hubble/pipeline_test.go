package hubble

import (
	"bytes"
	"context"
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

	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)

// mockFlowSource implements FlowSource for testing.
type mockFlowSource struct {
	flows      []*flowpb.Flow
	lostEvents []*flowpb.LostEvent
}

func (m *mockFlowSource) StreamDroppedFlows(_ context.Context, _ []string, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
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

// channelFlowSource returns pre-made channels for testing.
type channelFlowSource struct {
	flows chan *flowpb.Flow
	lost  chan *flowpb.LostEvent
}

func (c *channelFlowSource) StreamDroppedFlows(_ context.Context, _ []string, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
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
