---
phase: 21-cilium-compatibility-matrix-runtime-detection
plan: 02
subsystem: docs
tags: [readme, cilium, compatibility, golden-test, documentation]

# Dependency graph
requires: []
provides:
  - "README '## Supported Cilium versions' section: one documented floor (>= 1.14) + PR-verified per-feature floor table"
  - "Corrected proxy-visibility annotation boundary (<= 1.16, removed at 1.17) replacing the shipped '<= 1.19' documentation bug"
  - "cmd/cpg/readme_compat_test.go golden test pinning both version tokens and PR citations against regression"
affects: [21-01-runtime-detection, 21-03, 21-04, future-doc-edits-to-README]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Golden doc-consistency test: package main test reads ../../README.md via os.ReadFile + strings.Contains assertions (no shell grep, no cluster, no build tags) — first precedent of this kind in the repo"

key-files:
  created:
    - cmd/cpg/readme_compat_test.go
  modified:
    - README.md

key-decisions:
  - "PR-reference pinning added to the golden test beyond Task 2's literal action list, to satisfy threat model T-21-02-01's stated mitigation ('the golden test pins the PR references and version tokens') — see Deviations"
  - "Kept 'Marked deprecated upstream' framing (reworded to 'before removal') rather than deleting it — it was not itself flagged as wrong by research, only the ≤1.19 claim was"
  - "No explicit <a id> anchor added for '## Supported Cilium versions' — relies on GitHub's default heading-to-slug conversion, consistent with all other README headings except 'L7 Prerequisites' (which needs its anchor referenced from log messages)"

requirements-completed: [COMPAT-01, COMPAT-03]

# Metrics
duration: ~12min
completed: 2026-07-22
---

# Phase 21 Plan 02: Supported Cilium Versions Documentation Summary

**New README "Supported Cilium versions" section (floor >= 1.14 + 7-row PR-cited feature table) and the COMPAT-03 fix replacing the shipped "proxy-visibility ≤ 1.19" bug with the correct ≤1.16/removed-at-1.17 boundary, locked in by a new golden consistency test.**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-07-22T14:15:00+02:00 (approx, first edit)
- **Completed:** 2026-07-22T14:21:22+02:00 (last commit)
- **Tasks:** 2 (plan), 3 commits (one extra Rule 2 deviation commit)
- **Files modified:** 2 (README.md modified, cmd/cpg/readme_compat_test.go created)

## Accomplishments

- Authored an entirely new `## Supported Cilium versions` README section (no prior "Compatibility"/"Supported" heading existed anywhere in the file) — one documented floor (`>= 1.14`) plus a 7-row per-feature table transcribed verbatim from 21-RESEARCH.md's Version Pin Table, including the two previously-unpinned entries (`Verdict_AUDIT` >= 1.10, observer `GetNodes()` >= 1.10) alongside the three milestone-pre-verified ones (`cilium-dbg` >= 1.15, `enableDefaultDeny` >= 1.16, `proxy-visibility` removed at 1.17).
- Fixed the shipped COMPAT-03 documentation bug: the proxy-visibility list item no longer claims "still widely supported (Cilium ≤ 1.19)" — it now states the annotation works only through Cilium 1.16 and was removed from the agent runtime at 1.17 (a no-op on 1.17+), cross-referencing the new compat table.
- Extended the `pkg/k8s/` project-structure one-liner to mention "version detection" (forward-looking for 21-01's runtime detection code).
- Added `cmd/cpg/readme_compat_test.go` (`TestReadmeCompatSection`): a deterministic, pure-file-read golden test asserting the section heading, all four version tokens (1.14/1.15/1.16/1.17), the three merged-PR citations (#28085/#30572/#35019), the COMPAT-03 negative guard (no line ever pairs `proxy-visibility` with `1.19` again), and a positive guard that the removal boundary is actually stated in the same paragraph as a `proxy-visibility` mention.
- Empirically verified the test's regression-catching power via a scratch/restore cycle (not committed): confirmed it fails when the heading is renamed, and fails when the `proxy-visibility`/`1.19` pairing is reintroduced; confirmed README.md was restored byte-identical (md5 match) afterward.

## Task Commits

Each task was committed atomically:

1. **Task 1: Add the "Supported Cilium versions" section + fix the proxy-visibility boundary** - `35d33f7` (docs)
2. **Task 2: Add cmd/cpg/readme_compat_test.go golden consistency test** - `1e90a54` (test)
3. **Deviation follow-up: pin merged-PR citations in the golden test** - `2f7eecb` (test, Rule 2)

_No plan-metadata commit — worktree mode; orchestrator handles STATE.md/ROADMAP.md after merge._

## Files Created/Modified

- `README.md` — new `## Supported Cilium versions` section (lines 64-80, between `## Install` and `## Quick start`); corrected proxy-visibility paragraph (lines 304-316, `### Three ways to enable L7 visibility` item 1); `pkg/k8s/` project-structure line extended with "version detection"
- `cmd/cpg/readme_compat_test.go` — new file, `TestReadmeCompatSection` + `proxyVisibilityBoundaryStated` helper (97 lines)

## Decisions Made

- Used markdown link style `[#NNNNN](https://github.com/cilium/cilium/pull/NNNNN)` for PR citations in the table, matching 21-RESEARCH.md's own citation style, since no existing README precedent for PR links existed to follow.
- Cross-referenced the new compat table from the corrected proxy-visibility paragraph as plain bold text ("See the **Supported Cilium versions** table above") rather than a markdown anchor link, avoiding any dependency on GitHub's heading-to-slug anchor generation for a plan-scope cross-reference that only needs to be readable, not clickable.
- Kept the table to exactly the 7 rows the plan enumerated (no additional rows invented), in the plan's specified order.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Golden test did not pin merged-PR citations, per threat model T-21-02-01's own stated mitigation**
- **Found during:** Post-Task-2 threat-model review (before final self-check)
- **Issue:** The plan's `<threat_model>` register assigns threat `T-21-02-01` (Information Disclosure via misinformation) a `mitigate` disposition, with mitigation text explicitly stating: "the golden test pins the PR references and version tokens so a later unsourced edit is caught." Task 2's own `<action>` list (which I followed literally for the initial implementation) enumerated four assertions — heading, version tokens, negative 1.19 guard, positive boundary guard — none of which pin the PR reference strings (`#28085`, `#30572`, `#35019`). The initial test therefore satisfied Task 2's literal action list and all of its `<acceptance_criteria>`, but left a real gap against the threat model's own documented mitigation: a future edit that quietly dropped a PR citation from the README table (while leaving the bare version number) would not have been caught.
- **Fix:** Added three additional `assert.Contains` checks in `TestReadmeCompatSection` for `#28085`, `#30572`, `#35019` — the exact three PR numbers Task 1's own `<acceptance_criteria>` already requires as literal strings in the README, so the test's scope stays anchored to an already-agreed, verifiable requirement rather than expanding arbitrarily.
- **Files modified:** `cmd/cpg/readme_compat_test.go`
- **Verification:** `rtk proxy go test ./cmd/cpg/... -run TestReadmeCompatSection -count=1 -v` passes; `gofmt -l` clean; `go vet ./cmd/cpg/...` clean; `golangci-lint run ./cmd/cpg/...` reports 0 issues.
- **Committed in:** `2f7eecb`

---

**Total deviations:** 1 auto-fixed (Rule 2).
**Impact on plan:** Strengthens the golden test's regression coverage to match the plan's own threat-model mitigation text; no scope creep beyond what Task 1's acceptance criteria and the threat model already required. Zero behavior changes, zero new dependencies.

## Issues Encountered

None. Both README edits landed exactly where the plan specified (verified against the exact line numbers/content read from the file before editing), and the golden test passed on the first implementation attempt.

## User Setup Required

None — no external service configuration required. Documentation + one pure-Go test file, zero new dependencies (threat register item `T-21-02-SC` explicitly accepts this: "Docs + one pure-stdlib test file; zero new packages").

## Verification Performed

- `rg -n "## Supported Cilium versions" README.md` — matches exactly once, at line 64, positioned between `## Install` and `## Quick start`.
- `rg -n "proxy-visibility" README.md | rg "1\.19"` — empty (no line pairs the two strings).
- Version tokens `1.14`/`1.15`/`1.16`/`1.17` and PR references `#28085`/`#30572`/`#35019` all present and verified via `rg`.
- `rg -n "version detection" README.md` — matches the updated `pkg/k8s/` project-structure line.
- `rtk proxy go test ./cmd/cpg/... -run TestReadmeCompatSection -count=1` — green.
- `rtk proxy go test ./cmd/cpg/... -count=1` (full package) — green, 26.9s, no regressions from adding the new test file to `package main`.
- `rtk proxy go build ./...`, `rtk proxy go vet ./cmd/cpg/...`, `gofmt -l cmd/cpg/readme_compat_test.go` — all clean.
- `rtk proxy golangci-lint run ./cmd/cpg/...` — 0 issues.
- Regression-catching power of the new test empirically confirmed via a scratch/restore cycle (heading rename and 1.19-pairing reintroduction both correctly fail the test); README.md restored byte-identical (md5 verified) afterward, not committed.

## Next Phase Readiness

- COMPAT-01 and COMPAT-03 are fully shipped and regression-locked. No code behavior changed in this plan (documentation + test only), so 21-01 (runtime detection, COMPAT-02) can proceed independently — this plan's `pkg/k8s/` project-structure line update ("version detection") is purely descriptive and creates no code dependency.
- The new golden test lives in `cmd/cpg` (`package main`), same package 21-01's `cmd/cpg/generate.go`/`replay.go` changes will touch — no conflict expected since `readme_compat_test.go` is a self-contained new file with no shared symbols.
- No blockers for 21-03/21-04.

## Self-Check: PASSED

- FOUND: README.md
- FOUND: cmd/cpg/readme_compat_test.go
- FOUND: .planning/phases/21-cilium-compatibility-matrix-runtime-detection/21-02-SUMMARY.md
- FOUND commit: 35d33f7 (Task 1)
- FOUND commit: 1e90a54 (Task 2)
- FOUND commit: 2f7eecb (Rule 2 deviation)
- FOUND commit: 77983ea (this SUMMARY)
- Re-ran `rtk proxy go test ./cmd/cpg/... -run TestReadmeCompatSection -count=1` — green.
