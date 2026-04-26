---
phase: 08-http-l7-generation
plan: 04
subsystem: testing
tags: [cli, e2e, replay, l7, http, vis-01, cobra, zap-observer]

requires:
  - phase: 08-http-l7-generation
    provides: HTTP codegen, VIS-01 wiring, evidence v2 L7Ref population, l7_http.jsonl fixture
provides:
  - End-to-end CLI-level replay tests covering HTTP-01..HTTP-05 + VIS-01 acceptance criteria
  - Negative invariant: --l7=false on L7-bearing fixture is byte-identical to no flag
  - README #l7-prerequisites anchor placeholder for VIS-01 hint link
affects: [09-dns-l7-generation, 09-docs-and-prerequisites]

tech-stack:
  added: []
  patterns:
    - "cmd-level observed logger via initObservedLoggerForTesting + zap/zaptest/observer"
    - "logger restoration via t.Cleanup in initLoggerForTesting"

key-files:
  created: []
  modified:
    - cmd/cpg/replay_test.go
    - cmd/cpg/testhelpers_test.go
    - README.md

key-decisions:
  - "Reuse existing l7_http.jsonl fixture from Plan 08-03 — no new fixture needed"
  - "Inject observed logger via package-level logger var swap (mirror of initLoggerForTesting) rather than refactor PipelineConfig plumbing"
  - "Place README placeholder before Dry-run section so workflow-related sections cluster together; full content lands in Phase 9"

patterns-established:
  - "Observed-logger swap pattern: initObservedLoggerForTesting returns *observer.ObservedLogs, restores prev logger via t.Cleanup"
  - "E2E byte-stability check via walkRelFiles + bytes.Equal per relative path"

requirements-completed: [HTTP-01, HTTP-02, HTTP-03, HTTP-04, HTTP-05, VIS-01]

duration: 12min
completed: 2026-04-25
---

# Phase 8 Plan 4: E2E Replay Tests for HTTP L7 + VIS-01 Anchor

**End-to-end `cpg replay --l7` tests asserting HTTP rule emission, byte-stable L4-only fallback, and VIS-01 warning firing — plus the README anchor that closes the VIS-01 hint loop.**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-04-25T08:09:00Z
- **Completed:** 2026-04-25T08:21:26Z
- **Tasks:** 2
- **Files modified:** 3

## Accomplishments

- `TestReplay_L7HTTPGeneration` exercises the full CLI surface: cobra command → FileSource → pipeline → CNP YAML on disk → evidence v2 with `L7Ref{Protocol:http,...}`. Asserts every Phase 8 success criterion (anchored regex paths, normalized methods, no headerMatches/host/hostExact, query-param stripping, evidence L7Ref).
- `TestReplay_L7HTTP_DisabledByteStable` locks the negative invariant: even on the L7-bearing fixture, `--l7=false` must produce byte-identical YAML to no-flag invocation, with no `http:` block leaking through.
- `TestReplay_L7HTTP_EmptyFixtureFiresWarning` proves VIS-01 fires exactly once on the L4-only fixture under `--l7`, with the hint pointing to `#l7-prerequisites` and the message containing `--l7` verbatim.
- README `#l7-prerequisites` anchor placeholder added — closes the VIS-01 hint-link loop ahead of Phase 9's full content.
- Phase 7's `TestReplay_L7FlagByteStable` continues to pass (no regression on the v1.1 byte-stability invariant).

## Task Commits

1. **Task 1: e2e replay tests for HTTP generation + VIS-01** — `db25609` (test)
2. **Task 2: README #l7-prerequisites anchor placeholder** — `f6a9744` (docs)

## Files Created/Modified

- `cmd/cpg/replay_test.go` — three new tests appended (HTTP generation, byte-stable disabled, VIS-01 empty-fixture warning).
- `cmd/cpg/testhelpers_test.go` — added `initObservedLoggerForTesting` returning `*observer.ObservedLogs`; updated `initLoggerForTesting` to restore the previous logger via `t.Cleanup`.
- `README.md` — inserted `## L7 Prerequisites` placeholder section with HTML anchor `<a id="l7-prerequisites"></a>` before the Dry-run section.

## Decisions Made

- **Logger restoration in `initLoggerForTesting`:** added `t.Cleanup` to restore the previous logger. Previously the helper leaked the no-op logger into subsequent tests; the new `initObservedLoggerForTesting` would otherwise have been silently overwritten by parallel/sequential tests.
- **Anchor placement in README:** placed before Dry-run rather than at the end so the workflow-related sections cluster together. Phase 9 can expand this section in place without restructuring.

## Deviations from Plan

**1. [Rule 2 - Missing Critical] `initLoggerForTesting` did not restore the previous logger**
- **Found during:** Task 1 (designing the observed-logger helper)
- **Issue:** Adding `initObservedLoggerForTesting` alongside the existing `initLoggerForTesting` would create a sticky-state bug: if a test calling `initLoggerForTesting` runs after one that swapped in an observed logger, both helpers leak state across tests.
- **Fix:** Added `t.Cleanup` to both helpers to restore the previous package-level `logger`.
- **Files modified:** `cmd/cpg/testhelpers_test.go`
- **Verification:** Full `go test ./...` green (279 tests).
- **Committed in:** `db25609` (Task 1 commit).

---

**Total deviations:** 1 auto-fixed (1 missing critical correctness)
**Impact on plan:** No scope creep. Necessary for test-isolation correctness.

## Issues Encountered

None.

## Verification Matrix

| Check | Result |
| --- | --- |
| `go test ./cmd/cpg/ -run TestReplay_L7 -v` | 4 GREEN (parses, default-false, FlagByteStable, HTTPGeneration, DisabledByteStable, EmptyFixtureFiresWarning) |
| `go test ./...` | 279 GREEN across 9 packages |
| `go vet ./...` | clean |
| `grep -n l7-prerequisites README.md pkg/hubble/pipeline.go` | both present, exact anchor match |

## Phase 8 Acceptance — All Criteria Now CLI-Observable

- HTTP-01: HTTP rule extraction → `TestReplay_L7HTTPGeneration` (`http:` block present)
- HTTP-02: Method casing normalization → `TestReplay_L7HTTPGeneration` (`method: GET` from lowercase `get` fixture)
- HTTP-03: Anchored regex paths → `TestReplay_L7HTTPGeneration` (`^/api/v1/users$`, `^/healthz$`, `^/api/v1/orders$` after query strip)
- HTTP-04: Multi-(method, path) merge → `TestReplay_L7HTTPGeneration` (single port rule carries multiple http entries)
- HTTP-05: No headerMatches/host/hostExact → `TestReplay_L7HTTPGeneration` (negative assertions)
- VIS-01: Empty L7 records warning → `TestReplay_L7HTTP_EmptyFixtureFiresWarning` (exactly-once + #l7-prerequisites hint)

## Next Phase Readiness

- Phase 8 closed. v1.2 Phase 9 (DNS L7 generation + docs) can now expand the `#l7-prerequisites` anchor with the actual two-step workflow content; the link target is reserved.
- VIS-01 emits a single hint string (`see README L7 prerequisites: #l7-prerequisites`); Phase 9 should preserve this exact substring or update it in lockstep with `pkg/hubble/pipeline.go:203`.
- DNS L7 codegen will reuse the same observed-logger pattern in `cmd/cpg` for VIS-01-style tests over DNS records.

---
*Phase: 08-http-l7-generation*
*Completed: 2026-04-25*

## Self-Check: PASSED

- `cmd/cpg/replay_test.go` — modified, present
- `cmd/cpg/testhelpers_test.go` — modified, present
- `README.md` — modified, present (`l7-prerequisites` anchor confirmed on line 178)
- `.planning/phases/08-http-l7-generation/08-04-SUMMARY.md` — created
- Commit `db25609` — verified in `git log`
- Commit `f6a9744` — verified in `git log`
- `go test ./...` — 279 passed in 9 packages
- `go vet ./...` — clean
