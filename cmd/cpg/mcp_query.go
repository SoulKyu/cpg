package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/hubble"
	"github.com/SoulKyu/cpg/pkg/output"
	"github.com/SoulKyu/cpg/pkg/session"
)

// registerQueryTools registers Phase 18's read-side query MCP tools —
// list_policies, get_policy (QRY-02), get_cluster_health (QRY-04), and
// get_evidence (QRY-03, 18-04) — on server, wired to mgr. list_dropped_flows
// (18-05) registers alongside these last. This is the Phase 18
// composition-root entry point cmd/cpg/mcp.go's runMCPServer calls right
// after registerSessionTools (Phase 17); the same readonly discipline
// applies unchanged — every handler here reaches only mgr.Status (via
// resolveSession) plus pkg/output/pkg/hubble/pkg/explain filesystem readers
// over the session tmpdir, never a K8s write verb, never new pkg/session API
// (D-08). get_evidence's registration lives in its own function
// (registerGetEvidenceTool, mcp_query_evidence.go) because its InputSchema
// needs the mustQuerySchema enum-patching mechanism (D-14); the other 3
// tools stay inline below since their schemas need no such treatment.
func registerQueryTools(server *mcp.Server, mgr *session.Manager) {
	registerGetEvidenceTool(server, mgr)

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_policies",
		Description: "Lists metadata for every generated CiliumNetworkPolicy in this session: " +
			"namespace, workload, the CNP's metadata.name, which of ingress/egress carry rules, " +
			"rule counts, and the absolute YAML path on disk. Every listed policy is, by " +
			"construction, for a policy-actionable drop — infra/transient/noise drops the " +
			"classifier suppressed never produce a policy file (see get_cluster_health for " +
			"infra/transient counts instead). Unpaginated and best-effort: policy files may be " +
			"added or updated between calls during an active capture, so call again for the " +
			"latest view.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  jsonschema.Ptr(false),
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, args sessionRef) (*mcp.CallToolResult, listPoliciesResult, error) {
		return handleListPolicies(mgr, args)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_policy",
		Description: "Returns the full CiliumNetworkPolicy YAML plus namespace/workload/name/" +
			"rule-count metadata for one generated policy (discover available namespace/workload " +
			"pairs via list_policies first). Only policy-actionable drops ever produce a CNP; " +
			"infra/transient drops never appear here — see get_cluster_health for those. An " +
			"unknown namespace/workload pair returns an actionable error suggesting " +
			"list_policies; a namespace or workload containing '.', '..', or a path separator " +
			"is rejected before any file access.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  jsonschema.Ptr(false),
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, args getPolicyArgs) (*mcp.CallToolResult, getPolicyResult, error) {
		return handleGetPolicy(mgr, args)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_cluster_health",
		Description: "Returns the session's finalized cluster-health report — per-drop-reason " +
			"counts (by node and workload) and Cilium-docs remediation URLs — for infra/transient " +
			"drops the classifier deliberately excluded from policy generation (list_policies/" +
			"get_policy only ever cover policy-actionable drops; infra/transient noise never " +
			"produces a CiliumNetworkPolicy). Health is finalize-only: while capturing, this " +
			"returns a non-error marker asking you to call stop_session first, never a live/" +
			"partial count. After stop, an absent report is the common case and means zero " +
			"infra/transient drops were observed this session — not a failure; only a session " +
			"that crashed with a genuine pipeline error before any drop was recorded returns an " +
			"isError.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  jsonschema.Ptr(false),
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, args sessionRef) (*mcp.CallToolResult, getClusterHealthResult, error) {
		return handleGetClusterHealth(mgr, args)
	})
}

// resolveSession resolves session_id via mgr.Status verbatim (D-08) — the
// SESS-06 "not found or expired" text and StatusResult{TmpDir,State,Error}
// flow through untouched, zero new pkg/session API. The first call in
// every query-tool handler's body (18-PATTERNS.md "Shared Patterns").
func resolveSession(mgr *session.Manager, sessionID string) (session.StatusResult, error) {
	return mgr.Status(sessionID)
}

// availableAfterStopMarker is the shared non-error placeholder shape for a
// query-tool section with no data yet because the session is still
// state=="capturing" (D-02) — cluster-health.json (and any equivalent
// mid-capture aggregate) is finalize-only; there is no live in-memory
// counter to serve instead. get_cluster_health (Task 2) embeds this
// directly; list_dropped_flows's aggregates half (18-05) reuses the
// identical field names/semantics for its own capturing-state marker —
// same field, same meaning, never an error.
type availableAfterStopMarker struct {
	AvailableAfterStop bool   `json:"available_after_stop,omitempty"`
	Message            string `json:"message,omitempty" jsonschema:"context for a non-report result: why no data is available yet"`
}

// ---- list_policies (QRY-02) ----

// policyMetaRow is one list_policies row: everything an LLM needs to decide
// whether to call get_policy for the full YAML, without fetching it first.
type policyMetaRow struct {
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	Name      string `json:"name" jsonschema:"the CiliumNetworkPolicy's metadata.name"`
	// Directions lists which of ingress/egress this policy carries rules
	// for — derived from len(Spec.Ingress)>0 / len(Spec.Egress)>0.
	Directions       []string `json:"directions" jsonschema:"which of ingress/egress this policy carries rules for"`
	IngressRuleCount int      `json:"ingress_rule_count"`
	EgressRuleCount  int      `json:"egress_rule_count"`
	Path             string   `json:"path" jsonschema:"absolute path to the policy YAML file on disk"`
}

// listPoliciesResult is list_policies' structuredContent shape (D-17).
type listPoliciesResult struct {
	Policies []policyMetaRow `json:"policies"`
}

func handleListPolicies(mgr *session.Manager, args sessionRef) (*mcp.CallToolResult, listPoliciesResult, error) {
	status, err := resolveSession(mgr, args.SessionID)
	if err != nil {
		return nil, listPoliciesResult{}, err
	}

	policiesDir := filepath.Join(status.TmpDir, "policies")
	nsEntries, err := os.ReadDir(policiesDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// No policy has been written yet — an empty list, not an error.
			return nil, listPoliciesResult{Policies: []policyMetaRow{}}, nil
		}
		return nil, listPoliciesResult{}, fmt.Errorf("listing policies directory %s: %w", policiesDir, err)
	}

	rows := make([]policyMetaRow, 0, len(nsEntries))
	for _, nsEntry := range nsEntries {
		if !nsEntry.IsDir() {
			continue
		}
		namespace := nsEntry.Name()
		nsDir := filepath.Join(policiesDir, namespace)
		files, err := os.ReadDir(nsDir)
		if err != nil {
			// Best-effort listing (D-06): a namespace directory that became
			// unreadable between the outer ReadDir and here (an active
			// capture may be writing concurrently) is logged and skipped,
			// never fails the whole listing.
			logger.Warn("list_policies: reading namespace directory failed, skipping",
				zap.String("dir", nsDir), zap.Error(err))
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
				continue // also skips atomic-write .tmp-* siblings (writer.go)
			}
			workload := strings.TrimSuffix(f.Name(), ".yaml")
			path := filepath.Join(nsDir, f.Name())
			cnp, err := output.ReadPolicyFile(path)
			if err != nil {
				// Best-effort (D-06): a single unreadable/mid-write policy
				// file is logged and skipped rather than failing the whole
				// listing — drift-during-write tolerant.
				logger.Warn("list_policies: skipping unreadable policy file",
					zap.String("path", path), zap.Error(err))
				continue
			}
			rows = append(rows, policyRowFromCNP(namespace, workload, path, cnp))
		}
	}

	return nil, listPoliciesResult{Policies: rows}, nil
}

// policyRowFromCNP builds one list_policies row from a parsed CNP. cnp.Spec
// is a *api.Rule pointer (nil-guarded here) — a policy with no rules at all
// would otherwise panic on len(nil.Ingress).
func policyRowFromCNP(namespace, workload, path string, cnp *ciliumv2.CiliumNetworkPolicy) policyMetaRow {
	row := policyMetaRow{
		Namespace:  namespace,
		Workload:   workload,
		Name:       cnp.Name,
		Path:       path,
		Directions: []string{},
	}
	if cnp.Spec != nil {
		row.IngressRuleCount = len(cnp.Spec.Ingress)
		row.EgressRuleCount = len(cnp.Spec.Egress)
		if row.IngressRuleCount > 0 {
			row.Directions = append(row.Directions, "ingress")
		}
		if row.EgressRuleCount > 0 {
			row.Directions = append(row.Directions, "egress")
		}
	}
	return row
}

// ---- get_policy (QRY-02/D-11/D-17) ----

// getPolicyArgs is get_policy's argument surface — all three fields
// required (no omitempty; D-11).
type getPolicyArgs struct {
	SessionID string `json:"session_id" jsonschema:"the opaque session_id returned by start_session"`
	Namespace string `json:"namespace" jsonschema:"the policy's namespace, as returned by list_policies"`
	Workload  string `json:"workload" jsonschema:"the policy's workload name, as returned by list_policies"`
}

// getPolicyResult is get_policy's structuredContent shape (D-17).
type getPolicyResult struct {
	Namespace        string `json:"namespace"`
	Workload         string `json:"workload"`
	Name             string `json:"name" jsonschema:"the CiliumNetworkPolicy's metadata.name"`
	YAML             string `json:"yaml" jsonschema:"the full CiliumNetworkPolicy YAML document"`
	Path             string `json:"path" jsonschema:"absolute path to the policy YAML file on disk"`
	IngressRuleCount int    `json:"ingress_rule_count"`
	EgressRuleCount  int    `json:"egress_rule_count"`
}

func handleGetPolicy(mgr *session.Manager, args getPolicyArgs) (*mcp.CallToolResult, getPolicyResult, error) {
	status, err := resolveSession(mgr, args.SessionID)
	if err != nil {
		return nil, getPolicyResult{}, err
	}

	// Path-traversal guard (T-18-03-01) BEFORE any filepath.Join — the exact
	// write-side guard (pkg/output/writer.go) reused symmetrically here on
	// the read side.
	if err := evidence.ValidatePolicyRef(args.Namespace, args.Workload); err != nil {
		return nil, getPolicyResult{}, err
	}

	path := filepath.Join(status.TmpDir, "policies", args.Namespace, args.Workload+".yaml")
	cnp, err := output.ReadPolicyFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, getPolicyResult{}, fmt.Errorf(
				"no policy found for %s/%s at %s; call list_policies to see available namespace/workload pairs",
				args.Namespace, args.Workload, path)
		}
		return nil, getPolicyResult{}, err
	}

	yamlBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, getPolicyResult{}, fmt.Errorf("reading policy YAML %s: %w", path, err)
	}

	result := getPolicyResult{
		Namespace: args.Namespace,
		Workload:  args.Workload,
		Name:      cnp.Name,
		YAML:      string(yamlBytes),
		Path:      path,
	}
	if cnp.Spec != nil {
		result.IngressRuleCount = len(cnp.Spec.Ingress)
		result.EgressRuleCount = len(cnp.Spec.Egress)
	}
	return nil, result, nil
}

// ---- get_cluster_health (QRY-04/D-13) ----

// getClusterHealthResult is get_cluster_health's structuredContent shape
// (D-17): exactly one of Report (state=stopped+file present, passthrough),
// AvailableAfterStop (state=capturing), or NoDrops (state=stopped+file
// absent+no pipeline error) is populated per call — one typed struct so the
// SDK infers a single outputSchema, never a hand-crafted dual preview/
// content block.
type getClusterHealthResult struct {
	availableAfterStopMarker
	// NoDrops is true for the common "session stopped, cluster-health.json
	// was never written because zero infra/transient drops occurred" case
	// (D-13's corrected 3-way branch) — never an error.
	NoDrops bool                        `json:"no_drops,omitempty" jsonschema:"true when no infra/transient drops were observed this session (not a failure)"`
	Report  *hubble.ClusterHealthReport `json:"report,omitempty" jsonschema:"the finalized cluster-health report; present only once the session is stopped and drops were observed"`
}

func handleGetClusterHealth(mgr *session.Manager, args sessionRef) (*mcp.CallToolResult, getClusterHealthResult, error) {
	status, err := resolveSession(mgr, args.SessionID)
	if err != nil {
		return nil, getClusterHealthResult{}, err
	}

	// Inline recompute of pkg/session/manager.go Stop's exact formula — D-08:
	// zero new Manager API, the outputHash/healthPath derivation is
	// deterministic and shared with Stop's own StopResult.ClusterHealthPath.
	outputHash := evidence.HashOutputDir(filepath.Join(status.TmpDir, "policies"))
	healthPath := filepath.Join(status.TmpDir, "evidence", outputHash, "cluster-health.json")

	result, err := clusterHealthBranch(status, healthPath)
	return nil, result, err
}

// clusterHealthBranch implements D-13's corrected 3-way branch (4 states
// counting "capturing") as a pure function over already-resolved session
// state plus a filesystem read — factored out of the tool handler so the
// branch logic itself is directly unit-testable without a real session or
// pipeline (see TestClusterHealthBranch).
func clusterHealthBranch(status session.StatusResult, healthPath string) (getClusterHealthResult, error) {
	if status.State == session.StateCapturing.String() {
		return getClusterHealthResult{
			availableAfterStopMarker: availableAfterStopMarker{
				AvailableAfterStop: true,
				Message:            "cluster health is finalized only after stop_session; call stop_session first",
			},
		}, nil
	}

	report, err := hubble.ReadClusterHealth(healthPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if status.Error == "" {
				// The common case: a healthy session with zero infra/
				// transient drops never writes cluster-health.json at all
				// (pkg/hubble/health_writer.go's finalize() no-ops when
				// zero drops were accumulated) — not a failure, the
				// Pitfall-1 correction to a naive binary framing.
				return getClusterHealthResult{
					NoDrops: true,
					availableAfterStopMarker: availableAfterStopMarker{
						Message: "no infra/transient drops observed this session",
					},
				}, nil
			}
			// Genuine crash-before-any-drop: cite the pipeline's own
			// terminal error (SESS-06-adjacent surfacing, D-16).
			return getClusterHealthResult{}, fmt.Errorf(
				"cluster health unavailable: session ended with error before any drop was recorded: %s", status.Error)
		}
		// Any other error (parse failure, unsupported schema_version) —
		// return it as-is, isError (D-16).
		return getClusterHealthResult{}, err
	}

	return getClusterHealthResult{Report: report}, nil
}
