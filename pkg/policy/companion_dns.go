// Package policy — kube-dns companion rule injector.
//
// This file implements the DNS-02 invariant: every CiliumNetworkPolicy that
// carries `toFQDNs` MUST also carry a companion egress rule allowing UDP+TCP
// on port 53 to the kube-dns workload. Without that companion rule, ToFQDNs
// is unusable in practice — the resolver IP would be denied by the implicit
// default-deny semantics that ToFQDNs introduces (see Cilium docs: "An
// explicit rule to allow for DNS traffic is needed for the pods").
//
// The selector pair (k8s-app=kube-dns, io.kubernetes.pod.namespace=kube-system)
// is hardcoded for v1.2; selector autodetection across CNI distributions is
// deferred to v1.3 (DNS-FUT-02 / L7-FUT-03).
//
// DNS-03 invariant: the companion rule's `rules.dns` block lists each
// observed FQDN as a literal matchName entry — never a matchPattern. This
// keeps the "no glob auto-generation" promise intact even for the companion.
package policy

import (
	"sort"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	"github.com/cilium/cilium/pkg/policy/api"
)

// kube-dns companion rule selector and ports — hardcoded for v1.2.
const (
	kubeDNSSelectorAppKey = "k8s-app"
	kubeDNSSelectorAppVal = "kube-dns"
	kubeDNSSelectorNSKey  = "io.kubernetes.pod.namespace"
	kubeDNSSelectorNSVal  = "kube-system"
)

// ensureKubeDNSCompanion enforces the DNS-02 invariant on the supplied CNP.
//
// Behavior:
//  1. Walk cnp.Spec.Egress and collect every FQDN matchName referenced via
//     ToFQDNs. If none are present, return without modification (no FQDN →
//     no companion needed).
//  2. If any existing egress rule already covers kube-dns 53/UDP+TCP with a
//     DNS L7 block, return without modification (idempotent — repeated calls
//     never duplicate the companion).
//  3. Otherwise APPEND a fresh egress rule selecting kube-dns and listing
//     each observed FQDN as a literal `rules.dns.matchName` entry. The new
//     rule is appended (not prepended) so existing test golden orderings for
//     non-FQDN CNPs remain stable.
//
// The function is safe to call on a nil-Spec CNP (no-op).
func ensureKubeDNSCompanion(cnp *ciliumv2.CiliumNetworkPolicy) {
	if cnp == nil || cnp.Spec == nil {
		return
	}

	observed := collectFQDNMatchNames(cnp.Spec.Egress)
	if len(observed) == 0 {
		return
	}

	if hasKubeDNSCompanion(cnp.Spec.Egress) {
		return
	}

	dnsRules := make([]api.PortRuleDNS, len(observed))
	for i, n := range observed {
		dnsRules[i] = api.PortRuleDNS{MatchName: n}
	}

	cnp.Spec.Egress = append(cnp.Spec.Egress, api.EgressRule{
		EgressCommonRule: api.EgressCommonRule{
			ToEndpoints: []api.EndpointSelector{kubeDNSSelector()},
		},
		ToPorts: api.PortRules{{
			Ports: []api.PortProtocol{
				{Port: "53", Protocol: api.ProtoUDP},
				{Port: "53", Protocol: api.ProtoTCP},
			},
			Rules: &api.L7Rules{DNS: dnsRules},
		}},
	})
}

// collectFQDNMatchNames walks every egress rule's ToFQDNs slice and returns
// the unique matchName values in sorted order. Empty MatchPattern entries are
// ignored (DNS-03 ensures cpg never generates them upstream of this call, but
// the function is defensive).
func collectFQDNMatchNames(egress []api.EgressRule) []string {
	seen := make(map[string]struct{})
	for _, eg := range egress {
		for _, sel := range eg.ToFQDNs {
			if sel.MatchName == "" {
				continue
			}
			seen[sel.MatchName] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// hasKubeDNSCompanion reports whether the egress slice already carries a rule
// that selects kube-dns AND opens both 53/UDP and 53/TCP. This is the
// idempotency check called from ensureKubeDNSCompanion.
func hasKubeDNSCompanion(egress []api.EgressRule) bool {
	for _, eg := range egress {
		if !egressSelectsKubeDNS(eg) {
			continue
		}
		hasUDP, hasTCP := false, false
		for _, pr := range eg.ToPorts {
			for _, p := range pr.Ports {
				if p.Port != "53" {
					continue
				}
				switch p.Protocol {
				case api.ProtoUDP:
					hasUDP = true
				case api.ProtoTCP:
					hasTCP = true
				case api.ProtoAny:
					hasUDP = true
					hasTCP = true
				}
			}
		}
		if hasUDP && hasTCP {
			return true
		}
	}
	return false
}

// egressSelectsKubeDNS reports whether the egress rule's ToEndpoints carries
// the kube-dns selector pair. Tolerant of Cilium's "any:" / "k8s:" label
// source prefix that appears after YAML roundtrips.
func egressSelectsKubeDNS(eg api.EgressRule) bool {
	for _, ep := range eg.ToEndpoints {
		if ep.LabelSelector == nil {
			continue
		}
		hasApp, hasNS := false, false
		for k, v := range ep.LabelSelector.MatchLabels {
			bare := stripLabelSourcePrefix(k)
			if bare == kubeDNSSelectorAppKey && v == kubeDNSSelectorAppVal {
				hasApp = true
			}
			if bare == kubeDNSSelectorNSKey && v == kubeDNSSelectorNSVal {
				hasNS = true
			}
		}
		if hasApp && hasNS {
			return true
		}
	}
	return false
}

// stripLabelSourcePrefix trims the leading Cilium label-source prefix
// ("any:", "k8s:") from a label key for source-agnostic comparison.
func stripLabelSourcePrefix(k string) string {
	for _, prefix := range []string{"any:", "k8s:"} {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			return k[len(prefix):]
		}
	}
	return k
}

// kubeDNSSelector returns the EndpointSelector matching kube-dns pods in the
// kube-system namespace. The selector is built directly from a slim
// LabelSelector to avoid the GetCiliumKeyFrom corruption path that
// pkg/labels.BuildEndpointSelector documents.
func kubeDNSSelector() api.EndpointSelector {
	return api.EndpointSelector{
		LabelSelector: &slim_metav1.LabelSelector{
			MatchLabels: map[string]string{
				kubeDNSSelectorAppKey: kubeDNSSelectorAppVal,
				kubeDNSSelectorNSKey:  kubeDNSSelectorNSVal,
			},
		},
	}
}
