package policy

import (
	"fmt"
	"sort"
	"strings"
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
		{"all-letter hex run stays word", "decade", segWord},
		{"hex run uppercase hex with digit", "ABC123", segHexRun},
		{"hex run too short stays word", "abc1", segWord},
		{"uuid uppercase hex accepted", "6BA7B810-9DAD-11D1-80B4-00C04FD430C8", segUUID},
		{"version-like multi digit", "v12", segVersion},
		{"version-like with suffix stays word", "v1beta", segWord},
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

// TestCollapseHTTPPaths_HexPrefixTemplate proves the 0x-hex class (L7GEN-02
// SC4): a homogeneous 0x-prefixed position generalizes to the POSIX-ERE
// `0x[0-9a-fA-F]+` fragment (lowercase prefix only, per R3 masking).
func TestCollapseHTTPPaths_HexPrefixTemplate(t *testing.T) {
	paths := collapsePathSet(
		"GET /api/v1/orders/0x1f",
		"GET /api/v1/orders/0xa2",
		"GET /api/v1/orders/0xdeadbeef",
	)
	rules := CollapseHTTPPaths(paths, 2)
	require.Len(t, rules, 1)
	assert.Equal(t, `^/api/v1/orders/0x[0-9a-fA-F]+$`, rules[0].Path)
}

// TestCollapseHTTPPaths_HexRunTemplate proves the hex-run class: ≥6 hex chars
// with ≥1 digit generalizes to `[0-9a-fA-F]+`.
func TestCollapseHTTPPaths_HexRunTemplate(t *testing.T) {
	paths := collapsePathSet(
		"GET /api/v1/items/3f2a1c",
		"GET /api/v1/items/9b8c7d",
	)
	rules := CollapseHTTPPaths(paths, 2)
	require.Len(t, rules, 1)
	assert.Equal(t, `^/api/v1/items/[0-9a-fA-F]+$`, rules[0].Path)
}

// TestCollapseHTTPPaths_UUIDTemplate proves the UUID class generalizes to the
// full 8-4-4-4-12 hex class pattern (never a bare `[^/]+` or `.+`).
func TestCollapseHTTPPaths_UUIDTemplate(t *testing.T) {
	paths := collapsePathSet(
		"GET /api/v1/refs/6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		"GET /api/v1/refs/6ba7b811-9dad-11d1-80b4-00c04fd430c9",
	)
	rules := CollapseHTTPPaths(paths, 2)
	require.Len(t, rules, 1)
	assert.Equal(t,
		`^/api/v1/refs/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
		rules[0].Path)
}

// TestCollapseHTTPPaths_VersionStaysLiteral proves version-like variance never
// generalizes (L7GEN-02): `v1`/`v2` at one position yields two separate
// literal rules, never a `v[0-9]`-style template.
func TestCollapseHTTPPaths_VersionStaysLiteral(t *testing.T) {
	paths := collapsePathSet(
		"GET /api/v1/v1",
		"GET /api/v1/v2",
	)
	rules := CollapseHTTPPaths(paths, 2)
	require.Len(t, rules, 2)
	got := []string{rules[0].Path, rules[1].Path}
	sort.Strings(got)
	assert.Equal(t, []string{`^/api/v1/v1$`, `^/api/v1/v2$`}, got)
	for _, r := range rules {
		assert.NotContains(t, r.Path, "v[0-9]", "version segment must never generalize")
	}
}

// TestCollapseHTTPPaths_MixedClassFallsBackToLiteral proves mixed-class
// positions (numeric + word at one position) fall back to literal rules.
func TestCollapseHTTPPaths_MixedClassFallsBackToLiteral(t *testing.T) {
	paths := collapsePathSet(
		"GET /api/v1/mixed/123",
		"GET /api/v1/mixed/abc",
	)
	rules := CollapseHTTPPaths(paths, 2)
	require.Len(t, rules, 2)
	got := []string{rules[0].Path, rules[1].Path}
	sort.Strings(got)
	assert.Equal(t, []string{`^/api/v1/mixed/123$`, `^/api/v1/mixed/abc$`}, got)
}

// TestCollapseHTTPPaths_SingleObservationStaysLiteral proves a single distinct
// observed path never generalizes regardless of the gate (L7GEN-04 empty: a
// group of 1 has no observed variance).
func TestCollapseHTTPPaths_SingleObservationStaysLiteral(t *testing.T) {
	paths := collapsePathSet("GET /api/v1/users/42")
	rules := CollapseHTTPPaths(paths, 1)
	require.Len(t, rules, 1)
	assert.Equal(t, `^/api/v1/users/42$`, rules[0].Path,
		"single distinct path must stay literal even with gate 1 — no variance")
}

// TestCollapseHTTPPaths_EncodedSegmentsStayLiteral proves percent-encoded and
// UTF-8 segments stay literal byte-for-byte (KNOWN_LIMITATIONS #8: never
// decoded before matching) — `%` and every metacharacter are QuoteMeta'd.
func TestCollapseHTTPPaths_EncodedSegmentsStayLiteral(t *testing.T) {
	paths := collapsePathSet(
		"GET /users/Andr%C3%A9",
		"GET /users/Jos%C3%A9",
	)
	rules := CollapseHTTPPaths(paths, 2)
	require.Len(t, rules, 2)
	got := []string{rules[0].Path, rules[1].Path}
	sort.Strings(got)
	assert.Equal(t, []string{`^/users/Andr%C3%A9$`, `^/users/Jos%C3%A9$`}, got,
		"percent-encoding must be preserved byte-for-byte")
}

// TestCollapseHTTPPaths_MetacharactersStayEscaped proves regex metacharacters
// in literal segments adjacent to a generalized position are QuoteMeta-escaped
// in the emitted template.
func TestCollapseHTTPPaths_MetacharactersStayEscaped(t *testing.T) {
	paths := collapsePathSet(
		"GET /api/v1/foo.bar/1",
		"GET /api/v1/foo.bar/2",
	)
	rules := CollapseHTTPPaths(paths, 2)
	require.Len(t, rules, 1)
	assert.Equal(t, `^/api/v1/foo\.bar/[0-9]+$`, rules[0].Path,
		"literal segment metacharacters must remain escaped adjacent to a class fragment")
}

// TestCollapseHTTPPaths_AdjacentSegmentsGeneralizeIndependently proves
// adjacent varying segments generalize independently in one template
// (L7GEN-04 adjacency) and that adjacent mixed classes at one position stay
// literal.
func TestCollapseHTTPPaths_AdjacentSegmentsGeneralizeIndependently(t *testing.T) {
	t.Run("two adjacent class positions in one template", func(t *testing.T) {
		paths := collapsePathSet(
			"GET /api/v1/users/1/orders/3f2a1c",
			"GET /api/v1/users/2/orders/9b8c7d",
		)
		rules := CollapseHTTPPaths(paths, 2)
		require.Len(t, rules, 1)
		assert.Equal(t, `^/api/v1/users/[0-9]+/orders/[0-9a-fA-F]+$`, rules[0].Path)
	})

	t.Run("adjacent mixed classes stay literal", func(t *testing.T) {
		paths := collapsePathSet(
			"GET /api/v1/x/1/2",
			"GET /api/v1/x/a/3",
		)
		rules := CollapseHTTPPaths(paths, 2)
		require.Len(t, rules, 2, "mixed-class position must split the paths into literals")
		got := []string{rules[0].Path, rules[1].Path}
		sort.Strings(got)
		assert.Equal(t, []string{`^/api/v1/x/1/2$`, `^/api/v1/x/a/3$`}, got)
	})
}

// TestCollapseHTTPPaths_AllPositionsVary proves a fully-varying path set
// yields a proper class template — never `[^/]+` or `.+` (POSIX-ERE class
// fragments only, L7GEN-02).
func TestCollapseHTTPPaths_AllPositionsVary(t *testing.T) {
	paths := collapsePathSet(
		"GET /123",
		"GET /456",
	)
	rules := CollapseHTTPPaths(paths, 2)
	require.Len(t, rules, 1)
	assert.Equal(t, `^/[0-9]+$`, rules[0].Path)
	assert.NotContains(t, rules[0].Path, "[^/]")
	assert.NotContains(t, rules[0].Path, ".+")
}

// TestCollapseHTTPPaths_PatternLengthCap proves the T-25-02 control: a group
// whose candidate template pattern exceeds maxL7TemplatePatternLen (256 chars)
// falls back to per-path literal rules instead of emitting an oversized regex.
func TestCollapseHTTPPaths_PatternLengthCap(t *testing.T) {
	longLiteral := strings.Repeat("a", 250)
	paths := collapsePathSet(
		"GET /api/v1/"+longLiteral+"/1",
		"GET /api/v1/"+longLiteral+"/2",
	)
	rules := CollapseHTTPPaths(paths, 2)
	require.Len(t, rules, 2, "over-cap candidate must fall back to literal rules")
	got := []string{rules[0].Path, rules[1].Path}
	sort.Strings(got)
	assert.Equal(t, []string{
		`^/api/v1/` + longLiteral + `/1$`,
		`^/api/v1/` + longLiteral + `/2$`,
	}, got)
	for _, r := range rules {
		assert.Contains(t, r.Path, longLiteral,
			"fallback rules must be the per-path anchored literals")
	}
	// The candidate TEMPLATE pattern (long literal + /[0-9]+ class fragment)
	// would exceed the cap — assert that arithmetic explicitly.
	candidate := "^/api/v1/" + longLiteral + "/[0-9]+$/"
	assert.Greater(t, len(candidate), maxL7TemplatePatternLen,
		"test setup: candidate template must exceed the cap")
}

// TestCollapseHTTPPaths_GateBoundary pins the L7GEN-04 boundary in both
// directions: exactly N distinct paths collapse to the template, N-1 stay
// literal — never a silent widening at the threshold.
func TestCollapseHTTPPaths_GateBoundary(t *testing.T) {
	paths := collapsePathSet(
		"GET /api/v1/orders/1",
		"GET /api/v1/orders/2",
		"GET /api/v1/orders/3",
		"GET /api/v1/orders/4",
		"GET /api/v1/orders/5",
	)

	t.Run("exactly N distinct paths collapse to the template", func(t *testing.T) {
		rules := CollapseHTTPPaths(paths, 5)
		require.Len(t, rules, 1)
		assert.Equal(t, `^/api/v1/orders/[0-9]+$`, rules[0].Path)
	})

	t.Run("N-1 distinct paths stay literal", func(t *testing.T) {
		rules := CollapseHTTPPaths(paths, 6)
		require.Len(t, rules, 5)
		for _, r := range rules {
			assert.NotContains(t, r.Path, "[0-9]+", "below the gate no template may appear")
			assert.True(t, strings.HasPrefix(r.Path, "^/api/v1/orders/") && strings.HasSuffix(r.Path, "$"),
				"expected anchored literal rule, got %q", r.Path)
		}
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
