---
phase: 07-l7-infrastructure-prep
plan: 02
subsystem: evidence
tags: [evidence, schema, json, l7, http, dns, tdd]

requires:
  - phase: 07-01
    provides: "policy merge semantics fix (mergePortRules preserves Rules) — independent of schema bump but lands on the same branch"
provides:
  - "Evidence on-disk schema bumped from v1 to v2"
  - "Optional L7Ref struct (Protocol, HTTPMethod, HTTPPath, DNSMatchName) reserved on RuleEvidence with omitempty"
  - "Reader and writer reject any non-v2 schema with a wipe instruction naming `$XDG_CACHE_HOME/cpg/evidence/`"
  - "TDD-first commit ordering: failing test commit precedes the schema bump"
affects: [phase-08-http-l7, phase-09-dns-l7, evidence-cache, upgrade-ux]

tech-stack:
  added: []
  patterns:
    - "TDD-first: failing test lands in a separate commit before the implementation that makes it pass"
    - "Schema-version literal in the rejection error (incident-response grep-bait)"
    - "omitempty on optional sub-objects to preserve byte-stable JSON for unchanged shapes"

key-files:
  created:
    - "pkg/evidence/reader_test.go"
  modified:
    - "pkg/evidence/schema.go"
    - "pkg/evidence/reader.go"
    - "pkg/evidence/writer.go"
    - "pkg/evidence/schema_test.go"
    - "pkg/evidence/writer_test.go"
    - "cmd/cpg/explain_test.go"
    - "cmd/cpg/replay_test.go"

key-decisions:
  - "No back-compat reader path for v1 evidence: v1.1 shipped 2026-04-24, no production caches in flight"
  - "Identical wipe-instruction error in both reader (read path) and writer (merge path) so all upgrade-edge surfaces give the same UX"
  - "L7Ref placed between Protocol and FlowCount in RuleEvidence (groups identity fields together)"
  - "Sub-fields HTTPMethod / HTTPPath / DNSMatchName are all omitempty so HTTP variants do not leak DNS keys and vice-versa"

patterns-established:
  - "Schema-bump commits must mention `$XDG_CACHE_HOME/cpg/evidence/` in the body so `git log --grep=evidence` surfaces them during incident response"
  - "Reader and writer schema rejection paths share the same actionable message"

requirements-completed: [EVID2-01]

duration: 3 min
completed: 2026-04-25
---

# Phase 7 Plan 02: Evidence Schema v1 -> v2 Summary

**Evidence on-disk schema bumped from v1 to v2 with optional L7Ref (HTTP/DNS) and a wipe-instruction rejection naming `$XDG_CACHE_HOME/cpg/evidence/` for grep-bait upgrade UX.**

## Performance

- **Duration:** ~3 min
- **Started:** 2026-04-25T07:31:12Z
- **Completed:** 2026-04-25T07:34:30Z
- **Tasks:** 2 (TDD: failing test + implementation)
- **Files modified:** 7 (1 created, 6 modified)

## Accomplishments

- `SchemaVersion` constant bumped 1 -> 2 in `pkg/evidence/schema.go`.
- New `L7Ref` struct (`Protocol`, `HTTPMethod`, `HTTPPath`, `DNSMatchName`) reserved on disk; all sub-fields except `Protocol` are `omitempty`.
- `RuleEvidence` carries `L7 *L7Ref \`json:"l7,omitempty"\`` so v1.1 L4-only evidence shape is byte-identical modulo the schema_version bump.
- Reader rejection message updated to include the literal string `$XDG_CACHE_HOME/cpg/evidence/` plus a `rm -rf $XDG_CACHE_HOME/cpg/evidence/` wipe instruction.
- Writer mismatch path (refusing-to-merge against stale v1 files) carries the same actionable wipe message.
- TDD-first commit ordering preserved: `2a963a1` (failing test) precedes `084f6fe` (bump).

## Exact rejection message (grep-bait)

> `unsupported schema_version <N> in <path> (this cpg understands 2). v1.2 evidence schema is incompatible with previous versions. Wipe the evidence cache and re-run cpg: rm -rf $XDG_CACHE_HOME/cpg/evidence/`

This string appears verbatim — with the leading `$` and no shell expansion — so operators can grep both the rejection error and `git log --all --grep="\$XDG_CACHE_HOME/cpg/evidence"` for the upgrade audit trail.

## Task Commits

Two-commit TDD history on `master`:

1. **Task 1: Failing test for v2 reader rejection message format** — `2a963a1` (test)
   - Adds `TestReader_RejectsNonV2SchemaWithWipeInstruction` (table-driven, asserts both v1 and v3 are rejected with the literal grep-bait string and a wipe hint).
   - Fails on master (3 sub-test failures) before the bump lands.

2. **Task 2: Bump schema v1 to v2 with optional L7Ref** — `084f6fe` (feat)
   - `SchemaVersion = 2`, `L7Ref` struct, `RuleEvidence.L7` field with `omitempty`.
   - Reader and writer rejection messages updated.
   - Existing `SchemaVersion == 1` assertions in `pkg/evidence` and `cmd/cpg` test fixtures bumped to 2.
   - New tests: `TestWriter_EmitsSchemaV2`, `TestRuleEvidence_OmitsL7WhenNil`, `TestRuleEvidence_RoundTripsHTTPL7`, `TestRuleEvidence_RoundTripsDNSL7`.

## Files Created/Modified

- `pkg/evidence/schema.go` — `SchemaVersion = 2`, new `L7Ref` struct, `RuleEvidence.L7 *L7Ref` field, schema bump comment.
- `pkg/evidence/reader.go` — Rejection error names `$XDG_CACHE_HOME/cpg/evidence/` and instructs `rm -rf`.
- `pkg/evidence/writer.go` — `refusing to merge` path carries the same wipe instruction.
- `pkg/evidence/reader_test.go` — **created** `TestReader_RejectsNonV2SchemaWithWipeInstruction` (table-driven for v1 and v3).
- `pkg/evidence/schema_test.go` — `SchemaVersion: 1` assertions bumped to `2`.
- `pkg/evidence/writer_test.go` — `assert.Equal(t, 1, …)` bumped to `2`; added `TestWriter_EmitsSchemaV2`, `TestRuleEvidence_OmitsL7WhenNil`, `TestRuleEvidence_RoundTripsHTTPL7`, `TestRuleEvidence_RoundTripsDNSL7`.
- `cmd/cpg/explain_test.go` — fixture `SchemaVersion: 1` bumped to `2`.
- `cmd/cpg/replay_test.go` — round-trip assertion `1` bumped to `2`.

## Decisions Made

- **No back-compat reader path.** v1.1 shipped 2026-04-24, no v1 caches expected in production; reader rejects v1 explicitly with a wipe hint instead of attempting any silent migration.
- **Wipe message is identical in reader and writer.** Both read-time rejection and write-time mismatch use the same actionable string; users hit the same UX regardless of which path discovers the stale file first.
- **Phase 7 reserves the L7 shape only.** No writer populates `L7` in this plan — Phase 8 (HTTP) and Phase 9 (DNS) will light up the codegen branches.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None. The grep `pkg/evidence/`/`cmd/`/`pkg/hubble/` for `schema_version` / `SchemaVersion` surfaced two extra fixture sites (`cmd/cpg/explain_test.go`, `cmd/cpg/replay_test.go`) that hardcoded `1`; these were updated to `2` in the same commit as the bump (in scope per Task 2 step 4).

## User Setup Required

None at the codebase level. **Operators upgrading from cpg v1.1 to v1.2 must wipe `$XDG_CACHE_HOME/cpg/evidence/`** (or `~/.cache/cpg/evidence/` on the XDG fallback path) before running `cpg generate` or `cpg replay`. The first run against a stale cache will fail with the grep-bait error message naming the directory.

## Next Phase Readiness

- Phase 8 (HTTP L7 codegen) can now write `RuleEvidence.L7 = &L7Ref{Protocol: "http", HTTPMethod: ..., HTTPPath: ...}` directly on disk without another schema bump.
- Phase 9 (DNS L7 codegen) can populate `RuleEvidence.L7 = &L7Ref{Protocol: "dns", DNSMatchName: ...}` symmetrically.
- `cpg explain` (Phase 9 plan for `--http-method` / `--http-path` / `--dns-pattern` filters) reads the L7 sub-object directly — no decoder changes needed.
- All v1.1 happy-path tests for evidence read/write continue to pass with no shape change beyond the version bump.

## Verification

- `go test ./pkg/evidence/... -count=1` — 22 passed.
- `go test ./... -count=1` — 216 passed.
- `grep -rn "SchemaVersion = " pkg/evidence/` returns exactly `pkg/evidence/schema.go:15: const SchemaVersion = 2`.
- `git log --all --grep="\$XDG_CACHE_HOME/cpg/evidence"` surfaces both `2a963a1` (failing test) and `084f6fe` (bump) — the audit trail an incident responder needs.

## Self-Check: PASSED

- All 9 declared key-files exist on disk.
- Both task commits (`2a963a1`, `084f6fe`) present in `git log --oneline --all`.
- `git log --all --grep="\$XDG_CACHE_HOME/cpg/evidence"` returns both commits (incident-response audit trail OK).
- `go test ./...` green (216 passed).

---
*Phase: 07-l7-infrastructure-prep*
*Plan: 02*
*Completed: 2026-04-25*
