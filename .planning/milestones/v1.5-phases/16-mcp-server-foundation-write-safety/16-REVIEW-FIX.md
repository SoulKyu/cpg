---
phase: 16-mcp-server-foundation-write-safety
fixed_at: 2026-07-20T17:44:56Z
review_path: .planning/phases/16-mcp-server-foundation-write-safety/16-REVIEW.md
iteration: 1
findings_in_scope: 2
fixed: 2
skipped: 0
status: all_fixed
---

# Phase 16: Code Review Fix Report

**Fixed at:** 2026-07-20T17:44:56Z
**Source review:** .planning/phases/16-mcp-server-foundation-write-safety/16-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 2 (critical_warning scope; IN-01 excluded as Info-tier)
- Fixed: 2
- Skipped: 0

## Fixed Issues

### WR-01: New atomic-write cleanup paths fail the project's own CI lint gate

**Files modified:** `pkg/output/writer.go`
**Commit:** `2389f2f`
**Applied fix:** Prefixed all 5 unchecked cleanup-path error returns (`tmp.Close()` and 4x `os.Remove(tmpPath)` at lines 87-100) with `_ =`, matching the codebase's existing `_ = conn.Close()` idiom (`pkg/hubble/client.go:72,86`). Source matched the review exactly at the cited lines; applied the review's fix snippet verbatim.

Verified:
- `golangci-lint run --max-same-issues=0 --new-from-rev=a39d61a ./pkg/output/...` → `0 issues.` (ran the binary directly at `/home/gule/go/bin/golangci-lint`, bypassing the RTK hook's unsupported `--out-format` injection)
- `go build ./...` → success
- `go test ./pkg/output/... -race` → 25 passed
- `gofmt -l` → clean

### WR-02: `cpg mcp` fails completely silently on any startup/runtime error

**Files modified:** `cmd/cpg/mcp.go`, `cmd/cpg/mcp_test.go`
**Commit:** `4fb7ba2`
**Applied fix:** Dropped `SilenceErrors: true` from the `mcp` subcommand definition (kept `SilenceUsage: true`), per the task's minimal-fix guidance — cobra's default error printer already targets stderr in this codebase (no `SetOut`/`SetErr` is ever called in production; confirmed directly against `cobra@v1.10.2` source: `Print`/`Println` use `OutOrStderr()`, `PrintErrln` uses `ErrOrStderr()`, both falling back to `os.Stderr`). Rewrote `newMCPCmd`'s doc comment to explain the new rationale instead of the old (incorrect) "protects stdout" framing. Strengthened `TestMCPCobraFlagErrorStaysOffStdout` to capture both stdout and stderr via `os.Pipe()`, asserting stdout stays empty AND stderr is now non-empty on a flag-parse error — closing the exact test gap the review identified (the old test only asserted stdout emptiness, so it would have passed identically whether or not any diagnostic was ever printed). Did not touch `generate`/`replay`/`explain` commands.

Verified:
- Rebuilt `cpg` binary and reproduced all 3 scenarios from the review against the fixed binary:
  - `cpg mcp --totally-unknown-flag` → stdout: 0 bytes, stderr: `Error: unknown flag: --totally-unknown-flag`, exit 1
  - `cpg mcp --log-level=bogus` → stdout: 0 bytes, stderr: `Error: unrecognized level: "bogus"`, exit 1
  - `cpg mcp --version` → stdout: 0 bytes, stderr: `Error: unknown flag: --version`, exit 1
- `cpg generate --totally-unknown-flag` still prints its usual `Error: ... / Usage: ...` to confirm sibling command behavior is unchanged
- `go build ./...` → success
- `go vet ./cmd/cpg/... ./pkg/output/...` → no issues
- `golangci-lint run --max-same-issues=0 --new-from-rev=a39d61a ./cmd/cpg/...` → `0 issues.`
- `gofmt -l` → clean (struct literal field alignment auto-adjusted after removing the `SilenceErrors` field)
- `go test ./cmd/cpg/... -race` → 95 passed, 1 pre-existing failure unrelated to this fix (see note below)

## Note: pre-existing unrelated test failure discovered during verification

While running the task's required `go test ./cmd/cpg/... -race` gate, `TestMCPModeStdoutNeverDefaultsToRealStdout` (`cmd/cpg/mcp_test.go:22`) failed intermittently as part of the **full** `cmd/cpg` package suite (it passes in isolation and in small subsets). This was confirmed to be **pre-existing and unrelated to WR-01/WR-02**: `git stash` of both WR-02 files reproduced the identical failure on the unmodified baseline code (same assertion, same line, same package-level `os.Stdout`/`os.Stderr` pointer-identity symptom), and re-applying the fix reproduces the exact same single failure count (95 passed / 1 failed) both before and after — i.e., the fix introduces zero new failures and fixes/breaks nothing about this test.

Root cause is not fully isolated (requires the full ~96-test package run to reproduce; the only production code that ever executes `os.Stdout = os.Stderr` is `mcp.go`'s `RunE`, which no test in the current suite reaches — `newMCPCmd()` has exactly one caller, the flag-error test, which fails before `RunE` runs). This is not a WR-01/WR-02/IN-01 finding and out of this fix's scope; flagging it here because it means **`go test ./cmd/cpg/... -race` as currently written is not `-race`-clean on `master` today, independent of Phase 16.** Recommend a follow-up finding/ticket to isolate and fix the cross-test `os.Stdout`/`os.Stderr` pollution source.

## Skipped Issues

None — both in-scope findings (WR-01, WR-02) were fixed. IN-01 was out of scope for this run (`fix_scope: critical_warning`; IN-01 is Info-tier).

---

_Fixed: 2026-07-20T17:44:56Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
