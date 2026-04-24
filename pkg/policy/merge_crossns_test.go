package policy_test

import (
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)

// TestMergeRoundtrip_CrossNamespace tests merge with cross-namespace peers
// (common with monitoring tools like coroot that observe other namespaces).
func TestMergeRoundtrip_CrossNamespace(t *testing.T) {
	// Ingress to coroot from kube-system
	flows := []*flowpb.Flow{
		{
			TrafficDirection: flowpb.TrafficDirection_INGRESS,
			Source: &flowpb.Endpoint{
				Labels:    []string{"k8s:app=kube-dns"},
				Namespace: "kube-system",
			},
			Destination: &flowpb.Endpoint{
				Labels:    []string{"k8s:app=coroot"},
				Namespace: "coroot",
			},
			L4: &flowpb.Layer4{
				Protocol: &flowpb.Layer4_TCP{
					TCP: &flowpb.TCP{DestinationPort: 8080},
				},
			},
		},
	}

	original, _ := policy.BuildPolicy("coroot", "coroot", flows, nil, policy.AttributionOptions{})
	origYAML, err := yaml.Marshal(original)
	require.NoError(t, err)
	t.Logf("Original YAML:\n%s", origYAML)

	var fromDisk ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(origYAML, &fromDisk))

	incoming, _ := policy.BuildPolicy("coroot", "coroot", flows, nil, policy.AttributionOptions{})
	merged := policy.MergePolicy(&fromDisk, incoming)
	mergedYAML, err := yaml.Marshal(merged)
	require.NoError(t, err)
	t.Logf("Merged YAML:\n%s", mergedYAML)

	assert.Equal(t, string(origYAML), string(mergedYAML),
		"Cross-namespace merge should produce identical YAML")
	assert.Len(t, merged.Spec.Ingress, 1)
}

// TestMergeRoundtrip_FallbackLabels tests merge with pods that have no
// standard app label (uses all k8s labels as fallback).
func TestMergeRoundtrip_FallbackLabels(t *testing.T) {
	flows := []*flowpb.Flow{
		{
			TrafficDirection: flowpb.TrafficDirection_INGRESS,
			Source: &flowpb.Endpoint{
				Labels:    []string{"k8s:component=etcd", "k8s:tier=control-plane"},
				Namespace: "kube-system",
			},
			Destination: &flowpb.Endpoint{
				Labels:    []string{"k8s:app=coroot"},
				Namespace: "coroot",
			},
			L4: &flowpb.Layer4{
				Protocol: &flowpb.Layer4_TCP{
					TCP: &flowpb.TCP{DestinationPort: 2379},
				},
			},
		},
	}

	original, _ := policy.BuildPolicy("coroot", "coroot", flows, nil, policy.AttributionOptions{})
	origYAML, err := yaml.Marshal(original)
	require.NoError(t, err)
	t.Logf("Original YAML:\n%s", origYAML)

	var fromDisk ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(origYAML, &fromDisk))

	incoming, _ := policy.BuildPolicy("coroot", "coroot", flows, nil, policy.AttributionOptions{})
	merged := policy.MergePolicy(&fromDisk, incoming)
	mergedYAML, err := yaml.Marshal(merged)
	require.NoError(t, err)
	t.Logf("Merged YAML:\n%s", mergedYAML)

	assert.Equal(t, string(origYAML), string(mergedYAML),
		"Fallback labels merge should produce identical YAML")
	assert.Len(t, merged.Spec.Ingress, 1)
}

// TestMergeRoundtrip_MultiPeerAccumulation simulates real streaming behavior:
// Flush 1: peers A,B → written to disk
// Flush 2: peers C → merged with existing
// Flush 3: peers A → merged again (should not duplicate A)
func TestMergeRoundtrip_MultiPeerAccumulation(t *testing.T) {
	flowsFlush1 := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client-a"}, []string{"k8s:app=server"}, "default", 80),
		testdata.IngressTCPFlow([]string{"k8s:app=client-b"}, []string{"k8s:app=server"}, "default", 443),
	}
	flowsFlush2 := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client-c"}, []string{"k8s:app=server"}, "default", 8080),
	}
	flowsFlush3 := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client-a"}, []string{"k8s:app=server"}, "default", 80),
	}

	// Flush 1
	p1, _ := policy.BuildPolicy("default", "server", flowsFlush1, nil, policy.AttributionOptions{})
	yaml1, err := yaml.Marshal(p1)
	require.NoError(t, err)
	t.Logf("After flush 1:\n%s", yaml1)

	// Flush 2: merge new peer
	var disk1 ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(yaml1, &disk1))
	p2, _ := policy.BuildPolicy("default", "server", flowsFlush2, nil, policy.AttributionOptions{})
	merged2 := policy.MergePolicy(&disk1, p2)
	yaml2, err := yaml.Marshal(merged2)
	require.NoError(t, err)
	t.Logf("After flush 2:\n%s", yaml2)
	require.Len(t, merged2.Spec.Ingress, 3, "Should have 3 rules")

	// Flush 3: same as flush 1 (peer A) - should NOT duplicate
	var disk2 ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(yaml2, &disk2))
	p3, _ := policy.BuildPolicy("default", "server", flowsFlush3, nil, policy.AttributionOptions{})
	merged3 := policy.MergePolicy(&disk2, p3)
	yaml3, err := yaml.Marshal(merged3)
	require.NoError(t, err)
	t.Logf("After flush 3:\n%s", yaml3)

	assert.Equal(t, string(yaml2), string(yaml3),
		"Flush 3 should NOT add duplicate rules")
	assert.Len(t, merged3.Spec.Ingress, 3, "Should still have exactly 3 rules")
}

// TestMergeRoundtrip_MixedWorldAndEndpoints tests accumulation with a mix
// of CIDR and endpoint rules across flush cycles.
func TestMergeRoundtrip_MixedWorldAndEndpoints_Accumulation(t *testing.T) {
	flowsFlush1 := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=server"}, "default", 80),
		testdata.WorldIngressTCPFlow("1.2.3.4", 443, []string{"k8s:app=server"}, "default"),
	}
	flowsFlush2 := []*flowpb.Flow{
		testdata.WorldIngressTCPFlow("5.6.7.8", 8080, []string{"k8s:app=server"}, "default"),
	}

	// Flush 1
	p1, _ := policy.BuildPolicy("default", "server", flowsFlush1, nil, policy.AttributionOptions{})
	yaml1, err := yaml.Marshal(p1)
	require.NoError(t, err)
	t.Logf("After flush 1:\n%s", yaml1)

	// Flush 2: new CIDR
	var disk1 ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(yaml1, &disk1))
	p2, _ := policy.BuildPolicy("default", "server", flowsFlush2, nil, policy.AttributionOptions{})
	merged2 := policy.MergePolicy(&disk1, p2)
	yaml2, err := yaml.Marshal(merged2)
	require.NoError(t, err)
	t.Logf("After flush 2:\n%s", yaml2)

	// Flush 3: same as flush 1 (should NOT duplicate)
	var disk2 ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(yaml2, &disk2))
	p3, _ := policy.BuildPolicy("default", "server", flowsFlush1, nil, policy.AttributionOptions{})
	merged3 := policy.MergePolicy(&disk2, p3)
	yaml3, err := yaml.Marshal(merged3)
	require.NoError(t, err)
	t.Logf("After flush 3:\n%s", yaml3)

	assert.Equal(t, string(yaml2), string(yaml3),
		"Flush 3 should NOT change the policy")
}
