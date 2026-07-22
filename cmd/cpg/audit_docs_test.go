package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadmeAuditWindowSection is the golden pinning test for AUD-03
// criterion 5 (see
// .planning/phases/23-managed-audit-window-sec-01-evolution/23-04-PLAN.md).
//
// It pins the fact that README.md keeps the MCP server's "readonly, period"
// claim byte-for-byte AND documents `cpg audit-window` as the one scoped,
// lifecycle-bound mutating command with its exclusive RBAC step-up
// (pods/exec create, ciliumendpoints list/watch) -- so a future edit cannot
// silently drop either half of that honest disclosure.
//
// Assertions use strings.Contains over the file content directly (not shell
// grep), mirroring readme_compat_test.go's style, so the test runs
// deterministically with no cluster, no build tags, and no external
// process.
func TestReadmeAuditWindowSection(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	require.NoError(t, err, "README.md must be readable at the repo root")
	readme := string(data)

	assert.Contains(t, readme,
		"cpg never mutates your cluster and never writes outside its own session tmpdir",
		"T-23-11: README must keep the MCP server's readonly claim verbatim")

	assert.Contains(t, readme, "cpg audit-window",
		"AUD-03 c5: README must document cpg audit-window as the mutating command")

	assert.Contains(t, readme, "PolicyAuditMode",
		"AUD-03 c5: README must name the per-endpoint PolicyAuditMode option")

	assert.Contains(t, readme, "pods/exec",
		"AUD-03 c5: README must list the pods/exec RBAC step-up")

	assert.Contains(t, readme, "ciliumendpoints",
		"AUD-03 c5: README must list the ciliumendpoints RBAC step-up")
}
