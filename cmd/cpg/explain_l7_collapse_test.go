package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/flowsource"
	"github.com/SoulKyu/cpg/pkg/hubble"
)

// TestExplainCompatibleWithCollapsedRun proves Decision 2 (attribution stays
// keyed by literal observed paths): a collapse-enabled replay emits the
// template `^/api/v1/orders/[0-9]+$` in the policy while the evidence dir
// still carries literal HTTPPath entries (`^/api/v1/orders/1$` …), so
// `cpg explain --json` exits 0 and surfaces the literal-path evidence.
func TestExplainCompatibleWithCollapsedRun(t *testing.T) {
	initLoggerForTesting(t)
	outDir := t.TempDir()
	evDir := t.TempDir()

	src, err := flowsource.NewFileSource("../../testdata/flows/l7-collapse-below-threshold.jsonl", zap.NewNop())
	require.NoError(t, err)

	absOut, err := filepath.Abs(outDir)
	require.NoError(t, err)
	cfg := hubble.PipelineConfig{
		FlushInterval:     50 * time.Millisecond,
		OutputDir:         outDir,
		Logger:            zap.NewNop(),
		L7Enabled:         true,
		CollapseL7Paths:   true,
		MinFlowsPerL7Rule: 0, // resolves to DefaultMinFlowsPerL7Rule (5)
		EvidenceEnabled:   true,
		EvidenceDir:       evDir,
		OutputHash:        evidence.HashOutputDir(absOut),
		EvidenceCaps:      evidence.MergeCaps{MaxSamples: 5, MaxSessions: 5},
		SessionID:         "test-collapse-session",
		SessionSource:     evidence.SourceInfo{Type: "replay", File: "l7-collapse-below-threshold.jsonl"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, hubble.RunPipelineWithSource(ctx, cfg, src))

	// Sanity: the run's policy carries the collapsed template.
	data, err := os.ReadFile(filepath.Join(outDir, "production", "api-server.yaml"))
	require.NoError(t, err, "policy YAML must exist")
	assert.Contains(t, string(data), "path: ^/api/v1/orders/[0-9]+$")

	// explain --json over the run's evidence dir must exit 0 and still show
	// literal HTTPPath values backing the template (Decision 2 evidence).
	cmd := newExplainCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"production/api-server",
		"--output-dir", outDir,
		"--evidence-dir", evDir,
		"--json",
	})
	require.NoError(t, cmd.Execute())

	var parsed struct {
		MatchedRules []struct {
			L7 *struct {
				HTTPPath string `json:"http_path"`
			} `json:"l7"`
		} `json:"matched_rules"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed), "explain output must be valid JSON: %s", buf.String())
	require.NotEmpty(t, parsed.MatchedRules, "explain must surface rule evidence")

	paths := make(map[string]bool)
	for _, r := range parsed.MatchedRules {
		if r.L7 != nil && r.L7.HTTPPath != "" {
			paths[r.L7.HTTPPath] = true
		}
	}
	assert.True(t, paths[`^/api/v1/orders/1$`], "evidence must retain literal HTTPPath keys")
	assert.True(t, paths[`^/api/v1/users/1$`], "evidence must retain literal HTTPPath keys")
	assert.NotContains(t, paths, "^/api/v1/orders/[0-9]+$",
		"attribution must stay literal-keyed: no template key expected")
}
