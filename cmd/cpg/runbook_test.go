package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunbookNeverSuggestsDaemonWideAudit is the golden pinning test for
// AUD-02 criterion 3 (see
// .planning/phases/22-bootstrap-artifact-generation/22-03-PLAN.md).
//
// It pins two facts about docs/bootstrap-runbook.md so a future edit cannot
// silently regress them:
//
//  1. The runbook's capture step references `cpg generate --include-audit`
//     (the Phase 20 surface) verbatim.
//  2. The hyphenated daemon-wide token "policy-audit-mode" appears ONLY
//     inside the runbook's leading warning block -- the region from the top
//     of the file down to (but not including) the first "## " section
//     heading. A later mention would mean the runbook drifted into
//     suggesting the dangerous daemon-wide shortcut outside its warning
//     against it (Pitfall 4, 22-RESEARCH.md).
//
// Assertions use strings.Contains over the file content directly (not shell
// grep), mirroring readme_compat_test.go's style, so the test runs
// deterministically with no cluster, no build tags, and no external
// process.
func TestRunbookNeverSuggestsDaemonWideAudit(t *testing.T) {
	data, err := os.ReadFile("../../docs/bootstrap-runbook.md")
	require.NoError(t, err, "docs/bootstrap-runbook.md must exist and be readable")
	require.NotEmpty(t, data, "docs/bootstrap-runbook.md must not be empty")
	runbook := string(data)

	assert.Contains(t, runbook, "cpg generate --include-audit",
		"AUD-02 c3: runbook capture step must reference cpg generate --include-audit verbatim")

	lines := strings.Split(runbook, "\n")
	warningEnd := firstSectionHeadingIndex(lines)
	require.Greater(t, warningEnd, 0,
		"runbook must contain a leading warning block followed by at least one '## ' section heading")

	var warningMentions int
	for i, line := range lines {
		if !strings.Contains(line, "policy-audit-mode") {
			continue
		}
		if i >= warningEnd {
			t.Errorf("line %d mentions the daemon-wide token %q outside the leading warning block "+
				"(first '## ' heading is at line %d): %q", i+1, "policy-audit-mode", warningEnd+1, line)
			continue
		}
		warningMentions++
	}

	assert.Greater(t, warningMentions, 0,
		"the leading warning block (before the first '## ' heading) must mention "+
			"'policy-audit-mode' at least once")

	assert.True(t, warningBlockCautionsAgainstDaemonWideAudit(lines[:warningEnd]),
		"the leading warning block must pair 'policy-audit-mode' with a caution word "+
			"(not/never/avoid/danger)")
}

// firstSectionHeadingIndex returns the index of the first line beginning
// with "## " (a top-level markdown section heading), or -1 if none exists.
func firstSectionHeadingIndex(lines []string) int {
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			return i
		}
	}
	return -1
}

// warningBlockCautionsAgainstDaemonWideAudit reports whether the given
// leading-block lines mention "policy-audit-mode" alongside at least one
// caution word, so the warning is not merely a neutral mention.
func warningBlockCautionsAgainstDaemonWideAudit(block []string) bool {
	joined := strings.ToLower(strings.Join(block, "\n"))
	if !strings.Contains(joined, "policy-audit-mode") {
		return false
	}
	for _, caution := range []string{"not", "never", "avoid", "danger"} {
		if strings.Contains(joined, caution) {
			return true
		}
	}
	return false
}
