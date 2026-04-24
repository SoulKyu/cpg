package policy_test

import (
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)

func TestMergePolicy_ICMPAppendedToExistingTCPPeer(t *testing.T) {
	// Existing: peer A with TCP port 8080
	existing, _ := policy.BuildPolicy("coroot", "clickhouse", []*flowpb.Flow{
		testdata.IngressTCPFlow(
			[]string{"k8s:app.kubernetes.io/component=coroot-node-agent"},
			[]string{"k8s:app.kubernetes.io/component=clickhouse"},
			"coroot", 8080,
		),
	}, nil, policy.AttributionOptions{})
	// Incoming: same peer A with ICMP EchoRequest (type 8)
	incoming, _ := policy.BuildPolicy("coroot", "clickhouse", []*flowpb.Flow{
		testdata.IngressICMPv4Flow(
			[]string{"k8s:app.kubernetes.io/component=coroot-node-agent"}, "coroot",
			[]string{"k8s:app.kubernetes.io/component=clickhouse"}, "coroot", 8,
		),
	}, nil, policy.AttributionOptions{})

	merged := policy.MergePolicy(existing, incoming)
	require.NotNil(t, merged.Spec)

	// Should have two rules: one ToPorts, one ICMPs (separate per Cilium spec)
	require.Len(t, merged.Spec.Ingress, 2, "expected separate ToPorts and ICMPs rules")

	var hasPort, hasICMP bool
	for _, r := range merged.Spec.Ingress {
		if len(r.ToPorts) > 0 {
			hasPort = true
			assert.Equal(t, "8080", r.ToPorts[0].Ports[0].Port)
		}
		if len(r.ICMPs) > 0 {
			hasICMP = true
			assert.Equal(t, "IPv4", r.ICMPs[0].Fields[0].Family)
		}
	}
	assert.True(t, hasPort, "should have ToPorts rule")
	assert.True(t, hasICMP, "should have ICMPs rule")
}

func TestMergePolicy_ICMPDedup(t *testing.T) {
	// Both have ICMP type 8 from same peer
	existing, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.IngressICMPv4Flow(
			[]string{"k8s:app=client"}, "default",
			[]string{"k8s:app=server"}, "default", 8,
		),
	}, nil, policy.AttributionOptions{})
	incoming, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.IngressICMPv4Flow(
			[]string{"k8s:app=client"}, "default",
			[]string{"k8s:app=server"}, "default", 8,
		),
	}, nil, policy.AttributionOptions{})

	merged := policy.MergePolicy(existing, incoming)
	require.NotNil(t, merged.Spec)

	// Should have one ICMP rule with one field (no dup)
	require.Len(t, merged.Spec.Ingress, 1)
	require.Len(t, merged.Spec.Ingress[0].ICMPs, 1)
	assert.Len(t, merged.Spec.Ingress[0].ICMPs[0].Fields, 1)
}

func TestMergePolicy_ICMPNewTypeMerged(t *testing.T) {
	// Existing: ICMP type 8 (EchoRequest)
	existing, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.IngressICMPv4Flow(
			[]string{"k8s:app=client"}, "default",
			[]string{"k8s:app=server"}, "default", 8,
		),
	}, nil, policy.AttributionOptions{})
	// Incoming: ICMP type 0 (EchoReply)
	incoming, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.IngressICMPv4Flow(
			[]string{"k8s:app=client"}, "default",
			[]string{"k8s:app=server"}, "default", 0,
		),
	}, nil, policy.AttributionOptions{})

	merged := policy.MergePolicy(existing, incoming)
	require.NotNil(t, merged.Spec)

	// Should have one ICMP rule with two fields
	require.Len(t, merged.Spec.Ingress, 1)
	require.Len(t, merged.Spec.Ingress[0].ICMPs, 1)
	assert.Len(t, merged.Spec.Ingress[0].ICMPs[0].Fields, 2)
}

func TestMergePolicy_EntityRulesAppended(t *testing.T) {
	// Existing: ingress from kube-apiserver on port 443
	existing, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.EntityIngressFlow(
			[]string{"reserved:kube-apiserver"},
			[]string{"k8s:app=server"}, "default", 443,
		),
	}, nil, policy.AttributionOptions{})
	// Incoming: ingress from host on port 8080
	incoming, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.EntityIngressFlow(
			[]string{"reserved:host"},
			[]string{"k8s:app=server"}, "default", 8080,
		),
	}, nil, policy.AttributionOptions{})

	merged := policy.MergePolicy(existing, incoming)
	require.NotNil(t, merged.Spec)

	// Different entities → two separate rules
	assert.Len(t, merged.Spec.Ingress, 2)
}

func TestMergePolicy_EntityPortsMerged(t *testing.T) {
	// Both from kube-apiserver, different ports
	existing, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.EntityIngressFlow(
			[]string{"reserved:kube-apiserver"},
			[]string{"k8s:app=server"}, "default", 443,
		),
	}, nil, policy.AttributionOptions{})
	incoming, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{
		testdata.EntityIngressFlow(
			[]string{"reserved:kube-apiserver"},
			[]string{"k8s:app=server"}, "default", 6443,
		),
	}, nil, policy.AttributionOptions{})

	merged := policy.MergePolicy(existing, incoming)
	require.NotNil(t, merged.Spec)

	// Same entity → one rule with two ports
	require.Len(t, merged.Spec.Ingress, 1)
	require.Len(t, merged.Spec.Ingress[0].ToPorts, 1)
	assert.Len(t, merged.Spec.Ingress[0].ToPorts[0].Ports, 2)
}

func TestMergePolicy_EgressICMPAppended(t *testing.T) {
	// Existing: egress TCP to peer
	existing, _ := policy.BuildPolicy("default", "client", []*flowpb.Flow{
		testdata.EgressUDPFlow(
			[]string{"k8s:app=client"},
			[]string{"k8s:app=dns"}, "default", 53,
		),
	}, nil, policy.AttributionOptions{})
	// Incoming: egress ICMP to same peer
	incoming, _ := policy.BuildPolicy("default", "client", []*flowpb.Flow{
		testdata.EgressICMPv4Flow(
			[]string{"k8s:app=client"}, "default",
			[]string{"k8s:app=dns"}, "10.0.0.1", 8,
		),
	}, nil, policy.AttributionOptions{})

	merged := policy.MergePolicy(existing, incoming)
	require.NotNil(t, merged.Spec)

	// Different rule types (port vs ICMP) → should not merge into same rule
	// The ICMP egress flow goes to a different "peer" (no namespace on dest) so it's a new rule
	var hasPort, hasICMP bool
	for _, r := range merged.Spec.Egress {
		if len(r.ToPorts) > 0 {
			hasPort = true
		}
		if len(r.ICMPs) > 0 {
			hasICMP = true
		}
	}
	assert.True(t, hasPort, "should have ToPorts egress rule")
	assert.True(t, hasICMP, "should have ICMPs egress rule")
}
