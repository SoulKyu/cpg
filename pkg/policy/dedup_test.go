package policy_test

import (
	"testing"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/SoulKyu/cpg/pkg/policy"
)

func makePolicy(name string, ingress []api.IngressRule, egress []api.EgressRule) *ciliumv2.CiliumNetworkPolicy {
	return &ciliumv2.CiliumNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: &api.Rule{
			Ingress: ingress,
			Egress:  egress,
		},
	}
}

func TestPoliciesEquivalent_SameSpecDifferentMeta(t *testing.T) {
	ingress := []api.IngressRule{
		{
			IngressCommonRule: api.IngressCommonRule{
				FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.1/32")},
			},
			ToPorts: api.PortRules{
				{Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}}},
			},
		},
	}

	a := makePolicy("policy-a", ingress, nil)
	b := makePolicy("policy-b", ingress, nil)

	equiv, err := policy.PoliciesEquivalent(a, b)
	require.NoError(t, err)
	assert.True(t, equiv, "same spec with different metadata should be equivalent")
}

func TestPoliciesEquivalent_DifferentIngressRules(t *testing.T) {
	a := makePolicy("policy", []api.IngressRule{
		{
			IngressCommonRule: api.IngressCommonRule{
				FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.1/32")},
			},
			ToPorts: api.PortRules{
				{Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}}},
			},
		},
	}, nil)

	b := makePolicy("policy", []api.IngressRule{
		{
			IngressCommonRule: api.IngressCommonRule{
				FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.2/32")},
			},
			ToPorts: api.PortRules{
				{Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}}},
			},
		},
	}, nil)

	equiv, err := policy.PoliciesEquivalent(a, b)
	require.NoError(t, err)
	assert.False(t, equiv, "different ingress rules should not be equivalent")
}

func TestPoliciesEquivalent_DifferentOrderSameRules(t *testing.T) {
	ruleA := api.IngressRule{
		IngressCommonRule: api.IngressCommonRule{
			FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.1/32")},
		},
		ToPorts: api.PortRules{
			{Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}}},
		},
	}
	ruleB := api.IngressRule{
		IngressCommonRule: api.IngressCommonRule{
			FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.2/32")},
		},
		ToPorts: api.PortRules{
			{Ports: []api.PortProtocol{{Port: "443", Protocol: api.ProtoTCP}}},
		},
	}

	a := makePolicy("policy", []api.IngressRule{ruleA, ruleB}, nil)
	b := makePolicy("policy", []api.IngressRule{ruleB, ruleA}, nil)

	equiv, err := policy.PoliciesEquivalent(a, b)
	require.NoError(t, err)
	assert.True(t, equiv, "same rules in different order should be equivalent")
}

// TestPoliciesEquivalent_SamePeerPortsAndICMPSwappedOrder locks the fix for
// order-dependent equivalence: two policies whose SAME-peer ports rule and
// ICMP rule are listed in swapped order must compare equal. Before the fix,
// ingress/egress keys ignored ports/ICMP, so both rules hashed to one key and
// the non-stable sort left them in input order — [ports, icmp] vs [icmp, ports]
// serialized differently and compared unequal.
func TestPoliciesEquivalent_SamePeerPortsAndICMPSwappedOrder(t *testing.T) {
	icmpType := intstr.FromInt32(8)
	portsRule := api.IngressRule{
		IngressCommonRule: api.IngressCommonRule{
			FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.1/32")},
		},
		ToPorts: api.PortRules{
			{Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}}},
		},
	}
	icmpRule := api.IngressRule{
		IngressCommonRule: api.IngressCommonRule{
			FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.1/32")},
		},
		ICMPs: api.ICMPRules{{Fields: []api.ICMPField{{Family: "IPv4", Type: &icmpType}}}},
	}

	a := makePolicy("policy", []api.IngressRule{portsRule, icmpRule}, nil)
	b := makePolicy("policy", []api.IngressRule{icmpRule, portsRule}, nil)

	equiv, err := policy.PoliciesEquivalent(a, b)
	require.NoError(t, err)
	assert.True(t, equiv, "same-peer ports/ICMP rules in swapped order must be equivalent")
}

// TestPoliciesEquivalent_SamePeerDifferentPortsNotEquivalent guards against the
// discriminating key over-collapsing: two same-peer rules with different ports
// must still compare unequal.
func TestPoliciesEquivalent_SamePeerDifferentPortsNotEquivalent(t *testing.T) {
	a := makePolicy("policy", []api.IngressRule{{
		IngressCommonRule: api.IngressCommonRule{FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.1/32")}},
		ToPorts:           api.PortRules{{Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}}}},
	}}, nil)
	b := makePolicy("policy", []api.IngressRule{{
		IngressCommonRule: api.IngressCommonRule{FromCIDR: api.CIDRSlice{api.CIDR("10.0.0.1/32")}},
		ToPorts:           api.PortRules{{Ports: []api.PortProtocol{{Port: "443", Protocol: api.ProtoTCP}}}},
	}}, nil)

	equiv, err := policy.PoliciesEquivalent(a, b)
	require.NoError(t, err)
	assert.False(t, equiv, "same peer, different ports must not be equivalent")
}

func TestPoliciesEquivalent_NilPolicy(t *testing.T) {
	p := makePolicy("policy", nil, nil)

	equiv, err := policy.PoliciesEquivalent(p, nil)
	require.NoError(t, err)
	assert.False(t, equiv, "policy vs nil should not be equivalent")
	equiv, err = policy.PoliciesEquivalent(nil, p)
	require.NoError(t, err)
	assert.False(t, equiv, "nil vs policy should not be equivalent")
	equiv, err = policy.PoliciesEquivalent(nil, nil)
	require.NoError(t, err)
	assert.True(t, equiv, "nil vs nil should be equivalent")
}
