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

## Milestone: v1.5 — MCP Integration

**Shipped:** 2026-07-22
**Phases:** 4 (16-19) | **Plans:** 21 (44 tasks) | **Sessions:** ~4 (one per phase pair + close)

### What Was Built
- `cpg mcp`: readonly MCP stdio server — protocol-safe wire (pre-swap stdout capture + global backstop, zapslog-bridged stderr logging), single-slot session lifecycle (`capturing→stopped→gone`, retained-stopped queryability, bounded cleanup fan-out on every exit path), 5 paginated query tools with typed schemas and fail-closed cursors
- Structural readonly proof: RTA/SSA reachability audit (0 K8s write verbs unconditionally, 5-function fs allowlist), mutation-tested diagnostics
- Real-subprocess stdio e2e under `-race`: graceful 8-tool lifecycle + ungraceful-disconnect variant; README MCP harness section
- Tests 484 → 610; shipped via PR #18 with a same-day GO-2026-5970 fix (x/text v0.39.0)

### What Worked
- Interface-first wave splits (17-01 OnFinal hook; 19-02 building the relay `started` signal 19-04 would consume) let dependent plans land without touching shared infra.
- Research-by-execution de-risked the two hard Phase 19 problems before planning: the naive CHA/RTA scan's 70-102 false positives were discovered empirically, and the e2e async-GetFlows race was found by running the design, not reviewing it.
- The plan-checker caught a false CONTEXT claim (dropclass enum on `get_evidence`) before it reached code — goal-backward verification against source, not against upstream docs.
- Per-phase code review + same-day fixes kept debt at zero across all four phases (the audit self-check tautology and the subprocess kill-guard would have been latent landmines).
- Manual `merge --no-ff` per worktree (skipping the buggy cleanup helper) ran 5 waves across phases 18-19 with zero conflicts.

### What Was Inefficient
- Phase 17 needed three gap-closure rounds on the same truth (SESS-03 autonomous stop) — each verification narrowed the guard but the class (every pipeline exit path must release the slot) could have been enumerated once up front.
- The worktree cleanup helper (`worktree.cleanup-wave`) self-blocks whenever a SUMMARY is committed in the worktree — every wave paid a manual-merge detour; the helper is effectively abandoned for this repo.
- A `v1.5` GSD tag would have collided with release-please's unrelated product `v1.5.0/v1.5.1` — milestone numbering and product SemVer diverged long ago; caught at close, worth deciding once per repo instead.

### Patterns Established
- Security claims ship as reachability proofs, not conventions: the SEC-01 audit is re-runnable and auto-sweeps future tools; the guarantee's wording ("readonly by default") is now load-bearing product text.
- MCP tool contract: typed `structuredContent` + `outputSchema`, truthful annotations, taxonomy-teaching descriptions, actionable `isError` — the template for any future tool.
- E2e against a fake in-process gRPC relay via the `server` bypass (no cluster, no kubeconfig) is the standard harness for transport-level proofs.
- Milestone close decisions: no GSD git tags (release-please owns tags); phase dirs archived per milestone.

### Key Lessons
1. Whole-program callgraph scans over a `package main` mixing surfaces need function-level rooting — package/import-level scanning cannot express "reachable from the MCP composition root" and drowns in third-party dispatch.
2. Empirical validation during research (build the risky thing once, against the real repo) converts planning unknowns into verified designs — both Phase 19 hard problems were solved before the planner ran.
3. A subprocess e2e is only as honest as its synchronization: "the tool returned" ≠ "the pipeline reached the relay" ≠ "artifacts hit disk" — each assertion needs its own gate or it flakes.
4. New vuln advisories land on their own schedule: a PR can fail govulncheck for a dependency that was clean yesterday; treat the bump (minimal, to the advisory's fixed version) + full `-race` regression as routine.

### Cost Observations
- Model mix: sonnet executors/researcher/checker/verifier, opus planner + code reviewer, Fable 5 orchestrating
- Sessions: ~4 across 3 calendar days (2026-07-20 → 2026-07-22); ~2.3M subagent tokens on Phase 19 + close
- Notable: worktree-parallel wave 1 (3 plans) completed in ~23 min wall-clock; the audit test costs ~60-75s per `-race` CI run, budgeted deliberately

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Sessions | Phases | Key Change |
|-----------|----------|--------|------------|
| v1.0–v1.3 | n/a | 13 | TDD-first gsd plan execution (see milestone archives) |
| v1.4 | 1 | 2 | Direct multi-agent workflow (review → verify → fix → PR) replacing per-phase plans for audit work |
| v1.5 | ~4 | 4 | Full gsd pipeline (discuss --auto → plan → worktree-parallel execute → review-fix → verify) per phase; research-by-execution for hard designs |

### Cumulative Quality

| Milestone | Tests | Lint issues | Reachable vulns |
|-----------|-------|-------------|-----------------|
| v1.3 | 418 | ungated (CI never ran) | unknown (never scanned in CI) |
| v1.4 | 484 | 26 (baseline 30, zero new; debt scoped to v1.5) | 0 |
| v1.5 | 610 | 26 (zero new; LINT-01..03 debt still open, now v1.6+ candidate) | 0 (GO-2026-5970 fixed in-flight) |

### Top Lessons (Verified Across Milestones)

1. Drift-guard tests (schema versions, classifier sentinels) convert risky bumps into checklists — validated in v1.2 (evidence schema) and v1.4 (cilium bump).
2. Fail-before/pass-after regression tests per fix keep multi-agent changes reviewable — v1.3 quick tasks and v1.4 both relied on it.
