// Package policy — L7 path collapse engine (Phase 25, L7GEN-01/02/04).
//
// This file implements the deterministic, segment-wise HTTP path generalizer
// behind --l7-collapse-paths: observed literal paths are grouped per
// (method, segment count), positions with observed variance AND a homogeneous
// generalizable class (numeric, 0x-hex, hex-run, UUID) become POSIX-ERE
// character-class fragments, and everything else stays literal via
// regexp.QuoteMeta. A confidence gate (min distinct observed paths, default
// DefaultMinFlowsPerL7Rule) keeps below-threshold groups on their literal
// anchored rules — never a silent widening of the permitted set.
//
// Invariants pinned by REQUIREMENTS.md:
//   - Emitted patterns are POSIX-ERE-safe only: [0-9], [0-9a-fA-F],
//     0x[0-9a-fA-F]+, bounded repetition. Never \d, \w, backreferences, or
//     lookarounds (Cilium/Envoy RE2 flavor).
//   - Raw wire-form segments are classified and quoted byte-for-byte; URL
//     decoding NEVER happens before matching (KNOWN_LIMITATIONS #8).
//   - Templates stay anchored ^…$ mirroring anchorPath (pkg/policy/l7.go).
package policy

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/cilium/cilium/pkg/policy/api"
)

// segmentClass classifies a single path segment for generalization.
type segmentClass int

const (
	segWord      segmentClass = iota // anything else — never generalizes
	segNumeric                       // ^[0-9]+$
	segHexPrefix                     // ^0x[0-9a-fA-F]+$ (lowercase prefix only, per R3 masking)
	segHexRun                        // ^[0-9a-fA-F]{6,}$ with ≥1 digit (all-letter runs stay words)
	segUUID                          // canonical 8-4-4-4-12 hex UUID
	segVersion                       // ^v[0-9]+$ — recognized but deliberately never generalizes
)

// Segment classifiers compiled once at package level. Order of application in
// classifyPathSegment: UUID → 0x-hex → numeric → hex-run → version → word.
var (
	uuidSegmentRe      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	hexPrefixSegmentRe = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)
	numericSegmentRe   = regexp.MustCompile(`^[0-9]+$`)
	hexRunSegmentRe    = regexp.MustCompile(`^[0-9a-fA-F]{6,}$`)
	versionSegmentRe   = regexp.MustCompile(`^v[0-9]+$`)
)

// classifyPathSegment maps a raw wire-form path segment to its class.
// Pure string matching on the raw segment: no URL decoding and no numeric
// parsing, so arbitrarily large numerals still classify as segNumeric with
// no overflow concerns.
func classifyPathSegment(seg string) segmentClass {
	switch {
	case uuidSegmentRe.MatchString(seg):
		return segUUID
	case hexPrefixSegmentRe.MatchString(seg):
		return segHexPrefix
	case numericSegmentRe.MatchString(seg):
		return segNumeric
	// Hex-run guard: an all-letter hex run like "decade" must stay a word —
	// require at least one digit (R3 masking precedent for hex runs).
	case hexRunSegmentRe.MatchString(seg) && strings.ContainsAny(seg, "0123456789"):
		return segHexRun
	case versionSegmentRe.MatchString(seg):
		return segVersion
	default:
		return segWord
	}
}

// generalizable reports whether the class may become a character-class
// fragment in a template. segVersion and segWord are recognized but NEVER
// generalize (L7GEN-02: version-like and word-like segments stay literal).
func (c segmentClass) generalizable() bool {
	switch c {
	case segNumeric, segHexPrefix, segHexRun, segUUID:
		return true
	default:
		return false
	}
}

// patternFragment returns the POSIX-ERE character-class fragment for a
// generalizable class. Undefined for segWord/segVersion (callers must check
// generalizable() first).
func (c segmentClass) patternFragment() string {
	switch c {
	case segNumeric:
		return "[0-9]+"
	case segHexPrefix:
		return "0x[0-9a-fA-F]+"
	case segHexRun:
		return "[0-9a-fA-F]+"
	case segUUID:
		return "[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"
	default:
		return ""
	}
}

// L7PathSet holds raw literal observed paths (wire form, never decoded)
// grouped by normalized HTTP method.
type L7PathSet map[string]map[string]struct{}

// L7CollapseState is the Aggregator-owned whole-run observation store that
// solves the flush-window problem: each flush window's raw path observations
// land in Collected during BuildPolicy, and Absorb() folds them into Retained
// (the whole-run set) after the flush emits. Collapse is computed at every
// flush over Retained ∪ Collected, so the first flush after the gate is
// crossed already emits the authoritative whole-run set.
//
// Keyed opaquely by collapseObsKey(direction, peerKey, portProtoKey).
// Single-goroutine contract: mutated and read only inside the Aggregator's
// flush path (no locking by design; L7GEN-01 concurrency invariant).
type L7CollapseState struct {
	Retained  map[string]L7PathSet
	Collected map[string]L7PathSet
}

// NewL7CollapseState allocates a ready-to-use collapse state.
func NewL7CollapseState() *L7CollapseState {
	return &L7CollapseState{
		Retained:  make(map[string]L7PathSet),
		Collected: make(map[string]L7PathSet),
	}
}

// observe records one raw literal path observation into the window set.
func (s *L7CollapseState) observe(obsKey, method, rawPath string) {
	c, ok := s.Collected[obsKey]
	if !ok {
		c = make(L7PathSet)
		s.Collected[obsKey] = c
	}
	m, ok := c[method]
	if !ok {
		m = make(map[string]struct{})
		c[method] = m
	}
	m[rawPath] = struct{}{}
}

// Absorb folds the window's Collected observations into Retained and clears
// Collected. Called by the Aggregator after each flush's per-bucket emission
// loop, on the single aggregator goroutine.
func (s *L7CollapseState) Absorb() {
	for obsKey, collected := range s.Collected {
		retained, ok := s.Retained[obsKey]
		if !ok {
			retained = make(L7PathSet)
			s.Retained[obsKey] = retained
		}
		for method, paths := range collected {
			m, ok := retained[method]
			if !ok {
				m = make(map[string]struct{})
				retained[method] = m
			}
			for p := range paths {
				m[p] = struct{}{}
			}
		}
		delete(s.Collected, obsKey)
	}
}

// collapseObsKey builds the opaque observation-store key for one
// (direction, peer, port/proto) collapse group.
func collapseObsKey(direction, peerKey, portProtoKey string) string {
	return direction + "|" + peerKey + "|" + portProtoKey
}

// DefaultMinFlowsPerL7Rule is the conservative default confidence gate: the
// minimum number of DISTINCT observed HTTP paths backing a collapsed template
// (L7GEN-04; the metric is distinct paths even though the flag name says
// "flows" — the requirement text is authoritative).
const DefaultMinFlowsPerL7Rule = 5

// ResolveMinFlowsPerL7Rule resolves the gate fail-closed: values < 1 map to
// DefaultMinFlowsPerL7Rule so a zero can never disable the gate (T-25-03).
func ResolveMinFlowsPerL7Rule(n int) int {
	if n < 1 {
		return DefaultMinFlowsPerL7Rule
	}
	return n
}

// maxL7TemplatePatternLen caps candidate template pattern length. A group
// whose pattern exceeds the cap falls back to literal anchored rules for that
// group (T-25-02: Envoy RE2 program-size NACK control, cilium#41314).
const maxL7TemplatePatternLen = 256

// MergeOptions controls merge reconciliation behavior.
type MergeOptions struct {
	// ReplaceL7HTTP, when true, makes each matching (port, proto) bucket's
	// incoming HTTP sub-list REPLACE the existing HTTP sub-list instead of
	// union-merging. Set on every collapse-enabled flush (Decision 1): the
	// emitted HTTP set is whole-run-authoritative and must supersede literal
	// rules written by earlier flushes whose dedup keys differ from the
	// template. DNS sub-lists are never replaced.
	ReplaceL7HTTP bool
}

// collapseGroup is one (segment-count) group of observed paths for a method.
type collapseGroup struct {
	paths []string
}

// splitPathSegments splits a raw path into its "/" segments (leading slash
// stripped). The empty path yields one empty segment, mirroring anchorPath's
// "/" normalization.
func splitPathSegments(p string) []string {
	return strings.Split(strings.TrimPrefix(p, "/"), "/")
}

// literalAnchoredRule returns the anchored literal regex for a raw path,
// mirroring anchorPath: empty path → "^/$", otherwise QuoteMeta + ^…$.
func literalAnchoredRule(rawPath string) string {
	if rawPath == "" {
		return "^/$"
	}
	return "^" + regexp.QuoteMeta(rawPath) + "$"
}

// rawPathFromURL returns the PATH COMPONENT OF THE RAW WIRE FORM of rawURL,
// stripping scheme/host/query/fragment WITHOUT percent-decoding (mirrors
// pathFromURL's stripping contract but keeps %XX escapes byte-for-byte).
//
// KNOWN_LIMITATIONS #8 invariant (L7GEN-02): classification, QuoteMeta, and
// collapse operate on the wire-form path — "%C3%A9" is never decoded before
// matching. The non-collapse path (pathFromURL/anchorPath in l7.go) keeps its
// historical decoded behavior, untouched per plan verification; collapse
// emission is computed from the raw observations collected here.
func rawPathFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err == nil {
		// EscapedPath returns the RawPath (wire form) when it is a valid
		// encoding of u.Path — no percent-decoding, unlike u.Path.
		return u.EscapedPath()
	}
	// Defensive fallback: manually trim query/fragment.
	if i := strings.IndexByte(rawURL, '#'); i >= 0 {
		rawURL = rawURL[:i]
	}
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		rawURL = rawURL[:i]
	}
	return rawURL
}

// CollapseHTTPPaths generalizes one observation set (method → raw literal
// paths) into Cilium PortRuleHTTP entries:
//
//   - Group per method by segment count.
//   - A segment position is a "class position" iff it has ≥2 distinct values
//     AND every value shares one generalizable class.
//   - Paths cluster by their literal-position values; a cluster emits one
//     template iff it has ≥1 class position, holds ≥ minDistinct distinct
//     paths, and its candidate pattern fits maxL7TemplatePatternLen.
//   - Every path whose cluster does not emit a template becomes a literal
//     anchored rule — collapse never reduces the permitted set below what was
//     observed.
//
// Output is sorted by (Method, Path) lexicographic — the same key sortL7Rules
// uses — so collapsed YAML is byte-stable across runs.
func CollapseHTTPPaths(paths L7PathSet, minDistinct int) []api.PortRuleHTTP {
	var out []api.PortRuleHTTP
	methods := make([]string, 0, len(paths))
	for method := range paths {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	for _, method := range methods {
		out = append(out, collapseMethodPaths(method, paths[method], minDistinct)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// collapseMethodPaths applies the grouping algorithm to one method's raw path
// set. See CollapseHTTPPaths for the algorithm contract.
func collapseMethodPaths(method string, pathSet map[string]struct{}, minDistinct int) []api.PortRuleHTTP {
	// Group paths by segment count.
	bySegCount := make(map[int]*collapseGroup)
	var segCounts []int
	for p := range pathSet {
		n := len(splitPathSegments(p))
		g, ok := bySegCount[n]
		if !ok {
			g = &collapseGroup{}
			bySegCount[n] = g
			segCounts = append(segCounts, n)
		}
		g.paths = append(g.paths, p)
	}
	sort.Ints(segCounts)

	var out []api.PortRuleHTTP
	for _, n := range segCounts {
		out = append(out, collapseSegmentCountGroup(method, bySegCount[n].paths, minDistinct)...)
	}
	return out
}

// collapseSegmentCountGroup classifies positions within one segment-count
// group, clusters paths by literal-position values, and emits templates or
// literal fallback rules.
func collapseSegmentCountGroup(method string, groupPaths []string, minDistinct int) []api.PortRuleHTTP {
	sort.Strings(groupPaths) // deterministic cluster construction

	segs := make([][]string, len(groupPaths))
	for i, p := range groupPaths {
		segs[i] = splitPathSegments(p)
	}
	numSegs := len(segs[0])

	// Position analysis: a position is a class position iff it has ≥2 distinct
	// values AND all values share one generalizable class.
	classAt := make([]bool, numSegs)
	classFrag := make([]string, numSegs)
	for pos := 0; pos < numSegs; pos++ {
		values := make(map[string]struct{})
		for _, s := range segs {
			values[s[pos]] = struct{}{}
		}
		if len(values) < 2 {
			continue // single-observation positions stay literal
		}
		first := segs[0][pos]
		cls := classifyPathSegment(first)
		if !cls.generalizable() {
			continue
		}
		homogeneous := true
		for v := range values {
			if classifyPathSegment(v) != cls {
				homogeneous = false
				break
			}
		}
		if homogeneous {
			classAt[pos] = true
			classFrag[pos] = cls.patternFragment()
		}
	}

	hasClassPos := false
	for _, c := range classAt {
		if c {
			hasClassPos = true
			break
		}
	}

	// No class positions anywhere → every path stays literal (no variance to
	// generalize; a group of single-observation positions never widens).
	if !hasClassPos {
		out := make([]api.PortRuleHTTP, 0, len(groupPaths))
		for _, p := range groupPaths {
			out = append(out, api.PortRuleHTTP{Method: method, Path: literalAnchoredRule(p)})
		}
		return out
	}

	// Cluster paths by their literal-position values.
	type cluster struct {
		pattern string
		paths   []string
	}
	clusters := make(map[string]*cluster)
	var clusterOrder []string
	for i, p := range groupPaths {
		litSegs := make([]string, numSegs)
		for pos := 0; pos < numSegs; pos++ {
			if classAt[pos] {
				litSegs[pos] = classFrag[pos]
			} else {
				litSegs[pos] = regexp.QuoteMeta(segs[i][pos])
			}
		}
		pattern := "^/" + strings.Join(litSegs, "/") + "$"
		sig := strings.Join(litSegs, "\x00")
		c, ok := clusters[sig]
		if !ok {
			c = &cluster{pattern: pattern}
			clusters[sig] = c
			clusterOrder = append(clusterOrder, sig)
		}
		c.paths = append(c.paths, p)
	}

	var out []api.PortRuleHTTP
	for _, sig := range clusterOrder {
		c := clusters[sig]
		// Gate (L7GEN-04) + pattern length cap (T-25-02): below the threshold
		// or over the cap, the group's literal rules are kept instead.
		if len(c.paths) >= minDistinct && len(c.pattern) <= maxL7TemplatePatternLen {
			out = append(out, api.PortRuleHTTP{Method: method, Path: c.pattern})
			continue
		}
		for _, p := range c.paths {
			out = append(out, api.PortRuleHTTP{Method: method, Path: literalAnchoredRule(p)})
		}
	}
	return out
}
