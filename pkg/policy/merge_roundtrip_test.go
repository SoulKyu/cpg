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

// TestMergeAfterYAMLRoundtrip simulates the real bug scenario:
// 1. BuildPolicy from flows → write YAML to disk
// 2. Same flows arrive in next flush → BuildPolicy again
// 3. Read existing YAML from disk → unmarshal
// 4. MergePolicy(unmarshaled, new) → should NOT duplicate rules
// 5. Compare marshaled YAML → should be identical
func TestMergeAfterYAMLRoundtrip_EndpointRules(t *testing.T) {
	flows := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default", 80,
	)

	// Flush 1: build and "write to disk"
	original, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{flows}, nil, policy.AttributionOptions{})
	originalYAML, err := yaml.Marshal(original)
	require.NoError(t, err)
	t.Logf("Original YAML:\n%s", originalYAML)

	// Simulate reading from disk
	var fromDisk ciliumv2.CiliumNetworkPolicy
	err = yaml.Unmarshal(originalYAML, &fromDisk)
	require.NoError(t, err)

	// Flush 2: same flows → same policy
	incoming, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{flows}, nil, policy.AttributionOptions{})

	// Merge
	merged := policy.MergePolicy(&fromDisk, incoming)

	// Serialize merged
	mergedYAML, err := yaml.Marshal(merged)
	require.NoError(t, err)
	t.Logf("Merged YAML:\n%s", mergedYAML)

	// YAML should be byte-for-byte identical
	assert.Equal(t, string(originalYAML), string(mergedYAML),
		"Merged YAML should be identical to original when no new rules added")

	// Rules should not be duplicated
	require.Len(t, merged.Spec.Ingress, 1, "Should have exactly 1 ingress rule")
	require.Len(t, merged.Spec.Ingress[0].ToPorts, 1)
	assert.Len(t, merged.Spec.Ingress[0].ToPorts[0].Ports, 1, "Should have exactly 1 port")
}

func TestMergeAfterYAMLRoundtrip_MultipleRules(t *testing.T) {
	// Flush 1: two different peers
	flows1 := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client-a"}, []string{"k8s:app=server"}, "default", 80),
		testdata.IngressTCPFlow([]string{"k8s:app=client-b"}, []string{"k8s:app=server"}, "default", 443),
	}
	original, _ := policy.BuildPolicy("default", "server", flows1, nil, policy.AttributionOptions{})
	originalYAML, err := yaml.Marshal(original)
	require.NoError(t, err)
	t.Logf("Original YAML:\n%s", originalYAML)

	// Read from disk
	var fromDisk ciliumv2.CiliumNetworkPolicy
	err = yaml.Unmarshal(originalYAML, &fromDisk)
	require.NoError(t, err)

	// Flush 2: same flows
	incoming, _ := policy.BuildPolicy("default", "server", flows1, nil, policy.AttributionOptions{})

	// Merge
	merged := policy.MergePolicy(&fromDisk, incoming)
	mergedYAML, err := yaml.Marshal(merged)
	require.NoError(t, err)
	t.Logf("Merged YAML:\n%s", mergedYAML)

	assert.Equal(t, string(originalYAML), string(mergedYAML),
		"Merged YAML should be identical to original")
	assert.Len(t, merged.Spec.Ingress, 2, "Should still have exactly 2 ingress rules")
}

func TestMergeAfterYAMLRoundtrip_CIDRRules(t *testing.T) {
	flows := []*flowpb.Flow{
		testdata.WorldIngressTCPFlow("1.2.3.4", 80, []string{"k8s:app=server"}, "default"),
	}

	original, _ := policy.BuildPolicy("default", "server", flows, nil, policy.AttributionOptions{})
	originalYAML, err := yaml.Marshal(original)
	require.NoError(t, err)
	t.Logf("Original YAML:\n%s", originalYAML)

	var fromDisk ciliumv2.CiliumNetworkPolicy
	err = yaml.Unmarshal(originalYAML, &fromDisk)
	require.NoError(t, err)

	incoming, _ := policy.BuildPolicy("default", "server", flows, nil, policy.AttributionOptions{})
	merged := policy.MergePolicy(&fromDisk, incoming)
	mergedYAML, err := yaml.Marshal(merged)
	require.NoError(t, err)
	t.Logf("Merged YAML:\n%s", mergedYAML)

	assert.Equal(t, string(originalYAML), string(mergedYAML),
		"CIDR rule merge should produce identical YAML")
	assert.Len(t, merged.Spec.Ingress, 1, "Should have exactly 1 ingress rule")
}

func TestMergeAfterYAMLRoundtrip_MixedEndpointAndCIDR(t *testing.T) {
	flows := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=server"}, "default", 80),
		testdata.WorldIngressTCPFlow("1.2.3.4", 443, []string{"k8s:app=server"}, "default"),
	}

	original, _ := policy.BuildPolicy("default", "server", flows, nil, policy.AttributionOptions{})
	originalYAML, err := yaml.Marshal(original)
	require.NoError(t, err)
	t.Logf("Original YAML:\n%s", originalYAML)

	var fromDisk ciliumv2.CiliumNetworkPolicy
	err = yaml.Unmarshal(originalYAML, &fromDisk)
	require.NoError(t, err)

	incoming, _ := policy.BuildPolicy("default", "server", flows, nil, policy.AttributionOptions{})
	merged := policy.MergePolicy(&fromDisk, incoming)
	mergedYAML, err := yaml.Marshal(merged)
	require.NoError(t, err)
	t.Logf("Merged YAML:\n%s", mergedYAML)

	assert.Equal(t, string(originalYAML), string(mergedYAML),
		"Mixed rules merge should produce identical YAML")
	assert.Len(t, merged.Spec.Ingress, 2, "Should have exactly 2 ingress rules (1 CIDR + 1 endpoint)")
}

// TestMergeAfterYAMLRoundtrip_ThreeFlushCycles simulates 3 flush cycles
// where different subsets of flows appear each time.
func TestMergeAfterYAMLRoundtrip_ThreeFlushCycles(t *testing.T) {
	// Flush 1: peer A
	flush1 := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client-a"}, []string{"k8s:app=server"}, "default", 80),
	}
	policy1, _ := policy.BuildPolicy("default", "server", flush1, nil, policy.AttributionOptions{})
	yaml1, err := yaml.Marshal(policy1)
	require.NoError(t, err)
	t.Logf("After flush 1:\n%s", yaml1)

	// Flush 2: peer B (different peer)
	flush2 := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client-b"}, []string{"k8s:app=server"}, "default", 443),
	}
	policy2, _ := policy.BuildPolicy("default", "server", flush2, nil, policy.AttributionOptions{})

	// Read existing from "disk" and merge
	var disk1 ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(yaml1, &disk1))
	merged2 := policy.MergePolicy(&disk1, policy2)
	yaml2, err := yaml.Marshal(merged2)
	require.NoError(t, err)
	t.Logf("After flush 2 (merged):\n%s", yaml2)
	require.Len(t, merged2.Spec.Ingress, 2, "Should have 2 rules after flush 2")

	// Flush 3: peer A again (same as flush 1)
	flush3 := []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client-a"}, []string{"k8s:app=server"}, "default", 80),
	}
	policy3, _ := policy.BuildPolicy("default", "server", flush3, nil, policy.AttributionOptions{})

	var disk2 ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(yaml2, &disk2))
	merged3 := policy.MergePolicy(&disk2, policy3)
	yaml3, err := yaml.Marshal(merged3)
	require.NoError(t, err)
	t.Logf("After flush 3 (should be identical to flush 2):\n%s", yaml3)

	assert.Equal(t, string(yaml2), string(yaml3),
		"Flush 3 should produce identical YAML to flush 2 (no new content)")
	assert.Len(t, merged3.Spec.Ingress, 2, "Should still have exactly 2 ingress rules")
}

// TestMergeAfterYAMLRoundtrip_EgressCIDR tests egress CIDR rule dedup across roundtrips.
func TestMergeAfterYAMLRoundtrip_EgressCIDR(t *testing.T) {
	flows := []*flowpb.Flow{
		testdata.WorldEgressTCPFlow([]string{"k8s:app=client"}, "default", "8.8.8.8", 443),
	}

	original, _ := policy.BuildPolicy("default", "client", flows, nil, policy.AttributionOptions{})
	originalYAML, err := yaml.Marshal(original)
	require.NoError(t, err)
	t.Logf("Original YAML:\n%s", originalYAML)

	var fromDisk ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(originalYAML, &fromDisk))

	incoming, _ := policy.BuildPolicy("default", "client", flows, nil, policy.AttributionOptions{})
	merged := policy.MergePolicy(&fromDisk, incoming)
	mergedYAML, err := yaml.Marshal(merged)
	require.NoError(t, err)
	t.Logf("Merged YAML:\n%s", mergedYAML)

	assert.Equal(t, string(originalYAML), string(mergedYAML))
	assert.Len(t, merged.Spec.Egress, 1, "Should have exactly 1 egress rule")
}
