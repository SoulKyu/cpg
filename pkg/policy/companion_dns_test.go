package policy

import (
	"testing"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
)

// assertHasKubeDNSCompanion verifies the DNS-02 invariant: every CNP whose
// generated rules contain toFQDNs ALSO contains a companion egress rule
// matching kube-dns (k8s-app=kube-dns, io.kubernetes.pod.namespace=kube-system)
// allowing UDP+TCP/53 with at least one DNS matchName entry. Used as a shared
// test helper across this plan and downstream plans (09-02, 09-04).
func assertHasKubeDNSCompanion(t *testing.T, cnp *ciliumv2.CiliumNetworkPolicy) {
	t.Helper()
	if cnp == nil || cnp.Spec == nil {
		t.Fatalf("assertHasKubeDNSCompanion: cnp or cnp.Spec is nil")
	}
	for _, eg := range cnp.Spec.Egress {
		if !egressMatchesKubeDNSSelector(eg) {
			continue
		}
		hasUDP, hasTCP, hasDNSRule := false, false, false
		for _, pr := range eg.ToPorts {
			for _, p := range pr.Ports {
				if p.Port == "53" {
					switch p.Protocol {
					case api.ProtoUDP:
						hasUDP = true
					case api.ProtoTCP:
						hasTCP = true
					}
				}
			}
			if pr.Rules != nil && len(pr.Rules.DNS) > 0 {
				hasDNSRule = true
			}
		}
		if hasUDP && hasTCP && hasDNSRule {
			return
		}
	}
	t.Errorf("kube-dns companion egress rule missing or incomplete (need k8s-app=kube-dns + io.kubernetes.pod.namespace=kube-system + 53/UDP + 53/TCP + DNS rule)")
}

// egressMatchesKubeDNSSelector reports whether the egress rule's ToEndpoints
// carry the kube-dns selector pair.
func egressMatchesKubeDNSSelector(eg api.EgressRule) bool {
	for _, ep := range eg.ToEndpoints {
		if ep.LabelSelector == nil {
			continue
		}
		ml := ep.LabelSelector.MatchLabels
		if matchKubeDNSLabels(ml) {
			return true
		}
	}
	return false
}

// matchKubeDNSLabels checks both plain and any:-prefixed forms of the kube-dns
// labels (Cilium adds "any:" or "k8s:" source prefix during YAML roundtrip).
func matchKubeDNSLabels(ml map[string]string) bool {
	hasApp := false
	hasNS := false
	for k, v := range ml {
		// Strip Cilium label-source prefix (any:, k8s:) for comparison.
		bare := stripCiliumLabelSource(k)
		if bare == "k8s-app" && v == "kube-dns" {
			hasApp = true
		}
		if bare == "io.kubernetes.pod.namespace" && v == "kube-system" {
			hasNS = true
		}
	}
	return hasApp && hasNS
}

// stripCiliumLabelSource trims the leading "any:" or "k8s:" source prefix that
// Cilium prepends to bare label keys during EndpointSelector serialization.
func stripCiliumLabelSource(k string) string {
	for _, prefix := range []string{"any:", "k8s:"} {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			return k[len(prefix):]
		}
	}
	return k
}

// fqdnEgressCNP builds a synthetic CNP carrying a single egress rule with the
// supplied FQDN match names. Used as input fixture for ensureKubeDNSCompanion
// tests.
func fqdnEgressCNP(names ...string) *ciliumv2.CiliumNetworkPolicy {
	fqdns := make(api.FQDNSelectorSlice, len(names))
	dnsRules := make([]api.PortRuleDNS, len(names))
	for i, n := range names {
		fqdns[i] = api.FQDNSelector{MatchName: n}
		dnsRules[i] = api.PortRuleDNS{MatchName: n}
	}
	return &ciliumv2.CiliumNetworkPolicy{
		Spec: &api.Rule{
			Egress: []api.EgressRule{
				{
					ToFQDNs: fqdns,
					ToPorts: api.PortRules{{
						Ports: []api.PortProtocol{{Port: "53", Protocol: api.ProtoUDP}},
						Rules: &api.L7Rules{DNS: dnsRules},
					}},
				},
			},
		},
	}
}

func TestEnsureKubeDNSCompanion_EmptyCNP_NoOp(t *testing.T) {
	cnp := &ciliumv2.CiliumNetworkPolicy{Spec: &api.Rule{}}
	ensureKubeDNSCompanion(cnp)
	if len(cnp.Spec.Egress) != 0 {
		t.Errorf("empty CNP must remain empty after ensureKubeDNSCompanion; got %d egress rules", len(cnp.Spec.Egress))
	}
}

func TestEnsureKubeDNSCompanion_NoFQDN_NoOp(t *testing.T) {
	// CNP with an L4-only egress rule (no toFQDNs anywhere) must NOT receive a
	// companion injection.
	cnp := &ciliumv2.CiliumNetworkPolicy{
		Spec: &api.Rule{
			Egress: []api.EgressRule{
				{
					EgressCommonRule: api.EgressCommonRule{ToCIDR: api.CIDRSlice{"8.8.8.8/32"}},
					ToPorts: api.PortRules{{
						Ports: []api.PortProtocol{{Port: "443", Protocol: api.ProtoTCP}},
					}},
				},
			},
		},
	}
	before := len(cnp.Spec.Egress)
	ensureKubeDNSCompanion(cnp)
	if got := len(cnp.Spec.Egress); got != before {
		t.Errorf("non-FQDN CNP must not gain a companion rule; before=%d after=%d", before, got)
	}
}

func TestEnsureKubeDNSCompanion_SingleFQDN_AddsCompanion(t *testing.T) {
	cnp := fqdnEgressCNP("api.example.com")
	ensureKubeDNSCompanion(cnp)
	assertHasKubeDNSCompanion(t, cnp)
	// Companion is appended (not prepended) to preserve existing test golden orders.
	if len(cnp.Spec.Egress) != 2 {
		t.Fatalf("expected 2 egress rules (FQDN + companion); got %d", len(cnp.Spec.Egress))
	}
}

func TestEnsureKubeDNSCompanion_MultiFQDN_CompanionListsAllNames(t *testing.T) {
	cnp := fqdnEgressCNP("z.example.com", "a.example.com", "m.example.com")
	ensureKubeDNSCompanion(cnp)
	assertHasKubeDNSCompanion(t, cnp)

	// Locate the companion rule and assert its DNS list is sorted and contains
	// all observed names.
	var companion *api.EgressRule
	for i := range cnp.Spec.Egress {
		if egressMatchesKubeDNSSelector(cnp.Spec.Egress[i]) {
			companion = &cnp.Spec.Egress[i]
			break
		}
	}
	if companion == nil {
		t.Fatalf("companion rule not found")
	}
	var got []string
	for _, pr := range companion.ToPorts {
		if pr.Rules != nil {
			for _, d := range pr.Rules.DNS {
				got = append(got, d.MatchName)
			}
		}
	}
	want := []string{"a.example.com", "m.example.com", "z.example.com"}
	if len(got) != len(want) {
		t.Fatalf("companion DNS rule list length mismatch: got=%v want=%v", got, want)
	}
	// Names are deduplicated/sorted: any permutation is acceptable as long as
	// all three are present.
	seen := map[string]bool{}
	for _, n := range got {
		seen[n] = true
	}
	for _, n := range want {
		if !seen[n] {
			t.Errorf("companion DNS rule missing matchName=%q (got=%v)", n, got)
		}
	}
}

func TestEnsureKubeDNSCompanion_Idempotent(t *testing.T) {
	cnp := fqdnEgressCNP("api.example.com")
	ensureKubeDNSCompanion(cnp)
	first := len(cnp.Spec.Egress)
	ensureKubeDNSCompanion(cnp)
	second := len(cnp.Spec.Egress)
	if first != second {
		t.Errorf("ensureKubeDNSCompanion must be idempotent; first=%d second=%d", first, second)
	}
	assertHasKubeDNSCompanion(t, cnp)
}

func TestEnsureKubeDNSCompanion_NeverEmitsMatchPattern(t *testing.T) {
	// DNS-03: no glob/matchPattern is ever emitted, including in the companion.
	cnp := fqdnEgressCNP("api.example.com", "www.example.org")
	ensureKubeDNSCompanion(cnp)
	for _, eg := range cnp.Spec.Egress {
		for _, sel := range eg.ToFQDNs {
			if sel.MatchPattern != "" {
				t.Errorf("ToFQDNs entry must not carry MatchPattern (DNS-03); got %q", sel.MatchPattern)
			}
		}
		for _, pr := range eg.ToPorts {
			if pr.Rules == nil {
				continue
			}
			for _, d := range pr.Rules.DNS {
				if d.MatchPattern != "" {
					t.Errorf("L7Rules.DNS entry must not carry MatchPattern (DNS-03); got %q", d.MatchPattern)
				}
			}
		}
	}
}

