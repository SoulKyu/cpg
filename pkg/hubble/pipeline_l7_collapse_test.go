package hubble

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/flowsource"
	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)

const l7CollapseUsersFixture = "../../testdata/flows/l7-collapse-users.jsonl"

// runCollapsePipeline replays a fixture with L7 codegen enabled and the
// collapse flags set to the supplied values, mirroring runReplayPipeline.
func runCollapsePipeline(t *testing.T, fixture string, collapse bool, minFlowsPerL7Rule int, evidenceEnabled bool) (outDir, evDir string, logs *observer.ObservedLogs) {
	t.Helper()
	outDir = t.TempDir()
	evDir = t.TempDir()
	logger, observed := newObservedLogger()

	src, err := flowsource.NewFileSource(fixture, logger)
	require.NoError(t, err)

	cfg := PipelineConfig{
		FlushInterval:     50 * time.Millisecond,
		OutputDir:         outDir,
		Logger:            logger,
		L7Enabled:         true,
		CollapseL7Paths:   collapse,
		MinFlowsPerL7Rule: minFlowsPerL7Rule,
	}
	if evidenceEnabled {
		absOut, err := filepath.Abs(outDir)
		require.NoError(t, err)
		cfg.EvidenceEnabled = true
		cfg.EvidenceDir = evDir
		cfg.OutputHash = evidence.HashOutputDir(absOut)
		cfg.EvidenceCaps = evidence.MergeCaps{MaxSamples: 5, MaxSessions: 5}
		cfg.SessionID = "test-session"
		cfg.SessionSource = evidence.SourceInfo{Type: "replay", File: fixture}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, RunPipelineWithSource(ctx, cfg, src))

	return outDir, evDir, observed
}

// collapseYAML reads the emitted policy YAML for the users fixture workload.
func collapseYAML(t *testing.T, outDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "production", "api-server.yaml"))
	require.NoError(t, err, "policy YAML must exist")
	return string(data)
}

// TestL7CollapseGolden proves SC1 (L7GEN-01): 10k distinct observed
// GET /api/v1/users/<n> paths collapse to exactly one anchored template rule
// plus the literal ^/healthz$ rule — 10k paths, 2 rules.
func TestL7CollapseGolden(t *testing.T) {
	outDir, _, _ := runCollapsePipeline(t, l7CollapseUsersFixture, true, 0, false)

	yaml := collapseYAML(t, outDir)
	assert.Contains(t, yaml, "http:", "expected http sub-block in YAML")
	assert.Equal(t, 1, strings.Count(yaml, `path: ^/api/v1/users/[0-9]+$`),
		"exactly one users template rule expected")
	assert.Contains(t, yaml, `path: ^/healthz$`, "literal healthz rule expected")
	assert.Equal(t, 2, strings.Count(yaml, "path: "), "expected exactly 2 http path entries")
	assert.NotContains(t, yaml, `path: ^/api/v1/users/1$`, "no stale literal user paths may be emitted")
}

// collapseIngressFlow builds a dropped ingress HTTP GET flow for the users path.
func collapseIngressFlow(url string) *flowpb.Flow {
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app.kubernetes.io/name=frontend"},
		[]string{"k8s:app.kubernetes.io/name=api-server"},
		"production",
		8080,
	)
	f.L7 = &flowpb.Layer7{
		Record: &flowpb.Layer7_Http{
			Http: &flowpb.HTTP{Method: "GET", Url: url},
		},
	}
	return f
}

// TestL7Collapse_CrossFlushReplace proves the whole-run retained state +
// replace-mode merge (Decision 1): a first flush window observing only 2
// distinct user paths (below gate) emits literal rules; a second window
// pushing the whole-run set over the gate emits the template, and merge-on-
// write with ReplaceL7HTTP leaves NO stale literals. Runs under -race with a
// tiny flush interval (L7GEN-01 concurrency edge).
func TestL7Collapse_CrossFlushReplace(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewUnhandledTracker(logger)
	agg := NewAggregator(50*time.Millisecond, logger, tracker)
	agg.SetL7Enabled(true)
	agg.SetCollapseL7Paths(true)
	agg.SetMinFlowsPerL7Rule(5)

	in := make(chan *flowpb.Flow, 16)
	out := make(chan policy.PolicyEvent, 16)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- agg.Run(ctx, in, out, nil)
	}()

	// Window 1: only 2 distinct user paths — below the gate → literals.
	in <- collapseIngressFlow("/api/v1/users/1")
	in <- collapseIngressFlow("/api/v1/users/2")
	var ev1 policy.PolicyEvent
	select {
	case ev1 = <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first flush")
	}
	assert.True(t, ev1.ReplaceL7HTTP, "collapse-enabled flush must set ReplaceL7HTTP")
	lit1 := collapseEventHTTPPaths(ev1)
	assert.Contains(t, lit1, `^/api/v1/users/1$`, "below-gate first flush must emit literals")
	assert.NotContains(t, lit1, "^/api/v1/users/[0-9]+$", "below-gate first flush must not emit a template")

	// Window 2: whole-run set crosses the gate → the template replaces.
	for n := 3; n <= 7; n++ {
		in <- collapseIngressFlow(fmt.Sprintf("/api/v1/users/%d", n))
	}
	var ev2 policy.PolicyEvent
	select {
	case ev2 = <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second flush")
	}
	lit2 := collapseEventHTTPPaths(ev2)
	assert.Contains(t, lit2, "^/api/v1/users/[0-9]+$", "over-gate flush must emit the template")

	close(in)
	require.NoError(t, <-done)

	// Merge-on-write reconciliation: the second (authoritative) event
	// REPLACES the first flush's literals.
	merged := policy.MergePolicyWithOptions(ev1.Policy, ev2.Policy, policy.MergeOptions{ReplaceL7HTTP: true})
	final := collapseEventHTTPPaths(merged)
	assert.Contains(t, final, "^/api/v1/users/[0-9]+$")
	assert.NotContains(t, final, `^/api/v1/users/1$`, "stale literal must be replaced, not coexist")
	assert.NotContains(t, final, `^/api/v1/users/2$`, "stale literal must be replaced, not coexist")
}

// collapseEventHTTPPaths flattens the HTTP rule paths of an event's policy.
func collapseEventHTTPPaths(ev policy.PolicyEvent) []string {
	var paths []string
	if ev.Policy == nil || ev.Policy.Spec == nil {
		return nil
	}
	for _, ing := range ev.Policy.Spec.Ingress {
		for _, tp := range ing.ToPorts {
			if tp.Rules == nil {
				continue
			}
			for _, h := range tp.Rules.HTTP {
				paths = append(paths, h.Path)
			}
		}
	}
	return paths
}

// TestL7Collapse_DefaultOffUnchanged proves the default-off invariant: with
// all new fields zero the existing l7_http.jsonl fixture emits exactly the
// pre-phase YAML expectations (full byte-identity golden regression is plan
// 25-02).
func TestL7Collapse_DefaultOffUnchanged(t *testing.T) {
	outDir, _, _ := runCollapsePipeline(t, l7HTTPFixture, false, 0, false)

	yaml := collapseYAML(t, outDir)
	assert.Contains(t, yaml, `path: ^/api/v1/users$`)
	assert.Contains(t, yaml, "method: GET")
	assert.Contains(t, yaml, "method: POST")
	assert.NotContains(t, yaml, "[0-9]+", "no collapsed templates may appear with collapse disabled")
}

// TestAggregator_SetCollapseL7Paths mirrors the SetL7Enabled setter tests:
// setters store their values, allocation happens for the collapse state, and
// flush forwards MinFlowsPerL7Rule/Collapse into AttributionOptions
// (observable via the emitted gate behavior).
func TestAggregator_SetCollapseL7Paths(t *testing.T) {
	logger := zaptest.NewLogger(t)
	agg := NewAggregator(10*time.Millisecond, logger, NewUnhandledTracker(logger))
	assert.Nil(t, agg.l7Collapse, "collapse state must be nil by default")

	agg.SetCollapseL7Paths(true)
	agg.SetMinFlowsPerL7Rule(7)
	assert.True(t, agg.collapseL7Paths)
	assert.NotNil(t, agg.l7Collapse, "enabling collapse must allocate the whole-run state")
	assert.Equal(t, 7, agg.minFlowsPerL7Rule)

	// Forwarding proof: 6 distinct paths sit below the explicit gate of 7,
	// so the flush must emit literals (default resolution of 0→5 would have
	// collapsed) — MinFlowsPerL7Rule is really forwarded into BuildPolicy.
	in := make(chan *flowpb.Flow, 16)
	out := make(chan policy.PolicyEvent, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- agg.Run(ctx, in, out, nil) }()

	for n := 1; n <= 6; n++ {
		in <- collapseIngressFlow(fmt.Sprintf("/api/v1/users/%d", n))
	}
	var ev policy.PolicyEvent
	select {
	case ev = <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for flush")
	}
	paths := collapseEventHTTPPaths(ev)
	assert.NotContains(t, paths, "^/api/v1/users/[0-9]+$",
		"gate value 7 must be forwarded: 6 distinct paths stay literal")
	assert.Contains(t, paths, `^/api/v1/users/1$`)

	close(in)
	require.NoError(t, <-done)

	// Whole-run state grew via Absorb: the window observations are retained.
	assert.NotEmpty(t, agg.l7Collapse.Retained, "Absorb() must retain window observations")
	assert.Empty(t, agg.l7Collapse.Collected, "Absorb() must clear the window set")
}
