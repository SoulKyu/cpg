// pkg/policy/builder_attribution_test.go
package policy

import (
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ingressTCPFlow(srcLabels, dstLabels []string, namespace string, port uint32) *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Labels: srcLabels, Namespace: "default"},
		Destination:      &flowpb.Endpoint{Labels: dstLabels, Namespace: namespace},
		L4:               &flowpb.Layer4{Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{DestinationPort: port}}},
	}
}

func TestBuildPolicyEmitsAttribution(t *testing.T) {
	flows := []*flowpb.Flow{
		ingressTCPFlow([]string{"k8s:app=frontend"}, []string{"k8s:app=api"}, "prod", 8080),
		ingressTCPFlow([]string{"k8s:app=frontend"}, []string{"k8s:app=api"}, "prod", 8080),
		ingressTCPFlow([]string{"k8s:app=worker"}, []string{"k8s:app=api"}, "prod", 8443),
	}

	cnp, attrib := BuildPolicy("prod", "api", flows, nil, AttributionOptions{MaxSamples: 10})

	require.NotNil(t, cnp)
	require.Len(t, cnp.Spec.Ingress, 2)

	ruleKeys := make(map[string]*RuleAttribution)
	for i := range attrib {
		ruleKeys[attrib[i].Key.String()] = &attrib[i]
	}

	k1 := RuleKey{Direction: "ingress", Peer: Peer{Type: PeerEndpoint, Labels: map[string]string{"app": "frontend"}}, Port: "8080", Protocol: "TCP"}
	k2 := RuleKey{Direction: "ingress", Peer: Peer{Type: PeerEndpoint, Labels: map[string]string{"app": "worker"}}, Port: "8443", Protocol: "TCP"}

	a1, ok := ruleKeys[k1.String()]
	require.True(t, ok, "attribution missing for %s", k1.String())
	assert.Equal(t, int64(2), a1.FlowCount)
	assert.Len(t, a1.Samples, 2)

	a2, ok := ruleKeys[k2.String()]
	require.True(t, ok)
	assert.Equal(t, int64(1), a2.FlowCount)
}
