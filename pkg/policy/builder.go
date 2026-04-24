package policy

import (
	"sort"
	"strconv"
	"strings"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/SoulKyu/cpg/pkg/labels"
)

// FlowTracker receives flows that cannot be converted to policy rules.
type FlowTracker interface {
	Track(f *flowpb.Flow, reason UnhandledReason)
}

// ReservedWorldIdentity is the Cilium reserved identity for external/world traffic.
const ReservedWorldIdentity uint32 = 2

// policyNamePrefix is the CNP name prefix shared by the builder and dedup consumers.
const policyNamePrefix = "cpg-"

// PolicyName returns the CiliumNetworkPolicy name generated for a workload.
func PolicyName(workload string) string {
	return policyNamePrefix + workload
}

var reservedEntityMap = map[string]api.Entity{
	"reserved:kube-apiserver": api.EntityKubeAPIServer,
	"reserved:host":          api.EntityHost,
	"reserved:remote-node":   api.EntityRemoteNode,
	"reserved:health":        api.EntityHealth,
}

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

func reservedEntity(ep *flowpb.Endpoint) api.Entity {
	if ep == nil {
		return ""
	}
	for _, l := range ep.Labels {
		if e, ok := reservedEntityMap[l]; ok {
			return e
		}
	}
	return ""
}

func flowSourceIP(f *flowpb.Flow) string {
	if f.IP == nil {
		return ""
	}
	return f.IP.Source
}

func flowDestinationIP(f *flowpb.Flow) string {
	if f.IP == nil {
		return ""
	}
	return f.IP.Destination
}

// PolicyEvent wraps a generated CiliumNetworkPolicy with its target location.
type PolicyEvent struct {
	Namespace   string
	Workload    string
	Policy      *ciliumv2.CiliumNetworkPolicy
	Attribution []RuleAttribution // nil when AttributionOptions.MaxSamples == 0
}

// BuildPolicy transforms a set of Hubble dropped flows into a CiliumNetworkPolicy.
func BuildPolicy(namespace, workload string, flows []*flowpb.Flow, tracker FlowTracker, opts AttributionOptions) (*ciliumv2.CiliumNetworkPolicy, []RuleAttribution) {
	cnp := &ciliumv2.CiliumNetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cilium.io/v2",
			Kind:       "CiliumNetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PolicyName(workload),
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "cpg",
			},
		},
		Spec: &api.Rule{},
	}

	var ingressFlows, egressFlows []*flowpb.Flow
	for _, f := range flows {
		if f.L4 == nil {
			if tracker != nil {
				tracker.Track(f, ReasonNoL4)
			}
			continue
		}
		switch f.TrafficDirection {
		case flowpb.TrafficDirection_INGRESS:
			ingressFlows = append(ingressFlows, f)
		case flowpb.TrafficDirection_EGRESS:
			egressFlows = append(egressFlows, f)
		}
	}

	if selectorLabels := pickSelectorLabels(flows); selectorLabels != nil {
		cnp.Spec.EndpointSelector = labels.BuildEndpointSelector(selectorLabels)
	}

	ingressRules, ingressAttrib := buildIngressRules(ingressFlows, namespace, tracker, opts)
	egressRules, egressAttrib := buildEgressRules(egressFlows, namespace, tracker, opts)
	cnp.Spec.Ingress = ingressRules
	cnp.Spec.Egress = egressRules

	var attrib []RuleAttribution
	attrib = append(attrib, ingressAttrib...)
	attrib = append(attrib, egressAttrib...)
	if len(attrib) == 0 {
		attrib = nil
	}
	return cnp, attrib
}

func pickSelectorLabels(flows []*flowpb.Flow) []string {
	for _, f := range flows {
		if f.TrafficDirection == flowpb.TrafficDirection_INGRESS && f.Destination != nil {
			return f.Destination.Labels
		}
		if f.TrafficDirection == flowpb.TrafficDirection_EGRESS && f.Source != nil {
			return f.Source.Labels
		}
	}
	return nil
}

func peerKey(peerLabels []string) string {
	selected := labels.SelectLabels(peerLabels)
	keys := make([]string, 0, len(selected))
	for k := range selected {
		keys = append(keys, k+"="+selected[k])
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// flowProto describes the protocol extracted from a flow's L4 layer.
type flowProto struct {
	port  uint32
	proto api.L4Proto
	icmp  bool
}

func extractProto(f *flowpb.Flow) *flowProto {
	if f.L4 == nil {
		return nil
	}
	if tcp := f.L4.GetTCP(); tcp != nil {
		return &flowProto{port: tcp.DestinationPort, proto: api.ProtoTCP}
	}
	if udp := f.L4.GetUDP(); udp != nil {
		return &flowProto{port: udp.DestinationPort, proto: api.ProtoUDP}
	}
	if icmp4 := f.L4.GetICMPv4(); icmp4 != nil {
		return &flowProto{port: icmp4.Type, proto: api.ProtoICMP, icmp: true}
	}
	if icmp6 := f.L4.GetICMPv6(); icmp6 != nil {
		return &flowProto{port: icmp6.Type, proto: api.ProtoICMPv6, icmp: true}
	}
	return nil
}

func icmpFamily(proto api.L4Proto) string {
	if proto == api.ProtoICMPv6 {
		return api.IPv6Family
	}
	return api.IPv4Family
}

// peerRules collects ports and ICMP types for a peer grouping.
type peerRules struct {
	ports      []api.PortProtocol
	icmpFields []api.ICMPField
	seen       map[string]struct{}
	// attribution: one entry per rule key produced from this bucket
	attrib map[string]*RuleAttribution
}

func newPeerRules() *peerRules {
	return &peerRules{seen: make(map[string]struct{}), attrib: make(map[string]*RuleAttribution)}
}

func (pr *peerRules) recordAttribution(key RuleKey, f *flowpb.Flow, maxSamples int) {
	if maxSamples <= 0 {
		return
	}
	k := key.String()
	entry, ok := pr.attrib[k]
	if !ok {
		entry = &RuleAttribution{Key: key}
		pr.attrib[k] = entry
	}
	entry.FlowCount++
	if ts := flowTime(f); !ts.IsZero() {
		if entry.FirstSeen.IsZero() || ts.Before(entry.FirstSeen) {
			entry.FirstSeen = ts
		}
		if ts.After(entry.LastSeen) {
			entry.LastSeen = ts
		}
	}
	if len(entry.Samples) < maxSamples {
		entry.Samples = append(entry.Samples, f)
	} else {
		// FIFO newest: drop oldest, append new
		entry.Samples = append(entry.Samples[1:], f)
	}
}

func (pr *peerRules) addFlow(fp *flowProto) {
	portStr := strconv.FormatUint(uint64(fp.port), 10)
	dedupKey := portStr + "/" + string(fp.proto)
	if _, dup := pr.seen[dedupKey]; dup {
		return
	}
	pr.seen[dedupKey] = struct{}{}
	if fp.icmp {
		icmpType := intstr.FromInt32(int32(fp.port))
		pr.icmpFields = append(pr.icmpFields, api.ICMPField{
			Family: icmpFamily(fp.proto),
			Type:   &icmpType,
		})
		return
	}
	pr.ports = append(pr.ports, api.PortProtocol{
		Port:     portStr,
		Protocol: fp.proto,
	})
}

// endpointBucket groups rules for an endpoint peer matched by labels.
type endpointBucket struct {
	selector api.EndpointSelector
	*peerRules
}

// cidrBucket groups rules for a CIDR peer (world identity).
type cidrBucket struct {
	cidr api.CIDR
	*peerRules
}

// peerBuckets accumulates grouped flows during directional traversal.
type peerBuckets struct {
	peers       map[string]*endpointBucket
	cidrs       map[string]*cidrBucket
	entities    map[api.Entity]*peerRules
	peerOrder   []string
	cidrOrder   []string
	entityOrder []api.Entity
}

func newPeerBuckets() *peerBuckets {
	return &peerBuckets{
		peers:    make(map[string]*endpointBucket),
		cidrs:    make(map[string]*cidrBucket),
		entities: make(map[api.Entity]*peerRules),
	}
}

// reasons are tracker reasons used per direction.
type reasons struct {
	nilPeer   UnhandledReason
	unknownL4 UnhandledReason
	worldNoIP UnhandledReason
}

var (
	ingressReasons = reasons{nilPeer: ReasonNilSource, unknownL4: ReasonUnknownProtocol, worldNoIP: ReasonWorldNoIP}
	egressReasons  = reasons{nilPeer: ReasonNilDestination, unknownL4: ReasonUnknownProtocol, worldNoIP: ReasonWorldNoIP}
)

// groupFlows walks flows and distributes them into entity/cidr/endpoint buckets.
// peer and peerIP extract the direction-specific endpoint and IP respectively.
func groupFlows(
	flows []*flowpb.Flow,
	policyNamespace string,
	tracker FlowTracker,
	r reasons,
	direction string,
	opts AttributionOptions,
	peer func(*flowpb.Flow) *flowpb.Endpoint,
	peerIP func(*flowpb.Flow) string,
) *peerBuckets {
	b := newPeerBuckets()
	for _, f := range flows {
		ep := peer(f)
		if ep == nil {
			if tracker != nil {
				tracker.Track(f, r.nilPeer)
			}
			continue
		}
		fp := extractProto(f)
		if fp == nil {
			if tracker != nil {
				tracker.Track(f, r.unknownL4)
			}
			continue
		}
		if entity := reservedEntity(ep); entity != "" {
			er, exists := b.entities[entity]
			if !exists {
				er = newPeerRules()
				b.entities[entity] = er
				b.entityOrder = append(b.entityOrder, entity)
			}
			er.addFlow(fp)
			er.recordAttribution(ruleKeyFor(direction, Peer{Type: PeerEntity, Entity: string(entity)}, fp), f, opts.MaxSamples)
			continue
		}
		if isWorldIdentity(ep) {
			ip := peerIP(f)
			if ip == "" {
				if tracker != nil {
					tracker.Track(f, r.worldNoIP)
				}
				continue
			}
			cidrStr := ip + "/32"
			cb, exists := b.cidrs[cidrStr]
			if !exists {
				cb = &cidrBucket{cidr: api.CIDR(cidrStr), peerRules: newPeerRules()}
				b.cidrs[cidrStr] = cb
				b.cidrOrder = append(b.cidrOrder, cidrStr)
			}
			cb.addFlow(fp)
			cb.recordAttribution(ruleKeyFor(direction, Peer{Type: PeerCIDR, CIDR: cidrStr}, fp), f, opts.MaxSamples)
			continue
		}
		key := peerKey(ep.Labels)
		eb, exists := b.peers[key]
		if !exists {
			eb = &endpointBucket{
				selector:  labels.BuildPeerSelector(ep.Labels, ep.Namespace, policyNamespace),
				peerRules: newPeerRules(),
			}
			b.peers[key] = eb
			b.peerOrder = append(b.peerOrder, key)
		}
		eb.addFlow(fp)
		eb.recordAttribution(ruleKeyFor(direction, Peer{Type: PeerEndpoint, Labels: selectedLabelsFromFlow(ep)}, fp), f, opts.MaxSamples)
	}
	return b
}

func buildIngressRules(flows []*flowpb.Flow, policyNamespace string, tracker FlowTracker, opts AttributionOptions) ([]api.IngressRule, []RuleAttribution) {
	b := groupFlows(flows, policyNamespace, tracker, ingressReasons, "ingress", opts,
		func(f *flowpb.Flow) *flowpb.Endpoint { return f.Source },
		flowSourceIP)
	var rules []api.IngressRule
	var attrib []RuleAttribution
	for _, entity := range b.entityOrder {
		er := b.entities[entity]
		rules = append(rules, ingressRulesFrom(api.IngressCommonRule{FromEntities: api.EntitySlice{entity}}, er.ports, er.icmpFields)...)
		for _, a := range er.attrib {
			attrib = append(attrib, *a)
		}
	}
	for _, key := range b.cidrOrder {
		cb := b.cidrs[key]
		rules = append(rules, ingressRulesFrom(api.IngressCommonRule{FromCIDR: api.CIDRSlice{cb.cidr}}, cb.ports, cb.icmpFields)...)
		for _, a := range cb.attrib {
			attrib = append(attrib, *a)
		}
	}
	for _, key := range b.peerOrder {
		eb := b.peers[key]
		rules = append(rules, ingressRulesFrom(api.IngressCommonRule{FromEndpoints: []api.EndpointSelector{eb.selector}}, eb.ports, eb.icmpFields)...)
		for _, a := range eb.attrib {
			attrib = append(attrib, *a)
		}
	}
	return rules, attrib
}

func buildEgressRules(flows []*flowpb.Flow, policyNamespace string, tracker FlowTracker, opts AttributionOptions) ([]api.EgressRule, []RuleAttribution) {
	b := groupFlows(flows, policyNamespace, tracker, egressReasons, "egress", opts,
		func(f *flowpb.Flow) *flowpb.Endpoint { return f.Destination },
		flowDestinationIP)
	var rules []api.EgressRule
	var attrib []RuleAttribution
	for _, entity := range b.entityOrder {
		er := b.entities[entity]
		rules = append(rules, egressRulesFrom(api.EgressCommonRule{ToEntities: api.EntitySlice{entity}}, er.ports, er.icmpFields)...)
		for _, a := range er.attrib {
			attrib = append(attrib, *a)
		}
	}
	for _, key := range b.cidrOrder {
		cb := b.cidrs[key]
		rules = append(rules, egressRulesFrom(api.EgressCommonRule{ToCIDR: api.CIDRSlice{cb.cidr}}, cb.ports, cb.icmpFields)...)
		for _, a := range cb.attrib {
			attrib = append(attrib, *a)
		}
	}
	for _, key := range b.peerOrder {
		eb := b.peers[key]
		rules = append(rules, egressRulesFrom(api.EgressCommonRule{ToEndpoints: []api.EndpointSelector{eb.selector}}, eb.ports, eb.icmpFields)...)
		for _, a := range eb.attrib {
			attrib = append(attrib, *a)
		}
	}
	return rules, attrib
}

func ingressRulesFrom(common api.IngressCommonRule, ports []api.PortProtocol, icmps []api.ICMPField) []api.IngressRule {
	var out []api.IngressRule
	if len(ports) > 0 {
		out = append(out, api.IngressRule{IngressCommonRule: common, ToPorts: api.PortRules{{Ports: ports}}})
	}
	if len(icmps) > 0 {
		out = append(out, api.IngressRule{IngressCommonRule: common, ICMPs: api.ICMPRules{{Fields: icmps}}})
	}
	return out
}

func egressRulesFrom(common api.EgressCommonRule, ports []api.PortProtocol, icmps []api.ICMPField) []api.EgressRule {
	var out []api.EgressRule
	if len(ports) > 0 {
		out = append(out, api.EgressRule{EgressCommonRule: common, ToPorts: api.PortRules{{Ports: ports}}})
	}
	if len(icmps) > 0 {
		out = append(out, api.EgressRule{EgressCommonRule: common, ICMPs: api.ICMPRules{{Fields: icmps}}})
	}
	return out
}

// ruleKeyFor builds a RuleKey for the given direction, peer and flow protocol.
func ruleKeyFor(direction string, peer Peer, fp *flowProto) RuleKey {
	return RuleKey{
		Direction: direction,
		Peer:      peer,
		Port:      strconv.FormatUint(uint64(fp.port), 10),
		Protocol:  protoDisplayName(fp.proto),
	}
}

// protoDisplayName returns a human-readable protocol name.
func protoDisplayName(p api.L4Proto) string {
	switch p {
	case api.ProtoTCP:
		return "TCP"
	case api.ProtoUDP:
		return "UDP"
	case api.ProtoICMP:
		return "ICMPv4"
	case api.ProtoICMPv6:
		return "ICMPv6"
	default:
		return "UNKNOWN"
	}
}

// selectedLabelsFromFlow returns the selected labels for an endpoint as a map.
func selectedLabelsFromFlow(ep *flowpb.Endpoint) map[string]string {
	return labels.SelectLabels(ep.Labels)
}

// flowTime extracts a timestamp from a Hubble flow, falling back to zero when
// absent (Hubble always populates this in practice but tests may omit it).
func flowTime(f *flowpb.Flow) time.Time {
	if f == nil || f.Time == nil {
		return time.Time{}
	}
	return f.Time.AsTime()
}
