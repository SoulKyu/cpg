---
phase: 08-http-l7-generation
plan: 01
subsystem: policy
tags: [http, l7, regex, cilium, tdd, anti-feature]

# Dependency graph
requires:
  - phase: 07-l7-foundation
    provides: "L7Discriminator on RuleKey, sortL7Rules in normalizeRule, mergePortRules Rules-field preservation"
provides:
  - "extractHTTPRules(*flowpb.Flow) []api.PortRuleHTTP — pure-function HTTP L7 extraction"
  - "normalizeHTTPMethod(string) string — case+whitespace method normalizer (HTTP-02)"
  - "Path anchoring contract: ^regexp.QuoteMeta(path)$ with empty→^/$ (HTTP-03)"
  - "HTTP-05 anti-feature lint: dedicated test asserting Headers/Host/HeaderMatches stay zero"
affects:
  - "08-02 (BuildPolicy integration with --l7 flag)"
  - "08-03 (evidence_writer.go L7Ref population)"
  - "08-04 (replay end-to-end fixture tests)"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Pure-function L7 extraction primitives separated from builder integration"
    - "Stub-then-fail RED state (functions return nil so tests compile) before GREEN implementation"
    - "HTTP-05 enforced via writer-side lint test, not just godoc"

key-files:
  created:
    - "pkg/policy/l7.go"
    - "pkg/policy/l7_test.go"
  modified: []

key-decisions:
  - "Helpers kept package-private (lowercase) per CONTEXT decision — internal contract only"
  - "Single-entry return: extractHTTPRules returns at most one PortRuleHTTP per flow (the L4 builder accumulates across flows in 08-02)"
  - "net/url.Parse handles both bare paths and full URLs — no manual scheme-stripping needed"
  - "PortRuleHTTP in vendored cilium v1.19.1 has Host but NOT HostExact — assertion narrowed to Headers/Host/HeaderMatches"
  - "Empty path → ^/$ rather than dropping the entry, so root-path observations produce a valid rule"

patterns-established:
  - "TDD with stub-RED: write tests that compile against trivial stubs, watch them fail, then replace stubs with real implementation"
  - "Anti-feature CI enforcement: explicit unit test that constructs adversarial input (headers carrying Authorization/Cookie) and asserts the output has no header data"

requirements-completed: [HTTP-02, HTTP-03, HTTP-05]

# Metrics
duration: 12min
completed: 2026-04-25
---

# Phase 8 Plan 01: HTTP L7 Extraction Primitives Summary

**Pure-function `extractHTTPRules` + `normalizeHTTPMethod` helpers in `pkg/policy/l7.go` codifying HTTP-02 method casing, HTTP-03 regex path anchoring, and HTTP-05 (no headers) — fully unit-tested, zero production callers (Plan 08-02 wires them).**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-04-25
- **Completed:** 2026-04-25
- **Tasks:** 2 (TDD: RED → GREEN)
- **Files modified:** 2 (both created)

## Accomplishments
- `extractHTTPRules` converts `Flow.L7.Http` records into anchored, escaped Cilium `PortRuleHTTP` entries with normalized method casing.
- `normalizeHTTPMethod` codifies the HTTP-02 contract (`strings.ToUpper(strings.TrimSpace(s))`) as a reusable internal helper.
- `TestExtractHTTPRules_NeverEmitsHeaders` provides a writer-side lint enforcing HTTP-05 even when adversarial input carries `Authorization` / `Cookie` headers.
- `TestExtractHTTPRules_PathAnchored` property test compiles every emitted regex and asserts it matches the literal observed path while rejecting both prefix and suffix variants.
- Byte-stability for `--l7=false` preserved (no production code references the new helpers yet).

## Task Commits

Each task was committed atomically:

1. **Task 1: Failing tests + stubs (RED)** — `0317223` (test)
2. **Task 2: Implementation (GREEN)** — `3c271dd` (feat)

_TDD: test commit precedes implementation commit, per project convention._

## Files Created/Modified
- `pkg/policy/l7.go` — extraction primitives + file-level HTTP-05 anti-feature godoc.
- `pkg/policy/l7_test.go` — table-driven tests, HTTP-05 lint test, regex anchoring property test.

## Decisions Made
- **Package-private helpers** (lowercase `extractHTTPRules` / `normalizeHTTPMethod`) — explicitly called out in CONTEXT as an internal contract.
- **`net/url.Parse` for path extraction** — handles both bare paths and full URLs uniformly; query and fragment are naturally excluded from `URL.Path`. Defensive fallback strips `?` and `#` manually if Parse fails.
- **Empty path emits `^/$`** rather than dropping the rule — a flow with method but no URL path is still a valid HTTP observation against the root.
- **Single-entry return** — each call to `extractHTTPRules` operates on one flow, returning at most one rule. Multi-(method, path) accumulation happens in the caller (Plan 08-02 calls `extractHTTPRules` per flow inside the bucket walk).
- **Test in `package policy` (internal), not `policy_test`** — required to access the unexported helpers. Existing tests like `dedup_test.go` use `policy_test`; this file is the first internal-package test in the directory and that's intentional.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Bug] Removed `HostExact` assertion from anti-feature lint test**
- **Found during:** Task 1 (RED build)
- **Issue:** Plan's `<interfaces>` block lists `HostExact` as a field on `api.PortRuleHTTP`, but Cilium v1.19.1 (vendored) only exposes `Host`. Compile failed: `e.HostExact undefined`.
- **Fix:** Dropped the `HostExact` check; kept `Headers`, `Host`, `HeaderMatches` assertions which together cover the HTTP-05 anti-feature surface area present in this Cilium version.
- **Files modified:** `pkg/policy/l7_test.go`
- **Verification:** Build succeeded; HTTP-05 invariant still enforced against the actually-existing fields.
- **Committed in:** `0317223` (Task 1 commit — caught before the test file was first staged).

---

**Total deviations:** 1 auto-fixed (1 bug — interface-doc mismatch).
**Impact on plan:** Cosmetic; the anti-feature contract is still enforced against every header-related field that Cilium v1.19.1 actually exposes. The plan's interface comment will need updating if a future Cilium version reintroduces `HostExact`.

## Issues Encountered
None beyond the deviation above.

## User Setup Required
None — pure code change, no external service configuration.

## Next Phase Readiness
- `extractHTTPRules` + `normalizeHTTPMethod` ready for Plan 08-02 to wire into `BuildPolicy` behind the `--l7` flag.
- `--l7=false` byte-stability with v1.1 outputs is preserved (no production code path references the new helpers yet — verified via `go test ./...` 262 passes).
- The HTTP-05 anti-feature lint will travel with any future change to `extractHTTPRules`; if a maintainer ever populates Headers/Host/HeaderMatches the test fails immediately.

## Self-Check: PASSED

- Files exist:
  - `/home/gule/Workspace/team-infrastructure/cpg/pkg/policy/l7.go` — FOUND
  - `/home/gule/Workspace/team-infrastructure/cpg/pkg/policy/l7_test.go` — FOUND
- Commits exist:
  - `0317223` test(08-01): add failing tests for HTTP extraction primitives — FOUND
  - `3c271dd` feat(08-01): implement HTTP L7 extraction primitives — FOUND
- `go test ./pkg/policy/...` — 31 tests in scope passed.
- `go test ./...` — 262 tests passed across 9 packages.
- `go vet ./...` — clean.

---
*Phase: 08-http-l7-generation*
*Completed: 2026-04-25*
