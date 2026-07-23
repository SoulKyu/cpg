---
phase: 20-include-audit-verdict-ingestion
fixed_at: 2026-07-22T10:52:56Z
review_path: .planning/phases/20-include-audit-verdict-ingestion/20-REVIEW.md
iteration: 1
findings_in_scope: 1
fixed: 1
skipped: 0
status: all_fixed
---

# Phase 20: Code Review Fix Report

**Fixed at:** 2026-07-22T10:52:56Z
**Source review:** .planning/phases/20-include-audit-verdict-ingestion/20-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 1 (fix scope: Critical + Warning; 0 Critical, 1 Warning — 6 Info findings out of scope)
- Fixed: 1
- Skipped: 0

## Fixed Issues

### WR-01: AUDIT ingestion has no positive observability signal — counter exists but is never surfaced

**Files modified:** `pkg/hubble/pipeline.go`, `pkg/session/session.go`, `pkg/hubble/pipeline_test.go`, `pkg/hubble/pipeline_audit_test.go`, `pkg/session/session_test.go`
**Commit:** ada7b30 (review resolution note: 6820cd0)
**Applied fix:** Mirrored the `L7HTTPCount`/`L7DNSCount` plumbing verbatim:

1. `SessionStats.AuditVerdictCount` added (`pkg/hubble/pipeline.go`), placed after `L7DNSCount` with an L7-style doc comment noting that with the flag unset upstream verdict filters keep it at 0.
2. `SessionStats.Log()` emits `zap.Uint64("audit_verdict_count", ...)` after `l7_dns_count`.
3. Populated from `agg.AuditVerdictCount()` in the post-`g.Wait()` block, alongside the L7 counters.
4. `StopResult.AuditVerdictCount uint64 \`json:"audit_verdict_count"\`` added after `L7DNSCount` (`pkg/session/session.go`) and wired in `buildSummary` after the L7 lines.
5. Tests extended: `TestSession_BuildSummary` (populated subtest asserts 6, zero subtest asserts 0), `TestSessionStats_Log` (key presence + value 9), `TestPipeline_AuditIngested_GeneratedLikeDropped` (end-to-end: session summary reports `audit_verdict_count=1` for the one-AUDIT-flow fixture, mirroring `TestRunPipeline_PopulatesLostEvents`).

**Deliberately unchanged sub-items** (per the mirror-L7-exactly instruction):

- The `cpg generate configuration` / `cpg replay configuration` log lines do NOT gain `include-audit`: they do not log `--l7` either (verified — no `zap.Bool("l7", ...)` anywhere in cmd/cpg), so adding `include-audit` alone would create a new asymmetry rather than mirror the L7 pattern.
- The unconditional "streaming dropped flows" message at `pipeline.go:181` is shared with the L7 path and was not in the mandated fix scope.

**Verification:** `gofmt -l` clean on all 5 files; `go build ./...` OK; `go vet ./...` OK; full `go test ./... -count=1 -race` green (all 12 packages). `cmd/cpg/mcp_audit_test.go` byte-identical across the fix range (`git diff --stat 6f82e84..6820cd0 -- cmd/cpg/mcp_audit_test.go` empty — SEC-01 preserved; that test is a structural callgraph audit with no `StopResult` field expectations, so the new JSON field cannot affect it). Zero new dependencies.

## Skipped Issues

None.

---

_Fixed: 2026-07-22T10:52:56Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
