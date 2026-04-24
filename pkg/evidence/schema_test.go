// pkg/evidence/schema_test.go
package evidence

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaRoundtrip(t *testing.T) {
	original := PolicyEvidence{
		SchemaVersion: 1,
		Policy: PolicyRef{
			Name:      "cpg-api-server",
			Namespace: "production",
			Workload:  "api-server",
		},
		Sessions: []SessionInfo{{
			ID:             "2026-04-24T14:02:11Z-a3f2",
			StartedAt:      time.Date(2026, 4, 24, 14, 2, 11, 0, time.UTC),
			EndedAt:        time.Date(2026, 4, 24, 14, 15, 48, 0, time.UTC),
			CPGVersion:     "1.6.0",
			Source:         SourceInfo{Type: "replay", File: "/tmp/flows.jsonl"},
			FlowsIngested:  12847,
			FlowsUnhandled: 42,
		}},
		Rules: []RuleEvidence{{
			Key:       "ingress:ep:app=weird-thing:TCP:8080",
			Direction: "ingress",
			Peer: PeerRef{
				Type:   "endpoint",
				Labels: map[string]string{"app": "weird-thing"},
			},
			Port:                 "8080",
			Protocol:             "TCP",
			FlowCount:            23,
			FirstSeen:            time.Date(2026, 4, 24, 14, 2, 11, 0, time.UTC),
			LastSeen:             time.Date(2026, 4, 24, 14, 15, 48, 0, time.UTC),
			ContributingSessions: []string{"2026-04-24T14:02:11Z-a3f2"},
			Samples: []FlowSample{{
				Time:     time.Date(2026, 4, 24, 14, 2, 11, 0, time.UTC),
				Src:      FlowEndpoint{Namespace: "default", Workload: "weird-thing", Pod: "weird-thing-5d4f"},
				Dst:      FlowEndpoint{Namespace: "production", Workload: "api-server", Pod: "api-server-abc"},
				Port:     8080,
				Protocol: "TCP",
				Verdict:  "DROPPED",
			}},
		}},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded PolicyEvidence
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original, decoded)
}

func TestSchemaEmptySkeleton(t *testing.T) {
	skel := NewSkeleton(PolicyRef{Name: "cpg-x", Namespace: "ns", Workload: "x"})
	assert.Equal(t, 1, skel.SchemaVersion)
	assert.Equal(t, "cpg-x", skel.Policy.Name)
	assert.Empty(t, skel.Sessions)
	assert.Empty(t, skel.Rules)
}
