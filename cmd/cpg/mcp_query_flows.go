package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/dropclass"
	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/hubble"
	"github.com/SoulKyu/cpg/pkg/session"
)

// listDroppedFlowsArgs is list_dropped_flows' argument surface (QRY-01/D-03):
// only session_id is required; namespace/workload/dropclass/direction are
// optional AND-combined filters, and limit/cursor drive pagination over the
// combined samples+aggregates view (D-07).
type listDroppedFlowsArgs struct {
	SessionID string `json:"session_id" jsonschema:"the opaque session_id returned by start_session"`
	Namespace string `json:"namespace,omitempty" jsonschema:"filter: namespace — applies to both samples and aggregates"`
	Workload  string `json:"workload,omitempty" jsonschema:"filter: workload — applies to both samples and aggregates"`
	DropClass string `json:"dropclass,omitempty" jsonschema:"filter: policy-actionable vs infra/transient/noise/unknown (see tool description); applies to both samples and aggregates"`
	Direction string `json:"direction,omitempty" jsonschema:"filter: ingress or egress — applies to the samples half ONLY (aggregates carry no direction data, see tool description)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max rows per page across samples+aggregates combined (default 50, max 200)"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"opaque pagination token from a previous call's next_cursor"`
}

// DroppedFlowSample is one flattened samples[] record: a raw
// evidence.FlowSample enriched with its parent RuleEvidence's
// namespace/workload/direction/peer/port/protocol context — a FlowSample
// alone carries none of those (pkg/evidence/schema.go). snake_case JSON tags
// match the session-tools/query-tools convention.
type DroppedFlowSample struct {
	Namespace  string                `json:"namespace"`
	Workload   string                `json:"workload"`
	Direction  string                `json:"direction" jsonschema:"ingress or egress, from the parent rule"`
	Peer       evidence.PeerRef      `json:"peer"`
	Port       string                `json:"port"`
	Protocol   string                `json:"protocol"`
	Time       time.Time             `json:"time"`
	Src        evidence.FlowEndpoint `json:"src"`
	Dst        evidence.FlowEndpoint `json:"dst"`
	Verdict    string                `json:"verdict"`
	DropReason string                `json:"drop_reason,omitempty"`
}

// droppedFlowAggregateRow is one flattened aggregates[] record: cluster
// health's Drops[].ByWorkload map split into a per-(namespace, workload,
// reason, class, count) row (D-01).
type droppedFlowAggregateRow struct {
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	Reason    string `json:"reason"`
	Class     string `json:"class"`
	Count     uint64 `json:"count"`
}

// listDroppedFlowsAggregates is the aggregates section of
// listDroppedFlowsResult: EITHER the shared availableAfterStopMarker
// (state=="capturing", D-02 — mirrors get_cluster_health exactly) OR Rows
// (state=="stopped") — never both populated at once. Embedding the marker
// keeps this consistent with getClusterHealthResult's own established shape
// (mcp_query.go).
type listDroppedFlowsAggregates struct {
	availableAfterStopMarker
	Rows []droppedFlowAggregateRow `json:"rows,omitempty" jsonschema:"per-(namespace,workload,reason) infra/transient counts; absent while capturing (see available_after_stop) or when zero rows match the filters"`
}

// listDroppedFlowsResult is list_dropped_flows' structuredContent shape
// (D-17): Samples and Aggregates.Rows are the PAGE — pagination applies to
// the combined, filtered samples+aggregates view as one set (D-04/D-07),
// never to each section independently.
type listDroppedFlowsResult struct {
	Samples    []DroppedFlowSample        `json:"samples"`
	Aggregates listDroppedFlowsAggregates `json:"aggregates"`
	TotalCount int                        `json:"total_count" jsonschema:"count of every item (samples+aggregate rows) in this filtered view across all pages — NOT a true flow total (see get_status/stop_session flows_seen for that, D-04)"`
	HasMore    bool                       `json:"has_more"`
	NextCursor string                     `json:"next_cursor,omitempty" jsonschema:"pass verbatim as cursor to fetch the next page; absent on the last page"`
}

// registerListDroppedFlowsTool registers list_dropped_flows (QRY-01) on
// server, wired to mgr. Kept in its own function — like
// registerGetEvidenceTool (mcp_query_evidence.go) — because its InputSchema
// needs the mustQuerySchema enum-patching mechanism (D-14) for BOTH
// dropclass and direction.
func registerListDroppedFlowsTool(server *mcp.Server, mgr *session.Manager) {
	schema := mustQuerySchema[listDroppedFlowsArgs](map[string][]any{
		// pkg/dropclass.DropClass.String()'s 5 values (D-14) — single source
		// of truth, referenced here as the literal enum list per this
		// phase's established mustQuerySchema convention (mcp_query_evidence.go).
		"dropclass": {"policy", "infra", "transient", "noise", "unknown"},
		"direction": {"ingress", "egress"},
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_dropped_flows",
		Description: "Returns a two-section composed view of this session's dropped flows: " +
			"samples[] (individual flow records flattened from the capped per-rule evidence — " +
			"policy-actionable drops that produced, or would produce, a CiliumNetworkPolicy rule) " +
			"and aggregates[] (per-namespace/workload/reason counts of infra/transient noise the " +
			"classifier excluded from policy generation entirely — see get_cluster_health for the " +
			"full report). The two sections are never derived from each other: dropclass=policy/" +
			"unknown flows only ever populate samples[], dropclass=infra/transient flows only ever " +
			"populate aggregates[], and dropclass=noise never appears in either (discarded as " +
			"internal bookkeeping) — an empty result for one of those combinations is by design, " +
			"not a bug. This is a sampled/aggregated view, not a raw flow log: samples[] is capped " +
			"per rule (FIFO) and aggregates[] is finalize-only. While capturing, aggregates carries " +
			"an available_after_stop marker (call stop_session first) though samples[] is still " +
			"served live. The optional direction filter (ingress|egress) narrows samples[] only — " +
			"cluster-health.json has no direction dimension, so aggregate rows are always returned " +
			"regardless of the direction filter, never silently hidden. namespace/workload/dropclass " +
			"filters are AND-combined and apply to both sections identically. Pagination (limit/" +
			"cursor/total_count/has_more) applies to the combined samples+aggregates view as one " +
			"set; total_count counts filtered view items, not true flow totals (see get_status/" +
			"stop_session for those). The underlying file set may shift between calls during an " +
			"active capture (best-effort re-scan, no server-side snapshot); an invalid or stale " +
			"cursor returns an actionable error rather than a panic.",
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  jsonschema.Ptr(false),
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, args listDroppedFlowsArgs) (*mcp.CallToolResult, listDroppedFlowsResult, error) {
		return handleListDroppedFlows(mgr, args)
	})
}

// droppedFlowItem is one element of the combined, filtered, deterministically
// sorted samples+aggregates view — the D-07 single pagination window both
// response sections share (see buildCombinedItems).
type droppedFlowItem struct {
	namespace string
	workload  string
	index     int
	isSample  bool
	sample    DroppedFlowSample
	aggregate droppedFlowAggregateRow
}

func handleListDroppedFlows(mgr *session.Manager, args listDroppedFlowsArgs) (*mcp.CallToolResult, listDroppedFlowsResult, error) {
	status, err := resolveSession(mgr, args.SessionID)
	if err != nil {
		return nil, listDroppedFlowsResult{}, err
	}

	// Path-traversal guard (T-18-05-01) on any SET namespace/workload filter,
	// before any path use — reusing evidence.ValidatePolicyRef's exact
	// traversal rule symmetrically with the write side.
	if err := validateFilterComponent("namespace", args.Namespace); err != nil {
		return nil, listDroppedFlowsResult{}, err
	}
	if err := validateFilterComponent("workload", args.Workload); err != nil {
		return nil, listDroppedFlowsResult{}, err
	}

	// D-08: re-derive outputHash/evidenceDir with the exact formula
	// buildPipelineConfig/Manager.Stop/get_evidence already use.
	outputHash := evidence.HashOutputDir(filepath.Join(status.TmpDir, "policies"))
	evidenceDir := filepath.Join(status.TmpDir, "evidence")

	samples, err := collectDroppedFlowSamples(evidenceDir, outputHash)
	if err != nil {
		return nil, listDroppedFlowsResult{}, err
	}

	var aggregateRows []droppedFlowAggregateRow
	var aggregatesMarker availableAfterStopMarker
	if status.State == session.StateCapturing.String() {
		// D-02: never call ReadClusterHealth mid-capture — the file may be
		// absent for reasons unrelated to a crash (Pitfall 1); the samples
		// half above is already served live regardless.
		aggregatesMarker = availableAfterStopMarker{
			AvailableAfterStop: true,
			Message:            "aggregates are finalized only after stop_session; call stop_session first (samples above are already live)",
		}
	} else {
		healthPath := filepath.Join(evidenceDir, outputHash, "cluster-health.json")
		report, err := hubble.ReadClusterHealth(healthPath)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				// A genuinely malformed/wrong-version file — distinct from
				// absence — is a real error (mirrors clusterHealthBranch's
				// own "any other error" fallthrough).
				return nil, listDroppedFlowsResult{}, err
			}
			// Absent means zero infra/transient drops this session — not an
			// error, consistent with QRY-04's 3-way branch (Pitfall 1).
		} else {
			aggregateRows = collectDroppedFlowAggregates(report)
		}
	}

	filteredSamples := make([]DroppedFlowSample, 0, len(samples))
	for _, s := range samples {
		if !matchesNamespaceWorkload(args.Namespace, args.Workload, s.Namespace, s.Workload) {
			continue
		}
		if args.Direction != "" && s.Direction != args.Direction {
			continue
		}
		if !matchesDropClass(classifyDropReasonName(s.DropReason), args.DropClass) {
			continue
		}
		filteredSamples = append(filteredSamples, s)
	}

	filteredAggregates := make([]droppedFlowAggregateRow, 0, len(aggregateRows))
	for _, r := range aggregateRows {
		if !matchesNamespaceWorkload(args.Namespace, args.Workload, r.Namespace, r.Workload) {
			continue
		}
		// direction is deliberately NOT applied here — aggregates carry no
		// direction dimension; hiding rows on an unrelated filter would
		// silently under-report infra/transient counts (Pitfall 2/T-18-05-04).
		if args.DropClass != "" && r.Class != args.DropClass {
			continue
		}
		filteredAggregates = append(filteredAggregates, r)
	}

	items := buildCombinedItems(filteredSamples, filteredAggregates)

	var after *paginateBoundaryKey
	if args.Cursor != "" {
		key, err := decodeCursor(args.Cursor)
		if err != nil {
			return nil, listDroppedFlowsResult{}, err
		}
		after = &key
	}

	keyOf := func(it droppedFlowItem) paginateBoundaryKey {
		return paginateBoundaryKey{Namespace: it.namespace, Workload: it.workload, Index: it.index}
	}
	page, nextCursor, hasMore, totalCount := paginate(items, keyOf, after, args.Limit, defaultFlowLimit, maxFlowLimit)

	pageSamples := make([]DroppedFlowSample, 0, len(page))
	pageAggregates := make([]droppedFlowAggregateRow, 0, len(page))
	for _, it := range page {
		if it.isSample {
			pageSamples = append(pageSamples, it.sample)
		} else {
			pageAggregates = append(pageAggregates, it.aggregate)
		}
	}

	return nil, listDroppedFlowsResult{
		Samples: pageSamples,
		Aggregates: listDroppedFlowsAggregates{
			availableAfterStopMarker: aggregatesMarker,
			Rows:                     pageAggregates,
		},
		TotalCount: totalCount,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// validateFilterComponent guards an optional namespace/workload filter value
// against directory-traversal before any path use (T-18-05-01), reusing
// evidence.ValidatePolicyRef's exact traversal rule. ValidatePolicyRef
// requires BOTH arguments non-empty (get_policy/get_evidence's contract,
// where namespace/workload are always both required) — list_dropped_flows's
// namespace/workload filters are each independently optional, so an empty
// filter here means "no filter set", not an invalid path component. A safe
// placeholder ("_", which itself always passes the traversal check) stands
// in for the OTHER, unset argument so only the actually-supplied value's
// real characters are validated.
func validateFilterComponent(kind, value string) error {
	if value == "" {
		return nil
	}
	if kind == "namespace" {
		return evidence.ValidatePolicyRef(value, "_")
	}
	return evidence.ValidatePolicyRef("_", value)
}

// collectDroppedFlowSamples walks <evidenceDir>/<outputHash>/<ns>/<wl>.json —
// exactly list_policies' directory-walk pattern (mcp_query.go) applied to
// the evidence tree instead of the policies tree — flattening every
// PolicyEvidence/RuleEvidence/FlowSample into a DroppedFlowSample. A missing
// evidence directory (zero evidence written yet) yields an empty slice, not
// an error. A single unreadable namespace directory or evidence file is
// logged and skipped (best-effort, D-06) rather than failing the whole call.
func collectDroppedFlowSamples(evidenceDir, outputHash string) ([]DroppedFlowSample, error) {
	hashDir := filepath.Join(evidenceDir, outputHash)
	nsEntries, err := os.ReadDir(hashDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []DroppedFlowSample{}, nil
		}
		return nil, fmt.Errorf("listing evidence directory %s: %w", hashDir, err)
	}

	reader := evidence.NewReader(evidenceDir, outputHash)
	samples := make([]DroppedFlowSample, 0)
	for _, nsEntry := range nsEntries {
		if !nsEntry.IsDir() {
			continue // skips cluster-health.json, the sibling file at this level
		}
		namespace := nsEntry.Name()
		nsDir := filepath.Join(hashDir, namespace)
		files, err := os.ReadDir(nsDir)
		if err != nil {
			logger.Warn("list_dropped_flows: reading namespace directory failed, skipping",
				zap.String("dir", nsDir), zap.Error(err))
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			workload := strings.TrimSuffix(f.Name(), ".json")
			pe, err := reader.Read(namespace, workload)
			if err != nil {
				logger.Warn("list_dropped_flows: skipping unreadable evidence file",
					zap.String("namespace", namespace), zap.String("workload", workload), zap.Error(err))
				continue
			}
			for _, rule := range pe.Rules {
				for _, sample := range rule.Samples {
					samples = append(samples, DroppedFlowSample{
						Namespace:  namespace,
						Workload:   workload,
						Direction:  rule.Direction,
						Peer:       rule.Peer,
						Port:       rule.Port,
						Protocol:   rule.Protocol,
						Time:       sample.Time,
						Src:        sample.Src,
						Dst:        sample.Dst,
						Verdict:    sample.Verdict,
						DropReason: sample.DropReason,
					})
				}
			}
		}
	}
	return samples, nil
}

// collectDroppedFlowAggregates flattens report.Drops[].ByWorkload — each
// "<namespace>/<workload>" (or "_unknown/<workload>") key — into one row per
// (namespace, workload, reason), sorted deterministically for D-06/D-07's
// stable-pagination-order requirement (map iteration order is otherwise
// undefined in Go).
func collectDroppedFlowAggregates(report *hubble.ClusterHealthReport) []droppedFlowAggregateRow {
	rows := make([]droppedFlowAggregateRow, 0)
	for _, drop := range report.Drops {
		for wkey, count := range drop.ByWorkload {
			ns, workload := splitWorkloadKey(wkey)
			rows = append(rows, droppedFlowAggregateRow{
				Namespace: ns,
				Workload:  workload,
				Reason:    drop.Reason,
				Class:     drop.Class,
				Count:     count,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Namespace != rows[j].Namespace {
			return rows[i].Namespace < rows[j].Namespace
		}
		if rows[i].Workload != rows[j].Workload {
			return rows[i].Workload < rows[j].Workload
		}
		return rows[i].Reason < rows[j].Reason
	})
	return rows
}

// splitWorkloadKey splits a health_writer.go ByWorkload key
// ("<namespace>/<workload>", possibly "_unknown/<workload>") on its first
// "/". Kubernetes namespace/workload names cannot themselves contain "/", so
// this is unambiguous; the ", ok" fallback below is defensive only — every
// key health_writer.go's accumulate() produces already contains exactly one
// "/".
func splitWorkloadKey(key string) (namespace, workload string) {
	ns, wl, ok := strings.Cut(key, "/")
	if !ok {
		return "_unknown", key
	}
	return ns, wl
}

// matchesNamespaceWorkload applies the optional namespace/workload filters
// (empty means unset, always matches) identically to either half — both
// halves resolve "the endpoint that would receive the generated policy" via
// the same policyTargetEndpoint helper upstream (pkg/hubble/aggregator.go),
// so namespace/workload mean the same thing on both sides (18-PATTERNS.md
// Pattern 3).
func matchesNamespaceWorkload(filterNS, filterWL, itemNS, itemWL string) bool {
	if filterNS != "" && filterNS != itemNS {
		return false
	}
	if filterWL != "" && filterWL != itemWL {
		return false
	}
	return true
}

// matchesDropClass applies the optional dropclass filter (empty means
// unset, always matches) against an already-computed class label.
func matchesDropClass(class dropclass.DropClass, filter string) bool {
	return filter == "" || class.String() == filter
}

// classifyDropReasonName recovers the DropClass for a FlowSample's
// DropReason string (e.g. "POLICY_DENIED") by reversing it through
// flowpb.DropReason_value — the exact inverse of how evidence_writer.go
// populated it in the first place (f.GetDropReasonDesc().String()) — then
// running it through the single canonical classifier (D-14). An empty or
// unrecognized name classifies as Unknown rather than silently matching
// DROP_REASON_UNKNOWN(0)'s own "transient" bucket, since "no reason
// recorded" and "explicitly DROP_REASON_UNKNOWN" are different situations.
func classifyDropReasonName(name string) dropclass.DropClass {
	if name == "" {
		return dropclass.DropClassUnknown
	}
	if val, ok := flowpb.DropReason_value[name]; ok {
		return dropclass.Classify(flowpb.DropReason(val))
	}
	return dropclass.DropClassUnknown
}

// buildCombinedItems merges samples and aggregate rows into one
// deterministically sorted slice: primary order (namespace, workload) — the
// identity both halves share (18-PATTERNS.md Pattern 3) — then samples
// before aggregate rows within a matching pair, with sort.SliceStable
// preserving each half's own already-deterministic internal order
// (directory-walk order for samples, namespace/workload/reason order for
// aggregates) as the final tiebreaker. index is then assigned per
// (namespace, workload) group: the D-05 "stable index" that, together with
// (namespace, workload), forms the paginateBoundaryKey both response
// sections share one pagination window over. This must be a per-group
// position, never a global absolute offset — mcp_query_pagination.go's
// paginateBoundaryKey doc explains why an absolute index breaks gracelessly
// under D-06's best-effort-rescan model.
func buildCombinedItems(samples []DroppedFlowSample, aggregates []droppedFlowAggregateRow) []droppedFlowItem {
	items := make([]droppedFlowItem, 0, len(samples)+len(aggregates))
	for _, s := range samples {
		items = append(items, droppedFlowItem{namespace: s.Namespace, workload: s.Workload, isSample: true, sample: s})
	}
	for _, a := range aggregates {
		items = append(items, droppedFlowItem{namespace: a.Namespace, workload: a.Workload, isSample: false, aggregate: a})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].namespace != items[j].namespace {
			return items[i].namespace < items[j].namespace
		}
		if items[i].workload != items[j].workload {
			return items[i].workload < items[j].workload
		}
		return items[i].isSample && !items[j].isSample
	})

	idx := 0
	prevNS, prevWL := "", ""
	first := true
	for i := range items {
		if first || items[i].namespace != prevNS || items[i].workload != prevWL {
			idx = 0
			prevNS, prevWL = items[i].namespace, items[i].workload
			first = false
		}
		items[i].index = idx
		idx++
	}
	return items
}
