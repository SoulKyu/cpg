package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadmeCompatSection is the golden consistency test for COMPAT-01 and
// COMPAT-03 (see .planning/phases/21-cilium-compatibility-matrix-runtime-detection).
//
// It pins two facts about README.md's user-facing Cilium compatibility prose
// so a future edit cannot silently regress them:
//
//  1. COMPAT-01: a "## Supported Cilium versions" section exists, declaring
//     the documented floor (1.14) plus the PR-verified per-feature floor
//     table (1.15 / 1.16 / 1.17 entries and their merged-PR citations).
//  2. COMPAT-03: the proxy-visibility annotation's documented boundary is
//     the correct one (removed from the agent runtime at 1.17, works only
//     through 1.16) — NOT the shipped bug that claimed support "through
//     Cilium 1.19" (verified absent from the entire vendored cilium@v1.19.4
//     module tree).
//
// Assertions use strings.Contains over the file content directly (not shell
// grep) so a stray code comment mentioning these tokens cannot self-invalidate
// the check, and so the test runs deterministically with no cluster, no
// build tags, and no external process.
func TestReadmeCompatSection(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	require.NoError(t, err, "README.md must be readable at the repo root")
	readme := string(data)

	assert.Contains(t, readme, "## Supported Cilium versions",
		"COMPAT-01: README must declare the 'Supported Cilium versions' section")

	for _, tok := range []string{"1.14", "1.15", "1.16", "1.17"} {
		assert.Contains(t, readme, tok,
			"COMPAT-01: README compat section must carry the PR-verified version token %q", tok)
	}

	// T-21-02-01 mitigation: pin the merged-PR citations backing the three
	// version-boundary claims (cilium-dbg rename, enableDefaultDeny,
	// proxy-visibility removal) so an unsourced future edit is caught, not
	// just the bare version numbers.
	for _, pr := range []string{"#28085", "#30572", "#35019"} {
		assert.Contains(t, readme, pr,
			"COMPAT-01: README compat table must cite merged PR %q backing its version claim", pr)
	}

	// AUD-02 c5 (22-03-PLAN.md): the existing enableDefaultDeny/1.16 compat
	// row must stay cross-referenced to `cpg bootstrap` and the runbook it
	// links to, rather than regressing to a bare, uncontextualized PR
	// citation or -- worse -- fragmenting into a duplicate row.
	assert.Contains(t, readme, "docs/bootstrap-runbook.md",
		"AUD-02 c5: README must link docs/bootstrap-runbook.md")
	assert.Contains(t, readme, "cpg bootstrap",
		"AUD-02 c5: README must mention cpg bootstrap alongside its enableDefaultDeny compat row")

	lines := strings.Split(readme, "\n")

	// COMPAT-03 negative guard: the shipped bug paired "proxy-visibility"
	// with "1.19" on the same line (claiming support through Cilium 1.19).
	// No line may ever reintroduce that exact pairing.
	for i, line := range lines {
		if !strings.Contains(line, "proxy-visibility") {
			continue
		}
		assert.NotContains(t, line, "1.19",
			"COMPAT-03 regression: line %d pairs 'proxy-visibility' with '1.19' again: %q", i+1, line)
	}

	// COMPAT-03 positive guard: at least one "proxy-visibility" mention
	// states the real 1.16/1.17 removal boundary within its own prose
	// block (the lines from that mention down to the next blank line).
	assert.True(t, proxyVisibilityBoundaryStated(lines),
		"COMPAT-03: README must state the proxy-visibility removal boundary "+
			"(1.16 or 1.17) in the same paragraph as a 'proxy-visibility' mention")
}

// proxyVisibilityBoundaryStated scans README lines for any occurrence of
// "proxy-visibility", collects the prose block from that line down to the
// next blank line (or end of file), and reports whether at least one such
// block mentions the 1.16 or 1.17 version boundary.
func proxyVisibilityBoundaryStated(lines []string) bool {
	for i, line := range lines {
		if !strings.Contains(line, "proxy-visibility") {
			continue
		}
		var block []string
		for j := i; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				break
			}
			block = append(block, lines[j])
		}
		joined := strings.Join(block, "\n")
		if strings.Contains(joined, "1.16") || strings.Contains(joined, "1.17") {
			return true
		}
	}
	return false
}
