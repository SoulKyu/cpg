# Project Research Summary — cpg v1.2 L7 Policies

**Project:** cpg (Cilium Policy Generator)
**Domain:** Go CLI — Hubble flow → CiliumNetworkPolicy YAML generation, extending L4 (shipped v1.0/v1.1) with L7 HTTP + DNS
**Researched:** 2026-04-25
**Confidence:** HIGH (Cilium API + codebase verified directly; workflow constraints confirmed in upstream docs)

## Executive Summary

cpg v1.2 is a focused extension of an already-shipped, well-factored L4 policy generator. The L7 work is **integration, not stack expansion**: every required type already lives in the vendored `cilium/cilium v1.19.1` (`pkg/policy/api` for `L7Rules`/`PortRuleHTTP`/`PortRuleDNS`/`FQDNSelector` and `api/v1/flow` for `Layer7`/`HTTP`/`DNS`). No new Go module dependencies. The streaming pipeline (`hubble.client → aggregator → BuildPolicy → Writer/EvidenceWriter`) stays structurally unchanged — L7 enrichment lives inside per-port rule structures within `BuildPolicy` buckets.

The single dominant constraint is operational, not architectural: **Hubble only emits `Flow.L7` when traffic is proxied by Envoy / the DNS proxy**, which itself requires `enable-l7-proxy=true` AND a per-workload visibility trigger (an existing L7 CNP, or the legacy `policy.cilium.io/proxy-visibility` annotation). cpg cannot bootstrap visibility from L4-only flows; it must detect-and-warn when `--l7` is on but no L7 records arrive. Two-step workflow (deploy L4 → enable visibility → re-run cpg with `--l7`) is canonical and must be documented prominently.

Three risks dominate the build order. (1) `mergePortRules` in `pkg/policy/merge.go` currently drops the `Rules` field — a latent bug today, silent L7 data loss the moment generation ships; must be fixed first. (2) The evidence schema must bump v1 → v2 (the v1.1 reader rejects unknown versions); a v1-compat read path is required so existing user caches survive. (3) HTTP `path` is RE2 regex (must `regexp.QuoteMeta` + anchor `^…$`) while DNS `matchPattern` is glob — different syntaxes in the same CRD; keep them apart at the type level. Mitigated by an explicit 3-phase split (infra-prep → HTTP gen → DNS gen + explain L7) totaling ~13 dev-days (~2.5 weeks).

## Key Findings

### Recommended Stack

Zero new module dependencies. All L7 types are already present via `github.com/cilium/cilium v1.19.1` (transitively in `go.mod`), and both target packages (`pkg/policy/api`, `api/v1/flow`) are already imported by the existing L4 codepaths. The work is wiring, not stack expansion. (See STACK.md.)

**Core technologies (additions only):**
- `github.com/cilium/cilium/pkg/policy/api` v1.19.1 — `L7Rules`, `PortRuleHTTP`, `PortRuleDNS` (type alias of `FQDNSelector`), `FQDNSelector`, `EgressRule.ToFQDNs`. Authoritative CRD types, already used for L4 in `pkg/policy/builder.go`.
- `github.com/cilium/cilium/api/v1/flow` v1.19.1 — `Flow.L7` (`*Layer7`) with `GetHttp()`/`GetDns()` accessors and `HTTP`/`DNS` proto messages. Field already present on every flow; v1.2 stops ignoring it.
- Go stdlib `regexp` — `regexp.QuoteMeta` for HTTP path escaping. Nothing else.

**Verified via `go doc` against the vendored v1.19.1 source.** Notable correction vs prior research: `PortRuleDNS` is a *type alias* of `FQDNSelector`, not a parallel struct (matters for DeepEqual and dedup).

### Expected Features

(See FEATURES.md. P1 estimate ~13 dev-days / ~2.5 weeks.)

**Must have (table stakes):**
- `--l7` opt-in flag — default OFF, preserves v1.1 behavior; on-flag wires HTTP/DNS extraction into `BuildPolicy`.
- L7-empty detection + actionable warning — when `--l7` is on but zero L7 records arrive, emit a copy-pasteable remediation (annotation command or starter-CNP) and a non-zero exit on `--l7-only`.
- HTTP method+path rules from `flow.L7.Http` — verbose, one rule per observation, no auto-regex.
- DNS `matchName` rules from `flow.L7.Dns.Query` — literal queries → `MatchName`; wildcards deferred.
- Mandatory companion DNS allow rule — every CNP carrying `toFQDNs` MUST also carry an egress rule allowing UDP+TCP/53 to `k8s-app=kube-dns` in `kube-system` with `rules.dns: [{matchPattern: "*"}]`. Atomic, auto-emitted.
- Combined L4+L7 in same CNP — L7 attaches to existing `toPorts` entry by `(port, protocol)` key, not a sibling CNP.
- Two-step workflow documented in README + `--help` — front-and-center.
- `cpg explain` renders L7 attribution — evidence schema bump to v2 with `L7Ref{Type, HTTPMethod, HTTPPath, DNSPattern}`.
- `cpg replay --l7` parity with `generate`.
- `--dry-run` shows L7 diff (free from existing YAML diff; L4→L7 transition warrants an explicit banner).

**Should have (competitive):**
- Honest "one rule per observation" default — sells in PR review ("this rule allowed exactly these 17 paths we saw"); differentiator vs generators that hallucinate via auto-regex.
- gRPC handled as HTTP — no special-casing; `POST /<service>/<method>` covers it.
- L7-aware unhandled-flow categories (`l7_visibility_off`, `incomplete_l7_http`, `incomplete_l7_dns`, `unknown_http_method`).

**Defer (v1.3+):**
- `--l7-collapse-paths` + `--l7-collapse-min N` — opt-in regex inference for noisy services.
- `--l7-fqdn-wildcard-depth N` — opt-in FQDN suffix collapse.
- `ToFQDNs` correlation from L4 → IP → cached DNS RESPONSE → FQDN — non-trivial multi-flow correlation; v1.2 stays with `PortRuleDNS` from direct DNS query observations and `toCIDR` for L4-to-external denials.
- `cpg apply` — already deferred per PROJECT.md.

**Anti-features (NEVER):** Header-based rules (leak Authorization/Cookie tokens), Host-header rules, Kafka L7 (deprecated upstream), gRPC-as-distinct (covered by POST), generic `L7Proto`, auto-on L7 (must be opt-in), auto-bootstrap of L7 visibility annotation by cpg.

### Architecture Approach

Extend, don't restructure. v1.1 codebase is layer-agnostic at the pipeline level; L7 lives inside `pkg/policy/builder.go` rule construction and a new `pkg/policy/l7.go`. Pipeline, hubble client, aggregator, output writer, and `cpg generate`/`replay` CLI surface remain unchanged. (See ARCHITECTURE.md.)

**Major components touched:**
1. `pkg/policy/l7.go` (NEW) — `extractHTTP`, `extractDNSQuery`, `extractPathFromURL` (regex-quote + anchor), `httpRuleKey`, `buildFQDNEgressRules` (separate code path because `ToFQDNs` is mutually exclusive with `ToEndpoints`/`ToCIDR` in a single EgressRule).
2. `pkg/policy/builder.go` (MODIFY) — `peerRules` gains `httpRules`/`httpSeen`/`dnsRules`/`dnsSeen` maps keyed by `port/proto`; `groupFlows` calls extractors; `*RulesFrom` helpers attach `*api.L7Rules` to `PortRule`. `BuildPolicy` signature preserved.
3. `pkg/policy/merge.go` + `pkg/policy/dedup.go` (MODIFY) — fix `mergePortRules` (preserve `Rules`, merge per port/proto); extend `normalizeRule` to sort `Rules.HTTP` (by method+path) and `Rules.DNS` (by matchName/matchPattern) for deterministic equivalence; preserve nil-vs-empty-list distinction.
4. `pkg/evidence/{schema,writer,reader}.go` (MODIFY) — bump `SchemaVersion` to 2; `RuleKey` extends with optional L7 discriminator (otherwise two L7 rules on same `(direction, peer, port, proto)` collide); keep v1 read-only fallback for one minor cycle.
5. `cmd/cpg/explain*.go` (MODIFY) — three new filter flags (`--http-method`, `--http-path`, `--dns-pattern`), exact-match in v1.2; render `L7Ref` line per rule in text/JSON/YAML.

**Critical decision: `AggKey` does NOT extend with L7.** L7 is a property of a *rule* inside a CNP, not of the policy itself. Adding port/L7 to `AggKey` would shatter buckets and produce one CNP per port — opposite of what we want. L7 keying lives one level deeper, inside `peerRules`.

### Critical Pitfalls

(See PITFALLS.md for all 19 + integration gotchas.)

1. **L7 visibility chicken-and-egg (Pitfall 1)** — Hubble emits `Flow.L7` only when Envoy or DNS proxy intercepts. cpg cannot turn visibility on. Detect-and-warn (with copy-pasteable annotation command) and exit non-zero on `--l7-only` when zero L7 records observed. Hard requirement, not nice-to-have.
2. **`mergePortRules` silent L7 drop (Pitfall 8 + Architecture risk)** — current code flattens into `result[0].Ports` and discards `Rules`. Harmless today, breaks the moment L7 generation ships. Fix MUST land before any L7 codegen.
3. **HTTP path regex injection / under-anchoring (Pitfall 3)** — `rules.http[].path` is RE2, not glob, and not auto-anchored. `/api/v1.0/users` matches `/api/v1X0/users`; `/api/v1/users` matches `/evil/api/v1/users`. Security-impacting. Builder helper must `regexp.QuoteMeta` + `^…$` anchor; lint-before-write.
4. **HTTP path vs DNS pattern syntax (Pitfall 6)** — `path` is RE2 regex, `matchPattern` is DNS glob. Two separate builder helpers with strong types (`HTTPPath`, `DNSPattern`); never share a "pattern" helper.
5. **`toFQDNs` without DNS companion (Pitfall 5)** — without paired UDP+TCP/53 allow + `rules.dns: [{matchPattern: "*"}]` to kube-dns, the FQDN policy silently fails. Generator MUST emit both rules atomically in the same CNP. Hardcode `k8s-app=kube-dns` selector with a YAML comment in v1.2; runtime selector autodetect deferred to v1.3.
6. **Evidence schema v1 → v2 mandatory** — v1.1 reader rejects unknown versions; need v2 writer + v1-compat reader path. `RuleKey` extends with L7 discriminator to avoid attribution collisions.
7. **HTTP method casing (Pitfall 4)** — Cilium matcher is case-sensitive; some replay captures emit lowercase. `strings.ToUpper` at ingestion + whitelist (`GET POST PUT PATCH DELETE HEAD OPTIONS`).
8. **Path explosion (Pitfall 2)** — REST IDs blow up rule count. Documented as a known v1.2 limitation; opt-in path templating deferred to v1.3 (default behavior in v1.2 is honest verbose output, one rule per observation).

## Implications for Roadmap

All three architecture-touching researchers (STACK, ARCHITECTURE, PITFALLS) converge on the same 3-phase split. Phases 7–9 continue cpg's existing roadmap numbering (v1.0 = phases 1–3, v1.1 = phases 4–6).

### Phase 7: Infra-prep (no user-visible behavior change)
**Rationale:** All three downstream phases depend on three foundational fixes that, if shipped piecemeal with L7 generation, cause silent data loss or schema breakage. Land them first; v1.1 L4 output is byte-identical at the end of this phase.
**Delivers:**
- `mergePortRules` preserves `Rules` field, dedups L7 per port/proto, refuses to mix HTTP+DNS on same port/proto.
- `normalizeRule` sorts `Rules.HTTP` (by method+path) and `Rules.DNS` (by matchName/matchPattern) — `PoliciesEquivalent` deterministic for L7. Cluster-dedup inherits the fix.
- Evidence `SchemaVersion = 2` with `L7Ref` (additive); reader keeps v1 decode path for one cycle, refuses v3+; `RuleKey` extends with optional L7 discriminator.
- L7-visibility detection scaffold (skip-counter `l7_visibility_off`, warning copy + exit-code wiring) — hooked but unused until phase 8 wires `--l7`.
**Addresses:** PITFALLS 8 (L4 shadowing L7 — merge correctness), 12 (cluster-dedup blind to L7), evidence schema bump.
**Avoids:** Silent L7 data loss in any subsequent phase; evidence cache breakage on user upgrade.

### Phase 8: HTTP generation
**Rationale:** HTTP is the lower-risk L7 track structurally — it attaches to existing `toPorts` entries (no separate-EgressRule complication). DNS adds the FQDN-egress-rule complication and is built on the HTTP scaffolding.
**Delivers:**
- `pkg/policy/l7.go` HTTP path: `extractHTTP`, regex-escaped + anchored path, uppercase-normalized method, whitelist filter on methods.
- `peerRules` gains `httpRules`/`httpSeen` maps; `groupFlows` calls extractor; `ingressRulesFrom`/`egressRulesFrom` attach `*api.L7Rules{HTTP: …}` to matching `PortRule`.
- `--l7` opt-in flag wired in `cmd/cpg/{generate,replay}.go` (default OFF; preserves v1.1 behavior).
- L7-visibility detection actually fires when `--l7` set + zero L7 records observed; copy-pasteable annotation command in warning text; non-zero exit on `--l7-only`.
- Evidence v2 emission for HTTP rules (`L7Ref{Type:"http", HTTPMethod, HTTPPath}`); flow samples carry `l7_method`/`l7_path` for `cpg explain`.
- `RuleAttribution.RuleKey` carries L7 discriminator end-to-end.
- Incomplete-L7-record validator at ingestion: `incomplete_l7_http` skip counter for empty method or empty URL; `L7FlowType_RESPONSE` filtered out (no method/path to extract).
- Live-cluster validation of DROPPED vs REDIRECTED verdict behavior (open question) — filter expansion is a one-line change if needed.
**Uses:** `cilium/cilium/pkg/policy/api.PortRuleHTTP`, `api/v1/flow.HTTP` accessor.
**Implements:** ARCHITECTURE Q1 + Q2 + Q4 (HTTP slice).
**Addresses:** PITFALLS 1, 3, 4, 10, 14, 19; FEATURES table-stakes HTTP_GEN.

### Phase 9: DNS generation + `cpg explain` L7
**Rationale:** DNS adds the FQDN-egress-rule split (cannot coexist with `ToEndpoints`/`ToCIDR` in same EgressRule) and the mandatory companion-rule pairing. Lands on top of phase-8 scaffolding.
**Delivers:**
- DNS extractor in `pkg/policy/l7.go`: `extractDNSQuery` (request-only, trailing-dot stripped); `dnsGlob` helper distinct from HTTP path helper (typed at compile time to prevent cross-syntax bugs).
- `buildFQDNEgressRules` post-processing: emits paired EgressRules — (a) `toFQDNs` with the observed FQDN, (b) companion egress to `k8s-app=kube-dns/kube-system` on UDP+TCP/53 with `rules.dns: [{matchPattern: "*"}]`. Always atomic; never one without the other. Hardcoded selector + YAML comment listing the assumption.
- DNS dispatch keyed off `flow.GetL7().GetDns() != nil` (NOT port==53), to avoid Pitfall 7 (CIDR-when-should-be-FQDN trap kicks in for v1.3 correlation work, but v1.2 keeps the dispatch correct).
- Evidence v2 emission for DNS rules (`L7Ref{Type:"dns", DNSPattern}`).
- `cpg explain` filter flags: `--http-method`, `--http-path` (exact-match in v1.2), `--dns-pattern`. Renderer adds an L7 line per rule in text/JSON/YAML.
- README + `cpg generate --help`/`--l7 --help` block: two-step workflow front-and-center; capture-window guidance ("run for at least one full traffic cycle"); performance impact comment template auto-emitted on every L7 policy.
- `--dry-run` banner for L4→L7 transitions; FQDN-without-companion would-be-error caught at write time.
- Wildcard FQDN warning (`*.amazonaws.com` → identity exhaustion, suggest CIDR alternative).
- Optional polish: `pkg/output/annotate.go` annotates new L7 rule kinds with comments (non-blocking; render-without-comment is acceptable).
**Uses:** `cilium/cilium/pkg/policy/api.PortRuleDNS`, `FQDNSelector`, `EgressRule.ToFQDNs`, `api/v1/flow.DNS` accessor.
**Implements:** ARCHITECTURE Q1 (DNS slice), Q5 (FQDN egress split), Q8 (`cpg explain` L7).
**Addresses:** PITFALLS 5, 6, 7 (dispatch only), 13, 15, 16, 17; FEATURES table-stakes DNS_GEN, CLI explain L7, two-step workflow docs.

### Phase Ordering Rationale

- **Phase 7 first** — `mergePortRules` silent-data-loss bug is non-negotiable infra-prep. Evidence schema bump must precede any writer that wants to populate L7 fields. Both are zero-behavior-change at end-of-phase, so the branch is mergeable mid-stream.
- **Phase 8 before 9** — HTTP is structurally simpler (no EgressRule split, no companion rule). DNS reuses every piece of HTTP infrastructure (extractor pattern, evidence v2, attribution L7 discriminator, detection warning). DNS-first order would have to retrofit those onto HTTP later — wasteful.
- **`cpg explain` lands in phase 9, not in a separate phase** — extending filters and renderers is ~30–80 lines of cmd wiring against an already-stable schema; bundling with DNS keeps the phase counts honest at 3 (matches ~13 dev-day estimate at ~4-5 days/phase).

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 8 — DROPPED vs REDIRECTED verdict** — needs live-cluster validation. With L7 visibility on, denied L7 traffic may arrive as `Verdict_REDIRECTED` rather than `Verdict_DROPPED` (current Hubble client filter). One-line filter expansion if needed; trade-off is REDIRECTED also includes successful proxy traffic (must verdict-aware-handle to avoid generating policies *from allowed flows*). Recommend `/gsd:research-phase` before phase 8 implementation if no live-cluster access during phase 7.
- **Phase 9 — DNS REFUSED via FORWARDED verdict** — Cilium denies DNS via REFUSED rcode; flow may still show `Verdict_FORWARDED`. v1.2 with DROPPED-only filter will miss this. Document limitation; live-cluster validation recommended; `--include-l7-forwarded` flag deferred to v1.3.

Phases with standard patterns (skip research-phase):
- **Phase 7** — pure refactor + schema bump, all paths verified in codebase + STACK research. No additional research needed.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All types verified via `go doc` against vendored `cilium/cilium v1.19.1`. Zero new deps. PortRuleDNS-as-type-alias correction logged. |
| Features | HIGH | Cilium L7 schema, two-step workflow, companion-DNS requirement all confirmed in upstream docs (HIGH). MEDIUM only on regex-collapse heuristics — design choice deliberately deferred. |
| Architecture | HIGH | Codebase analysis direct; integration points enumerated by file + line. AggKey-stays-flat decision unanimous across stack/architecture/pitfalls research. |
| Pitfalls | HIGH | Verified against Cilium docs, Hubble flow proto, existing cpg codebase, and prior research archive. All 12 critical + 7 moderate pitfalls have prevention + warning-sign + phase mapping. |

**Overall confidence:** HIGH. Recommendation: proceed to roadmap creation.

### Gaps to Address

- **Empty-L7 warning copy/UX** — exact wording, exit-code semantics for `--l7-only`, and whether to also emit a structured JSON event need finalization in phase 8 requirements. Source material in PITFALLS 1 is sufficient as starting point.
- **kube-dns selector autodetection** — recommended hardcoded `k8s-app=kube-dns` (covers CoreDNS too) with YAML comment in v1.2; autodetect deferred to v1.3 (we already have a kube client when `--cluster-dedup` is on). Decide hardcoded copy in phase 9 requirements.
- **DROPPED vs REDIRECTED verdict** — needs live-cluster validation in phase 8. Mitigation: filter expansion is a one-line change; document trade-off (REDIRECTED includes successful proxy traffic — needs verdict-aware handling so we don't generate policies from allowed flows).
- **`--min-flows-per-l7-rule` default** — recommend default 1 in v1.2 with comment-out for low-confidence rules (`# low-confidence: 2 flows over 11m`); revisit after user feedback. Acceptable per PITFALLS 9 if `cpg explain` is documented as the gate.
- **DNS REFUSED via FORWARDED verdict** — documented as known v1.2 limitation; `--include-l7-forwarded` deferred to v1.3.

## Sources

### Primary (HIGH confidence)
- `go doc` on vendored `github.com/cilium/cilium@v1.19.1` — `pkg/policy/api` (`L7Rules`, `PortRuleHTTP`, `PortRulesHTTP`, `PortRuleDNS` type alias, `PortRulesDNS`, `FQDNSelector`, `EgressRule.ToFQDNs` exclusivity), `api/v1/flow` (`Layer7`, `L7FlowType`, `HTTP`, `HTTPHeader`, `DNS`).
- [Cilium L7 Policy Language](https://docs.cilium.io/en/stable/security/policy/language/) — official L7 rule syntax, RE2 regex on `path`, method case sensitivity.
- [Cilium DNS-Based Policies](https://docs.cilium.io/en/stable/security/dns/) — DNS proxy + companion rule requirement; matchPattern glob.
- [Cilium L7 Protocol Visibility](https://docs.cilium.io/en/stable/observability/visibility/) — chicken-and-egg confirmation; visibility annotation prerequisite.
- [Hubble Flow Proto](https://docs.cilium.io/en/stable/_api/v1/flow/README/) — `L7.Http`, `L7.Dns`, `DestinationNames` schema.
- [Cilium policy/api Go types on pkg.go.dev](https://pkg.go.dev/github.com/cilium/cilium/pkg/policy/api).
- [RFC 9110 §9.1 — HTTP method case sensitivity](https://www.rfc-editor.org/rfc/rfc9110#name-method).
- [Go regexp / RE2 syntax](https://pkg.go.dev/regexp/syntax) — anchoring + QuoteMeta.
- Existing cpg codebase (`pkg/policy/{builder,merge,dedup,attribution}.go`, `pkg/hubble/{client,aggregator}.go`, `pkg/output/writer.go`, `pkg/evidence/schema.go`, `cmd/cpg/explain*.go`) — direct read.
- `.planning/PROJECT.md` — v1.2 scope lock dated 2026-04-25.

### Secondary (MEDIUM confidence)
- [Cilium L7 Protocol Visibility — v1.20-dev docs](https://docs.cilium.io/en/latest/observability/visibility/) — schema unchanged in current main.
- WebSearch 2026-04-25 confirming `policy.cilium.io/proxy-visibility` as "historically supported but no longer recommended."
- [OneUptime — Cilium L7 Network Policies (2026-03-13)](https://oneuptime.com/blog/post/2026-03-13-cilium-l7-network-policies/view) — community confirmation of method/path/header model.
- [Cilium FQDN wildcard issue #22081](https://github.com/cilium/cilium/issues/22081) — wildcard subdomain limitations.
- [Cilium issue #31197 — FQDN DNS proxy truncation](https://github.com/cilium/cilium/issues/31197).
- [Cilium issues #35525 / #43964 / #30581 — Envoy redirect resets](https://github.com/cilium/cilium/issues/35525).
- [Debug Cilium toFQDN Network Policies (Medium)](https://mcvidanagama.medium.com/debug-cilium-tofqdn-network-policies-b5c4837e3fc4).

### Tertiary (LOW confidence)
- Prior research at `.planning/research/archive-2026-04-25/{STACK,FEATURES,ARCHITECTURE,PITFALLS,SUMMARY}.md` — superseded; this round re-verified types directly. Notable correction: `PortRuleDNS` is a type alias of `FQDNSelector`, not a parallel struct. Archive retains canonical material for v1.3-deferred topics (`cpg apply`, drift, RBAC pre-flight, Envoy-redirect-on-apply).

---
*Research completed: 2026-04-25*
*Ready for roadmap: yes*
