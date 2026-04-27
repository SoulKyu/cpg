---
phase: quick-260427-aml-v1-3-code-review-fixes
plan: 01
subsystem: pkg/dropclass, pkg/hubble, cmd/cpg
tags: [hardening, code-review, tdd, non-blocking, levenshtein, refactor]
dependency-graph:
  requires: []
  provides: [QFIX-C1, QFIX-C2, QFIX-C3, QFIX-I1, QFIX-I2, QFIX-I3, QFIX-I4, QFIX-I5, QFIX-I7, QFIX-I8, QFIX-M1, QFIX-M2, QFIX-M3, QFIX-M4, QFIX-M5, QFIX-M6, QFIX-M7]
  affects: [pipeline, aggregator, health_writer, summary, classifier, hints, commonflags]
tech-stack:
  added: [sync.Once for idempotent Snapshot, Levenshtein DP helper, SummaryPathState enum]
  patterns: [non-blocking select with default + counter, adaptive fmt.Fprintf %-*s, cobra PreRunE]
key-files:
  created:
    - pkg/dropclass/version_test.go
  modified:
    - pkg/hubble/aggregator.go
    - pkg/hubble/aggregator_test.go
    - pkg/hubble/pipeline.go
    - pkg/hubble/pipeline_test.go
    - pkg/hubble/health_writer.go
    - pkg/hubble/health_writer_test.go
    - pkg/hubble/summary.go
    - pkg/hubble/summary_test.go
    - pkg/dropclass/classifier.go
    - pkg/dropclass/classifier_test.go
    - pkg/dropclass/hints.go
    - pkg/dropclass/hints_test.go
    - cmd/cpg/commonflags.go
    - cmd/cpg/commonflags_test.go
    - cmd/cpg/generate.go
    - cmd/cpg/replay.go
    - README.md
decisions:
  - "SummaryPathState 3-value enum (Written/DryRun/EvidenceOff) replaces bool dryRun to cleanly express the no-evidence path"
  - "Levenshtein top-5 suggestions use zero-dependency inline DP — no new import"
  - "Snapshot() uses sync.Once + atomic.Bool dual gate: Once protects concurrent first callers, atomic.Bool provides lock-free fast path on subsequent calls"
  - "policyTargetEndpoint returns nil for both unknown-direction and nil-endpoint; caller re-checks direction to preserve distinct tracker reason codes"
  - "M1 deep-link policy: non-empty hints must NOT equal the bare troubleshooting/ page URL (not requiring '#' anchor — egress-gateway and encryption pages are specific enough)"
metrics:
  duration: ~90 minutes
  completed: 2026-04-27
  tasks: 7
  files: 17
---

# Quick Task 260427-aml: v1.3 Code Review Fixes — Summary

**One-liner:** 16 atomic post-ship fixes (3 critical, 7 important, 6 minor) from the superpowers code review — non-blocking healthCh, SummaryPathState enum, Levenshtein suggestions, DropClass.String() dedup, Snapshot idempotency, tie-boundary top-N, adaptive summary width, and policyTargetEndpoint refactor.

## Fixes Shipped (16/16)

| ID | Task | Fix |
|----|------|-----|
| C1 | 1 | Non-blocking healthCh send with `select { case healthCh<-ev: default: a.healthChDrops++ }` |
| C2 | 1 | Fallback snapshot from agg counters when hw==nil and InfraDropTotal>0 |
| C3 | 2 | SummaryPathState enum (Written/DryRun/EvidenceOff) — no misleading path under --no-evidence |
| I1 | 2 | SetWarnLogger godoc documents process-global constraint and nil-safe usage |
| I2 | 3 | validateCommonFlags() wired as cobra PreRunE on generate and replay commands |
| I3 | 3 | Levenshtein top-5 suggestions in --ignore-drop-reason error messages |
| I4 | 4+7 | Snapshot() idempotent via sync.Once + atomic.Bool gate; race-safe for concurrent callers |
| I5 | 4 | DropClass.String() single source of truth; dropClassString() and dropClassLabel() removed |
| I7 | 1 | Defensive `, ok` pattern on flowpb.DropReason_name lookup in aggregator FILTER-01 |
| I8 | 5 | top3() extends limit through all entries tied at the boundary count |
| M1 | 6 | dropReasonHint stripped to 6 deep-link entries; generic page URLs → "" (omitempty in JSON) |
| M2 | 6 | README CI example: `timeout --preserve-status 300 cpg generate` with rationale note |
| M3 | 7 | policyTargetEndpoint() helper deduplicates INGRESS/EGRESS switch across buildDropEvent + keyFromFlow |
| M4 | 7 | TestClassifierVersionMatchesGoMod reads go.mod, fails loudly on cilium version drift |
| M5 | 5 | Adaptive summaryWidth: minReasonNameWidth=38, maxReasonNameWidth=60; %-*s format |
| M6 | 5 | summary_test fixtures use STALE_OR_UNROUTABLE_IP (real Transient); init() drift guard |
| M7 | 7 | Stage 1b explicit close ordering (policyCh, evidenceCh, healthCh) — no defer reversal risk |

## Test Count Delta

- Before: ~421 tests (10 packages)
- After: 442 tests (+21)
- New test functions: TestAggregatorHealthChDrops_BackPressure, TestAggregatorHealthChDrops_ZeroWhenNoDrops, TestRunPipeline_FallbackSnapshotNoEvidence, TestPrintClusterHealthSummaryEvidenceOff, TestPrintClusterHealthSummaryDryRunPathLine, TestPrintClusterHealthSummaryWrittenPathLine, TestLevenshtein, TestSuggestClosest, TestValidateIgnoreDropReasonsLevenshtein, TestPreRunE_RejectsInvalidDropReason, TestPreRunE_RejectsInvalidProtocol, TestPreRunE_ValidFlagsPass, TestDropClassString, TestHealthWriterSnapshotIdempotent, TestHealthWriterSnapshotNilSafe, TestTop3TieBoundary, TestTop3StrictTop3, TestTop3AllTied, TestPrintClusterHealthSummaryAdaptiveWidth, TestPrintClusterHealthSummaryShortNameNoWiden, TestRemediationHintDeepLinkPolicy, TestRemediationHintGenericURLReturnsEmpty, TestRemediationHintKnownDeepLinks, TestClassifierVersionMatchesGoMod, TestPolicyTargetEndpoint

## Commit SHAs

| Task | Commit | Description |
|------|--------|-------------|
| 1 (C1+C2+I7) | 3ef8573 | Non-blocking healthCh + fallback snapshot + ,ok lookup |
| 2 (C3+I1) | 71c1b50 | SummaryPathState enum + DropClass.String() + SetWarnLogger godoc |
| 3 (I2+I3) | 1e5f398 | PreRunE validation + Levenshtein top-5 + ,ok cleanup |
| 4 (I4+I5) | 7bd5910 | DropClass.String() dedup + Snapshot finalized gate + omitempty |
| 5 (I8+M5+M6) | 4107d25 | topN tie boundary + adaptive width + Transient fixture |
| 6 (M1+M2) | 281f685 | Generic-URL hints empty + README --preserve-status |
| 7 (M3+M4+M7) | e3b3e77 | policyTargetEndpoint + ClassifierVersion drift guard + explicit closes + Snapshot race-safe |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical] DropClass.String() added in Task 2 instead of Task 4**
- **Found during:** Task 2 — summary.go needed `s.Class.String()` which didn't exist yet
- **Fix:** Added `DropClass.String()` to classifier.go during Task 2 commit; Task 4 cleaned up the duplicate helpers and removed `dropClassString()`/`dropClassLabel()`
- **Files modified:** pkg/dropclass/classifier.go (Task 2 commit)
- **Commit:** 71c1b50

**2. [Rule 1 - Bug] sync.Once added to Snapshot() for race safety**
- **Found during:** Task 7 final suite run with `-race`
- **Issue:** `TestHealthWriterSnapshotIdempotent` spawned 8 concurrent Snapshot() goroutines; the `if hw.finalized.Load()` + `hw.cachedSnapshot = result` + `hw.finalized.Store(true)` sequence was not atomic — two goroutines could simultaneously enter the slow path and both write `cachedSnapshot`
- **Fix:** Wrap slow path in `sync.Once.Do()`; add `sync` import; keep `atomic.Bool` as a lock-free fast-path gate for post-Once calls
- **Files modified:** pkg/hubble/health_writer.go
- **Commit:** e3b3e77

**3. [Rule 1 - Bug] M1 deep-link policy relaxed: "#" anchor not required**
- **Found during:** Task 6 test run
- **Issue:** UNENCRYPTED_TRAFFIC, NO_EGRESS_GATEWAY, DROP_NO_EGRESS_IP use specific topic pages without "#" anchors — they are actionable, just not anchor-deep-links
- **Fix:** Test checks "URL != bare genericTroubleshootingURL" instead of "URL contains '#'" — preserves the M1 intent (no useless generic-page hints) without rejecting legitimate topic-page links
- **Commit:** 281f685

## Known Stubs

None — all fixes are fully wired with no placeholder logic.

## Self-Check: PASSED

- All 7 commits exist: 3ef8573, 71c1b50, 1e5f398, 7bd5910, 4107d25, 281f685, e3b3e77
- `go test -race ./...` green (442 tests, 0 failures)
- `go build ./...` clean
- `go vet ./...` clean
- `grep -q 'timeout --preserve-status 300 cpg generate' README.md` → found
