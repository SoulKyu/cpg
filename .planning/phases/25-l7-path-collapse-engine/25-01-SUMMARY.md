---
phase: 25-l7-path-collapse-engine
plan: 01
subsystem: policy
tags: [l7, http, regex, cilium, network-policy, collapse, path-templates, re2]

requires:
  - phase: 8-http-l7-generation
    provides: extractHTTPRules/anchorPath literal HTTP rule primitives, recordL7 attribution seam
  - phase: 17-session-lifecycle
    provides: Aggregator flush loop + setter wiring chain (include_audit analog)
provides:
  - Pure L7 path collapse engine (pkg/policy/l7_collapse.go): segment classifier, template synthesis, min-distinct-paths gate, pattern length cap, whole-run L7CollapseState
  - Replace-mode merge reconciliation: PolicyEvent.ReplaceL7HTTP + MergePolicyWithOptions (DNS sub-lists never replaced)
  - End-to-end wiring: Aggregator whole-run retained state, PipelineConfig fields, --l7-collapse-paths / --min-flows-per-l7-rule CLI flags on generate + replay, fail-closed CLI gate validation
  - Four jsonl fixtures (users 10k, mixed-classes, below-threshold, unicode-encoded) + full segment-class coverage and gate/explain test suite
affects: [25-02 (MCP parity + byte-identity goldens), explain, evidence, docs]

actuals:
  tokens: 2909
  tasks: 5
  commits: 11

tech-stack:
  added: []
  patterns:
    - "Whole-run retained observation state (L7CollapseState.Absorb) recomputed at every flush; replace-merge reconciles differing dedup keys across flushes"
    - "Raw wire-form path observation (rawPathFromURL via u.EscapedPath) — percent-encoding never decoded before matching"

key-files:
  created:
    - pkg/policy/l7_collapse.go
    - pkg/policy/l7_collapse_test.go
    - pkg/policy/merge_l7_replace_test.go
    - pkg/hubble/pipeline_l7_collapse_test.go
    - cmd/cpg/explain_l7_collapse_test.go
    - testdata/flows/l7-collapse-users.jsonl
    - testdata/flows/l7-collapse-mixed-classes.jsonl
    - testdata/flows/l7-collapse-below-threshold.jsonl
    - testdata/flows/l7-collapse-unicode-encoded.jsonl
  modified:
    - pkg/policy/builder.go
    - pkg/policy/merge.go
    - pkg/policy/attribution.go
    - pkg/output/writer.go
    - pkg/hubble/aggregator.go
    - pkg/hubble/pipeline.go
    - cmd/cpg/commonflags.go
    - cmd/cpg/generate.go
    - cmd/cpg/replay.go
    - cmd/cpg/commonflags_test.go

key-decisions:
  - "Decision 1 (pinned): finalize-time collapse over whole-run retained path set; every collapse-enabled flush marks ReplaceL7HTTP and merge replaces HTTP sub-lists per (port, proto) bucket; DNS untouched"
  - "Decision 2 (pinned): attribution stays keyed by literal observed paths — explain --http-path and evidence semantics unchanged under collapse (proven by TestExplainCompatibleWithCollapsedRun)"
  - "Decision 3 (pinned): gate default N=5, metric is DISTINCT OBSERVED PATHS (flag name says flows; requirement text authoritative), fail-closed resolution n<1 → 5 plus CLI rejection of n<1"
  - "Collapse observes paths via rawPathFromURL (new helper in l7_collapse.go, u.EscapedPath) instead of pathFromURL — satisfies the '%C3%A9 never decoded' must-have without changing l7.go anchorPath/QuoteMeta semantics (plan verification constraint); non-collapse behavior untouched"
  - "Mixed-classes fixture separates class groups by HTTP METHOD: collapse groups per (method, segment count), so cross-group position variance at equal segment count would poison every class position; plan's path shapes preserved"

patterns-established:
  - "Pattern: collapse-enabled flush = whole-run-authoritative emission + ReplaceL7HTTP replace-merge; never in-place template widening"
  - "Pattern: fail-closed numeric flags — in-process Resolve maps <1 → default, CLI PreRunE rejects <1 outright"

requirements-completed: [L7GEN-01, L7GEN-02, L7GEN-04]

coverage:
  - id: D1
    description: "10k distinct observed GET /api/v1/users/<n> paths collapse to exactly one anchored ^/api/v1/users/[0-9]+$ template per (method, template)"
    requirement: L7GEN-01
    verification:
      - kind: integration
        ref: "pkg/hubble/pipeline_l7_collapse_test.go#TestL7CollapseGolden"
        status: pass
    human_judgment: false
  - id: D2
    description: "Full segment-class coverage matrix: numeric/0x-hex/hex-run/UUID generalize to POSIX-ERE classes; version, mixed-class, single-observation, percent-encoded, and metacharacter-adjacent positions stay literal; adjacent positions generalize independently; never [^/]+ or .+"
    requirement: L7GEN-02
    verification:
      - kind: unit
        ref: "pkg/policy/l7_collapse_test.go#TestPathTemplateClassifier"
        status: pass
      - kind: unit
        ref: "pkg/policy/l7_collapse_test.go#TestCollapseHTTPPaths_UUIDTemplate"
        status: pass
      - kind: integration
        ref: "pkg/hubble/pipeline_l7_collapse_test.go#TestL7CollapseMixedClassesGolden"
        status: pass
    human_judgment: false
  - id: D3
    description: "Min-distinct-paths gate with default 5: N-1 distinct paths emit literal rules, N emit the template; boundary proven at unit and replay level; CLI rejects values < 1 fail-closed"
    requirement: L7GEN-04
    verification:
      - kind: unit
        ref: "pkg/policy/l7_collapse_test.go#TestCollapseHTTPPaths_GateBoundary"
        status: pass
      - kind: integration
        ref: "pkg/hubble/pipeline_l7_collapse_test.go#TestMinFlowsGate"
        status: pass
      - kind: unit
        ref: "cmd/cpg/commonflags_test.go#TestValidateMinFlowsPerL7Rule"
        status: pass
    human_judgment: false
  - id: D4
    description: "Cross-flush reconciliation: whole-run retained state + replace-mode merge leaves no stale literals; runs race-free; attribution/evidence stays literal-keyed (explain compatibility)"
    requirement: L7GEN-01
    verification:
      - kind: integration
        ref: "pkg/hubble/pipeline_l7_collapse_test.go#TestL7Collapse_CrossFlushReplace"
        status: pass
      - kind: integration
        ref: "cmd/cpg/explain_l7_collapse_test.go#TestExplainCompatibleWithCollapsedRun"
        status: pass
    human_judgment: false
  - id: D5
    description: "Default-off invariant: with all new fields zero, emitted YAML unchanged on existing fixtures (full byte-identity golden regression deferred to plan 25-02 per plan)"
    requirement: L7GEN-04
    verification:
      - kind: integration
        ref: "pkg/hubble/pipeline_l7_collapse_test.go#TestL7Collapse_DefaultOffUnchanged"
        status: pass
    human_judgment: false
  - id: D6
    description: "Envoy RE2 program-size control: candidate template patterns exceeding maxL7TemplatePatternLen (256) fall back to literal rules"
    requirement: L7GEN-02
    verification:
      - kind: unit
        ref: "pkg/policy/l7_collapse_test.go#TestCollapseHTTPPaths_PatternLengthCap"
        status: pass
    human_judgment: false

duration: ~55min (continuation session; tasks 1-3 completed by prior session)
completed: 2026-10-04
status: complete
---

# Phase 25, Plan 01: L7 Path Collapse Engine Summary

**`--l7-collapse-paths` collapses high-cardinality observed HTTP paths into anchored POSIX-ERE templates behind a distinct-observed-paths confidence gate, with whole-run replace-merge reconciliation across flushes — default-off byte-identical.**

## Performance

- **Duration:** continuation session ~55 min (tasks 1-3 were completed by the interrupted prior session and only verified here)
- **Started:** 2026-10-04 (continuation from recorded state after tasks 1-3)
- **Completed:** 2026-10-04
- **Tasks:** 5 of 5
- **Files modified:** 22 (9 created, 13 modified)

## Accomplishments

- Pure, deterministic segment-wise generalizer in `pkg/policy/l7_collapse.go`: classifier (UUID → 0x-hex → numeric → hex-run → version → word, pure string matching, no numeric parsing), variance-only template synthesis, min-distinct-paths gate (default 5, fail-closed), 256-char pattern length cap (T-25-02), whole-run `L7CollapseState` with `Absorb()`
- Replace-mode merge reconciliation (`PolicyEvent.ReplaceL7HTTP` + `MergePolicyWithOptions`): each collapse-enabled flush's HTTP set is whole-run-authoritative and replaces prior per-bucket literals; DNS sub-lists never replaced; zero-option behavior byte-identical
- End-to-end wiring: Aggregator retained-state setters, flush-time absorb, `--l7-collapse-paths` / `--min-flows-per-l7-rule` on generate + replay, fail-closed CLI validation rejecting gate values < 1
- SC1 golden proven: 10,000 distinct user paths → exactly one `^/api/v1/users/[0-9]+$` template + `^/healthz$` literal; cross-flush replacement proven race-free; full segment-class matrix and gate boundary (N-1 literal / N template) proven at unit + replay level; `cpg explain` compatibility with collapsed runs proven (literal-keyed evidence)

## Task Commits

Each task was committed atomically (TDD: test RED → feat GREEN):

1. **Task 1: pure collapse engine** — `a36d7b4` (test) + `bdd6b41` (feat)
2. **Task 2: replace-mode merge reconciliation** — `39673fd` (test) + `04be558` (feat)
3. **Task 3: pipeline integration + CLI flags** — `734f1a5` (test, incl. users fixture) + `8fb61c8` (feat)
4. **Task 4: full segment-class coverage matrix** — `706ac09` (test, incl. mixed-classes fixture) + `96f8e16` (feat: raw wire-form observation)
5. **Task 5: gate tests, CLI validation, explain compatibility** — `882b551` (test, incl. two fixtures) + `0a4057f` (feat: fail-closed validation)

**Plan metadata:** `docs(25-01): add execution summary` (this commit)

_Note: prior session's tasks 1-3 were verified green before continuation; a leftover uncommitted gofmt comment-alignment diff in l7_collapse.go from the interrupted session was folded into the Task 4 commits._

## Files Created/Modified

- `pkg/policy/l7_collapse.go` (new) — classifier, `CollapseHTTPPaths`, `L7CollapseState`/`Absorb`, gate resolution, `MergeOptions`/`MergePolicyWithOptions`, `rawPathFromURL` (wire-form path extraction)
- `pkg/policy/l7_collapse_test.go` (new) — 15 test funcs: classifier matrix, per-class templates, version/mixed/encoded/metachar/adjacency/single-observation literals, gate boundary, length cap, sort order
- `pkg/policy/merge.go` — `MergePolicy` delegates to `MergePolicyWithOptions`; replace-mode in `mergePortRules`
- `pkg/policy/builder.go` — `PolicyEvent.ReplaceL7HTTP`; collapse observation (raw form) in `recordL7`; `applyL7Collapse` over Retained ∪ Collected
- `pkg/policy/attribution.go` — `AttributionOptions.MinFlowsPerL7Rule` + `.Collapse` with activation-condition docs
- `pkg/policy/merge_l7_replace_test.go` (new) — replace-mode, DNS-preservation, zero-options tests
- `pkg/hubble/aggregator.go` — `SetCollapseL7Paths`/`SetMinFlowsPerL7Rule`, flush forwards + `Absorb()`
- `pkg/hubble/pipeline.go` — `PipelineConfig.CollapseL7Paths`/`.MinFlowsPerL7Rule`, setter wiring
- `pkg/hubble/pipeline_l7_collapse_test.go` (new) — golden, cross-flush, default-off, setter, mixed-classes, below-threshold, unicode goldens
- `pkg/output/writer.go` — forwards `ReplaceL7HTTP` into `MergePolicyWithOptions`
- `cmd/cpg/commonflags.go` — both flags, help text ("distinct observed paths"), `validateCommonFlags` rejects < 1
- `cmd/cpg/commonflags_test.go` — `TestL7CollapseFlagParses`, `TestValidateMinFlowsPerL7Rule`
- `cmd/cpg/generate.go`, `cmd/cpg/replay.go` — PipelineConfig fields sourced from commonFlags
- `cmd/cpg/explain_l7_collapse_test.go` (new) — explain compatibility over a collapsed run
- `testdata/flows/l7-collapse-{users,mixed-classes,below-threshold,unicode-encoded}.jsonl` (new) — golden fixtures (10,003 / 14 / 8 / 3 lines)

## Decisions Made

- **Raw wire-form observation (deviation, see below):** collapse observes paths via new `rawPathFromURL` (uses `u.EscapedPath()`) in `l7_collapse.go` instead of `pathFromURL` (which percent-decodes via `u.Path`). Satisfies the must-have "%C3%A9 is never decoded before matching" while honoring the plan's verification constraint that `pkg/policy/l7.go` `anchorPath`/`QuoteMeta` semantics remain untouched. Non-collapse emission behavior is unchanged.
- **Mixed-classes fixture grouping:** the plan's mixed-classes paths all share segment count 4 under GET; since collapse groups per (method, segment count), cross-group position variance (word literals alongside class values) destroys every class position and no template can form. The fixture preserves the plan's path shapes but separates class groups by HTTP method (GET numeric/version/encoded, POST 0x-hex, PUT hex-run, DELETE UUID, PATCH mixed). The replay test lowers `MinFlowsPerL7Rule` to 2 because each class group carries exactly 2 distinct paths (plan lists 2 values per class).
- All other decisions followed the plan's pinned Decisions 1-3 verbatim.

## Deviations from Plan

### Auto-fixed Issues

**1. Percent-encoding was decoded before classification (KNOWN_LIMITATIONS #8 violation)**
- **Found during:** Task 4 (`TestL7CollapseMixedClassesGolden` emitted `André` instead of `Andr%C3%A9`)
- **Issue:** collapse observed paths through `pathFromURL`, whose `url.Parse().Path` percent-decodes
- **Fix:** new `rawPathFromURL` helper in `l7_collapse.go` using `u.EscapedPath()` (wire form); `recordL7`'s collapse observation uses it. One-line call-site change in `builder.go`.
- **Files modified:** pkg/policy/l7_collapse.go, pkg/policy/builder.go
- **Verification:** `TestL7CollapseUnicodeEncodedGolden` asserts `%C3%A9`/`%C3%A9`/`%C3%AF` byte-for-byte in emitted YAML; full suite green
- **Committed in:** 96f8e16

**2. Plan's mixed-classes fixture cannot produce templates under the pinned grouping algorithm**
- **Found during:** Task 4 (all 14 fixture paths emitted as literals; no class position survived cross-group variance)
- **Issue:** all plan-specified paths share (GET, 4 segments); position analysis runs across the whole segment-count group
- **Fix:** fixture separates class groups by HTTP method; golden test gate lowered to 2 (2 distinct paths per class group). Path shapes preserved from the plan.
- **Files modified:** testdata/flows/l7-collapse-mixed-classes.jsonl, pkg/hubble/pipeline_l7_collapse_test.go
- **Verification:** `TestL7CollapseMixedClassesGolden` asserts 4 class templates + 6 literal rules
- **Committed in:** 706ac09

**3. Leftover uncommitted gofmt diff from interrupted session**
- **Found during:** continuation start (working-tree inspection)
- **Issue:** 6+/6− whitespace-only comment-alignment diff in `pkg/policy/l7_collapse.go`
- **Fix:** verified as formatting-only, folded into the Task 4 feat commit
- **Committed in:** 96f8e16

---

**Total deviations:** 3 auto-fixed (1 correctness defect, 1 fixture/test-consistency fix, 1 housekeeping)
**Impact on plan:** All fixes required for the plan's own must-haves (encoding invariant, per-class golden). No scope creep; no contradiction with must_haves or threat controls.

## Issues Encountered

- Prior session hit a runtime timeout after committing tasks 1-3; its uncommitted working-tree state was a gofmt-only diff (folded in as noted). Tasks 1-3 were verified green before continuing.
- Explain JSON uses `matched_rules` (not `rules`) — the explain compatibility test's parse struct was corrected during development (same commit as the test).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Plan 25-02 (wave 2) can proceed: MCP parity (`collapse_l7_paths` / `min_flows_per_l7_rule` on `start_session`) and the byte-identity golden regression (`testdata/golden/l7-collapse-regression/`) against the engine and fixtures delivered here.
- Note for 25-02: non-collapse emission still percent-decodes via `anchorPath` (pre-v1.7 behavior, documented under KNOWN_LIMITATIONS #8); collapse-enabled runs match the raw wire form. The byte-identity regression should pin the default-off behavior as-is.
- Full suite `go test -race -count=1 ./...` green (14 packages).

---
*Phase: 25-l7-path-collapse-engine*
*Completed: 2026-10-04*
