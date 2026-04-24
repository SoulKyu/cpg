package hubble

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/policy"
)

func TestEvidenceWriterPersistsAttribution(t *testing.T) {
	tmp := t.TempDir()
	sessionID := "test-session-1"
	session := evidence.SessionInfo{
		ID: sessionID, StartedAt: time.Now(), EndedAt: time.Now(),
		CPGVersion: "test", Source: evidence.SourceInfo{Type: "replay", File: "x.jsonl"},
	}

	ew := newEvidenceWriter(tmp, "hash0", evidence.MergeCaps{MaxSamples: 10, MaxSessions: 10}, session, zap.NewNop())

	now := timestamppb.New(time.Unix(1700000000, 0))
	flow := &flowpb.Flow{
		Time:             now,
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Labels: []string{"k8s:app=client"}, Namespace: "default", PodName: "client-abc"},
		Destination:      &flowpb.Endpoint{Labels: []string{"k8s:app=api"}, Namespace: "prod", PodName: "api-xyz"},
		Verdict:          flowpb.Verdict_DROPPED,
	}

	pe := policy.PolicyEvent{
		Namespace: "prod", Workload: "api",
		Policy: &ciliumv2.CiliumNetworkPolicy{
			TypeMeta:   metav1.TypeMeta{APIVersion: "cilium.io/v2", Kind: "CiliumNetworkPolicy"},
			ObjectMeta: metav1.ObjectMeta{Name: "cpg-api", Namespace: "prod"},
			Spec:       &api.Rule{},
		},
		Attribution: []policy.RuleAttribution{{
			Key: policy.RuleKey{
				Direction: "ingress",
				Peer:      policy.Peer{Type: policy.PeerEndpoint, Labels: map[string]string{"app": "client"}},
				Port:      "8080", Protocol: "TCP",
			},
			FlowCount: 1,
			FirstSeen: flow.GetTime().AsTime(),
			LastSeen:  flow.GetTime().AsTime(),
			Samples:   []*flowpb.Flow{flow},
		}},
	}

	ew.handle(pe)
	ew.finalize(3, 0)

	path := filepath.Join(tmp, "hash0", "prod", "api.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var pev evidence.PolicyEvidence
	require.NoError(t, json.Unmarshal(data, &pev))
	assert.Len(t, pev.Sessions, 1)
	assert.Equal(t, sessionID, pev.Sessions[0].ID)
	require.Len(t, pev.Rules, 1)
	assert.Equal(t, "ingress:ep:app=client:TCP:8080", pev.Rules[0].Key)
	require.Len(t, pev.Rules[0].Samples, 1)
	assert.Equal(t, "default", pev.Rules[0].Samples[0].Src.Namespace)
}
