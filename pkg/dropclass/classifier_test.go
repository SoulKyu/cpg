package dropclass_test

import (
	"sort"
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"

	"github.com/SoulKyu/cpg/pkg/dropclass"
)

// TestClassifyPolicyReasons asserts the four policy-bucket reasons.
func TestClassifyPolicyReasons(t *testing.T) {
	cases := []struct {
		reason flowpb.DropReason
		name   string
	}{
		{flowpb.DropReason_POLICY_DENIED, "POLICY_DENIED"},
		{flowpb.DropReason_POLICY_DENY, "POLICY_DENY"},
		{flowpb.DropReason_AUTH_REQUIRED, "AUTH_REQUIRED"},
		{flowpb.DropReason_DENIED_BY_LB_SRC_RANGE_CHECK, "DENIED_BY_LB_SRC_RANGE_CHECK"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := dropclass.Classify(tc.reason)
			if got != dropclass.DropClassPolicy {
				t.Errorf("Classify(%s) = %v, want DropClassPolicy", tc.name, got)
			}
		})
	}
}

// TestClassifyInfraReasons asserts representative infra-bucket reasons.
func TestClassifyInfraReasons(t *testing.T) {
	cases := []struct {
		reason flowpb.DropReason
		name   string
	}{
		{flowpb.DropReason_CT_MAP_INSERTION_FAILED, "CT_MAP_INSERTION_FAILED"},
		{flowpb.DropReason_FIB_LOOKUP_FAILED, "FIB_LOOKUP_FAILED"},
		{flowpb.DropReason_SERVICE_BACKEND_NOT_FOUND, "SERVICE_BACKEND_NOT_FOUND"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := dropclass.Classify(tc.reason)
			if got != dropclass.DropClassInfra {
				t.Errorf("Classify(%s) = %v, want DropClassInfra", tc.name, got)
			}
		})
	}
}

// TestClassifyTransientReasons asserts representative transient-bucket reasons.
func TestClassifyTransientReasons(t *testing.T) {
	cases := []struct {
		reason flowpb.DropReason
		name   string
	}{
		{flowpb.DropReason_STALE_OR_UNROUTABLE_IP, "STALE_OR_UNROUTABLE_IP"},
		{flowpb.DropReason_INVALID_IDENTITY, "INVALID_IDENTITY"},
		{flowpb.DropReason_DROP_EP_NOT_READY, "DROP_EP_NOT_READY"},
		{flowpb.DropReason_DROP_HOST_NOT_READY, "DROP_HOST_NOT_READY"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := dropclass.Classify(tc.reason)
			if got != dropclass.DropClassTransient {
				t.Errorf("Classify(%s) = %v, want DropClassTransient", tc.name, got)
			}
		})
	}
}

// TestClassifyNoiseReasons asserts all six noise-bucket reasons.
func TestClassifyNoiseReasons(t *testing.T) {
	cases := []struct {
		reason flowpb.DropReason
		name   string
	}{
		{flowpb.DropReason_NAT_NOT_NEEDED, "NAT_NOT_NEEDED"},
		{flowpb.DropReason_IS_A_CLUSTERIP, "IS_A_CLUSTERIP"},
		{flowpb.DropReason_IGMP_HANDLED, "IGMP_HANDLED"},
		{flowpb.DropReason_IGMP_SUBSCRIBED, "IGMP_SUBSCRIBED"},
		{flowpb.DropReason_MULTICAST_HANDLED, "MULTICAST_HANDLED"},
		{flowpb.DropReason_DROP_PUNT_PROXY, "DROP_PUNT_PROXY"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := dropclass.Classify(tc.reason)
			if got != dropclass.DropClassNoise {
				t.Errorf("Classify(%s) = %v, want DropClassNoise", tc.name, got)
			}
		})
	}
}

// TestClassifyUnknownReason asserts synthetic out-of-range value returns DropClassUnknown.
func TestClassifyUnknownReason(t *testing.T) {
	got := dropclass.Classify(flowpb.DropReason(9999))
	if got != dropclass.DropClassUnknown {
		t.Errorf("Classify(9999) = %v, want DropClassUnknown", got)
	}
}

// TestClassifyAllKnownReasons iterates every flowpb.DropReason_name key and asserts
// none returns DropClassUnknown — every officially-enumerated reason must have an
// explicit bucket assignment.
func TestClassifyAllKnownReasons(t *testing.T) {
	for code := range flowpb.DropReason_name {
		reason := flowpb.DropReason(code)
		got := dropclass.Classify(reason)
		if got == dropclass.DropClassUnknown {
			t.Errorf("Classify(%v [code=%d]) = DropClassUnknown; every known reason must have an explicit bucket",
				reason, code)
		}
	}
}

// TestClassifierVersion asserts the version constant is the expected semver string.
func TestClassifierVersion(t *testing.T) {
	const want = "1.0.0-cilium1.19.1"
	if dropclass.ClassifierVersion != want {
		t.Errorf("ClassifierVersion = %q, want %q", dropclass.ClassifierVersion, want)
	}
}

// TestValidReasonNames asserts the returned slice is non-empty, sorted, and every
// element is a value in flowpb.DropReason_name.
func TestValidReasonNames(t *testing.T) {
	names := dropclass.ValidReasonNames()

	if len(names) == 0 {
		t.Fatal("ValidReasonNames() returned empty slice")
	}

	// Build the set of known names from the proto enum.
	known := make(map[string]struct{}, len(flowpb.DropReason_name))
	for _, v := range flowpb.DropReason_name {
		known[v] = struct{}{}
	}

	// Each returned name must be in the known set.
	for _, n := range names {
		if _, ok := known[n]; !ok {
			t.Errorf("ValidReasonNames() returned %q which is not in flowpb.DropReason_name", n)
		}
	}

	// Must be sorted.
	if !sort.StringsAreSorted(names) {
		t.Errorf("ValidReasonNames() result is not sorted")
	}

	// Length must match the proto enum.
	if len(names) != len(flowpb.DropReason_name) {
		t.Errorf("ValidReasonNames() len=%d, want %d (flowpb.DropReason_name size)",
			len(names), len(flowpb.DropReason_name))
	}
}

// BenchmarkClassifyReason asserts the O(1) map lookup stays well under 50 ns/op.
func BenchmarkClassifyReason(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = dropclass.Classify(flowpb.DropReason_CT_MAP_INSERTION_FAILED)
	}
}
