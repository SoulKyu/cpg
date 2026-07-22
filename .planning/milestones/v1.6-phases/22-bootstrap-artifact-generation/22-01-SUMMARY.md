---
phase: 22-bootstrap-artifact-generation
plan: 01
subsystem: policy
tags: [cilium, ciliumnetworkpolicy, default-deny, cilium-35558, testify, sigs.k8s.io-yaml]

# Dependency graph
requires:
  - phase: 21-cilium-compatibility-matrix-runtime-detection
    provides: pkg/k8s.DetectCiliumVersion + featureFloors table (consumed by 22-02, not this plan)
provides:
  - "pkg/policy.BuildBootstrapPolicy(namespace string) *ciliumv2.CiliumNetworkPolicy — the single in-memory constructor of a namespaced default-deny CNP, reused identically by the CLI command (22-02) and the MCP tool (22-02)"
  - "The named cilium/cilium#35558 regression acceptance test (TestBuildBootstrapPolicy_EnforcesDefaultDeny_Sanitizes)"
affects: [22-02-bootstrap-cli-and-mcp-tool, 22-03-runbook-and-readme]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "One-element-empty-rule-object pattern ([]api.IngressRule{{}} / []api.EgressRule{{}}) for default-deny CNPs — passes Sanitize(), survives marshal (unlike the empty-slice form which is the #35558 bug itself)"
    - "Bootstrap artifact naming built by direct string concatenation (default-deny-<ns>), independent of PolicyName/policyNamePrefix, to stay out of generate's cpg-* merge/dedup logic"

key-files:
  created:
    - pkg/policy/bootstrap_builder.go
    - pkg/policy/bootstrap_builder_test.go
  modified: []

key-decisions:
  - "Followed 22-RESEARCH.md's empirically-verified Code Examples section verbatim for the struct construction, per the plan's explicit instruction to NOT copy 22-PATTERNS.md's inline sketch (which does not compile)"
  - "Test package is policy_test (external test package), matching pkg/policy/builder_test.go's existing convention"
  - "Reworded a doc comment to avoid literally containing the forbidden-helper token strings (PolicyName/policyNamePrefix) so the acceptance-criteria grep for forbidden usage doesn't false-positive on an explanatory comment"

requirements-completed: [AUD-02]

# Metrics
duration: ~15min
completed: 2026-07-22
---

# Phase 22 Plan 01: Bootstrap CNP Builder + #35558 Regression Test Summary

**`pkg/policy.BuildBootstrapPolicy` constructs a Sanitize()-passing, marshal-surviving default-deny CiliumNetworkPolicy using the one-element-empty-rule form, pinned by a named cilium/cilium#35558 acceptance test.**

## Performance

- **Duration:** ~15 min
- **Completed:** 2026-07-22T14:32:51Z
- **Tasks:** 2/2 completed
- **Files modified:** 2 created

## Accomplishments
- `BuildBootstrapPolicy(namespace string) *ciliumv2.CiliumNetworkPolicy` — namespaced default-deny CNP constructor, zero new `go.mod` dependencies
- Named acceptance test asserting `Spec.Sanitize()` succeeds on the real vendored `api.Rule` (not a string-substring check), guarding the exact failure class of cilium/cilium#35558
- Verified naming (`default-deny-<ns>`, never `cpg-*`), select-all endpoint selector, both `EnableDefaultDeny` directions true, and marshaled-YAML token survival (`enableDefaultDeny`, `- {}`, no `ingress: []`/`egress: []`)

## Task Commits

Each task was committed atomically:

1. **Task 1: Create pkg/policy/bootstrap_builder.go — BuildBootstrapPolicy** - `2a1b0fc` (feat)
2. **Task 2: Create pkg/policy/bootstrap_builder_test.go — the named #35558 acceptance test** - `a5af49d` (test)

**Plan metadata:** (this commit, see below)

## Files Created/Modified
- `pkg/policy/bootstrap_builder.go` - `BuildBootstrapPolicy`, the namespaced default-deny CNP constructor
- `pkg/policy/bootstrap_builder_test.go` - the named `TestBuildBootstrapPolicy_EnforcesDefaultDeny_Sanitizes` acceptance test

## Decisions Made
- Transcribed the struct construction verbatim from 22-RESEARCH.md's "Code Examples" section (empirically verified against vendored `github.com/cilium/cilium@v1.19.4`), not from 22-PATTERNS.md's non-compiling sketch, per the plan's explicit `read_first` warning
- Reworded one doc comment (see Deviations) to keep the acceptance-criteria forbidden-pattern grep accurate

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Worktree was 74 commits behind master; phase 22 planning files were missing**
- **Found during:** Setup, before Task 1
- **Issue:** This worktree's branch (`worktree-agent-a01e27ebcab4939c4`) was forked from an older commit (`fed42d7`) that predates the phase 22 planning commits on `master` (`247d13e`, `99d9d9d`). None of the plan/research/validation files referenced in the task prompt existed in the worktree.
- **Fix:** Ran `git merge --ff-only master` inside the worktree. This was a pure fast-forward (worktree branch had zero unique commits ahead of master at that point), so it is non-destructive and safe per the destructive-git-prohibition rules (no rebase, no reset --hard, no force-push involved).
- **Files modified:** None directly — brought 80 files (including all of `.planning/phases/22-bootstrap-artifact-generation/`) into the worktree working tree via fast-forward.
- **Verification:** `git log --oneline -3` showed the worktree HEAD now matches master's tip; `.planning/phases/22-bootstrap-artifact-generation/22-01-PLAN.md` etc. became readable.
- **Committed in:** N/A (fast-forward merge, not a new commit — the worktree's own commit list is unchanged, just advanced).

**2. [Rule 1 - Bug] Doc comment triggered the plan's own forbidden-pattern grep**
- **Found during:** Task 1, post-write verification
- **Issue:** The initial doc comment on `BuildBootstrapPolicy` explained the naming decision by literally writing "never via PolicyName/policyNamePrefix", which matched the acceptance-criteria grep `rg -n "PolicyName|policyNamePrefix|..."` intended to catch actual usage of those forbidden helpers — a false positive from an explanatory comment, not real usage.
- **Fix:** Reworded the comment to describe the same rationale without using the literal token strings (now says "independent of this package's per-workload naming helper").
- **Files modified:** `pkg/policy/bootstrap_builder.go`
- **Verification:** `rg -n "PolicyName|policyNamePrefix|NewESFromMatchRequirements|DefaultDeny\{" pkg/policy/bootstrap_builder.go` returns no match; `go build`/`go vet` still clean.
- **Committed in:** `2a1b0fc` (part of Task 1 commit — edited before first commit, no separate commit needed)

---

**Total deviations:** 2 auto-fixed (1 environment/setup bug, 1 self-verification bug)
**Impact on plan:** Both fixes necessary to execute the plan as specified; neither changed the plan's intended code shape or scope.

## Issues Encountered
None beyond the deviations above.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
`BuildBootstrapPolicy` is ready for reuse by 22-02's CLI command (`cpg bootstrap`) and MCP tool (`get_bootstrap_policy`) — both are specified to call it identically. No blockers.

---
*Phase: 22-bootstrap-artifact-generation*
*Completed: 2026-07-22*

## Self-Check: PASSED
- FOUND: pkg/policy/bootstrap_builder.go
- FOUND: pkg/policy/bootstrap_builder_test.go
- FOUND: commit 2a1b0fc
- FOUND: commit a5af49d
