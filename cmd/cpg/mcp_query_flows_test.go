package main

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/hubble"
)

// buildDroppedFlowsSampleEvidence returns a hand-built PolicyEvidence with 2
// rules — one egress, one ingress — each carrying exactly 1 FlowSample, both
// classified "policy" (DropReason "POLICY_DENIED"). This is the fixed
// 2-sample building block every list_dropped_flows sub-test in this file
// seeds via writeEvidenceFixture (mcp_query_evidence_test.go's helper, same
// package). Rules are listed egress-before-ingress deliberately — matching
// pkg/evidence.Merge's real (Direction, Key) sort order ("egress" <
// "ingress" lexicographically) — so the pagination sub-test's page-by-page
// trace is exercised against a realistic file-order, not an arbitrary one.
func buildDroppedFlowsSampleEvidence(ns, workload string) evidence.PolicyEvidence {
	now := time.Now()
	return evidence.PolicyEvidence{
		SchemaVersion: evidence.SchemaVersion,
		Policy:        evidence.PolicyRef{Name: "cpg-" + workload, Namespace: ns, Workload: workload},
		Rules: []evidence.RuleEvidence{
			{
				Key:       "egress-53",
				Direction: "egress",
				Peer:      evidence.PeerRef{Type: "cidr", CIDR: "10.0.0.5/32"},
				Port:      "53",
				Protocol:  "udp",
				FlowCount: 1,
				FirstSeen: now.Add(-time.Hour),
				LastSeen:  now,
				Samples: []evidence.FlowSample{{
					Time:       now,
					Src:        evidence.FlowEndpoint{Namespace: ns, Workload: workload},
					Dst:        evidence.FlowEndpoint{IP: "10.0.0.5"},
					Port:       53,
					Protocol:   "udp",
					Verdict:    "DROPPED",
					DropReason: "POLICY_DENIED",
				}},
			},
			{
				Key:       "ingress-8080",
				Direction: "ingress",
				Peer:      evidence.PeerRef{Type: "endpoint", Labels: map[string]string{"app": "client"}},
				Port:      "8080",
				Protocol:  "tcp",
				FlowCount: 1,
				FirstSeen: now.Add(-time.Hour),
				LastSeen:  now,
				Samples: []evidence.FlowSample{{
					Time:       now,
					Src:        evidence.FlowEndpoint{Namespace: ns, Workload: "client"},
					Dst:        evidence.FlowEndpoint{Namespace: ns, Workload: workload},
					Port:       8080,
					Protocol:   "tcp",
					Verdict:    "DROPPED",
					DropReason: "POLICY_DENIED",
				}},
			},
		},
	}
}

// singleDropHealthReport returns a 1-drop-reason ClusterHealthReport whose
// ByWorkload map has exactly the one supplied "namespace/workload" key — the
// base fixture every non-namespace-filter sub-test uses so the aggregates
// half flattens to exactly 1 row (Task 1's behavior list: "2 evidence
// samples + 1 cluster-health drop returns ... aggregates[] len 1").
func singleDropHealthReport(byWorkloadKey string, count uint64) hubble.ClusterHealthReport {
	return hubble.ClusterHealthReport{
		SchemaVersion:     1,
		ClassifierVersion: "v1",
		Session: hubble.HealthSession{
			Started:   time.Now().Add(-time.Minute),
			Ended:     time.Now(),
			FlowsSeen: count,
		},
		Drops: []hubble.HealthDropJSON{{
			Reason:      "CT_MAP_INSERTION_FAILED",
			Class:       "infra",
			Count:       count,
			Remediation: "https://docs.cilium.io/en/stable/",
			ByNode:      map[string]uint64{"node-1": count},
			ByWorkload:  map[string]uint64{byWorkloadKey: count},
		}},
	}
}

// droppedFlowAggregateRowOut mirrors droppedFlowAggregateRow's JSON shape for
// decodeStructured round-tripping.
type droppedFlowAggregateRowOut struct {
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	Reason    string `json:"reason"`
	Class     string `json:"class"`
	Count     uint64 `json:"count"`
}

// listDroppedFlowsAggregatesOut mirrors listDroppedFlowsAggregates' JSON
// shape (the embedded availableAfterStopMarker fields plus Rows).
type listDroppedFlowsAggregatesOut struct {
	AvailableAfterStop bool                         `json:"available_after_stop"`
	Message            string                       `json:"message"`
	Rows               []droppedFlowAggregateRowOut `json:"rows"`
}

// listDroppedFlowsOut mirrors list_dropped_flows' listDroppedFlowsResult JSON
// shape for decodeStructured round-tripping. Samples decodes straight into
// []DroppedFlowSample, the real exported type, mirroring
// mcp_query_evidence_test.go's getEvidenceOut precedent of reusing the real
// per-record type rather than a parallel test-only shape.
type listDroppedFlowsOut struct {
	Samples    []DroppedFlowSample           `json:"samples"`
	Aggregates listDroppedFlowsAggregatesOut `json:"aggregates"`
	TotalCount int                           `json:"total_count"`
	HasMore    bool                          `json:"has_more"`
	NextCursor string                        `json:"next_cursor"`
}

// callListDroppedFlows invokes the list_dropped_flows tool and, on a
// non-error result, decodes structuredContent into listDroppedFlowsOut —
// mirroring mcp_query_evidence_test.go's callGetEvidence helper exactly.
func callListDroppedFlows(t *testing.T, ctx context.Context, cs *mcp.ClientSession, args map[string]any) (*mcp.CallToolResult, listDroppedFlowsOut) {
	t.Helper()
	resp, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_dropped_flows", Arguments: args})
	require.NoError(t, err, "a tool error must not surface as a transport/protocol error")
	var out listDroppedFlowsOut
	if !resp.IsError {
		decodeStructured(t, resp.StructuredContent, &out)
	}
	return resp, out
}

// stopBypassSession calls stop_session and requires it to succeed —
// consolidates the 4-line stop_session boilerplate every stopped-state
// sub-test below repeats.
func stopBypassSession(t *testing.T, ctx context.Context, cs *mcp.ClientSession, sessionID string) {
	t.Helper()
	stopResp, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "stop_session", Arguments: map[string]any{"session_id": sessionID}})
	require.NoError(t, err)
	require.False(t, stopResp.IsError)
}

// TestMCPQueryListDroppedFlows proves QRY-01's composed view end to end over
// the in-memory transport: samples[]/aggregates[] as genuinely distinct
// sections that are never synthesized from each other (D-01), the D-02
// available_after_stop marker while capturing (with samples[] still served
// live), direction filtering the samples half only while leaving aggregates
// unfiltered (Pitfall 2/T-18-05-04), dropclass=noise's structurally-empty
// aggregates (Pitfall 3), namespace filtering narrowing both halves
// consistently, combined-view pagination (D-04/D-07), and the mandated D-15
// description/schema contract (the verbatim D-04 phrase, the dropclass/
// direction schema enums).
func TestMCPQueryListDroppedFlows(t *testing.T) {
	initLoggerForTesting(t)

	t.Run("composed_view_stopped_session", func(t *testing.T) {
		cs, ctx, cleanup := connectQueryTestClient(t)
		defer cleanup()

		sessionID, tmpDir := startBypassSession(t, ctx, cs, "5s")
		writeEvidenceFixture(t, tmpDir, "prod", "api", buildDroppedFlowsSampleEvidence("prod", "api"))
		writeClusterHealthFixture(t, tmpDir, singleDropHealthReport("prod/api", 5))
		stopBypassSession(t, ctx, cs, sessionID)

		resp, out := callListDroppedFlows(t, ctx, cs, map[string]any{"session_id": sessionID})
		require.False(t, resp.IsError)
		require.Len(t, out.Samples, 2, "samples[] is its own distinct section, flattened from the capped evidence")
		require.Len(t, out.Aggregates.Rows, 1, "aggregates[] is its own distinct section, flattened from cluster-health.json")
		assert.False(t, out.Aggregates.AvailableAfterStop, "a stopped session's aggregates must not carry the capturing marker")

		row := out.Aggregates.Rows[0]
		assert.Equal(t, "prod", row.Namespace)
		assert.Equal(t, "api", row.Workload)
		assert.Equal(t, "CT_MAP_INSERTION_FAILED", row.Reason)
		assert.Equal(t, "infra", row.Class)
		assert.Equal(t, uint64(5), row.Count)

		for _, s := range out.Samples {
			assert.Equal(t, "prod", s.Namespace)
			assert.Equal(t, "api", s.Workload)
			assert.Equal(t, "POLICY_DENIED", s.DropReason)
		}
	})

	t.Run("capturing_marker_samples_still_live", func(t *testing.T) {
		cs, ctx, cleanup := connectQueryTestClient(t)
		defer cleanup()

		// Generous timeout (10s): pkg/hubble/client.go's waitForConnReady
		// only returns on ITS OWN timeout firing, never early on a mere
		// connection refusal against the D-07 bypass address — mirroring
		// TestMCPQueryGetClusterHealth's own documented, non-racy pattern.
		sessionID, tmpDir := startBypassSession(t, ctx, cs, "10s")
		writeEvidenceFixture(t, tmpDir, "prod", "api", buildDroppedFlowsSampleEvidence("prod", "api"))

		resp, out := callListDroppedFlows(t, ctx, cs, map[string]any{"session_id": sessionID})
		require.False(t, resp.IsError, "capturing must be a non-error available_after_stop marker, not a tool error")
		assert.True(t, out.Aggregates.AvailableAfterStop)
		assert.Contains(t, out.Aggregates.Message, "stop_session")
		assert.Empty(t, out.Aggregates.Rows)
		assert.Len(t, out.Samples, 2, "the samples half is served live even while capturing — evidence files are atomic on disk (D-02)")
	})

	t.Run("direction_filters_samples_only", func(t *testing.T) {
		cs, ctx, cleanup := connectQueryTestClient(t)
		defer cleanup()

		sessionID, tmpDir := startBypassSession(t, ctx, cs, "5s")
		writeEvidenceFixture(t, tmpDir, "prod", "api", buildDroppedFlowsSampleEvidence("prod", "api"))
		writeClusterHealthFixture(t, tmpDir, singleDropHealthReport("prod/api", 5))
		stopBypassSession(t, ctx, cs, sessionID)

		resp, out := callListDroppedFlows(t, ctx, cs, map[string]any{"session_id": sessionID, "direction": "ingress"})
		require.False(t, resp.IsError)
		require.Len(t, out.Samples, 1, "direction=ingress narrows samples[] to the one ingress rule's sample")
		assert.Equal(t, "ingress", out.Samples[0].Direction)
		require.Len(t, out.Aggregates.Rows, 1, "aggregates[] has no direction dimension — it must be returned unfiltered (Pitfall 2/T-18-05-04)")
	})

	t.Run("dropclass_noise_yields_empty_aggregates", func(t *testing.T) {
		cs, ctx, cleanup := connectQueryTestClient(t)
		defer cleanup()

		sessionID, tmpDir := startBypassSession(t, ctx, cs, "5s")
		writeEvidenceFixture(t, tmpDir, "prod", "api", buildDroppedFlowsSampleEvidence("prod", "api"))
		writeClusterHealthFixture(t, tmpDir, singleDropHealthReport("prod/api", 5))
		stopBypassSession(t, ctx, cs, sessionID)

		resp, out := callListDroppedFlows(t, ctx, cs, map[string]any{"session_id": sessionID, "dropclass": "noise"})
		require.False(t, resp.IsError, "dropclass=noise must be a normal, empty result — never an error (Pitfall 3)")
		assert.Empty(t, out.Aggregates.Rows, "noise never reaches cluster-health.json — structurally always empty")
		assert.Empty(t, out.Samples, "noise never reaches evidence either — structurally always empty")
	})

	t.Run("namespace_filter_narrows_both_halves", func(t *testing.T) {
		cs, ctx, cleanup := connectQueryTestClient(t)
		defer cleanup()

		sessionID, tmpDir := startBypassSession(t, ctx, cs, "5s")
		writeEvidenceFixture(t, tmpDir, "prod", "api", buildDroppedFlowsSampleEvidence("prod", "api"))
		writeEvidenceFixture(t, tmpDir, "staging", "web", buildDroppedFlowsSampleEvidence("staging", "web"))
		writeClusterHealthFixture(t, tmpDir, hubble.ClusterHealthReport{
			SchemaVersion: 1,
			Drops: []hubble.HealthDropJSON{{
				Reason:     "CT_MAP_INSERTION_FAILED",
				Class:      "infra",
				Count:      8,
				ByNode:     map[string]uint64{"node-1": 8},
				ByWorkload: map[string]uint64{"prod/api": 5, "staging/web": 3},
			}},
		})
		stopBypassSession(t, ctx, cs, sessionID)

		resp, out := callListDroppedFlows(t, ctx, cs, map[string]any{"session_id": sessionID, "namespace": "prod"})
		require.False(t, resp.IsError)
		require.Len(t, out.Samples, 2, "namespace=prod narrows out staging/web's samples")
		for _, s := range out.Samples {
			assert.Equal(t, "prod", s.Namespace)
		}
		require.Len(t, out.Aggregates.Rows, 1, "namespace=prod narrows out staging/web's aggregate row")
		assert.Equal(t, "prod", out.Aggregates.Rows[0].Namespace)
	})

	t.Run("pagination_over_combined_view", func(t *testing.T) {
		cs, ctx, cleanup := connectQueryTestClient(t)
		defer cleanup()

		sessionID, tmpDir := startBypassSession(t, ctx, cs, "5s")
		writeEvidenceFixture(t, tmpDir, "prod", "api", buildDroppedFlowsSampleEvidence("prod", "api"))
		writeClusterHealthFixture(t, tmpDir, singleDropHealthReport("prod/api", 5))
		stopBypassSession(t, ctx, cs, sessionID)

		// 3 combined items total (2 samples + 1 aggregate row) — limit=1
		// walks the full boundary-key-cursor trace page by page.
		resp1, out1 := callListDroppedFlows(t, ctx, cs, map[string]any{"session_id": sessionID, "limit": 1})
		require.False(t, resp1.IsError)
		assert.Equal(t, 3, out1.TotalCount, "total_count reflects filtered view items (2 samples + 1 aggregate row), not a raw flow total (D-04)")
		assert.True(t, out1.HasMore)
		require.NotEmpty(t, out1.NextCursor)
		assert.Equal(t, 1, len(out1.Samples)+len(out1.Aggregates.Rows), "exactly one combined item per page")

		resp2, out2 := callListDroppedFlows(t, ctx, cs, map[string]any{"session_id": sessionID, "limit": 1, "cursor": out1.NextCursor})
		require.False(t, resp2.IsError)
		assert.True(t, out2.HasMore)
		assert.Equal(t, 3, out2.TotalCount)
		assert.Equal(t, 1, len(out2.Samples)+len(out2.Aggregates.Rows))

		resp3, out3 := callListDroppedFlows(t, ctx, cs, map[string]any{"session_id": sessionID, "limit": 1, "cursor": out2.NextCursor})
		require.False(t, resp3.IsError)
		assert.False(t, out3.HasMore, "the third page exhausts the combined 3-item view")
		assert.Empty(t, out3.NextCursor)
		assert.Equal(t, 1, len(out3.Samples)+len(out3.Aggregates.Rows))

		// Every item across all 3 pages must be distinct — no skip, no dup.
		total := len(out1.Samples) + len(out1.Aggregates.Rows) +
			len(out2.Samples) + len(out2.Aggregates.Rows) +
			len(out3.Samples) + len(out3.Aggregates.Rows)
		assert.Equal(t, 3, total)
	})

	t.Run("description_and_schema_contract", func(t *testing.T) {
		cs, ctx, cleanup := connectQueryTestClient(t)
		defer cleanup()

		toolsResult, err := cs.ListTools(ctx, nil)
		require.NoError(t, err)
		byName := make(map[string]*mcp.Tool, len(toolsResult.Tools))
		for _, tool := range toolsResult.Tools {
			byName[tool.Name] = tool
		}
		require.Contains(t, byName, "list_dropped_flows")
		tool := byName["list_dropped_flows"]

		assert.Contains(t, tool.Description, "a sampled/aggregated view, not a raw flow log", "D-04's verbatim phrase")

		schema, ok := tool.InputSchema.(map[string]any)
		require.True(t, ok, "InputSchema must round-trip as map[string]any over the wire")
		props, ok := schema["properties"].(map[string]any)
		require.True(t, ok)

		dropclassProp, ok := props["dropclass"].(map[string]any)
		require.True(t, ok, "dropclass must be a schema property")
		dropclassEnum, ok := dropclassProp["enum"].([]any)
		require.True(t, ok, "dropclass must carry an enum constraint (D-14)")
		assert.ElementsMatch(t, []any{"policy", "infra", "transient", "noise", "unknown"}, dropclassEnum)

		directionProp, ok := props["direction"].(map[string]any)
		require.True(t, ok, "direction must be a schema property")
		directionEnum, ok := directionProp["enum"].([]any)
		require.True(t, ok, "direction must carry an enum constraint (D-14)")
		assert.ElementsMatch(t, []any{"ingress", "egress"}, directionEnum)

		assert.ElementsMatch(t, []string{"session_id"}, requiredFields(t, tool.InputSchema),
			"only session_id is required — every filter/pagination field is optional")
	})
}
