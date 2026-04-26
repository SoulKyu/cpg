---
phase: 13-flags-and-exit-code
plan: "01"
type: tdd
wave: 1
depends_on: []
files_modified:
  - pkg/hubble/aggregator.go
  - pkg/hubble/aggregator_test.go
autonomous: true
requirements: [FILTER-01]

must_haves:
  truths:
    - "Flows whose DropReasonDesc matches an entry in ignoreDropReasons are skipped before the classification gate and do NOT increment flowsSeen, infraDrops, or reach healthCh"
    - "ignoredByDropReason per-reason counter incremented for each skipped flow"
    - "IgnoredByDropReason() accessor returns a copy (mirrors IgnoredByProtocol())"
    - "SetIgnoreDropReasons([]string) normalises to uppercase (canonical enum form) and populates aggregator field"
  artifacts:
    - path: "pkg/hubble/aggregator.go"
      provides: "SetIgnoreDropReasons, IgnoredByDropReason, Run() updated filter precedence"
      contains: "ignoreDropReasons map"
    - path: "pkg/hubble/aggregator_test.go"
      provides: "TDD tests for the new filter"
  key_links:
    - from: "Aggregator.Run()"
      to: "ignoreDropReasons map"
      via: "check at top of flow loop BEFORE ignoreProtocols check"
      pattern: "ignoreDropReasons"
---

<objective>
Add `--ignore-drop-reason` filter to the Aggregator: a map-based pre-classification gate that silently drops flows by exact DropReason match before they reach the classification gate or flowsSeen counter.

Purpose: FILTER-01 requires flows matching ignored reasons to be excluded from ALL output (no flowsSeen, no infraDrops, no healthCh, no CNP). This is distinct from the classification gate which counts infra/transient flows in flowsSeen.

Output: SetIgnoreDropReasons / IgnoredByDropReason on Aggregator; Run() updated filter precedence; failing then passing tests.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/phases/13-flags-and-exit-code/13-CONTEXT.md

@.planning/phases/11-aggregator-suppression-and-health-writer/11-01-aggregator-classification-gate-SUMMARY.md
</context>

<interfaces>
<!-- Key contracts from existing aggregator — executor must reference these directly. -->

From pkg/hubble/aggregator.go — existing filter precedence in Run() (lines 328–356):

```go
// PA5: drop ignored protocols BEFORE bucketing
if len(a.ignoreProtocols) > 0 {
    if name := flowL4ProtoName(f); name != "" {
        if _, drop := a.ignoreProtocols[name]; drop {
            a.ignoredByProtocol[name]++
            continue
        }
    }
}
// HEALTH-01/05: Classification gate
if f.Verdict == flowpb.Verdict_DROPPED && f.GetDropReasonDesc() != flowpb.DropReason_DROP_REASON_UNKNOWN {
    class := dropclass.Classify(f.GetDropReasonDesc())
    switch class {
    case dropclass.DropClassInfra, dropclass.DropClassTransient:
        a.flowsSeen++
        a.infraDrops[f.GetDropReasonDesc()]++
        if healthCh != nil { healthCh <- buildDropEvent(f, class) }
        continue
    case dropclass.DropClassNoise:
        continue
    }
}
```

New filter MUST be inserted BEFORE the ignoreProtocols check (filter precedence from CONTEXT.md decisions):
1. NEW: `--ignore-drop-reason` check (first, user explicit exclusion)
2. existing `--ignore-protocol` check
3. existing classification gate

From pkg/hubble/aggregator.go — existing pattern fields (lines 100–113):

```go
ignoreProtocols    map[string]struct{}
ignoredByProtocol  map[string]uint64
infraDrops         map[flowpb.DropReason]uint64
```

From pkg/hubble/aggregator.go — ValidIgnoreProtocols / SetIgnoreProtocols pattern (lines 37–56, 137–147):

```go
var validIgnoreProtocols = map[string]struct{}{ "tcp": {}, ... }
func ValidIgnoreProtocols() []string { ... sort.Strings(out); return out }
func (a *Aggregator) SetIgnoreProtocols(protos []string) {
    set := make(map[string]struct{}, len(protos)); for _, p := range protos { set[strings.ToLower(p)] = {} }
    a.ignoreProtocols = set
}
func (a *Aggregator) IgnoredByProtocol() map[string]uint64 { /* copy */ }
```

New methods to add (mirror exact patterns):
- `SetIgnoreDropReasons(reasons []string)` — stores UPPERCASE reason names as map keys
- `IgnoredByDropReason() map[string]uint64` — returns copy of per-reason counter
- The counter key is the UPPERCASE reason name string (matches ValidReasonNames() canonical form)

From pkg/dropclass/classifier.go — ValidReasonNames() (line 299):
```go
func ValidReasonNames() []string { /* sorted copy of flowpb.DropReason_name values */ }
```

Note: flowpb.DropReason_name is map[int32]string where values are UPPERCASE names like "CT_MAP_INSERTION_FAILED".
The aggregator must convert flow reason to name for counter key:
```go
flowpb.DropReason_name[int32(f.GetDropReasonDesc())]
```
This is already the pattern used in health_writer.go for sorted drops.

From pkg/hubble/aggregator_test.go — test helper patterns:
- `makeFlow(dir, reason)` helpers already exist
- Existing Run() signature: `agg.Run(ctx, in, out, healthCh)`
</interfaces>

<tasks>

<task type="tdd">
  <name>Task 1: RED — Write failing tests for SetIgnoreDropReasons + Run() filter</name>
  <files>pkg/hubble/aggregator_test.go</files>
  <behavior>
    - TestAggregatorIgnoreDropReasonFilter: flow with ignored reason is skipped; flowsSeen=0, infraDrops empty, no healthCh send, no CNP bucket created
    - TestAggregatorIgnoreDropReasonCounter: two flows with same ignored reason → ignoredByDropReason["CT_MAP_INSERTION_FAILED"]=2
    - TestAggregatorIgnoreDropReasonPrecedence: ignored reason fires BEFORE ignoreProtocols (even if both would match, reason filter increments only ignoredByDropReason, not ignoredByProtocol)
    - TestAggregatorIgnoreDropReasonCaseInsensitive: SetIgnoreDropReasons([]string{"ct_map_insertion_failed"}) normalises to "CT_MAP_INSERTION_FAILED" and correctly skips a matching flow
    - TestAggregatorIgnoreDropReasonNonMatching: flow with a different reason than ignored list passes through normally (no counter increment)
    - TestIgnoredByDropReasonCopy: IgnoredByDropReason() returns a copy; mutating the returned map does not affect internal state
  </behavior>
  <action>
    Write all tests in pkg/hubble/aggregator_test.go. Tests MUST fail (RED) because SetIgnoreDropReasons / IgnoredByDropReason do not exist yet.

    Run to confirm RED:
    ```
    cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/... 2>&1 | head -20
    ```

    Commit: `test(13-01): add failing tests for ignore-drop-reason aggregator filter`
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/... 2>&1 | grep -E "FAIL|does not compile|undefined"</automated>
  </verify>
  <done>At least 6 new test functions exist, package fails to compile or tests fail with "undefined" errors on SetIgnoreDropReasons/IgnoredByDropReason</done>
</task>

<task type="tdd">
  <name>Task 2: GREEN — Implement SetIgnoreDropReasons + Run() filter</name>
  <files>pkg/hubble/aggregator.go</files>
  <behavior>
    All 6 new tests pass. All existing aggregator tests continue to pass. go vet clean.
  </behavior>
  <action>
    Add to pkg/hubble/aggregator.go:

    1. Two new fields on Aggregator struct (after ignoredByProtocol):
    ```go
    // ignoreDropReasons is the uppercase set of DropReason name strings
    // whose flows must be dropped before classification. Populated via
    // SetIgnoreDropReasons (phase 13 FILTER-01).
    ignoreDropReasons map[string]struct{}

    // ignoredByDropReason counts flows dropped via --ignore-drop-reason per
    // reason name (uppercase canonical form). Surfaced via IgnoredByDropReason().
    ignoredByDropReason map[string]uint64
    ```

    2. Initialise in NewAggregator():
    ```go
    ignoredByDropReason: make(map[string]uint64),
    ```

    3. Add SetIgnoreDropReasons (mirror SetIgnoreProtocols exactly, but UPPERCASE not lowercase):
    ```go
    func (a *Aggregator) SetIgnoreDropReasons(reasons []string) {
        if len(reasons) == 0 {
            a.ignoreDropReasons = nil
            return
        }
        set := make(map[string]struct{}, len(reasons))
        for _, r := range reasons {
            set[strings.ToUpper(r)] = struct{}{}
        }
        a.ignoreDropReasons = set
    }
    ```

    4. Add IgnoredByDropReason (mirror IgnoredByProtocol exactly):
    ```go
    func (a *Aggregator) IgnoredByDropReason() map[string]uint64 {
        out := make(map[string]uint64, len(a.ignoredByDropReason))
        for k, v := range a.ignoredByDropReason {
            out[k] = v
        }
        return out
    }
    ```

    5. Insert new filter block in Run() BEFORE the existing ignoreProtocols block:
    ```go
    // FILTER-01: drop flows matching --ignore-drop-reason BEFORE protocol
    // filter and classification gate. User-explicit exclusion takes
    // precedence. These flows do NOT increment flowsSeen, infraDrops, or
    // reach healthCh.
    if len(a.ignoreDropReasons) > 0 {
        name := flowpb.DropReason_name[int32(f.GetDropReasonDesc())]
        if name != "" {
            if _, drop := a.ignoreDropReasons[name]; drop {
                a.ignoredByDropReason[name]++
                continue
            }
        }
    }
    ```

    Placement: IMMEDIATELY before the `if len(a.ignoreProtocols) > 0` block (around line 329).

    Run all tests to confirm GREEN:
    ```
    cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/... -race -count=1
    ```

    Commit: `feat(13-01): add ignore-drop-reason filter to aggregator`
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/hubble/... -race -count=1 -v 2>&1 | grep -E "PASS|FAIL|panic" | tail -5</automated>
  </verify>
  <done>All pkg/hubble tests pass with -race. go vet ./pkg/hubble/... clean. IgnoredByDropReason() and SetIgnoreDropReasons() exported and callable.</done>
</task>

</tasks>

<verification>
```bash
cd /home/gule/Workspace/team-infrastructure/cpg
go test ./pkg/hubble/... -race -count=1
go vet ./pkg/hubble/...
go build ./...
```
All pass.
</verification>

<success_criteria>
- SetIgnoreDropReasons([]string) and IgnoredByDropReason() map[string]uint64 exported on Aggregator
- Run() filter precedence: ignoreDropReasons → ignoreProtocols → classification gate
- A flow with an ignored reason: flowsSeen unchanged, infraDrops unchanged, no healthCh send, ignoredByDropReason[name]++
- Input normalisation: lowercase input → uppercase key in internal map
- All existing pkg/hubble tests still pass
- go build ./... clean
</success_criteria>

<output>
After completion, create `/home/gule/Workspace/team-infrastructure/cpg/.planning/phases/13-flags-and-exit-code/13-01-aggregator-ignore-drop-reason-SUMMARY.md`
</output>
