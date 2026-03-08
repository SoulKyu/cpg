package hubble

import (
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFilters_AllNamespaces(t *testing.T) {
	filters := buildFilters(nil, true)

	require.Len(t, filters, 1, "all-namespaces should produce a single filter")
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Empty(t, filters[0].SourcePod, "should not filter by source pod")
	assert.Empty(t, filters[0].DestinationPod, "should not filter by destination pod")
}

func TestBuildFilters_SingleNamespace(t *testing.T) {
	filters := buildFilters([]string{"production"}, false)

	require.Len(t, filters, 2, "single namespace should produce two OR-ed filters")

	// First filter: source pod with namespace prefix
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Equal(t, []string{"production/"}, filters[0].SourcePod)
	assert.Empty(t, filters[0].DestinationPod)

	// Second filter: destination pod with namespace prefix
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[1].Verdict)
	assert.Empty(t, filters[1].SourcePod)
	assert.Equal(t, []string{"production/"}, filters[1].DestinationPod)
}

func TestBuildFilters_MultipleNamespaces(t *testing.T) {
	filters := buildFilters([]string{"prod", "staging"}, false)

	require.Len(t, filters, 2, "multiple namespaces should produce two OR-ed filters")

	expectedPrefixes := []string{"prod/", "staging/"}

	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Equal(t, expectedPrefixes, filters[0].SourcePod)
	assert.Empty(t, filters[0].DestinationPod)

	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[1].Verdict)
	assert.Empty(t, filters[1].SourcePod)
	assert.Equal(t, expectedPrefixes, filters[1].DestinationPod)
}

func TestBuildFilters_EmptyNamespaces(t *testing.T) {
	filters := buildFilters(nil, false)

	require.Len(t, filters, 1, "empty namespaces should behave like all-namespaces")
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Empty(t, filters[0].SourcePod)
	assert.Empty(t, filters[0].DestinationPod)
}
