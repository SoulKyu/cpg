---
phase: 22-bootstrap-artifact-generation
plan: 03
subsystem: docs
tags: [runbook, readme, compat-matrix, golden-test, AUD-02]
dependency_graph:
  requires: []
  provides:
    - docs/bootstrap-runbook.md
    - cmd/cpg/runbook_test.go (TestRunbookNeverSuggestsDaemonWideAudit)
    - README.md enableDefaultDeny row cross-reference to cpg bootstrap
  affects:
    - cmd/cpg/readme_compat_test.go (TestReadmeCompatSection, extended)
tech_stack:
  added: []
  patterns:
    - "golden text-pinning tests (os.ReadFile + strings.Contains, no shell grep)"
    - "block-scan helper (region-restriction assertion, mirrors proxyVisibilityBoundaryStated)"
key_files:
  created:
    - docs/bootstrap-runbook.md
    - cmd/cpg/runbook_test.go
  modified:
    - README.md
    - cmd/cpg/readme_compat_test.go
decisions:
  - "Runbook headings mirror the upstream 11-section order positionally, but the two 'Entire Daemon' sections (enable/disable) are intentionally omitted as actionable steps -- daemon-wide policy-audit-mode is mentioned only in the leading warning block, never as a how-to step, per the plan's core invariant."
  - "README Task 2: extended the existing single enableDefaultDeny/1.16 row's Notes cell with a cpg bootstrap / get_bootstrap_policy cross-reference + runbook link, per 22-RESEARCH.md Open Question 1 resolution -- no duplicate row added, preserving COMPAT-01's single-source-of-truth."
metrics:
  duration: "~35 minutes"
  completed: 2026-07-22
---

# Phase 22 Plan 03: Bootstrap Runbook + README Compat Cross-Reference Summary

Wrote `docs/bootstrap-runbook.md` modeled 1:1 on Cilium's "Creating Policies from Verdicts" phase order (live-reverified against docs.cilium.io 1.19.6), with a first-lines-only warning against daemon-wide `policy-audit-mode`, pinned by a new golden test; extended the existing README `enableDefaultDeny`/1.16 compat row to cross-reference `cpg bootstrap` without duplicating it.

## What Was Built

### Task 1: `docs/bootstrap-runbook.md` + `cmd/cpg/runbook_test.go`

- **Assumption A2 re-verification (required by the plan):** No WebFetch tool was available in this environment; used `curl` via Bash to fetch `https://docs.cilium.io/en/stable/security/policy-creation/` directly (HTTP 200, Cilium 1.19.6 docs) and extracted the `<h2>` heading sequence. Confirmed the exact 11-section order already captured in 22-RESEARCH.md: Setup Cilium / Deploy the Demo Application / Scale down the deathstar Deployment / Enable Policy Audit Mode (Entire Daemon) / Enable Policy Audit Mode (Specific Endpoint) / Observe policy verdicts / Create the Network Policy / Disable Policy Audit Mode (Entire Daemon) / Disable Policy Audit Mode (Specific Endpoint) / Verify Policy Audit Mode is Disabled / Clean-up. Also confirmed the exact hyphenated ConfigMap key (`policy-audit-mode`) and the per-endpoint `cilium-dbg endpoint config <id> PolicyAuditMode=Enabled/Disabled` command shape used in the runbook's actionable sections.
- `docs/bootstrap-runbook.md` (174 lines): leading blockquote warning (before any `## ` heading) against daemon-wide `policy-audit-mode`, followed by 10 `## ` sections mirroring the upstream order: Prerequisites, Bootstrap the Namespace, Deploy / Scale Considerations, Enable Per-Endpoint Audit Mode, Observe Policy Verdicts, Capture with cpg generate --include-audit, Create and Apply Generated Policies, Disable Per-Endpoint Audit Mode, Verify Enforcement, Clean-up. The two upstream "Entire Daemon" sections are deliberately not present as actionable steps -- their content lives only in the leading warning, per the plan's core invariant.
- `cmd/cpg/runbook_test.go`: `TestRunbookNeverSuggestsDaemonWideAudit` (mirrors `readme_compat_test.go`'s `os.ReadFile` + `strings.Contains` idiom, `package main`, no shell grep) asserts: (1) file exists/non-empty; (2) contains `cpg generate --include-audit` verbatim; (3) every line containing the hyphenated token `policy-audit-mode` falls strictly before the first `## ` heading (block-scan helper `firstSectionHeadingIndex`, analogous to `proxyVisibilityBoundaryStated`); (4) the warning block itself mentions `policy-audit-mode` paired with a caution word (not/never/avoid/danger), via `warningBlockCautionsAgainstDaemonWideAudit`.

### Task 2: README compat row cross-reference + `readme_compat_test.go` extension

- `README.md:78` -- the existing `enableDefaultDeny` CNP field row's Notes cell extended: `PR #30572 -- used by \`cpg bootstrap\` / \`get_bootstrap_policy\`; see the [bootstrap runbook](docs/bootstrap-runbook.md)`. The `>= 1.16` floor and `#30572` citation are unchanged and on the same row -- no duplicate row added.
- Added a one-line pointer immediately after the compat table (optional per plan, included for discoverability) linking `docs/bootstrap-runbook.md` and mentioning `cpg bootstrap -n <namespace>`.
- `cmd/cpg/readme_compat_test.go`: extended `TestReadmeCompatSection` (same function, same already-read `readme` string, no duplicate file read) with two new assertions -- README must contain `docs/bootstrap-runbook.md` and `cpg bootstrap` -- placed after the existing PR-citation loop, before the line-scan checks.

## Deviations from Plan

### Environment adjustment (not a Rule 1-4 deviation, tooling substitution only)

- **WebFetch tool unavailable in this executor's toolset (Read/Write/Edit/Bash only).** The plan's Task 1 `<action>` requires "First WebFetch `https://docs.cilium.io/en/stable/security/policy-creation/`". Substituted `curl -A "Mozilla/5.0" -L` via Bash, which achieved the identical goal (re-verify Assumption A2 against the live current docs page) with an equivalent result: HTTP 200, exact 11-section `<h2>` order extracted and cross-checked against 22-RESEARCH.md's State of the Art table -- a full match, no drift found. No content changed as a result; this is a tooling substitution, not a plan deviation.

### Rule 3 - worktree behind master

- **Found during:** Setup, before Task 1.
- **Issue:** This worktree's branch (`worktree-agent-aacd6e16157a0d728`) was created from commit `fed42d7`, 50 commits behind `master`. The `.planning/phases/22-bootstrap-artifact-generation/` directory (including `22-03-PLAN.md` itself) did not exist in the worktree, along with all of Phase 20/21 source code the runbook and README changes reference (`--include-audit`, Cilium version detection).
- **Fix:** Verified the worktree branch had zero unique commits not already in `master` (`git log --oneline master..HEAD` empty), then fast-forwarded (`git merge --ff-only master`) to bring in the missing plan files and source code. This was a pure fast-forward (no merge commit, no conflict, no rebase of local work) since the worktree branch was strictly an ancestor of master.
- **Files affected:** none beyond the fast-forward itself (80 files from upstream commits, none touched further by this plan).
- **Commit:** N/A (fast-forward, not a new commit) -- `git merge --ff-only master` updated `fed42d7..99d9d9d`.

None - no code-level auto-fixes were needed; both tasks executed as written.

## Verification Results

- `rtk proxy go test ./cmd/cpg/... -run TestRunbookNeverSuggestsDaemonWideAudit -count=1 -race` -- PASS
- `rtk proxy go test ./cmd/cpg/... -run TestReadmeCompatSection -count=1 -race` -- PASS
- `rtk proxy go test ./cmd/cpg/... -run "TestRunbookNeverSuggestsDaemonWideAudit|TestReadmeCompatSection" -count=1 -race` -- PASS (both)
- `rg -c "enableDefaultDeny.*CNP field" README.md` -- returns `1` (no duplicate row)
- `rg -n "docs/bootstrap-runbook.md" README.md` -- matches (lines 78, 83)
- `rg -n "cpg generate --include-audit" docs/bootstrap-runbook.md` -- matches (heading + body prose, verbatim)
- `git diff --stat go.mod go.sum` -- empty (no dependency additions)
- `rtk proxy go build ./...` -- succeeds, no errors
- `rtk proxy go vet ./cmd/cpg/...` -- clean
- `rtk proxy go test ./cmd/cpg/... -count=1 -race` (full package, per validation strategy's "after every task commit" sampling extended to full-package confidence check, includes `TestMCPAuditReadonlyReachability` SEC-01 audit) -- PASS, 151.9s (`ok github.com/SoulKyu/cpg/cmd/cpg 151.880s`)

## Auth Gates

None encountered.

## Known Stubs

None. Both deliverables are complete, non-placeholder content pinned by passing golden tests.

## Threat Flags

None. This plan's `<threat_model>` fully covers the surface touched (T-22-03-01 runbook drift, T-22-03-02 README fragmentation) and no new network/auth/filesystem surface was introduced -- pure docs + text-golden tests, zero `go.mod` changes.

## Self-Check: PASSED

- FOUND: docs/bootstrap-runbook.md
- FOUND: cmd/cpg/runbook_test.go
- FOUND: commit ab70c42 (docs(22-03): add bootstrap + audit-onboarding runbook with golden test)
- FOUND: commit 74745c1 (docs(22-03): cross-reference enableDefaultDeny compat row to cpg bootstrap)
