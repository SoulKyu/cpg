---
phase: 10-classifier-core
plan: 02
type: tdd
wave: 2
depends_on:
  - 10-01
files_modified:
  - pkg/dropclass/classifier.go
  - pkg/dropclass/classifier_test.go
autonomous: true
requirements:
  - CLASSIFY-02
must_haves:
  truths:
    - "Classify(flowpb.DropReason(9999)) returns DropClassUnknown and emits a WARN log"
    - "Calling Classify(9999) 100 times emits exactly one WARN across the session (dedup)"
    - "The WARN is never emitted for known (recognized) reasons"
    - "WarnLogger can be set once per process; zero-value (no logger) is safe (no panic)"
  artifacts:
    - path: "pkg/dropclass/classifier.go"
      provides: "WarnLogger setter + sync.Map dedup + warn-on-unknown path in Classify()"
      exports: ["SetWarnLogger"]
  key_links:
    - from: "pkg/dropclass/classifier.go"
      to: "go.uber.org/zap"
      via: "package-level *zap.Logger set via SetWarnLogger(); guarded by sync.Mutex or sync/atomic"
      pattern: "SetWarnLogger"
    - from: "pkg/hubble/aggregator.go"
      to: "pkg/dropclass/classifier.go"
      via: "aggregator will call dropclass.SetWarnLogger(a.logger) before Run() in phase 11"
      pattern: "warnedReserved"
---

<objective>
Extend `pkg/dropclass/classifier.go` with a session-scoped deduplicated WARN log for
unrecognized DropReason values. Unknown reasons already return DropClassUnknown (Plan 01);
this plan adds the once-per-unique-value WARN so operators see when cpg encounters an
unexpected Cilium version with new drop reasons.

Purpose: CLASSIFY-02 — unknown reasons are surfaced, not silently swallowed.
Output: SetWarnLogger() function + sync.Map dedup path in Classify(); new tests pass.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/10-classifier-core/10-CONTEXT.md
@.planning/phases/10-classifier-core/10-01-SUMMARY.md
</context>

<interfaces>
<!-- Key patterns from Plan 01 output and aggregator dedup model. -->

From pkg/dropclass/classifier.go (written in Plan 01):
```go
// Classify returns DropClassUnknown for unrecognized reasons (no WARN yet after Plan 01).
func Classify(reason flowpb.DropReason) DropClass

// This plan adds:
func SetWarnLogger(l *zap.Logger)
// + package-level sync.Map warnedUnknown and *zap.Logger warnLogger
```

From pkg/hubble/aggregator.go (dedup model to mirror):
```go
// warnedReserved map[string]struct{} — dedup: key checked before logging
// Phase 11 will wire: dropclass.SetWarnLogger(a.logger) before a.Run()
warnedReserved map[string]struct{}

// Pattern used:
if _, seen := a.warnedReserved[warnKey]; !seen {
    a.warnedReserved[warnKey] = struct{}{}
    a.logger.Warn("...", ...)
}
```

Concurrency note: Classify() will be called from multiple goroutines in phase 11
(aggregator Run() hot path). Use sync.Map.LoadOrStore — zero-alloc on the fast path
(known reasons exit before reaching the sync.Map).

Logger injection:
```go
import (
    "sync"
    "go.uber.org/zap"
    flowpb "github.com/cilium/cilium/api/v1/flow"
)

var (
    warnLogger    *zap.Logger  // nil = no-op (safe)
    warnLoggerMu  sync.RWMutex
    warnedUnknown sync.Map     // map[int32]struct{} — key = int32(reason)
)

func SetWarnLogger(l *zap.Logger) {
    warnLoggerMu.Lock()
    warnLogger = l
    warnLoggerMu.Unlock()
}
```
</interfaces>

<tasks>

<task type="tdd">
  <name>Task 1: Write failing tests for dedup WARN on unknown reason</name>
  <read_first>
    - pkg/dropclass/classifier_test.go (existing tests from Plan 01 — extend, do not replace)
    - pkg/dropclass/classifier.go (current state after Plan 01)
  </read_first>
  <files>pkg/dropclass/classifier_test.go</files>
  <behavior>
    Add to classifier_test.go (do NOT remove existing tests):

    - TestClassifyUnknownEmitsWarn:
      1. Create zaptest.NewLogger(t) (import "go.uber.org/zap/zaptest")
      2. Call dropclass.SetWarnLogger(logger)
      3. Call dropclass.Classify(flowpb.DropReason(9999)) — must return DropClassUnknown
      4. Assert logger observed exactly 1 warn-level log entry containing "unrecognized"
         (use zaptest/observer or zaptest buffer — prefer zaptest.NewLogger + assert via
         zap/zapcore or zaptest.LogEntries)

    - TestClassifyUnknownDedupWarn:
      1. Reset with a fresh observed logger (zaptest.NewLogger)
      2. Call dropclass.SetWarnLogger(logger)
      3. Call dropclass.Classify(flowpb.DropReason(8888)) 100 times in a loop
      4. Assert logger received exactly 1 warn entry for reason 8888 (not 100)

    - TestClassifyUnknownDedupPerValue:
      1. Two distinct unknown values: 8887 and 8886
      2. Call each 50 times
      3. Assert exactly 2 warn entries total (one per unique unknown value)

    - TestClassifyKnownNoWarn:
      1. Set observed logger
      2. Call Classify(flowpb.DropReason_POLICY_DENIED) 10 times
      3. Assert 0 warn entries — known reasons must never trigger the WARN path

    - TestClassifyNilLoggerSafe:
      1. Call dropclass.SetWarnLogger(nil)
      2. Call dropclass.Classify(flowpb.DropReason(7777)) — must NOT panic, must return DropClassUnknown

    Note: test isolation — each test that sets a logger must reset warnedUnknown state.
    Export a ResetWarnState() function (unexported is fine if tests are in package dropclass).
    If tests are in package dropclass_test, export it as dropclass.ResetWarnStateForTest() with
    a build tag `//go:build test` OR put it in a file named export_test.go:
      func ResetWarnStateForTest() { warnedUnknown.Range(func(k, _ any) bool { warnedUnknown.Delete(k); return true }) }
    Use zaptest/observer for assertion: import "go.uber.org/zap/zapcore" + "go.uber.org/zap/zaptest/observer"

    Commit: `test(10-02): add failing tests for deduplicated WARN on unknown drop reason`
  </behavior>
  <action>
    Extend pkg/dropclass/classifier_test.go with the five tests above.
    Do NOT modify classifier.go yet — go test must fail to compile (SetWarnLogger undefined).
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/dropclass/... 2>&1 | grep -E "undefined|FAIL|cannot" | head -5</automated>
  </verify>
  <done>go test fails to compile due to undefined SetWarnLogger — RED state confirmed.</done>
  <acceptance_criteria>
    - `go test ./pkg/dropclass/...` exits non-zero with "undefined: dropclass.SetWarnLogger" or similar
    - No existing tests are removed from classifier_test.go
    - export_test.go (or equivalent) exists for ResetWarnStateForTest
  </acceptance_criteria>
</task>

<task type="tdd">
  <name>Task 2: Implement SetWarnLogger + dedup WARN in Classify()</name>
  <read_first>
    - pkg/dropclass/classifier.go (Plan 01 implementation)
    - pkg/dropclass/classifier_test.go (tests just written — implement to make them pass)
  </read_first>
  <files>pkg/dropclass/classifier.go</files>
  <action>
    Extend pkg/dropclass/classifier.go (do NOT replace existing code):

    Add package-level vars at top of file (after existing map vars):
    ```go
    var (
        warnLogger   *zap.Logger
        warnLoggerMu sync.RWMutex
        warnedUnknown sync.Map // key: int32, value: struct{}
    )

    // SetWarnLogger sets the logger used to emit a single WARN per unique
    // unrecognized DropReason value. Safe to call before or after Classify.
    // Pass nil to disable warn logging (zero-value safe).
    func SetWarnLogger(l *zap.Logger) {
        warnLoggerMu.Lock()
        warnLogger = l
        warnLoggerMu.Unlock()
    }
    ```

    Extend Classify() to emit dedup WARN when returning DropClassUnknown:
    ```go
    func Classify(reason flowpb.DropReason) DropClass {
        if c, ok := dropReasonClass[reason]; ok {
            return c
        }
        // Unknown reason: return Unknown (never Policy — that would regress the CT_MAP bug).
        // Emit a single deduplicated WARN per unique unrecognized value so operators can
        // detect forward-compatibility issues with newer Cilium versions.
        key := int32(reason)
        if _, loaded := warnedUnknown.LoadOrStore(key, struct{}{}); !loaded {
            warnLoggerMu.RLock()
            l := warnLogger
            warnLoggerMu.RUnlock()
            if l != nil {
                l.Warn("unrecognized Cilium DropReason — classified as Unknown; consider upgrading cpg",
                    zap.Int32("drop_reason_code", key),
                )
            }
        }
        return DropClassUnknown
    }
    ```

    Add imports: "sync", "go.uber.org/zap"

    Create pkg/dropclass/export_test.go for test isolation:
    ```go
    package dropclass

    // ResetWarnStateForTest clears the dedup map between tests.
    // Only called from tests; not exported to production callers.
    func ResetWarnStateForTest() {
        warnedUnknown.Range(func(k, _ any) bool {
            warnedUnknown.Delete(k)
            return true
        })
    }
    ```

    Commit: `feat(10-02): add SetWarnLogger + dedup WARN for unrecognized drop reasons`
  </action>
  <verify>
    <automated>cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/dropclass/... -v -count=1 -race 2>&1 | tail -20</automated>
  </verify>
  <done>
    All tests pass including new dedup-WARN tests.
    -race detector reports no data races.
    Existing Plan 01 tests still pass (no regression).
  </done>
  <acceptance_criteria>
    - `go test ./pkg/dropclass/... -v -race 2>&1 | grep -E "^--- (PASS|FAIL)"` shows all PASS, zero FAIL
    - `go test ./pkg/dropclass/... -race 2>&1 | grep "DATA RACE"` returns empty (no race)
    - `grep "SetWarnLogger" pkg/dropclass/classifier.go` present
    - `grep "warnedUnknown" pkg/dropclass/classifier.go` present (sync.Map field)
    - `grep "LoadOrStore" pkg/dropclass/classifier.go` present (dedup mechanism)
    - `go vet ./pkg/dropclass/...` exits 0
    - All Plan 01 tests still pass: TestClassifyAllKnownReasons, TestClassifyPolicyReasons, etc.
  </acceptance_criteria>
</task>

</tasks>

<verification>
cd /home/gule/Workspace/team-infrastructure/cpg && go test ./pkg/dropclass/... -count=1 -race -v 2>&1 | grep -E "^(ok|FAIL|--- (PASS|FAIL))"
go vet ./pkg/dropclass/...
</verification>

<success_criteria>
- All pkg/dropclass tests pass with -race
- Classify(9999) returns DropClassUnknown and emits exactly 1 WARN per unique value
- SetWarnLogger(nil) is safe (no panic)
- No regression on Plan 01 tests
- Package builds with zero vet warnings
</success_criteria>

<output>
After completion, create `.planning/phases/10-classifier-core/10-02-SUMMARY.md`
</output>
