// pkg/evidence/writer_test.go
package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriterCreatesFile(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, "hash0", MergeCaps{MaxSamples: 10, MaxSessions: 10})

	ref := PolicyRef{Name: "cpg-api", Namespace: "prod", Workload: "api"}
	session := SessionInfo{ID: "s1", StartedAt: ts(0), EndedAt: ts(10)}
	rule := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP", FlowCount: 1,
		Samples: []FlowSample{sample(1, "a")},
	}

	require.NoError(t, w.Write(ref, session, []RuleEvidence{rule}))

	path := filepath.Join(dir, "hash0", "prod", "api.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var got PolicyEvidence
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, 2, got.SchemaVersion)
	assert.Equal(t, ref, got.Policy)
	assert.Len(t, got.Sessions, 1)
	assert.Len(t, got.Rules, 1)
}

func TestWriterMergesWithExisting(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, "hash0", MergeCaps{MaxSamples: 10, MaxSessions: 10})

	ref := PolicyRef{Name: "cpg-api", Namespace: "prod", Workload: "api"}
	session1 := SessionInfo{ID: "s1", StartedAt: ts(0), EndedAt: ts(10)}
	rule := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP", FlowCount: 1,
		FirstSeen: ts(1), LastSeen: ts(5),
		ContributingSessions: []string{"s1"},
		Samples:              []FlowSample{sample(1, "a")},
	}
	require.NoError(t, w.Write(ref, session1, []RuleEvidence{rule}))

	// Second run, same rule
	session2 := SessionInfo{ID: "s2", StartedAt: ts(20), EndedAt: ts(30)}
	rule2 := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP", FlowCount: 2,
		FirstSeen: ts(21), LastSeen: ts(25),
		ContributingSessions: []string{"s2"},
		Samples:              []FlowSample{sample(21, "d"), sample(25, "e")},
	}
	require.NoError(t, w.Write(ref, session2, []RuleEvidence{rule2}))

	got, err := NewReader(dir, "hash0").Read("prod", "api")
	require.NoError(t, err)
	assert.Len(t, got.Sessions, 2)
	assert.Len(t, got.Rules, 1)
	assert.Equal(t, int64(3), got.Rules[0].FlowCount)
	assert.Equal(t, ts(1), got.Rules[0].FirstSeen)
	assert.Equal(t, ts(25), got.Rules[0].LastSeen)
}

// TestWriter_EmitsSchemaV2 confirms freshly written evidence carries
// schema_version 2 on disk.
func TestWriter_EmitsSchemaV2(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, "hash0", MergeCaps{MaxSamples: 10, MaxSessions: 10})

	ref := PolicyRef{Name: "cpg-api", Namespace: "prod", Workload: "api"}
	session := SessionInfo{ID: "s1", StartedAt: ts(0), EndedAt: ts(10)}
	rule := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP", FlowCount: 1,
		Samples: []FlowSample{sample(1, "a")},
	}
	require.NoError(t, w.Write(ref, session, []RuleEvidence{rule}))

	path := filepath.Join(dir, "hash0", "prod", "api.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.EqualValues(t, 2, raw["schema_version"], "writer must emit schema_version: 2")
}

// TestRuleEvidence_OmitsL7WhenNil asserts the omitempty contract: a RuleEvidence
// without an L7 ref must not produce a `"l7"` key in JSON. v1.1 L4-only
// evidence shape is preserved byte-for-byte modulo the schema_version bump.
func TestRuleEvidence_OmitsL7WhenNil(t *testing.T) {
	rule := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer:     PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port:     "80",
		Protocol: "TCP",
		// L7 left nil
	}
	out, err := json.Marshal(rule)
	require.NoError(t, err)
	assert.NotContains(t, string(out), `"l7"`,
		"L4-only RuleEvidence must not serialize an l7 key (omitempty broken)")
}

// TestRuleEvidence_RoundTripsHTTPL7 covers the HTTP variant of L7Ref.
func TestRuleEvidence_RoundTripsHTTPL7(t *testing.T) {
	rule := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80:http:GET:/api", Direction: "ingress",
		Peer:     PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port:     "80",
		Protocol: "TCP",
		L7: &L7Ref{
			Protocol:   "http",
			HTTPMethod: "GET",
			HTTPPath:   "/api",
		},
	}
	out, err := json.Marshal(rule)
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, `"l7"`)
	assert.Contains(t, s, `"protocol":"http"`)
	assert.Contains(t, s, `"http_method":"GET"`)
	assert.Contains(t, s, `"http_path":"/api"`)
	assert.NotContains(t, s, `"dns_matchname"`, "DNS field must be omitted for HTTP L7Ref")

	var got RuleEvidence
	require.NoError(t, json.Unmarshal(out, &got))
	require.NotNil(t, got.L7)
	assert.Equal(t, "http", got.L7.Protocol)
	assert.Equal(t, "GET", got.L7.HTTPMethod)
	assert.Equal(t, "/api", got.L7.HTTPPath)
}

// TestRuleEvidence_RoundTripsDNSL7 covers the DNS variant of L7Ref.
func TestRuleEvidence_RoundTripsDNSL7(t *testing.T) {
	rule := RuleEvidence{
		Key: "egress:fqdn:s3.amazonaws.com:UDP:53", Direction: "egress",
		Peer:     PeerRef{Type: "entity", Entity: "world"},
		Port:     "53",
		Protocol: "UDP",
		L7: &L7Ref{
			Protocol:     "dns",
			DNSMatchName: "s3.amazonaws.com",
		},
	}
	out, err := json.Marshal(rule)
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, `"protocol":"dns"`)
	assert.Contains(t, s, `"dns_matchname":"s3.amazonaws.com"`)
	assert.NotContains(t, s, `"http_method"`, "HTTP method must be omitted for DNS L7Ref")
	assert.NotContains(t, s, `"http_path"`, "HTTP path must be omitted for DNS L7Ref")

	var got RuleEvidence
	require.NoError(t, json.Unmarshal(out, &got))
	require.NotNil(t, got.L7)
	assert.Equal(t, "dns", got.L7.Protocol)
	assert.Equal(t, "s3.amazonaws.com", got.L7.DNSMatchName)
}

func TestWriterIgnoresUnknownSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hash0", "prod", "api.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`{"schema_version": 999}`), 0o644))

	w := NewWriter(dir, "hash0", MergeCaps{MaxSamples: 10, MaxSessions: 10})
	ref := PolicyRef{Name: "cpg-api", Namespace: "prod", Workload: "api"}
	session := SessionInfo{ID: "s1", StartedAt: time.Now()}

	err := w.Write(ref, session, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema_version")
}
