package policy

import (
	"fmt"
	"sort"
	"testing"

	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPathTemplateClassifier verifies classifyPathSegment against one
// representative segment per class (L7GEN-02). Classification is pure string
// matching on the raw wire-form segment — no URL decoding, no numeric parsing.
func TestPathTemplateClassifier(t *testing.T) {
	tests := []struct {
		name string
		seg  string
		want segmentClass
	}{
		{"numeric", "123", segNumeric},
		{"numeric no overflow on huge numerals", "99999999999999999999", segNumeric},
		{"hex prefix lowercase", "0x1f", segHexPrefix},
		{"hex run with digit", "3f2a1c", segHexRun},
		{"uuid checked before hex-run", "6ba7b810-9dad-11d1-80b4-00c04fd430c8", segUUID},
		{"version-like stays literal", "v2", segVersion},
		{"word", "users", segWord},
		{"percent-encoded stays word (never decoded)", "Andr%C3%A9", segWord},
		{"uppercase 0x prefix is NOT hex-prefix per R3 masking", "0X1f", segWord},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyPathSegment(tc.seg)
			assert.Equal(t, tc.want, got, "segment %q", tc.seg)
		})
	}
}

// collapsePathSet is a test helper building an L7PathSet from "METHOD path" pairs.
func collapsePathSet(pairs ...string) L7PathSet {
	set := make(L7PathSet)
	for _, p := range pairs {
		var method, path string
		_, _ = fmt.Sscanf(p, "%s %s", &method, &path)
		m, ok := set[method]
		if !ok {
			m = make(map[string]struct{})
			set[method] = m
		}
		m[path] = struct{}{}
	}
	return set
}

// TestCollapseHTTPPaths_NumericTemplate proves the L7GEN-01 tracer: a gate
// passing numeric group collapses to one anchored template; a below-gate
// group keeps its literal anchored rules (L7GEN-04 boundary, never a silent
// widening).
func TestCollapseHTTPPaths_NumericTemplate(t *testing.T) {
	t.Run("above gate collapses to one template", func(t *testing.T) {
		paths := collapsePathSet(
			"GET /api/v1/users/1",
			"GET /api/v1/users/2",
			"GET /api/v1/users/3",
		)
		rules := CollapseHTTPPaths(paths, 2)
		require.Len(t, rules, 1, "exactly one rule per (method, template)")
		assert.Equal(t, "GET", rules[0].Method)
		assert.Equal(t, `^/api/v1/users/[0-9]+$`, rules[0].Path)
	})

	t.Run("below gate keeps literal anchored rules", func(t *testing.T) {
		paths := collapsePathSet(
			"GET /api/v1/users/1",
			"GET /api/v1/users/2",
		)
		rules := CollapseHTTPPaths(paths, 5)
		require.Len(t, rules, 2)
		got := []string{rules[0].Path, rules[1].Path}
		sort.Strings(got)
		assert.Equal(t, []string{`^/api/v1/users/1$`, `^/api/v1/users/2$`}, got)
	})
}

// TestCollapseHTTPPaths_SortsByMethodPath pins the output ordering to the
// same (Method, Path) lexicographic key sortL7Rules uses, so collapsed YAML
// is byte-stable across runs (L7GEN-04).
func TestCollapseHTTPPaths_SortsByMethodPath(t *testing.T) {
	paths := collapsePathSet(
		"POST /api/v1/orders/1",
		"POST /api/v1/orders/2",
		"POST /api/v1/orders/3",
		"GET /api/v1/users/1",
		"GET /api/v1/users/2",
		"GET /api/v1/users/3",
		"GET /api/v1/items/1",
		"GET /api/v1/items/2",
		"GET /api/v1/items/3",
	)
	rules := CollapseHTTPPaths(paths, 2)
	require.Len(t, rules, 3)
	var got []api.PortRuleHTTP
	got = append(got, rules...)
	assert.True(t, sort.SliceIsSorted(got, func(i, j int) bool {
		if got[i].Method != got[j].Method {
			return got[i].Method < got[j].Method
		}
		return got[i].Path < got[j].Path
	}), "output must be sorted by (Method, Path): %v", rules)
	assert.Equal(t, `^/api/v1/items/[0-9]+$`, rules[0].Path)
	assert.Equal(t, `^/api/v1/users/[0-9]+$`, rules[1].Path)
	assert.Equal(t, `^/api/v1/orders/[0-9]+$`, rules[2].Path)
}
