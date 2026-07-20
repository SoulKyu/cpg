# Project Research Summary

**Project:** CPG — Cilium Policy Generator
**Domain:** MCP (Model Context Protocol) stdio server integration into an existing Go CLI (Kubernetes/Cilium network-policy observability)
**Researched:** 2026-07-20
**Confidence:** HIGH

## Executive Summary

v1.5 adds a readonly `cpg mcp` stdio server on top of an already-shipping Go CLI (Hubble → CiliumNetworkPolicy generator). This is a **wrap, don't redesign** milestone: all four research files converge on the same posture. The official `github.com/modelcontextprotocol/go-sdk/mcp` (Tier 1, stable v1.6.1) is the correct SDK — zero toolchain change, stable semver, institutional backing — over the more popular but pre-1.0 `mark3labs/mcp-go`. The entire new surface is 7 tools (`start_session`/`get_status`/`stop_session` + 4 readonly query tools) built from one new package (`pkg/session`, wrapping the existing `hubble.RunPipeline` entrypoint completely unmodified), one promoted package (`pkg/explain`, a mechanical move mirroring the exact precedent already set once by `pkg/flowsource`'s v1.1 promotion), and small additive exports on `pkg/output`/`pkg/hubble`. Nothing in the core pipeline (`pkg/hubble`, `pkg/evidence`, `pkg/k8s`, `pkg/dropclass`) needs to change to make this work.

The recommended approach: session lifecycle wraps the existing blocking `RunPipeline` call in a goroutine against a **detached, cancellable context** (not the tool handler's request-scoped context, which gets cancelled the instant the call returns) — reusing the exact ctx-cancel shutdown path already exercised by Ctrl+C on `cpg generate` today. Query tools are pure filesystem readers scoped to the session tmpdir, never touching pipeline internals or the live cluster, with pagination (`limit`/`cursor`) mandatory on any tool that can return many records, and an explicit opaque `session_id` (SEP-2567) threaded through every session-scoped call. Every tool sets `readOnlyHint: true`, but — critically — this is backed by a structural fact (the composition root never imports a K8s write verb), not by the hint itself, which the spec explicitly calls advisory and untrusted.

The chief risk is the stdio wire itself: cpg already has three existing `os.Stdout`-defaulting writer seams (`PipelineConfig.Stdout`, `policyWriter.diffOut`, cobra's usage-on-error path) that a naive integration trips on day one, invisible to a human eyeballing a terminal and only surfacing as silent JSON-RPC corruption in a real harness — this must be closed by explicit wiring plus an automated in-memory-transport stdout-purity test, not code review. Second, and requiring explicit product decisions rather than silent defaults: this research surfaced **four concrete tensions** between what FEATURES.md's tool contracts assume and what ARCHITECTURE.md's direct source-reading found actually exists on disk today (session-model shape, `list_dropped_flows`'s data source, `cluster-health.json`'s finalize-only timing, and a non-atomic writer creating a torn-read risk for the new query-tool consumers). These are reconciled explicitly below and must land in REQUIREMENTS.md, not be silently defaulted during build.

## Key Findings

### Recommended Stack

Full detail: `.planning/research/STACK.md`

The stack decision is narrow and low-risk: one real new dependency (the MCP SDK itself), everything else is either already vendored or arrives transitively. `go-sdk` was chosen over `mcp-go` primarily on **stability evidence, not popularity** — `mcp-go` (8,910 stars vs. go-sdk's 4,822) is pre-1.0 and its own docs describe real recent breaking changes (Sampling capability type, schema-tag rename), while `go-sdk` is a stable v1.x under semver and is the only Go SDK listed on modelcontextprotocol.io's official Tier-1 SDK page.

**Core technologies:**
- `github.com/modelcontextprotocol/go-sdk/mcp` v1.6.1 — MCP server runtime (session lifecycle, tool dispatch, schema inference, stdio JSON-RPC framing) — official Tier-1 SDK, stable semver, requires `go 1.25.0` (cpg is already on `1.25.1`, zero toolchain change)
- `go.uber.org/zap/exp/zapslog` — bridges go-sdk's internal `*slog.Logger` hook into cpg's existing zap stderr pipeline — already bundled inside the pinned `zap v1.27.1`, import-only, no new go.mod line
- `golang.org/x/sync/errgroup` — already a direct dependency, reused for the session's background-goroutine lifecycle exactly as `pkg/hubble/pipeline.go` already uses it
- `github.com/google/jsonschema-go` — transitive via go-sdk; only import directly if a tool needs schema constraints beyond struct-tag inference (e.g., a `DropClass` enum)
- **Rejected:** `mark3labs/mcp-go` (pre-1.0, documented breaking-change history); any HTTP/SSE transport or OAuth machinery (`golang.org/x/oauth2`, `golang-jwt`) — v1.5 is stdio-only, per milestone scope

One version note worth carrying forward: `go mod tidy` will bump `golang.org/x/oauth2` transitively (SDK requires ≥v0.35.0, cpg pins v0.34.0 indirect today) — inert, since OAuth is an HTTP-transport-only concern and cpg is stdio-only; not a new attack surface to review, just a version-diff line reviewers should expect.

### Expected Features

Full detail: `.planning/research/FEATURES.md`

**Must have (table stakes):**
- snake_case verb_noun tool names, no `cpg_` prefix — hosts already auto-namespace (`mcp__cpg__start_session`)
- Onboarding-depth tool descriptions that explicitly teach cpg's policy-actionable vs. infra/transient dropclass distinction inline — the single highest-leverage, lowest-cost differentiator, since a naive description risks the LLM proposing policies for infra noise (exactly what the classifier exists to prevent)
- Tool annotations (`readOnlyHint`/`destructiveHint`/`idempotentHint`/`openWorldHint`), truthfully set on all 7 tools
- `structuredContent` + `outputSchema` on every data-returning tool, `content` text block always included for back-compat
- Mandatory `limit`/`cursor` pagination with `total_count`/`has_more` on any tool returning many records (`list_dropped_flows`, `get_evidence`) — MCP's protocol-level pagination only covers `tools/list`, never `tools/call`
- `isError`-based tool execution errors with specific, actionable text (not generic failures)
- Explicit opaque `session_id` handle per **SEP-2567** (Final, accepted 2026), required on every session-scoped call

**Should have (differentiators):**
- Dual preview + reference pattern: a small human-readable sample in `content`, full/paginated data in `structuredContent`
- `list_policies` (cheap metadata) / `get_policy` (full YAML) split, mirroring AWS CloudWatch's and GitHub MCP's list/get convention
- Reuse the existing `cpg explain --output json` renderer verbatim for `get_evidence` — near-zero new design work, keeps CLI and MCP surfaces from drifting apart
- Plain absolute tmpdir paths instead of MCP Resources — Resources have a documented, real client-support gap (Claude Code only surfaces them via manual `@mention`), and cpg's stdio same-filesystem deployment makes a plain path strictly better for a model-driven loop
- Passthrough of `cluster-health.json`'s existing Cilium-docs remediation URLs — free value from an already-shipped artifact

**Defer / reject:**
- Mutating `apply_policy` tool — breaks the readonly guarantee outright; no `cpg apply` CLI exists yet to wrap
- Elicitation, sampling, MCP prompts, progress notifications — all explicitly rejected for v1.5. Sampling in particular would just be a new transport for the AI-plausibility feature PROJECT.md already shelved on 2026-04-25; progress notifications have no blocking call to attach to given the start/poll/stop design
- OAuth/authorization — not a rejected feature, simply out of scope: the spec scopes authorization to HTTP transports only, stdio servers get credentials from the environment

### Architecture Approach

Full detail: `.planning/research/ARCHITECTURE.md`

Purely additive: one new cobra subcommand, one new package, one promoted package, small additive exports — nothing in the existing pipeline changes. Query tools never share in-memory state with the pipeline; the filesystem (session tmpdir) is the only channel between the write side and the read side, which keeps the "no parallel in-memory path" constraint intact for free.

**Major components:**
1. `cmd/cpg/mcp.go` (+ `mcp_tools.go`) — cobra command, MCP SDK transport wiring, translates JSON-RPC tool calls into Go calls into `pkg/session` and the readers
2. `pkg/session` (new) — `SessionManager`: single active session (mutex-guarded), `os.MkdirTemp`, `context.WithCancel` wrapping `hubble.RunPipeline` in a goroutine — the highest-novelty, highest-concurrency-risk new code in the milestone
3. `pkg/explain` (new, promoted) — mechanical move of already-decoupled filter+render logic out of `cmd/cpg`, shared by the CLI `cpg explain` and the new `get_evidence` tool; the render functions already take an `io.Writer` first parameter, so this is a package-boundary move, not a rewrite
4. Query readers — additive exports (`pkg/output` policy-listing helper, `pkg/hubble.ReadClusterHealth`) plus the unmodified `pkg/evidence.Reader`

One naming note worth preserving: the new domain package must not be called `pkg/mcp` — the SDK's own package is also named `mcp`, which would force an import alias everywhere; `pkg/session` sidesteps this for free, since only `cmd/cpg/mcp.go` (package `main`) ever imports the SDK.

### Critical Pitfalls

Full detail: `.planning/research/PITFALLS.md`

1. **stdout is the wire, and cpg already aims writers at it** — `PipelineConfig.Stdout` and `policyWriter.diffOut` both default to `os.Stdout` when `nil`, and cobra's usage-on-error path also targets stdout unless `SilenceUsage`/`SilenceErrors` are set. Fix: wire every seam explicitly (`io.Discard` or a captured buffer), set `Silence*`, and back it with an automated stdout-purity test (assert every line parses as JSON-RPC across a full session) rather than relying on code review to catch it forever.
2. **Blocking a tool handler on the capture pipeline** — `RunPipeline` blocks until `ctx.Done()`; the `context.Context` MCP hands a tool handler is request-scoped and typically cancelled the instant the handler returns, so a naive `go RunPipeline(handlerCtx, ...)` gets killed before it starts. Fix: spawn on a **detached** context (`context.WithoutCancel`) wrapped in its own `context.WithCancel`, store the cancel func in the session, return immediately.
3. **Client/harness death orphans the session** — documented against Claude Code itself (orphaned MCP processes, issues #22612/#39170). The spec's only portable shutdown signal is stdin EOF. Fix: treat the transport's `Run()` returning, for any reason, as the single root shutdown trigger and fan it out — cancel the session context, close the port-forward `stopCh`, `os.RemoveAll` the tmpdir — each with a bounded deadline so one wedged cleanup can't block process exit.
4. **Unbounded query results blow the LLM's context window** — Claude Code hard-caps MCP tool output at 25,000 tokens by default. Fix: mandatory list/get split with pagination built into the *first* version of every "many-of-X" tool, never retrofitted after an oversized response ships.
5. **Non-atomic policy writer creates a torn-read risk for the very tools this milestone adds** — `pkg/output/writer.go` uses direct `os.WriteFile` with no temp+rename, unlike the evidence and health writers, which already use that pattern. This has never mattered because `generate`/`replay` are single-consumer, run-to-completion CLI invocations; MCP query tools are the first *concurrent* reader this code will ever have. (See Cross-File Tension 4 below for the recommended fix and its sequencing.)
6. **"Readonly" is a hint, not an enforcement mechanism** — the spec explicitly calls `readOnlyHint` advisory and untrusted. Cpg's readonly guarantee is real today only because zero K8s write verbs are reachable from any code path; the moment a future `cpg apply` command exists in the same binary, the guarantee becomes "did someone remember to exclude this tool" rather than "this binary cannot do it." Enforce structurally at the composition root (`cmd/cpg/mcp.go` only ever registers handlers that call read-only functions), re-verified on every new tool, not a one-time audit.

## Cross-File Tensions & Reconciliation

Four places where the four research files' recommendations don't trivially line up, surfaced explicitly rather than papered over. All four need an explicit line item in REQUIREMENTS.md — none should be resolved by silent default during implementation.

### Tension 1: Single-session model (ARCHITECTURE) vs. explicit `session_id` handle (FEATURES/SEP-2567)

ARCHITECTURE.md's Anti-Pattern 4 recommends a single-session model: `pkg/session.Manager` holds one `*Session` (nil when idle), guarded by a mutex; a second `start_session` while one is active returns an explicit error rather than silently discarding in-flight data. Rationale: matches the milestone's own tool names, the "flat memory profile" constraint, and the existing CLI's single-shot mental model, with zero existing concurrency precedent in the codebase to build against.

FEATURES.md, backed directly by **SEP-2567 (Final, accepted 2026)**, recommends every session-scoped tool require an explicit opaque `session_id` argument — even for a single-process stdio server — because opaque handles produce a crisp "session `sess_xyz` not found or expired" error instead of an ambiguous "no session" state, survive context compaction (they're plain strings in the transcript), and keep the design forward-compatible if cpg ever ships multi-session or HTTP transport.

**These are not in conflict — they compose.** "Single concurrent session" is a runtime *capacity* constraint (how many sessions `pkg/session.Manager` will run at once: exactly one). "Explicit `session_id`" is a *protocol design* choice (how the handle is represented and threaded through calls) — orthogonal to capacity. Concretely: `start_session` mints exactly one opaque `session_id` at a time; the manager enforces single-active-session by rejecting a second `start_session` call with an explicit, actionable error; every session-scoped tool still requires `session_id` even though only one value could ever be valid, because it costs nothing extra, gives the SEP's crisp expired/unknown-session error if the LLM calls a query tool with a stale ID after `stop_session`, and avoids a breaking tool-schema change if a future milestone ever adds multi-session support.

**Requirements action:** confirm as one requirement, not two competing ones — "single concurrent session; explicit opaque `session_id` handle threaded through every session-scoped call; a second concurrent `start_session` is rejected with an actionable error, not queued or silently replaced."

### Tension 2: `list_dropped_flows` has no existing complete data source

ARCHITECTURE.md's build-order step 3d and Open Question 3 flag this directly from reading `pkg/hubble/aggregator.go`: neither the evidence samples (FIFO-capped, attached only to policy-worthy rules) nor the health snapshot (aggregate counts, Infra/Transient only — `DropEvent` carries no timestamp/port/verdict) add up to a complete raw dropped-flow log. FEATURES.md, by contrast, lists `list_dropped_flows` as a P1/MVP launch item with `limit`/`cursor`/`since`/namespace/dropclass filtering, describing it as "the tool most likely to blow a token budget if shipped without pagination" — its tool-contract design implicitly assumes flow-level data availability that ARCHITECTURE's source-reading shows doesn't fully exist today. This isn't a contradiction between the two files so much as FEATURES designing the ideal contract before ARCHITECTURE's grounding revealed the sourcing gap underneath it.

**Recommended scoping for v1.5:** ship `list_dropped_flows` as a **composed view** over the existing capped evidence samples plus aggregate health counts — no new pipeline writer, staying inside this milestone's "integrate, don't redesign" discipline. Explicitly document that it is *not* a raw flow log (no full per-drop timestamp/port/verdict, no per-flow record for infra/transient drops beyond aggregate counts) so the tool description doesn't overpromise — an overpromising description here is exactly the failure mode PITFALLS' schema/UX pitfalls warn about. Log a genuine new minimal flow-sample writer (a 4th tee target alongside `policyCh`/`evidenceCh`/`healthCh`, with its own FIFO cap) as a deliberate fast-follow if the composed view proves insufficient in practice — that is real pipeline design work, out of this milestone's "reuse only" scope, and deserves its own sizing/requirements pass rather than being folded in silently.

**Requirements action:** confirm the composed-view scoping explicitly in REQUIREMENTS.md; FEATURES.md's MVP list currently reads as if the full-fidelity tool is directly buildable as specified — it isn't, without this scoping decision.

### Tension 3: `cluster-health.json` is finalize-only — live health during an active session is a gap

ARCHITECTURE.md's Pattern 2 and Open Question 1 establish this directly from the pipeline source: the file is written exactly once, after `g.Wait()` returns — it does not exist at all until `stop_session`. The Scaling Considerations table calls this "the primary 'what breaks first' for this integration." FEATURES.md lists `get_cluster_health` as a P1 "thin passthrough of the existing `cluster-health.json`" without itself surfacing the mid-session-absence problem — again, only visible from ARCHITECTURE's direct source read.

This overlaps with a second, related gap: `SessionStats` (flows seen, policies written so far) is also only logged once, at the very end — there's no API today for `get_status` to report live numeric counters mid-session either.

**Recommended resolution for v1.5:** ship with "not available until stop" as documented, non-error behavior for both — `get_cluster_health` returns an explicit "session still capturing; cluster health available after `stop_session`" result (not an error, per FEATURES' `isError` guidance), and `get_status` reports coarse state (running/stopped, artifact file counts on disk) rather than true live counters, which are achievable with zero pipeline changes. Treat a small additive `pkg/hubble` change (periodic health flush, or an optional `*SessionStats` hook on `PipelineConfig`) as a deliberate, explicitly-scoped v1.5.x/v1.6 enhancement, not something this integration should reach for by default.

**Requirements action:** decide both together (they're the same underlying "no live view into an in-flight pipeline" gap) before Phase 4's tool-response schemas are finalized — `get_status`/`get_cluster_health` need a `status: capturing | stopped` / `health: available | not_yet_available`-shaped field either way, and that shape should be designed once, deliberately, not discovered mid-implementation.

### Tension 4: `pkg/output/writer.go`'s non-atomic write — torn-read risk for the new query tools

ARCHITECTURE and PITFALLS independently converge on the same technical finding and the same fix, but frame the *urgency* differently. ARCHITECTURE (Pattern 2, Anti-Pattern 3) is cautious: it calls the atomic temp+rename fix "a real, low-risk improvement" but explicitly warns against "silently widening scope" mid-MCP-build, recommending only a read-side retry-on-parse-error for this integration and flagging the writer fix "for the roadmap/PITFALLS track instead of doing it here." PITFALLS (Pitfall 5) is more assertive: it calls the fix "small, mechanical, low-risk... internally consistent with cpg's own prior art" (the evidence and health writers already use temp+rename), and its Technical Debt table rates leaving the gap unfixed as acceptable "**Never**, once query tools read that directory concurrently with an active session — fix before wiring the reader." Recovery cost is rated LOW either way.

**Reconciliation:** there is no real disagreement on the technical fix — both files agree temp+rename is correct, low-risk, and matches existing prior art. The tension is procedural: is this an MCP-milestone change, or a prerequisite bug fix that predates it? PITFALLS' framing is the more actionable resolution and should govern: land the fix as a **small, separately-reviewable change** to `pkg/output/writer.go` (mirroring `pkg/evidence/writer.go`/`pkg/hubble/health_writer.go`'s exact existing pattern), sequenced early — either just before or as the first explicit item of this milestone — not bundled invisibly inside a larger query-tools commit, and not deferred to "harden later." This honors ARCHITECTURE's "don't silently widen scope" caution (it's still an explicit, isolated, reviewed change, not a silent scope-creep) while satisfying PITFALLS' "never, once query tools read concurrently" urgency. Keep the read-side retry-on-parse-error as defense-in-depth regardless — cheap, and useful robustness even after the writer fix — but it is not a substitute for the writer fix, only a supplement.

**Requirements/roadmap action:** sequence this as its own small, explicit task early in the roadmap (see Phase 3 below), not silently deferred past the milestone.

## Implications for Roadmap

Based on combined research — particularly ARCHITECTURE.md's explicit dependency-ordered build order and PITFALLS.md's pitfall-to-phase mapping, which independently converge on nearly identical groupings — suggested phase structure:

### Phase 1: MCP Server Skeleton & Protocol Safety
**Rationale:** Must be proven before any tool logic is layered on top (ARCHITECTURE build-order step 1; PITFALLS names this "first phase" for both Pitfall 1 and Pitfall 10). Cheap to build, de-risks everything downstream.
**Delivers:** `cpg mcp` subcommand registered in `main.go`, zero tools, stdio transport wired via `mcp.StdioTransport` + `signal.NotifyContext`, `SilenceUsage`/`SilenceErrors` set, existing `buildLogger()` reused unchanged (already stderr-only), an in-memory-transport (`NewInMemoryTransports()`) protocol test harness with a stdout-purity assertion as its first test.
**Uses:** `github.com/modelcontextprotocol/go-sdk/mcp` v1.6.1, `zap/exp/zapslog` bridge.
**Avoids:** Pitfall 1 (stdout pollution), Pitfall 10 (no protocol-level tests).
**Structural decision made here (not deferred):** the composition-root readonly constraint (Pitfall 7) — `cmd/cpg/mcp.go` may only ever register handlers reaching read-only functions — should be decided as a rule at this stage even though it's *verified* later (Phase 5).

### Phase 2: Session Lifecycle (start_session / get_status / stop_session)
**Rationale:** Every other tool depends on a working session handle; ARCHITECTURE flags this as the highest-novelty, highest-concurrency-risk piece and recommends proving it in isolation (unit-tested with a fake `FlowSource`, no real cluster, no MCP SDK) before any tool wiring touches it.
**Delivers:** `pkg/session` package — `SessionManager` wrapping `hubble.RunPipeline` completely unmodified via a **detached**, cancellable context in a background goroutine; single-active-session guard; explicit opaque `session_id` (SEP-2567 shape).
**Implements:** ARCHITECTURE Pattern 1 (session manager wraps the pipeline entrypoint unchanged, stop = ctx cancel).
**Avoids:** Pitfall 2 (blocking tool handler on the pipeline), Pitfall 3 (orphaned sessions on client/harness death), Pitfall 8 partially (kubeconfig bounded-timeout wrapper around the initial client-build call).
**Resolves:** Cross-File Tension 1 (single-session capacity + explicit `session_id` handle) — this is where that reconciliation gets implemented; confirm the requirement wording here before coding.

### Phase 3: Read-Side Foundations (parallelizable with Phase 2)
**Rationale:** Each piece depends only on an already-existing package, independent of session/MCP wiring — ARCHITECTURE explicitly calls this parallelizable with Phase 2. Promote `pkg/explain` here since it touches existing `cmd/cpg` files and tests; land the atomic-write fix here (or earlier, standalone) per Tension 4's resolution, before any query tool reads from `pkg/output`.
**Delivers:** `pkg/output/writer.go` brought to the same temp+rename pattern already used by the evidence/health writers (Tension 4 fix); `pkg/hubble.ReadClusterHealth` + exported `ClusterHealthReport`/`HealthDropJSON` types; `pkg/explain` promoted from `cmd/cpg` (mechanical move, existing `cpg explain` test suite re-run immediately to confirm nothing broke).
**Implements:** ARCHITECTURE Pattern 3 (promote `cmd/cpg` presentation logic to an importable package — direct precedent from `pkg/flowsource`'s v1.1 promotion).
**Avoids:** Pitfall 5 (writer/reader torn-read races) — fixed at the source, not just papered over with read-side retries.

### Phase 4: Query Tools (dropped flows, policies, evidence, cluster health)
**Rationale:** Depends on Phase 2 (session_id, tmpdir) and Phase 3 (readers). This is where the actual MCP tool contracts, schemas, and response shapes get designed and reviewed — and where Tensions 2 and 3 must already be resolved, since they change these tools' response shapes.
**Delivers:** `list_dropped_flows`, `list_policies` + `get_policy`, `get_evidence` (reusing the promoted `pkg/explain` renderer verbatim), `get_cluster_health` — all with pagination (`limit`/`cursor`/`total_count`/`has_more`), `structuredContent`+`outputSchema`, `isError` actionable errors, tool annotations, and descriptions that explicitly teach the dropclass taxonomy.
**Addresses:** Nearly all remaining FEATURES.md table-stakes and differentiator items.
**Avoids:** Pitfall 4 (unbounded results), Pitfall 6 (schema mistakes — enum `DropClass` not raw `DropReason`, no root-level `oneOf`/`anyOf`/`allOf`, "exactly one of" validated in handler logic).
**Requires resolution before design is final:** Tension 2 (`list_dropped_flows` composed-view scoping) and Tension 3 (live cluster-health/status shape).

### Phase 5: Security / Readonly Hardening & Operational Docs
**Rationale:** Cross-cutting audit pass once the tool table is complete. The *structural* decision (composition root only calls read-only functions) was already made in Phase 1 — this phase verifies and documents it, and closes the remaining pitfalls that are judgment calls rather than code patterns.
**Delivers:** Import-graph readonly audit (no reachable K8s write verb, no filesystem write outside the session tmpdir — re-run on every future tool addition); documented `env` block requirements for MCP host configs (`KUBECONFIG`/`HOME`/`PATH`/`TMPDIR` — nothing is inherited by default); an explicit written decision on `HTTPPath`/label secret exposure (ship-documented-risk vs. best-effort redaction); distinct, specific error strings per kubeconfig/auth failure mode.
**Avoids:** Pitfall 7 (readonly-as-hint-not-enforcement), Pitfall 8 (kubeconfig env/docs half), Pitfall 9 (secrets traveling differently through an LLM than through committed YAML).

### Phase 6: End-to-End Stdio Validation
**Rationale:** Last, per ARCHITECTURE's build order — proves the full stdio contract holds across a complete session lifecycle, with everything from Phases 1–5 wired together.
**Delivers:** Integration test driving `initialize → start_session → get_status → each query tool → stop_session → process exit`, asserting stdout carries only valid JSON-RPC frames throughout; `-race` extended to all new packages, consistent with cpg's existing "tests passing with `-race`" discipline; an ungraceful-disconnect variant (kill the transport mid-session, assert port-forward + tmpdir are gone within a bounded deadline).

### Phase Ordering Rationale

- Phases 1–3 are almost entirely internal/invisible (no new user-facing tool works yet) but exist because ARCHITECTURE's dependency read is explicit: session lifecycle and the read-side helpers are prerequisites, not just "nice to build first" — Phase 4's tools cannot be correctly designed until Tensions 1–4 are resolved, which happens naturally by the end of Phase 3.
- Phases 2 and 3 are independent of each other (different packages, different risk profiles) and can run in parallel if the roadmapper wants to compress the schedule — ARCHITECTURE calls this out explicitly.
- Security hardening is deliberately Phase 5, not folded into Phase 4, because PITFALLS' own phase mapping keeps it as a discrete audit pass — but the roadmapper should note the *structural* readonly rule is a Phase 1 decision, only *verified* in Phase 5, to avoid the false impression that readonly safety is bolted on at the end.
- Phase 6 is last by construction — it's the integration proof, not a place where new capability is built.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 4 (query tools):** contingent on which option is chosen for Tension 2 — if a new minimal flow-sample writer is chosen over the composed-view scoping, that is genuinely new pipeline design work (a 4th tee target, its own FIFO cap sizing) not covered by this research pass and would benefit from a focused `--research-phase` pass before implementation.
- **Phase 5 (secrets/redaction decision within security hardening):** low technical complexity, but the `HTTPPath`/label exposure choice is a product/security judgment call rather than an implementation pattern — flag for explicit stakeholder decision rather than technical research per se.

Phases with standard patterns (skip research-phase — code-level shapes are already fully specified by ARCHITECTURE.md's Patterns 1–3 and PITFALLS' concrete fixes):
- **Phase 1:** exact cobra/SDK wiring snippet already given in STACK.md; `SilenceUsage` behavior verified via `go doc -src` against the pinned cobra version.
- **Phase 2:** exact `Session`/`Manager` shape and detached-context pattern already given in ARCHITECTURE Pattern 1; the one open item (Tension 1 wording) is a requirements confirmation, not a research gap.
- **Phase 3:** direct precedent already exists in the codebase twice over (evidence/health writers' temp+rename; `pkg/flowsource`'s promotion history) — mechanical work.
- **Phase 6:** in-memory transport testing approach already documented (go-sdk's `NewInMemoryTransports()`), golden-sequence test shape already specified.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Context7 + official modelcontextprotocol.io SDK-tier page + GitHub API version/star verification + direct source reads (zap `config.go`, go-sdk `logging.go`/`server.go`) at the exact pinned tag. Very little inference; version-compatibility claims verified against cpg's actual `go.mod`. |
| Features | HIGH (core spec) / MEDIUM (ecosystem) | Core MCP claims (tool annotations, structured content, pagination scope, SEP-2567's Final/accepted status) verified via Context7 + the official spec pages. Ecosystem-adoption and Claude-Code-specific claims (Resources client-support gap, `mcp_` auto-namespacing) are WebSearch-sourced but cross-checked against at least one primary/official source each. |
| Architecture | HIGH | The strongest-grounded of the four files — integration points, writer atomicity, and concurrency behavior verified by directly reading the actual `pkg/hubble/pipeline.go`, `pkg/output/writer.go`, `pkg/evidence/*` source, not by pattern inference. zap/cobra defaults verified via `go doc` against the exact pinned versions. |
| Pitfalls | HIGH (spec/library/codebase) / MEDIUM-LOW inline (ecosystem) | Grounded in the official MCP spec (draft + stable 2025-06-18), current official Claude Code MCP docs (timeouts, env handling, output-token limits), vendored cobra/zap source read directly, and the live cpg codebase read/grepped this session. Community-sourced claims (e.g., Claude Code orphaned-process GitHub issues, general MCP schema-design blog guidance) are explicitly flagged MEDIUM inline rather than presented as verified fact. |

**Overall confidence:** HIGH — an unusually strong research pass. All four files performed direct reads of the actual cpg source (not just pattern induction from generic MCP guidance), and the MCP-specific claims are grounded in the official spec plus an accepted SEP. The residual uncertainty is concentrated entirely in the four cross-file tensions above — and those are correctly surfaced as *product/requirements decisions the research revealed*, not gaps the research failed to close.

### Gaps to Address

- **Cross-File Tension 1** (single-session capacity vs. explicit `session_id` handle): reconciliation proposed above; needs one explicit REQUIREMENTS.md line item combining both, not two separate/competing ones — handle during Phase 2 planning.
- **Cross-File Tension 2** (`list_dropped_flows` data-source scoping): needs an explicit REQUIREMENTS.md decision on the composed-view scope before Phase 4's tool contract is finalized; if the composed view is later judged insufficient, the flow-sample-writer fast-follow needs its own sizing/research pass.
- **Cross-File Tension 3** (live cluster-health/status during an active session): needs an explicit REQUIREMENTS.md decision — "not available until stop" documented behavior recommended for v1.5, decided jointly with the live-counters gap since both stem from the same underlying limitation.
- **Cross-File Tension 4** (non-atomic `pkg/output/writer.go`): fix recommended as a small, standalone, early-sequenced change (not deferred) — needs to be an explicit roadmap task, not silently absorbed into a larger commit.
- **Minor:** MCP spec draft `2026-07-28` / go-sdk `v1.7.0-pre.1..3` are not GA and not on the public spec pages yet — correctly excluded from v1.5 scope by STACK.md; re-check at the next milestone, no action needed now.
- **Minor:** `cpg apply` (already "Planned" in PROJECT.md) is a live future risk to the readonly guarantee the moment it exists in the same binary — not an action item for v1.5, but PITFALLS flags it explicitly so a future apply-tool design carries the structural-exclusion requirement (Pitfall 7) forward rather than rediscovering it.

## Sources

### Primary (HIGH confidence)
- Context7 `/modelcontextprotocol/go-sdk` — stdio transport, `AddTool`/`ToolHandlerFor`, schema inference, `ServerOptions.Logger` default, `ToolAnnotations`
- `modelcontextprotocol.io/docs/sdk`, `/specification/2025-11-25/server/{tools,resources,prompts,utilities/pagination,utilities/progress}`, `/specification/{draft,2025-06-18}/basic/transports`, `/seps/2567-sessionless-mcp` (SEP-2567, Final, accepted 2026)
- `code.claude.com/docs/en/mcp` (fetched 2026-07-20) — stdio timeout ceilings, env-variable non-inheritance, `MAX_MCP_OUTPUT_TOKENS`, root-level schema-union handling
- `anthropic.com/engineering/writing-tools-for-agents` — namespacing, token-budget management, description-quality impact
- `github.com/modelcontextprotocol/go-sdk` — README, releases (v1.6.1), `go.mod` at the pinned tag, issue #224 (`Server.Run` context-cancellation bug, fixed via PR #234)
- Local repo inspection (this research pass, direct reads/greps): `pkg/hubble/pipeline.go`, `pkg/hubble/writer.go`, `pkg/hubble/health_writer.go`, `pkg/hubble/client.go`, `pkg/hubble/aggregator.go`, `pkg/output/writer.go`, `pkg/evidence/{reader,writer,schema}.go`, `pkg/k8s/{client,portforward}.go`, `pkg/dropclass/classifier.go`, `pkg/flowsource/source.go`, `cmd/cpg/{main,generate,explain,explain_render,explain_filter,explain_target}.go`, `go.mod`, `go.sum`, `.planning/PROJECT.md`
- `go doc` against cpg's exact pinned versions — `go.uber.org/zap` (`NewProductionConfig`/`NewDevelopmentConfig`/`NewDevelopment` all default to stderr), `github.com/spf13/cobra.Command.ExecuteC` (usage-on-error targets stdout unless `SilenceUsage`)

### Secondary (MEDIUM confidence)
- Production MCP server prior art: AWS CloudWatch MCP + `DESIGN_GUIDELINES.md`, GitHub MCP Server, Playwright MCP, Browserbase MCP, WireMCP (counter-example)
- `github.com/mark3labs/mcp-go` — evaluated and rejected as the SDK choice; used as a breaking-change/comparison data point
- Claude Code GitHub issues: orphaned MCP processes (#22612, #39170), env-variable stripping (#1254, #10955)
- Resources client-support-gap and dual preview+reference pattern write-ups (layered.dev, PulseMCP, futuresearch.ai) — WebSearch-synthesized, directionally consistent across independent sources, partially corroborated by official Claude Code docs
- `blog.modelcontextprotocol.io/posts/2026-03-16-tool-annotations` — annotations as an untrusted-hint vocabulary
- client-go SPDY goroutine-leak history (kubernetes/kubernetes#105830, #96339), exec-credential-plugin stdin/TTY behavior (kubernetes/kubernetes#98451)

### Tertiary (LOW confidence)
None load-bearing for this synthesis — every claim used above was independently rated HIGH or MEDIUM by its source research file; MEDIUM-confidence ecosystem claims are flagged inline where they appear rather than presented as verified fact.

---
*Research completed: 2026-07-20*
*Ready for roadmap: yes*
