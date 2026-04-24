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
	assert.Equal(t, 1, got.SchemaVersion)
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
