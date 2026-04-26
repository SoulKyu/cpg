package dropclass

import (
	"sort"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

// DropClass is the bucket a Cilium drop reason falls into.
type DropClass int

const (
	DropClassUnknown  DropClass = iota // 0 — unrecognized reason; never Policy
	DropClassPolicy                    // 1 — absent/misconfigured CNP; cpg generates policy
	DropClassInfra                     // 2 — datapath/infra failure; surface in health JSON
	DropClassTransient                 // 3 — startup race or normal CT transition; count only
	DropClassNoise                     // 4 — internal bookkeeping; ignore entirely
)

// dropReasonClass maps every flowpb.DropReason to its bucket.
// O(1) lookup — NOT a switch (switch on non-consecutive int32 enum is O(n)).
// Source: .planning/research/FEATURES.md canonical classification table (Cilium v1.19.1).
var dropReasonClass = map[flowpb.DropReason]DropClass{
	// ── TRANSIENT ──────────────────────────────────────────────────────────────
	// No signal; treat as transient, do not generate policy.
	flowpb.DropReason_DROP_REASON_UNKNOWN: DropClassTransient,

	// Stale/unroutable IP: pod restart / IP reuse lag; resolves when CT entries age out.
	flowpb.DropReason_STALE_OR_UNROUTABLE_IP: DropClassTransient,

	// Pre-endpoint-ID-table era; deprecated but still in enum.
	flowpb.DropReason_NO_MATCHING_LOCAL_CONTAINER_FOUND: DropClassTransient,

	// Endpoint not yet fully programmed; normal during pod startup race.
	flowpb.DropReason_NO_CONFIGURATION_AVAILABLE_TO_PERFORM_POLICY_DECISION: DropClassTransient,

	// Identity not yet allocated during pod startup; resolves when kvstore propagates.
	flowpb.DropReason_INVALID_IDENTITY: DropClassTransient,

	// Source identity not yet known to this node; propagation lag.
	flowpb.DropReason_UNKNOWN_SENDER: DropClassTransient,

	// Normal network behavior; routing loop detection.
	flowpb.DropReason_TTL_EXCEEDED: DropClassTransient,

	// Cilium agent starting up; drops during node init. Resolves without action.
	flowpb.DropReason_DROP_HOST_NOT_READY: DropClassTransient,

	// Pod endpoint being programmed (common on new pod start). Resolves within seconds.
	flowpb.DropReason_DROP_EP_NOT_READY: DropClassTransient,

	// ── NOISE ──────────────────────────────────────────────────────────────────
	// Internal Cilium bookkeeping; not an error.
	flowpb.DropReason_NAT_NOT_NEEDED: DropClassNoise,

	// Expected datapath short-circuit for ClusterIP traffic.
	flowpb.DropReason_IS_A_CLUSTERIP: DropClassNoise,

	// IGMP multicast join/leave; expected datapath event.
	flowpb.DropReason_IGMP_HANDLED: DropClassNoise,

	// IGMP subscription; expected.
	flowpb.DropReason_IGMP_SUBSCRIBED: DropClassNoise,

	// Multicast handled internally; not an error.
	flowpb.DropReason_MULTICAST_HANDLED: DropClassNoise,

	// Traffic redirected to Envoy proxy; this is a redirect, not a drop error.
	flowpb.DropReason_DROP_PUNT_PROXY: DropClassNoise,

	// ── POLICY ─────────────────────────────────────────────────────────────────
	// Primary signal: L3/L4 deny due to absent allow rule.
	flowpb.DropReason_POLICY_DENIED: DropClassPolicy,

	// LoadBalancer spec.loadBalancerSourceRanges intentional deny — policy-fixable.
	flowpb.DropReason_DENIED_BY_LB_SRC_RANGE_CHECK: DropClassPolicy,

	// Explicit denylist rule in CNP hit; separate from POLICY_DENIED (133).
	flowpb.DropReason_POLICY_DENY: DropClassPolicy,

	// SPIRE mTLS required; could be SPIRE infra misconfiguration — v1.4 review.
	flowpb.DropReason_AUTH_REQUIRED: DropClassPolicy,

	// ── INFRA ──────────────────────────────────────────────────────────────────
	// Layer 2 hardware/overlay misconfiguration; CNP cannot fix.
	flowpb.DropReason_INVALID_SOURCE_MAC:      DropClassInfra,
	flowpb.DropReason_INVALID_DESTINATION_MAC: DropClassInfra,

	// Spoofed or misconfigured source; datapath enforcement.
	flowpb.DropReason_INVALID_SOURCE_IP: DropClassInfra,

	// Malformed packet; datapath protection.
	flowpb.DropReason_INVALID_PACKET_DROPPED: DropClassInfra,

	// Conntrack BPF map corruption or malformed TCP.
	flowpb.DropReason_CT_TRUNCATED_OR_INVALID_HEADER: DropClassInfra,

	// TCP state machine issue; not policy.
	flowpb.DropReason_CT_MISSING_TCP_ACK_FLAG: DropClassInfra,

	// Unknown L4 in conntrack; datapath gap.
	flowpb.DropReason_CT_UNKNOWN_L4_PROTOCOL: DropClassInfra,

	// CT map write failure; deprecated but keep bucket.
	flowpb.DropReason_CT_CANNOT_CREATE_ENTRY_FROM_PACKET: DropClassInfra,

	// Non-IP traffic; datapath does not support.
	flowpb.DropReason_UNSUPPORTED_L3_PROTOCOL: DropClassInfra,

	// BPF tail-call table miss; kernel/cilium version mismatch.
	flowpb.DropReason_MISSED_TAIL_CALL: DropClassInfra,

	// BPF packet write failure; datapath bug.
	flowpb.DropReason_ERROR_WRITING_TO_PACKET: DropClassInfra,

	// Unrecognized L4 in policy engine.
	flowpb.DropReason_UNKNOWN_L4_PROTOCOL: DropClassInfra,

	// Unexpected ICMP variant; datapath gap.
	flowpb.DropReason_UNKNOWN_ICMPV4_CODE: DropClassInfra,
	flowpb.DropReason_UNKNOWN_ICMPV4_TYPE: DropClassInfra,
	flowpb.DropReason_UNKNOWN_ICMPV6_CODE: DropClassInfra,
	flowpb.DropReason_UNKNOWN_ICMPV6_TYPE: DropClassInfra,

	// Tunnel/overlay metadata failure.
	flowpb.DropReason_ERROR_RETRIEVING_TUNNEL_KEY: DropClassInfra,

	// Deprecated but still in enum.
	flowpb.DropReason_ERROR_RETRIEVING_TUNNEL_OPTIONS: DropClassInfra,
	flowpb.DropReason_INVALID_GENEVE_OPTION:           DropClassInfra,

	// Next-hop resolution failure; routing issue.
	flowpb.DropReason_UNKNOWN_L3_TARGET_ADDRESS: DropClassInfra,

	// Hardware offload / BPF checksum bug.
	flowpb.DropReason_ERROR_WHILE_CORRECTING_L3_CHECKSUM: DropClassInfra,
	flowpb.DropReason_ERROR_WHILE_CORRECTING_L4_CHECKSUM: DropClassInfra,

	// The triggering prod bug: conntrack BPF map full.
	flowpb.DropReason_CT_MAP_INSERTION_FAILED: DropClassInfra,

	// Unsupported IPv6 extension; datapath gap.
	flowpb.DropReason_INVALID_IPV6_EXTENSION_HEADER: DropClassInfra,

	// Fragmented packets; MTU or overlay config.
	flowpb.DropReason_IP_FRAGMENTATION_NOT_SUPPORTED: DropClassInfra,

	// Cilium kube-proxy LB map stale.
	flowpb.DropReason_SERVICE_BACKEND_NOT_FOUND: DropClassInfra,

	// Overlay routing gap; CNP cannot fix.
	flowpb.DropReason_NO_TUNNEL_OR_ENCAPSULATION_ENDPOINT: DropClassInfra,

	// NAT46/64 feature disabled; cluster config.
	flowpb.DropReason_FAILED_TO_INSERT_INTO_PROXYMAP: DropClassInfra,

	// BPF bandwidth manager rate limit hit.
	flowpb.DropReason_REACHED_EDT_RATE_LIMITING_DROP_HORIZON: DropClassInfra,

	// CT state machine inconsistency; Cilium agent restart may help.
	flowpb.DropReason_UNKNOWN_CONNECTION_TRACKING_STATE: DropClassInfra,

	// Node-level routing gap.
	flowpb.DropReason_LOCAL_HOST_IS_UNREACHABLE: DropClassInfra,

	// Non-Ethernet L2; datapath gap.
	flowpb.DropReason_UNSUPPORTED_L2_PROTOCOL: DropClassInfra,

	// SNAT table miss; NAT config issue.
	flowpb.DropReason_NO_MAPPING_FOR_NAT_MASQUERADE: DropClassInfra,

	// Protocol not supported by SNAT engine.
	flowpb.DropReason_UNSUPPORTED_PROTOCOL_FOR_NAT_MASQUERADE: DropClassInfra,

	// Missing kernel route / ARP neighbor; routing misconfiguration.
	flowpb.DropReason_FIB_LOOKUP_FAILED: DropClassInfra,

	// Tunnel-in-tunnel blocked; overlay config.
	flowpb.DropReason_ENCAPSULATION_TRAFFIC_IS_PROHIBITED: DropClassInfra,

	// IP fragment reassembly failure.
	flowpb.DropReason_FIRST_LOGICAL_DATAGRAM_FRAGMENT_NOT_FOUND: DropClassInfra,

	// ICMPv6 type blocked by datapath policy.
	flowpb.DropReason_FORBIDDEN_ICMPV6_MESSAGE: DropClassInfra,

	// BPF socket-LB table miss.
	flowpb.DropReason_SOCKET_LOOKUP_FAILED: DropClassInfra,

	// BPF socket assignment error.
	flowpb.DropReason_SOCKET_ASSIGN_FAILED: DropClassInfra,

	// Protocol not interceptable by Envoy proxy.
	flowpb.DropReason_PROXY_REDIRECTION_NOT_SUPPORTED_FOR_PROTOCOL: DropClassInfra,

	// VLAN filter config; not CNP.
	flowpb.DropReason_VLAN_FILTERED: DropClassInfra,

	// VXLAN overlay misconfiguration.
	flowpb.DropReason_INVALID_VNI: DropClassInfra,

	// TC BPF map failure.
	flowpb.DropReason_INVALID_TC_BUFFER: DropClassInfra,

	// SRv6 segment ID missing; SRv6 config issue.
	flowpb.DropReason_NO_SID: DropClassInfra,

	// SRv6 state missing; deprecated.
	flowpb.DropReason_MISSING_SRV6_STATE: DropClassInfra,

	// NAT46/NAT64 translation failures; NAT config.
	flowpb.DropReason_NAT46: DropClassInfra,
	flowpb.DropReason_NAT64: DropClassInfra,

	// CT BPF map completely absent; severe Cilium agent issue.
	flowpb.DropReason_CT_NO_MAP_FOUND: DropClassInfra,

	// NAT BPF map absent; severe Cilium agent issue.
	flowpb.DropReason_SNAT_NO_MAP_FOUND: DropClassInfra,

	// ClusterMesh misconfiguration.
	flowpb.DropReason_INVALID_CLUSTER_ID: DropClassInfra,

	// DSR encap config issue.
	flowpb.DropReason_UNSUPPORTED_PROTOCOL_FOR_DSR_ENCAP: DropClassInfra,

	// Egress gateway policy matched but no gateway node; EgressGatewayPolicy misconfiguration.
	flowpb.DropReason_NO_EGRESS_GATEWAY: DropClassInfra,

	// WireGuard strict mode: unencrypted traffic blocked.
	flowpb.DropReason_UNENCRYPTED_TRAFFIC: DropClassInfra,

	// Node identity not yet allocated; severe init issue.
	flowpb.DropReason_NO_NODE_ID: DropClassInfra,

	// API rate limiting in cilium-agent.
	flowpb.DropReason_DROP_RATE_LIMITED: DropClassInfra,

	// EgressGateway policy: no IP assigned to gateway interface.
	flowpb.DropReason_DROP_NO_EGRESS_IP: DropClassInfra,
}

// validReasonNamesSorted is built once at init from flowpb.DropReason_name values.
var validReasonNamesSorted []string

func init() {
	names := make([]string, 0, len(flowpb.DropReason_name))
	for _, name := range flowpb.DropReason_name {
		names = append(names, name)
	}
	sort.Strings(names)
	validReasonNamesSorted = names
}

// Classify returns the DropClass for a given Cilium drop reason.
// Returns DropClassUnknown for any reason not in the taxonomy map.
// O(1) map lookup — safe for high-frequency hot path.
func Classify(reason flowpb.DropReason) DropClass {
	if c, ok := dropReasonClass[reason]; ok {
		return c
	}
	return DropClassUnknown
}

// ValidReasonNames returns a sorted copy of all flowpb.DropReason enum names.
// Used by --ignore-drop-reason flag validation (phase 13).
func ValidReasonNames() []string {
	out := make([]string, len(validReasonNamesSorted))
	copy(out, validReasonNamesSorted)
	return out
}
