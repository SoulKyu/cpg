# Phase 18: Query Tools - Research

**Researched:** 2026-07-21
**Domain:** MCP readonly query-tool surface over an existing Go CLI's session tmpdir (filesystem-only reads; zero new external dependencies; zero live-cluster/K8s interaction)
**Confidence:** HIGH — every finding below is grounded in a direct read of cpg's own source at its current committed state, plus the vendored `go-sdk`/`jsonschema-go` source at the exact pinned versions in `go.mod`. No web search was required or performed; this phase is pure internal-codebase archaeology, not ecosystem discovery.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Composed view & mid-capture availability (QRY-01 × QRY-04)**
- **D-01:** `list_dropped_flows` response = two explicit sections: `samples[]` (per-flow records from the capped evidence FlowSamples — policy-actionable drops by construction) and `aggregates[]` (per-reason infra/transient counts). Never synthesize pseudo-flow records from aggregate counters — the two halves have different fidelities and the schema says so.
- **D-02:** Mid-capture split: the samples half is served live (evidence files are atomic on disk during capture); the aggregates half returns an explicit `available_after_stop`-style marker while state=capturing (cluster-health.json is finalize-only; no in-memory pipeline hook — that's LIVE-01, v2). Exact mirror of QRY-04's non-error semantics. After stop, the full composed view reads cluster-health.json.
- **D-03:** Filter surface: `namespace`, `workload`, `dropclass` (enum), `direction` (ingress|egress) — all optional, AND-combined. No `since`/time-range filter in v1.5: misleading on a FIFO-capped sampled view.
- **D-04:** `total_count` counts items in the filtered *view* (samples + aggregate rows), never true flow totals — true totals stay in `get_status`/`stop_session` (`flows_seen`). The tool description carries REQUIREMENTS' wording verbatim: "a sampled/aggregated view, not a raw flow log".

**Pagination mechanics**
- **D-05:** Cursor = opaque base64 token encoding the position in a deterministic sort order (namespace, workload, stable index). Invalid or stale cursor → `isError` with actionable text ("invalid cursor; retry without cursor to restart from the first page").
- **D-06:** Consistency model: best-effort re-scan per call. The file set may shift between pages during an active capture — documented in the tool description, never an error, no server-side snapshot state (single-session server; pinning is complexity without payoff).
- **D-07:** Pagination applies to `list_dropped_flows` and `get_evidence` only (the "many records" tools per REQUIREMENTS/FEATURES). `list_policies` returns all metadata unpaginated (cheap rows, realistic cardinality in the dozens); `get_policy`/`get_cluster_health` are single-object. Default/max `limit` values: planner's pick, bounded well under the 25k-token MCP output cap (order of magnitude: default ~50 / max ~200 for flows; default ~20 for evidence rules).

**Read-side foundations**
- **D-08:** Query tools resolve a session via the existing `Manager.Status(id)` verbatim — it already returns `TmpDir`/`State`/`Error` with SESS-06 unknown/purged semantics. No new Manager API. `outputHash` for evidence paths is re-derived with `evidence.HashOutputDir(filepath.Join(tmpDir, "policies"))` — deterministic, matches `buildPipelineConfig`.
- **D-09:** `pkg/explain` promotion scope: the filter (`explainFilter.match`) + the three renderers (`renderJSON`/`renderText`/`renderYAML`) + the `explainOutput` type move to `pkg/explain` with exported names; cobra wiring and target parsing (`explain_target.go`) stay in `cmd/cpg`. Existing explain test suite re-runs unchanged (the `pkg/flowsource` v1.1 promotion precedent). "Identical to `cpg explain --output json`" (QRY-03) = the same renderer/struct produces the payload, not a re-implementation. Package is never named `pkg/mcp`.
- **D-10:** `get_evidence` argument surface: `namespace` + `workload` required (the evidence Reader's unit), optional filters mirroring the CLI explain flags (`direction`, `peer`, `port`, `protocol`, `http_method`, `http_path`, `dns_pattern`). Target discovery happens via `list_policies` (policies ↔ evidence are 1:1 by (namespace, workload)). No enumerate-all-evidence mode in v1.5. Pagination unit = matched `RuleEvidence` entries.
- **D-11:** `get_policy` identifies a policy by `namespace` + `workload` — the canonical on-disk key (`<tmpdir>/policies/<ns>/<workload>.yaml`). The roadmap criterion's "name" is interpreted as this key; the CNP `metadata.name` is returned in the response. `list_policies` rows: namespace, workload, CNP name, direction(s), rule counts, absolute path.
- **D-12:** Cluster-health reader: export the report types (`clusterHealthReport` → `ClusterHealthReport`, `healthDropJSON` → exported) and add `pkg/hubble.ReadClusterHealth(path)`. Passthrough semantics — no value transformation, per-reason Cilium remediation URLs intact. Typed structs are required to satisfy QRY-05's `outputSchema`; a raw `json.RawMessage` passthrough is rejected for exactly that reason.
- **D-13:** `get_cluster_health` branches: state=capturing → non-error `available_after_stop` result (QRY-04, locked); state=stopped + file present → passthrough; state=stopped + file absent (crash before finalize) → `isError` citing the pipeline error from `StatusResult.Error`. *(Researcher: verify whether the health writer's finalize runs on a pipeline crash — determines how often the third branch fires.)* **— See Common Pitfalls #1 and Open Question #1 below: this branch's framing needs a correction based on source-code evidence.**

**QRY-05 contract discipline**
- **D-14:** `dropclass` in schemas = string enum `policy|infra|transient|noise|unknown` — single source of truth is `pkg/dropclass.DropClass.String()`. Never expose the protobuf `DropReason` enum as a schema type; reason names remain informative strings alongside the class. *(Researcher: confirm how go-sdk expresses enums — jsonschema struct tag vs. explicit schema.)* **— See "State of the Art" and Code Examples below: fully confirmed, exact mechanism identified.**
- **D-15:** Every query-tool description carries 3 mandatory elements: (1) what it returns, (2) a 1–2 sentence dropclass taxonomy lesson (policy-actionable vs infra/transient — the classifier exists to stop the LLM from proposing policies for infra noise), (3) the tool-specific caveat (sampled view for QRY-01, available-after-stop for QRY-04/aggregates, drift-during-capture for paginated tools). Exact prose = planner.
- **D-16:** Annotations on all 4 query tools: `ReadOnlyHint: true`, `IdempotentHint: true`, `OpenWorldHint: false`. Error handling = return the Go error and let the SDK convert to `isError` (Phase 17 pattern, never hand-construct the result); texts specific and actionable — unknown session reuses the shipped SESS-06 message, policy-not-found suggests calling `list_policies`, invalid cursor per D-05.
- **D-17:** Result delivery matches the Phase 17 session tools: typed structs via `mcp.AddTool` (SDK infers `outputSchema` and emits JSON content). `get_policy` structuredContent: namespace, workload, CNP name, full YAML string, absolute path, rule counts. No hand-crafted dual preview/content blocks in v1.5 — consistency with the session tools wins.

### Claude's Discretion
- Exact JSON field names (snake_case, consistent with `StartResult`/`StatusResult`/`StopResult`).
- Exact default/max `limit` values within D-07's bound.
- Exact description prose (D-15's three elements are mandatory).
- File layout: `cmd/cpg/mcp_query_tools.go` (or similar) and `pkg/explain` file split.
- Cursor token encoding details (opaque per D-05).

### Deferred Ideas (OUT OF SCOPE)
None new — FLOW-01 (dedicated flow-sample writer), LIVE-01 (live mid-session counters), and REDACT-01 (HTTPPath redaction) were already tracked as v2 requirements before this discussion and stay there.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| QRY-01 | `list_dropped_flows(session_id, …filters)` composed view (capped evidence samples + aggregate health counts), paginated, explicitly documented as sampled/aggregated | Confirmed data sources for both halves (evidence `FlowSample[]` via `pkg/evidence.Reader`, health `Drops[].ByWorkload`/`ByNode` via new `pkg/hubble.ReadClusterHealth`); **found and documented a real gap**: the aggregates half's underlying `DropEvent`/`healthDropJSON` types carry no `direction` field at all, so D-03's `direction` filter cannot apply to that half (Pitfall #2, Open Question #1); confirmed `namespace`/`workload` semantics are consistent across both halves (both derive from the same `policyTargetEndpoint` helper, aggregator.go:243) — no design ambiguity there; confirmed `dropclass=noise`/`dropclass=unknown` will structurally always yield zero aggregate rows (Pitfall #3) |
| QRY-02 | `list_policies`/`get_policy`, torn-read safe, consistent during active writes | Confirmed `pkg/output/writer.go` is already atomic (temp+rename, SEC-02 landed Phase 16) — reads are torn-safe by construction, no new read-side retry strictly required (defense-in-depth still recommended per milestone Pitfall 5); confirmed exact CNP fields for rule counts/direction (`cnp.Spec.Ingress []api.IngressRule` / `cnp.Spec.Egress []api.EgressRule`, `cnp.ObjectMeta.Name`); confirmed `pkg/output.Writer.ReadExisting` already exported (raw YAML bytes) but the **parsed** CNP path (`readExistingPolicy`) is still unexported — needs a new exported sibling for `list_policies`' metadata rows |
| QRY-03 | `get_evidence(session_id, …filters)`, paginated, byte-identical-shape to `cpg explain --output json` via promoted `pkg/explain` | Confirmed exact `explainOutput` JSON shape (`policy`/`sessions`/`matched_rules`, 2-space-indented `json.NewEncoder`) from `cmd/cpg/explain_render.go`; confirmed `explainFilter.match` and the CLI's exact filter-building logic (`explain_filter.go`, `explain.go`); **clarified a subtlety**: "identical" governs the per-record shape (`PolicyRef`/`SessionInfo`/`RuleEvidence`), not the outer envelope — pagination is a layer the MCP tool adds *around* the promoted renderer's full matched-set output, not something baked into the renderer itself |
| QRY-04 | `get_cluster_health(session_id)`: finalized passthrough once stopped, explicit non-error "available after stop" while capturing | **Directly answers the phase's #1 explicit research question** — see Common Pitfalls #1: `hw.finalize()` runs unconditionally after `g.Wait()` in `pkg/hubble/pipeline.go`, regardless of whether the pipeline errored — the "stopped + file absent" branch is overwhelmingly caused by **zero infra/transient drops observed**, not by a crash-before-finalize race. D-13's binary framing needs a 3-way correction, detailed below |
| QRY-05 | `structuredContent`+`outputSchema`, truthful annotations, taxonomy-teaching descriptions, actionable `isError` | **Directly answers the phase's #2 explicit research question** — confirmed via direct read of `jsonschema-go v0.4.3`'s `infer.go` and `go-sdk v1.6.1`'s `server.go`: the `jsonschema` struct tag has **zero** support for `enum=`/`minimum=`/etc. constraint syntax — it is *always* a plain description string, and tags matching `WORD=` are explicitly rejected at schema-build time. The only mechanism is explicit `*jsonschema.Schema` construction, passed via `mcp.Tool.InputSchema`, which bypasses reflection-based inference entirely. Full working code pattern given below |

</phase_requirements>

## Summary

Phase 18 is the read-side half of v1.5's MCP surface, and — unlike Phases 16/17 — it introduces **zero new external dependencies**. Everything it needs (`github.com/modelcontextprotocol/go-sdk` v1.6.1, its transitive `github.com/google/jsonschema-go` v0.4.3, `sigs.k8s.io/yaml`, the `ciliumv2` API types) is already vendored and resolved in `go.mod`/`go.sum`. The entire phase is: promote two small chunks of existing `cmd/cpg` logic into importable packages (`pkg/explain`; a new exported listing/parsing helper on `pkg/output`; a new exported reader on `pkg/hubble`), then register 5 read-only tool handlers in the same composition-root style Phase 17 already established (`mcp.AddTool` + typed structs + "return the Go error, let the SDK convert it"). No pipeline change, no new writer, no live-cluster interaction of any kind.

Two of CONTEXT.md's three explicit research questions resolve cleanly and completely from direct source reads, with high confidence: (1) `healthWriter.finalize()` in `pkg/hubble/pipeline.go` runs **unconditionally** after `g.Wait()` returns — its call site is never gated on whether the pipeline's error is nil — so "cluster-health.json is missing after the session stopped" is overwhelmingly a **"zero infra/transient drops this session"** signal, not a crash signal; CONTEXT.md's D-13 branch needs a narrow but important reframing (below). (2) go-sdk v1.6.1's schema inference (via `jsonschema-go` v0.4.3) parses the `jsonschema` struct tag as pure free-text description — there is no `enum=`/constraint-keyword syntax at all, and the library actively rejects tags shaped like one. `dropclass`'s enum constraint (D-14) can only be added via explicit `*jsonschema.Schema` construction assigned to `Tool.InputSchema`, which the SDK will use as-is instead of its own reflection-based inference. A complete, ready-to-adapt code pattern is included below, and it extends naturally to `direction` (also a small closed set used by both `list_dropped_flows` and `get_evidence`).

The third explicit question (D-09's `explainOutput` shape) is fully confirmed: `{policy: evidence.PolicyRef, sessions: []evidence.SessionInfo, matched_rules: []evidence.RuleEvidence}`, 2-space-indented via `json.NewEncoder`. One clarification worth carrying into planning: "identical to `cpg explain --output json`" governs the *shape of each record*, not the outer response envelope — `get_evidence`'s pagination metadata (`limit`/`cursor`/`total_count`/`has_more`) wraps around the promoted renderer's (unpaginated) full matched-rule list; the MCP handler slices that list before emitting `structuredContent`, it does not ask the renderer itself to paginate.

Beyond the three explicit questions, this research surfaced one load-bearing gap the planner needs to resolve explicitly: the `direction` filter locked into D-03 has **no data to act on** for `list_dropped_flows`'s aggregates half — `DropEvent` (aggregator.go) and `healthDropJSON`/`cluster-health.json` (health_writer.go) carry no traffic-direction field anywhere. This isn't fixable within this phase's "no new pipeline writer" boundary (adding direction to health accumulation would be exactly the kind of silent scope-widening the milestone's own ARCHITECTURE.md warns against). The recommended resolution — apply `direction` to the samples half only, and document explicitly that aggregate rows are never direction-scoped — is detailed in Common Pitfalls #2 and Open Question #1.

**Primary recommendation:** Treat this phase as "export three small reader-side helpers, wire five `mcp.AddTool` registrations following Phase 17's exact pattern, hand-build one shared `*jsonschema.Schema` per args struct that needs an enum field." No new packages in `go.mod`, no pipeline changes, no live-cluster code paths.

## Architectural Responsibility Map

> cpg is a single Go binary / stdio MCP server, not a multi-tier web app. The generic browser/SSR/API/CDN/DB tiers don't apply; tiers below are adapted to this domain's actual boundaries (confirmed by reading `cmd/cpg/mcp.go`, `pkg/session/manager.go`, and the milestone ARCHITECTURE.md).

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Tool registration, schema/annotation contract (QRY-05) | MCP Composition Root (`cmd/cpg/mcp_query_tools.go`, new) | — | Same tier Phase 17's session tools already live in; the only place that imports the SDK's `mcp` package |
| Session/tmpdir resolution for all 5 tools | Session State (`pkg/session.Manager.Status`) | MCP Composition Root | D-08 locks zero new Manager API — the tool handler is a thin caller |
| `list_dropped_flows` composed-view assembly, filtering, pagination | Domain Read-Model (new logic in `cmd/cpg/mcp_query_tools.go` or a small new reader) | Filesystem/Storage (session tmpdir) | Genuinely new integration logic — no existing package does this composition; must NOT live in `pkg/hubble` or `pkg/evidence` (those stay single-purpose readers) |
| `list_policies`/`get_policy` metadata + YAML | Domain Read-Model (`pkg/output`, new exported helper) | Filesystem/Storage | Reuses `pkg/output.Writer.ReadExisting` (already exported) for raw YAML; needs one new exported parse-to-CNP helper for metadata rows |
| `get_evidence` | Domain Read-Model (`pkg/explain`, promoted) | Filesystem/Storage (`pkg/evidence.Reader`) | Zero new design — mechanical promotion of already-decoupled CLI logic (D-09) |
| `get_cluster_health` | Domain Read-Model (`pkg/hubble.ReadClusterHealth`, new export) | Filesystem/Storage | Passthrough only, per D-12 — no value transformation |
| K8s / live Hubble Relay | **Not touched by this phase at all** | — | Deliberate absence — Phase 18 tools are pure tmpdir readers; any accidental import of `pkg/k8s` write-adjacent helpers here would violate SEC-01 (audited Phase 19) |

## Standard Stack

### Core (all already resolved in `go.mod`/`go.sum` — zero new packages)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/modelcontextprotocol/go-sdk/mcp` | v1.6.1 [VERIFIED: go.mod] | Tool registration (`mcp.AddTool`), schema inference/validation, `isError` conversion | Already the phase 16/17 standard; zero API surface change needed for Phase 18 beyond calling `mcp.AddTool` 5 more times |
| `github.com/google/jsonschema-go/jsonschema` | v0.4.3 [VERIFIED: go.mod, go.sum — transitive via go-sdk since Phase 16's legitimacy gate (16-02-PLAN.md)] | Explicit `*jsonschema.Schema` construction for the `dropclass`/`direction` enum fields (D-14) | Already vetted by Phase 16's dependency-legitimacy gate as part of go-sdk's own dependency tree; Phase 18 promotes it from an indirect to a **direct** import (see Package Legitimacy Audit) |
| `sigs.k8s.io/yaml` | v1.6.0 [VERIFIED: go.mod — already a direct dependency] | CNP YAML marshal/unmarshal for `list_policies`/`get_policy` | Already used identically by `pkg/output/writer.go`'s `readExistingPolicy` and `cmd/cpg/explain_target.go` |
| `github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2` (`ciliumv2`) | v1.19.4 [VERIFIED: go.mod] | `CiliumNetworkPolicy` struct for parsing generated policy YAML | Already the type `pkg/output`, `pkg/policy` build against |

### Supporting
No new supporting libraries. `pkg/evidence.Reader`, `pkg/dropclass.DropClass`, `pkg/session.Manager` are all reused unmodified per D-08/D-09/D-12/D-14.

### Alternatives Considered
Not applicable — no new dependency decision exists in this phase. The only "choice" is a mechanism choice within an already-chosen library (explicit schema construction vs. struct tags for the enum, resolved definitively below — struct tags are not an option, not a preference).

**Installation:**
```bash
# No `go get` required — jsonschema-go is already in go.sum. Once cmd/cpg imports
# it directly (for the explicit Schema construction pattern below), run:
go mod tidy
# This only moves `github.com/google/jsonschema-go v0.4.3` from an `// indirect`
# to a direct require line — no version change, no new download.
```

**Version verification:**
```bash
$ grep jsonschema-go /home/gule/Workspace/team-infrastructure/cpg/go.mod
	github.com/google/jsonschema-go v0.4.3 // indirect
$ grep 'go-sdk ' /home/gule/Workspace/team-infrastructure/cpg/go.mod
	github.com/modelcontextprotocol/go-sdk v1.6.1
```
Both confirmed present and already resolved — no registry lookup needed, no staleness risk (these are the exact versions already running in Phases 16/17's shipped code).

## Package Legitimacy Audit

**Not applicable — this phase installs zero new external packages.** All libraries touched (`go-sdk`, `jsonschema-go`, `sigs.k8s.io/yaml`, `ciliumv2`) are already present in `go.mod`/`go.sum`, and `github.com/modelcontextprotocol/go-sdk` (which pulls in `jsonschema-go` transitively) already passed Phase 16's dependency-legitimacy gate (`16-02-PLAN.md`: "go-sdk v1.6.1 dependency legitimacy gate + install"). The only `go.mod` change this phase causes is `github.com/google/jsonschema-go` moving from an indirect to a direct require line (via `go mod tidy` after `cmd/cpg` imports its `jsonschema` subpackage directly) — same version, same already-audited code, no new attack surface.

**Packages removed due to slopcheck [SLOP] verdict:** none (n/a — no new packages)
**Packages flagged as suspicious [SUS]:** none (n/a — no new packages)

## Architecture Patterns

### System Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│ MCP Host / LLM harness (external process)                                │
└───────────────┬───────────────────────────────────────────▲──────────────┘
                 │ tools/call (list_dropped_flows, list_policies,          │ structuredContent
                 │  get_policy, get_evidence, get_cluster_health)          │ + outputSchema + isError
┌────────────────▼───────────────────────────────────────────┴─────────────┐
│ cmd/cpg/mcp_query_tools.go  (NEW — composition root, package main)        │
│                                                                            │
│  1. mgr.Status(session_id) ──► TmpDir, State, Error (D-08, zero new API) │
│  2. dispatch by tool:                                                    │
│                                                                            │
│  ┌──────────────────┐  ┌───────────────┐  ┌──────────────┐  ┌──────────┐│
│  │ list_dropped_flows│  │ list_policies/│  │ get_evidence  │  │get_cluster││
│  │ (composed view)   │  │ get_policy    │  │               │  │_health    ││
│  └────────┬──────────┘  └───────┬───────┘  └───────┬───────┘  └────┬─────┘│
│           │ reads BOTH           │ reads             │ reads         │ reads│
└───────────┼──────────────────────┼───────────────────┼───────────────┼─────┘
            ▼                      ▼                   ▼               ▼
  ┌──────────────────┐  ┌────────────────────┐ ┌─────────────────┐ ┌────────────────┐
  │pkg/evidence.Reader│  │pkg/output (new      │ │pkg/explain (NEW, │ │pkg/hubble.      │
  │.Read(ns,wl)       │  │exported CNP-parse   │ │promoted from     │ │ReadClusterHealth│
  │→ FlowSample[]      │  │helper) + existing   │ │cmd/cpg): filter  │ │(NEW export)     │
  │(samples half)      │  │.ReadExisting(ns,wl) │ │+ render, reused  │ │→ ClusterHealth  │
  │                    │  │(raw YAML)           │ │verbatim          │ │Report            │
  └─────────┬──────────┘  └──────────┬──────────┘ └────────┬─────────┘ └────────┬────────┘
            │                        │                     │                    │
            ▼                        ▼                     ▼                    ▼
   <tmpdir>/evidence/       <tmpdir>/policies/     <tmpdir>/evidence/    <tmpdir>/evidence/
   <hash>/<ns>/<wl>.json    <ns>/<wl>.yaml         <hash>/<ns>/<wl>.json <hash>/cluster-health.json
   (also feeds aggregates                                                (absent while capturing —
    half via ClusterHealth-                                               D-02/QRY-04, OR absent
    Report.Drops[].ByWorkload)                                            because zero infra/
                                                                           transient drops — see
                                                                           Pitfall #1)
```

A reader tracing `list_dropped_flows` end to end: MCP host calls the tool → handler resolves `TmpDir`/`State` via `Manager.Status` → for the samples half, walk every `<tmpdir>/evidence/<hash>/**/*.json`, parse each via `evidence.Reader`/raw `PolicyEvidence`, flatten every `RuleEvidence.Samples[]` into a composed record enriched with the parent rule's `namespace`/`workload`/`direction` (a raw `FlowSample` alone carries none of those) → for the aggregates half, if `State == capturing` emit the `available_after_stop` marker (D-02); if stopped, call `pkg/hubble.ReadClusterHealth` and flatten `Drops[].ByWorkload` into per-namespace/workload/reason count rows → apply the AND-combined filters (D-03) — noting `direction` only ever matches something in the samples half (see Pitfall #2) → paginate the combined, filtered rows via the D-05 cursor scheme → emit `structuredContent` (`samples[]`, `aggregates[]`, `total_count`, `has_more`, `next_cursor`).

### Recommended Project Structure
```
cmd/cpg/
├── mcp.go                 # composition root (existing, Phase 16/17) — add
│                           #   registerQueryTools(server, mgr) call here
├── mcp_tools.go            # session tools (existing, Phase 17, unchanged)
├── mcp_query_tools.go      # NEW: 5 tool registrations + handler glue
├── explain.go              # THINNED — cobra wiring + cmd.OutOrStdout() only;
│                           #   calls into pkg/explain for filter+render
├── explain_filter.go        # PROMOTED OUT — logic moves to pkg/explain/filter.go
├── explain_render.go         # PROMOTED OUT — logic moves to pkg/explain/render.go
├── explain_target.go         # STAYS — cobra-arg / YAML-path resolution is
│                           #   CLI-only per D-09, not shared with get_evidence
pkg/explain/                 # NEW package (mechanical move, D-09)
├── filter.go                # exported Filter + Match(), same logic as explainFilter.match
├── render.go                 # exported RenderJSON/RenderText/RenderYAML + Output type
│                           #   (explainOutput → Output), unpaginated — MCP tool
│                           #   handler paginates the matched-rule slice it gets back
pkg/output/
├── writer.go                # existing — ADD one new exported func, e.g.
│                           #   ReadPolicyFile(path) (*ciliumv2.CiliumNetworkPolicy, error)
│                           #   (reuses readExistingPolicy's unmarshal logic)
pkg/hubble/
├── health_writer.go          # existing — export ClusterHealthReport/HealthDropJSON
│                           #   (clusterHealthReport/healthDropJSON today)
├── health_reader.go           # NEW sibling file — ReadClusterHealth(path string)
│                           #   (*ClusterHealthReport, error), SchemaVersion-gated
│                           #   like evidence.Reader.Read
```

### Pattern 1: Explicit `*jsonschema.Schema` construction for enum-constrained args (the D-14 answer)

**What:** go-sdk v1.6.1's `mcp.AddTool[In, Out]` infers `Tool.InputSchema` via `jsonschema.ForType` **only when `Tool.InputSchema` is nil**. If the caller pre-populates `Tool.InputSchema` with a `*jsonschema.Schema` value before calling `mcp.AddTool`, the SDK uses it verbatim (`setSchema`, `mcp/server.go:453` — "Schema was provided: check cache by pointer, or resolve it"). This is the *only* way to add an `Enum` constraint, because `jsonschema.For`'s struct-tag inference (`jsonschema-go/jsonschema/infer.go:329-337`) treats the entire `jsonschema:"..."` tag value as a plain `Description` string — nothing else — and explicitly **rejects** any tag matching `^[^ \t\n]*=` (i.e. `WORD=...`) at schema-build time: `"tag must not begin with 'WORD=': %q"`. There is no `enum=`, `minimum=`, or any other constraint-keyword tag syntax in this version.

**When to use:** Any args struct with a small, closed-set string field — in this phase, `dropclass` (`list_dropped_flows`) and `direction` (`list_dropped_flows`, `get_evidence`).

**Example:**
```go
// Source: direct read of github.com/google/jsonschema-go@v0.4.3/jsonschema/{infer,schema}.go
// and github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/server.go (setSchema, toolForErr)
import "github.com/google/jsonschema-go/jsonschema"

type listDroppedFlowsArgs struct {
	SessionID string `json:"session_id" jsonschema:"the opaque session_id returned by start_session"`
	Namespace string `json:"namespace,omitempty" jsonschema:"filter: namespace"`
	Workload  string `json:"workload,omitempty" jsonschema:"filter: workload"`
	DropClass string `json:"dropclass,omitempty" jsonschema:"filter: policy-actionable vs infra/transient/noise/unknown (see tool description)"`
	Direction string `json:"direction,omitempty" jsonschema:"filter: ingress or egress"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max rows per page (default/max: planner's choice, D-07)"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"opaque pagination token from a previous call's has_more response"`
}

// mustQuerySchema builds the struct-tag-inferred schema, then patches in the
// two enum constraints jsonschema struct tags cannot express. Panics only on
// an internal programming error (unsupported Go field type) — same
// fail-fast convention mcp.AddTool itself uses for schema errors.
func mustQuerySchema[T any](enumFields map[string][]any) *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("building schema for %T: %v", *new(T), err))
	}
	for field, values := range enumFields {
		prop, ok := schema.Properties[field]
		if !ok {
			panic(fmt.Sprintf("schema for %T has no property %q to constrain", *new(T), field))
		}
		prop.Enum = values
	}
	return schema
}

// At registration time:
schema := mustQuerySchema[listDroppedFlowsArgs](map[string][]any{
	"dropclass": {"policy", "infra", "transient", "noise", "unknown"}, // pkg/dropclass.DropClass.String() values (D-14)
	"direction": {"ingress", "egress"},
})
mcp.AddTool(server, &mcp.Tool{
	Name:        "list_dropped_flows",
	Description: "...", // D-15's 3 mandatory elements
	InputSchema: schema,
	Annotations: &mcp.ToolAnnotations{
		ReadOnlyHint:   true,
		IdempotentHint: true,
		OpenWorldHint:  jsonschema.Ptr(false), // *bool field — D-16 requires explicit false, not omission
	},
}, handleListDroppedFlows)
```
**Note on `OpenWorldHint`:** unlike `ReadOnlyHint`/`IdempotentHint` (plain `bool`), `ToolAnnotations.OpenWorldHint` is `*bool` [VERIFIED: go-sdk v1.6.1 `mcp/protocol.go:1372-1377`] — omitting it defaults to "true" per the spec doc comment, so D-16's locked `OpenWorldHint: false` **must** be set via a pointer. `jsonschema.Ptr[T any](x T) *T` [VERIFIED: `jsonschema-go@v0.4.3/jsonschema/schema.go:541`] is already available in the same package the enum pattern imports — no extra helper needed. Phase 17's `mcp_tools.go` never sets this field on any of its 3 tools (leaves it at the spec's implicit "true" default) — Phase 18 is the first phase where this pointer subtlety actually matters.

### Pattern 2: Pagination is a layer *around* the promoted `pkg/explain` core, not inside it (clarifies D-09/QRY-03)

**What:** `cpg explain --output json` is not paginated and stays that way — `pkg/explain`'s promoted `RenderJSON`-equivalent (or, more precisely, the promoted `Output`/`explainOutput` struct) keeps producing the **full** filtered `matched_rules` set, exactly as today. QRY-03's "identical to `cpg explain --output json`" constraint is about the shape of `policy`/`sessions`/`matched_rules[i]`, not about whether the whole set arrives in one call.

**When to use:** `get_evidence`'s MCP handler calls the promoted filter (`pkg/explain.Filter.Match` or equivalent) to get the complete matched `[]evidence.RuleEvidence`, THEN slices that Go slice according to `limit`/`cursor` before constructing `structuredContent` — the pagination metadata (`total_count`, `has_more`, `next_cursor`) lives in the MCP response envelope, one level above the promoted renderer's own output shape.

**Trade-off:** keeps `pkg/explain` genuinely shared and untouched by the MCP-specific pagination concern — the CLI's `cpg explain --output json` and a hypothetical future non-paginated consumer both keep working unmodified. The alternative (teaching the renderer itself to paginate) would couple `pkg/explain` to an MCP-only concept for no benefit.

### Pattern 3: `namespace`/`workload` filter semantics are already consistent across both `list_dropped_flows` halves — no ambiguity to resolve

**What:** `buildDropEvent` (aggregator.go, feeds the aggregates half via `cluster-health.json`) and `keyFromFlow` (feeds the policy/evidence bucketing that produces the samples half) **share the identical `policyTargetEndpoint` helper** — confirmed directly in source: `// both buildDropEvent and keyFromFlow delegate to this helper (M3 dedup)` [VERIFIED: `pkg/hubble/aggregator.go:243`]. Both resolve "the endpoint that would receive the generated policy" (ingress destination / egress source), never the arbitrary peer. So a `namespace`/`workload` filter on `list_dropped_flows` means the same thing for both halves without any extra design work: "drops attributed to (or that would be attributed to) this workload needing a policy."

**When to use:** Apply `namespace`/`workload` filters identically to both halves — for samples, match against the evidence file's `PolicyEvidence.Policy.Namespace`/`.Workload` (the file's own identity — equivalently, `get_policy`'s own key, D-11); for aggregates, match against the `"namespace/workload"` keys inside each `healthDropJSON.ByWorkload` map (splitting on the first `/`, honoring the `_unknown` sentinel `health_writer.go`'s `accumulate()` already uses for missing labels).

### Anti-Patterns to Avoid

- **Synthesizing a fake `direction` for aggregate rows:** since `DropEvent`/`healthDropJSON` have no direction field (Pitfall #2 below), do not infer or guess one (e.g., from `DropReason` name heuristics) to make the `direction` filter "work" on aggregates — this fabricates data the pipeline never captured. Document the limitation instead (D-15's mandatory caveat element already gives a natural home for this sentence).
- **Re-deriving the CNP-YAML-to-struct parse logic instead of exporting `readExistingPolicy`'s logic:** `pkg/output/writer.go` already has this exact unmarshal path; duplicating it in the query-tool handler forks a parse path that must stay in sync forever (same class of risk ARCHITECTURE.md's Anti-Pattern 3 warns about for the writer itself).
- **Exposing the raw 76-value `flowpb.DropReason` protobuf enum as a schema-level `enum`:** D-14 is explicit that only the 5-value `DropClass` gets schema-enum treatment; `DropReason` names stay as documented free-text strings (milestone PITFALLS.md Pitfall 6, reaffirmed here with the concrete mechanism now nailed down).
- **Offset-based (absolute integer position) cursor tokens under D-06's "no server-side snapshot, best-effort re-scan" model:** an absolute row-index cursor ("skip 50") silently skips or repeats items when the underlying file set changes size between calls (files added/removed mid-capture). Prefer a **boundary-key** cursor — the last-emitted sort key (e.g., `namespace/workload` + an index within that file) — so a re-scan resumes from "the first item after X" rather than "item N," which degrades gracefully (occasional skip/dup at the exact boundary) rather than catastrophically (whole pages silently shifted) under concurrent writes.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| CNP YAML → struct parsing for `list_policies` metadata rows | A second YAML-unmarshal-into-`ciliumv2.CiliumNetworkPolicy` path in the query-tool handler | Export a sibling of `pkg/output/writer.go`'s unexported `readExistingPolicy` (e.g. `output.ReadPolicyFile(path)`) | Same unmarshal logic already exists, already handles the `sigs.k8s.io/yaml` roundtrip cpg standardized on; forking it means two places can silently disagree after a future CNP schema change |
| Per-rule flow attribution rendering for `get_evidence` | A new MCP-only JSON shape for evidence | The promoted `pkg/explain` renderer/struct (D-09), verbatim | This is the single highest reuse-to-value item in the whole v1.5 milestone per FEATURES.md — the CLI and MCP surfaces must never drift apart |
| Cluster-health report parsing/validation | A second `SchemaVersion`-gated JSON unmarshal for `cluster-health.json` | `pkg/hubble.ReadClusterHealth` (D-12, new export), mirroring `evidence.Reader.Read`'s exact schema-version-gate pattern (`pkg/evidence/reader.go:36-42`) | One canonical reader for a finalize-only file that only this phase will ever read from Go code |
| DropClass enum constraint in a tool schema | A hand-maintained duplicate list of `"policy"/"infra"/"transient"/"noise"/"unknown"` strings scattered across schema-construction call sites | Single source of truth `pkg/dropclass.DropClass.String()`'s 5 return values, referenced once when building the shared `mustQuerySchema` enum list | classifier.go's own doc comment: "the single source of truth for DropClass labels — no other package should duplicate this mapping" |
| Pagination cursor mechanism | A generic pagination library or a bespoke stateful server-side cursor registry | A stateless opaque base64-encoded boundary key (Pattern/Anti-Pattern above), decoded fresh on every call | D-06 explicitly rejects server-side snapshot state ("single-session server; pinning is complexity without payoff") — a stateless boundary-key token is the minimum viable mechanism that satisfies D-05/D-06 together |

**Key insight:** every "don't hand-roll" item in this phase is "don't build a second version of something cpg already has" — there is no third-party library gap anywhere in this phase's scope. The risk profile is entirely about internal consistency (two parse paths, two enum lists, two JSON shapes silently diverging), not about missing tooling.

## Common Pitfalls

### Pitfall 1: "stopped + cluster-health.json absent" is a "zero drops" signal far more often than a crash signal — D-13's branching needs a 3-way correction

**What goes wrong:** CONTEXT.md's D-13 frames the tool's third branch as "state=stopped + file absent (**crash before finalize**) → `isError` citing the pipeline error." Implemented literally, this makes `get_cluster_health` return `isError` for the single most common **successful, healthy** outcome of a short or clean capture session: zero infra/transient drops observed.

**Why it happens:** Directly reading `pkg/hubble/pipeline.go`'s errgroup wiring [VERIFIED: `pkg/hubble/pipeline.go:317-362`] shows `hw.finalize(stats)` is called **unconditionally** after `err = g.Wait()` — there is no `if err == nil` gate anywhere between them. `finalize()` itself [VERIFIED: `pkg/hubble/health_writer.go:88-96`] is a no-op (does not write the file) in exactly two cases: `hw == nil` (never true in MCP mode — `buildPipelineConfig` always sets `EvidenceEnabled: true` [VERIFIED: `pkg/session/pipeline_config.go:89`]), or `len(hw.drops) == 0` — i.e. **zero Infra/Transient drops were ever accumulated this session**, which is the normal, unremarkable case for a short session or a healthy cluster. So: the file being absent after `stop_session` (whether via explicit stop or via WR-01's autonomous-crash transition, `pkg/session/manager.go:215-265`) says almost nothing about whether the pipeline crashed — it says "no infra/transient drops occurred before the pipeline stopped." A genuine pipeline error (Stage 0's non-EOF stream failure, `pipeline.go:219-242`) still runs `hw.finalize()` on its way out, and cluster-health.json **will** exist if any Infra/Transient drop was accumulated before the crash.

The only cases where `hw.finalize()` genuinely does not (or cannot) run are: (a) an unrecovered panic inside one of the pipeline's own errgroup goroutines — but Go crashes the *entire process* on an unhandled panic in any goroutine, so the whole `cpg mcp` server dies too; there is no live process left to answer `get_cluster_health` in that scenario, making it moot for this tool's design; (b) `finalize()`'s own atomic-write step failing (disk full, permission denied) — logged as a `zap.Warn` [VERIFIED: `pkg/hubble/pipeline.go:360-362`] but **not** propagated into the pipeline's returned error, so `StatusResult.Error` would be empty even though the file is genuinely missing — a case D-13's "isError citing StatusResult.Error" text has nothing to cite.

There's also a narrower, self-resolving race worth naming: `Manager.Stop`'s bounded wait (`stopWait`, default 5s) [VERIFIED: `pkg/session/manager.go:423-434`] can time out and mark `State = StateStopped` while the pipeline goroutine (and its `finalize()` call) is still in flight — a `get_cluster_health` call issued in that narrow window would also see "stopped + absent," resolving to "present" on a retry moments later.

**How to avoid:** Recommend a 3-way (not 2-way) branch for the planner:
1. `state == stopped` + file present → passthrough (regardless of whether the pipeline crashed — a crash with drops observed still has a useful report).
2. `state == stopped` + file absent + `StatusResult.Error == ""` → **not an error**. This is the common "zero infra/transient drops this session" case (or the narrow Stop-bounded-wait race, which self-resolves). Return a non-error, explicit "no infra/transient drops observed this session" result — analogous in spirit to D-02's `available_after_stop` marker, not a failure.
3. `state == stopped` + file absent + `StatusResult.Error != ""` → `isError` citing `StatusResult.Error` (the scenario D-13 actually intended — but genuinely rarer than the framing suggests, since a crash usually still leaves a report if any drops happened first).

**Warning signs:** if implemented as a strict 2-way branch, every short/successful test-fixture session in the new test suite that has zero simulated infra drops will produce an `isError` result from `get_cluster_health` — an easy, misleading thing to paper over in a hand-written test rather than recognizing as a design bug.

**Phase to address:** now, before `get_cluster_health`'s handler is written — recorded as Open Question #1 below since it revises a CONTEXT.md-locked decision's *framing* (not its intent).

---

### Pitfall 2: `direction` (D-03's locked filter) has zero data to act on in the aggregates half of `list_dropped_flows`

**What goes wrong:** D-03 locks `direction` (ingress|egress) as an AND-combined filter across `list_dropped_flows`. Applied to the aggregates half, this filter has nothing to match against — it will either silently no-op (return all aggregate rows regardless of the filter) or, if implemented naively assuming every row has a direction, panic/misbehave.

**Why it happens:** `DropEvent` (aggregator.go, the type populated for every Infra/Transient flow and fed to `healthCh`) [VERIFIED: `pkg/hubble/aggregator.go:21-27`] carries exactly `Reason`, `Class`, `Namespace`, `Workload`, `NodeName` — no direction field. `buildDropEvent` [VERIFIED: `pkg/hubble/aggregator.go:256-277`] constructs it from the flow without ever reading `f.TrafficDirection`. `healthDropJSON` (the `cluster-health.json` on-disk shape, and the type D-12 exports as-is) [VERIFIED: `pkg/hubble/health_writer.go:253-265`] equally has no direction field — only `Reason`, `Class`, `Count`, `Remediation`, `ByNode`, `ByWorkload`. This is a genuine, structural absence in the health-reporting data model (shipped Phase 11, unrelated to this phase), not an oversight in this phase's design — and fixing it would require changing what `healthCh`/`accumulate()` capture, which is exactly the kind of pipeline-writer change this phase's "no new pipeline writer" boundary forecloses (mirroring the milestone ARCHITECTURE.md's own caution against silently widening scope).

By contrast, the samples half's `RuleEvidence.Direction` field [VERIFIED: `pkg/evidence/schema.go:54` — `Direction string `json:"direction"` // "ingress" | "egress"`] **does** carry direction — so `direction` filtering is fully meaningful there.

**How to avoid:** Document explicitly (this is exactly the kind of caveat D-15 already mandates a slot for): `direction` filters the `samples[]` half only; `aggregates[]` rows are returned regardless of the `direction` filter's value, because cluster-health.json has no direction dimension to filter on. Do not hide `aggregates[]` rows when `direction` is set (that would silently under-report infra/transient drop counts) — surface them unfiltered and say so.

**Warning signs:** a test that sets `direction=ingress` and asserts the aggregates count changes will always fail (or, if written to expect no change, will silently encode this gap without anyone noticing it needed documenting).

**Phase to address:** now — the tool description text (D-15) is the natural place to encode this; recorded as Open Question #1.

---

### Pitfall 3: `dropclass=noise` and `dropclass=unknown` are guaranteed to return zero aggregate rows — and possibly zero sample rows too — this is by design, not a bug to chase

**What goes wrong:** A filter combination that always returns an empty result set looks, to someone debugging it later, like a bug in the composed-view assembly logic.

**Why it happens:** `Aggregator.Run`'s classification-gate switch [VERIFIED: `pkg/hubble/aggregator.go:417-443`] shows: `DropClassNoise` → `continue` (discarded entirely — never counted anywhere, never reaches `healthCh`, per classifier.go's own comment "internal bookkeeping; ignore entirely"). `DropClassInfra`/`DropClassTransient` → the only two classes that reach `healthCh` and thus `cluster-health.json`. `DropClassPolicy` and `DropClassUnknown` both fall through to the same bucketing path that eventually produces CNP rules and evidence — meaning `dropclass=unknown` in the aggregates half will *also* always be empty (Unknown never reaches the health path either). So of the 5-value enum (`policy|infra|transient|noise|unknown`), only `infra`/`transient` can ever produce non-empty `aggregates[]` rows, and only `policy` is the "intended" value for non-empty `samples[]` rows (evidence is captured for the flows that survive to become policy rule candidates).

**How to avoid:** State this plainly in the tool description (this is a great, concrete anchor for D-15's mandatory "1-2 sentence dropclass taxonomy lesson" — it's not just background theory, it directly explains observed tool behavior). Do not add defensive code that treats an empty result for `dropclass=noise` as an error condition or a sign of a broken filter.

**Phase to address:** now — description-writing, not code-defensive-programming.

---

### Pitfall 4: `evidence.Reader.Read`'s not-found error is wrapped — compare with `errors.Is`/`evidence.IsNotExist`, never a raw string/type check

**What goes wrong:** `get_evidence`'s "policy not found, call `list_policies`" actionable error text (D-16) needs to distinguish "no evidence file for this namespace/workload" from any other read failure.

**Why it happens:** `Reader.Read` wraps the underlying `os.ReadFile` error: `fmt.Errorf("reading evidence %s: %w", path, err)` [VERIFIED: `pkg/evidence/reader.go:26-31`]. A naive `err == os.ErrNotExist` or string-matching check will never match. The package already exports the correct helper: `evidence.IsNotExist(err) bool` (wraps `errors.Is(err, fs.ErrNotExist)`) [VERIFIED: `pkg/evidence/reader.go:46-49`] — `cmd/cpg/explain.go` itself uses the equivalent `errors.Is(err, fs.ErrNotExist)` pattern directly [VERIFIED: `cmd/cpg/explain.go:71-76`] to produce its own actionable CLI error text.

**How to avoid:** Reuse `evidence.IsNotExist(err)` (or the identical `errors.Is(err, fs.ErrNotExist)` form) in the `get_evidence` handler exactly as `cmd/cpg/explain.go` already does, to build the "no evidence for ns/workload — call list_policies" text.

**Phase to address:** now — trivial once known, easy to get subtly wrong (silent fallthrough to a generic error) if not.

---

### Pitfall 5: policies and evidence are "1:1 by (namespace, workload)" (D-10) as a design intent, but not synchronized as a filesystem fact — a policy can transiently exist without its evidence file, or vice versa

**What goes wrong:** `list_policies` discovers a `(namespace, workload)` pair; a `get_evidence` call for that exact pair returns "not found" moments later, looking like data corruption or a broken 1:1 assumption.

**Why it happens:** Pipeline Stage 1b fans a single `PolicyEvent` out to **both** `policyCh` and `evidenceCh` sequentially in program order, but the policy writer and evidence writer are two independent goroutines consuming two independent buffered (64) channels concurrently [VERIFIED: `pkg/hubble/pipeline.go:250-273, 276-295`] — there is no cross-writer synchronization guaranteeing both files land atomically together on disk. Both writers are individually torn-read-safe (temp+rename), but the *pair's joint existence* is not atomic. During an active capture, a brief window can exist where one file has been written/updated for a given flush cycle and the other hasn't yet.

**How to avoid:** This is exactly the kind of "drift-during-capture" caveat D-15 already mandates a slot for on paginated/live-adjacent tools — extend it to cover `get_evidence`/`get_policy` too: "if this returns not-found for a target `list_policies` just showed you, retry — the pipeline may be mid-flush." Treat it as consistent with D-06's already-accepted "best-effort re-scan, no snapshot" model, not as a new failure class needing new machinery.

**Phase to address:** description-writing now; no code change needed beyond honest error text (D-16's already-mandated actionable-text discipline covers this).

---

### Pitfall 6: unbounded results and schema mistakes (inherited from milestone research, reaffirmed with cpg-specific numbers)

**What goes wrong / why it happens:** Already fully documented in `.planning/research/PITFALLS.md` Pitfall 4 (Claude Code's ~25,000-token default MCP output cap) and Pitfall 6 (schema design mistakes: raw `DropReason` enum, root-level schema unions). Re-verified here with concrete cpg sizing: `MergeCaps{MaxSamples: 10, MaxSessions: 10}` [VERIFIED: `pkg/session/pipeline_config.go:92`] bounds each `RuleEvidence` to at most 10 `FlowSample` entries — a rule record (fixed fields + up to 10 compact samples + optional `L7Ref`) is on the order of a few hundred bytes to ~1-2KB as JSON. D-07's suggested defaults (~50 flows / ~20 evidence rules per page) land comfortably under the 25k-token ceiling even accounting for JSON's token density — this is a reasonable order of magnitude, not just an arbitrary guess, though it has not been measured against a real capture session (flagged in Assumptions Log below).

**How to avoid:** D-07's bounds already cover this; no new mitigation needed beyond implementing pagination in the *first* version of `list_dropped_flows`/`get_evidence`, per the milestone's own repeated guidance.

**Phase to address:** now, as already planned.

## Code Examples

### `pkg/hubble.ReadClusterHealth` — the D-12 export, mirroring `evidence.Reader.Read`'s schema-version gate

```go
// Source: pattern mirrors pkg/evidence/reader.go:26-44 exactly (same
// SchemaVersion-gate idiom, same os.IsNotExist passthrough for the
// "not yet finalized" case D-13/QRY-04 needs to distinguish).
// New file: pkg/hubble/health_reader.go

// ClusterHealthReport is the exported form of clusterHealthReport
// (health_writer.go) — D-12's typed struct requirement for outputSchema.
type ClusterHealthReport struct {
	SchemaVersion     int            `json:"schema_version"`
	ClassifierVersion string         `json:"classifier_version"`
	Session           HealthSession  `json:"session"`
	Drops             []HealthDropJSON `json:"drops"`
}

type HealthSession struct {
	Started        time.Time `json:"started"`
	Ended          time.Time `json:"ended"`
	FlowsSeen      uint64    `json:"flows_seen"`
	InfraDropTotal uint64    `json:"infra_drops_total"` // NOTE: json tag is "infra_drops_total" (plural), not "infra_drop_total" — matches health_writer.go's existing tag exactly
}

type HealthDropJSON struct {
	Reason      string            `json:"reason"`
	Class       string            `json:"class"`
	Count       uint64            `json:"count"`
	Remediation string            `json:"remediation,omitempty"`
	ByNode      map[string]uint64 `json:"by_node"`
	ByWorkload  map[string]uint64 `json:"by_workload"`
}

// ReadClusterHealth reads and validates cluster-health.json. Returns an
// error wrapping fs.ErrNotExist when the file doesn't exist (QRY-04's
// "capturing" / "zero drops" cases both hit this path — see Pitfall #1
// for why absence must NOT be assumed to mean "crashed").
func ReadClusterHealth(path string) (*ClusterHealthReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading cluster health %s: %w", path, err)
	}
	var report ClusterHealthReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parsing cluster health %s: %w", path, err)
	}
	if report.SchemaVersion != 1 { // matches health_writer.go's SchemaVersion: 1 literal
		return nil, fmt.Errorf("unsupported cluster-health schema_version %d in %s (this cpg understands 1)",
			report.SchemaVersion, path)
	}
	return &report, nil
}
```

### Flattened sample record for `list_dropped_flows`'s samples half (field names are Claude's Discretion — shape is not)

```go
// A raw evidence.FlowSample carries no namespace/workload/direction of its
// own — that context lives on the PARENT RuleEvidence/PolicyEvidence. The
// composed view (D-01) needs a flattened, enriched record:
type DroppedFlowSample struct {
	Namespace string             `json:"namespace"`
	Workload  string             `json:"workload"`
	Direction string             `json:"direction"` // from the parent RuleEvidence.Direction
	Peer      evidence.PeerRef   `json:"peer"`
	Port      string             `json:"port"`
	Protocol  string             `json:"protocol"`
	Time      time.Time          `json:"time"`
	Src       evidence.FlowEndpoint `json:"src"`
	Dst       evidence.FlowEndpoint `json:"dst"`
	Verdict   string             `json:"verdict"`
	DropReason string            `json:"drop_reason,omitempty"`
}
// Built by: for each evidence file (PolicyEvidence), for each RuleEvidence,
// for each FlowSample in RuleEvidence.Samples — emit one DroppedFlowSample
// carrying the RuleEvidence's Direction/Peer/Port/Protocol context alongside
// the FlowSample's own Time/Src/Dst/Verdict/DropReason fields.
```

## State of the Art

> Framed against CONTEXT.md's own three explicit "researcher: verify" markers — this phase's version of "old assumption vs. current reality."

| CONTEXT.md's Open Question | What Was Uncertain | Confirmed Answer |
|---|---|---|
| D-13: does `finalize()` run on crash? | Whether "stopped + file absent" cleanly maps to "crashed" | **No** — `finalize()` runs unconditionally after `g.Wait()`; absence is overwhelmingly "zero infra/transient drops," not "crashed." 3-way branch needed, not 2-way (Pitfall #1) |
| D-14: how does go-sdk express schema enums? | `jsonschema:"enum=..."` tag syntax vs. explicit construction | **Explicit construction only** — the tag has zero constraint-keyword support and actively rejects `WORD=`-shaped tags. `Tool.InputSchema` set to a hand-built/patched `*jsonschema.Schema` bypasses reflection entirely (Pattern 1, full code given) |
| D-09: exact `explainOutput` JSON shape? | Whether the promoted renderer's exact structure was fully known | **Fully confirmed**: `{policy: PolicyRef, sessions: []SessionInfo, matched_rules: []RuleEvidence}`, 2-space-indented `json.NewEncoder`. Clarified: pagination wraps this shape, it doesn't live inside it (Pattern 2) |

**Deprecated/outdated:** nothing in this phase deprecates prior art — it is additive promotion + additive exports only.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Default page sizes (~50 flows / ~20 evidence rows, per D-07's own suggested order of magnitude) stay comfortably under the ~25k-token MCP output cap | Common Pitfalls #6 | LOW — estimated from field-count/size reasoning against `MaxSamples: 10` caps, not measured against a real capture session's actual JSON payload size. If wrong, the fix is a config-only default-value tune, not a redesign — recommend the planner sanity-check payload size against a synthetic large fixture in the Wave 0 test (see Validation Architecture) rather than trusting this estimate blindly |

**If this table is empty:** N/A — one assumption logged above; every other claim in this research was verified via direct source reads at the exact versions/commits in the working tree, not training-data recall or web search.

## Open Questions (RESOLVED)

1. **How should `get_cluster_health`'s "stopped + file absent" branch actually be worded/gated, given Pitfall #1's finding?**
   - What we know: `finalize()` runs unconditionally; absence is overwhelmingly "zero drops," occasionally a genuine crash-with-no-prior-drops, rarely a silent finalize-write failure (no error to cite in that last sub-case) or a transient Stop-bounded-wait race.
   - What's unclear: whether the planner wants a full 3-way branch (recommended) or accepts the simpler 2-way framing D-13 wrote, with the tool description simply noting that "absent" usually means zero drops rather than a crash (cheaper to implement, slightly less precise for the LLM).
   - Recommendation: 3-way branch (Pitfall #1's proposal) — it's a small `if`/`else if`/`else` addition over the 2-way version, and prevents `isError` from firing on the single most common healthy-session outcome.

2. **Should `direction` silently no-op on the aggregates half of `list_dropped_flows`, or should the tool refuse/warn when `direction` is combined with a request that would otherwise include aggregates?**
   - What we know: `cluster-health.json`/`DropEvent` have zero direction data; the samples half has real direction data.
   - What's unclear: whether "silently return all aggregate rows regardless of the direction filter, documented in the description" (recommended, Pitfall #2) or some other UX (e.g., omitting the `aggregates[]` key entirely when `direction` is explicitly set) better serves the LLM's downstream reasoning.
   - Recommendation: silently include, always documented — matches D-02's precedent of using explicit description text/markers over hiding data, and avoids a confusing "sometimes this key exists, sometimes it doesn't" response shape.

3. **Exact wording for the `namespace`/`workload`-in-aggregates lookup when the health writer's `_unknown` sentinel is hit** (e.g., a flow with no resolvable workload label) — does a `workload=foo` filter match `_unknown` entries never, or is `_unknown` itself a valid filterable value?
   - What we know: `health_writer.go`'s `accumulate()` uses the literal string `"_unknown"` for missing node/workload labels [VERIFIED: `pkg/hubble/health_writer.go:68-83`].
   - What's unclear: whether the LLM-facing filter should ever need to explicitly query for `_unknown` rows, or whether they should simply always be included/excluded by default when a workload filter is set.
   - Recommendation: treat `_unknown` as an ordinary string value for filtering purposes (no special-casing) — simplest, most consistent with "the filter matches the literal on-disk key."

## Environment Availability

**Step 2.6: SKIPPED** — this phase has no external dependencies beyond the Go toolchain and already-vendored packages (see Standard Stack). No new CLI tools, runtimes, services, or databases are introduced; every artifact this phase reads was already written to the session tmpdir by Phase 17's pipeline. There is no live-cluster, live-Hubble-Relay, or live-K8s-API interaction anywhere in this phase's scope (Architectural Responsibility Map, above, states this explicitly as a deliberate absence).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` package + `github.com/stretchr/testify` v1.11.1 [VERIFIED: go.mod] |
| Config file | none — plain `go test`, driven by `Makefile` |
| Quick run command | `go test ./cmd/cpg/... ./pkg/explain/... ./pkg/output/... ./pkg/hubble/... -race -count=1` |
| Full suite command | `make test` (→ `go test ./... -count=1 -race`) [VERIFIED: `Makefile`] |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| QRY-01 | composed view assembly, filters (namespace/workload/dropclass/direction), pagination | unit + in-memory MCP | `go test ./cmd/cpg/... -run TestMCPQueryListDroppedFlows -race` | ❌ Wave 0 |
| QRY-01 | `direction` filter is samples-only (Pitfall #2) — must not silently break aggregates | unit | `go test ./cmd/cpg/... -run TestMCPQueryListDroppedFlows_DirectionFilterAggregatesUnaffected -race` | ❌ Wave 0 |
| QRY-02 | `list_policies`/`get_policy` consistent during active writes | unit + `-race` concurrent-writer test | `go test ./cmd/cpg/... -run TestMCPQueryListPolicies -race` | ❌ Wave 0 |
| QRY-02 | new exported CNP-parse helper | unit | `go test ./pkg/output/... -run TestReadPolicyFile -race` | ❌ Wave 0 |
| QRY-03 | `get_evidence` output shape identical (per-record) to `cpg explain --output json` | unit (parity test against existing explain fixtures) | `go test ./pkg/explain/... -run TestRenderJSON -race` (existing `explain_render_test.go` behavior re-run post-promotion, unchanged) | ✅ existing (`cmd/cpg/explain_test.go` — to be moved/adapted) |
| QRY-03 | `get_evidence` pagination over matched rules | unit | `go test ./cmd/cpg/... -run TestMCPQueryGetEvidence_Pagination -race` | ❌ Wave 0 |
| QRY-04 | `finalize()` runs on pipeline error, writes cluster-health.json when drops were accumulated (Pitfall #1 — currently untested combination) | unit (pipeline-level, `EvidenceEnabled: true` + `errStreamSource` + at least one Infra-classified flow before the error) | `go test ./pkg/hubble/... -run TestRunPipeline_FinalizesHealthOnStreamError -race` | ❌ Wave 0 — **this is the load-bearing gap this research found; existing `TestRunPipeline_SurfacesStreamError` uses `EvidenceEnabled: false` (unset) so `hw` is nil and never exercises this path** |
| QRY-04 | `get_cluster_health` 3-way branch (capturing / stopped-present / stopped-absent-clean / stopped-absent-crashed) | unit + in-memory MCP | `go test ./cmd/cpg/... -run TestMCPQueryGetClusterHealth -race` | ❌ Wave 0 |
| QRY-05 | every query tool: `structuredContent`+`outputSchema` present, annotations truthful, dropclass enum present in schema | in-memory MCP (extends `TestMCPSessionToolsListed`'s pattern) | `go test ./cmd/cpg/... -run TestMCPQueryToolsListed -race` | ❌ Wave 0 |
| QRY-05 | actionable `isError` texts (unknown session reuses SESS-06 text, policy-not-found suggests `list_policies`, invalid cursor per D-05) | in-memory MCP | `go test ./cmd/cpg/... -run TestMCPQueryToolsErrorTexts -race` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./cmd/cpg/... ./pkg/explain/... ./pkg/output/... ./pkg/hubble/... -race -count=1`
- **Per wave merge:** `make test` (full suite, `-race`, all ~490+ existing tests plus this phase's additions)
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `pkg/hubble/pipeline_test.go` — `TestRunPipeline_FinalizesHealthOnStreamError`: proves `hw.finalize()` writes `cluster-health.json` even when `RunPipelineWithSource` returns a non-nil error, given `EvidenceEnabled: true` and at least one accumulated Infra/Transient drop before the stream error fires. **This directly closes the gap this research identified in Pitfall #1** — the existing `TestRunPipeline_SurfacesStreamError` does not exercise this combination.
- [ ] `pkg/hubble/health_reader_test.go` — new file for `ReadClusterHealth`: missing file (`fs.ErrNotExist`), malformed JSON, wrong `SchemaVersion`, happy path against a fixture matching `health_writer.go`'s exact write format.
- [ ] `pkg/output/writer_test.go` (or a new sibling) — new exported CNP-parse helper: happy path, missing file, malformed YAML.
- [ ] `pkg/explain/` — new package test files, adapted from the existing `cmd/cpg/explain_filter_test.go`/`explain_test.go` (re-run unchanged per D-09's "mechanical move" framing — this is the `pkg/flowsource` v1.1 promotion precedent, already proven safe once).
- [ ] `cmd/cpg/mcp_query_tools_test.go` (new) — extends the existing `startInMemoryMCPSession` harness (`cmd/cpg/mcp_harness_test.go`) with the 5 new tool scenarios; covers the QRY-01..05 rows above.
- [ ] Framework install: none — `testify`/`-race`/in-memory-transport harness are all already present and working.

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | Structural readonly composition root (SEC-01, decided Phase 16, audited Phase 19) — this phase's 5 new handlers must reach only `pkg/session.Manager.Status`, `pkg/evidence.Reader`, `pkg/output` readers, `pkg/explain`, `pkg/hubble.ReadClusterHealth` — never any `pkg/k8s` write-adjacent helper, never a filesystem write outside the session tmpdir |
| V5 Input Validation | yes | Enum-constrained fields (`dropclass`, `direction`) validated by go-sdk's own `resolved.Validate` before the handler runs (Pattern 1); `session_id` opaque-string validation already exists (`Manager.Status`'s SESS-06 path); cursor tokens must be defensively decoded (invalid/malformed → `isError`, never a panic) |
| V6 Cryptography | no | The pagination cursor is an opaque encoding for API hygiene, not a security boundary — it carries no secret and needs no signing/HMAC. Do not over-engineer this into a security control; it only needs to survive round-tripping and fail closed (reject, don't crash) on tampering |
| V12 File and Resources | yes | **Concrete, actionable finding**: `namespace`/`workload` arguments supplied by the LLM feed directly into filesystem path construction (`get_policy`, `get_evidence`, the samples half of `list_dropped_flows`). `pkg/evidence.ValidatePolicyRef`/`validatePathComponent` [VERIFIED: `pkg/evidence/paths.go:45-68`] already exists and is exported specifically to guard exactly this construction on the *write* side (`pkg/output/writer.go:36`); Phase 18 must apply the same guard symmetrically on the *read* side before any `filepath.Join` — reuse `evidence.ValidatePolicyRef`, do not write a second path-traversal check |

### Known Threat Patterns for cpg's Query Tools

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Path traversal via `namespace`/`workload` args (e.g. `"../../etc"`) reaching `filepath.Join` for `get_policy`/`get_evidence` | Tampering / Information Disclosure | Reuse `evidence.ValidatePolicyRef(namespace, workload)` before every path construction (already exported, already tested, already the write-side standard) |
| Malformed/adversarial `cursor` argument (invalid base64, or a decoded value indexing out of range) | Denial of Service (handler panic → server-wide crash, since Go panics in any goroutine kill the whole `cpg mcp` process) | Defensive decode: check every error return, never index without a bounds check; invalid cursor → `isError` per D-05, never a panic |
| Oversized filter combinations forcing a full directory walk on every call | Denial of Service (resource exhaustion) | Already bounded by the existing FIFO evidence caps (`MaxSamples`/`MaxSessions`) and cluster-size-bound file counts (milestone ARCHITECTURE.md's Scaling Considerations table) — no new mitigation needed for cpg's stated single-user interactive-session scale |
| `HTTPPath`/label values (potentially containing tokens/session IDs embedded in URLs) reaching the LLM's context via `get_evidence`/`list_dropped_flows` | Information Disclosure | **Inherited, not introduced, by this phase** — milestone PITFALLS.md Pitfall 9 already flagged this and deferred a redaction pass to v2 (REDACT-01). Phase 18 is the first phase that actually *ships* this data to an LLM tool result — worth reiterating for continuity that the "ship documented risk" decision becomes concretely live here, even though the redaction work itself stays out of scope |

## Sources

### Primary (HIGH confidence — direct reads of cpg's own source at the current working-tree state)
- `pkg/hubble/pipeline.go` (full file) — errgroup wiring, `hw.finalize()` call-site unconditionality (Pitfall #1)
- `pkg/hubble/health_writer.go` (full file) — `finalize()`'s no-op conditions, unexported types D-12 exports
- `pkg/hubble/aggregator.go` (targeted reads: `DropEvent`, `buildDropEvent`, `policyTargetEndpoint`, `Aggregator.Run`'s classification-gate switch) — Pitfalls #2/#3, Pattern 3
- `pkg/session/manager.go`, `session.go`, `pipeline_config.go` (full files) — D-08's `Manager.Status` shape, `EvidenceEnabled: true` always-on in MCP mode, WR-01's autonomous-crash transition
- `cmd/cpg/explain_render.go`, `explain_filter.go`, `explain_target.go`, `explain.go` (full files) — D-09's exact `explainOutput` shape, filter logic, CLI-only target resolution
- `cmd/cpg/mcp_tools.go`, `mcp.go` (full files) — the exact `mcp.AddTool` registration pattern Phase 18 extends, existing tag style confirming zero prior `enum`-tag usage
- `pkg/evidence/schema.go`, `reader.go`, `paths.go` (full files) — `RuleEvidence.Direction`, `IsNotExist` helper, `ValidatePolicyRef`
- `pkg/output/writer.go` (full file) — atomic write confirmation (SEC-02), `ReadExisting`/`readExistingPolicy` split
- `pkg/dropclass/classifier.go` (full file) — `DropClass.String()`'s 5 values, single-source-of-truth doc comment
- `pkg/policy/builder.go` (targeted reads) — CNP `Spec.Ingress`/`Spec.Egress`/`ObjectMeta.Name`/`PolicyName` for `list_policies` metadata rows
- `cmd/cpg/mcp_harness_test.go`, `mcp_session_test.go` (full files) — the in-memory transport test harness to extend
- `pkg/hubble/pipeline_test.go` (targeted read) — confirmed `TestRunPipeline_SurfacesStreamError` does NOT exercise the `EvidenceEnabled: true` + crash combination (the Wave 0 gap)
- `pkg/session/manager_test.go` (targeted read) — confirmed `TestManager_PipelineErrorAutonomouslyStopsSession` swaps out the entire `runPipeline` function, so it never exercises the real `pkg/hubble` finalize path either
- `go.mod`, `Makefile` — dependency versions, test commands
- `.planning/config.json` — `nyquist_validation: true`, `security_enforcement` absent (both sections included per protocol)

### Primary (HIGH confidence — direct reads of vendored library source at cpg's exact pinned versions)
- `github.com/google/jsonschema-go@v0.4.3/jsonschema/infer.go` (full file) — struct-tag inference treats `jsonschema:"..."` as pure description, explicitly rejects `WORD=`-prefixed tags (D-14's definitive answer)
- `github.com/google/jsonschema-go@v0.4.3/jsonschema/schema.go` (targeted reads) — `Schema.Enum []any` field, `Ptr[T any]` helper
- `github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/server.go` (targeted reads: `AddTool`, `toolForErr`, `setSchema`) — confirms an explicitly-provided `Tool.InputSchema` bypasses reflection-based inference entirely
- `github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/protocol.go` (targeted reads) — `ToolAnnotations` struct, confirming `OpenWorldHint`/`DestructiveHint` are `*bool` while `ReadOnlyHint`/`IdempotentHint` are plain `bool`
- `github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/tool.go` (full file) — `ToolHandlerFor` contract, `applySchema` validation flow

### Secondary (inherited from the v1.5 milestone research pass, MEDIUM confidence per that research's own rating — not re-derived here)
- `.planning/research/SUMMARY.md`, `FEATURES.md`, `ARCHITECTURE.md`, `PITFALLS.md` — Tension 2/3 (composed-view scoping, finalize-only health), Pattern 3 (`pkg/explain` promotion precedent from `pkg/flowsource`), Pitfalls 4/5/6/9 (unbounded results, torn reads, schema mistakes, secrets-via-LLM) — all cited inline above rather than re-verified independently, since they were already HIGH/MEDIUM-rated by that research pass and nothing in this phase's scope contradicts them

### Tertiary (LOW confidence)
None load-bearing — this research required no web search; every claim traces to a direct, dated codebase or vendored-source read at the current working-tree/pinned-version state.

## Metadata

**Confidence breakdown:**
- D-13 (health finalize on crash): HIGH — direct, unambiguous control-flow read; only the "how common is each sub-case in practice" framing needed correcting, and that correction is itself grounded in the same source read
- D-14 (go-sdk enum mechanism): HIGH — direct read of both libraries at their exact pinned versions, cross-confirmed at two call sites (`infer.go`'s tag rejection AND `server.go`'s "explicit schema bypasses inference" logic)
- D-09 (explain JSON shape): HIGH — direct read of the exact struct and encoder call
- Direction-filter gap (bonus finding): HIGH — direct read of both `DropEvent` and `healthDropJSON` struct definitions, neither has a direction field
- Namespace/workload cross-half consistency (bonus finding): HIGH — grounded in an explicit in-source comment naming the shared helper
- Pagination sizing (Assumption A1): MEDIUM — reasoned from confirmed cap values (`MaxSamples: 10`), not measured against real payload data

**Research date:** 2026-07-21
**Valid until:** Stable for the remaining life of this milestone (v1.5) — all findings are grounded in already-committed code and already-pinned dependency versions that will not change during Phase 18/19's execution. Re-verify only if `go.mod`'s `go-sdk`/`jsonschema-go` versions are bumped before Phase 18 lands.
