# Phase 16: MCP Server Foundation & Write Safety - Research

**Researched:** 2026-07-20
**Domain:** MCP (Model Context Protocol) stdio server skeleton — go-sdk v1.6.1 wiring, stdout wire safety, zap/slog log bridging, atomic file writes — inside an existing Go CLI (cpg)
**Confidence:** HIGH — nearly every load-bearing claim in this document was independently verified this session by reading the actual go-sdk v1.6.1 source at the pinned tag (via direct `curl`/`raw.githubusercontent.com`, not summarized), cross-checked against the live cpg codebase (files read directly, line numbers reconfirmed), and against cobra v1.10.2's actual source. This is a "wrap an already-fully-scoped milestone phase" research pass, not exploratory.

## Summary

Phase 16 has two independent deliverables: (1) a protocol-safe `cpg mcp` stdio server skeleton with zero tools registered, and (2) an atomic-write fix to `pkg/output/writer.go`. Both are small, mechanical, and fully scoped by the milestone-level research (`.planning/research/{SUMMARY,STACK,ARCHITECTURE,PITFALLS}.md`) and by `16-CONTEXT.md`'s locked decisions D-01 through D-06. This document does not re-litigate those decisions — it verifies the exact go-sdk v1.6.1 / zap v1.27.1 / cobra v1.10.2 API surface needed to implement them correctly, and it surfaces one important correction the milestone research got mechanically wrong.

**The one finding that changes the plan:** `16-CONTEXT.md`'s D-01 says "creates the SDK stdio transport first (it captures the real `os.Stdout`)". Direct source verification shows this is **not how `mcp.StdioTransport` behaves**. `StdioTransport` is an empty `struct{}` — constructing `&mcp.StdioTransport{}` captures nothing. Its `Connect(ctx)` method reads the **package-level `os.Stdout` variable lazily, at call time**, and `Connect()` is invoked synchronously *inside* `server.Run()`. If the swap (`os.Stdout = os.Stderr`) happens before `server.Run()` is called (the only place it *can* happen, since `Run()` blocks), `StdioTransport.Connect()` would bind its writer to `os.Stderr`, and **no JSON-RPC would ever reach the real stdout — the server would appear to hang from the client's side.** The fix is mechanical and still fully satisfies D-01's intent: use `mcp.IOTransport{Reader, Writer io.ReadCloser/io.WriteCloser}` instead, constructed with `os.Stdin`/`os.Stdout` as explicit struct-literal field values. Struct-literal field assignment copies the `*os.File` pointer value at that exact statement, so the transport is provably immune to whatever the package-level `os.Stdout` variable is reassigned to afterward. See Architecture Patterns → Pattern A for the exact corrected code.

**Primary recommendation:** Build `cmd/cpg/mcp.go` with `newMCPCmd()` (cobra registration + `SilenceUsage`/`SilenceErrors` + `IOTransport` capture-then-swap) delegating to a small, separately-callable `runMCPServer(ctx, transport mcp.Transport) error` helper that constructs the `mcp.Server` + zapslog bridge and calls `server.Run`. That helper is what the D-04 in-memory-transport test harness calls directly (bypassing real stdio entirely), which is what makes the harness "reusable" for Phases 17-19 as more tools are registered. Mirror `pkg/evidence/writer.go`'s exact atomic temp+rename pattern into `pkg/output/writer.go` verbatim — it is already proven, twice, in this codebase.

## Architectural Responsibility Map

cpg is a stdio CLI/backend process, not a browser/web-tiered app. Tiers below are adapted accordingly.

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| `cpg mcp` cobra subcommand registration | Process Entrypoint | — | Same tier as existing `generate`/`replay`/`explain` registration in `main.go` |
| stdout wire-purity (seam wiring + global swap) | Protocol/Transport Boundary | Process Entrypoint | This *is* the trust boundary between cpg's internals and the MCP host — enforcement belongs at the transport construction site, not scattered across callers |
| stderr unified logging (zap + zapslog bridge) | Observability | — | Pure cross-cutting concern; already fully isolated in `buildLogger()` |
| Atomic policy YAML write (SEC-02) | Storage/Filesystem | — | `pkg/output/writer.go` is cpg's only non-atomic persistence path; this brings it to parity with `pkg/evidence`/`pkg/hubble` health writer |
| Stdout-purity test harness (in-memory transport + `os.Pipe`) | Protocol/Transport Boundary (test infra) | Process Entrypoint (cobra flag-error scenario) | Two distinct mechanisms in one test file — see Validation Architecture |
| Seam-audit unit test (MCP-mode defaults never nil-default to stdout) | Protocol/Transport Boundary (test infra) | — | Tests a helper function, not a live pipeline invocation (see Open Questions) |

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Stdout hardening (SRV-02)**
- **D-01:** Explicit seam wiring **plus** a global backstop: `cpg mcp` startup creates the SDK stdio transport first (it captures the real `os.Stdout`), then sets `os.Stdout = os.Stderr`. Any future stray print (`fmt.Print`, third-party dep) lands visibly in stderr logs instead of corrupting the JSON-RPC wire.
- **D-02:** Human-output seams (`PipelineConfig.Stdout` session summary block, dry-run `diffOut`) are explicitly wired to **stderr** in MCP mode — not `io.Discard` (keeps traces in harness logs), not a per-session buffer (Phase 17 may upgrade the summary seam to a buffer for `stop_session`'s structured summary; that shape belongs to Phase 17).
- **D-03:** `SilenceUsage`/`SilenceErrors` set **on the mcp command only** — cobra honors "executed command OR root", so child-level flags cover all errors during `cpg mcp` (flag parse, `PersistentPreRunE`, `RunE`). Existing CLI UX for generate/replay/explain unchanged. Required regardless of D-01: cobra flag-parse errors occur before `RunE`, i.e. before the swap.

**Stdout-purity test (SRV-02 verification)**
- **D-04:** Reusable in-memory-transport harness (go-sdk `NewInMemoryTransports`) with `os.Stdout` captured via `os.Pipe` while the session is driven. Phase 16 scenario: initialize handshake, empty `tools/list`, unknown method, cobra flag-error path. Phases 17–19 extend the same harness as tools land.
- **D-05:** Plus a **seam-audit unit test**: the MCP-mode config constructor (the function building `PipelineConfig`/`diffOut`/`Silence*` wiring) never leaves an `os.Stdout` default — covers "every stdout-defaulting seam" honestly without a live session.
- **D-06:** Phase 16 assertion semantics: **zero bytes leaked** to the captured `os.Stdout` (`len == 0`) — on an in-memory transport, frames never touch stdout, so any byte is a leak. The "every stdout line parses as a JSON-RPC frame" assertion belongs to Phase 19's real-stdio subprocess e2e (SRV-04); do not duplicate a subprocess test in Phase 16.

**Upstream-locked (research/requirements — do not re-litigate)**
- SDK: `github.com/modelcontextprotocol/go-sdk/mcp` **v1.6.1** (official Tier-1; `mark3labs/mcp-go` rejected — pre-1.0).
- go-sdk internal logs bridged via `zap/exp/zapslog` — already bundled in zap v1.27.1, import-only, no new go.mod line.
- MCP code lives in `cmd/cpg/mcp.go` — never a `pkg/mcp` package (name collision with the SDK's own `mcp` package).
- Structural readonly rule is *decided* here (composition root only ever registers read-only handlers), *verified* in Phase 19 (SEC-01).
- `go mod tidy` will bump transitive `golang.org/x/oauth2` to ≥ v0.35.0 — inert (OAuth is HTTP-transport-only; cpg is stdio-only). Expected diff line, not scope creep.

### Claude's Discretion
- `cpg mcp` command UX: inherits existing persistent flags (`--debug`/`--log-level`/`--json`); `buildLogger()` reused unchanged (already stderr-only); command visible in `cpg --help` with a standard description.
- Atomic writer (SEC-02): mechanically mirror the existing CreateTemp→write→Close→Rename pattern (same-dir temp file, no fsync — match prior art exactly); preserve 0644 perms, the annotate step, and the merge/compare flow.
- Test/harness file layout within `cmd/cpg`.

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within phase scope.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SRV-02 | stdout carries only JSON-RPC frames across a full session lifecycle — enforced by explicit wiring of every stdout-defaulting seam (`PipelineConfig.Stdout`, dry-run `diffOut`, cobra `SilenceUsage`/`SilenceErrors`) and verified by an automated stdout-purity test on an in-memory transport | Architecture Patterns (corrected D-01 mechanism via `IOTransport`), Common Pitfalls (StdioTransport lazy-capture trap), Code Examples (`newMCPCmd`/`runMCPServer` split), Validation Architecture (harness design) |
| SRV-03 | All server logs go to stderr through the existing zap logger; go-sdk internal logging is bridged into it via `zap/exp/zapslog` (one unified log stream) | Standard Stack (`zapslog.NewHandler` verified signature), Code Examples (bridge wiring), existing `buildLogger()` already confirmed stderr-only in all 3 branches |
| SEC-02 | `pkg/output/writer.go` writes policy YAML atomically (temp+rename, same pattern as the evidence and health writers) — landed as an early standalone change before any query tool reads that directory | Code Examples (exact mirror of `pkg/evidence/writer.go:70-88`), Common Pitfalls (errcheck-debt parity note), Validation Architecture (existing test suite compatibility + new atomicity test) |
</phase_requirements>

## Standard Stack

Full milestone-level detail: `.planning/research/STACK.md`. This section covers only what Phase 16 touches, with symbols re-verified against the exact pinned tags this session.

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/modelcontextprotocol/go-sdk/mcp` | v1.6.1 | MCP server runtime: `Server`, `ServerOptions`, `Transport`/`IOTransport`/`StdioTransport`/`InMemoryTransport`, stdio JSON-RPC framing | `[VERIFIED: proxy.golang.org, modelcontextprotocol.io Tier-1 SDK page, direct source read at v1.6.1 tag]` — official reference implementation, locked upstream per CONTEXT.md |
| `go.uber.org/zap/exp/zapslog` | bundled in already-pinned `go.uber.org/zap v1.27.1` — no new go.mod line | `zapslog.NewHandler(core zapcore.Core, opts ...HandlerOption) *Handler` — adapts a `*zap.Logger`'s `Core()` into a `*slog.Logger` for `ServerOptions.Logger` | `[VERIFIED: raw.githubusercontent.com/uber-go/zap/v1.27.1/exp/zapslog/handler.go, HTTP 200]` — package and constructor signature confirmed to exist at the exact pinned tag |

**Version verification (this session, not milestone-inherited):**
```
$ curl -s https://proxy.golang.org/github.com/modelcontextprotocol/go-sdk/@v/v1.6.1.info
{"Version":"v1.6.1","Time":"2026-05-22T11:30:38Z", ...}   # HTTP 200 — confirmed on the Go module proxy (registry-equivalent)

$ curl -s https://proxy.golang.org/go.uber.org/zap/@v/v1.27.1.info
{"Version":"v1.27.1","Time":"2025-11-19T21:20:44Z", ...}  # HTTP 200 — already cpg's exact pin, unchanged
```

**Installation:**
```bash
go get github.com/modelcontextprotocol/go-sdk/mcp@v1.6.1
go mod tidy   # bumps golang.org/x/oauth2 v0.34.0 -> v0.35.0 (transitive, inert — see Upstream-locked)
```

Confirmed via go-sdk's own `go.mod` at the v1.6.1 tag: `golang.org/x/oauth2 v0.35.0` is a **direct** dependency of go-sdk (not just transitive-via-something-else), and `golang.org/x/tools v0.42.0` is required but cpg's existing `v0.44.0` (newer) wins under Go's minimum-version-selection — zero change to that line. No other cpg dependency shifts.

### Verified go-sdk v1.6.1 API surface (this session, direct source reads)

All of the below were read directly from `raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.1/mcp/{server,transport,client}.go` and `.../jsonrpc/jsonrpc.go` and `.../internal/jsonrpc2/messages.go` this session — not from training data, not from a summarized fetch.

```go
// mcp/server.go
func NewServer(impl *Implementation, options *ServerOptions) *Server
func (s *Server) Run(ctx context.Context, t Transport) error
func (s *Server) Connect(ctx context.Context, t Transport, opts *ServerSessionOptions) (*ServerSession, error)
func (s *Server) AddTool(t *Tool, h ToolHandler)                       // Phase 17/18 use — not called this phase
func AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out]) // generic variant, Phase 17/18

type ServerOptions struct {
    Instructions string
    Logger *slog.Logger   // nil -> slog.New(slog.DiscardHandler) internally; SRV-03 sets this
    // ... KeepAlive, PageSize, Capabilities, etc. — not touched by Phase 16
}

type Implementation struct {
    Name    string `json:"name"`
    Title   string `json:"title,omitempty"`
    Version string `json:"version"`
    // WebsiteURL, ... — optional
}

// mcp/transport.go
type Transport interface {
    Connect(ctx context.Context) (Connection, error)  // called exactly once, by Server.Connect/Run
}

type StdioTransport struct{}                                      // ZERO fields
func (*StdioTransport) Connect(context.Context) (Connection, error) {
    return newIOConn(rwc{os.Stdin, nopCloserWriter{os.Stdout}}), nil  // reads globals HERE, lazily
}

type IOTransport struct {                                         // explicit fields — USE THIS for D-01
    Reader io.ReadCloser
    Writer io.WriteCloser
}
func (t *IOTransport) Connect(context.Context) (Connection, error) {
    return newIOConn(rwc{t.Reader, t.Writer}), nil                 // reads STRUCT FIELDS, captured at literal-eval time
}

type InMemoryTransport struct{ /* unexported net.Pipe half */ }
func NewInMemoryTransports() (*InMemoryTransport, *InMemoryTransport)  // net.Pipe()-backed, symmetric

// rwc.Close() (transport.go:340-349) — calls BOTH r.rc.Close() and r.wc.Close() for real.
// StdioTransport protects the real stdout fd from being closed via nopCloserWriter's no-op
// Close(); it does NOT protect stdin (rc.Close() really closes it). Mirror this asymmetry if
// hand-building an IOTransport — see Architecture Patterns, Pattern A.

// mcp/client.go — for the D-04 harness's client-driving side
func NewClient(impl *Implementation, options *ClientOptions) *Client
func (c *Client) Connect(ctx context.Context, t Transport, opts *ClientSessionOptions) (*ClientSession, error)
// Connect() performs the initialize handshake automatically (confirmed via NewInMemoryTransports doc:
// "the client initializes the MCP session during connection").
func (cs *ClientSession) ListTools(ctx context.Context, params *ListToolsParams) (*ListToolsResult, error)

// mcp/server.go:729-739 — listTools handler is UNCONDITIONAL, not gated by capability advertisement:
func (s *Server) listTools(_ context.Context, req *ListToolsRequest) (*ListToolsResult, error) {
    // ... res.Tools = []*Tool{} // avoid JSON null — explicit empty array, not an error, when zero tools registered
}

// jsonrpc/jsonrpc.go — public aliases, usable for the "unknown method" test scenario
type Request = internal/jsonrpc2.Request   // { ID ID; Method string; Params json.RawMessage; Extra any }
type Message = internal/jsonrpc2.Message
```

**Corrections to the milestone-level STACK.md** (flagged honestly per research philosophy — training-knowledge-adjacent claims that direct verification this session shows need adjusting):
1. STACK.md's wiring snippet uses `&mcp.StdioTransport{}` directly inside a cobra `RunE` that (implicitly, per D-01) also needs to swap `os.Stdout`. As shown above, this combination is **broken** — see Architecture Patterns Pattern A for the corrected version using `IOTransport`.
2. STACK.md's dev-tool mention `mcp.NewLoggingTransport(mcp.NewStdioTransport(), logFile)` — **no such constructors exist**. `LoggingTransport{Transport, Writer}` and `StdioTransport{}` are both bare struct literals; there is no `New*` constructor for either. Low-stakes (dev-tool, not a Phase 16 deliverable) but worth not copy-pasting verbatim.

### Cobra `SilenceUsage`/`SilenceErrors` mechanism — verified against cpg's exact pinned v1.10.2

Direct read of `github.com/spf13/cobra@v1.10.2/command.go:1084-1170` (`Command.ExecuteC`), confirming D-03's claimed mechanism exactly:

```go
// cobra's ExecuteC, after cmd.execute(flags) returns a non-nil err (flag-parse error,
// PersistentPreRunE error, or RunE error — exactly the paths D-03 targets):
if !cmd.SilenceErrors && !c.SilenceErrors {   // cmd = resolved subcommand (mcp), c = root
    c.PrintErrln(cmd.ErrPrefix(), err.Error())
}
if !cmd.SilenceUsage && !c.SilenceUsage {
    c.Println(cmd.UsageString())
}
```
Setting `SilenceUsage: true, SilenceErrors: true` on **just** the `mcp` subcommand's `*cobra.Command` struct literal makes `cmd.SilenceErrors`/`cmd.SilenceUsage` both `true`, short-circuiting each `&&` to `false` regardless of root's (default `false`) values — confirming D-03's claim "executed command OR root" is exactly right, and confirming root/`generate`/`replay`/`explain` are unaffected. `[VERIFIED: github.com/spf13/cobra@v1.10.2/command.go, direct read]`.

Note: cpg's own test files (`generate_test.go:285-286`, `explain_test.go:226`, `replay_test.go` ×7) already set `cmd.SilenceUsage = true` / `cmd.SilenceErrors = true` locally to quiet test output — precedent for setting these fields already exists in this codebase, just not yet in production command construction.

## Package Legitimacy Audit

One new external package this phase: `github.com/modelcontextprotocol/go-sdk` (zapslog is import-only from an already-vendored, already-audited dependency — no new audit needed).

**slopcheck result** (installed `slopcheck==0.6.1` this session, ran `slopcheck install github.com/modelcontextprotocol/go-sdk --ecosystem go`):

```
[SUS] github.com/modelcontextprotocol/go-sdk (go)
  > Created 59 days ago. Relatively new.
  > Name ends with '-sdk' -- classic LLM naming pattern. Package exists but the name screams 'LLM bait'.
  > No source repository linked. Harder to verify what this code actually does.
```

> **Note on methodology:** this `slopcheck install` invocation (run with `--force` to see past the SUS gate) actually executed `go get`, modifying this project's `go.mod`/`go.sum` as a side effect. That was reverted immediately via `git restore go.mod go.sum` — confirmed clean via `git status` before continuing. The planner should NOT re-run `slopcheck install` against a real project checkout without `--dry-run`-equivalent caution; treat the verdict captured above as authoritative and do not re-invoke.

**Why the [SUS] flags do not hold up under direct verification** (all three checked this session, not assumed):
1. *"Created 59 days ago"* — this measures the **latest resolved version's publish date** (2026-05-22, confirmed via `proxy.golang.org` above), not the project's age. go-sdk has shipped multiple `v1.x` releases before v1.6.1 (STACK.md's milestone research independently confirmed via GitHub API: 4,822 stars, 68 open issues, pushed 2026-07-17, "maintained in collaboration with Google" per its own README) — 59-day-old **point release** is a normal release cadence for an actively maintained SDK, not a freshness red flag.
2. *"Name ends with '-sdk' — classic LLM naming pattern"* — `go-sdk` is the literal, official name chosen by the MCP steering body for the reference Go implementation, listed by that exact name on `modelcontextprotocol.io/docs/sdk`'s Tier-1 SDK table. This is a generic heuristic false positive on a real project's real name.
3. *"No source repository linked"* — **directly falsified this session**: I fetched real `.go` files (`server.go`, `transport.go`, `client.go`, real license headers "Copyright 2025 The Go MCP SDK Authors") from `github.com/modelcontextprotocol/go-sdk` at the `v1.6.1` tag via both the GitHub REST API and `raw.githubusercontent.com`. The repository unambiguously exists and is the real source. This is a Go-ecosystem metadata-discovery gap in slopcheck (Go module paths *are* their own source location; there is no separate `repository:` field the way npm's `package.json` has one), not a real signal.

| Package | Registry | Age (of resolved version) | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `github.com/modelcontextprotocol/go-sdk` | Go module proxy (`proxy.golang.org`) | 59 days (v1.6.1, 2026-05-22) | N/A (Go proxy doesn't expose download counts) | `github.com/modelcontextprotocol/go-sdk` — confirmed real, read directly at pinned tag | `[SUS]` (see rebuttal above) | **Approved, flagged** — locked upstream per CONTEXT.md; keep, tag inline per protocol |

**Packages removed due to slopcheck [SLOP] verdict:** none.
**Packages flagged as suspicious [SUS]:** `github.com/modelcontextprotocol/go-sdk` [WARNING: slopcheck flagged as suspicious — verify before using. See rebuttal above; this package is the official Tier-1 MCP Go SDK, independently verified via `modelcontextprotocol.io`'s own SDK listing and direct source reads at the pinned tag this session.] The planner should still insert a `checkpoint:human-verify` before the `go get` install task per the Package Legitimacy Gate protocol — this is a process requirement regardless of how strong the rebuttal is, since the package choice itself is upstream-locked and the checkpoint's job is operator awareness, not re-deciding the choice.

Go's module system has no `postinstall`-script mechanism equivalent to npm's — Step 4 of the legitimacy protocol (suspicious postinstall scripts) does not apply to this ecosystem.

## Architecture Patterns

### System Architecture Diagram

```
MCP Host / LLM harness (external process)
    │ spawns `cpg mcp` as a subprocess; writes JSON-RPC requests to child's stdin
    ▼
cpg mcp  (cobra RunE, cmd/cpg/mcp.go — package main)
    │
    ├─[1] ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
    │        (existing precedent: cmd/cpg/generate.go:162-163)
    │
    ├─[2] CAPTURE real stdin/stdout into struct fields — BEFORE any swap:
    │        transport := &mcp.IOTransport{
    │            Reader: os.Stdin,
    │            Writer: noopCloseWriter{os.Stdout},   ◄── copies the *os.File pointer NOW;
    │        }                                              immune to any later os.Stdout reassignment
    │
    ├─[3] os.Stdout = os.Stderr    ◄── GLOBAL BACKSTOP (D-01): any future stray fmt.Print*
    │                                   or third-party dep write now lands on stderr, visible
    │                                   in logs, instead of corrupting the JSON-RPC wire
    ▼
runMCPServer(ctx, transport)   ◄── factored out for testability; the in-memory-transport
    │                               stdout-purity harness (D-04) calls THIS directly,
    │                               substituting an InMemoryTransport for the real IOTransport
    │
    ├─► logger.Core() → zapslog.NewHandler(core) → *slog.Logger
    │      (package-level *zap.Logger from buildLogger(), already stderr-only in all 3
    │       branches — cmd/cpg/main.go:71-102 — reused UNCHANGED)
    ▼
mcp.NewServer(&mcp.Implementation{Name:"cpg", Version: version},
              &mcp.ServerOptions{Logger: bridgedSlogLogger})
    │   ZERO tools registered — Phase 17 adds session tools, Phase 18 adds query tools
    ▼
server.Run(ctx, transport)
    │   internally: s.Connect(ctx, t, nil) [SYNCHRONOUS — reads transport.Reader/Writer,
    │   the CAPTURED real stdin/stdout struct fields, NOT the swapped package var]
    │   then blocks: select { case <-ctx.Done(): ss.Close(); return ctx.Err()
    │                          case err := <-sessionClosed: return err }
    ▼
newline-delimited JSON-RPC ◄═══════════════════════════════════► MCP Host
   (stdin: requests IN)              (stdout: ONLY JSON-RPC frames OUT —
                                       the real *os.File, captured pre-swap)

═══════════════════════════ independent, pre-existing write path (SEC-02 touches only the innermost box) ═══════════════════════════

cpg generate / cpg replay (existing, UNMODIFIED this phase)
    ▼
hubble.RunPipelineWithSource(...) → policyWriter.handle(pe) → output.Writer.Write(pe)
                                                                      │
                                                        [SEC-02 fix lands HERE, this phase]
                                                                      ▼
                                              os.CreateTemp(sameDir) → Write → Close → os.Rename
                                                                      │
                                                                      ▼
                                                  policies/<ns>/<workload>.yaml
                                          (torn-read-safe for Phase 18's future concurrent
                                           MCP query-tool readers — no reader exists yet)
```

### Recommended file layout
```
cmd/cpg/
├── mcp.go            # newMCPCmd(), runMCPServer(), noopCloseWriter, the seam-defaults helper (D-05)
├── mcp_test.go        # cobra flag-error-path scenario (D-04 scenario 4); seam-audit unit test (D-05)
└── mcp_harness_test.go  # reusable in-memory-transport + os.Pipe stdout-capture harness (D-04 scenarios 1-3);
                          # Phases 17-19 extend this file, not reinvent it
pkg/output/
└── writer.go          # SEC-02: os.WriteFile(path, data, 0644) → CreateTemp+Write+Close+Rename
    writer_test.go      # existing 8 tests unchanged (final-state assertions); + 1-2 new atomicity tests
```

### Pattern A: Corrected D-01 mechanism — `IOTransport`, not `StdioTransport`, when a swap is involved

**What:** `mcp.StdioTransport{}` is a zero-field struct; its `Connect()` method reads the **package-level** `os.Stdin`/`os.Stdout` variables lazily, at the moment `Connect()` executes (which is *inside* `server.Run()`, after any code that ran before the `Run()` call). `mcp.IOTransport{Reader, Writer}` takes explicit `io.ReadCloser`/`io.WriteCloser` struct fields — Go evaluates and copies those field values at the point the struct literal executes, which is a distinct, earlier moment than `Connect()`'s lazy global read.

**When to use:** Any time transport construction must happen before a global-variable swap that would otherwise affect what the transport binds to. This is exactly D-01's scenario.

**Example (verified against go-sdk v1.6.1 source this session):**
```go
// cmd/cpg/mcp.go
package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"go.uber.org/zap/exp/zapslog"
)

// noopCloseWriter wraps a writer with a no-op Close, mirroring go-sdk's own
// unexported nopCloserWriter (transport.go:109-113) used inside StdioTransport.
// Without this, IOTransport's rwc.Close() (transport.go:340-349) would really
// close the real stdout fd on session end/ctx-cancel — matching the SDK's own
// deliberate choice to protect stdout (it does NOT protect stdin the same way;
// os.Stdin is passed through directly, matching StdioTransport's own asymmetry).
type noopCloseWriter struct{ io.Writer }

func (noopCloseWriter) Close() error { return nil }

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "mcp",
		Short:         "Run cpg as a readonly MCP server over stdio",
		SilenceUsage:  true, // D-03 — covers flag-parse/PersistentPreRunE/RunE errors for THIS command only
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Capture the REAL stdin/stdout into the transport's own fields
			// BEFORE the global swap below (D-01). Struct-literal field
			// assignment copies the *os.File pointer NOW; the later
			// reassignment of the os.Stdout package variable cannot affect
			// these already-bound fields.
			transport := &mcp.IOTransport{
				Reader: os.Stdin,
				Writer: noopCloseWriter{os.Stdout},
			}

			// Global backstop (D-01).
			os.Stdout = os.Stderr

			return runMCPServer(ctx, transport)
		},
	}
}

// runMCPServer builds the MCP server (zapslog-bridged logging, zero tools —
// Phase 17/18 add them) and runs it against transport. Factored out of RunE
// so the D-04 stdout-purity test harness can call it directly with an
// in-memory transport, exercising the EXACT SAME server-construction and
// logging wiring the real stdio path uses, without touching os.Stdin/os.Stdout
// at all.
func runMCPServer(ctx context.Context, transport mcp.Transport) error {
	bridgedLogger := slog.New(zapslog.NewHandler(logger.Core())) // logger = existing package-level *zap.Logger

	server := mcp.NewServer(
		&mcp.Implementation{Name: "cpg", Version: version},
		&mcp.ServerOptions{Logger: bridgedLogger},
	)
	// Zero tools registered this phase.

	return server.Run(ctx, transport)
}
```

**Trade-offs:** This is one more struct type (`noopCloseWriter`) than the milestone STACK.md snippet implied, but it is the *only* mechanism that makes D-01's stated intent ("transport captures the real stdout before the swap") actually true given go-sdk v1.6.1's real behavior. The alternative — manually splitting `Server.Connect()` + a hand-rolled `select{ctx.Done(), ss.Wait()}` loop to preserve `&mcp.StdioTransport{}` usage — is strictly more code and re-implements what `Server.Run()` already does correctly; `IOTransport` is the smaller diff.

### Pattern B: Atomic write — mirror `pkg/evidence/writer.go` verbatim

**What:** `pkg/output/writer.go:81` currently does `os.WriteFile(path, data, 0644)` — open, truncate, write, close, no atomicity. Two other writers in this exact codebase already solve this identically:

```go
// pkg/evidence/writer.go:70-88 (VERIFIED — read directly this session, line numbers current)
tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
if err != nil {
    return fmt.Errorf("creating temp file: %w", err)
}
tmpPath := tmp.Name()
if _, err := tmp.Write(out); err != nil {
    tmp.Close()
    os.Remove(tmpPath)
    return fmt.Errorf("writing temp file: %w", err)
}
if err := tmp.Close(); err != nil {
    os.Remove(tmpPath)
    return fmt.Errorf("closing temp file: %w", err)
}
if err := os.Rename(tmpPath, path); err != nil {
    os.Remove(tmpPath)
    return fmt.Errorf("atomic rename: %w", err)
}
```
`pkg/hubble/health_writer.go:144-162` repeats the identical pattern with `"health writer: "`-prefixed error strings. Both use same-directory `os.CreateTemp` (same filesystem/volume — required for `os.Rename`'s atomicity guarantee on POSIX), no `fsync` call.

**When to use:** Directly inside `(*Writer).Write` in `pkg/output/writer.go`, replacing the `os.WriteFile(path, data, 0644)` call at line 81 — everything above it (namespace dir creation, existing-policy read/merge, `annotateRules`) stays unchanged; only the final write step changes.

**Trade-offs — explicitly noted, not silently fixed:** The mirrored pattern leaves `tmp.Close()` and `os.Remove(tmpPath)` calls unchecked (bare calls, no `_ = ` or `//nolint`), exactly as the two existing prior-art writers already do. `.golangci.yml` has `errcheck` enabled with no exclusions beyond `check-type-assertions: true` — meaning this mirrored code will trip errcheck on the same lines the prior art already trips it on (STATE.md's `LINT-01`: "16 errcheck" issues tracked as v2/v1.5+ debt). This is **consistent with, not a regression against,** existing debt, and is exactly what CONTEXT.md's Claude's-Discretion note asks for ("match prior art exactly") — do not "improve" the mirrored block with new error handling, that would deviate from the locked mirroring decision and produce a writer that looks inconsistent with its two siblings.

**File permissions:** preserve `0644` exactly as today (`os.CreateTemp` creates the temp file at `0600` by default — this must be explicitly `os.Chmod`'d to `0644` before or via `os.Rename`, OR verified that the existing evidence/health writer pattern already handles this correctly; **verify this specific detail during implementation** — see Open Questions).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| JSON-RPC / MCP protocol framing, initialize handshake, capability negotiation | Custom NDJSON parser/dispatcher | go-sdk's `Server`/`Transport`/`Connection` | Official Tier-1 SDK; spec compliance and the initialize handshake are handled entirely by `Server.Connect`/`Run` |
| `slog` ↔ `zap` log bridging | Custom `slog.Handler` wrapping zap manually | `zap/exp/zapslog.NewHandler(logger.Core())` | Already-vendored (zero go.mod change), purpose-built by the zap maintainers for exactly this |
| Atomic file writes | A file-locking library, or a custom write-ahead-log scheme | `os.CreateTemp` (same dir) → `Write` → `Close` → `os.Rename` | POSIX `rename()` atomicity is already proven twice in this exact codebase; a new dependency for a ~15-line pattern already written correctly twice is pure risk with no benefit |
| stdout/stdin capture in tests | A custom mock-writer/pipe-simulation harness | stdlib `os.Pipe()` + `t.Cleanup` restore | Direct precedent already exists for the *logger* half of this exact idiom: `cmd/cpg/testhelpers_test.go`'s `initLoggerForTesting`/`initObservedLoggerForTesting` swap the package-level `logger` var with `t.Cleanup` restore — extend the same idiom for `os.Stdout` |
| Signal-triggered graceful shutdown | Custom `signal.Notify` + channel plumbing | `signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)` | Direct precedent already shipping in this exact codebase (`cmd/cpg/generate.go:162-163`) |
| "Does cobra silence usage" question | Reading cobra's docs and guessing | Read `ExecuteC` directly (done this session, see Standard Stack) | Cobra's behavior here is a common source of confusion (many assume root-level `Silence*` is required) — verified exact mechanism removes any guesswork |

**Key insight:** every technical need in Phase 16 already has a working precedent somewhere in this exact codebase (atomic writes ×2, context-cancel shutdown ×1, logger test-swapping ×1) or is fully handled by the official SDK. There is no genuinely novel engineering problem in this phase — only correct wiring.

## Common Pitfalls

### Pitfall 1 (NEW — this session's primary finding): `StdioTransport` captures `os.Stdout` lazily, breaking the D-01 swap-ordering assumption

**What goes wrong:** Implementing D-01 literally as worded — "creates the SDK stdio transport first (it captures the real `os.Stdout`), then sets `os.Stdout = os.Stderr`" — using `&mcp.StdioTransport{}` as the transport produces a server that silently binds its writer to `os.Stderr` (the swapped value) the moment `server.Run()` internally calls `Connect()`. The client sees no output at all; from its side, the process looks hung or the connection looks dead. This would NOT be caught by a superficial code review — the code *reads* as if it does the right thing (transport constructed, then swap, then Run).

**Why it happens:** `StdioTransport{}` has zero fields; "constructing" it is a no-op. The actual `os.Stdin`/`os.Stdout` read happens inside `Connect()`, called synchronously by `Run()` — by which point, if the swap statement precedes the `Run()` call (the only place it structurally can, since `Run()` blocks), the global has already changed.

**How to avoid:** Use `mcp.IOTransport{Reader: os.Stdin, Writer: noopCloseWriter{os.Stdout}}` instead — see Architecture Patterns, Pattern A. Struct-literal field assignment captures the pointer value at that statement, immune to later reassignment of the package variable.

**Warning signs:** A stdio-purity test that only checks "no bytes on stdout" (D-06's exact semantics) would **not** catch this bug — zero bytes on the (swapped) stdout is exactly what this broken version produces too, for the wrong reason (nothing is being written anywhere useful, not because nothing leaked). The Phase 19 real-subprocess e2e test (SRV-04) is what would eventually catch this, but only after a lot of confusing manual debugging. Recommend an explicit smoke check during implementation: manually run `cpg mcp` and pipe a raw `initialize` JSON-RPC line into it, confirm a real JSON-RPC response comes back on the terminal.

**Phase to address:** Phase 16, at implementation time — this is not a future-phase concern, it is a correctness bug in the skeleton itself.

### Pitfall 2 (from milestone PITFALLS.md, Phase-16-scoped): stdout is the wire

Full detail: `.planning/research/PITFALLS.md` Pitfall 1. Phase-16-specific nuance verified this session: Phase 16 registers **zero tools** and never calls `hubble.RunPipeline`/`RunPipelineWithSource` (no session manager exists until Phase 17). This means `PipelineConfig.Stdout` (pipeline.go:93/356-358) and `policyWriter.diffOut` (writer.go:35/129-133) are **not on any live code path this phase executes** — the actually load-bearing protections for Phase 16's real `cpg mcp` process are (a) the D-01 global swap, (b) D-03's cobra `Silence*`, and (c) zap's already-verified-stderr-only `buildLogger()`. See Open Questions for what "explicit seam wiring" (D-02/D-05) concretely means for a phase with no live pipeline invocation.

### Pitfall 3 (from milestone PITFALLS.md): no protocol-level test harness

Full detail: `.planning/research/PITFALLS.md` Pitfall 10. Directly actionable this phase — `NewInMemoryTransports()` is confirmed to exist exactly as documented (`net.Pipe()`-backed, symmetric, verified via source read this session). Build the harness now; every later phase extends it rather than inventing a new one.

### Pitfall 4 (minor, corrects STACK.md): `mcp.NewLoggingTransport` does not exist

`LoggingTransport{Transport, Writer}` is a bare struct with no constructor function, confirmed via source read. If a `--debug`-gated raw-JSON-RPC-mirroring dev aid is ever wanted, construct it as `&mcp.LoggingTransport{Transport: realTransport, Writer: os.Stderr}` directly — this is optional, not a Phase 16 deliverable, noted only to prevent a compile-error surprise if someone reaches for the milestone snippet verbatim.

### Pitfall 5: mirrored atomic-writer errcheck lint debt

Covered in Architecture Patterns Pattern B — the mirrored `tmp.Close()`/`os.Remove()` unchecked calls will add 2 more lines to the existing `LINT-01` errcheck debt count. This is expected and consistent with "mirror prior art exactly" — do not silently "fix" it, that would make the three writers inconsistent with each other for no requirement-driven reason.

## Code Examples

### Seam-defaults helper for D-05 (see Open Questions for scope rationale)

```go
// cmd/cpg/mcp.go — small, standalone, unit-testable. Phase 17's pkg/session
// will call this (or an equivalent) when it constructs a real PipelineConfig
// for start_session; Phase 16 pre-builds and pre-tests the contract now.
func mcpModeStdout() io.Writer {
	return os.Stderr // never nil, never os.Stdout — D-02
}
```
```go
// cmd/cpg/mcp_test.go — D-05 seam-audit unit test
func TestMCPModeStdoutNeverDefaultsToRealStdout(t *testing.T) {
	got := mcpModeStdout()
	assert.NotNil(t, got)
	assert.NotEqual(t, os.Stdout, got, "MCP-mode stdout seam must never resolve to the real os.Stdout")
}
```

### Stdout-purity harness skeleton (D-04, scenarios 1-3)

```go
// cmd/cpg/mcp_harness_test.go
func TestMCPStdoutPurity(t *testing.T) {
	// Capture the REAL os.Stdout via os.Pipe — proves nothing leaks there,
	// independent of the in-memory transport used for actual protocol traffic.
	r, w, err := os.Pipe()
	require.NoError(t, err)
	realStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = realStdout })

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	initLoggerForTesting(t) // existing helper, cmd/cpg/testhelpers_test.go

	serverErrCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { serverErrCh <- runMCPServer(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil) // performs initialize handshake automatically
	require.NoError(t, err)
	require.NotNil(t, cs.InitializeResult())

	toolsResult, err := cs.ListTools(ctx, nil) // scenario: empty tools/list
	require.NoError(t, err)
	assert.Empty(t, toolsResult.Tools)

	// scenario: unknown method — low-level Connection, bypassing the typed Client API
	// (jsonrpc.Request is a public alias: github.com/modelcontextprotocol/go-sdk/jsonrpc)
	rawConn, err := clientTransport.Connect(ctx)
	require.NoError(t, err)
	id, _ := jsonrpc.MakeID(1)
	require.NoError(t, rawConn.Write(ctx, &jsonrpc.Request{ID: id, Method: "totally/unknown", Params: json.RawMessage("{}")}))
	resp, err := rawConn.Read(ctx)
	require.NoError(t, err) // a JSON-RPC ERROR response is still a well-formed frame, not a Go error

	cs.Close()
	cancel()
	<-serverErrCh

	w.Close()
	leaked, _ := io.ReadAll(r)
	assert.Empty(t, leaked, "D-06: zero bytes on the real os.Stdout — in-memory transport never touches it")
}
```

> **Correction (revision) — one transport pair per session:** The skeleton above calls `clientTransport.Connect(ctx)` a SECOND time (for the unknown-method scenario) after `client.Connect(ctx, clientTransport, nil)` already consumed that transport half. Each `InMemoryTransport` half is a single `net.Pipe()` end and go-sdk's `Transport.Connect` is contractually "called exactly once" (transport.go:124) — the double `Connect` can hang or flake under `go test -race`. Fix: run the unknown-method scenario against its OWN independent `mcp.NewInMemoryTransports()` pair + its own `runMCPServer` goroutine, sharing only the single `os.Stdout` `os.Pipe` capture (mirrors go-sdk's own `mcp/server_test.go` one-pair-per-session precedent). Plan 16-03 Task 2 implements this split; extend the same one-pair-per-scenario shape in Phases 17-19.

### Cobra flag-error path (D-04, scenario 4 — structurally separate mechanism)

```go
// cmd/cpg/mcp_test.go — exercises newMCPCmd()'s actual cobra wiring, not runMCPServer.
// Fails before RunE ever runs, so it never reaches the transport/swap logic at all.
func TestMCPCobraFlagErrorStaysOffStdout(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	realStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = realStdout })

	cmd := newMCPCmd()
	cmd.SetArgs([]string{"--totally-unknown-flag"})
	_ = cmd.Execute() // error expected; assertion is about stdout, not the error itself

	w.Close()
	leaked, _ := io.ReadAll(r)
	assert.Empty(t, leaked, "D-03: SilenceUsage/SilenceErrors must keep cobra's own error/usage text off stdout")
}
```

## Assumptions Log

Nearly every load-bearing claim in this document was independently verified this session (direct source reads at pinned tags, Go module proxy checks, cobra source reads) rather than carried forward from training data. The one item below is a scoping judgment call, not a factual claim, and is listed here for transparency rather than because it fits the `[ASSUMED]` factual-claim pattern.

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The "seam-defaults helper" framing for D-05 (that Phase 16 pre-builds a small testable helper Phase 17's `pkg/session` will later consume, rather than exercising a live `PipelineConfig` this phase) is this researcher's interpretation of D-02/D-05's intent, reasoned from the fact that Phase 16 registers zero tools and never calls `RunPipeline`. It is not stated verbatim in CONTEXT.md. | Open Questions, Code Examples | Low — if the planner disagrees and wants a fuller `PipelineConfig`-shaped helper now, that's a strictly larger (not contradictory) version of the same idea; no rework, just more surface area than strictly needed this phase |

**If this table looks thin:** that is accurate, not an oversight — this phase's scope maps unusually cleanly onto directly-verifiable SDK/library/codebase facts.

## Open Questions (RESOLVED)

1. **What exactly does "explicitly wired" mean for `diffOut` given it has no external setter?**
   - What we know: `policyWriter.diffOut` (`pkg/hubble/writer.go:35`) is a field on an **unexported** struct, set only inside `pkg/hubble`'s own `RunPipelineWithSource` via `newPolicyWriter(...)` — the caller (future `pkg/session`, and this phase's `cmd/cpg/mcp.go`) has **no hook** to set it. It only matters when `cfg.DryRun == true`; the milestone STACK.md's own guidance says MCP sessions must never set `DryRun: true`.
   - What's unclear: whether D-02's "explicitly wired to stderr" is satisfied structurally (document + assert "MCP mode never sets `DryRun: true`", making `diffOut` provably dead code — zero `pkg/hubble` changes) or requires a small additive `PipelineConfig.DiffOut io.Writer` field threaded into `newPolicyWriter` (defense-in-depth, consistent with D-01's "explicit wiring PLUS a global backstop" philosophy, but a `pkg/hubble` modification ARCHITECTURE.md's build order otherwise marks "unmodified").
   - Recommendation: default to the structural/zero-`pkg/hubble`-change option for Phase 16 (nothing calls `RunPipeline` this phase regardless, so the risk is currently zero either way) and revisit if Phase 17's session design ends up wanting a preview/dry-run tool. Flag explicitly for the planner to decide rather than silently picking one.
   - **RESOLVED (Plan 16-03 objective — structural decision):** The structural / zero-`pkg/hubble`-change option was adopted. Plan 16-03's objective locks "NO `pkg/hubble` change": `diffOut` only matters when `DryRun == true`, MCP mode never sets it, and Phase 16 registers zero tools / never calls `RunPipeline`, so `diffOut` is provably dead code this phase. D-02's intent is honored via a small `mcpModeStdout()` helper (returns `os.Stderr`) pinned by the D-05 seam-audit test — no additive `PipelineConfig.DiffOut` field. Forward-tracked: Phase 17 must call `mcpModeStdout()` when it builds the first MCP-mode `PipelineConfig` (see Plan 16-03's "Handoff to Phase 17" note).

2. **File permission preservation through `os.CreateTemp` + `os.Rename` for SEC-02.**
   - What we know: `os.CreateTemp` creates files at mode `0600` by default (Go stdlib behavior), not `0644`. The existing `pkg/evidence/writer.go`/`pkg/hubble/health_writer.go` atomic writers write JSON that nothing else reads permission-sensitively; `pkg/output/writer.go`'s current direct `os.WriteFile(path, data, 0644)` explicitly sets `0644`.
   - What's unclear: whether `os.Rename` preserves the **temp file's** mode (0600) onto the final path, silently changing generated policy YAML from `0644` to `0600` — this would be an observable regression (GitOps tooling/other readers expecting `0644`) not caught by any of `pkg/output/writer_test.go`'s existing assertions except `TestWriter_FilePermissions` (which explicitly asserts `os.FileMode(0644)` and WOULD catch a regression here — good, but only if the planner keeps this test running against the new code path, which it will by default since it's the same test file).
   - Recommendation: after `os.CreateTemp`, explicitly `os.Chmod(tmpPath, 0644)` before `os.Rename`, OR verify via a quick local experiment that `os.Rename` preserves the destination's semantics some other way. `TestWriter_FilePermissions` (already exists, unchanged) is the correctness gate — make sure it passes, don't just assume.
   - **RESOLVED (Plan 16-01 Task 1, step 4 — atomic-writer chmod):** The explicit `os.Chmod(tmpPath, 0644)`-before-`os.Rename` recommendation was adopted. Plan 16-01 Task 1 step 4 chmods the temp file to 0644 between `tmp.Close()` and the rename (a deliberate deviation from the evidence/health analogs, which never chmod), so generated policy YAML keeps its 0644 mode instead of `os.CreateTemp`'s default 0600. `TestWriter_FilePermissions` (writer_test.go:126) remains the regression gate.

3. **Exact placement of the `runMCPServer` test-injection seam relative to zap logger construction.**
   - What we know: `runMCPServer` as sketched reaches for the package-level `logger` var directly (matching `generate.go`/`replay.go`/`explain.go`'s existing convention), which means SRV-03's log-bridging behavior is testable via the existing `initObservedLoggerForTesting(t)` helper.
   - What's unclear: whether the planner wants `runMCPServer` to assert/require `logger != nil` explicitly (defensive), given `PersistentPreRunE` always sets it in production but a hand-constructed test call to `runMCPServer` could theoretically skip that if `initLoggerForTesting`/`initObservedLoggerForTesting` isn't called first.
   - Recommendation: low-stakes; a nil-`logger` would panic on `.Core()` immediately and loudly in any test that forgets the setup helper — acceptable fail-fast behavior, no special-casing needed.
   - **RESOLVED (research recommendation adopted — no special-casing):** The low-stakes "no defensive nil-`logger` check" recommendation was adopted. Plan 16-03's `runMCPServer` reads the package-level `logger` directly (matching the generate/replay/explain convention); a nil `logger` panics loudly on `.Core()` in any test that forgets `initLoggerForTesting`/`initObservedLoggerForTesting` — acceptable fail-fast, no special-casing added.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Building/testing all Phase 16 code | Yes | `go1.25.12` (verified via `go version` this session) — exact match to `go.mod`'s `toolchain go1.25.12` directive | — |
| Network access to `proxy.golang.org` / GitHub | `go get github.com/modelcontextprotocol/go-sdk/mcp@v1.6.1` | Yes (verified this session — successful module-proxy fetches, GitHub API/raw content fetches) | — | If offline at implementation time: pre-populate `GOMODCACHE` or vendor the module |
| Kubernetes cluster / Hubble Relay | **Not required this phase** — Phase 16 registers zero tools and never calls `hubble.RunPipeline` | N/A | — | — |

Phase 16 is the rare phase in this milestone with no runtime external-service dependency at all — consistent with ARCHITECTURE.md's stated rationale for sequencing it first ("cheap to build, de-risks everything downstream").

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `github.com/stretchr/testify` (require/assert) + `go.uber.org/zap/zaptest`/`zaptest/observer` for structured log assertions — all already in use throughout the codebase, zero new dependency |
| Config file | none — `go test` needs no config; `.golangci.yml` governs lint only, not test execution |
| Quick run command | `go test ./cmd/cpg/... -run TestMCP -v` (scoped to new mcp files) / `go test ./pkg/output/... -run TestWriter -v` (SEC-02) |
| Full suite command | `go test ./... -race` (existing project-wide discipline — "484 tests across 10 packages" per STATE.md, all `-race`-clean) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SRV-02 | Zero bytes leak to real `os.Stdout` across initialize/empty-tools-list/unknown-method on an in-memory transport | integration (in-memory transport + `os.Pipe` capture) | `go test ./cmd/cpg/... -run TestMCPStdoutPurity -race -v` | ❌ Wave 0 — new `cmd/cpg/mcp_harness_test.go` |
| SRV-02 | Cobra flag-error path never writes to real `os.Stdout` | integration (cobra `SetArgs`+`Execute` + `os.Pipe` capture) | `go test ./cmd/cpg/... -run TestMCPCobraFlagErrorStaysOffStdout -v` | ❌ Wave 0 — new `cmd/cpg/mcp_test.go` |
| SRV-02 | Seam-audit: MCP-mode stdout defaults never resolve to `os.Stdout` | unit | `go test ./cmd/cpg/... -run TestMCPModeStdoutNeverDefaultsToRealStdout -v` | ❌ Wave 0 — new `cmd/cpg/mcp_test.go` |
| SRV-03 | go-sdk internal logs bridge into cpg's existing stderr zap stream via `zapslog` | unit (assert `ServerOptions.Logger` non-nil, wraps `logger.Core()`; optionally drive a log-triggering SDK event and assert it appears via `initObservedLoggerForTesting`) | `go test ./cmd/cpg/... -run TestMCPLogging -v` | ❌ Wave 0 — new `cmd/cpg/mcp_test.go` |
| SEC-02 | `pkg/output/writer.go` writes via temp+rename; no torn reads under concurrent access | unit + race | `go test ./pkg/output/... -race -v` | ✅ existing `pkg/output/writer_test.go` (8 tests, all final-state assertions — expected to pass unchanged) + ❌ 1-2 new tests (no leftover `.tmp-*` files; concurrent reader-while-writing under `-race`) |

### Sampling Rate

- **Per task commit:** the quick-run command scoped to whichever package the task touched (`cmd/cpg` or `pkg/output`)
- **Per wave merge:** `go test ./... -race`
- **Phase gate:** full suite green + `golangci-lint run` (existing CI gate) before `/gsd-verify-work`

### Wave 0 Gaps

- [ ] `cmd/cpg/mcp.go` — does not exist yet; `newMCPCmd`/`runMCPServer`/`noopCloseWriter`/seam-defaults helper all net-new
- [ ] `cmd/cpg/mcp_test.go` — seam-audit unit test + cobra flag-error scenario
- [ ] `cmd/cpg/mcp_harness_test.go` — reusable in-memory-transport + `os.Pipe` stdout-purity harness (D-04 scenarios 1-3); this file is explicitly designed to be extended by Phases 17-19, not rewritten
- [ ] `go.mod`/`go.sum` — `go get github.com/modelcontextprotocol/go-sdk/mcp@v1.6.1` + `go mod tidy` not yet run (gate behind `checkpoint:human-verify` per Package Legitimacy Audit)
- [ ] `pkg/output/writer_test.go` — extend with 1-2 new atomicity-focused tests (no template exists in this codebase for a live concurrent-reader-during-write race test; `pkg/hubble/health_writer_test.go`'s `TestHealthWriterAtomicWrite` only asserts post-hoc file validity, not concurrent access — author this test fresh, informed by Pitfall 5's "poll while a writer goroutine actively appends" framing in the milestone PITFALLS.md)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture, Design and Threat Modeling | yes | Trust-boundary separation between the JSON-RPC wire (stdout) and diagnostic output (stderr) is itself an architectural security control — this phase's entire purpose |
| V2 Authentication | no | Out of scope this phase; stdio servers get credentials from the environment (Phase 17/19 concern for kubeconfig, not applicable to a zero-tool skeleton) |
| V3 Session Management | no | No MCP session-scoped tools exist yet this phase |
| V4 Access Control | no | No tools registered — SEC-01's structural readonly rule is *decided* conceptually but has nothing to *enforce against* yet (zero tool handlers exist) |
| V5 Input Validation | partial | The "unknown method" test scenario exercises the SDK's own JSON-RPC-level input handling (method-not-found), not cpg-authored validation logic — cpg has no custom parsing this phase |
| V6 Cryptography | no | Not touched this phase |
| V7 Error Handling and Logging | yes | Structured, stderr-only logging (zap + zapslog bridge) is exactly V7's concern; no sensitive data flows through this phase's logs (no session/query data exists yet to leak) |
| V12 File and Resources | yes | SEC-02's atomic writer is squarely a file-integrity control — prevents a reader (future Phase 18 query tool) from observing a corrupt/partial file |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Stray non-JSON-RPC byte on stdout corrupts the wire, silently breaking client parsing | Tampering (protocol stream integrity) | D-01 explicit-seam-wiring + global backstop swap; automated stdout-purity test (D-04/D-06) — this phase's core deliverable |
| Torn/partial read of `policies/**.yaml` by a concurrent reader (future Phase 18 query tool) | Tampering (data integrity under concurrency) | SEC-02's atomic temp+rename write, mirroring already-proven `pkg/evidence`/`pkg/hubble` health-writer prior art |
| Process fails to exit on `ctx.Done()` (SIGTERM/SIGINT), wedging the MCP host's shutdown sequence | Denial of Service | `server.Run`'s documented behavior (verified this session): `select` on `ctx.Done()` vs. session-closed, explicitly closes the connection and returns `ctx.Err()` on cancellation — no additional cpg-side code needed for this phase's zero-tool skeleton; becomes relevant again in Phase 17 once a session goroutine exists that must also observe cancellation |
| A future `cpg apply` command (already "Planned" in PROJECT.md) sharing binary wiring with `cpg mcp`'s eventual tool table | Elevation of Privilege | Not this phase's concern to fix, but this phase is where the pattern of "composition root only calls what it's given" starts — `runMCPServer` registers exactly zero tool handlers, establishing the discipline Phase 17-19 must continue |

## Sources

### Primary (HIGH confidence — direct verification this session)
- `raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.1/mcp/server.go` — `NewServer`, `ServerOptions` (full struct), `Server.Run` (exact body, doc comment), `Server.Connect`, `listTools` handler behavior with zero tools
- `raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.1/mcp/transport.go` — `StdioTransport` (confirmed zero-field, lazy global read in `Connect()`), `IOTransport` (explicit fields), `NewInMemoryTransports` (`net.Pipe()`-backed), `LoggingTransport` (no constructor), `rwc.Close()` semantics (real close on both sides unless wrapped)
- `raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.1/mcp/client.go` — `NewClient`, `Client.Connect` (auto-initialize), `ClientSession.ListTools`
- `raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.1/mcp/protocol.go` — `Implementation` struct fields
- `raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.1/jsonrpc/jsonrpc.go` + `internal/jsonrpc2/messages.go` — `jsonrpc.Request` public alias and fields, for the "unknown method" test mechanism
- `raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.1/go.mod` — confirmed `golang.org/x/oauth2 v0.35.0` direct dependency, `go 1.25.0` minimum
- `raw.githubusercontent.com/uber-go/zap/v1.27.1/exp/zapslog/handler.go` — `zapslog.NewHandler(core zapcore.Core, opts ...HandlerOption) *Handler` exact signature, HTTP 200 confirmed at the exact pinned tag
- `raw.githubusercontent.com/spf13/cobra/v1.10.2/command.go` — `Command.ExecuteC` full body (lines 1084-1170), confirming D-03's "executed command OR root" `SilenceUsage`/`SilenceErrors` mechanism exactly
- `proxy.golang.org/github.com/modelcontextprotocol/go-sdk/@v/v1.6.1.info` and `.../go.uber.org/zap/@v/v1.27.1.info` — registry-equivalent existence/version/date confirmation (HTTP 200 both)
- `slopcheck==0.6.1` (installed this session) — ran against `github.com/modelcontextprotocol/go-sdk` with `--ecosystem go`, produced the `[SUS]` verdict quoted and rebutted in Package Legitimacy Audit
- Local repo inspection (this session, direct reads): `cmd/cpg/main.go` (full file), `pkg/hubble/pipeline.go` (full file), `pkg/hubble/writer.go` (full file), `pkg/output/writer.go` (full file), `pkg/evidence/writer.go` (full file), `pkg/hubble/health_writer.go` (full file), `pkg/output/writer_test.go` (full file), `cmd/cpg/testhelpers_test.go` (full file), `cmd/cpg/generate.go:140-210`, `cmd/cpg/explain.go` (grep for `OutOrStdout`), `go.mod`, `.golangci.yml`, `.planning/config.json`

### Secondary (MEDIUM confidence — inherited from same-day milestone research, not independently re-verified this session)
- `.planning/research/STACK.md` — GitHub star/issue counts for go-sdk vs. mcp-go (4,822 vs 8,910 stars); general ecosystem-adoption framing
- `.planning/research/ARCHITECTURE.md`, `.planning/research/PITFALLS.md` — broader milestone architecture/pitfall context this phase document narrows and corrects where directly re-verified

### Tertiary (LOW confidence)
None load-bearing in this document.

## Metadata

**Confidence breakdown:**
- Standard stack (go-sdk/zapslog/cobra API surface): HIGH — every symbol used in the Code Examples was read directly from source at the exact pinned tag this session, not recalled from training data
- Architecture (D-01 corrected mechanism, atomic-writer mirror): HIGH — the `StdioTransport`/`IOTransport` finding is the single most rigorously verified claim in this document (read the actual `Connect()` method bodies); atomic-writer pattern is a byte-for-byte read of existing, already-shipping cpg code
- Pitfalls: HIGH for the new StdioTransport finding and the cobra mechanism (both direct source reads); MEDIUM-HIGH for the inherited milestone pitfalls narrowed to Phase-16 scope (re-verified line numbers, not re-verified every underlying claim)
- Package legitimacy: HIGH — slopcheck's `[SUS]` verdict is reported honestly per protocol, and independently rebutted point-by-point with direct evidence gathered this session (not hand-waved away)

**Research date:** 2026-07-20
**Valid until:** 30 days for the codebase-structure claims (line numbers will drift as Phase 16 itself is implemented — re-grep before Phase 17 planning if this document is reused); go-sdk API surface claims are stable for the `v1.6.1` release line per semver (re-verify only if the pin changes, e.g. to the `v1.7.0` prerelease line noted as explicitly out of scope in STACK.md)
