package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/hubble"
	"github.com/SoulKyu/cpg/pkg/output"
	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
	"github.com/SoulKyu/cpg/pkg/session"
)

// startBypassSession starts a session against the D-07 bypass address
// ("127.0.0.1:1" — an explicit server address that skips kubeconfig/auto
// port-forward entirely, mcp_session_test.go's own pattern) so query-tool
// tests get a real session tmpdir without a live cluster. It returns the
// opaque session_id and the absolute tmp_dir, both read off
// start_session/get_status's structuredContent exactly as
// TestMCPSessionLifecycleWiringAndStdoutPurity does.
func startBypassSession(t *testing.T, ctx context.Context, cs *mcp.ClientSession, timeout string) (sessionID, tmpDir string) {
	t.Helper()

	startResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "start_session",
		Arguments: map[string]any{
			"server":         "127.0.0.1:1",
			"timeout":        timeout,
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

	return startOut.SessionID, statusOut.TmpDir
}

// connectQueryTestClient wires up one independent in-memory MCP session and
// returns the connected client session plus the ctx/cancel/drain triple the
// caller must invoke on cleanup — mirroring the exact
// startInMemoryMCPSession + mcp.NewClient + Connect sequence every test in
// this package already uses (mcp_session_test.go).
func connectQueryTestClient(t *testing.T) (cs *mcp.ClientSession, ctx context.Context, cleanup func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	clientT, drain := startInMemoryMCPSession(ctx)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	require.NoError(t, err)

	return cs, ctx, func() {
		cancel()
		drain()
		_ = cs.Close()
	}
}

// writeTestPolicy builds a real CiliumNetworkPolicy from flows via
// policy.BuildPolicy and writes it to policiesDir/ns/workload.yaml via
// pkg/output.Writer — the same production writer path start_session's
// pipeline uses — so list_policies/get_policy are exercised against a
// realistic, schema-correct fixture rather than a hand-rolled YAML string.
func writeTestPolicy(t *testing.T, policiesDir, ns, workload string, flows []*flowpb.Flow) string {
	t.Helper()
	cnp, _ := policy.BuildPolicy(ns, workload, flows, nil, policy.AttributionOptions{})
	w := output.NewWriter(policiesDir, zap.NewNop())
	require.NoError(t, w.Write(policy.PolicyEvent{Namespace: ns, Workload: workload, Policy: cnp}))
	return filepath.Join(policiesDir, ns, workload+".yaml")
}

// writeClusterHealthFixture marshals report as cluster-health.json at the
// exact path get_cluster_health derives (evidence/<hash>/cluster-health.json,
// hash = evidence.HashOutputDir(<tmpDir>/policies)) so tests can seed the
// "stopped + file present" branch without a real pipeline (finalize() only
// ever writes this file when at least one infra/transient drop was
// accumulated — never reachable from a fixture-free D-07 bypass session).
func writeClusterHealthFixture(t *testing.T, tmpDir string, report hubble.ClusterHealthReport) string {
	t.Helper()
	outputHash := evidence.HashOutputDir(filepath.Join(tmpDir, "policies"))
	dir := filepath.Join(tmpDir, "evidence", outputHash)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "cluster-health.json")
	data, err := json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// policyRowOut mirrors list_policies' policyMetaRow JSON shape for
// decodeStructured round-tripping in tests.
type policyRowOut struct {
	Namespace        string   `json:"namespace"`
	Workload         string   `json:"workload"`
	Name             string   `json:"name"`
	Directions       []string `json:"directions"`
	IngressRuleCount int      `json:"ingress_rule_count"`
	EgressRuleCount  int      `json:"egress_rule_count"`
	Path             string   `json:"path"`
}

func findPolicyRow(t *testing.T, rows []policyRowOut, workload string) policyRowOut {
	t.Helper()
	for _, r := range rows {
		if r.Workload == workload {
			return r
		}
	}
	t.Fatalf("workload %q not found in list_policies rows: %+v", workload, rows)
	return policyRowOut{}
}

// TestMCPQueryListPolicies proves QRY-02's list_policies over the in-memory
// transport: two seeded policies (one ingress-only, one egress-only) yield
// two rows whose namespace/workload/name/directions/rule-counts/path are
// all correct.
func TestMCPQueryListPolicies(t *testing.T) {
	initLoggerForTesting(t)

	cs, ctx, cleanup := connectQueryTestClient(t)
	defer cleanup()

	sessionID, tmpDir := startBypassSession(t, ctx, cs, "5s")
	policiesDir := filepath.Join(tmpDir, "policies")

	writeTestPolicy(t, policiesDir, "prod", "api",
		[]*flowpb.Flow{testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=api"}, "prod", 8080)})
	writeTestPolicy(t, policiesDir, "prod", "web",
		[]*flowpb.Flow{testdata.EgressUDPFlow([]string{"k8s:app=web"}, []string{"k8s:app=dns"}, "prod", 53)})

	listResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_policies",
		Arguments: map[string]any{"session_id": sessionID},
	})
	require.NoError(t, err)
	require.False(t, listResp.IsError)

	var listOut struct {
		Policies []policyRowOut `json:"policies"`
	}
	decodeStructured(t, listResp.StructuredContent, &listOut)
	require.Len(t, listOut.Policies, 2)

	api := findPolicyRow(t, listOut.Policies, "api")
	assert.Equal(t, "prod", api.Namespace)
	assert.Equal(t, "cpg-api", api.Name)
	assert.ElementsMatch(t, []string{"ingress"}, api.Directions)
	assert.Equal(t, 1, api.IngressRuleCount)
	assert.Equal(t, 0, api.EgressRuleCount)
	assert.Equal(t, filepath.Join(policiesDir, "prod", "api.yaml"), api.Path)

	web := findPolicyRow(t, listOut.Policies, "web")
	assert.Equal(t, "prod", web.Namespace)
	assert.Equal(t, "cpg-web", web.Name)
	assert.ElementsMatch(t, []string{"egress"}, web.Directions)
	assert.Equal(t, 0, web.IngressRuleCount)
	assert.Equal(t, 1, web.EgressRuleCount)
}

// TestMCPQueryGetPolicy proves QRY-02/D-11's get_policy over the in-memory
// transport: the full YAML + metadata round-trips for a seeded policy; an
// unknown workload returns an actionable isError naming list_policies; and
// a path-traversal namespace ("..") is rejected before any file access.
func TestMCPQueryGetPolicy(t *testing.T) {
	initLoggerForTesting(t)

	cs, ctx, cleanup := connectQueryTestClient(t)
	defer cleanup()

	sessionID, tmpDir := startBypassSession(t, ctx, cs, "5s")
	policiesDir := filepath.Join(tmpDir, "policies")
	policyPath := writeTestPolicy(t, policiesDir, "prod", "api",
		[]*flowpb.Flow{testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=api"}, "prod", 8080)})

	rawYAML, err := os.ReadFile(policyPath)
	require.NoError(t, err)

	getResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_policy",
		Arguments: map[string]any{
			"session_id": sessionID,
			"namespace":  "prod",
			"workload":   "api",
		},
	})
	require.NoError(t, err)
	require.False(t, getResp.IsError)

	var getOut struct {
		Namespace        string `json:"namespace"`
		Workload         string `json:"workload"`
		Name             string `json:"name"`
		YAML             string `json:"yaml"`
		Path             string `json:"path"`
		IngressRuleCount int    `json:"ingress_rule_count"`
		EgressRuleCount  int    `json:"egress_rule_count"`
	}
	decodeStructured(t, getResp.StructuredContent, &getOut)
	assert.Equal(t, "prod", getOut.Namespace)
	assert.Equal(t, "api", getOut.Workload)
	assert.Equal(t, "cpg-api", getOut.Name)
	assert.Equal(t, string(rawYAML), getOut.YAML)
	assert.Equal(t, policyPath, getOut.Path)
	assert.Equal(t, 1, getOut.IngressRuleCount)
	assert.Equal(t, 0, getOut.EgressRuleCount)

	missingResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_policy",
		Arguments: map[string]any{
			"session_id": sessionID,
			"namespace":  "prod",
			"workload":   "missing",
		},
	})
	require.NoError(t, err)
	require.True(t, missingResp.IsError, "an unknown namespace/workload pair must be a tool error")
	missingText, ok := missingResp.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, missingText.Text, "list_policies")

	traversalResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_policy",
		Arguments: map[string]any{
			"session_id": sessionID,
			"namespace":  "..",
			"workload":   "x",
		},
	})
	require.NoError(t, err)
	require.True(t, traversalResp.IsError, "a directory-traversal namespace must be rejected")
	traversalText, ok := traversalResp.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, traversalText.Text, "directory-traversal")
}

// TestMCPQueryGetClusterHealth proves QRY-04/D-13's corrected 3-way branch
// (4 cases counting the capturing state) over the in-memory transport.
//
// Timing note on the "capturing" and "stopped_present"/"stopped_absent_
// no_error" sub-tests: pkg/hubble/client.go's waitForConnReady loops on
// conn.WaitForStateChange until either connectivity.Ready or its OWN
// timeout fires — a refused D-07 bypass dial cycles through
// CONNECTING/TRANSIENT_FAILURE/backoff but never reaches Ready, so the
// session provably stays "capturing" for the full configured timeout
// (verified against pkg/hubble/client.go). Using a generous timeout and
// never waiting for it makes these sub-tests deterministic, not racy.
//
// The "stopped_absent_with_error" sub-test is the one genuine exception: it
// needs the pipeline to fail ON ITS OWN (not via stop_session, which never
// populates StatusResult.Error per WR-01's sessionCtx.Err()==nil guard), so
// it uses a short real timeout and require.Eventually to poll for the
// autonomous transition — mirroring pkg/session/manager_test.go's own
// TestManager_PipelineErrorAutonomouslyStopsSession pattern at the
// black-box MCP-harness level.
func TestMCPQueryGetClusterHealth(t *testing.T) {
	initLoggerForTesting(t)

	t.Run("capturing", func(t *testing.T) {
		cs, ctx, cleanup := connectQueryTestClient(t)
		defer cleanup()

		sessionID, _ := startBypassSession(t, ctx, cs, "10s")

		healthResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_cluster_health",
			Arguments: map[string]any{"session_id": sessionID},
		})
		require.NoError(t, err)
		require.False(t, healthResp.IsError, "capturing must be a non-error available_after_stop marker")

		var out struct {
			AvailableAfterStop bool   `json:"available_after_stop"`
			Message            string `json:"message"`
		}
		decodeStructured(t, healthResp.StructuredContent, &out)
		assert.True(t, out.AvailableAfterStop)
		assert.Contains(t, out.Message, "stop_session")
	})

	t.Run("stopped_present", func(t *testing.T) {
		cs, ctx, cleanup := connectQueryTestClient(t)
		defer cleanup()

		sessionID, tmpDir := startBypassSession(t, ctx, cs, "5s")
		writeClusterHealthFixture(t, tmpDir, hubble.ClusterHealthReport{
			SchemaVersion:     1,
			ClassifierVersion: "v1",
			Session: hubble.HealthSession{
				Started:        time.Now().Add(-time.Minute),
				Ended:          time.Now(),
				FlowsSeen:      10,
				InfraDropTotal: 3,
			},
			Drops: []hubble.HealthDropJSON{{
				Reason:      "CT_MAP_INSERTION_FAILED",
				Class:       "infra",
				Count:       3,
				Remediation: "https://docs.cilium.io/en/stable/",
				ByNode:      map[string]uint64{"node-1": 3},
				ByWorkload:  map[string]uint64{"prod/api": 3},
			}},
		})

		stopResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "stop_session",
			Arguments: map[string]any{"session_id": sessionID},
		})
		require.NoError(t, err)
		require.False(t, stopResp.IsError)

		healthResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_cluster_health",
			Arguments: map[string]any{"session_id": sessionID},
		})
		require.NoError(t, err)
		require.False(t, healthResp.IsError)

		var out struct {
			Report *struct {
				SchemaVersion int `json:"schema_version"`
				Drops         []struct {
					Reason string `json:"reason"`
					Count  uint64 `json:"count"`
				} `json:"drops"`
			} `json:"report"`
		}
		decodeStructured(t, healthResp.StructuredContent, &out)
		require.NotNil(t, out.Report)
		require.Len(t, out.Report.Drops, 1)
		assert.Equal(t, "CT_MAP_INSERTION_FAILED", out.Report.Drops[0].Reason)
		assert.Equal(t, uint64(3), out.Report.Drops[0].Count)
	})

	t.Run("stopped_absent_no_error", func(t *testing.T) {
		cs, ctx, cleanup := connectQueryTestClient(t)
		defer cleanup()

		sessionID, _ := startBypassSession(t, ctx, cs, "5s")

		stopResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "stop_session",
			Arguments: map[string]any{"session_id": sessionID},
		})
		require.NoError(t, err)
		require.False(t, stopResp.IsError)

		healthResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_cluster_health",
			Arguments: map[string]any{"session_id": sessionID},
		})
		require.NoError(t, err)
		require.False(t, healthResp.IsError, "absent report + no pipeline error must not be a failure (D-13)")

		var out struct {
			NoDrops bool `json:"no_drops"`
		}
		decodeStructured(t, healthResp.StructuredContent, &out)
		assert.True(t, out.NoDrops)
	})

	t.Run("stopped_absent_with_error", func(t *testing.T) {
		cs, ctx, cleanup := connectQueryTestClient(t)
		defer cleanup()

		// A short timeout against the unreachable D-07 bypass address lets
		// the pipeline genuinely fail on its own (WR-01 autonomous exit) —
		// distinct from an explicit stop_session cancellation, which never
		// populates StatusResult.Error.
		sessionID, _ := startBypassSession(t, ctx, cs, "1s")

		require.Eventually(t, func() bool {
			statusResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
				Name:      "get_status",
				Arguments: map[string]any{"session_id": sessionID},
			})
			if err != nil || statusResp.IsError {
				return false
			}
			var statusOut struct {
				State string `json:"state"`
				Error string `json:"error"`
			}
			decodeStructured(t, statusResp.StructuredContent, &statusOut)
			return statusOut.State == "stopped" && statusOut.Error != ""
		}, 10*time.Second, 50*time.Millisecond, "pipeline must autonomously fail against the unreachable bypass address")

		healthResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_cluster_health",
			Arguments: map[string]any{"session_id": sessionID},
		})
		require.NoError(t, err)
		require.True(t, healthResp.IsError, "a genuine crash before any drop must surface as isError")
		tc, ok := healthResp.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		assert.NotEmpty(t, tc.Text)
	})
}

// TestClusterHealthBranch directly unit-tests clusterHealthBranch's 4-case
// D-13 branch logic — a pure function over an already-resolved
// session.StatusResult plus a filesystem path — without needing a real
// session or pipeline. This complements TestMCPQueryGetClusterHealth's
// end-to-end wire-level coverage with fast, fully deterministic coverage of
// the exact same branch logic the handler calls.
func TestClusterHealthBranch(t *testing.T) {
	t.Run("capturing", func(t *testing.T) {
		result, err := clusterHealthBranch(session.StatusResult{State: "capturing"}, "/nonexistent/cluster-health.json")
		require.NoError(t, err)
		assert.True(t, result.AvailableAfterStop)
		assert.NotEmpty(t, result.Message)
		assert.False(t, result.NoDrops)
		assert.Nil(t, result.Report)
	})

	t.Run("stopped_present", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cluster-health.json")
		report := hubble.ClusterHealthReport{
			SchemaVersion: 1,
			Drops: []hubble.HealthDropJSON{{
				Reason:     "NO_MAPPING",
				Class:      "infra",
				Count:      1,
				ByNode:     map[string]uint64{"node-1": 1},
				ByWorkload: map[string]uint64{"prod/api": 1},
			}},
		}
		data, err := json.Marshal(report)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0o644))

		result, err := clusterHealthBranch(session.StatusResult{State: "stopped"}, path)
		require.NoError(t, err)
		require.NotNil(t, result.Report)
		assert.Equal(t, "NO_MAPPING", result.Report.Drops[0].Reason)
		assert.False(t, result.AvailableAfterStop)
		assert.False(t, result.NoDrops)
	})

	t.Run("stopped_absent_no_error", func(t *testing.T) {
		dir := t.TempDir()
		result, err := clusterHealthBranch(session.StatusResult{State: "stopped", Error: ""}, filepath.Join(dir, "missing.json"))
		require.NoError(t, err)
		assert.True(t, result.NoDrops)
		assert.False(t, result.AvailableAfterStop)
		assert.Nil(t, result.Report)
	})

	t.Run("stopped_absent_with_error", func(t *testing.T) {
		dir := t.TempDir()
		result, err := clusterHealthBranch(session.StatusResult{State: "stopped", Error: "relay connection reset by peer"}, filepath.Join(dir, "missing.json"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "relay connection reset by peer")
		assert.Equal(t, getClusterHealthResult{}, result)
	})
}

// TestMCPQueryToolsListed is Phase 18's closing tool-count assertion: now
// that list_dropped_flows (18-05) is the 5th and last query tool, the
// composition root's total is pinned exactly — 3 session tools (Phase 17)
// plus 5 query tools (Phase 18). Earlier per-plan tests deliberately used
// GreaterOrEqual/Contains while the tool table was still growing
// (mcp_session_test.go's TestMCPSessionToolsListed, this file's own
// pre-18-05 history) — this is the one place the exact total is checked.
func TestMCPQueryToolsListed(t *testing.T) {
	initLoggerForTesting(t)

	cs, ctx, cleanup := connectQueryTestClient(t)
	defer cleanup()

	toolsResult, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Len(t, toolsResult.Tools, 8, "3 session + 5 query tools")

	byName := make(map[string]*mcp.Tool, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{
		"start_session", "get_status", "stop_session",
		"list_dropped_flows", "list_policies", "get_policy", "get_evidence", "get_cluster_health",
	} {
		assert.Contains(t, byName, name)
	}

	assert.ElementsMatch(t, []string{"session_id"}, requiredFields(t, byName["list_policies"].InputSchema),
		"list_policies must require only session_id")
	assert.ElementsMatch(t, []string{"session_id"}, requiredFields(t, byName["get_cluster_health"].InputSchema),
		"get_cluster_health must require only session_id")
	assert.ElementsMatch(t, []string{"session_id", "namespace", "workload"}, requiredFields(t, byName["get_policy"].InputSchema),
		"get_policy must require session_id, namespace, and workload (D-11, no omitempty)")
	assert.ElementsMatch(t, []string{"session_id"}, requiredFields(t, byName["list_dropped_flows"].InputSchema),
		"list_dropped_flows must require only session_id — every filter/pagination field is optional")
}

// TestMCPQueryToolsQRY05Contract centrally proves QRY-05's cross-cutting
// discipline holds across all 5 query tools at once — individual tool test
// files (mcp_query_tools_test.go's own TestMCPQueryGetPolicy/
// TestMCPQueryGetClusterHealth, mcp_query_evidence_test.go's
// TestMCPQueryGetEvidenceInputSchema, mcp_query_flows_test.go's own
// description_and_schema_contract sub-test) already prove tool-specific
// behavior in depth; this is the one place the SHARED contract is checked
// for the whole set in one pass: truthful annotations (ReadOnlyHint true,
// OpenWorldHint explicitly false — D-16) as observed over the wire, a
// non-empty outputSchema for every data-returning tool (mcp.AddTool's typed
// structs infer this automatically, D-17), and the dropclass/direction enum
// constraints on the 2 tools that carry them (D-14).
func TestMCPQueryToolsQRY05Contract(t *testing.T) {
	initLoggerForTesting(t)

	cs, ctx, cleanup := connectQueryTestClient(t)
	defer cleanup()

	toolsResult, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	byName := make(map[string]*mcp.Tool, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		byName[tool.Name] = tool
	}

	queryToolNames := []string{"list_dropped_flows", "list_policies", "get_policy", "get_evidence", "get_cluster_health"}
	for _, name := range queryToolNames {
		tool, ok := byName[name]
		require.True(t, ok, "%s must be registered", name)
		require.NotNil(t, tool.Annotations, "%s must carry annotations", name)
		assert.True(t, tool.Annotations.ReadOnlyHint, "%s must be ReadOnlyHint (QRY-05/SEC-01)", name)
		require.NotNil(t, tool.Annotations.OpenWorldHint, "%s must set OpenWorldHint explicitly — omission defaults to the spec's implicit true (D-16)", name)
		assert.False(t, *tool.Annotations.OpenWorldHint, "%s must be OpenWorldHint=false", name)
		assert.NotEmpty(t, tool.OutputSchema, "%s is data-returning and must expose a non-empty outputSchema (D-17)", name)
	}

	for _, name := range []string{"list_dropped_flows", "get_evidence"} {
		schema, ok := byName[name].InputSchema.(map[string]any)
		require.True(t, ok, "%s InputSchema must round-trip as map[string]any over the wire", name)
		props, ok := schema["properties"].(map[string]any)
		require.True(t, ok, "%s must have schema properties", name)
		directionProp, ok := props["direction"].(map[string]any)
		require.True(t, ok, "%s must have a direction schema property", name)
		directionEnum, ok := directionProp["enum"].([]any)
		require.True(t, ok, "%s direction must carry an enum constraint (D-14)", name)
		assert.ElementsMatch(t, []any{"ingress", "egress"}, directionEnum)
	}

	dropclassSchema, ok := byName["list_dropped_flows"].InputSchema.(map[string]any)
	require.True(t, ok)
	dropclassProps, ok := dropclassSchema["properties"].(map[string]any)
	require.True(t, ok)
	dropclassProp, ok := dropclassProps["dropclass"].(map[string]any)
	require.True(t, ok, "list_dropped_flows must have a dropclass schema property")
	dropclassEnum, ok := dropclassProp["enum"].([]any)
	require.True(t, ok, "list_dropped_flows dropclass must carry an enum constraint (D-14)")
	assert.ElementsMatch(t, []any{"policy", "infra", "transient", "noise", "unknown"}, dropclassEnum)
}

// TestMCPQueryToolsErrorTexts centralizes D-16's actionable-error-text
// contract across all 5 query tools (18-03/18-04/18-05): an unknown
// session_id must resolve to a tool error carrying the verbatim SESS-06
// phrase "not found or expired" for every one of them, reusing
// Manager.Status's own text untouched (D-08) rather than each handler
// inventing its own wording.
func TestMCPQueryToolsErrorTexts(t *testing.T) {
	initLoggerForTesting(t)

	cs, ctx, cleanup := connectQueryTestClient(t)
	defer cleanup()

	cases := []struct {
		name string
		args map[string]any
	}{
		{"list_policies", map[string]any{"session_id": "sess_bogus"}},
		{"get_policy", map[string]any{"session_id": "sess_bogus", "namespace": "prod", "workload": "api"}},
		{"get_cluster_health", map[string]any{"session_id": "sess_bogus"}},
		{"get_evidence", map[string]any{"session_id": "sess_bogus", "namespace": "prod", "workload": "api"}},
		{"list_dropped_flows", map[string]any{"session_id": "sess_bogus"}},
	}
	for _, tc := range cases {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		require.NoError(t, err, "%s: a tool error must not surface as a transport/protocol error", tc.name)
		require.True(t, result.IsError, "%s: an unknown session_id must resolve to a tool error", tc.name)
		require.Len(t, result.Content, 1, "%s: error result must carry exactly one content block", tc.name)
		content, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok, "%s: error content must be TextContent, got %T", tc.name, result.Content[0])
		assert.Contains(t, content.Text, "not found or expired", "%s: must reuse the SESS-06 phrase verbatim", tc.name)
	}
}

// TestMCPQueryToolsNotFoundAndCursorErrorTexts centralizes D-16's remaining
// two actionable-error-text families: an unknown namespace/workload pair
// suggests list_policies (get_policy, get_evidence), and an invalid/
// malformed cursor returns the shared D-05 text (get_evidence,
// list_dropped_flows) — never a panic. get_policy/get_evidence each already
// have deep per-tool coverage of these paths elsewhere in this package
// (this file's own TestMCPQueryGetPolicy, mcp_query_evidence_test.go's
// TestMCPQueryGetEvidence); this is the one place list_dropped_flows'
// invalid-cursor text is proven too, and that all 3 tools agree on wording.
// The not-found cases need no fixture: os.ReadFile's not-found path is
// identical whether or not any sibling file/directory has ever been
// written under the session tmpdir. The cursor cases DO need a real
// evidence fixture at prod/api for get_evidence: its handler resolves the
// evidence file BEFORE decoding the cursor (mcp_query_evidence.go), so an
// unseeded target would surface the not-found text instead of the cursor
// text this sub-test is actually proving.
func TestMCPQueryToolsNotFoundAndCursorErrorTexts(t *testing.T) {
	initLoggerForTesting(t)

	cs, ctx, cleanup := connectQueryTestClient(t)
	defer cleanup()

	sessionID, tmpDir := startBypassSession(t, ctx, cs, "5s")
	writeEvidenceFixture(t, tmpDir, "prod", "api", buildEvidenceFixture("prod", "api"))

	notFoundCases := []struct {
		name string
		args map[string]any
	}{
		{"get_policy", map[string]any{"session_id": sessionID, "namespace": "prod", "workload": "missing"}},
		{"get_evidence", map[string]any{"session_id": sessionID, "namespace": "prod", "workload": "missing"}},
	}
	for _, tc := range notFoundCases {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		require.NoError(t, err, "%s: a tool error must not surface as a transport/protocol error", tc.name)
		require.True(t, result.IsError, "%s: an unknown namespace/workload pair must be a tool error", tc.name)
		content, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok, "%s: error content must be TextContent, got %T", tc.name, result.Content[0])
		assert.Contains(t, content.Text, "list_policies", "%s: not-found text must suggest list_policies", tc.name)
	}

	cursorCases := []struct {
		name string
		args map[string]any
	}{
		{"get_evidence", map[string]any{"session_id": sessionID, "namespace": "prod", "workload": "api", "cursor": "garbage"}},
		{"list_dropped_flows", map[string]any{"session_id": sessionID, "cursor": "garbage"}},
	}
	for _, tc := range cursorCases {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
		require.NoError(t, err, "%s: a tool error must not surface as a transport/protocol error", tc.name)
		require.True(t, result.IsError, "%s: an invalid cursor must be a tool error, never a panic", tc.name)
		content, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok, "%s: error content must be TextContent, got %T", tc.name, result.Content[0])
		assert.Contains(t, content.Text, "invalid cursor", "%s: must carry the shared D-05 text", tc.name)
	}
}
