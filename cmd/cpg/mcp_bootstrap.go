package main

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/session"
)

// bootstrapArgs is get_bootstrap_policy's argument surface: namespace is
// required, no omitempty (D-11 pattern, mirrored from mcp_query.go's
// getPolicyArgs).
type bootstrapArgs struct {
	Namespace string `json:"namespace" jsonschema:"target namespace (required)"`
}

// bootstrapResult is get_bootstrap_policy's structuredContent shape (D-17):
// the CNP YAML plus the detected version/compat fields, so a caller can see
// both the artifact and the gate decision that produced it in one response.
type bootstrapResult struct {
	Namespace          string   `json:"namespace"`
	YAML               string   `json:"yaml" jsonschema:"the full CiliumNetworkPolicy YAML document"`
	ClusterVersion     string   `json:"cluster_version,omitempty" jsonschema:"the detected minimum Cilium version across the cluster; empty when undetermined"`
	VersionSource      string   `json:"version_source" jsonschema:"how cluster_version was obtained: pod-images, get-nodes, or undetermined"`
	BelowFloorFeatures []string `json:"below_floor_features,omitempty" jsonschema:"feature floors the detected version does not meet"`
	VersionWarning     string   `json:"version_warning,omitempty" jsonschema:"non-empty when the version was undetermined and the artifact was generated with a warning"`
}

// registerBootstrapTool registers the readonly get_bootstrap_policy MCP
// tool on server. Wired into runMCPServer right after registerQueryTools
// (cmd/cpg/mcp.go). Unlike the query tools, bootstrap is NOT session-bound:
// it takes only a namespace and performs its own version detection, so mgr
// is unused here but kept in the signature for registration-shape
// consistency with registerQueryTools/registerSessionTools.
func registerBootstrapTool(server *mcp.Server, _ *session.Manager) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_bootstrap_policy",
		Description: "Returns a namespaced default-deny CiliumNetworkPolicy as YAML " +
			"(enableDefaultDeny + explicit ingress/egress presence, cilium/cilium#35558-safe). " +
			"Detects the cluster's Cilium version; refuses below the 1.16 floor when the version " +
			"is determined, proceeds with a warning when undetermined.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  jsonschema.Ptr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args bootstrapArgs) (*mcp.CallToolResult, bootstrapResult, error) {
		return handleGetBootstrapPolicy(ctx, args)
	})
}

// handleGetBootstrapPolicy validates args.Namespace, applies the shared
// bootstrapVersionGate (bootstrap.go, reused verbatim so the CLI and MCP
// surfaces can never drift), and returns the marshaled CNP YAML plus compat
// info. NEVER touches the filesystem — the YAML is returned as tool-result
// content only, keeping this handler's write surface at zero for SEC-01.
func handleGetBootstrapPolicy(ctx context.Context, args bootstrapArgs) (*mcp.CallToolResult, bootstrapResult, error) {
	// D-16: validate before I/O.
	if args.Namespace == "" {
		return nil, bootstrapResult{}, fmt.Errorf("namespace is required")
	}

	compat := bootstrapDetectVersion(ctx, logger)
	warning, err := bootstrapVersionGate(compat)
	if err != nil {
		return nil, bootstrapResult{}, err
	}

	cnp := policy.BuildBootstrapPolicy(args.Namespace)
	data, err := yaml.Marshal(cnp)
	if err != nil {
		return nil, bootstrapResult{}, fmt.Errorf("marshaling bootstrap policy: %w", err)
	}

	return nil, bootstrapResult{
		Namespace:          args.Namespace,
		YAML:               string(data),
		ClusterVersion:     compat.ClusterVersion,
		VersionSource:      compat.Source,
		BelowFloorFeatures: compat.BelowFloorFeatures,
		VersionWarning:     warning,
	}, nil
}
