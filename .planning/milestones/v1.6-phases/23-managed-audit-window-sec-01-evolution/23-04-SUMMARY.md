---
phase: 23-managed-audit-window-sec-01-evolution
plan: 04
subsystem: docs
tags: [readme, runbook, rbac, golden-tests, audit-window, cilium]

requires:
  - phase: 23-managed-audit-window-sec-01-evolution
    provides: "pkg/auditwindow.Manager (23-02) and the locked cpg audit-window CLI surface decision (CONTEXT.md) that this plan documents ahead of 23-03's compiled command"
provides:
  - "README.md 'Readonly by default' section: MCP readonly claim preserved + cpg audit-window as the sole mutating command + exclusive RBAC (pods/exec, ciliumendpoints) + Pitfall-4 scoping limitation"
  - "docs/bootstrap-runbook.md Enable/Disable Per-Endpoint Audit Mode sections rewritten to drive cpg audit-window --ttl, with honest new-endpoint race documentation"
  - "cmd/cpg/audit_docs_test.go: golden pins TestReadmeAuditWindowSection + TestRunbookAuditWindowStep"
affects: [23-03, 23-VALIDATION]

tech-stack:
  added: []
  patterns:
    - "golden-pin doc tests via strings.Contains over os.ReadFile (mirrors readme_compat_test.go / runbook_test.go style) -- no cluster, no build tags"

key-files:
  created:
    - cmd/cpg/audit_docs_test.go
  modified:
    - README.md
    - docs/bootstrap-runbook.md

key-decisions:
  - "Placed the new README section ('Readonly by default') between 'Supported Cilium versions' and 'Quick start' -- keeps the MCP section's 'never mutates' claim untouched while giving the mutating-command disclosure its own anchor (#readonly-by-default) that the runbook links back to"
  - "Kept the runbook's Enable Per-Endpoint Audit Mode section deriving $ENDPOINT/$CILIUM_POD (now framed as 'if you need these for the Hubble observation step') so the unchanged Observe/Verify sections stay internally consistent instead of referencing undefined shell variables"
  - "Used 'daemon-wide audit mode setting' prose (no hyphen) everywhere new text discusses the precondition/daemon-wide concept, per RESEARCH Pitfall 1 -- verified via grep that 'policy-audit-mode' appears nowhere outside the pre-existing leading warning block"

patterns-established:
  - "Doc-drift prevention: any future CLI-surface/RBAC claim in README or the runbook should get a strings.Contains golden pin in cmd/cpg/audit_docs_test.go (or a sibling file), not just prose trust"

requirements-completed: [AUD-03]

duration: ~25min
completed: 2026-07-22
---

# Phase 23 Plan 04: README/Runbook Audit-Window Doc Evolution Summary

**README now states "readonly by default, one scoped mutating exception" and the runbook drives the real `cpg audit-window --ttl` command with an honest new-endpoint race disclosure -- both locked in by two new golden-pin tests.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-22T16:24:00Z (approx, first file read)
- **Completed:** 2026-07-22T16:49:22Z
- **Tasks:** 2/2 completed
- **Files modified:** 3 (README.md, docs/bootstrap-runbook.md, cmd/cpg/audit_docs_test.go)

## Accomplishments
- README gained a "Readonly by default" section (anchor `#readonly-by-default`) that keeps the MCP server's "cpg never mutates your cluster and never writes outside its own session tmpdir" claim byte-for-byte, while introducing `cpg audit-window` as the one scoped, lifecycle-bound mutating command and disclosing its exclusive RBAC step-up (`pods/exec` create in `kube-system`, `ciliumendpoints` list/watch) plus the Pitfall-4 limitation that `pods/exec` cannot be RBAC-scoped to cilium-agent pods by name.
- `docs/bootstrap-runbook.md`'s "Enable Per-Endpoint Audit Mode" and "Disable Per-Endpoint Audit Mode" sections now drive the real `cpg audit-window -n <namespace> --ttl 30m` command instead of manual `kubectl exec cilium-dbg endpoint config` steps, and honestly document the new-endpoint race window (a brand-new endpoint can be briefly enforced instead of audited before the watch's flip lands) as documented-not-solved, matching the locked CONTEXT.md decision.
- Two new golden pins in `cmd/cpg/audit_docs_test.go` (`TestReadmeAuditWindowSection`, `TestRunbookAuditWindowStep`) lock all of the above so future edits cannot silently regress the readonly-claim/RBAC disclosure pairing or let the runbook drift back to manual exec steps.
- Confirmed via `grep` that the hyphenated `policy-audit-mode` token was not introduced anywhere in README.md, and the pre-existing `TestRunbookNeverSuggestsDaemonWideAudit` pin (which confines that token to the runbook's leading warning block) stays green.

## Task Commits

Each task was committed atomically:

1. **Task 1: README readonly-by-default + audit-window mutating-command + RBAC section, pinned** - `c8bf7b8` (docs)
2. **Task 2: Runbook wires real command + honest race, pinned; existing daemon-wide pin stays green** - `bd32e1a` (docs)

_No plan-metadata commit requested for this plan (SUMMARY-only commit per orchestrator instruction, no STATE.md/ROADMAP.md/REQUIREMENTS.md update)._

## Files Created/Modified
- `README.md` - Added "## Readonly by default" section (between "Supported Cilium versions" and "Quick start"): readonly-by-default statement, `cpg audit-window` mutating-command description, exclusive RBAC step-up, Pitfall-4 scoping limitation.
- `docs/bootstrap-runbook.md` - Rewrote "Enable Per-Endpoint Audit Mode" and "Disable Per-Endpoint Audit Mode" to drive `cpg audit-window --ttl`; added new-endpoint race note and RBAC callout; kept `$ENDPOINT`/`$CILIUM_POD` derivation (now explicitly scoped to the Hubble-observation use case) so downstream "Observe Policy Verdicts" / "Verify Enforcement" sections stay internally consistent.
- `cmd/cpg/audit_docs_test.go` (new) - `TestReadmeAuditWindowSection` (readonly claim + `cpg audit-window` + `PolicyAuditMode` + `pods/exec` + `ciliumendpoints` all present in README.md) and `TestRunbookAuditWindowStep` (`cpg audit-window`, `--ttl`, `race`, `pods/exec` all present in the runbook).

## Decisions Made
- Section placement: put the new README section right after "Supported Cilium versions" (which already links the bootstrap runbook) rather than inside the MCP section, so the MCP section's readonly claim is untouched and the mutating-command disclosure gets its own home the runbook can link back to.
- Did not touch "Observe Policy Verdicts" / "Verify Enforcement" runbook sections' actual commands (kubectl exec into cilium-agent for `hubble observe` and for reading `PolicyAuditMode` back) -- those are independent, read-only-adjacent operations from before this phase and out of this plan's file scope; only fixed the now-dangling variable derivation by keeping it in the Enable section with updated framing.
- No hyphenated `policy-audit-mode` token added in either doc's new prose; used "daemon-wide audit mode setting" throughout, per RESEARCH Pitfall 1.

## Deviations from Plan

None - plan executed exactly as written. Both `audit_docs_test.go` tests were written incrementally per-task (Task 1 added `TestReadmeAuditWindowSection` only; Task 2 appended `TestRunbookAuditWindowStep` to the same file) as the plan's task/commit structure specifies.

## Issues Encountered

The first `rtk proxy go test ./cmd/cpg/... -count=1 -race -timeout 300s` invocation exceeded the Bash tool's 120s foreground timeout and was moved to a background task; its output file remained empty for several minutes (env quirk, not a test failure). Re-ran the same command synchronously with an explicit longer client-side timeout and got a clean `ok ... 159.829s` — full package suite green, including `TestReadmeAuditWindowSection`, `TestRunbookAuditWindowStep`, `TestReadmeCompatSection`, and `TestRunbookNeverSuggestsDaemonWideAudit`. No test-code issue; purely a tooling/timeout artifact worth noting for future long `-race` runs in this repo.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- 23-03 (the compiled `cpg audit-window` cobra command + SEC-01 tripwire) can land independently — this plan's golden tests read markdown files only and have no dependency on the compiled command symbol existing yet.
- Once 23-03 lands, `cpg audit-window --help`'s actual flag text (`-n`/`--namespace`, `--ttl` default) should be spot-checked against this plan's runbook/README prose for drift, though no code changes are expected (the documented `--ttl 30m` default matches 23-RESEARCH.md's recommended default and 23-03-PLAN.md's stated default).
- `git diff --exit-code go.mod go.sum` confirmed clean (no new dependencies, docs/test-only change).

---
*Phase: 23-managed-audit-window-sec-01-evolution*
*Completed: 2026-07-22*
