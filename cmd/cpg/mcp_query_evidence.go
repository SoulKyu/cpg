package main

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/explain"
	"github.com/SoulKyu/cpg/pkg/session"
)

// getEvidenceArgs is get_evidence's argument surface (QRY-03/D-10):
// session_id/namespace/workload are required (no omitempty). The remaining
// fields mirror the REAL explain.Filter fields exactly, the same set
// cmd/cpg/explain.go's buildFilter reads off cobra flags — direction, port,
// peer (KEY=VAL), peer_cidr, http_method, http_path, dns_pattern — plus
// limit/cursor for pagination.
//
// There is deliberately NO protocol field: explain.Filter has no Protocol
// field (its fields are Direction/Port/PeerLabel/PeerCIDR/Since/Now/
// HTTPMethod/HTTPPath/DNSPattern), so a `protocol` schema property would be
// a dead field with nothing behind it — a QRY-05/D-16 truthful-behavior
// violation the plan-checker caught during planning.
type getEvidenceArgs struct {
	SessionID  string `json:"session_id" jsonschema:"the opaque session_id returned by start_session"`
	Namespace  string `json:"namespace" jsonschema:"the policy's namespace, as returned by list_policies"`
	Workload   string `json:"workload" jsonschema:"the policy's workload name, as returned by list_policies"`
	Direction  string `json:"direction,omitempty" jsonschema:"filter: ingress or egress"`
	Port       string `json:"port,omitempty" jsonschema:"filter: rules using this port"`
	Peer       string `json:"peer,omitempty" jsonschema:"filter: endpoint peer label, KEY=VAL"`
	PeerCIDR   string `json:"peer_cidr,omitempty" jsonschema:"filter: CIDR peer contained in this CIDR"`
	HTTPMethod string `json:"http_method,omitempty" jsonschema:"filter: rules attributed to this HTTP method (case-insensitive)"`
	HTTPPath   string `json:"http_path,omitempty" jsonschema:"filter: rules attributed to this HTTP path (literal exact match)"`
	DNSPattern string `json:"dns_pattern,omitempty" jsonschema:"filter: rules attributed to this DNS matchName (trailing dot stripped)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max rules per page (default 20, max 100)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"opaque pagination token from a previous call's next_cursor"`
}

// getEvidenceResult is get_evidence's structuredContent shape (D-17): the
// same policy/sessions envelope pkg/explain.Output carries, but MatchedRules
// is the PAGE, not the full matched set — pagination metadata wraps around
// the promoted renderer's output (Pattern 2), it is never baked into
// pkg/explain itself. Per-record shape is byte-identical to
// `cpg explain --output json`'s matched_rules entries (QRY-03).
type getEvidenceResult struct {
	Policy       evidence.PolicyRef      `json:"policy"`
	Sessions     []evidence.SessionInfo  `json:"sessions"`
	MatchedRules []evidence.RuleEvidence `json:"matched_rules"`
	TotalCount   int                     `json:"total_count" jsonschema:"count of every matched rule across all pages, not just this page"`
	HasMore      bool                    `json:"has_more"`
	NextCursor   string                  `json:"next_cursor,omitempty" jsonschema:"pass verbatim as cursor to fetch the next page; absent on the last page"`
}

// registerGetEvidenceTool registers get_evidence (QRY-03) on server, wired to
// mgr. Kept in its own function — unlike the 3 tools registered inline in
// registerQueryTools (mcp_query.go) — because its InputSchema requires the
// mustQuerySchema enum-patching mechanism (D-14): building it here keeps the
// schema construction next to the args struct and handler it describes.
func registerGetEvidenceTool(server *mcp.Server, mgr *session.Manager) {
	schema := mustQuerySchema[getEvidenceArgs](map[string][]any{
		"direction": {"ingress", "egress"},
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_evidence",
		Description: "Returns paginated per-rule flow evidence for one generated policy — " +
			"per-record shape identical to `cpg explain --output json` (discover available " +
			"namespace/workload pairs via list_policies first). Only policy-actionable drops " +
			"ever produce evidence; infra/transient/noise drops the classifier suppressed " +
			"never reach this tool (see get_cluster_health for those counts instead). " +
			"Optional filters (direction/port/peer/peer_cidr/http_method/http_path/" +
			"dns_pattern) AND-narrow the matched rule set; pagination applies to the " +
			"matched rules, not the whole evidence file. The evidence file set may shift " +
			"between calls during an active capture (best-effort re-scan, no server-side " +
			"snapshot) — an invalid or stale cursor returns an actionable error rather " +
			"than a panic, and if a target list_policies just showed you comes back " +
			"not-found, the pipeline may be mid-flush; retry.",
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  jsonschema.Ptr(false),
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, args getEvidenceArgs) (*mcp.CallToolResult, getEvidenceResult, error) {
		return handleGetEvidence(mgr, args)
	})
}

// indexedRuleEvidence pairs a matched RuleEvidence with its position in the
// evidence file's own (unfiltered) Rules array — the "stable in-file index"
// half of the D-05 boundary-key cursor. pkg/evidence's Merge keeps Rules
// sorted by (Direction, Key) after every write, so this index is stable
// across re-scans as long as the rule set itself doesn't change; a
// concurrent insert can shift it by one, which the cursor's
// scan-past-the-boundary resumption (paginate) tolerates gracefully (D-06) —
// exactly the degrade-gracefully trade-off the anti-pattern section of
// 18-RESEARCH.md documents.
type indexedRuleEvidence struct {
	rule evidence.RuleEvidence
	idx  int
}

func handleGetEvidence(mgr *session.Manager, args getEvidenceArgs) (*mcp.CallToolResult, getEvidenceResult, error) {
	status, err := resolveSession(mgr, args.SessionID)
	if err != nil {
		return nil, getEvidenceResult{}, err
	}

	// Path-traversal guard (T-18-04-01) BEFORE any filepath.Join — the same
	// write-side guard (pkg/output/writer.go) reused symmetrically on the
	// read side (18-PATTERNS.md "Shared Patterns").
	if err := evidence.ValidatePolicyRef(args.Namespace, args.Workload); err != nil {
		return nil, getEvidenceResult{}, err
	}

	// D-08: re-derive outputHash/evidenceDir with the exact formula
	// buildPipelineConfig/Manager.Stop already use — zero new pkg/session API.
	outputHash := evidence.HashOutputDir(filepath.Join(status.TmpDir, "policies"))
	evidenceDir := filepath.Join(status.TmpDir, "evidence")
	reader := evidence.NewReader(evidenceDir, outputHash)

	pe, err := reader.Read(args.Namespace, args.Workload)
	if err != nil {
		if evidence.IsNotExist(err) {
			return nil, getEvidenceResult{}, fmt.Errorf(
				"no evidence for %s/%s — call list_policies to see available targets",
				args.Namespace, args.Workload)
		}
		return nil, getEvidenceResult{}, err
	}

	filter, err := buildEvidenceFilter(args)
	if err != nil {
		return nil, getEvidenceResult{}, err
	}

	matched := make([]indexedRuleEvidence, 0, len(pe.Rules))
	for i, r := range pe.Rules {
		if filter.Match(r) {
			matched = append(matched, indexedRuleEvidence{rule: r, idx: i})
		}
	}

	var after *paginateBoundaryKey
	if args.Cursor != "" {
		key, err := decodeCursor(args.Cursor)
		if err != nil {
			return nil, getEvidenceResult{}, err
		}
		after = &key
	}

	// Namespace/workload are constant across every matched rule (one
	// evidence file per call, D-10) — the boundary key's Index alone carries
	// the resume position (Pattern 2: pagination wraps the promoted
	// renderer's full matched-rule output, never baked into pkg/explain).
	keyOf := func(ir indexedRuleEvidence) paginateBoundaryKey {
		return paginateBoundaryKey{Namespace: args.Namespace, Workload: args.Workload, Index: ir.idx}
	}
	page, nextCursor, hasMore, totalCount := paginate(matched, keyOf, after, args.Limit, defaultEvidenceLimit, maxEvidenceLimit)

	pageRules := make([]evidence.RuleEvidence, len(page))
	for i, ir := range page {
		pageRules[i] = ir.rule
	}

	return nil, getEvidenceResult{
		Policy:       pe.Policy,
		Sessions:     pe.Sessions,
		MatchedRules: pageRules,
		TotalCount:   totalCount,
		HasMore:      hasMore,
		NextCursor:   nextCursor,
	}, nil
}

// buildEvidenceFilter constructs an explain.Filter from getEvidenceArgs,
// mirroring cmd/cpg/explain.go's buildFilter construction exactly (D-10) —
// same fields, same L7 normalization (uppercase http_method, trailing-dot-
// stripped dns_pattern), same explain.ParsePeerLabel peer parse, same
// net.ParseCIDR peer_cidr parse. There is no Since/Now population: D-03
// excludes a time-range filter from get_evidence's argument surface, so
// explain.Filter's Since/Now stay at their zero value, which Filter.Match
// already treats as "unset".
func buildEvidenceFilter(args getEvidenceArgs) (explain.Filter, error) {
	f := explain.Filter{Direction: args.Direction, Port: args.Port}

	if args.Peer != "" {
		k, v, ok := explain.ParsePeerLabel(args.Peer)
		if !ok {
			return explain.Filter{}, fmt.Errorf("peer must be KEY=VAL, got %q", args.Peer)
		}
		f.PeerLabel.Set = true
		f.PeerLabel.Key = k
		f.PeerLabel.Value = v
	}

	if args.PeerCIDR != "" {
		_, ipnet, err := net.ParseCIDR(args.PeerCIDR)
		if err != nil {
			return explain.Filter{}, fmt.Errorf("peer_cidr %q: %w", args.PeerCIDR, err)
		}
		f.PeerCIDR = ipnet
	}

	f.HTTPMethod = strings.ToUpper(strings.TrimSpace(args.HTTPMethod))
	f.HTTPPath = args.HTTPPath
	f.DNSPattern = strings.TrimSuffix(strings.TrimSpace(args.DNSPattern), ".")

	return f, nil
}
