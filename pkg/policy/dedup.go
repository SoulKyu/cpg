package policy

import (
	"fmt"
	"sort"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	"sigs.k8s.io/yaml"
)

// PoliciesEquivalent compares two CiliumNetworkPolicies by their Spec only
// (ignoring metadata). Returns (equivalent, error). Rules are normalized
// (sorted) before comparison. reflect.DeepEqual is unreliable here because
// Cilium's EndpointSelector has unexported fields that differ between freshly
// built selectors and YAML-roundtripped ones; YAML serialization normalizes
// label keys through Cilium's MarshalJSON for consistent output.
func PoliciesEquivalent(a, b *ciliumv2.CiliumNetworkPolicy) (bool, error) {
	if a == nil && b == nil {
		return true, nil
	}
	if a == nil || b == nil {
		return false, nil
	}
	if a.Spec == nil && b.Spec == nil {
		return true, nil
	}
	if a.Spec == nil || b.Spec == nil {
		return false, nil
	}

	aCopy := a.Spec.DeepCopy()
	bCopy := b.Spec.DeepCopy()
	normalizeRule(aCopy)
	normalizeRule(bCopy)

	aData, err := yaml.Marshal(aCopy)
	if err != nil {
		return false, fmt.Errorf("marshal policy a: %w", err)
	}
	bData, err := yaml.Marshal(bCopy)
	if err != nil {
		return false, fmt.Errorf("marshal policy b: %w", err)
	}
	return string(aData) == string(bData), nil
}

// normalizeRule sorts ingress/egress rules and their ports for deterministic comparison.
func normalizeRule(r *api.Rule) {
	// Sort ports within each ingress rule
	for i := range r.Ingress {
		for j := range r.Ingress[i].ToPorts {
			sortPorts(r.Ingress[i].ToPorts[j].Ports)
			sortL7Rules(r.Ingress[i].ToPorts[j].Rules)
		}
	}
	// Sort ports within each egress rule
	for i := range r.Egress {
		for j := range r.Egress[i].ToPorts {
			sortPorts(r.Egress[i].ToPorts[j].Ports)
			sortL7Rules(r.Egress[i].ToPorts[j].Rules)
		}
	}

	// Sort ingress rules by key. SliceStable keeps input order for any pair
	// whose keys still tie (genuinely identical rules serialize the same, so
	// their relative order is irrelevant to the byte comparison).
	sort.SliceStable(r.Ingress, func(i, j int) bool {
		return ingressRuleKey(r.Ingress[i]) < ingressRuleKey(r.Ingress[j])
	})

	// Sort egress rules by key.
	sort.SliceStable(r.Egress, func(i, j int) bool {
		return egressRuleKey(r.Egress[i]) < egressRuleKey(r.Egress[j])
	})
}

// ruleContentParts returns discriminator strings for a rule's ToPorts/ICMPs so
// two rules targeting the SAME peer but differing in type (ports vs ICMP), in
// their port/proto set, or in their L7 payload get DISTINCT sort keys. Without
// these, a same-peer [ports, icmp] pair collides on one key and normalizeRule's
// sort leaves them in input order, making PoliciesEquivalent order-dependent.
func ruleContentParts(ports api.PortRules, icmps api.ICMPRules) []string {
	var parts []string
	for _, pr := range ports {
		for _, p := range pr.Ports {
			parts = append(parts, "port:"+p.Port+"/"+string(p.Protocol))
		}
		if pr.Rules != nil {
			for _, h := range pr.Rules.HTTP {
				parts = append(parts, "http:"+httpRuleKey(h))
			}
			for _, d := range pr.Rules.DNS {
				parts = append(parts, "dns:"+dnsRuleKey(d))
			}
		}
	}
	for _, ir := range icmps {
		for _, f := range ir.Fields {
			parts = append(parts, "icmp:"+icmpFieldKey(f))
		}
	}
	return parts
}

// sortL7Rules deterministically sorts L7 sub-lists on a PortRule so YAML
// output stays byte-stable across runs (EVID2-04). HTTP entries sort by
// (Method, Path) lexicographic; DNS entries sort by MatchName lexicographic
// (MatchPattern is not auto-generated in v1.2 — DNS-03). Empty/nil Rules is
// a no-op; the nil-vs-empty-list distinction is preserved.
func sortL7Rules(rules *api.L7Rules) {
	if rules == nil {
		return
	}
	if len(rules.HTTP) > 1 {
		sort.SliceStable(rules.HTTP, func(i, j int) bool {
			if rules.HTTP[i].Method != rules.HTTP[j].Method {
				return rules.HTTP[i].Method < rules.HTTP[j].Method
			}
			return rules.HTTP[i].Path < rules.HTTP[j].Path
		})
	}
	if len(rules.DNS) > 1 {
		sort.SliceStable(rules.DNS, func(i, j int) bool {
			return rules.DNS[i].MatchName < rules.DNS[j].MatchName
		})
	}
}

// sortPorts sorts port/protocol pairs for deterministic comparison.
func sortPorts(ports []api.PortProtocol) {
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Port != ports[j].Port {
			return ports[i].Port < ports[j].Port
		}
		return ports[i].Protocol < ports[j].Protocol
	})
}

// ingressRuleKey generates a deterministic string key for an ingress rule.
func ingressRuleKey(r api.IngressRule) string {
	var parts []string
	for _, ep := range r.FromEndpoints {
		if ep.LabelSelector != nil {
			keys := make([]string, 0, len(ep.LabelSelector.MatchLabels))
			for k, v := range ep.LabelSelector.MatchLabels {
				keys = append(keys, k+"="+v)
			}
			sort.Strings(keys)
			parts = append(parts, fmt.Sprintf("ep:%v", keys))
		}
	}
	for _, cidr := range r.FromCIDR {
		parts = append(parts, "cidr:"+string(cidr))
	}
	for _, entity := range r.FromEntities {
		parts = append(parts, "entity:"+string(entity))
	}
	parts = append(parts, ruleContentParts(r.ToPorts, r.ICMPs)...)
	sort.Strings(parts)
	return fmt.Sprintf("%v", parts)
}

// egressRuleKey generates a deterministic string key for an egress rule.
func egressRuleKey(r api.EgressRule) string {
	var parts []string
	for _, ep := range r.ToEndpoints {
		if ep.LabelSelector != nil {
			keys := make([]string, 0, len(ep.LabelSelector.MatchLabels))
			for k, v := range ep.LabelSelector.MatchLabels {
				keys = append(keys, k+"="+v)
			}
			sort.Strings(keys)
			parts = append(parts, fmt.Sprintf("ep:%v", keys))
		}
	}
	for _, cidr := range r.ToCIDR {
		parts = append(parts, "cidr:"+string(cidr))
	}
	for _, entity := range r.ToEntities {
		parts = append(parts, "entity:"+string(entity))
	}
	for _, fqdn := range r.ToFQDNs {
		parts = append(parts, "fqdn:"+fqdn.MatchName+"|"+fqdn.MatchPattern)
	}
	parts = append(parts, ruleContentParts(r.ToPorts, r.ICMPs)...)
	sort.Strings(parts)
	return fmt.Sprintf("%v", parts)
}
