---
status: reviewed
phase: 25
date: 2026-10-04
scope: 6f6d0bf..HEAD — pkg/policy/l7_collapse.go (+tests), pkg/policy/{merge,attribution,builder}.go, pkg/output/writer.go, pkg/hubble/{aggregator,pipeline}.go (+tests), cmd/cpg/{commonflags,generate,replay,mcp_tools,explain}.go (+tests), pkg/session/{session,pipeline_config}.go (+tests), testdata fixtures + goldens
counts:
  blocker: 0
  warning: 1
  info: 3
review_method: source + test inspection (no builds/tests run; suite verified green by orchestrator). No git tooling available in the review session — commit-range diff not directly inspected; working tree confirmed clean vs reviewer-launch HEAD (only .planning scratch untracked, allowed).
---

# Phase 25 Review — L7 Path Collapse Engine

## Verified correct (evidence)

- **Collapse engine core** (`pkg/policy/l7_collapse.go`): classification order UUID → 0x-hex → numeric → hex-run(≥6, ≥1 digit) → version → word (lines 33–83); version and word never generalize (`generalizable()`, lines 92–101); POSIX-ERE-only fragments (`patternFragment()`, lines 104–117); grouping per (method, segment count) then per literal-signature cluster (`collapseMethodPaths`/`collapseSegmentCountGroup`, lines 292–410); mixed-class positions fall back to literal (homogeneity check, lines 351–370); QuoteMeta on every non-class segment including metacharacters adjacent to fragments (line 386).
- **Fail-closed gate N=5** (`l7_collapse.go:171–180` `ResolveMinFlowsPerL7Rule` maps <1 → `DefaultMinFlowsPerL7Rule`); gate metric is **distinct paths per cluster** (`len(c.paths) >= minDistinct`, line 400) — boundary N collapses / N-1 stays literal pinned by `TestCollapseHTTPPaths_GateBoundary` (l7_collapse_test.go:277–303).
- **256-char cap** (`l7_collapse.go:193–195`, applied at line 400) with arithmetic test (`TestCollapseHTTPPaths_PatternLengthCap`, l7_collapse_test.go:249–275) — T-25-02 Envoy RE2 program-size control present.
- **Wire-form encoding invariant** (`rawPathFromURL`, lines 229–256): `u.EscapedPath()` (no decode); fallback trims `#`/`?` only. Pinned by `TestCollapseHTTPPaths_EncodedSegmentsStayLiteral` and the unicode replay golden (`TestL7CollapseUnicodeEncodedGolden`, pipeline_l7_collapse_test.go:235–244). Empty path → `^/$` consistent (lines 220–226, 388).
- **Replace-mode merge** (`merge.go:212–326`): `mergeL7Rules` with `replaceHTTP` replaces only when the incoming HTTP sub-list is non-empty (line 322); DNS always union-merges (lines 338–352); an L4-only incoming bucket never wipes existing HTTP (`consume` only merges when `pr.Rules != nil`, line 282). Cannot reduce the within-run permitted set: incoming set = CollapseHTTPPaths output over whole-run Retained∪Collected, which contains a literal fallback for every observed path (`builder.go:554–563`). Pinned by `TestMergePolicy_ReplaceL7HTTP_*` (merge_l7_replace_test.go).
- **Attribution stays keyed by literal paths** (Decision 2): `recordL7` observes raw paths into Collapse state but attribution uses the unchanged `ruleKeyForL7` (`builder.go:516–527`); `TestExplainCompatibleWithCollapsedRun` proves `cpg explain` still surfaces literal-path evidence under a collapsed run.
- **Concurrency**: `l7Collapse` state mutated/read only inside `Aggregator.Run`'s select loop — `flush` from ticker and ctx.Done of the same goroutine (`aggregator.go:417–524, 576–604`); `Absorb()` after every bucket's BuildPolicy consumed Collected (`aggregator.go:595–600`). `TestL7Collapse_CrossFlushReplace` runs a windowed fixture under -race with 50ms flush.
- **Fail-closed validation, three layers**: CLI `PreRunE` rejects `< 1` (`commonflags.go:210–216`; tests commonflags_test.go:358–397); MCP rejects negative before `mgr.Start` (`mcp_tools.go:135–142`; `TestMCPCollapseArgs`); 0/omitted resolves to 5 in exactly one place (`pipeline_config.go:104–111`). Both flags on generate and replay (`commonflags.go:92–93`, wired `generate.go:300–301`, `replay.go:128–129`).
- **Security posture**: phase adds zero cluster writes; the only K8s mutation surface remains the pre-existing `pkg/k8s/exec.go` audit-mode toggle. `writer.go` still local-only with `ValidatePolicyRef` guard.
- **Test quality**: SC1 `TestL7CollapseGolden` (10k→1 + ^/healthz exact counts); SC2 `TestL7NoCollapseByteIdentity` (goldens regenerated from pre-phase-25 commit e4159a8, documented provenance, "never edit a golden" prohibition, pipeline_l7_golden_test.go:1–45); SC3 `TestMinFlowsGate` (N-1 vs N in one fixture, exact rule count); SC4 `TestPathTemplateClassifier` table + `TestL7CollapseMixedClassesGolden` (exact 10-rule count); SC5 `TestMCPCollapseArgs` + `TestMCPCLICollapseParity`. Assertions non-vacuous.
- **Determinism**: output sorted by (Method, Path) — same key as `sortL7Rules`; cluster sig uses QuoteMeta'd segments joined by `\x00` (QuoteMeta injective → exact cluster identity).

## Findings

- id: R-01
  severity: WARNING
  file: pkg/output/writer.go:54 (+ pkg/policy/merge.go:322, pkg/hubble/aggregator.go:592)
  description: >
    Replace-mode merge is applied unconditionally against the on-disk existing policy, so its
    whole-run-authority semantics extend beyond the pinned "earlier flushes" contract to
    **earlier runs**: a collapse-enabled run that observes a subset of the paths a previous
    collapse-enabled run observed will NARROW the policy's HTTP permitted set (previous-run-only
    literals are dropped), and any hand-edited HTTP rules on a cpg-managed (port, proto) bucket
    are silently discarded on the next collapse-enabled flush. Pre-phase union-merge never
    narrowed. Plan Decision 1 (25-01-PLAN.md:37) pins only cross-flush replacement; cross-run
    narrowing is reachable via the same writer code path but is nowhere documented or gated.
  evidence: >
    writer.go:54 `policy.MergePolicyWithOptions(existing, event.Policy, MergeOptions{ReplaceL7HTTP: event.ReplaceL7HTTP})`
    where `existing` is the previously written file (prior run or hand-edited); merge.go `mergeL7Rules`
    replaces the entire HTTP sub-list when the incoming list is non-empty. Within-run invariants hold
    (never reduces below observed); the reduction risk is strictly cross-run / hand-authored content.
  suggestion: >
    Document cross-run authority in README/KNOWN_LIMITATIONS ("--l7-collapse-paths makes each run's
    HTTP set authoritative and replaces prior-run HTTP rules on matching buckets"), or gate replace
    to cpg-managed emissions only, or warn at flush time when replace drops existing HTTP rules that
    the current run did not observe.
- id: R-02
  severity: INFO
  file: pkg/policy/l7_collapse.go:69–72 vs 104–117
  description: >
    The hex-run classifier requires ≥1 digit (all-letter runs stay words), but the emitted fragment
    `[0-9a-fA-F]+` also matches all-letter runs. Inherent within-class widening (same as `[0-9]+`
    matching unobserved numbers), never a reduction. Note only; no action required.
- id: R-03
  severity: INFO
  file: pkg/policy/builder.go:516–527 (vs l7.go:104–113)
  description: >
    Under collapse, literal fallback rules use the wire form (`^/users/Andr%C3%A9$`) while
    attribution/evidence keys use the decoded anchorPath form (`^/users/André$`); the same
    observation therefore emits different YAML text with collapse on vs off. Documented trade-off
    (KNOWN_LIMITATIONS #8, Decision 2), but the only explain/collapse integration test uses ASCII
    paths (explain_l7_collapse_test.go:31), so the encoded-path divergence is untested.
  suggestion: add one encoded-path case to TestExplainCompatibleWithCollapsedRun or note the
    divergence next to the evidence key docs.
- id: R-04
  severity: INFO
  file: pkg/hubble/aggregator.go:554–563, 576–600
  description: >
    Cross-flush staleness is bounded and self-healing: a (port, proto) bucket with no flows in a
    later window keeps its earlier literal rules untouched until the port reappears, at which point
    replace-mode wipes them in favor of the whole-run set. Stale-but-correct literals never widen.
    No action required; recorded so the behavior is not mistaken for a bug later.
