package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wantToolCount pins SKL success criterion 5's tool-count tripwire. Bump
// this ONLY alongside a full .claude/skills/ + .claude/agents/cpg-operator.md
// sweep for the new tool.
const wantToolCount = 9

// toolNameToken matches backtick-quoted, verb-prefixed snake_case
// identifiers — every one of the 9 current MCP tool names starts with
// start_/get_/stop_/list_ (see 24-RESEARCH.md "MCP Tool Reference"). Skill
// authors are required (by this test) to always backtick-quote tool names
// in prose, mirroring the convention already visible in README.md's MCP
// tool table. Deliberately narrow so it excludes arg-name prose like
// all_namespaces.
var toolNameToken = regexp.MustCompile("`((?:start|get|stop|list)_[a-z_]+)`")

// TestSkillsConsistencyTripwire is the compiled mitigation for the exact
// drift Phase 24 targets: a future MCP tool added without a corresponding
// skill/agent/README sweep. It enumerates the live tool registry (never a
// hardcoded list), then cross-checks it against the six markdown artifacts
// authored in plan 24-01.
func TestSkillsConsistencyTripwire(t *testing.T) {
	registry := liveToolRegistry(t)
	require.Len(t, registry, wantToolCount,
		"MCP tool count changed — sweep .claude/skills/cpg-*/SKILL.md and "+
			".claude/agents/cpg-operator.md before bumping this pin")

	skillPaths, err := filepath.Glob("../../.claude/skills/cpg-*/SKILL.md")
	require.NoError(t, err)
	require.NotEmpty(t, skillPaths, "expected at least one cpg-* skill (mislocated dir?)")
	agentPath := "../../.claude/agents/cpg-operator.md"

	allPaths := append(append([]string{}, skillPaths...), agentPath)
	coveredBySmoke := map[string]bool{}

	for _, path := range allPaths {
		data, err := os.ReadFile(path)
		require.NoError(t, err, "must be able to read %s", path)
		tokens := toolNameToken.FindAllStringSubmatch(string(data), -1)
		for _, m := range tokens {
			name := m[1]
			assert.True(t, registry[name],
				"%s references phantom tool %q — not in the live %d-tool registry", path, name, wantToolCount)
			if strings.Contains(path, "cpg-mcp-smoke") {
				coveredBySmoke[name] = true
			}
		}
	}

	for name := range registry {
		assert.True(t, coveredBySmoke[name],
			"cpg-mcp-smoke/SKILL.md must mention every registered tool (coverage floor); missing %q", name)
	}

	readmeData, err := os.ReadFile("../../README.md")
	require.NoError(t, err, "README.md must be readable at the repo root")
	readme := string(readmeData)

	assert.Contains(t, readme, "## Agent tooling",
		"README must declare the 'Agent tooling' section")

	for _, skill := range []string{
		"cpg-triage",
		"cpg-audit-onboard",
		"cpg-policy-review",
		"cpg-health-report",
		"cpg-mcp-smoke",
	} {
		assert.Contains(t, readme, skill,
			"README '## Agent tooling' section must list skill %q", skill)
	}
}

// liveToolRegistry enumerates the exact tool set runMCPServer registers, via
// the same in-memory harness mcp_harness_test.go/mcp_e2e_test.go already
// use — never a hardcoded literal list.
func liveToolRegistry(t *testing.T) map[string]bool {
	t.Helper()
	initLoggerForTesting(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientTransport, drain := startInMemoryMCPSession(ctx)
	defer drain()

	client := mcp.NewClient(&mcp.Implementation{Name: "skills-tripwire", Version: "0.0.0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	toolsResult, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)

	registry := make(map[string]bool, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		registry[tool.Name] = true
	}
	return registry
}
