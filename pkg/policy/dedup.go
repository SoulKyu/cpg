package policy

import (
	"fmt"
	"reflect"
	"sort"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
)

// PoliciesEquivalent compares two CiliumNetworkPolicies by their Spec only
// (ignoring metadata). Rules are normalized (sorted) before comparison so that
// ordering differences do not cause false negatives.
func PoliciesEquivalent(a, b *ciliumv2.CiliumNetworkPolicy) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Spec == nil && b.Spec == nil {
		return true
	}
	if a.Spec == nil || b.Spec == nil {
		return false
	}

	// DeepCopy to avoid mutating originals
	aCopy := a.Spec.DeepCopy()
	bCopy := b.Spec.DeepCopy()

	// Normalize: sort rules and ports
	normalizeRule(aCopy)
	normalizeRule(bCopy)

	// Compare EndpointSelector + Ingress + Egress
	if !reflect.DeepEqual(aCopy.EndpointSelector, bCopy.EndpointSelector) {
		return false
	}
	if !reflect.DeepEqual(aCopy.Ingress, bCopy.Ingress) {
		return false
	}
	if !reflect.DeepEqual(aCopy.Egress, bCopy.Egress) {
		return false
	}
	return true
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
