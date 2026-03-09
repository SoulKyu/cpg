package policy

import (
	"reflect"
	"strings"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/labels"
	"github.com/cilium/cilium/pkg/policy/api"
)

// MergePolicy merges incoming policy rules into an existing policy.
// For each incoming rule, if a matching peer (same endpoints/entities/CIDR)
// and same rule type (ports vs ICMP) exists, contents are merged.
// If no match exists, the rule is appended as a new entry.
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
			if ingressRulesMatch(exRule, inRule) {
				result.Spec.Ingress[i].ToPorts = mergePortRules(exRule.ToPorts, inRule.ToPorts)
				result.Spec.Ingress[i].ICMPs = mergeICMPRules(exRule.ICMPs, inRule.ICMPs)
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
			if egressRulesMatch(exRule, inRule) {
				result.Spec.Egress[i].ToPorts = mergePortRules(exRule.ToPorts, inRule.ToPorts)
				result.Spec.Egress[i].ICMPs = mergeICMPRules(exRule.ICMPs, inRule.ICMPs)
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

// ingressRulesMatch returns true if two ingress rules target the same peer
// (endpoints, entities, or CIDR) and the same rule type (ports vs ICMP).
func ingressRulesMatch(a, b api.IngressRule) bool {
	if !matchEndpoints(a.FromEndpoints, b.FromEndpoints) {
		return false
	}
	if !matchEntities(a.FromEntities, b.FromEntities) {
		return false
	}
	if !matchCIDRSlice(a.FromCIDR, b.FromCIDR) {
		return false
	}
	return isICMPRule(a) == isICMPRule(b)
}

// egressRulesMatch returns true if two egress rules target the same peer
// (endpoints, entities, or CIDR) and the same rule type (ports vs ICMP).
func egressRulesMatch(a, b api.EgressRule) bool {
	if !matchEndpoints(a.ToEndpoints, b.ToEndpoints) {
		return false
	}
	if !matchEntities(a.ToEntities, b.ToEntities) {
		return false
	}
	if !matchCIDRSlice(a.ToCIDR, b.ToCIDR) {
		return false
	}
	return isICMPRule(a) == isICMPRule(b)
}

// isICMPRule returns true if the rule carries ICMP fields rather than port specs.
func isICMPRule(r interface{}) bool {
	switch v := r.(type) {
	case api.IngressRule:
		return len(v.ICMPs) > 0
	case api.EgressRule:
		return len(v.ICMPs) > 0
	default:
		return false
	}
}

// matchEntities compares two EntitySlice values for equality.
func matchEntities(a, b api.EntitySlice) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// matchCIDRSlice compares two CIDRSlice values for equality.
func matchCIDRSlice(a, b api.CIDRSlice) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// matchEndpoints compares two slices of EndpointSelector by their matchLabels.
// Handles the "any:" prefix that Cilium adds during YAML roundtrip by normalizing
// keys before comparison.
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
		if !matchLabelsNormalized(aLabels, bLabels) {
			return false
		}
	}
	return true
}

// matchLabelsNormalized compares two label maps after normalizing label keys
// through Cilium's GetCiliumKeyFrom. This handles the "any:" prefix that Cilium
// adds to plain keys AND the dot-to-colon transformation for keys like
// "io.kubernetes.pod.namespace" → "io:kubernetes.pod.namespace" that occurs
// during EndpointSelector YAML serialization/deserialization roundtrips.
func matchLabelsNormalized(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	normA := normalizeLabels(a)
	normB := normalizeLabels(b)
	return reflect.DeepEqual(normA, normB)
}

// normalizeLabels converts all label keys to Cilium's canonical serialized
// format ("source:key"). Keys from BuildPolicy have no source prefix (e.g.,
// "app", "io.kubernetes.pod.namespace"), while keys from YAML roundtrip have
// the Cilium serialized form (e.g., "any:app", "io:kubernetes.pod.namespace").
// GetCiliumKeyFrom handles the conversion for plain keys, but must NOT be
// applied to already-serialized keys (it would double-prefix them).
func normalizeLabels(lbls map[string]string) map[string]string {
	result := make(map[string]string, len(lbls))
	for k, v := range lbls {
		if strings.IndexByte(k, ':') < 0 {
			// Plain key (no source prefix) → convert to serialized form
			k = labels.GetCiliumKeyFrom(k)
		}
		result[k] = v
	}
	return result
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

// mergeICMPRules merges incoming ICMP rules into existing ones, deduplicating
// by family and type.
func mergeICMPRules(existing, incoming api.ICMPRules) api.ICMPRules {
	if len(existing) == 0 {
		return incoming
	}
	if len(incoming) == 0 {
		return existing
	}

	result := make(api.ICMPRules, 1)
	result[0].Fields = append(result[0].Fields, existing[0].Fields...)

	seen := make(map[string]struct{})
	for _, f := range existing[0].Fields {
		seen[icmpFieldKey(f)] = struct{}{}
	}

	for _, ir := range incoming {
		for _, f := range ir.Fields {
			key := icmpFieldKey(f)
			if _, dup := seen[key]; !dup {
				result[0].Fields = append(result[0].Fields, f)
				seen[key] = struct{}{}
			}
		}
	}

	return result
}

// icmpFieldKey returns a dedup key for an ICMPField based on family and type.
func icmpFieldKey(f api.ICMPField) string {
	typeStr := "*"
	if f.Type != nil {
		typeStr = f.Type.String()
	}
	return f.Family + "/" + typeStr
}
