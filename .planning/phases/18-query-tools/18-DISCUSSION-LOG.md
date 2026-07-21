# Phase 18: Query Tools - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-21
**Phase:** 18-query-tools
**Areas discussed:** Composed view & mid-capture availability, Pagination mechanics, Read-side foundations, QRY-05 contract discipline
**Mode:** Fully autonomous per user instruction ("En full autonomie, pour chaque question, tu prends les recommandations de fable5") — recommended option auto-selected for every question, no AskUserQuestion.

---

## Composed view & mid-capture availability (QRY-01 × QRY-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Two explicit sections | `samples[]` (evidence FlowSamples) + `aggregates[]` (infra/transient counts) — different fidelities, honest schema | ✓ |
| Unified pseudo-records | Flatten aggregates into fake flow records for one homogeneous list | |
| Samples-only + header counts | Drop the aggregates half into a summary header | |

**Auto-selected:** Two explicit sections (D-01). Rationale: Tension 2's overpromise warning — synthesizing flow records from counters fakes fidelity the data doesn't have.

| Option | Description | Selected |
|--------|-------------|----------|
| Samples live / aggregates after stop | Serve evidence samples during capture; aggregates return the QRY-04-style non-error marker until stop | ✓ |
| Whole tool after stop only | Simplest, but hides live evidence that already exists on disk | |
| In-memory health hook | Live aggregates via pipeline hook — that's LIVE-01, deferred v2 | |

**Auto-selected:** Samples live / aggregates after stop (D-02). Filters: minimal set namespace/workload/dropclass/direction, no `since` (D-03). `total_count` = view items, not true flow totals (D-04).

---

## Pagination mechanics

| Option | Description | Selected |
|--------|-------------|----------|
| Opaque base64 cursor | Position in deterministic sort (ns, workload, index); forward-compatible, SEP-style | ✓ |
| Plain numeric offset | Simpler but leaks internals and invites arithmetic clients | |
| Page numbers | Coarsest; awkward with drifting file sets | |

**Auto-selected:** Opaque base64 cursor (D-05); invalid cursor → actionable isError.

| Option | Description | Selected |
|--------|-------------|----------|
| Best-effort re-scan per call | Documented drift during capture; no server state | ✓ |
| Snapshot pinning per cursor | Stable pages but server-side state + invalidation complexity | |

**Auto-selected:** Best-effort re-scan (D-06). Pagination scope: only list_dropped_flows + get_evidence; list_policies unpaginated; limits = planner's pick under the 25k-token cap (D-07).

---

## Read-side foundations

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse Manager.Status(id) | TmpDir/State/Error + SESS-06 semantics already exposed; zero new API | ✓ |
| New Manager.Lookup API | Dedicated read accessor — new surface without new information | |

**Auto-selected:** Reuse Status (D-08); outputHash re-derived deterministically.

| Option | Description | Selected |
|--------|-------------|----------|
| Promote filter+render+types | `pkg/explain` gets explainFilter, renderers, explainOutput; cobra+target stay in cmd | ✓ |
| Promote everything incl. target parsing | MCP never parses `NS/workload` strings — dead weight in a package | |

**Auto-selected:** Filter+render+types (D-09). get_evidence requires namespace+workload with CLI-mirror optional filters, discovery via list_policies (D-10). get_policy keyed by namespace+workload = the on-disk key; CNP metadata.name returned in response (D-11). Cluster-health: export types + `ReadClusterHealth`, typed for outputSchema — raw passthrough rejected (D-12); crash-before-finalize branch = isError citing pipeline error (D-13, researcher verifies finalize-on-crash).

---

## QRY-05 contract discipline

| Option | Description | Selected |
|--------|-------------|----------|
| String enum from dropclass String() | `policy\|infra\|transient\|noise\|unknown`, single source of truth | ✓ |
| Expose protobuf DropReason enum | 76-value protobuf enum in a tool schema — Pitfall 6 violation | |

**Auto-selected:** String enum (D-14). Descriptions: 3 mandatory elements — returns / taxonomy lesson / tool caveat (D-15). Annotations: ReadOnlyHint+IdempotentHint true, OpenWorldHint false; Go-error → isError pattern from Phase 17 (D-16). Result delivery = typed structs like session tools, no hand-crafted dual preview in v1.5 (D-17).

---

## Claude's Discretion

- Exact JSON field names (snake_case, consistent with session tool results)
- Exact default/max limit values within D-07's bound
- Exact description prose (D-15 elements mandatory)
- File layout (`cmd/cpg/mcp_query_tools.go`, `pkg/explain` split)
- Cursor token encoding details

## Deferred Ideas

None new — FLOW-01, LIVE-01, REDACT-01 already tracked as v2 requirements.
