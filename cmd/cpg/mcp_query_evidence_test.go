package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

// evidenceTestNS/evidenceTestWorkload are the fixed (namespace, workload)
// pair every get_evidence fixture in this file uses.
const (
	evidenceTestNS       = "prod"
	evidenceTestWorkload = "api"
)

// buildEvidenceFixture returns a hand-built PolicyEvidence document with 3
// rules deliberately chosen so exactly ONE rule matches each get_evidence
// filter field (D-10's full filter surface), giving one assertion per field
// against a single shared fixture:
//
//   - rule 0 (index 0): ingress, port 8080, endpoint peer app=client — the
//     only rule matching port=8080 and peer=app=client.
//   - rule 1 (index 1): egress, port 53, CIDR peer 10.0.0.5/32, DNS L7 — the
//     only rule matching peer_cidr=10.0.0.0/24 and dns_pattern=example.com.
//   - rule 2 (index 2): ingress, port 9090, endpoint peer app=metrics, HTTP
//     L7 — the only rule matching http_method=GET and http_path=/health.
//
// direction=ingress matches rules 0 and 2 (excludes rule 1).
func buildEvidenceFixture(ns, workload string) evidence.PolicyEvidence {
	now := time.Now()
	return evidence.PolicyEvidence{
		SchemaVersion: evidence.SchemaVersion,
		Policy:        evidence.PolicyRef{Name: "cpg-" + workload, Namespace: ns, Workload: workload},
		Sessions: []evidence.SessionInfo{{
			ID:            "sess-1",
			StartedAt:     now.Add(-time.Hour),
			EndedAt:       now,
			CPGVersion:    "test",
			Source:        evidence.SourceInfo{Type: "live"},
			FlowsIngested: 10,
		}},
		Rules: []evidence.RuleEvidence{
			{
				Key:       "ingress-8080-client",
				Direction: "ingress",
				Peer:      evidence.PeerRef{Type: "endpoint", Labels: map[string]string{"app": "client"}},
				Port:      "8080",
				Protocol:  "tcp",
				FlowCount: 5,
				FirstSeen: now.Add(-time.Hour),
				LastSeen:  now,
				Samples: []evidence.FlowSample{{
					Time:     now,
					Src:      evidence.FlowEndpoint{Namespace: ns, Workload: "client"},
					Dst:      evidence.FlowEndpoint{Namespace: ns, Workload: workload},
					Port:     8080,
					Protocol: "tcp",
					Verdict:  "DROPPED",
				}},
			},
			{
				Key:       "egress-53-cidr",
				Direction: "egress",
				Peer:      evidence.PeerRef{Type: "cidr", CIDR: "10.0.0.5/32"},
				Port:      "53",
				Protocol:  "udp",
				L7:        &evidence.L7Ref{Protocol: "dns", DNSMatchName: "example.com"},
				FlowCount: 3,
				FirstSeen: now.Add(-time.Hour),
				LastSeen:  now,
			},
			{
				Key:       "ingress-9090-metrics",
				Direction: "ingress",
				Peer:      evidence.PeerRef{Type: "endpoint", Labels: map[string]string{"app": "metrics"}},
				Port:      "9090",
				Protocol:  "tcp",
				L7:        &evidence.L7Ref{Protocol: "http", HTTPMethod: "GET", HTTPPath: "/health"},
				FlowCount: 2,
				FirstSeen: now.Add(-time.Hour),
				LastSeen:  now,
			},
		},
	}
}

// writeEvidenceFixture marshals pe at the exact path get_evidence's handler
// derives (evidence/<hash>/<ns>/<workload>.json, hash =
// evidence.HashOutputDir(<tmpDir>/policies)) — mirroring
// mcp_query_tools_test.go's writeClusterHealthFixture pattern so this test
// seeds a real fixture without a real pipeline.
func writeEvidenceFixture(t *testing.T, tmpDir, ns, workload string, pe evidence.PolicyEvidence) string {
	t.Helper()
	outputHash := evidence.HashOutputDir(filepath.Join(tmpDir, "policies"))
	dir := filepath.Join(tmpDir, "evidence", outputHash, ns)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, workload+".json")
	data, err := json.MarshalIndent(pe, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// seedEvidenceQuerySession starts one D-07 bypass session and seeds
// buildEvidenceFixture's 3-rule PolicyEvidence at evidenceTestNS/
// evidenceTestWorkload, returning the connected client session and the
// session_id every subtest calls get_evidence with.
func seedEvidenceQuerySession(t *testing.T) (cs *mcp.ClientSession, ctx context.Context, cleanup func(), sessionID string) {
	t.Helper()
	cs, ctx, cleanup = connectQueryTestClient(t)
	sessionID, tmpDir := startBypassSession(t, ctx, cs, "5s")
	writeEvidenceFixture(t, tmpDir, evidenceTestNS, evidenceTestWorkload, buildEvidenceFixture(evidenceTestNS, evidenceTestWorkload))
	return cs, ctx, cleanup, sessionID
}

// getEvidenceOut mirrors get_evidence's getEvidenceResult JSON shape for
// decodeStructured round-tripping — MatchedRules decodes straight into
// []evidence.RuleEvidence, which doubles as this test's proof that the
// per-record shape is exactly evidence.RuleEvidence (QRY-03's "identical to
// cpg explain --output json" contract), not a parallel MCP-only shape.
type getEvidenceOut struct {
	Policy       evidence.PolicyRef      `json:"policy"`
	Sessions     []evidence.SessionInfo  `json:"sessions"`
	MatchedRules []evidence.RuleEvidence `json:"matched_rules"`
	TotalCount   int                     `json:"total_count"`
	HasMore      bool                    `json:"has_more"`
	NextCursor   string                  `json:"next_cursor"`
}

// callGetEvidence invokes the get_evidence tool and, on a non-error result,
// decodes structuredContent into getEvidenceOut. Callers that expect an
// isError result should call cs.CallTool directly instead (see the
// malformed/unknown/invalid-cursor subtests below), since decoding an error
// result's (absent) structuredContent is not meaningful.
func callGetEvidence(t *testing.T, ctx context.Context, cs *mcp.ClientSession, args map[string]any) (*mcp.CallToolResult, getEvidenceOut) {
	t.Helper()
	resp, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_evidence", Arguments: args})
	require.NoError(t, err, "a tool error must not surface as a transport/protocol error")
	var out getEvidenceOut
	if !resp.IsError {
		decodeStructured(t, resp.StructuredContent, &out)
	}
	return resp, out
}

// TestMCPQueryGetEvidence proves QRY-03/D-10 end to end over the in-memory
// transport: per-record shape parity with pkg/explain.Output, pagination
// over the matched-rule set, one narrowing assertion per filter field, and
// the D-16 actionable-error contract (malformed peer, unknown target,
// invalid cursor).
func TestMCPQueryGetEvidence(t *testing.T) {
	initLoggerForTesting(t)

	t.Run("shape_and_unfiltered", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		resp, out := callGetEvidence(t, ctx, cs, map[string]any{
			"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
		})
		require.False(t, resp.IsError)
		assert.Equal(t, "cpg-"+evidenceTestWorkload, out.Policy.Name)
		assert.Equal(t, evidenceTestNS, out.Policy.Namespace)
		require.Len(t, out.Sessions, 1)
		require.Len(t, out.MatchedRules, 3)
		assert.Equal(t, 3, out.TotalCount)
		assert.False(t, out.HasMore)
		assert.Empty(t, out.NextCursor)

		r0 := out.MatchedRules[0]
		assert.Equal(t, "ingress-8080-client", r0.Key)
		assert.Equal(t, "ingress", r0.Direction)
		assert.Equal(t, "8080", r0.Port)
		assert.Equal(t, "endpoint", r0.Peer.Type)
		assert.Equal(t, "client", r0.Peer.Labels["app"])
		require.Len(t, r0.Samples, 1)
	})

	t.Run("pagination", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		resp1, out1 := callGetEvidence(t, ctx, cs, map[string]any{
			"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
			"limit": 1,
		})
		require.False(t, resp1.IsError)
		require.Len(t, out1.MatchedRules, 1)
		assert.Equal(t, "ingress-8080-client", out1.MatchedRules[0].Key)
		assert.True(t, out1.HasMore)
		assert.NotEmpty(t, out1.NextCursor)
		assert.Equal(t, 3, out1.TotalCount)

		resp2, out2 := callGetEvidence(t, ctx, cs, map[string]any{
			"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
			"limit": 1, "cursor": out1.NextCursor,
		})
		require.False(t, resp2.IsError)
		require.Len(t, out2.MatchedRules, 1)
		assert.Equal(t, "egress-53-cidr", out2.MatchedRules[0].Key)
		assert.True(t, out2.HasMore)
		assert.Equal(t, 3, out2.TotalCount)
	})

	t.Run("direction_filter", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		_, out := callGetEvidence(t, ctx, cs, map[string]any{
			"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
			"direction": "ingress",
		})
		require.Len(t, out.MatchedRules, 2)
		for _, r := range out.MatchedRules {
			assert.Equal(t, "ingress", r.Direction)
		}
	})

	t.Run("port_filter", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		_, out := callGetEvidence(t, ctx, cs, map[string]any{
			"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
			"port": "8080",
		})
		require.Len(t, out.MatchedRules, 1)
		assert.Equal(t, "ingress-8080-client", out.MatchedRules[0].Key)
	})

	t.Run("peer_filter", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		_, out := callGetEvidence(t, ctx, cs, map[string]any{
			"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
			"peer": "app=client",
		})
		require.Len(t, out.MatchedRules, 1)
		assert.Equal(t, "ingress-8080-client", out.MatchedRules[0].Key)
	})

	t.Run("peer_cidr_filter", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		_, out := callGetEvidence(t, ctx, cs, map[string]any{
			"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
			"peer_cidr": "10.0.0.0/24",
		})
		require.Len(t, out.MatchedRules, 1)
		assert.Equal(t, "egress-53-cidr", out.MatchedRules[0].Key)
	})

	t.Run("http_method_filter", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		_, out := callGetEvidence(t, ctx, cs, map[string]any{
			"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
			"http_method": "GET",
		})
		require.Len(t, out.MatchedRules, 1)
		assert.Equal(t, "ingress-9090-metrics", out.MatchedRules[0].Key)
	})

	t.Run("http_path_filter", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		_, out := callGetEvidence(t, ctx, cs, map[string]any{
			"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
			"http_path": "/health",
		})
		require.Len(t, out.MatchedRules, 1)
		assert.Equal(t, "ingress-9090-metrics", out.MatchedRules[0].Key)
	})

	t.Run("dns_pattern_filter", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		_, out := callGetEvidence(t, ctx, cs, map[string]any{
			"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
			"dns_pattern": "example.com",
		})
		require.Len(t, out.MatchedRules, 1)
		assert.Equal(t, "egress-53-cidr", out.MatchedRules[0].Key)
	})

	t.Run("malformed_peer_isError", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		resp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "get_evidence",
			Arguments: map[string]any{
				"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
				"peer": "no-equals-sign",
			},
		})
		require.NoError(t, err)
		require.True(t, resp.IsError, "a peer filter with no '=' must be a tool error, never a panic")
		tc, ok := resp.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, tc.Text, "peer must be KEY=VAL")
	})

	t.Run("unknown_target_isError", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		resp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "get_evidence",
			Arguments: map[string]any{
				"session_id": sessionID, "namespace": evidenceTestNS, "workload": "missing",
			},
		})
		require.NoError(t, err)
		require.True(t, resp.IsError, "an unknown namespace/workload pair must be a tool error")
		tc, ok := resp.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, tc.Text, "list_policies")
	})

	t.Run("invalid_cursor_isError", func(t *testing.T) {
		cs, ctx, cleanup, sessionID := seedEvidenceQuerySession(t)
		defer cleanup()

		resp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "get_evidence",
			Arguments: map[string]any{
				"session_id": sessionID, "namespace": evidenceTestNS, "workload": evidenceTestWorkload,
				"cursor": "garbage",
			},
		})
		require.NoError(t, err)
		require.True(t, resp.IsError, "an invalid cursor must be a tool error, never a panic")
		tc, ok := resp.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, tc.Text, "invalid cursor")
	})
}

// TestMCPQueryGetEvidenceInputSchema proves QRY-05's schema contract for
// get_evidence: session_id/namespace/workload are the only required fields,
// direction is schema-enum-constrained to exactly [ingress, egress] (D-14,
// via mustQuerySchema — NOT struct-tag inference, which cannot express an
// enum), and there is NO protocol property (D-10's plan-checker correction:
// explain.Filter has no Protocol field, so a schema field with nothing
// behind it would be a dead, misleading surface).
func TestMCPQueryGetEvidenceInputSchema(t *testing.T) {
	initLoggerForTesting(t)

	cs, ctx, cleanup := connectQueryTestClient(t)
	defer cleanup()

	toolsResult, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	byName := make(map[string]*mcp.Tool, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		byName[tool.Name] = tool
	}
	require.Contains(t, byName, "get_evidence")

	assert.ElementsMatch(t, []string{"session_id", "namespace", "workload"},
		requiredFields(t, byName["get_evidence"].InputSchema))

	schema, ok := byName["get_evidence"].InputSchema.(map[string]any)
	require.True(t, ok, "InputSchema must round-trip as map[string]any over the wire")
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	assert.NotContains(t, props, "protocol", "get_evidence must not expose a protocol schema field (D-10)")

	directionProp, ok := props["direction"].(map[string]any)
	require.True(t, ok, "direction must be a schema property")
	enumRaw, ok := directionProp["enum"].([]any)
	require.True(t, ok, "direction must carry an enum constraint (D-14)")
	assert.ElementsMatch(t, []any{"ingress", "egress"}, enumRaw)
}
