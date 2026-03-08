package policy

import (
	"reflect"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
)

// MergePolicy merges incoming policy rules into an existing policy.
// For each incoming rule, if a matching peer (same FromEndpoints/ToEndpoints)
// exists in the existing policy, new ports are added to that rule.
// If no matching peer exists, the rule is appended as a new entry.
// ObjectMeta is preserved from the existing policy.
func MergePolicy(existing, incoming *ciliumv2.CiliumNetworkPolicy) *ciliumv2.CiliumNetworkPolicy {
	result := existing.DeepCopy()

	if incoming.Spec == nil {
		return result
	}

	// Merge ingress rules
	for _, inRule := range incoming.Spec.Ingress {
		merged := false
		for i, exRule := range result.Spec.Ingress {
			if matchEndpoints(exRule.FromEndpoints, inRule.FromEndpoints) {
				result.Spec.Ingress[i].ToPorts = mergePortRules(exRule.ToPorts, inRule.ToPorts)
				merged = true
				break
			}
		}
		if !merged {
			result.Spec.Ingress = append(result.Spec.Ingress, inRule)
		}
	}

	// Merge egress rules
	for _, inRule := range incoming.Spec.Egress {
		merged := false
		for i, exRule := range result.Spec.Egress {
			if matchEndpoints(exRule.ToEndpoints, inRule.ToEndpoints) {
				result.Spec.Egress[i].ToPorts = mergePortRules(exRule.ToPorts, inRule.ToPorts)
				merged = true
				break
			}
		}
		if !merged {
			result.Spec.Egress = append(result.Spec.Egress, inRule)
		}
	}

	return result
}

// matchEndpoints compares two slices of EndpointSelector by their matchLabels.
func matchEndpoints(a, b []api.EndpointSelector) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		var aLabels, bLabels map[string]string
		if a[i].LabelSelector != nil {
			aLabels = a[i].LabelSelector.MatchLabels
		}
		if b[i].LabelSelector != nil {
			bLabels = b[i].LabelSelector.MatchLabels
		}
		if !reflect.DeepEqual(aLabels, bLabels) {
			return false
		}
	}
	return true
}

// mergePortRules merges incoming port rules into existing ones, deduplicating
// by port number and protocol.
func mergePortRules(existing, incoming api.PortRules) api.PortRules {
	if len(existing) == 0 {
		return incoming
	}
	if len(incoming) == 0 {
		return existing
	}

	// Collect all existing ports into the first PortRule
	result := make(api.PortRules, 1)
	result[0].Ports = append(result[0].Ports, existing[0].Ports...)

	// Build dedup set from existing
	seen := make(map[string]struct{})
	for _, p := range existing[0].Ports {
		seen[p.Port+"/"+string(p.Protocol)] = struct{}{}
	}

	// Add incoming ports that are not duplicates
	for _, pr := range incoming {
		for _, p := range pr.Ports {
			key := p.Port + "/" + string(p.Protocol)
			if _, dup := seen[key]; !dup {
				result[0].Ports = append(result[0].Ports, p)
				seen[key] = struct{}{}
			}
		}
	}

	return result
}
