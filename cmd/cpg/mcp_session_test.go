package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requiredFields extracts the JSON Schema "required" property-name list
// from a tool's InputSchema. On the client side, go-sdk's mcp.Tool docs
// state InputSchema "will hold the default JSON marshaling of the server's
// input schema" — i.e. a map[string]any after the tools/list round trip,
// with "required" (if present) a []any of property-name strings
// (jsonschema-go's Schema.Required json tag). Returns nil when the schema
// has no required properties (start_session's case: every arg carries
// omitempty).
func requiredFields(t *testing.T, inputSchema any) []string {
	t.Helper()
	schema, ok := inputSchema.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := schema["required"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		require.True(t, ok, "required entries must be strings, got %T", v)
		out = append(out, s)
	}
	return out
}

// decodeStructured re-marshals a CallToolResult.StructuredContent value
// (map[string]any on the client side, per the go-sdk's JSON round trip) into
// out, a pointer to a small local struct whose json tags match the relevant
// subset of session.StartResult/StatusResult fields.
func decodeStructured(t *testing.T, structuredContent any, out any) {
	t.Helper()
	raw, err := json.Marshal(structuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, out))
}

// TestMCPSessionToolsListed proves the composition-root registration
// (Task 1): exactly the 3 session tools are on the wire, and their inferred
// schemas encode D-05's "every start_session arg is optional, session_id is
// always required" contract (Pattern 0) — not just at the Go struct-tag
// level (already covered by unit-level struct inspection), but as actually
// observed by an MCP client over the transport.
func TestMCPSessionToolsListed(t *testing.T) {
	initLoggerForTesting(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientT, drain := startInMemoryMCPSession(ctx)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	require.NoError(t, err)

	toolsResult, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Len(t, toolsResult.Tools, 3, "exactly start_session/get_status/stop_session must be registered")

	byName := make(map[string]*mcp.Tool, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		byName[tool.Name] = tool
	}
	assert.Contains(t, byName, "start_session")
	assert.Contains(t, byName, "get_status")
	assert.Contains(t, byName, "stop_session")

	assert.ElementsMatch(t, []string{"session_id"}, requiredFields(t, byName["get_status"].InputSchema),
		"get_status must require session_id")
	assert.ElementsMatch(t, []string{"session_id"}, requiredFields(t, byName["stop_session"].InputSchema),
		"stop_session must require session_id")
	assert.Empty(t, requiredFields(t, byName["start_session"].InputSchema),
		"start_session must list no required args — every D-05 field carries omitempty")

	cancel()
	drain()
	_ = cs.Close()
}

// TestMCPSessionUnknownIDReturnsIsError proves the SESS-06 error surface
// over the wire: get_status/stop_session against a session_id that was
// never issued must resolve to a well-formed tool-error result (never a
// transport/protocol-level error, never a panic) whose content names the
// exact D-02 phrase "not found or expired". No session is ever started in
// this test — the Manager's slot is nil from construction, so this exercises
// the unknown-id branch of Status/Stop directly.
func TestMCPSessionUnknownIDReturnsIsError(t *testing.T) {
	initLoggerForTesting(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientT, drain := startInMemoryMCPSession(ctx)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	require.NoError(t, err)

	for _, toolName := range []string{"get_status", "stop_session"} {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      toolName,
			Arguments: map[string]any{"session_id": "sess_bogus"},
		})
		// A handler-returned Go error is auto-converted into a tool-error
		// result by the go-sdk (Pattern 0) — it must never surface as a Go
		// error from CallTool itself.
		require.NoError(t, err, "%s: a tool error must not surface as a transport/protocol error", toolName)
		require.True(t, result.IsError, "%s: an unknown session_id must resolve to a tool error", toolName)
		require.Len(t, result.Content, 1, "%s: error result must carry exactly one content block", toolName)
		tc, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok, "%s: error content must be TextContent, got %T", toolName, result.Content[0])
		assert.Contains(t, tc.Text, "not found or expired", "%s: error content must mention D-02's exact phrase", toolName)
	}

	cancel()
	drain()
	_ = cs.Close()
}

// TestMCPSessionLifecycleWiringAndStdoutPurity proves two things end to end
// over the in-memory transport, using the D-07 server-bypass address so no
// kubeconfig/cluster is required:
//
//  1. SESS-05 wiring: start_session creates a session tmpdir that survives
//     get_status (Manager.Start ran, the tmpdir is real); cancelling the
//     server-root ctx (simulating transport death) and draining
//     runMCPServer — which only returns after mgr.Shutdown() completes —
//     removes that tmpdir. This is the first time this cleanup fan-out is
//     exercised through the actual MCP server lifecycle rather than at the
//     pkg/session.Manager unit level (17-03 already proved Shutdown()
//     itself is correct in isolation).
//  2. Pitfall I / the Phase 16 mcpModeStdout() handoff: this is the first
//     place an MCP-mode PipelineConfig is actually constructed and run
//     (Phase 16 registered zero tools and built none), so it is the first
//     test that can catch a regression where the pipeline's human-readable
//     summary leaks onto the real os.Stdout instead of os.Stderr.
func TestMCPSessionLifecycleWiringAndStdoutPurity(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	realStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = realStdout })
	// Deliberately NOT swapping os.Stderr: mcpModeStdout() must route the
	// pipeline's human-readable summary there, and this test needs the real
	// stderr stream undisturbed to make that a meaningful assertion (via the
	// absence of anything on stdout, below).

	initLoggerForTesting(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientT, drain := startInMemoryMCPSession(ctx)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	require.NoError(t, err)

	// D-07 bypass: an explicit (unreachable) server address skips
	// kubeconfig/port-forward entirely, so this test needs no cluster. A
	// short timeout/flush_interval keeps the background pipeline goroutine's
	// eventual dial failure fast and irrelevant to what this test checks.
	startResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "start_session",
		Arguments: map[string]any{
			"server":         "127.0.0.1:1",
			"timeout":        "2s",
			"flush_interval": "1s",
		},
	})
	require.NoError(t, err)
	require.False(t, startResp.IsError, "start_session against the D-07 bypass address must not error")

	var startOut struct {
		SessionID string `json:"session_id"`
	}
	decodeStructured(t, startResp.StructuredContent, &startOut)
	require.NotEmpty(t, startOut.SessionID)

	statusResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_status",
		Arguments: map[string]any{"session_id": startOut.SessionID},
	})
	require.NoError(t, err)
	require.False(t, statusResp.IsError)

	var statusOut struct {
		TmpDir string `json:"tmp_dir"`
	}
	decodeStructured(t, statusResp.StructuredContent, &statusOut)
	require.NotEmpty(t, statusOut.TmpDir)
	require.DirExists(t, statusOut.TmpDir, "Manager.Start must have created the session tmpdir")

	// Simulate transport death: cancel the server-root ctx. drain() blocks
	// on runMCPServer's own return, which only happens AFTER mgr.Shutdown()
	// (SESS-05) has already run to completion inside runMCPServer.
	cancel()
	drain()
	_ = cs.Close()

	require.NoDirExists(t, statusOut.TmpDir, "SESS-05 wiring: Shutdown must remove the retained tmpdir")

	require.NoError(t, w.Close())
	leaked, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Empty(t, leaked, "Pitfall I: mcpModeStdout() must keep the session summary off the real os.Stdout")
}
