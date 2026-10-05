# Phase 26: Namespace Creation Awareness - Research

**Researched:** 2026-10-04
**Domain:** Go CLI (cobra/client-go/Hubble pipeline) — K8s namespace watch (live) + flow-timestamp heuristic (replay) feeding the v1.3 end-of-run session-summary seam
**Confidence:** HIGH (all API-shape claims verified against the exact pinned dependency versions in the module cache; all codebase claims verified by reading the source files this session)

## Summary

Phase 26 closes the batch blind spot documented in `docs/KNOWN_LIMITATIONS.md` §9 ("Dynamic namespace creation — new tenant namespace appears after the cpg run; its denied-flows do not generate rules. Re-run required."). Two independent detection mechanisms feed one shared end-of-run report:

1. **NSAW-01 (live):** during `cpg generate`, a read-only core/v1 namespaces watch (`client-go`, already a pinned dependency at v0.35.4) collects namespaces that appear after run start. The watch is advisory-only — it runs in a goroutine bounded by the pipeline context, and any failure (kubeconfig missing under `--server` bypass, RBAC denial, connection error, watch channel close) emits exactly one warning and disables the feature for the run. Generation is never gated on it.
2. **NSAW-02 (offline):** during `cpg replay`, the aggregator — which already runs a single-goroutine flow loop — records the first-observed timestamp per namespace (source and destination endpoints). At end of run, namespaces whose first observed flow is strictly after the earliest observed flow timestamp in the capture are reported. No K8s connectivity is involved.

Both mechanisms ride the **existing v1.3 session-summary seam**: a new no-op-when-empty print block, written to the same resolved `cfg.Stdout` writer that `PrintClusterHealthSummary` uses (`pkg/hubble/pipeline.go:405-446`). Because MCP mode already sets `PipelineConfig.Stdout = mcpModeStdout()` (os.Stderr, `cmd/cpg/mcp.go:141`), stdout purity in MCP mode is preserved *structurally* — no new wiring needed. When no new namespaces are detected, the block prints nothing, satisfying the "zero new namespaces leaves the summary byte-identical to v1.6" success criterion exactly the way `PrintClusterHealthSummary` already handles zero drops (`summary.go:41-42`: `if len(snapshots) == 0 { return }`).

**No new external packages.** Everything needed (client-go watch API, fake clientset testing, `timestamppb` timestamp access on `flowpb.Flow`) ships in already-pinned dependencies verified in this session. There is nothing to install and no legitimacy-gate surface.

**Primary recommendation:** add a `pkg/k8s` namespace watcher (list-snapshot + watch, fail-safe warn-once), a single-goroutine first-seen map in the aggregator, two pure reporting helpers in `pkg/hubble`, and one new no-op-when-empty summary block printed from `RunPipelineWithSource` next to `PrintClusterHealthSummary`.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| NSAW-01 | During `cpg generate`, watch namespace-creation events and list at end of run every namespace created after run start ("namespace X appeared after run start — re-run to cover it") | client-go `CoreV1().Namespaces().Watch(ctx, metav1.ListOptions)` verified at module-cache `kubernetes/typed/core/v1/namespace.go:48`; `watch.Event{Type: "ADDED"}` at `apimachinery pkg/watch/watch.go:57,70`; fake-clientset watch-reactor testing pattern already used in `pkg/k8s/*_test.go`. Fail-safe warn-once precedents: `maybeRunL7Preflight` / `maybeRunVersionPreflight` (`cmd/cpg/generate.go:39-88`) and `versionDetectTimeout` WR-01 bounded-probe pattern |
| NSAW-02 | On `replay`, report namespaces whose first observed flow arrives after capture start (flow-timestamp heuristic, no K8s watch) | `flowpb.Flow.Time *timestamppb.Timestamp` verified at module-cache `cilium@v1.19.4 api/v1/flow/flow.pb.go:1447`; fixtures already carry `flow.time` (`testdata/flows/small.jsonl` line 1); namespace attribution helper `policyTargetEndpoint` + `ep.Namespace` verified at `pkg/hubble/aggregator.go:269-299`; single-goroutine aggregator invariant (same as `l7Collapse`, L7GEN-01 comment) makes an unlocked first-seen map safe |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Namespace-creation watch (NSAW-01) | CLI process (`cmd/cpg generate`) via `pkg/k8s` | — | `pkg/k8s` owns all kubeconfig/cluster interaction (CODEBASE_MAP); CLI generate is the only caller with a kubeconfig + watch lifecycle |
| First-seen namespace timestamps (NSAW-02) | Pipeline / aggregator (`pkg/hubble`) | — | The aggregator's flow loop is the only place every replayed flow passes through; single-goroutine, no locking |
| End-of-run new-namespace report | Pipeline (`pkg/hubble` summary) | — | Rides the v1.3 session-summary seam (`cfg.Stdout` resolution in `RunPipelineWithSource`); one print site keeps byte-identity auditable |
| Watch lifecycle + fail-safe | `pkg/k8s` watcher, started by `cmd/cpg` | — | Mirrors how preflight/version detection are started by `cmd/cpg` and kept advisory |
| MCP stdout purity | MCP seam (`cmd/cpg/mcp.go`) | — | Already solved: `mcpModeStdout()` = os.Stderr; the new block inherits it by using `cfg.Stdout` |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `k8s.io/client-go` (already in go.mod) | v0.35.4 | `CoreV1().Namespaces().List` + `.Watch` | Already the project's cluster client; no new dependency `[VERIFIED: go.mod + module cache namespace.go:48]` |
| `k8s.io/client-go/kubernetes/fake` + `k8s.io/client-go/testing` | v0.35.4 | Watch-reactor unit tests (`PrependWatchReactor`, `watch.NewFake()`) | Established repo pattern (`pkg/k8s/preflight_test.go:88` uses `fake.NewSimpleClientset`) `[VERIFIED: testing/fake.go:115, apimachinery pkg/watch/watch.go:116]` |
| `k8s.io/apimachinery/pkg/watch` | v0.35.4 | `watch.Interface`, `Event{Type, Object}` | Standard watch surface `[VERIFIED: pkg/watch/watch.go:54-74]` |
| `github.com/cilium/cilium/api/v1/flow` (already in go.mod) | v1.19.4 | `Flow.Time` for the replay heuristic | Already consumed by the pipeline `[VERIFIED: flow.pb.go:1447]` |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `google.golang.org/protobuf/types/known/timestamppb` | (transitive, pinned) | `.AsTime()` / `.IsValid()` on `Flow.Time` | Parsing flow timestamps in the replay heuristic |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Plain list+watch | SharedInformerFactory / informers | Informers add reflector/relist machinery, caches, and threads for a one-shot advisory feature; overkill and harder to make warn-once. Reject. |
| Plain list+watch | Field selector `metadata.creationTimestamp>…` on namespaces Watch | Namespaces support field selectors only for `metadata.name` and `status.phase` — creationTimestamp is not server-side filterable `[ASSUMED]`. Filter client-side instead. |
| Watch-only (no initial List) | — | Without a snapshot you cannot distinguish "new" from "pre-existing but ADDED-delivered"; List-then-watch removes the ambiguity. Reject watch-only. |

**Installation:**
```bash
# None — zero new modules. Everything is already pinned in go.mod/go.sum.
```

## Package Legitimacy Audit

**No new packages are installed by this phase.** The `k8s.io/*` and `github.com/cilium/cilium` modules are existing pinned dependencies already vetted in prior phases. The package-legitimacy gate is therefore satisfied vacuously: nothing to check, nothing removed, nothing flagged.

## Architecture Patterns

### System Architecture Diagram

```
                ┌─────────────────────────────────────────────────────────┐
 NSAW-01 (live) │ cpg generate                                            │
                │                                                         │
 kubeconfig ──► │ cmd/cpg: start pkg/k8s.NamespaceWatcher(ctx)            │
                │    ├─ List namespaces → snapshot set (pre-run state)    │
                │    └─ Watch(ADDED events) ──► collector                 │
                │         ├─ name ∉ snapshot → record "new namespace"     │
                │         └─ any failure (RBAC/dial/close) → warn ONCE,   │
                │              stop watching (generation unaffected)      │
                │                                                         │
 Hubble Relay ► │ hubble.RunPipelineWithSource  ◄─── new-ns collector     │
                │    ├─ aggregator (flows → policies)                     │
                │    └─ after g.Wait():                                   │
                │         PrintClusterHealthSummary(stdout, …)   (v1.3)   │
                │         PrintNewNamespacesSummary(stdout, ns, hint)     │
                │              └─ empty list ⇒ prints NOTHING (byte-id)   │
                └─────────────────────────────────────────────────────────┘

                ┌─────────────────────────────────────────────────────────┐
 NSAW-02 (replay)│ cpg replay file.jsonl                                  │
                │    flowsource.FileSource ──► flows (with Flow.Time)     │
                │         └─► aggregator flow loop:                       │
                │              firstSeen[ns] = min(Time) for source+dest  │
                │    after g.Wait():                                      │
                │      captureStart = min over firstSeen                  │
                │      report ns where firstSeen[ns] > captureStart       │
                │      └─► PrintNewNamespacesSummary (same seam, no K8s)  │
                └─────────────────────────────────────────────────────────┘

 MCP mode: start_session builds PipelineConfig with Stdout = mcpModeStdout()
 (= os.Stderr, cmd/cpg/mcp.go:141). The new block uses cfg.Stdout ⇒ never
 touches real os.Stdout. CLI generate/replay leave Stdout nil ⇒ os.Stdout.
```

### Recommended Project Structure
```
pkg/k8s/
├── ns_watch.go            # NamespaceWatcher: list-snapshot + watch + warn-once (NEW)
└── ns_watch_test.go       # fake-clientset watch-reactor tests (NEW)
pkg/hubble/
├── ns_awareness.go        # Pure helpers: FirstSeen collection helpers, capture-start +
│                          # report-list computation, PrintNewNamespacesSummary (NEW)
└── ns_awareness_test.go   # Golden-ish stdout tests incl. empty⇒zero-bytes (NEW)
pkg/hubble/
└── aggregator.go          # + firstSeenByNamespace map, recorded in the Run loop; + accessor
cmd/cpg/
└── generate.go            # + start watcher after kubeconfig resolution; pass result into cfg
```

### Pattern 1: List-snapshot + watch, fail-safe warn-once (NSAW-01)
**What:** Snapshot existing namespaces with `List` at run start; `Watch` in a goroutine; an `ADDED` event whose object name is absent from the snapshot is a new namespace. Every failure path — `List` error, `Watch` construction error, RBAC denial, watch channel close/error event — emits exactly one warning and permanently disables the feature for this run.
**When to use:** `cpg generate` only (with a kubeconfig). Never in replay (offline), never blocking startup.
**Example:**
```go
// Sketch — names/APIs verified against module cache v0.35.4.
// Source: k8s.io/client-go@v0.35.4 kubernetes/typed/core/v1/namespace.go:48,
//         k8s.io/apimachinery@v0.35.4 pkg/watch/watch.go:54-74
existing := map[string]struct{}{}
list, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
if err != nil { warnOnce(); return collector }        // fail-safe: empty collector
for i := range list.Items { existing[list.Items[i].Name] = struct{}{} }

w, err := clientset.CoreV1().Namespaces().Watch(ctx, metav1.ListOptions{})
if err != nil { warnOnce(); return collector }
go func() {
    defer w.Stop()
    for ev := range w.ResultChan() {
        if ev.Type != watch.Added { continue }        // also handle watch.Error → warnOnce+stop
        ns, ok := ev.Object.(*corev1.Namespace)
        if !ok || ns.Name == "" { continue }
        if _, seen := existing[ns.Name]; seen { continue }
        collector.Record(ns.Name)                     // mutex-protected or channel send
    }
    // channel closed (apiserver watch expiry) → optional single warn; feature ends
}()
```
Note: `CreationTimestamp` (via `metav1.ObjectMeta`) can be cross-checked against run start as a belt-and-braces filter; `metav1.Time.IsZero()` exists `[VERIFIED: apimachinery pkg/apis/meta/v1/time.go:61]`.

### Pattern 2: Single-goroutine first-seen map (NSAW-02)
**What:** In `Aggregator.Run`'s flow loop (the single consumer of the flow channel), record `firstSeen[ns]` = the earliest non-zero `Flow.Time` per namespace, taking namespaces from both source and destination endpoints.
**When to use:** Replay (and it is equally correct — though inert — for live generate).
**Concurrency:** the Run loop is single-goroutine by design; the same "no locking by design" invariant is already documented for `l7Collapse` (L7GEN-01 comment, `pkg/hubble/aggregator.go:140-147`). Read the map only after `g.Wait()`, from the accessor pattern used by `agg.InfraDrops()`.

### Pattern 3: No-op-when-empty summary block (the byte-identity mechanism)
**What:** A new `PrintNewNamespacesSummary(out io.Writer, namespaces []string, …)` in `pkg/hubble`, called from `RunPipelineWithSource` immediately after `PrintClusterHealthSummary(stdout, …)` (`pkg/hubble/pipeline.go:446`), using the **same** resolved `stdout` local. `if len(namespaces) == 0 { return }` — the exact guard `PrintClusterHealthSummary` uses (`pkg/hubble/summary.go:41-42`).
**Why:** success criterion 1 ("zero new namespaces leaves the summary byte-identical to v1.6") and criterion 4 (rides the v1.3 session-summary mechanism; MCP purity inherited from `cfg.Stdout`).

### Anti-Patterns to Avoid
- **Informer/shared-informer machinery for a one-shot advisory watch:** threads, caches, relist semantics; impossible to keep "warn once, never block" simple. Use bare List+Watch.
- **Gating generation on watch success:** the watch must never appear in any error return path of `RunPipelineWithSource`; failures are `logger.Warn` only (mirror `maybeRunVersionPreflight`, `cmd/cpg/generate.go:66-88`).
- **Filtering the report by the run's `--namespace` flag:** the blind spot is precisely the namespaces the *filter* excluded; report all newly created cluster namespaces.
- **Relying on `Watch` without an initial snapshot:** you cannot tell "new" from "synthetic initial ADDED" without either a snapshot or a timestamp comparison. `[ASSUMED]` for the synthetic-ADDED behavior itself — the snapshot pattern makes it moot.
- **Using file mtime as replay "capture start":** captures pass through pipes/redirection (`hubble observe … > drops.jsonl`, stdin `-`); mtime is unrelated to observation start. Use min observed `Flow.Time`.
- **Recomputing the report inside MCP-specific code:** no MCP-specific branch at all — the seam is `cfg.Stdout`, full stop.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Namespace change detection | Polling loop / DIY event dedup | client-go `Namespaces().Watch` | Server-push, ordered, standard; hand-rolled polling races and burns API quota |
| Watch unit tests | A mock `watch.Interface` from scratch | `fake.NewSimpleClientset` + `PrependWatchReactor("namespaces", …)` + `watch.NewFake()` | Established, race-free; already the repo's fake pattern in `pkg/k8s` |
| Protobuf timestamp parsing | Manual seconds/nanos arithmetic | `timestamppb.Timestamp.AsTime()` / `.IsValid()` | Nil-safety and sub-second precision handled; `Flow.Time` is nullable so nil handling is mandatory, not optional |
| Warm/reconnect watch logic | Exponential-backoff reconnector | Deliberately none — warn once, stop | NSAW-01's contract is fail-safe visibility, not availability; reconnect logic would add state and failure modes to a purely advisory feature |

**Key insight:** every mechanism this phase needs already exists in pinned dependencies and the codebase's own seams — the real work is *wiring and print-site discipline*, not new machinery.

## Common Pitfalls

### Pitfall 1: Synthetic ADDED events make every namespace look "new"
**What goes wrong:** a `Watch` started without a snapshot/resourceVersion delivers the current state of all existing namespaces as `ADDED` events; a naive "report every ADDED" reports the whole cluster.
**Why it happens:** that is standard watch semantics for an unset resourceVersion. `[ASSUMED]` (server-side behavior; cannot be executed against a cluster in this environment).
**How to avoid:** List-snapshot first; report only names absent from the snapshot. Optionally AND with `CreationTimestamp.After(runStart)`.
**Warning signs:** a test with a fake clientset seeded with namespaces reports them as new.

### Pitfall 2: Byte-identity regression in the v1.6 summary
**What goes wrong:** printing even a header line when zero new namespaces were seen breaks success criterion 1.
**How to avoid:** the print function returns early on an empty list (mirror `summary.go:41-42`); add a test asserting the summary output for a no-new-namespace run is byte-for-byte equal to the output produced without the feature compiled in. The pre-phase-golden procedure referenced in `pkg/hubble/pipeline_l7_golden_test.go:1-21` (pinned to the v1.6.0 tag) is the repo's precedent for byte-identity evidence.
**Warning signs:** any `Fprintf` outside the `len(namespaces) > 0` branch.

### Pitfall 3: `Flow.Time` is nil / zero in replay captures
**What goes wrong:** `Flow.Time` is an optional proto field (`flow.pb.go:1447`, `json:"time,omitempty"`); a missing timestamp yields a zero time, which sorts "before" everything and corrupts both capture-start and first-seen logic (either floods or starves the report).
**How to avoid:** skip flows with nil/invalid `Time` for the first-seen map (`timestamppb` `.IsValid()`); if *no* flow carries a timestamp, the heuristic must report nothing (fail-safe, mirrors the summary no-op).
**Warning signs:** fixtures like `testdata/flows/malformed.jsonl`-adjacent inputs; hand-edited captures.

### Pitfall 4: Defining "capture start" wrong
**What goes wrong:** using wall-clock replay time, file mtime, or the *last* flow timestamp misclassifies namespaces.
**How to avoid:** capture start = the **minimum observed flow timestamp across the capture** (the first line of a chronological capture). A namespace is reportable iff `firstSeen[ns] > captureStart` strictly. Consequence to document: the namespace of the very first flow can never be reported — that is correct behavior (it existed at capture start, as far as the heuristic can know).
**Warning signs:** off-by-one reports where the first-observed namespace is flagged.

### Pitfall 5: Watch goroutine outliving the run or blocking shutdown
**What goes wrong:** the watcher holds the context open or sends on an unbuffered channel after `g.Wait()`, hanging Ctrl+C shutdown.
**How to avoid:** derive the watcher's lifetime from the same `ctx` passed to `RunPipeline` (already a `signal.NotifyContext`); collect via buffered channel or mutex-guarded slice; never send to the pipeline's flow/policy channels. The WR-01 bounded-probe comment (`cmd/cpg/generate.go:29-34`) is the repo's written rationale for keeping advisory probes off the critical path.
**Warning signs:** `go test -race` leaks detected by goroutine-leak checks; a hang at end of `generate --timeout`.

### Pitfall 6: RBAC / no-kubeconfig paths silently *blocking* instead of warning
**What goes wrong:** `--server` bypass in `generate` never loads a kubeconfig; `List`/`Watch` against a restricted service account can 403. Both must result in at most one warning line and a disabled feature — never an error return, never repeated warnings per event.
**How to avoid:** warn-once via a single `warnOnce` closure per run (repo precedent: VIS-01/AUD-01 "single warning per pipeline run" patterns in `pipeline.go:366-382`); skip the watcher entirely when kubeconfig is nil under `--server` (mirror `maybeRunL7Preflight`'s nil check, `generate.go:40-47`).
**Warning signs:** repeated WARN lines during a long run; non-zero exit on RBAC denial.

### Pitfall 7: Namespace attribution asymmetry in the replay heuristic
**What goes wrong:** if first-seen is recorded only for the *policy-target* endpoint (`policyTargetEndpoint`, `aggregator.go:269-285`), a brand-new namespace acting purely as a *source* (client side) is never reported — a real awareness gap. If recorded for both endpoints, the report may include namespaces cpg would not directly generate policies for.
**How to avoid:** decide consciously. **Recommendation:** record both source and destination endpoint namespaces — the requirement is about namespace *existence* awareness, and the re-run hint remains valid regardless of which side of the flow the new namespace sat on. Document the choice in the report's hint text. Flag as a planner decision (A5).

### Pitfall 8: Watch expiry mid-run ends awareness silently
**What goes wrong:** apiserver watches expire (server-chosen timeout); the result channel closes and the rest of a long run is unwatched.
**How to avoid:** acceptable per the fail-safe contract, but emit the one warning when the channel closes so the operator knows awareness stopped; do NOT auto-reconnect (see Don't Hand-Roll). `[ASSUMED]` expiry behavior (standard apiserver behavior; not executable here).

## Code Examples

### Accessing flow timestamps (verified shape)
```go
// Source: cilium@v1.19.4 api/v1/flow/flow.pb.go:1447 (pinned by go.sum)
type Flow struct {
	Time *timestamppb.Timestamp `protobuf:"bytes,1,opt,name=time,proto3" json:"time,omitempty"`
	// …
}
// Fixture evidence — testdata/flows/small.jsonl line 1:
// {"flow":{"time":"2026-04-24T14:00:00Z","verdict":"DROPPED",
//   "traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],
//   "namespace":"default"},"destination":{…,"namespace":"production"}, …}}
```

### Namespace attribution (existing helper, verbatim semantics)
```go
// Source: pkg/hubble/aggregator.go:269-285
// policyTargetEndpoint returns the endpoint that policy decisions target for a
// given flow direction: INGRESS targets the destination, EGRESS targets the
// source. Returns nil for unknown or unset directions.
//   (… and at :285: if ep.Namespace != "" { ns = ep.Namespace })
```

### The no-op-when-empty guard being mirrored
```go
// Source: pkg/hubble/summary.go:39-42
func PrintClusterHealthSummary(out io.Writer, snapshots []HealthDropSnapshot, …) {
	if len(snapshots) == 0 {
		return
	}
	// …
```

### Watch API surface (verified, v0.35.4)
```go
// Source: k8s.io/client-go@v0.35.4 kubernetes/typed/core/v1/namespace.go:48
Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
// Source: k8s.io/apimachinery@v0.35.4 pkg/watch/watch.go:57,70
Added EventType = "ADDED"
type Event struct { Type EventType; Object runtime.Object }
// Source: k8s.io/client-go@v0.35.4 testing/fake.go:115 (test-side)
func (c *Fake) PrependWatchReactor(resource string, reaction WatchReactionFunc)
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| KNOWN_LIMITATIONS §9 "cron/watcher wrapper" workaround | In-tool awareness + re-run hint (this phase) | v1.7 / Phase 26 | §9 should be updated at doc time to describe the report instead of the workaround (planner: include a doc task) |
| Watch via client-go informers for any change detection | Bare List+Watch acceptable for advisory one-shot features | — | Keeps fail-safe surface minimal |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | A namespaces `Watch` with unset resourceVersion delivers existing namespaces as synthetic `ADDED` events (standard apiserver behavior) | Pitfall 1 | Low — the List-snapshot pattern makes the report correct under either behavior; only the belt-and-braces timestamp filter's necessity changes |
| A2 | Namespaces field selectors do not support `metadata.creationTimestamp` | Standard Stack (alternatives) | None — the design already filters client-side |
| A3 | Apiserver watches expire after a server-chosen timeout (~5–10 min), closing the result channel | Pitfall 8 | Low — behavior chosen (warn-once, stop) is correct under both expiry and clean close |
| A4 | "Capture start" = min observed flow timestamp (requirement says "capture start" but a jsonpb dump carries no explicit capture-start marker) | Pitfall 4 | Medium — different definition (e.g. file mtime) would misclassify; planner should state this definition in the plan |
| A5 | Replay first-seen records **both** source and destination endpoint namespaces (vs target-only) | Pitfall 7 | Low-Medium — target-only under-reports new namespaces acting as sources; both-endpoints may report namespaces with no generated policy. Either satisfies NSAW-02's letter; pick and document |
| A6 | MCP `start_session` is live-only (no replay source), so NSAW-02 has no MCP surface | Architecture | None — verified from `pkg/session/manager.go:285-340` (resolveSetup only resolves live server/port-forward); reporting block is inert without a watcher/first-seen data anyway |
| A7 | Go toolchain absence in *this research environment* does not reflect the executor environment | Environment Availability | Medium — if the executor environment also lacks Go, all `go test` validation steps fail until Go ≥ 1.25.1 is installed |

## Open Questions (RESOLVED)

1. **Exact wording/placement of the new-namespace block relative to the cluster-health block**
   - What we know: must print to the same `stdout` seam; must be absent (zero bytes) when the list is empty.
   - What's unclear: before vs after `PrintClusterHealthSummary`, and whether `cpg replay` uses an identical block header.
   - RESOLVED: printed AFTER `PrintClusterHealthSummary`; one shared print function (`PrintNewNamespacesSummary`) for both paths with a `NewNamespaceSource` hint selecting the copy — watch: `  - namespace <name> appeared after run start — re-run to cover it`, replay: `  - namespace <name> first observed after capture start — re-run to cover it`, header `New namespaces detected during this run:`. Pinned in plan 26-01 objective + `<interfaces>` (also encoded in 26-01 must_haves).

2. **Should NSAW-01 apply in MCP `start_session` mode?**
   - What we know: MCP sessions resolve a kubeconfig (port-forward, cluster_dedup), so a watch is *possible*; requirement text scopes NSAW-01 to `cpg generate`.
   - RESOLVED: CLI-generate-only this phase — no watcher in `pkg/session`; MCP `start_session` leaves `PipelineConfig.NewNamespaces` nil (heuristic fallback only, inert for its report semantics; rides mcpModeStdout). Zero MCP-surface change, zero stdout-purity risk. Pinned in plan 26-02 objective (also encoded in 26-02 must_haves; enforceable via the replay/session grep guards in 26-02 acceptance criteria).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Building/testing the project (`make test`) | ✗ (not found on PATH, /usr/local, /opt, snap, asdf/mise in this research environment) | project pins `go 1.25.1` / toolchain `go1.25.12` | Install Go ≥ 1.25.1 before execution; module cache (`~/go/pkg/mod`) is already populated for pinned deps |
| golangci-lint | `make lint` | ✗ | — | `make test` suffices for phase validation; lint is CI-side |
| Kubernetes cluster (live watch e2e) | NSAW-01 end-to-end proof | ✗ (unit-testable via fake clientset) | — | Fake-clientset watch-reactor tests; no live cluster required (repo convention: "no live cluster required", docs/CODEBASE_MAP.md) |
| Node.js + gsd-tools | (research tooling only) | ✓ | — | — |

**Missing dependencies with no fallback:** none for the *code* (all verified from module cache); the Go toolchain is required at execution time — planner should treat its availability as an executor-environment precondition (A7).

**Missing dependencies with fallback:** live-cluster e2e → fake clientset watch-reactor tests (established repo pattern).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` + `stretchr/testify` v1.11.1 (assert/require), race detector |
| Config file | none — conventional Go layout; commands via Makefile |
| Quick run command | `go test ./pkg/hubble/ ./pkg/k8s/ -run 'NewNamespace|NamespaceWatch|FirstSeen' -count=1 -race` |
| Full suite command | `go test ./... -count=1 -race` (= `make test`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| NSAW-01 | ADDED event not in snapshot ⇒ recorded as new namespace | unit (fake clientset + `PrependWatchReactor` + `watch.NewFake()`) | `go test ./pkg/k8s/ -run TestNamespaceWatch -count=1 -race` | ❌ Wave 0 (`pkg/k8s/ns_watch_test.go`) |
| NSAW-01 | List error / Watch error / RBAC 403 / channel close ⇒ one warning, empty collector, no error propagated | unit | `go test ./pkg/k8s/ -run TestNamespaceWatchFailSafe -count=1 -race` | ❌ Wave 0 |
| NSAW-01 | End-of-run summary lists new namespaces with re-run hint; watcher result threaded through `RunPipelineWithSource` | integration (in-process pipeline with `flowsource.FlowSource` fake + injected collector — precedent: `pkg/hubble/pipeline_test.go`) | `go test ./pkg/hubble/ -run TestPipelineNewNamespaces -count=1 -race` | ❌ Wave 0 |
| NSAW-01 | Zero new namespaces ⇒ summary output byte-identical to output without the feature | unit (dual-buffer comparison; precedent: pre-phase-golden procedure in `pipeline_l7_golden_test.go`) | `go test ./pkg/hubble/ -run TestSummaryByteIdentity -count=1` | ❌ Wave 0 |
| NSAW-02 | First flow after capture start per namespace computed from fixtures with staggered timestamps | unit (new fixture, e.g. `testdata/flows/ns_staggered.jsonl`, mirroring `small.jsonl` shape) | `go test ./pkg/hubble/ -run TestFirstSeenNamespaces -count=1 -race` | ❌ Wave 0 |
| NSAW-02 | Flows with nil/zero `Time` skipped; all-zero capture ⇒ empty report | unit | `go test ./pkg/hubble/ -run TestFirstSeenZeroTime -count=1 -race` | ❌ Wave 0 |
| SC-4 | MCP stdout purity unaffected by the new block | integration (extend `TestMCPSessionLifecycleWiringAndStdoutPurity`, `cmd/cpg/mcp_session_test.go:158-240`) | `go test ./cmd/cpg/ -run TestMCPSessionLifecycleWiringAndStdoutPurity -count=1 -race` | ✅ exists — extend |

### Sampling Rate
- **Per task commit:** quick run command above (pkg/hubble + pkg/k8s scoped, ~seconds)
- **Per wave merge:** `go test ./... -count=1 -race` (full suite; ~610+ existing tests)
- **Phase gate:** full suite green + byte-identity test green before `/gsd:verify-work`

### Wave 0 Gaps
- [ ] `pkg/k8s/ns_watch_test.go` — watch/fail-safe coverage for NSAW-01
- [ ] `pkg/hubble/ns_awareness_test.go` — pure-helper + print-block tests incl. byte-identity
- [ ] `testdata/flows/ns_staggered.jsonl` — replay fixture with staggered namespace first-seen timestamps (one namespace existing at capture start, one appearing mid-capture)
- [ ] Go toolchain present in executor environment (see Environment Availability / A7)

## Security Domain

`security_enforcement` is not disabled in `.planning/config.json` (key absent ⇒ enabled). cpg's core security posture is the readonly guarantee (`docs/security.md`: "only lists, watches, and reads Kubernetes and Hubble data").

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No new auth surfaces; kubeconfig auth unchanged |
| V3 Session Management | no | — |
| V4 Access Control | yes | Watch is read-only (namespaces list/watch verbs); fail-safe never escalates; readonly posture statement in docs/security.md remains true |
| V5 Input Validation | yes | Namespace names rendered into terminal output come from the API server — format as data (list rendering like existing `top3`), never interpolated into shell/paths; flow-derived namespace strings already flow through the same print paths |
| V6 Cryptography | no | — |
| V14 Config | no | No new config surface |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| RBAC-denied watch turned into hard failure (DoS of generation) | Denial of Service | Warn-once, proceed; feature is advisory by contract (SC-2) |
| Unbounded namespace list memory during very chatty runs | DoS | Cap/collect as sorted string slice; namespaces are a small finite set in practice; no per-event allocation retention beyond the name |
| Information disclosure via new-namespace names into evidence/MCP output | Information Disclosure | Report block goes only to `cfg.Stdout` seam (stderr in MCP mode); never into evidence JSON |

## Sources

### Primary (HIGH confidence)
- Module cache (exact pinned versions from go.sum): `k8s.io/client-go@v0.35.4` (`kubernetes/typed/core/v1/namespace.go:48`, `testing/fake.go:115`, `kubernetes/fake/clientset_generated.go:146`), `k8s.io/apimachinery@v0.35.4` (`pkg/watch/watch.go:54-74`, `pkg/apis/meta/v1/time.go:61`), `k8s.io/api@v0.35.4` (`core/v1/types.go:7098`), `github.com/cilium/cilium@v1.19.4` (`api/v1/flow/flow.pb.go:1447`) — all read this session via `grep`/`read` on `~/go/pkg/mod`
- Repo source read this session: `pkg/hubble/pipeline.go` (PipelineConfig, RunPipelineWithSource, summary call site :446), `pkg/hubble/summary.go`, `pkg/hubble/aggregator.go`, `cmd/cpg/generate.go`, `cmd/cpg/replay.go`, `cmd/cpg/mcp.go`, `pkg/session/manager.go`, `pkg/flowsource/file.go`, `docs/CODEBASE_MAP.md`, `docs/security.md`, `docs/KNOWN_LIMITATIONS.md` §9, `Makefile`, `testdata/flows/small.jsonl`

### Secondary (MEDIUM confidence)
- None — no web sources were reachable/needed; all claims are codebase- or module-cache-verified

### Tertiary (LOW confidence)
- Server-side watch semantics (synthetic ADDED, watch expiry, namespace field-selector limits) — tagged `[ASSUMED]` (A1–A3, A6); not executable in this environment

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new deps; all API shapes read from pinned module sources
- Architecture: HIGH — every integration seam (PipelineConfig.Stdout, aggregator single-goroutine invariant, summary call site, MCP seam) read and cited this session
- Pitfalls: HIGH for codebase-side pitfalls, MEDIUM for the four server-side/behavioral assumptions explicitly logged A1–A4

**Research date:** 2026-10-04
**Valid until:** 2026-11-03 (stable — pinned deps; repo research is commit-anchored)
