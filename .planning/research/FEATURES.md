# Feature Research

**Domain:** MCP (Model Context Protocol) tool surface for a readonly Kubernetes/Cilium network-policy observability CLI (`cpg mcp`)
**Researched:** 2026-07-20
**Confidence:** HIGH (core MCP spec claims verified via Context7 + official modelcontextprotocol.io spec pages incl. the 2025-11-25 revision and the accepted SEP-2567; ecosystem-adoption claims and Claude Code specifics MEDIUM — WebSearch-sourced, cross-checked against at least one primary/official source each)

**Scope note:** This file researches ONLY the new v1.5 MCP feature surface (`cpg mcp` subcommand, session tools, query tools). It does not re-research already-shipped `cpg generate`/`replay`/`explain` functionality — those are treated as existing capabilities the MCP layer wraps.

## Feature Landscape

### Table Stakes (Users Expect These)

These are the baseline conventions every credible infra/observability MCP server (AWS CloudWatch, GitHub, Grafana) already follows. Missing them makes cpg's MCP server feel broken or unsafe to an LLM harness, even if the underlying data is correct.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| snake_case verb_noun tool names, no `cpg_` prefix | Ecosystem convention (GitHub MCP: `get_file_contents`, `list_branches`; AWS design guidelines mandate snake_case). Hosts already namespace by server — Claude Code's Agent SDK exposes tools as `mcp__cpg__start_session`, and the Messages API uses `cpg:start_session`. A redundant `cpg_` prefix in the tool's own `name` field is dead weight the model has to parse twice. | LOW | `start_session`, `get_status`, `stop_session`, `list_dropped_flows`, `list_policies`, `get_policy`, `get_evidence`, `get_cluster_health` |
| Onboarding-level tool descriptions | Anthropic's tool-writing guidance: small description refinements measurably reduce agent error rates (cited SWE-bench improvement). Write descriptions as if explaining to a new teammate — spell out units, defaults, and what "dropped" vs "infra/transient" means. | LOW–MEDIUM | Highest-leverage differentiator is actually embedding cpg's drop-reason taxonomy semantics *in the description text* (see Differentiators) |
| Tool annotations: `readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint` | Spec-defined (`ToolAnnotations`, MCP 2025-11-25 schema). Every tool in this milestone is genuinely read-only — declaring it lets clients skip destructive-op confirmation friction and lets security-conscious hosts allowlist the server automatically. Spec explicitly warns these are hints from possibly-untrusted servers, so they must be *actually true*, not aspirational. | LOW | All 7 tools: `readOnlyHint: true`, `destructiveHint: false`. `start_session` gets `openWorldHint: true` (opens a live Hubble/K8s connection) and `idempotentHint: false` (second call while a session is active should error, not silently succeed). Query tools (`get_status`, `list_*`, `get_*`) get `openWorldHint: false` + `idempotentHint: true` — they only read the session tmpdir. |
| Structured output: `content` (text) + `structuredContent` (JSON) + `outputSchema` | Formalized in the 2025-11-25 spec; the spec **requires** the text block for backward compat even when `structuredContent` is present. AWS design guidelines and Anthropic's tooling guide both push structured, schema-validated responses over prose parsing. | LOW–MEDIUM | cpg's internal types (dropclass results, evidence records, `cluster-health.json`) are already Go structs with JSON tags from v1.1–v1.3 — deriving `outputSchema` is largely mechanical, not new design work. |
| `limit` + `cursor` pagination on any tool that can return many records | MCP's protocol-level cursor pagination (`nextCursor`) **only applies to `tools/list`, `resources/list`, `prompts/list`** — never to `tools/call` results. Any tool returning an unbounded set (flows, evidence samples) must implement its own filtering in the `inputSchema`, following the pattern AWS CloudWatch (`execute_cwl_insights_batch` auto-chunks at 10k records, `| limit N`) and the wider MCP pagination-pattern writeups converge on: explicit `limit`/`cursor` args, response always carries `total_count` (or estimate) and `has_more`. | MEDIUM | Applies to `list_dropped_flows` and `get_evidence` (evidence already has FIFO caps server-side from v1.1 — the tool just needs to expose windowing on top). `list_policies`/`get_cluster_health` are naturally small and don't need it. |
| `isError: true` tool-execution errors with actionable text | Spec draws a hard line: **protocol errors** (unknown tool, malformed args) are standard JSON-RPC errors the model is unlikely to self-correct from; **tool execution errors** (`isError: true` in the result) carry text the model *can* act on ("session `sess_xyz` not found or already stopped — call `start_session` first"). Get this wrong and the LLM either silently ignores failures or retries blindly. | LOW | Concretely: "no active session," "Hubble Relay unreachable," "policy `name` not found in this session," "capture produced zero dropped flows yet" (informational, not necessarily `isError`). |
| Explicit session handle (`session_id`) threaded through every session-scoped tool call | Directly backed by **SEP-2567 "Sessionless MCP via Explicit State Handles"** (Final, accepted 2026). The SEP explicitly calls out stdio servers relying on process-lifetime state as the *most common* anti-pattern today, and says such servers "SHOULD NOT rely on process-lifetime state and SHOULD migrate to explicit handles" — even though a stdio process isn't subject to the load-balancer/sticky-routing problem the SEP is mainly solving. Reasons that still apply to a single-process stdio server: opaque handles produce clear "expired/unknown session" errors instead of ambiguous "no session" states, they survive context compaction (they're plain strings in the transcript), and the design is forward-compatible if cpg ever ships an HTTP transport or multi-session support. | LOW–MEDIUM | `start_session(...)` returns `{"session_id": "sess_a1b2c3", "tmpdir": "...", "started_at": "..."}` in `structuredContent`. `get_status`, `stop_session`, `list_dropped_flows`, `list_policies`, `get_policy`, `get_evidence`, `get_cluster_health` all take `session_id` as a required argument. Per SEP-2567 guidance: keep the handle opaque (no encoded structure), document its lifetime in `start_session`'s description ("session and its tmpdir are destroyed on `stop_session` or server shutdown"), and return a specific "expired" error rather than a generic one. |
| Readonly reality matches readonly annotations | Not a new decision (already an architectural constraint from PROJECT.md), but worth stating as a table-stakes *test surface*: no tool in the MCP server may call any K8s/Cilium mutating API or write outside the session tmpdir. This is what makes the `readOnlyHint` claims trustworthy rather than aspirational. | LOW (verification, not new code) | Enforced by construction: MCP tools are readers over `pkg/output`/`pkg/evidence`/`pkg/dropclass` artifacts already written by the existing `generate` pipeline running headless into the session tmpdir — there is no code path for the MCP layer to reach a K8s write API. |

### Differentiators (Competitive Advantage)

Not required to be "a working MCP server," but this is where cpg's MCP layer earns being noticeably better than a naive wrapper around the existing CLI output.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Dual preview + reference pattern on `list_dropped_flows` / `get_evidence` | Production MCP reporting pipelines that dump full result sets routinely burn 70–80% of context before analysis starts. The documented mitigation (seen in "ResourceLink for large datasets" writeups) is a **preview sample in `content` (a handful of representative flows/samples, human-readable) + full counts and a stable reference in `structuredContent`** so the model can reason immediately without ingesting everything, and fetch more only if it decides it needs to. | MEDIUM | E.g., `list_dropped_flows` text block: "42 dropped flows (18 policy-actionable, 24 infra/transient — see `get_cluster_health`). Showing first 5." Full 42 in `structuredContent.flows` bounded by `limit`, with `has_more`/`next_cursor` for the rest. |
| `list_policies` (cheap metadata) + `get_policy` (full YAML) split | Mirrors AWS CloudWatch's `describe_log_groups` → `analyze_log_group` split and GitHub's list/get pattern. Avoids forcing the model to pull every generated policy's full YAML into context just to see how many exist or pick one to inspect. | LOW–MEDIUM | `list_policies` returns `{name, path, rule_count}[]` only; `get_policy(session_id, name)` returns the YAML as inline text (individual policy files are small — no pagination needed here, unlike flows/evidence). |
| Reuse cpg's existing `explain` JSON renderer verbatim for `get_evidence` | `cpg explain --output json` (shipped v1.1/v1.2, with `--http-method`/`--http-path`/`--dns-pattern` filters shipped v1.2) is already a machine-consumable, battle-tested rendering of per-rule flow evidence. Piping that renderer straight into `structuredContent` means the MCP surface and the CLI surface can never drift apart, and it's close to zero new design work — the format decision was already made and tested. | LOW | This is the single highest reuse-to-value ratio item in the whole milestone. |
| Plain absolute tmpdir paths instead of MCP resources for file-like artifacts | MCP `resources/read` support is inconsistent across hosts today — several writeups converge on "most MCP clients don't support Resources well, if at all," and adoption is a chicken-and-egg problem (servers don't build them because clients don't surface them, and vice versa). Concretely for cpg's stated target harness, Claude Code exposes resources only via manual `@mention` autocomplete (a human action), which doesn't fit a model-driven diagnostic loop. Because cpg's session tmpdir lives on the same filesystem as the harness process (stdio transport, harness spawns the server), returning the **absolute path as a plain string field** lets Claude Code's own `Read`/`Glob` tools open the file directly — zero MCP-resources plumbing, zero client-support risk. | LOW | Return `path` alongside YAML text in `get_policy`, and the evidence/health file paths in their respective tool outputs. This is a pragmatic call specific to a local-stdio, same-filesystem deployment — it would not hold for a remote/HTTP MCP server. |
| Embed remediation URLs directly in `get_cluster_health` output | `cluster-health.json` (shipped v1.3) already carries a Cilium-docs remediation URL per drop reason. Surfacing that verbatim in the tool's `structuredContent` saves the LLM a follow-up web-search round trip mid-diagnosis — free value from an existing artifact. | LOW | Pure passthrough of an existing file. |
| Tool descriptions that teach the dropclass taxonomy inline | cpg's core differentiator (per PROJECT.md) is the drop-reason classifier distinguishing policy-actionable drops from infra/transient noise. If `list_dropped_flows`'s description doesn't explain that distinction, the LLM is liable to propose policies for infra drops (exactly the failure mode the classifier exists to prevent) or ask the user redundant clarifying questions. | LOW | Cheap to write, disproportionately valuable — this is where "even small description refinements yield dramatic improvements" (Anthropic) applies most directly to cpg's actual domain risk. |

### Anti-Features (Commonly Requested, Often Problematic)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|------------------|-------------|
| Mutating/`apply_policy` tool in v1.5 | Natural conversational next step after "review this generated policy" is "just apply it" | Breaks the milestone's explicit readonly guarantee (no cluster mutation); no `cpg apply` CLI command exists yet to wrap in the first place (still "Planned" in PROJECT.md); an LLM-triggered cluster write without a hard human confirmation gate is a real production-safety risk for a default-deny cluster | Defer until `cpg apply` (dry-run-by-default, `--force` to apply) ships as a CLI command; if/when an MCP `apply` tool is added later, gate it behind explicit non-readonly opt-in plus a confirmation step, not a bare tool call |
| Elicitation (form-mode) for collecting session parameters (namespace, duration, filters) mid-call | Feels like better UX than requiring the LLM to have all args upfront | Elicitation is a newer capability (URL mode is brand-new in 2025-11-25) with materially weaker client support than tools themselves; spec requires servers to securely bind elicitation state to user identity, which is disproportionate machinery for a single-user local stdio process; the conversational harness already gathers these parameters from the human before calling `start_session` | Take all session parameters as ordinary `start_session` arguments (namespace, `--all-namespaces` equivalent, duration, `--l7`, `--ignore-drop-reason` equivalent); the LLM asks the user in natural language, same as it does for any other tool call today |
| Sampling (server calls back into the client's LLM for semantic judgment, e.g. "is this drop plausible") | MCP's `sampling/createMessage` exists precisely for "let the server borrow the client's LLM" | This is the same AI-assisted semantic-plausibility idea PROJECT.md already explored and explicitly shelved on 2026-04-25 (hallucination risk on confident-sounding reasoning, signal quality tied to often-poor label hygiene, deterministic blast-radius analysis judged more useful) — MCP sampling would just be the transport for reintroducing it. It also inverts the milestone's own stated design principle: "cpg stays deterministic, LLM brings the intelligence." | Keep cpg's tools purely deterministic; the outer LLM harness (which already has full conversational context) does the semantic reasoning over the structured facts cpg provides |
| MCP prompts (canned slash-command templates, e.g. `/cpg:diagnose`) | Looks like free UX — "give users a one-shot diagnostic macro" | Adoption is thin even among comparable infra MCP servers (CloudWatch, Grafana, GitHub don't lead with prompts); prompts are **user-controlled** by spec design (surfaced as something a human explicitly selects, e.g. a slash command), which fits a manual runbook better than an autonomous conversational diagnosis flow; premature templating risks freezing a workflow before real usage patterns are known | Rely on well-written tool descriptions plus README/CLAUDE.md-level guidance for the harness; revisit only if usage data shows a specific multi-tool sequence gets invoked identically often enough to be worth canning |
| Progress notifications (`notifications/progress`) for the capture session | "Long-running Hubble capture" sounds like the canonical progress-notification use case | Progress notifications correlate to a *single blocking request* via a `progressToken` the client attaches to that request. cpg's chosen architecture (`start_session` returns immediately; a background capture writes to the tmpdir; `get_status` is polled separately) never has a call that blocks for the capture's duration — there's nothing for a progress notification to attach to. Adding the primitive anyway duplicates the status channel and adds protocol plumbing with no UX gain. | `get_status(session_id)` already returns live counters (flows seen, policies generated, elapsed time) on demand — that response *is* the progress signal, polled instead of pushed |
| Unbounded/unfiltered flow or evidence dumps ("just return everything cpg captured") | Simplest possible implementation; "let the model see all the data" | Directly causes the token-budget blowout this research flags repeatedly — Claude Code's own default tool-response ceiling is ~25k tokens (per Anthropic's tool-writing guidance), and a live capture can trivially produce more dropped-flow records than that in a busy cluster | Mandatory `limit`+`cursor` with sane defaults (table stakes item above); this row exists to name the failure mode the pagination requirement is specifically preventing |
| MCP resources as the *primary* (or only) access path for policy YAML / evidence | Resources are the protocol's "designed for this" primitive for file-like, application-driven data | Documented, real adoption gap: several MCP hosts implement `resources/list` but not `resources/read`, or read but not `subscribe`; Claude Code's own resource UX is manual `@mention`, not something the model reaches for autonomously mid-diagnosis | Tools + plain file paths (see Differentiators); resources can be added later as a *secondary* convenience once client support is less patchy, without it being load-bearing |

#### Not Applicable (transport-scoped, not a rejected feature)

- **OAuth 2.1 / MCP Authorization spec** — The MCP authorization specification is explicitly scoped to HTTP transports. The spec states stdio implementations "SHOULD NOT" follow it and should instead retrieve credentials from the environment. `cpg mcp` is stdio-only (the harness spawns the process, per the milestone's own framing) and already gets cluster access the same way the existing CLI does (kubeconfig / env). This isn't a feature that was considered and rejected — the concern simply doesn't arise for this transport. No REQ-ID needed; worth a one-line note in the roadmap so a future reviewer doesn't flag its absence as a gap.

## Feature Dependencies

```
start_session(namespace?, all_namespaces?, duration?, l7?, ignore_drop_reason?)
    └──requires──> pkg/hubble + pkg/flowsource (existing live gRPC connection, auto port-forward)
    └──requires──> pkg/policy + pkg/output (existing generation/writing, redirected to session tmpdir via os.MkdirTemp)
    └──requires──> pkg/evidence (existing schema v2 writer, FIFO caps — reused unchanged)
    └──requires──> pkg/dropclass (existing classifier + cluster-health.json writer — reused unchanged)
    └──produces──> session_id (opaque handle, SEP-2567 pattern)

get_status(session_id) / stop_session(session_id) / list_dropped_flows(session_id) /
list_policies(session_id) / get_policy(session_id, name) / get_evidence(session_id, ...) /
get_cluster_health(session_id)
    └──requires──> start_session having minted session_id (session must exist / not be expired)
    └──requires──> tmpdir artifacts already written by the session's background writers
    └──requires("readers over tmpdir only")──> milestone constraint: query tools never touch the
                    live cluster directly — this is why an "explain vs live cluster diff" tool is
                    NOT proposed here: existing dedup (both local-file and live-cluster, shipped
                    v1.0) already runs upstream during generation, so tmpdir policies are already
                    "new, not duplicate" by construction

get_evidence(session_id, target, http_method?, http_path?, dns_pattern?)
    └──reuses──> existing `cpg explain` JSON renderer (v1.1/v1.2) — output format decision already
                    made and tested; MCP layer does not reinvent it

stop_session(session_id)
    └──requires──> start_session
    └──triggers──> tmpdir cleanup (also triggered at server process shutdown — readonly guarantee
                    depends on both cleanup paths existing, not just the happy path)

Tool annotations (readOnlyHint, etc.) ──enhances──> client trust / auto-approval UX (no protocol
                    dependency — pure metadata)

limit+cursor pagination ──enhances──> list_dropped_flows, get_evidence (prevents the token-budget
                    failure mode named in Anti-Features)

MCP resources / elicitation / sampling / prompts ──conflicts with──> the milestone's own design
                    principle ("cpg stays deterministic, LLM brings the intelligence") and/or the
                    readonly guarantee — this is why each is classified as anti-feature/deferred
                    rather than merely "not yet built"
```

### Dependency Notes

- **All session-scoped tools require `start_session`'s handle:** per SEP-2567, `session_id` is an ordinary string threaded through every subsequent call — there is no protocol-level session concept to lean on instead, even though the transport is stdio (single process). This is a hard prerequisite for every other tool's design, not just an implementation detail.
- **Query tools require the session's tmpdir artifacts, not the live cluster:** this is a milestone-level architectural constraint ("query tools: ... all implemented as readers over the session tmpdir artifacts"), and it is *enabled by* existing dedup already having run during generation — the dependency chain means query-tool correctness is inherited from v1.0's dedup logic, not re-derived.
- **`get_evidence` reuses the existing `explain` renderer:** this is a deliberate low-complexity choice — the alternative (a new bespoke MCP-only evidence format) would fork cpg's output semantics in two places that need to stay in sync forever.
- **Resources/elicitation/sampling/prompts conflict with the milestone's design principle:** all four MCP primitives are technically available but each one either reopens a product decision already made (sampling → shelved AI-plausibility feature) or works against the stated goal of a small, deterministic, readonly tool surface (resources → adoption gap and unneeded indirection; elicitation → statefulness/security overhead for a single-user process; prompts → premature templating). Listing this as a conflict, not just an omission, is meant to keep future milestones from re-litigating each one independently.

## MVP Definition

### Launch With (v1.5)

- [ ] `start_session` / `get_status` / `stop_session` with explicit opaque `session_id` — the load-bearing pattern every other tool depends on
- [ ] `list_dropped_flows` with `limit`/`cursor`/`since`/namespace/dropclass filtering — the tool most likely to blow a token budget if shipped without pagination
- [ ] `list_policies` + `get_policy` (list/get split) — avoids bulk-dumping every generated YAML
- [ ] `get_evidence` reusing the existing `explain` JSON renderer — near-zero-cost, high-consistency reuse
- [ ] `get_cluster_health` as a thin passthrough of the existing `cluster-health.json`
- [ ] Tool annotations (`readOnlyHint`/`destructiveHint`/`idempotentHint`/`openWorldHint`) on all 7 tools, truthfully set
- [ ] `structuredContent` + `outputSchema` on every data-returning tool, text block always included for back-compat
- [ ] `isError`-based tool execution errors with specific, actionable messages (not generic failures)
- [ ] Descriptions written at onboarding depth, explicitly encoding the policy-actionable vs infra/transient distinction

### Add After Validation (v1.5.x)

- [ ] `resource_link` as a *secondary* access path for policy YAML, once real usage shows the plain-path approach is hitting a client-support wall — trigger: a target harness that can't read local files directly
- [ ] `list_sessions` — only useful if/when multi-session-per-process is ever supported; the current single-session-at-a-time design (one background capture, one ephemeral tmpdir) makes it dead weight today

### Future Consideration (v2+)

- [ ] Mutating `apply_policy` tool — defer until the standalone `cpg apply` CLI command exists (still "Planned," not built) and until there's a considered human-confirmation gate design; this is the one place where elicitation might eventually earn its keep ("confirm apply to cluster?")
- [ ] Progress notifications — only worth revisiting if a future tool introduces a genuinely long *blocking* call; the session/status-polling architecture chosen for v1.5 doesn't have one

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|----------------------|----------|
| Explicit `session_id` handle (SEP-2567 pattern) | HIGH | LOW | P1 |
| Tool annotations (readOnly/destructive/idempotent/openWorld) | HIGH | LOW | P1 |
| `structuredContent` + `outputSchema` on query tools | HIGH | MEDIUM | P1 |
| `limit`/`cursor` pagination on flows + evidence | HIGH | MEDIUM | P1 |
| `isError` actionable error reporting | HIGH | LOW | P1 |
| `list_policies`/`get_policy` split | MEDIUM | LOW | P1 |
| Reuse `explain` JSON renderer for `get_evidence` | HIGH | LOW | P1 |
| `get_cluster_health` passthrough | MEDIUM | LOW | P1 |
| Plain file paths instead of MCP resources | MEDIUM | LOW | P1 |
| Domain-teaching tool descriptions (dropclass semantics) | HIGH | LOW | P1 |
| `resource_link` secondary path for policy YAML | LOW | LOW | P3 |
| MCP prompts | LOW | LOW | P3 (defer) |
| Progress notifications | LOW | MEDIUM | P3 (skip for v1.5) |
| Elicitation | LOW | MEDIUM | P3 (skip for v1.5) |
| Sampling | — (anti-feature) | — | Reject |
| Mutating/`apply_policy` tool | — (anti-feature for v1.5) | — | Reject for v1.5 |

**Priority key:**
- P1: Must have for launch
- P2: Should have, add when possible
- P3: Nice to have, future consideration

## Competitor / Reference Analysis

Real-world MCP servers examined for prior art, all in the infra/observability or session-automation space:

| Concern | AWS CloudWatch MCP | GitHub MCP | Playwright / Browserbase MCP | cpg mcp (proposed) |
|---------|--------------------|------------|-------------------------------|---------------------|
| Tool naming | snake_case, verb_noun (`get_metric_data`, `analyze_log_group`) | snake_case, verb_noun (`get_file_contents`, `list_branches`) — every tool maps to exactly one toolset | Short verbs (`start`, `end`, `navigate`, `act`, `observe`, `extract`) | snake_case, verb_noun, no redundant `cpg_` prefix (host auto-namespaces) |
| Long-running work | `execute_log_insights_query` returns a query ID; separate `get_logs_insight_query_results` polls it; `cancel_logs_insight_query` to abort | Mostly synchronous CRUD; IDs (issue/PR numbers) are the natural handles | Session stays open across tool calls; tools operate within it | `start_session` returns `session_id` immediately (background capture); `get_status` polled; `stop_session` to end |
| Pagination | `execute_cwl_insights_batch` auto-chunks at a 10k-record limit; `\| limit N` clause pattern | Cursor-based, pass-through of GitHub API pagination | N/A (not a bulk-data domain) | `limit`+`cursor` args, `total_count`/`has_more` in response |
| Session/state model | Query ID is the de facto explicit handle (predates SEP-2567 but same shape) | Stateless — every call self-contained, resource IDs (issue/PR numbers) act as handles | Explicit `start`/`end` tools bound a session; `--isolated` flag for fresh-per-session state | Explicit opaque `session_id`, single active session per process |
| Read-only posture | Cross-account `profile_name='prod-readonly'` convention; IAM/SCP enforce no mutation | N/A (GitHub MCP is intentionally read/write, gated by scopes) | N/A (browser automation is inherently interactive/mutating) | Every tool `readOnlyHint: true`; **no mutating tool exists at all** in the v1.5 surface (architectural, not a flag) |

## Sources

**Official MCP specification (Context7-resolved `/websites/modelcontextprotocol_io_specification_2025-11-25` + direct WebFetch of modelcontextprotocol.io, HIGH confidence):**
- [MCP overview / getting started](https://modelcontextprotocol.io/docs/getting-started/intro) — coordinator-supplied authoritative source, used to frame tools/resources/prompts/notifications as the canonical feature surface
- [Tools specification](https://modelcontextprotocol.io/specification/2025-11-25/server/tools) — naming rules, annotations, structured content, output schema, `isError`/protocol-error split
- [Resources specification](https://modelcontextprotocol.io/specification/2025-11-25/server/resources) — URI schemes, subscriptions, when servers expose resources vs tools
- [Prompts specification](https://modelcontextprotocol.io/specification/2025-11-25/server/prompts) — user-controlled trigger model, argument schema
- [Pagination specification](https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/pagination) — cursor-based, list-operations-only scope
- [Progress specification](https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/progress) — `progressToken` correlates to a single in-flight request
- [Elicitation specification](https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation) — form/URL modes, statefulness and identity-binding requirements, "MUST NOT request sensitive info via form mode"
- [Sampling specification](https://modelcontextprotocol.io/specification/2025-11-25/client/sampling) — server-initiated LLM completion via the client
- [SEP-2567: Sessionless MCP via Explicit State Handles](https://modelcontextprotocol.io/seps/2567-sessionless-mcp) — **Final, Standards Track**, accepted 2026; primary source for the explicit `session_id` handle recommendation, including the specific guidance that stdio servers "SHOULD NOT rely on process-lifetime state and SHOULD migrate to explicit handles"
- Authorization scoping to HTTP transport (stdio "SHOULD NOT" implement OAuth, credentials from environment instead) — cross-checked via WebSearch against `modelcontextprotocol.io/specification/draft/basic/authorization`, MEDIUM confidence (search-synthesized, consistent across multiple independent write-ups)

**Anthropic first-party guidance (HIGH confidence, official engineering blog):**
- [Writing effective tools for AI agents](https://www.anthropic.com/engineering/writing-tools-for-agents) — namespacing, parameter naming, token-budget management, ~25k-token default response ceiling in Claude Code, `response_format` concise/detailed pattern

**Production MCP servers examined (MEDIUM confidence, official repos/docs, WebFetch/WebSearch):**
- [AWS CloudWatch MCP Server](https://awslabs.github.io/mcp/servers/cloudwatch-mcp-server) — tool catalog, async query-ID pattern, pagination/chunking behavior
- [AWS MCP DESIGN_GUIDELINES.md](https://github.com/awslabs/mcp/blob/main/DESIGN_GUIDELINES.md) — naming limits, error-handling conventions, resources-vs-tools framing
- [GitHub MCP Server](https://github.com/github/github-mcp-server) — verb_noun snake_case convention, toolset-per-tool mapping
- [Playwright MCP](https://github.com/microsoft/playwright-mcp) — session-mode design (persistent/isolated/extension), context management
- Browserbase MCP (`start`/`end`/`navigate`/`act`/`observe`/`extract`) — explicit session-tool precedent, via WebSearch synthesis of official docs
- [WireMCP](https://github.com/0xKoda/WireMCP) — counter-example: synchronous single-call packet capture with no session/pagination model, used to contrast against cpg's chosen session-polling design

**Ecosystem adoption / client-support gap (MEDIUM confidence — WebSearch-synthesized blog commentary, directionally consistent across multiple independent sources, partially corroborated by official Claude Code docs):**
- Resources adoption gap and client-support inconsistency across MCP hosts — multiple independent write-ups (layered.dev, PulseMCP client-capability-gap post) agree on the chicken-and-egg dynamic
- [Claude Code MCP docs](https://code.claude.com/docs/en/mcp) — confirms resources are surfaced via manual `@mention`, not autonomous model access
- [anthropics/claude-code#18763](https://github.com/anthropics/claude-code/issues/18763) — confirms host-side auto-namespacing (`mcp__server__tool` / `Server:tool`) so server authors don't need a redundant prefix
- "ResourceLink for large datasets" / dual preview+reference pattern — futuresearch.ai and an arXiv write-up on large-dataset MCP patterns, used for the `list_dropped_flows`/`get_evidence` response-shape recommendation

---
*Feature research for: cpg v1.5 MCP server integration (readonly stdio tool surface only)*
*Researched: 2026-07-20*
