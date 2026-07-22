package main

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/k8s"
	"github.com/SoulKyu/cpg/pkg/output"
)

// TestGetBootstrapPolicy_Success proves the happy path over the in-memory
// MCP transport: a determined, at/above-floor CompatInfo stub yields a
// non-error response whose YAML unmarshals to a Sanitize()-passing CNP with
// enableDefaultDeny set.
func TestGetBootstrapPolicy_Success(t *testing.T) {
	initLoggerForTesting(t)
	withFakeBootstrapDetectVersion(t, k8s.CompatInfo{ClusterVersion: "1.19.4", Source: "pod-images"})

	cs, ctx, cleanup := connectQueryTestClient(t)
	defer cleanup()

	resp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_bootstrap_policy",
		Arguments: map[string]any{"namespace": "demo"},
	})
	require.NoError(t, err)
	require.False(t, resp.IsError, "expected get_bootstrap_policy to succeed for a determined, at/above-floor version")

	var out bootstrapResult
	decodeStructured(t, resp.StructuredContent, &out)
	assert.Equal(t, "demo", out.Namespace)
	assert.NotEmpty(t, out.YAML)
	assert.Equal(t, "1.19.4", out.ClusterVersion)
	assert.Empty(t, out.VersionWarning)

	cnp, err := output.UnmarshalPolicy([]byte(out.YAML))
	require.NoError(t, err)
	require.NotNil(t, cnp.Spec)
	require.NoError(t, cnp.Spec.Sanitize())
	require.NotNil(t, cnp.Spec.EnableDefaultDeny.Ingress)
	assert.True(t, *cnp.Spec.EnableDefaultDeny.Ingress)
	require.NotNil(t, cnp.Spec.EnableDefaultDeny.Egress)
	assert.True(t, *cnp.Spec.EnableDefaultDeny.Egress)
}

// TestGetBootstrapPolicy_MissingNamespace proves the D-16 validate-before-I/O
// path: an empty namespace returns isError before any version detection is
// attempted.
func TestGetBootstrapPolicy_MissingNamespace(t *testing.T) {
	initLoggerForTesting(t)
	withFakeBootstrapDetectVersion(t, k8s.CompatInfo{ClusterVersion: "1.19.4"})

	cs, ctx, cleanup := connectQueryTestClient(t)
	defer cleanup()

	resp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_bootstrap_policy",
		Arguments: map[string]any{"namespace": ""},
	})
	require.NoError(t, err)
	assert.True(t, resp.IsError, "expected an isError for a missing namespace")
}

// TestGetBootstrapPolicy_BelowFloor proves the hard-refusal branch applies
// identically to the MCP surface as the CLI: a determined, below-floor
// CompatInfo stub yields isError with a message naming the version and the
// 1.16 floor.
func TestGetBootstrapPolicy_BelowFloor(t *testing.T) {
	initLoggerForTesting(t)
	withFakeBootstrapDetectVersion(t, k8s.CompatInfo{
		ClusterVersion:     "1.15.0",
		BelowFloorFeatures: []string{"enableDefaultDeny CNP field (requires >= 1.16.0)"},
	})

	cs, ctx, cleanup := connectQueryTestClient(t)
	defer cleanup()

	resp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_bootstrap_policy",
		Arguments: map[string]any{"namespace": "demo"},
	})
	require.NoError(t, err)
	require.True(t, resp.IsError, "expected an isError for a determined, below-floor version")

	require.NotEmpty(t, resp.Content)
	text, ok := resp.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected isError content to be TextContent")
	assert.Contains(t, text.Text, "1.15.0")
	assert.Contains(t, text.Text, "1.16")
}
