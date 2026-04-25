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

func TestRenderTextShowsRuleMeta(t *testing.T) {
	buf := new(bytes.Buffer)
	require.NoError(t, renderText(buf, sampleEvidence(), sampleEvidence().Rules, 10, false))

	out := buf.String()
	assert.Contains(t, out, "Policy: cpg-api")
	assert.Contains(t, out, "Ingress rule")
	assert.Contains(t, out, "app=x")
	assert.Contains(t, out, "8080/TCP")
	assert.Contains(t, out, "Flow count:  3")
	assert.Contains(t, out, "default/client")
}

func TestRenderJSON(t *testing.T) {
	buf := new(bytes.Buffer)
	require.NoError(t, renderJSON(buf, sampleEvidence(), sampleEvidence().Rules))
	var got explainOutput
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, "cpg-api", got.Policy.Name)
	assert.Len(t, got.MatchedRules, 1)
}

func TestRenderYAML(t *testing.T) {
	buf := new(bytes.Buffer)
	require.NoError(t, renderYAML(buf, sampleEvidence(), sampleEvidence().Rules))
	assert.Contains(t, buf.String(), "policy:")
	assert.Contains(t, buf.String(), "matched_rules:")
}

func httpRuleEvidence() evidence.RuleEvidence {
	return evidence.RuleEvidence{
		Key: "egress:ep:app=api:TCP:80:http:GET:^/api/v1/users$", Direction: "egress",
		Peer: evidence.PeerRef{Type: "endpoint", Labels: map[string]string{"app": "api"}},
		Port: "80", Protocol: "TCP",
		L7: &evidence.L7Ref{
			Protocol:   "http",
			HTTPMethod: "GET",
			HTTPPath:   "^/api/v1/users$",
		},
		FlowCount: 2,
		FirstSeen: time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC),
		LastSeen:  time.Date(2026, 4, 24, 14, 5, 0, 0, time.UTC),
	}
}

func dnsRuleEvidence() evidence.RuleEvidence {
	return evidence.RuleEvidence{
		Key: "egress:fqdn:api.example.com:UDP:53:dns:api.example.com", Direction: "egress",
		Peer: evidence.PeerRef{Type: "entity", Entity: "world"},
		Port: "53", Protocol: "UDP",
		L7: &evidence.L7Ref{
			Protocol:     "dns",
			DNSMatchName: "api.example.com",
		},
		FlowCount: 1,
		FirstSeen: time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC),
		LastSeen:  time.Date(2026, 4, 24, 14, 1, 0, 0, time.UTC),
	}
}

func TestRenderTextL7HTTP(t *testing.T) {
	pe := sampleEvidence()
	r := httpRuleEvidence()
	buf := new(bytes.Buffer)
	require.NoError(t, renderText(buf, pe, []evidence.RuleEvidence{r}, 10, false))
	out := buf.String()
	assert.Contains(t, out, "L7:")
	assert.Contains(t, out, "HTTP GET ^/api/v1/users$")
}

func TestRenderTextL7DNS(t *testing.T) {
	pe := sampleEvidence()
	r := dnsRuleEvidence()
	buf := new(bytes.Buffer)
	require.NoError(t, renderText(buf, pe, []evidence.RuleEvidence{r}, 10, false))
	out := buf.String()
	assert.Contains(t, out, "L7:")
	assert.Contains(t, out, "DNS api.example.com")
}

func TestRenderTextL4OnlyNoL7Line(t *testing.T) {
	// L4-only rule must not produce any "L7:" line — preserves v1.1 layout.
	pe := sampleEvidence()
	buf := new(bytes.Buffer)
	require.NoError(t, renderText(buf, pe, pe.Rules, 10, false))
	out := buf.String()
	assert.NotContains(t, out, "L7:")
}

func TestRenderJSONL7HTTP(t *testing.T) {
	pe := sampleEvidence()
	r := httpRuleEvidence()
	buf := new(bytes.Buffer)
	require.NoError(t, renderJSON(buf, pe, []evidence.RuleEvidence{r}))
	var got explainOutput
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got.MatchedRules, 1)
	require.NotNil(t, got.MatchedRules[0].L7)
	assert.Equal(t, "http", got.MatchedRules[0].L7.Protocol)
	assert.Equal(t, "GET", got.MatchedRules[0].L7.HTTPMethod)
	assert.Equal(t, "^/api/v1/users$", got.MatchedRules[0].L7.HTTPPath)
	// omitempty: dns_matchname must NOT be present in HTTP rule's JSON.
	assert.NotContains(t, buf.String(), "dns_matchname")
}

func TestRenderJSONL4OnlyOmitsL7(t *testing.T) {
	pe := sampleEvidence()
	buf := new(bytes.Buffer)
	require.NoError(t, renderJSON(buf, pe, pe.Rules))
	// L4-only rule should omit l7 key entirely (omitempty pointer).
	assert.NotContains(t, buf.String(), `"l7"`)
}

func TestRenderYAMLL7DNS(t *testing.T) {
	pe := sampleEvidence()
	r := dnsRuleEvidence()
	buf := new(bytes.Buffer)
	require.NoError(t, renderYAML(buf, pe, []evidence.RuleEvidence{r}))
	out := buf.String()
	assert.Contains(t, out, "l7:")
	assert.Contains(t, out, "protocol: dns")
	assert.Contains(t, out, "dns_matchname: api.example.com")
}

func TestRenderTextEmptyMatchListsAvailable(t *testing.T) {
	buf := new(bytes.Buffer)
	err := renderText(buf, sampleEvidence(), nil, 10, false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No rules matched")
	assert.Contains(t, buf.String(), "Available rules:")
	assert.Contains(t, buf.String(), "app=x")
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

	var got explainOutput
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Len(t, got.MatchedRules, 1)
}
