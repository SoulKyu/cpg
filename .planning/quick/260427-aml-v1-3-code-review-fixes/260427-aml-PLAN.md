---
phase: quick-260427-aml-v1-3-code-review-fixes
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - pkg/dropclass/classifier.go
  - pkg/dropclass/hints.go
  - pkg/dropclass/version_test.go
  - pkg/dropclass/classifier_test.go
  - pkg/dropclass/hints_test.go
  - pkg/hubble/aggregator.go
  - pkg/hubble/aggregator_test.go
  - pkg/hubble/health_writer.go
  - pkg/hubble/health_writer_test.go
  - pkg/hubble/summary.go
  - pkg/hubble/summary_test.go
  - pkg/hubble/pipeline.go
  - pkg/hubble/pipeline_test.go
  - cmd/cpg/commonflags.go
  - cmd/cpg/commonflags_test.go
  - cmd/cpg/generate.go
  - cmd/cpg/replay.go
  - cmd/cpg/generate_test.go
  - README.md
autonomous: true
requirements:
  - QFIX-C1
  - QFIX-C2
  - QFIX-C3
  - QFIX-I1
  - QFIX-I2
  - QFIX-I3
  - QFIX-I4
  - QFIX-I5
  - QFIX-I7
  - QFIX-I8
  - QFIX-M1
  - QFIX-M2
  - QFIX-M3
  - QFIX-M4
  - QFIX-M5
  - QFIX-M6
  - QFIX-M7

must_haves:
  truths:
    - "Aggregator never blocks on healthCh send; drops counted in healthChDrops"
    - "Session summary block prints when --no-evidence is set and infra drops > 0"
    - "Health-path line under --dry-run --no-evidence does not mislead operator"
    - "Flag validation runs in PreRunE (no port-forward attempted on bad flags)"
    - "Unknown drop reason error message lists ≤5 fuzzy-matched suggestions, not all 76"
    - "DropClass.String() is the single source of truth (no duplicates in health_writer/commonflags)"
    - "Snapshot() is idempotent and safe to call after errgroup g.Wait()"
    - "Top-N tie boundary in summary shows all tied entries, no hidden +N more"
    - "Generic-URL hints render as empty (no Hint: line, no remediation field)"
    - "README CI example uses timeout --preserve-status to preserve cpg exit code"
    - "policyTargetEndpoint helper deduplicates INGRESS/EGRESS direction switch"
    - "ClassifierVersion drift guard test fails loudly on Cilium version bump"
    - "Adaptive summary width handles long DropReason names without truncation"
    - "summary_test fixture uses real Transient-class reason (STALE_OR_UNROUTABLE_IP)"
    - "Stage 1b channel close ordering is explicit (no surprise LIFO defer semantics)"
  artifacts:
    - path: "pkg/dropclass/classifier.go"
      provides: "DropClass.String() method, godoc on SetWarnLogger, defensive lookup pattern"
    - path: "pkg/dropclass/version_test.go"
      provides: "ClassifierVersion drift guard against go.mod cilium version"
    - path: "pkg/hubble/aggregator.go"
      provides: "non-blocking healthCh send + healthChDrops counter, policyTargetEndpoint helper, defensive map lookup"
    - path: "pkg/hubble/health_writer.go"
      provides: "Snapshot() finalized atomic.Bool gate, dropClassString removed"
    - path: "pkg/hubble/summary.go"
      provides: "topN with tie inclusion, adaptive summaryWidth, omit empty hint"
    - path: "pkg/hubble/pipeline.go"
      provides: "fallback snapshot when hw==nil, dry-run+no-evidence path messaging, explicit Stage 1b close ordering"
    - path: "cmd/cpg/commonflags.go"
      provides: "validateCommonFlags wrapper, Levenshtein top-5 in drop-reason error, dropClassLabel removed"
    - path: "cmd/cpg/generate.go"
      provides: "PreRunE: validateCommonFlags wired"
    - path: "cmd/cpg/replay.go"
      provides: "PreRunE: validateCommonFlags wired"
    - path: "README.md"
      provides: "timeout --preserve-status fix in CI cron example"
  key_links:
    - from: "pkg/hubble/aggregator.go (Run loop)"
      to: "healthCh"
      via: "non-blocking select with default + counter increment"
      pattern: "case healthCh <- ev:.*default:"
    - from: "pkg/hubble/pipeline.go (PrintClusterHealthSummary call site)"
      to: "agg.InfraDrops()"
      via: "fallback snapshot when hw==nil"
      pattern: "hw == nil.*InfraDrops|fallback.*Snapshot"
    - from: "cmd/cpg/{generate,replay}.go"
      to: "validateCommonFlags"
      via: "cobra PreRunE field"
      pattern: "PreRunE: validateCommonFlags"
    - from: "cmd/cpg/commonflags.go (validateIgnoreDropReasons)"
      to: "Levenshtein-based suggestions"
      via: "top-5 closest matches builder"
      pattern: "did you mean.*\\?"
    - from: "pkg/dropclass/version_test.go"
      to: "go.mod cilium version"
      via: "regex extraction + suffix assertion"
      pattern: "cilium v.*ClassifierVersion"
---

<objective>
v1.3 post-ship hardening: 16 atomic fixes from the superpowers code review (3 critical, 7 important, 6 minor).

Purpose: close real bugs (blocking healthCh send, missing summary on --no-evidence, late flag validation), reduce ergonomic friction (Levenshtein suggestions, adaptive summary width, hint dedup), and refactor (DropClass.String dedup, policyTargetEndpoint helper, version drift guard).

Output: 7 atomic commits across pkg/dropclass, pkg/hubble, cmd/cpg, README. All fixes TDD-first where production logic changes.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@.planning/PROJECT.md
@.planning/milestones/v1.3-MILESTONE-AUDIT.md

# Production code touched by every task — executor must not re-explore.
@pkg/dropclass/classifier.go
@pkg/dropclass/hints.go
@pkg/dropclass/version.go
@pkg/hubble/aggregator.go
@pkg/hubble/health_writer.go
@pkg/hubble/summary.go
@pkg/hubble/pipeline.go
@cmd/cpg/commonflags.go
@cmd/cpg/generate.go
@cmd/cpg/replay.go
@README.md

<interfaces>
Key contracts the executor must respect (extracted from code, no exploration needed):

From pkg/dropclass/classifier.go:
```go
type DropClass int
const (
    DropClassUnknown DropClass = iota
    DropClassPolicy
    DropClassInfra
    DropClassTransient
    DropClassNoise
)
func Classify(reason flowpb.DropReason) DropClass
func SetWarnLogger(l *zap.Logger)
func ValidReasonNames() []string
const ClassifierVersion = "1.0.0-cilium1.19.1"
```

From pkg/dropclass/hints.go:
```go
func RemediationHint(reason flowpb.DropReason) string  // "" when not in map
```

From pkg/hubble/aggregator.go:
```go
type DropEvent struct { Reason flowpb.DropReason; Class dropclass.DropClass; Namespace, Workload, NodeName string }
func (a *Aggregator) Run(ctx context.Context, in <-chan *flowpb.Flow, out chan<- policy.PolicyEvent, healthCh chan<- DropEvent) error
func (a *Aggregator) InfraDrops() map[flowpb.DropReason]uint64
func (a *Aggregator) InfraDropTotal() uint64
func effectiveEndpoint(f *flowpb.Flow) *flowpb.Endpoint  // INGRESS->Dest, EGRESS->Source — TARGET FOR M3 EXTRACTION
```

From pkg/hubble/health_writer.go:
```go
func (hw *healthWriter) Snapshot() []HealthDropSnapshot  // returns nil when hw==nil
type HealthDropSnapshot struct { Reason flowpb.DropReason; Class dropclass.DropClass; Count uint64; ByNode, ByWorkload map[string]uint64 }
func dropClassString(c dropclass.DropClass) string  // TO BE REMOVED in I5
```

From pkg/hubble/summary.go:
```go
func PrintClusterHealthSummary(out io.Writer, snapshots []HealthDropSnapshot, stats *SessionStats, healthPath string, dryRun bool)
const summaryWidth = 52  // M5 makes this dynamic
```

From cmd/cpg/commonflags.go:
```go
func validateIgnoreProtocols(in []string) ([]string, error)
func validateIgnoreDropReasons(in []string, logger *zap.Logger) ([]string, error)
func dropClassLabel(c dropclass.DropClass) string  // TO BE REMOVED in I5
```

Cobra PreRunE signature (Go style):
```go
func(cmd *cobra.Command, args []string) error
```

flowpb maps available:
```go
flowpb.DropReason_name  map[int32]string
flowpb.DropReason_value map[string]int32
```
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Critical pipeline robustness — C1 (non-blocking healthCh) + C2 (fallback snapshot under --no-evidence)</name>
  <files>pkg/hubble/aggregator.go, pkg/hubble/aggregator_test.go, pkg/hubble/pipeline.go, pkg/hubble/pipeline_test.go</files>
  <behavior>
    Aggregator (C1):
    - Test: with size-1 buffered healthCh and NO consumer, sending N=10 DropEvents causes 9 drops counted in healthChDrops; first send succeeds (channel buffer absorbs it); Run() never blocks (use a 200ms timeout in test, must complete well under that).
    - Test: with cancelled ctx mid-run, healthCh send select chooses ctx.Done() branch and Run returns ctx.Err() (or nil per pre-existing semantics — match current contract: ctx.Done case currently returns nil after final flush; for the healthCh select use ctx.Done() to allow exit, return nil to keep contract identical).
    - Test: HealthChDrops() exposes uint64 counter, zero when no drops occurred.

    Pipeline (C2):
    - Test: PipelineConfig with EvidenceEnabled=false, inject infra drops via a mock source (or directly populate Aggregator and call PrintClusterHealthSummary path). Assert the cluster-health summary block IS printed to PipelineConfig.Stdout (use bytes.Buffer) with the per-reason counts visible. Top nodes/workloads should print "(none)" since hw==nil never accumulated them.
  </behavior>
  <action>
    C1 — pkg/hubble/aggregator.go:
    1. Add field to Aggregator struct: `healthChDrops uint64` (after infraDrops).
    2. Add accessor: `func (a *Aggregator) HealthChDrops() uint64 { return a.healthChDrops }`.
    3. In Run() loop ~line 399, replace `healthCh <- buildDropEvent(f, class)` with:
       ```go
       if healthCh != nil {
           ev := buildDropEvent(f, class)
           select {
           case healthCh <- ev:
           case <-ctx.Done():
               // Drain remaining buckets before returning to honor existing
               // ctx.Done() flush contract above.
               a.flush(buckets, out)
               return nil
           default:
               a.healthChDrops++
           }
       }
       ```
       Note: keep the `if healthCh != nil` guard; preserves nil-safe behavior.

    C2 — pkg/hubble/pipeline.go ~lines 309-315:
    1. Refactor the summary call site so summary prints even when hw==nil:
       ```go
       var snapshots []HealthDropSnapshot
       if hw != nil {
           snapshots = hw.Snapshot()
       } else if stats.InfraDropTotal > 0 {
           // Build minimal snapshot from aggregator counters; no node/workload attribution.
           snapshots = make([]HealthDropSnapshot, 0, len(stats.InfraDropsByReason))
           for reason, count := range stats.InfraDropsByReason {
               snapshots = append(snapshots, HealthDropSnapshot{
                   Reason: reason,
                   Class:  dropclass.Classify(reason),
                   Count:  count,
                   ByNode:     map[string]uint64{},
                   ByWorkload: map[string]uint64{},
               })
           }
       }
       PrintClusterHealthSummary(stdout, snapshots, stats, healthPath, cfg.DryRun)
       ```
    2. Add `"github.com/SoulKyu/cpg/pkg/dropclass"` import if not already present (it already is via aggregator package — verify; if pipeline.go doesn't import it directly, add).

    Use existing TDD pattern from v1.3 phases: RED commit (failing test), GREEN commit (impl). For this task ONE atomic commit is acceptable since both fixes are tightly coupled to pipeline robustness and should land together.
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test -race -timeout 60s ./pkg/hubble/... -run 'TestAggregator|TestPipeline|TestRunPipeline' -v</automated>
  </verify>
  <done>healthChDrops counter populated under back-pressure; summary block prints under --no-evidence with infra drops > 0; full hubble package test suite passes with -race.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: C3 — clearer dry-run + --no-evidence summary suffix</name>
  <files>pkg/hubble/summary.go, pkg/hubble/summary_test.go, pkg/hubble/pipeline.go</files>
  <behavior>
    - Test: PrintClusterHealthSummary called with dryRun=true AND a marker indicating evidence is disabled — output line MUST NOT claim cpg "would write" to a path it never agreed to manage. Either omit the path line OR contain "(evidence disabled".
    - Test: dryRun=true, evidence enabled (path is real intent) — preserve existing "(dry-run, not written)" suffix.
    - Test: dryRun=false, evidence enabled — bare path printed, no suffix.
  </behavior>
  <action>
    Decision: thread evidenceDisabled into PrintClusterHealthSummary as a 3rd state. Cleanest API: change signature to accept an enum or a second bool. Use a small enum to keep call site readable.

    pkg/hubble/summary.go:
    1. Replace bool dryRun parameter with new typed enum:
       ```go
       type SummaryPathState int
       const (
           SummaryPathWritten        SummaryPathState = iota // path is real, file written
           SummaryPathDryRun                                  // dry-run, would have written
           SummaryPathEvidenceOff                             // --no-evidence, file not written
       )
       func PrintClusterHealthSummary(out io.Writer, snapshots []HealthDropSnapshot, stats *SessionStats, healthPath string, state SummaryPathState)
       ```
    2. In the path line:
       - SummaryPathWritten: `cluster-health.json: <path>`
       - SummaryPathDryRun: `cluster-health.json: <path> (dry-run, not written)`
       - SummaryPathEvidenceOff: omit the path line entirely (operator never agreed to a managed file); print `cluster-health.json: (evidence disabled — file not written)` instead — keeps the section anchor for users grep-ing logs.

    pkg/hubble/pipeline.go:
    Update the single call site to compute state:
    ```go
    state := SummaryPathWritten
    switch {
    case hw == nil && cfg.EvidenceEnabled == false:
        state = SummaryPathEvidenceOff
    case cfg.DryRun:
        state = SummaryPathDryRun
    }
    PrintClusterHealthSummary(stdout, snapshots, stats, healthPath, state)
    ```

    Update existing summary tests to pass the new typed parameter (mechanical migration: dryRun=true → SummaryPathDryRun, dryRun=false → SummaryPathWritten).
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test -race -timeout 60s ./pkg/hubble/... -run 'TestPrintClusterHealthSummary' -v</automated>
  </verify>
  <done>Three summary path states render correctly; --no-evidence run never prints a misleading path; existing summary_test cases migrated to new enum signature; full hubble suite green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Validation hardening — I2 (PreRunE) + I3 (Levenshtein top-5) + I7 (defensive ,ok pattern)</name>
  <files>cmd/cpg/commonflags.go, cmd/cpg/commonflags_test.go, cmd/cpg/generate.go, cmd/cpg/replay.go, cmd/cpg/generate_test.go, pkg/hubble/aggregator.go</files>
  <behavior>
    I2 (PreRunE):
    - Test: build a *cobra.Command via newGenerateCmd(), execute with `--ignore-drop-reason TYPO`, assert err is non-nil AND no port-forward / pipeline construction was attempted (no kubeconfig load). Same for newReplayCmd().
    - Test: valid `--ignore-drop-reason POLICY_DENIED` passes PreRunE without error.

    I3 (Levenshtein):
    - Test: validateIgnoreDropReasons with input "CT_MAP_INSERT_FAIL" returns error containing "CT_MAP_INSERTION_FAILED" in the suggestions list.
    - Test: error message contains AT MOST 5 suggested reasons (split on comma, trim, count).
    - Test: error message length is bounded (assert < 500 chars).
    - Test: validateIgnoreProtocols with "tcpp" returns error listing all 5 valid protocols (no Levenshtein needed — small set).

    I7 (defensive lookup):
    - Test: aggregator processes a flow whose DropReasonDesc is a future enum value not in flowpb.DropReason_name; the `name, ok` lookup correctly returns ok=false; flow falls through to keyFromFlow without panic and without false-matching the empty-string key in ignoreDropReasons. (Synthesize via int32(99999) cast.)
  </behavior>
  <action>
    I2 — extract validateCommonFlags wrapper in commonflags.go:
    ```go
    // validateCommonFlags is the cobra PreRunE handler shared by generate and replay.
    // Runs BEFORE RunE so flag errors abort before kubeconfig load / port-forward.
    func validateCommonFlags(cmd *cobra.Command, _ []string) error {
        f := parseCommonFlags(cmd)
        if _, err := validateIgnoreProtocols(f.ignoreProtocols); err != nil {
            return err
        }
        // logger may be nil during PreRunE if main() hasn't initialized it yet;
        // pass it anyway — validateIgnoreDropReasons handles nil logger gracefully.
        if _, err := validateIgnoreDropReasons(f.ignoreDropReasons, logger); err != nil {
            return err
        }
        return nil
    }
    ```
    Wire on cobra commands:
    - generate.go: add `PreRunE: validateCommonFlags,` to the &cobra.Command{} struct.
    - replay.go: same.
    Remove the duplicate validateIgnoreProtocols / validateIgnoreDropReasons calls from runGenerate and runReplay (they re-validate harmlessly but produce the value — keep the *result-producing* calls in RunE since RunE needs the normalized slices; PreRunE is purely a fail-fast gate). Net: PreRunE rejects invalid input before any side effect; RunE re-runs validators only to obtain the normalized slices (calls are pure functions, fine to repeat).

    I3 — Levenshtein for drop reasons in commonflags.go:
    1. Add a small private helper (no new dependency — write a 30-line classic DP impl):
       ```go
       func levenshtein(a, b string) int {
           // Standard 2-row DP, ~30 lines.
       }
       func suggestClosest(input string, candidates []string, n int) []string {
           // Score all candidates by levenshtein(input, c), return top n by ascending distance.
           // Stable sort by distance then by name for determinism.
       }
       ```
    2. In validateIgnoreDropReasons, replace the error construction:
       ```go
       suggestions := suggestClosest(v, all, 5)
       return nil, fmt.Errorf(
           "unknown drop reason %q: did you mean any of: %s? See https://docs.cilium.io/en/stable/observability/hubble/#dropreason for the full list",
           raw, strings.Join(suggestions, ", "),
       )
       ```
    3. validateIgnoreProtocols stays as-is (5 valid values, full list is bounded; no Levenshtein needed).

    I7 — defensive `, ok` pattern:
    - pkg/hubble/aggregator.go ~line 368: change
      `name := flowpb.DropReason_name[int32(f.GetDropReasonDesc())]; if name != ""` to
      `if name, ok := flowpb.DropReason_name[int32(f.GetDropReasonDesc())]; ok && name != "" { ... }`.
    - Audit other lookups in aggregator.go and commonflags.go for the same pattern (the validateIgnoreDropReasons FILTER-03 lookup uses `flowpb.DropReason_value[v]` with `, exists` already — fine).
    - Audit health_writer.go sort.Slice: already uses direct map access for SORTING only; ok-check would not change behavior, leave unless trivial. (Skip — adding ok-check on a sort key returning empty string still yields correct ordering of unknown reasons last; not a correctness gap.)

    Three TDD-first commits OK, but a single atomic commit per fix-cluster is acceptable here since all three are flag/lookup defensive-code:
    - test(quick): add failing tests for PreRunE + Levenshtein + ,ok lookup
    - feat(quick): wire PreRunE, Levenshtein suggestions, defensive lookups
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test -race -timeout 60s ./cmd/cpg/... ./pkg/hubble/... -run 'TestValidate|TestPreRunE|TestLevenshtein|TestSuggestClosest|TestAggregator' -v</automated>
  </verify>
  <done>PreRunE rejects bad flags before any pipeline side effect; Levenshtein top-5 suggestions land; aggregator defensive lookup with `, ok`; no panics on unknown enum values; cmd/cpg + hubble suites green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 4: API hygiene — I1 (SetWarnLogger godoc) + I4 (Snapshot sync.Once) + I5 (DropClass.String dedup)</name>
  <files>pkg/dropclass/classifier.go, pkg/dropclass/classifier_test.go, pkg/hubble/health_writer.go, pkg/hubble/health_writer_test.go, cmd/cpg/commonflags.go, cmd/cpg/commonflags_test.go</files>
  <behavior>
    I4 (Snapshot finalized gate):
    - Test: call hw.Snapshot() concurrently from 8 goroutines after accumulating 100 events; all goroutines receive the SAME snapshot (deep-equal). First-call wins, subsequent calls return cached pointer/copy.
    - Test: nil-safe — Snapshot() on nil hw returns nil (existing behavior preserved).

    I5 (DropClass.String):
    - Test: dropclass_test.go covers `(DropClass).String()` for all 5 values returning "policy"/"infra"/"transient"/"noise"/"unknown".
    - Test: existing health_writer + commonflags tests still pass after callsite migration.

    I1 is godoc-only — no test required, but classifier_test.go must still pass.
  </behavior>
  <action>
    I1 — pkg/dropclass/classifier.go: replace the SetWarnLogger godoc with:
    ```
    // SetWarnLogger configures the package-global logger used to emit deduplicated
    // WARN messages for unrecognized DropReason values. Process-global state:
    // last writer wins. The dedup map (warnedUnknown) is also process-global.
    //
    // Intended for single-pipeline binaries (cpg generate / cpg replay).
    // Concurrent pipelines in the same process will race on the logger pointer
    // and share the dedup map — document as a constraint, not a bug. Pass nil
    // to disable warn logging.
    ```

    I4 — pkg/hubble/health_writer.go:
    1. Add field: `finalized atomic.Bool` and `cachedSnapshot []HealthDropSnapshot` on healthWriter struct.
    2. Add `import "sync/atomic"`.
    3. Update Snapshot():
       ```go
       func (hw *healthWriter) Snapshot() []HealthDropSnapshot {
           if hw == nil {
               return nil
           }
           if hw.finalized.Load() {
               return hw.cachedSnapshot
           }
           // First call: build snapshot, cache, mark finalized.
           result := make([]HealthDropSnapshot, 0, len(hw.drops))
           for _, e := range hw.drops {
               result = append(result, HealthDropSnapshot{
                   Reason: e.reason, Class: e.class, Count: e.count,
                   ByNode: shallowCopyMap(e.byNode),
                   ByWorkload: shallowCopyMap(e.byWorkload),
               })
           }
           hw.cachedSnapshot = result
           hw.finalized.Store(true)
           return result
       }
       ```
       Note: this is technically NOT race-safe for the *first* call against concurrent accumulate(), but the godoc explicitly forbids that:
    4. Update godoc above Snapshot:
       ```
       // Snapshot returns the accumulated drop entries. MUST be called only after
       // the consumer goroutine that calls accumulate() has exited (i.e. after
       // errgroup g.Wait() returns). The first call captures the snapshot; all
       // subsequent calls return the cached result, even from concurrent goroutines.
       // Returns nil when hw is nil (dry-run / evidence disabled).
       ```

    I5 — DropClass.String() consolidation:
    1. pkg/dropclass/classifier.go — add method:
       ```go
       // String returns the lowercase label for a DropClass value: "policy",
       // "infra", "transient", "noise", or "unknown" (also for any unrecognized
       // numeric value).
       func (c DropClass) String() string {
           switch c {
           case DropClassPolicy:    return "policy"
           case DropClassInfra:     return "infra"
           case DropClassTransient: return "transient"
           case DropClassNoise:     return "noise"
           default:                 return "unknown"
           }
       }
       ```
    2. pkg/hubble/health_writer.go — DELETE `dropClassString` function; replace callsite (line ~107):
       `Class: dropClassString(e.class)` → `Class: e.class.String()`.
       Also pkg/hubble/summary.go line 40: `class := dropClassString(s.Class)` → `class := s.Class.String()`.
    3. cmd/cpg/commonflags.go — DELETE `dropClassLabel` function; replace callsite line 145:
       `zap.String("class", dropClassLabel(class))` → `zap.String("class", class.String())`.

    Order: do I1 + I5 in one commit (classifier.go changes), then I4 in a separate commit (health_writer.go changes touching Snapshot semantics). Two commits, both TDD-first.
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test -race -timeout 60s ./pkg/dropclass/... ./pkg/hubble/... ./cmd/cpg/... -v</automated>
  </verify>
  <done>SetWarnLogger godoc clarifies process-global semantics; Snapshot() idempotent + cached; DropClass.String() is single source of truth; duplicate helpers removed; full suite green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 5: Summary polish — I8 (tie boundary) + M5 (adaptive width) + M6 (real Transient fixture)</name>
  <files>pkg/hubble/summary.go, pkg/hubble/summary_test.go</files>
  <behavior>
    I8:
    - Test: top3 with input `{a:10, b:5, c:5, d:5}` returns ALL 4 entries formatted "a (10), b (5), c (5), d (5)" with NO "+N more" suffix (nothing hidden).
    - Test: top3 with input `{a:10, b:9, c:8, d:1}` returns first 3 + "(+1 more)" — strict top-3, no tie at boundary.
    - Test: top3 with `{a:10, b:10, c:10, d:10, e:10}` returns all 5 (all tied at top).

    M5:
    - Test: PrintClusterHealthSummary with snapshot containing reason `NO_CONFIGURATION_AVAILABLE_TO_PERFORM_POLICY_DECISION` (52 chars) — assert the printed line contains the FULL reason name (no truncation, no trailing "...") and frame width adapts.
    - Test: with short reason names only (e.g., `POLICY_DENY`), frame width does NOT exceed the original 52 (no needless widening).

    M6:
    - Test: existing summary_test cases that asserted DropClassTransient now use `STALE_OR_UNROUTABLE_IP` (a real Transient per dropReasonClass map). Verify dropclass.Classify(flowpb.DropReason_STALE_OR_UNROUTABLE_IP) == DropClassTransient in the test setup (this catches future map drift too).
  </behavior>
  <action>
    I8 — pkg/hubble/summary.go top3:
    Rename to topN (still a private helper) or keep top3 but extend logic. Cleanest: keep name, change semantics to "top 3 + all ties at the boundary":
    ```go
    func top3(m map[string]uint64) string {
        if len(m) == 0 { return "(none)" }
        // ... existing kv slice + sort ...
        limit := 3
        if len(items) < limit { limit = len(items) }
        // Extend limit through all ties at the boundary.
        boundaryCount := items[limit-1].n
        for limit < len(items) && items[limit].n == boundaryCount {
            limit++
        }
        parts := make([]string, limit)
        for i := 0; i < limit; i++ {
            parts[i] = fmt.Sprintf("%s (%d)", items[i].name, items[i].n)
        }
        result := strings.Join(parts, ", ")
        if extra := len(items) - limit; extra > 0 {
            result += fmt.Sprintf(" (+%d more)", extra)
        }
        return result
    }
    ```

    M5 — adaptive summaryWidth:
    1. Remove the `const summaryWidth = 52`.
    2. In PrintClusterHealthSummary, compute dynamically AFTER sorting snapshots:
       ```go
       const (
           minReasonNameWidth = 38  // historical baseline
           maxReasonNameWidth = 60  // safety cap
       )
       reasonW := minReasonNameWidth
       for _, s := range snapshots {
           name := flowpb.DropReason_name[int32(s.Reason)]
           if l := len(name); l > reasonW {
               reasonW = l
           }
       }
       if reasonW > maxReasonNameWidth { reasonW = maxReasonNameWidth }
       // Frame width = reasonW + space + "[label]" (max 11) + 2 spaces + count column (~14) ≈ reasonW + 28
       frameWidth := reasonW + 28
       frame := strings.Repeat("━", frameWidth)
       // ... and use fmt.Fprintf(out, "  %-*s [%s]  %d flows\n", reasonW, name, class, s.Count) instead of hardcoded %-38s
       ```

    M6 — pkg/hubble/summary_test.go lines ~53, 175, 196:
    - Replace `flowpb.DropReason_POLICY_DENIED` (which is DropClassPolicy, NOT Transient) with `flowpb.DropReason_STALE_OR_UNROUTABLE_IP` everywhere the fixture sets Class=DropClassTransient.
    - Add a setup-time assertion: `require.Equal(t, dropclass.DropClassTransient, dropclass.Classify(flowpb.DropReason_STALE_OR_UNROUTABLE_IP))` to catch future taxonomy drift.

    Single TDD-cycle commit acceptable — all three are summary.go correctness/polish.
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test -race -timeout 60s ./pkg/hubble/... -run 'TestPrintClusterHealthSummary|TestTop3|TestTopN' -v</automated>
  </verify>
  <done>Tie boundary inclusion verified; long DropReason names render without truncation; test fixture uses correct-class reason; summary suite green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 6: Hints + README — M1 (empty hint for non-deep-link) + M2 (timeout --preserve-status)</name>
  <files>pkg/dropclass/hints.go, pkg/dropclass/hints_test.go, pkg/hubble/health_writer.go, pkg/hubble/summary.go, README.md</files>
  <behavior>
    M1:
    - Test: RemediationHint(CT_MAP_INSERTION_FAILED) returns the deep-link URL (contains "#handling-drop-ct-map-insertion-failed"), unchanged.
    - Test: RemediationHint(CT_NO_MAP_FOUND) — currently returns the generic page URL — now returns "" (no deep link justifies surfacing).
    - Test: hints_test.go iterates ALL entries in dropReasonHint; for any entry whose value lacks a "#" anchor, value MUST be "" (allowlist enforcement).
    - Test: cluster-health.json schema — when a drop entry has empty Remediation, the JSON omits the field. (If healthDropJSON.Remediation lacks `,omitempty` tag, this test forces adding it.)
    - Test: PrintClusterHealthSummary with a snapshot whose RemediationHint returns "" — output does NOT contain "Hint:" line for that reason.

    M2 is documentation — no test required; verify via README diff.
  </behavior>
  <action>
    M1 — pkg/dropclass/hints.go:
    1. Audit dropReasonHint map entries. Any value lacking a "#" anchor (i.e. pointing at the bare troubleshooting page) → change value to "". Preserve the map keys (still classifies as Infra; just no actionable URL).
       Entries to flip to "" (those listing only the bare page):
       CT_NO_MAP_FOUND, CT_TRUNCATED_OR_INVALID_HEADER, CT_MISSING_TCP_ACK_FLAG, CT_UNKNOWN_L4_PROTOCOL, CT_CANNOT_CREATE_ENTRY_FROM_PACKET, UNKNOWN_CONNECTION_TRACKING_STATE, SOCKET_LOOKUP_FAILED, SOCKET_ASSIGN_FAILED, UNKNOWN_L3_TARGET_ADDRESS, LOCAL_HOST_IS_UNREACHABLE, INVALID_SOURCE_MAC, INVALID_DESTINATION_MAC, INVALID_SOURCE_IP, INVALID_PACKET_DROPPED, UNSUPPORTED_L3_PROTOCOL, INVALID_IPV6_EXTENSION_HEADER, IP_FRAGMENTATION_NOT_SUPPORTED, FIRST_LOGICAL_DATAGRAM_FRAGMENT_NOT_FOUND, ERROR_WHILE_CORRECTING_L3_CHECKSUM, ERROR_WHILE_CORRECTING_L4_CHECKSUM, UNKNOWN_L4_PROTOCOL, UNSUPPORTED_L2_PROTOCOL, UNKNOWN_ICMPV4_CODE, UNKNOWN_ICMPV4_TYPE, UNKNOWN_ICMPV6_CODE, UNKNOWN_ICMPV6_TYPE, FORBIDDEN_ICMPV6_MESSAGE, ERROR_RETRIEVING_TUNNEL_KEY, ERROR_RETRIEVING_TUNNEL_OPTIONS, INVALID_GENEVE_OPTION, NO_TUNNEL_OR_ENCAPSULATION_ENDPOINT, ENCAPSULATION_TRAFFIC_IS_PROHIBITED, UNSUPPORTED_PROTOCOL_FOR_DSR_ENCAP, MISSED_TAIL_CALL, ERROR_WRITING_TO_PACKET, INVALID_TC_BUFFER, FAILED_TO_INSERT_INTO_PROXYMAP, NO_MAPPING_FOR_NAT_MASQUERADE, UNSUPPORTED_PROTOCOL_FOR_NAT_MASQUERADE, SNAT_NO_MAP_FOUND, NAT46, NAT64, REACHED_EDT_RATE_LIMITING_DROP_HORIZON, DROP_RATE_LIMITED, VLAN_FILTERED, INVALID_VNI, NO_SID, MISSING_SRV6_STATE, PROXY_REDIRECTION_NOT_SUPPORTED_FOR_PROTOCOL, INVALID_CLUSTER_ID, NO_NODE_ID.
       Entries to KEEP (have deep links):
       CT_MAP_INSERTION_FAILED, SERVICE_BACKEND_NOT_FOUND, FIB_LOOKUP_FAILED, UNENCRYPTED_TRAFFIC, NO_EGRESS_GATEWAY, DROP_NO_EGRESS_IP.
    2. RemediationHint() unchanged — already returns map zero-value "" via direct map access.

    pkg/hubble/health_writer.go:
    Verify healthDropJSON.Remediation tag — change to `json:"remediation,omitempty"` if not already.

    pkg/hubble/summary.go:
    The existing `if hint := dropclass.RemediationHint(s.Reason); hint != ""` already guards the Hint line — verify nothing else needs change. (Already correct.)

    M2 — README.md (## Exit codes section, line ~607):
    Replace `timeout 300 cpg generate -n production --fail-on-infra-drops \` with `timeout --preserve-status 300 cpg generate -n production --fail-on-infra-drops \`.
    Add a one-line explanatory note immediately above the code block:
    > Note: `--preserve-status` ensures `timeout` propagates `cpg`'s exit code (0 vs 1) instead of returning 124 when the deadline is reached. Without it, a CI job that hits the timeout would mask whether infra drops were detected.
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test -race -timeout 60s ./pkg/dropclass/... ./pkg/hubble/... -v && grep -q 'timeout --preserve-status 300 cpg generate' README.md</automated>
  </verify>
  <done>Generic-URL hint entries return ""; cluster-health.json omits empty remediation field; summary skips Hint line; README CI example uses --preserve-status with rationale; all suites green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 7: Refactor + drift guard — M3 (policyTargetEndpoint helper) + M4 (ClassifierVersion drift test) + M7 (explicit Stage 1b close ordering)</name>
  <files>pkg/hubble/aggregator.go, pkg/hubble/aggregator_test.go, pkg/hubble/pipeline.go, pkg/dropclass/version_test.go</files>
  <behavior>
    M3:
    - Test: existing aggregator_test cases still pass after extraction (no behavior change).
    - Test: a single new TestPolicyTargetEndpoint covers INGRESS→Destination, EGRESS→Source, UNKNOWN→nil, nil-Destination, nil-Source paths.

    M4:
    - Test (NEW pkg/dropclass/version_test.go): reads go.mod from repo root (relative path `../../go.mod`), greps for `github.com/cilium/cilium v` line, extracts version (e.g. "v1.19.1"), strips the leading "v", expects ClassifierVersion to end with "-cilium" + that version. Failure message MUST direct dev to bump ClassifierVersion AND audit pkg/dropclass for any new DropReason enum values.

    M7:
    - No new test (channel close ordering is internal to pipeline.go and exercised by existing pipeline_test); manual code review verifies explicit close after loop body, no defer.
  </behavior>
  <action>
    M3 — pkg/hubble/aggregator.go:
    1. Add helper at file scope (replace effectiveEndpoint OR have effectiveEndpoint delegate to it — pick: rename effectiveEndpoint to policyTargetEndpoint since it's the more accurate name and the function does exactly that):
       ```go
       // policyTargetEndpoint returns the endpoint that policy decisions target
       // for a given flow direction: INGRESS targets the destination,
       // EGRESS targets the source. Returns nil for unknown or unset directions.
       func policyTargetEndpoint(f *flowpb.Flow) *flowpb.Endpoint {
           switch f.GetTrafficDirection() {
           case flowpb.TrafficDirection_INGRESS:
               return f.GetDestination()
           case flowpb.TrafficDirection_EGRESS:
               return f.GetSource()
           default:
               return nil
           }
       }
       ```
    2. Delete the existing effectiveEndpoint function (lines ~244-253). Update buildDropEvent (line ~221) to call policyTargetEndpoint.
    3. Replace the duplicate switch in keyFromFlow (lines ~429-440):
       ```go
       ep := policyTargetEndpoint(f)
       if ep == nil {
           // Need to distinguish "unknown direction" from "nil endpoint" for
           // tracker reason classification — preserve original semantics:
           if f.TrafficDirection != flowpb.TrafficDirection_INGRESS && f.TrafficDirection != flowpb.TrafficDirection_EGRESS {
               a.tracker.Track(f, policy.ReasonUnknownDir)
           } else {
               a.tracker.Track(f, policy.ReasonNilEndpoint)
           }
           return AggKey{}, true
       }
       ```
       Note: the original code separates "unknown direction" tracker reason from "nil endpoint" reason. Preserve this — the helper returns nil for both cases, so the call site re-checks direction. Slightly less clean but semantics-preserving.

    M4 — NEW pkg/dropclass/version_test.go:
    ```go
    package dropclass

    import (
        "os"
        "regexp"
        "strings"
        "testing"
    )

    // TestClassifierVersionMatchesGoMod is a drift guard: when go.mod bumps
    // github.com/cilium/cilium to a new version, this test fails until
    // ClassifierVersion is also bumped. The author MUST audit pkg/dropclass
    // for new DropReason enum values before bumping the version suffix.
    func TestClassifierVersionMatchesGoMod(t *testing.T) {
        // go.mod path relative to this test file (pkg/dropclass/) is ../../go.mod.
        data, err := os.ReadFile("../../go.mod")
        if err != nil {
            t.Fatalf("reading go.mod: %v", err)
        }
        re := regexp.MustCompile(`(?m)^\s*github\.com/cilium/cilium\s+v(\S+)\s*$`)
        m := re.FindStringSubmatch(string(data))
        if len(m) != 2 {
            t.Fatalf("could not find github.com/cilium/cilium version in go.mod")
        }
        ciliumVer := m[1] // e.g. "1.19.1"
        wantSuffix := "-cilium" + ciliumVer
        if !strings.HasSuffix(ClassifierVersion, wantSuffix) {
            t.Fatalf(
                "ClassifierVersion drift: %q does not end with %q.\n"+
                    "go.mod has cilium v%s. Bump ClassifierVersion AND audit pkg/dropclass for new DropReason enum values.",
                ClassifierVersion, wantSuffix, ciliumVer,
            )
        }
    }
    ```

    M7 — pkg/hubble/pipeline.go ~lines 214-223:
    Replace the defer-stack with explicit closes after the loop:
    ```go
    g.Go(func() error {
        // Close ordering matters: policyCh and evidenceCh must close before
        // healthCh because Stage 2 / 2b consumers exit when their channels
        // close, and we want them down before the health drainer (Stage 2c)
        // unblocks. Explicit closes (not defer) make this LIFO contract
        // visible and resilient to future code edits.
        for pe := range policies {
            policyCh <- pe
            evidenceCh <- pe
        }
        close(policyCh)
        close(evidenceCh)
        close(healthCh)
        return nil
    })
    ```

    Single atomic commit acceptable — three independent surface-level fixes; landing together keeps git log clean.
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test -race -timeout 60s ./... && go build ./...</automated>
  </verify>
  <done>policyTargetEndpoint helper deduplicates direction switch; TestClassifierVersionMatchesGoMod fails loudly on cilium version bump; Stage 1b closes are explicit with rationale; full repo build + test suite green with -race.</done>
</task>

</tasks>

<verification>
- All 7 tasks committed atomically (RED + GREEN per task where TDD applies).
- `go test -race -timeout 120s ./...` green from repo root.
- `go build ./...` clean.
- `go vet ./...` clean.
- README.md renders the corrected `timeout --preserve-status 300 cpg generate ...` snippet with rationale note.
- No regressions in 418 pre-existing tests; net test count grows by ≥10 (new tests for healthChDrops, fallback snapshot, PreRunE, Levenshtein, defensive lookup, Snapshot idempotency, DropClass.String, top-N tie boundary, adaptive width, version drift, policyTargetEndpoint).
</verification>

<success_criteria>
- 16/16 fixes from the superpowers code review applied (3 critical, 7 important, 6 minor).
- Test count net positive (≥428 tests).
- 7 atomic commits, each with conventional commit prefix:
  - fix(quick): non-blocking healthCh send + fallback snapshot under --no-evidence (C1+C2)
  - fix(quick): clarify summary path under --dry-run --no-evidence (C3)
  - feat(quick): PreRunE flag validation + Levenshtein top-5 suggestions + defensive map lookups (I2+I3+I7)
  - refactor(quick): DropClass.String() + Snapshot finalized gate + SetWarnLogger godoc (I1+I4+I5)
  - fix(quick): summary topN tie boundary + adaptive width + Transient fixture (I8+M5+M6)
  - fix(quick): omit generic-URL hints + README timeout --preserve-status (M1+M2)
  - refactor(quick): policyTargetEndpoint helper + ClassifierVersion drift guard + explicit Stage 1b close (M3+M4+M7)
- Full `go test -race ./...` green.
</success_criteria>

<output>
After completion, create `.planning/quick/260427-aml-v1-3-code-review-fixes/260427-aml-SUMMARY.md` summarizing:
- Fixes shipped (16/16 with one-line per fix referencing C/I/M ID)
- Test count delta
- Commit SHAs (one per task)
- Any deviations from the original review specs (with justification)
</output>
