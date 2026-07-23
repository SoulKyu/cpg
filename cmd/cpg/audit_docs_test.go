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
// It pins the fact that docs/mcp-server.md keeps the MCP server's
// "readonly, period" claim byte-for-byte AND docs/security.md documents
// `cpg audit-window` as the one scoped, lifecycle-bound mutating command
// with its exclusive RBAC step-up (pods/exec create, ciliumendpoints
// list/watch) -- so a future edit cannot silently drop either half of that
// honest disclosure. (Originally pinned against README.md; the prose moved
// to docs/ when the README was slimmed down to an overview.)
//
// Assertions use strings.Contains over the file content directly (not shell
// grep), mirroring readme_compat_test.go's style, so the test runs
// deterministically with no cluster, no build tags, and no external
// process.
func TestReadmeAuditWindowSection(t *testing.T) {
	mcpData, err := os.ReadFile("../../docs/mcp-server.md")
	require.NoError(t, err, "docs/mcp-server.md must exist and be readable")

	assert.Contains(t, string(mcpData),
		"cpg never mutates your cluster and never writes outside its own session tmpdir",
		"T-23-11: docs/mcp-server.md must keep the MCP server's readonly claim verbatim")

	secData, err := os.ReadFile("../../docs/security.md")
	require.NoError(t, err, "docs/security.md must exist and be readable")
	security := string(secData)

	assert.Contains(t, security, "cpg audit-window",
		"AUD-03 c5: security model must document cpg audit-window as the mutating command")

	assert.Contains(t, security, "PolicyAuditMode",
		"AUD-03 c5: security model must name the per-endpoint PolicyAuditMode option")

	assert.Contains(t, security, "pods/exec",
		"AUD-03 c5: security model must list the pods/exec RBAC step-up")

	assert.Contains(t, security, "ciliumendpoints",
		"AUD-03 c5: security model must list the ciliumendpoints RBAC step-up")
}

// TestRunbookAuditWindowStep is the golden pinning test for AUD-03
// criterion 5's runbook half. It pins that docs/bootstrap-runbook.md wires
// the real `cpg audit-window --ttl` command (not the old manual kubectl-exec
// steps), documents the new-endpoint race honestly, and mentions the
// pods/exec RBAC step-up it needs.
//
// This test does NOT touch TestRunbookNeverSuggestsDaemonWideAudit's
// existing assertions or helpers -- it only reads the same file
// independently, so both tests stay green side by side.
func TestRunbookAuditWindowStep(t *testing.T) {
	data, err := os.ReadFile("../../docs/bootstrap-runbook.md")
	require.NoError(t, err, "docs/bootstrap-runbook.md must exist and be readable")
	runbook := string(data)

	assert.Contains(t, runbook, "cpg audit-window",
		"AUD-03 c5: runbook must wire the real cpg audit-window command")

	assert.Contains(t, runbook, "--ttl",
		"AUD-03 c5: runbook must show the --ttl flag")

	assert.Contains(t, runbook, "race",
		"AUD-03 c5: runbook must honestly document the new-endpoint race window")

	assert.Contains(t, runbook, "pods/exec",
		"AUD-03 c5: runbook must mention the pods/exec RBAC step-up")
}
