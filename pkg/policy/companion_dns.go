// Package policy — DNS companion rule injector.
//
// This file implements the DNS-02 invariant: every CiliumNetworkPolicy that
// carries `toFQDNs` MUST also carry a companion egress rule allowing UDP+TCP
// on port 53 to the kube-dns workload. Without that companion rule, ToFQDNs
// is unusable in practice — the resolver IP would be denied by the implicit
// default-deny semantics that ToFQDNs introduces (see Cilium docs: "An
// explicit rule to allow for DNS traffic is needed for the pods").
//
// DNS-FUT-02: the companion selects the pods behind the kube-system DNS
// Service, detected live by pkg/k8s.DetectDNSSelector and passed in through
// AttributionOptions.DNSSelector. Without a detected selector (cpg replay,
// --server bypass, RBAC denied) it falls back to k8s-app In [kube-dns,
// coredns]: kube-dns is the kubeadm/EKS/GKE/AKS/k3s label, coredns the
// upstream CoreDNS Helm chart default (e.g. Outscale OKS).
//
// DNS-03 invariant: the companion rule's `rules.dns` block lists each
// observed FQDN as a literal matchName entry — never a matchPattern. This
// keeps the "no glob auto-generation" promise intact even for the companion.
package policy

import (
	"maps"
	"slices"
	"sort"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	"github.com/cilium/cilium/pkg/policy/api"
)

const (
	kubeDNSSelectorAppKey = "k8s-app"
	kubeDNSSelectorNSKey  = "io.kubernetes.pod.namespace"
	kubeDNSSelectorNSVal  = "kube-system"
)

// fallbackDNSAppValues are the k8s-app values the fallback companion selects.
var fallbackDNSAppValues = []string{"kube-dns", "coredns"}

// ensureKubeDNSCompanion enforces the DNS-02 invariant on the supplied CNP.
//
// Behavior:
//  1. Walk cnp.Spec.Egress and collect every FQDN matchName referenced via
//     ToFQDNs. If none are present, return without modification (no FQDN →
//     no companion needed).
//  2. If any existing egress rule already covers the DNS pods on 53/UDP+TCP,
//     return without modification (idempotent — repeated calls never
//     duplicate the companion).
//  3. Otherwise APPEND a fresh egress rule selecting the DNS pods
//     (dnsSelector, or the fallback when nil) and listing
//     each observed FQDN as a literal `rules.dns.matchName` entry. The new
//     rule is appended (not prepended) so existing test golden orderings for
//     non-FQDN CNPs remain stable.
//
// The function is safe to call on a nil-Spec CNP (no-op).
func ensureKubeDNSCompanion(cnp *ciliumv2.CiliumNetworkPolicy, dnsSelector map[string]string) {
	if cnp == nil || cnp.Spec == nil {
		return
	}

	observed := collectFQDNMatchNames(cnp.Spec.Egress)
	if len(observed) == 0 {
		return
	}

	if hasKubeDNSCompanion(cnp.Spec.Egress, dnsSelector) {
		return
	}

	dnsRules := make([]api.PortRuleDNS, len(observed))
	for i, n := range observed {
		dnsRules[i] = api.PortRuleDNS{MatchName: n}
	}

	cnp.Spec.Egress = append(cnp.Spec.Egress, api.EgressRule{
		EgressCommonRule: api.EgressCommonRule{
			ToEndpoints: []api.EndpointSelector{kubeDNSSelector(dnsSelector)},
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
// that selects the DNS pods AND opens both 53/UDP and 53/TCP. This is the
// idempotency check called from ensureKubeDNSCompanion.
func hasKubeDNSCompanion(egress []api.EgressRule, dnsSelector map[string]string) bool {
	for _, eg := range egress {
		if !egressSelectsKubeDNS(eg, dnsSelector) {
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

// egressSelectsKubeDNS reports whether the egress rule's ToEndpoints select
// the kube-system DNS pods: via the detected selector, the fallback
// k8s-app In expression, or a k8s-app=kube-dns|coredns matchLabels entry
// (legacy cpg output, hand edits). Tolerant of Cilium's "any:" / "k8s:" label
// source prefix that appears after YAML roundtrips.
func egressSelectsKubeDNS(eg api.EgressRule, dnsSelector map[string]string) bool {
	for _, ep := range eg.ToEndpoints {
		if ep.LabelSelector != nil && selectsKubeDNS(ep.LabelSelector, dnsSelector) {
			return true
		}
	}
	return false
}

func selectsKubeDNS(ls *slim_metav1.LabelSelector, dnsSelector map[string]string) bool {
	ml := make(map[string]string, len(ls.MatchLabels))
	for k, v := range ls.MatchLabels {
		ml[stripLabelSourcePrefix(k)] = v
	}
	if ml[kubeDNSSelectorNSKey] != kubeDNSSelectorNSVal {
		return false
	}
	if len(dnsSelector) > 0 && containsLabels(ml, dnsSelector) {
		return true
	}
	if slices.Contains(fallbackDNSAppValues, ml[kubeDNSSelectorAppKey]) {
		return true
	}
	for _, req := range ls.MatchExpressions {
		if stripLabelSourcePrefix(req.Key) == kubeDNSSelectorAppKey &&
			req.Operator == slim_metav1.LabelSelectorOpIn &&
			slices.ContainsFunc(req.Values, func(v string) bool { return slices.Contains(fallbackDNSAppValues, v) }) {
			return true
		}
	}
	return false
}

func containsLabels(have, want map[string]string) bool {
	for k, v := range want {
		if got, ok := have[k]; !ok || got != v {
			return false
		}
	}
	return true
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

// kubeDNSSelector returns the EndpointSelector matching the DNS pods in
// kube-system: the detected dnsSelector when set, else the fallback k8s-app
// In expression. The selector is built directly from a slim LabelSelector to
// avoid the GetCiliumKeyFrom corruption path that
// pkg/labels.BuildEndpointSelector documents.
func kubeDNSSelector(dnsSelector map[string]string) api.EndpointSelector {
	ls := &slim_metav1.LabelSelector{MatchLabels: map[string]string{}}
	if len(dnsSelector) > 0 {
		maps.Copy(ls.MatchLabels, dnsSelector)
	} else {
		ls.MatchExpressions = []slim_metav1.LabelSelectorRequirement{{
			Key:      kubeDNSSelectorAppKey,
			Operator: slim_metav1.LabelSelectorOpIn,
			Values:   slices.Clone(fallbackDNSAppValues),
		}}
	}
	ls.MatchLabels[kubeDNSSelectorNSKey] = kubeDNSSelectorNSVal
	return api.EndpointSelector{LabelSelector: ls}
}
