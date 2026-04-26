---
phase: 10-classifier-core
plan: 01
type: tdd
wave: 1
depends_on: []
files_modified:
  - pkg/dropclass/classifier.go
  - pkg/dropclass/classifier_test.go
  - pkg/dropclass/hints.go
  - pkg/dropclass/hints_test.go
  - pkg/dropclass/version.go
autonomous: true
requirements:
  - CLASSIFY-01
  - CLASSIFY-03
must_haves:
  truths:
    - "Classify(flowpb.DropReason_POLICY_DENIED) returns DropClassPolicy"
    - "Classify(flowpb.DropReason_CT_MAP_INSERTION_FAILED) returns DropClassInfra"
    - "Classify(flowpb.DropReason_DROP_HOST_NOT_READY) returns DropClassTransient"
    - "Classify(flowpb.DropReason_NAT_NOT_NEEDED) returns DropClassNoise"
    - "Every key in flowpb.DropReason_name maps to a non-zero (non-Unknown) DropClass"
    - "RemediationHint(CT_MAP_INSERTION_FAILED) returns a non-empty docs.cilium.io URL"
    - "ClassifierVersion equals '1.0.0-cilium1.19.1'"
    - "ValidReasonNames() returns sorted list of all enum name strings"
    - "BenchmarkClassifyReason reports < 50 ns/op"
  artifacts:
    - path: "pkg/dropclass/classifier.go"
      provides: "DropClass enum, Classify(), ValidReasonNames()"
      exports: ["DropClass", "DropClassPolicy", "DropClassInfra", "DropClassTransient", "DropClassNoise", "DropClassUnknown", "Classify", "ValidReasonNames"]
    - path: "pkg/dropclass/classifier_test.go"
      provides: "Full taxonomy coverage + benchmark"
    - path: "pkg/dropclass/hints.go"
      provides: "RemediationHint() + per-reason URL table"
      exports: ["RemediationHint"]
    - path: "pkg/dropclass/hints_test.go"
      provides: "URL format validation for every mapped hint"
    - path: "pkg/dropclass/version.go"
      provides: "ClassifierVersion constant"
      exports: ["ClassifierVersion"]
  key_links:
    - from: "pkg/dropclass/classifier.go"
      to: "github.com/cilium/cilium/api/v1/flow"
      via: "map[flowpb.DropReason]DropClass O(1) lookup"
      pattern: "dropReasonClass\\[reason\\]"
    - from: "pkg/dropclass/hints.go"
      to: "pkg/dropclass/classifier.go"
      via: "RemediationHint uses same flowpb.DropReason key type"
      pattern: "dropReasonHint\\[reason\\]"
---

<objective>
Create the `pkg/dropclass/` package: DropClass enum, O(1) taxonomy map covering all 76 Cilium
v1.19.1 DropReason values, RemediationHint URL table, ClassifierVersion constant, and
ValidReasonNames() helper. All tests pass with complete enumerated coverage.

Purpose: Phase 10 foundation — every downstream phase (11-13) imports this package.
Output: 5 files in pkg/dropclass/, go test ./pkg/dropclass/... passes, bench < 50 ns/op.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/phases/10-classifier-core/10-CONTEXT.md
@.planning/research/FEATURES.md
</context>

<interfaces>
<!-- Cilium v1.19.1 types the executor needs directly. No codebase exploration needed. -->

From github.com/cilium/cilium/api/v1/flow (go.mod: cilium/cilium v1.19.1):
```go
// flowpb import alias used project-wide
import flowpb "github.com/cilium/cilium/api/v1/flow"

type DropReason int32

// Iterate all known values via:
//   for code, name := range flowpb.DropReason_name { ... }
// or access by constant:
var _ = flowpb.DropReason_POLICY_DENIED          // 133
var _ = flowpb.DropReason_CT_MAP_INSERTION_FAILED // 155
var _ = flowpb.DropReason_DROP_REASON_UNKNOWN     // 0

// DropReason_name is a map[int32]string of all enum values.
var DropReason_name map[int32]string
```

From pkg/hubble/aggregator.go (dedup-warn pattern to mirror in Plan 02):
```go
// warnedReserved map[string]struct{} — dedup WARN once per unique key
// plan 02 mirrors this with sync.Map for concurrent Classify() calls
warnedReserved map[string]struct{}
```

Module path: github.com/SoulKyu/cpg (go.mod)
Go version: 1.25.1
Logger: go.uber.org/zap (injected, NOT package-global — but Classify() is a pure function;
        dedup WARN belongs in Plan 02 which adds a package-level sync.Map)
</interfaces>

<tasks>

<task type="tdd">
  <name>Task 1: Write failing tests for classifier taxonomy + hints + version</name>
  <read_first>
    - .planning/research/FEATURES.md (full taxonomy table, sections 1 and edge-cases)
    - .planning/phases/10-classifier-core/10-CONTEXT.md (decisions section)
  </read_first>
  <files>pkg/dropclass/classifier_test.go, pkg/dropclass/hints_test.go</files>
  <behavior>
    - TestClassifyAllKnownReasons: iterates every key in flowpb.DropReason_name, calls Classify(),
      asserts result != DropClassUnknown (every known reason must have an explicit bucket)
    - TestClassifyPolicyReasons: POLICY_DENIED(133), POLICY_DENY(181), AUTH_REQUIRED(189),
      DENIED_BY_LB_SRC_RANGE_CHECK(177) → each returns DropClassPolicy
    - TestClassifyInfraReasons: CT_MAP_INSERTION_FAILED(155), FIB_LOOKUP_FAILED(169),
      SERVICE_BACKEND_NOT_FOUND(158) → each returns DropClassInfra
    - TestClassifyTransientReasons: STALE_OR_UNROUTABLE_IP(151), INVALID_IDENTITY(171),
      DROP_EP_NOT_READY(203), DROP_HOST_NOT_READY(202) → each returns DropClassTransient
    - TestClassifyNoiseReasons: NAT_NOT_NEEDED(173), IS_A_CLUSTERIP(174), IGMP_HANDLED(199),
      IGMP_SUBSCRIBED(200), MULTICAST_HANDLED(201), DROP_PUNT_PROXY(205) → DropClassNoise
    - TestClassifyUnknownReason: Classify(flowpb.DropReason(9999)) → DropClassUnknown
    - TestClassifierVersion: assert dropclass.ClassifierVersion == "1.0.0-cilium1.19.1"
    - TestValidReasonNames: len > 0, sorted (each element < next), all values appear in
      flowpb.DropReason_name values set
    - TestRemediationHintInfraReasons: RemediationHint(CT_MAP_INSERTION_FAILED) non-empty,
      starts with "https://docs.cilium.io"
    - TestRemediationHintNonInfra: RemediationHint(POLICY_DENIED) == "" (hints only for infra)
    - TestRemediationHintAllInfraHaveURLs: iterate every reason classified Infra, assert
      RemediationHint returns non-empty string starting with "https://"
    - BenchmarkClassifyReason: b.N iterations of Classify(flowpb.DropReason_CT_MAP_INSERTION_FAILED),
      no allocs expected
  </behavior>
  <action>
    Create pkg/dropclass/classifier_test.go and pkg/dropclass/hints_test.go with package dropclass.
    Import flowpb "github.com/cilium/cilium/api/v1/flow" and testing.
    All test functions reference dropclass.Classify, dropclass.ClassifierVersion,
    dropclass.ValidReasonNames, dropclass.RemediationHint — these do not exist yet so `go test`
    must fail to compile. That is the expected RED state.
    Commit: `test(10-01): add failing tests for drop-reason classifier + hints`
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/dropclass/... 2>&1 | grep -E "FAIL|cannot|undefined|build" | head -10</automated>
  </verify>
  <done>go test fails to compile with "undefined: dropclass.Classify" or similar — RED state confirmed.</done>
  <acceptance_criteria>
    - `go test ./pkg/dropclass/...` exits non-zero with build error mentioning Classify or DropClass
    - Both test files exist: pkg/dropclass/classifier_test.go and pkg/dropclass/hints_test.go
    - No stub implementation files exist yet
  </acceptance_criteria>
</task>

<task type="tdd">
  <name>Task 2: Implement classifier taxonomy, hints map, and version constant</name>
  <read_first>
    - .planning/research/FEATURES.md (canonical classification table — all 76 rows)
    - pkg/dropclass/classifier_test.go (just written — implement against these tests)
  </read_first>
  <files>
    pkg/dropclass/classifier.go,
    pkg/dropclass/hints.go,
    pkg/dropclass/version.go
  </files>
  <action>
    Create pkg/dropclass/version.go:
    ```go
    package dropclass

    // ClassifierVersion identifies the taxonomy version embedded in cluster-health.json.
    // Bump manually when bucket assignments change. Format: semver-ciliumVERSION.
    const ClassifierVersion = "1.0.0-cilium1.19.1"
    ```

    Create pkg/dropclass/classifier.go:
    - Package dropclass; imports flowpb "github.com/cilium/cilium/api/v1/flow", "sort"
    - DropClass type (int):
      ```go
      type DropClass int

      const (
          DropClassUnknown  DropClass = iota // 0 — unrecognized reason; never Policy
          DropClassPolicy                    // 1 — absent/misconfigured CNP; cpg generates policy
          DropClassInfra                     // 2 — datapath/infra failure; surface in health JSON
          DropClassTransient                 // 3 — startup race or normal CT transition; count only
          DropClassNoise                     // 4 — internal bookkeeping; ignore entirely
      )
      ```
    - dropReasonClass map[flowpb.DropReason]DropClass — package-level var, populated in init()
      or as a var literal. Map must contain ALL 76 reasons from FEATURES.md table:
      - Policy (4): 133 POLICY_DENIED, 181 POLICY_DENY, 189 AUTH_REQUIRED,
        177 DENIED_BY_LB_SRC_RANGE_CHECK
        Add comment on AUTH_REQUIRED: "// SPIRE mTLS required; could be SPIRE infra misconfiguration — v1.4 review"
      - Noise (6): 173 NAT_NOT_NEEDED, 174 IS_A_CLUSTERIP, 199 IGMP_HANDLED,
        200 IGMP_SUBSCRIBED, 201 MULTICAST_HANDLED, 205 DROP_PUNT_PROXY
      - Transient (8): 0 DROP_REASON_UNKNOWN, 151 STALE_OR_UNROUTABLE_IP,
        152 NO_MATCHING_LOCAL_CONTAINER_FOUND, 165 NO_CONFIGURATION_AVAILABLE_TO_PERFORM_POLICY_DECISION,
        171 INVALID_IDENTITY, 172 UNKNOWN_SENDER, 196 TTL_EXCEEDED,
        202 DROP_HOST_NOT_READY, 203 DROP_EP_NOT_READY
      - All remaining ~50 reasons: DropClassInfra (see FEATURES.md table for exact list:
        130-134, 135-150 excl. 138+148+149 deprecated still mapped, 153-157, 158, 160-164,
        166-170, 175-176, 178-180, 182-188, 190-198, 204)
    - Classify(reason flowpb.DropReason) DropClass:
      ```go
      func Classify(reason flowpb.DropReason) DropClass {
          if c, ok := dropReasonClass[reason]; ok {
              return c
          }
          return DropClassUnknown
      }
      ```
    - validReasonNamesSorted []string — package-level var built in init() from
      flowpb.DropReason_name values, sorted. Used by ValidReasonNames().
    - ValidReasonNames() []string:
      ```go
      func ValidReasonNames() []string {
          out := make([]string, len(validReasonNamesSorted))
          copy(out, validReasonNamesSorted)
          return out
      }
      ```

    Create pkg/dropclass/hints.go:
    - dropReasonHint map[flowpb.DropReason]string — URL per infra reason
      Every infra-classified reason MUST have an entry. Non-infra reasons MUST NOT.
      Use https://docs.cilium.io/en/stable/operations/troubleshooting/ as base.
      Key entries:
        155 CT_MAP_INSERTION_FAILED → "https://docs.cilium.io/en/stable/operations/troubleshooting/#handling-drop-ct-map-insertion-failed"
        158 SERVICE_BACKEND_NOT_FOUND → "https://docs.cilium.io/en/stable/operations/troubleshooting/#service-backend-not-found"
        169 FIB_LOOKUP_FAILED → "https://docs.cilium.io/en/stable/operations/troubleshooting/#fib-lookup-failed"
        195 UNENCRYPTED_TRAFFIC → "https://docs.cilium.io/en/stable/operations/encryption/"
        194 NO_EGRESS_GATEWAY → "https://docs.cilium.io/en/stable/network/egress-gateway/egress-gateway-troubleshooting/"
        204 DROP_NO_EGRESS_IP → "https://docs.cilium.io/en/stable/network/egress-gateway/egress-gateway-troubleshooting/"
        For all other infra reasons without specific pages: "https://docs.cilium.io/en/stable/operations/troubleshooting/"
    - RemediationHint(reason flowpb.DropReason) string:
      ```go
      func RemediationHint(reason flowpb.DropReason) string {
          return dropReasonHint[reason] // "" for non-infra reasons
      }
      ```

    After implementation, commit: `feat(10-01): implement drop-reason classifier, hints map, version`
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/dropclass/... -v -run . -bench BenchmarkClassifyReason -benchtime=1s 2>&1 | tail -20</automated>
  </verify>
  <done>
    All tests pass (PASS). BenchmarkClassifyReason shows ns/op < 50.
    `go vet ./pkg/dropclass/...` exits 0.
  </done>
  <acceptance_criteria>
    - `go test ./pkg/dropclass/... -v 2>&1 | grep -E "^--- (PASS|FAIL)"` shows all PASS, zero FAIL
    - `go test ./pkg/dropclass/... -bench=BenchmarkClassifyReason -benchtime=1s 2>&1 | grep "ns/op"` shows value < 50
    - `grep -r "DropClassUnknown\|DropClassPolicy\|DropClassInfra\|DropClassTransient\|DropClassNoise" pkg/dropclass/classifier.go | wc -l` ≥ 5 (all 5 constants exported)
    - `grep "ClassifierVersion" pkg/dropclass/version.go` contains "1.0.0-cilium1.19.1"
    - `grep "ValidReasonNames" pkg/dropclass/classifier.go` present
    - `go vet ./pkg/dropclass/...` exits 0
    - `grep -c "DropClassInfra" pkg/dropclass/classifier.go` ≥ 40 (taxonomy map has ~50 infra entries)
  </acceptance_criteria>
</task>

</tasks>

<verification>
cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/dropclass/... -count=1 -race 2>&1 | tail -5
go vet ./pkg/dropclass/...
</verification>

<success_criteria>
- pkg/dropclass/ package compiles with zero errors
- All classifier_test.go and hints_test.go tests pass
- TestClassifyAllKnownReasons: every flowpb.DropReason_name key maps to a non-Unknown class
- BenchmarkClassifyReason: < 50 ns/op (O(1) map lookup confirmed)
- ClassifierVersion == "1.0.0-cilium1.19.1"
- ValidReasonNames() returns sorted non-empty list matching flowpb.DropReason_name values
- All infra-classified reasons have a non-empty RemediationHint URL
- go vet passes, -race passes
</success_criteria>

<output>
After completion, create `.planning/phases/10-classifier-core/10-01-SUMMARY.md`
</output>
