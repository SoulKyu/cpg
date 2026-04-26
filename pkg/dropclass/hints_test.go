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

// TestRemediationHintAllInfraHaveURLs iterates every reason classified as Infra and
// asserts RemediationHint returns a non-empty URL starting with "https://".
func TestRemediationHintAllInfraHaveURLs(t *testing.T) {
	for code := range flowpb.DropReason_name {
		reason := flowpb.DropReason(code)
		if dropclass.Classify(reason) != dropclass.DropClassInfra {
			continue
		}
		hint := dropclass.RemediationHint(reason)
		if hint == "" {
			t.Errorf("RemediationHint(%v [code=%d]) = empty; all infra reasons must have a hint URL", reason, code)
			continue
		}
		if !strings.HasPrefix(hint, "https://") {
			t.Errorf("RemediationHint(%v [code=%d]) = %q; must start with https://", reason, code, hint)
		}
	}
}
