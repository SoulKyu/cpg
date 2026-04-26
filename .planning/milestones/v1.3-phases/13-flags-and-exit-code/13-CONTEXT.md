# Phase 13: Flags + Exit Code - Context

**Gathered:** 2026-04-26
**Status:** Ready for planning
**Mode:** Auto-generated (autonomous mode, decisions locked in REQUIREMENTS + research SUMMARY)

<domain>
## Phase Boundary

Add user-facing CLI surface for cluster-health features:
1. `--ignore-drop-reason <reason>` — repeatable, comma-separated, case-insensitive — excludes flows by reason name BEFORE classification
2. `--fail-on-infra-drops` — opt-in non-zero exit (code 1) when ≥1 infra drop observed; default behavior unchanged (exit 0 always)
3. README documentation: exit-code semantics + recommended CI cron pattern

Out of scope: all v1.4+ items (openmetrics, semantic policy intersection, taxonomy auto-bump CI, AUTH_REQUIRED SPIRE inspection).
</domain>

<decisions>
## Implementation Decisions (locked)

### Files
- `cmd/cpg/commonflags.go` — MODIFY: add both flags to commonFlags struct + StringSlice/Bool registration mirroring `--ignore-protocol`
- `cmd/cpg/commonflags.go` — MODIFY: add `validateIgnoreDropReasons()` mirroring `validateIgnoreProtocols()`
- `cmd/cpg/generate.go` — MODIFY: pass ignoreDropReasons to PipelineConfig + check stats.InfraDropTotal at end + os.Exit(1) when --fail-on-infra-drops AND total>0
- `cmd/cpg/replay.go` — MODIFY: same wiring as generate.go
- `pkg/hubble/aggregator.go` — MODIFY: SetIgnoreDropReasons([]string) + filter check at top of Run() loop (before classification gate); IgnoredByDropReason() counter accessor
- `pkg/hubble/aggregator_test.go` — MODIFY: tests for new filter
- `cmd/cpg/commonflags_test.go` — MODIFY: validation tests
- `README.md` — MODIFY: document both flags in Flags section + Offline replay shared-flags line + new "## Exit codes" section + CI cron example

### Flag pattern (mirror PA5 --ignore-protocol exactly)
- `--ignore-drop-reason` is `cobra.StringSlice`, repeatable, comma-separated
- Validation at `parseCommonFlags` time: reject unknown reason names with error message listing all valid reasons
- Case-insensitive matching: lowercase user input, lowercase taxonomy keys for comparison
- `dropclass.ValidReasonNames()` — already exposed in phase 10 — is the source of truth for valid names
- WARN emitted when user passes a reason already classified Infra/Transient (FILTER-03)

### Filter precedence in aggregator Run()
1. NEW: `--ignore-drop-reason` filter (drops flow + counter, NOT counted in flowsSeen — user explicitly excluded)
2. existing `--ignore-protocol` filter (counter, NOT counted in flowsSeen)
3. existing classification gate (Infra/Transient → suppressed, COUNTED in flowsSeen)
4. existing keyFromFlow + bucket logic

NOTE: --ignore-drop-reason runs FIRST so user explicit exclusion takes precedence over classification (which is auto-suppressing).

### Exit code semantics
- Default: cpg always exits 0 (existing behavior preserved — Pitfall P3 backward compat is non-negotiable)
- `--fail-on-infra-drops` set + stats.InfraDropTotal > 0 → os.Exit(1)
- Use exit code **1** (not 2) per SUMMARY.md decision (Terraform exit-2 collision in GitOps pipelines)
- Implementation: cobra RunE returns nil normally; os.Exit(1) called from generate/replay after RunPipelineWithSource returns nil and stats inspected
- `RunPipelineWithSource` returns SessionStats already? (verify in code; if not, add via additive change — phase 11 may have already done this)

### Documentation in README
- Add `--ignore-drop-reason` row in Filtering section of `## Flags`
- Add `--fail-on-infra-drops` row in CI section (or new section)
- Add `## Exit codes` section:
  ```
  | Code | Meaning                                          |
  |------|--------------------------------------------------|
  | 0    | Success (default — even with infra drops)        |
  | 1    | Infra drops detected AND --fail-on-infra-drops   |
  ```
- Add CI cron example:
  ```bash
  # Cron: alert when infra drops appear in last hour
  cpg replay /tmp/last-hour.jsonl --fail-on-infra-drops || alert-team
  ```

### Testing strategy
- TDD-first
- Validation tests: case mix, comma-sep, repeatable, unknown reason → error
- Filter test: mixed flow stream with ignored reason → assert ignored counter incremented, no flowsSeen / no infraDrops / no CNP
- Exit code test (table-driven, no actual os.Exit): factor logic into testable func that returns exit code
- WARN test: pass infra-classified reason → zaptest observer captures warning

### Anti-features
- NO config file for ignored reasons (flag only)
- NO env var equivalent (flag only)
- NO exit code 2 (collision avoidance)
- NO short flag aliases (long names only — these are advanced flags)
</decisions>

<code_context>
## Existing Code Insights

### Reusable assets
- `cmd/cpg/commonflags.go:78-79` — `--ignore-protocol` registration pattern
- `cmd/cpg/commonflags.go:13-30` — `validateIgnoreProtocols()` is the exact mirror to copy
- `pkg/dropclass.ValidReasonNames()` — phase 10 deliverable, ready to use
- `pkg/dropclass.Classify()` for FILTER-03 redundancy WARN
- `pkg/hubble/aggregator.go` — `--ignore-protocol` filter at top of Run() loop is the placement model
- `pkg/hubble/pipeline.go` — `RunPipelineWithSource` returns SessionStats (added in phase 11) so cmd/ layer can inspect InfraDropTotal

### Established patterns
- Bool flags via `f.Bool("name", default, "help")`
- StringSlice via `f.StringSlice(...)`
- Validation in parseCommonFlags returning error
- Exit code: there's existing `os.Exit(1)` somewhere in main.go for fatal errors — preserve current behavior, only add new path when explicit flag set
</code_context>

<specifics>
## Specific Ideas

- Reason names are CILIUM enum names (e.g. `CT_MAP_INSERTION_FAILED`, not `ct_map_insertion_failed` for canonical form, but accept any case from user)
- Error message format: `unknown drop reason %q: valid values are %s` (mirror validateIgnoreProtocols word-for-word)
- WARN message format: `--ignore-drop-reason %q is redundant: reason is already classified as %s and suppressed by default`
</specifics>

<deferred>
## Deferred Ideas

- `--include-noise` (currently Noise class is silently discarded; flag could surface them) — v1.4 candidate
- Exit code 2 for "warnings only" (would require a third class of severity) — over-engineering
- `--health-only` mode that skips CNP generation entirely — not requested
</deferred>
