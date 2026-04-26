---
phase: 11-aggregator-suppression-and-health-writer
plan: 01
type: tdd
wave: 1
depends_on: []
files_modified:
  - pkg/hubble/aggregator.go
  - pkg/hubble/aggregator_test.go
autonomous: true
requirements: [HEALTH-01, HEALTH-05]

must_haves:
  truths:
    - "A flow with CT_MAP_INSERTION_FAILED drop reason never reaches keyFromFlow — no CNP bucket is created"
    - "A flow with any Infra or Transient drop reason increments flowsSeen (observed traffic count is total, not policy-eligible)"
    - "Infra/Transient flows increment the infraDrops counter accessible via InfraDrops() and InfraDropTotal()"
    - "Policy and Unknown class flows continue through keyFromFlow unaffected"
    - "Noise class flows are silently dropped (no counter, no bucket — bookkeeping events)"
    - "Classification gate runs AFTER --ignore-protocol gate, BEFORE keyFromFlow"
    - "healthCh receives a DropEvent for every suppressed Infra/Transient/Unknown flow (nil healthCh is safe — no-op)"
  artifacts:
    - path: "pkg/hubble/aggregator.go"
      provides: "infraDrops map + Run(healthCh) signature + InfraDrops()/InfraDropTotal() accessors"
      contains: "infraDrops"
    - path: "pkg/hubble/aggregator_test.go"
      provides: "TDD test suite: suppression + counter invariant + flowsSeen accuracy"
      contains: "TestAggregatorClassificationSuppression"
  key_links:
    - from: "pkg/hubble/aggregator.go Run()"
      to: "pkg/dropclass.Classify()"
      via: "classification gate after --ignore-protocol block"
      pattern: "dropclass\\.Classify"
    - from: "pkg/hubble/aggregator.go Run()"
      to: "healthCh chan<- DropEvent"
      via: "send DropEvent before continue"
      pattern: "healthCh <-"
---

<objective>
Add drop-reason classification gate to Aggregator.Run() so Infra/Transient flows are suppressed from CNP generation but still counted in flowsSeen.

Purpose: Prevents bogus CNP generation for infrastructure-level drops (CT_MAP_INSERTION_FAILED, etc.) — the root cause of the v1.3 trigger bug.
Output: Modified aggregator.go with classification step; new DropEvent type; new InfraDrops()/InfraDropTotal() accessors; comprehensive TDD test suite.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/phases/11-aggregator-suppression-and-health-writer/11-CONTEXT.md
@.planning/phases/10-classifier-core/10-01-taxonomy-and-hints-SUMMARY.md
@.planning/phases/10-classifier-core/10-02-unknown-dedup-warn-SUMMARY.md

<interfaces>
<!-- Key types and contracts the executor needs. Extracted from codebase. -->

From pkg/dropclass/classifier.go:
```go
type DropClass int

const (
    DropClassUnknown   DropClass = iota  // 0 — unrecognized; never Policy
    DropClassPolicy                       // 1 — generate CNP
    DropClassInfra                        // 2 — surface in health JSON
    DropClassTransient                    // 3 — count only
    DropClassNoise                        // 4 — ignore entirely
)

func Classify(reason flowpb.DropReason) DropClass
func SetWarnLogger(l *zap.Logger)
```

From pkg/dropclass/hints.go:
```go
func RemediationHint(reason flowpb.DropReason) string  // "" for non-infra
```

From pkg/dropclass/version.go:
```go
const ClassifierVersion = "1.0.0-cilium1.19.1"
```

From pkg/hubble/aggregator.go (current Run signature):
```go
func (a *Aggregator) Run(ctx context.Context, in <-chan *flowpb.Flow, out chan<- policy.PolicyEvent) error

// Current filter precedence in Run():
// 1. L7 count (l7HTTPCount, l7DNSCount)
// 2. --ignore-protocol gate (ignoredByProtocol counter + continue)
// 3. keyFromFlow + bucket accumulation
// flowsSeen is incremented AFTER keyFromFlow returns skip=false
```

From pkg/hubble/pipeline.go (current caller):
```go
g.Go(func() error {
    return agg.Run(gctx, flows, policies)
})
```
</interfaces>
</context>

<feature>
  <name>Aggregator classification gate with DropEvent channel</name>
  <files>pkg/hubble/aggregator.go, pkg/hubble/aggregator_test.go</files>
  <behavior>
    Counter invariant (critical — per PITFALLS Pitfall 6):
    - Send 5 policy flows + 3 infra flows → flowsSeen=8, infraDrops=3, exactly 5 policy buckets created
    - Send 3 transient flows → flowsSeen=3, infraDrops=3 (transient counted in infraDrops), 0 buckets
    - Send 2 noise flows → flowsSeen=0, infraDrops=0, 0 buckets (noise silently discarded)
    - Send 1 unknown-class flow → flowsSeen=1, infraDrops=0, 1 bucket (unknown errs on side of generating)

    Suppression:
    - CT_MAP_INSERTION_FAILED flow → classified Infra → NOT bucketed → no PolicyEvent emitted
    - POLICY_DENIED flow → classified Policy → bucketed → PolicyEvent emitted normally
    - STALE_OR_UNROUTABLE_IP flow → classified Transient → NOT bucketed
    - NAT_NOT_NEEDED flow → classified Noise → silently dropped

    healthCh behavior:
    - Infra flow → DropEvent sent to healthCh (Reason, Class, NodeName, Namespace, Workload)
    - Transient flow → DropEvent sent to healthCh
    - Unknown flow → DropEvent sent to healthCh (per CONTEXT.md: unknown goes through healthCh too)
    - Noise flow → nothing sent to healthCh
    - Policy flow → nothing sent to healthCh
    - nil healthCh → no panic, suppress without send

    Filter precedence (enforced by test):
    - --ignore-protocol fires first (unchanged)
    - classification gate fires second
    - keyFromFlow fires third

    flowsSeen semantics (CRITICAL — see PITFALLS Pitfall 6):
    - flowsSeen is incremented for Policy, Infra, Transient, and Unknown flows
    - flowsSeen is NOT incremented for Noise flows or --ignore-protocol skips
    - Increment must happen BEFORE the classification branch, not inside keyFromFlow

    Run() signature change:
    - New param: healthCh chan<- DropEvent (passed as nil when no health writer; no-op)
    - pipeline.go caller site must be updated in plan 11-02 — this plan only adds the param

    dropclass.SetWarnLogger wiring:
    - Call dropclass.SetWarnLogger(a.logger) once in NewAggregator() constructor
      (not in Run() — logger is already set before Run is called)

    InfraDrops() / InfraDropTotal() accessors:
    - InfraDrops() map[flowpb.DropReason]uint64 — returns copy (same pattern as IgnoredByProtocol())
    - InfraDropTotal() uint64 — convenience sum of all infraDrops values
  </behavior>
  <implementation>
    Step 1 (RED): Write failing tests in aggregator_test.go covering all behavior above.
    Use table-driven test for class routing. Use nil healthCh for basic tests; use buffered chan for DropEvent tests.
    Tests must fail before implementation.

    Step 2 (GREEN): Implement in aggregator.go:

    1. Add DropEvent struct (in this file, same package as healthWriter which will consume it):
    ```go
    // DropEvent is the minimal record for a flow suppressed by the classification gate.
    // Consumed by healthWriter to accumulate cluster-health.json counters.
    type DropEvent struct {
        Reason    flowpb.DropReason
        Class     dropclass.DropClass
        Namespace string
        Workload  string  // formatted as labels.WorkloadName(ep.Labels); "_unknown" if nil
        NodeName  string  // f.NodeName; "_unknown" if empty
    }
    ```

    2. Add to Aggregator struct:
    ```go
    // infraDrops accumulates per-reason counts for flows suppressed by classification.
    // Surfaced via InfraDrops() + InfraDropTotal() for SessionStats + --fail-on-infra-drops (phase 13).
    infraDrops map[flowpb.DropReason]uint64
    ```
    Initialize in NewAggregator: `infraDrops: make(map[flowpb.DropReason]uint64)`

    3. Wire SetWarnLogger in NewAggregator:
    ```go
    dropclass.SetWarnLogger(logger)
    ```

    4. Change Run() signature:
    ```go
    func (a *Aggregator) Run(ctx context.Context, in <-chan *flowpb.Flow, out chan<- policy.PolicyEvent, healthCh chan<- DropEvent) error
    ```

    5. Insert classification gate in Run() AFTER --ignore-protocol block, BEFORE keyFromFlow:
    ```go
    // Classification gate (HEALTH-01/05): classify flow before bucketing.
    // flowsSeen counts all observed flows regardless of class (Pitfall 6).
    // Noise is silently discarded; Policy/Unknown proceed to keyFromFlow.
    class := dropclass.Classify(f.GetDropReasonDesc())
    switch class {
    case dropclass.DropClassInfra, dropclass.DropClassTransient, dropclass.DropClassUnknown:
        a.flowsSeen++
        a.infraDrops[f.GetDropReasonDesc()]++
        if healthCh != nil {
            healthCh <- buildDropEvent(f, class)
        }
        continue
    case dropclass.DropClassNoise:
        continue
    }
    // DropClassPolicy falls through to keyFromFlow
    ```

    6. buildDropEvent helper (unexported):
    ```go
    func buildDropEvent(f *flowpb.Flow, class dropclass.DropClass) DropEvent {
        ns, workload := "_unknown", "_unknown"
        if ep := effectiveEndpoint(f); ep != nil {
            if ep.Namespace != "" {
                ns = ep.Namespace
            }
            if len(ep.Labels) > 0 {
                workload = labels.WorkloadName(ep.Labels)
            }
        }
        node := f.GetNodeName()
        if node == "" {
            node = "_unknown"
        }
        return DropEvent{
            Reason:    f.GetDropReasonDesc(),
            Class:     class,
            Namespace: ns,
            Workload:  workload,
            NodeName:  node,
        }
    }
    ```

    7. effectiveEndpoint helper (unexported, mirrors keyFromFlow logic):
    ```go
    // effectiveEndpoint returns the policy-target endpoint for health event labeling.
    // Mirrors keyFromFlow direction logic; returns nil if direction is unknown.
    func effectiveEndpoint(f *flowpb.Flow) *flowpb.Endpoint {
        switch f.TrafficDirection {
        case flowpb.TrafficDirection_INGRESS:
            return f.Destination
        case flowpb.TrafficDirection_EGRESS:
            return f.Source
        default:
            return nil
        }
    }
    ```

    8. Add InfraDrops() and InfraDropTotal() accessors:
    ```go
    func (a *Aggregator) InfraDrops() map[flowpb.DropReason]uint64 {
        out := make(map[flowpb.DropReason]uint64, len(a.infraDrops))
        for k, v := range a.infraDrops {
            out[k] = v
        }
        return out
    }

    func (a *Aggregator) InfraDropTotal() uint64 {
        var total uint64
        for _, v := range a.infraDrops {
            total += v
        }
        return total
    }
    ```

    NOTE: pipeline.go still calls agg.Run(gctx, flows, policies) with the old 3-arg signature — this will be a compile error until plan 11-02 updates the call site. Add a temporary compile-time note or update in the same commit if needed. The safer approach: update pipeline.go call site in this plan to pass nil as healthCh, then plan 11-02 replaces nil with the real channel.

    Per CONTEXT.md: Unknown class flows go through to healthCh AND also fall through to keyFromFlow (they err on the side of generating CNPs). Re-reading: CONTEXT.md says "Policy + Unknown classes go through to keyFromFlow". So Unknown flows:
    - DO increment flowsSeen
    - Do NOT increment infraDrops
    - DO NOT send to healthCh
    - DO proceed to keyFromFlow

    Correction to the switch above for Unknown:
    ```go
    case dropclass.DropClassInfra, dropclass.DropClassTransient:
        a.flowsSeen++
        a.infraDrops[f.GetDropReasonDesc()]++
        if healthCh != nil {
            healthCh <- buildDropEvent(f, class)
        }
        continue
    case dropclass.DropClassNoise:
        continue
    // DropClassPolicy and DropClassUnknown fall through to keyFromFlow
    ```
  </implementation>
</feature>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Write failing tests (RED) — classification gate + counter invariant</name>
  <files>pkg/hubble/aggregator_test.go</files>
  <read_first>
    - pkg/hubble/aggregator_test.go (existing tests to understand patterns)
    - pkg/hubble/aggregator.go (current struct + Run signature)
    - pkg/dropclass/classifier.go (DropClass constants)
  </read_first>
  <behavior>
    Tests to write (all must FAIL before implementation):
    - TestAggregatorClassificationSuppression: CT_MAP_INSERTION_FAILED flow → 0 buckets, flowsSeen=1, infraDrops=1
    - TestAggregatorPolicyFlowPassthrough: POLICY_DENIED flow → 1 bucket, flowsSeen=1, infraDrops=0
    - TestAggregatorFlowsSeenInvariant: 5 policy + 3 infra → flowsSeen=8, infraDrops=3, 5 buckets
    - TestAggregatorNoiseDiscarded: NAT_NOT_NEEDED flow → 0 buckets, flowsSeen=0, infraDrops=0
    - TestAggregatorTransientCounted: STALE_OR_UNROUTABLE_IP flow → 0 buckets, flowsSeen=1, infraDrops=1
    - TestAggregatorHealthChReceivesDropEvent: infra flow with non-nil healthCh → DropEvent received with correct Reason/Class/NodeName
    - TestAggregatorHealthChNilSafe: infra flow with nil healthCh → no panic
    - TestAggregatorFilterPrecedence: flow matching --ignore-protocol + infra class → protocol counter increments, NOT infraDrops (proto filter fires first)
    - TestInfraDropTotal: verify sum convenience method
    - TestInfraDropsCopy: verify InfraDrops() returns independent copy
  </behavior>
  <action>
    Write test functions in pkg/hubble/aggregator_test.go. Tests call the NEW Run() signature `agg.Run(ctx, in, out, healthCh)` — they will fail to compile (or fail at runtime) until implementation.

    Build helper: `makeInfraFlow(reason flowpb.DropReason) *flowpb.Flow` that sets Verdict=DROPPED, DropReasonDesc=reason, NodeName="node-1", Source endpoint with Namespace="test" + labels for EGRESS direction.

    Run `go test ./pkg/hubble/... 2>&1 | head -30` to confirm compilation failure (expected at RED stage).
  </action>
  <verify>
    <automated>go test ./pkg/hubble/... 2>&1 | grep -E "FAIL|undefined|cannot use" | head -20</automated>
  </verify>
  <done>Tests exist and fail (compile error on new Run signature or undefined DropEvent).</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Implement classification gate (GREEN)</name>
  <files>pkg/hubble/aggregator.go, pkg/hubble/pipeline.go</files>
  <read_first>
    - pkg/hubble/aggregator.go (full file — modifying Run + struct)
    - pkg/hubble/pipeline.go (find agg.Run call site to patch to nil healthCh)
  </read_first>
  <behavior>
    All RED tests from Task 1 must pass. Additional invariants:
    - `go vet ./pkg/hubble/...` passes
    - `go test -race ./pkg/hubble/...` passes
    - `go build ./...` passes (pipeline.go call site updated to pass nil healthCh)
  </behavior>
  <action>
    Implement all changes described in the feature implementation section above.

    CRITICAL ordering in Run() loop:
    1. L7 count (UNCHANGED — before all gates)
    2. --ignore-protocol gate (UNCHANGED — continue on match)
    3. Classification gate (NEW — Infra/Transient → flowsSeen++, infraDrops++, healthCh send, continue; Noise → continue; Policy/Unknown fall through)
    4. keyFromFlow + bucket (flowsSeen incremented here for Policy/Unknown — note: for Policy/Unknown, flowsSeen is incremented inside the existing `if !skip` block AFTER keyFromFlow. For Infra/Transient, flowsSeen must be incremented BEFORE the continue in the new gate.)

    Also update pkg/hubble/pipeline.go Stage 1 goroutine to pass nil as healthCh:
    ```go
    g.Go(func() error {
        return agg.Run(gctx, flows, policies, nil)
    })
    ```
    This is a temporary nil until plan 11-02 creates the real channel.

    Also update SessionStats and the post-g.Wait() block in pipeline.go:
    ```go
    stats.InfraDropTotal     = agg.InfraDropTotal()
    stats.InfraDropsByReason = agg.InfraDrops()
    ```
    Add to SessionStats struct:
    ```go
    InfraDropTotal      uint64
    InfraDropsByReason  map[flowpb.DropReason]uint64
    ```
    Add to SessionStats.Log():
    ```go
    zap.Uint64("infra_drop_total", s.InfraDropTotal),
    zap.Any("infra_drops_by_reason", s.InfraDropsByReason),
    ```

    Wire dropclass.SetWarnLogger in NewAggregator (one-time, before any Run call):
    ```go
    dropclass.SetWarnLogger(logger)
    ```
    Add import: `"github.com/SoulKyu/cpg/pkg/dropclass"`
  </action>
  <verify>
    <automated>go test -race ./pkg/hubble/... -run "TestAggregator" -v 2>&1 | tail -20</automated>
  </verify>
  <done>
    All TestAggregator* tests pass -race. go vet clean. go build ./... succeeds.
    infraDrops counter correctly separates suppressed flows from policy flows.
    flowsSeen=8 for 5 policy + 3 infra input (Pitfall 6 invariant verified).
  </done>
</task>

</tasks>

<verification>
```bash
go test -race -count=1 ./pkg/hubble/... -run "TestAggregator" -v
go vet ./pkg/hubble/...
go build ./...
```

Spot-check counter invariant:
```bash
go test -race ./pkg/hubble/... -run "TestAggregatorFlowsSeenInvariant" -v
```
</verification>

<success_criteria>
- CT_MAP_INSERTION_FAILED flow → flowsSeen=1, infraDrops=1, 0 CNP buckets
- POLICY_DENIED flow → flowsSeen=1, infraDrops=0, 1 CNP bucket
- 5 policy + 3 infra → flowsSeen=8, infraDrops=3 (Pitfall 6 invariant)
- NAT_NOT_NEEDED (Noise) → flowsSeen=0, infraDrops=0, no healthCh send
- nil healthCh → no panic on infra flow
- go test -race ./pkg/hubble/... passes
- go build ./... passes (pipeline.go patched with nil healthCh)
</success_criteria>

<output>
After completion, create `.planning/phases/11-aggregator-suppression-and-health-writer/11-01-aggregator-classification-gate-SUMMARY.md`
</output>
