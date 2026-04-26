---
phase: 13-flags-and-exit-code
plan: "03"
type: execute
wave: 3
depends_on: [13-02]
files_modified:
  - cmd/cpg/main.go
  - cmd/cpg/generate.go
  - cmd/cpg/replay.go
  - README.md
autonomous: true
requirements: [EXIT-01, EXIT-02]

must_haves:
  truths:
    - "cpg exits 0 by default even when infra drops are observed (backward-compat invariant — Pitfall P3)"
    - "cpg exits 1 when --fail-on-infra-drops is set AND stats.InfraDropTotal > 0"
    - "exit-code logic is testable without os.Exit (factored into a helper function)"
    - "README documents both new flags, the exit-code table, and a CI cron example"
  artifacts:
    - path: "cmd/cpg/main.go"
      provides: "ExitCodeError type, updated Execute() error handling"
      contains: "ExitCodeError"
    - path: "cmd/cpg/generate.go"
      provides: "infra-drop exit check after RunPipeline returns"
      contains: "FailOnInfraDrops"
    - path: "cmd/cpg/replay.go"
      provides: "infra-drop exit check after RunPipelineWithSource returns"
      contains: "FailOnInfraDrops"
    - path: "README.md"
      provides: "Exit codes section + CI cron example + --ignore-drop-reason and --fail-on-infra-drops in Flags section"
      contains: "fail-on-infra-drops"
  key_links:
    - from: "cmd/cpg/generate.go:runGenerate"
      to: "cmd/cpg/main.go:ExitCodeError"
      via: "return &ExitCodeError{Code:1} when flag set + InfraDropTotal>0"
      pattern: "ExitCodeError"
    - from: "cmd/cpg/main.go"
      to: "os.Exit"
      via: "errors.As(err, &ec) → os.Exit(ec.Code)"
      pattern: "errors.As"
---

<objective>
Implement `--fail-on-infra-drops` exit-code logic and document both new flags in README.

EXIT-01: When --fail-on-infra-drops is set and InfraDropTotal > 0, cpg exits with code 1. Default behavior (exit 0) is unchanged — backward-compat invariant (Pitfall P3, non-negotiable).

EXIT-02: README gets: --ignore-drop-reason and --fail-on-infra-drops in the Flags section; a new `## Exit codes` section with the 2-row table; a CI cron example using --fail-on-infra-drops.

Implementation: ExitCodeError sentinel type in main.go + errors.As intercept in Execute() block. generate.go and replay.go return the sentinel after RunPipeline/RunPipelineWithSource returns nil. Exit logic factored into a testable shouldExitForInfraDrops(flag bool, total uint64) bool helper.

Output: Working exit-code CI integration + updated README.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/phases/13-flags-and-exit-code/13-CONTEXT.md
</context>

<interfaces>
<!-- Key contracts executor needs — extracted from current codebase. -->

From cmd/cpg/main.go (current Execute block, lines 58-60):
```go
if err := rootCmd.Execute(); err != nil {
    os.Exit(1)
}
```

New main.go pattern (Pattern B from STACK.md Q3):
```go
// ExitCodeError carries a specific exit code for conditions that should not
// be treated as fatal errors (e.g. --fail-on-infra-drops).
type ExitCodeError struct {
    Code int
    Msg  string
}
func (e *ExitCodeError) Error() string { return e.Msg }

// In main():
if err := rootCmd.Execute(); err != nil {
    var ec *ExitCodeError
    if errors.As(err, &ec) {
        os.Exit(ec.Code)
    }
    os.Exit(1)
}
```

From cmd/cpg/generate.go — runGenerate() tail (after the RunPipeline call):
```go
// Current:
return hubble.RunPipeline(ctx, hubble.PipelineConfig{ ... })

// New pattern — need to capture stats:
// BUT RunPipeline/RunPipelineWithSource currently returns error only, not stats.
// The stats are internal to RunPipelineWithSource.
// SOLUTION: RunPipelineWithSource already has access to stats BEFORE returning.
// Pipeline must signal the exit condition without exposing stats externally.
// APPROACH: if FailOnInfraDrops is set in PipelineConfig AND InfraDropTotal > 0,
// RunPipelineWithSource returns &ExitCodeError{Code:1, Msg:"infra drops detected"}
// INSTEAD of returning nil.
// This keeps stats internal and avoids changing the function signature.
```

IMPORTANT: The exit-code error must be returned by RunPipelineWithSource (not the cmd layer) because stats are internal. The pipeline sets:
```go
// At the end of RunPipelineWithSource, after all stats are collected:
if err == nil && cfg.FailOnInfraDrops && stats.InfraDropTotal > 0 {
    return &ExitCodeError{Code: 1, Msg: fmt.Sprintf("infra drops detected: %d flows suppressed", stats.InfraDropTotal)}
}
return err
```

BUT ExitCodeError is defined in cmd/cpg/main.go (package main). Pipeline is in pkg/hubble. IMPORT CYCLE risk.

SOLUTION: Define ExitCodeError in pkg/hubble (or a tiny pkg/exitcode package), import it in both pipeline.go and main.go. OR: keep it in cmd/cpg and have pipeline.go return a sentinel error value that main.go checks differently.

SIMPLEST CORRECT APPROACH (no new package, no import cycle):
- Define ExitCodeError in pkg/hubble/pipeline.go (alongside PipelineConfig).
- main.go imports pkg/hubble already → can errors.As on hubble.ExitCodeError.
- generate.go/replay.go import pkg/hubble already → can return it.
- RunPipelineWithSource returns hubble.ExitCodeError when appropriate.

This is the cleanest path. ExitCodeError belongs in pkg/hubble since it's pipeline-originated.

From pkg/hubble/pipeline.go — end of RunPipelineWithSource (lines 268-288):
```go
stats.Log(cfg.Logger)
return err  // ← this is what we modify
```

New tail:
```go
stats.Log(cfg.Logger)
// EXIT-01: --fail-on-infra-drops opt-in exit code.
// Default behavior (exit 0) is UNCHANGED when flag is not set (Pitfall P3).
if err == nil && cfg.FailOnInfraDrops && stats.InfraDropTotal > 0 {
    return &ExitCodeError{
        Code: 1,
        Msg:  fmt.Sprintf("--fail-on-infra-drops: %d infra drop(s) observed", stats.InfraDropTotal),
    }
}
return err
```

ExitCodeError struct (add to pipeline.go, before PipelineConfig):
```go
// ExitCodeError carries a specific numeric exit code for conditions that
// represent a successful pipeline run with a non-default exit signal.
// Used by --fail-on-infra-drops (EXIT-01): the pipeline completed correctly
// but the caller (cmd/cpg/main.go) should exit with Code instead of 0.
type ExitCodeError struct {
    Code int
    Msg  string
}
func (e *ExitCodeError) Error() string { return e.Msg }
```

main.go updated Execute block (add "errors" import):
```go
if err := rootCmd.Execute(); err != nil {
    var ec *hubble.ExitCodeError
    if errors.As(err, &ec) {
        os.Exit(ec.Code)
    }
    os.Exit(1)
}
```

Testable helper (in pipeline.go, for unit test coverage without os.Exit):
```go
// shouldExitForInfraDrops returns true when --fail-on-infra-drops is set
// and at least one infra drop was observed. Factored out for unit testing.
func shouldExitForInfraDrops(failOnInfraDrops bool, infraDropTotal uint64) bool {
    return failOnInfraDrops && infraDropTotal > 0
}
```

Unit test coverage (in pkg/hubble/pipeline_test.go or a new file):
- shouldExitForInfraDrops(false, 10) == false (flag not set → always 0)
- shouldExitForInfraDrops(true, 0) == false (flag set but no infra drops → exit 0)
- shouldExitForInfraDrops(true, 5) == true (flag set + drops → exit 1)

README additions (see success_criteria for exact text).
</interfaces>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: ExitCodeError + shouldExitForInfraDrops + pipeline wiring</name>
  <files>pkg/hubble/pipeline.go, cmd/cpg/main.go</files>
  <behavior>
    - shouldExitForInfraDrops(false, 100) == false
    - shouldExitForInfraDrops(true, 0) == false
    - shouldExitForInfraDrops(true, 1) == true
    - RunPipelineWithSource returns nil when FailOnInfraDrops=false regardless of InfraDropTotal
    - RunPipelineWithSource returns *ExitCodeError{Code:1} when FailOnInfraDrops=true AND InfraDropTotal>0
    - errors.As(*ExitCodeError, &ec) succeeds; ec.Code == 1
  </behavior>
  <action>
    **pkg/hubble/pipeline.go:**

    1. Add ExitCodeError struct and Error() method before PipelineConfig definition.

    2. Add shouldExitForInfraDrops(failOnInfraDrops bool, infraDropTotal uint64) bool helper.

    3. At end of RunPipelineWithSource (after stats.Log call, before `return err`):
    ```go
    if shouldExitForInfraDrops(cfg.FailOnInfraDrops, stats.InfraDropTotal) {
        return &ExitCodeError{
            Code: 1,
            Msg:  fmt.Sprintf("--fail-on-infra-drops: %d infra drop(s) observed", stats.InfraDropTotal),
        }
    }
    return err
    ```

    Write unit tests for shouldExitForInfraDrops in pkg/hubble/pipeline_exit_test.go (3 table-driven cases).

    **cmd/cpg/main.go:**

    1. Add `"errors"` to imports.
    2. Add `hubble "github.com/SoulKyu/cpg/pkg/hubble"` import if not already present.
    3. Update Execute() error block:
    ```go
    if err := rootCmd.Execute(); err != nil {
        var ec *hubble.ExitCodeError
        if errors.As(err, &ec) {
            os.Exit(ec.Code)
        }
        os.Exit(1)
    }
    ```

    Run tests:
    ```
    cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/... ./cmd/cpg/... -race -count=1
    ```

    Commit: `feat(13-03): ExitCodeError + --fail-on-infra-drops exit logic`
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/... ./cmd/cpg/... -race -count=1 -v 2>&1 | grep -E "shouldExitFor|ExitCode|PASS|FAIL" | head -20</automated>
  </verify>
  <done>shouldExitForInfraDrops tests pass. RunPipelineWithSource returns ExitCodeError when flag+drops. main.go compiles with errors.As intercept. go vet clean.</done>
</task>

<task type="auto">
  <name>Task 2: README documentation (EXIT-02)</name>
  <files>README.md</files>
  <action>
    Make the following targeted additions to README.md. Read the file first to find exact insertion points.

    **1. In the `## Flags` section, Filtering block — add two rows after `--ignore-protocol`:**

    ```
        --ignore-drop-reason strs  Exclude flows by Cilium drop reason name before classification;
                                   repeatable / comma-separated / case-insensitive.
                                   Passing a reason already classified as infra or transient emits
                                   a warning (it is already suppressed by default).

    CI integration:
        --fail-on-infra-drops      Exit with code 1 when ≥1 infra drop is observed (default:
                                   always exit 0). Use in CI/cron pipelines to alert on cluster
                                   health issues.
    ```

    **2. In `## Offline replay` section, update the "Flags shared with generate" sentence to include the two new flags:**

    Current: `Flags shared with generate (--output-dir, --cluster-dedup, --flush-interval, --ignore-protocol) work identically.`

    Replace with: `Flags shared with generate (--output-dir, --cluster-dedup, --flush-interval, --ignore-protocol, --ignore-drop-reason, --fail-on-infra-drops) work identically.`

    **3. Add a new `## Exit codes` section immediately before the `## Limitations` section (or at end of Flags section if no Limitations exists). Find the correct location by reading the file.**

    ```markdown
    ## Exit codes

    | Code | Meaning                                                                 |
    |------|-------------------------------------------------------------------------|
    | 0    | Success — policies generated (or previewed). Default even with infra drops. |
    | 1    | `--fail-on-infra-drops` was set **and** ≥1 infra drop was observed.     |

    Any other non-zero exit means cpg encountered a fatal error (connection
    failure, bad flag, etc.).

    ### CI / cron example

    ```bash
    # Alert when infra drops appear in a captured window
    cpg replay /tmp/last-hour.jsonl --fail-on-infra-drops \
      || alert-team "cpg detected infra drops — check cluster-health.json"
    ```

    With `cpg generate` (live stream — run for a fixed window with timeout):

    ```bash
    timeout 300 cpg generate -n production --fail-on-infra-drops \
      || alert-team "infra drops in production — see cluster-health.json"
    ```
    ```

    After edits, verify the README renders correctly (no broken markdown):
    ```
    cd /home/gule/Workspace/team-infrastructure/cpg && grep -c "fail-on-infra-drops" README.md
    ```
    Expected: at least 4 occurrences.

    Commit: `docs(13-03): document --ignore-drop-reason, --fail-on-infra-drops, exit codes in README`
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && grep -c "fail-on-infra-drops" README.md && grep -c "ignore-drop-reason" README.md && grep -c "Exit codes" README.md</automated>
  </verify>
  <done>README contains: --fail-on-infra-drops ≥4 times, --ignore-drop-reason ≥2 times, "Exit codes" section header at least once. go build ./... still clean.</done>
</task>

</tasks>

<verification>
```bash
cd /home/gule/Workspace/team-infrastructure/cpg
go test ./... -race -count=1
go vet ./...
go build ./...

# Smoke-test flag appears in help
./bin/cpg generate --help 2>&1 | grep fail-on-infra-drops
./bin/cpg replay --help 2>&1 | grep fail-on-infra-drops

# README audit
grep "fail-on-infra-drops" README.md | wc -l   # ≥4
grep "Exit codes" README.md                     # exists
```
</verification>

<success_criteria>
- `cpg generate --fail-on-infra-drops` exits 1 when infra drops observed (manual or integration test)
- `cpg generate` (without flag) exits 0 regardless of infra drops (P3 backward-compat invariant)
- shouldExitForInfraDrops(false, N) == false for all N (unit tested)
- shouldExitForInfraDrops(true, 0) == false (unit tested)
- shouldExitForInfraDrops(true, 1+) == true (unit tested)
- README: --ignore-drop-reason in Flags section
- README: --fail-on-infra-drops in Flags section
- README: ## Exit codes section with 2-row table
- README: CI cron example using --fail-on-infra-drops
- go test ./... -race passes
</success_criteria>

<output>
After completion, create `/home/gule/Workspace/team-infrastructure/cpg/.planning/phases/13-flags-and-exit-code/13-03-exit-code-and-readme-SUMMARY.md`
</output>
