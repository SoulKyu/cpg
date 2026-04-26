---
phase: 13-flags-and-exit-code
plan: "02"
type: tdd
wave: 2
depends_on: [13-01]
files_modified:
  - cmd/cpg/commonflags.go
  - cmd/cpg/commonflags_test.go
  - cmd/cpg/generate.go
  - cmd/cpg/replay.go
  - pkg/hubble/pipeline.go
autonomous: true
requirements: [FILTER-01, FILTER-02, FILTER-03]

must_haves:
  truths:
    - "--ignore-drop-reason flag registered on both generate and replay; repeatable and comma-separated (StringSlice)"
    - "--fail-on-infra-drops bool flag registered on both generate and replay"
    - "validateIgnoreDropReasons() rejects unknown reason names at parse time with error listing valid names"
    - "validateIgnoreDropReasons() emits WARN (via logger) for reasons already classified as Infra or Transient"
    - "PipelineConfig carries IgnoreDropReasons []string and FailOnInfraDrops bool"
    - "RunPipelineWithSource calls agg.SetIgnoreDropReasons(cfg.IgnoreDropReasons)"
    - "generate.go and replay.go pass both new fields to PipelineConfig"
  artifacts:
    - path: "cmd/cpg/commonflags.go"
      provides: "validateIgnoreDropReasons(), addCommonFlags updated, parseCommonFlags updated, commonFlags struct updated"
      contains: "ignoreDropReasons"
    - path: "cmd/cpg/commonflags_test.go"
      provides: "TDD validation tests"
    - path: "pkg/hubble/pipeline.go"
      provides: "PipelineConfig.IgnoreDropReasons, PipelineConfig.FailOnInfraDrops, agg.SetIgnoreDropReasons call"
      contains: "IgnoreDropReasons"
  key_links:
    - from: "cmd/cpg/commonflags.go:validateIgnoreDropReasons"
      to: "pkg/dropclass.ValidReasonNames()"
      via: "allowlist for unknown-reason check"
      pattern: "dropclass.ValidReasonNames"
    - from: "cmd/cpg/commonflags.go:validateIgnoreDropReasons"
      to: "pkg/dropclass.Classify()"
      via: "FILTER-03 WARN: reason already classified Infra/Transient"
      pattern: "dropclass.Classify"
    - from: "pkg/hubble/pipeline.go:RunPipelineWithSource"
      to: "agg.SetIgnoreDropReasons"
      via: "cfg.IgnoreDropReasons passed after agg construction"
      pattern: "SetIgnoreDropReasons"
---

<objective>
Wire `--ignore-drop-reason` and `--fail-on-infra-drops` flags into the CLI surface and pipeline config.

FILTER-02: validateIgnoreDropReasons() rejects unknown reason names at parse time (mirrors validateIgnoreProtocols exactly).
FILTER-03: validateIgnoreDropReasons() emits WARN when a passed reason is already Infra or Transient class (redundant with default suppression).
FILTER-01 (cmd side): PipelineConfig.IgnoreDropReasons wired through to agg.SetIgnoreDropReasons in pipeline.go.
EXIT-01 (flag only): PipelineConfig.FailOnInfraDrops field added; generate.go/replay.go pass it; exit logic itself is plan 13-03.

Purpose: Clean flag-to-pipeline contract. validateIgnoreDropReasons() is the gate for both FILTER-02 rejection and FILTER-03 warning.

Output: Both flags usable from CLI. PipelineConfig extended. Pipeline calls SetIgnoreDropReasons. Exit logic NOT yet implemented (plan 13-03).
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

From cmd/cpg/commonflags.go — validateIgnoreProtocols (exact mirror to copy):
```go
func validateIgnoreProtocols(in []string) ([]string, error) {
    if len(in) == 0 { return nil, nil }
    allow := make(map[string]struct{}, len(hubble.ValidIgnoreProtocols()))
    for _, p := range hubble.ValidIgnoreProtocols() { allow[p] = struct{}{} }
    out := make([]string, 0, len(in))
    for _, raw := range in {
        v := strings.ToLower(raw)
        if _, ok := allow[v]; !ok {
            return nil, fmt.Errorf("unknown protocol %q: valid values are %s", raw, strings.Join(hubble.ValidIgnoreProtocols(), ", "))
        }
        out = append(out, v)
    }
    return out, nil
}
```

New validateIgnoreDropReasons — DIFFERENCES from above:
- allowlist is from `dropclass.ValidReasonNames()` (not hubble.ValidIgnoreProtocols)
- comparison uses strings.ToUpper (canonical enum names are uppercase)
- error message: `"unknown drop reason %q: valid values are %s"` (per CONTEXT.md specifics)
- AFTER the unknown check: iterate normalized reasons and call `dropclass.Classify(flowpb.DropReason(flowpb.DropReason_value[name]))` — if class is Infra or Transient, log WARN (requires a logger parameter OR returns warnings as []string for caller to log)
- WARN format: `"--ignore-drop-reason %q is redundant: reason is already classified as %s and suppressed by default"` (per CONTEXT.md specifics)
- Returns ([]string, error) — normalized (uppercase) names on success, error on unknown reason
- WARN is logged via zap.Logger, so validateIgnoreDropReasons must accept a *zap.Logger parameter:
  `func validateIgnoreDropReasons(in []string, logger *zap.Logger) ([]string, error)`

From cmd/cpg/commonflags.go — struct and registration patterns:
```go
type commonFlags struct {
    // ... existing fields ...
    ignoreProtocols []string
}
// Add:
//   ignoreDropReasons  []string
//   failOnInfraDrops   bool

// addCommonFlags line to add:
f.StringSlice("ignore-drop-reason", nil, "exclude flows by drop reason name before classification (repeatable, comma-separated, case-insensitive). Run with --help to see valid values.")
f.Bool("fail-on-infra-drops", false, "exit with code 1 when ≥1 infra drop is observed (default: always exit 0)")

// parseCommonFlags lines to add:
out.ignoreDropReasons, _ = f.GetStringSlice("ignore-drop-reason")
out.failOnInfraDrops, _ = f.GetBool("fail-on-infra-drops")
```

From cmd/cpg/generate.go — runGenerate call pattern:
```go
ignoreProtocols, err := validateIgnoreProtocols(f.ignoreProtocols)
if err != nil { return err }
// Add AFTER above:
ignoreDropReasons, err := validateIgnoreDropReasons(f.ignoreDropReasons, logger)
if err != nil { return err }
```
Then pass to PipelineConfig:
```go
IgnoreDropReasons: ignoreDropReasons,
FailOnInfraDrops:  f.failOnInfraDrops,
```

Same pattern in replay.go.

From pkg/hubble/pipeline.go — PipelineConfig struct additions:
```go
// IgnoreDropReasons is the uppercase, already-validated set of DropReason
// name strings whose flows must be excluded before classification (phase 13
// FILTER-01). Caller (cmd/cpg) is responsible for validation.
IgnoreDropReasons []string

// FailOnInfraDrops: when true, RunPipelineWithSource signals to the caller
// that exit code 1 should be used if InfraDropTotal > 0. The pipeline
// itself does not call os.Exit — that is cmd/cpg's responsibility (plan 13-03).
FailOnInfraDrops bool
```

From pkg/hubble/pipeline.go — agg setup block (lines 137-143):
```go
agg.SetL7Enabled(cfg.L7Enabled)
agg.SetIgnoreProtocols(cfg.IgnoreProtocols)
// Add:
agg.SetIgnoreDropReasons(cfg.IgnoreDropReasons)
```

Import needed in commonflags.go:
```go
"github.com/SoulKyu/cpg/pkg/dropclass"
flowpb "github.com/cilium/cilium/api/v1/flow"
```
</interfaces>

<tasks>

<task type="tdd">
  <name>Task 1: RED — Write failing tests for validateIgnoreDropReasons</name>
  <files>cmd/cpg/commonflags_test.go</files>
  <behavior>
    - TestValidateIgnoreDropReasonsEmpty: nil/empty input → nil, nil
    - TestValidateIgnoreDropReasonsValid: "CT_MAP_INSERTION_FAILED" → normalized uppercase, no error
    - TestValidateIgnoreDropReasonsCaseInsensitive: "ct_map_insertion_failed" → "CT_MAP_INSERTION_FAILED", no error
    - TestValidateIgnoreDropReasonsCommaSeparated: StringSlice already splits on comma before call; test that two valid reasons both normalize
    - TestValidateIgnoreDropReasonsUnknown: "TOTALLY_MADE_UP" → error containing "unknown drop reason"
    - TestValidateIgnoreDropReasonsRedundantWarn: pass a reason classified as Infra (e.g. "CT_MAP_INSERTION_FAILED") → no error, but WARN logged (use zaptest.NewLogger observer or check zap observer for WARN message containing "redundant")
    - TestValidateIgnoreDropReasonsRedundantTransient: pass a reason classified as Transient (e.g. "TTL_EXCEEDED") → WARN logged, no error
    - TestValidateIgnoreDropReasonsPolicyNoWarn: pass a reason classified as Policy (e.g. "POLICY_DENIED") → no error, no WARN (policy reasons are non-redundant — user wants to ignore even policy drops)
  </behavior>
  <action>
    Create or extend cmd/cpg/commonflags_test.go. Tests fail (RED) because validateIgnoreDropReasons does not exist yet.

    Use zaptest for logger capture:
    ```go
    import (
        "go.uber.org/zap"
        "go.uber.org/zap/zaptest/observer"
    )
    core, logs := observer.New(zap.WarnLevel)
    logger := zap.New(core)
    _, err := validateIgnoreDropReasons([]string{"CT_MAP_INSERTION_FAILED"}, logger)
    // assert logs.Len() == 1, logs.All()[0].Message contains "redundant"
    ```

    Run to confirm RED:
    ```
    cd /home/gule/Workspace/team-infrastructure/cpg && go test ./cmd/cpg/... 2>&1 | head -20
    ```

    Commit: `test(13-02): add failing tests for validateIgnoreDropReasons`
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test ./cmd/cpg/... 2>&1 | grep -E "FAIL|undefined|does not compile"</automated>
  </verify>
  <done>Tests exist and fail with compile errors (validateIgnoreDropReasons undefined)</done>
</task>

<task type="tdd">
  <name>Task 2: GREEN — Implement flags, validation, PipelineConfig wiring</name>
  <files>cmd/cpg/commonflags.go, cmd/cpg/generate.go, cmd/cpg/replay.go, pkg/hubble/pipeline.go</files>
  <behavior>
    All 8 new tests pass. All existing cmd/cpg and pkg/hubble tests pass. go vet clean. go build clean.
  </behavior>
  <action>
    **1. cmd/cpg/commonflags.go:**

    Add imports (dropclass, flowpb, zap).

    Extend commonFlags struct:
    ```go
    ignoreDropReasons []string
    failOnInfraDrops  bool
    ```

    Add to addCommonFlags():
    ```go
    f.StringSlice("ignore-drop-reason", nil,
        "exclude flows by drop reason name before classification "+
        "(repeatable, comma-separated, case-insensitive). "+
        "Passing a reason already classified as infra/transient emits a warning.")
    f.Bool("fail-on-infra-drops", false,
        "exit with code 1 when ≥1 infra drop is observed (default: always exit 0)")
    ```

    Add to parseCommonFlags():
    ```go
    out.ignoreDropReasons, _ = f.GetStringSlice("ignore-drop-reason")
    out.failOnInfraDrops, _ = f.GetBool("fail-on-infra-drops")
    ```

    Add validateIgnoreDropReasons function:
    ```go
    // validateIgnoreDropReasons normalizes --ignore-drop-reason input (uppercase),
    // rejects unknown names (FILTER-02), and warns when a name is already
    // classified Infra/Transient (FILTER-03). nil/empty input is a no-op.
    func validateIgnoreDropReasons(in []string, logger *zap.Logger) ([]string, error) {
        if len(in) == 0 { return nil, nil }

        // Build allowlist from canonical protobuf enum names (UPPERCASE).
        all := dropclass.ValidReasonNames()
        allow := make(map[string]struct{}, len(all))
        for _, n := range all { allow[n] = struct{}{} }

        out := make([]string, 0, len(in))
        for _, raw := range in {
            v := strings.ToUpper(raw)
            if _, ok := allow[v]; !ok {
                return nil, fmt.Errorf("unknown drop reason %q: valid values are %s",
                    raw, strings.Join(all, ", "))
            }
            // FILTER-03: warn when reason is already suppressed by default.
            reasonVal, exists := flowpb.DropReason_value[v]
            if exists {
                class := dropclass.Classify(flowpb.DropReason(reasonVal))
                if class == dropclass.DropClassInfra || class == dropclass.DropClassTransient {
                    if logger != nil {
                        logger.Warn("--ignore-drop-reason is redundant: reason is already classified and suppressed by default",
                            zap.String("reason", v),
                            zap.String("class", dropClassLabel(class)),
                        )
                    }
                }
            }
            out = append(out, v)
        }
        return out, nil
    }

    // dropClassLabel returns a human-readable label for a DropClass value.
    // Local helper — avoids importing dropclass String() which doesn't exist.
    func dropClassLabel(c dropclass.DropClass) string {
        switch c {
        case dropclass.DropClassInfra:     return "infra"
        case dropclass.DropClassTransient: return "transient"
        case dropclass.DropClassPolicy:    return "policy"
        case dropclass.DropClassNoise:     return "noise"
        default:                           return "unknown"
        }
    }
    ```

    **2. cmd/cpg/generate.go — runGenerate():**

    After `validateIgnoreProtocols` call, add:
    ```go
    ignoreDropReasons, err := validateIgnoreDropReasons(f.ignoreDropReasons, logger)
    if err != nil { return err }
    ```

    Add to PipelineConfig literal:
    ```go
    IgnoreDropReasons: ignoreDropReasons,
    FailOnInfraDrops:  f.failOnInfraDrops,
    ```

    **3. cmd/cpg/replay.go — runReplay():**

    Same pattern as generate.go: call validateIgnoreDropReasons after validateIgnoreProtocols, pass both new fields to PipelineConfig.

    **4. pkg/hubble/pipeline.go:**

    Add to PipelineConfig struct (after IgnoreProtocols field):
    ```go
    // IgnoreDropReasons is the uppercase, already-validated set of DropReason
    // name strings excluded before classification (FILTER-01 phase 13).
    IgnoreDropReasons []string

    // FailOnInfraDrops signals that the caller should exit with code 1 when
    // InfraDropTotal > 0. The pipeline does NOT call os.Exit (plan 13-03).
    FailOnInfraDrops bool
    ```

    Add after `agg.SetIgnoreProtocols(cfg.IgnoreProtocols)` line:
    ```go
    agg.SetIgnoreDropReasons(cfg.IgnoreDropReasons)
    ```

    Note: FailOnInfraDrops is stored in PipelineConfig but RunPipelineWithSource does NOT act on it yet — that is plan 13-03's job.

    Run all tests:
    ```
    cd /home/gule/Workspace/team-infrastructure/cpg && go test ./... -race -count=1
    ```

    Commit: `feat(13-02): wire --ignore-drop-reason and --fail-on-infra-drops flags`
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test ./... -race -count=1 2>&1 | tail -10</automated>
  </verify>
  <done>go test ./... -race passes. go build ./... clean. go vet ./... clean. Both flags appear in cpg generate --help output.</done>
</task>

</tasks>

<verification>
```bash
cd /home/gule/Workspace/team-infrastructure/cpg
go test ./... -race -count=1
go vet ./...
go build ./...
./bin/cpg generate --help 2>&1 | grep -E "ignore-drop-reason|fail-on-infra-drops"
./bin/cpg replay --help 2>&1 | grep -E "ignore-drop-reason|fail-on-infra-drops"
```
All pass. Both flags visible in help output of both subcommands.
</verification>

<success_criteria>
- validateIgnoreDropReasons(nil, logger) → (nil, nil)
- validateIgnoreDropReasons(["TOTALLY_MADE_UP"], logger) → (nil, error containing "unknown drop reason")
- validateIgnoreDropReasons(["ct_map_insertion_failed"], logger) → (["CT_MAP_INSERTION_FAILED"], nil) + WARN logged
- validateIgnoreDropReasons(["POLICY_DENIED"], logger) → (["POLICY_DENIED"], nil) + no WARN
- Both flags registered on generate and replay subcommands
- PipelineConfig.IgnoreDropReasons and FailOnInfraDrops fields present
- agg.SetIgnoreDropReasons called in RunPipelineWithSource
- go test ./... -race passes
</success_criteria>

<output>
After completion, create `/home/gule/Workspace/team-infrastructure/cpg/.planning/phases/13-flags-and-exit-code/13-02-commonflags-and-pipeline-wiring-SUMMARY.md`
</output>
