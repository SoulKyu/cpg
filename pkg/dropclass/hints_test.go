package dropclass_test

import (
	"strings"
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"

	"github.com/SoulKyu/cpg/pkg/dropclass"
)

// TestRemediationHintInfraReasons asserts a known infra reason returns a non-empty
// Cilium docs URL.
func TestRemediationHintInfraReasons(t *testing.T) {
	hint := dropclass.RemediationHint(flowpb.DropReason_CT_MAP_INSERTION_FAILED)
	if hint == "" {
		t.Fatal("RemediationHint(CT_MAP_INSERTION_FAILED) returned empty string")
	}
	if !strings.HasPrefix(hint, "https://docs.cilium.io") {
		t.Errorf("RemediationHint(CT_MAP_INSERTION_FAILED) = %q; want prefix https://docs.cilium.io", hint)
	}
}

// TestRemediationHintNonInfra asserts non-infra reasons return empty string (hints
// are only provided for infra-classified reasons).
func TestRemediationHintNonInfra(t *testing.T) {
	hint := dropclass.RemediationHint(flowpb.DropReason_POLICY_DENIED)
	if hint != "" {
		t.Errorf("RemediationHint(POLICY_DENIED) = %q; want empty string (hints only for infra)", hint)
	}
}

// TestRemediationHintAllInfraHaveURLs has been superseded by M1: only infra reasons
// with actionable deep-link anchors return a URL. Generic troubleshooting page URLs
// (no "#" anchor) are now empty strings so the "Hint:" line is suppressed.
// The updated test below (TestRemediationHintDeepLinkPolicy) enforces the new contract.

// genericTroubleshootingURL is the bare Cilium troubleshooting page that M1
// mandates must NOT appear as a hint value (no actionable deep link).
const genericTroubleshootingURL = "https://docs.cilium.io/en/stable/operations/troubleshooting/"

// TestRemediationHintDeepLinkPolicy verifies M1: no non-empty hint in the
// dropReasonHint map points at the bare generic troubleshooting page.
// Only specific topic pages or anchored URLs (#...) are accepted — they give
// operators an actionable starting point, unlike the generic page.
func TestRemediationHintDeepLinkPolicy(t *testing.T) {
	for code := range flowpb.DropReason_name {
		reason := flowpb.DropReason(code)
		hint := dropclass.RemediationHint(reason)
		if hint == "" {
			continue // empty is fine — no hint surfaced
		}
		// Non-empty hint must NOT be the bare generic troubleshooting page.
		if hint == genericTroubleshootingURL {
			t.Errorf("RemediationHint(%v [code=%d]) = %q; generic-URL entries must return empty (M1)",
				reason, code, hint)
		}
		// Must still be a valid https:// URL.
		if !strings.HasPrefix(hint, "https://") {
			t.Errorf("RemediationHint(%v [code=%d]) = %q; must start with https://", reason, code, hint)
		}
	}
}

// TestRemediationHintGenericURLReturnsEmpty verifies M1: CT_NO_MAP_FOUND previously
// had a generic page URL; after M1 it must return "".
func TestRemediationHintGenericURLReturnsEmpty(t *testing.T) {
	hint := dropclass.RemediationHint(flowpb.DropReason_CT_NO_MAP_FOUND)
	if hint != "" {
		t.Errorf("RemediationHint(CT_NO_MAP_FOUND) = %q; generic-URL entries must return empty after M1", hint)
	}
}

// TestRemediationHintKnownDeepLinks verifies the 6 entries that retain deep links.
func TestRemediationHintKnownDeepLinks(t *testing.T) {
	cases := []flowpb.DropReason{
		flowpb.DropReason_CT_MAP_INSERTION_FAILED,
		flowpb.DropReason_SERVICE_BACKEND_NOT_FOUND,
		flowpb.DropReason_FIB_LOOKUP_FAILED,
		flowpb.DropReason_UNENCRYPTED_TRAFFIC,
		flowpb.DropReason_NO_EGRESS_GATEWAY,
		flowpb.DropReason_DROP_NO_EGRESS_IP,
	}
	for _, reason := range cases {
		hint := dropclass.RemediationHint(reason)
		if hint == "" {
			t.Errorf("RemediationHint(%v) = empty; expected a deep-link URL", reason)
		}
	}
}
