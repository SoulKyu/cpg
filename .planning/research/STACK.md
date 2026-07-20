# Stack Research

**Domain:** MCP (Model Context Protocol) server integration, stdio transport, Go 1.25 CLI backend
**Researched:** 2026-07-20
**Confidence:** HIGH

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `github.com/modelcontextprotocol/go-sdk/mcp` | v1.6.1 (latest stable tag, 2026-05-22) | MCP server runtime: session lifecycle, tool registration/dispatch, JSON Schema inference, stdio JSON-RPC framing | **Only Go SDK listed on the official SDK page** (modelcontextprotocol.io/docs/sdk), classified **Tier 1** (same tier as the TypeScript/Python/C# SDKs) and explicitly "maintained in collaboration with Google." Stable `v1.x` — semver-committed, no breaking changes within the major version. Requires `go 1.25.0`; cpg is already on `go 1.25.1` / toolchain `go1.25.12` — zero toolchain change. |
| `go.uber.org/zap/exp/zapslog` | bundled inside the already-pinned `go.uber.org/zap v1.27.1` (no new go.mod line — verified the `exp/zapslog` package exists at the exact `v1.27.1` tag cpg already depends on) | `slog.Handler` adapter that lets go-sdk's internal `*slog.Logger` hook write through cpg's existing zap cores | go-sdk's `ServerOptions.Logger` is a `*slog.Logger`; bridging it into zap means the SDK's own session lifecycle logs (connect/disconnect/errors) land in the same structured stderr stream as the rest of cpg instead of a second, disconnected logging path. |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/google/jsonschema-go` | v0.4.3 (transitive, pinned by go-sdk's own go.mod — do not pin separately) | JSON Schema types + `jsonschema.For[T]` reflection-based schema inference | Invisible plumbing for the common case — `mcp.AddTool[In, Out]` infers `InputSchema`/`OutputSchema` from Go struct types plus `jsonschema:"description text"` field tags. Only import it **directly** if a tool needs schema constraints structs can't express (enum, min/max, regex pattern) — e.g. `dropped-flows` tool's severity filter as an enum. |
| `golang.org/x/sync/errgroup` | v0.20.0 (already a **direct** dependency in cpg's go.mod) | Goroutine-group lifecycle for the background Hubble capture launched by `start_session` running alongside `server.Run` | Already the pattern used in `pkg/hubble/pipeline.go` for the live capture pipeline (`golang.org/x/sync/errgroup` import confirmed at that file). Reuse it for the MCP session runner instead of hand-rolling goroutine/channel bookkeeping — one less concurrency idiom in the codebase. |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `mcp.LoggingTransport` (from the go-sdk itself, no extra dep) | Wraps `&mcp.StdioTransport{}` to mirror raw JSON-RPC traffic to a file/buffer for debugging | Use during development of the new tool handlers: `mcp.NewLoggingTransport(mcp.NewStdioTransport(), logFile)` — writes to any `io.Writer`, never to stdout, so it's safe to leave wired to `os.Stderr` or a debug file behind a `--debug` flag. |
| `MCPGODEBUG` env var | go-sdk's internal debug/compat knobs (e.g. `hintomitempty=1`, `allowsessionsinstateless=1`) | No code change needed; documented in go-sdk's `docs/mcpgodebug.md`. Only relevant if a future SDK bump changes default wire behavior and cpg needs the old behavior temporarily. |
| Existing CI (`golangci-lint`, `govulncheck`, `go test -race`) | Lints/vuln-scans/tests the new MCP code paths | No new tool or config needed — a new `pkg/mcpserver/` (or `cmd/cpg/mcp.go`) package is automatically covered by the existing pipeline. |

## Installation

```bash
# Core — pins the MCP SDK to the exact version researched (consistent with cpg's
# existing exact-pin convention for cilium v1.19.4 and SHA-pinned GH Actions)
go get github.com/modelcontextprotocol/go-sdk/mcp@v1.6.1
go mod tidy

# Nothing else to install:
# - github.com/google/jsonschema-go arrives transitively; `go get` it directly
#   only if/when a tool needs schema constraints beyond struct-tag inference.
# - go.uber.org/zap/exp/zapslog ships inside the already-vendored
#   go.uber.org/zap v1.27.1 — it's an import, not a go.mod change.
```

## Integration with the Existing cobra/zap Stack

**cobra:** `cpg mcp` is one more subcommand, wired exactly like `newGenerateCmd()` / `newReplayCmd()` / `newExplainCmd()` in `cmd/cpg/main.go`. go-sdk's `Server.Run(ctx, transport)` does **not** install SIGINT/SIGTERM handling itself (verified from source: it only selects on `ctx.Done()` vs. the session-closed channel) — the `RunE` must wrap `cmd.Context()` with `signal.NotifyContext` so `stop_session`'s tmpdir cleanup runs on Ctrl-C / harness shutdown, not just on client-initiated disconnect:

```go
func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run cpg as a readonly MCP server over stdio",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			server := mcp.NewServer(&mcp.Implementation{Name: "cpg", Version: version}, &mcp.ServerOptions{
				Logger: slog.New(zapslog.NewHandler(logger.Core())), // logger = existing package-level *zap.Logger
			})
			registerSessionTools(server) // start_session / status / stop_session
			registerQueryTools(server)   // dropped-flows / policies / explain / cluster-health

			return server.Run(ctx, &mcp.StdioTransport{})
		},
	}
}
```

**zap and stdout — the good news, verified, not assumed:** cpg's existing `buildLogger()` (`cmd/cpg/main.go:71-102`) needs **zero changes**. Checked zap's source directly (`config.go`, `master` branch): both `zap.NewProductionConfig()` and `zap.NewDevelopmentConfig()` already default `OutputPaths: []string{"stderr"}` and `ErrorOutputPaths: []string{"stderr"}`, and `zap.NewDevelopment()` is a thin wrapper over the latter. All three of `buildLogger()`'s branches (`--json`, `--debug`, default console) were already stderr-only before this milestone. The only genuine stdout risks for `cpg mcp` are:

1. **Any new `fmt.Println`/`fmt.Printf`/stdlib `log.Print*`** written in the new MCP code — none of the existing zap paths are at risk, but a careless debug print anywhere in the new tool handlers corrupts the newline-delimited JSON-RPC stream. Use the existing `logger` (stderr) or `fmt.Fprintln(os.Stderr, ...)`.
2. **Reusing `pkg/hubble/writer.go` / `pkg/hubble/pipeline.go` unmodified.** Both already have an injectable `io.Writer` seam that defaults to `os.Stdout` when left nil (`writer.go:35,131` — "defaults to os.Stdout when nil"; `pipeline.go:92,358` — same, used today for the CLI's dry-run diff output and the v1.3 session-summary block). `start_session`'s background capture **must** pass that parameter explicitly (`io.Discard`, or a `bytes.Buffer` whose contents get surfaced back through a tool's structured result) rather than leaving it nil — the seam already exists for tests, so this is reuse, not new plumbing.

go-sdk itself is safe by default even without the zap bridge: `ServerOptions.Logger` defaults to `slog.New(slog.DiscardHandler)` when left `nil` (verified in `mcp/logging.go`'s `ensureLogger`) — the SDK never touches stdout *or* stderr unless cpg opts in. Wiring `zapslog` is about **operability** (seeing SDK-internal session errors in cpg's existing structured logs), not about avoiding a stdout leak — that leak simply cannot happen from the SDK's own logging path.

**Readonly guarantee → tool annotations:** go-sdk's `mcp.Tool.Annotations` includes `ToolAnnotations{ReadOnlyHint, IdempotentHint bool; DestructiveHint, OpenWorldHint *bool; Title string}` (verified in `mcp/protocol.go`). Every one of cpg's 7 planned tools (`start_session`, `status`, `stop_session`, dropped-flows, generated-policies, explain/evidence, cluster-health) should set `ReadOnlyHint: true` — this is a spec-level signal to the LLM harness, not just documentation, and it directly encodes the milestone's "never mutates the cluster" contract into the protocol surface itself. (`start_session`/`stop_session` mutate *cpg's own ephemeral tmpdir*, not the cluster — still readonly with respect to the cluster and any files outside the session dir.)

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|--------------------------|
| `github.com/modelcontextprotocol/go-sdk` | `github.com/mark3labs/mcp-go` (v0.56.0) | If the MCP surface were large/dynamic (tools registered/deregistered at runtime), or the team wanted the extra convenience of `server.ServeStdio(s)` (bundles SIGINT/SIGTERM handling that go-sdk makes you wire yourself). It's also more widely adopted by raw GitHub stars (8,910 vs. 4,822 as of 2026-07-20) and predates the official SDK by roughly 1.5 years, so tutorials/examples skew toward it. None of that outweighs the stability gap for cpg's fixed, small (7-tool) surface — see rationale below. |
| Struct-tag `jsonschema` inference (bundled with go-sdk) | Hand-written `jsonschema.Schema` literals, or `invopop/jsonschema` (mcp-go's older, now-superseded schema generator — mcp-go itself migrated to `google/jsonschema-go` by v0.56.0, per its current go.mod) | Only when a field needs constraints structs can't express via tags alone (enum sets, numeric ranges, regex patterns) — then build/customize a `jsonschema.Schema` via `jsonschema.For[T](&jsonschema.ForOptions{...})` and pass it as `Tool.InputSchema`/`OutputSchema` explicitly. |

### Why go-sdk over mcp-go, in detail

- **Official standing.** `modelcontextprotocol.io/docs/sdk` lists exactly one Go SDK — `go-sdk`, Tier 1. `mcp-go` does not appear on that page at all; it is a well-regarded third-party implementation, not an officially recognized one.
- **API stability, evidenced not assumed.** `go-sdk` is `v1.6.1` — a stable major version under semver. `mcp-go` is `v0.56.0` — still pre-1.0, and its own docs currently document **real, recent breaking changes** within the pre-1.0 series: `ClientCapabilities.Sampling`/`ServerCapabilities.Sampling` changed from `*struct{}` to `*mcp.SamplingCapability` (compile-time break), and the struct-tag schema syntax changed from `jsonschema_description:"…"` to `jsonschema:"…"` with `jsonschema:"required"` deprecated in favor of `omitempty` absence. For a milestone that wants to add MCP once and not re-chase the API every few weeks, the stable SDK is the lower-maintenance choice.
- **Institutional backing.** Maintained in collaboration with Google; MCP itself is stewarded by Anthropic. No other Go option has comparable backing.
- **Protocol parity where it matters.** Both SDKs implement the same current spec revision (`2025-11-25` — see table below), so there's no functional-completeness gap driving the choice either way.
- **Structured output fits cpg's query tools directly.** `mcp.AddTool[In, Out]` auto-populates `CallToolResult.StructuredContent` from a typed `Out` return value and auto-infers `OutputSchema` from that type (SEP-2106) — cpg's query tools (dropped flows, generated policies, explain/evidence, cluster health) can return the same Go structs the existing writers/readers already use, with zero manual JSON-schema authoring.
- **Zero HTTP/transport bloat either way.** Both SDKs ship SSE/StreamableHTTP support in the same module as stdio; cpg only imports/uses `&mcp.StdioTransport{}` regardless of which SDK is picked, so this isn't a differentiator — it's a reason neither choice requires an extra transport dependency (see "What NOT to Use").

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|--------------|
| `mcp.SSEHandler` / `mcp.StreamableHTTPHandler` (go-sdk's HTTP transport types) or any HTTP-transport setup | v1.5 scope is stdio-only — the harness spawns `cpg mcp` as a subprocess. HTTP transport pulls in the SDK's OAuth machinery (`golang.org/x/oauth2`, `golang-jwt/jwt/v5`) which is irrelevant here and would add a network-exposed surface + auth code path with nothing exercising or securing it. | `&mcp.StdioTransport{}` only — this was also the coordinator's explicit constraint (no OAuth/authorization; that's an HTTP-transport concern). |
| `fmt.Println` / `fmt.Printf` / bare `log.Print*` anywhere in the new MCP code | Corrupts the newline-delimited JSON-RPC stream on stdout — the harness sees garbled frames and the session breaks silently or fatally. | The existing package-level `*zap.Logger` (already stderr-only) or `fmt.Fprintln(os.Stderr, ...)`. |
| Calling `pkg/hubble/writer.go` / `pipeline.go` diff-writer paths with a `nil` `io.Writer` from inside an MCP tool handler | Both default that parameter to `os.Stdout` when nil (pre-existing behavior for the CLI's `--dry-run` diff and v1.3 session-summary block) — silently corrupts stdio framing if triggered from `start_session`. | Pass `io.Discard` or a captured `bytes.Buffer` explicitly through the parameter that already exists for test injection. |
| A second logging library for the MCP path (slog-only setup, logrus, zerolog, etc.) | Violates the "no new logging lib" constraint and splits structured logs across two pipelines, defeating the point of one `zap`-backed operational log. | `zap`, bridged into go-sdk's `*slog.Logger` hook via `zap/exp/zapslog` (already bundled, no new dependency). |
| `mark3labs/mcp-go`'s `server.ServeStdio(s)` convenience wrapper | Not applicable once go-sdk is the chosen SDK — this is a note for anyone tempted to mix packages. | `server.Run(ctx, &mcp.StdioTransport{})` + explicit `signal.NotifyContext(...)` (see integration snippet above). |

## Stack Patterns by Variant

**If a tool call must not block indefinitely (e.g. `status` on a stuck capture):**
- Rely on the `ctx context.Context` that's already the first parameter of every `ToolHandlerFor[In, Out]` handler.
- Because go-sdk propagates client-side cancellation as a `notifications/cancelled` message directly onto that context (verified in go-sdk's design docs) — no manual polling/timeout plumbing needed beyond a normal `context.WithTimeout` if cpg wants a server-side ceiling too.

**If a tool's output must be both human-readable (for chat transcripts) and machine-parseable (for the harness to act on):**
- Return the typed `Out` struct from the handler and leave `CallToolResult.Content` nil.
- Because go-sdk auto-populates `Content` with JSON text derived from the structured value when `Content` is left unset — cpg gets both channels from a single typed return, no hand-written duplicate text formatting.

**If the binary is invoked as a kubectl plugin vs. standalone (`cpg mcp` today already inherits this ambiguity from `main.go`):**
- Reuse the existing `isKubectlPlugin()` helper when constructing `mcp.Implementation{Name: ...}`.
- Because it's already resolved once at startup for the cobra `Use:` string; the MCP server identity can reflect the same invocation context without a second detection path.

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|------------------|-------|
| `github.com/modelcontextprotocol/go-sdk v1.6.1` | `go 1.25.1` (cpg's module directive) / toolchain `go1.25.12` | SDK's own `go.mod` requires `go 1.25.0` minimum — cpg already exceeds it. Zero toolchain change. |
| `github.com/modelcontextprotocol/go-sdk v1.6.1` | `golang.org/x/oauth2` (cpg currently pins `v0.34.0` indirect) | SDK requires `v0.35.0` → `go mod tidy` will bump this transitively. The package is unused code for cpg (OAuth is HTTP-transport-only, and cpg is stdio-only) — the bump is inert, not a new attack surface to review. |
| `github.com/modelcontextprotocol/go-sdk v1.6.1` | `golang.org/x/tools` (cpg currently pins `v0.44.0` indirect) | SDK requires only `v0.42.0`; Go's minimum-version-selection keeps cpg's existing (newer) `v0.44.0` — no change at all. |
| `github.com/modelcontextprotocol/go-sdk v1.6.1` | `github.com/google/go-cmp v0.7.0` | Already pinned identically in cpg's `go.sum` — no change. |
| `go.uber.org/zap/exp/zapslog` | `go.uber.org/zap v1.27.1` (cpg's existing pin) | Confirmed present at the exact `v1.27.1` tag via GitHub API — import-only, no version bump. Requires Go 1.21+ (cpg's 1.25.x already clears this). |
| Protocol spec revision `2025-11-25` (current "latest" per modelcontextprotocol.io) | `go-sdk` v1.4.0 – v1.6.1 (stable channel) **and** `mark3labs/mcp-go` v0.56.0 | Both SDKs are at wire-protocol parity on the current published spec (plus backward compat to `2025-06-18`, `2025-03-26`, `2024-11-05`) — spec support was not a differentiator in the SDK choice. |
| Protocol spec draft `2026-07-28` | `go-sdk` **v1.7.0-pre.1..pre.3 only** (prerelease, latest `pre.3` published 2026-07-17) | Not yet on modelcontextprotocol.io's public spec pages and not GA in go-sdk. Do not adopt for v1.5 — stay on the `v1.6.1` stable channel and re-check at the next milestone. |

## Sources

- Context7 `/modelcontextprotocol/go-sdk` — stdio transport (`StdioTransport`, `server.Run`), `AddTool`/`ToolHandlerFor` signatures, struct-tag schema inference, `ServerOptions.Logger` default, `ToolAnnotations`, `LoggingTransport` debug helper
- Context7 `/mark3labs/mcp-go` — `ServeStdio`, `WithInputSchema`/`WithOutputSchema`, `NewToolResultStructured`, breaking-change history (Sampling capability type change, schema tag rename)
- Context7 `/uber-go/zap` — `zapslog.NewHandler` usage
- https://modelcontextprotocol.io/docs/sdk — official SDK tier listing; Go = Tier 1, `mcp-go` absent (user-directed source, treated as authority per instructions)
- https://modelcontextprotocol.io/specification/latest — current spec revision `2025-11-25` (user-directed source)
- https://modelcontextprotocol.io/docs/develop/build-server (Go tab) — official Go quickstart: `go get github.com/modelcontextprotocol/go-sdk/mcp`, stdio logging guidance ("never use fmt.Println/fmt.Printf... use log.Println() which defaults to stderr"), `go 1.24+` system requirement
- https://github.com/modelcontextprotocol/go-sdk — README (Google collaboration statement), `releases` (v1.6.1 stable 2026-05-22; v1.7.0-pre.1..3 prereleases through 2026-07-17), `go.mod` at the `v1.6.1` tag (dependency list, `go 1.25.0` directive)
- https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.1/mcp/logging.go — `ensureLogger` default (`slog.New(slog.DiscardHandler)`)
- https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.6.1/mcp/server.go — `Server.Run` has no built-in signal handling (only `ctx.Done()` vs. session-closed select)
- https://github.com/mark3labs/mcp-go — README (protocol `2025-11-25` support statement), `releases` (v0.56.0, 2026-07-09), `go.mod` at the `v0.56.0` tag
- https://raw.githubusercontent.com/uber-go/zap/master/config.go — `NewProductionConfig`/`NewDevelopmentConfig` both default `OutputPaths`/`ErrorOutputPaths` to `["stderr"]`
- GitHub REST API (`gh api`) — repo stats as of 2026-07-20 (go-sdk: 4,822 stars / 68 open issues / pushed 2026-07-17; mcp-go: 8,910 stars / 36 open issues / pushed 2026-07-09); confirmed `exp/zapslog` present in the `uber-go/zap` repo at the `v1.27.1` tag
- Local repo inspection — `/home/gule/Workspace/team-infrastructure/cpg/go.mod`, `go.sum`, `cmd/cpg/main.go` (`buildLogger`, cobra wiring), `pkg/hubble/writer.go`, `pkg/hubble/pipeline.go` (existing `os.Stdout`-defaulting `io.Writer` seams), `pkg/hubble/pipeline.go` (`golang.org/x/sync/errgroup` usage)

---
*Stack research for: MCP server integration (readonly, stdio transport) for cpg v1.5*
*Researched: 2026-07-20*
