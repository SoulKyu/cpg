---
last_mapped_commit: eff36ca37f3c162ebd12c5ddf50350d36a9906b3
last_mapped_at: 2026-10-03
---
# Code Conventions & Lint Setup

<!-- refreshed: 2026-10-03 -->

**Analysis Date:** 2026-10-03

## Go style

- Module `github.com/SoulKyu/cpg`, Go 1.25.1 (toolchain go1.25.12) per `go.mod`. Single binary entrypoint: `cmd/cpg/main.go` (subcommands: `generate`, `replay`, `explain`, `mcp`, `bootstrap`, `audit-window`, all registered in `main()`).
- Standard Go project layout: CLI wiring in `cmd/cpg/`, reusable logic in `pkg/` (`auditwindow`, `diff`, `dropclass`, `evidence`, `explain`, `flowsource`, `hubble`, `k8s`, `labels`, `output`, `policy`, `session`). Each package has a single-responsibility doc comment, e.g. `pkg/flowsource/source.go` ("decouples the streaming source of Hubble flows from the streaming pipeline") and `pkg/explain/doc.go`.
- Lowercase package-internal interfaces are the dominant seam: `flowsource.FlowSource` (`pkg/flowsource/source.go`), `policy.FlowTracker` (`pkg/policy/builder.go`), `hubble.flushingTracker` (`pkg/hubble/aggregator.go`), `hubble.flowStream` (`pkg/hubble/client.go`), `main.auditWindowManager` (`cmd/cpg/audit_window.go`). Producers accept interfaces "so tests can substitute a stub"; some interfaces are intentionally unexported.
- Types are named after the domain concept, not the pattern: `hubble.ExitCodeError`, `hubble.PipelineConfig` (not "Options"), `commonFlags` (cmd-local struct shared by `generate`/`replay` in `cmd/cpg/commonflags.go`).
- Comments explain *why* and cite plan/invariant IDs, e.g. "// FILTER-03: warn when reason is already suppressed by default" (`cmd/cpg/commonflags.go`), "// API-DESIGN: the --timeout knob must have an effect" (`pkg/hubble/client.go`), "// I-3: operates on []rune...". Invariant IDs (DNS-02, QRY-03, AC-4, D-05) also appear in test names/comments — preserve this cross-reference style.

## Logging (zap)

- One package-level `logger *zap.Logger` in `cmd/cpg/main.go`, built in the cobra root `PersistentPreRunE` by `buildLogger()`:
  - `--json` → `zap.NewProductionConfig()`; `--debug` shortcut or `--log-level <level>` parsed via `zapcore.ParseLevel`.
  - Otherwise `zap.NewDevelopment()` / `zap.NewDevelopmentConfig()` console encoder.
- `logger.Sync()` in `PersistentPostRun` with `//nolint:errcheck` (`cmd/cpg/main.go:47`).
- Structured fields everywhere, never string interpolation: `zap.String("reason", v)`, `zap.Uint64("total_lost", totalLost)` (`pkg/hubble/aggregator.go:584-605`).
- `logger` is threaded through constructors (`NewAggregator(interval, logger, tracker)`, `NewClient(server, tls, timeout, logger)`) and `PipelineConfig.Logger`. Nil-tolerance is explicit: `validateIgnoreDropReasons` "handles nil logger gracefully" (`cmd/cpg/commonflags.go`).
- MCP bridge: `go.uber.org/zap/exp/zapslog` adapts zap into the MCP go-sdk's slog interface so there is one log stream (`cmd/cpg/mcp.go:112-116`).
- Tests: `zaptest.NewLogger(t)` for standard, `zaptest/observer` core (`observer.New(zapcore.DebugLevel)`) when asserting on emitted entries — see `cmd/cpg/testhelpers_test.go` (`initObservedLoggerForTesting`).

## Error handling

- Wrap with context using `%w` (`fmt.Errorf("creating gRPC client: %w", err)` in `pkg/hubble/client.go`; 91 `%w` wrap sites across `pkg/` + `cmd/`).
- Callers inspect with `errors.As` / `errors.Is`, never string matching: `errors.As(err, &ec)` for `*hubble.ExitCodeError` → `os.Exit(ec.Code)` (`cmd/cpg/main.go:66`); `errors.Is(err, fs.ErrNotExist)` in `cmd/cpg/explain.go`, `cmd/cpg/mcp_query*.go`; `errors.As` for `exec.CodeExitError` (`pkg/k8s/exec.go:87`).
- Sentinel-style typed error for non-failure exits: `ExitCodeError{Code, Msg}` in `pkg/hubble/pipeline.go` — "the pipeline completed correctly but the caller should exit with Code".
- Validation happens in cobra `PreRunE` before `RunE` so flag errors abort before kubeconfig load / port-forward (`validateCommonFlags` in `cmd/cpg/commonflags.go`). Unknown-value errors include Levenshtein "did you mean" suggestions (`suggestClosest`, deterministic tie-break) plus a docs URL.
- errcheck is enforced with `check-type-assertions: true`; deliberate ignores use `_ =` (`_ = conn.Close()` in `pkg/hubble/client.go`) or `//nolint:errcheck`.

## Flag handling (`cmd/cpg/commonflags.go`)

- Pattern: define cobra flags in `addCommonFlags(cmd)`, parse into a plain struct in `parseCommonFlags(cmd) commonFlags`, validate in `validateCommonFlags` wired as shared `PreRunE` for `generate` and `replay`.
- Normalization helpers return `(cleaned, error)` and are nil/empty no-ops: `validateIgnoreProtocols` (lowercase), `validateIgnoreDropReasons` (uppercase, canonical names from `dropclass.ValidReasonNames()`).
- Command-specific flags stay in the subcommand file (e.g. `cmd/cpg/replay.go` adds the positional jsonl capture path).

## Package-level CLAUDE.md files

Present in `cmd/cpg/`, `pkg/hubble/`, `pkg/output/`, `pkg/labels/`, `pkg/policy/`, `pkg/policy/testdata/`. **They are auto-generated `claude-mem` activity logs** (a `<claude-mem-context>` block with dated "Recent Activity" table rows) — not curated coding instructions. Only `pkg/policy/testdata/companion_dns_assert.go` carries real shared-assertion documentation. Do not treat the CLAUDE.md files as authoritative convention docs; treat the code comments as source of truth.

## Lint setup

- `.golangci.yml`: golangci-lint **v2 config** (`version: "2"`), `default: standard` plus `errorlint` enabled; `run.timeout: 5m`; `errcheck.check-type-assertions: true`.
- CI (`.github/workflows/ci.yml`) runs golangci-lint v2.12.2 via `golangci-lint-action` with `only-new-issues: true` — pre-existing debt is explicitly deferred ("29 issues: errcheck + staticcheck SA1019 ... Gate new code only"), so new code must be lint-clean.
- Suppression style: targeted `//nolint:errcheck` / `//nolint:unused // consumed by plan 17-03's Manager` with a reason (`pkg/session/session.go:99-109`).
- Makefile targets: `make all` (= `lint test build`), `make build` (ldflags `-X main.version=$(git describe --tags --always --dirty)` → `bin/cpg`), `make test` (= `go test ./... -count=1 -race`), `make lint` (= `golangci-lint run`), `make clean`.
