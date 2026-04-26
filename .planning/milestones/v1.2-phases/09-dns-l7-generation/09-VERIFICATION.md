---
phase: 09-dns-l7-generation
verified: 2026-04-24T00:00:00Z
status: passed
score: 5/5 success criteria verified, 8/8 requirements satisfied
test_count: 319 passed across 9 packages (241 top-level + 78 subtests)
re_verification: false
---

# Phase 9: DNS L7 Generation + explain L7 + Docs — Verification Report

**Phase Goal:** Users running `cpg generate --l7` (or `cpg replay --l7`) against a cluster with DNS proxy see `toFQDNs` egress rules emitted with a mandatory companion DNS-allow rule; `cpg explain` surfaces L7 attribution; the two-step workflow is documented end-to-end.
**Status:** passed
**Mode:** Initial verification (no prior VERIFICATION.md)

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Flow.L7.Dns query → toFQDNs.matchName + dns.matchName paired (literal, trailing-dot stripped) (DNS-01) | VERIFIED | `pkg/policy/l7.go:63-78` (`extractDNSQuery` strips trailing dot + whitespace, returns `("", false)` on nil/empty); `pkg/policy/builder.go:475-477` (recordL7 DNS branch calls extractDNSQuery on egress); `pkg/policy/builder.go:565-580` (buildEgressRules emits dedicated EgressRule with paired `api.FQDNSelector{MatchName}` + `api.PortRuleDNS{MatchName}` on UDP/TCP 53, sorted) |
| 2 | Companion DNS-53 rule auto-emitted for every CNP with toFQDNs — unit-test invariant (DNS-02) | VERIFIED | `pkg/policy/companion_dns.go:50-81` (`ensureKubeDNSCompanion` walks ToFQDNs, idempotent via `hasKubeDNSCompanion` at line 60, appends companion with kube-dns selector + UDP+TCP/53); `pkg/policy/builder.go:135` (`ensureKubeDNSCompanion(cnp)` invoked at end of BuildPolicy); shared invariant helper `pkg/policy/testdata/companion_dns_assert.go::AssertHasKubeDNSCompanion` reused by `pkg/hubble/pipeline_l7_dns_test.go` and `cmd/cpg/replay_test.go::TestReplay_L7DNSGeneration` |
| 3 | No matchPattern glob in v1.2; fallback CIDR v1.1 byte-identical when no Flow.L7.Dns (DNS-03, DNS-04) | VERIFIED | `rg "MatchPattern" pkg/policy/{l7.go,builder.go,companion_dns.go}` returns only one comment line (`companion_dns.go:84`); `pkg/policy/builder.go:475` gates DNS branch on `opts.L7Enabled` (top-of-function); `cmd/cpg/replay_test.go:435 TestReplay_L7DNSDisabled_FallbackByteStable` asserts no-flag vs `--l7=false` produce byte-identical CIDR-only egress |
| 4 | cpg explain --http-method/--http-path/--dns-pattern (exact match) + L7 rendering text/JSON/YAML (L7CLI-02, L7CLI-03) | VERIFIED | `cmd/cpg/explain.go:42-44` (3 long-only string flags); `cmd/cpg/explain.go:153-160` (buildFilter uppercases method, strips trailing dot from dns-pattern); `cmd/cpg/explain_filter.go:64-83` (L7 branch in match() — drops L4-only rules when any L7 filter set, AND-combines, gates per Protocol); `cmd/cpg/explain_render.go:67-72` (text writeRule emits `L7: HTTP <method> <path>` and `L7: DNS <matchname>`); JSON/YAML rendering passes through via existing schema v2 `L7Ref` json tags (omitempty) |
| 5 | README two-step workflow + starter L7-visibility CNP snippet (VIS-02, VIS-03) | VERIFIED | `README.md:234` exact-once `<a id="l7-prerequisites"></a>` anchor preserved; `README.md:236-370` authoritative content (two-step workflow, three visibility methods, capture-window guidance, honest v1.2 limitations); `README.md:307` starter CNP `cpg-l7-visibility-bootstrap` shipped (match-all HTTP `{}` + DNS matchPattern `*` for visibility bootstrap only — does NOT contradict DNS-03 since cpg's own output never emits matchPattern); `README.md:80,100,265-267` `--l7` flag mentioned in Quick start / replay sections; `README.md:446-449` explain L7 filter usage |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `pkg/policy/l7.go` | extractDNSQuery added | VERIFIED | line 63-78, nil-safe, trailing-dot + whitespace stripped |
| `pkg/policy/companion_dns.go` | ensureKubeDNSCompanion + kubeDNSSelector | VERIFIED | line 50-81 (injector), line 180-189 (selector); idempotent via hasKubeDNSCompanion (line 111); label-source-prefix tolerant (line 167-174) |
| `pkg/policy/builder.go` | recordL7 DNS branch + post-process companion call + dnsNames/dnsOrder peerRules | VERIFIED | dnsNames+dnsOrder declared line 220-221, addDNSName populated line 235-246, BuildPolicy calls ensureKubeDNSCompanion line 135, buildEgressRules emits FQDN egress line 565-580 |
| `pkg/policy/testdata/companion_dns_assert.go` | AssertHasKubeDNSCompanion helper | VERIFIED | exported cross-package; consumed by pkg/hubble integration test + cmd/cpg replay test |
| `pkg/hubble/aggregator.go` | l7DNSCount increment | VERIFIED | line 150-151 increments in Run loop on `f.GetL7().GetDns() != nil`, independent of L7Enabled |
| `pkg/hubble/evidence_writer.go` | DNS branch in convert() | VERIFIED | line 94-104 switch on Key.L7.Protocol with `case "dns"` populating L7Ref{Protocol:"dns", DNSMatchName}; Phase 8 TODO removed (`rg "TODO\(phase-9\)"` returns 0) |
| `pkg/hubble/pipeline_l7_dns_test.go` | TestPipeline_L7DNS integration | VERIFIED | reuses testdata.AssertHasKubeDNSCompanion against on-disk YAML; asserts no matchPattern, evidence DNS L7Ref present, L7DNSCount > 0 |
| `cmd/cpg/explain.go` | 3 L7 flags + buildFilter wiring | VERIFIED | line 42-44 + 153-160 |
| `cmd/cpg/explain_filter.go` | explainFilter L7 fields + match() L7 branch | VERIFIED | line 26-28 fields, line 64-83 match() gating |
| `cmd/cpg/explain_render.go` | Text-format L7 line | VERIFIED | line 67-72 (HTTP + DNS branches) |
| `cmd/cpg/replay_test.go` | TestReplay_L7DNSGeneration + TestReplay_L7DNSDisabled_FallbackByteStable | VERIFIED | line 356 (e2e: toFQDNs sorted + companion + no matchPattern + DNS L7Ref), line 435 (DNS-04 byte-stability lock) |
| `testdata/flows/l7_dns.jsonl` | 2-3 DNS-bearing flows | VERIFIED | 3 lines, EGRESS DROPPED, DNS queries api.example.com / www.example.org / typo.example.com (NXDOMAIN), reserved:world dst, UDP/53 |
| `README.md` | #l7-prerequisites filled + starter CNP + --l7 mentions | VERIFIED | anchor exact-once at line 234; starter CNP `cpg-l7-visibility-bootstrap` at line 307; explain L7 filter examples at line 446-449 |

### Key Link Verification

| From | To | Via | Status |
|------|-----|-----|--------|
| BuildPolicy | extractDNSQuery | recordL7 DNS branch (builder.go:475-477) | WIRED |
| BuildPolicy | ensureKubeDNSCompanion | post-process call (builder.go:135) | WIRED |
| Aggregator.Run | l7DNSCount increment | aggregator.go:150-151 (`if f.GetL7().GetDns() != nil`) | WIRED |
| evidenceWriter.convert | L7Ref Protocol=dns | evidence_writer.go:94-104 switch on Key.L7.Protocol | WIRED |
| pipeline.go VIS-01 gate | aggregator.L7DNSCount() | already wired Phase 8; counter now actually moves | WIRED |
| explain buildFilter | explainFilter L7 fields | explain.go:153-160 (3 GetString + normalization) | WIRED |
| explain match() | RuleEvidence.L7 | explain_filter.go:64-83 | WIRED |
| explain renderText | L7Ref.Protocol switch | explain_render.go:67-72 | WIRED |
| README #l7-prerequisites anchor | pipeline.go VIS-01 hint string | README.md:234 anchor exact-once preserved | WIRED |
| TestReplay_L7DNSGeneration | testdata/flows/l7_dns.jsonl | passed as positional arg to replay command | WIRED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full repo tests pass | `go test ./...` | 9 packages, 319 tests pass (241 top-level + 78 subtests) | PASS |
| go vet clean | `go vet ./...` | no issues | PASS |
| README anchor exact-once | `grep -c 'id="l7-prerequisites"' README.md` | 1 | PASS |
| Starter CNP shipped | `grep -q 'cpg-l7-visibility-bootstrap' README.md` | found | PASS |
| Explain L7 flags documented | `grep -E '\-\-(http-method\|dns-pattern\|http-path)' README.md` | found | PASS |
| DNS-03 — no MatchPattern in cpg output code | `rg 'MatchPattern' pkg/policy/{l7.go,builder.go,companion_dns.go}` | only one doc-comment match | PASS |
| Phase 8 TODO removed | `rg 'TODO\(phase-9\)' pkg/hubble/evidence_writer.go` | 0 matches | PASS |
| Fixture present | `wc -l testdata/flows/l7_dns.jsonl` | 3 lines | PASS |

### Requirements Coverage (8 v1.2 requirements claimed by Phase 9)

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| DNS-01 | 09-01, 09-02, 09-04 | Flow.L7.Dns → toFQDNs.matchName + dns.matchName | SATISFIED | extractDNSQuery + builder DNS emission + e2e TestReplay_L7DNSGeneration |
| DNS-02 | 09-01, 09-02, 09-04 | Companion DNS-53 rule auto-emitted for every CNP with toFQDNs | SATISFIED | ensureKubeDNSCompanion + AssertHasKubeDNSCompanion invariant in 3 test surfaces |
| DNS-03 | 09-01, 09-04 | No matchPattern glob auto-generated; only matchName literals | SATISFIED | rg confirms zero MatchPattern in generation code; e2e replay test asserts substring absent in YAML |
| DNS-04 | 09-04 | Fallback to v1.1 CIDR-based egress byte-identical when no Flow.L7.Dns | SATISFIED | TestReplay_L7DNSDisabled_FallbackByteStable + recordL7 gates DNS branch on opts.L7Enabled at top |
| L7CLI-02 | 09-03 | --http-method / --http-path / --dns-pattern exact-match filters | SATISFIED | 3 long-only flags + buildFilter normalization + AND-combined match() with per-protocol gating |
| L7CLI-03 | 09-03 | L7 attribution rendered in text/JSON/YAML | SATISFIED | text writeRule L7 line; JSON/YAML pass-through via schema v2 L7Ref json tags; contract-locking tests in explain_test.go |
| VIS-02 | 09-04 | README documents two-step workflow | SATISFIED | README.md:236-267 (Why this matters + Step 1 / Step 2a / Step 2b) |
| VIS-03 | 09-04 | Starter L7-visibility CNP snippet | SATISFIED | README.md:307 cpg-l7-visibility-bootstrap (valid Cilium YAML, fill-in comments) |

**Coverage:** 8/8 satisfied. No orphaned requirements (REQUIREMENTS.md traceability table maps DNS-01..DNS-04, L7CLI-02, L7CLI-03, VIS-02, VIS-03 to Phase 9; all are claimed by 09-* plans).

### v1.2 Milestone Status

22/22 v1.2 requirements satisfied:
- Phase 7 (8): EVID2-01..04, VIS-04..06, L7CLI-01 — completed
- Phase 8 (6): HTTP-01..05, VIS-01 — completed
- Phase 9 (8): DNS-01..04, L7CLI-02, L7CLI-03, VIS-02, VIS-03 — completed (this report)

**v1.2 milestone is feature-complete.**

### Anti-Patterns Found

None. No TODOs / FIXMEs / placeholder comments / `return nil` stubs in shipped Phase 9 code. The Phase 8 `TODO(phase-9)` in evidence_writer.go was removed in 09-02. Starter CNP in README intentionally uses `matchPattern: "*"` for visibility bootstrap (informational, not generated by cpg) — explicitly documented as such; does not violate DNS-03.

### Human Verification Required

None. All success criteria verifiable via static checks + automated test suite (passing).

### Gaps Summary

No gaps. Phase 9 ships v1.2 feature-complete:
- DNS L7 generation with mandatory kube-dns companion (DNS-01, DNS-02).
- DNS-03 invariant (no glob auto-generation) holds across all generation paths including the companion rule.
- DNS-04 byte-stability locked by sibling e2e test.
- cpg explain L7 filters + rendering across 3 formats (L7CLI-02, L7CLI-03).
- README two-step workflow with copy-pasteable starter visibility CNP (VIS-02, VIS-03).
- All 22 v1.2 requirements complete.
- 319 tests pass across 9 packages; go vet clean.

---

*Verified: 2026-04-24*
*Verifier: Claude (gsd-verifier)*
