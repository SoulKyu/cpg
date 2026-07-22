# Phase 16: MCP Server Foundation & Write Safety - Pattern Map

**Mapped:** 2026-07-20
**Files analyzed:** 7 (3 new, 3 modified, 1 dependency-manifest pair)
**Analogs found:** 5 / 7 (2 exact, 3 role-match, 2 none)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|-----------------|---------------|
| `cmd/cpg/mcp.go` (new) | controller (cobra subcommand + server bootstrap) | streaming (long-lived stdio session, blocks on ctx) | `cmd/cpg/generate.go` | role-match |
| `cmd/cpg/mcp_test.go` (new) | test | request-response (cobra flag-error path) + unit (seam audit) | `cmd/cpg/generate_test.go` (SilenceUsage precedent), `cmd/cpg/replay_test.go` (SilenceUsage precedent) | role-match |
| `cmd/cpg/mcp_harness_test.go` (new) | test (protocol harness) | event-driven (in-memory transport session) | none in codebase — see No Analog Found | none |
| `cmd/cpg/main.go` (modified: register `newMCPCmd()`) | config (composition root) | startup/registration (one-shot, not runtime data flow) | itself — `rootCmd.AddCommand(...)` block | exact |
| `pkg/output/writer.go` (modified: atomic write) | service (file writer) | file-I/O | `pkg/evidence/writer.go` | exact |
| `pkg/output/writer_test.go` (modified: +1-2 atomicity tests) | test | file-I/O | `pkg/hubble/health_writer_test.go` (`TestHealthWriterAtomicWrite`) | role-match |
| `go.mod` / `go.sum` (modified: add go-sdk v1.6.1) | config (dependency manifest) | — | itself — existing `require` block conventions | none (mechanical `go get`) |

## Pattern Assignments

### `cmd/cpg/mcp.go` (controller, streaming)

**Analog:** `cmd/cpg/generate.go` (primary — long-lived RunE + signal handling), `cmd/cpg/replay.go` (secondary — simpler cobra skeleton), `cmd/cpg/main.go` (buildLogger reuse)

**Imports pattern** — `cmd/cpg/generate.go:3-22` and `cmd/cpg/replay.go:3-18` (both show the two-group import convention: stdlib, then blank line, then third-party, then blank line, then `github.com/SoulKyu/cpg/...`):
```go
// cmd/cpg/replay.go:3-18
import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/flowsource"
	"github.com/SoulKyu/cpg/pkg/hubble"
)
```
Apply the same three-group convention to `mcp.go`, with `github.com/modelcontextprotocol/go-sdk/mcp` and `go.uber.org/zap/exp/zapslog` in the third-party group (see RESEARCH.md Architecture Patterns Pattern A for the exact new-package import list — `context`, `io`, `log/slog`, `os`, `os/signal`, `syscall`, then `github.com/modelcontextprotocol/go-sdk/mcp`, `github.com/spf13/cobra`, `go.uber.org/zap/exp/zapslog`).

**Cobra command struct-literal pattern** (`cmd/cpg/replay.go:20-47`, the smallest/cleanest of the three existing commands — no domain-specific flags to imitate since `mcp` registers none):
```go
func newReplayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "replay <file.jsonl|->",
		PreRunE: validateCommonFlags,
		Short:   "Generate policies from a captured Hubble jsonpb dump",
		Long: `...`,
		Args: cobra.ExactArgs(1),
		RunE: runReplay,
	}
	addCommonFlags(cmd)
	return cmd
}
```
`mcp.go`'s `newMCPCmd()` follows this shape but drops `PreRunE`/`Args`/`addCommonFlags` (no positional args, no domain flags — CONTEXT.md's Claude's-Discretion note: inherits only the persistent `--debug`/`--log-level`/`--json` flags already on `rootCmd`) and adds `SilenceUsage: true, SilenceErrors: true` inline in the struct literal (D-03 — see Shared Patterns below for the precedent this establishes at production-command level, not just in tests).

**Long-running RunE + graceful shutdown pattern** (`cmd/cpg/generate.go:162-163`):
```go
ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
defer cancel()
```
Copy verbatim into `mcp.go`'s `RunE` — same signal set, same `cmd.Context()` base, same `defer cancel()` placement immediately after construction. This is also the only place `context` and `os/signal`/`syscall` are needed in the new file.

**Logger reuse pattern** (`cmd/cpg/main.go:18-19` package-level var, `cmd/cpg/generate.go:181` usage) — `mcp.go`'s `runMCPServer` reaches for the same package-level `logger *zap.Logger` var directly, exactly as `generate.go`/`replay.go`/`explain.go` do; no new logger-construction code needed. `buildLogger()` (`cmd/cpg/main.go:71-102`) is reused unchanged via the existing `PersistentPreRunE` — confirmed all 3 branches (JSON, debug, default console) write to stderr only, satisfying SRV-03 with zero new code.

**Core pattern (go-sdk wiring, D-01 corrected mechanism):** No codebase analog exists for this part (first MCP integration) — use RESEARCH.md Architecture Patterns → Pattern A verbatim (`cmd/cpg/mcp.go` code block, lines 296-368 of `16-RESEARCH.md`). Key structural point reconfirmed against this codebase's conventions: build the transport (`mcp.IOTransport{Reader: os.Stdin, Writer: noopCloseWriter{os.Stdout}}`) **before** `os.Stdout = os.Stderr`, matching this file's own `signal.NotifyContext`-then-later-mutation ordering style already used in `generate.go`.

**No error-wrapping pattern applies here** — `RunE` returns `server.Run(ctx, transport)`'s error directly (mirrors `replay.go:132` `return hubble.RunPipelineWithSource(ctx, cfg, source)` — no extra `fmt.Errorf` wrap at the top RunE level for the terminal call).

---

### `cmd/cpg/mcp_test.go` (test, request-response + unit)

**Analog:** `cmd/cpg/generate_test.go` (SilenceUsage/SilenceErrors + SetArgs + Execute precedent), `cmd/cpg/replay_test.go` (same precedent, 10 occurrences), `cmd/cpg/testhelpers_test.go` (logger-swap idiom to extend for `os.Stdout`)

**Cobra flag-error test pattern** (`cmd/cpg/generate_test.go:283-291`, exact structural match for D-04 scenario 4 / D-06):
```go
func TestReplayCmd_RejectsNoL7PreflightFlag(t *testing.T) {
	cmd := newReplayCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--no-l7-preflight", "../../testdata/flows/small.jsonl"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-l7-preflight")
}
```
`TestMCPCobraFlagErrorStaysOffStdout` follows this exact shape: `cmd := newMCPCmd()`, `cmd.SetArgs([]string{"--totally-unknown-flag"})`, `_ = cmd.Execute()`, then assert on the captured `os.Stdout` pipe instead of `err.Error()` content (see RESEARCH.md Code Examples for the full `os.Pipe`-wrapped version — the pipe-capture part has no direct precedent in `cmd/cpg`, extend `testhelpers_test.go`'s swap-with-`t.Cleanup`-restore idiom below to `os.Stdout` the same way it's already used for `logger`).

**Test isolation / swap-with-cleanup idiom** (`cmd/cpg/testhelpers_test.go:14-19`, the ONLY existing precedent in this package for "swap a package-level/global resource, restore via `t.Cleanup`"):
```go
func initLoggerForTesting(t *testing.T) {
	t.Helper()
	prev := logger
	logger = zap.NewNop()
	t.Cleanup(func() { logger = prev })
}
```
Extend the identical idiom for `os.Stdout` capture in both `mcp_test.go` and `mcp_harness_test.go`:
```go
r, w, err := os.Pipe()
require.NoError(t, err)
realStdout := os.Stdout
os.Stdout = w
t.Cleanup(func() { os.Stdout = realStdout })
```
This is not copy-paste of `initLoggerForTesting` itself (different resource type) but the exact same **shape**: capture previous value, swap, `t.Cleanup` restores — reuse this shape, do not invent a different teardown mechanism (e.g. defer-only, which would run before other deferred assertions read the pipe).

**Seam-audit unit test** — no direct precedent (this is a small novel helper + test), but structurally a plain table-free unit test like any other in this package (e.g. `cmd/cpg/generate_test.go:275-279` `TestValidateIgnoreProtocols_EmptyIsNoOp`, minimal arrange/act/assert, no cobra involved). Use RESEARCH.md Code Examples' `mcpModeStdout()`/`TestMCPModeStdoutNeverDefaultsToRealStdout` verbatim.

**Observed-logger pattern for SRV-03 logging test** (`cmd/cpg/testhelpers_test.go:24-31`, already used by `cmd/cpg/replay_test.go:39` `logs := initObservedLoggerForTesting(t)`):
```go
func initObservedLoggerForTesting(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	prev := logger
	logger = zap.New(core)
	t.Cleanup(func() { logger = prev })
	return logs
}
```
Reuse directly (no modification needed) for `TestMCPLogging`'s assertion that `runMCPServer` wires `ServerOptions.Logger` from the package-level `logger`.

---

### `cmd/cpg/mcp_harness_test.go` (test, event-driven)

**No close analog** — see No Analog Found. Build directly from RESEARCH.md Code Examples → "Stdout-purity harness skeleton (D-04, scenarios 1-3)" (full skeleton already given, `16-RESEARCH.md` lines 472-517). The `os.Pipe` capture half reuses the same swap-with-`t.Cleanup` shape as `testhelpers_test.go:14-19` (see above); the `initLoggerForTesting(t)` call inside the skeleton is a direct call to the existing helper, unmodified.

---

### `cmd/cpg/main.go` (config, registration)

**Analog:** itself — this is a same-file, same-pattern, one-line addition.

**Registration pattern** (`cmd/cpg/main.go:57-59`, exact statement to extend):
```go
rootCmd.AddCommand(newGenerateCmd())
rootCmd.AddCommand(newReplayCmd())
rootCmd.AddCommand(newExplainCmd())
```
Add a fourth line, same style, same position (before `rootCmd.Execute()` at line 61): `rootCmd.AddCommand(newMCPCmd())`. No other change to `main.go` — `buildLogger` (lines 71-102), `PersistentPreRunE`/`PersistentPostRun` (lines 37-49), and the `hubble.ExitCodeError` exit-code handling (lines 61-67) all apply to `cpg mcp` for free through cobra's normal dispatch; `mcp`'s own `SilenceUsage`/`SilenceErrors` (D-03) do not require any change here since they're set on the child command, not root.

---

### `pkg/output/writer.go` (service, file-I/O)

**Analog:** `pkg/evidence/writer.go` (primary — same repo, same language, same problem, unprefixed error-message style matching this file's own existing tone), `pkg/hubble/health_writer.go` (secondary — same pattern, different error-prefix convention, do NOT copy the prefix style)

**Imports pattern** (`pkg/evidence/writer.go:4-11`, minimal stdlib-only additions needed — `pkg/output/writer.go` already imports `os` and `path/filepath` at lines 5-6, so no new import lines are needed for the atomic-write block itself):
```go
import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)
```
`pkg/output/writer.go`'s current imports (lines 3-16) already cover everything the atomic-write block needs (`fmt`, `os`, `path/filepath` all present) — this is a same-file edit, not an import-adding one.

**Core atomic-write pattern — mirror verbatim** (`pkg/evidence/writer.go:70-88`, this is the exact block CONTEXT.md's Claude's-Discretion note says to mirror "same-dir temp file, no fsync — match prior art exactly"):
```go
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
return nil
```
**Error-message convention — use evidence/writer.go's unprefixed style, NOT health_writer.go's:** `pkg/hubble/health_writer.go:144-162` repeats the identical block but prefixes every message with `"health writer: "` (e.g. `"health writer: creating temp file: %w"`). `pkg/output/writer.go`'s own existing error strings (lines 41, 49, 57, 73, 82 of the current file — e.g. `"creating namespace directory %s: %w"`, `"writing policy file %s: %w"`) are unprefixed, matching `pkg/evidence/writer.go`'s convention exactly. Follow the target file's own existing tone — do not introduce a `"policy writer: "` (or similar) prefix that neither the file's current code nor its primary analog uses.

**Exact insertion point** — `pkg/output/writer.go:81`:
```go
if err := os.WriteFile(path, data, 0644); err != nil {
	return fmt.Errorf("writing policy file %s: %w", path, err)
}
```
Replace this single `if` block with the atomic-write block above. Everything above line 81 (namespace dir creation at lines 39-42, existing-policy read/merge at lines 46-76, `annotateRules` at line 79) is unchanged.

**CAVEAT — file permission gap neither existing analog handles (SEC-02 Open Question #2, RESEARCH.md):** `os.CreateTemp` creates files at mode `0600`, not `0644`. Neither `pkg/evidence/writer.go` nor `pkg/hubble/health_writer.go` chmods the temp file before `os.Rename` — both write JSON that has no permission-sensitive reader today. `pkg/output/writer.go`'s **current** code explicitly sets `0644` (the line being replaced), and `pkg/output/writer_test.go:113-127` (`TestWriter_FilePermissions`, unchanged, still runs) explicitly asserts `os.FileMode(0644)`. Mirroring the analog verbatim **without adjustment** will silently regress generated policy YAML from `0644` to `0600` and fail that existing test. Add an explicit `os.Chmod(tmpPath, 0644)` between `tmp.Close()` and `os.Rename(tmpPath, path)` — this is a deliberate, documented deviation from "mirror exactly," required because `pkg/output/writer.go` is the one writer of the three where output permissions are an observed contract (GitOps tooling reads these files), not an oversight to silently copy.

**errcheck lint-debt parity (expected, not a defect):** `tmp.Close()` and `os.Remove(tmpPath)` calls in the mirrored block are bare (no `_ = `), exactly as both existing analogs already are. `.golangci.yml`'s `errcheck` will flag these same lines here too — this is intentional parity with existing debt (STATE.md's `LINT-01`), not a new problem to fix.

---

### `pkg/output/writer_test.go` (test, file-I/O)

**Analog:** `pkg/hubble/health_writer_test.go` (`TestHealthWriterAtomicWrite`) for the "file exists and is valid" shape; **no analog** for concurrent-access/no-leftover-temp-file assertions (RESEARCH.md explicitly notes this gap).

**Existing post-hoc validity test pattern** (`pkg/hubble/health_writer_test.go:142-154`):
```go
func TestHealthWriterAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	hw := newHealthWriter(dir, "abc123", zaptest.NewLogger(t), time.Now())
	hw.accumulate(makeDropEvent(flowpb.DropReason_CT_MAP_INSERTION_FAILED, dropclass.DropClassInfra, "node-1", "adserver"))
	require.NoError(t, hw.finalize(makeStats(1, 1)))

	expectedPath := filepath.Join(dir, "abc123", "cluster-health.json")
	data, err := os.ReadFile(expectedPath)
	require.NoError(t, err, "cluster-health.json must exist at evidence dir + hash + filename")

	var report clusterHealthReport
	require.NoError(t, json.Unmarshal(data, &report), "file must be valid JSON")
}
```
This pattern only asserts final state (file exists, parses) — it does NOT prove atomicity under concurrency, only that the happy path produces a valid file. Model a `TestWriter_NoLeftoverTempFile` test after this shape (`t.TempDir()`, write, `os.ReadDir` the namespace dir, assert no `*.tmp-*` entries remain) using the same `t.TempDir()` + `require.NoError` idiom already used throughout `pkg/output/writer_test.go` (e.g. `TestWriter_NewFileCreation`, lines 33-53, same file).

**Existing permission-pinning test, unchanged, now the correctness gate for the SEC-02 caveat above** (`pkg/output/writer_test.go:113-127`):
```go
func TestWriter_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()
	w := NewWriter(dir, logger)

	event := buildTestEvent("default", "server")
	err := w.Write(event)
	require.NoError(t, err)

	path := filepath.Join(dir, "default", "server.yaml")
	info, err := os.Stat(path)
	require.NoError(t, err)
	// File should be 0644
	assert.Equal(t, os.FileMode(0644), info.Mode().Perm())
}
```
Do not modify this test — it is the regression detector for the `os.Chmod` caveat noted above.

**No template exists for the concurrent-reader-while-writing race test** (RESEARCH.md Validation Architecture "Wave 0 Gaps"). Author fresh: a writer goroutine calling `w.Write` repeatedly while a reader goroutine polls `os.ReadFile`/`os.Stat`, run under `-race`, asserting the reader never observes a partial/truncated file. `pkg/output/writer_test.go`'s existing `testify`/`t.TempDir()` conventions (imports at lines 1-15 of the current file) still apply; only the concurrency shape is new.

---

### `go.mod` / `go.sum` (config, dependency manifest)

**No pattern to mirror** — mechanical `go get github.com/modelcontextprotocol/go-sdk/mcp@v1.6.1 && go mod tidy`. Current direct-require block convention (`go.mod:7-22`) is alphabetically-ish grouped by domain; the new line lands in the existing `require (...)` block per `gofmt`/`go mod tidy`'s own ordering — no manual placement decision needed. Existing pin style for reference:
```go
require (
	github.com/cilium/cilium v1.19.4
	github.com/google/uuid v1.6.0
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1
	go.uber.org/zap v1.27.1
	...
)
```
`go.uber.org/zap v1.27.1` (line 13) is already pinned — `zap/exp/zapslog` needs zero `go.mod` change (import-only, per RESEARCH.md Standard Stack). Expected side-effect diff line: `golang.org/x/oauth2 v0.34.0 → v0.35.0` (currently `// indirect` at `go.mod:107`) — inert per CONTEXT.md's Upstream-locked note, not scope creep.

**Gate:** per RESEARCH.md's Package Legitimacy Audit, insert a `checkpoint:human-verify` before the `go get` install task regardless of the (rebutted) `slopcheck [SUS]` verdict — process requirement, not a re-litigation of the package choice.

## Shared Patterns

### Cobra `SilenceUsage`/`SilenceErrors` on a command struct literal
**Source:** `cmd/cpg/generate_test.go:285-286`, `cmd/cpg/replay_test.go:47,84,187,...` (×10) — precedent exists only at **test-local** scope today (`cmd.SilenceUsage = true` set on a cobra.Command returned by `newXCmd()`, inside the test function itself), never yet in production command construction.
**Apply to:** `cmd/cpg/mcp.go`'s `newMCPCmd()` — first production use of these fields, set directly in the `&cobra.Command{...}` struct literal (D-03):
```go
&cobra.Command{
	Use:           "mcp",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          ...,
}
```

### Graceful shutdown via `signal.NotifyContext`
**Source:** `cmd/cpg/generate.go:162-163`, `cmd/cpg/replay.go:72-73` (identical statement in both)
**Apply to:** `cmd/cpg/mcp.go`'s `RunE`, verbatim:
```go
ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
defer cancel()
```

### Package-level `logger *zap.Logger` reuse
**Source:** `cmd/cpg/main.go:18-19` (declaration), `cmd/cpg/generate.go:181` / `cmd/cpg/replay.go:91` (usage)
**Apply to:** `cmd/cpg/mcp.go`'s `runMCPServer` — reference the same package-level `logger` var directly; no constructor parameter, no new logger type.

### Test swap-with-`t.Cleanup`-restore idiom
**Source:** `cmd/cpg/testhelpers_test.go:14-19` (`initLoggerForTesting`) and `:24-31` (`initObservedLoggerForTesting`)
**Apply to:** `cmd/cpg/mcp_test.go` and `cmd/cpg/mcp_harness_test.go` — extend the identical capture/swap/`t.Cleanup`-restore shape from the `logger` package var to the `os.Stdout` package var (via `os.Pipe`). Same shape, different resource — do not invent a `defer`-only variant.

### Atomic temp+rename file write
**Source:** `pkg/evidence/writer.go:70-88` (primary — unprefixed error style), `pkg/hubble/health_writer.go:144-162` (secondary — `"health writer: "`-prefixed error style, pattern only, not the prefix convention)
**Apply to:** `pkg/output/writer.go`, replacing the `os.WriteFile(path, data, 0644)` call at line 81 — **plus** an explicit `os.Chmod(tmpPath, 0644)` neither analog needs but this file's existing `0644` contract (pinned by `TestWriter_FilePermissions`) requires. See the file-specific caveat above for why this is a deliberate deviation, not free-form embellishment.

### stdout-defaulting seam shape (context for D-02/D-05, not itself modified this phase)
**Source:** `pkg/hubble/pipeline.go:91-93,356-358` (`PipelineConfig.Stdout`), `pkg/hubble/writer.go:35,129-133` (`policyWriter.diffOut`) — both follow the identical `field io.Writer` + `if field == nil { field = os.Stdout }` shape, already established in this codebase before Phase 16.
**Apply to:** informs `cmd/cpg/mcp.go`'s `mcpModeStdout()` helper (D-05) — the helper's contract ("never nil, never `os.Stdout`, always `os.Stderr`" in MCP mode) is the same *shape* of seam these two existing fields use, just pre-resolved to a fixed value rather than defaulted lazily. Per RESEARCH.md Open Questions #1, neither `pipeline.go` nor `writer.go` is modified this phase — no live code path calls `RunPipeline`/`RunPipelineWithSource` from `cpg mcp` yet (zero tools registered).

## No Analog Found

Files/patterns with no close match in the codebase (planner should use RESEARCH.md patterns instead):

| File / Pattern | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `cmd/cpg/mcp_harness_test.go` — in-memory-transport-driven protocol session harness | test (protocol harness) | event-driven | No existing test in this codebase drives a full client/server session over any transport (in-memory or otherwise) — every existing test exercises cobra `Execute()` directly or calls package functions. Use RESEARCH.md Code Examples "Stdout-purity harness skeleton" verbatim; the `os.Pipe` half reuses `testhelpers_test.go`'s swap idiom (see Shared Patterns), but the `mcp.NewInMemoryTransports()` + `mcp.NewClient` driving half is genuinely new. |
| go-sdk server/transport/logging-bridge wiring itself (`mcp.NewServer`, `mcp.IOTransport`, `zapslog.NewHandler`) | service (protocol runtime) | streaming | First MCP integration in this codebase — no prior art to mirror by definition. Use RESEARCH.md Architecture Patterns → Pattern A (verified against go-sdk v1.6.1 source this session, including the `StdioTransport` lazy-capture correction). |
| `go.mod` / `go.sum` dependency addition | config | — | Mechanical `go get`/`go mod tidy`; no code pattern applies. Gate behind `checkpoint:human-verify` per Package Legitimacy Audit (slopcheck `[SUS]`, rebutted). |
| Concurrent-reader-while-writing race test for `pkg/output/writer_test.go` | test | file-I/O | `pkg/hubble/health_writer_test.go`'s `TestHealthWriterAtomicWrite` only asserts post-hoc file validity, never drives a concurrent reader against an in-progress write. Author fresh, informed by the file-specific note above. |

## Metadata

**Analog search scope:** `cmd/cpg/` (all `.go` files), `pkg/output/` (all `.go` files), `pkg/evidence/writer.go`, `pkg/hubble/{health_writer,health_writer_test,pipeline,writer}.go`, `go.mod`
**Files scanned/read this session:** `cmd/cpg/main.go`, `cmd/cpg/generate.go`, `cmd/cpg/replay.go`, `cmd/cpg/explain.go`, `cmd/cpg/testhelpers_test.go`, `cmd/cpg/generate_test.go` (targeted), `cmd/cpg/replay_test.go` (targeted), `pkg/evidence/writer.go`, `pkg/hubble/health_writer.go`, `pkg/hubble/health_writer_test.go` (targeted), `pkg/hubble/pipeline.go` (targeted), `pkg/hubble/writer.go`, `pkg/output/writer.go`, `pkg/output/writer_test.go`, `go.mod` — 15 files, all ≤ 617 lines, no re-reads
**Pattern extraction date:** 2026-07-20
