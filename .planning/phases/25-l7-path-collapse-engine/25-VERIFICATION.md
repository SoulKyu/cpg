---
status: passed
phase: 25-l7-path-collapse-engine
date: 2026-10-04
---

# Phase 25 Verification — L7 Path Collapse Engine

Verifier: GSD verifier, goal-backward verification against the actual codebase.
Method: static code verification (read + symbol search) of every must-have truth, prohibition,
and requirement (per-truth evidence below, all file:line citations from the verifier run), plus
**orchestrator-executed** verification of every backstop item (SC1–SC5, full `-race` suite,
golden provenance re-derivation).

## Score

**Must-haves verified: 21/21** (17 static code + test-assertion evidence; 4 backstop truths
confirmed by orchestrator execution — see Execution Evidence).
**No gaps found.** All code paths, wiring seams, prohibitions, and requirement IDs are
present and consistent with the plans.

## Execution Evidence (orchestrator, 2026-10-04 — closes H1/H2/H3)

| Item | Command | Result |
|------|---------|--------|
| SC1 (10k→1 template) | `go test -race -count=1 ./pkg/hubble/ -run TestL7CollapseGolden` | ok 3.7s |
| SC2 (byte-identity) | `go test -race -count=1 ./pkg/hubble/ -run TestL7NoCollapseByteIdentity` | ok 1.0s |
| SC3 (gate fallback) | `go test -race -count=1 ./pkg/hubble/ -run TestMinFlowsGate` | ok 1.0s |
| SC4 (class matrix) | `go test -race -count=1 ./pkg/policy/ -run TestPathTemplateClassifier` | ok 1.0s |
| SC5 (MCP parity + args) | `go test -race -count=1 -timeout 300s ./cmd/cpg/ -run 'TestMCPCLICollapseParity\|TestMCPCollapseArgs'` | ok 8.7s |
| H2 full suite | `GOMEMLIMIT=4GiB GOGC=50 go test ./... -count=1 -race -timeout 900s` | 13 packages ok |
| H3 golden provenance | worktree at `e4159a8`, replay both fixtures, `diff -r` vs `testdata/golden/l7-collapse-regression/` | **byte-identical** (l7_http + mixed_tcp_icmp/{server,client}) |

## Per-truth evidence

### Plan 25-01 (15 truths)

| # | Truth (abbrev) | Verdict | Evidence |
|---|---|---|---|
| 1 | 10k paths → 1 anchored template (SC1/L7GEN-01) | VERIFIED | `pkg/hubble/pipeline_l7_collapse_test.go:77-85` TestL7CollapseGolden asserts exactly one `path: ^/api/v1/users/[0-9]+$`, `^/healthz$`, exactly 2 `path:` entries, no stale literal. Fixture 10,003 lines confirmed. **Command run: ok.** |
| 2 | Variance + homogeneous class only; version/word/single/mixed stay literal (L7GEN-02) | VERIFIED | `pkg/policy/l7_collapse.go:65-69` (`generalizable()` excludes segWord/segVersion); `:320-368` ≥2 distinct + homogeneity; classifier matrix `l7_collapse_test.go:17-53`; `TestL7CollapseMixedClassesGolden` asserts literals + `NotContains "v[0-9]"`. |
| 3 | Templates anchored `^…$`; non-generalized segments QuoteMeta'd | VERIFIED | `l7_collapse.go:224-231`, `:377-394`; only POSIX-ERE fragments (`:82-95`); no `\d`/`\w` in file. |
| 4 | Gate N; N-1 emits literals, never silent widening (L7GEN-04) | VERIFIED | `l7_collapse.go:409`; `TestCollapseHTTPPaths_GateBoundary` (`:280`); `TestMinFlowsGate`. **Command run: ok.** |
| 5 | Over-cap pattern falls back to literals (T-25-02) | VERIFIED | `l7_collapse.go:195` (256 cap) + `:409`; `TestCollapseHTTPPaths_PatternLengthCap`. |
| 6 | Collapse never reduces the permitted set | VERIFIED | literal fallback for every path in every non-emitting cluster (`:410-413`, `:359-366`). |
| 7 | Cross-flush: template replaces earlier literals | VERIFIED | `aggregator.go:590-601` Absorb; `ReplaceL7HTTP` (`:591`); `merge.go:325-344`; `writer.go:54`; `TestL7Collapse_CrossFlushReplace` -race. |
| 8 | Replace-merge touches only HTTP; DNS untouched | VERIFIED | `merge.go:346-358`; `TestMergePolicy_ReplaceL7HTTP_PreservesDNS`. |
| 9 | Race-free under -race | VERIFIED (executed) | Single-goroutine contract (static evidence) **+ `go test -race` full suite green (H2)**. |
| 10 | Percent-encoded/UTF-8 stay wire-form; never decoded | VERIFIED | `rawPathFromURL` `u.EscapedPath()` (`:229-253`); `TestL7CollapseUnicodeEncodedGolden` byte-for-byte. |
| 11 | `/`-only set stays `^/$`; empty set → no rules | VERIFIED | `:224-231`; `collapseMethodPaths` iterates only observed paths (`:273-291`). |
| 12 | Single distinct path never generalizes | VERIFIED | `:337`; `TestCollapseHTTPPaths_SingleObservationStaysLiteral`. |
| 13 | Adjacent varying segments generalize independently | VERIFIED | per-position classes (`:334-366`); `TestCollapseHTTPPaths_AdjacentSegmentsGeneralizeIndependently`; mixed golden emits 4 templates. |
| 14 | Output sorted (Method, Path) — byte-stable | VERIFIED | `:283-290`; `TestCollapseHTTPPaths_SortsByMethodPath`. |
| 15 | Default-off invariant (zero fields → unchanged YAML) | VERIFIED (executed) | `TestL7NoCollapseByteIdentity` raw-byte `require.Equal` + `require.Len` pins. **Command run: ok (SC2).** |

### Plan 25-02 (6 truths)

| # | Truth (abbrev) | Verdict | Evidence |
|---|---|---|---|
| 1 | Without the flag, YAML byte-identical to pre-flag build (SC2) | VERIFIED (executed + provenance re-derived) | raw-byte compare `pipeline_l7_golden_test.go:107-131`; goldens exist; **H3 re-derivation from `e4159a8` byte-identical**. Golden-source deviation (pre-phase-25 commit instead of v1.6.0 tag — tag predates L7 work) documented in test header + 25-02-SUMMARY; accepted. |
| 2 | MCP session `collapse_l7_paths: true` produces CLI-identical rules (SC5/L7GEN-03) | VERIFIED (executed) | `cmd/cpg/mcp_e2e_test.go:928+` TestMCPCLICollapseParity; **command run: ok.** |
| 3 | MCP `min_flows_per_l7_rule` fail-closed (T-25-05) | VERIFIED | `mcp_tools.go:140-141` reject <0 before `mgr.Start`; `pipeline_config.go:110-111` single defaulting site; `TestMCPCollapseArgs`. **Command run: ok.** |
| 4 | MCP arg surface mirrors include_audit pattern | VERIFIED | `mcp_tools.go:32-33` omitempty + jsonschema; `session.go:151-162`; `pipeline_config.go:110-111`. |
| 5 | L7GEN-03 unclassified edge stays flagged (by design) | FLAGGED ASSUMPTION (accepted) | Engine never decodes (`:236-253`); cross-version upstream question (KNOWN_LIMITATIONS #8) remains a documented manual check — carried to KNOWN_LIMITATIONS as the open item. |
| 6 | Golden provenance backstop | VERIFIED (executed) | Provenance commands in `pipeline_l7_golden_test.go:16-32`; **re-derivation diff empty**. |

## Requirement traceability

| REQ-ID | REQUIREMENTS.md | Plan frontmatter | Evidence |
|---|---|---|---|
| L7GEN-01 | Phase 25, line 10 | 25-01 + 25-02 | TestL7CollapseGolden; TestL7NoCollapseByteIdentity |
| L7GEN-02 | Phase 25, line 11 | 25-01 | TestPathTemplateClassifier + class matrix + TestL7CollapseMixedClassesGolden |
| L7GEN-03 | Phase 25, line 12 | 25-02 | TestMCPCollapseArgs; TestMCPCLICollapseParity |
| L7GEN-04 | Phase 25, line 13 | 25-01 | TestMinFlowsGate; TestCollapseHTTPPaths_GateBoundary; TestValidateMinFlowsPerL7Rule |

All four L7GEN IDs accounted for; no orphans, no gaps.

## Prohibitions (code-level)

1. **No generalization without gate** — VERIFIED (`l7_collapse.go:409-413`; only emission site).
2. **No template widening in place** — VERIFIED (immutable `api.PortRuleHTTP`; wholesale replace with fresh copy, `merge.go:325-344`).
3. **No URL-decoding before match** — VERIFIED (`EscapedPath()`, `:246`; `anchorPath` in `pkg/policy/l7.go` untouched).

## Human verification remaining

- **H4 (flagged by design, not a defect):** cross-version check whether Hubble's Http.Url carries
  percent-encoded vs decoded paths consistently (upstream Cilium question, KNOWN_LIMITATIONS #8).
  Engine behavior (never decode) is correct regardless; the assumption stays flagged and should be
  tracked in docs (candidate KNOWN_LIMITATIONS entry, with review finding R-01's cross-run narrowing note).

## Gaps

None.
