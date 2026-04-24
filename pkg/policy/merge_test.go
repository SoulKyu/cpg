package policy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	flowpb "github.com/cilium/cilium/api/v1/flow"

	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)

func TestMergePolicy_AddPortToExistingPeer(t *testing.T) {
	// Existing: peer A port 80
	existing, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=server"}, "default", 80),
	}, nil, policy.AttributionOptions{})
	// Incoming: peer A port 443
	incoming, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=server"}, "default", 443),
	}, nil, policy.AttributionOptions{})

	merged := policy.MergePolicy(existing, incoming)
	require.NotNil(t, merged)
	require.NotNil(t, merged.Spec)

	// Should be one rule with two ports
	require.Len(t, merged.Spec.Ingress, 1)
	require.Len(t, merged.Spec.Ingress[0].ToPorts, 1)
	ports := merged.Spec.Ingress[0].ToPorts[0].Ports
	require.Len(t, ports, 2)

	portStrings := []string{ports[0].Port, ports[1].Port}
	assert.Contains(t, portStrings, "80")
	assert.Contains(t, portStrings, "443")
}

func TestMergePolicy_AddNewPeer(t *testing.T) {
	// Existing: peer A
	existing, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client-a"}, []string{"k8s:app=server"}, "default", 80),
	}, nil, policy.AttributionOptions{})
	// Incoming: peer B
	incoming, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client-b"}, []string{"k8s:app=server"}, "default", 80),
	}, nil, policy.AttributionOptions{})

	merged := policy.MergePolicy(existing, incoming)
	require.NotNil(t, merged.Spec)

	// Should have two separate ingress rules
	assert.Len(t, merged.Spec.Ingress, 2)
}

func TestMergePolicy_DuplicatePortSkipped(t *testing.T) {
	// Existing: peer A port 80/TCP
	existing, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=server"}, "default", 80),
	}, nil, policy.AttributionOptions{})
	// Incoming: same peer A port 80/TCP (duplicate)
	incoming, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=server"}, "default", 80),
	}, nil, policy.AttributionOptions{})

	merged := policy.MergePolicy(existing, incoming)
	require.NotNil(t, merged.Spec)

	require.Len(t, merged.Spec.Ingress, 1)
	require.Len(t, merged.Spec.Ingress[0].ToPorts, 1)
	// Should still be just one port (no duplicate)
	assert.Len(t, merged.Spec.Ingress[0].ToPorts[0].Ports, 1)
}

func TestMergePolicy_EgressMerge(t *testing.T) {
	// Existing: egress to dns port 53
	existing, _ := policy.BuildPolicy("default", "client", []*flowpb.Flow{
		testdata.EgressUDPFlow([]string{"k8s:app=client"}, []string{"k8s:app=dns"}, "default", 53),
	}, nil, policy.AttributionOptions{})
	// Incoming: egress to dns port 5353
	incoming, _ := policy.BuildPolicy("default", "client", []*flowpb.Flow{
		testdata.EgressUDPFlow([]string{"k8s:app=client"}, []string{"k8s:app=dns"}, "default", 5353),
	}, nil, policy.AttributionOptions{})

	merged := policy.MergePolicy(existing, incoming)
	require.NotNil(t, merged.Spec)

	require.Len(t, merged.Spec.Egress, 1)
	require.Len(t, merged.Spec.Egress[0].ToPorts, 1)
	ports := merged.Spec.Egress[0].ToPorts[0].Ports
	require.Len(t, ports, 2)

	portStrings := []string{ports[0].Port, ports[1].Port}
	assert.Contains(t, portStrings, "53")
	assert.Contains(t, portStrings, "5353")
}

func TestMergePolicy_PreservesObjectMeta(t *testing.T) {
	existing, _ := policy.BuildPolicy("production", "api", []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=api"}, "production", 80),
	}, nil, policy.AttributionOptions{})
	incoming, _ := policy.BuildPolicy("production", "api", []*flowpb.Flow{
		testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=api"}, "production", 443),
	}, nil, policy.AttributionOptions{})

	merged := policy.MergePolicy(existing, incoming)

	// ObjectMeta should be from existing
	assert.Equal(t, "cpg-api", merged.Name)
	assert.Equal(t, "production", merged.Namespace)
	assert.Equal(t, "cpg", merged.Labels["app.kubernetes.io/managed-by"])
}
