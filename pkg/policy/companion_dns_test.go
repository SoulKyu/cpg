package policy

import (
	"reflect"
	"testing"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	"github.com/cilium/cilium/pkg/policy/api"
)

// assertHasKubeDNSCompanion verifies the DNS-02 invariant: every CNP whose
// generated rules contain toFQDNs ALSO contains a companion egress rule
// matching the cluster DNS pods (io.kubernetes.pod.namespace=kube-system +
// k8s-app in (kube-dns, coredns))
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
	t.Errorf("kube-dns companion egress rule missing or incomplete (need k8s-app in (kube-dns, coredns) + io.kubernetes.pod.namespace=kube-system + 53/UDP + 53/TCP + DNS rule)")
}

// egressMatchesKubeDNSSelector reports whether the egress rule's ToEndpoints
// select the DNS pods via the fallback selector (kube-system +
// k8s-app in (kube-dns, coredns)) or the legacy k8s-app=kube-dns matchLabels.
func egressMatchesKubeDNSSelector(eg api.EgressRule) bool {
	return egressSelectsKubeDNS(eg, nil)
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
	ensureKubeDNSCompanion(cnp, nil)
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
	ensureKubeDNSCompanion(cnp, nil)
	if got := len(cnp.Spec.Egress); got != before {
		t.Errorf("non-FQDN CNP must not gain a companion rule; before=%d after=%d", before, got)
	}
}

func TestEnsureKubeDNSCompanion_SingleFQDN_AddsCompanion(t *testing.T) {
	cnp := fqdnEgressCNP("api.example.com")
	ensureKubeDNSCompanion(cnp, nil)
	assertHasKubeDNSCompanion(t, cnp)
	// Companion is appended (not prepended) to preserve existing test golden orders.
	if len(cnp.Spec.Egress) != 2 {
		t.Fatalf("expected 2 egress rules (FQDN + companion); got %d", len(cnp.Spec.Egress))
	}
}

func TestEnsureKubeDNSCompanion_MultiFQDN_CompanionListsAllNames(t *testing.T) {
	cnp := fqdnEgressCNP("z.example.com", "a.example.com", "m.example.com")
	ensureKubeDNSCompanion(cnp, nil)
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
	ensureKubeDNSCompanion(cnp, nil)
	first := len(cnp.Spec.Egress)
	ensureKubeDNSCompanion(cnp, nil)
	second := len(cnp.Spec.Egress)
	if first != second {
		t.Errorf("ensureKubeDNSCompanion must be idempotent; first=%d second=%d", first, second)
	}
	assertHasKubeDNSCompanion(t, cnp)
}

func TestEnsureKubeDNSCompanion_NeverEmitsMatchPattern(t *testing.T) {
	// DNS-03: no glob/matchPattern is ever emitted, including in the companion.
	cnp := fqdnEgressCNP("api.example.com", "www.example.org")
	ensureKubeDNSCompanion(cnp, nil)
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

func companionSelector(t *testing.T, cnp *ciliumv2.CiliumNetworkPolicy) *slim_metav1.LabelSelector {
	t.Helper()
	last := cnp.Spec.Egress[len(cnp.Spec.Egress)-1]
	if len(last.ToEndpoints) != 1 || last.ToEndpoints[0].LabelSelector == nil {
		t.Fatalf("companion must carry exactly one label selector; got %+v", last.ToEndpoints)
	}
	return last.ToEndpoints[0].LabelSelector
}

// DNS-FUT-02: without a detected selector (replay, --server, RBAC denied) the
// companion selects both the legacy kube-dns label and the upstream CoreDNS
// chart default (k8s-app=coredns, e.g. Outscale OKS).
func TestEnsureKubeDNSCompanion_Fallback_SelectsKubeDNSAndCoreDNS(t *testing.T) {
	cnp := fqdnEgressCNP("api.example.com")
	ensureKubeDNSCompanion(cnp, nil)

	sel := companionSelector(t, cnp)
	wantLabels := map[string]string{"io.kubernetes.pod.namespace": "kube-system"}
	if !reflect.DeepEqual(sel.MatchLabels, wantLabels) {
		t.Errorf("matchLabels = %v, want %v", sel.MatchLabels, wantLabels)
	}
	wantExpr := []slim_metav1.LabelSelectorRequirement{{
		Key: "k8s-app", Operator: slim_metav1.LabelSelectorOpIn, Values: []string{"kube-dns", "coredns"},
	}}
	if !reflect.DeepEqual(sel.MatchExpressions, wantExpr) {
		t.Errorf("matchExpressions = %+v, want %+v", sel.MatchExpressions, wantExpr)
	}
}

// DNS-FUT-02: a selector detected from the kube-system DNS Service is used
// verbatim, pinned to kube-system.
func TestEnsureKubeDNSCompanion_DetectedSelector(t *testing.T) {
	detected := map[string]string{
		"app.kubernetes.io/instance": "coredns",
		"app.kubernetes.io/name":     "coredns",
		"k8s-app":                    "coredns",
	}
	cnp := fqdnEgressCNP("api.example.com")
	ensureKubeDNSCompanion(cnp, detected)

	sel := companionSelector(t, cnp)
	want := map[string]string{
		"app.kubernetes.io/instance":  "coredns",
		"app.kubernetes.io/name":      "coredns",
		"k8s-app":                     "coredns",
		"io.kubernetes.pod.namespace": "kube-system",
	}
	if !reflect.DeepEqual(sel.MatchLabels, want) {
		t.Errorf("matchLabels = %v, want %v", sel.MatchLabels, want)
	}
	if len(sel.MatchExpressions) != 0 {
		t.Errorf("detected selector must not carry matchExpressions; got %+v", sel.MatchExpressions)
	}

	before := len(cnp.Spec.Egress)
	ensureKubeDNSCompanion(cnp, detected)
	if len(cnp.Spec.Egress) != before {
		t.Errorf("detected-selector companion must be idempotent; before=%d after=%d", before, len(cnp.Spec.Egress))
	}
}

// A companion already present in the legacy (k8s-app=kube-dns) or hand-edited
// (k8s-app=coredns) matchLabels form must not be duplicated.
func TestEnsureKubeDNSCompanion_ExistingMatchLabelsForms_NotDuplicated(t *testing.T) {
	for _, app := range []string{"kube-dns", "coredns"} {
		t.Run(app, func(t *testing.T) {
			cnp := fqdnEgressCNP("api.example.com")
			cnp.Spec.Egress = append(cnp.Spec.Egress, api.EgressRule{
				EgressCommonRule: api.EgressCommonRule{ToEndpoints: []api.EndpointSelector{{
					LabelSelector: &slim_metav1.LabelSelector{MatchLabels: map[string]string{
						"k8s:k8s-app":                     app,
						"k8s:io.kubernetes.pod.namespace": "kube-system",
					}},
				}}},
				ToPorts: api.PortRules{{Ports: []api.PortProtocol{
					{Port: "53", Protocol: api.ProtoUDP},
					{Port: "53", Protocol: api.ProtoTCP},
				}}},
			})
			before := len(cnp.Spec.Egress)
			ensureKubeDNSCompanion(cnp, nil)
			if len(cnp.Spec.Egress) != before {
				t.Errorf("existing %s companion must not be duplicated; before=%d after=%d", app, before, len(cnp.Spec.Egress))
			}
		})
	}
}
