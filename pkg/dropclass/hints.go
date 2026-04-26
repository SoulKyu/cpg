package dropclass

import flowpb "github.com/cilium/cilium/api/v1/flow"

// dropReasonHint maps every INFRA-classified drop reason to a Cilium docs URL.
// Non-infra reasons are absent — RemediationHint returns "" for them.
// Base URL: https://docs.cilium.io/en/stable/operations/troubleshooting/
var dropReasonHint = map[flowpb.DropReason]string{
	// ── Conntrack ──────────────────────────────────────────────────────────────
	// The triggering production bug: conntrack BPF map full.
	// Fix: raise bpf-ct-global-tcp-max or lower conntrack-gc-interval.
	flowpb.DropReason_CT_MAP_INSERTION_FAILED: "https://docs.cilium.io/en/stable/operations/troubleshooting/#handling-drop-ct-map-insertion-failed",

	// CT BPF map completely absent.
	flowpb.DropReason_CT_NO_MAP_FOUND: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// CT state machine issue (deprecated but still present).
	flowpb.DropReason_CT_TRUNCATED_OR_INVALID_HEADER:         "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_CT_MISSING_TCP_ACK_FLAG:                "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_CT_UNKNOWN_L4_PROTOCOL:                 "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_CT_CANNOT_CREATE_ENTRY_FROM_PACKET:     "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_UNKNOWN_CONNECTION_TRACKING_STATE:      "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── Service / LB ───────────────────────────────────────────────────────────
	// Cilium kube-proxy LB map stale; re-create backends or check EndpointSlice sync.
	flowpb.DropReason_SERVICE_BACKEND_NOT_FOUND: "https://docs.cilium.io/en/stable/operations/troubleshooting/#service-backend-not-found",

	// BPF socket-LB failures.
	flowpb.DropReason_SOCKET_LOOKUP_FAILED: "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_SOCKET_ASSIGN_FAILED: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── Routing / FIB ──────────────────────────────────────────────────────────
	// Missing kernel route / ARP neighbor; routing misconfiguration.
	flowpb.DropReason_FIB_LOOKUP_FAILED: "https://docs.cilium.io/en/stable/operations/troubleshooting/#fib-lookup-failed",

	// Next-hop resolution failure.
	flowpb.DropReason_UNKNOWN_L3_TARGET_ADDRESS: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// Node-level routing gap.
	flowpb.DropReason_LOCAL_HOST_IS_UNREACHABLE: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── Encryption ─────────────────────────────────────────────────────────────
	// WireGuard strict mode: unencrypted traffic blocked.
	flowpb.DropReason_UNENCRYPTED_TRAFFIC: "https://docs.cilium.io/en/stable/operations/encryption/",

	// ── Egress Gateway ─────────────────────────────────────────────────────────
	// Egress gateway policy matched but no gateway node.
	flowpb.DropReason_NO_EGRESS_GATEWAY: "https://docs.cilium.io/en/stable/network/egress-gateway/egress-gateway-troubleshooting/",

	// EgressGateway policy: no IP assigned to gateway interface.
	flowpb.DropReason_DROP_NO_EGRESS_IP: "https://docs.cilium.io/en/stable/network/egress-gateway/egress-gateway-troubleshooting/",

	// ── Layer 2 / MAC ──────────────────────────────────────────────────────────
	flowpb.DropReason_INVALID_SOURCE_MAC:      "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_INVALID_DESTINATION_MAC: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── Layer 3 / IP ───────────────────────────────────────────────────────────
	flowpb.DropReason_INVALID_SOURCE_IP:                  "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_INVALID_PACKET_DROPPED:             "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_UNSUPPORTED_L3_PROTOCOL:            "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_INVALID_IPV6_EXTENSION_HEADER:      "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_IP_FRAGMENTATION_NOT_SUPPORTED:     "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_FIRST_LOGICAL_DATAGRAM_FRAGMENT_NOT_FOUND: "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_ERROR_WHILE_CORRECTING_L3_CHECKSUM: "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_ERROR_WHILE_CORRECTING_L4_CHECKSUM: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── Layer 4 ────────────────────────────────────────────────────────────────
	flowpb.DropReason_UNKNOWN_L4_PROTOCOL:  "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_UNSUPPORTED_L2_PROTOCOL: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── ICMP ───────────────────────────────────────────────────────────────────
	flowpb.DropReason_UNKNOWN_ICMPV4_CODE:    "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_UNKNOWN_ICMPV4_TYPE:    "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_UNKNOWN_ICMPV6_CODE:    "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_UNKNOWN_ICMPV6_TYPE:    "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_FORBIDDEN_ICMPV6_MESSAGE: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── Tunnel / Overlay ───────────────────────────────────────────────────────
	flowpb.DropReason_ERROR_RETRIEVING_TUNNEL_KEY:     "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_ERROR_RETRIEVING_TUNNEL_OPTIONS: "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_INVALID_GENEVE_OPTION:            "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_NO_TUNNEL_OR_ENCAPSULATION_ENDPOINT: "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_ENCAPSULATION_TRAFFIC_IS_PROHIBITED: "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_UNSUPPORTED_PROTOCOL_FOR_DSR_ENCAP: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── BPF / Datapath ─────────────────────────────────────────────────────────
	flowpb.DropReason_MISSED_TAIL_CALL:       "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_ERROR_WRITING_TO_PACKET: "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_INVALID_TC_BUFFER:       "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── NAT ────────────────────────────────────────────────────────────────────
	flowpb.DropReason_FAILED_TO_INSERT_INTO_PROXYMAP:           "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_NO_MAPPING_FOR_NAT_MASQUERADE:            "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_UNSUPPORTED_PROTOCOL_FOR_NAT_MASQUERADE:  "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_SNAT_NO_MAP_FOUND:                        "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_NAT46:                                    "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_NAT64:                                    "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── Rate Limiting / Bandwidth ──────────────────────────────────────────────
	// BPF bandwidth manager rate limit hit; tune bandwidth-manager or check NIC limits.
	flowpb.DropReason_REACHED_EDT_RATE_LIMITING_DROP_HORIZON: "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_DROP_RATE_LIMITED:                      "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── VLAN ───────────────────────────────────────────────────────────────────
	flowpb.DropReason_VLAN_FILTERED: "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_INVALID_VNI:   "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── SRv6 ───────────────────────────────────────────────────────────────────
	flowpb.DropReason_NO_SID:             "https://docs.cilium.io/en/stable/operations/troubleshooting/",
	flowpb.DropReason_MISSING_SRV6_STATE: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── Proxy ──────────────────────────────────────────────────────────────────
	flowpb.DropReason_PROXY_REDIRECTION_NOT_SUPPORTED_FOR_PROTOCOL: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── ClusterMesh ────────────────────────────────────────────────────────────
	flowpb.DropReason_INVALID_CLUSTER_ID: "https://docs.cilium.io/en/stable/operations/troubleshooting/",

	// ── Node identity ──────────────────────────────────────────────────────────
	flowpb.DropReason_NO_NODE_ID: "https://docs.cilium.io/en/stable/operations/troubleshooting/",
}

// RemediationHint returns a Cilium docs URL for the given drop reason.
// Returns "" for non-infra reasons (POLICY, TRANSIENT, NOISE, UNKNOWN).
// URL validity is asserted by hints_test.go.
func RemediationHint(reason flowpb.DropReason) string {
	return dropReasonHint[reason]
}
