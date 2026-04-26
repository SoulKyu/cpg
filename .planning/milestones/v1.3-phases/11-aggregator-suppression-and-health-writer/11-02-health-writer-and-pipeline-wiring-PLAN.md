---
phase: 11-aggregator-suppression-and-health-writer
plan: 02
type: tdd
wave: 2
depends_on: [11-01]
files_modified:
  - pkg/hubble/health_writer.go
  - pkg/hubble/health_writer_test.go
  - pkg/hubble/pipeline.go
  - cmd/cpg/generate.go
  - cmd/cpg/replay.go
autonomous: true
requirements: [HEALTH-02, HEALTH-04]

must_haves:
  truths:
    - "cluster-health.json is written atomically to $XDG_CACHE_HOME/cpg/evidence/<hash>/cluster-health.json after session end"
    - "cluster-health.json schema matches CONTEXT.md spec: schema_version, classifier_version, session block, drops array with reason/class/count/remediation/by_node/by_workload"
    - "Running with --dry-run leaves cluster-health.json unwritten (healthWriter is nil; channel drains without writing)"
    - "healthCh receives DropEvents from the aggregator via pipeline Stage 2c goroutine"
    - "finalize() is called after g.Wait() returns — no race with consumer goroutine"
    - "Zero infra drops observed → cluster-health.json is not written (no empty file)"
  artifacts:
    - path: "pkg/hubble/health_writer.go"
      provides: "healthWriter struct, accumulate(), finalize() atomic write"
      contains: "healthWriter"
    - path: "pkg/hubble/health_writer_test.go"
      provides: "TDD tests: schema validation + atomic write + dry-run + empty session"
      contains: "TestHealthWriter"
    - path: "pkg/hubble/pipeline.go"
      provides: "healthCh third channel, Stage 2c goroutine, PipelineConfig.HealthEnabled, post-Wait finalize"
      contains: "healthCh"
  key_links:
    - from: "pkg/hubble/pipeline.go"
      to: "pkg/hubble/health_writer.go hw.finalize()"
      via: "post g.Wait() block"
      pattern: "hw\\.finalize"
    - from: "pkg/hubble/pipeline.go Stage 2c"
      to: "healthCh"
      via: "goroutine draining healthCh into hw.accumulate()"
      pattern: "for.*healthCh"
    - from: "pkg/hubble/health_writer.go finalize()"
      to: "cluster-health.json"
      via: "os.CreateTemp + os.Rename (mirror evidence/writer.go)"
      pattern: "os\\.Rename"
---

<objective>
Implement healthWriter that accumulates DropEvents and writes cluster-health.json atomically; wire the third channel (healthCh) in pipeline.go; add dry-run gate; update generate.go and replay.go.

Purpose: Completes the HEALTH-02 and HEALTH-04 requirements — operators see infra drops in a structured file and --dry-run is respected.
Output: pkg/hubble/health_writer.go (new), pkg/hubble/health_writer_test.go (new), modified pipeline.go + generate.go + replay.go.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/phases/11-aggregator-suppression-and-health-writer/11-CONTEXT.md
@.planning/phases/11-aggregator-suppression-and-health-writer/11-01-aggregator-classification-gate-SUMMARY.md

<interfaces>
<!-- Key types and contracts the executor needs. Extracted from codebase. -->

From pkg/hubble/aggregator.go (after plan 11-01):
```go
// DropEvent — defined in aggregator.go, package hubble
type DropEvent struct {
    Reason    flowpb.DropReason
    Class     dropclass.DropClass
    Namespace string
    Workload  string   // labels.WorkloadName(ep.Labels); "_unknown" if nil
    NodeName  string   // f.GetNodeName(); "_unknown" if empty
}

func (a *Aggregator) InfraDropTotal() uint64
func (a *Aggregator) InfraDrops() map[flowpb.DropReason]uint64
// Run signature now: Run(ctx, in, out chan, healthCh chan<- DropEvent) error
// pipeline.go currently passes nil as healthCh — plan 11-02 replaces with real chan
```

From pkg/dropclass (phase 10):
```go
func RemediationHint(reason flowpb.DropReason) string  // "" for non-infra
const ClassifierVersion = "1.0.0-cilium1.19.1"
```

From pkg/evidence/writer.go (atomic write pattern to mirror exactly):
```go
tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
// write data
tmp.Close()
os.Rename(tmpPath, path)
```

From pkg/hubble/pipeline.go (current structure, after plan 11-01):
```go
type PipelineConfig struct {
    // ... existing fields ...
    DryRun bool
    EvidenceDir  string
    OutputHash   string
    // ... etc
}

type SessionStats struct {
    // ... existing fields including InfraDropTotal + InfraDropsByReason added in plan 11-01 ...
}

// Stage 1 currently: agg.Run(gctx, flows, policies, nil) — nil healthCh placeholder
// Stage 1b fan-out: closes policyCh + evidenceCh
// Post g.Wait(): stats.InfraDropTotal = agg.InfraDropTotal()
```

cluster-health.json target schema (from CONTEXT.md):
```json
{
  "schema_version": 1,
  "classifier_version": "1.0.0-cilium1.19.1",
  "session": {
    "started": "RFC3339",
    "ended": "RFC3339",
    "flows_seen": int,
    "infra_drops_total": int
  },
  "drops": [
    {
      "reason": "CT_MAP_INSERTION_FAILED",
      "class": "infra",
      "count": int,
      "remediation": "https://docs.cilium.io/...",
      "by_node": {"node-1": 3},
      "by_workload": {"prod/adserver": 2, "_unknown": 1}
    }
  ]
}
```
</interfaces>
</context>

<feature>
  <name>healthWriter + pipeline third-channel wiring</name>
  <files>pkg/hubble/health_writer.go, pkg/hubble/health_writer_test.go, pkg/hubble/pipeline.go, cmd/cpg/generate.go, cmd/cpg/replay.go</files>
  <behavior>
    healthWriter behavior:
    - accumulate(DropEvent): fold event into in-memory map keyed by reason; update by_node and by_workload counters
    - finalize(): if zero drops accumulated → no-op (no empty file); if hw == nil → no-op (dry-run path)
    - finalize() writes JSON atomically: os.CreateTemp → write → Close → os.Rename
    - Output path: filepath.Join(evidenceDir, outputHash, "cluster-health.json")
      (same dir as per-workload evidence files; uses same hash as EvidenceDir+OutputHash from PipelineConfig)
    - JSON structure: ClusterHealthReport struct with schema_version=1, classifier_version from ClassifierVersion constant, session block, drops array sorted by reason name for deterministic output

    Pipeline changes:
    - Add healthCh := make(chan DropEvent, 64) — same buffer size as policyCh/evidenceCh
    - Stage 1: agg.Run(gctx, flows, policies, healthCh) — replace nil with real channel
    - Stage 1b fan-out: add `defer close(healthCh)` alongside policyCh/evidenceCh closes
    - Stage 2c (new goroutine): drain healthCh → if hw != nil { hw.accumulate(pe) }
    - Post g.Wait(): hw.finalize(stats) — pass SessionStats so session block is populated
    - PipelineConfig gains no new fields for health (EvidenceDir + OutputHash already present; health file lives in same dir; health is always enabled when evidence is enabled and !DryRun)

    Dry-run gate:
    - `var hw *healthWriter` — nil when cfg.DryRun is true OR cfg.EvidenceEnabled is false
    - When hw == nil, Stage 2c goroutine still drains healthCh (prevents channel block) but discards
    - finalize() is called with hw pointer; if nil → no-op log: "would write cluster-health.json"

    generate.go / replay.go changes:
    - NO flag changes needed (EvidenceDir + OutputHash already plumbed)
    - Both already set cfg.DryRun — the nil-hw gate is fully inside RunPipelineWithSource
    - Only change needed: call dropclass.SetWarnLogger BEFORE pipeline (already done in NewAggregator in plan 11-01 — no generate.go change needed)

    Zero-drop behavior:
    - If no DropEvents accumulated in hw → finalize() is a no-op (no file written, no error)
    - Log line: "health writer: no infra/transient drops observed — skipping cluster-health.json"

    Sorting for determinism:
    - drops array sorted by reason name (flowpb.DropReason_name[int32(reason)]) for stable JSON output
    - by_node and by_workload maps are marshaled as-is (JSON object key order is non-deterministic but acceptable — not tested for exact order)
  </behavior>
  <implementation>
    Step 1 (RED): Write failing tests in health_writer_test.go. Tests import pkg/hubble directly.

    Step 2 (GREEN): Implement health_writer.go and patch pipeline.go.

    health_writer.go structure:
    ```go
    package hubble

    // healthDropEntry accumulates counters for a single drop reason.
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

    func newHealthWriter(evidenceDir, outputHash string, logger *zap.Logger, startedAt time.Time) *healthWriter

    func (hw *healthWriter) accumulate(e DropEvent)

    // finalize writes cluster-health.json atomically. No-op if zero drops.
    func (hw *healthWriter) finalize(stats *SessionStats) error
    ```

    JSON output structs (unexported, used only for marshaling):
    ```go
    type clusterHealthReport struct {
        SchemaVersion     int              `json:"schema_version"`
        ClassifierVersion string           `json:"classifier_version"`
        Session           healthSession    `json:"session"`
        Drops             []healthDropJSON `json:"drops"`
    }

    type healthSession struct {
        Started        time.Time `json:"started"`
        Ended          time.Time `json:"ended"`
        FlowsSeen      uint64    `json:"flows_seen"`
        InfraDropTotal uint64    `json:"infra_drops_total"`
    }

    type healthDropJSON struct {
        Reason      string            `json:"reason"`
        Class       string            `json:"class"`
        Count       uint64            `json:"count"`
        Remediation string            `json:"remediation"`
        ByNode      map[string]uint64 `json:"by_node"`
        ByWorkload  map[string]uint64 `json:"by_workload"`
    }
    ```

    finalize() output path:
    ```go
    path := filepath.Join(hw.evidenceDir, hw.outputHash, "cluster-health.json")
    // MkdirAll before CreateTemp (same pattern as evidence/writer.go)
    ```

    pipeline.go additions:
    ```go
    healthCh := make(chan DropEvent, 64)

    var hw *healthWriter
    if cfg.EvidenceEnabled && !cfg.DryRun {
        hw = newHealthWriter(cfg.EvidenceDir, cfg.OutputHash, cfg.Logger, stats.StartTime)
    }

    // Stage 1: replace nil with healthCh
    g.Go(func() error {
        return agg.Run(gctx, flows, policies, healthCh)
    })

    // Stage 1b: add close(healthCh)
    g.Go(func() error {
        defer close(policyCh)
        defer close(evidenceCh)
        defer close(healthCh)
        for pe := range policies {
            policyCh <- pe
            evidenceCh <- pe
        }
        return nil
    })

    // Stage 2c: drain healthCh
    g.Go(func() error {
        for e := range healthCh {
            if hw != nil {
                hw.accumulate(e)
            }
        }
        return nil
    })

    // Post g.Wait():
    if err := hw.finalize(stats); err != nil {
        cfg.Logger.Warn("health writer finalize failed", zap.Error(err))
    }
    ```

    Note: hw.finalize() must be nil-safe:
    ```go
    func (hw *healthWriter) finalize(stats *SessionStats) error {
        if hw == nil {
            return nil
        }
        // ...
    }
    ```

    generate.go / replay.go: NO changes required. EvidenceDir and OutputHash are already plumbed through PipelineConfig. The dry-run gate lives in RunPipelineWithSource.
  </implementation>
</feature>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Write failing tests (RED) — healthWriter schema + atomic write + dry-run</name>
  <files>pkg/hubble/health_writer_test.go</files>
  <read_first>
    - pkg/hubble/aggregator.go (DropEvent struct definition — may be added by plan 11-01)
    - pkg/evidence/writer.go (atomic write pattern reference)
    - pkg/dropclass/classifier.go (DropClass constants, ClassifierVersion)
  </read_first>
  <behavior>
    Tests to write (all must FAIL before implementation):
    - TestHealthWriterSchemaVersion: finalize writes schema_version=1
    - TestHealthWriterClassifierVersion: finalize embeds dropclass.ClassifierVersion
    - TestHealthWriterCounterAccumulation: 3 DropEvents with same CT_MAP_INSERTION_FAILED reason → drops[0].count=3
    - TestHealthWriterByNodeCounter: 2 events from "node-1", 1 from "node-2" → by_node={"node-1":2,"node-2":1}
    - TestHealthWriterByWorkloadCounter: events from different workloads → by_workload correct
    - TestHealthWriterAtomicWrite: finalize writes to correct path and file is valid JSON
    - TestHealthWriterNoWriteOnZeroDrops: accumulate nothing → finalize() returns nil, no file created
    - TestHealthWriterNilSafe: nil *healthWriter → finalize(stats) returns nil (no panic)
    - TestHealthWriterDryRun: hw=nil (dry-run simulation) → finalize is no-op, no file written
    - TestHealthWriterSessionBlock: session.flows_seen and infra_drops_total populated from stats
    - TestHealthWriterDropsSorted: multiple reasons → drops array sorted by reason name
  </behavior>
  <action>
    Create pkg/hubble/health_writer_test.go. Use a temp dir (t.TempDir()) for file path.

    For path computation in tests, replicate the production logic:
    ```go
    path := filepath.Join(tempDir, "testhash", "cluster-health.json")
    hw := newHealthWriter(tempDir, "testhash", zaptest.NewLogger(t), time.Now())
    ```

    Tests will fail to compile until health_writer.go is created.

    Run: `go test ./pkg/hubble/... -run "TestHealthWriter" 2>&1 | head -20` — confirm compile failure.
  </action>
  <verify>
    <automated>go test ./pkg/hubble/... 2>&1 | grep -E "undefined|cannot|FAIL" | head -10</automated>
  </verify>
  <done>Test file exists, tests fail to compile (undefined healthWriter). RED phase confirmed.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Implement healthWriter + pipeline wiring (GREEN)</name>
  <files>pkg/hubble/health_writer.go, pkg/hubble/pipeline.go</files>
  <read_first>
    - pkg/hubble/pipeline.go (full file — adding healthCh channel + Stage 2c + post-Wait finalize)
    - pkg/evidence/writer.go (exact atomic write pattern to mirror)
    - pkg/dropclass/hints.go (RemediationHint signature)
    - pkg/dropclass/version.go (ClassifierVersion constant)
  </read_first>
  <behavior>
    All RED tests from Task 1 must pass. Additional:
    - go test -race ./pkg/hubble/... passes (no race on healthCh/accumulate)
    - go test -race ./... passes (full suite)
    - go vet ./... passes
    - go build ./... passes
  </behavior>
  <action>
    1. Create pkg/hubble/health_writer.go with full implementation per feature spec above.

    2. Modify pkg/hubble/pipeline.go:
       - Add healthCh := make(chan DropEvent, 64)
       - Add var hw *healthWriter with dry-run gate (cfg.EvidenceEnabled && !cfg.DryRun)
       - Update Stage 1 agg.Run call: replace nil with healthCh
       - Update Stage 1b fan-out: add defer close(healthCh)
       - Add Stage 2c goroutine draining healthCh
       - Add hw.finalize(stats) call in post-g.Wait() block, before stats.Log()

    3. Dry-run log: when hw == nil (dry-run mode) AND infra drops observed, emit:
       ```go
       if cfg.DryRun && stats.InfraDropTotal > 0 {
           cfg.Logger.Info("dry-run: would write cluster-health.json",
               zap.Uint64("infra_drop_total", stats.InfraDropTotal),
               zap.String("path", filepath.Join(cfg.EvidenceDir, cfg.OutputHash, "cluster-health.json")),
           )
       }
       ```
       (Mirror policyWriter.dryRunEmit() pattern)

    IMPORTANT: generate.go and replay.go require NO changes — EvidenceDir, OutputHash, and DryRun are already plumbed. Verify by running `go build ./cmd/cpg/...`.
  </action>
  <verify>
    <automated>go test -race -count=1 ./pkg/hubble/... -run "TestHealthWriter" -v 2>&1 | tail -30</automated>
  </verify>
  <done>
    All TestHealthWriter* pass -race.
    cluster-health.json written to correct path with valid JSON matching schema.
    nil hw finalize is a no-op (no panic).
    Zero-drop case writes no file.
    go test -race ./... passes.
    go build ./... passes.
  </done>
</task>

</tasks>

<verification>
```bash
# Full test suite with race detector
go test -race -count=1 ./...

# Verify cluster-health.json path (evidence dir, not output dir — Pitfall 5)
go test -race ./pkg/hubble/... -run "TestHealthWriterAtomicWrite" -v

# Build sanity
go build ./...
go vet ./...
```

End-to-end smoke test (manual — optional, no fixture yet):
```bash
# Replay with a capture containing infra drops → check $XDG_CACHE_HOME/cpg/evidence/<hash>/cluster-health.json
# Dry-run → confirm no cluster-health.json written
```
</verification>

<success_criteria>
- cluster-health.json written to cfg.EvidenceDir/cfg.OutputHash/cluster-health.json (NOT in output-dir)
- schema_version=1, classifier_version="1.0.0-cilium1.19.1" in output
- CT_MAP_INSERTION_FAILED × 3 flows from node-1/prod/adserver → drops[0].count=3, by_node.node-1=3, by_workload.prod/adserver=3
- --dry-run (hw=nil) → no file written, no panic
- Zero infra drops → no file written
- go test -race ./... passes
- go build ./... passes
</success_criteria>

<output>
After completion, create `.planning/phases/11-aggregator-suppression-and-health-writer/11-02-health-writer-and-pipeline-wiring-SUMMARY.md`
</output>
