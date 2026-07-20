# Phase 17: Session Lifecycle - Research

**Researched:** 2026-07-20
**Domain:** Go concurrency / process-lifecycle management for a new `pkg/session` package wrapping cpg's existing streaming pipeline behind 3 MCP tools (`start_session`/`get_status`/`stop_session`)
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Session state machine & post-stop retention (resolves the SESS-06 ↔ QRY-04 tension)**
- **D-01:** State machine is `capturing → stopped → gone`. `stop_session` finalizes artifacts and **RETAINS** the tmpdir — removal happens at the **next `start_session`** or at **server shutdown**, never at stop. A stopped session stays queryable: `get_status` now, Phase 18 query tools read policies/evidence/cluster-health cold. This supersedes PROJECT.md's "tmpdir cleaned at stop_session" wording.
- **D-02:** SESS-06's "session not found or expired" applies to **unknown IDs and replaced/purged sessions only** — NOT to the retained stopped session. Binding interpretation for Phase 18: QRY-04's "available after stop_session" works naturally against the retained tmpdir.
- **D-03:** `stop_session` is **idempotent**: a second stop returns the same final summary with an "already stopped" marker, never `isError`. Harness retries after timeouts must not surface parasitic failures.
- **D-04:** `start_session` while a stopped session is retained: **silent purge + note** — the new start succeeds, removes the old tmpdir, and the response notes "previous session sess_X discarded". The old ID becomes "not found or expired". SESS-02's rejection applies only to an ACTIVE (capturing) session.

**start_session argument surface (resolves SESS-01's "existing generate filters")**
- **D-05:** Exposed args: `namespace`/`all_namespaces` (SESS-01), `l7`, `ignore_drop_reasons`, `ignore_protocols`, `server`, `tls`, `timeout`, `cluster_dedup` (default false), `flush_interval`. Evidence caps stay at binary defaults. Excluded as nonsensical in MCP mode: `--dry-run`, `--no-evidence`, `--output-dir`, `--fail-on-infra-drops`.
- **D-06:** Reuse the existing CLI validations verbatim (`validateIgnoreDropReasons`, `validateIgnoreProtocols` in `cmd/cpg/commonflags.go`) — already tested, same normalization (UPPERCASE reasons, lowercase protocols).
- **D-07:** `server` bypasses the auto port-forward (same semantics as the CLI flag). Side benefit: Phase 19's SRV-04 e2e test can point `start_session` at a local fake gRPC server without kubeconfig. `timeout` (default 10s) bounds connection establishment so a bad relay makes `start_session` fail fast with an actionable `isError`.

**stop_session final summary**
- **D-08:** Complete `SessionStats` reach the session manager via a **small additive nil-safe hook on `PipelineConfig`** (e.g. `OnFinal func(SessionStats)`), fired exactly once after `g.Wait()` when stats are fully populated; nil = no-op, CLI paths untouched. This is NOT LIVE-01 (zero mid-session counters — end-of-run only). Rationale: `cluster-health.json` only persists `flows_seen`/`infra_drops_total`/`started`/`ended` — `PoliciesWritten/Skipped/Failed`, `LostEvents`, and L7 counts are otherwise unrecoverable from disk.
- **D-09:** stop_session result = **typed `structuredContent` only** (SessionStats-derived struct + absolute `cluster-health.json` path + tmpdir path). The human `PrintClusterHealthSummary` block stays on **stderr** via `mcpModeStdout()` — Phase 16's handoff honored literally, no per-session buffer seam.

**session_id**
- **D-10:** MCP `session_id` = opaque **`sess_<uuid>`**, mapped by the Manager to the session. The internal evidence `SessionID` keeps its existing `RFC3339-uuid4` format (evidence schema v2 untouched). The start log line carries both IDs for correlation.

### Claude's Discretion
- Exact SESS-05 per-step cleanup deadline values (bounded, one wedged step never blocks exit — pick sensible constants).
- `get_status` artifact file-count semantics (which files counted: policy YAML, evidence, health) and response field names.
- `pkg/session` file layout and Manager API shape (research Pattern 1 is the reference).
- Port-forward/kubeconfig failure error texts at `start_session` (distinct, actionable, per PITFALLS Pitfall 8).
- Field names of the stop-summary struct (`outputSchema` discipline arrives formally with QRY-05 in Phase 18; stay consistent with it).

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within phase scope. (LIVE-01 live counters, FLOW-01 flow-sample writer, REDACT-01 redaction were already tracked as v2 requirements before this discussion.)
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SESS-01 | `start_session` (namespace/all-ns + generate filters) returns opaque `session_id`; pipeline runs in a background goroutine on a detached cancellable context, writing to an ephemeral `os.MkdirTemp` tmpdir | Pattern 1 (state machine), Pattern 2 (context provenance), Pattern 5 (PipelineConfig recipe), Code Example 1-2, Pitfall A/C/H/I |
| SESS-02 | Exactly one concurrent session: second `start_session` while active is rejected with an actionable error naming the active `session_id` | Pattern 1, Code Example 2 (`Manager.Start`), reconciled explicitly with D-04's stopped-session purge (NOT a rejection case) |
| SESS-03 | `get_status(session_id)` returns coarse state (capturing/stopped), elapsed time, artifact file counts — no live pipeline counters | Pattern 1 (stopped sessions stay queryable — pure filesystem read, zero live resources), Code Example 4, Validation Architecture map |
| SESS-04 | `stop_session(session_id)`: ctx cancelled, artifacts finalized (`cluster-health.json`, session stats), final summary returned | Pattern 1, Pattern 4 (`OnFinal` hook — the one `pkg/hubble` change), Code Example 3 (`Manager.Stop`), Pitfall E/F/G |
| SESS-05 | Transport termination (any reason) triggers cancel + port-forward close + tmpdir removal, each step bounded so one wedge never blocks process exit | Pattern 3 (bounded shutdown fan-out), Code Example 5 (`runMCPServer` wiring), Pitfall D, Validation Architecture (ungraceful-disconnect test) |
| SESS-06 | Unknown/expired `session_id` on any session-scoped tool → crisp "not found or expired" error, never generic failure | Pattern 0 (go-sdk error-as-`isError` idiom), Pattern 1 (D-02's literal-requirements-vs-locked-decision reconciliation — **read this first**), Code Example 4 |
</phase_requirements>

## Summary

Phase 17 adds exactly one new package (`pkg/session`) and one additive `pkg/hubble` change (a nil-safe `OnFinal` hook on `PipelineConfig`, D-08) — everything else is composition-root glue in `cmd/cpg/mcp_tools.go` calling code that already exists and is already tested (`generate.go`'s connection recipe, `commonflags.go`'s validators, `k8s.PortForwardToRelay`). **Zero new `go.mod` entries** — every dependency `pkg/session` needs (`go-sdk`, `google/uuid`, `zap`, `client-go`) is already a direct, pinned dependency, verified live against this repo's `go.mod`/`go.sum` this session.

The two things worth getting exactly right, in order of consequence:

1. **The state machine is `capturing → stopped → gone` with retention (D-01), not the "clean up at stop" model the milestone-level `ARCHITECTURE.md` illustrated.** Its Pattern 1 sample code (`defer os.RemoveAll(tmpDir)` unconditionally inside the launch goroutine) predates Phase 17's CONTEXT.md discussion and is now **superseded** — a literal port of that sample would delete the tmpdir at `stop_session`, breaking `get_status` on a stopped session (SESS-03) and Phase 18's QRY-04 outright. Similarly, **REQUIREMENTS.md's own SESS-06 text** ("unknown or stopped session_id") is superseded by CONTEXT.md's D-02: the error applies to unknown/purged IDs only, never to a retained stopped session. Both corrections are load-bearing for this phase — see Pattern 1.
2. **The session's background context must be a child of the MCP server's root/signal context, not derived from the tool-call's request context.** Verified directly against the pinned `go-sdk` v1.6.1 source (`mcp/server.go:1485-1491`): "cancellation is handled [in] the jsonrpc2 package... cancellation is preempted" — a tool handler's incoming `ctx` can be cancelled independently of the session's intended lifetime. But it must still be a *descendant* of the server's `signal.NotifyContext`-derived root ctx so SIGTERM propagates automatically (CONTEXT.md code_context: "detached from the tool-call request ctx, yet a child of the server's ctx"). Concretely: `pkg/session.Manager` stores the server-root ctx once at construction and forks `sessionCtx := context.WithCancel(m.rootCtx)` from *that*, never from the handler's per-call `ctx` parameter.

A third, code-verified, non-obvious finding with real crash potential: **`flush_interval` left at its Go zero-value panics.** `pkg/hubble/aggregator.go:365` calls `time.NewTicker(a.interval)` with zero validation upstream, and `time.NewTicker` panics for `d <= 0`. An MCP client omitting the (optional, per D-05) `flush_interval` argument sends nothing; if the handler forwards that straight into `PipelineConfig.FlushInterval` it is Go's zero value (`0`), and the aggregator's `errgroup.Go` goroutine panics — which brings down the whole `cpg mcp` process, not just the one tool call. `timeout` has a related but softer failure mode: `pkg/hubble/client.go:70` only bounds the gRPC dial when `c.timeout > 0`; a zero timeout silently *removes* the bound D-07 explicitly asks for. Both must be explicitly defaulted (10s / 5s, matching the CLI's own flag defaults) before reaching `PipelineConfig` — see Pitfall A.

**Primary recommendation:** build `pkg/session.Manager` as a single mutex-guarded `*Session` slot (nil = idle) implementing the `capturing → stopped → gone` state machine verbatim per D-01..D-04, forking the background pipeline goroutine from a server-rooted (not request-scoped) context, with an explicit synchronous `Shutdown()` fan-out called from `cmd/cpg/mcp.go` immediately after `server.Run(...)` returns — not relying on ctx-propagation alone to guarantee cleanup completes before process exit.

## Architectural Responsibility Map

cpg has no browser/frontend tier — it is a single Go binary acting as both the MCP protocol server and the domain engine. Tiers below are adapted from that reality; the analytical goal (catch a capability landing in the wrong layer) is unchanged.

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Tool-call JSON-RPC framing, arg validation reuse | MCP Transport (`cmd/cpg/mcp*.go`) | — | Composition root; only place allowed to call `commonflags.go`'s unexported validators (D-06) and the go-sdk |
| Single-active-session enforcement, `session_id` issuance/lookup, state machine | Session Orchestration (`pkg/session`) | — | New business logic this phase; independent of MCP protocol details (unit-testable without the SDK, per SUMMARY.md build order) |
| Kubeconfig load, port-forward establishment, cluster-dedup snapshot | Session Orchestration (calls into) | External Services (Hubble Relay / K8s API) | `pkg/k8s` is transport-agnostic and unmodified; Session Orchestration owns *when* and *bounded-by-what-timeout* it's called (new: Pitfall H) |
| Live flow capture, aggregation, policy/evidence/health generation | Pipeline Engine (`pkg/hubble`, `pkg/output`, `pkg/evidence`) | — | Completely unmodified except the D-08 `OnFinal` hook; Session Orchestration is a new *caller*, not a new writer |
| Session status (elapsed, artifact counts), stop-summary assembly | Session Orchestration | Ephemeral Storage (session tmpdir) | Coarse status is a filesystem read + in-memory state snapshot, never touches pipeline internals directly |
| Cleanup fan-out on transport death / SIGTERM | MCP Transport (invokes, after `server.Run` returns) | Session Orchestration (executes: cancel + bounded wait + tmpdir removal) | The trigger point (transport returning) is a go-sdk/transport concern; the actual teardown logic belongs to Session Orchestration so it's unit-testable without a real transport |
| Ephemeral artifact persistence | Ephemeral Storage (`os.MkdirTemp` session tmpdir) | — | Stands in for a "database" tier here — the only channel between write side (pipeline) and read side (Phase 18 query tools), per milestone ARCHITECTURE.md |

## Standard Stack

### Core — zero new dependencies

Every import `pkg/session` needs is already a direct dependency, verified live this session (`go list -m ...` against this repo's actual `go.mod`):

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/modelcontextprotocol/go-sdk/mcp` | v1.6.1 | `AddTool`/`ToolHandlerFor` for the 3 session tools; consumed only by `cmd/cpg/mcp*.go` (package `main`), never by `pkg/session` itself (naming-collision rule from Phase 16) | Already landed Phase 16; Tier-1 official SDK (STACK.md) |
| `github.com/google/uuid` | v1.6.0 | `sess_<uuid>` generation (D-10) | Already a direct dep (`generate.go`'s internal `SessionID`); `rander = crypto/rand.Reader` by default — **verified** via direct source read (`uuid.go:9,40`, `version4.go:27`) — cryptographically unpredictable, correct for a session handle |
| `go.uber.org/zap` | v1.27.1 | `pkg/session`'s logger field, same convention as every other `pkg/*` package | Project's sole logging library |
| `k8s.io/client-go` (`rest` package only) | v0.35.4 | `*rest.Config` type flowing through `k8s.LoadKubeConfig`/`PortForwardToRelay` | Already a direct dep; `pkg/session` never imports client-go beyond this type |

No supporting-library table: `pkg/session`'s single-goroutine-per-session shape needs no coordination library beyond a plain buffered `chan error` — see Alternatives Considered.

### Alternatives Considered

| Instead of | Could use | Tradeoff |
|------------|-----------|----------|
| Plain `done := make(chan error, 1)` + `select`/`time.After` for the bounded stop-wait | `golang.org/x/sync/errgroup` (STACK.md's generic suggestion for "the session's background-goroutine lifecycle") | `errgroup` earns its keep coordinating *multiple* related goroutines with shared cancellation — exactly what `pipeline.go` already does *internally*. `pkg/session` only ever launches **one** goroutine per session; a buffered channel is simpler, equally correct, and avoids an import that adds no value here. Recommend against reaching for `errgroup` in `pkg/session` itself. |
| `timeout`/`flush_interval` as plain string args, parsed via `time.ParseDuration` + `> 0` validation | `time.Duration` struct fields (Go's "obvious" choice) | **Verified by direct execution** against the pinned `jsonschema-go` v0.4.3 (see Pitfall B): `time.Duration` infers as bare `{"type": "integer"}` with no unit — an LLM would have to compute nanoseconds, and `"10s"` fails to unmarshal outright (`json: cannot unmarshal string into Go struct field ... of type time.Duration`). String + `ParseDuration` mirrors cpg's own CLI duration-flag UX (`--timeout 10s`) and is the only LLM-usable shape. |
| Single `*Session` field on `Manager` (nil = idle) | `map[string]*Session` multi-session registry | ARCHITECTURE.md Anti-Pattern 4 and REQUIREMENTS.md's explicit "Multi-session capacity" Out-of-Scope entry both reject this; building it now is speculative complexity against an unrequested capability. |

**Installation:** none. Verified this session:
```
$ go list -m github.com/google/uuid github.com/modelcontextprotocol/go-sdk go.uber.org/zap go.uber.org/zap/exp k8s.io/client-go
github.com/google/uuid v1.6.0
github.com/modelcontextprotocol/go-sdk v1.6.1
go.uber.org/zap v1.27.1
go.uber.org/zap/exp v0.3.0
k8s.io/client-go v0.35.4
```

## Package Legitimacy Audit

**Not triggered — Phase 17 installs no new external packages.** Every dependency `pkg/session` imports is already a direct entry in `go.mod`, already vetted by Phase 16's STACK.md research and already exercised in production (`cmd/cpg/generate.go`). No `slopcheck`/registry-verification run was performed because there is nothing new to verify.

| Package | Registry | Disposition |
|---------|----------|-------------|
| `github.com/modelcontextprotocol/go-sdk` | Go modules | Pre-existing (Phase 16) — no action |
| `github.com/google/uuid` | Go modules | Pre-existing (v1.0-era) — no action |
| `go.uber.org/zap` | Go modules | Pre-existing (v1.0-era) — no action |
| `k8s.io/client-go` | Go modules | Pre-existing (v1.0-era) — no action |

If a later revision of this phase's plan needs a package not listed here, re-run the full Package Legitimacy Gate before adding it to `go.mod`.

## Architecture Patterns

### System Architecture Diagram

```
                        MCP Host / LLM Client (stdio, JSON-RPC)
                                       │
                 tools/call: start_session | get_status | stop_session
                                       │
                                       ▼
                cmd/cpg/mcp_tools.go  (tool handlers — composition root)
                                       │
        ┌──────────────────────────────┼──────────────────────────────────┐
        │ start_session                 │ get_status(id) / stop_session(id)│
        ▼                               ▼
  validate args                  pkg/session.Manager.Status(id) / .Stop(id)
  (commonflags.go verbatim,             │
   D-06; namespace/all_ns               ▼
   mutual exclusivity;         id == active.ID?  ──no──► SESS-06 error (D-02: NOT
   duration parse+range,               │yes                for a retained-stopped id)
   Pitfall A)                          ▼
        │                       state == capturing?
        ▼                        ┌────┴─────┐
  pkg/session.Manager.Start     yes          no (stopped)
   (reqCtx, args)                │            │
        │                        ▼            ▼
        ▼                  cancel();     same summary +
  existing session?         bounded       "already stopped"
  ┌────┴──────────┐         wait on       marker, no error
 none/stopped   capturing   done chan          (D-03)
  │               │         │
  ▼               ▼         ▼
D-04 purge:    SESS-02   finalize response
RemoveAll      reject,   (SessionStats + absolute
(old tmpdir),  actionable cluster-health.json path,
note           error      D-09)
"discarded"    naming id
  │
  ▼
os.MkdirTemp (new session tmpdir)
  │
  ▼
setupCtx = context.WithTimeout(reqCtx, timeout)  ◄─ bounded, DISTINCT from the
  │                                                  gRPC dial timeout (Pitfall H)
  ▼
server == "" ?  ──no (D-07 bypass)──────────────────┐
  │yes                                                │
  ▼                                                    │
k8s.LoadKubeConfig + k8s.PortForwardToRelay             │
(pkg/k8s, unmodified — generate.go's exact recipe)      │
  │                                                    │
  └───────────────────┬────────────────────────────────┘
                       ▼
         cluster_dedup? → k8s.LoadClusterPoliciesForNamespaces (same setupCtx)
                       │
                       ▼
         build hubble.PipelineConfig
         (OutputDir/EvidenceDir under tmpdir, Stdout: mcpModeStdout(),
          OnFinal hook wired, SessionID: RFC3339-uuid4 per D-10)
                       │
                       ▼
         sessionCtx = context.WithCancel(m.rootCtx)  ◄─ child of the SERVER
                       │                                 root ctx, NEVER the
                       ▼                                 tool-call reqCtx (Pitfall C)
         go func() {
             err := m.runPipeline(sessionCtx, cfg)   ── hubble.RunPipeline,
             portForwardCleanup()                        UNMODIFIED (Pattern 1)
             s.done <- err
         }()
                       │
                       ▼
         return sess_<uuid> immediately — non-blocking (SESS-01)


  ── Shutdown fan-out (SESS-05) — triggered from cmd/cpg/mcp.go ──────────────
  runMCPServer(ctx, transport)
        │
        ▼
  server.Run(ctx, transport)   blocks until EITHER:
        │                        (a) ctx.Done() — SIGTERM via signal.NotifyContext
        │                        (b) transport session ends — stdin EOF, harness
        │                            crash (verified: mcp/server.go:946-973,
        │                            both paths return from Run)
        ▼  "for any reason" (Pitfall 3 / PITFALLS.md)
  manager.Shutdown()   synchronous, called BEFORE runMCPServer returns:
        │                1. cancel() the active session's ctx, if any
        │                   (cancel/close(stopCh) are non-blocking by
        │                    construction — no deadline needed for these)
        │                2. bounded wait on the session's done chan
        │                3. os.RemoveAll(tmpdir) — for the just-stopped
        │                   session AND any retained stopped session
        │                   (D-01: "...or at server shutdown")
        ▼
  RunE / main() returns → process exits with cleanup already complete
```

### Recommended Project Structure

```
pkg/session/
├── manager.go        # Manager: Start/Status/Stop/Shutdown, mutex-guarded single-slot state machine
├── session.go         # Session struct, State enum (capturing/stopped), StartArgs/StatusResult/StopResult
├── pipeline_config.go  # buildPipelineConfig(args, tmpDir, server) — ports generate.go's recipe (Pattern 5)
└── manager_test.go     # -race unit tests, fake FlowSource injection (no real cluster, no MCP SDK)

cmd/cpg/
├── mcp.go              # existing (Phase 16) — runMCPServer gains: construct Manager, call Shutdown() after Run
├── mcp_tools.go         # NEW — start_session/get_status/stop_session handlers, arg validation, tool registration
└── mcp_harness_test.go  # existing (Phase 16) — extended with session-tool golden-sequence scenarios
```

### Pattern 0: go-sdk tool-handler & shutdown mechanics this phase depends on

**What (all verified against the pinned `go-sdk` v1.6.1 source/`go doc` this session, HIGH confidence):**
- `ToolHandlerFor[In, Out] func(ctx context.Context, req *CallToolRequest, input In) (*CallToolResult, Out, error)`. Returning a non-nil `error` is **automatically** converted into `CallToolResult{IsError: true, Content: [error text]}` — never construct `CallToolResult{IsError: true}` by hand for SESS-06/SESS-02's error paths; just `return nil, Out{}, fmt.Errorf("session %q not found or expired", id)`.
- If `CallToolResult` returned is `nil` and `Out` is non-nil, go-sdk auto-populates **both** `StructuredContent` (the typed value) **and** `Content` (its JSON text, for back-compat). D-09's "structuredContent only" means *cpg authors no extra hand-written text block* — it does not mean `Content` stays empty; the SDK's auto-mirroring is the intended, idiomatic behavior, not a violation of D-09.
- `mcp.AddTool[In, Out]` infers the JSON schema from struct tags. **Fields without `omitempty`/`omitzero` become schema-`required`** (`go doc github.com/google/jsonschema-go/jsonschema.For`, confirmed). Every optional `start_session` arg (D-05's whole list except nothing — all of them are optional) needs `omitempty`; `session_id` on `get_status`/`stop_session` must NOT have `omitempty` (it is always required).
- `Server.Run(ctx, transport)` returns via **either** `ctx.Done()` (source: `mcp/server.go:959-964`) **or** the transport's own session ending, e.g. stdin EOF (`mcp/server.go:965-971`) — confirming PITFALLS.md's "transport Run() returning, for any reason, is the single root shutdown trigger" precisely at this SDK version.
- A tool handler's incoming `ctx` **is not safe to fork the background pipeline from**: `ServerSession.cancel` — the JSON-RPC `notifications/cancelled` handler — carries the doc comment *"cancellation is handled [in] the jsonrpc2 package... It should never be invoked in practice because cancellation is preempted"* (`mcp/server.go:1485-1491`), confirming the request-scoped ctx can be torn down by the transport layer independently of the handler's own logic finishing. This is the precise mechanism behind Pitfall 2/Pitfall C.

### Pattern 1: Session state machine with retention — supersedes the milestone sample

**What:** `capturing → stopped → gone`, single `*Session` slot on `Manager` (nil = idle). This is the authoritative shape for Phase 17 — it **corrects two stale references** a naive implementer would otherwise copy verbatim:

1. `ARCHITECTURE.md`'s Pattern 1 sample code includes `defer os.RemoveAll(tmpDir)` unconditionally inside the launch goroutine. **Do not port this.** D-01 requires the tmpdir to survive `stop_session` (removed only at the *next* `start_session`'s purge, or at server shutdown). The milestone research predates this phase's CONTEXT.md discussion; CONTEXT.md is the authoritative, more specific source and explicitly states it supersedes even `PROJECT.md`'s prior wording.
2. `REQUIREMENTS.md`'s literal SESS-06 text — *"unknown **or stopped** `session_id`"* — is superseded by CONTEXT.md's **D-02**: the crisp "not found or expired" error fires for unknown IDs and purged/replaced sessions only, **never** for a session that is merely stopped-and-retained. Get this wrong and `get_status`/Phase 18's query tools break on every session the instant `stop_session` returns (directly contradicting SESS-03's "a stopped session stays queryable" and Phase 18's QRY-04).

A consequence worth stating explicitly because it simplifies the rest of the design: **a `stopped` session has zero live resources** — no goroutine, no open ctx, no port-forward. Once the pipeline goroutine's `RunPipeline` call returns (for any reason) and its port-forward defer fires, all that remains is a directory of files. `get_status`/future Phase 18 query tools against a stopped session are therefore *pure filesystem reads* — matching milestone ARCHITECTURE.md's "filesystem is the only channel" principle exactly, just also true for the stopped case, not only the capturing case.

**State transition table:**

| Call | `Manager.session == nil` | `session.State == capturing` | `session.State == stopped` |
|---|---|---|---|
| `start_session` | create, `State=capturing` | **SESS-02**: reject, actionable error naming `session.ID` | **D-04**: purge (`os.RemoveAll` old tmpdir, note "previous session sess_X discarded"), then create |
| `get_status(id)` | **SESS-06** error | if `id` matches: coarse status; else SESS-06 | if `id` matches: coarse status (retained); else SESS-06 |
| `stop_session(id)` | **SESS-06** error | if `id` matches: cancel+wait+finalize, `State=stopped` | **D-03**: if `id` matches: same summary + "already stopped" marker, **not** `isError`; else SESS-06 |

**Example (shape, not literal implementation):**
```go
// pkg/session/manager.go
type State int
const (
    StateCapturing State = iota
    StateStopped
)

type Session struct {
    ID        string // "sess_" + uuid.New().String() — D-10
    TmpDir    string
    StartedAt time.Time
    StoppedAt time.Time // zero until stopped
    State     State
    cancel    context.CancelFunc
    done      chan error         // buffered 1
    stopOnce  sync.Once           // guards concurrent stop_session races — see Pitfall F
    final     atomic.Pointer[hubble.SessionStats] // written by OnFinal, read by Stop/Status — see Pitfall G
}

type Manager struct {
    mu      sync.Mutex
    session *Session // nil = idle; single slot enforces SESS-02 (Anti-Pattern 4)
    rootCtx context.Context // server-lifetime ctx, captured once at construction — Pattern 2
    logger  *zap.Logger
    runPipeline func(ctx context.Context, cfg hubble.PipelineConfig) error // defaults to hubble.RunPipeline; swappable in tests
}

func NewManager(rootCtx context.Context, logger *zap.Logger) *Manager {
    return &Manager{rootCtx: rootCtx, logger: logger, runPipeline: hubble.RunPipeline}
}
```

### Pattern 2: Detached-but-rooted context provenance

**What:** the session's background context must be **detached from the tool-call's request ctx** (so the call returning, or a `notifications/cancelled` for that specific call, does not kill the session — Pitfall 2/C) **but still a descendant of the MCP server's long-lived root ctx** (so SIGTERM propagates automatically — CONTEXT.md's explicit requirement). PITFALLS.md's generic `context.WithoutCancel(context.Background())` snippet satisfies the first half only and **loses SIGTERM propagation** — not a fit for this phase's stated constraint.

**The correct construction:** `Manager` stores the server-root ctx once, at construction (`NewManager(rootCtx, logger)`), where `rootCtx` is the *same* ctx `cmd/cpg/mcp.go`'s `runMCPServer(ctx, transport)` already receives (itself derived from `signal.NotifyContext` in production, or the harness test's ctx in `mcp_harness_test.go`). `Manager.Start`'s incoming `ctx` parameter (the tool handler's per-call request ctx) is used **only** to bound the synchronous setup phase (kubeconfig/port-forward/cluster-dedup — see Pattern 5); the instant the background goroutine is spawned, it forks from `m.rootCtx`, never from that parameter:

```go
func (m *Manager) Start(reqCtx context.Context, args StartArgs) (*Session, string, error) {
    // ... state-machine checks (Pattern 1), tmpdir creation ...
    setupCtx, cancel := context.WithTimeout(reqCtx, args.Timeout) // bounded by the REQUEST ctx — fine,
    defer cancel()                                                  // this portion is synchronous (Pattern 5)

    // ... LoadKubeConfig / PortForwardToRelay / LoadClusterPoliciesForNamespaces using setupCtx ...

    sessionCtx, sessionCancel := context.WithCancel(m.rootCtx) // NOT reqCtx — Pattern 2
    // ... spawn goroutine using sessionCtx ...
}
```

Storing a ctx as a struct field (`m.rootCtx`) is normally a Go anti-pattern for per-call contexts — the accepted exception is exactly this shape: a long-lived component's *own* lifecycle boundary, not a per-call scope. Document the deviation with a comment at the field, since a reviewer unfamiliar with the reasoning will otherwise flag it.

### Pattern 3: Bounded shutdown fan-out, triggered explicitly — not implied by ctx propagation

**What:** SESS-05 requires cleanup to *complete* before process exit, not merely be *requested*. Because `sessionCtx` is a child of `m.rootCtx`, SIGTERM cancelling `rootCtx` **does** propagate to `sessionCtx` automatically — but propagation alone does not guarantee the pipeline goroutine has actually finished unwinding (draining channels, writing final files, closing the port-forward) by the time `main()` returns and the process exits. `Server.Run` returning (Pattern 0) is the single trigger point; `cmd/cpg/mcp.go` must call a **synchronous, bounded** `Manager.Shutdown()` immediately after, and wait for it, before `runMCPServer`/`RunE` itself returns:

```go
// cmd/cpg/mcp.go — runMCPServer, extended (signature UNCHANGED: still (ctx, transport) error,
// so mcp_harness_test.go's existing startInMemoryMCPSession helper keeps working unmodified)
func runMCPServer(ctx context.Context, transport mcp.Transport) error {
    mgr := session.NewManager(ctx, bridgedSlogLoggerAsZap()) // or the package-level zap logger directly
    server := mcp.NewServer(&mcp.Implementation{Name: "cpg", Version: version},
        &mcp.ServerOptions{Logger: bridgedSlogLogger()})
    registerSessionTools(server, mgr) // start_session/get_status/stop_session

    err := server.Run(ctx, transport) // blocks until SIGTERM OR stdin EOF/harness crash (Pattern 0)
    mgr.Shutdown()                      // SESS-05 fan-out — synchronous, bounded internally
    return err
}
```

**Why "cancel" and "close the port-forward" need no separate deadline of their own:** both are non-blocking Go operations by construction — `context.CancelFunc` is documented idempotent and instant (calling it twice is a safe no-op), and `close(stopCh)` (the port-forward's teardown signal, `pkg/k8s/portforward.go:93-94`) never blocks; the underlying SPDY goroutines finish asynchronously in the background. The **two** steps that genuinely need independent bounded deadlines are: (1) waiting to *observe* the pipeline goroutine's exit (`<-session.done`), because that involves real I/O (draining channels, writing `cluster-health.json`), and (2) `os.RemoveAll(tmpdir)`, a real syscall that could in principle hang on a wedged filesystem. Structure `Shutdown()` so a stuck step (1) does not prevent step (2) from at least being attempted — e.g. `select { case <-s.done: default: }` after a bounded wait, then unconditionally attempt the `RemoveAll` regardless of whether the wait succeeded.

### Pattern 4: `OnFinal` hook — the one `pkg/hubble` change (D-08)

**What:** `PipelineConfig` gets one new nil-safe field. `SessionStats` is fully populated at `pkg/hubble/pipeline.go:322` (right after `stats.InfraDropsByReason = agg.InfraDrops()`), *before* `ew.finalize`/`hw.finalize`/the stdout summary print. Insert the hook call immediately after that line:

```go
// pkg/hubble/pipeline.go — PipelineConfig struct (add field near Stdout, ~line 93)
    // OnFinal, if non-nil, is called exactly once after g.Wait() with the fully
    // populated SessionStats — before ew/hw.finalize(). Nil-safe (CLI paths
    // never set it). Added for cpg mcp's session manager (D-08): cluster-health.json
    // alone does not carry PoliciesWritten/Skipped/Failed, LostEvents, or L7 counts.
    OnFinal func(SessionStats)

// pkg/hubble/pipeline.go — RunPipelineWithSource, immediately after line 322
    stats.InfraDropsByReason = agg.InfraDrops()
    if cfg.OnFinal != nil {
        cfg.OnFinal(*stats)
    }
    // ... existing VIS-01 gate, ew.finalize, hw.finalize, stdout summary unchanged below ...
```

The session manager's `Start` wires `cfg.OnFinal = func(stats hubble.SessionStats) { s.final.Store(&stats) }` — storing via `atomic.Pointer` (not a plain field) because this closure runs on the **pipeline's own goroutine**, while `get_status`/`stop_session` read it from a **different** goroutine (the tool-handler's). This is exactly the kind of cross-goroutine access `go test -race` (already the project-wide convention — `Makefile:9`) is positioned to catch if done with a bare field instead.

### Pattern 5: `PipelineConfig` construction recipe, adapted for a session tmpdir

**What:** `generate.go:225-256` is the reference recipe (already read this session). For a session, `OutputDir`/`EvidenceDir` move under the ephemeral tmpdir, and every stdout-defaulting seam must resolve to `mcpModeStdout()` — completing the Phase 16 handoff (`cmd/cpg/mcp.go`'s own doc comment: *"Phase 17's session-construction code — the first place an MCP-mode PipelineConfig is built... MUST set PipelineConfig.Stdout = mcpModeStdout()"*).

```go
// pkg/session — path recipe, verified against pkg/evidence/paths.go and pkg/hubble/health_writer.go:138
outputDir  := filepath.Join(tmpDir, "policies")
evidenceDir := filepath.Join(tmpDir, "evidence")
outputHash := evidence.HashOutputDir(outputDir) // deterministic hash of an already-unique MkdirTemp path — no collision risk across sessions
healthPath := filepath.Join(evidenceDir, outputHash, "cluster-health.json") // exact formula health_writer.go:138 uses — needed verbatim for stop_session's D-09 response

cfg := hubble.PipelineConfig{
    Server: server, TLSEnabled: args.TLS, Timeout: args.Timeout, // defaulted — Pitfall A
    Namespaces: args.Namespaces, AllNamespaces: args.AllNamespaces,
    OutputDir: outputDir, FlushInterval: args.FlushInterval,       // defaulted — Pitfall A
    Logger: m.logger, ClusterPolicies: clusterPolicies,             // only if cluster_dedup
    EvidenceEnabled: true, EvidenceDir: evidenceDir, OutputHash: outputHash,
    EvidenceCaps: evidence.MergeCaps{MaxSamples: 10, MaxSessions: 10}, // binary defaults, D-05
    SessionID: fmt.Sprintf("%s-%s", time.Now().UTC().Format(time.RFC3339), uuid.New().String()[:4]), // EXACT existing recipe — evidence schema v2 untouched, D-10
    SessionSource: evidence.SourceInfo{Type: "live", Server: server},
    CPGVersion: version, L7Enabled: args.L7,
    IgnoreProtocols: args.IgnoreProtocols, IgnoreDropReasons: args.IgnoreDropReasons, // pre-validated by cmd/cpg — Pitfall J
    Stdout: mcpModeStdout(), // Phase 16 handoff — regression = SRV-02 break (Pitfall I)
    OnFinal: func(s hubble.SessionStats) { session.final.Store(&s) }, // Pattern 4
    // DryRun/DryRunDiff/FailOnInfraDrops: never set — D-05 excludes these from MCP mode entirely
}
```

Note `cluster_dedup` (D-05) mirrors `generate.go:197-212` exactly: it independently calls `k8s.LoadKubeConfig()` even when `server` was given directly (D-07 bypass only skips the port-forward, not a *separate* kubeconfig load for the dedup snapshot) — both calls belong inside the same bounded `setupCtx` (Pitfall H).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Opaque, unpredictable session handle | A custom random-string generator (counter+salt, `math/rand`, etc.) | `"sess_" + uuid.New().String()` (`github.com/google/uuid`, already direct dep) | Verified `crypto/rand`-backed (`uuid.go:9,40`); a hand-rolled generator is exactly the kind of thing that quietly regresses to `math/rand` under refactoring |
| Human-friendly duration input from an LLM | A bespoke duration mini-parser ("10 seconds" → Duration) | `time.ParseDuration` (stdlib) on a string arg, with an explicit `> 0` check | `ParseDuration` already accepts the exact syntax cpg's own `--timeout`/`--flush-interval` CLI flags use ("10s", "500ms") — free consistency, zero new code, well-tested stdlib |
| Bounded "wait at most N seconds, then move on" cleanup step | A hand-rolled polling loop (`for { ... time.Sleep(...) }`) | `select { case <-done: ... case <-time.After(deadline): ... }` (stdlib idiom, already used identically in `pipeline_test.go`'s `require.Eventually` pattern) | Standard, race-free, no busy-waiting |
| Atomic artifact writes | A new atomic-write helper for anything `pkg/session` itself writes | Nothing new needed — `pkg/session` writes **zero** files itself; every artifact write goes through the already-atomic `pkg/evidence`/`pkg/hubble/health_writer.go` writers, and `pkg/output/writer.go` (fixed in Phase 16, SEC-02) | `pkg/session` is purely an orchestrator; duplicating write logic here would be a straight regression of the "pipeline is unmodified" constraint |

**Key insight:** every "hand-roll" temptation in this phase resolves to "use what `generate.go`/stdlib already does" — the phase's actual novelty is entirely in *orchestration* (state machine, context provenance, bounded shutdown), not in any new low-level primitive.

## Common Pitfalls

### Pitfall A: zero-value numeric args reach `PipelineConfig` unfixed — one crashes the process, one silently removes a safety bound
**What goes wrong:** an MCP client omits the optional (D-05) `flush_interval`/`timeout` args. If the handler forwards the resulting Go zero-value straight through, two independent, verified failure modes trigger:
- `flush_interval = 0` → `pkg/hubble/aggregator.go:365`'s `time.NewTicker(a.interval)` **panics** (`time.NewTicker` panics for `d <= 0`, stdlib-documented). This runs inside an `errgroup.Go` goroutine (`pipeline.go:238-240`) — an unrecovered panic there takes down the entire `cpg mcp` process, not just the one tool call. This is the single highest-severity finding in this research pass.
- `timeout = 0` → `pkg/hubble/client.go:70`'s `if c.timeout > 0 { waitForConnReady... }` guard is **false**, so the gRPC dial proceeds with **no** bound at all — the exact opposite of D-07's stated intent ("timeout (default 10s) bounds connection establishment so a bad relay makes `start_session` fail fast").
**Why it happens:** cobra's `Duration("timeout", 10*time.Second, ...)` flag has its default baked into the flag registration itself; an *omitted* JSON field in an MCP tool call has no such mechanism — it just unmarshals to Go's zero value.
**How to avoid:** default explicitly in the handler/`Manager.Start`, matching the CLI's own documented defaults (`timeout`: 10s per `generate.go:104`; `flush_interval`: 5s per `commonflags.go:71`) *before* either value reaches `PipelineConfig`. Also reject a negative parsed duration (`time.ParseDuration("-5s")` succeeds syntactically) with an explicit `> 0` check.
**Warning signs:** `cpg mcp` process exits/crashes shortly after a `start_session` call that omitted `flush_interval`; `start_session` against an unreachable relay hangs instead of failing fast within ~10s.

### Pitfall B: `time.Duration` struct fields are unusable for LLM-facing tool args
**What goes wrong:** a naive `type StartArgs struct { Timeout time.Duration \`json:"timeout,omitempty"\` }` produces a JSON schema of bare `{"type": "integer"}` (verified this session by executing `jsonschema.For[T]` against the pinned `jsonschema-go` v0.4.3) — no unit is conveyed beyond free-text description, and the LLM would need to compute nanoseconds (`10s` = `10000000000`). Worse, passing the natural string form fails outright: `json: cannot unmarshal string into Go struct field .d of type time.Duration` (also verified by direct execution).
**How to avoid:** represent `timeout`/`flush_interval` as `string` fields in the args struct (`jsonschema:"Go duration string, e.g. \"30s\" (default: 10s)"`), parsed via `time.ParseDuration` in the handler. See Pattern 5 / Alternatives Considered.
**Phase to address:** `start_session`'s arg-struct design, before any handler code is written — retrofitting after an LLM harness has "learned" the wrong shape is a breaking contract change (same class of cost PITFALLS.md's Pitfall 6 recovery-cost table already flags for schema mistakes generally).

### Pitfall C: forking the background pipeline from the tool handler's request ctx
**What goes wrong:** the handler's incoming `ctx` parameter is request-scoped and can be cancelled independently of the session's intended lifetime (verified: `mcp/server.go:1485-1491`, "cancellation is preempted" by the jsonrpc2 layer on `notifications/cancelled`, plus the generic "typically cancelled once the handler returns" behavior common to MCP SDKs). `go m.runPipeline(ctx, cfg)` using that `ctx` directly gets killed before or immediately after `start_session` returns.
**How to avoid:** Pattern 2 — fork `sessionCtx` from `m.rootCtx` (captured once at `Manager` construction from the server's own long-lived ctx), never from the per-call handler ctx.
**Warning signs:** `start_session` reports success but `get_status` immediately after shows zero flows and a session that already looks stopped; capture duration in tests never exceeds the time the tool call itself took.

### Pitfall D: relying on ctx-cancellation propagation alone to guarantee cleanup finishes
**What goes wrong:** because `sessionCtx` is a child of `m.rootCtx`, SIGTERM does propagate — but propagation only *starts* the pipeline's unwind; it does not make `main()` wait for it to *finish*. Without an explicit synchronous call, the process can exit while the background goroutine is mid-drain, before the port-forward closes or the tmpdir is removed.
**How to avoid:** Pattern 3 — `runMCPServer` must call and wait on `manager.Shutdown()` immediately after `server.Run(...)` returns, for *both* return paths (ctx.Done and transport-session-ended), not only on an explicit signal branch.
**Warning signs:** `$TMPDIR` accumulating session directories across repeated ungraceful-disconnect test runs; the SESS-05 integration test passes when `stop_session` is called explicitly but fails on the "kill the transport mid-session" variant.

### Pitfall E: porting `ARCHITECTURE.md`'s illustrative sample verbatim
**What goes wrong:** its Pattern 1 code snippet (`defer os.RemoveAll(tmpDir)` unconditionally, inside the launch goroutine, before D-01 existed) directly contradicts this phase's locked retention model.
**How to avoid:** treat this RESEARCH.md's Pattern 1 as authoritative for Phase 17; the milestone doc's sample is explicitly superseded (see Pattern 1's opening paragraph) — do not copy its cleanup logic.
**Warning signs:** `get_status` on a session right after `stop_session` returns "not found or expired" instead of a stopped-state summary.

### Pitfall F: concurrent `stop_session` calls race on the single-buffered `done` channel
**What goes wrong:** two overlapping `stop_session(id)` calls for the same session both see `State == capturing`, both call `cancel()` (safe, idempotent) and both `select` on `<-s.done` — but only one receive gets the value; the other blocks until its own bounded-wait timeout fires, even though the pipeline actually finished promptly. Not a correctness bug (the D-03 idempotent-summary path still resolves it eventually), but a real, avoidable latency/UX regression a harness retry could trigger.
**How to avoid:** guard the actual cancel+wait+finalize sequence with a `sync.Once` on the `Session` (sketched in Pattern 1's `Session` struct) so every concurrent caller past the first blocks on the *same* teardown completing, then all return the identical summary.
**Warning signs:** a `-race`-clean but flaky-latency test: two goroutines calling `Stop` concurrently, one consistently takes the full timeout to return.

### Pitfall G: holding the `Manager` mutex across the bounded pipeline-exit wait
**What goes wrong:** if `Stop`'s lock is held for the entire `select { case <-done: ...; case <-time.After(deadline): }`, every concurrent `get_status`/`start_session` call blocks for up to the full stop-deadline — turning a single slow stop into a server-wide stall.
**How to avoid:** release the mutex before the bounded wait (snapshot the `*Session` pointer under lock, unlock, wait, re-lock only to flip `State`/`StoppedAt`) — sketched in the Validation Architecture's concurrency test list below.
**Warning signs:** `get_status` latency spikes correlate with an in-flight `stop_session` call in tests or logs.

### Pitfall H: bounding the gRPC dial but not the kubeconfig/port-forward/cluster-dedup setup
**What goes wrong:** `--timeout`/D-07's `timeout` arg, as implemented in `pkg/hubble/client.go:70` (`waitForConnReady`), bounds **only** the gRPC connection establishment — verified by direct source read. `k8s.LoadKubeConfig()`, `k8s.PortForwardToRelay()` (which does its own internal `select` on `readyCh`/`errCh`/`ctx.Done()`, `portforward.go:71-79`), and (if `cluster_dedup`) `k8s.LoadClusterPoliciesForNamespaces` all take the *ambient* ctx with no independent deadline of their own in `generate.go`'s existing usage. An interactive `exec:`-credential-plugin hang (milestone PITFALLS.md Pitfall 8) anywhere in that chain would hang `start_session` indefinitely if nothing bounds it.
**How to avoid:** wrap the *entire* synchronous setup phase — kubeconfig load through cluster-dedup snapshot, not just the gRPC dial — in one `setupCtx := context.WithTimeout(reqCtx, args.Timeout)` (Pattern 2/5). A `context.DeadlineExceeded` here should translate to the specific, actionable message PITFALLS.md recommends: *"kubeconfig auth did not complete within Ns — re-authenticate outside the MCP session (e.g. run `kubectl get pods` once in a real shell) and retry"* rather than a bare deadline-exceeded string.
**Warning signs:** `start_session` hangs past its stated `timeout` value when kubeconfig resolution (not the relay dial) is the actual bottleneck.

### Pitfall I: forgetting the Phase 16 `mcpModeStdout()` handoff
**What goes wrong:** `pkg/session` is the **first** place an MCP-mode `PipelineConfig` actually gets constructed and passed to `RunPipeline` (Phase 16 registered zero tools and never built one). If `Stdout` is left nil or set to something other than `mcpModeStdout()`, `pipeline.go:356-358`'s `if stdout == nil { stdout = os.Stdout }` default (or an accidental real-stdout wire) reintroduces the exact stdout-corruption class Phase 16's `TestMCPModeStdoutNeverDefaultsToRealStdout`/`TestMCPStdoutPurity` exist to prevent — but those tests can't catch a regression in code that doesn't exist yet at Phase 16 time.
**How to avoid:** `cfg.Stdout = mcpModeStdout()` (the existing helper, `cmd/cpg/mcp.go:119-121`) in the PipelineConfig construction recipe (Pattern 5), verified by extending Phase 16's stdout-purity harness with a live session scenario (see Validation Architecture).
**Warning signs:** the very first `stop_session` in a real harness leaks the human-readable cluster-health summary block onto the JSON-RPC stdio stream.

### Pitfall J: `commonflags.go`'s validators are unexported — `pkg/session` cannot call them
**What goes wrong:** `validateIgnoreProtocols`/`validateIgnoreDropReasons` (D-06, "reuse verbatim") live in `package main` (`cmd/cpg/commonflags.go`), lowercase and therefore invisible outside that package. A design that tries to import them from `pkg/session` won't compile; a design that reimplements their logic inside `pkg/session` violates "verbatim" and creates a second normalization path to drift out of sync.
**How to avoid:** validation happens in `cmd/cpg/mcp_tools.go` (also `package main`, same package as the validators) **before** calling `Manager.Start` — the tool handler validates and normalizes `ignore_protocols`/`ignore_drop_reasons` (and the `namespace`/`all_namespaces` mutual-exclusivity check, mirroring `generateFlags.validate()`'s two-line check, which isn't a standalone reusable function but is trivial to inline), returning an `isError` result directly on failure. `pkg/session.StartArgs` receives only already-validated, already-normalized values.
**Warning signs:** a build error importing `commonflags.go` functions from `pkg/session`; or, if reimplemented, a normalization behavior (case-folding, allowlist) that silently drifts from the CLI's over time.

## Code Examples

### 1. `start_session` tool registration (shape)
```go
// cmd/cpg/mcp_tools.go — Source: verified go-sdk v1.6.1 API (go doc, this session)
type StartArgs struct {
    Namespace         []string `json:"namespace,omitempty" jsonschema:"namespace filter, repeatable"`
    AllNamespaces     bool     `json:"all_namespaces,omitempty" jsonschema:"observe all namespaces"`
    L7                bool     `json:"l7,omitempty" jsonschema:"enable L7 (HTTP/DNS) policy generation"`
    IgnoreDropReasons []string `json:"ignore_drop_reasons,omitempty" jsonschema:"exclude flows by drop reason name before classification"`
    IgnoreProtocols   []string `json:"ignore_protocols,omitempty" jsonschema:"drop flows whose L4 protocol matches: tcp, udp, icmpv4, icmpv6, sctp"`
    Server            string   `json:"server,omitempty" jsonschema:"explicit Hubble Relay address; bypasses auto port-forward when set"`
    TLS               bool     `json:"tls,omitempty" jsonschema:"enable TLS for the gRPC connection"`
    Timeout           string   `json:"timeout,omitempty" jsonschema:"Go duration string, e.g. \"30s\" (default: 10s) — bounds kubeconfig+port-forward+dial setup"`
    ClusterDedup      bool     `json:"cluster_dedup,omitempty" jsonschema:"skip policies that already exist in cluster"`
    FlushInterval     string   `json:"flush_interval,omitempty" jsonschema:"Go duration string, e.g. \"5s\" (default: 5s)"`
}

mcp.AddTool(server, &mcp.Tool{
    Name:        "start_session",
    Description: "Start a live Hubble capture session. Returns immediately with an opaque session_id; the capture runs in the background. Only one session may be active at a time — call stop_session before starting another. Poll get_status to check progress.",
    Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, // readonly wrt the CLUSTER — mutates only cpg's own ephemeral tmpdir
}, func(ctx context.Context, req *mcp.CallToolRequest, args StartArgs) (*mcp.CallToolResult, StartResult, error) {
    if len(args.Namespace) > 0 && args.AllNamespaces {
        return nil, StartResult{}, fmt.Errorf("namespace and all_namespaces are mutually exclusive")
    }
    ignoreProtocols, err := validateIgnoreProtocols(args.IgnoreProtocols) // D-06, verbatim reuse
    if err != nil {
        return nil, StartResult{}, err
    }
    ignoreDropReasons, err := validateIgnoreDropReasons(args.IgnoreDropReasons, logger) // D-06, verbatim reuse
    if err != nil {
        return nil, StartResult{}, err
    }
    result, err := mgr.Start(ctx, session.StartArgs{ /* ... pre-validated fields ... */ })
    return nil, result, err // error path: SDK auto-sets IsError + Content from err.Error() (Pattern 0)
})
```

### 2. `Manager.Start` — state-machine + setup-bound sketch
```go
// pkg/session/manager.go
func (m *Manager) Start(reqCtx context.Context, args StartArgs) (StartResult, error) {
    m.mu.Lock()
    if m.session != nil && m.session.State == StateCapturing {
        active := m.session
        m.mu.Unlock()
        return StartResult{}, fmt.Errorf(
            "session %s already running (started %s ago); call stop_session first",
            active.ID, time.Since(active.StartedAt).Round(time.Second))
    }
    var discarded string
    if m.session != nil { // stopped-and-retained — D-04 silent purge
        discarded = m.session.ID
        _ = os.RemoveAll(m.session.TmpDir) // best-effort; Shutdown() also sweeps on process exit
        m.session = nil
    }
    m.mu.Unlock() // release before the (potentially slow) synchronous setup phase

    tmpDir, err := os.MkdirTemp("", "cpg-session-*")
    if err != nil {
        return StartResult{}, fmt.Errorf("creating session tmpdir: %w", err)
    }
    timeout := defaultDuration(args.Timeout, 10*time.Second)   // Pitfall A
    flushInterval := defaultDuration(args.FlushInterval, 5*time.Second) // Pitfall A
    setupCtx, cancel := context.WithTimeout(reqCtx, timeout)   // Pitfall H
    defer cancel()

    server, portForwardCleanup, err := m.resolveServer(setupCtx, args) // LoadKubeConfig+PortForwardToRelay, or D-07 bypass
    if err != nil {
        os.RemoveAll(tmpDir)
        return StartResult{}, err // distinct, actionable per-failure-mode text — Claude's Discretion, guided by Pitfall 8
    }
    // ... optional cluster_dedup via setupCtx ...
    // ... build cfg per Pattern 5 ...

    sessionCtx, sessionCancel := context.WithCancel(m.rootCtx) // Pattern 2 — NOT reqCtx
    s := &Session{ID: "sess_" + uuid.New().String(), TmpDir: tmpDir, StartedAt: time.Now(),
        State: StateCapturing, cancel: sessionCancel, done: make(chan error, 1)}
    go func() {
        err := m.runPipeline(sessionCtx, cfg)
        portForwardCleanup() // non-blocking (close(stopCh)) — before signaling done, so a caller that
        s.done <- err          // observes done also knows the port-forward is already closing
    }()

    m.mu.Lock()
    m.session = s
    m.mu.Unlock()
    return StartResult{SessionID: s.ID, DiscardedSession: discarded, Server: server}, nil
}
```

### 3. `Manager.Stop` — lock discipline + idempotency (D-03)
```go
// pkg/session/manager.go
func (m *Manager) Stop(id string) (StopResult, error) {
    m.mu.Lock()
    s := m.session
    if s == nil || s.ID != id {
        m.mu.Unlock()
        return StopResult{}, fmt.Errorf("session %q not found or expired", id) // SESS-06
    }
    if s.State == StateStopped {
        m.mu.Unlock()
        return s.buildSummary(true), nil // D-03: idempotent, "already stopped" marker, never isError
    }
    m.mu.Unlock() // Pitfall G — release before the bounded wait

    s.stopOnce.Do(func() { // Pitfall F — only the first concurrent caller does the real teardown
        s.cancel()
        select {
        case <-s.done:
        case <-time.After(pipelineExitDeadline): // e.g. 5s — Claude's Discretion, sensible starting point
            m.logger.Warn("session did not exit within deadline; proceeding", zap.String("session_id", id))
        }
        m.mu.Lock()
        s.State, s.StoppedAt = StateStopped, time.Now()
        m.mu.Unlock()
    })
    return s.buildSummary(false), nil
}
```

### 4. `get_status` / SESS-06 error shape
```go
func (m *Manager) Status(id string) (StatusResult, error) {
    m.mu.Lock()
    s := m.session
    m.mu.Unlock()
    if s == nil || s.ID != id {
        return StatusResult{}, fmt.Errorf("session %q not found or expired", id) // works for a
    }                                                                              // stopped session too — D-02
    elapsed := time.Since(s.StartedAt)
    if s.State == StateStopped {
        elapsed = s.StoppedAt.Sub(s.StartedAt) // frozen, not still ticking
    }
    policies, _ := countGlob(filepath.Join(s.TmpDir, "policies", "*", "*.yaml"))
    evidence, _ := countGlob(filepath.Join(s.TmpDir, "evidence", "*", "*", "*.json"))
    return StatusResult{State: s.State.String(), Elapsed: elapsed.String(),
        PolicyFileCount: policies, EvidenceFileCount: evidence}, nil
}
```

### 5. Pipeline OnFinal insertion (Pattern 4, exact diff location)
```go
// pkg/hubble/pipeline.go, immediately after line 322 (stats.InfraDropsByReason = agg.InfraDrops())
    if cfg.OnFinal != nil {
        cfg.OnFinal(*stats)
    }
```

## State of the Art

| Old Approach (milestone-level research, predates this phase's discussion) | Current Approach (this phase, D-01..D-10) | When Changed | Impact |
|--------------------------|------------------|---------------|--------|
| `stop_session` removes the tmpdir immediately (`ARCHITECTURE.md` Pattern 1 sample; `PROJECT.md`'s "tmpdir cleaned at stop_session" line) | tmpdir retained after stop; removed at next `start_session` (purge) or server shutdown | Phase 17 CONTEXT.md discussion, 2026-07-20 | `get_status`/Phase 18 query tools work against a stopped session; `PROJECT.md`'s Constraints section is now stale on this one line (expect a routine post-phase PROJECT.md sync, not a Phase 17 task) |
| SESS-06 fires for "unknown or stopped" `session_id` (REQUIREMENTS.md literal text) | Fires for unknown/purged only — never a retained stopped session (D-02) | Same discussion | Directly required for SESS-03/QRY-04 to be satisfiable at all |
| `context.WithoutCancel(context.Background())` for the detached session ctx (PITFALLS.md's generic snippet) | `context.WithCancel(m.rootCtx)` — detached from the request ctx, rooted in the server's own long-lived ctx | This research pass, grounded in CONTEXT.md's explicit "child of server ctx" requirement + direct go-sdk source read | SIGTERM correctly cancels an active session automatically; the milestone snippet alone would not |

**Deprecated/outdated for this phase specifically:** none at the library level — `go-sdk` v1.6.1 remains current (v1.7.0-pre.1..3 exist but are prerelease/non-GA, consistent with STACK.md's existing guidance not to adopt them for v1.5).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `sess_<uuid>` uses a full UUID4 string (36 chars), not a shortened form like the internal evidence `SessionID`'s 4-char suffix trick | Pattern 1, Pattern 5, D-10 | LOW — cosmetic; trivial to shorten later, no functional/format dependency elsewhere |
| A2 | Omitted `flush_interval` defaults to 5s (mirroring the CLI's `--flush-interval` flag default, `commonflags.go:71`) — D-05 only states timeout's default (10s) explicitly | Pitfall A, Code Example 2 | LOW — a different constant is a one-line change; the *requirement* to default explicitly (not leave at 0) is the load-bearing part and is independently verified (ticker panic) |
| A3 | L7 pre-flight (`maybeRunL7Preflight`, CLI-only today) is out of scope for `start_session` even when `l7=true` — CONTEXT.md doesn't address this either way | Open Questions #1 | LOW — preflight is purely advisory (warns, never blocks); omitting it only loses an early warning log, `--l7`'s actual codegen behavior is unaffected |
| A4 | Session tmpdir prefix `"cpg-session-*"` (matching PITFALLS.md's own monitoring-text expectation) rather than `"cpg-mcp-*"` (ARCHITECTURE.md's sample) | Code Example 2, Pattern 3 | LOW — cosmetic naming only, no behavioral dependency |

**If this table is empty:** N/A — see rows above. All four are low-risk naming/default/scope-boundary judgment calls, not core architecture; every load-bearing claim in this document (go-sdk API contract, `time.NewTicker` panic behavior, `jsonschema.For`'s Duration handling, `uuid`'s randomness source, `Server.Run`'s two return paths, existing file:line references) was independently verified this session via `go doc`, direct source reads at the exact pinned versions, or executed probes — not carried forward as unverified training-data claims.

## Open Questions

1. **Should `l7=true` trigger L7 cluster pre-flight inside `start_session`?**
   - What we know: `maybeRunL7Preflight` (`generate.go:31-56`) is explicitly commented "invoked AT MOST ONCE per cpg invocation" — reasoning specific to a single-shot CLI process, which doesn't cleanly generalize to a long-lived MCP server that might run many sessions across its lifetime.
   - What's unclear: CONTEXT.md's D-05 lists `l7` as an exposed arg but is silent on preflight; there's no `--no-l7-preflight`-equivalent MCP arg proposed either.
   - Recommendation: skip preflight entirely for v1.5's `start_session` (A3, LOW risk either way — preflight only warns, never blocks `--l7` from working) — flag for explicit confirmation if the planner disagrees.

2. **Exact bounded-deadline constants for `Shutdown()`/`Stop()`'s waits.**
   - What we know: SESS-05 requires "each step bounded by its own deadline"; explicitly Claude's Discretion per CONTEXT.md.
   - What's unclear: no specific numbers are locked.
   - Recommendation: 5s for the pipeline-exit wait (generous relative to Ctrl+C's existing, undocumented-but-presumably-fast shutdown on `cpg generate`), 2s for `os.RemoveAll` (local filesystem, should be near-instant even for hundreds of small files) — starting points, not locked values.

3. **Concurrent `stop_session` handling: `sync.Once` (Code Example 3) vs. accepting the double-timeout cost (Pitfall F).**
   - What we know: `sync.Once` fully resolves the race with ~5 lines of code.
   - What's unclear: whether the added complexity is worth it for what is, in production, an unlikely edge case (single LLM client, unlikely to fire two concurrent `stop_session` calls for the same session).
   - Recommendation: use `sync.Once` — it's cheap, directly testable under `-race`, and removes a class of flaky-latency test failures before they occur.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Build/test | ✓ | go1.25.12 (matches `go.mod` toolchain directive exactly) | — |
| `golangci-lint` | `make lint` | ✓ | v2.1.0 | — |
| Reachable Kubernetes cluster (`kubectl cluster-info`) | Manual/live verification of `start_session` against a real relay | ✗ (no cluster reachable in this research environment — `kubectl` configured but connection refused) | — | Not needed for Phase 17's automated tests by design — unit tests use a fake `FlowSource` (mirroring `pkg/hubble/pipeline_test.go`'s `mockFlowSource`) and never construct a real gRPC/K8s client; D-07's `server` bypass additionally lets even an integration-style test point at a local fake listener. Live-cluster verification, if desired, is a manual/operator step outside the automated suite. |

**Missing dependencies with no fallback:** none — Phase 17's automated test strategy was deliberately designed (by the milestone research, reaffirmed here) to need neither a cluster nor the MCP SDK for its core `pkg/session` coverage.

**Missing dependencies with fallback:** reachable cluster (see above).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `testify` (`assert`/`require`) + `go.uber.org/zap/zaptest`/`zaptest/observer` — all already pervasive across the codebase |
| Config file | none — `go test` needs none; race detection is a Makefile convention (`Makefile:9`) |
| Quick run command | `go test ./pkg/session/... -race -count=1` |
| Full suite command | `make test` (`go test ./... -count=1 -race`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SESS-01 | `start_session` returns quickly (non-blocking), opaque `session_id`, tmpdir created, pipeline runs in background | unit | `go test ./pkg/session/... -run TestManager_Start -race` | ❌ Wave 0 — `pkg/session/manager_test.go` |
| SESS-02 | second `start_session` while capturing is rejected, error names the active `session_id` | unit | `go test ./pkg/session/... -run TestManager_Start_RejectsConcurrent -race` | ❌ Wave 0 (same file) |
| SESS-02/D-04 | `start_session` while a *stopped* session is retained: silent purge, not a rejection | unit | `go test ./pkg/session/... -run TestManager_Start_PurgesStoppedSession -race` | ❌ Wave 0 (same file) |
| SESS-03 | `get_status` returns coarse state/elapsed/file counts for both capturing and stopped sessions | unit | `go test ./pkg/session/... -run TestManager_Status -race` | ❌ Wave 0 (same file) |
| SESS-04 | `stop_session` cancels ctx, waits for pipeline exit, returns `SessionStats`-derived summary + `cluster-health.json` path | unit | `go test ./pkg/session/... -run TestManager_Stop -race` | ❌ Wave 0 (same file) |
| SESS-04/D-03 | second `stop_session` on the same id is idempotent (same summary + "already stopped", not `isError`) | unit | `go test ./pkg/session/... -run TestManager_Stop_Idempotent -race` | ❌ Wave 0 (same file) |
| SESS-04/D-08 | `OnFinal` hook fires exactly once, after full stats population, nil-safe for existing CLI paths | unit | `go test ./pkg/hubble/... -run TestRunPipeline_OnFinal -race` | ❌ Wave 0 — extend `pkg/hubble/pipeline_test.go` |
| SESS-05 | transport death (ctx cancel or transport close) mid-session → cancel + port-forward close + tmpdir removed, bounded | integration | `go test ./cmd/cpg/... -run TestMCPSessionUngracefulDisconnect -race` | ❌ Wave 0 — extend `cmd/cpg/mcp_harness_test.go` |
| SESS-06 | unknown/purged `session_id` on any session-scoped tool → crisp "not found or expired", not generic | unit | `go test ./pkg/session/... -run TestManager_UnknownSessionID -race` | ❌ Wave 0 (same file) |
| SESS-06/D-02 | a *retained stopped* `session_id` does NOT trigger SESS-06 on `get_status` | unit | `go test ./pkg/session/... -run TestManager_Status_StoppedSessionStaysQueryable -race` | ❌ Wave 0 (same file) |
| Golden sequence (all) | `initialize → start_session → get_status → stop_session` over the in-memory transport, stdout stays JSON-RPC-only throughout | integration | `go test ./cmd/cpg/... -run TestMCPSessionLifecycleGoldenSequence -race` | ❌ Wave 0 — extend `cmd/cpg/mcp_harness_test.go`, reusing the existing `startInMemoryMCPSession` helper unchanged (Pattern 3's signature-preservation note) |
| Concurrency hygiene (not requirement-mapped, but load-bearing per Pitfall F/G) | concurrent `Stop` calls for the same id both return promptly; `Status` doesn't stall behind an in-flight `Stop` | unit | `go test ./pkg/session/... -run TestManager_ConcurrentStop -race` | ❌ Wave 0 (same file) |

### Sampling Rate
- **Per task commit:** `go test ./pkg/session/... ./cmd/cpg/... -race -count=1`
- **Per wave merge:** `make test` (full suite, `-race`, all 10+ packages)
- **Phase gate:** full suite green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `pkg/session/manager_test.go` — covers SESS-01, SESS-02, SESS-03, SESS-04, SESS-06 and their D-01..D-04/D-08 nuances; unit-tested with a fake `FlowSource`/injected `runPipeline` func (mirrors `pkg/hubble/pipeline_test.go`'s `mockFlowSource` pattern exactly), no real cluster, no MCP SDK
- [ ] `pkg/hubble/pipeline_test.go` extension — `OnFinal` hook fires-once/nil-safe coverage (D-08)
- [ ] `cmd/cpg/mcp_harness_test.go` extension (or a new `cmd/cpg/mcp_session_test.go`, package `main`) — golden-sequence + ungraceful-disconnect scenarios (SESS-05), reusing `startInMemoryMCPSession` unchanged
- [ ] Framework install: none — `testing`/`testify`/`zaptest` are already fully wired project-wide

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V2 Authentication | No | Stdio-local process; no cpg-level auth layer in v1.5 by explicit milestone scope (REQUIREMENTS.md Out-of-Scope: HTTP/SSE+OAuth). Cluster auth is entirely delegated to kubeconfig/K8s RBAC, unchanged by this phase. |
| V3 Session Management | Yes | `session_id` = `"sess_" + uuid.New().String()` — **verified** `crypto/rand`-backed (V3.2-class unpredictability requirement satisfied by construction, not by a hand-rolled generator); single-slot enforcement (SESS-02) prevents any session-fixation-shaped ambiguity; explicit termination semantics (D-01..D-04) |
| V4 Access Control | Yes | Phase 17 introduces **zero** new K8s write verbs — `start_session` only reaches `k8s.LoadKubeConfig`/`PortForwardToRelay`/`LoadClusterPoliciesForNamespaces` (List/Get + port-forward SubResource), the exact same set `generate.go` already uses. Re-verified structurally, not just asserted — Phase 19's SEC-01 audit re-runs this check on the full tool table, but Phase 17 must not regress it. |
| V5 Input Validation | Yes | Reuse `validateIgnoreProtocols`/`validateIgnoreDropReasons` verbatim (D-06); **new** control this phase must add: explicit range/default validation on `timeout`/`flush_interval` before they reach `PipelineConfig` (Pitfall A) — this is a genuine input-validation gap with a concrete DoS consequence (process-crashing panic on `flush_interval=0`), not a hypothetical hardening nicety |
| V6 Cryptography | Yes | Session-ID randomness via `github.com/google/uuid` (verified `crypto/rand`-backed) — never hand-roll |

### Known Threat Patterns for this phase

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|-----------------------|
| MCP client omits/sends `flush_interval: 0` (or any invalid numeric arg), crashing the whole `cpg mcp` process via an unrecovered goroutine panic | Denial of Service | Explicit default + range validation before constructing `PipelineConfig` (Pitfall A) — validate, don't trust the zero value |
| Client sends a negative duration string (`"-5s"`, which `time.ParseDuration` accepts syntactically) as `timeout`/`flush_interval` | Tampering / DoS | Explicit `> 0` check after `ParseDuration`, alongside the zero-value default |
| Repeated `start_session` calls attempting to spin up many concurrent port-forwards/goroutines | Denial of Service | Already structurally mitigated by SESS-02's single-active-session enforcement — no additional control needed this phase, just don't regress it |
| Session-ID guessing/enumeration to hijack another session's `get_status`/`stop_session` | Spoofing | Not a realistic threat surface in v1.5 (single stdio-local client, single possible session at a time) — still satisfied structurally by `crypto/rand`-backed UUIDs, cost-free |
| Kubeconfig/exec-plugin error text reaching an LLM's context verbatim | Information Disclosure (low severity) | These error paths and their exact wrapped-error text already exist unchanged in `generate.go`/`pkg/k8s` — not new to this phase. What IS new: these strings now travel to an LLM's context/harness telemetry rather than only a human's terminal. No evidence any existing error string embeds a credential/token (kubeconfig loading errors reference paths/config state, not secret material) — acceptable as-is; revisit only if a future error message is observed to echo raw kubeconfig content. |

## Sources

### Primary (HIGH confidence — direct source reads, `go doc`, or executed probes against this repo's exact pinned versions, this session)
- `pkg/hubble/pipeline.go:42-94,96-128,150-162,182,188-191,209,236-322,337-404` — `PipelineConfig`, `SessionStats`, `RunPipeline`/`RunPipelineWithSource`, stats-population point, stdout summary path
- `pkg/hubble/aggregator.go:142-156,361-365` — `NewAggregator` (no interval guard), `time.NewTicker(a.interval)` (Pitfall A)
- `pkg/hubble/client.go:53-94,105-123` — `StreamDroppedFlows`, `waitForConnReady` (`--timeout` bounds only the gRPC dial)
- `pkg/hubble/health_writer.go:88-170,138,193-226` — atomic write pattern, exact `cluster-health.json` path formula, `Snapshot()`
- `pkg/hubble/summary.go` — `PrintClusterHealthSummary` signature (stdout seam, D-09's "stays on stderr" target)
- `pkg/evidence/paths.go:17-25,41-43` — `HashOutputDir`, `ResolvePolicyPath` (Pattern 5's path recipe)
- `pkg/evidence/schema.go`, `pkg/evidence/reader.go` — `SchemaVersion`, `SessionInfo` shape (D-10's internal-ID format target)
- `pkg/output/writer.go:81-102` — confirms SEC-02's atomic temp+rename already landed (Phase 16)
- `pkg/k8s/portforward.go:27-102,58-79,93-94` — `PortForwardToRelay`, `stopCh`/`readyCh`/ctx select, non-blocking `close(stopCh)` cleanup
- `pkg/k8s/client.go:15-25` — `LoadKubeConfig`
- `pkg/k8s/cluster_dedup.go:1-33` — `LoadClusterPoliciesForNamespaces` signature
- `pkg/flowsource/source.go:14-16` — `FlowSource` interface (fake-injection seam for `pkg/session` unit tests)
- `pkg/hubble/pipeline_test.go:25-137` — `mockFlowSource`/`channelFlowSource` pattern, `require.Eventually` shutdown-assertion precedent
- `cmd/cpg/generate.go:58-257` (full file) — the `PipelineConfig` construction recipe Pattern 5 adapts, including the `cluster_dedup`-independent-of-`server` nuance (:197-212)
- `cmd/cpg/commonflags.go:20-37,184-246` — `validateIgnoreProtocols`/`validateIgnoreDropReasons` (unexported, `package main`, Pitfall J)
- `cmd/cpg/mcp.go:1-121` (full file) — `newMCPCmd`, `runMCPServer`, `mcpModeStdout()` (the exact Phase 16→17 handoff)
- `cmd/cpg/mcp_harness_test.go:1-119`, `cmd/cpg/mcp_test.go:1-97`, `cmd/cpg/testhelpers_test.go:1-32` — existing test harness/conventions this phase extends
- `cmd/cpg/main.go:1-104` — `buildLogger`, root command wiring
- `go.mod`, `go list -m` output (executed this session) — confirms zero new dependencies needed
- `go doc github.com/modelcontextprotocol/go-sdk/mcp.{ToolHandlerFor,Tool,ToolAnnotations,CallToolResult,AddTool,Server}` (executed this session, against the exact pinned `v1.6.1` in the local module cache)
- `$(go env GOMODCACHE)/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/server.go:946-973,1485-1491` — `Server.Run`'s two return paths; `ServerSession.cancel`'s "cancellation is preempted [by jsonrpc2]" doc comment (direct confirmation of Pitfall 2/C's mechanism at this exact pinned version)
- `go doc github.com/google/jsonschema-go/jsonschema.{For,ForOptions}` + an executed probe (`jsonschema.For[T]` against a `time.Duration` field, plus `encoding/json` round-trip of `"10s"` vs `10000000000`) against the pinned `v0.4.3` — Pitfall B, run and reverted cleanly this session (`git diff`/`git checkout -- go.mod` confirmed no residual change)
- `$(go env GOMODCACHE)/github.com/google/uuid@v1.6.0/uuid.go:9,40`, `version4.go:27` — confirms `rander = crypto/rand.Reader` default
- `.planning/PROJECT.md:18-20` — confirms the exact stale "tmpdir cleaned at stop_session" wording D-01 supersedes
- `.planning/REQUIREMENTS.md` §Session Lifecycle (SESS-01..06 exact text) — confirms the literal SESS-06 wording D-02 reinterprets
- `.planning/config.json` — `workflow.nyquist_validation: true` (Validation Architecture required), no `security_enforcement` key (Security Domain required, absent = enabled), `commit_docs: true`
- Local environment probes (executed this session): `go version` (go1.25.12), `kubectl version --client`/`kubectl cluster-info` (no reachable cluster), `golangci-lint version` (v2.1.0)

### Secondary (MEDIUM confidence — carried forward from milestone research, already vetted there)
- `.planning/research/SUMMARY.md`, `ARCHITECTURE.md`, `PITFALLS.md`, `STACK.md` (all dated 2026-07-20, this milestone) — Tension 1/3, Pattern 1-3, Pitfall 2/3/8, phase-mapping build order. This phase's research explicitly corrects/supersedes two specific points from these files (the tmpdir-retention sample and the generic `context.WithoutCancel` snippet) — see Pattern 1/2 and State of the Art.
- `.planning/phases/16-mcp-server-foundation-write-safety/16-CONTEXT.md` — D-01..D-06 stdout discipline, the `mcpModeStdout()` handoff this phase completes

### Tertiary (LOW confidence)
None — every claim in this document was either directly verified against source/executed probes this session, or is an explicit CONTEXT.md decision (locked, not a research finding to independently verify).

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new dependencies, all versions confirmed live via `go list -m` against this repo
- Architecture: HIGH — every pattern grounded in direct reads of the actual pinned-version source (both cpg's own code and go-sdk's), not pattern-matched from generic MCP guidance; the two supersession corrections (Pattern 1, Pattern 2) are the highest-value findings and are both independently verifiable by re-reading the cited file:line locations
- Pitfalls: HIGH — the two most severe findings (Pitfall A's ticker panic, Pitfall B's Duration schema shape) were confirmed by direct source read and live code execution respectively, not inference
- Discretionary items (exact deadlines, tmpdir prefix, field names): appropriately left as recommendations, not asserted facts — see Assumptions Log and Open Questions

**Research date:** 2026-07-20
**Valid until:** 30 days (stable Go stdlib + already-pinned dependencies; re-verify if `go-sdk` is bumped past v1.6.1 before this phase is planned/executed)
