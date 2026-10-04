package session

import (
	"bytes"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/hubble"
)

func TestDefaultDuration(t *testing.T) {
	tests := []struct {
		name     string
		d        time.Duration
		fallback time.Duration
		want     time.Duration
	}{
		{"zero falls back", 0, 5 * time.Second, 5 * time.Second},
		{"negative falls back", -3 * time.Second, 5 * time.Second, 5 * time.Second},
		{"positive passes through", 2 * time.Second, 5 * time.Second, 2 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, defaultDuration(tt.d, tt.fallback))
		})
	}
}

// sessionIDPattern matches the internal evidence SessionID format
// (RFC3339-hyphen-4hex, D-10) buildPipelineConfig produces — distinct from
// the MCP-facing "sess_<uuid>" Session.ID handle.
var sessionIDPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T.*-[0-9a-f]{4}$`)

func TestBuildPipelineConfig(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	var stdout bytes.Buffer
	var onFinalCalled bool
	onFinal := func(hubble.SessionStats) { onFinalCalled = true }

	args := StartArgs{
		Timeout:         0,
		FlushInterval:   0,
		Server:          "relay:4245",
		L7:              true,
		IncludeAudit:    true,
		IgnoreProtocols: []string{"tcp"},
	}

	cfg := buildPipelineConfig(args, tmpDir, args.Server, logger, "vTest", &stdout, nil, onFinal)

	wantOutputDir := filepath.Join(tmpDir, "policies")
	wantEvidenceDir := filepath.Join(tmpDir, "evidence")

	assert.Equal(t, wantOutputDir, cfg.OutputDir)
	assert.Equal(t, wantEvidenceDir, cfg.EvidenceDir)
	assert.Equal(t, evidence.HashOutputDir(wantOutputDir), cfg.OutputHash)

	// The crash guard (Pitfall A): zero-value StartArgs durations must
	// never reach PipelineConfig unfixed.
	assert.Equal(t, 10*time.Second, cfg.Timeout, "Timeout must default to 10s when StartArgs.Timeout==0")
	assert.Equal(t, 5*time.Second, cfg.FlushInterval, "FlushInterval must default to 5s when StartArgs.FlushInterval==0")

	assert.Same(t, &stdout, cfg.Stdout)
	require.NotNil(t, cfg.OnFinal)
	cfg.OnFinal(hubble.SessionStats{})
	assert.True(t, onFinalCalled, "OnFinal passed through to buildPipelineConfig must be the one wired onto PipelineConfig")

	assert.True(t, cfg.EvidenceEnabled)
	assert.Equal(t, evidence.MergeCaps{MaxSamples: 10, MaxSessions: 10}, cfg.EvidenceCaps)

	// D-05 exclusions: never set by buildPipelineConfig, must stay at Go's
	// zero value.
	assert.False(t, cfg.DryRun)
	assert.False(t, cfg.DryRunDiff)
	assert.False(t, cfg.FailOnInfraDrops)

	assert.Equal(t, "vTest", cfg.CPGVersion)
	assert.True(t, cfg.L7Enabled)
	assert.True(t, cfg.IncludeAudit, "IncludeAudit must pass through from StartArgs.IncludeAudit")
	assert.Equal(t, []string{"tcp"}, cfg.IgnoreProtocols)
	assert.Equal(t, "relay:4245", cfg.Server)
	assert.Equal(t, logger, cfg.Logger)
	assert.Nil(t, cfg.ClusterPolicies)

	assert.Regexp(t, sessionIDPattern, cfg.SessionID)
}

// TestBuildPipelineConfig_IncludeAuditDefaultsFalse confirms that a
// StartArgs with IncludeAudit unset (Go zero value) produces a
// PipelineConfig.IncludeAudit == false, matching AUD-01's opt-in contract
// (pre-v1.6 DROPPED-only behavior unchanged unless explicitly requested).
func TestBuildPipelineConfig_IncludeAuditDefaultsFalse(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	var stdout bytes.Buffer
	onFinal := func(hubble.SessionStats) {}

	args := StartArgs{Server: "relay:4245"}

	cfg := buildPipelineConfig(args, tmpDir, args.Server, logger, "vTest", &stdout, nil, onFinal)

	assert.False(t, cfg.IncludeAudit, "IncludeAudit must default to false when StartArgs.IncludeAudit is unset")
}

// TestBuildPipelineConfig_CollapseDefaults proves the plan 25-02
// StartArgs → PipelineConfig mapping for the L7 collapse fields
// (L7GEN-03): CollapseL7Paths passes through, and MinFlowsPerL7Rule is
// resolved through policy.ResolveMinFlowsPerL7Rule — 0/omitted (the MCP
// zero value for an omitted arg) maps to DefaultMinFlowsPerL7Rule (5),
// never disabling the gate (T-25-05 fail-closed); a positive value passes
// through unchanged. Resolution is harmless when collapse is off.
func TestBuildPipelineConfig_CollapseDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()
	var stdout bytes.Buffer
	onFinal := func(hubble.SessionStats) {}

	t.Run("collapse on, gate omitted resolves to default 5", func(t *testing.T) {
		args := StartArgs{Server: "relay:4245", CollapseL7Paths: true}
		cfg := buildPipelineConfig(args, tmpDir, args.Server, logger, "vTest", &stdout, nil, onFinal)
		assert.True(t, cfg.CollapseL7Paths, "CollapseL7Paths must pass through from StartArgs")
		assert.Equal(t, 5, cfg.MinFlowsPerL7Rule, "0/omitted gate must resolve to DefaultMinFlowsPerL7Rule (5)")
	})

	t.Run("positive gate passes through", func(t *testing.T) {
		args := StartArgs{Server: "relay:4245", CollapseL7Paths: true, MinFlowsPerL7Rule: 7}
		cfg := buildPipelineConfig(args, tmpDir, args.Server, logger, "vTest", &stdout, nil, onFinal)
		assert.True(t, cfg.CollapseL7Paths)
		assert.Equal(t, 7, cfg.MinFlowsPerL7Rule, "a positive gate value must pass through unchanged")
	})

	t.Run("all-zero args stay harmless with collapse off", func(t *testing.T) {
		args := StartArgs{Server: "relay:4245"}
		cfg := buildPipelineConfig(args, tmpDir, args.Server, logger, "vTest", &stdout, nil, onFinal)
		assert.False(t, cfg.CollapseL7Paths, "CollapseL7Paths must default to false (opt-in)")
		assert.Equal(t, 5, cfg.MinFlowsPerL7Rule, "resolution is harmless when collapse is off")
	})
}
