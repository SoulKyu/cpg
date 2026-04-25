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
	// httpRules collects per-(port, proto) HTTP L7 entries when
	// AttributionOptions.L7Enabled is true. Key is "<port>/<proto>" matching
	// the seen-key form so a port's L7 rules align to its L4 PortProtocol.
	// When empty (the v1.1 codepath), ingress/egress emission is byte-stable
	// with v1.1 — Rules are NOT attached to any PortRule.
	httpRules map[string][]api.PortRuleHTTP
	// httpSeen dedups within a bucket so two flows reporting GET /a on the
	// same (port, proto) collapse into a single PortRuleHTTP entry.
	httpSeen map[string]map[string]struct{}
}

func newPeerRules() *peerRules {
	return &peerRules{
		seen:      make(map[string]struct{}),
		attrib:    make(map[string]*RuleAttribution),
		httpRules: make(map[string][]api.PortRuleHTTP),
		httpSeen:  make(map[string]map[string]struct{}),
	}
}

// addHTTPRules appends rules to the bucket's httpRules slice for the given
// port-proto key, deduplicating by httpRuleKey so repeat observations of the
// same (method, path) do not produce duplicates.
func (pr *peerRules) addHTTPRules(portProtoKey string, rules []api.PortRuleHTTP) {
	if len(rules) == 0 {
		return
	}
	seen, ok := pr.httpSeen[portProtoKey]
	if !ok {
		seen = make(map[string]struct{})
		pr.httpSeen[portProtoKey] = seen
	}
	for _, h := range rules {
		k := httpRuleKey(h)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		pr.httpRules[portProtoKey] = append(pr.httpRules[portProtoKey], h)
	}
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
	if ts := FlowTime(f); !ts.IsZero() {
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
			peer := Peer{Type: PeerEntity, Entity: string(entity)}
			if !recordL7(er, f, fp, direction, peer, opts) {
				er.recordAttribution(ruleKeyFor(direction, peer, fp), f, opts.MaxSamples)
			}
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
			peer := Peer{Type: PeerCIDR, CIDR: cidrStr}
			if !recordL7(cb.peerRules, f, fp, direction, peer, opts) {
				cb.recordAttribution(ruleKeyFor(direction, peer, fp), f, opts.MaxSamples)
			}
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
		peer := Peer{Type: PeerEndpoint, Labels: selectedLabelsFromFlow(ep)}
		if !recordL7(eb.peerRules, f, fp, direction, peer, opts) {
			eb.recordAttribution(ruleKeyFor(direction, peer, fp), f, opts.MaxSamples)
		}
	}
	return b
}

// recordL7 attaches HTTP L7 rules + per-(method, path) attribution to a peer
// bucket when AttributionOptions.L7Enabled is true and the flow carries a
// non-nil L7 HTTP record producing at least one rule. Returns true when L7
// attribution was recorded — caller then SKIPS the bare L4 attribution so
// the evidence bucket reflects the L7-discriminated rules only (otherwise
// flows would double-count: once L4, once L7). Returns false when no L7
// rules were emitted (L7Enabled=false, no HTTP record, or empty method) so
// the caller falls back to the v1.1 L4 attribution path.
func recordL7(pr *peerRules, f *flowpb.Flow, fp *flowProto, direction string, peer Peer, opts AttributionOptions) bool {
	if !opts.L7Enabled {
		return false
	}
	if f.GetL7().GetHttp() == nil {
		return false
	}
	rules := extractHTTPRules(f)
	if len(rules) == 0 {
		return false
	}
	portKey := strconv.FormatUint(uint64(fp.port), 10) + "/" + string(fp.proto)
	pr.addHTTPRules(portKey, rules)
	for _, r := range rules {
		l7 := &L7Discriminator{Protocol: "http", HTTPMethod: r.Method, HTTPPath: r.Path}
		pr.recordAttribution(ruleKeyForL7(direction, peer, fp, l7), f, opts.MaxSamples)
	}
	return true
}

func buildIngressRules(flows []*flowpb.Flow, policyNamespace string, tracker FlowTracker, opts AttributionOptions) ([]api.IngressRule, []RuleAttribution) {
	b := groupFlows(flows, policyNamespace, tracker, ingressReasons, "ingress", opts,
		func(f *flowpb.Flow) *flowpb.Endpoint { return f.Source },
		flowSourceIP)
	var rules []api.IngressRule
	var attrib []RuleAttribution
	for _, entity := range b.entityOrder {
		er := b.entities[entity]
		rules = append(rules, ingressRulesFrom(api.IngressCommonRule{FromEntities: api.EntitySlice{entity}}, er.ports, er.icmpFields, er.httpRules)...)
		for _, a := range er.attrib {
			attrib = append(attrib, *a)
		}
	}
	for _, key := range b.cidrOrder {
		cb := b.cidrs[key]
		rules = append(rules, ingressRulesFrom(api.IngressCommonRule{FromCIDR: api.CIDRSlice{cb.cidr}}, cb.ports, cb.icmpFields, cb.httpRules)...)
		for _, a := range cb.attrib {
			attrib = append(attrib, *a)
		}
	}
	for _, key := range b.peerOrder {
		eb := b.peers[key]
		rules = append(rules, ingressRulesFrom(api.IngressCommonRule{FromEndpoints: []api.EndpointSelector{eb.selector}}, eb.ports, eb.icmpFields, eb.httpRules)...)
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
		rules = append(rules, egressRulesFrom(api.EgressCommonRule{ToEntities: api.EntitySlice{entity}}, er.ports, er.icmpFields, er.httpRules)...)
		for _, a := range er.attrib {
			attrib = append(attrib, *a)
		}
	}
	for _, key := range b.cidrOrder {
		cb := b.cidrs[key]
		rules = append(rules, egressRulesFrom(api.EgressCommonRule{ToCIDR: api.CIDRSlice{cb.cidr}}, cb.ports, cb.icmpFields, cb.httpRules)...)
		for _, a := range cb.attrib {
			attrib = append(attrib, *a)
		}
	}
	for _, key := range b.peerOrder {
		eb := b.peers[key]
		rules = append(rules, egressRulesFrom(api.EgressCommonRule{ToEndpoints: []api.EndpointSelector{eb.selector}}, eb.ports, eb.icmpFields, eb.httpRules)...)
		for _, a := range eb.attrib {
			attrib = append(attrib, *a)
		}
	}
	return rules, attrib
}

func ingressRulesFrom(common api.IngressCommonRule, ports []api.PortProtocol, icmps []api.ICMPField, httpByPort map[string][]api.PortRuleHTTP) []api.IngressRule {
	var out []api.IngressRule
	if len(ports) > 0 {
		out = append(out, api.IngressRule{IngressCommonRule: common, ToPorts: portRulesFor(ports, httpByPort)})
	}
	if len(icmps) > 0 {
		out = append(out, api.IngressRule{IngressCommonRule: common, ICMPs: api.ICMPRules{{Fields: icmps}}})
	}
	return out
}

func egressRulesFrom(common api.EgressCommonRule, ports []api.PortProtocol, icmps []api.ICMPField, httpByPort map[string][]api.PortRuleHTTP) []api.EgressRule {
	var out []api.EgressRule
	if len(ports) > 0 {
		out = append(out, api.EgressRule{EgressCommonRule: common, ToPorts: portRulesFor(ports, httpByPort)})
	}
	if len(icmps) > 0 {
		out = append(out, api.EgressRule{EgressCommonRule: common, ICMPs: api.ICMPRules{{Fields: icmps}}})
	}
	return out
}

// portRulesFor builds the PortRule slice for a peer bucket. When no L7 HTTP
// rules are attached to any port (httpByPort empty), it returns the v1.1
// shape: a single PortRule with all ports collapsed in. When at least one
// port carries L7 rules, it emits one PortRule per port — so HTTP rules
// attach to the matching port only — and the PortRules without L7 rules
// degrade gracefully into per-port entries with nil Rules. This trades
// L4-only YAML byte-stability under L7Enabled=true ONLY when L7 records are
// actually present; the all-empty case still collapses (verified by the
// L7Enabled toggle byte-identity test).
func portRulesFor(ports []api.PortProtocol, httpByPort map[string][]api.PortRuleHTTP) api.PortRules {
	hasAnyL7 := false
	for _, p := range ports {
		if len(httpByPort[p.Port+"/"+string(p.Protocol)]) > 0 {
			hasAnyL7 = true
			break
		}
	}
	if !hasAnyL7 {
		return api.PortRules{{Ports: ports}}
	}
	out := make(api.PortRules, 0, len(ports))
	for _, p := range ports {
		key := p.Port + "/" + string(p.Protocol)
		pr := api.PortRule{Ports: []api.PortProtocol{p}}
		if rules := httpByPort[key]; len(rules) > 0 {
			pr.Rules = &api.L7Rules{HTTP: rules}
		}
		out = append(out, pr)
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

// ruleKeyForL7 mirrors ruleKeyFor but populates the L7 discriminator so two
// rules differing only by HTTP method/path are NOT collapsed into the same
// attribution bucket (EVID2-02).
func ruleKeyForL7(direction string, peer Peer, fp *flowProto, l7 *L7Discriminator) RuleKey {
	k := ruleKeyFor(direction, peer, fp)
	k.L7 = l7
	return k
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
func FlowTime(f *flowpb.Flow) time.Time {
	if f == nil || f.Time == nil {
		return time.Time{}
	}
	return f.Time.AsTime()
}
