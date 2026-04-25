package policy

import (
	"reflect"
	"sort"
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
	return ingressIsICMP(a) == ingressIsICMP(b)
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
	return egressIsICMP(a) == egressIsICMP(b)
}

func ingressIsICMP(r api.IngressRule) bool { return len(r.ICMPs) > 0 }
func egressIsICMP(r api.EgressRule) bool   { return len(r.ICMPs) > 0 }

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
// by port number and protocol AND preserving any L7 Rules attached to a
// PortRule.
//
// Rules of engagement (EVID2-03):
//   - L4-only fast path (no entry on either side carries Rules): collapse
//     into a single PortRule with merged Ports list — byte-stable with v1.1.
//   - Any entry carrying Rules: group by (port, protocol) so the L7 rules of
//     two distinct ports do not collide; union-merge HTTP and DNS sub-lists
//     by canonical key when both sides populate the same (port, protocol)
//     bucket.
//
// NOTE: Cilium's L7Rules struct is OneOf-shaped — HTTP and DNS on the same
// port/protocol are structurally illegal in CNP (see research/PITFALLS, item
// 6). Phase 7 does not special-case mixed L7 protocols; Phase 8/9 will add
// an explicit refusal-with-warn at the build layer when both arrive on the
// same bucket.
func mergePortRules(existing, incoming api.PortRules) api.PortRules {
	if len(existing) == 0 {
		return incoming
	}
	if len(incoming) == 0 {
		return existing
	}

	// Fast path: no Rules on either side → preserve v1.1 collapse-into-one
	// behavior so existing fixtures stay byte-identical.
	if !portRulesCarryL7(existing) && !portRulesCarryL7(incoming) {
		result := make(api.PortRules, 1)
		seen := make(map[string]struct{})
		appendIfNew := func(p api.PortProtocol) {
			key := p.Port + "/" + string(p.Protocol)
			if _, dup := seen[key]; dup {
				return
			}
			seen[key] = struct{}{}
			result[0].Ports = append(result[0].Ports, p)
		}
		for _, pr := range existing {
			for _, p := range pr.Ports {
				appendIfNew(p)
			}
		}
		for _, pr := range incoming {
			for _, p := range pr.Ports {
				appendIfNew(p)
			}
		}
		return result
	}

	// L7 path: group PortRules by (port, protocol). Each (port, proto)
	// produces one merged PortRule whose Rules union-merges HTTP and DNS
	// sub-lists. First-observation order is preserved.
	type bucket struct {
		port  api.PortProtocol
		rules *api.L7Rules
	}
	buckets := make(map[string]*bucket)
	var order []string

	consume := func(prs api.PortRules) {
		for _, pr := range prs {
			for _, p := range pr.Ports {
				key := p.Port + "/" + string(p.Protocol)
				b, ok := buckets[key]
				if !ok {
					b = &bucket{port: p}
					buckets[key] = b
					order = append(order, key)
				}
				if pr.Rules != nil {
					b.rules = mergeL7Rules(b.rules, pr.Rules)
				}
			}
		}
	}
	consume(existing)
	consume(incoming)

	result := make(api.PortRules, 0, len(order))
	for _, key := range order {
		b := buckets[key]
		result = append(result, api.PortRule{
			Ports: []api.PortProtocol{b.port},
			Rules: b.rules,
		})
	}
	return result
}

// portRulesCarryL7 reports whether any PortRule in the slice carries an L7
// Rules pointer.
func portRulesCarryL7(prs api.PortRules) bool {
	for _, pr := range prs {
		if pr.Rules != nil {
			return true
		}
	}
	return false
}

// mergeL7Rules union-merges two L7Rules pointers. Returns nil only when both
// inputs are nil. HTTP entries dedup by (Method, Path, Host, Headers,
// HeaderMatches); DNS entries dedup by (MatchName, MatchPattern). First
// observation wins on ordering — new entries append.
func mergeL7Rules(a, b *api.L7Rules) *api.L7Rules {
	if a == nil && b == nil {
		return nil
	}
	if a == nil {
		// Return a copy so the caller does not share a pointer with the
		// incoming slice (defensive: protects future mutations in this pkg).
		out := *b
		return &out
	}
	if b == nil {
		return a
	}

	out := *a
	// HTTP union-merge.
	if len(b.HTTP) > 0 {
		seen := make(map[string]struct{}, len(out.HTTP)+len(b.HTTP))
		for _, h := range out.HTTP {
			seen[httpRuleKey(h)] = struct{}{}
		}
		for _, h := range b.HTTP {
			k := httpRuleKey(h)
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			out.HTTP = append(out.HTTP, h)
		}
	}
	// DNS union-merge.
	if len(b.DNS) > 0 {
		seen := make(map[string]struct{}, len(out.DNS)+len(b.DNS))
		for _, d := range out.DNS {
			seen[dnsRuleKey(d)] = struct{}{}
		}
		for _, d := range b.DNS {
			k := dnsRuleKey(d)
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			out.DNS = append(out.DNS, d)
		}
	}
	// Preserve L7Proto / L7 / Kafka from the existing side; Phase 8/9 will
	// extend this if/when we generate them.
	return &out
}

// httpRuleKey returns a canonical dedup key for an HTTP PortRule.
// Headers / HeaderMatches are NOT generated by cpg (HTTP-05 in REQUIREMENTS),
// but we include them in the key for completeness so a hand-edited or
// future-generated rule with header constraints does not accidentally collapse
// into a header-less observation.
func httpRuleKey(h api.PortRuleHTTP) string {
	hdrs := make([]string, len(h.Headers))
	copy(hdrs, h.Headers)
	sort.Strings(hdrs)
	hms := make([]string, 0, len(h.HeaderMatches))
	for _, hm := range h.HeaderMatches {
		if hm == nil {
			continue
		}
		hms = append(hms, string(hm.Mismatch)+"|"+hm.Name+"|"+hm.Value)
	}
	sort.Strings(hms)
	return h.Method + "|" + h.Path + "|" + h.Host + "|" +
		strings.Join(hdrs, ",") + "|" + strings.Join(hms, ",")
}

// dnsRuleKey returns a canonical dedup key for a DNS PortRule (FQDNSelector).
func dnsRuleKey(d api.PortRuleDNS) string {
	return d.MatchName + "|" + d.MatchPattern
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
