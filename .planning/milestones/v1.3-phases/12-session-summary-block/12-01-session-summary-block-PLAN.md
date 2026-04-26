---
phase: 12-session-summary-block
plan: "01"
type: tdd
wave: 1
depends_on: []
files_modified:
  - pkg/hubble/summary.go
  - pkg/hubble/summary_test.go
  - pkg/hubble/health_writer.go
  - pkg/hubble/pipeline.go
  - cmd/cpg/generate.go
  - cmd/cpg/replay.go
autonomous: true
requirements: [HEALTH-03]

must_haves:
  truths:
    - "After a run with >=1 infra drop, a summary block is printed to stdout listing each observed reason"
    - "Reasons are sorted: infra class before transient class, then by descending count within same class"
    - "Each reason line shows top-3 nodes and top-3 workloads by volume with (N more) suffix when truncated"
    - "Each reason line shows the RemediationHint URL when non-empty (Hint: ...)"
    - "The block ends with the absolute path to cluster-health.json"
    - "When dry-run, path line appends (dry-run, not written)"
    - "When zero infra drops, no summary block is printed (zero noise)"
  artifacts:
    - path: "pkg/hubble/summary.go"
      provides: "PrintClusterHealthSummary + top3 helpers"
      exports: ["PrintClusterHealthSummary"]
    - path: "pkg/hubble/summary_test.go"
      provides: "TDD tests — RED first, then GREEN"
    - path: "pkg/hubble/health_writer.go"
      provides: "Snapshot() []HealthDropSnapshot method on healthWriter"
      contains: "func (hw *healthWriter) Snapshot"
    - path: "pkg/hubble/pipeline.go"
      provides: "Stdout io.Writer field on PipelineConfig; PrintClusterHealthSummary call after finalize"
      contains: "PrintClusterHealthSummary"
  key_links:
    - from: "pkg/hubble/pipeline.go"
      to: "pkg/hubble/summary.go"
      via: "PrintClusterHealthSummary(stdout, hw.Snapshot(), stats, healthPath, cfg.DryRun)"
      pattern: "PrintClusterHealthSummary"
    - from: "cmd/cpg/generate.go + replay.go"
      to: "pkg/hubble/PipelineConfig"
      via: "cfg.Stdout = os.Stdout (default nil -> os.Stdout in pipeline)"
      pattern: "Stdout.*os.Stdout"
---

<objective>
Print a concise cluster-health summary block to stdout at the end of every
`cpg generate` and `cpg replay` run that observed >=1 infra/transient drop.

Purpose: Operators must never miss infrastructure-level Hubble drops. The JSON
artifact exists (cluster-health.json from phase 11) but is silent. This block
surfaces the key signal immediately in the terminal.

Output: pkg/hubble/summary.go, pkg/hubble/summary_test.go, Snapshot() method on
healthWriter, Stdout field on PipelineConfig, wiring in pipeline.go.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/phases/12-session-summary-block/12-CONTEXT.md
@.planning/phases/11-aggregator-suppression-and-health-writer/11-02-health-writer-and-pipeline-wiring-SUMMARY.md

@pkg/hubble/pipeline.go
@pkg/hubble/health_writer.go
@pkg/dropclass/hints.go
@cmd/cpg/generate.go
@cmd/cpg/replay.go
</context>

<interfaces>
<!-- Key types the executor needs. Extracted from pkg/hubble/health_writer.go and pipeline.go -->

From pkg/hubble/health_writer.go:
```go
// healthDropEntry — unexported, same package as summary.go
type healthDropEntry struct {
    reason     flowpb.DropReason
    class      dropclass.DropClass
    count      uint64
    byNode     map[string]uint64
    byWorkload map[string]uint64
}

type healthWriter struct {
    evidenceDir string
    outputHash  string
    logger      *zap.Logger
    drops       map[flowpb.DropReason]*healthDropEntry
    startedAt   time.Time
}
```

From pkg/hubble/pipeline.go:
```go
type PipelineConfig struct {
    // ... existing fields ...
    DryRun      bool
    EvidenceDir string
    OutputHash  string
    // NEW field to add:
    // Stdout io.Writer  // nil -> os.Stdout; injectable for tests
}

type SessionStats struct {
    InfraDropTotal     uint64
    InfraDropsByReason map[flowpb.DropReason]uint64
    // ... other fields
}
```

From pkg/dropclass/hints.go:
```go
func RemediationHint(reason flowpb.DropReason) string   // "" for non-infra
```

From pkg/dropclass/ (taxonomy):
```go
const (
    DropClassPolicy   DropClass = iota  // 0
    DropClassInfra                      // 1
    DropClassTransient                  // 2
    DropClassNoise                      // 3
    DropClassUnknown                    // 4
)
```
</interfaces>

<tasks>

<task type="tdd">
  <name>Task 1: Write failing tests (RED)</name>
  <files>pkg/hubble/summary_test.go</files>
  <behavior>
    - Test: full block — synthetic hw with 2 entries (CT_MAP_INSERTION_FAILED infra 47 flows, POLICY_DENIED_REVERSE transient 5 flows); assert printed text contains header line, reason lines sorted infra-before-transient, top nodes/workloads, Hint: URL for CT_MAP_INSERTION_FAILED, path line.
    - Test: zero infra drops (empty hw.drops) — assert output is exactly "".
    - Test: single contributor — 1 node, 1 workload — assert no "(+N more)" in output.
    - Test: >3 contributors — 5 nodes — assert "(+2 more)" suffix present.
    - Test: dry-run path — assert "(dry-run, not written)" present in path line.
    - Test: no remediation hint — reason with empty RemediationHint — assert no "Hint:" line.
    - Test: severity sort — infra entry with lower count than transient entry — infra still printed first.
    - Test: within same class, higher count printed first.
  </behavior>
  <action>
    Create pkg/hubble/summary_test.go.

    read_first: pkg/hubble/health_writer.go (understand healthWriter + healthDropEntry fields),
                pkg/hubble/health_writer_test.go (understand how tests build synthetic healthWriter),
                pkg/dropclass/hints.go (RemediationHint function signature).

    Pattern for building a synthetic healthWriter in tests:
    ```go
    hw := &healthWriter{
        evidenceDir: t.TempDir(),
        outputHash:  "abc123",
        logger:      zap.NewNop(),
        drops:       map[flowpb.DropReason]*healthDropEntry{ ... },
        startedAt:   time.Now(),
    }
    ```

    Call PrintClusterHealthSummary (not yet implemented) and assert output.
    Use strings.Builder or bytes.Buffer as the io.Writer target.

    All tests MUST fail (PrintClusterHealthSummary does not exist yet).
    Commit with message: `test(12-01): add failing tests for PrintClusterHealthSummary`
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/... -run TestPrintClusterHealthSummary 2>&1 | grep -E "FAIL|does not compile|undefined"</automated>
  </verify>
  <done>Tests exist and fail with "undefined: PrintClusterHealthSummary" (compile error or test failure). No passing tests yet.</done>
</task>

<task type="tdd">
  <name>Task 2: Implement summary block + Snapshot + pipeline wiring (GREEN)</name>
  <files>pkg/hubble/summary.go, pkg/hubble/health_writer.go, pkg/hubble/pipeline.go</files>
  <behavior>
    Same behaviors as Task 1 tests — all tests must pass after this task.
  </behavior>
  <action>
    read_first: pkg/hubble/summary_test.go (failing tests to make pass),
                pkg/hubble/pipeline.go (where to add Stdout field + call site),
                pkg/hubble/health_writer.go (where to add Snapshot()).

    Step A — Add Snapshot() to health_writer.go:

    Add a package-internal snapshot type and Snapshot() method:
    ```go
    // HealthDropSnapshot is the per-reason view used by the summary formatter.
    // Unexported: used only within pkg/hubble.
    type HealthDropSnapshot struct {
        Reason  flowpb.DropReason
        Class   dropclass.DropClass
        Count   uint64
        ByNode  map[string]uint64  // shallow copy
        ByWorkload map[string]uint64  // shallow copy
    }

    // Snapshot returns a copy of the accumulated drop entries.
    // Returns nil when hw is nil (dry-run / evidence disabled).
    func (hw *healthWriter) Snapshot() []HealthDropSnapshot {
        if hw == nil {
            return nil
        }
        result := make([]HealthDropSnapshot, 0, len(hw.drops))
        for _, e := range hw.drops {
            result = append(result, HealthDropSnapshot{
                Reason:     e.reason,
                Class:      e.class,
                Count:      e.count,
                ByNode:     shallowCopyMap(e.byNode),
                ByWorkload: shallowCopyMap(e.byWorkload),
            })
        }
        return result
    }
    ```
    Add private helper `func shallowCopyMap(m map[string]uint64) map[string]uint64`.

    Step B — Create pkg/hubble/summary.go:

    Package `hubble`. Imports: `fmt`, `io`, `os`, `path/filepath`, `sort`, `strings`,
    `flowpb "github.com/cilium/cilium/api/v1/flow"`, `"github.com/SoulKyu/cpg/pkg/dropclass"`.

    ```go
    const summaryWidth = 52  // ━ frame width (per CONTEXT.md: 76 max; keep compact)

    // PrintClusterHealthSummary writes the end-of-run cluster-health block to out.
    // No-op when snapshots is nil or empty (zero infra drops).
    // healthPath is the absolute path to cluster-health.json.
    // dryRun appends "(dry-run, not written)" to the path line.
    func PrintClusterHealthSummary(out io.Writer, snapshots []HealthDropSnapshot, stats *SessionStats, healthPath string, dryRun bool) {
        if len(snapshots) == 0 {
            return
        }
        // sort: infra before transient, then descending count within same class
        sort.Slice(snapshots, func(i, j int) bool {
            if snapshots[i].Class != snapshots[j].Class {
                return snapshots[i].Class < snapshots[j].Class  // infra(1) < transient(2)
            }
            return snapshots[i].Count > snapshots[j].Count
        })
        frame := strings.Repeat("━", summaryWidth)
        fmt.Fprintln(out, frame)
        fmt.Fprintln(out, "! Cluster-critical drops detected (NOT a policy issue)")
        fmt.Fprintln(out, frame)
        for _, s := range snapshots {
            name := flowpb.DropReason_name[int32(s.Reason)]
            class := dropClassString(s.Class)
            fmt.Fprintf(out, "  %-38s [%s]  %d flows\n", name, class, s.Count)
            fmt.Fprintf(out, "    Top nodes:     %s\n", top3(s.ByNode))
            fmt.Fprintf(out, "    Top workloads: %s\n", top3(s.ByWorkload))
            if hint := dropclass.RemediationHint(s.Reason); hint != "" {
                fmt.Fprintf(out, "    Hint: %s\n", hint)
            }
            fmt.Fprintln(out)
        }
        pathLine := healthPath
        if dryRun {
            pathLine += " (dry-run, not written)"
        }
        fmt.Fprintf(out, "cluster-health.json: %s\n", pathLine)
        fmt.Fprintln(out, frame)
    }

    // top3 formats up to the top-3 contributors from a name->count map.
    // Format: "name-a (32), name-b (12), name-c (3) (+N more)"
    func top3(m map[string]uint64) string { ... }
    ```

    top3 implementation:
    - Build []struct{name string; n uint64} from map, sort descending by n.
    - Take up to first 3, format as "name (N)".
    - If len > 3 append " (+M more)" where M = len-3.
    - If empty return "(none)".

    Step C — Add Stdout to PipelineConfig and wire call in pipeline.go:

    In PipelineConfig add:
    ```go
    // Stdout is the writer for human-readable output (session summary block).
    // Nil defaults to os.Stdout. Use bytes.Buffer in tests.
    Stdout io.Writer
    ```

    In RunPipelineWithSource, after `hw.finalize(stats)` and before `stats.Log(cfg.Logger)`:
    ```go
    // HEALTH-03: print cluster-health summary block to stdout.
    stdout := cfg.Stdout
    if stdout == nil {
        stdout = os.Stdout
    }
    healthPath := filepath.Join(cfg.EvidenceDir, cfg.OutputHash, "cluster-health.json")
    PrintClusterHealthSummary(stdout, hw.Snapshot(), stats, healthPath, cfg.DryRun)
    ```

    Note: cmd/cpg/generate.go and cmd/cpg/replay.go do NOT need to change — they already
    leave Stdout at nil which defaults to os.Stdout. No changes to cmd/ layer needed.

    Run tests: `go test ./pkg/hubble/... -run TestPrintClusterHealthSummary -race -v`
    All must pass.
    Run full suite: `go test ./... -race` — must stay green.
    Commit: `feat(12-01): implement PrintClusterHealthSummary + healthWriter.Snapshot()`
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/... -run TestPrintClusterHealthSummary -race -v 2>&1 | tail -20 && go test ./... -race 2>&1 | tail -10</automated>
  </verify>
  <done>
    All TestPrintClusterHealthSummary tests pass with -race.
    Full suite passes (no regressions).
    summary.go exists with PrintClusterHealthSummary exported.
    health_writer.go has Snapshot() method.
    pipeline.go calls PrintClusterHealthSummary after hw.finalize.
  </done>
</task>

</tasks>

<verification>
```bash
# 1. All new tests pass
cd /home/gule/Workspace/team-infrastructure/cpg
go test ./pkg/hubble/... -run TestPrintClusterHealthSummary -race -v

# 2. Full suite green
go test ./... -race

# 3. Snapshot() exists
grep -n "func (hw \*healthWriter) Snapshot" pkg/hubble/health_writer.go

# 4. PrintClusterHealthSummary called from pipeline
grep -n "PrintClusterHealthSummary" pkg/hubble/pipeline.go

# 5. Frame chars present (no ANSI)
grep -n "━" pkg/hubble/summary.go

# 6. Output to Stdout field (not zap logger)
grep -n "fmt.Fprint" pkg/hubble/summary.go
```
</verification>

<success_criteria>
- pkg/hubble/summary.go exists; PrintClusterHealthSummary formats and prints the block
- healthWriter.Snapshot() returns []HealthDropSnapshot; nil-safe
- PipelineConfig.Stdout io.Writer added (nil -> os.Stdout)
- pipeline.go calls PrintClusterHealthSummary after hw.finalize
- All TestPrintClusterHealthSummary tests pass with -race
- Full test suite passes with -race (no regressions)
- Output uses ━ frames, no ANSI codes, no emojis
- Dry-run appends "(dry-run, not written)" to path line
- Zero infra drops -> no output
- Infra class printed before transient class
</success_criteria>

<output>
After completion, create `.planning/phases/12-session-summary-block/12-01-session-summary-block-SUMMARY.md`
following the summary template at `@$HOME/.claude/get-shit-done/templates/summary.md`.
</output>
