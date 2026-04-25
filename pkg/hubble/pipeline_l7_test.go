package hubble

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/flowsource"
)

const l7HTTPFixture = "../../testdata/flows/l7_http.jsonl"
const l4OnlyFixture = "../../testdata/flows/small.jsonl"
const emptyFixture = "../../testdata/flows/empty.jsonl"

// newObservedLogger returns a zap logger that captures every entry into an
// observable buffer for assertions.
func newObservedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return zap.New(core), logs
}

func runReplayPipeline(t *testing.T, fixture string, l7Enabled bool, evidenceEnabled bool) (outDir, evDir string, logs *observer.ObservedLogs) {
	t.Helper()
	outDir = t.TempDir()
	evDir = t.TempDir()
	logger, observed := newObservedLogger()

	src, err := flowsource.NewFileSource(fixture, logger)
	require.NoError(t, err)

	cfg := PipelineConfig{
		FlushInterval: 50 * time.Millisecond,
		OutputDir:     outDir,
		Logger:        logger,
		L7Enabled:     l7Enabled,
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
		cfg.CPGVersion = "test"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, RunPipelineWithSource(ctx, cfg, src))

	return outDir, evDir, observed
}

// TestPipeline_L7HTTP_GeneratedAndEvidence asserts that running the pipeline
// against an L7-bearing fixture with L7Enabled=true produces a CNP with an
// HTTP rules block, evidence v2 carries L7Ref{Protocol:"http"}, and VIS-01
// does NOT fire.
func TestPipeline_L7HTTP_GeneratedAndEvidence(t *testing.T) {
	outDir, evDir, logs := runReplayPipeline(t, l7HTTPFixture, true, true)

	// CNP YAML contains http block with method GET and the anchored path.
	yamlPath := filepath.Join(outDir, "production", "api-server.yaml")
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err, "policy YAML must exist")
	yaml := string(data)
	assert.Contains(t, yaml, "rules:", "expected rules block in YAML")
	assert.Contains(t, yaml, "http:", "expected http sub-block in YAML")
	assert.Contains(t, yaml, "method: GET", "GET method should be emitted")
	assert.Contains(t, yaml, "method: POST", "POST method should be emitted")
	assert.Contains(t, yaml, `path: ^/api/v1/users$`, "anchored regex path expected")
	// HTTP-05 lint: never emit Headers/Host/HostExact even from cpg.
	assert.NotContains(t, yaml, "headerMatches")
	assert.NotContains(t, yaml, "host:")
	assert.NotContains(t, yaml, "hostExact")

	// VIS-01 must NOT fire since L7 records were observed.
	for _, e := range logs.All() {
		assert.NotContains(t, e.Message, "no L7 records observed", "VIS-01 must not fire when L7 records present")
	}

	// Evidence file carries L7Ref for at least one rule.
	absOut, _ := filepath.Abs(outDir)
	hash := evidence.HashOutputDir(absOut)
	evPath := filepath.Join(evDir, hash, "production", "api-server.json")
	raw, err := os.ReadFile(evPath)
	require.NoError(t, err, "evidence file must exist at %s", evPath)
	var pev evidence.PolicyEvidence
	require.NoError(t, json.Unmarshal(raw, &pev))
	assert.Equal(t, evidence.SchemaVersion, pev.SchemaVersion)
	hasL7 := false
	for _, r := range pev.Rules {
		if r.L7 != nil && r.L7.Protocol == "http" && r.L7.HTTPMethod != "" && r.L7.HTTPPath != "" {
			hasL7 = true
			break
		}
	}
	assert.True(t, hasL7, "at least one RuleEvidence must carry L7Ref{Protocol:http,...}")
}

// TestPipeline_L7Empty_FiresWarning asserts that VIS-01 fires exactly once
// when L7Enabled=true but the fixture carries no L7 records.
func TestPipeline_L7Empty_FiresWarning(t *testing.T) {
	outDir, _, logs := runReplayPipeline(t, l4OnlyFixture, true, false)

	matches := 0
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "no L7 records observed") {
			matches++
			// hint must include the README anchor + flag name.
			fields := e.ContextMap()
			hint, ok := fields["hint"].(string)
			assert.True(t, ok, "hint field must be a string")
			assert.Contains(t, hint, "#l7-prerequisites")
			assert.Contains(t, e.Message, "--l7")
			// workloads slice must be non-empty and sorted.
			ws, ok := fields["workloads"].([]interface{})
			if !ok {
				if asStrings, okStr := fields["workloads"].([]string); okStr {
					assert.NotEmpty(t, asStrings)
				}
			} else {
				assert.NotEmpty(t, ws, "workloads must be non-empty")
			}
		}
	}
	assert.Equal(t, 1, matches, "VIS-01 must fire exactly once")

	// CNP YAML must NOT contain http rules block.
	yamlPath := filepath.Join(outDir, "production", "api-server.yaml")
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "http:", "no L7 records → no http block")
}

// TestPipeline_L7Disabled_NoWarning asserts that VIS-01 does not fire when
// L7Enabled=false even though no L7 records exist.
func TestPipeline_L7Disabled_NoWarning(t *testing.T) {
	_, _, logs := runReplayPipeline(t, l4OnlyFixture, false, false)
	for _, e := range logs.All() {
		assert.NotContains(t, e.Message, "no L7 records observed",
			"VIS-01 must NOT fire when --l7 is not set")
	}
}

// TestPipeline_L7Disabled_L7FlowsIgnored asserts that with L7Enabled=false,
// even an L7-bearing fixture produces L4-only output identical to a v1.1
// run, evidence carries no L7Ref, and VIS-01 does not fire.
func TestPipeline_L7Disabled_L7FlowsIgnored(t *testing.T) {
	outDir, evDir, logs := runReplayPipeline(t, l7HTTPFixture, false, true)

	yamlPath := filepath.Join(outDir, "production", "api-server.yaml")
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err)
	yaml := string(data)
	assert.NotContains(t, yaml, "http:", "L7 disabled → no http block in YAML")
	assert.NotContains(t, yaml, "rules:", "L7 disabled → no rules sub-block in YAML")

	for _, e := range logs.All() {
		assert.NotContains(t, e.Message, "no L7 records observed",
			"VIS-01 must NOT fire when --l7 is not set, regardless of L7 flow content")
	}

	absOut, _ := filepath.Abs(outDir)
	hash := evidence.HashOutputDir(absOut)
	evPath := filepath.Join(evDir, hash, "production", "api-server.json")
	raw, err := os.ReadFile(evPath)
	require.NoError(t, err)
	var pev evidence.PolicyEvidence
	require.NoError(t, json.Unmarshal(raw, &pev))
	for _, r := range pev.Rules {
		assert.Nil(t, r.L7, "with L7 disabled no RuleEvidence may carry L7Ref")
	}
}

// TestPipeline_L7Enabled_NoFlows_NoWarning asserts that VIS-01 does not fire
// when L7Enabled=true but the fixture is empty (totalFlows == 0).
func TestPipeline_L7Enabled_NoFlows_NoWarning(t *testing.T) {
	// empty.jsonl exists in testdata/flows/.
	if _, err := os.Stat(emptyFixture); err != nil {
		t.Skipf("empty fixture missing: %v", err)
	}
	_, _, logs := runReplayPipeline(t, emptyFixture, true, false)
	for _, e := range logs.All() {
		assert.NotContains(t, e.Message, "no L7 records observed",
			"VIS-01 must NOT fire when there are no flows at all")
	}
}
