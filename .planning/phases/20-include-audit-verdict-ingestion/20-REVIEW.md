---
phase: 20-include-audit-verdict-ingestion
reviewed: 2026-07-22T10:40:41Z
depth: standard
files_reviewed: 21
files_reviewed_list:
  - cmd/cpg/commonflags.go
  - cmd/cpg/commonflags_test.go
  - cmd/cpg/generate.go
  - cmd/cpg/mcp_tools.go
  - cmd/cpg/replay.go
  - pkg/flowsource/file.go
  - pkg/flowsource/file_test.go
  - pkg/flowsource/source.go
  - pkg/flowsource/source_test.go
  - pkg/hubble/aggregator.go
  - pkg/hubble/aggregator_test.go
  - pkg/hubble/client.go
  - pkg/hubble/client_test.go
  - pkg/hubble/pipeline.go
  - pkg/hubble/pipeline_audit_test.go
  - pkg/hubble/pipeline_test.go
  - pkg/session/manager_test.go
  - pkg/session/pipeline_config.go
  - pkg/session/pipeline_config_test.go
  - pkg/session/session.go
  - testdata/flows/with_audit.jsonl
findings:
  critical: 0
  warning: 1
  info: 6
  total: 7
status: issues_found
---

# Phase 20: Code Review Report

**Reviewed:** 2026-07-22T10:40:41Z
**Depth:** standard
**Files Reviewed:** 21
**Status:** issues_found

## Summary

Adversarial review of the `--include-audit` / `include_audit` verdict-widening change (diff base `9481ba6`..HEAD). All 21 in-scope files were read in full; the phase diff was isolated and every widened filter site was traced end to end (gRPC `buildFilters`, `FileSource` replay gate, aggregator classification gate, `PipelineConfig` threading, CLI flag, MCP arg, `pkg/session` passthrough).

Phase security constraints were verified mechanically, not assumed:

- **Default-path equivalence (flag unset):** proven by boolean-algebra inspection at all three verdict sites — `client.go:196-223` produces the identical `[]Verdict{DROPPED}` wire content; `file.go:116` reduces to `f.Verdict != DROPPED`; `aggregator.go:451` reduces to `f.Verdict == DROPPED`. AUD-01 warning is gated on `cfg.IncludeAudit`. Regression tests pin all of this (`TestAggregator_AuditNotClassifiedWhenDisabled`, `TestPipeline_AuditDisabled_AuditFlowsIgnored`, `TestBuildFilters_*` non-audit variants, `TestIncludeAuditFlagParses` default case, `TestBuildPipelineConfig_IncludeAuditDefaultsFalse`).
- **No scope creep:** verdict set is exactly `{DROPPED, AUDIT}` at every site; no other verdict handling exists downstream (only `pkg/evidence/schema.go:93` stores a per-sample verdict string, populated dynamically from `f.GetVerdict().String()` at `pkg/hubble/evidence_writer.go:130` — AUDIT samples are recorded truthfully).
- **SEC-01:** `git diff 9481ba6..HEAD -- cmd/cpg/mcp_audit_test.go go.mod go.sum` is empty — MCP readonly audit test byte-identical, zero new dependencies.
- **Verification:** `go build ./...` clean, `go vet` clean, `go test -count=1 -race` green on `pkg/hubble`, `pkg/flowsource`, `pkg/session`, `cmd/cpg`.
- **Concurrency:** `auditVerdictCount` is a plain `uint64` written only by the aggregator's `Run()` goroutine and read only after `errgroup.Wait()` (`pipeline.go:364`), matching the established M-3 single-goroutine-ownership contract of `flowsSeen`/`l7HTTPCount`. Race detector confirms.

No incorrect behavior, security vulnerability, or data-loss risk was found. One Warning (an observability asymmetry in a security-relevant opt-in) and six Info items follow.

## Warnings

### WR-01: AUDIT ingestion has no positive observability signal — counter exists but is never surfaced

**File:** `pkg/hubble/pipeline.go:110-161` (SessionStats/Log), `pkg/session/session.go:196-207` (StopResult), `cmd/cpg/generate.go:184-195`, `cmd/cpg/replay.go:91-98`
**Issue:** The implementation explicitly mirrors the L7 diagnostic-counter pattern (`aggregator.go:95-99`: "mirrors the L7 counters' 'regardless of flag' rationale") but stops halfway. `L7HTTPCount`/`L7DNSCount` are surfaced on `SessionStats` (pipeline.go:125-128), in the session-summary log (pipeline.go:154-155), and in the MCP `StopResult` (session.go:201-202). `AuditVerdictCount` is tracked identically inside the aggregator but is surfaced **nowhere**: not on `SessionStats`, not in `stats.Log()`, not in `StopResult`. Its only consumer is the zero-case AUD-01 warning (pipeline.go:364). Additionally, the `cpg generate configuration` and `cpg replay configuration` log lines omit the `include-audit` flag state, and `pipeline.go:181` still logs "streaming dropped flows" unconditionally.

Consequence: an operator who opts into ingesting would-be-drop AUDIT verdicts — traffic that was actually **forwarded** — into allow-policy generation gets no count of how many AUDIT flows were mixed into the generated policies, and an MCP client polling `stop_session` cannot distinguish an audit-heavy session from a drop-only one. Per-sample verdicts in evidence files (`evidence_writer.go:130`) partially mitigate, but evidence is optional (`--no-evidence`, dry-run) and `StopResult`/summary never read it. For a GitOps artifact generator whose session summary is the review evidence, this degrades auditability of a security-relevant knob.
**Fix:**
```go
// pipeline.go — SessionStats:
// AuditVerdictCount: number of Verdict_AUDIT flows observed during the
// session (populated regardless of IncludeAudit, like the L7 counters).
AuditVerdictCount uint64
// after g.Wait():
stats.AuditVerdictCount = agg.AuditVerdictCount()
// SessionStats.Log():
zap.Uint64("audit_verdict_count", s.AuditVerdictCount),

// session.go StopResult:
AuditVerdictCount uint64 `json:"audit_verdict_count"`

// generate.go/replay.go configuration log:
zap.Bool("include-audit", f.includeAudit),
```

**Resolution:** RESOLVED in commit `ada7b30` (2026-07-22). `AuditVerdictCount` added to `SessionStats` (populated from `agg.AuditVerdictCount()` after `g.Wait()`, alongside the L7 counters), to `stats.Log()` as `audit_verdict_count`, and to the MCP `StopResult` as `audit_verdict_count` (wired in `buildSummary`). Tests extended: `TestSession_BuildSummary` (both subtests), `TestSessionStats_Log` (key + value), and `TestPipeline_AuditIngested_GeneratedLikeDropped` (end-to-end `audit_verdict_count=1` in the session summary). Two sub-items deliberately NOT changed, per the mirror-L7-exactly constraint: the generate/replay configuration log lines omit `include-audit` because they do not log `--l7` either (adding one without the other would create a new asymmetry), and the unconditional "streaming dropped flows" message at `pipeline.go:181` is likewise shared with the L7 path. `cmd/cpg/mcp_audit_test.go` untouched (SEC-01); full `go build`/`go vet`/`go test -count=1 -race` green.

## Info

### IN-01: pkg/flowsource documentation not updated for the widened contract

**File:** `pkg/flowsource/file.go:22-24`, `pkg/flowsource/file.go:69-71`, `pkg/flowsource/source.go:12-15`
**Issue:** The `FileSource` type doc still reads "streams DROPPED flows … non-DROPPED verdicts are skipped with a counter", the `StreamDroppedFlows` method doc still says "streams DROPPED flows to the returned channel", and the `FlowSource` interface doc does not document the new `includeAudit` parameter at all. All three are now inaccurate when the flag is set.
**Fix:** Update the three doc comments to state that `includeAudit=true` widens emission to `{DROPPED, AUDIT}` and that the default remains DROPPED-only.

### IN-02: FileSource counter/log-field names drift under includeAudit=true

**File:** `pkg/flowsource/file.go:33-36`, `pkg/flowsource/file.go:135-148`
**Issue:** `zap.Int64("flows_dropped", s.stats.flowsEmitted.Load())` now includes emitted AUDIT flows under a field named `flows_dropped`, and `NonDroppedSkipped` semantically becomes "neither DROPPED nor accepted-AUDIT" when the flag is set. Log-only inaccuracy; no behavioral impact.
**Fix:** Rename the log field to `flows_emitted` (and consider `FlowsEmitted`-aligned wording in the "replay complete"/"replay truncated" entries), or document the widened meaning on `FileSourceStats`.

### IN-03: No unit-level FileSource test exercises includeAudit=true

**File:** `pkg/flowsource/file_test.go` (all 8 `StreamDroppedFlows` call sites pass `false`), `testdata/flows/with_non_dropped.jsonl` (contains FORWARDED only, no AUDIT line)
**Issue:** The audit branch of `file.go:116` is covered only indirectly via the pipeline E2E tests (`TestPipeline_AuditIngested_GeneratedLikeDropped`, `TestPipeline_AuditDisabled_AuditFlowsIgnored`). No unit test asserts the counter contract at the source level: AUDIT flow → `NonDroppedSkipped++` when flag off, AUDIT flow → `FlowsEmitted++` when flag on.
**Fix:** Add an AUDIT line to `with_non_dropped.jsonl` (or a small dedicated fixture) and two `FileSource`-level tests asserting `Stats()` counters for both flag values.

### IN-04: Workloads assertion in AUD-01 test silently no-ops on unexpected field type

**File:** `pkg/hubble/pipeline_audit_test.go:62-69`
**Issue:** If `fields["workloads"]` decodes to neither `[]interface{}` nor `[]string`, both type assertions fail and no assertion executes — the test still passes while verifying nothing about the workloads payload. Affects test reliability (a regression in the warning's field encoding would go unnoticed).
**Fix:** Add a `default`/else branch: `t.Fatalf("workloads field has unexpected type %T", fields["workloads"])`.

### IN-05: AUDIT flows with infra-class reasons count as "infra drop(s)" toward --fail-on-infra-drops and cluster-health.json

**File:** `pkg/hubble/aggregator.go:451-473`, `pkg/hubble/pipeline.go:431-436`
**Issue:** With `includeAudit=true`, an AUDIT-verdict flow carrying an Infra/Transient `DropReasonDesc` increments `InfraDropTotal`, lands in cluster-health.json as a cluster-critical drop, and can flip `--fail-on-infra-drops` to exit 1 — even though the traffic was actually forwarded (audit mode). In practice Cilium emits AUDIT only from policy audit mode (would-be POLICY_DENIED), so the combination is essentially unreachable today, and `TestAggregator_ClassifiesAuditWhenEnabled` pins the uniform treatment as intended per the phase's "exact same pipeline" requirement. Recording so the semantic conflation is a documented decision rather than an accident.
**Fix:** No code change required; consider one sentence in the `--include-audit` flag help or README noting that AUDIT flows are classified identically to DROPPED, including infra-drop accounting.

### IN-06: Two adjacent positional bools on the widened FlowSource interface

**File:** `pkg/flowsource/source.go:15`
**Issue:** `StreamDroppedFlows(ctx, namespaces, allNS bool, includeAudit bool)` — transposing `allNS`/`includeAudit` at a call site compiles silently. Currently safe: the only production call site (`pipeline.go:176`) passes named `cfg.AllNamespaces, cfg.IncludeAudit` fields; test doubles ignore both. Risk is confined to future call sites.
**Fix:** If a third option ever lands, migrate the tail parameters to a `StreamOptions{AllNamespaces, IncludeAudit bool}` struct.

---

_Reviewed: 2026-07-22T10:40:41Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
