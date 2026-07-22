---
phase: 16-mcp-server-foundation-write-safety
plan: 03
subsystem: api
tags: [mcp, go-sdk, cobra, zap, slog, zapslog, jsonrpc, stdio]

# Dependency graph
requires:
  - phase: 16-mcp-server-foundation-write-safety (plan 02)
    provides: github.com/modelcontextprotocol/go-sdk v1.6.1 pinned in go.mod (no importer until this plan)
provides:
  - "cpg mcp cobra subcommand, registered in main.go, zero tools registered"
  - "IOTransport capture-then-swap stdout-purity mechanism (D-01), with noopCloseWriter protecting the real stdout fd"
  - "zapslog bridge (bridgedSlogLogger) unifying go-sdk internal logs into cpg's existing stderr zap stream (SRV-03)"
  - "mcpModeStdout() seam helper pinning the MCP-mode human-output contract to os.Stderr (D-02/D-05), contract-pinned but not yet wired to a live PipelineConfig"
  - "reusable mcp_harness_test.go in-memory-transport + os.Pipe stdout-purity harness (startInMemoryMCPSession), extensible by Phases 17-19"
affects: [17-session-tools, 18-query-tools, 19-security-hardening-and-e2e]

# Tech tracking
tech-stack:
  added:
    - "go.uber.org/zap/exp v0.3.0 (zapslog package) — NEW independent dependency, see Deviations"
    - "github.com/modelcontextprotocol/go-sdk v1.6.1 — promoted from indirect to direct (first importer)"
  patterns:
    - "IOTransport struct-literal capture BEFORE global os.Stdout=os.Stderr swap (never mcp.StdioTransport — lazy-capture trap)"
    - "newMCPCmd/runMCPServer split for testability: RunE does capture+swap+signal wiring, runMCPServer is the pure, transport-agnostic composition root"
    - "startInMemoryMCPSession(ctx) (client, drain) harness helper: one mcp.NewInMemoryTransports() pair per protocol scenario, each half Connect-ed exactly once, shutdown via context cancellation (mirrors go-sdk's own TestServerRunContextCancel)"
    - "seam-defaults helper (mcpModeStdout) pre-built and contract-pinned by a unit test before any live call site exists"

key-files:
  created:
    - cmd/cpg/mcp.go
    - cmd/cpg/mcp_harness_test.go
    - cmd/cpg/mcp_test.go
  modified:
    - cmd/cpg/main.go
    - go.mod
    - go.sum

key-decisions:
  - "go.uber.org/zap/exp v0.3.0 added as a new direct dependency (operator-approved) — zapslog is NOT bundled inside go.uber.org/zap; correction to 16-CONTEXT.md/16-RESEARCH.md, see Deviations"
  - "D-01: mcp.IOTransport{Reader: os.Stdin, Writer: noopCloseWriter{os.Stdout}} constructed before os.Stdout=os.Stderr; mcp.StdioTransport never used (its Connect() lazily re-reads the swapped os.Stdout)"
  - "D-02/D-05: mcpModeStdout() returns os.Stderr, seam-audit-tested; diffOut needs no pkg/hubble change (MCP mode never sets DryRun:true this phase, so it is provably dead code)"
  - "D-03: SilenceUsage/SilenceErrors set on the mcp cobra command only, not rootCmd"
  - "Session B's raw unknown-method scenario relies on go-sdk's jsonrpc2.processResult always writing a Response for any call-shaped Request (verified in internal/jsonrpc2/conn.go) — confirmed a well-formed JSON-RPC error frame comes back even though the session was never initialized, avoiding a subprocess-level assumption"
  - "Harness shutdown uses context cancellation (not session.Close()) as the primary mechanism, mirroring go-sdk's own mcp/cmd_test.go TestServerRunContextCancel precedent exactly"

patterns-established:
  - "cmd/cpg/mcp_harness_test.go's startInMemoryMCPSession: Phases 17-19 add new protocol/tool scenarios with one more call to this helper, not by reinventing transport plumbing"
  - "mcpModeStdout() as the single source of truth for the MCP-mode stderr-only human-output seam — Phase 17 must call it, not redefine an equivalent"

requirements-completed: [SRV-02, SRV-03]

# Metrics
duration: ~35min (commits span 7min; includes go-sdk/zapslog/jsonrpc2 source verification against the local module cache)
completed: 2026-07-20
---

# Phase 16 Plan 03: MCP Server Skeleton (cpg mcp) Summary

**`cpg mcp` cobra subcommand wiring a zero-tool go-sdk v1.6.1 server over `mcp.IOTransport` (stdout captured before the `os.Stdout=os.Stderr` backstop swap), with go-sdk logs bridged into cpg's existing stderr zap stream via the newly-added `go.uber.org/zap/exp/zapslog` dependency, verified by a reusable in-memory-transport stdout-purity harness.**

## Performance

- **Duration:** ~35 min (three commits span 2026-07-20T17:00:42Z–17:07:48Z; additional time spent verifying go-sdk v1.6.1 / zap/exp v0.3.0 / jsonrpc2 API surface directly against the local Go module cache before writing test code)
- **Started:** 2026-07-20T17:00:42Z (approx., first task commit)
- **Completed:** 2026-07-20T17:10:26Z
- **Tasks:** 3/3 completed
- **Files modified:** 6 (3 created, 3 modified)

## Accomplishments

- `cpg mcp` is a registered, buildable, protocol-safe cobra subcommand with zero tools registered — the composition root Phases 17-19 build session/query tools onto.
- Stdout-purity proven end-to-end on in-memory transports: two independent sessions (initialize + empty tools/list; unknown method) leak zero bytes to the real `os.Stdout`, plus a cobra flag-error path and a seam-audit unit test cover the other two stdout-defaulting seams (D-04's four scenarios, all green under `-race`).
- go-sdk internal logs unified into cpg's existing stderr-only zap stream via `zapslog.NewHandler(logger.Core())`.
- Corrected a factual error in the locked phase research before it could propagate into Phases 17-19 (see Deviations).

## Task Commits

Each task was committed atomically:

1. **Task 1: Create cmd/cpg/mcp.go (server skeleton) and register it in main.go** - `93e7b3e` (feat)
2. **Task 2: Create cmd/cpg/mcp_harness_test.go (reusable in-memory stdout-purity harness)** - `4568c19` (test)
3. **Task 3: Create cmd/cpg/mcp_test.go (seam-audit, cobra flag-error, zapslog-bridge tests)** - `2f19514` (test)

**Plan metadata:** SUMMARY.md commit (this file) — see below.

## Files Created/Modified

- `cmd/cpg/mcp.go` - `noopCloseWriter`, `newMCPCmd`, `runMCPServer`, `bridgedSlogLogger`, `mcpModeStdout`; the IOTransport capture-then-swap skeleton
- `cmd/cpg/mcp_harness_test.go` - `startInMemoryMCPSession` helper + `TestMCPStdoutPurity` (two independent in-memory sessions, one shared `os.Stdout` pipe capture)
- `cmd/cpg/mcp_test.go` - `TestMCPModeStdoutNeverDefaultsToRealStdout`, `TestMCPCobraFlagErrorStaysOffStdout`, `TestMCPLoggingBridgesToZapStderr`
- `cmd/cpg/main.go` - added `rootCmd.AddCommand(newMCPCmd())`
- `go.mod` / `go.sum` - `github.com/modelcontextprotocol/go-sdk v1.6.1` promoted indirect→direct; `go.uber.org/zap/exp v0.3.0` added direct (new dependency, see Deviations)

## Decisions Made

- **D-01 mechanism confirmed correct as researched:** `mcp.IOTransport{Reader: os.Stdin, Writer: noopCloseWriter{os.Stdout}}` constructed as a struct literal BEFORE `os.Stdout = os.Stderr`; `mcp.StdioTransport` never used anywhere in `mcp.go` (only referenced in doc comments explaining why it's avoided).
- **D-02/Open-Question-#1 structural answer (locked by the plan objective, implemented as specified):** no `pkg/hubble` change. `mcpModeStdout()` returns `os.Stderr`, pinned by a seam-audit unit test; `diffOut` needs no wiring since MCP mode never sets `DryRun: true` and Phase 16 never calls `RunPipeline`.
- **Harness shutdown via context cancellation, not `session.Close()`:** verified against go-sdk's own `mcp/cmd_test.go` `TestServerRunContextCancel` (same `ctx` passed to both `server.Run` and `client.Connect`; `cancel()` is what unblocks `server.Run`'s `select`). This is more deterministic than relying on `Close()`-triggered pipe-closure propagation and matches upstream's own tested pattern exactly.
- **Session B (unknown method) verified against the actual jsonrpc2 dispatch code, not assumed:** `ServerSession.handle` rejects any non-`initialize`/`ping`/`notifications/initialized` method on an uninitialized session with `fmt.Errorf("method %q is invalid during session initialization", ...)` — but `internal/jsonrpc2/conn.go`'s `processResult` unconditionally converts ANY handler error into a written-back `Response` for a call-shaped request (one with an ID), so the raw client's `Read` still receives a well-formed JSON-RPC error frame, never a Go transport error or a hang. Confirmed this by reading the actual dispatch code in the local module cache, not by inference from the plan text alone.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking, escalated per package-legitimacy protocol] `zap/exp/zapslog` is an independently versioned module, not bundled in `go.uber.org/zap`**

- **Found during:** the PRIOR agent's Task 1 attempt (before this continuation began), at the human-verify checkpoint gate.
- **Issue:** `16-CONTEXT.md` ("Upstream-locked" section) and `16-RESEARCH.md` (Standard Stack table + Sources) both stated: *"go-sdk internal logs bridged via `zap/exp/zapslog` — already bundled in zap v1.27.1, import-only, no new go.mod line."* This is factually wrong. `go.uber.org/zap/exp` is a **separate Go module** (confirmed via `proxy.golang.org/go.uber.org/zap/exp/@v/list` → `v0.1.0`, `v0.2.0`, `v0.3.0`; its own `go.mod` requires `go.uber.org/zap v1.26.0`, not the same module as `go.uber.org/zap` itself). The plain module path `go.uber.org/zap/exp/zapslog` does not resolve (404 on the proxy) — only `go.uber.org/zap/exp` resolves as a module, with `zapslog` as a package within it.
- **Resolution:** the prior agent surfaced this as a `checkpoint:human-verify` (package-legitimacy gate, since a `go get` of an unplanned new dependency is excluded from auto-fix per the deviation rules). **Operator response: "verified: option A"** (2026-07-20) — approved adding `go.uber.org/zap/exp` as a new direct dependency at `v0.3.0`.
- **Fix (this continuation, Task 1):** `go get go.uber.org/zap/exp/zapslog@v0.3.0` (resolves to module `go.uber.org/zap/exp` v0.3.0) followed by `go mod tidy`. Re-verified the exact API this session by unzipping the module from the proxy: `zapslog/handler.go` contains `func NewHandler(core zapcore.Core, opts ...HandlerOption) *Handler` — matches `bridgedSlogLogger()`'s usage (`zapslog.NewHandler(logger.Core())`) exactly, no code changes needed beyond the dependency addition itself.
- **Files modified:** `go.mod`, `go.sum` (part of Task 1's commit `93e7b3e`)
- **Verification:** `go build ./...`, `go vet ./cmd/cpg/...`, full `go test ./... -race` all green; `go.mod` shows `go.uber.org/zap/exp v0.3.0` as a direct require alongside the pre-existing `go.uber.org/zap v1.27.1`.
- **Committed in:** `93e7b3e` (Task 1 commit)

**Correction recorded here for Phases 17-19:** do not assume `zapslog` is free/bundled. It is `go.uber.org/zap/exp v0.3.0` (a distinct, independently-versioned module in the same git repo's `exp/` subdirectory, tagged `exp/v0.3.0`), already present in `go.mod` as of this plan. No further action needed downstream — just don't re-introduce the "bundled, no go.mod line" assumption if it resurfaces in future research.

---

**Total deviations:** 1 auto-fixed via escalated package-legitimacy checkpoint (operator-approved before this continuation began; this continuation executed the approved fix and re-verified the exact API against the real module contents).
**Impact on plan:** No scope creep — same `zapslog.NewHandler` call site and behavior the plan specified; only the go.mod mechanics differ from what the research assumed. All three tasks otherwise executed exactly as planned.

## Issues Encountered

- **`jsonrpc.MakeID(1)` rejected an `int` argument** (`parse error: invalid ID type int`) during Task 2 test development. `MakeID` only accepts `nil`, `float64`, or `string` per its doc comment and implementation (`internal/jsonrpc2/messages.go`). Fixed by passing `float64(1)` instead of the bare int literal `1` used in the RESEARCH.md code skeleton (`jsonrpc.MakeID(1)`), which would have failed identically if copied verbatim. Caught immediately by the task's own `-race` test run before commit; not a deviation from plan intent, just a literal-type correction to the skeleton code, folded into Task 2's commit (`4568c19`).
- A `-race` run surfaced a data race on the package-level `logger` variable the FIRST time the `MakeID` bug caused an early test abort (test cleanup racing with a still-running session-B goroutine that never got `cancel()`+drained). This resolved itself once the `MakeID` fix let the test reach its normal `cancelB()`/`drainB()` shutdown path — re-ran 10x under `-race` afterward with zero races, confirming it was a downstream symptom of the abort, not an independent bug in the harness's synchronization design.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

**Handoff to Phase 17 (D-02 forward obligation — carried forward from the plan objective, not silently dropped):**

`mcpModeStdout()` (`cmd/cpg/mcp.go`) is defined and seam-audit-tested (`TestMCPModeStdoutNeverDefaultsToRealStdout`, `cmd/cpg/mcp_test.go`) in this plan, but has **no reachable call site yet** — Phase 16 registers zero tools and never constructs a `PipelineConfig`. D-02's "human-output seams explicitly wired to stderr in MCP mode" is therefore only **contract-pinned** here, not literally connected.

**Phase 17's session-construction code — the first place an MCP-mode `PipelineConfig` is built (for `start_session`) — MUST set `PipelineConfig.Stdout = mcpModeStdout()`.** This completes the deferred D-02 wiring that this plan only pinned via the seam-audit test. This obligation should be recorded in STATE.md's Accumulated Context when the orchestrator merges this wave (this SUMMARY is the source record — the executor for this plan does not write STATE.md directly per this wave's isolation contract).

**Other readiness notes:**
- `cmd/cpg/mcp_harness_test.go`'s `startInMemoryMCPSession` helper is designed for direct reuse: Phase 17/18 add new scenarios (tool calls) with one more call to this helper against the same `runMCPServer`, not by rebuilding transport plumbing.
- `runMCPServer(ctx, transport mcp.Transport)` is the stable composition-root signature Phase 17/18 will extend with `server.AddTool(...)` calls — no signature change anticipated.
- No blockers. Full project suite (`go test ./... -race`, 10 packages) and `golangci-lint run ./cmd/cpg/...` (zero new issues; 6 pre-existing errcheck issues in untouched `explain_render.go` are tracked LINT-01 debt) both green.

---
*Phase: 16-mcp-server-foundation-write-safety*
*Completed: 2026-07-20*

## Self-Check: PASSED

- FOUND: `cmd/cpg/mcp.go`
- FOUND: `cmd/cpg/mcp_harness_test.go`
- FOUND: `cmd/cpg/mcp_test.go`
- FOUND: `.planning/phases/16-mcp-server-foundation-write-safety/16-03-SUMMARY.md`
- FOUND: commit `93e7b3e` (Task 1)
- FOUND: commit `4568c19` (Task 2)
- FOUND: commit `2f19514` (Task 3)
- FOUND: `rootCmd.AddCommand(newMCPCmd())` in `cmd/cpg/main.go`
- FOUND: `go.uber.org/zap/exp v0.3.0` in `go.mod`
- FOUND: `github.com/modelcontextprotocol/go-sdk v1.6.1` as a direct require in `go.mod`
