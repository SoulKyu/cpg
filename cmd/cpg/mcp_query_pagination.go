package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// Default/max page-size bounds (D-07), expressed as named consts so every
// paginated query tool references the same numbers instead of hand-copying
// them. Both pairs are bounded well under the ~25k-token MCP output cap:
//
//   - Flow-scale (list_dropped_flows, 18-05): compact per-flow rows, so a
//     larger default/max stays cheap.
//   - Evidence-scale (get_evidence, this plan): each RuleEvidence row can
//     carry up to MergeCaps.MaxSamples=10 embedded FlowSample entries
//     (pkg/session/pipeline_config.go), so a smaller default/max keeps a
//     single page well under the cap.
const (
	// defaultFlowLimit/maxFlowLimit are consumed by list_dropped_flows
	// (18-05, mcp_query_flows.go) — defined here, per D-07, as the single
	// source of truth every flow-scale paginated tool references instead of
	// hand-copying its own numbers.
	defaultFlowLimit = 50
	maxFlowLimit     = 200

	defaultEvidenceLimit = 20
	maxEvidenceLimit     = 100
)

// mustQuerySchema builds the struct-tag-inferred *jsonschema.Schema for T via
// jsonschema.For[T], then patches an Enum constraint onto each named field in
// enumFields. This is the ONLY mechanism go-sdk v1.6.1 + jsonschema-go v0.4.3
// support for adding an enum constraint to a tool's InputSchema (D-14,
// 18-RESEARCH.md Pattern 1): the `jsonschema:"..."` struct tag is parsed as
// pure free-text description, and any tag shaped like `WORD=...` is actively
// rejected at schema-build time — there is no `enum=` tag syntax at all.
//
// Panics on a schema-build error or an unknown enumFields key — both are
// internal programming errors caught at registration time (e.g. a mis-typed
// field name), the same fail-fast convention mcp.AddTool itself uses for
// schema construction errors.
func mustQuerySchema[T any](enumFields map[string][]any) *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("mustQuerySchema: building schema for %T: %v", *new(T), err))
	}
	for field, values := range enumFields {
		prop, ok := schema.Properties[field]
		if !ok {
			panic(fmt.Sprintf("mustQuerySchema: %T has no property %q to constrain", *new(T), field))
		}
		prop.Enum = values
	}
	return schema
}

// paginateBoundaryKey identifies an item's position in a deterministic
// (namespace, workload, index) sort order — the resumption point encoded in
// the D-05 opaque cursor. Index is a stable in-file/in-slice position, NEVER
// an absolute offset into the current call's result set: an absolute
// integer position silently skips or repeats rows when the underlying file
// set shifts between paginated calls during an active capture (Anti-Pattern,
// 18-RESEARCH.md). get_evidence holds Namespace/Workload constant (one
// evidence file per call) and varies only Index; list_dropped_flows (18-05)
// varies all three across many evidence/health files.
type paginateBoundaryKey struct {
	Namespace string `json:"ns"`
	Workload  string `json:"wl"`
	Index     int    `json:"idx"`
}

// compareBoundaryKey orders two boundary keys by (Namespace, Workload,
// Index), returning -1/0/1 like strings.Compare/cmp.Compare. This is the
// order paginate assumes callers already sorted their input slice by.
func compareBoundaryKey(a, b paginateBoundaryKey) int {
	if a.Namespace != b.Namespace {
		if a.Namespace < b.Namespace {
			return -1
		}
		return 1
	}
	if a.Workload != b.Workload {
		if a.Workload < b.Workload {
			return -1
		}
		return 1
	}
	switch {
	case a.Index < b.Index:
		return -1
	case a.Index > b.Index:
		return 1
	default:
		return 0
	}
}

// encodeCursor returns an opaque base64 token encoding key. Callers never
// construct or parse the returned string directly — it round-trips only
// through decodeCursor, on this process or a future call to it.
func encodeCursor(key paginateBoundaryKey) string {
	data, err := json.Marshal(key)
	if err != nil {
		// paginateBoundaryKey is a plain {string,string,int} struct — this
		// can only fail on an internal invariant violation (e.g. a future
		// field of a non-JSON-marshalable type), never on caller input,
		// which never reaches this function directly.
		panic(fmt.Sprintf("encodeCursor: marshaling boundary key: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

// decodeCursor parses an opaque cursor token produced by encodeCursor back
// into a boundary key. Fails closed — returns an error, never panics — on
// invalid base64 or a malformed/truncated JSON payload (T-18-04-02): an
// LLM-adversarial or corrupted cursor value must never crash the handler
// goroutine (an unrecovered panic in any goroutine kills the whole cpg mcp
// process). The error text is the shared D-05 actionable message every
// paginated query tool surfaces verbatim as its isError text.
func decodeCursor(token string) (paginateBoundaryKey, error) {
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return paginateBoundaryKey{}, fmt.Errorf("invalid cursor; retry without cursor to restart from the first page")
	}
	var key paginateBoundaryKey
	if err := json.Unmarshal(data, &key); err != nil {
		return paginateBoundaryKey{}, fmt.Errorf("invalid cursor; retry without cursor to restart from the first page")
	}
	return key, nil
}

// clampLimit normalizes a caller-supplied page-size limit: a non-positive
// value (zero, or omitted — which unmarshals to zero) falls back to
// defaultLimit; a value above maxLimit is clamped down to maxLimit; anything
// else passes through unchanged.
func clampLimit(limit, defaultLimit, maxLimit int) int {
	switch {
	case limit <= 0:
		return defaultLimit
	case limit > maxLimit:
		return maxLimit
	default:
		return limit
	}
}

// paginate slices items — which the caller must already have sorted in the
// same deterministic order keyOf's boundary keys imply — into one page.
// after is the previous call's decoded next_cursor (nil to start from the
// first page); limit/defaultLimit/maxLimit are clamped via clampLimit before
// slicing.
//
// Resumption is boundary-key based, not offset based (D-06): paginate scans
// for the first item whose key sorts strictly after `after`, rather than
// jumping to a numeric index. This degrades gracefully (an occasional
// skip/dup at the exact boundary) rather than catastrophically (whole pages
// silently shifted) when the underlying item set changes size between calls
// during an active capture — the explicit trade-off the D-05/D-06 opaque
// boundary-key cursor design makes.
//
// Returns the page slice, the opaque cursor for the next page (empty string
// on the last page), whether more items remain, and the total count of
// items across every page (D-04: counts the filtered view, not a raw total).
func paginate[T any](items []T, keyOf func(T) paginateBoundaryKey, after *paginateBoundaryKey, limit, defaultLimit, maxLimit int) (page []T, nextCursor string, hasMore bool, totalCount int) {
	limit = clampLimit(limit, defaultLimit, maxLimit)

	start := 0
	if after != nil {
		start = len(items)
		for i, it := range items {
			if compareBoundaryKey(keyOf(it), *after) > 0 {
				start = i
				break
			}
		}
	}

	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	if start > len(items) {
		start = len(items)
	}

	page = items[start:end]
	hasMore = end < len(items)
	if hasMore {
		nextCursor = encodeCursor(keyOf(items[end-1]))
	}
	totalCount = len(items)
	return page, nextCursor, hasMore, totalCount
}
