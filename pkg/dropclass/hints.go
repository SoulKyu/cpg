package dropclass

import flowpb "github.com/cilium/cilium/api/v1/flow"

// dropReasonHint maps every INFRA-classified drop reason to a Cilium docs URL.
// Non-infra reasons are absent — RemediationHint returns "" for them.
// Base URL: https://docs.cilium.io/en/stable/operations/troubleshooting/
// dropReasonHint maps INFRA-classified drop reasons to actionable Cilium docs URLs.
// Only entries with a deep-link anchor ("#...") are included — generic page URLs
// are omitted (M1: empty hint is surfaced as "" by RemediationHint, suppressing the
// "Hint:" line in the cluster-health summary and the remediation field in the JSON).
//
// Entries to KEEP (have deep links):
//   CT_MAP_INSERTION_FAILED, SERVICE_BACKEND_NOT_FOUND, FIB_LOOKUP_FAILED,
//   UNENCRYPTED_TRAFFIC, NO_EGRESS_GATEWAY, DROP_NO_EGRESS_IP.
//
// All other INFRA entries previously pointing at the bare troubleshooting page
// have been removed — RemediationHint returns "" for them, which healthDropJSON
// serializes with omitempty (field absent) and PrintClusterHealthSummary skips
// the "Hint:" line.
var dropReasonHint = map[flowpb.DropReason]string{
	// ── Conntrack ──────────────────────────────────────────────────────────────
	// The triggering production bug: conntrack BPF map full.
	// Fix: raise bpf-ct-global-tcp-max or lower conntrack-gc-interval.
	flowpb.DropReason_CT_MAP_INSERTION_FAILED: "https://docs.cilium.io/en/stable/operations/troubleshooting/#handling-drop-ct-map-insertion-failed",

	// ── Service / LB ───────────────────────────────────────────────────────────
	// Cilium kube-proxy LB map stale; re-create backends or check EndpointSlice sync.
	flowpb.DropReason_SERVICE_BACKEND_NOT_FOUND: "https://docs.cilium.io/en/stable/operations/troubleshooting/#service-backend-not-found",

	// ── Routing / FIB ──────────────────────────────────────────────────────────
	// Missing kernel route / ARP neighbor; routing misconfiguration.
	flowpb.DropReason_FIB_LOOKUP_FAILED: "https://docs.cilium.io/en/stable/operations/troubleshooting/#fib-lookup-failed",

	// ── Encryption ─────────────────────────────────────────────────────────────
	// WireGuard strict mode: unencrypted traffic blocked.
	flowpb.DropReason_UNENCRYPTED_TRAFFIC: "https://docs.cilium.io/en/stable/operations/encryption/",

	// ── Egress Gateway ─────────────────────────────────────────────────────────
	// Egress gateway policy matched but no gateway node.
	flowpb.DropReason_NO_EGRESS_GATEWAY: "https://docs.cilium.io/en/stable/network/egress-gateway/egress-gateway-troubleshooting/",

	// EgressGateway policy: no IP assigned to gateway interface.
	flowpb.DropReason_DROP_NO_EGRESS_IP: "https://docs.cilium.io/en/stable/network/egress-gateway/egress-gateway-troubleshooting/",
}

// RemediationHint returns a Cilium docs URL for the given drop reason.
// Returns "" for non-infra reasons (POLICY, TRANSIENT, NOISE, UNKNOWN).
// URL validity is asserted by hints_test.go.
func RemediationHint(reason flowpb.DropReason) string {
	return dropReasonHint[reason]
}
