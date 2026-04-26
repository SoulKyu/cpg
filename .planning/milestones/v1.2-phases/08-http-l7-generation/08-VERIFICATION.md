---
phase: 08-http-l7-generation
verified: 2026-04-24T00:00:00Z
status: passed
score: 5/5 success criteria + 6/6 requirements verified
test_suite:
  total: 279
  packages: 9
  failures: 0
---

# Phase 8: HTTP L7 Generation — Verification Report

**Phase Goal:** Users running `cpg generate --l7` (or `cpg replay --l7`) against a cluster with L7 visibility see correct, byte-stable HTTP rules emitted alongside L4 port rules in generated CNP YAML, with passive empty-L7 detection when visibility is missing.

**Verified:** 2026-04-24
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement — Success Criteria

| # | Criterion | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Flow.L7.Http → toPorts.rules.http on matching L4 port (HTTP-01, HTTP-04) | VERIFIED | `pkg/policy/builder.go:426-444` `recordL7` extracts + attaches via `addHTTPRules`; `pkg/policy/builder.go:537-558` `portRulesFor` emits `Rules: &api.L7Rules{HTTP: ...}` on per-port PortRule. E2E proof `cmd/cpg/replay_test.go:188-252` `TestReplay_L7HTTPGeneration` asserts `rules:` + `http:` blocks in YAML against `testdata/flows/l7_http.jsonl`. |
| 2 | HTTP method uppercase normalized pre-merge (HTTP-02) | VERIFIED | `pkg/policy/l7.go:54-56` `normalizeHTTPMethod = strings.ToUpper(strings.TrimSpace(s))` invoked in `extractHTTPRules` BEFORE returning entry (`l7.go:39-44`); empty drops entry. `pkg/policy/l7_test.go:120-138` table covers `get`→`GET`, `  PoSt  `→`POST`. E2E `cmd/cpg/replay_test.go:218-220` asserts lowercase `get` from fixture line 4 emits as `method: GET` and verifies no lowercase leakage. |
| 3 | Path = ^regexp.QuoteMeta(...)$ (HTTP-03) | VERIFIED | `pkg/policy/l7.go:63-69` `anchorPath` = `"^" + regexp.QuoteMeta(path) + "$"`; `pathFromURL` strips scheme/host/query/fragment via `net/url.Parse`. Property test `pkg/policy/l7_test.go:171-204` `TestExtractHTTPRules_PathAnchored` compiles regex and asserts (a) matches literal, (b) rejects `/evil`+path prefix, (c) rejects path+`/extra` suffix. E2E `cmd/cpg/replay_test.go:223-226` asserts `path: ^/api/v1/users$`, `path: ^/healthz$`, `path: ^/api/v1/orders$` (query stripped). |
| 4 | headerMatches/host/hostExact NEVER emitted (HTTP-05) | VERIFIED | `pkg/policy/l7.go:43-47` `extractHTTPRules` constructs `PortRuleHTTP{Method, Path}` only — Headers/Host/HeaderMatches deliberately zero (godoc comment ll. 9-13). Adversarial unit test `pkg/policy/l7_test.go:144-166` `TestExtractHTTPRules_NeverEmitsHeaders` builds a flow with `Authorization: Bearer secret-token` + `Cookie` headers and asserts every output entry has empty Headers/Host/HeaderMatches. E2E lint `cmd/cpg/replay_test.go:228-233` `assert.NotContains(yaml, "headerMatches")`, `"hostExact"`, and regex-guarded `^\s+host:\s`. |
| 5 | VIS-01 single warning naming workloads + #l7-prerequisites anchor | VERIFIED | `pkg/hubble/pipeline.go:199-205` single emission site post-`g.Wait()`, gated `cfg.L7Enabled && stats.FlowsSeen > 0 && agg.L7HTTPCount()+agg.L7DNSCount() == 0`, includes `zap.Strings("workloads", agg.ObservedWorkloads())` (sorted, `aggregator.go:107-114`) and `zap.String("hint", "see README L7 prerequisites: #l7-prerequisites")`. README anchor confirmed `README.md:178` `<a id="l7-prerequisites"></a>`. E2E proof `cmd/cpg/replay_test.go:300-346` `TestReplay_L7HTTP_EmptyFixtureFiresWarning` asserts exactly-once + `--l7` in message + `#l7-prerequisites` in hint + non-empty workloads + flows>0. Negative tests `pkg/hubble/pipeline_l7_test.go:150-200` confirm no fire when `L7Enabled=false` or zero flows. |

**Score:** 5/5 success criteria verified.

## Requirements Coverage

| Requirement | Status | Evidence |
| --- | --- | --- |
| HTTP-01 | SATISFIED | `recordL7` emits HTTP rules; `portRulesFor` attaches them to matching port (`builder.go:537-558`). E2E asserted YAML `http:` block. |
| HTTP-02 | SATISFIED | `normalizeHTTPMethod` (`l7.go:54-56`) called pre-emit. Mixed-case unit + E2E coverage. |
| HTTP-03 | SATISFIED | `anchorPath` + `regexp.QuoteMeta` (`l7.go:63-69`). Property test compiles + verifies anchoring. |
| HTTP-04 | SATISFIED | `addHTTPRules` (`builder.go:226-243`) dedups via `httpRuleKey`; `portRulesFor` emits ONE PortRule per (port, proto) carrying the merged HTTP slice. Verified by `TestBuildPolicy_L7Enabled_MultiHTTPRule_SamePort_SinglePortRule` (per 08-02-SUMMARY). |
| HTTP-05 | SATISFIED | `extractHTTPRules` constructor (`l7.go:43-47`) leaves header fields zero. Adversarial test in `l7_test.go:144-166`; writer-side YAML lint in E2E. |
| VIS-01 | SATISFIED | Single emission `pipeline.go:199-205`, README anchor `README.md:178`, exact-once asserted in E2E test. |

## Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `pkg/policy/l7.go` | extractHTTPRules + normalizeHTTPMethod + anchorPath | VERIFIED | 93 lines, all primitives present, godoc HTTP-05 contract documented |
| `pkg/policy/l7_test.go` | Table + HTTP-05 lint + path-anchoring property | VERIFIED | 205 lines, 3 test functions covering 14 cases + adversarial headers + 7 anchor cases |
| `pkg/policy/builder.go` | L7 codegen branch + portRulesFor + ruleKeyForL7 | VERIFIED | `recordL7` (l.426), `portRulesFor` (l.537), `ruleKeyForL7` (l.573); peerRules carries `httpRules` + `httpSeen` |
| `pkg/policy/attribution.go` | AttributionOptions.L7Enabled + L7Discriminator | VERIFIED | `L7Discriminator` (l.38-43), `AttributionOptions.L7Enabled` (l.128) |
| `pkg/policy/builder_l7_test.go` | TDD coverage HTTP-01/HTTP-04 + byte-stability | VERIFIED | Present, exercises 8 scenarios per 08-02-SUMMARY |
| `pkg/hubble/pipeline.go` | L7Enabled forwarding + VIS-01 + L7HTTPCount | VERIFIED | `agg.SetL7Enabled(cfg.L7Enabled)` l.112; VIS-01 l.199-205; SessionStats.L7HTTPCount l.63 |
| `pkg/hubble/aggregator.go` | L7 counters + ObservedWorkloads + flowsSeen | VERIFIED | All accessors present; `Run()` increments l7HTTPCount on every L7 flow (l.143-145), flowsSeen + seenWorkloads (l.150-151); `flush()` passes `L7Enabled: a.l7Enabled` to BuildPolicy (l.215-218) |
| `pkg/hubble/evidence_writer.go` | L7Ref population for HTTP | VERIFIED | `convert()` l.94-105: `switch a.Key.L7.Protocol { case "http": re.L7 = &evidence.L7Ref{Protocol:"http", HTTPMethod, HTTPPath} }`; DNS branch is a TODO(phase-9) sentinel |
| `pkg/hubble/pipeline_l7_test.go` | Pipeline integration tests | VERIFIED | 5 tests covering generation, VIS-01 fire, L7-disabled no-fire, L7-disabled L7-flow ignore, empty-fixture no-fire |
| `cmd/cpg/replay_test.go` | E2E replay tests + Phase 7 byte-stability | VERIFIED | `TestReplay_L7HTTPGeneration` (l.188), `TestReplay_L7HTTP_DisabledByteStable` (l.258), `TestReplay_L7HTTP_EmptyFixtureFiresWarning` (l.300), `TestReplay_L7FlagByteStable` retained (l.39) |
| `testdata/flows/l7_http.jsonl` | 4-flow L7 fixture | VERIFIED | 4 lines: GET/POST /api/v1/users, GET /healthz, lowercase get /api/v1/orders?id=42 |
| `README.md` | #l7-prerequisites anchor | VERIFIED | `README.md:178` `## L7 Prerequisites <a id="l7-prerequisites"></a>` |

## Key Link Verification

| From | To | Via | Status | Details |
| --- | --- | --- | --- | --- |
| `cmd/cpg/replay --l7` | `PipelineConfig.L7Enabled` | flag plumbing | WIRED | Phase 7 plumbing retained; `pipeline.go:49` |
| `RunPipelineWithSource` | `agg.SetL7Enabled` | direct call | WIRED | `pipeline.go:112` |
| `aggregator.flush` | `policy.BuildPolicy` `L7Enabled` | AttributionOptions | WIRED | `aggregator.go:215-218` |
| `groupFlows` | `extractHTTPRules` | `recordL7` | WIRED | `builder.go:372,394,411` (entity/cidr/endpoint branches each call `recordL7`) → `builder.go:433` calls `extractHTTPRules` |
| `peerRules` | `Rules: &api.L7Rules{HTTP: ...}` | `portRulesFor` | WIRED | `builder.go:553` |
| `RuleKey.L7` | `evidence.L7Ref` HTTP | `evidence_writer.convert` | WIRED | `evidence_writer.go:94-101` |
| `pipeline VIS-01` | `README #l7-prerequisites` | hint string | WIRED | `pipeline.go:203` ↔ `README.md:178` (literal `#l7-prerequisites` match in both files via grep) |

## Data-Flow Trace (Level 4)

| Artifact | Data Source | Real Data? | Status |
| --- | --- | --- | --- |
| Generated CNP `http:` block | `Flow.L7.Http` via `extractHTTPRules` → `peerRules.httpRules` → `portRulesFor` → CNP marshalling | Yes (E2E proves YAML carries actual method/path) | FLOWING |
| Evidence `L7Ref` | `RuleKey.L7` populated in `recordL7` → marshalled by `evidence_writer.convert` | Yes (E2E test reads JSON, asserts non-empty L7.HTTPMethod/HTTPPath) | FLOWING |
| VIS-01 `workloads` field | `agg.seenWorkloads` populated in `Run()` keyFromFlow accept branch → sorted via `ObservedWorkloads()` | Yes (E2E asserts non-empty slice) | FLOWING |
| VIS-01 `flows` field | `agg.flowsSeen` (BUG-01 incidentally fixed) → `stats.FlowsSeen` | Yes (E2E asserts >0) | FLOWING |

## Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Full repo test suite | `go test ./... -count=1` | 9 packages OK, 279 tests pass | PASS |
| Phase 7 byte-stability still passes | included in suite | `TestReplay_L7FlagByteStable` still green | PASS |
| L7 fixture parses | included via `TestReplay_L7HTTPGeneration` | fixture loads + replay executes | PASS |
| README anchor + pipeline hint match | `grep -n l7-prerequisites README.md pkg/hubble/pipeline.go` | both present, exact string match | PASS |

## Anti-Patterns Found

None. The single TODO is `evidence_writer.go:103` — a deliberate Phase-9 sentinel inside a switch case body for the `dns` protocol branch, intentionally left empty so Phase 9 plugs in the case body. No DNS attributions exist in Phase 8, so the empty branch is unreachable. Classification: Info (intentional, documented, non-blocking).

## Gaps Summary

None. All 5 ROADMAP success criteria and all 6 mapped requirements (HTTP-01..05, VIS-01) are implemented, wired end-to-end (CLI → pipeline → builder → CNP YAML → evidence file), data-flow verified via E2E tests against `testdata/flows/l7_http.jsonl`, and the v1.1/Phase-7 byte-stability invariant is preserved (`TestReplay_L7FlagByteStable` still green among 279 passing tests).

---
*Verified: 2026-04-24*
*Verifier: Claude (gsd-verifier)*
