package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/explain"
)

func sampleEvidence() evidence.PolicyEvidence {
	return evidence.PolicyEvidence{
		SchemaVersion: 2,
		Policy:        evidence.PolicyRef{Name: "cpg-api", Namespace: "prod", Workload: "api"},
		Sessions: []evidence.SessionInfo{{
			ID:        "s1",
			StartedAt: time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC),
			EndedAt:   time.Date(2026, 4, 24, 14, 15, 0, 0, time.UTC),
			Source:    evidence.SourceInfo{Type: "replay", File: "f.jsonl"},
		}},
		Rules: []evidence.RuleEvidence{{
			Key: "ingress:ep:app=x:TCP:8080", Direction: "ingress",
			Peer: evidence.PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
			Port: "8080", Protocol: "TCP",
			FlowCount: 3, FirstSeen: time.Date(2026, 4, 24, 14, 0, 1, 0, time.UTC), LastSeen: time.Date(2026, 4, 24, 14, 2, 5, 0, time.UTC),
			Samples: []evidence.FlowSample{{
				Time: time.Date(2026, 4, 24, 14, 0, 1, 0, time.UTC),
				Src:  evidence.FlowEndpoint{Namespace: "default", Workload: "client"},
				Dst:  evidence.FlowEndpoint{Namespace: "prod", Workload: "api"},
				Port: 8080, Protocol: "TCP", Verdict: "DROPPED",
			}},
		}},
	}
}

func seedEvidence(t *testing.T, evDir, outDir string) {
	t.Helper()
	hash := evidence.HashOutputDir(outDir)
	p := filepath.Join(evDir, hash, "prod", "api.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	data, err := json.Marshal(sampleEvidence())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, data, 0o644))
}

func TestExplainFindsEvidenceAndPrintsText(t *testing.T) {
	outDir := t.TempDir()
	evDir := t.TempDir()
	seedEvidence(t, evDir, outDir)

	initLoggerForTesting(t)

	cmd := newExplainCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"prod/api", "--output-dir", outDir, "--evidence-dir", evDir})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "Policy: cpg-api")
	assert.Contains(t, out, "app=x")
}

func TestExplainReturnsClearErrorWhenEvidenceMissing(t *testing.T) {
	outDir := t.TempDir()
	evDir := t.TempDir()
	initLoggerForTesting(t)

	cmd := newExplainCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"prod/api", "--output-dir", outDir, "--evidence-dir", evDir})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no evidence found")
}

func TestBuildFilterNormalizesL7Inputs(t *testing.T) {
	cmd := newExplainCmd()
	require.NoError(t, cmd.ParseFlags([]string{
		"--http-method", "get",
		"--http-path", "^/foo$",
		"--dns-pattern", "api.example.com.",
	}))
	f, err := buildFilter(cmd)
	require.NoError(t, err)
	assert.Equal(t, "GET", f.HTTPMethod, "method should be uppercased")
	assert.Equal(t, "^/foo$", f.HTTPPath, "path should be left as-is")
	assert.Equal(t, "api.example.com", f.DNSPattern, "trailing dot should be stripped")
}

func TestBuildFilterDefaultsEmpty(t *testing.T) {
	cmd := newExplainCmd()
	require.NoError(t, cmd.ParseFlags([]string{}))
	f, err := buildFilter(cmd)
	require.NoError(t, err)
	assert.Empty(t, f.HTTPMethod)
	assert.Empty(t, f.HTTPPath)
	assert.Empty(t, f.DNSPattern)
}

func TestExplainJSONOutput(t *testing.T) {
	outDir := t.TempDir()
	evDir := t.TempDir()
	seedEvidence(t, evDir, outDir)
	initLoggerForTesting(t)

	cmd := newExplainCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"prod/api", "--output-dir", outDir, "--evidence-dir", evDir, "--json"})
	require.NoError(t, cmd.Execute())

	var got explain.Output
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Len(t, got.MatchedRules, 1)
}
