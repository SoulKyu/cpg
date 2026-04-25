---
phase: 08-http-l7-generation
plan: 03
subsystem: hubble
tags: [http, l7, pipeline, vis-01, evidence-v2, tdd]

# Dependency graph
requires:
  - phase: 08-http-l7-generation
    plan: 02
    provides: "AttributionOptions.L7Enabled gate + BuildPolicy HTTP codegen branch + L7-discriminated RuleKey"
  - phase: 07-l7-infrastructure-prep
    provides: "PipelineConfig.L7Enabled flag + RuleKey.L7 + evidence.L7Ref schema"
provides:
  - "PipelineConfig.L7Enabled forwarded into AttributionOptions via aggregator (HTTP-01 + HTTP-04 reachable from CLI)"
  - "Aggregator.SetL7Enabled / L7HTTPCount / L7DNSCount / FlowsSeen / ObservedWorkloads accessors"
  - "SessionStats.L7HTTPCount + L7DNSCount fields, populated post-Wait, logged in session summary"
  - "VIS-01: single empty-L7-records warning per pipeline run when --l7 set + flows>0 + zero L7 records"
  - "Evidence v2 L7Ref{Protocol:http, HTTPMethod, HTTPPath} populated by hubble/evidence_writer.convert"
  - "Side-effect: SessionStats.FlowsSeen no longer stuck at 0 (v1.0 audit BUG-01 incidentally fixed)"
  - "testdata/flows/l7_http.jsonl: 4-flow synthetic HTTP fixture for downstream replay tests"
affects:
  - "08-04 (replay end-to-end + integration test consuming the fixture)"
  - "Phase 9 DNS (one-line VIS-01 plug-in: l7DNSCount increment)"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Aggregator-side L7 counters with diagnostic-only semantics (incremented regardless of L7Enabled)"
    - "VIS-01 emitted from a single, deterministic site (post g.Wait, pre stats.Log) — no sync.Once needed"
    - "Sorted workload list in warning fields for deterministic test assertions"

key-files:
  created:
    - "pkg/hubble/pipeline_l7_test.go"
    - "testdata/flows/l7_http.jsonl"
  modified:
    - "pkg/hubble/pipeline.go"
    - "pkg/hubble/aggregator.go"
    - "pkg/hubble/evidence_writer.go"

key-decisions:
  - "L7HTTP counter increments unconditionally (independent of l7Enabled) so the VIS-01 gate correctly fires only when --l7 was requested AND no L7 records arrived. A counter that respected the flag would always be 0 under --l7=false, defeating the diagnostic."
  - "SessionStats counters are hydrated from the aggregator after g.Wait() rather than incremented inside the aggregator goroutine. This avoids racing the writer goroutines and keeps the pipeline.go orchestration as the single source of truth for SessionStats."
  - "VIS-01 sums HTTP + DNS counters even though Phase 8 leaves DNS at 0. This makes Phase 9 a one-line wire-up rather than a re-architect."
  - "Fixed the long-standing v1.0 BUG-01 (FlowsSeen always 0) as a Rule-2 deviation: VIS-01 needed an accurate flowsSeen > 0 gate, so leaving it at 0 would make the warning fire on empty-flow runs (false positive). Fix is contained to the aggregator increment in the input-channel branch of Run()."
  - "Evidence DNS branch is a TODO(phase-9) sentinel — the protocol switch is already in place so Phase 9 only adds the case body."

metrics:
  duration: "~4 min"
  completed_date: "2026-04-25"
  tasks_completed: 2
  files_modified: 3
  files_created: 2
  tests_added: 5
  tests_total_repo: 276
---

# Phase 8 Plan 03: Pipeline L7 Wiring + VIS-01 Summary

**One-liner:** Pipeline now forwards `--l7` into BuildPolicy via the aggregator, fires a single sorted-workloads VIS-01 warning on empty-L7-records, and populates evidence v2 `L7Ref{Protocol:"http",...}` for HTTP rules — closing VIS-01 and making HTTP-01/HTTP-04 reachable from the CLI.

## What Shipped

### Test fixture
- `testdata/flows/l7_http.jsonl` — 4 dropped INGRESS flows (production/frontend → production/api-server, TCP/8080) carrying `Flow.L7.Http`:
  1. `GET /api/v1/users`
  2. `POST /api/v1/users` (multi-method same path → exercises HTTP-04 merge)
  3. `GET /healthz`
  4. `get /api/v1/orders?id=42` (lowercase method + query string → exercises HTTP-02 normalize + HTTP-03 query strip)

### Aggregator (pkg/hubble/aggregator.go)
- New fields: `l7Enabled`, `l7HTTPCount`, `l7DNSCount`, `flowsSeen`, `seenWorkloads`.
- New methods: `SetL7Enabled`, `L7HTTPCount`, `L7DNSCount`, `FlowsSeen`, `ObservedWorkloads` (sorted).
- `Run()` increments `l7HTTPCount` on every flow with non-nil `Flow.L7.Http` (regardless of `l7Enabled`), increments `flowsSeen` and records the workload after `keyFromFlow` accepts the flow.
- `flush()` passes `AttributionOptions{L7Enabled: a.l7Enabled}` into `policy.BuildPolicy`.

### Pipeline (pkg/hubble/pipeline.go)
- `SessionStats` extended with `L7HTTPCount` + `L7DNSCount`; both logged in the session summary.
- `RunPipelineWithSource` now calls `agg.SetL7Enabled(cfg.L7Enabled)` after constructing the aggregator.
- After `g.Wait()`: hydrate `stats.{FlowsSeen,L7HTTPCount,L7DNSCount}` from the aggregator, then emit a single VIS-01 warning when `cfg.L7Enabled && flowsSeen > 0 && http+dns == 0`.
- VIS-01 message: `"--l7 set but no L7 records observed in window"` with fields `workloads ([]string, sorted)`, `flows (uint64)`, `hint ("see README L7 prerequisites: #l7-prerequisites")`.

### Evidence writer (pkg/hubble/evidence_writer.go)
- `convert()` switches on `a.Key.L7.Protocol`. For `"http"` it sets `re.L7 = &evidence.L7Ref{Protocol:"http", HTTPMethod, HTTPPath}`. For `"dns"` a `TODO(phase-9)` sentinel marks where Phase 9 plugs in.

## Tests

### `pkg/hubble/pipeline_l7_test.go` (5 tests, all green)
- `TestPipeline_L7HTTP_GeneratedAndEvidence`: replay the L7 fixture with `L7Enabled=true` + evidence; assert YAML has `rules:`/`http:` block, `method: GET/POST`, anchored regex paths, no `headerMatches`/`host`/`hostExact`; assert evidence file has at least one `RuleEvidence.L7{Protocol:"http", ...}`; assert VIS-01 does NOT fire.
- `TestPipeline_L7Empty_FiresWarning`: `L7Enabled=true` against L4-only `small.jsonl`; assert VIS-01 fires exactly once with the README anchor in `hint` and a non-empty `workloads` slice; assert YAML has no `http:` block.
- `TestPipeline_L7Disabled_NoWarning`: `L7Enabled=false` against `small.jsonl`; assert VIS-01 does NOT fire.
- `TestPipeline_L7Disabled_L7FlowsIgnored`: `L7Enabled=false` against the L7-bearing fixture; assert YAML has neither `http:` nor `rules:`, evidence carries no `L7Ref`, and VIS-01 does NOT fire.
- `TestPipeline_L7Enabled_NoFlows_NoWarning`: `L7Enabled=true` against `empty.jsonl`; assert VIS-01 does NOT fire (gate respects `flowsSeen > 0`).

## Verification

| Command | Result |
|---|---|
| `go build ./...` | success |
| `go vet ./...` | clean |
| `go test ./pkg/hubble/ -run TestPipeline_L7` | 5 passed |
| `go test ./...` | 276 passed across 9 packages |
| `go test ./cmd/cpg/ -run TestReplay_L7FlagByteStable` | passed (Phase 7 byte-stability invariant preserved) |
| Manual `cpg replay testdata/flows/l7_http.jsonl --l7` | CNP YAML with `rules.http` block, 4 rules emitted |
| Manual same command without `--l7` | L4-only YAML, no `rules:` sub-block |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed v1.0 audit BUG-01: SessionStats.FlowsSeen always 0**
- **Found during:** Task 2 implementation
- **Issue:** `SessionStats.FlowsSeen` had been declared and logged since v1.0 but never incremented anywhere. The plan's VIS-01 gate (`stats.FlowsSeen > 0`) would always evaluate false, suppressing the warning even on real empty-L7 sessions, while the gate `totalFlows == 0` for empty-input runs would also always be true — the gate was effectively undefined.
- **Fix:** Added `flowsSeen` counter on the aggregator, incremented in the input-channel branch of `Run()` after `keyFromFlow` accepts the flow. Pipeline hydrates `stats.FlowsSeen = agg.FlowsSeen()` after `g.Wait()`.
- **Files modified:** `pkg/hubble/aggregator.go`, `pkg/hubble/pipeline.go`
- **Commit:** `e260ebe`
- **Justification:** VIS-01 cannot work without an accurate `flowsSeen` count. This was the most contained fix consistent with the plan's "if not, add" instruction (line 220 of 08-03-PLAN.md).

## Known Stubs

None. The `evidence.L7Ref` DNS branch in `evidence_writer.convert` is a deliberate `TODO(phase-9)` sentinel inside a `switch` — it does not flow to UI, no DNS attributions exist yet (Phase 9 will produce them).

## Self-Check: PASSED

- Files created:
  - `pkg/hubble/pipeline_l7_test.go` — FOUND
  - `testdata/flows/l7_http.jsonl` — FOUND
- Files modified:
  - `pkg/hubble/pipeline.go` — FOUND
  - `pkg/hubble/aggregator.go` — FOUND
  - `pkg/hubble/evidence_writer.go` — FOUND
- Commits:
  - `d69896c` — FOUND (`test(08-03): add L7 fixture and failing pipeline integration tests`)
  - `e260ebe` — FOUND (`feat(08-03): pipeline L7 codegen + VIS-01 warning + evidence L7Ref`)
- Test suite: 276 passed, 0 failed, 0 skipped (other than empty fixture skip guard).
