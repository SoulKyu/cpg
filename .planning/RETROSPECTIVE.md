# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v1.4 — Audit Fable5

**Shipped:** 2026-07-20
**Phases:** 2 | **Plans:** 0 (direct multi-agent workflow, no gsd plans) | **Sessions:** 1

### What Was Built
- All 29 confirmed findings from a Fable 5 full-code review fixed with regression tests (PR #16, 13 commits, 44 files, 484 tests at close vs 418 at v1.3)
- The repository's first-ever running (and green) CI: `master` trigger fix, SHA-pinned actions, pinned tools, `only-new-issues` lint gate
- Security posture: cilium v1.19.4 + x/net v0.55.0 (two reachable vulns) + `toolchain go1.25.12` (patched stdlib)

### What Worked
- Review pipeline shape: 9 section reviewers → adversarial verification (killed 2/31 false positives, including a literal placeholder finding) → 6 package-group fixers → independent Opus review of the PR. Each layer caught something the previous one missed.
- Grouping fixer agents by *package* (not by review section) eliminated same-file collisions (`SessionStats` was shared by two sections) with zero worktree overhead.
- The repo's own drift guards did their job: `TestClassifierVersionMatchesGoMod` forced the DropReason audit on the cilium bump; `TestClassifyAllKnownReasons` was the audit.

### What Was Inefficient
- CI bring-up was a 3-round loop (lint debt → vulns → stale stdlib toolchain); first-ever pipeline activation on latent debt should be budgeted as its own step.
- Two fixer agents introduced their own errcheck debt (fix code with unchecked `Close`); caught only by re-linting the converged tree.
- One agent pinned `actions/checkout` to an untagged branch-tip commit labeled `# v4` — pin claims need `git ls-remote` verification, not trust.

### Patterns Established
- Milestone executed as: full review → confirmed-findings workflow → single PR → independent review → merge. GSD phases used as verification framing, not execution units.
- `only-new-issues: true` is a *temporary* gate with a named exit condition (v1.5 LINT-01..03 → drop the flag).
- Dependency bumps follow the sentinel protocol: bump → audit DropReason coverage → move `ClassifierVersion` suffix.

### Key Lessons
1. A CI that has never run is a liability disguised as green — the `main`/`master` trigger mismatch silently disabled every gate since the repo's creation.
2. Adversarial verification pays for itself: 2 false positives never reached the fix queue; the PR reviewer's one Important finding (untagged pin) was itself a verification-class catch.
3. `setup-go` with `go-version-file` resolves the module *minimum*, not a patched toolchain — the `toolchain` directive is the right knob for stdlib CVEs.

### Cost Observations
- Model mix: ~100% opus subagents (review + verify + fix + PR review), Fable 5 orchestrating; sonnet only for the GSD roadmapper
- Sessions: 1 (review → fixes → PR → CI green → milestone close in a single session, ~2.0M subagent tokens)
- Notable: package-group parallel fixing turned a 29-finding backlog into a merged PR in one day

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Sessions | Phases | Key Change |
|-----------|----------|--------|------------|
| v1.0–v1.3 | n/a | 13 | TDD-first gsd plan execution (see milestone archives) |
| v1.4 | 1 | 2 | Direct multi-agent workflow (review → verify → fix → PR) replacing per-phase plans for audit work |

### Cumulative Quality

| Milestone | Tests | Lint issues | Reachable vulns |
|-----------|-------|-------------|-----------------|
| v1.3 | 418 | ungated (CI never ran) | unknown (never scanned in CI) |
| v1.4 | 484 | 26 (baseline 30, zero new; debt scoped to v1.5) | 0 |

### Top Lessons (Verified Across Milestones)

1. Drift-guard tests (schema versions, classifier sentinels) convert risky bumps into checklists — validated in v1.2 (evidence schema) and v1.4 (cilium bump).
2. Fail-before/pass-after regression tests per fix keep multi-agent changes reviewable — v1.3 quick tasks and v1.4 both relied on it.
