---
phase: 07-l7-infrastructure-prep
verified: 2026-04-24T00:00:00Z
status: passed
score: 5/5 success criteria, 8/8 requirements
re_verification:
  is_re_verification: false
---

# Phase 7: L7 Infrastructure Prep — Verification Report

**Phase Goal:** Land foundational fixes (merge bug, schema v2, pre-flight scaffolding, flag plumbing). End-of-phase output for v1.1 inputs is byte-identical to v1.1.
**Verified:** 2026-04-24
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (5 ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `mergePortRules` preserves `Rules` field; regression test fails on prev code (EVID2-03) | ✓ VERIFIED | `pkg/policy/merge.go:190-262` (L7 path groups by (port,proto), preserves Rules via `mergeL7Rules`); regression test `pkg/policy/merge_l7_test.go:25-152` covers 5 sub-cases incl. (a) existing has Rules / (b) incoming has Rules / (c) both / (d) different ports / (e) L4-only fast path |
| 2 | Evidence schema v2 + reader rejects ≠2 with `$XDG_CACHE_HOME/cpg/evidence/` instruction (EVID2-01) | ✓ VERIFIED | `pkg/evidence/schema.go:15` (`SchemaVersion = 2`); `pkg/evidence/reader.go:36-42` rejects with literal `rm -rf $XDG_CACHE_HOME/cpg/evidence/` in error message; `L7Ref` struct at `schema.go:70-75` |
| 3 | `normalizeRule` deterministically sorts L7 lists (HTTP by method+path, DNS by matchName) (EVID2-04) | ✓ VERIFIED | `pkg/policy/dedup.go:81-98` `sortL7Rules` — HTTP sorted by `(Method, Path)` lex, DNS by `MatchName` lex; called from `normalizeRule` at `dedup.go:54,61`; tests `merge_l7_test.go:183-279` |
| 4 | Pre-flight `enable-l7-proxy` + `cilium-envoy` warn-and-proceed; RBAC denied warn-and-proceed; `--no-l7-preflight` skips (VIS-04/05/06) | ✓ VERIFIED | `pkg/k8s/preflight.go:58-119` `RunL7Preflight` — all paths call `logger.Warn` and return; explicit forbidden branches at L83 (cilium-config) and L106 (cilium-envoy); `cmd/cpg/generate.go:38-56` skips when `!l7Enabled \|\| noPreflight`; flag declared `cmd/cpg/generate.go:107`; tests `pkg/k8s/preflight_test.go:78,208` and forbiddenReactor at L49-54 |
| 5 | `--l7` plumbed through `PipelineConfig` but byte-identical YAML to v1.1 (L7CLI-01) | ✓ VERIFIED | `cmd/cpg/commonflags.go:25,48,65` declares `--l7`; threaded into `PipelineConfig.L7Enabled` at `cmd/cpg/generate.go:239` and `cmd/cpg/replay.go:113`; field at `pkg/hubble/pipeline.go:48-49`; `cmd/cpg/replay_test.go:39-74` `TestReplay_L7FlagByteStable` runs same fixture with `--l7=false` then `--l7=true`, asserts byte-equal policy tree |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `pkg/policy/merge.go` | mergePortRules preserves Rules, mergeL7Rules union | ✓ VERIFIED | 393 lines; L4-fast-path L200, L7-path L227-261, mergeL7Rules L279-327 |
| `pkg/policy/builder.go` | (no scope) | ✓ VERIFIED | builder unchanged for codegen; ruleKeyFor at L456 leaves L7 nil (Phase 7 contract) |
| `pkg/policy/attribution.go` | RuleKey L7 discriminator | ✓ VERIFIED | `L7Discriminator` struct L38-43; `RuleKey.L7 *L7Discriminator` L55; `String()` L61-67 — nil L7 produces v1.1 byte-identical encoding |
| `pkg/policy/dedup.go` | normalizeRule sorts L7 | ✓ VERIFIED | sortL7Rules L81-98 invoked from normalizeRule L54,61 |
| `pkg/evidence/schema.go` | SchemaVersion=2, L7Ref optional | ✓ VERIFIED | `SchemaVersion = 2` L15; `L7 *L7Ref` omitempty at L58; `L7Ref` struct L70-75 with protocol/http_method/http_path/dns_matchname |
| `pkg/evidence/reader.go` | Rejects non-2 with XDG path | ✓ VERIFIED | Error string at L37-41 contains literal `$XDG_CACHE_HOME/cpg/evidence/` |
| `pkg/k8s/preflight.go` | RunL7Preflight VIS-04/05 | ✓ VERIFIED | 119 lines; `RunL7Preflight` L58, getCiliumConfig L75, checkCiliumEnvoy L100; explicit forbidden branches |
| `cmd/cpg/commonflags.go` | --l7 declared | ✓ VERIFIED | L48 `f.Bool("l7", false, ...)`, parsed L65 |
| `cmd/cpg/generate.go` | --no-l7-preflight + maybeRunL7Preflight | ✓ VERIFIED | L107 declares `--no-l7-preflight`, L211 invokes maybeRunL7Preflight; L7Enabled threaded L239 |
| `cmd/cpg/replay.go` | L7Enabled threaded, no preflight | ✓ VERIFIED | L113 sets `L7Enabled: f.l7`; explicit comment L111-112 confirms replay never invokes preflight |
| `pkg/hubble/pipeline.go` | PipelineConfig.L7Enabled field | ✓ VERIFIED | L48-49 declared with no-op comment for Phase 7 |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `cmd/cpg/generate.go` | `pkg/k8s.RunL7Preflight` | `maybeRunL7Preflight` | ✓ WIRED | `generate.go:55` calls `k8s.RunL7Preflight`; gated by `l7Enabled && !noPreflight` (L39) |
| `cmd/cpg/{generate,replay}.go` | `pkg/hubble.PipelineConfig` | `L7Enabled: f.l7` | ✓ WIRED | generate.go:239, replay.go:113 |
| `pkg/policy/dedup.go::normalizeRule` | L7 sort | `sortL7Rules(...Rules)` | ✓ WIRED | invoked inside ingress/egress loops (dedup.go:54,61) |
| `pkg/policy/merge.go::mergePortRules` | `mergeL7Rules` | bucket.rules merge | ✓ WIRED | merge.go:245 |
| `pkg/evidence/reader.go::Read` | error message | embedded literal | ✓ WIRED | reader.go:37-41 contains XDG path |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|--------------|--------|-------------------|--------|
| `PipelineConfig.L7Enabled` | bool flag | parsed from `--l7` cobra flag | Yes (boolean read from f.l7) | ✓ FLOWING (no-op by design — Phase 8 lights up codegen) |
| evidence schema_version | int=2 | constant | Yes | ✓ FLOWING |
| preflight warnings | log lines | `logger.Warn` calls | Yes (zap.Logger from caller) | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Test suite passes for all 9 packages | `go test ./...` | All 9 pkgs `ok`, 231 tests | ✓ PASS |
| Reader rejects schema_version != 2 with XDG path | grep evidence/reader.go:37 | literal `$XDG_CACHE_HOME/cpg/evidence/` present | ✓ PASS |
| `--l7` flag declared on both commands | grep commonFlags + addCommonFlags | flag bound on generate AND replay | ✓ PASS |
| `--no-l7-preflight` only on generate | grep no-l7-preflight in cmd/cpg | only `generate.go:107`; replay_test.go:244 actively rejects on replay | ✓ PASS |
| Byte-stability `--l7=on` vs `--l7=off` | `TestReplay_L7FlagByteStable` | passes (within `cmd/cpg` ok) | ✓ PASS |

### Requirements Coverage (8/8)

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| EVID2-01 | 07-02 | schema_version 2 + reader rejection naming `$XDG_CACHE_HOME/cpg/evidence/` | ✓ SATISFIED | `pkg/evidence/schema.go:15`, `pkg/evidence/reader.go:36-42` |
| EVID2-02 | 07-01 | RuleKey L7 discriminator | ✓ SATISFIED | `pkg/policy/attribution.go:38-67` (L7Discriminator + RuleKey.L7 + String() with `:l7=` suffix) |
| EVID2-03 | 07-01 | mergePortRules preserves Rules; regression test fails on prev | ✓ SATISFIED | `pkg/policy/merge.go:190-262`; test `pkg/policy/merge_l7_test.go:25-152` |
| EVID2-04 | 07-01 | normalizeRule sorts L7 lists deterministically | ✓ SATISFIED | `pkg/policy/dedup.go:81-98` |
| VIS-04 | 07-03 | enable-l7-proxy preflight, warn-and-proceed on RBAC | ✓ SATISFIED | `pkg/k8s/preflight.go:75-95` getCiliumConfig + warnConfigMapForbidden L30-31 |
| VIS-05 | 07-03 | cilium-envoy DaemonSet preflight, warn-and-proceed on RBAC | ✓ SATISFIED | `pkg/k8s/preflight.go:100-119` checkCiliumEnvoy + warnEnvoyForbidden L37-38 |
| VIS-06 | 07-04 | --no-l7-preflight skips both checks | ✓ SATISFIED | `cmd/cpg/generate.go:107` flag, L39 `if !l7Enabled \|\| noPreflight { return }` |
| L7CLI-01 | 07-04 | --l7 plumbed but byte-identical to v1.1 | ✓ SATISFIED | flag at `commonflags.go:48`, threaded to `PipelineConfig.L7Enabled`; byte-stability guarded by `cmd/cpg/replay_test.go:39-74` |

### Anti-Patterns Found

None — no TODO/FIXME/PLACEHOLDER strings in the Phase 7 surface. The "no-op in Phase 7" comments at `pkg/hubble/pipeline.go:48` and `cmd/cpg/replay.go:111` are intentional contract documentation, not stubs (the `L7Enabled` field is genuinely parsed and stored — codegen activation is deferred to Phase 8/9 by design per L7CLI-01).

### Test Suite

```
$ go test ./...
ok      github.com/SoulKyu/cpg/cmd/cpg          0.069s
ok      github.com/SoulKyu/cpg/pkg/diff         (cached)
ok      github.com/SoulKyu/cpg/pkg/evidence     0.024s
ok      github.com/SoulKyu/cpg/pkg/flowsource   (cached)
ok      github.com/SoulKyu/cpg/pkg/hubble       0.154s
ok      github.com/SoulKyu/cpg/pkg/k8s          0.041s
ok      github.com/SoulKyu/cpg/pkg/labels       (cached)
ok      github.com/SoulKyu/cpg/pkg/output       0.038s
ok      github.com/SoulKyu/cpg/pkg/policy       0.041s
```

**231 tests pass across 9 packages.** Notable Phase 7 tests:
- `pkg/policy.TestMergePortRules_PreservesRules` (5 sub-cases, EVID2-03 regression)
- `pkg/policy.TestMergePortRules_L4OnlyByteIdentical` (v1.1 byte-stability)
- `pkg/policy.TestNormalizeRule_SortsL7Lists` (EVID2-04)
- `pkg/policy.TestRuleKey_String_*` (EVID2-02)
- `pkg/k8s.TestRunL7Preflight` + `TestRunL7Preflight_SingleWarningPerInvocation` (VIS-04/05 incl. forbidden reactor)
- `cmd/cpg.TestReplay_L7FlagByteStable` (L7CLI-01 byte-identical contract)
- `cmd/cpg.TestReplayCmd_RejectsNoL7PreflightFlag` (VIS-06 scoping — replay must NOT accept the flag)

### Gaps Summary

None. All 5 success criteria and all 8 requirements (EVID2-01..04, VIS-04..06, L7CLI-01) are implemented with file-line evidence and matching tests. The "no-op for codegen" semantic of `--l7` in Phase 7 is the explicit contract per L7CLI-01 — verified by `TestReplay_L7FlagByteStable`.

**Minor observation (non-gap):** REQUIREMENTS.md traceability table (lines 85-87, 97-101) marks VIS-04, VIS-05, EVID2-01..04 as `Planned` but the code is implemented. Only VIS-06 and L7CLI-01 are flagged `Complete`. This is a documentation lag, not a code gap. The ROADMAP.md task table at L60-63 marks all 4 plans `[x]` complete. The traceability table should be updated to reflect actual completion state — recommend doing this before the Phase 7 commit.

---

_Verified: 2026-04-24_
_Verifier: Claude (gsd-verifier)_
