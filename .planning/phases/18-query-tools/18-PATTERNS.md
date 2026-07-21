# Phase 18: Query Tools - Pattern Map

**Mapped:** 2026-07-21
**Files analyzed:** 16 (8 new, 6 modified, 2 removed/promoted-out)
**Analogs found:** 15 / 16 concrete file-level analogs; 1 cross-cutting mechanism (enum-schema construction) has no codebase analog — RESEARCH.md's vendor-verified pattern is the source instead

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `cmd/cpg/mcp_query_tools.go` — `list_policies`/`get_policy`/`get_evidence`/`get_cluster_health` handlers | controller (MCP tool registration) | request-response | `cmd/cpg/mcp_tools.go` | exact |
| `cmd/cpg/mcp_query_tools.go` — `list_dropped_flows` handler (composed view) | controller + domain read-model | CRUD-read + transform (fan-in 2 readers, paginate) | `cmd/cpg/mcp_tools.go` (registration shell only) | role-match — genuinely new composition logic, no existing analog for the assembly itself |
| `cmd/cpg/mcp.go` (add `registerQueryTools` call) | config / composition root | request-response | itself (existing `registerSessionTools` wiring) | exact |
| `pkg/explain/filter.go` (new, promoted) | utility (predicate) | transform | `cmd/cpg/explain_filter.go` | exact (this *is* the promotion source) |
| `pkg/explain/render.go` (new, promoted) | utility (renderer) | transform | `cmd/cpg/explain_render.go` | exact (this *is* the promotion source) |
| `cmd/cpg/explain_filter.go` (deleted — logic moves out) | n/a | n/a | — | n/a |
| `cmd/cpg/explain_render.go` (deleted — logic moves out) | n/a | n/a | — | n/a |
| `cmd/cpg/explain_target.go` | unchanged | n/a | — | n/a — D-09 locks this file untouched |
| `cmd/cpg/explain.go` (thinned — calls into `pkg/explain`) | controller (CLI) | request-response | itself (pre-thinning) + `pkg/flowsource` promotion precedent | exact |
| `pkg/output/writer.go` (+ exported `ReadPolicyFile`) | service (reader) | file-I/O | itself — `readExistingPolicy` (same file, line 129) | exact (self-export) |
| `pkg/hubble/health_writer.go` (export 3 types) | model | file-I/O | itself + `pkg/evidence/schema.go` (exported-struct convention) | exact (self-export) |
| `pkg/hubble/health_reader.go` (new) | service (reader) | file-I/O | `pkg/evidence/reader.go` | exact |
| `cmd/cpg/mcp_query_tools_test.go` (new) | test | request-response | `cmd/cpg/mcp_session_test.go` + `mcp_harness_test.go` | exact |
| `pkg/hubble/health_reader_test.go` (new) | test | file-I/O | `pkg/evidence/reader_test.go` | exact |
| `pkg/hubble/pipeline_test.go` (+ `TestRunPipeline_FinalizesHealthOnStreamError`) | test | event-driven (errgroup pipeline) | itself — `TestRunPipeline_SurfacesStreamError` (line 321) | exact |
| `pkg/output/writer_test.go` (+ `ReadPolicyFile` test) | test | file-I/O | itself — `TestWriter_NewFileCreation` (line 37) | exact |
| `pkg/explain/filter_test.go` (new, adapted) | test | transform | `cmd/cpg/explain_filter_test.go` | exact (mechanical move, unchanged assertions) |
| `pkg/explain/render_test.go` (new, adapted) | test | transform | `cmd/cpg/explain_test.go` (`TestRenderJSON`, `TestExplainJSONOutput`) | exact (mechanical move, unchanged assertions) |

---

## Pattern Assignments

### `cmd/cpg/mcp_query_tools.go` — the 4 single-object/list tools (`list_policies`, `get_policy`, `get_evidence`, `get_cluster_health`)

**Analog:** `cmd/cpg/mcp_tools.go` (full file, 166 lines — this is the *only* MCP tool registration file that exists; Phase 18 extends its exact conventions, does not invent new ones)

**Imports pattern** (`cmd/cpg/mcp_tools.go` lines 1-11):
```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/SoulKyu/cpg/pkg/session"
)
```
Query tools additionally import `pkg/evidence`, `pkg/explain`, `pkg/hubble`, `pkg/output`, `pkg/dropclass`, and — for the enum-schema pattern — `github.com/google/jsonschema-go/jsonschema` (this last one is the phase's one new *direct* import; see "No Analog Found" below).

**Args-struct + jsonschema-tag convention** (`cmd/cpg/mcp_tools.go` lines 21-40):
```go
type startSessionArgs struct {
	Namespace         []string `json:"namespace,omitempty" jsonschema:"namespace filter, repeatable"`
	...
}

type sessionRef struct {
	SessionID string `json:"session_id" jsonschema:"the opaque session_id returned by start_session"`
}
```
Rule confirmed by direct comment: a field WITHOUT `omitempty`/`omitzero` becomes schema-`required`; every optional filter carries `omitempty`. Apply identically to `getPolicyArgs{Namespace, Workload string}` (both required, no omitempty), `getEvidenceArgs` (Namespace/Workload required, the 7 filters + limit/cursor omitempty), etc.

**Single-object tool registration template** (`cmd/cpg/mcp_tools.go` lines 145-153 — `get_status`, the closest existing analog to `get_policy`/`get_cluster_health`):
```go
mcp.AddTool(server, &mcp.Tool{
	Name: "get_status",
	Description: "Return coarse session state (capturing/stopped), elapsed time, and " +
		"on-disk artifact file counts. Works for a stopped-but-retained session too.",
	Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
}, func(_ context.Context, _ *mcp.CallToolRequest, args sessionRef) (*mcp.CallToolResult, session.StatusResult, error) {
	result, err := mgr.Status(args.SessionID)
	return nil, result, err
})
```
**The error-handling rule, verbatim from `mcp_tools.go` line 139-142's comment** — copy this exactly, do not deviate:
```go
// Error path: return the Go error as-is — the go-sdk auto-converts a
// non-nil error into a tool-error result, with the error text as
// content (Pattern 0). Never hand-construct that result here.
return nil, result, err
```
D-16 locks this same discipline for all 5 query tools.

**Doc-comment convention for the registration function itself** (`cmd/cpg/mcp_tools.go` lines 82-93 — the exact style `registerQueryTools`'s doc comment should mirror):
```go
// registerSessionTools registers the 3 session-lifecycle MCP tools —
// start_session, get_status, stop_session — on server, wired to mgr. This is
// the composition-root entry point cmd/cpg/mcp.go's runMCPServer calls right
// after constructing the Manager; Phase 18 adds read-side query tools
// alongside these in the same composition-root style.
...
func registerSessionTools(server *mcp.Server, mgr *session.Manager) {
```
Note this comment *already predicts* `registerQueryTools` — it is the designed extension point.

**Session resolution** (shared by all 5 query tools, D-08): `mgr.Status(args.SessionID)` — `pkg/session/manager.go` lines 333-386. Exact returned shape:
```go
// pkg/session/manager.go:333
func (m *Manager) Status(id string) (StatusResult, error) {
	...
	if s == nil || s.ID != id {
		return StatusResult{}, fmt.Errorf("session %q not found or expired", id) // SESS-06
	}
	...
	return StatusResult{
		SessionID: sid, State: state.String(), Elapsed: ..., PolicyFileCount: ...,
		EvidenceFileCount: ..., TmpDir: tmpDir, Error: statusErr,
	}, nil
}
```
`StatusResult.TmpDir` + `.State` (`"capturing"` | `"stopped"`, `pkg/session/session.go` lines 51-60) + `.Error` are exactly what every query-tool handler needs: derive `evidence`/`policies` subdirs from `TmpDir`, branch on `State` for D-02/D-13's mid-capture semantics, surface `.Error` for D-13's third branch. **Zero new Manager API — call `Status`, nothing else.**

**tmpdir → policies/evidence path + outputHash derivation** (D-08, exact formula already used twice in the codebase — reuse it a third time, do not invent a new one):
```go
// pkg/session/pipeline_config.go:69-71 (buildPipelineConfig)
outputDir := filepath.Join(tmpDir, "policies")
evidenceDir := filepath.Join(tmpDir, "evidence")
outputHash := evidence.HashOutputDir(outputDir)

// pkg/session/manager.go:407-408 (Stop, recomputes identically for a stopped session)
outputHash := evidence.HashOutputDir(filepath.Join(tmpDir, "policies"))
healthPath := filepath.Join(tmpDir, "evidence", outputHash, "cluster-health.json")
```

**Result-struct JSON convention** (snake_case, `pkg/session/session.go` lines 152-174 — `StartResult`/`StatusResult`): every query-tool result struct follows this exact tagging style, e.g. `TmpDir string \`json:"tmp_dir"\``, optional fields carry `omitempty` plus a `jsonschema:"..."` doc string (see `StatusResult.Error` line 173 for the dual-tag style: `json:"error,omitempty" jsonschema:"..."`).

**Annotations — the one subtlety Phase 18 introduces that Phase 17 never needed** (RESEARCH.md Pattern 1, VERIFIED against `go-sdk@v1.6.1/mcp/protocol.go:1372-1377`): `OpenWorldHint` is `*bool`, not `bool` (unlike `ReadOnlyHint`/`IdempotentHint` which Phase 17 sets as plain `bool` at `mcp_tools.go` lines 105, 149, 161). D-16 requires `OpenWorldHint: false` explicitly, which needs a pointer:
```go
Annotations: &mcp.ToolAnnotations{
	ReadOnlyHint:   true,
	IdempotentHint: true,
	OpenWorldHint:  jsonschema.Ptr(false), // *bool — omission defaults to spec's implicit "true"
},
```

---

### `cmd/cpg/mcp_query_tools.go` — `list_dropped_flows` (composed view, no existing analog for the assembly logic itself)

RESEARCH.md's Architectural Responsibility Map is explicit: *"Genuinely new integration logic — no existing package does this composition; must NOT live in `pkg/hubble` or `pkg/evidence` (those stay single-purpose readers)."* The registration shell (tool name/description/annotations/error-return) still follows the `mcp_tools.go` template above — only the handler body's assembly logic is new. Use these three sources together:

**1. Samples-half flattening** (RESEARCH.md's ready-to-adapt struct, cross-checked against `pkg/evidence/schema.go` lines 51-95 — `RuleEvidence`/`FlowSample` carry no namespace/workload/direction of their own; that context lives on the parent):
```go
// A raw evidence.FlowSample carries no namespace/workload/direction of its
// own — that context lives on the PARENT RuleEvidence/PolicyEvidence.
type DroppedFlowSample struct {
	Namespace  string                `json:"namespace"`
	Workload   string                `json:"workload"`
	Direction  string                `json:"direction"` // from the parent RuleEvidence.Direction
	Peer       evidence.PeerRef      `json:"peer"`
	Port       string                `json:"port"`
	Protocol   string                `json:"protocol"`
	Time       time.Time             `json:"time"`
	Src        evidence.FlowEndpoint `json:"src"`
	Dst        evidence.FlowEndpoint `json:"dst"`
	Verdict    string                `json:"verdict"`
	DropReason string                `json:"drop_reason,omitempty"`
}
// Built by: for each evidence file (PolicyEvidence), for each RuleEvidence,
// for each FlowSample in RuleEvidence.Samples — emit one DroppedFlowSample.
```
Walk `<tmpdir>/evidence/<hash>/**/*.json` and read each via `pkg/evidence.Reader.Read(namespace, workload)` (`pkg/evidence/reader.go` lines 26-44) — namespace/workload for the walk come from the directory structure itself (`ResolvePolicyPath`, `pkg/evidence/paths.go` line 41: `<evidenceDir>/<outputHash>/<namespace>/<workload>.json`).

**2. Aggregates-half source** — `pkg/hubble.ReadClusterHealth` (new, see health_reader.go section below) → `ClusterHealthReport.Drops[].ByWorkload` (a `map[string]uint64` keyed `"<namespace>/<workload>"` or `"_unknown/<workload>"`, `pkg/hubble/health_writer.go` lines 78-83). D-02: while `State == "capturing"`, skip this half entirely and emit the `available_after_stop` marker instead — never call `ReadClusterHealth` mid-capture (the file may not exist yet for reasons unrelated to a crash — see Pitfall #1 in the Shared Patterns section below).

**3. Namespace/workload consistency across both halves** (RESEARCH.md Pattern 3, `pkg/hubble/aggregator.go` lines 240-277 — verified in-source comment):
```go
// pkg/hubble/aggregator.go:242-243
// This is the single source of truth for the INGRESS/EGRESS direction switch —
// both buildDropEvent and keyFromFlow delegate to this helper (M3 dedup).
func policyTargetEndpoint(f *flowpb.Flow) *flowpb.Endpoint { ... }
```
Both halves resolve "the endpoint that would receive the generated policy" — a `namespace`/`workload` filter means the same thing applied to either half, no extra translation needed.

**4. Pagination wraps the assembled list, it is not baked into any reader** (RESEARCH.md Pattern 2): build the full filtered `samples[]`+`aggregates[]` set first, THEN slice by `limit`/`cursor` before constructing `structuredContent` — exactly mirroring how `get_evidence` slices the promoted `pkg/explain` renderer's full matched-set output (see below). D-05's cursor is an opaque base64 boundary-key token (not an absolute offset — RESEARCH.md's Anti-Pattern section: an absolute index cursor silently skips/repeats rows when the file set shifts between paginated calls during an active capture).

**Known, accepted gap to carry into the tool description (D-15's mandatory caveat slot):** `direction` has zero data to filter on for the aggregates half — `DropEvent` (`pkg/hubble/aggregator.go` lines 21-27) and `HealthDropJSON`/`healthDropJSON` (`pkg/hubble/health_writer.go` lines 253-265) carry no direction field at all. Apply `direction` to `samples[]` only; return `aggregates[]` rows unfiltered by `direction` and say so in the description — do not fabricate a direction, do not hide the rows.

---

### `cmd/cpg/mcp.go` — wiring `registerQueryTools`

**Analog:** itself — the exact call site to extend (`cmd/cpg/mcp.go` lines 79-96, `runMCPServer`):
```go
func runMCPServer(ctx context.Context, transport mcp.Transport) error {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "cpg", Version: version},
		&mcp.ServerOptions{Logger: bridgedSlogLogger()},
	)

	// Phase 17 registers the 3 session-lifecycle tools ...; Phase 18 adds
	// read-side query tools in the same composition-root style.
	mgr := session.NewManager(ctx, logger, mcpModeStdout(), version)
	registerSessionTools(server, mgr)
	// <-- registerQueryTools(server, mgr) goes here, same signature shape

	err := server.Run(ctx, transport)
	mgr.Shutdown()
	return err
}
```
The doc comment at line 85-93 already anticipates this exact insertion point — no other change to `mcp.go` is needed (readonly composition-root discipline, SEC-01, continues to hold: query handlers only ever reach `mgr.Status`, never a K8s write-adjacent helper).

---

### `pkg/explain/filter.go` + `pkg/explain/render.go` (D-09 mechanical promotion)

**Analog / promotion source:** `cmd/cpg/explain_filter.go` (96 lines, full file already read) and `cmd/cpg/explain_render.go` (175 lines, full file already read) — these files' *entire content* is the new package's content, with 4 renames:

| Old (unexported, `cmd/cpg`) | New (exported, `pkg/explain`) | Source location |
|---|---|---|
| `type explainFilter struct` | `type Filter struct` | `explain_filter.go:11` |
| `func (f explainFilter) match(r evidence.RuleEvidence) bool` | `func (f Filter) Match(r evidence.RuleEvidence) bool` | `explain_filter.go:31` |
| `type explainOutput struct` | `type Output struct` | `explain_render.go:22-26` |
| `func renderJSON/renderText/renderYAML(...)` | `func RenderJSON/RenderText/RenderYAML(...)` | `explain_render.go:28,133,140` |

`parsePeerLabel` (`explain_filter.go:87-96`) moves and is **exported as `ParsePeerLabel`** — two cross-package callers need it: `cmd/cpg/explain.go:133`'s `buildFilter` (which stays in `cmd/cpg`) and 18-04's `get_evidence` handler (checker-verified correction; the original "stays unexported" claim was wrong). The render-internal helpers `writeRule`/`peerSummary`/`fmtEndpoint`/`colorizer` (`explain_render.go:57-175`) move along with their callers and stay unexported *within* `pkg/explain` (nothing outside the package calls them directly today).

**Exact promotion-commit precedent** — `pkg/flowsource` was promoted from `pkg/hubble/pipeline.go` the same way (commit `0aba62d`, "refactor: promote FlowSource interface to pkg/flowsource"):
```diff
- flowpb "github.com/cilium/cilium/api/v1/flow"
  ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
  ...
+ "github.com/SoulKyu/cpg/pkg/flowsource"
  "github.com/SoulKyu/cpg/pkg/output"

- // FlowSource abstracts the streaming source for testability.
- type FlowSource interface {
-	StreamDroppedFlows(ctx context.Context, namespaces []string, allNS bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error)
- }
-
  // PipelineConfig holds all configuration for the streaming pipeline.
  type PipelineConfig struct {
```
Net diff was `+2 -7` in the caller, plus a new ~16-line package (`pkg/flowsource/source.go`) with a doc comment (`// Package flowsource decouples ...`) and the exact same exported interface, just moved. Apply the identical shape: `pkg/explain/filter.go`/`render.go` get a package doc comment, the caller (`cmd/cpg/explain.go`) drops the moved declarations and adds one import line.

**Package doc comment convention to add** (mirrors `pkg/flowsource/source.go`'s style and `pkg/evidence/schema.go:1-3`'s style):
```go
// Package explain filters and renders per-rule evidence for a generated
// policy — the shared core behind `cpg explain` and the MCP get_evidence
// tool. Byte-identical output shape for both callers (QRY-03).
package explain
```

---

### `cmd/cpg/explain.go` (thinned)

**Analog:** itself, pre-thinning (full 162-line file already read). What stays vs. moves:

| Stays in `cmd/cpg/explain.go` | Moves to `pkg/explain` |
|---|---|
| `newExplainCmd()` cobra flag registration (lines 17-50) | — |
| `runExplain` — target resolution, `evidence.Reader` construction/`errors.Is(err, fs.ErrNotExist)` handling (lines 52-77) | — |
| `buildFilter(cmd) (explainFilter, error)` (lines 116-162) — cobra-flag reading is CLI-only per D-09 | its **return type** changes from `explainFilter` to `explain.Filter` |
| the `filter.match(r)`/`renderJSON(out, pe, matched)` **call sites** (lines 85, 100-110) | the **functions being called** move; call sites become `filter.Match(r)` / `explain.RenderJSON(out, pe, matched)` |

**Not-found error pattern to preserve verbatim** (`explain.go` lines 71-76 — also directly reusable by `get_evidence`'s handler per RESEARCH.md Pitfall #4):
```go
pe, err := reader.Read(target.Namespace, target.Workload)
if err != nil {
	if errors.Is(err, fs.ErrNotExist) {
		path := evidence.ResolvePolicyPath(evDir, hash, target.Namespace, target.Workload)
		return fmt.Errorf("no evidence found for %s/%s at %s (run `cpg generate` or `cpg replay` with the same --output-dir first)", target.Namespace, target.Workload, path)
	}
	return err
}
```
`get_evidence`'s handler should use the equivalent `pkg/evidence.IsNotExist(err)` helper (`pkg/evidence/reader.go` lines 46-49) and adapt the message to D-16's "policy-not-found suggests calling `list_policies`" wording instead of the CLI's "`run cpg generate`" wording.

---

### `pkg/output/writer.go` — new exported `ReadPolicyFile`

**Analog:** itself — `readExistingPolicy` (same file, lines 127-144), already doing exactly this unmarshal:
```go
// pkg/output/writer.go:127-144
func readExistingPolicy(path string) (*ciliumv2.CiliumNetworkPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cnp ciliumv2.CiliumNetworkPolicy
	if err := yaml.Unmarshal(data, &cnp); err != nil {
		return nil, fmt.Errorf("unmarshaling existing policy: %w", err)
	}
	return &cnp, nil
}
```
**Action:** add an exported sibling (e.g. `ReadPolicyFile(path string) (*ciliumv2.CiliumNetworkPolicy, error)`) that calls the same unmarshal logic — do not duplicate the `yaml.Unmarshal` call in the MCP handler (RESEARCH.md's Don't-Hand-Roll table, row 1: "forking it means two places can silently disagree after a future CNP schema change").

**Flag for planner attention (not a re-litigation of D-11, just a concrete inconsistency to resolve):** `readExistingPolicy`'s existing not-exist contract is silent `(nil, nil)` — appropriate for `Write()`'s internal "is there something to merge?" check. `list_policies`/`get_policy` are query tools that need to distinguish "no such policy" from "read error" the same way `pkg/evidence.Reader.Read` and the new `pkg/hubble.ReadClusterHealth` both do (wrap `fs.ErrNotExist`, let the caller `errors.Is`/`IsNotExist`). Decide whether `ReadPolicyFile` keeps the silent-nil contract (matching its sibling exactly) or adopts the wrapped-error contract (matching the other two readers this phase adds/uses) — Claude's Discretion territory, but pick one convention and apply it to all 3 readers consistently.

**Existing `ReadExisting`** (`pkg/output/writer.go` lines 112-125) already gives raw YAML bytes for `get_policy`'s "full YAML string" field (D-17) — reuse directly, no new code needed for that part.

---

### `pkg/hubble/health_writer.go` — export 3 types (D-12)

**Analog:** itself — the exact unexported definitions to rename in place (`pkg/hubble/health_writer.go` lines 239-265):
```go
// JSON output structs — unexported, used only for marshaling.
type clusterHealthReport struct {          // → ClusterHealthReport
	SchemaVersion     int              `json:"schema_version"`
	ClassifierVersion string           `json:"classifier_version"`
	Session           healthSession    `json:"session"`     // healthSession → HealthSession
	Drops             []healthDropJSON `json:"drops"`        // healthDropJSON → HealthDropJSON
}

type healthSession struct {                // → HealthSession
	Started        time.Time `json:"started"`
	Ended          time.Time `json:"ended"`
	FlowsSeen      uint64    `json:"flows_seen"`
	InfraDropTotal uint64    `json:"infra_drops_total"` // NOTE plural JSON tag, singular Go name
}

type healthDropJSON struct {               // → HealthDropJSON
	Reason      string            `json:"reason"`
	Class       string            `json:"class"`
	Count       uint64            `json:"count"`
	Remediation string            `json:"remediation,omitempty"`
	ByNode      map[string]uint64 `json:"by_node"`
	ByWorkload  map[string]uint64 `json:"by_workload"`
}
```
The one call site to update in the same file is `finalize()`'s report construction (`health_writer.go` line 121: `report := clusterHealthReport{...}` → `ClusterHealthReport{...}`). No other call site exists in the package — `hw.finalize` is the sole producer.

**Passthrough discipline (D-12):** these types are exported as-is, zero new/derived fields — the query tool must not add value transformation on top (`pkg/dropclass/classifier.go` line 257's `DropClass.String()` is the only place a Cilium-facing enum ever gets converted to a label string, and that conversion already happened before this JSON was ever written — the reader just deserializes it back).

---

### `pkg/hubble/health_reader.go` (new) — `ReadClusterHealth`

**Analog:** `pkg/evidence/reader.go` (49 lines, full file read) — the schema-version-gate idiom to mirror exactly:
```go
// pkg/evidence/reader.go:26-44
func (r *Reader) Read(namespace, workload string) (PolicyEvidence, error) {
	path := ResolvePolicyPath(r.evidenceDir, r.outputHash, namespace, workload)
	data, err := os.ReadFile(path)
	if err != nil {
		return PolicyEvidence{}, fmt.Errorf("reading evidence %s: %w", path, err)
	}
	var pe PolicyEvidence
	if err := json.Unmarshal(data, &pe); err != nil {
		return PolicyEvidence{}, fmt.Errorf("parsing evidence %s: %w", path, err)
	}
	if pe.SchemaVersion != SchemaVersion {
		return PolicyEvidence{}, fmt.Errorf(
			"unsupported schema_version %d in %s (this cpg understands %d). ...",
			pe.SchemaVersion, path, SchemaVersion)
	}
	return pe, nil
}

// IsNotExist reports whether err is the not-found variant returned by Read.
func IsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
```
`os.ReadFile`'s error already wraps `fs.ErrNotExist` when the file is absent (via `%w` in `fmt.Errorf`), which `errors.Is(err, fs.ErrNotExist)` — or the package's own `IsNotExist` helper — detects downstream. This is exactly the mechanism D-13's 3-way branch needs to distinguish "file absent" from "malformed/wrong-version file present."

**Ready-to-adapt implementation** (RESEARCH.md Code Examples, cross-verified against `health_writer.go`'s exact write format — `SchemaVersion: 1` literal at line 122):
```go
// New file: pkg/hubble/health_reader.go
func ReadClusterHealth(path string) (*ClusterHealthReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading cluster health %s: %w", path, err)
	}
	var report ClusterHealthReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parsing cluster health %s: %w", path, err)
	}
	if report.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported cluster-health schema_version %d in %s (this cpg understands 1)",
			report.SchemaVersion, path)
	}
	return &report, nil
}
```

**D-13's 3-way branch this reader feeds** (RESEARCH.md Pitfall #1 correction — `pkg/hubble/pipeline.go` line 360, confirmed `hw.finalize(stats)` runs UNCONDITIONALLY after `err = g.Wait()`, no `if err == nil` gate):
1. `state == "stopped"` + file present → passthrough (`ReadClusterHealth` succeeds).
2. `state == "stopped"` + file absent + `StatusResult.Error == ""` → **not an error** — the common "zero infra/transient drops" case. Non-error result, analogous to D-02's `available_after_stop` marker.
3. `state == "stopped"` + file absent + `StatusResult.Error != ""` → `isError` citing `StatusResult.Error`.

Branch 1 vs. 2/3 is `errors.Is(err, fs.ErrNotExist)` (or `os.IsNotExist`) on `ReadClusterHealth`'s returned error; 2 vs. 3 is `StatusResult.Error == ""`.

---

### Test files

#### `cmd/cpg/mcp_query_tools_test.go`

**Analogs:** `cmd/cpg/mcp_harness_test.go` (124 lines, full file read) + `cmd/cpg/mcp_session_test.go` (262 lines, full file read) — reuse their helpers directly, do not redefine:

```go
// mcp_harness_test.go:28-35 — reuse directly, do not redefine
func startInMemoryMCPSession(ctx context.Context) (client *mcp.InMemoryTransport, drain func()) {
	serverT, clientT := mcp.NewInMemoryTransports()
	errCh := make(chan error, 1)
	go func() { errCh <- runMCPServer(ctx, serverT) }()
	return clientT, func() { <-errCh }
}

// mcp_session_test.go:24-52 — reuse directly, do not redefine
func requiredFields(t *testing.T, inputSchema any) []string { ... }
func decodeStructured(t *testing.T, structuredContent any, out any) { ... }
```

**Tool-listing test template** (`mcp_session_test.go` lines 54-93, `TestMCPSessionToolsListed`) — extend the pattern, not the function, for `TestMCPQueryToolsListed`:
```go
toolsResult, err := cs.ListTools(ctx, nil)
require.NoError(t, err)
require.Len(t, toolsResult.Tools, 8, "3 session tools + 5 query tools")
byName := make(map[string]*mcp.Tool, len(toolsResult.Tools))
for _, tool := range toolsResult.Tools { byName[tool.Name] = tool }
assert.Contains(t, byName, "list_dropped_flows")
// ... assert required fields per tool, e.g.:
assert.ElementsMatch(t, []string{"namespace", "workload"}, requiredFields(t, byName["get_policy"].InputSchema))
```

**Actionable-error-text test template** (`mcp_session_test.go` lines 95-132, `TestMCPSessionUnknownIDReturnsIsError`) — the exact shape for D-16's 3 required error-text assertions (unknown session / policy-not-found / invalid cursor):
```go
result, err := cs.CallTool(ctx, &mcp.CallToolParams{
	Name: toolName, Arguments: map[string]any{"session_id": "sess_bogus"},
})
require.NoError(t, err, "a tool error must not surface as a transport/protocol error")
require.True(t, result.IsError)
tc, ok := result.Content[0].(*mcp.TextContent)
require.True(t, ok)
assert.Contains(t, tc.Text, "not found or expired") // SESS-06 phrase, reused verbatim
```

**D-07 bypass address** (`mcp_session_test.go` line 179: `"server": "127.0.0.1:1"`) — reuse for any query-tool test scenario needing a real (but empty) session tmpdir without a live cluster: `start_session` with the bypass address, then read `TmpDir` off `get_status`'s `structuredContent`, then seed fixture files directly under that `TmpDir` before calling the query tool under test.

#### `pkg/hubble/health_reader_test.go`

**Analog:** `pkg/evidence/reader_test.go` (76 lines, full file read) — the schema-version-rejection test to mirror (adapt wording: `ReadClusterHealth`'s error, per the Code Example above, does not include a "wipe the cache" instruction like `evidence.Reader.Read` does — just assert the schema_version number and path appear in the message):
```go
// pkg/evidence/reader_test.go:22-76 — TestReader_RejectsNonV2SchemaWithWipeInstruction
// structure to mirror for TestReadClusterHealth_RejectsWrongSchemaVersion:
//   1. t.TempDir(), write a well-formed JSON doc with only schema_version wrong
//   2. call the reader, require.Error
//   3. assert the error message contains the bad version number + path
```
Also cover: missing file (`errors.Is(err, fs.ErrNotExist)` / `os.IsNotExist`), malformed JSON, and the happy path against a fixture matching `health_writer.go`'s exact write format (`json.MarshalIndent(report, "", "  ")`, line 133).

#### `pkg/hubble/pipeline_test.go` — new `TestRunPipeline_FinalizesHealthOnStreamError`

**Analog:** itself — `TestRunPipeline_SurfacesStreamError` (lines 321-337) + its `errStreamSource` fixture (lines 296-316), the exact gap RESEARCH.md identified:
```go
// pkg/hubble/pipeline_test.go:296-316 — errStreamSource TODAY closes both
// flow channels immediately EMPTY before signaling the stream error. This is
// exactly why the existing test never accumulates any drop — there is
// nothing to accumulate. The new test needs a variant that emits at least
// one Infra-classified flow on the flow channel BEFORE closing it:
type errStreamSource struct{ err error }

func (e *errStreamSource) StreamDroppedFlows(...) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	fc := make(chan *flowpb.Flow)
	close(fc) // <-- new variant sends a flow here first, THEN closes
	lc := make(chan *flowpb.LostEvent)
	close(lc)
	return fc, lc, nil
}
func (e *errStreamSource) StreamErr() <-chan error {
	ec := make(chan error, 1)
	ec <- e.err
	close(ec)
	return ec
}

// pkg/hubble/pipeline_test.go:321-337 — the cfg shape to extend with
// EvidenceEnabled: true (today's test leaves it unset/false, which is
// PRECISELY why hw is nil and finalize() never gets exercised on this path):
cfg := PipelineConfig{
	FlushInterval: 10 * time.Millisecond,
	OutputDir:     tmpDir,
	Logger:        logger,
	// ADD for the new test: EvidenceEnabled: true, EvidenceDir: ..., OutputHash: ...
}
err := RunPipelineWithSource(context.Background(), cfg, source)
require.Error(t, err)
// ADD: assert cluster-health.json now exists at
// filepath.Join(cfg.EvidenceDir, cfg.OutputHash, "cluster-health.json")
```
Build the injected flow with `Verdict: flowpb.Verdict_DROPPED` and an Infra-classified `DropReasonDesc` (e.g. `flowpb.DropReason_CT_MAP_INSERTION_FAILED` — confirmed Infra-classified and remediation-linked at `pkg/dropclass/hints_test.go` line 15) plus a destination endpoint so `policyTargetEndpoint` (`pkg/hubble/aggregator.go:244`) resolves a namespace/workload. No existing `pkg/policy/testdata` helper builds a DROPPED/Infra flow directly (its flow builders — `pkg/policy/testdata/ingress_flow.go` — are all FORWARDED-style traffic-shape builders); construct the `*flowpb.Flow` literal inline in the new test.

#### `pkg/output/writer_test.go` — new `ReadPolicyFile` test

**Analog:** itself — `TestWriter_NewFileCreation` (lines 37-57):
```go
func TestWriter_NewFileCreation(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()
	w := NewWriter(dir, logger)
	event := buildTestEvent("default", "server")
	err := w.Write(event)
	require.NoError(t, err)
	path := filepath.Join(dir, "default", "server.yaml")
	...
}
```
Reuse `buildTestEvent(ns, workload string) policy.PolicyEvent` (writer_test.go lines 21-35, builds via `policy.BuildPolicy` + `testdata.IngressTCPFlow`) to produce a real on-disk YAML file via `w.Write(event)`, then call `ReadPolicyFile(path)` against that same path and assert the round-tripped CNP's `ObjectMeta.Name`/`Spec.Ingress`/`Spec.Egress` match. Add missing-file and malformed-YAML cases alongside.

#### `pkg/explain/filter_test.go` + `pkg/explain/render_test.go` (adapted, D-09 "mechanical move" — assertions unchanged)

**Analogs:** `cmd/cpg/explain_filter_test.go` (120 lines, full file read — 9 test functions, e.g. `TestFilterDirectionAndPort`, `TestFilterHTTPMethod`, `TestFilterAndCombination`) and `cmd/cpg/explain_test.go` (273 lines — targeted reads of `TestRenderJSON` lines 55-62, `TestExplainJSONOutput` lines 257-273):
```go
// cmd/cpg/explain_filter_test.go:13-20 — move verbatim, rename explainFilter → explain.Filter, f.match → f.Match
func TestFilterDirectionAndPort(t *testing.T) {
	rule := evidence.RuleEvidence{Direction: "ingress", Port: "8080"}
	f := explainFilter{Direction: "ingress", Port: "8080"}   // → explain.Filter{...}
	assert.True(t, f.match(rule))                             // → f.Match(rule)
	...
}

// cmd/cpg/explain_test.go:55-62 — the exact parity assertion QRY-03 rests on
func TestRenderJSON(t *testing.T) {
	buf := new(bytes.Buffer)
	require.NoError(t, renderJSON(buf, sampleEvidence(), sampleEvidence().Rules)) // → explain.RenderJSON
	var got explainOutput                                                          // → explain.Output
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, "cpg-api", got.Policy.Name)
	assert.Len(t, got.MatchedRules, 1)
}
```
`cmd/cpg/explain_test.go`'s remaining tests (`TestExplainFindsEvidenceAndPrintsText`, `TestExplainJSONOutput`, `TestBuildFilterNormalizesL7Inputs`, etc. — the ones that exercise cobra/`newExplainCmd()` end to end) stay in `cmd/cpg` unchanged, since `explain.go`'s cobra wiring itself is unchanged by D-09; only the pure filter/render unit tests move package.

---

## Shared Patterns

### Session resolution (all 5 query tools)
**Source:** `pkg/session/manager.go` lines 333-386 (`Manager.Status`)
**Apply to:** every query-tool handler, as the first line of the handler body
```go
result, err := mgr.Status(args.SessionID)
if err != nil {
	return nil, ZeroValueResult{}, err // SESS-06 "not found or expired" text, verbatim reuse
}
tmpDir := result.TmpDir
state := result.State // "capturing" | "stopped"
```
Zero new `pkg/session` surface — D-08 is explicit that this is the only call query tools ever make into that package.

### Error handling — return the Go error, never hand-construct `isError`
**Source:** `cmd/cpg/mcp_tools.go` lines 139-142 (comment) + every one of its 3 registrations (lines 106-165)
**Apply to:** all 5 query-tool handlers — D-16 locks this identically to Phase 17's convention. The go-sdk auto-converts a non-nil third return value into a tool-error result.

### Path-traversal guard on LLM-supplied `namespace`/`workload`
**Source:** `pkg/evidence/paths.go` lines 45-68 (`ValidatePolicyRef` / `validatePathComponent`) — already exported, already used on the *write* side (`pkg/output/writer.go` line 36: `evidence.ValidatePolicyRef(event.Namespace, event.Workload)`)
**Apply to:** `get_policy`, `get_evidence`, and the samples-half namespace/workload filtering in `list_dropped_flows` — every place an LLM-supplied string reaches a `filepath.Join`. RESEARCH.md's Security Domain section (V12) calls this out as the one concrete, actionable finding: reuse this exact guard symmetrically on the read side, do not write a second path-traversal check.
```go
// pkg/evidence/paths.go:51-56
func ValidatePolicyRef(namespace, workload string) error {
	if err := validatePathComponent("namespace", namespace); err != nil {
		return err
	}
	return validatePathComponent("workload", workload)
}
```

### Not-found detection — wrapped errors, never string/type checks
**Source:** `pkg/evidence/reader.go` lines 46-49 (`IsNotExist`) + `cmd/cpg/explain.go` lines 71-76 (`errors.Is(err, fs.ErrNotExist)` usage)
**Apply to:** `get_evidence` (evidence file), `get_policy`/`list_policies` (policy YAML — once `ReadPolicyFile` picks its not-exist convention, see the flag above), `get_cluster_health` (cluster-health.json)

### `DropClass` — single source of truth, never re-derive
**Source:** `pkg/dropclass/classifier.go` lines 257-270 (`DropClass.String()`)
```go
func (c DropClass) String() string {
	switch c {
	case DropClassPolicy: return "policy"
	case DropClassInfra: return "infra"
	case DropClassTransient: return "transient"
	case DropClassNoise: return "noise"
	default: return "unknown"
	}
}
```
**Apply to:** the `dropclass` schema-enum values (D-14) — `{"policy", "infra", "transient", "noise", "unknown"}`, referenced once when building the shared enum-patching helper (see "No Analog Found" below), never hand-copied a second time elsewhere.

**Behavioral note that belongs in D-15's taxonomy-lesson description text** (`pkg/hubble/aggregator.go` lines 417-443, the classification gate): only `infra`/`transient` ever reach `healthCh` → non-empty `aggregates[]` rows possible; `noise` is discarded entirely (`continue`, line 440) and `policy`/`unknown` both fall through to the CNP-generation path → non-empty `samples[]` rows possible only for `policy`. `dropclass=noise` and `dropclass=unknown` against `aggregates[]`, and `dropclass=infra`/`transient` against `samples[]`, are *structurally* always empty — not a bug to chase if observed in testing.

### Result-struct JSON tagging convention
**Source:** `pkg/session/session.go` lines 152-174 (`StartResult`, `StatusResult`)
**Apply to:** every new query-tool result struct — snake_case `json` tags, `omitempty` on optional fields, a `jsonschema:"..."` doc string alongside `json` on fields whose meaning isn't self-evident from the name alone (see `StatusResult.Error` line 173 for the dual-tag pattern).

---

## No Analog Found

Cross-cutting mechanisms with no existing codebase precedent — RESEARCH.md's vendor-source-verified pattern (not a codebase analog) is the correct and only source for these:

| Concern | Role | Data Flow | Reason no analog exists |
|---|---|---|---|
| `dropclass`/`direction` enum-constrained `InputSchema` construction | config (schema) | request-response | Phase 17's 3 session tools never use an enum-constrained field — every `startSessionArgs`/`sessionRef` field is a plain string/bool/[]string with a free-text `jsonschema:"..."` description. This phase is the first to need `Schema.Enum`, which requires bypassing struct-tag inference entirely (`jsonschema:"WORD=..."` tags are actively rejected — VERIFIED `jsonschema-go@v0.4.3/jsonschema/infer.go`). Use RESEARCH.md's `mustQuerySchema[T]` pattern verbatim (RESEARCH.md lines 224-258) — construct via `jsonschema.For[T](nil)` then patch `schema.Properties[field].Enum` before passing `Tool.InputSchema` to `mcp.AddTool`. |
| Opaque boundary-key pagination cursor (D-05/D-06/D-07) | utility | batch/pagination | No pagination exists anywhere in cpg today — Phase 17's 3 tools are all single-object responses. RESEARCH.md's Anti-Pattern section is explicit: build a stateless base64-encoded boundary key (last-emitted sort key, e.g. `namespace/workload` + an in-file index), decoded fresh every call — never an absolute-offset cursor (breaks under D-06's "no server-side snapshot, best-effort re-scan" model when the file set shifts mid-capture) and never server-side cursor state (single-session server; D-06 explicitly rejects the complexity). |
| `list_dropped_flows`'s two-reader composed-view assembly | domain read-model | transform (fan-in) | Genuinely new integration logic per RESEARCH.md's Architectural Responsibility Map — see the dedicated Pattern Assignment section above for the closest available building blocks (samples-half flattening struct, aggregates-half source, shared namespace/workload key helper, pagination-wraps-the-assembly principle). |

---

## Metadata

**Analog search scope:** `cmd/cpg/` (all `mcp*.go`, `explain*.go` files), `pkg/session/` (`manager.go`, `session.go`, `pipeline_config.go`), `pkg/evidence/` (`reader.go`, `paths.go`, `schema.go`, `reader_test.go`), `pkg/output/` (`writer.go`, `writer_test.go`), `pkg/hubble/` (`health_writer.go`, `pipeline.go`, `pipeline_test.go`, `aggregator.go`), `pkg/dropclass/` (`classifier.go`, `hints.go`), `pkg/policy/` (`builder.go`), `pkg/flowsource/` (promotion precedent, git history via commit `0aba62d`)
**Files scanned (read in full or targeted):** 24
**Pattern extraction date:** 2026-07-21
