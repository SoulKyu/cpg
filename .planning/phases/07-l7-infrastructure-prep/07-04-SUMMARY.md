---
phase: 07-l7-infrastructure-prep
plan: 04
subsystem: cli
tags: [cli, cobra, l7, flag-plumbing, byte-stability, preflight, kubernetes-fake]

requires:
  - phase: 07-l7-infrastructure-prep
    plan: 03
    provides: "pkg/k8s.RunL7Preflight (warn-and-proceed VIS-04 + VIS-05 cluster checks)"
provides:
  - "--l7 flag (Bool, default false) on `cpg generate` AND `cpg replay`"
  - "--no-l7-preflight flag (Bool, default false) on `cpg generate` ONLY (replay rejects it)"
  - "hubble.PipelineConfig.L7Enabled bool field (consumed nowhere in Phase 7 — no-op codegen)"
  - "maybeRunL7Preflight() testing seam in cmd/cpg/generate.go (gate matrix: l7Enabled && !noPreflight)"
  - "TestReplay_L7FlagByteStable: byte-identical YAML output asserted for --l7=false vs --l7=true on the same v1.1 fixture"
affects: [08-* (HTTP codegen lights up L7Enabled), 09-* (DNS codegen lights up L7Enabled)]

tech-stack:
  added: []
  patterns:
    - "Package-level swappable factory variable (l7ClientFactory) for kubernetes.Interface so tests substitute kubernetes/fake clientsets without DI plumbing through every call site."
    - "Tree-equality comparison helpers (assertTreesByteEqual, assertTreesSameShape, walkRelFiles, stripFirstSegment) for testing byte-stable codegen across runs that differ only in flag values."

key-files:
  created: []
  modified:
    - cmd/cpg/commonflags.go
    - cmd/cpg/generate.go
    - cmd/cpg/replay.go
    - cmd/cpg/generate_test.go
    - cmd/cpg/replay_test.go
    - pkg/hubble/pipeline.go
    - pkg/hubble/pipeline_test.go

key-decisions:
  - "Testing seam = package-level `var l7ClientFactory = kubernetes.NewForConfig`. Tests swap the factory via t.Cleanup-restored helper. Avoids leaking client-construction concerns into PipelineConfig and keeps the public command surface untouched."
  - "--no-l7-preflight is generate-only. Replay is offline by definition; adding the flag there would invite confusion (skip what?). Cobra surfaces unknown-flag errors automatically — TestReplayCmd_RejectsNoL7PreflightFlag asserts the rejection."
  - "PipelineConfig gains L7Enabled but NO consumer wires it in Phase 7. Aggregator and BuildPolicy signatures are unchanged. Phase 8 will add a constructor parameter when HTTP codegen actually consumes the flag."
  - "Byte-stability test uses testdata/flows/small.jsonl (an existing v1.1 fixture). No new fixture needed."
  - "Evidence-tree comparison reduced to shape-only because session UUID and timestamps legitimately differ run-to-run regardless of --l7. The byte-stability invariant is about CODEGEN (CNP YAML), not session-stamped sidecar JSON."

patterns-established:
  - "withFakeL7ClientFactory(t, fakeClient) — t.Cleanup-managed factory swap so multiple subtests can share a single helper."
  - "observedLoggerForTest() returns (*zap.Logger, *observer.ObservedLogs) for asserting on emitted warnings."

requirements-completed: [L7CLI-01, VIS-06]

metrics:
  duration_minutes: 12
  tasks_completed: 2
  files_modified: 7
  tests_added: 15
  tests_passing: 231
  completed_at: 2026-04-25T07:50:00Z
---

# Phase 7 Plan 04: --l7 / --no-l7-preflight Flag Plumbing + PipelineConfig.L7Enabled + Byte-Stability Integration Test Summary

Plumb the `--l7` flag end-to-end (CLI → commonFlags → PipelineConfig.L7Enabled) and the `--no-l7-preflight` flag from `cpg generate` to a single-call site in front of the pipeline. Phase 7 keeps L7Enabled as a no-op consumer; Phase 8 (HTTP) and Phase 9 (DNS) light up the codegen branch.

## What Shipped

### Flag Declarations (`cmd/cpg/commonflags.go`)
- `--l7` (Bool, default false) added to `addCommonFlags`. Long flag only — no short alias per CONTEXT decision.
- `commonFlags.l7` field; `parseCommonFlags` reads via `f.GetBool("l7")`.

### Generate-Only Flag (`cmd/cpg/generate.go`)
- `--no-l7-preflight` (Bool, default false) declared in `newGenerateCmd` after `addCommonFlags`. Not present on `cpg replay`.
- `generateFlags.noL7Preflight` field; `parseGenerateFlags` reads it.
- `maybeRunL7Preflight(ctx, kubeConfig, l7Enabled, noPreflight, logger)` extracted as package-level helper. Called from `runGenerate` AFTER kubeConfig is (or could be) loaded and BEFORE `hubble.RunPipeline`. Pre-flight is invoked iff `l7Enabled && !noPreflight`. Pre-flight is purely advisory: any failure to construct a client is logged as a warning and the pipeline proceeds.
- `L7Enabled: f.l7` wired into the `hubble.PipelineConfig` literal.

### Replay (`cmd/cpg/replay.go`)
- `--l7` inherited via `addCommonFlags`. NO pre-flight call site — replay is offline by definition.
- `L7Enabled: f.l7` wired into the `hubble.PipelineConfig` literal.

### PipelineConfig (`pkg/hubble/pipeline.go`)
- `L7Enabled bool` field added with godoc `// L7Enabled: no-op in v1.2 Phase 7; Phase 8 (HTTP) and Phase 9 (DNS) light up codegen.`
- No consumer in pipeline.go, aggregator, or builder. Phase 8 will introduce the consumer.

## Testing Seam

`l7ClientFactory` is a package-level `var` of type `func(*rest.Config) (kubernetes.Interface, error)`. Production initializes it to `kubernetes.NewForConfig`; tests swap it for one returning a `fake.NewSimpleClientset(...)` via `withFakeL7ClientFactory(t, client)`. The helper restores the previous factory on `t.Cleanup`, so subtests that share the same fake client can run in any order.

This avoids:
- DI plumbing through every call frame between `runGenerate` and `RunL7Preflight`.
- Leaking client-construction into `hubble.PipelineConfig`.
- Touching the public `cobra.Command` surface from tests.

## Byte-Stability Test

Location: `cmd/cpg/replay_test.go::TestReplay_L7FlagByteStable`.

Fixture: `testdata/flows/small.jsonl` (existing v1.1 jsonpb capture, 3 dropped flows: production/api-server ingress + production/db egress).

Procedure:
1. Run `cpg replay --l7=false` and `cpg replay --l7=true` against the same fixture into two separate `t.TempDir()` output directories.
2. Compare policy trees via `assertTreesByteEqual` — same set of relative paths AND every file byte-identical (`bytes.Equal`).
3. Compare evidence trees via `assertTreesSameShape` — same set of relative paths AFTER stripping the leading hash dir (the hash differs because output dirs differ; this is expected and unrelated to `--l7`). Evidence file CONTENTS legitimately differ run-to-run via session UUID and timestamps; the byte-stability invariant applies only to CNP YAML codegen.

If this test ever fails in Phase 7, the foundation for Phase 8/9 is broken. Phase 8 will keep the test passing for `--l7=false` runs (no L7 records → fall back to v1.1 codegen) and replace it with a fixture-diff test for `--l7=true` runs once HTTP codegen lights up.

## Test Suite Outcome

- Baseline before plan: 216 tests passing in 9 packages.
- After plan: 231 tests passing in 9 packages (+15 new).
- No regressions, no skipped tests.
- New tests:
  - `TestParseGenerateFlags_L7` (4 subtests: defaults, --l7 alone, --no-l7-preflight alone, both).
  - `TestMaybeRunL7Preflight_Gating` (4 subtests covering the gate matrix).
  - `TestReplayCmd_RejectsNoL7PreflightFlag`.
  - `TestReplayCmd_L7FlagParses`.
  - `TestReplayCmd_L7DefaultIsFalse`.
  - `TestReplay_L7FlagByteStable`.
  - `TestPipelineConfig_L7EnabledFieldExists`.

## Commits

- `b506a7c` — `feat(cli): plumb --l7 + --no-l7-preflight (no-op codegen, Phase 8 lights up)`
- `88f9c07` — `test(cli): byte-stability invariant for --l7 no-op + preflight gating tests (L7CLI-01, VIS-06)`

## Contract for Phase 8

Phase 8 will:
1. Add an L7Enabled-aware constructor parameter to the aggregator (or a setter mirroring `SetMaxSamples`).
2. Replace the no-op godoc on `PipelineConfig.L7Enabled` with the actual codegen branch description.
3. Update `TestReplay_L7FlagByteStable` so `--l7=true` runs against a fixture WITH `Flow.L7.Http` records produce DIFFERENT (HTTP-augmented) YAML, while `--l7=false` runs against the same fixture remain byte-identical to v1.1 output.
4. Wire VIS-01 (passive empty-L7-records detection) into the aggregator's L7-ingestion path.

## Deviations from Plan

None — plan executed exactly as written. The only minor judgment call was the evidence-tree shape comparison: the plan's "tree-by-tree byte equality of the output directories" wording was interpreted as applying to CNP YAML output (codegen) only, with evidence sidecar JSON compared by shape because session UUID + timestamps differ legitimately run-to-run regardless of `--l7`. This preserves the spirit of the byte-stability invariant (codegen is byte-stable) while not falsely failing on session-stamped state.

## Self-Check: PASSED

- `cmd/cpg/commonflags.go` — modified (l7 field + flag declared + parsed): FOUND.
- `cmd/cpg/generate.go` — modified (--no-l7-preflight + maybeRunL7Preflight + L7Enabled wired): FOUND.
- `cmd/cpg/replay.go` — modified (L7Enabled wired, no preflight): FOUND.
- `pkg/hubble/pipeline.go` — modified (L7Enabled field added): FOUND.
- `cmd/cpg/generate_test.go` — extended (TestParseGenerateFlags_L7, TestMaybeRunL7Preflight_Gating, TestReplayCmd_RejectsNoL7PreflightFlag): FOUND.
- `cmd/cpg/replay_test.go` — extended (TestReplayCmd_L7FlagParses/Default, TestReplay_L7FlagByteStable, helpers): FOUND.
- `pkg/hubble/pipeline_test.go` — extended (TestPipelineConfig_L7EnabledFieldExists): FOUND.
- Commit `b506a7c` (feat plumb): FOUND in `git log`.
- Commit `88f9c07` (test byte-stability): FOUND in `git log`.
- `go test ./... -count=1`: 231 passed, 0 failed.
