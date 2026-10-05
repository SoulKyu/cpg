---
phase: 26-namespace-creation-awareness
plan: 01
subsystem: pipeline
tags: [go, hubble, replay, namespaces, session-summary, tdd, byte-identity]

requires:
  - phase: 12-session-summary-block
    provides: PrintClusterHealthSummary seam + resolved cfg.Stdout local in RunPipelineWithSource
  - phase: 13-flags-and-exit-code
    provides: FILTER-01 ignoreDropReasons continue site in the aggregator Run loop (recording must precede it)
provides:
  - NSAW-02 offline replay heuristic: per-namespace first-seen recording in the Aggregator (both endpoints, A5)
  - Pure helpers in pkg/hubble/ns_awareness.go — noteFirstSeen, computeNewNamespaces (capture start = min Flow.Time, A4), PrintNewNamespacesSummary (no-op-when-empty byte-identity guard)
  - Aggregator.FirstSeenByNamespace() copy-returning accessor (M-3 single-goroutine contract)
  - End-of-run new-namespace report on the cfg.Stdout summary seam after PrintClusterHealthSummary (plan 26-02's watcher plugs into the same seam/print signature)
  - Fixture testdata/flows/ns_staggered.jsonl (default/production at capture start, latecomer at +10m)
affects: [26-02 live watcher, docs KNOWN_LIMITATIONS §9 update]

actuals:
  tokens: 6100
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "no-op-when-empty print guard (byte-identity mechanism, mirrors summary.go:41-42)"
    - "M-3 single-goroutine map + copy-returning accessor (mirrors infraDrops)"
    - "TDD RED with minimal compile stubs + gsd tdd-red-evidence gate (RED_EVIDENCE_OK)"

key-files:
  created:
    - pkg/hubble/ns_awareness.go
    - pkg/hubble/ns_awareness_test.go
    - pkg/hubble/pipeline_nsaw_test.go
    - testdata/flows/ns_staggered.jsonl
  modified:
    - pkg/hubble/aggregator.go
    - pkg/hubble/pipeline.go

key-decisions:
  - "Capture start DEFINED as min observed Flow.Time across the capture (A4) — never file mtime or wall-clock"
  - "Both source and destination endpoint namespaces recorded (A5 existence-awareness); empty names skipped"
  - "RED commit includes minimal compile stubs so every commit builds; RED validity proven via gsd check tdd-red-evidence (RED_EVIDENCE_OK, target tests failed on assertions)"
  - "Sequential executor committed on master (project convention for this single-checkout repo; protected-branch guard documented as a known deviation of this dispatch)"

patterns-established:
  - "NSAW report copy is planner-pinned: header 'New namespaces detected during this run:' + per-ns hint line; do not paraphrase"
  - "First-seen recorded BEFORE the FILTER-01/protocol-filter continues — unconditional per-flow bookkeeping"

requirements-completed: [NSAW-02]

coverage:
  - id: D1
    description: "Replay of ns_staggered.jsonl reports exactly 'latecomer' with the pinned copy line (NSAW-02 / ROADMAP SC3)"
    requirement: NSAW-02
    verification:
      - kind: integration
        ref: "pkg/hubble/pipeline_nsaw_test.go#TestPipelineNewNamespaces"
        status: pass
      - kind: unit
        ref: "pkg/hubble/ns_awareness_test.go#TestFirstSeenNamespaces"
        status: pass
    human_judgment: false
  - id: D2
    description: "Zero new namespaces leaves the v1.6 summary byte-identical — zero NSAW bytes over small.jsonl (ROADMAP SC1)"
    requirement: NSAW-02
    verification:
      - kind: integration
        ref: "pkg/hubble/pipeline_nsaw_test.go#TestSummaryByteIdentity"
        status: pass
      - kind: unit
        ref: "pkg/hubble/ns_awareness_test.go#TestPrintNewNamespacesSummary_EmptyAndNil"
        status: pass
    human_judgment: false
  - id: D3
    description: "Nil/invalid/zero Flow.Time fail-safe: nothing recorded, all-untimestamped capture reports nothing, never crashes"
    requirement: NSAW-02
    verification:
      - kind: unit
        ref: "pkg/hubble/ns_awareness_test.go#TestFirstSeenZeroTime"
        status: pass
    human_judgment: false
  - id: D4
    description: "Deterministic sorted report + strict capture-start-equality exclusion + empty-name guard"
    requirement: NSAW-02
    verification:
      - kind: unit
        ref: "pkg/hubble/ns_awareness_test.go#TestFirstSeenNamespaces_CaptureStartEquality"
        status: pass
      - kind: unit
        ref: "pkg/hubble/ns_awareness_test.go#TestFirstSeenNamespaces_SortedDeterministic"
        status: pass
      - kind: unit
        ref: "pkg/hubble/ns_awareness_test.go#TestFirstSeenNamespaces_EmptyNamespaceName"
        status: pass
    human_judgment: false

duration: 9 min
completed: 2026-10-05
status: complete
---

# Phase 26 Plan 01: Namespace Creation Awareness Summary

**NSAW-02 replay path end-to-end: the Aggregator records per-namespace first-seen timestamps (both endpoints), a pure helper computes the strictly-after-capture-start list (min Flow.Time, A4), and a no-op-when-empty report block rides the v1.3 cfg.Stdout summary seam — golden-proven by a staggered fixture replay and byte-identity-tested against the v1.6 summary.**

## Performance

- **Duration:** 9 min
- **Started:** 2026-10-05T07:34:14Z
- **Completed:** 2026-10-05T07:42:39Z
- **Tasks:** 2 (both tdd="true"; Task 1 tracer)
- **Files modified:** 6 (4 created, 2 modified)

## Accomplishments
- Full NSAW-02 tracer seam proven end-to-end: FileSource replay → aggregator first-seen recording → post-g.Wait() copy accessor → pure computation → `PrintNewNamespacesSummary` on the SAME resolved `stdout` local as `PrintClusterHealthSummary` (no MCP-specific branch; SC4 seam preserved).
- Byte-identity mechanism implemented and tested: `if len(namespaces) == 0 { return }` as the print function's FIRST statement; `TestSummaryByteIdentity` proves a small.jsonl replay writes zero NSAW bytes (ROADMAP SC1 zero-new-namespaces half).
- All probe edges closed with green tests: nil/invalid/zero timestamps (Pitfall 3), capture-start equality (strict After adjacency), sorted determinism, empty-namespace-name guard, both-endpoint attribution (A5).
- TDD discipline held: RED committed first with minimal compile stubs, validity machine-checked via `gsd check tdd-red-evidence` → `RED_EVIDENCE_OK`, then GREEN implementation.

## Task Commits

1. **Task 1 (tracer) RED: failing NSAW-02 tests** - `c84930e` (test)
2. **Task 1 (tracer) GREEN: NSAW-02 implementation** - `5899380` (feat)
3. **Task 2: edge + byte-identity tests** - `1da3a1a` (test)

_No REFACTOR commit: the GREEN implementation needed no cleanup (minimal, mirrors summary.go/aggregator.go analogs directly)._

## Files Created/Modified
- `pkg/hubble/ns_awareness.go` (created) — `NewNamespaceSource` + both consts; `noteFirstSeen` (both endpoints, empty-name guard, nil/IsValid/IsZero skip, min-keep); `computeNewNamespaces` (min-value capture start, strict After, sort.Strings, nil for degenerate inputs); `PrintNewNamespacesSummary` (no-op-when-empty guard FIRST, defensive sort, pinned copy)
- `pkg/hubble/aggregator.go` (modified) — `firstSeenByNamespace map[string]time.Time` field with M-3 single-goroutine doc (mirrors infraDrops/l7Collapse wording); `FirstSeenByNamespace()` copy-returning accessor with sort-at-call-site note; Run-loop `noteFirstSeen(a.firstSeenByNamespace, f)` immediately after the auditVerdictCount block, BEFORE the FILTER-01 continue, with lazy map allocation for zero-value construction
- `pkg/hubble/pipeline.go` (modified) — `PrintNewNamespacesSummary(stdout, computeNewNamespaces(agg.FirstSeenByNamespace()), NewNamespacesFromReplay)` immediately after `PrintClusterHealthSummary`, reusing the resolved `stdout` local (plan 26-02 wraps this in the watcher-precedence branch)
- `pkg/hubble/ns_awareness_test.go` (created) — TestFirstSeenNamespaces (+EmptyAndSingle, CaptureStartEquality, SortedDeterministic, EmptyNamespaceName), TestFirstSeenZeroTime, TestPrintNewNamespacesSummary_ReplayCopy/EmptyAndNil
- `pkg/hubble/pipeline_nsaw_test.go` (created) — TestPipelineNewNamespaces (FileSource end-to-end), TestSummaryByteIdentity (byte-identical provenance comment + companion computeNewNamespaces assertion)
- `testdata/flows/ns_staggered.jsonl` (created) — three lines copying small.jsonl's shape: default→production at 14:00:00 and 14:05:00, latecomer→production at 14:10:00

## Decisions Made
- **A4 pinned in code + docs:** capture start = minimum observed `Flow.Time` across the capture.
- **A5 pinned in code + docs:** both source and destination endpoint namespaces recorded (existence awareness); empty names skipped.
- **Nil-map-safety placement:** the pinned `noteFirstSeen(map, flow)` signature cannot reallocate a nil map, so `Run()` lazily allocates `firstSeenByNamespace` immediately before the recording call; `noteFirstSeen` additionally records nothing on a nil map (belt and braces for zero-value construction).
- **RED commit carries minimal compile stubs** so every commit builds; stub validity was NOT trusted — the RED gate required the target tests to fail on behavior assertions, machine-verified via `gsd check tdd-red-evidence` (verdict `RED_EVIDENCE_OK`).
- **Report-list assumption surfaced (flagged per must_haves):** when multiple namespaces share the minimum flow timestamp at capture start, ALL are treated as pre-existing (firstSeen == captureStart ⇒ not reported). Documented in `computeNewNamespaces` doc comment and TestFirstSeenNamespaces_CaptureStartEquality.

## Deviations from Plan

None - plan executed exactly as written.

**Total deviations:** 0 auto-fixed.
**Impact on plan:** none. (Process notes, not deviations: sequential-mode commits landed on `master` per this repo's single-checkout convention and the orchestrator's explicit dispatch instruction — the generic protected-branch guard is designed for parallel worktree executors; Task 2's tests all passed first run, so it has no RED counterpart — it is an edge-coverage test task over the Task-1 implementation, as its `<action>` anticipated: "No production-code changes are expected in this task".)

## Issues Encountered
None. Full `pkg/hubble` suite (610+ tests) stayed green under `-race` after both tasks; the -race suite confirms no data race on `firstSeenByNamespace`.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Plan 26-02 plugs into the exact seam this plan built: `PrintNewNamespacesSummary(stdout, list, source)` with `NewNamespacesFromWatch` already declared so the print signature never changes. 26-02 adds `pkg/k8s/ns_watch.go`, `PipelineConfig.NewNamespaces NewNamespacesSource`, and the watcher-precedence branch around the unconditional replay call in `RunPipelineWithSource` (marked with a comment at the print site).
- The full-suite gate `go test ./... -count=1 -race` is intentionally deferred to after 26-02 per the plan's `<verification>`.

## Self-Check: PASSED

- Files exist: pkg/hubble/ns_awareness.go, pkg/hubble/ns_awareness_test.go, pkg/hubble/pipeline_nsaw_test.go, testdata/flows/ns_staggered.jsonl, modified pkg/hubble/aggregator.go + pkg/hubble/pipeline.go — all verified on disk.
- Commits verified in git log: c84930e, 5899380, 1da3a1a.
- Both task verify commands re-run green under -race; no `no tests to run` warnings in the -run filters; full package green.

---
*Phase: 26-namespace-creation-awareness*
*Completed: 2026-10-05*
