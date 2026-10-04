package policy_test

import (
	"testing"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	slimmetav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/policy"
)

// replaceTestCNP builds a minimal CNP carrying one ingress rule whose single
// (port, proto) PortRule holds the given L7 rules.
func replaceTestCNP(http []api.PortRuleHTTP, dns []api.PortRuleDNS) *ciliumv2.CiliumNetworkPolicy {
	rules := &api.L7Rules{HTTP: http, DNS: dns}
	return &ciliumv2.CiliumNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "cpg-api", Namespace: "production"},
		Spec: &api.Rule{
			Ingress: []api.IngressRule{
				{
					IngressCommonRule: api.IngressCommonRule{
						FromEndpoints: []api.EndpointSelector{{LabelSelector: &slimmetav1.LabelSelector{MatchLabels: map[string]string{"app": "frontend"}}}},
					},
					ToPorts: []api.PortRule{
						{
							Ports: []api.PortProtocol{{Port: "8080", Protocol: api.ProtoTCP}},
							Rules: rules,
						},
					},
				},
			},
		},
	}
}

// replaceHTTPRules extracts the HTTP sub-list of the first ingress PortRule.
func replaceHTTPRules(cnp *ciliumv2.CiliumNetworkPolicy) []api.PortRuleHTTP {
	if cnp == nil || cnp.Spec == nil || len(cnp.Spec.Ingress) == 0 {
		return nil
	}
	tp := cnp.Spec.Ingress[0].ToPorts
	if len(tp) == 0 || tp[0].Rules == nil {
		return nil
	}
	return tp[0].Rules.HTTP
}

func replaceDNSRules(cnp *ciliumv2.CiliumNetworkPolicy) []api.PortRuleDNS {
	if cnp == nil || cnp.Spec == nil || len(cnp.Spec.Ingress) == 0 {
		return nil
	}
	tp := cnp.Spec.Ingress[0].ToPorts
	if len(tp) == 0 || tp[0].Rules == nil {
		return nil
	}
	return tp[0].Rules.DNS
}

// TestMergePolicy_ReplaceL7HTTP_ReplacesLiteralsWithTemplate proves the
// Decision-1 reconciliation contract: a collapse-enabled flush's
// whole-run-authoritative HTTP set REPLACES the literal rules written by
// earlier flushes (whose dedup keys differ), leaving no stale literals.
func TestMergePolicy_ReplaceL7HTTP_ReplacesLiteralsWithTemplate(t *testing.T) {
	existing := replaceTestCNP([]api.PortRuleHTTP{
		{Method: "GET", Path: `^/api/v1/users/1$`},
		{Method: "GET", Path: `^/api/v1/users/2$`},
	}, nil)
	incoming := replaceTestCNP([]api.PortRuleHTTP{
		{Method: "GET", Path: `^/api/v1/users/[0-9]+$`},
	}, nil)

	merged := policy.MergePolicyWithOptions(existing, incoming, policy.MergeOptions{ReplaceL7HTTP: true})

	got := replaceHTTPRules(merged)
	require.Len(t, got, 1, "replace-merge must leave exactly the template rule")
	assert.Equal(t, "GET", got[0].Method)
	assert.Equal(t, `^/api/v1/users/[0-9]+$`, got[0].Path)
	assert.NotContains(t, got, api.PortRuleHTTP{Method: "GET", Path: `^/api/v1/users/1$`},
		"stale literal rules written before the gate was crossed must be gone")
}

// TestMergePolicy_ReplaceL7HTTP_PreservesDNS proves replace-merge touches
// ONLY the HTTP sub-list: the kube-dns companion DNS matchName on the same
// bucket survives.
func TestMergePolicy_ReplaceL7HTTP_PreservesDNS(t *testing.T) {
	const kubeDNS = "kube-dns.kube-system.svc.cluster.local"
	existing := replaceTestCNP([]api.PortRuleHTTP{
		{Method: "GET", Path: `^/api/v1/users/1$`},
	}, []api.PortRuleDNS{{MatchName: kubeDNS}})
	incoming := replaceTestCNP([]api.PortRuleHTTP{
		{Method: "GET", Path: `^/api/v1/users/[0-9]+$`},
	}, []api.PortRuleDNS{{MatchName: kubeDNS}})

	merged := policy.MergePolicyWithOptions(existing, incoming, policy.MergeOptions{ReplaceL7HTTP: true})

	gotHTTP := replaceHTTPRules(merged)
	require.Len(t, gotHTTP, 1)
	assert.Equal(t, `^/api/v1/users/[0-9]+$`, gotHTTP[0].Path)

	gotDNS := replaceDNSRules(merged)
	require.Len(t, gotDNS, 1, "DNS sub-list must be preserved by replace-merge")
	assert.Equal(t, kubeDNS, gotDNS[0].MatchName)
}

// TestMergePolicy_ZeroOptionsUnchanged proves MergePolicyWithOptions with a
// zero MergeOptions behaves exactly like the pre-phase MergePolicy
// (union-merge, no replacement) — default-off invariant at the merge layer.
func TestMergePolicy_ZeroOptionsUnchanged(t *testing.T) {
	existing := replaceTestCNP([]api.PortRuleHTTP{
		{Method: "GET", Path: "/a"},
	}, nil)
	incoming := replaceTestCNP([]api.PortRuleHTTP{
		{Method: "GET", Path: "/a"},
		{Method: "POST", Path: "/b"},
	}, nil)

	merged := policy.MergePolicyWithOptions(existing, incoming, policy.MergeOptions{})
	got := replaceHTTPRules(merged)
	require.Len(t, got, 2, "zero options must union-merge (no replacement)")
	assert.Equal(t, "GET", got[0].Method)
	assert.Equal(t, "/a", got[0].Path)
	assert.Equal(t, "POST", got[1].Method)
	assert.Equal(t, "/b", got[1].Path)

	// MergePolicy delegate equivalence on the same inputs.
	viaDelegate := policy.MergePolicy(existing, incoming)
	assert.Equal(t, replaceHTTPRules(viaDelegate), got,
		"MergePolicy must behave identically to MergePolicyWithOptions with zero options")
}
