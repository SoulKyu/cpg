# Phase 18: Query Tools - Context

**Gathered:** 2026-07-21
**Status:** Ready for planning

<domain>
## Phase Boundary

An LLM can read a session's dropped flows, generated policies, per-rule evidence, and cluster health as safe, well-described, paginated MCP tool results — 5 new readonly tools (`list_dropped_flows`, `list_policies`, `get_policy`, `get_evidence`, `get_cluster_health`) registered in `cmd/cpg`'s composition root alongside the Phase 17 session tools, plus the read-side foundations folded into this phase: `pkg/explain` promotion out of `cmd/cpg`, exported cluster-health reader on `pkg/hubble`, and session resolution via the existing `Manager.Status`. All tools are pure filesystem readers over the session tmpdir (`policies/` + `evidence/` + `cluster-health.json`) — no pipeline change, no new writer. Requirements: QRY-01..05.

**Pre-decided upstream (do not re-litigate):** composed-view scoping for `list_dropped_flows` (REQUIREMENTS QRY-01 / research Tension 2 — no new flow-sample writer, FLOW-01 is v2); pagination contract `limit`/`cursor`/`total_count`/`has_more` (QRY-01); `get_cluster_health` returns a non-error "available after stop_session" result while capturing (QRY-04 / Tension 3 — no live counters, LIVE-01 is v2); retained-stopped-session queryability (Phase 17 D-01/D-02); structural readonly at the composition root (Phase 16, verified Phase 19); plain absolute tmpdir paths, never MCP resources (REQUIREMENTS Out of Scope).

</domain>

<decisions>
## Implementation Decisions

### Composed view & mid-capture availability (QRY-01 × QRY-04)
- **D-01:** `list_dropped_flows` response = two explicit sections: `samples[]` (per-flow records from the capped evidence FlowSamples — policy-actionable drops by construction) and `aggregates[]` (per-reason infra/transient counts). Never synthesize pseudo-flow records from aggregate counters — the two halves have different fidelities and the schema says so.
- **D-02:** Mid-capture split: the samples half is served live (evidence files are atomic on disk during capture); the aggregates half returns an explicit `available_after_stop`-style marker while state=capturing (cluster-health.json is finalize-only; no in-memory pipeline hook — that's LIVE-01, v2). Exact mirror of QRY-04's non-error semantics. After stop, the full composed view reads cluster-health.json.
- **D-03:** Filter surface: `namespace`, `workload`, `dropclass` (enum), `direction` (ingress|egress) — all optional, AND-combined. No `since`/time-range filter in v1.5: misleading on a FIFO-capped sampled view.
- **D-04:** `total_count` counts items in the filtered *view* (samples + aggregate rows), never true flow totals — true totals stay in `get_status`/`stop_session` (`flows_seen`). The tool description carries REQUIREMENTS' wording verbatim: "a sampled/aggregated view, not a raw flow log".

### Pagination mechanics
- **D-05:** Cursor = opaque base64 token encoding the position in a deterministic sort order (namespace, workload, stable index). Invalid or stale cursor → `isError` with actionable text ("invalid cursor; retry without cursor to restart from the first page").
- **D-06:** Consistency model: best-effort re-scan per call. The file set may shift between pages during an active capture — documented in the tool description, never an error, no server-side snapshot state (single-session server; pinning is complexity without payoff).
- **D-07:** Pagination applies to `list_dropped_flows` and `get_evidence` only (the "many records" tools per REQUIREMENTS/FEATURES). `list_policies` returns all metadata unpaginated (cheap rows, realistic cardinality in the dozens); `get_policy`/`get_cluster_health` are single-object. Default/max `limit` values: planner's pick, bounded well under the 25k-token MCP output cap (order of magnitude: default ~50 / max ~200 for flows; default ~20 for evidence rules).

### Read-side foundations
- **D-08:** Query tools resolve a session via the existing `Manager.Status(id)` verbatim — it already returns `TmpDir`/`State`/`Error` with SESS-06 unknown/purged semantics. No new Manager API. `outputHash` for evidence paths is re-derived with `evidence.HashOutputDir(filepath.Join(tmpDir, "policies"))` — deterministic, matches `buildPipelineConfig`.
- **D-09:** `pkg/explain` promotion scope: the filter (`explainFilter.match`) + the three renderers (`renderJSON`/`renderText`/`renderYAML`) + the `explainOutput` type move to `pkg/explain` with exported names; cobra wiring and target parsing (`explain_target.go`) stay in `cmd/cpg`. Existing explain test suite re-runs unchanged (the `pkg/flowsource` v1.1 promotion precedent). "Identical to `cpg explain --output json`" (QRY-03) = the same renderer/struct produces the payload, not a re-implementation. Package is never named `pkg/mcp`.
- **D-10:** `get_evidence` argument surface: `namespace` + `workload` required (the evidence Reader's unit), optional filters mirroring the CLI explain flags (`direction`, `peer`, `port`, `protocol`, `http_method`, `http_path`, `dns_pattern`). Target discovery happens via `list_policies` (policies ↔ evidence are 1:1 by (namespace, workload)). No enumerate-all-evidence mode in v1.5. Pagination unit = matched `RuleEvidence` entries.
- **D-11:** `get_policy` identifies a policy by `namespace` + `workload` — the canonical on-disk key (`<tmpdir>/policies/<ns>/<workload>.yaml`). The roadmap criterion's "name" is interpreted as this key; the CNP `metadata.name` is returned in the response. `list_policies` rows: namespace, workload, CNP name, direction(s), rule counts, absolute path.
- **D-12:** Cluster-health reader: export the report types (`clusterHealthReport` → `ClusterHealthReport`, `healthDropJSON` → exported) and add `pkg/hubble.ReadClusterHealth(path)`. Passthrough semantics — no value transformation, per-reason Cilium remediation URLs intact. Typed structs are required to satisfy QRY-05's `outputSchema`; a raw `json.RawMessage` passthrough is rejected for exactly that reason.
- **D-13:** `get_cluster_health` branches: state=capturing → non-error `available_after_stop` result (QRY-04, locked); state=stopped + file present → passthrough; state=stopped + file absent (crash before finalize) → `isError` citing the pipeline error from `StatusResult.Error`. *(Researcher: verify whether the health writer's finalize runs on a pipeline crash — determines how often the third branch fires.)*

### QRY-05 contract discipline
- **D-14:** `dropclass` in schemas = string enum `policy|infra|transient|noise|unknown` — single source of truth is `pkg/dropclass.DropClass.String()`. Never expose the protobuf `DropReason` enum as a schema type; reason names remain informative strings alongside the class. *(Researcher: confirm how go-sdk expresses enums — jsonschema struct tag vs. explicit schema.)*
- **D-15:** Every query-tool description carries 3 mandatory elements: (1) what it returns, (2) a 1–2 sentence dropclass taxonomy lesson (policy-actionable vs infra/transient — the classifier exists to stop the LLM from proposing policies for infra noise), (3) the tool-specific caveat (sampled view for QRY-01, available-after-stop for QRY-04/aggregates, drift-during-capture for paginated tools). Exact prose = planner.
- **D-16:** Annotations on all 4 query tools: `ReadOnlyHint: true`, `IdempotentHint: true`, `OpenWorldHint: false`. Error handling = return the Go error and let the SDK convert to `isError` (Phase 17 pattern, never hand-construct the result); texts specific and actionable — unknown session reuses the shipped SESS-06 message, policy-not-found suggests calling `list_policies`, invalid cursor per D-05.
- **D-17:** Result delivery matches the Phase 17 session tools: typed structs via `mcp.AddTool` (SDK infers `outputSchema` and emits JSON content). `get_policy` structuredContent: namespace, workload, CNP name, full YAML string, absolute path, rule counts. No hand-crafted dual preview/content blocks in v1.5 — consistency with the session tools wins.

### Claude's Discretion
- Exact JSON field names (snake_case, consistent with `StartResult`/`StatusResult`/`StopResult`).
- Exact default/max `limit` values within D-07's bound.
- Exact description prose (D-15's three elements are mandatory).
- File layout: `cmd/cpg/mcp_query_tools.go` (or similar) and `pkg/explain` file split.
- Cursor token encoding details (opaque per D-05).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` §Query Tools — QRY-01..05 exact wording; §Out of Scope (no resources, no apply tool, no multi-session); §v2 (FLOW-01/LIVE-01/REDACT-01 deliberately deferred)
- `.planning/ROADMAP.md` — Phase 18 goal + 5 success criteria; Phase 19 depends on this phase's tool table

### Milestone research (v1.5 MCP)
- `.planning/research/SUMMARY.md` — Tension 2 (composed-view scoping, binding here), Tension 3 (finalize-only health → QRY-04 shape), research "Phase 3+4" mapping onto this phase
- `.planning/research/FEATURES.md` — tool-contract table stakes (pagination, structuredContent+outputSchema, annotations, taxonomy-teaching descriptions), list/get split convention, canonical dropclass classification table
- `.planning/research/ARCHITECTURE.md` — Pattern 3 (`pkg/explain` promotion, flowsource precedent), query readers as pure tmpdir readers, no `pkg/mcp` naming rule
- `.planning/research/PITFALLS.md` — Pitfall 4 (unbounded results / 25k-token cap), Pitfall 5 (torn reads — fixed at source in Phase 16, read-side retry as defense-in-depth), Pitfall 6 (schema mistakes: enum DropClass, no root-level unions)

### Prior phase contracts
- `.planning/phases/17-session-lifecycle/17-CONTEXT.md` — D-01/D-02 retention model (stopped session queryable cold — the binding interpretation QRY-04 relies on), D-04 purge-on-new-start, D-10 session_id formats
- `.planning/phases/16-mcp-server-foundation-write-safety/16-CONTEXT.md` — stdout discipline (`mcpModeStdout()`), composition-root readonly rule, atomic policy writer (SEC-02) that makes tmpdir reads torn-safe

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `cmd/cpg/mcp_tools.go` `registerSessionTools` — the exact registration pattern to extend: typed `mcp.AddTool`, package-main validation before `pkg/session`, Go error → SDK `isError` auto-conversion
- `pkg/session/manager.go:333` `Manager.Status(id)` — the session resolver query tools reuse (TmpDir/State/Error + SESS-06 semantics), zero new Manager surface (D-08)
- `pkg/session/pipeline_config.go:73-75` — tmpdir layout contract: `policies/`, `evidence/`, `outputHash = evidence.HashOutputDir(outputDir)`
- `pkg/output/writer.go:39-44,116` — policy file layout `<outputDir>/<ns>/<workload>.yaml`, atomic temp+rename since Phase 16 (SEC-02)
- `pkg/evidence/reader.go` `Reader.Read(namespace, workload)` + `pkg/evidence/paths.go` `ResolvePolicyPath`/`ValidatePolicyRef` — the per-target evidence read path (validation included)
- `pkg/evidence/schema.go` — `PolicyEvidence`/`RuleEvidence`/`FlowSample`: the raw material of QRY-01 samples and QRY-03
- `cmd/cpg/explain_render.go` `renderJSON` + `explainOutput`; `cmd/cpg/explain_filter.go` `explainFilter` — the code that promotes to `pkg/explain` (D-09)
- `pkg/hubble/health_writer.go:239-260` `clusterHealthReport`/`healthDropJSON` (unexported today) + `finalize()` — the types D-12 exports
- `pkg/dropclass` `DropClass.String()` — canonical enum labels for D-14
- `cmd/cpg/mcp_harness_test.go` + `mcp_session_test.go` — in-memory-transport harness to extend with query-tool scenarios

### Established Patterns
- All three writers (policy/evidence/health) are atomic temp+rename — per-file reads are torn-safe; only the *set* of files drifts during capture (D-06)
- 539 tests all run `-race`; new query-tool tests follow suit on the in-memory harness (no real cluster)
- Session tools already model result structs with snake_case JSON + jsonschema tags — query results stay consistent

### Integration Points
- `cmd/cpg/mcp.go` `runMCPServer` — where `registerQueryTools(server, mgr)` (or equivalent) is called next to `registerSessionTools`; composition-root readonly discipline continues (read-path handlers only — Phase 19 audits this)
- `pkg/explain` (new) — imported by both `cmd/cpg` explain command and the `get_evidence` handler
- `pkg/hubble.ReadClusterHealth` (new export) — used by `get_cluster_health` and reusable by Phase 19's e2e test

</code_context>

<specifics>
## Specific Ideas

- The "session not found or expired" error text already shipped in `pkg/session` — query tools surface it verbatim via `Manager.Status`, no new wording.
- QRY-01's description must include REQUIREMENTS' exact phrase: "a sampled/aggregated view, not a raw flow log".
- The `available_after_stop` non-error shape is designed once and shared between `get_cluster_health` (QRY-04) and `list_dropped_flows`' aggregates half (D-02) — same field, same semantics.

</specifics>

<deferred>
## Deferred Ideas

None new — FLOW-01 (dedicated flow-sample writer), LIVE-01 (live mid-session counters), and REDACT-01 (HTTPPath redaction) were already tracked as v2 requirements before this discussion and stay there.

</deferred>

---

*Phase: 18-Query Tools*
*Context gathered: 2026-07-21*
