---
phase: 12-session-summary-block
plan: "01"
subsystem: hubble
tags: [tdd, stdout, summary, health, infra-drops]
dependency_graph:
  requires: [11-02-health-writer-and-pipeline-wiring]
  provides: [PrintClusterHealthSummary, healthWriter.Snapshot, PipelineConfig.Stdout]
  affects: [pkg/hubble/pipeline.go, pkg/hubble/health_writer.go, pkg/hubble/summary.go]
tech_stack:
  added: []
  patterns: [TDD-RED-GREEN, io.Writer-injection, sort-by-class-then-count, top3-truncation]
key_files:
  created:
    - pkg/hubble/summary.go
    - pkg/hubble/summary_test.go
  modified:
    - pkg/hubble/health_writer.go
    - pkg/hubble/pipeline.go
decisions:
  - "PrintClusterHealthSummary takes io.Writer (not os.Stdout directly) for testability; nil in PipelineConfig.Stdout defaults to os.Stdout at call site"
  - "Snapshot() added to healthWriter as nil-safe method returning []HealthDropSnapshot (shallow copy) — avoids re-reading cluster-health.json"
  - "Sorting strategy: DropClass enum value as proxy for severity (infra=1 < transient=2); descending count within same class"
  - "top3 helper: stable secondary sort by name ensures deterministic output for equal counts"
  - "No cmd/cpg changes needed: Stdout nil-default means generate.go and replay.go work as-is"
metrics:
  duration_seconds: 146
  completed_date: "2026-04-26T20:48:59Z"
  tasks_completed: 2
  files_changed: 4
  tests_added: 9
  total_tests: 391
requirements_satisfied: [HEALTH-03]
---

# Phase 12 Plan 01: Session Summary Block Summary

**One-liner:** `━`-framed stdout summary block after every run with >=1 infra/transient drop, sorted infra-before-transient with top-3 node/workload contributors and RemediationHint URLs.

## What Was Built

- `pkg/hubble/summary.go` — `PrintClusterHealthSummary(out io.Writer, snapshots []HealthDropSnapshot, stats *SessionStats, healthPath string, dryRun bool)` with `top3()` helper
- `pkg/hubble/health_writer.go` — `HealthDropSnapshot` type, `Snapshot()` nil-safe method, `shallowCopyMap()` helper
- `pkg/hubble/pipeline.go` — `Stdout io.Writer` field on `PipelineConfig`; HEALTH-03 wiring after `hw.finalize(stats)`
- `pkg/hubble/summary_test.go` — 9 TDD tests covering full block, zero drops, nil snapshots, single contributor, >3 contributors (+2 more), dry-run suffix, no hint, severity sort, within-class count sort

## Commits

| Task | Type | Hash | Description |
|------|------|------|-------------|
| 1 (RED) | test | 2f636f2 | add failing tests for PrintClusterHealthSummary |
| 2 (GREEN) | feat | 5fd943e | implement PrintClusterHealthSummary + healthWriter.Snapshot() |

## Output Format

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
! Cluster-critical drops detected (NOT a policy issue)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  CT_MAP_INSERTION_FAILED                [infra]  47 flows
    Top nodes:     node-a-1 (32), node-b-2 (12), node-c-3 (3)
    Top workloads: team-trading/mmtro-adserver (28), team-data/x (15), team-foo/y (4)
    Hint: https://docs.cilium.io/en/stable/operations/troubleshooting/#handling-drop-ct-map-insertion-failed

  POLICY_DENIED                          [transient]  5 flows
    Top nodes:     node-a-1 (5)
    Top workloads: team-trading/butler (5)

cluster-health.json: /home/gule/.cache/cpg/evidence/<hash>/cluster-health.json
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

## Decisions Made

- `io.Writer` injection pattern (not `os.Stdout` direct call) — test isolation without OS redirection
- `Snapshot()` nil-safe — dry-run code paths return nil cleanly; `PrintClusterHealthSummary` no-ops on nil/empty
- DropClass integer value used as severity proxy: `DropClassInfra=1 < DropClassTransient=2`
- `cmd/cpg/generate.go` and `cmd/cpg/replay.go` unchanged — `Stdout: nil` defaults to `os.Stdout` inside pipeline

## Deviations from Plan

None - plan executed exactly as written.

## Known Stubs

None.

## Self-Check: PASSED
