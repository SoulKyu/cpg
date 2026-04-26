# Architecture Research — v1.3 Cluster Health Surfacing

**Domain:** Extend existing CPG pipeline with drop-reason classification, suppression, health reporting, and CI exit codes
**Researched:** 2026-04-26
**Confidence:** HIGH (direct codebase analysis — all integration points read)
**Scope discipline:** v1.3 only. `cpg apply`, consolidation, Prometheus export excluded.

## Guiding Principle: Classify Early, Report Late

The pipeline already separates ingestion (Aggregator), transformation (BuildPolicy), and output (writer fan-out). v1.3 adds a single new classification step between ingestion and bucketing, and a new reporting writer parallel to the existing evidence writer. No existing goroutine is restructured; the fan-out model simply gains a third consumer.

---

## Q1 — Where Does the Classifier Live?

**Decision:** New standalone package `pkg/dropclass`.

**Rationale:**

- The classifier maps `flowpb.DropReason` → `DropClass` (policy / infra / transient) and carries a taxonomy map + remediation hints. This is pure data + lookup; it has no dependency on `flowpb.Flow` struct fields beyond `Flow.DropReasonDesc`, no dependency on `policy`, `hubble`, or any pipeline type.
- A dedicated package keeps the taxonomy testable in complete isolation (table-driven tests over the full Cilium drop-reason enum without importing aggregator or builder machinery).
- `pkg/hubble` is already large (14 files). Embedding the classifier there as `pkg/hubble/dropclass.go` would mix taxonomy data with pipeline orchestration and make the taxonomy unit tests transitively depend on all of `pkg/hubble`'s imports.
- `pkg/policy` governs CNP construction — wrong semantic home for infrastructure health data.

**New files:**

```
pkg/dropclass/
  classifier.go        # DropClass enum, Classify(reason) function, taxonomy map
  classifier_test.go   # table-driven coverage of all known DropReasons
  hints.go             # RemediationHint(reason) → string URL/instruction
  hints_test.go
```

`classifier.go` exports:

```go
type DropClass int

const (
    DropClassPolicy    DropClass = iota // policy-fixable: generate CNP
    DropClassInfra                      // infra-level: surface in cluster-health.json
    DropClassTransient                  // transient: count only, no remediation
    DropClassUnknown                    // unrecognized reason: treat as policy (safe default)
)

// Classify maps a Cilium DropReason to a DropClass.
// Unknown reasons return DropClassUnknown (→ treated as Policy so nothing is silently suppressed).
func Classify(reason flowpb.DropReason) DropClass

// RemediationHint returns a short URL or instruction for infra-class drops.
// Returns "" for non-infra drops.
func RemediationHint(reason flowpb.DropReason) string
```

**Import graph (no cycles):**

```
pkg/dropclass  imports  cilium/api/v1/flow (flowpb only)
pkg/hubble     imports  pkg/dropclass
cmd/cpg        imports  pkg/hubble (no direct dropclass import needed)
```

---

## Q2 — Third Channel or Extend evidenceCh?

**Decision:** Third channel `healthCh chan DropEvent` — independent from `evidenceCh`.

**Why not piggyback evidenceCh:**

- `evidenceCh` carries `policy.PolicyEvent` — a per-workload aggregated event with CNP + Attribution. Health data has a different shape: it is per-raw-flow (drop reason × node × pod) and must be collected for flows that were **suppressed before bucketing** (infra drops never reach `BuildPolicy` and therefore never produce a `PolicyEvent`). Piggybacking would require either: (a) emitting a synthetic `PolicyEvent` with no policy for infra drops (misleads the evidence writer) or (b) adding a union field to `PolicyEvent` (breaks the clean type). Both options are worse.
- The `healthCh` goroutine is simple (accumulate a map, write at end) — channel proliferation cost is one `make(chan DropEvent, 64)` and one `g.Go(...)`.
- Precedent: `policies → fan-out → policyCh + evidenceCh` was the same judgment call in v1.1 (shipped as the right architecture). `healthCh` extends the same pattern.

**DropEvent type** (defined in `pkg/hubble/health_writer.go` or inline in pipeline):

```go
// DropEvent is the minimal record the health writer needs from a suppressed flow.
type DropEvent struct {
    Reason    flowpb.DropReason
    Class     dropclass.DropClass
    Namespace string
    Workload  string
    NodeName  string
    PodName   string
    Count     uint64 // always 1; aggregated by healthWriter
}
```

`DropEvent` lives in `pkg/hubble` (same package as the writer that consumes it). It has no dependency on `pkg/policy` or `pkg/evidence`.

---

## Q3 — Suppression: FlowSource Boundary vs Aggregator?

**Decision:** Suppression (skip bucketing) inside the Aggregator's `Run()` loop, after classification. Counter accumulated on `Aggregator`.

**Why not at FlowSource boundary:**

- `FlowSource` (gRPC / file replay) is transport-layer only — it does not know classification semantics. Introducing drop-reason logic there would give `pkg/flowsource` a dependency on `pkg/dropclass`, coupling transport to domain logic.
- More critically: infra drops need to be **counted and forwarded to the health channel** before being discarded. The Aggregator is already the place where per-flow decisions are logged (see `--ignore-protocol` in `Run()`: count → `ignoredByProtocol` map → `continue`). Suppression at FlowSource would lose the flows entirely before they can be counted or forwarded.

**Placement in `Run()` loop — exact position:**

```
[flow arrives]
    │
    ├─ L7 counting (existing — counts regardless of classification)
    │
    ├─ --ignore-protocol filter (existing, PA5)
    │     ↓ continue on match
    │
    ├─ [NEW] drop-reason classification
    │     reason = f.DropReasonDesc
    │     class  = dropclass.Classify(reason)
    │     if class != DropClassPolicy:
    │         a.infraDrops[reason]++              // counter for session summary
    │         if healthCh != nil:
    │             healthCh <- buildDropEvent(f, class, reason)
    │         continue                            // suppress bucketing
    │
    ├─ keyFromFlow (existing)
    │
    └─ bucket accumulation (existing)
```

**Interaction with --ignore-protocol (Q6):**

Protocol filter runs **before** reason classification. Rationale: `--ignore-protocol` is an explicit user override ("I don't want TCP flows at all") that short-circuits any further processing. Reason classification is domain logic that only applies to flows the user has not already explicitly excluded. Order: proto-filter → reason-filter → keyFromFlow. Document this precedence in flag help text.

**Counter design** (mirrors `ignoredByProtocol`):

```go
// infraDrops accumulates per-reason counts for flows suppressed by classification.
// Surfaced via InfraDrops() → session summary + --fail-on-infra-drops.
infraDrops map[flowpb.DropReason]uint64
```

`Aggregator.InfraDrops() map[flowpb.DropReason]uint64` — returns copy (same contract as `IgnoredByProtocol()`).
`Aggregator.InfraDropTotal() uint64` — convenience sum for exit-code check.

---

## Q4 — cluster-health.json Lifecycle

**Decision:** Single atomic write at session end (same as evidence writer pattern).

**Why not per-flush incremental (like policy writer):**

- Health data is a diagnostic aggregate: counters × reason × workload × node. Partial files mid-session would show incomplete counts and mislead operators reading them during a long `cpg generate` run.
- The evidence writer precedent is the right model: collect all data in-memory, write atomically at the end using temp-file + rename.
- Per-flush would also require a merge strategy (what if a reason appears in flush 2 but not flush 1?). Atomic write removes that complexity entirely.
- Volume concern: health data is bounded (O(unique drop-reasons × workloads × nodes) — far smaller than policy files). Memory accumulation is not a practical issue.

**Atomic write implementation:**

```
os.CreateTemp(dir, "cluster-health.json.tmp-*")
json.MarshalIndent(report)
tmp.Write(data)
tmp.Close()
os.Rename(tmpPath, finalPath)
```

**Output path:** `<output-dir>/cluster-health.json` — placed alongside policy YAML files, not in the evidence cache. Rationale: operators expect health output alongside policies; the evidence cache is keyed by output-dir-hash and is intentionally not committed. `cluster-health.json` is a session artifact that belongs in the working directory.

---

## Q5 — Exit Code Path and Concurrency

**Decision:** `Aggregator` exposes `InfraDropTotal()`. `RunPipelineWithSource` returns the count via `SessionStats`. `cmd/cpg/generate.go` and `cmd/cpg/replay.go` check `--fail-on-infra-drops` after `hubble.RunPipeline*` returns and call `os.Exit(2)`.

**Concurrency model:**

The `Aggregator.Run()` goroutine is the sole writer to `infraDrops`. `InfraDropTotal()` is called only after `g.Wait()` completes (same pattern as `FlowsSeen()`, `IgnoredByProtocol()`). No mutex needed: the channel closure + errgroup guarantee happens-before semantics between the aggregator goroutine and the post-Wait read.

```go
// In RunPipelineWithSource, after g.Wait():
stats.InfraDropTotal = agg.InfraDropTotal()
stats.InfraDropsByReason = agg.InfraDrops()

// healthWriter finalizes here (atomic write), same timing as evidenceWriter.finalize()
if hw != nil {
    hw.finalize()
}
stats.Log(logger)
return err
```

**Exit code in cmd/cpg:**

```go
// generate.go and replay.go — after hubble.RunPipeline* returns
if f.failOnInfraDrops && stats.InfraDropTotal > 0 {
    os.Exit(2)
}
```

`hubble.RunPipelineWithSource` currently returns only `error`. Two options:

1. Return `(SessionStats, error)` — cleaner but breaks callers.
2. Accept a `*SessionStats` output parameter populated in-place — consistent with existing `stats` pointer pattern inside the function.

**Recommendation: option 2** — add `*SessionStats` as an optional out-param or expose via a dedicated `RunResult` struct. The function signature currently returns only `error`; promoting `SessionStats` to a return value is a clean API improvement worth making here since v1.3 is the first feature that needs post-run metrics at the call site.

**Graceful shutdown timing:** Context cancellation triggers `a.flush()` → `close(out)` → fan-out goroutine closes `policyCh` + `evidenceCh` + `healthCh` → all consumer goroutines drain and return → `g.Wait()` unblocks → post-Wait block runs. No race: the health writer's `finalize()` is called only after all `DropEvent`s have been received (channel is closed before finalize is called, same pattern as `evidenceWriter`).

---

## Q7 — --dry-run Interaction

**Decision:** `--dry-run` suppresses `cluster-health.json` write (same semantics as evidence + policies).

**Implementation:** Mirror `EvidenceEnabled && !DryRun` check.

```go
var hw *healthWriter
if !cfg.DryRun {
    hw = newHealthWriter(cfg.OutputDir, cfg.Logger)
}
```

When `hw == nil`, the health goroutine drains `healthCh` without writing (same nil-guard pattern as `evidenceWriter`).

**Log output in dry-run:** health writer logs `"would write cluster-health.json"` with a count of infra drops observed — consistent with `"would write policy"` from `policyWriter.dryRunEmit()`.

**--cluster-dedup interaction:** no interaction. Cluster dedup filters `PolicyEvent` in the policy writer (stage 2). Infra-class flows are suppressed before they produce a `PolicyEvent` and never reach the policy writer. Cluster dedup is orthogonal.

---

## Full Data Flow (v1.3)

```
              Hubble gRPC / jsonpb replay
                           │
                           ▼
           ┌───────────────────────────────────┐
           │  pkg/hubble/client.go (UNCHANGED) │
           │  StreamDroppedFlows               │
           └──────────────┬────────────────────┘
                          │ *flowpb.Flow
                          ▼
           ┌───────────────────────────────────┐
           │  pkg/hubble/aggregator.go (MODIFY)│
           │                                   │
           │  1. L7 count (unchanged)          │
           │  2. --ignore-protocol (unchanged) │
           │  3. [NEW] dropclass.Classify()    │
           │     infra/transient → count       │
           │     + send to healthCh → continue │
           │  4. keyFromFlow (unchanged)       │
           │  5. bucket (unchanged)            │
           │                                   │
           │  Exposes: InfraDrops()            │
           │           InfraDropTotal()        │
           └──────┬────────────┬───────────────┘
                  │            │
           policy.PolicyEvent  DropEvent
                  │            │
                  ▼            ▼
    ┌──────────── policiesCh   healthCh ──────────────┐
    │             (existing)   (NEW)                   │
    │                                                  │
    │  fan-out goroutine (Stage 1b) MODIFY:           │
    │   closes policyCh + evidenceCh + healthCh       │
    │                                                  │
    └──────────────────────────────────────────────────┘
           │           │           │
           ▼           ▼           ▼
    ┌──────────┐ ┌──────────┐ ┌──────────────────────┐
    │ policy   │ │ evidence │ │ health writer (NEW)  │
    │ writer   │ │ writer   │ │ pkg/hubble/           │
    │(Stage 2) │ │(Stage 2b)│ │ health_writer.go     │
    │UNCHANGED │ │UNCHANGED │ │                      │
    └──────────┘ └──────────┘ │ accumulates:         │
                               │  reason×workload×   │
                               │  node counters       │
                               │ finalize() → atomic  │
                               │ write cluster-       │
                               │ health.json          │
                               └──────────────────────┘

    After g.Wait():
    ┌─────────────────────────────────────────────────┐
    │  SessionStats (MODIFY)                          │
    │   + InfraDropTotal uint64                       │
    │   + InfraDropsByReason map[DropReason]uint64    │
    │  stats.Log() → extended session summary block   │
    └─────────────────────────────────────────────────┘
           │
           ▼
    cmd/cpg/generate.go + replay.go (MODIFY)
    │  check --fail-on-infra-drops
    └─ os.Exit(2) if InfraDropTotal > 0
```

---

## Component-Change Ledger

| Package / File | Status | Notes |
|----------------|--------|-------|
| `pkg/dropclass/classifier.go` | NEW | DropClass enum, Classify(), taxonomy map |
| `pkg/dropclass/classifier_test.go` | NEW | Table-driven, all known flowpb.DropReason values |
| `pkg/dropclass/hints.go` | NEW | RemediationHint() → URL/instruction string |
| `pkg/dropclass/hints_test.go` | NEW | |
| `pkg/hubble/aggregator.go` | MODIFY | Import dropclass; add classification step in Run(); infraDrops counter; InfraDrops() + InfraDropTotal() accessors; SetIgnoreDropReasons(); ignoredByReason map |
| `pkg/hubble/aggregator_test.go` | MODIFY | Tests for classification suppression, infraDrops counter, --ignore-drop-reason |
| `pkg/hubble/health_writer.go` | NEW | DropEvent type; healthWriter struct; accumulate(); finalize() → atomic JSON write |
| `pkg/hubble/health_writer_test.go` | NEW | |
| `pkg/hubble/pipeline.go` | MODIFY | HealthCh third channel; healthWriter goroutine (Stage 2c); PipelineConfig gains IgnoreDropReasons + FailOnInfraDrops fields; fan-out goroutine closes healthCh; post-Wait block populates InfraDrops on SessionStats + calls hw.finalize() |
| `pkg/hubble/pipeline.go` (SessionStats) | MODIFY | Add InfraDropTotal uint64; InfraDropsByReason map[flowpb.DropReason]uint64; extend Log() |
| `cmd/cpg/commonflags.go` | MODIFY | Add ignoreDropReasons []string; failOnInfraDrops bool; addCommonFlags() wires --ignore-drop-reason + --fail-on-infra-drops |
| `cmd/cpg/generate.go` | MODIFY | Parse + validate ignoreDropReasons (validateIgnoreDropReasons func); pass to PipelineConfig; check failOnInfraDrops → os.Exit(2) after RunPipeline |
| `cmd/cpg/replay.go` | MODIFY | Same as generate.go for flag plumbing + exit code |
| `pkg/hubble/client.go` | UNCHANGED | |
| `pkg/hubble/writer.go` | UNCHANGED | |
| `pkg/hubble/evidence_writer.go` | UNCHANGED | |
| `pkg/hubble/unhandled.go` | UNCHANGED | |
| `pkg/policy/` | UNCHANGED | BuildPolicy never receives infra-class flows; no changes needed |
| `pkg/evidence/` | UNCHANGED | Evidence only records policy-class flows |
| `pkg/output/` | UNCHANGED | |
| `pkg/flowsource/` | UNCHANGED | |
| `pkg/k8s/` | UNCHANGED | |

**Surface area:** 1 new package (4 files), 4 new files in `pkg/hubble`, 3 modified files in `cmd/cpg`, 2 modified files in `pkg/hubble`. No existing public API broken.

---

## Suggested Build Order

Dependencies drive the order: classifier first (pure domain logic, no pipeline imports), then aggregator integration (uses classifier), then writer (uses DropEvent from aggregator), then flag plumbing (uses all of the above), then exit code (uses pipeline output).

| # | Step | Files touched | Verifiable by | Dependencies |
|---|------|---------------|---------------|--------------|
| 1 | `pkg/dropclass`: classifier + hints | `classifier.go`, `hints.go` + tests | Table-driven tests over all `flowpb.DropReason` values; no pipeline imports | none |
| 2 | Aggregator classification + suppression | `aggregator.go`, `aggregator_test.go` | Unit tests: infra-class flow → not bucketed, infraDrops counter incremented; policy-class flow → bucketed as before | step 1 |
| 3 | `--ignore-drop-reason` flag in aggregator | `aggregator.go` (SetIgnoreDropReasons), `aggregator_test.go` | Tests mirror --ignore-protocol: ignored reasons not counted in infraDrops, not bucketed | step 2 |
| 4 | `healthCh` + `healthWriter` (accumulate only, no write yet) | `health_writer.go`, `pipeline.go` fan-out | Pipeline integration test: infra-class flows arrive on healthCh, policy-class flows do not; channel drains cleanly on context cancel | step 2 |
| 5 | `healthWriter.finalize()` → atomic `cluster-health.json` write | `health_writer.go`, `health_writer_test.go` | End-to-end replay test: known infra-class flows in fixture → cluster-health.json contains expected counters + hints; dry-run → no file written | step 4 |
| 6 | `SessionStats` infra-drop fields + extended `Log()` | `pipeline.go` | Session summary log contains infra-drop block when infra drops observed | step 2 |
| 7 | Flag plumbing: `--ignore-drop-reason` + `--fail-on-infra-drops` | `commonflags.go`, `generate.go`, `replay.go` | Cobra flag parsing + validation (validateIgnoreDropReasons mirrors validateIgnoreProtocols); --help output | step 3 |
| 8 | Exit code: `os.Exit(2)` when --fail-on-infra-drops | `generate.go`, `replay.go` | E2E replay test with infra-drop fixture + --fail-on-infra-drops → exit code 2; without flag → exit 0 | steps 5, 6, 7 |

**Steps 1-3 are pure aggregator work with no output side effects.** Anyone at step 3 gets suppression and counters but no file output yet — safe intermediate state. Steps 4-5 add the writer. Steps 7-8 expose user-facing flags.

---

## Integration Points Named Explicitly

| Integration point | Existing hook | Change required |
|-------------------|---------------|-----------------|
| Aggregator `Run()` loop — after PA5 proto-filter | `pkg/hubble/aggregator.go:243` (after `ignoredByProtocol` continue) | Add dropclass.Classify() + infraDrops counter + healthCh send |
| Fan-out goroutine (Stage 1b) | `pkg/hubble/pipeline.go:157-165` (defer closes) | Add `defer close(healthCh)`; add `healthCh <- buildDropEvent(f)` path — wait, fan-out receives from `policies` chan of `PolicyEvent`; infra drops must be forwarded **from aggregator directly to healthCh**, not via the fan-out. See note below. |
| `g.Wait()` post-processing block | `pkg/hubble/pipeline.go:201-224` | Add `stats.InfraDropTotal = agg.InfraDropTotal()`, `stats.InfraDropsByReason = agg.InfraDrops()`, `hw.finalize()` |
| `SessionStats.Log()` | `pkg/hubble/pipeline.go:80-94` | Add `zap.Uint64("infra_drop_total", ...)`, `zap.Any("infra_drops_by_reason", ...)` |

**Important architectural note on healthCh routing:**

Infra-class flows are suppressed **before** `BuildPolicy` and therefore never produce a `PolicyEvent`. The fan-out goroutine only reads from `policies chan PolicyEvent` — it cannot forward infra drops. The healthCh must be passed into the Aggregator directly, or the Aggregator sends to it from inside `Run()`.

**Recommended approach:** pass `healthCh` to `Aggregator.Run()` as a parameter.

```go
// Aggregator.Run signature (MODIFY)
func (a *Aggregator) Run(
    ctx context.Context,
    in <-chan *flowpb.Flow,
    out chan<- policy.PolicyEvent,
    healthCh chan<- DropEvent,  // NEW — nil when dry-run or health disabled
) error
```

This keeps the Aggregator's `Run()` self-contained (no stored channel field that could be set in wrong order) and matches the existing pattern where `out` is passed per-call, not stored on the struct.

---

## --dry-run + --cluster-dedup Interaction Summary

| Flag | Effect on cluster-health.json | Effect on suppression logic |
|------|-------------------------------|----------------------------|
| `--dry-run` | NOT written (hw == nil) | Classification + counting still runs; infraDrops populated; session log shows counts |
| `--cluster-dedup` | No effect | No interaction (dedup operates on PolicyEvent downstream of classification) |
| `--ignore-drop-reason <reason>` | Specified reasons not counted in infraDrops, not sent to healthCh | Applied after proto-filter, before classification |
| `--fail-on-infra-drops` | No effect on write | exit(2) checked after finalize() |

---

## Anti-Patterns to Avoid

### Suppress at FlowSource boundary

Classification belongs in the Aggregator where counts and channel routing coexist. Moving it to `pkg/flowsource` would silently discard infra drops with no counter, no health event, and no session summary entry.

### Synthetic PolicyEvent for infra drops

Do not emit a `PolicyEvent` with nil Policy to carry infra-drop data through the existing fan-out. The evidence writer would need nil-guards everywhere and the semantics of `PolicyEvent` would be corrupted. Use the dedicated `DropEvent` + `healthCh` path.

### Mutex on infraDrops

`infraDrops` is written only by the Aggregator's `Run()` goroutine (single writer). It is read only after `g.Wait()` (happens-before guarantee from errgroup). No mutex required. Adding one is both unnecessary and a false safety signal.

### Per-flush cluster-health.json

Partial health files mid-session are misleading. Atomic write at session end is the correct model; it matches evidence writer behavior and eliminates the need for a partial-file merge strategy.

---

## Sources

- Direct codebase reads: `pkg/hubble/aggregator.go`, `pkg/hubble/pipeline.go`, `pkg/hubble/evidence_writer.go`, `pkg/hubble/writer.go`, `pkg/evidence/schema.go`, `pkg/evidence/writer.go`, `cmd/cpg/generate.go`, `cmd/cpg/replay.go`, `cmd/cpg/commonflags.go`, `.planning/PROJECT.md` — HIGH confidence.
- v1.2 ARCHITECTURE.md (`--ignore-protocol` / PA5 pattern, fan-out pattern) — HIGH confidence.

---
*Architecture research for: CPG v1.3 Cluster Health Surfacing*
*Researched: 2026-04-26*
