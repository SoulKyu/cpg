# Phase 10: Classifier Core - Context

**Gathered:** 2026-04-26
**Status:** Ready for planning
**Mode:** Auto-generated (discuss skipped via workflow.skip_discuss + autonomous mode locked-in decisions)

<domain>
## Phase Boundary

Deliver a standalone `pkg/dropclass/` package that classifies every Cilium `flowpb.DropReason` enum value into exactly one of `{policy, infra, transient, unknown}`. Pure data + lookup, zero pipeline integration. Exposes:
- `DropClass` enum (Policy / Infra / Transient / Unknown)
- `Classify(flowpb.DropReason) DropClass` — O(1) map lookup
- `RemediationHint(flowpb.DropReason) string` — Cilium docs URL per reason
- `ClassifierVersion` constant (semver string baked at build time)
- `ValidReasonNames() []string` — for `--ignore-drop-reason` flag validation later

Out of scope this phase: aggregator integration, health writer, flag plumbing, exit code (those are phases 11-13).
</domain>

<decisions>
## Implementation Decisions (locked in REQUIREMENTS.md + research SUMMARY.md)

### Package layout
- `pkg/dropclass/classifier.go` — DropClass type + taxonomy map + Classify()
- `pkg/dropclass/classifier_test.go` — full coverage of every flowpb.DropReason enum value
- `pkg/dropclass/hints.go` — RemediationHint() + URL map
- `pkg/dropclass/hints_test.go`
- `pkg/dropclass/version.go` — ClassifierVersion constant (semver)

### Taxonomy authority
- Source: Cilium `flowpb.DropReason_name` map iterated via `for k, v := range flowpb.DropReason_name`
- Bucket assignments per FEATURES.md classification table (~75 enum values, ~94% infra/transient/noise)
- Pure-policy reasons: POLICY_DENIED (133), POLICY_DENY (181), AUTH_REQUIRED (189) [needs_review annotation], DENIED_BY_LB_SRC_RANGE_CHECK (177)
- Default fallback for unrecognized values: **Unknown** (NEVER Policy — that would regress the bug)

### Unknown-reason behavior
- Single deduplicated WARN log per unique unrecognized DropReason value across the session (use `sync.Map` or mutex-guarded `map[int32]struct{}`, mirror `warnedReserved` pattern from pkg/hubble/aggregator.go)
- Do not log on hot path more than once per unique value

### Classifier version
- Constant string in `version.go`: `ClassifierVersion = "1.0.0-cilium1.19.1"`
- Bumped manually when taxonomy changes
- Embedded in cluster-health.json (consumed by phase 11)

### Performance
- Classify() = O(1) map lookup (NOT switch — switch on non-consecutive int32 enum is O(n))
- Benchmark test (`BenchmarkClassifyReason`) asserts < 50 ns/op

### Testing strategy
- TDD: failing test → implementation, atomic commits
- Test enumerates every flowpb.DropReason_name key and asserts non-Unknown classification (catch new Cilium values during go.mod bumps)
- Negative test: synthetic out-of-range int32(9999) → Unknown + WARN logged once
- Hint URL test: every entry in URL map points to docs.cilium.io with non-empty path

### Anti-features
- NO openmetrics export
- NO interaction with aggregator / pipeline (phase 11)
- NO --ignore-drop-reason flag (phase 13)
- NO config file overrides — taxonomy is hard-coded for this milestone
</decisions>

<code_context>
## Existing Code Insights

### Reusable assets
- `pkg/hubble/evidence_writer.go:131` already calls `f.GetDropReasonDesc().String()` — proves the proto enum is accessible
- `pkg/hubble/aggregator.go` warnedReserved pattern (sync.Once-style dedup) is the model for warnedUnknownReason
- `pkg/evidence/writer.go` atomic write pattern (CreateTemp + Rename) — for phase 11 reuse, not this phase

### Established patterns
- Domain-driven packages under `pkg/`
- Each package has `<file>.go` + `<file>_test.go` colocated
- TDD-first commits: failing test in commit N, impl in commit N+1
- Go 1.25.1 stdlib only; no new deps
- zap structured logging available via injected logger (not package-global)

### Integration points (downstream phase 11)
- `pkg/hubble/aggregator.go` will import `pkg/dropclass` and call `Classify()` per-flow before `keyFromFlow`
- `pkg/hubble/health_writer.go` (NEW phase 11) will import `ClassifierVersion` for embedding in cluster-health.json
</code_context>

<specifics>
## Specific Ideas

- The taxonomy table from `.planning/research/FEATURES.md` is the authoritative source for bucket assignments. Apply verbatim.
- AUTH_REQUIRED (189): bucket = `Policy`, but add a comment noting the SPIRE-misconfig edge case (deferred to v1.4 per REQUIREMENTS.md "Future")
- Use `flowpb.DropReason` (int32 alias) directly as map key, not string
</specifics>

<deferred>
## Deferred Ideas

- CI script that diffs taxonomy against latest Cilium proto on go.mod bump (v1.4 candidate, in REQUIREMENTS.md Future)
- AUTH_REQUIRED dual-path SPIRE inspection (v1.4 candidate)
- Config file overrides (`drop_reason_classes.yaml`) — explicit anti-feature this milestone
</deferred>
