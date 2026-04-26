---
phase: 09-dns-l7-generation
plan: 03
subsystem: cli

tags: [cobra, cpg-explain, l7-filter, evidence-v2, http, dns]

requires:
  - phase: 07-l7-infrastructure-prep
    provides: evidence schema v2 RuleEvidence.L7 (L7Ref) with omitempty json/yaml tags
  - phase: 08-http-l7-generation
    provides: HTTP L7Ref population in evidence (Protocol/HTTPMethod/HTTPPath)
  - phase: 09-dns-l7-generation/01
    provides: DNS L7Ref population in evidence (Protocol/DNSMatchName)
provides:
  - cpg explain --http-method / --http-path / --dns-pattern exact-match filters
  - cpg explain text-format L7 line per rule (HTTP method+path or DNS matchName)
  - JSON/YAML L7 sub-object pass-through (locked by tests against future struct refactors)
affects: [09-04, README, v1.3-l7-explain]

tech-stack:
  added: []
  patterns:
    - L7 filters AND-combine; any L7 filter set drops L4-only rules
    - Input normalization at flag boundary (uppercase HTTP method, strip trailing dot on DNS pattern); literal exact match on storage values
    - Renderer L7 line uses 2-space indent + 13-char label column to align with Peer/Port/Flow count/First seen/Last seen

key-files:
  created: []
  modified:
    - cmd/cpg/explain.go
    - cmd/cpg/explain_filter.go
    - cmd/cpg/explain_filter_test.go
    - cmd/cpg/explain_render.go
    - cmd/cpg/explain_test.go

key-decisions:
  - Long-only Cobra flags (no short forms) — consistent with existing explain filters
  - Literal exact match (no regex/glob) — wildcard/regex deferred to v1.3
  - HTTP path filter compares against the stored anchored regex (e.g. ^/foo$) verbatim — no decoding/un-anchoring; matches what the user sees in evidence and YAML output
  - JSON/YAML rendering requires zero code changes — schema v2 L7Ref already has correct json tags; tests added to lock the contract
  - L4-only output byte-identical to v1.1 when no L7 filter is set and r.L7 == nil

patterns-established:
  - "L7 filter gating: 'if any L7 filter set then require r.L7 != nil' before per-field check — guarantees protocol/field mismatch returns false consistently"
  - "Per-field protocol gating: HTTPMethod/HTTPPath require Protocol=='http'; DNSPattern requires Protocol=='dns' — prevents accidental cross-protocol matches"

requirements-completed: [L7CLI-02, L7CLI-03]

duration: 3min
completed: 2026-04-25
---

# Phase 9 Plan 03: explain L7 filters + L7 rendering Summary

**cpg explain gains --http-method, --http-path, --dns-pattern exact-match filters and renders L7 attribution per rule in text/JSON/YAML formats.**

## Performance

- **Duration:** 3 min
- **Started:** 2026-04-25T11:44:01Z
- **Completed:** 2026-04-25T11:47:09Z
- **Tasks:** 2 (both TDD)
- **Files modified:** 5

## Accomplishments

- Three new long-only Cobra flags on `cpg explain`: `--http-method`, `--http-path`, `--dns-pattern`.
- `explainFilter.match()` gains an L7 branch that drops L4-only rules when any L7 filter is set, AND-combines filters, and per-field gates by Protocol.
- Text renderer adds a single `L7: HTTP <METHOD> <PATH>` or `L7: DNS <name>` line per rule when `r.L7 != nil`, aligned with the existing label column.
- JSON / YAML rendering carries the `l7` sub-object verbatim via the existing schema v2 `L7Ref` json tags (omitempty); tests added to lock the contract against future refactors.
- v1.1 output preserved byte-for-byte for L4-only evidence (no `L7:` line, no `l7` JSON key).

## Task Commits

1. **Task 1 RED:** `144810e` — `test(09-03): add failing tests for explain L7 filter flags`
2. **Task 1 GREEN:** `28f8331` — `feat(explain): --http-method, --http-path, --dns-pattern filters (L7CLI-02)`
3. **Task 2 RED:** `4d02093` — `test(09-03): add failing tests for explain L7 rendering`
4. **Task 2 GREEN:** `6b13d6b` — `feat(explain): render L7 attribution in text/JSON/YAML (L7CLI-03)`

## Files Created/Modified

- `cmd/cpg/explain.go` — added 3 string flags, `strings` import, buildFilter normalization (uppercase method, trim+strip-trailing-dot on dns-pattern).
- `cmd/cpg/explain_filter.go` — extended `explainFilter` struct with HTTPMethod/HTTPPath/DNSPattern; added L7 branch to `match()` after the existing checks.
- `cmd/cpg/explain_filter_test.go` — 5 new test functions covering HTTP method, HTTP path, DNS pattern, AND combination, and L4-only-with-no-L7-filters preservation.
- `cmd/cpg/explain_render.go` — `writeRule()` emits the `L7:` line between `Last seen:` and `Sample flows:` when `r.L7 != nil`, switching on `Protocol`.
- `cmd/cpg/explain_test.go` — 7 new tests: buildFilter normalization & defaults, text HTTP/DNS/L4-only, JSON HTTP + L4-only-omits-l7, YAML DNS.

## Decisions Made

- **Long-only flags:** consistent with existing explain filters; no short flag pollution.
- **Literal exact match:** no regex / glob in v1.2 (deferred to v1.3). User-supplied `--http-path` compares against the stored anchored regex (e.g. `^/foo$`) verbatim — surprising at first read but it matches what the user sees in `cpg explain --json` output and in the generated YAML, so it is the principled choice.
- **JSON/YAML rendering = zero code change:** schema v2 `L7Ref` already has the correct `json:"...,omitempty"` tags and `sigs.k8s.io/yaml.Marshal` honors json tags. Added contract-locking tests so a future struct refactor cannot silently break the wire format.
- **Per-field protocol gating:** `--http-method` on a DNS rule returns false (Protocol mismatch); `--dns-pattern` on an HTTP rule returns false. Prevents nonsensical cross-protocol matches and keeps the AND semantics intuitive.
- **L7 line layout:** `  L7:          ` matches the existing 2-space indent + 13-char label width used by Peer / Port / Flow count / First seen / Last seen. Single line per L7 entry per the plan spec.

## Deviations from Plan

None — plan executed exactly as written. Both tasks followed the prescribed TDD RED → GREEN cadence; no auto-fixes triggered; no architectural questions surfaced.

Minor stylistic note: the plan suggested `    L7: HTTP …` with 4-space indent in the user-prompt summary, while the plan body suggested `  L7:          ` (2-space indent + label-padded-to-13). I followed the plan body since it explicitly says "match it" against the existing labels — and the existing labels use 2-space indent. Tests assert via `Contains` on `L7:` and `HTTP GET …` / `DNS …` substrings, so both interpretations satisfy the tests; the implemented form aligns with v1.1 visual style.

## Issues Encountered

None.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- L7CLI-02 and L7CLI-03 closed. Phase 9 plans 1–3 complete; only `09-04` remains (README two-step workflow + starter visibility CNP, VIS-02 + VIS-03).
- v1.2 explain surface is feature-complete: filters AND-combine across L4 + L7, three output formats render L7 attribution.
- No blockers for plan 09-04 (docs-only).

## Self-Check: PASSED

- `cmd/cpg/explain.go` — FOUND
- `cmd/cpg/explain_filter.go` — FOUND
- `cmd/cpg/explain_filter_test.go` — FOUND
- `cmd/cpg/explain_render.go` — FOUND
- `cmd/cpg/explain_test.go` — FOUND
- commit `144810e` — FOUND
- commit `28f8331` — FOUND
- commit `4d02093` — FOUND
- commit `6b13d6b` — FOUND
- `go test ./...` — 317 passed in 9 packages
- `go build ./...` — clean

---
*Phase: 09-dns-l7-generation*
*Completed: 2026-04-25*
