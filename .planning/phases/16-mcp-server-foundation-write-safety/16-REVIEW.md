---
phase: 16-mcp-server-foundation-write-safety
reviewed: 2026-07-20T17:32:29Z
depth: standard
files_reviewed: 6
files_reviewed_list:
  - cmd/cpg/main.go
  - cmd/cpg/mcp.go
  - cmd/cpg/mcp_harness_test.go
  - cmd/cpg/mcp_test.go
  - pkg/output/writer.go
  - pkg/output/writer_test.go
findings:
  critical: 0
  warning: 2
  info: 1
  total: 3
status: issues_found
---

# Phase 16: Code Review Report

**Reviewed:** 2026-07-20T17:32:29Z
**Depth:** standard
**Files Reviewed:** 6
**Status:** issues_found

## Summary

Reviewed the atomic temp+rename write path in `pkg/output/writer.go` and the
`cpg mcp` stdio server skeleton in `cmd/cpg/mcp.go` (plus their paired tests
and the one-line `main.go` registration).

The core design is sound and matches its own stated goals under direct
verification: the capture-transport-before-swap ordering in `mcp.go` is
correct Go pointer/struct-literal semantics (confirmed against
`go-sdk@v1.6.1`'s own `StdioTransport`/`IOTransport` source), the
`zap/exp/zapslog` bridge signature matches the installed module, `buildLogger`
genuinely never routes to stdout in any branch, and the new `CreateTemp` →
`Write` → `Close` → `Chmod` → `Rename` sequence in `writer.go` is a correct,
same-directory atomic rename that a dedicated `-race` test exercises
successfully. Cross-checked `pkg/evidence/paths.go` (`ValidatePolicyRef`) and
confirmed the traversal guard is solid. Cross-checked `.planning/phases/16-.../16-CONTEXT.md`
and `PITFALLS.md` before finalizing findings — this correctly ruled out two
initially-plausible concerns (writer-vs-writer locking, and missing
real-stdio-transport test coverage) that turned out to be explicitly locked,
in-scope decisions (D-06 defers the live-transport proof to Phase 19/SRV-04;
the MCP server is structurally readonly, so `Writer.Write` never gains a
second caller).

Two real issues survived that scrutiny, both empirically reproduced against
the compiled binary, not just read from source:

1. The new atomic-write cleanup paths in `writer.go` have 5 unchecked error
   returns that `golangci-lint` flags as **new** issues against this phase's
   base commit — this fails CI's `only-new-issues: true` lint gate as
   currently configured.
2. `cpg mcp` swallows **every** startup/runtime failure completely: running
   the built binary with a bad flag or a bad `--log-level` value produces
   zero bytes on both stdout and stderr, exit code 1 — no diagnostic text
   anywhere, unlike every sibling command.

## Warnings

### WR-01: New atomic-write cleanup paths fail the project's own CI lint gate

**File:** `pkg/output/writer.go:87-100`
**Issue:**

All five cleanup calls added by this phase's temp+rename refactor discard
their error return, and `errcheck` (enabled in `.golangci.yml`, no
suppressions configured) flags every one of them:

```
pkg/output/writer.go:87:12: Error return value of `tmp.Close` is not checked (errcheck)
pkg/output/writer.go:88:12: Error return value of `os.Remove` is not checked (errcheck)
pkg/output/writer.go:92:12: Error return value of `os.Remove` is not checked (errcheck)
pkg/output/writer.go:96:12: Error return value of `os.Remove` is not checked (errcheck)
pkg/output/writer.go:100:12: Error return value of `os.Remove` is not checked (errcheck)
```

(Verified locally with `golangci-lint run --new-from-rev=a39d61a` —
`a39d61a` is this phase's base commit, i.e. the same comparison basis
`.github/workflows/ci.yml`'s `only-new-issues: true` uses. Only 4 of the 5
appear with default settings; golangci-lint's own `max-same-issues: 3`
default silently caps repeated identical messages — line 100 only surfaces
with `--max-same-issues=0`. All 5 are real and all 5 are new: `git blame`
confirms every line in this range was introduced by this phase's commit
`55f7cc20`.)

`git blame` also shows this is a faithful, deliberate mirror of the existing
`CreateTemp`→`Write`→`Close`→`Rename` pattern in `pkg/evidence/writer.go` and
`pkg/hubble/health_writer.go` (per 16-CONTEXT.md's explicit instruction to
"match prior art exactly") — those two files have the identical unchecked-error
shape today. The difference is that their instances predate this phase's base
commit, so they're absorbed into the "29 issues... deferred to the v1.5
lint-cleanup milestone" already carved out in `ci.yml`. This phase's instances
are new lines, so they are not covered by that deferral and will fail the gate.

**Fix:** Match the codebase's own established idiom for intentionally-ignored
cleanup errors (`_ = conn.Close()` in `pkg/hubble/client.go:72,86`):

```go
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("setting temp file permissions: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic rename: %w", err)
	}
```

### WR-02: `cpg mcp` fails completely silently on any startup/runtime error

**File:** `cmd/cpg/mcp.go:37-38` (interacts with `cmd/cpg/main.go:62-68`)
**Issue:**

`newMCPCmd()` sets both `SilenceUsage: true` and `SilenceErrors: true`. The
doc comment (mcp.go:29-32) frames this as protecting stdout: "so cobra's own
usage/error text never reaches stdout." That premise doesn't hold in this
codebase: nothing anywhere in production code calls `cmd.SetOut`/`SetErr`
(confirmed — only test files do), so cobra's `Print`/`Println`/`PrintErrln`
already fall back to `os.Stderr` by default (`cobra@v1.10.2/command.go:1436`
uses `c.OutOrStderr()`, not `OutOrStdout()`, for `Println`; `PrintErrln` uses
`ErrOrStderr()`). `SilenceUsage`/`SilenceErrors` were unnecessary to keep
cobra's own text off *stdout* — that protection was already free. What they
actually do is suppress the message from being printed **anywhere**, because
`main.go`'s own error handling (lines 62-68) never prints `err` either — it
only type-asserts for `*hubble.ExitCodeError` and calls `os.Exit`:

```go
if err := rootCmd.Execute(); err != nil {
	var ec *hubble.ExitCodeError
	if errors.As(err, &ec) {
		os.Exit(ec.Code)
	}
	os.Exit(1)
}
```

Reproduced against the actual compiled binary (not just read from source):

```
$ cpg mcp --totally-unknown-flag
$ echo $?
1
# stdout: 0 bytes, stderr: 0 bytes

$ cpg mcp --log-level=bogus     # valid flag, invalid value -> buildLogger()
                                  # fails inside PersistentPreRunE
$ echo $?
1
# stdout: 0 bytes, stderr: 0 bytes

$ cpg mcp --version              # --version is a rootCmd-only flag; unrecognized
                                  # on the mcp subcommand -> same silent path
$ echo $?
1
# stdout: 0 bytes, stderr: 0 bytes
```

Compare to `cpg generate --totally-unknown-flag`, which prints `Error:
unknown flag: --totally-unknown-flag` plus full usage to stderr, because
`generate` doesn't set `SilenceErrors`. `cpg mcp` is the only command in this
CLI that fails with zero diagnostic output. For a process meant to be spawned
and supervised by an MCP host (Claude Desktop, an IDE, etc.), a silent
`exit(1)` on a misconfigured flag or log level is a real operability problem
— the host has nothing to surface to the user or log.

Note this gap has no test coverage either: `TestMCPCobraFlagErrorStaysOffStdout`
(mcp_test.go) only asserts stdout is empty; it never asserts stderr contains
anything, so it would pass identically whether or not a diagnostic message
was ever printed.

**Fix:** Drop `SilenceErrors` (keep `SilenceUsage` if a full usage dump on
every runtime error is still undesirable for a long-running server — those
are independently-gated in cobra):

```go
return &cobra.Command{
	Use:          "mcp",
	Short:        "Run cpg as a readonly MCP server over stdio",
	SilenceUsage: true,
	// SilenceErrors intentionally not set: cobra's error printer already
	// targets stderr in this codebase (no SetOut/SetErr is ever called in
	// production), so it never touched the stdout wire. Silencing it too
	// just deletes the only diagnostic text a failed `cpg mcp` produces.
	RunE: func(cmd *cobra.Command, _ []string) error {
		...
```

If both must stay silenced for some other reason not captured in the current
comment, then `RunE`/`main()` needs an explicit
`fmt.Fprintln(os.Stderr, "Error:", err)` (or a `logger.Error(...)` call, when
the logger is available) before returning/exiting, and
`TestMCPCobraFlagErrorStaysOffStdout` should gain a companion assertion that
stderr is non-empty so this can't regress silently again.

## Info

### IN-01: `Write()` doc comment doesn't mention the new atomicity guarantee

**File:** `pkg/output/writer.go:32-34`
**Issue:** This phase's entire SEC-02 deliverable is the atomic temp+rename
guarantee, but the exported `Write` method's doc comment still only
describes the merge behavior, not the write mechanism. A caller (e.g. a
future Phase 18 MCP reader relying on "no torn reads") can't learn the
guarantee exists from the doc comment alone.
**Fix:**
```go
// Write writes a PolicyEvent to disk as a YAML file, atomically (temp file
// in the same directory, then rename) so a concurrent reader never observes
// a partial write.
// If the file already exists, it reads the existing policy, merges it with the
// incoming policy using MergePolicy, and writes the merged result.
func (w *Writer) Write(event policy.PolicyEvent) error {
```

---

_Reviewed: 2026-07-20T17:32:29Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
