# Phase 16: MCP Server Foundation & Write Safety - Context

**Gathered:** 2026-07-20
**Status:** Ready for planning

<domain>
## Phase Boundary

`cpg mcp` runs as a protocol-safe stdio process — pure JSON-RPC on stdout, unified stderr logging (existing zap logger + go-sdk logs bridged via `zap/exp/zapslog`) — with **zero tools registered** (session tools = Phase 17, query tools = Phase 18), plus `pkg/output/writer.go` brought to atomic temp+rename writes. Requirements: SRV-02, SRV-03, SEC-02.

</domain>

<decisions>
## Implementation Decisions

### Stdout hardening (SRV-02)
- **D-01:** Explicit seam wiring **plus** a global backstop: `cpg mcp` startup creates the SDK stdio transport first (it captures the real `os.Stdout`), then sets `os.Stdout = os.Stderr`. Any future stray print (`fmt.Print`, third-party dep) lands visibly in stderr logs instead of corrupting the JSON-RPC wire.
- **D-02:** Human-output seams (`PipelineConfig.Stdout` session summary block, dry-run `diffOut`) are explicitly wired to **stderr** in MCP mode — not `io.Discard` (keeps traces in harness logs), not a per-session buffer (Phase 17 may upgrade the summary seam to a buffer for `stop_session`'s structured summary; that shape belongs to Phase 17).
- **D-03:** `SilenceUsage`/`SilenceErrors` set **on the mcp command only** — cobra honors "executed command OR root", so child-level flags cover all errors during `cpg mcp` (flag parse, `PersistentPreRunE`, `RunE`). Existing CLI UX for generate/replay/explain unchanged. Required regardless of D-01: cobra flag-parse errors occur before `RunE`, i.e. before the swap.

### Stdout-purity test (SRV-02 verification)
- **D-04:** Reusable in-memory-transport harness (go-sdk `NewInMemoryTransports`) with `os.Stdout` captured via `os.Pipe` while the session is driven. Phase 16 scenario: initialize handshake, empty `tools/list`, unknown method, cobra flag-error path. Phases 17–19 extend the same harness as tools land.
- **D-05:** Plus a **seam-audit unit test**: the MCP-mode config constructor (the function building `PipelineConfig`/`diffOut`/`Silence*` wiring) never leaves an `os.Stdout` default — covers "every stdout-defaulting seam" honestly without a live session.
- **D-06:** Phase 16 assertion semantics: **zero bytes leaked** to the captured `os.Stdout` (`len == 0`) — on an in-memory transport, frames never touch stdout, so any byte is a leak. The "every stdout line parses as a JSON-RPC frame" assertion belongs to Phase 19's real-stdio subprocess e2e (SRV-04); do not duplicate a subprocess test in Phase 16.

### Upstream-locked (research/requirements — do not re-litigate)
- SDK: `github.com/modelcontextprotocol/go-sdk/mcp` **v1.6.1** (official Tier-1; `mark3labs/mcp-go` rejected — pre-1.0).
- go-sdk internal logs bridged via `zap/exp/zapslog` — already bundled in zap v1.27.1, import-only, no new go.mod line.
- MCP code lives in `cmd/cpg/mcp.go` — never a `pkg/mcp` package (name collision with the SDK's own `mcp` package).
- Structural readonly rule is *decided* here (composition root only ever registers read-only handlers), *verified* in Phase 19 (SEC-01).
- `go mod tidy` will bump transitive `golang.org/x/oauth2` to ≥ v0.35.0 — inert (OAuth is HTTP-transport-only; cpg is stdio-only). Expected diff line, not scope creep.

### Claude's Discretion
- `cpg mcp` command UX: inherits existing persistent flags (`--debug`/`--log-level`/`--json`); `buildLogger()` reused unchanged (already stderr-only); command visible in `cpg --help` with a standard description.
- Atomic writer (SEC-02): mechanically mirror the existing CreateTemp→write→Close→Rename pattern (same-dir temp file, no fsync — match prior art exactly); preserve 0644 perms, the annotate step, and the merge/compare flow.
- Test/harness file layout within `cmd/cpg`.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Milestone research (v1.5 MCP)
- `.planning/research/SUMMARY.md` — synthesis: stack choice, four cross-file tensions, phase mapping (Phase 16 ≈ research "Phase 1" + the Tension-4 writer fix)
- `.planning/research/STACK.md` — go-sdk v1.6.1 rationale + exact cobra/SDK wiring snippet
- `.planning/research/ARCHITECTURE.md` — Patterns 1–3, dependency-ordered build order, package-naming rule (no `pkg/mcp`)
- `.planning/research/PITFALLS.md` — Pitfall 1 (stdout is the wire), Pitfall 5 (torn-read writer), Pitfall 7 (readonly is structural, not a hint), Pitfall 10 (protocol-level tests)

### Planning
- `.planning/REQUIREMENTS.md` — SRV-02, SRV-03, SEC-02 exact wording (§MCP Server Core, §Security & Hardening)
- `.planning/ROADMAP.md` — Phase 16 goal + 3 success criteria

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/evidence/writer.go:70-84` and `pkg/hubble/health_writer.go:144-159` — the exact atomic temp+rename pattern SEC-02 mirrors into `pkg/output/writer.go`
- `cmd/cpg/main.go:71` `buildLogger()` — already stderr-only (zap prod/dev configs both default to stderr); reuse unchanged for SRV-03
- zap v1.27.1 already bundles `zap/exp/zapslog` — bridge is import-only

### Established Patterns
- Persistent flags `--debug`/`--log-level`/`--json` on rootCmd (`cmd/cpg/main.go:53-55`) — `cpg mcp` inherits them for free
- `hubble.ExitCodeError` handling in `main()` — mcp exit path composes with it
- All tests run with `-race` (484 tests across 10 packages) — new mcp tests follow suit

### Integration Points (the stdout-defaulting seams to wire)
- `pkg/hubble/pipeline.go:93` — `PipelineConfig.Stdout` (nil → `os.Stdout` at :356-358)
- `pkg/hubble/writer.go:35` — `diffOut` (nil → `os.Stdout` at :129-131)
- cobra usage-on-error: no `SilenceUsage`/`SilenceErrors` anywhere today (grep-verified)
- `cmd/cpg/explain.go:97` `cmd.OutOrStdout()` — CLI-only path, unaffected in mcp mode (explain logic reaches MCP only via the Phase 18 `pkg/explain` promotion)
- `pkg/output/writer.go:81` — direct `os.WriteFile`, the SEC-02 target
- `cmd/cpg/main.go:57-59` — `rootCmd.AddCommand(...)`, where `newMCPCmd()` registers

</code_context>

<specifics>
## Specific Ideas

- Swap ordering is deliberate: transport construction captures the real `os.Stdout` **before** the global `os.Stdout = os.Stderr` swap (Go var swap doesn't affect the already-captured `*os.File`).
- Stray prints must stay *visible* (stderr), never silently discarded — that's why D-02 rejects `io.Discard`.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 16-MCP Server Foundation & Write Safety*
*Context gathered: 2026-07-20*
