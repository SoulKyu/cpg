# Phase 11: Aggregator Suppression + Health Writer - Context

**Gathered:** 2026-04-26
**Status:** Ready for planning
**Mode:** Auto-generated (autonomous mode, decisions locked in REQUIREMENTS + research SUMMARY)

<domain>
## Phase Boundary

Wire `pkg/dropclass` into the live + replay pipelines so that:
1. Aggregator classifies each flow's drop reason BEFORE `keyFromFlow`; flows classified as `Infra` or `Transient` are suppressed (no CNP generated) but still counted toward `flowsSeen`
2. A new `pkg/hubble/health_writer.go` consumes a third fan-out channel `healthCh chan DropEvent` and accumulates per-reason × per-node × per-workload counters
3. At session end, atomic write to `<output-dir>/cluster-health.json` (operator visibility — even though research debated evidence-dir, SUMMARY.md decided output-dir-with-prominent-banner-path; we'll document the path in phase 12 banner)

Wait — SUMMARY.md actually said evidence dir + prominent banner. Lock that in: write to `$XDG_CACHE_HOME/cpg/evidence/<hash>/cluster-health.json`. Phase 12 will print the path in banner.

Out of scope this phase: session summary block (phase 12), `--ignore-drop-reason` flag (phase 13), `--fail-on-infra-drops` exit code (phase 13).
</domain>

<decisions>
## Implementation Decisions (locked in REQUIREMENTS.md + research SUMMARY.md)

### Files
- `pkg/hubble/aggregator.go` — MODIFY: add classification step in Run() loop after --ignore-protocol gate, before keyFromFlow; new `infraDrops` counter; new `InfraDrops()` and `InfraDropTotal()` accessors; Run() signature gains `healthCh chan<- DropEvent` param
- `pkg/hubble/aggregator_test.go` — MODIFY: add tests for suppression + counting + flowsSeen invariant
- `pkg/hubble/health_writer.go` — NEW: `DropEvent` struct, `healthWriter` consumer, `finalize()` atomic write
- `pkg/hubble/health_writer_test.go` — NEW
- `pkg/hubble/pipeline.go` — MODIFY: third channel + Stage 2c goroutine + `SessionStats` fields + `hw.finalize()` call + `PipelineConfig` fields (DryRun gate)
- `cmd/cpg/generate.go` and `cmd/cpg/replay.go` — MODIFY: wire `dropclass.SetWarnLogger(logger)` once at startup; pass new pipeline config

### Counter semantics (CRITICAL — locked by SUMMARY)
- Infra/Transient drops INCREMENT `flowsSeen` (observed traffic count is total observed, not policy-eligible)
- Infra/Transient drops INCREMENT `infraDrops` counter (new)
- Infra/Transient drops do NOT call `keyFromFlow` (no bucket creation, no CNP)
- Infra/Transient drops DO send to `healthCh` (for cluster-health.json accumulation)

### Filter precedence in aggregator Run()
1. existing protocol filter (`--ignore-protocol`) — counted as `IgnoredByProtocol`
2. NEW classification gate — Policy/Unknown go through; Infra/Transient/Noise are suppressed + counted + sent to healthCh
3. existing `keyFromFlow` + bucket logic

### cluster-health.json schema (from FEATURES.md)
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
      "by_node": {"<node>": int, ...},
      "by_workload": {"<ns/workload>": int, ...}
    }
  ]
}
```

### File location
- `$XDG_CACHE_HOME/cpg/evidence/<hash>/cluster-health.json` (same dir as evidence, hashed by output dir per existing v1.1 decision)
- Atomic write via os.CreateTemp + os.Rename (mirror `pkg/evidence/writer.go` exactly)

### Concurrency
- Third channel `healthCh chan DropEvent` (buffered, mirrors `policyCh` size)
- Stage 2c goroutine in pipeline.go consumes healthCh into in-memory accumulator (sync-safe via channel ownership)
- finalize() called once after `g.Wait()` returns (no race with consumer)

### Dry-run parity
- `PipelineConfig.DryRun=true` → healthWriter created with nil writer path → finalize() is a no-op (mirrors evidence writer pattern at pkg/evidence/writer.go)

### Testing strategy
- TDD-first
- Aggregator tests: 5 policy + 3 infra flows → assert flowsSeen=8, infraDrops=3, 0 buckets for infra
- health_writer tests: synthetic DropEvent stream → assert cluster-health.json schema + atomic write
- Reuse pattern: aggregator already has counter+log pattern from PA5 (--ignore-protocol)

### Anti-features
- NO --ignore-drop-reason flag (phase 13)
- NO --fail-on-infra-drops exit code (phase 13)
- NO session summary block (phase 12)
- NO openmetrics
</decisions>

<code_context>
## Existing Code Insights

### Reusable assets
- `pkg/hubble/aggregator.go` — `--ignore-protocol` filter pattern (counter + early-continue) is the exact mirror to follow, but our classification gate counts BEFORE continue (preserves flowsSeen)
- `pkg/evidence/writer.go` — atomic write (CreateTemp + Rename), eviction-dir hashing, dry-run nil-guard
- `pkg/hubble/pipeline.go` — channel fan-out (tee) pattern from v1.1
- `pkg/dropclass/Classify()` — O(1) lookup ready, `pkg/dropclass/RemediationHint()` ready, `pkg/dropclass/SetWarnLogger()` ready, `ClassifierVersion` constant ready
- `pkg/dropclass/ValidReasonNames()` — phase 13 will use this; not needed here

### Established patterns
- Channel goroutine spawned in `pipeline.go` Stage section, joined via errgroup
- `PipelineConfig` is a plain struct extended additively (no breaking changes)
- `RunPipelineWithSource` returns `error` today — extend additively
</code_context>

<specifics>
## Specific Ideas

- Take taxonomy from `pkg/dropclass`, do NOT duplicate Cilium DropReason values in this phase
- The `DropEvent` struct should carry: reason name (string), class (DropClass), node (string from Flow.NodeName), workload (string formatted as ns/name), reason_int (for stable map keys if needed)
- For the `by_node` / `by_workload` maps, use bare workload identifiers; if Flow lacks NodeName or workload info, bucket under "_unknown"
</specifics>

<deferred>
## Deferred Ideas

- Per-flow timestamps in cluster-health.json (would balloon size; counters suffice)
- HTTP / DNS L7 sub-classification of drops (not meaningful — L4 reason is the trigger)
- Streaming partial writes during long replays (atomic-final is fine for v1.3)
</deferred>
