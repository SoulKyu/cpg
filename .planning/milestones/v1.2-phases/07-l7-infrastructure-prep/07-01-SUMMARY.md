---
phase: 07-l7-infrastructure-prep
plan: 01
subsystem: policy
tags: [policy, merge, normalize, dedup, l7, http, dns, rulekey, evidence]

requires:
  - phase: 01-foundation
    provides: pkg/policy domain — BuildPolicy, MergePolicy, PoliciesEquivalent, RuleKey, normalizeRule
provides:
  - "mergePortRules preserves PortRule.Rules across merges (EVID2-03)"
  - "L4-only fast path keeps v1.1 byte-stable output"
  - "L7 path groups by (port, protocol) and union-merges Rules.HTTP / Rules.DNS by canonical key"
  - "normalizeRule deterministically sorts Rules.HTTP by (Method, Path) and Rules.DNS by MatchName (EVID2-04)"
  - "RuleKey carries an optional *L7Discriminator; nil-L7 keys stringify byte-identically to v1.1 (EVID2-02)"
affects: [08-* HTTP plans, 09-* DNS plans — both rely on these primitives without further policy-layer churn]

tech-stack:
  added: []
  patterns:
    - "TDD-first: regression test for the merge bug landed in a separate FAILING commit BEFORE the fix commit, auditable in git log"
    - "Internal-package test file (`package policy`) used to exercise the private `mergePortRules` directly — same pattern as `attribution_test.go`"
    - "Fast path / slow path split in `mergePortRules` so the L4-only behavior remains byte-identical to v1.1 — invariant for all existing fixtures"
    - "Optional pointer fields on key structs (RuleKey.L7) with omitempty-style stringification — backward-compatible extension"

key-files:
  created:
    - pkg/policy/merge_l7_test.go
  modified:
    - pkg/policy/merge.go
    - pkg/policy/dedup.go
    - pkg/policy/attribution.go

key-decisions:
  - "TDD invariant honored: failing-test commit (68bd4ef) precedes fix commit (6d87aa0). git log shows the regression bug in isolation before the fix — self-documenting in history."
  - "L4-only fast path retained byte-for-byte: `mergePortRules` checks `portRulesCarryL7` upfront. Any side carrying Rules switches to the (port, protocol)-keyed grouping path; otherwise the original flatten-into-result[0] is preserved verbatim. All v1.1 fixtures pass with zero expectation edits."
  - "HTTP dedup key includes Headers / HeaderMatches even though HTTP-05 forbids cpg from generating them. Defensive: protects against future hand-edited or third-party rules accidentally collapsing into a header-less observation."
  - "DNS dedup key is (MatchName, MatchPattern) so v1.3's matchPattern wildcard work (DNS-FUT-01) does not break dedup semantics retroactively."
  - "RuleKey.L7 stringification uses `:l7=key=val,key=val` with omitempty parts — single appended segment keeps the existing 4-segment v1.1 prefix intact for log parsers / map key consumers."
  - "L7Rules' OneOf-shaped HTTP/DNS coexistence is NOT special-cased in Phase 7 — a comment in `mergePortRules` names the future Phase 8/9 work for explicit refusal-with-warn at the build layer."

patterns-established:
  - "Bucket-keyed merge for L7-aware port rules: `map[port+\"/\"+proto]*bucket` plus an `order []string` slice for stable insertion-order output. Reusable for any future per-(port,proto) accumulation."
  - "`portRulesCarryL7(api.PortRules) bool` predicate as the fast/slow path discriminator — keep this primitive small and inline-able."
  - "L7 sort helper `sortL7Rules(*api.L7Rules)` is a no-op on nil and on len ≤ 1 — preserves nil-vs-empty distinction (Pitfall 12) without branching at every callsite."

requirements-completed: [EVID2-02, EVID2-03, EVID2-04]

duration: ~25min
completed: 2026-04-25
---

# Phase 07 Plan 01: pkg/policy L7-readiness — mergePortRules / normalizeRule / RuleKey Summary

**Three pkg/policy correctness fixes that Phase 8 (HTTP) and Phase 9 (DNS) depend on: `mergePortRules` now preserves `PortRule.Rules` across merges, `normalizeRule` deterministically sorts L7 sub-lists for byte-stable YAML, and `RuleKey` carries an optional L7 discriminator so two rules differing only in HTTP method/path don't dedup into the same evidence bucket. Zero user-visible behavior change for v1.1 inputs (209/209 tests green, no expectation edits).**

## Performance

- **Duration:** ~25 min
- **Tasks:** 2 (TDD: failing-test commit then fix commit)
- **Files modified:** 1 created, 3 modified
- **Commits:**
  - `68bd4ef` — `test(07-01): add failing regression test for mergePortRules Rules-drop bug (EVID2-03)`
  - `6d87aa0` — `fix(policy): preserve Rules in mergePortRules + sort L7 lists in normalizeRule + L7 discriminator on RuleKey (EVID2-02, EVID2-03, EVID2-04)`

## Accomplishments

### EVID2-03 — `mergePortRules` Rules preservation (`pkg/policy/merge.go`)

- New `mergePortRules` (`pkg/policy/merge.go:172-271`):
  - **L4-only fast path**: when neither side has `PortRule.Rules` set, behavior is identical to v1.1 — single PortRule, merged Ports list, `Rules == nil`. All v1.1 fixtures pass byte-stable.
  - **L7 path**: when any side carries `Rules`, group by `port + "/" + protocol`. Each (port, proto) bucket produces one merged PortRule whose Rules are union-merged via `mergeL7Rules`.
- New helpers:
  - `portRulesCarryL7(api.PortRules) bool` — fast/slow path discriminator.
  - `mergeL7Rules(a, b *api.L7Rules) *api.L7Rules` — union-merge HTTP and DNS sub-lists (first observation wins on order; new entries append; duplicates skipped).
  - `httpRuleKey(api.PortRuleHTTP) string` — canonical dedup key including Headers / HeaderMatches (defensive).
  - `dnsRuleKey(api.PortRuleDNS) string` — `(MatchName, MatchPattern)` dedup key.

### EVID2-04 — `normalizeRule` deterministic L7 sort (`pkg/policy/dedup.go`)

- `sortL7Rules(*api.L7Rules)` (`pkg/policy/dedup.go:84-103`):
  - HTTP entries sorted by `(Method, Path)` lex (`sort.SliceStable`).
  - DNS entries sorted by `MatchName` lex.
  - nil Rules → no-op. `len ≤ 1` → no spurious sort. Empty (non-nil) lists stay non-nil (Pitfall 12).
- Wired into `normalizeRule` (`pkg/policy/dedup.go:50-66`) for both ingress and egress PortRules.

### EVID2-02 — `RuleKey` L7 discriminator (`pkg/policy/attribution.go`)

- New `L7Discriminator` struct (`pkg/policy/attribution.go:31-39`): `Protocol`, `HTTPMethod`, `HTTPPath`, `DNSMatchName` — all omitempty in encoding.
- `RuleKey.L7 *L7Discriminator` (`pkg/policy/attribution.go:50`) added as optional.
- `RuleKey.String()` (`pkg/policy/attribution.go:60-66`):
  - `L7 == nil` → `"direction:peer:protocol:port"` byte-identical to v1.1.
  - `L7 != nil` → appends `":l7=proto=…,method=…,path=…,dns=…"` with omitempty parts.
- `encodeL7(*L7Discriminator) string` formats the appended segment.

### Test coverage (`pkg/policy/merge_l7_test.go`, internal-package test)

| Test | Assertions |
| ---- | ---------- |
| `TestMergePortRules_PreservesRules` | 5 sub-cases: existing-only Rules, incoming-only Rules, both Rules union-merge by (Method, Path), different ports each preserve own Rules, L4-only fast path byte-stable. |
| `TestMergePortRules_L4OnlyByteIdentical` | Multi-port L4-only merge collapses to single PortRule with `Rules == nil`, ordering identical to v1.1. |
| `TestNormalizeRule_SortsL7Lists` | 5 sub-cases: HTTP sorted by (Method, Path); DNS sorted by MatchName; nil Rules untouched; empty (non-nil) lists stay empty; single-element list untouched. |
| `TestRuleKey_L7Discriminator` | 4 sub-cases: nil L7 → v1.1 byte-identical; HTTP discriminator appended; HTTPPath difference produces distinct strings; DNS discriminator appended. |

## Verification

```text
$ go test ./pkg/policy/... -count=1
ok  github.com/SoulKyu/cpg/pkg/policy   (69 tests passed)

$ go test ./... -count=1
209 passed in 9 packages
```

- **TDD audit trail (`git log --oneline`)**: failing-test commit `68bd4ef` precedes fix commit `6d87aa0`. The Rules-drop bug is auditable in isolation against `master^^` (the test alone fails on the pre-fix tree).
- **Byte-stability invariant**: zero expectation edits to any pre-existing test in `pkg/policy/`, `pkg/output/`, or anywhere downstream. The L4-only fast path in `mergePortRules` mirrors the original implementation line-for-line in semantics.
- **Surface area unchanged**: no schema, no CLI flags, no pipeline wiring. This is pure pkg/policy-internal correctness work — Phase 7 plans 02 (schema v2), 03 (preflight), and 04 (CLI plumbing) build on top without touching this layer again.

## Deviations from Plan

None — plan executed exactly as written. The plan's `<files_modified>` list mentioned `dedup_test.go` and `attribution_test.go` as candidate test homes; I consolidated all new tests into `merge_l7_test.go` (single internal-package test file) for better cohesion since the three changes all participate in the same L7-readiness story. Existing internal-package precedent: `attribution_test.go`, `builder_attribution_test.go`. No expectation changes to either of those files.

## Self-Check: PASSED

- `pkg/policy/merge_l7_test.go` — FOUND
- `pkg/policy/merge.go` — modified, contains `mergeL7Rules`, `portRulesCarryL7`, `httpRuleKey`, `dnsRuleKey`
- `pkg/policy/dedup.go` — modified, contains `sortL7Rules`
- `pkg/policy/attribution.go` — modified, contains `L7Discriminator`, `encodeL7`, `RuleKey.L7`
- Commit `68bd4ef` — FOUND in `git log`
- Commit `6d87aa0` — FOUND in `git log`
- Commit ordering: TDD-correct (test before fix)
- `go test ./... -count=1` — 209 passed across 9 packages
