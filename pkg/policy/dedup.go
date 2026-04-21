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
		}
	}
	// Sort ports within each egress rule
	for i := range r.Egress {
		for j := range r.Egress[i].ToPorts {
			sortPorts(r.Egress[i].ToPorts[j].Ports)
		}
	}

	// Sort ingress rules by key
	sort.Slice(r.Ingress, func(i, j int) bool {
		return ingressRuleKey(r.Ingress[i]) < ingressRuleKey(r.Ingress[j])
	})

	// Sort egress rules by key
	sort.Slice(r.Egress, func(i, j int) bool {
		return egressRuleKey(r.Egress[i]) < egressRuleKey(r.Egress[j])
	})
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
	sort.Strings(parts)
	return fmt.Sprintf("%v", parts)
}
