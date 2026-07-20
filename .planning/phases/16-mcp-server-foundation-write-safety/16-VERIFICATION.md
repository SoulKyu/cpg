---
phase: 16-mcp-server-foundation-write-safety
verified: 2026-07-20T18:09:04Z
status: passed
score: 12/12 must-haves verified
overrides_applied: 0
---

# Phase 16: MCP Server Foundation & Write Safety Verification Report

**Phase Goal:** `cpg mcp` runs as a protocol-safe stdio process — pure JSON-RPC on stdout, unified stderr logging — and the on-disk policy writer is torn-read safe, before any session or query tool is built on top of it
**Verified:** 2026-07-20T18:09:04Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

All claims below were checked against the actual codebase (not SUMMARY.md prose): source read of every modified file, `go build`/`go vet` on a clean tree, targeted and full-suite `go test -race` runs (including `-count=5` and `-json -count=3` to specifically re-provoke the test2json-aliasing flake fixed post-review), `golangci-lint run --new-from-rev=<phase base commit>`, and manual reproduction against a freshly-built `cpg` binary of the WR-02 silent-error fix. `post_plan_changes` items (WR-01/WR-02 fixes, seam-audit hardening commits `2cf0754`/`ab665c5`, the `zap/exp` module correction) were independently re-verified in the current tree, not taken on faith.

### Observable Truths

| #   | Truth | Status | Evidence |
| --- | ----- | ------ | -------- |
| 1 | Roadmap SC1 / Two independent in-memory MCP sessions (Session A: initialize + empty `tools/list`; Session B: unknown method) complete with each transport half `Connect`-ed exactly once, zero bytes leaked to the real `os.Stdout` | ✓ VERIFIED | `cmd/cpg/mcp_harness_test.go` `TestMCPStdoutPurity` — read source: two independent `mcp.NewInMemoryTransports()` pairs via `startInMemoryMCPSession`, single `os.Pipe` capture spanning both, `assert.Empty(leaked)`. Ran `go test ./cmd/cpg/... -race -v -run TestMCPStdoutPurity` → PASS; also green under `-race -count=5` and `-json -count=3` |
| 2 | Cobra flag-parse errors on `cpg mcp` never reach `os.Stdout` | ✓ VERIFIED | `TestMCPCobraFlagErrorStaysOffStdout` passes. Independently reproduced against a freshly built binary: `cpg mcp --totally-unknown-flag`, `--log-level=bogus`, `--version` all produced 0 stdout bytes, exit 1. **Mechanism note:** the plan's must-have literally names "SilenceUsage/SilenceErrors" as the mechanism; WR-02 (post-plan code-review fix) removed `SilenceErrors` after proving it was unnecessary for stdout purity (cobra's own printer already targets stderr in this codebase) and was actively harmful (it silenced the *only* diagnostic). The observable truth — zero bytes on stdout — still holds; verified as a legitimate reinterpretation, not a regression (see Anti-Patterns / WR-02 discussion below) |
| 3 | MCP-mode human-output seam (`mcpModeStdout()`) resolves to `os.Stderr`, never `os.Stdout`, never nil | ✓ VERIFIED | `TestMCPModeStdoutNeverDefaultsToRealStdout` — `assert.Same(os.Stderr, got)`, `assert.NotNil`. Hardened post-review (commits `2cf0754`, `ab665c5`) with an init-time `realProcessStdout` capture and an aliasing guard for `go test -json` mode (which internally aliases `os.Stderr=os.Stdout`). Re-ran under `-json -count=3`: 3/3 pass, zero fail actions in the JSON event stream |
| 4 | Roadmap SC2 / go-sdk internal logs and cpg's zap logs share one stderr stream via `zap/exp/zapslog` | ✓ VERIFIED | `TestMCPLoggingBridgesToZapStderr` passes (message logged via `bridgedSlogLogger()` observed in cpg's own zap core). Independently confirmed `buildLogger()` (main.go) never defaults to stdout: read `go.uber.org/zap@v1.27.1/config.go` source directly — `NewProductionConfig()` and `NewDevelopmentConfig()` both hardcode `OutputPaths: []string{"stderr"}`; `NewDevelopment()` calls `NewDevelopmentConfig().Build()`. All three `buildLogger` branches are stderr-only by construction |
| 5 | Roadmap SC3 / `pkg/output/writer.go` writes policy YAML via a same-dir temp file renamed into place, never a direct in-place truncate+write | ✓ VERIFIED | Read `pkg/output/writer.go:81-102`: `os.CreateTemp(filepath.Dir(path), ...)` → `tmp.Write` → `tmp.Close` → `os.Chmod(tmpPath, 0644)` → `os.Rename(tmpPath, path)`. Grepped the file for `os.WriteFile` — zero matches (fully replaced) |
| 6 | A concurrent reader polling during writes never observes a partial or truncated policy file | ✓ VERIFIED | `TestWriter_ConcurrentReaderNeverSeesPartialFile` (100-iteration writer/reader race, varying port per iteration to force a genuine rename every time) — ran `go test ./pkg/output/... -race -v`: PASS, no data-race report |
| 7 | Generated policy files retain mode 0644 | ✓ VERIFIED | `TestWriter_FilePermissions` (pre-existing regression gate) still passes; explicit `os.Chmod(tmpPath, 0644)` sits between `tmp.Close()` and `os.Rename()` in writer.go:95 |
| 8 | No leftover `.tmp-*` files remain in the namespace directory after a successful write | ✓ VERIFIED | `TestWriter_AtomicNoLeftoverTempFiles` passes; asserts zero `.tmp-*` dir entries and that the written file unmarshals as valid CNP YAML |
| 9 | `cpg mcp` is a registered cobra subcommand visible in `cpg --help` | ✓ VERIFIED | `go run ./cmd/cpg --help` lists `mcp  Run cpg as a readonly MCP server over stdio`; `go run ./cmd/cpg mcp --help` returns the short description without hanging |
| 10 | Operator explicitly approved the go-sdk dependency after reviewing the slopcheck `[SUS]` verdict and its rebuttal | ✓ VERIFIED | Process evidence: `16-02-PLAN.md` Task 1 is a `gate="blocking-human"` checkpoint; `16-02-SUMMARY.md` records "Operator response: approved" (2026-07-20) with `git status --short` confirmed clean before the `go get` in Task 2; the dependency's presence in `go.mod`/`go.sum` is downstream evidence the gate was actually passed (Task 2's automated verify would not have run otherwise) |
| 11 | `go.mod` requires `github.com/modelcontextprotocol/go-sdk` pinned at exactly v1.6.1 | ✓ VERIFIED | `go.mod:10` — `github.com/modelcontextprotocol/go-sdk v1.6.1` in the direct-require block (promoted from indirect by plan 16-03's `go mod tidy`, as designed) |
| 12 | `go.sum` records checksums for go-sdk and its transitive dependencies | ✓ VERIFIED | `go.sum:151-152` (go-sdk `h1:`/`go.mod h1:` pair) and `go.sum:249-250` (`go.uber.org/zap/exp v0.3.0`, the corrected zapslog dependency); `go mod verify` → "all modules verified" |

**Score:** 12/12 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `pkg/output/writer.go` | Atomic temp+rename write, `os.CreateTemp`/`os.Chmod` | ✓ VERIFIED | Present, wired into `(*Writer).Write`, no leftover `os.WriteFile` |
| `pkg/output/writer.go` | File-mode preservation, `os.Chmod` before rename | ✓ VERIFIED | `os.Chmod(tmpPath, 0644)` at line 95, before `os.Rename` at line 99 |
| `pkg/output/writer_test.go` | `TestWriter_ConcurrentReaderNeverSeesPartialFile` | ✓ VERIFIED | Present, passes under `-race` |
| `go.mod` | Pinned `github.com/modelcontextprotocol/go-sdk v1.6.1` | ✓ VERIFIED | Direct require, exact pin |
| `go.sum` | Checksums for go-sdk module graph | ✓ VERIFIED | Present |
| `cmd/cpg/mcp.go` | `newMCPCmd`, `runMCPServer`, `noopCloseWriter`, `bridgedSlogLogger`, `mcpModeStdout`; contains `mcp.IOTransport` | ✓ VERIFIED | All 5 symbols present and used; `mcp.StdioTransport` never used (only referenced in a doc comment explaining the avoidance) |
| `cmd/cpg/mcp.go` | Global stdout backstop after transport capture (D-01), contains `os.Stdout = os.Stderr` | ✓ VERIFIED | Line 64, textually and logically after `IOTransport` struct-literal construction (lines 56-59) |
| `cmd/cpg/mcp.go` | zapslog bridge for go-sdk logs (SRV-03), contains `zapslog.NewHandler` | ✓ VERIFIED | Line 99, inside `bridgedSlogLogger()` |
| `cmd/cpg/main.go` | mcp command registration, contains `newMCPCmd` | ✓ VERIFIED | Line 60 — `rootCmd.AddCommand(newMCPCmd())` |
| `cmd/cpg/mcp_harness_test.go` | Reusable in-memory-transport stdout-purity harness, contains `NewInMemoryTransports` | ✓ VERIFIED | `startInMemoryMCPSession` helper wraps `mcp.NewInMemoryTransports()`, designed for Phase 17-19 reuse |
| `cmd/cpg/mcp_test.go` | Seam-audit, cobra flag-error, zapslog-bridge tests, contains `TestMCPCobraFlagErrorStaysOffStdout` | ✓ VERIFIED | Present, all 3 named tests pass |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | --- | --- | ------ | ------- |
| `pkg/output/writer.go Write()` | `os.Rename(tmpPath, path)` | same-directory temp file (`filepath.Dir(path)`) | ✓ WIRED | `os.CreateTemp(filepath.Dir(path), ...)` at line 81; `os.Rename` at line 99 |
| `pkg/output/writer.go Write()` | `os.Chmod(tmpPath, 0644)` | explicit chmod before rename | ✓ WIRED | Line 95, between `tmp.Close()` (91) and `os.Rename` (99) |
| `go.mod` require block | `github.com/modelcontextprotocol/go-sdk v1.6.1` | `go get` at the pinned tag | ✓ WIRED | Direct require present, exact version |
| `cmd/cpg/mcp.go newMCPCmd RunE` | `mcp.IOTransport{Reader: os.Stdin, Writer: noopCloseWriter{os.Stdout}}` | struct-literal capture of real stdout BEFORE the `os.Stdout=os.Stderr` swap | ✓ WIRED | Lines 56-64 confirm capture-then-swap ordering |
| `cmd/cpg/mcp.go runMCPServer` | `zapslog.NewHandler(logger.Core())` | `slog.New` over the existing package-level zap logger, via `bridgedSlogLogger()` | ✓ WIRED | `runMCPServer` (line 80) calls `bridgedSlogLogger()` (line 98-99), which calls `zapslog.NewHandler(logger.Core())` |
| `cmd/cpg/main.go` | `newMCPCmd()` | `rootCmd.AddCommand` | ✓ WIRED | Line 60 |
| `cmd/cpg/mcp_harness_test.go` | `runMCPServer` | in-memory server transport(s) driven by an `mcp.Client` and a raw `Connection` | ✓ WIRED | `startInMemoryMCPSession` launches `runMCPServer(ctx, serverT)` in a goroutine; consumed by both Session A and Session B in `TestMCPStdoutPurity` |

### Data-Flow Trace (Level 4)

Not a UI/dashboard phase (no state→render seam in the traditional React/Vue sense), so the standard Level 4 procedure doesn't map directly. Applied the equivalent check for a backend/infra phase — does data flow through the wiring for real, or is it a stub:

| Artifact | "Data" Variable | Source | Produces Real Data | Status |
| -------- | ---------------- | ------ | ------------------- | ------ |
| `bridgedSlogLogger()` → go-sdk `ServerOptions.Logger` | log record | `logger.Core()` (package-level zap logger built by `buildLogger()`) | Yes — `TestMCPLoggingBridgesToZapStderr` proves a message logged through the bridge is observed in the real zap core, not a nop/discarded sink | ✓ FLOWING |
| `pkg/output/writer.go Write()` → policy YAML on disk | merged `ciliumv2.CiliumNetworkPolicy` | `policy.MergePolicy(existing, event.Policy)` | Yes — `TestWriter_MergeOnWrite`/`TestWriter_WritesDifferentPolicy` (pre-existing, still green) show genuinely different content across writes, not a static/empty payload | ✓ FLOWING |
| `runMCPServer` → `mcp.NewServer(...)` | `Implementation{Name, Version}` | `version` package var (set via `-ldflags` at real build time, `"dev"` in tests) | N/A — cosmetic metadata field, not a correctness-relevant data seam this phase | not applicable |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Full build succeeds | `go build ./...` | Success | ✓ PASS |
| Vet clean on touched packages | `go vet ./cmd/cpg/... ./pkg/output/...` | No issues found | ✓ PASS |
| `mcp` listed in top-level help | `go run ./cmd/cpg --help` | `mcp  Run cpg as a readonly MCP server over stdio` | ✓ PASS |
| `cpg mcp --help` doesn't hang | `go run ./cmd/cpg mcp --help` | Returns short description immediately (short-circuits before RunE) | ✓ PASS |
| `pkg/output` suite green under `-race` | `go test ./pkg/output/... -race -v` | 25 passed | ✓ PASS |
| Named MCP tests green under `-race` | `go test ./cmd/cpg/... -race -v -run 'TestMCP'` | 4/4 PASS: `TestMCPStdoutPurity`, `TestMCPModeStdoutNeverDefaultsToRealStdout`, `TestMCPCobraFlagErrorStaysOffStdout`, `TestMCPLoggingBridgesToZapStderr` | ✓ PASS |
| `cmd/cpg` package stable under repeated `-race` runs | `go test ./cmd/cpg/... -race -count=5` | ok | ✓ PASS |
| Full repo suite green under `-race` | `go test ./... -race` (10 packages) | all `ok` | ✓ PASS |
| Seam-audit test immune to `test2json` stderr-aliasing (regression target of commits `2cf0754`/`ab665c5`) | `go test ./cmd/cpg/... -run TestMCPModeStdoutNeverDefaultsToRealStdout -json -count=3` | 3/3 pass actions, zero fail actions in the event stream | ✓ PASS |
| Full package under `-race -json -count=3` (broader regression check) | `go test ./cmd/cpg/... -race -count=3 -v -json` | all `TestMCP*` pass actions present, zero fail actions | ✓ PASS |
| Zero new lint issues vs. phase base commit | `golangci-lint run --max-same-issues=0 --new-from-rev=a39d61a ./pkg/output/... ./cmd/cpg/...` | `0 issues.` (`a39d61a` confirmed as an ancestor of HEAD and the actual pre-phase-16 commit) | ✓ PASS |
| WR-02 fix reproduced against a freshly built binary (not just read from source) | `cpg mcp --totally-unknown-flag`; `cpg mcp --log-level=bogus`; `cpg mcp --version` | All 3: stdout 0 bytes, stderr non-empty diagnostic (`Error: unknown flag: ...` / `Error: unrecognized level: "bogus"` / `Error: unknown flag: --version`), exit 1 | ✓ PASS |
| Zero tools registered (design intent, not accidental) | `grep -n "AddTool" cmd/cpg/mcp.go` | No matches | ✓ PASS |
| `mcpModeStdout()` has exactly one call site (documented Phase 17 handoff, not dead code) | `grep -rn "mcpModeStdout(" cmd/cpg/ pkg/` | Only the seam-audit test calls it | ✓ PASS (matches documented non-goal) |

### Probe Execution

SKIPPED — no `scripts/*/tests/probe-*.sh` convention exists in this repository (Go CLI project; verification is via `go test`, already covered under Behavioral Spot-Checks above). No probe references found in the phase's PLAN/SUMMARY files either.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ---------- | ----------- | ------ | -------- |
| SRV-02 | 16-02, 16-03 | stdout carries only JSON-RPC frames across a full session lifecycle — enforced by explicit wiring of every stdout-defaulting seam, verified by an automated stdout-purity test on an in-memory transport | ✓ SATISFIED | Truths #1-3 above; `TestMCPStdoutPurity`, `TestMCPCobraFlagErrorStaysOffStdout`, `TestMCPModeStdoutNeverDefaultsToRealStdout` all green under `-race`. **REQUIREMENTS.md traceability table still shows "Pending" and the checkbox is unchecked — stale tracking, see Anti-Patterns below** |
| SRV-03 | 16-02, 16-03 | All server logs go to stderr through the existing zap logger; go-sdk internal logging is bridged into it via `zap/exp/zapslog` | ✓ SATISFIED | Truth #4 above; `TestMCPLoggingBridgesToZapStderr` green; `buildLogger()` independently confirmed stderr-only via zap v1.27.1 source. **REQUIREMENTS.md traceability table still shows "Pending" and the checkbox is unchecked — stale tracking, see Anti-Patterns below** |
| SEC-02 | 16-01 | `pkg/output/writer.go` writes policy YAML atomically (temp+rename, same pattern as the evidence and health writers) — landed as an early standalone change before any query tool reads that directory | ✓ SATISFIED | Truths #5-8 above; REQUIREMENTS.md correctly shows this as `[x]` / "Complete" (updated by plan 16-01's commit `01f432c`) |

**Orphaned requirements check:** REQUIREMENTS.md's Traceability table maps exactly SRV-02, SRV-03, SEC-02 to Phase 16 — all three appear in at least one plan's `requirements:` frontmatter (16-01: SEC-02; 16-02 and 16-03: SRV-02, SRV-03). No orphans.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| `.planning/REQUIREMENTS.md` | 14-15, 78-79 | SRV-02 and SRV-03 rows still read "Pending" (checkbox `- [ ]` and Traceability status `Pending`), even though 16-03-SUMMARY.md declares `requirements-completed: [SRV-02, SRV-03]` and code evidence independently confirms both are implemented and tested. Only SEC-02's row was updated (by plan 16-01's commit `01f432c`); no commit from plan 16-02 or 16-03 touched this file. | WARNING | Documentation-only gap — does not affect runtime behavior, already cross-checked against code directly for the Requirements Coverage table above. Should be fixed (flip both rows to `[x]`/"Complete") before/at milestone audit so REQUIREMENTS.md remains a trustworthy source of truth without requiring a code review to confirm. |
| `.planning/STATE.md` | 1-14, 25-32, 110-114 | Progress counters (`completed_plans: 0`, `percent: 0`), Current Position (`Plan: 1 of 3`, `Status: Executing Phase 16`), and Session Continuity (`Stopped at: Phase 16 context gathered`) all reflect a pre-wave-2 snapshot. `git log` shows the last commit touching STATE.md is `bd29530` ("update tracking after wave 1"), which predates plan 16-03, the code review (`16-REVIEW.md`), the review-fix (`16-REVIEW-FIX.md`), and the seam-audit hardening commits (`2cf0754`, `ab665c5`) entirely. | WARNING | 16-03-PLAN.md's `<output>` block explicitly instructed: *"record a Phase 17 carry-forward obligation in the Accumulated Context... This ensures the deferred D-02 wiring propagates into STATE.md and is not silently dropped."* 16-03-SUMMARY.md recorded the obligation in its own "Next Phase Readiness" section but explicitly deferred the STATE.md write to "the orchestrator" — which has not yet happened. `PROJECT.md`'s Key Decisions table (STATE.md's stated source for logged decisions) also has zero mentions of `mcpModeStdout`/`PipelineConfig.Stdout`/Phase 16/Phase 17. **Concrete risk:** if Phase 17 planning consults STATE.md's Accumulated Context (its designed channel) rather than re-reading 16-03-SUMMARY.md directly, the mandatory `PipelineConfig.Stdout = mcpModeStdout()` wiring obligation could be silently dropped — the exact failure mode the plan's authors were explicitly trying to prevent. Recommend closing this before Phase 17 planning starts. |

No debt markers (`TBD`/`FIXME`/`XXX`/`TODO`/`HACK`/`PLACEHOLDER`) found in any file modified by this phase (`cmd/cpg/mcp.go`, `cmd/cpg/mcp_test.go`, `cmd/cpg/mcp_harness_test.go`, `cmd/cpg/main.go`, `pkg/output/writer.go`, `pkg/output/writer_test.go`).

### Human Verification Required

None. Every must-have truth for this phase is mechanically verifiable (protocol behavior via in-memory transport + `-race` tests, logging via an observed zap core, write atomicity via a race-clean concurrent test, CLI behavior reproduced against a freshly built binary) and was independently re-executed above, not just read from SUMMARY prose. The one genuinely human-gated item (go-sdk supply-chain legitimacy) already completed its blocking-human checkpoint during execution with a documented approval record (16-02-SUMMARY.md) and downstream evidence (`go.mod`/`go.sum`) consistent with the gate having passed — nothing remains open for a human to test now.

The real-stdio subprocess e2e (every stdout line parses as a JSON-RPC frame over an actual OS pipe) is explicitly out of scope for this phase (Phase 19 / SRV-04 / D-06) and was not treated as a gap, per the phase's own locked scoping decision in `16-CONTEXT.md`.

### Gaps Summary

No gaps against this phase's own must-have truths, artifacts, or key links — all 12 are VERIFIED with direct, independently-reproduced evidence (not SUMMARY.md claims taken at face value). `go build`, `go vet`, and the full 10-package `-race` suite are all green on the current tree; `golangci-lint`'s new-issue gate is clean; the two post-review fixes (WR-01, WR-02) and the seam-audit hardening are confirmed present and effective in the final code, including against the specific `-json` test-mode flake they were written to fix.

Two WARNING-level findings were surfaced under Anti-Patterns — both are documentation/tracking gaps (REQUIREMENTS.md traceability rows, STATE.md progress + Phase 17 handoff note), not code defects. They do not block this phase's goal from being considered achieved (the protocol-safety and write-safety guarantees are real and tested), but the STATE.md gap in particular carries forward risk into Phase 17 and should be closed before that phase is planned.

---

_Verified: 2026-07-20T18:09:04Z_
_Verifier: Claude (gsd-verifier)_
