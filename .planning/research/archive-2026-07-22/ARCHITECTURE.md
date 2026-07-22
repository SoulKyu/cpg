# Architecture Research

**Domain:** MCP server integration into an existing Go CLI (readonly stdio MCP layer over cpg's live Hubble→CiliumNetworkPolicy pipeline)
**Researched:** 2026-07-20
**Confidence:** HIGH (integration points and races verified by reading the actual pipeline/writer/reader source; MCP transport constraint verified against the current official spec; zap/cobra defaults verified against the exact pinned versions via `go doc`)

This research answers ONE question: how does a new MCP layer integrate with cpg's existing architecture for v1.5. It does not propose changing the existing pipeline, writers, or package boundaries beyond small, additive, precedent-following extensions called out explicitly below.

## Existing Architecture (as read from the code — not redesigned)

```
cmd/cpg/{main,generate,replay,explain}.go (package main, cobra)
        │  construct PipelineConfig, call hubble.RunPipeline(WithSource)
        ▼
pkg/hubble/pipeline.go — RunPipelineWithSource(ctx, cfg, source)
        │  errgroup.WithContext(ctx); ctx cancel unwinds every stage
        ▼
 flows ──► Aggregator.Run() ──► policies chan ──► tee (Stage 1b)
                                                     │        │
                                              policyCh      evidenceCh
                                                 │              │
                                         pkg/output.Writer   evidenceWriter
                                         (NOT atomic write)  → pkg/evidence.Writer
                                                              (atomic: tmp+rename)
                                   healthCh (Infra/Transient DropEvents)
                                                 │
                                          healthWriter.accumulate() (in-memory)
                                          → finalize() ONCE after g.Wait()
                                          → cluster-health.json (atomic: tmp+rename)
```

Confirmed by reading `pkg/hubble/pipeline.go:150-404`, `pkg/output/writer.go:35-86`, `pkg/evidence/writer.go:33-89`, `pkg/hubble/health_writer.go:88-170`.

## System Overview — v1.5 addition

```
┌────────────────────────────────────────────────────────────────────────┐
│ MCP Host / LLM harness (external process)                               │
└───────────────┬─────────────────────────────────────────▲──────────────┘
                 │ stdin (JSON-RPC)                          │ stdout (JSON-RPC ONLY)
┌────────────────▼─────────────────────────────────────────┴──────────────┐
│ cpg mcp  (cmd/cpg/mcp.go, package main — NEW)                            │
│  - cobra command: SilenceUsage=true, SilenceErrors=true (see Anti-Pat.) │
│  - zap logger via existing buildLogger() → stderr (already the default) │
│  - MCP SDK stdio transport owns stdin/stdout exclusively                │
│                                                                          │
│  ┌─────────────────────────┐     ┌───────────────────────────────────┐ │
│  │ Session tools            │     │ Query tools (READERS ONLY)        │ │
│  │ start_session/status/    │     │ dropped_flows / policies /        │ │
│  │ stop_session             │     │ explain / cluster_health          │ │
│  └────────────┬─────────────┘     └───────────────┬───────────────────┘ │
│               │ calls                              │ calls              │
│  ┌────────────▼─────────────┐     ┌───────────────▼───────────────────┐ │
│  │ pkg/session (NEW)         │     │ pkg/output, pkg/hubble (health     │ │
│  │ SessionManager: ctx+cancel│     │ reader — NEW export), pkg/evidence │ │
│  │ wraps hubble.RunPipeline  │     │ (Reader — unmodified), pkg/explain │ │
│  │ UNMODIFIED entrypoint     │     │ (NEW, promoted from cmd/cpg)       │ │
│  └────────────┬─────────────┘     └───────────────┬───────────────────┘ │
│               │ PipelineConfig{Stdout: io.Discard, │ reads               │
│               │   OutputDir/EvidenceDir = tmpdir}  │                     │
└───────────────┼────────────────────────────────────┼─────────────────────┘
                 ▼                                    ▼
     pkg/k8s.PortForwardToRelay → Hubble Relay   session tmpdir (os.MkdirTemp)
     (unmodified)                                 ├── policies/<ns>/<wl>.yaml
                                                   ├── evidence/<hash>/<ns>/<wl>.json
                                                   └── evidence/<hash>/cluster-health.json
```

## Component Responsibilities

| Component | Responsibility | New / Modified / Unmodified |
|-----------|----------------|------------------------------|
| `cmd/cpg/mcp.go` (+ split files, e.g. `mcp_tools.go`) | Cobra command `cpg mcp`; MCP SDK transport wiring; translates tool-call JSON ↔ Go calls into `pkg/session` and the readers | **New** |
| `pkg/session` | Session lifecycle: `os.MkdirTemp`, `context.WithCancel`, launches `hubble.RunPipeline` in a goroutine, tracks single active session, cleanup on stop/shutdown | **New** |
| `pkg/explain` | `explainFilter`-equivalent + `renderText/JSON/YAML` promoted out of `cmd/cpg` so both `cpg explain` and `cpg mcp` can call them | **New** (promoted, precedent below) |
| `pkg/hubble` | Pipeline orchestration (`RunPipeline`, `RunPipelineWithSource`, `PipelineConfig`) | **Unmodified** — already general enough (see Pattern 1) |
| `pkg/hubble` health reader | Read + parse `cluster-health.json` with `SchemaVersion` gate | **Modified** (additive export) |
| `pkg/output` | Policy YAML write (existing) + new listing/read helper for the query tool | **Modified** (additive export) |
| `pkg/evidence` | `Reader`/`Writer`/schema for per-rule evidence | **Unmodified** — already fully parameterized (evidence dir + hash are plain args, nothing CLI-specific) |
| `pkg/k8s` | `LoadKubeConfig`, `PortForwardToRelay`, `RunL7Preflight`, `LoadClusterPoliciesForNamespaces` | **Unmodified** — reused exactly as `cmd/cpg/generate.go` uses them today |
| `pkg/flowsource`, `pkg/policy`, `pkg/labels`, `pkg/dedup`, `pkg/dropclass`, `pkg/diff` | Internal pipeline dependencies | **Unmodified** — fully encapsulated behind `RunPipelineWithSource`, MCP layer never touches them directly |

## New vs Modified Components — explicit list

**New files/packages:**
- `cmd/cpg/mcp.go` (+ optional `cmd/cpg/mcp_tools.go`) — cobra command + MCP tool-handler glue, `package main`
- `pkg/session/*.go` — `SessionManager`, `Session`, tmpdir + ctx-cancel lifecycle
- `pkg/explain/*.go` — promoted filter + render logic (mechanical move, see below)
- go.mod: one new dependency for the MCP Go SDK (library choice is STACK.md's call, not this file's; the *integration shape* — a `StdioTransport` owning stdin/stdout, wrapped for logging — is described here for grounding only)

**Modified files (additive, non-breaking):**
- `pkg/output/writer.go` — export a policy-listing helper that reuses the existing (currently unexported) `readExistingPolicy` parse path, so the query tool doesn't duplicate YAML-unmarshal-into-`ciliumv2.CiliumNetworkPolicy` logic
- `pkg/hubble/health_writer.go` (or a new sibling `health_reader.go` in the same package) — export `ClusterHealthReport`/`HealthDropJSON` (currently unexported `clusterHealthReport`/`healthDropJSON`, `pkg/hubble/health_writer.go:239-265`) plus a `ReadClusterHealth(path string)` function that checks `SchemaVersion` the same way `evidence.Reader.Read` does (`pkg/evidence/reader.go:36-42`)
- `cmd/cpg/explain.go`, `explain_filter.go`, `explain_render.go` — thinned; logic moves to `pkg/explain`, `cmd/cpg/explain.go` becomes a thin wrapper (mirrors `generate.go`'s relationship to `hubble.RunPipeline`)
- `cmd/cpg/main.go` — one line, `rootCmd.AddCommand(newMCPCmd())`, same pattern as the three existing `AddCommand` calls (`cmd/cpg/main.go:57-59`)

**Explicitly unmodified (zero changes, reused as-is):**
- `pkg/hubble/pipeline.go` — `PipelineConfig` already has every field the session manager needs (`Stdout io.Writer`, `Logger`, `OutputDir`, `EvidenceDir`, `OutputHash`, `SessionID`, `SessionSource`, `EvidenceCaps`, etc. — `pkg/hubble/pipeline.go:42-94`). The session manager is simply a **new caller** of `RunPipeline`/`RunPipelineWithSource`, constructed the same way `cmd/cpg/generate.go:225-256` already does it.
- `pkg/evidence/*` — `Reader`/`Writer` take `evidenceDir`/`outputHash` as plain constructor args; nothing assumes `$XDG_CACHE_HOME` or a CLI context.
- `pkg/k8s/*` — port-forward and kubeconfig loading are already transport-agnostic.

## Architectural Patterns

### Pattern 1: Session manager wraps the pipeline entrypoint unchanged; stop = ctx cancel

**What:** `RunPipeline`/`RunPipelineWithSource` (`pkg/hubble/pipeline.go:155-162`) is a **blocking** call built on `errgroup.WithContext(ctx)` (`pipeline.go:209`). Cancelling the context the caller passed in is *already* the shutdown mechanism `cpg generate`/`cpg replay` use for Ctrl+C: `signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)` + `defer cancel()` (`cmd/cpg/generate.go:162-163`). Cancellation propagates: the outer ctx is passed into `source.StreamDroppedFlows(ctx, ...)`; the live gRPC client's stream goroutine selects on `stream.Context().Done()` (`pkg/hubble/client.go:150-186`) and exits cleanly, closing `flows`/`lostEvents`, which drains through the aggregator and the Stage 1b tee, closing `policyCh`/`evidenceCh`/`healthCh` in the documented LIFO order (`pipeline.go:246-265`), letting every goroutine in the errgroup return and `g.Wait()` unblock.

**When to use:** `stop_session` should call the session's stored `context.CancelFunc` (from `context.WithCancel(baseCtx)` created at `start_session`), then wait — with a bounded timeout — on a `done chan error` that the goroutine running `RunPipeline` sends its return value to, before deleting the tmpdir. This reuses a shutdown path that already ships and is exercised (Ctrl+C on `cpg generate`), so it carries near-zero new risk.

**Trade-offs:** `RunPipeline` must be launched in its own goroutine by `start_session` (it never returns until cancelled), so `start_session` itself returns immediately after recording `cancel`/`done` — it does not await pipeline completion. `stop_session`'s bounded wait is a defensive addition (nothing in the existing pipeline can hang indefinitely today, but an MCP tool call must never block forever regardless).

**Example (shape, not literal implementation):**
```go
// pkg/session
type Session struct {
    ID        string
    TmpDir    string
    StartedAt time.Time
    cancel    context.CancelFunc
    done      chan error
}

func (m *Manager) Start(baseCtx context.Context, args StartArgs) (*Session, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.active != nil {
        return nil, ErrSessionAlreadyRunning // see Pattern-adjacent decision below
    }
    tmpDir, err := os.MkdirTemp("", "cpg-mcp-*")
    if err != nil { return nil, err }
    ctx, cancel := context.WithCancel(baseCtx)
    cfg := buildPipelineConfig(args, tmpDir) // mirrors cmd/cpg/generate.go:225-256
    cfg.Stdout = io.Discard                  // MUST — see Anti-Pattern 1
    s := &Session{ID: newID(), TmpDir: tmpDir, StartedAt: time.Now(), cancel: cancel, done: make(chan error, 1)}
    go func() {
        defer os.RemoveAll(tmpDir) // cleanup on every exit path, not just explicit stop
        s.done <- hubble.RunPipeline(ctx, cfg)
    }()
    m.active = s
    return s, nil
}
```

### Pattern 2: Reads split cleanly into "safe as-is" and "needs a defensive retry" by writer, not by artifact type

**What:** cpg already has two different write-safety guarantees on disk, and the query tools must know which is which:

| Artifact | Write mechanism | Concurrent-read safety |
|---|---|---|
| `evidence/<hash>/<ns>/<wl>.json` | `os.CreateTemp` in the same dir → write → close → `os.Rename` (`pkg/evidence/writer.go:70-88`) | **Safe.** POSIX `rename()` is atomic; a concurrent `evidence.Reader.Read()` (`pkg/evidence/reader.go:26-44`) always sees a complete pre- or post-write file, never a torn one. No new code needed. |
| `evidence/<hash>/cluster-health.json` | Same temp+rename pattern, but **written exactly once**, only after `g.Wait()` returns (`pipeline.go:344`, `health_writer.go:144-162`) | **Safe from tearing, but absent-by-design during an active session.** For a long-running MCP session this file will not exist at all until `stop_session`. The query tool must treat `os.IsNotExist` as "session still capturing," not an error. |
| `policies/<ns>/<wl>.yaml` | Direct `os.WriteFile(path, data, 0644)` — **no temp+rename** (`pkg/output/writer.go:35-86`, write at line 81) | **Not safe against tearing.** A read landing mid-write (especially the read-merge-write update path for an existing workload, lines 47-68) can observe a truncated or partially-overwritten file. |

**When to use:** The "generated policies" query tool must defensively retry-on-parse-error (read → YAML-unmarshal fails → wait a short backoff → retry once or twice → surface a "policy file is being updated, try again" result rather than a hard error). This is a **read-side accommodation only** — it does not touch `pkg/output/writer.go`. Changing the writer to atomic temp+rename would be a real, low-risk improvement, but it is a modification to existing write behavior that this milestone's scope ("integrate, don't redesign") does not call for; flag it for the roadmap/PITFALLS track instead of doing it here.

**Trade-offs:** The cluster-health "absent until stop_session" behavior is the most consequential finding in this research for the `status`/`cluster_health` tools — see Open Questions below; it is a design gap in the *current* pipeline, not something a purely additive MCP layer can paper over without either (a) accepting coarse/absent live health data, or (b) a small, explicit pipeline change (periodic flush) that the requirements/roadmap phase should decide on deliberately.

### Pattern 3: Promote `cmd/cpg` presentation logic to an importable package — direct precedent exists

**What:** `cmd/cpg` is `package main`; nothing under it is importable by a new `pkg/session`/`cmd/cpg/mcp_tools.go` caller. PROJECT.md's Key Decisions table already documents this exact move once: `FlowSource` was promoted from `cmd/` into `pkg/flowsource` in v1.1 specifically "to decouple replay (file) from live (gRPC); testable without Hubble" — the stated reason was **a second caller appeared**. v1.5 creates a second caller for explain's filter+render logic (`cpg explain` CLI + the new MCP explain/evidence query tool), which is the same trigger condition.

Critically, the render functions are **already** decoupled from stdout: `renderText(w io.Writer, ...)`, `renderJSON(w io.Writer, ...)`, `renderYAML(w io.Writer, ...)` (`cmd/cpg/explain_render.go:28,133,140`) all take an `io.Writer` as their first parameter. The *only* stdout coupling in the explain path is the caller's choice at `cmd/cpg/explain.go:97` (`out := cmd.OutOrStdout()`). Same for the filter: `explainFilter.match()` (`cmd/cpg/explain_filter.go:31-85`) depends only on `pkg/evidence` — zero cobra coupling.

**When to use:** Move `explainFilter` + `renderText`/`renderJSON`/`renderYAML` (and, if the MCP tool wants "explain by policy YAML path" in addition to "explain by namespace/workload," `resolveFromYAML` from `explain_target.go:30-57`) into a new `pkg/explain` package, verbatim. `cmd/cpg/explain.go`'s `runExplain` keeps building `explainFilter` from `cmd.Flags()` (cobra-specific) and keeps calling `cmd.OutOrStdout()`; the new MCP tool handler builds the same filter type from MCP JSON tool-call arguments and passes a `bytes.Buffer` instead. Both converge on the same `pkg/explain` core, `evidence.NewReader(evDir, hash)` unchanged.

**Trade-offs:** This is a mechanical, low-risk move (no logic changes, just package boundary + import fixes), but it is the one place existing files (`cmd/cpg/explain.go`, `explain_filter.go`, `explain_render.go`, and their `_test.go` siblings) must be touched — do it early so `cpg explain`'s existing test coverage proves nothing broke before building the MCP tool on top of it.

## Data Flow

### Write flow (unchanged — session manager is a new caller, not a new writer)

```
start_session ──► pkg/session.Manager.Start()
                      │ builds PipelineConfig{OutputDir: <tmp>/policies,
                      │   EvidenceDir: <tmp>/evidence, Stdout: io.Discard, ...}
                      ▼
              hubble.RunPipeline(ctx, cfg)   ← UNMODIFIED, same function generate.go calls
                      │
                      ▼ (existing pipeline, existing writers — see System Overview)
              <tmp>/policies/**.yaml, <tmp>/evidence/**.json, <tmp>/evidence/**/cluster-health.json
```

### Read flow (all new — query tools never touch pipeline internals, only the tmpdir)

```
query tool call ──► cmd/cpg/mcp_tools.go handler
                        │ looks up active *pkg/session.Session for TmpDir
                        ▼
        ┌───────────────┼────────────────────┬───────────────────────┐
        ▼               ▼                    ▼                       ▼
 pkg/output          pkg/evidence.Reader  pkg/hubble.ReadClusterHealth pkg/explain
 (list+parse          (unmodified,         (NEW export, SchemaVersion  (NEW, filter+render
  policies/**.yaml,    Read(ns, wl))        gated like evidence)        over evidence.Reader)
  retry-on-parse-err)
```

### Key data flows

1. **Session start:** MCP `start_session` tool-call → `pkg/session.Manager.Start` → `os.MkdirTemp` → (optional) `k8s.LoadKubeConfig` + `k8s.PortForwardToRelay` (unmodified, same as `generate.go:166-182`) → `go hubble.RunPipeline(ctx, cfg)` → tool call returns session metadata immediately (does not block on the pipeline).
2. **Query during an active session:** tool-call → resolve session's `TmpDir` → dispatch to the matching reader (evidence, output listing, or health) → for policies, apply retry-on-transient-parse-error (Pattern 2); for cluster-health, return an explicit "not available yet — session still capturing" result when the file does not exist rather than an error.
3. **Session stop:** MCP `stop_session` tool-call → `cancel()` the session's context → wait (bounded) on the `done` channel → `os.RemoveAll(TmpDir)` (already deferred in the launch goroutine, so this is a safety net, not the primary cleanup path) → clear `Manager.active`.
4. **Process shutdown:** `cmd/cpg/mcp.go`'s root context is built with the same `signal.NotifyContext(..., os.Interrupt, syscall.SIGTERM)` pattern already used by `generate`/`replay`; the session's context is a child of it, so a SIGTERM to the `cpg mcp` process cancels any active session automatically, and the `defer os.RemoveAll(tmpDir)` in the launch goroutine still fires.

## Suggested Build Order

Ordered by actual dependency structure, not by tool-list order.

1. **Stdio-safety skeleton first (blocks nothing else, but must be proven before anything is layered on top).** `cmd/cpg/mcp.go`: register the command in `main.go`, set `SilenceUsage`/`SilenceErrors` (see Anti-Pattern 1), reuse `buildLogger()` unchanged (already stderr-safe — verified below), stub `RunE` that starts the chosen SDK's stdio transport with zero tools registered and exits cleanly on stdin close. Manually verify byte-for-byte that stdout carries nothing but what the SDK writes. This is cheap and de-risks everything downstream.
2. **`pkg/session` (independent of the MCP SDK and of the query tools).** Build `SessionManager` wrapping `hubble.RunPipeline` exactly as `cmd/cpg/generate.go:145-257` already does, targeting `os.MkdirTemp`. Unit-test with `hubble.RunPipelineWithSource` + a fake `flowsource.FlowSource` (same technique the existing `pkg/hubble/pipeline_test.go` already uses) — no real cluster, no MCP SDK required. This is the highest-novelty, highest-concurrency-risk piece; prove it in isolation.
3. **Read-side extensions, parallelizable with step 2 (each depends only on an existing package):**
   - 3a. `pkg/output` — export the policy-listing helper.
   - 3b. `pkg/hubble` — export `ClusterHealthReport` + `ReadClusterHealth`.
   - 3c. Promote `pkg/explain` (Pattern 3) — do this before or alongside 3a/3b since it touches existing `cmd/cpg` files and their tests; run the existing `cpg explain` test suite immediately after to confirm the move was mechanical.
   - 3d. "Dropped flows" projection (composes 3b's health snapshot + `pkg/evidence.Reader` samples) — sequenced after 3b since it depends on it. **Flag for requirements, not solved here:** neither evidence samples (FIFO-capped, attached only to policy-worthy rules) nor the health snapshot (aggregate counts, Infra/Transient only, no per-flow detail — `pkg/hubble/aggregator.go:21-27`'s `DropEvent` carries no timestamp/port/verdict) add up to a complete raw flow log. Decide during requirements whether the composed view is sufficient or whether a new minimal flow-sample writer (a 4th tee target alongside `policyCh`/`evidenceCh`/`healthCh`) is needed.
4. **MCP tool wiring (`cmd/cpg/mcp_tools.go`), depends on steps 2 and 3.** Register session tools against `pkg/session.Manager`; register query tools against the readers from step 3, each scoped to the active session's `TmpDir`. Replace the step-1 stub.
5. **End-to-end stdio validation, last.** Drive `cpg mcp` through initialize → start_session → status → each query tool → stop_session → process exit, asserting stdout contains only valid JSON-RPC frames throughout — this is where the `PipelineConfig.Stdout` default, the `diffOut` default, and the cobra `SilenceUsage` gotcha (Anti-Pattern 1) all get proven together. Extend `-race` to the new packages, consistent with the existing "484 tests passing with `-race`" discipline (`.planning/PROJECT.md` Current State).

## Scaling Considerations

Not a multi-user web service; the relevant axis is capture duration and query load against a single tmpdir, not concurrent users.

| Scale | Behavior |
|---|---|
| Short session (single query, minutes) | Everything above holds with no adjustment; evidence FIFO caps (`--evidence-samples`/`--evidence-sessions`, already tunable) bound growth exactly as they do for `cpg generate` today. |
| Long-running session (hours, left open by the LLM harness) | Policy/evidence file counts stay bounded by distinct namespace/workload pairs observed (cluster-size-bound, not runaway). The one thing that degrades with session duration is **cluster-health staleness** (Pattern 2): the file simply does not exist until `stop_session`, so a `status`/`cluster_health` query an hour into a session returns nothing today — this is the primary "what breaks first" for this integration, and it is a design gap, not a volume problem. |
| High flow-volume cluster (many namespaces, high drop rate) | Already handled by the existing aggregator's flush interval and channel buffering (`policies`/`policyCh`/`evidenceCh`/`healthCh` are all buffered 64, `pipeline.go:188-191`) — the MCP layer adds a reader on the side, it does not sit in this hot path at all, so it cannot become a new bottleneck for ingestion. |

## Anti-Patterns to Avoid

### Anti-Pattern 1: Assuming "reuse the existing logger" is sufficient for stdio safety

**What people might do:** Reuse `buildLogger()` as-is and assume the stdio channel is safe because zap "usually" goes to stderr.

**Why it's wrong:** It's true but incomplete. Verified directly against the pinned versions in `go.mod` via `go doc`: `zap.NewProductionConfig()`, `zap.NewDevelopmentConfig()`, and `zap.NewDevelopment()` **all** default their output to standard error — so `cmd/cpg/main.go`'s `buildLogger()` (lines 71-102) is already stdio-safe in every branch (`--json`, `--debug`, default), and this needs no change. But there are **three other, unrelated stdout writers already in the codebase** that a `cpg mcp` session would exercise and that zap's defaults do nothing to fix:
1. `PipelineConfig.Stdout` defaults to `os.Stdout` when `nil` (`pkg/hubble/pipeline.go:356-359`) — the session-summary block. The session manager **must** explicitly set `Stdout: io.Discard`.
2. `policyWriter.diffOut` defaults to `os.Stdout` when `nil` (`pkg/hubble/writer.go:35,129-133`) — only triggered when `DryRun` is true; the session manager must never set `DryRun: true` for a live MCP session (defense in depth: also explicitly set the field if a preview mode is ever added).
3. Cobra itself: on any flag/arg error, `Command.ExecuteC()` calls `c.Println(cmd.UsageString())` — which goes to `OutOrStdout()`, i.e. **stdout**, by default — unless `SilenceUsage` is set (confirmed via `go doc -src github.com/spf13/cobra.Command.ExecuteC` against the pinned v1.10.2). None of the three existing commands (`generate`, `replay`, `explain`) set `SilenceUsage`/`SilenceErrors` today (fine for them — stdout is human text anyway). `cmd/cpg/mcp.go` **must** set both, or a single mistyped flag on startup dumps a usage string onto the JSON-RPC stream before the transport loop even begins.

**Do this instead:** Treat "nothing but valid MCP messages on stdout" as an invariant enforced at three independent points (zap config, `PipelineConfig.Stdout`/`diffOut`, cobra `Silence*`), not one. This exact constraint is spec, not convention: *"The server MUST NOT write anything to its stdout that is not a valid MCP message... The server MAY write UTF-8 strings to its standard error (stderr) for logging purposes."* — [MCP stdio transport spec](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports).

### Anti-Pattern 2: Reusing cobra `RunE` functions directly as MCP tool handlers

**What people might do:** Call `runExplain(cmd, args)` directly from an MCP tool handler by fabricating a `cobra.Command`.

**Why it's wrong:** `runExplain` reads flags via `cmd.Flags().GetString(...)` and writes via `cmd.OutOrStdout()` (`cmd/cpg/explain.go:52-114`) — coupling the logic to cobra's flag/writer plumbing for no reason, and reintroducing the stdout risk from Anti-Pattern 1.

**Do this instead:** Call the promoted `pkg/explain` core (Pattern 3) directly with a `bytes.Buffer`; build the filter from MCP tool-call JSON arguments instead of `cmd.Flags()`.

### Anti-Pattern 3: Silently widening scope to "fix" the writer races

**What people might do:** Notice the non-atomic `pkg/output/writer.go` write (Pattern 2) and "fix" it by adding temp+rename while building the MCP layer.

**Why it's wrong:** It's a real, legitimate improvement, but it's a behavior change to code three prior milestones' worth of tests depend on, unrelated to "integrate the MCP layer," and explicitly out of this milestone's stated scope (existing architecture is not being redesigned).

**Do this instead:** Handle it on the read side only (retry-on-parse-error in the new query tool), and record the writer-hardening idea as a PITFALLS/roadmap candidate, not something this integration silently does.

### Anti-Pattern 4: Defaulting to a multi-session model because "MCP servers should handle concurrent clients"

**What people might do:** Build a session-ID-keyed registry supporting N concurrent captures from the start, reasoning that MCP servers in general should be stateless/concurrent-safe.

**Why it's wrong for this milestone:** The milestone's own tool names — `start_session` / `status` / `stop_session`, no session-id parameter — describe a single-session model, matching the stated goal ("run **a** live Hubble capture session"), the "flat memory profile" constraint, and the existing CLI's single-shot mental model (`cpg generate` is one process, one capture, package-level `var logger *zap.Logger` and `var version` are process-wide singletons with no existing concurrency precedent — `cmd/cpg/main.go:16,19`). Nothing in `pkg/k8s.PortForwardToRelay` technically blocks a second concurrent forward, but building for it now is speculative complexity against a spec that doesn't ask for it.

**Do this instead:** `pkg/session.Manager` holds one `*Session` (nil when idle) guarded by a mutex; `start_session` while a session is already active returns an explicit error ("session already running; call stop_session first") rather than silently discarding in-flight capture data. Flag single-vs-multi-session explicitly as a requirements decision (not fully spelled out in PROJECT.md today) rather than assuming either answer silently.

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| Hubble Relay (gRPC, via `pkg/hubble.Client`) | Reused unmodified: `hubble.NewClient` + `k8s.PortForwardToRelay` when `--server` is not given, exactly as `cmd/cpg/generate.go:166-182` does it today | No new integration surface; the session manager is a new *caller*, not a new client. |
| MCP Host / LLM harness | stdio transport (spec-mandated newline-delimited JSON-RPC on stdin/stdout, logging on stderr) | Library choice for the Go-side SDK is STACK.md's call; the *shape* — a transport object owning stdin/stdout, wrapped separately for stderr logging — matches the ecosystem's `StdioTransport` + `LoggingTransport` pattern (MEDIUM confidence, WebSearch-derived from `github.com/modelcontextprotocol/go-sdk`, illustrative only). |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| `cmd/cpg/mcp.go` ↔ `pkg/session` | Direct Go calls (`Start`/`Status`/`Stop`) | Same shape as `cmd/cpg/generate.go` ↔ `pkg/hubble` today. |
| `cmd/cpg/mcp.go` ↔ `pkg/output`, `pkg/hubble` (health), `pkg/evidence`, `pkg/explain` | Direct Go calls, all reads scoped to `session.TmpDir` | Query tools never share in-memory pipeline state — filesystem is the only channel, matching the stated "no parallel in-memory path" goal. |
| `pkg/session` ↔ `pkg/hubble` | `hubble.RunPipeline(ctx, cfg)` — the one call site | Zero new API surface required on `pkg/hubble`'s write path. |
| New `pkg/session`/SDK glue naming | — | Avoid naming cpg's own package `pkg/mcp`: the official Go SDK's own package is *also* named `mcp` (`github.com/modelcontextprotocol/go-sdk/mcp`), which would force an import alias everywhere both are used. Naming the domain package `pkg/session` sidesteps this for free; only `cmd/cpg/mcp.go` (package `main`) ever imports the SDK's `mcp` package, with no collision. |

## Open Questions for Requirements/Roadmap

These are genuine gaps surfaced by reading the code, not resolved here — they need an explicit decision, not a silent default:

1. **Live `status`/`cluster_health` during an active session.** `cluster-health.json` is a finalize-only artifact (Pattern 2) — it does not exist until `stop_session`. Decide: ship v1.5 with "not available until stop" as the documented behavior, or accept a small additive `pkg/hubble` change (periodic flush, or exposing `*SessionStats` via an optional hook on `PipelineConfig`) as in-scope.
2. **Numeric live status (flows seen, policies written so far).** `SessionStats` (`pipeline.go:97-128`) is built and only logged once, at the very end (`stats.Log(cfg.Logger)`, `pipeline.go:394`) — there is no API today for a caller to peek at counters while the pipeline runs. Coarse status (artifact file counts on disk, session running/stopped state) is achievable with zero pipeline changes; true live counters are not, without a small additive hook.
3. **"Dropped flows" query tool's data source.** No existing writer produces a complete raw flow log — only policy-attributed evidence samples (capped) and Infra/Transient aggregate counts exist on disk today (build-order step 3d). Decide whether the composed view is sufficient for v1.5 or whether a new minimal flow-sample writer is warranted.
4. **Single-session vs multi-session** (Anti-Pattern 4) — recommended single-session, needs explicit confirmation in REQUIREMENTS.md.

## Sources

- `pkg/hubble/pipeline.go` (full file read) — pipeline orchestration, `PipelineConfig`, `Stdout` default, `SessionStats`
- `pkg/hubble/writer.go`, `pkg/hubble/health_writer.go`, `pkg/hubble/evidence_writer.go`, `pkg/hubble/client.go` — write paths and ctx-cancel propagation
- `pkg/output/writer.go` — non-atomic policy write path
- `pkg/evidence/reader.go`, `writer.go`, `paths.go`, `schema.go` — atomic write, parameterized reader
- `pkg/k8s/portforward.go`, `client.go` — reused connectivity helpers
- `pkg/flowsource/source.go` — `FlowSource` interface (test-injection precedent)
- `cmd/cpg/main.go`, `generate.go`, `replay.go`, `explain.go`, `explain_render.go`, `explain_filter.go`, `explain_target.go`, `commonflags.go` — existing cobra wiring, `buildLogger`, stdout coupling points
- `go.mod` — pinned versions (`go.uber.org/zap v1.27.1`, `github.com/spf13/cobra v1.10.2`, `go 1.25.1` / `toolchain go1.25.12`); no MCP SDK dependency present yet
- `go doc go.uber.org/zap.{NewProductionConfig,NewDevelopmentConfig,NewDevelopment}` (pinned v1.27.1) — confirmed default output is stderr in all three constructors
- `go doc -src github.com/spf13/cobra.Command.ExecuteC` / `.PrintErrln` (pinned v1.10.2) — confirmed usage-on-error prints to stdout unless `SilenceUsage` is set
- [MCP stdio transport specification](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports) — HIGH confidence, exact current spec text: "The server MUST NOT write anything to its stdout that is not a valid MCP message" / "The server MAY write UTF-8 strings to its standard error (stderr) for logging purposes"
- WebSearch: `github.com/modelcontextprotocol/go-sdk` `StdioTransport`/`LoggingTransport` pattern — MEDIUM confidence, illustrative of ecosystem convention only, not a library recommendation (that's STACK.md's scope)
- `.planning/PROJECT.md` — Key Decisions table (`FlowSource` promotion precedent, "Domain-driven pkg/ structure" constraint), Current Milestone target features, Constraints section

---
*Architecture research for: cpg v1.5 MCP server integration*
*Researched: 2026-07-20*
