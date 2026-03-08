package policy

import (
	"sort"
	"strconv"
	"strings"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gule/cpg/pkg/labels"
)

// ReservedWorldIdentity is the Cilium reserved identity for external/world traffic.
const ReservedWorldIdentity uint32 = 2

// isWorldIdentity returns true if the endpoint represents external (world) traffic.
func isWorldIdentity(ep *flowpb.Endpoint) bool {
	if ep == nil {
		return false
	}
	if ep.Identity == ReservedWorldIdentity {
		return true
	}
	for _, l := range ep.Labels {
		if l == "reserved:world" {
			return true
		}
	}
	return false
}

// getSourceIP extracts the source IP from a flow's IP layer (nil-safe).
func getSourceIP(f *flowpb.Flow) string {
	if f.IP == nil {
		return ""
	}
	return f.IP.Source
}

// getDestinationIP extracts the destination IP from a flow's IP layer (nil-safe).
func getDestinationIP(f *flowpb.Flow) string {
	if f.IP == nil {
		return ""
	}
	return f.IP.Destination
}

// PolicyEvent wraps a generated CiliumNetworkPolicy with its target location.
type PolicyEvent struct {
	Namespace string
	Workload  string
	Policy    *ciliumv2.CiliumNetworkPolicy
}

// BuildPolicy transforms a set of Hubble dropped flows into a CiliumNetworkPolicy.
// For INGRESS flows: endpointSelector = destination, IngressRule with FromEndpoints = source.
// For EGRESS flows: endpointSelector = source, EgressRule with ToEndpoints = destination.
func BuildPolicy(namespace, workload string, flows []*flowpb.Flow) *ciliumv2.CiliumNetworkPolicy {
	cnp := &ciliumv2.CiliumNetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cilium.io/v2",
			Kind:       "CiliumNetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cpg-" + workload,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "cpg",
			},
		},
		Spec: &api.Rule{},
	}

	// Group flows by direction
	var ingressFlows, egressFlows []*flowpb.Flow
	for _, f := range flows {
		if f.L4 == nil {
			continue
		}
		switch f.TrafficDirection {
		case flowpb.TrafficDirection_INGRESS:
			ingressFlows = append(ingressFlows, f)
		case flowpb.TrafficDirection_EGRESS:
			egressFlows = append(egressFlows, f)
		}
	}

	// Set endpointSelector from the first flow that has labels
	if len(flows) > 0 {
		var selectorLabels []string
		for _, f := range flows {
			if f.TrafficDirection == flowpb.TrafficDirection_INGRESS && f.Destination != nil {
				selectorLabels = f.Destination.Labels
				break
			}
			if f.TrafficDirection == flowpb.TrafficDirection_EGRESS && f.Source != nil {
				selectorLabels = f.Source.Labels
				break
			}
		}
		if selectorLabels != nil {
			cnp.Spec.EndpointSelector = labels.BuildEndpointSelector(selectorLabels)
		}
	}

	// Build ingress rules: group by peer (source labels)
	cnp.Spec.Ingress = buildIngressRules(ingressFlows, namespace)

	// Build egress rules: group by peer (destination labels)
	cnp.Spec.Egress = buildEgressRules(egressFlows, namespace)

	return cnp
}

// peerKey creates a deterministic string key from peer labels for grouping.
func peerKey(peerLabels []string) string {
	selected := labels.SelectLabels(peerLabels)
	keys := make([]string, 0, len(selected))
	for k := range selected {
		keys = append(keys, k+"="+selected[k])
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// extractPort extracts port number and protocol from a flow's L4 layer.
// Returns empty strings if L4 is nil or has no TCP/UDP.
func extractPort(f *flowpb.Flow) (port string, proto api.L4Proto) {
	if f.L4 == nil {
		return "", ""
	}
	if tcp := f.L4.GetTCP(); tcp != nil {
		return strconv.FormatUint(uint64(tcp.DestinationPort), 10), api.ProtoTCP
	}
	if udp := f.L4.GetUDP(); udp != nil {
		return strconv.FormatUint(uint64(udp.DestinationPort), 10), api.ProtoUDP
	}
	return "", ""
}

// buildIngressRules groups ingress flows by source peer and builds IngressRules.
// World identity flows produce CIDR-based rules instead of endpoint selectors.
func buildIngressRules(flows []*flowpb.Flow, policyNamespace string) []api.IngressRule {
	type peerPorts struct {
		selector api.EndpointSelector
		ports    []api.PortProtocol
		seen     map[string]struct{}
	}

	type cidrPorts struct {
		cidr  api.CIDR
		ports []api.PortProtocol
		seen  map[string]struct{}
	}

	peers := make(map[string]*peerPorts)
	cidrs := make(map[string]*cidrPorts)
	var peerOrder, cidrOrder []string

	for _, f := range flows {
		if f.Source == nil {
			continue
		}
		port, proto := extractPort(f)
		if port == "" {
			continue
		}

		// Handle world identity: produce CIDR rule
		if isWorldIdentity(f.Source) {
			ip := getSourceIP(f)
			if ip == "" {
				continue
			}
			cidrStr := ip + "/32"
			cp, exists := cidrs[cidrStr]
			if !exists {
				cp = &cidrPorts{
					cidr: api.CIDR(cidrStr),
					seen: make(map[string]struct{}),
				}
				cidrs[cidrStr] = cp
				cidrOrder = append(cidrOrder, cidrStr)
			}
			dedupKey := port + "/" + string(proto)
			if _, dup := cp.seen[dedupKey]; !dup {
				cp.ports = append(cp.ports, api.PortProtocol{
					Port:     port,
					Protocol: proto,
				})
				cp.seen[dedupKey] = struct{}{}
			}
			continue
		}

		key := peerKey(f.Source.Labels)
		pp, exists := peers[key]
		if !exists {
			pp = &peerPorts{
				selector: labels.BuildPeerSelector(f.Source.Labels, f.Source.Namespace, policyNamespace),
				seen:     make(map[string]struct{}),
			}
			peers[key] = pp
			peerOrder = append(peerOrder, key)
		}

		dedupKey := port + "/" + string(proto)
		if _, dup := pp.seen[dedupKey]; !dup {
			pp.ports = append(pp.ports, api.PortProtocol{
				Port:     port,
				Protocol: proto,
			})
			pp.seen[dedupKey] = struct{}{}
		}
	}

	rules := make([]api.IngressRule, 0, len(cidrOrder)+len(peerOrder))

	// CIDR rules first
	for _, key := range cidrOrder {
		cp := cidrs[key]
		rules = append(rules, api.IngressRule{
			IngressCommonRule: api.IngressCommonRule{
				FromCIDR: api.CIDRSlice{cp.cidr},
			},
			ToPorts: api.PortRules{
				{Ports: cp.ports},
			},
		})
	}

	// Endpoint selector rules
	for _, key := range peerOrder {
		pp := peers[key]
		rules = append(rules, api.IngressRule{
			IngressCommonRule: api.IngressCommonRule{
				FromEndpoints: []api.EndpointSelector{pp.selector},
			},
			ToPorts: api.PortRules{
				{Ports: pp.ports},
			},
		})
	}
	return rules
}

// buildEgressRules groups egress flows by destination peer and builds EgressRules.
// World identity flows produce CIDR-based rules instead of endpoint selectors.
func buildEgressRules(flows []*flowpb.Flow, policyNamespace string) []api.EgressRule {
	type peerPorts struct {
		selector api.EndpointSelector
		ports    []api.PortProtocol
		seen     map[string]struct{}
	}

	type cidrPorts struct {
		cidr  api.CIDR
		ports []api.PortProtocol
		seen  map[string]struct{}
	}

	peers := make(map[string]*peerPorts)
	cidrs := make(map[string]*cidrPorts)
	var peerOrder, cidrOrder []string

	for _, f := range flows {
		if f.Destination == nil {
			continue
		}
		port, proto := extractPort(f)
		if port == "" {
			continue
		}

		// Handle world identity: produce CIDR rule
		if isWorldIdentity(f.Destination) {
			ip := getDestinationIP(f)
			if ip == "" {
				continue
			}
			cidrStr := ip + "/32"
			cp, exists := cidrs[cidrStr]
			if !exists {
				cp = &cidrPorts{
					cidr: api.CIDR(cidrStr),
					seen: make(map[string]struct{}),
				}
				cidrs[cidrStr] = cp
				cidrOrder = append(cidrOrder, cidrStr)
			}
			dedupKey := port + "/" + string(proto)
			if _, dup := cp.seen[dedupKey]; !dup {
				cp.ports = append(cp.ports, api.PortProtocol{
					Port:     port,
					Protocol: proto,
				})
				cp.seen[dedupKey] = struct{}{}
			}
			continue
		}

		key := peerKey(f.Destination.Labels)
		pp, exists := peers[key]
		if !exists {
			pp = &peerPorts{
				selector: labels.BuildPeerSelector(f.Destination.Labels, f.Destination.Namespace, policyNamespace),
				seen:     make(map[string]struct{}),
			}
			peers[key] = pp
			peerOrder = append(peerOrder, key)
		}

		dedupKey := port + "/" + string(proto)
		if _, dup := pp.seen[dedupKey]; !dup {
			pp.ports = append(pp.ports, api.PortProtocol{
				Port:     port,
				Protocol: proto,
			})
			pp.seen[dedupKey] = struct{}{}
		}
	}

	rules := make([]api.EgressRule, 0, len(cidrOrder)+len(peerOrder))

	// CIDR rules first
	for _, key := range cidrOrder {
		cp := cidrs[key]
		rules = append(rules, api.EgressRule{
			EgressCommonRule: api.EgressCommonRule{
				ToCIDR: api.CIDRSlice{cp.cidr},
			},
			ToPorts: api.PortRules{
				{Ports: cp.ports},
			},
		})
	}

	// Endpoint selector rules
	for _, key := range peerOrder {
		pp := peers[key]
		rules = append(rules, api.EgressRule{
			EgressCommonRule: api.EgressCommonRule{
				ToEndpoints: []api.EndpointSelector{pp.selector},
			},
			ToPorts: api.PortRules{
				{Ports: pp.ports},
			},
		})
	}
	return rules
}
