package output

import (
	"fmt"
	"sort"
	"strings"

	slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	"github.com/cilium/cilium/pkg/policy/api"
)

// annotateRules injects human-readable comments before each ingress/egress
// rule in the serialized YAML, describing which traffic pattern it allows.
func annotateRules(yamlData []byte, spec *api.Rule) []byte {
	if spec == nil {
		return yamlData
	}

	ingressDescs := make([]string, len(spec.Ingress))
	for i, r := range spec.Ingress {
		ingressDescs[i] = describeIngressRule(r)
	}
	egressDescs := make([]string, len(spec.Egress))
	for i, r := range spec.Egress {
		egressDescs[i] = describeEgressRule(r)
	}

	lines := strings.Split(string(yamlData), "\n")
	var result []string
	section := ""
	ruleIdx := 0

	for _, line := range lines {
		// Detect section boundaries at spec level (2-space indent)
		if line == "  ingress:" {
			section = "ingress"
			ruleIdx = 0
		} else if line == "  egress:" {
			section = "egress"
			ruleIdx = 0
		} else if len(line) > 2 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' && line[2] != '-' {
			// Another spec-level key (endpointSelector:, etc.)
			section = ""
		}

		// Inject comment before each rule array item (2-space indent + "- ")
		if section != "" && strings.HasPrefix(line, "  - ") {
			var desc string
			if section == "ingress" && ruleIdx < len(ingressDescs) {
				desc = ingressDescs[ruleIdx]
			} else if section == "egress" && ruleIdx < len(egressDescs) {
				desc = egressDescs[ruleIdx]
			}
			if desc != "" {
				result = append(result, "  # "+desc)
			}
			ruleIdx++
		}

		result = append(result, line)
	}

	return []byte(strings.Join(result, "\n"))
}

func describeIngressRule(r api.IngressRule) string {
	peer := describePeer(r.FromEndpoints, r.FromEntities, r.FromCIDR, "from")
	traffic := describeTraffic(r.ToPorts, r.ICMPs)
	return fmt.Sprintf("%s %s", traffic, peer)
}

func describeEgressRule(r api.EgressRule) string {
	peer := describePeer(r.ToEndpoints, r.ToEntities, r.ToCIDR, "to")
	traffic := describeTraffic(r.ToPorts, r.ICMPs)
	return fmt.Sprintf("%s %s", traffic, peer)
}

func describePeer(endpoints []api.EndpointSelector, entities api.EntitySlice, cidrs api.CIDRSlice, direction string) string {
	if len(entities) > 0 {
		names := make([]string, len(entities))
		for i, e := range entities {
			names[i] = string(e)
		}
		return fmt.Sprintf("%s entity %s", direction, strings.Join(names, ", "))
	}
	if len(cidrs) > 0 {
		cidrStrs := make([]string, len(cidrs))
		for i, c := range cidrs {
			cidrStrs[i] = string(c)
		}
		return fmt.Sprintf("%s CIDR %s", direction, strings.Join(cidrStrs, ", "))
	}
	if len(endpoints) > 0 {
		descs := make([]string, 0, len(endpoints))
		for _, ep := range endpoints {
			if d := describeSelector(ep); d != "" {
				descs = append(descs, d)
			}
		}
		if len(descs) > 0 {
			return fmt.Sprintf("%s %s", direction, strings.Join(descs, " or "))
		}
	}
	return direction + " any"
}

// describeSelector renders a single endpoint selector, folding both its
// MatchLabels and its MatchExpressions into one comma-separated clause. It
// returns "" only for a truly empty selector (which matches any endpoint).
func describeSelector(ep api.EndpointSelector) string {
	if ep.LabelSelector == nil {
		return ""
	}
	parts := make([]string, 0, len(ep.MatchLabels)+len(ep.MatchExpressions))
	for k, v := range ep.MatchLabels {
		parts = append(parts, k+"="+v)
	}
	for _, expr := range ep.MatchExpressions {
		parts = append(parts, describeMatchExpression(expr))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func describeMatchExpression(expr slim_metav1.LabelSelectorRequirement) string {
	switch expr.Operator {
	case slim_metav1.LabelSelectorOpExists:
		return expr.Key + " exists"
	case slim_metav1.LabelSelectorOpDoesNotExist:
		return expr.Key + " does not exist"
	case slim_metav1.LabelSelectorOpIn:
		return fmt.Sprintf("%s in (%s)", expr.Key, strings.Join(expr.Values, ", "))
	case slim_metav1.LabelSelectorOpNotIn:
		return fmt.Sprintf("%s not in (%s)", expr.Key, strings.Join(expr.Values, ", "))
	default:
		return fmt.Sprintf("%s %s %s", expr.Key, expr.Operator, strings.Join(expr.Values, ", "))
	}
}

func describeTraffic(ports api.PortRules, icmps api.ICMPRules) string {
	var parts []string
	for _, pr := range ports {
		for _, p := range pr.Ports {
			parts = append(parts, fmt.Sprintf("%s/%s", p.Protocol, p.Port))
		}
	}
	for _, ir := range icmps {
		for _, f := range ir.Fields {
			if f.Type != nil {
				parts = append(parts, fmt.Sprintf("%s(type=%s)", f.Family, f.Type.String()))
			} else {
				parts = append(parts, f.Family)
			}
		}
	}
	if len(parts) == 0 {
		return "all traffic"
	}
	return strings.Join(parts, ", ")
}
