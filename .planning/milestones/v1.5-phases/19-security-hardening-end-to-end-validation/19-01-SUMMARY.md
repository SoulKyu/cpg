---
phase: 19-security-hardening-end-to-end-validation
plan: 01
subsystem: security
tags: [go-ssa, callgraph, rta, static-analysis, security-audit, go-mod, golang.org/x/tools]

# Dependency graph
requires:
  - phase: 16-mcp-server-foundation-write-safety
    provides: runMCPServer composition root (cmd/cpg/mcp.go) that this audit's BFS is rooted at
  - phase: 17-session-lifecycle
    provides: pkg/session.Manager (Start/Shutdown$1), pkg/session.DeriveSessionPaths — 2 of the 5 allowlisted fs-write callers plus the tmpdir-layout single source of truth
  - phase: 18-query-tools
    provides: registerQueryTools composition, plus pkg/output/pkg/evidence/pkg/hubble atomic writers — the remaining 3 allowlisted callers
provides:
  - cmd/cpg/mcp_audit_test.go: TestMCPAuditReadonlyReachability, a re-runnable SEC-01 structural readonly audit
  - golang.org/x/tools promoted to a direct (test-only) go.mod dependency
affects: [19-02, 19-03, 19-04, any future MCP tool registration (registerXTools) work]

# Tech tracking
tech-stack:
  added: "golang.org/x/tools v0.44.0 (go/packages, go/ssa, go/ssa/ssautil, go/callgraph, go/callgraph/rta) — direct test-only dependency, zero new go.sum hashes (already-resolved transitive promotion)"
  patterns: "RTA-rooted-at-main+init -> BFS-restrict-to-module-owned-functions -> direct SSA call-instruction scan (no third-party recursion) for 'is dangerous capability X reachable from safe entry point Y' audits"

key-files:
  created: [cmd/cpg/mcp_audit_test.go]
  modified: [go.mod]

key-decisions:
  - "K8s write-verb match (Property 1, D-02) is verb-name-only on IsInvoke() calls, with NO receiver-type filtering and NO allowlist — baseline is zero, any hit is new (plan review dropped an earlier, under-specified type-shape check)"
  - "Filesystem-write allowlist (Property 2, D-03) is keyed per-function (SSA symbol string), not per-call-site — any watched os.* call from one of the 5 allowlisted functions is accepted, but a brand-new writer function still fails"
  - "Call-path diagnostic reconstructed via BFS parent pointers recorded during the Stage-2 BFS (not golang.org/x/tools/go/callgraph.PathSearch) — matches the plan's explicit design and avoids depending on PathSearch's unspecified traversal order"
  - "RTA rooted at main+init per its own documented contract, never at runMCPServer directly (off-label); runMCPServer is used only as the BFS start node over the resulting whole-program graph"

patterns-established:
  - "SEC-01-style structural audit: whole-program RTA reachability computed the documented way, then BFS-restricted to the analyzed module's own functions, then a direct (non-recursive) SSA call-instruction scan of only those functions — avoids the 70-102 spurious call paths a naive whole-program 'is X reachable' query produces on a codebase this size"

requirements-completed: [SEC-01]

# Metrics
duration: ~15min
completed: 2026-07-21
---

# Phase 19 Plan 01: SEC-01 Structural Readonly Audit Summary

**Re-runnable Go test proves zero K8s write verbs and zero unallowlisted filesystem writes are reachable from the MCP composition root, via RTA callgraph + direct SSA call-instruction scan; golang.org/x/tools promoted to a direct test-only dependency with zero new go.sum hashes.**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-07-21T18:16:11Z
- **Completed:** 2026-07-21T18:31:33Z
- **Tasks:** 2/2 completed
- **Files modified:** 2 (1 created, 1 modified)

## Accomplishments

- Implemented `TestMCPAuditReadonlyReachability` (`cmd/cpg/mcp_audit_test.go`): builds SSA for the whole program, computes a whole-program callgraph via RTA rooted at `main`+`init` (per RTA's documented contract), BFS-restricts the `runMCPServer` subgraph to cpg-owned functions, then directly scans only those functions' own SSA call instructions — never recursing into third-party callees.
- Encoded both SEC-01 properties: Property 1 (D-02) fails unconditionally on any reachable K8s write-verb (Create/Update/Patch/Delete/Apply) interface-dispatch call, no allowlist; Property 2 (D-03) fails on any reachable, unallowlisted call to a watched write-capable `os.*` function, checked against an exact 5-function allowlist (the atomic writers + session tmpdir lifecycle ops).
- Verified the failure mechanism end-to-end: temporarily broke one allowlist entry, confirmed the test failed with the exact expected diagnostic (offending function symbol + a full BFS call path from `runMCPServer`), then reverted — confirming the D-04 re-runnability contract actually works, not just that the happy path passes.
- Promoted `golang.org/x/tools` from indirect to direct in `go.mod` via `go mod tidy`; confirmed `go.sum` has a zero-line diff (a promotion of an already-hashed transitive dependency, not a new download).
- Confirmed `go build ./...` succeeds and the audit passes under `-race` (62.65s, within the ~45-76s budget); ran the full-suite regression (`go test ./... -race -count=1`) — all 12 packages green.
- Marked requirement **SEC-01** complete in `.planning/REQUIREMENTS.md` (checkbox + traceability table).

## Task Commits

Each task was committed atomically:

1. **Task 1: Implement the SEC-01 RTA reachability + direct-call-scan audit test** - `258decc` (feat)
2. **Task 2: Promote golang.org/x/tools indirect to direct and verify under -race with zero go.sum churn** - `3c91e0b` (chore)

**Plan metadata:** committed after this SUMMARY.md (see below)

## Files Created/Modified

- `cmd/cpg/mcp_audit_test.go` - `TestMCPAuditReadonlyReachability`: Stage 1 (packages.Load + ssautil.AllPackages + prog.Build), Stage 2 (rta.Analyze rooted at main+init, then bfsFromRoot/callPathFrom over the resulting callgraph), Stage 3 (direct call-instruction scan of cpg-owned reachable functions against `disallowedFSWrite`/`k8sWriteVerbs`/`fsWriteAllowlist`)
- `go.mod` - `golang.org/x/tools v0.44.0` moved from the indirect block to the direct require block (1 insertion, 1 deletion; net line count in the file unchanged)

## Decisions Made

- Followed the plan's Task 1 `<action>` text (the corrected, authoritative design) over the RESEARCH.md/PATTERNS.md pseudocode's shared `assertAllowlisted(...)` naming for both properties: implemented Property 1 (K8s write verbs) as an unconditional fail-on-any-hit with no allowlist, and Property 2 (fs writes) as an allowlist check — these are genuinely different semantics and the plan explicitly flags the pseudocode's verb-property naming as a pre-correction leftover.
- Used hand-rolled BFS with parent pointers (not `golang.org/x/tools/go/callgraph.PathSearch`) for call-path reconstruction, matching the plan's explicit instruction to "record parent pointers during the Stage-2 BFS" — keeps the path-reconstruction mechanism fully understood/owned rather than depending on an unspecified-traversal-order library utility.
- No golden files; no `-timeout` flag added below 120s, per plan and RESEARCH.md Pitfall 5 guidance.

## Deviations from Plan

None - plan executed exactly as written. Both tasks matched their `<action>` specifications precisely; no bugs, missing functionality, or blocking issues were encountered that required auto-fixing under Rules 1-3, and no architectural questions arose under Rule 4.

## Issues Encountered

None. The design was empirically pre-validated in 19-RESEARCH.md against this exact repository, and the implementation reproduced the validated result on the first test run (0 violations, 13.35s without `-race`; 62.65s with `-race`, both within budget).

## User Setup Required

None - no external service configuration required. `golang.org/x/tools` was already a hashed, resolved transitive dependency; the `go mod tidy` promotion required no network fetch beyond what was already in the module cache/go.sum.

## Next Phase Readiness

- SEC-01 is fully satisfied and re-runnable: any future `registerXTools` call or new MCP tool handler is automatically swept into this same audit with zero test edits.
- `golang.org/x/tools` is now a direct dependency, available without further `go.mod` changes for any other test in `cmd/cpg` that might need SSA/callgraph analysis (e.g. if 19-02's e2e work needs related tooling, though its own RESEARCH.md scope does not call for it).
- No blockers for 19-02/19-03/19-04 (SRV-04 e2e test, SEC-03 README section) — this plan's `depends_on: []` and wave-1 status mean those plans were not gated on this one, but this plan introduces no conflicting file changes with them (`cmd/cpg/mcp_audit_test.go` is a new, isolated file; `go.mod`'s single-line move is unlikely to conflict).

---
*Phase: 19-security-hardening-end-to-end-validation*
*Completed: 2026-07-21*

## Self-Check: PASSED

- FOUND: `cmd/cpg/mcp_audit_test.go`
- FOUND: `.planning/phases/19-security-hardening-end-to-end-validation/19-01-SUMMARY.md`
- FOUND commit: `258decc` (Task 1)
- FOUND commit: `3c91e0b` (Task 2)
