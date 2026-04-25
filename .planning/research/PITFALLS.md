# Pitfalls Research

**Domain:** Adding L7 (HTTP + DNS) policy generation to existing L4 generator (cpg v1.2)
**Researched:** 2026-04-25
**Confidence:** HIGH (verified against Cilium docs, Hubble flow proto, existing cpg codebase, prior research archive)

> **Scope note.** v1.2 is L7-only: `cpg apply`, drift detection, RBAC pre-flight, and Envoy-redirect-on-apply (P1, P4, P11 in archive) are deferred to v1.3 and are explicitly out of scope. Prior research archived at `.planning/research/archive-2026-04-25/PITFALLS.md` remains canonical for those topics.

---

## Critical Pitfalls

### Pitfall 1: L7 Visibility Off — `cpg generate --l7` silently produces L4-only output

**What goes wrong:**
Hubble only emits the `Flow.L7` field when traffic is being proxied by Envoy / the DNS proxy. That requires either (a) `enable-l7-proxy=true` on the Cilium agent (default true on most installs) AND (b) a `policy.cilium.io/proxy-visibility` annotation on the target pod, OR (c) an existing CNP with `rules.http`/`rules.dns` already redirecting that port. Without those, every flow has `L7 == nil`. If cpg's L7 builder simply iterates flows checking `f.GetL7() != nil` and finds zero, it writes the same L4 policies as v1.1 — but the user *thinks* they got L7 enforcement.

**Why it happens:**
Visibility is a Cilium-side prerequisite that cpg cannot satisfy by reading flows alone. The L7 field being empty is indistinguishable from "no L7 traffic happened" without auxiliary detection.

**How to avoid:**
- After flush, if **any** `toPorts` candidate matches a known L7 port (80, 8080, 443 with TLS-terminating gateway, 53/UDP) AND zero L7 records were observed for that workload, log a `WARN` with copy-pasteable remediation:
  ```
  WARN  no L7 records for production/api-server on 8080/TCP — L7 generation skipped.
        Enable visibility: kubectl annotate pod -n production -l app.kubernetes.io/name=api-server \
          policy.cilium.io/proxy-visibility='<Egress/53/UDP/DNS>,<Ingress/8080/TCP/HTTP>'
        Or apply a baseline L7 CNP first, then re-run cpg.
  ```
- Track the counter as a first-class skip reason `l7_visibility_off` in the Unhandled flows summary (parallel to `no_l4`).
- Document in README that L7 generation is **observational** — cpg cannot turn visibility on for you. Two-step workflow is canonical.
- **Do not silently downgrade to L4** when `--l7-only` is passed; exit non-zero so CI catches it.

**Warning signs:**
- `--l7` produced a policy file that is byte-identical to the v1.1 L4-only output.
- `Flow.L7` field is `nil` for 100% of observed flows on application ports.
- Hubble UI shows the same flows without method/path/query data.

**Phase to address:**
L7 ingestion phase. Detection runs before the builder — it gates whether L7 generation ran at all and produces an actionable warning.

---

### Pitfall 2: Path Explosion — REST IDs blow up rule count

**What goes wrong:**
A 10-minute capture of a typical REST API (`GET /api/users/123`, `GET /api/users/124`, `GET /api/orders/abc-uuid-…`, …) yields thousands of unique path strings. A naive builder that emits one `{method, path}` rule per observed (method, path) pair produces policies with 5000+ entries: unreviewable, unmergeable, and pushing Envoy filter-chain compile time into seconds. Operators see policies they can't audit, then disable cpg.

**Why it happens:**
`Flow.L7.Http.Url` carries the literal request URL. There's no semantic "route template" in the flow record — the API gateway / framework's route table is the only authoritative source, and Hubble doesn't have it.

**How to avoid:**
- **Default-on path templating** in the builder. Heuristics applied left-to-right per segment:
  - All-digits segment ≥2 chars → `\d+`
  - UUID v4 (36 chars, 8-4-4-4-12 hex) → `[0-9a-f-]{36}`
  - 24+ char hex (Mongo ObjectID, sha) → `[0-9a-f]+`
  - Base64-ish (≥16 chars `[A-Za-z0-9+/=_-]`) → `[A-Za-z0-9+/=_-]+`
- **Hard cap** per `(workload, method, peer)` tuple, default 50 rules. On overflow: collapse to a prefix rule (`^/api/.*`) and `WARN` with the dropped-paths count.
- **Opt-out**: `--l7-paths=literal` (no templating, raw paths) for users who *want* exact match — but require `--l7-paths-cap=N` explicit too, so they can't shoot themselves in the foot accidentally.
- **Observation budget visibility**: `cpg explain` should surface "X paths observed, Y collapsed into Z templates" so users see the lossiness.

**Warning signs:**
- Generated YAML > 1 MB for a single workload.
- `kubectl apply` slow (Envoy regenerates filter chains).
- `cilium-agent` CPU spike correlated with policy import.
- More than 100 distinct paths under a single `toPorts.rules.http`.

**Phase to address:**
L7 HTTP builder. Templating must ship in the same PR as raw path extraction — releasing literal-only first creates dependency on a bad default.

---

### Pitfall 3: Regex Injection / Under-anchoring on HTTP Path

**What goes wrong:**
Cilium's `rules.http[].path` is a Go `regexp` (RE2), **not** a glob and **not** anchored. Two failure modes:
1. **Under-escaping**: observed `/api/v1.0/users` written as `path: "/api/v1.0/users"` matches `/api/v1X0/users` (the `.` is a wildcard). For dotted version segments, query-strings, or paths with `?`/`*`/`+`/`(`, the rule is silently more permissive than intended.
2. **Under-anchoring**: `path: "/api/v1/users"` matches anywhere in the request path. `/evil/api/v1/users` is allowed. Cilium's Envoy filter does not auto-anchor.

This is a **security-impacting** bug — the rule looks correct in YAML review but allows traffic the operator believes is denied.

**Why it happens:**
RE2 semantics differ from glob (operators expect glob from K8s NetworkPolicy / nginx). Cilium docs mention regex but the example rules in tutorials happen to not contain regex metacharacters, so the trap is invisible until a path with a `.` shows up.

**How to avoid:**
- Builder helper `regexEscapePath(p string) string`: wrap `regexp.QuoteMeta(p)` and add `^` + `$` anchors.
- Templating substitutions (Pitfall 2) must produce already-escaped fragments — no double-escaping `\d+`.
- Round-trip test: every generated `path` value must compile via `regexp.MustCompile` AND its match-set must equal the observed input(s) (literal) or the templated set (templated). Property test: random URL paths through builder → only the input(s) match.
- Lint pass before write: reject any unescaped `(`, `[`, `{`, `?`, `+`, `*`, `|` not produced by templating.

**Warning signs:**
- `cilium policy get` shows path values without leading `^` or trailing `$`.
- An audit query (e.g., `curl /evil/api/v1/users`) succeeds against a policy that "should" forbid it.
- Path contains a literal `.` not preceded by `\`.

**Phase to address:**
L7 HTTP builder. Regex escaping is a same-PR sibling of templating; ship together.

---

### Pitfall 4: HTTP Method Casing — `get` vs `GET`

**What goes wrong:**
Cilium's `rules.http[].method` matcher is case-sensitive and Cilium normalizes nothing. Hubble flow protos historically populated `L7.Http.Method` from the wire, which is canonically uppercase, but some flow producers / replay captures / older Cilium versions emit lowercase or mixed-case. cpg copying the field verbatim writes `method: get`, which never matches real HTTP/1.1 traffic (always uppercase on the wire) → silent allow-nothing.

**Why it happens:**
Observed in Cilium issues around envoy filter regen. RFC 9110 §9.1: methods are case-sensitive and uppercase by convention. But Hubble's L7 parser path varies between Envoy proxy and DNS proxy producers; replay captures from older Cilium runtimes can have lowercased fields.

**How to avoid:**
- Normalize at ingestion: `strings.ToUpper(flow.GetL7().GetHttp().GetMethod())`.
- Whitelist known methods (`GET POST PUT PATCH DELETE HEAD OPTIONS`); skip with `unknown_http_method` skip-counter for anything else.
- Round-trip test: lowercase / mixed-case input → uppercase output.

**Warning signs:**
- Generated YAML contains lowercase methods.
- Apply succeeds, but Hubble shows DROPPED on traffic the policy claims to allow.

**Phase to address:**
L7 HTTP builder. One-line normalization, one test, ship in the first L7 PR.

---

### Pitfall 5: ToFQDNs Without Companion DNS Allow Rule (silent breakage)

**What goes wrong:**
`toFQDNs` works because Cilium's in-agent DNS proxy intercepts the resolver response and pins the FQDN→IP mapping for that identity. If DNS itself isn't allowed (UDP/53 to kube-dns) AND inspected (`rules.dns.matchPattern`), the proxy never sees the answer, the mapping is never populated, and traffic to the FQDN is dropped — silently. Worse: nothing in the Hubble feed of that workload says "DNS was denied"; the user only sees "egress to api.example.com is dropped despite my policy allowing it." This is **the** classic ToFQDNs trap.

**Why it happens:**
The two rules belong logically together but live in different `toPorts`/`toEndpoints` blocks. Operators writing by hand from a tutorial copy the FQDN block and forget the DNS block. cpg generating from observed FQDN flows can fall into the same trap if the DNS-companion rule is treated as optional.

**How to avoid:**
- `toFQDNs` generator MUST also emit the kube-dns companion in the **same egress rule list** of the same CNP:
  ```yaml
  egress:
  - toEndpoints:
    - matchLabels:
        k8s:io.kubernetes.pod.namespace: kube-system
        k8s:k8s-app: kube-dns
    toPorts:
    - ports: [{port: "53", protocol: ANY}]
      rules:
        dns:
        - matchPattern: "*"
  - toFQDNs:
    - matchPattern: "*.example.com"
    toPorts:
    - ports: [{port: "443", protocol: TCP}]
  ```
- Idempotency: if cluster-dedup detects an existing cluster-wide DNS allow CNP / CCNP that covers this workload, skip the companion and add a YAML comment `# DNS allow handled by <cluster-policy-name>` so reviewers understand.
- Make the companion match-pattern **scoped** to the FQDN being allowed (`matchPattern: "*.example.com"` not `"*"`) when the user is privacy-sensitive — flag-gated `--l7-dns-scope=match|wildcard`, default `match`.

**Warning signs:**
- Egress traffic to FQDN target dropped despite policy "allowing" it.
- `hubble observe -t l7` shows DNS queries in DROPPED state.
- Generated CNP has `toFQDNs` but no sibling `rules.dns` block.

**Phase to address:**
L7 DNS builder. Hard requirement — `toFQDNs` and the companion ship in the same code path; generator never emits one without the other.

---

### Pitfall 6: ToFQDNs `matchPattern` Glob ≠ HTTP `path` Regex

**What goes wrong:**
Cilium uses **two different syntaxes** in the same CRD:
- `rules.http[].path` → RE2 regex
- `toFQDNs[].matchPattern` and `rules.dns[].matchPattern` → DNS-style glob (`*` = any subdomain label, `?` = single char)

Copy-pasting between them, or asking a single helper to "make a pattern," produces wrong-syntax matchers. `*.example.com` in the path field matches nothing reasonable (regex: any-char × 0 or more, then `example` then any-char then `com`); `^.*\.example\.com$` in the FQDN field is treated literally — it never matches a real FQDN.

**Why it happens:**
The fields have similar names (`matchPattern` vs `path`) and both accept "patterns". The CRD schema doesn't enforce different syntax — both are strings.

**How to avoid:**
- Two **separate** builder helpers, no shared "pattern" helper:
  - `httpPathRegex(literal string) string` (Pitfall 3)
  - `dnsGlob(domain string) string` returning `*.<domain>` or stripped trailing-dot exact
- Strong types in Go: `type HTTPPath string` and `type DNSPattern string`. The builder API takes the typed values; a string can't accidentally flow into the wrong slot.
- Test matrix: feed the cross-product (HTTP literal, HTTP regex, DNS exact, DNS glob) into both helpers and assert the wrong-helper output is rejected by validation.

**Warning signs:**
- A `toFQDNs.matchPattern` value contains `^`, `$`, `\.`, `(`, `)`, `[`.
- A `rules.http.path` value contains a leading `*.`.
- Cilium agent logs `invalid pattern` on policy import.

**Phase to address:**
L7 builder phase, type-design subtask. Cheap to get right at the start, expensive to retrofit.

---

### Pitfall 7: Reading the IP Layer of a DNS Flow Instead of the DNS Layer

**What goes wrong:**
A Hubble flow representing a DNS *resolution* contains both an L4 layer (UDP/53 to kube-dns) and an L7 DNS payload (`L7.Dns.Query`, `L7.Dns.Ips`, `L7.Dns.Rcode`). If the L7 builder dispatches on "destination port == 53" rather than `L7.GetDns() != nil`, two failures:
1. It writes a `toEndpoints` rule allowing UDP/53 to kube-dns (correct but redundant) and **misses** the FQDN rule entirely.
2. For the *application* flow that was made possible by the resolution (e.g., HTTPS to the resolved IP), the builder writes a `toCIDR` rule for the resolved IP — defeating the whole point of FQDN policies (the IP rotates, the policy goes stale).

**Why it happens:**
The L7 DNS record sits inside `L7.Dns`; the IP layer of the *subsequent* HTTPS flow shows a CIDR. Treating each flow independently and matching on L4 alone produces CIDR egress rules that the operator wanted to be FQDN rules.

**How to avoid:**
- DNS dispatch keys off `flow.GetL7().GetDns() != nil` (or equivalently `flow.GetL7().GetType() == flow.L7FlowType_RESPONSE` AND `Dns` populated) — never off `port == 53`.
- For the application-flow side (the HTTPS flow to the resolved IP), maintain a per-(source-identity) DNS cache built from observed DNS RESPONSE flows: `IP → FQDN`. When generating a CIDR egress rule, look up the dest IP in the cache. If found, emit `toFQDNs` instead of `toCIDR` and add the kube-dns companion (Pitfall 5).
- Cache TTL bounded: observed-IP entries expire after 1h or end-of-session. Document that cpg's FQDN inference is best-effort: if no DNS resolution was captured for an IP, we fall back to CIDR with an INFO log.
- `cpg explain` shows the inferred FQDN with provenance: "IP 1.2.3.4 → api.example.com (inferred from DNS RESPONSE flow at 14:02:11)".

**Warning signs:**
- Generated policy has `toCIDR: 1.2.3.4/32` for an IP that's clearly a CDN / cloud-managed endpoint (rotating).
- The `toFQDNs` block is empty even though DNS traffic was captured.
- Re-running cpg a day later produces a different `toCIDR` set for the same workload (IP rotation).

**Phase to address:**
L7 DNS builder. Cross-flow correlation (DNS resp → IP → subsequent flow) is the harder design problem; specify it before coding.

---

### Pitfall 8: L4-only `toPorts` Becomes "Allow All L7" When L7 Is Enabled

**What goes wrong:**
Cilium policy semantics: a `toPorts` block **without** `rules` allows all traffic on that port (including all HTTP, all DNS). A `toPorts` **with** `rules.http: [{}]` (empty match) is also "allow all HTTP". A `toPorts` with `rules.http: [{method: GET, path: ^/foo$}]` is restrictive.

The trap: if cpg merges a v1.1-generated L4-only policy with new L7 observations, two paths exist:
- **Wrong**: Keep the L4-only port entry AND add a separate L7 entry for the same port. Cilium evaluates `toPorts` as a *union* — the L4-only entry wins, and the L7 rules are ignored. Operator audits the YAML, sees method/path, believes enforcement is L7. It isn't.
- **Right**: Replace the L4-only entry with the L7-restrictive entry. But this is a behavior change (was: "any TCP/8080"; now: "only GET /foo"), which can break legitimate traffic that wasn't observed in the capture window.

**Why it happens:**
`pkg/policy/merge.go::mergePortRules()` (lines 172+) currently dedupes by (port, protocol) only. Adding L7 to the mix without a deliberate merge strategy collapses to the wrong path.

**How to avoid:**
- Per-port merge policy is explicit and labeled in code:
  - `mergeL7Strategy = "replace"` (default with `--l7`): port becomes L7-restricted; emit a `WARN` listing what was previously L4-only and asking the user to re-capture if they expected wider traffic.
  - `mergeL7Strategy = "augment"` (advanced): keep L4-only AND add a separate, sibling CNP with L7 rules so the L4 one is delete-able later. Requires the user to manage two CNPs.
  - **Refuse to silently mix L4-only and L7 entries on the same port within one CNP**.
- Diff in `--dry-run` must highlight L4 → L7 transition with an explicit banner: `[!] toPorts/8080: L4-only → L7-restrictive (was implicitly allow-all-HTTP, now restricted to N method+path rules)`. This is the user's last chance to catch over-tightening.
- Test cases: (a) merge L4-only + L7 fresh, (b) merge L7 + L7 same-type, (c) merge L7 + L7 type-conflict (HTTP vs DNS on same port — see archive Pitfall 2).

**Warning signs:**
- Generated CNP has two `toPorts` entries with the same port: one with `rules.http`, one without.
- Audit traffic shows methods/paths the policy "shouldn't" allow but does — because the L4-only entry shadowed the L7 one.

**Phase to address:**
L7 merge phase. Must precede the HTTP builder going to disk.

---

### Pitfall 9: Capture-Window Drift — Observed ≪ Production Reality

**What goes wrong:**
A 10-minute capture sees `GET /api/v1/users/me` but not the once-per-day `POST /api/v1/admin/sync`. Generated policy enforces `GET` only. Apply at 02:00, the daily sync at 03:00 fires, gets dropped, on-call gets paged. The L7 builder's exhaustiveness is fundamentally bounded by the capture window — and this gap is invisible from inside cpg.

**Why it happens:**
L4 generation suffered the same problem (a port that wasn't hit during capture isn't in the policy), but the blast radius was small (one port = one rule). L7 expands the surface: one port has dozens of (method, path) combos, and rare ones are over-represented in production tail traffic.

**How to avoid:**
- `cpg explain` (already shipped v1.1) is the primary mitigation — every rule has flow evidence with `first_seen`/`last_seen`/`flow_count`. Document the workflow: "before applying, run `cpg explain` and look for rules with flow_count < 5 — those are the ones most likely to be capture-window artifacts."
- Add an L7-specific session summary at flush:
  ```
  L7 coverage:  api-server  HTTP: 4 methods, 12 templated paths, 891 flows
                api-server  source diversity: 3 distinct peer identities
                api-server  observation: 11m 47s
  ```
- New `--min-flows-per-l7-rule N` flag (default 3): rules backed by fewer than N flows are emitted as commented-out YAML lines `# low-confidence: 2 flows over 11m`. User uncomments deliberately.
- README: explicit "**Run for at least one full traffic cycle**" guidance — for batch / cron / weekly traffic, capture must span one full period.
- `--dry-run` diff includes a "rules added since previous run" section; if the user has been iterating, they see new rare-traffic rules accumulate.

**Warning signs:**
- Production drops on traffic that "should" be allowed, post-apply.
- A single-flow rule for a method/path the operator doesn't recognize.
- Capture session shorter than the workload's known traffic period.

**Phase to address:**
L7 ingestion phase (flow counting) + L7 explain phase (surface low-confidence rules). Documentation is part of the phase.

---

### Pitfall 10: Hubble Partial L7 Records — Method Without Path

**What goes wrong:**
Some Cilium versions, some Envoy filter configs, and some replay captures emit L7 HTTP records with `Method` populated but `Url` / `Path` empty (or vice versa). The DNS path is similar: `Query` may be present but `Ips` empty (NXDOMAIN, truncation). A naive builder either:
- Silently writes `path: ""` → matches everything (worse than no rule).
- Crashes on a nil dereference.
- Writes a half-rule the user can't review.

**Why it happens:**
Envoy's HTTP filter has a fast-path that doesn't always serialize the full URL into the access log, depending on the proxy config (`policy_enforcement_mode`, `http_normalize_path`). Replay captures from multiple Cilium versions mix records.

**How to avoid:**
- Validation per L7 record at the ingestion boundary, fail-soft:
  - HTTP REQUEST: require non-empty `Method` AND non-empty `Url`/`Path`. Otherwise skip with `incomplete_l7_http` counter.
  - DNS RESPONSE: require non-empty `Query` AND `Rcode == 0` for FQDN inference. Otherwise skip with `incomplete_l7_dns` counter.
  - HTTP RESPONSE flows (`L7FlowType_RESPONSE` for HTTP): always skip — they carry no method/path. (Per archive P9.)
- Skip-reason counter surfaces in the flush summary; user can `--debug` to see which records were dropped and why.
- **Never** emit a half-rule. Empty path → no rule at all.

**Warning signs:**
- `incomplete_l7_*` counter > 0 in the flush summary.
- Generated CNP has a `path: ""` or `method: ""` value (would indicate the validator was bypassed — bug).

**Phase to address:**
L7 ingestion phase. The validator gates entry to the builder; cheaper than fixing every builder code path.

---

### Pitfall 11: Compounding with Existing `peerKey` Aggregation

**What goes wrong:**
v1.1 aggregates flows by `peerKey = (peer-selector, port, protocol)`. Adding L7 means two flows with identical `peerKey` but different (method, path) must produce a *single* `toPorts` entry with multiple `rules.http[]` items, not two `toPorts` entries that differ only in their L7 rules. If the aggregator stays as v1.1 — keying only by L3/L4 — it does this correctly by accident; but if the L7 builder runs *after* L4 aggregation, only the last-observed (method, path) survives the map collapse.

**Why it happens:**
The aggregator was designed for a value type that has no L7 dimension. Adding L7 either requires:
- Extending the value type to be a list of L7 rules (and the aggregator merges lists at flush), OR
- Keying by L7 and getting back the peerKey-explosion of Pitfall 2 inside the aggregator.

**How to avoid:**
- Keep peerKey identical to v1.1 (L3/L4 only — `peer × port × protocol`).
- Aggregator value gains an `L7Rules` field — a deduplicating set of (method, templated-path) for HTTP, (matchPattern) for DNS. Merge on insert: dedup by tuple, cap at the rule limit (Pitfall 2).
- The builder consumes the `L7Rules` set unchanged and emits one `toPorts` block per peerKey.
- Aggregator unit tests: 100 flows with same peerKey, 5 distinct (method, path) → 1 toPorts entry, 5 http rules.

**Warning signs:**
- Generated CNP has duplicate `toPorts` entries with same port that differ only in L7.
- `flow_count` in evidence sums > total observed flows (double-counting from key explosion).

**Phase to address:**
L7 aggregation phase, before builder. This is a v1.1-internals refactor disguised as an L7 feature.

---

### Pitfall 12: Compounding with Cluster Dedup — L7 Comparison Asymmetry

**What goes wrong:**
`--cluster-dedup` (v1.0) skips a generated policy if it's structurally equivalent to a live cluster policy. Two compounding traps:
- **L7 not normalized in `PoliciesEquivalent`**: `pkg/policy/dedup.go::normalizeRule()` (line 49+) sorts L4 fields only. A locally-generated CNP with HTTP rules in `[GET, POST]` order vs a cluster CNP with `[POST, GET]` order is reported as "different" → cpg overwrites every flush. (Mirrors archive P8.)
- **Cluster has L4-only, generated has L7 (or vice versa)**: dedup can falsely call them equivalent if it strips L7 before comparing, or falsely call them different if it doesn't. Either way the operator gets the wrong answer about whether the cluster is up-to-date.

**How to avoid:**
- Extend `normalizeRule` to deeply sort `PortRule.Rules.HTTP` (by (method, path)), `PortRule.Rules.DNS` (by (matchName, matchPattern)), `PortRule.Rules.Kafka` (by topic/role). Empty/nil L7 normalizes the same way as a missing field — but a populated-with-empty-list L7 (`rules.http: []`) is **not** equivalent to a missing L7 (semantically: empty list = "match nothing", missing = "allow all").
- `PoliciesEquivalent` test matrix: (L4 only, L4+empty-HTTP, L4+populated-HTTP) cross-product, both directions, all asserted distinct except identity.

**Warning signs:**
- cpg writes the same policy every flush even with `--cluster-dedup` set.
- Diff between cluster and local shows only key ordering changes.

**Phase to address:**
L7 dedup phase. Must precede first L7 release, otherwise `--cluster-dedup` regresses.

---

## Moderate Pitfalls

### Pitfall 13: DNS Trailing-Dot Mismatch
Hubble DNS query is FQDN with trailing dot (`api.example.com.`); Cilium `matchName` requires no trailing dot. Strip with `strings.TrimSuffix(query, ".")` at ingestion. (Archive P10.)

### Pitfall 14: HTTP Response Flows Have No Method/Path
`L7FlowType_RESPONSE` HTTP flows carry status code only. Filter to `REQUEST` only at the L7 ingestion boundary. (Archive P9.)

### Pitfall 15: ToFQDNs Identity Exhaustion on Wildcards
`*.amazonaws.com` resolves to hundreds of IPs, each gets a Cilium identity → identity-space pressure, agent CPU. WARN on wildcards that would match common cloud providers (`*.amazonaws.com`, `*.azure.*`, `*.googleapis.com`). Suggest CIDR alternatives in the warning text. (Archive P12.)

### Pitfall 16: `kube-dns` Selector Is Cluster-Specific
The companion DNS rule (Pitfall 5) needs the right selector for *this cluster's* DNS service. Most clusters use `k8s-app=kube-dns` (covers CoreDNS too — service is named `kube-dns` even when CoreDNS is the impl). Some use NodeLocal DNS (`k8s-app=node-local-dns`). Detect at runtime if `--cluster-dedup` is on (we already have a kube client); otherwise fall back to `k8s-app=kube-dns` and emit a YAML comment listing the assumption.

### Pitfall 17: Envoy Performance Impact Not Communicated
Adding L7 to a port adds an Envoy hop per packet. p99 latency can rise by 1-5ms; throughput drops 10-30% on TLS-terminating workloads. cpg should emit a YAML header comment on every L7 policy:
```yaml
# cpg: L7 policy. Enables Envoy proxy for the listed ports.
# Performance impact: +1-5ms p99 latency, ~10-30% throughput reduction on TLS.
# Connection migration: applying this for the first time on a port that previously
# had no L7 enforcement will reset existing connections (TCP RST). Roll out off-peak.
```
This is the v1.2-scoped echo of archive P1 (Envoy redirect on apply): we don't *prevent* the disruption (apply isn't in v1.2), we *warn* in the artifact.

### Pitfall 18: Identity Churn Compounds L7 Cost
Each Cilium identity gets its own Envoy filter chain. Workloads with high pod-restart rate (CronJobs, HPA-thrashing, preemptible workers) cause filter-chain regen storms when L7 is enabled. This isn't cpg's bug to fix, but cpg should detect: if observed source identities for a workload exceed (say) 50 over the capture window, WARN that L7 enforcement may amplify identity-churn cost.

### Pitfall 19: TLS-Terminating Ingress Hides L7 Visibility
Cilium can only inspect HTTP, not HTTPS-not-yet-terminated. If the workload is talking to TLS endpoints directly (no Envoy SNI termination configured), `L7.Http` is `nil` for those flows even though the L4 layer says port 443. Don't try to generate HTTP rules for port 443 unless an L7 record was actually observed — fall back to L4 with an INFO note "port 443 TLS not L7-inspected".

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Skip path templating, ship literal-only first | Faster v1.2 cut | Users adopt with raw paths, complain about explosion, blame cpg | Never — ship templating in same PR |
| Skip regex anchoring/escaping (Pitfall 3) | Tiny code reduction | Security regression: rules more permissive than reviewed | Never |
| Treat HTTP and DNS patterns as one helper | Less code | Pitfall 6: cross-syntax bugs at runtime | Never |
| Detect L7 visibility off but proceed silently | Smaller error path | User ships a fake-L7 policy to prod, no enforcement | Never |
| Don't emit DNS companion automatically (Pitfall 5) | Builder simpler | Every FQDN policy fails for users until they figure out the missing piece | Never |
| Punt the aggregator refactor (Pitfall 11) — re-key by full L4+L7 tuple | Less plumbing | Rule explosion inside aggregator, evidence double-counts | Never |
| Default `--min-flows-per-l7-rule=1` (Pitfall 9) | More rules captured by default | Capture-window flukes become enforced rules | Acceptable for v1.2 if `cpg explain` is documented as the gate |
| Skip cluster-dedup L7 normalization in v1.2 | Less code | `--cluster-dedup` regresses for L7 users; constant rewrites | Never — same release as builder |

## Integration Gotchas (cpg-internal compounds)

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| `pkg/policy/builder.go::BuildPolicy` (lines 109, 171) | Skip flow on `f.L4 == nil` only; assume L7 implies L4 | Keep L4 guard; **also** dispatch to L7 sub-builder when `f.GetL7() != nil`, even when the L4 path emits an L4 rule |
| `pkg/hubble/aggregator` peerKey | Add (method, path) to peerKey to avoid losing L7 detail | Keep peerKey at L4; add `L7Set` to value type (Pitfall 11) |
| `pkg/policy/merge.go::mergePortRules` (line 172) | Concat `Rules` slices unconditionally | Merge L7 rules under same port; honor type-union (HTTP xor DNS xor Kafka per port-rule); explicit replace-vs-augment policy (Pitfall 8) |
| `pkg/policy/dedup.go::normalizeRule` (line 49) | Sort L4 only | Sort HTTP by (method, path), DNS by (matchName, matchPattern); preserve nil-vs-empty distinction (Pitfall 12) |
| `pkg/labels/selector.go` for kube-dns companion | Hardcode `k8s-app=kube-dns` | Hardcode is acceptable for v1.2 with comment; runtime detection deferred (Pitfall 16) |
| `pkg/evidence/writer.go` schema | Reuse v1 schema for L7 (it has no method/path field) | Bump to schema v2; samples carry `l7_method`, `l7_path` / `l7_query`, `l7_rcode`; reader rejects unknown fields per existing pinning policy |
| `pkg/diff/yaml.go` for `--dry-run` | Treat L4→L7 transition as a normal field change | Dedicated banner output for L4→L7 transitions and FQDN-without-DNS-companion warnings (Pitfall 8) |
| `cpg explain` rendering | Show ports only | Surface method/path/FQDN per rule + flow_count per (method, path) so capture-window low-confidence is visible (Pitfall 9) |
| `pkg/hubble/unhandled.go` skip counters | Add `l7_visibility_off`, `incomplete_l7_http`, `incomplete_l7_dns`, `unknown_http_method` | Each surfaces in flush summary like existing `no_l4` etc. |
| Reserved-identity warnings (host, kube-apiserver) | Apply only at L4 generation | Same warn applies for L7; ensure code path covers L7 path |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Path explosion (Pitfall 2) | YAML > 1MB, slow `kubectl apply`, agent CPU | Templating + per-port rule cap | ~100 paths/port, ~5k paths total |
| FQDN identity blow-up (Pitfall 15) | Cilium identities > 10k, agent restart loop | Warn on cloud-provider wildcards, suggest CIDR | CDN/cloud domains, immediately |
| Envoy filter regen on identity churn (Pitfall 18) | agent CPU spikes correlated with pod churn | Warn on >50 source identities per workload | High-restart workloads with L7 |
| Envoy hop overhead (Pitfall 17) | p99 latency rise post-apply | YAML header comment with explicit perf cost | Always present, varies by workload |
| L7 flow volume during capture | Hubble Relay backpressure, dropped flows in cpg pipeline | L7 is opt-in (`--l7`); document `--flush-interval` tuning | High-RPS workloads with visibility broadly enabled |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Unanchored / unescaped regex path (Pitfall 3) | Generated rule is more permissive than reviewed; bypass via crafted URL | `regexp.QuoteMeta` + `^…$` anchors; lint-before-write |
| L4-only entry shadowing L7 entry on same port (Pitfall 8) | Operator believes traffic is L7-restricted; isn't. Audit-bypassable | Refuse to mix in builder; explicit replace-vs-augment policy |
| Method case mismatch (Pitfall 4) | Rule never matches; user *thinks* GET-only, actually nothing allowed (fail-closed but invisible) | Normalize at ingestion + whitelist |
| FQDN without DNS allow (Pitfall 5) | Effective deny → operator opens broader rule to "fix" → wider attack surface | Auto-generate companion |
| Generated rule from compromised flow data | If Hubble/replay file is tampered, generated policy could whitelist attacker traffic | Replay file source provenance is operator's responsibility; document in README. Out of scope for cpg to validate. |
| Inferring FQDN from observed IP (Pitfall 7) | Wrong FQDN inferred if multiple FQDNs resolve to same IP | Cache only RESPONSE→IP mappings observed within capture; conflicts → fall back to CIDR + WARN |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| `--l7` produces empty policies silently when visibility is off | User loses confidence in cpg, suspects bug | Hard WARN + non-zero exit on `--l7-only`; copy-pasteable annotation command (Pitfall 1) |
| Captured-too-short policy gets applied, breaks rare traffic (Pitfall 9) | 03:00 page on missing daily-cron rule | `--min-flows-per-l7-rule` flag, low-confidence comments, README guidance |
| YAML diff of L4→L7 transition looks like "small change" | Operator approves without realizing connection-reset risk | Explicit banner in `--dry-run` for L4→L7 transitions |
| HTTP/DNS pattern syntax confusion (Pitfall 6) | Operator copy-pastes a pattern that looks reasonable but doesn't match | Typed APIs in code; in README, side-by-side example showing both syntaxes with the *same* domain to make the difference visible |
| `cpg explain` for L7 hides flow_count per (method, path) | Reviewer can't tell which rules are well-supported | Render one row per (method, path) with its own flow_count, first/last seen |
| FQDN companion DNS rule is "noise" in YAML review | Reviewer asks "why is this rule here?" | YAML comment auto-inserted: `# Required by toFQDNs above; without this, FQDN resolution is denied and the policy never matches.` |

## "Looks Done But Isn't" Checklist

- [ ] **L7 HTTP builder:** Method normalized to uppercase? Path regex-escaped AND anchored? Templating applied? Per-port rule cap honored? Tests for all four?
- [ ] **L7 DNS builder:** Trailing dot stripped? Companion kube-dns rule emitted in same CNP? Wildcard-FQDN warning fires? `matchName` vs `matchPattern` chosen correctly per input?
- [ ] **L7 dispatch:** Builder dispatches on `flow.GetL7() != nil`, not `port == 80`? RESPONSE flows filtered out for HTTP? `Rcode==0` filtered for DNS?
- [ ] **L7 visibility detection:** When zero L7 records observed but L7 mode requested, WARN with remediation command? Skip-counter `l7_visibility_off` populated?
- [ ] **Aggregator:** peerKey unchanged (L3/L4 only)? Value type carries deduped L7 set? 100-flow / 5-tuple test passes with single `toPorts` block?
- [ ] **Merge:** L7 rules merged under same port? L4-only + L7 same port refuses to silently mix? Replace-vs-augment policy explicit and tested?
- [ ] **Dedup `normalizeRule`:** HTTP sorted by (method, path)? DNS sorted? Empty-list-vs-nil distinction preserved? Cluster-dedup round-trip with L7 stable across flushes?
- [ ] **Evidence schema:** Bumped to v2? Reader rejects v1+L7? Method/path/query persisted? `cpg explain` reads new fields?
- [ ] **Diff (`--dry-run`):** L4→L7 transition banner present? FQDN-without-companion would-be-error caught at write time?
- [ ] **README:** Two-step workflow documented? Capture-window guidance present? L7 visibility prerequisite documented? Performance impact comment template shown?

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Visibility off, fake-L7 policy applied (Pitfall 1) | LOW | Delete policy, enable visibility annotations, re-run cpg with `--l7`. No traffic was ever L7-enforced; behavior unchanged. |
| Path explosion in committed YAML (Pitfall 2) | MEDIUM | Re-run cpg with templating; commit replacement; review diff carefully (now broader). |
| Under-anchored regex bypassed in audit (Pitfall 3) | HIGH | Treat as security incident. Patch CNP manually, then re-run cpg with fixed builder, then commit. Audit logs for any bypass attempts. |
| Method-casing bug (Pitfall 4) | LOW | Re-generate, re-apply. Workloads were fail-closed (denied), not breached. |
| FQDN without DNS companion (Pitfall 5) | LOW | Add companion (manually or re-run with fixed builder); FQDN policy starts working. |
| HTTP/DNS pattern syntax mix-up (Pitfall 6) | LOW | Cilium agent rejected the bad-syntax policy on import; fix and re-apply. |
| CIDR-when-should-be-FQDN (Pitfall 7) | MEDIUM | Re-capture with DNS visibility on; re-run cpg; replace CIDR rules with FQDN rules. Policies must be re-applied on each IP rotation otherwise. |
| L4 shadowing L7 (Pitfall 8) | HIGH if security-relevant | Fix builder merge; re-generate; review diff; re-apply during a maintenance window (connection reset on Envoy redirect). |
| Capture-window drift breaks production (Pitfall 9) | HIGH (page) | Roll back policy; lengthen capture window across a full traffic period; re-run; re-review. |
| Aggregator dup `toPorts` (Pitfall 11) | LOW | Patch aggregator; re-run from same captures. |
| Cluster-dedup constantly rewriting (Pitfall 12) | LOW (annoyance) | Patch `normalizeRule`; rewrites stop. |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 1: Visibility off | L7 ingestion | Integration test with no-L7 capture → emits WARN, exits non-zero with `--l7-only` |
| 2: Path explosion | L7 HTTP builder | Unit test: 5000 unique IDs → ≤50 templated rules; YAML byte size budget |
| 3: Regex injection / under-anchor | L7 HTTP builder | Property test: random URLs round-trip through builder, only original(s) match generated regex |
| 4: Method casing | L7 ingestion | Table test: `get/Get/GET` all → `GET`; `unknown` → skip |
| 5: ToFQDNs DNS companion | L7 DNS builder | Test: `toFQDNs` rule built ⇒ companion present in same `egress` list |
| 6: HTTP vs DNS pattern syntax | L7 builder design | Type test: cross-feeding rejected at compile time |
| 7: DNS layer vs IP layer | L7 DNS ingestion | Test: DNS RESPONSE flow + subsequent HTTPS to resolved IP → `toFQDNs` rule (not `toCIDR`) |
| 8: L4 shadowing L7 | L7 merge | Test: merge of v1.1 L4-only CNP with new L7 obs → single `toPorts` per port; explicit strategy logged |
| 9: Capture-window drift | L7 explain + UX | Manual test: `cpg explain` shows flow_count per (method, path); `--min-flows-per-l7-rule` flag works |
| 10: Partial L7 records | L7 ingestion | Table test: empty-method, empty-path, empty-query, `Rcode!=0` → skipped + counter incremented |
| 11: Aggregator peerKey + L7 | L7 aggregation | Test: 100 flows × 5 (method,path) → 1 toPorts, 5 http rules |
| 12: Cluster-dedup blind to L7 | L7 dedup | Test: shuffled HTTP-rule order between local and cluster → equivalent; missing-vs-empty L7 → not equivalent |
| 13: DNS trailing dot | L7 DNS ingestion | Unit: `api.example.com.` → `api.example.com` |
| 14: HTTP RESPONSE filtered | L7 ingestion | Unit: RESPONSE flow → skipped, REQUEST flow → built |
| 15: Identity exhaustion on wildcards | L7 DNS builder | Unit: `*.amazonaws.com` → emit WARN, suggest CIDR |
| 16: kube-dns selector cluster-specific | L7 DNS builder | Comment in YAML; runtime detection deferred |
| 17: Envoy perf cost not communicated | L7 builder output | Generated YAML contains header comment; snapshot test |
| 18: Identity churn × L7 | L7 ingestion telemetry | Unit: >50 source identities triggers WARN |
| 19: TLS-not-terminated 443 | L7 ingestion | Unit: port 443 flow with `L7==nil` → L4 rule + INFO note, no HTTP rule attempted |

## Sources

- [Cilium L7 Protocol Visibility](https://docs.cilium.io/en/stable/observability/visibility/) — visibility annotation prerequisite (Pitfall 1)
- [Cilium DNS-Based Policies](https://docs.cilium.io/en/stable/security/dns/) — `toFQDNs` mechanics, DNS companion (Pitfall 5), matchPattern glob (Pitfall 6)
- [Cilium Policy Language Reference](https://docs.cilium.io/en/stable/security/policy/language/) — RE2 regex on `path` (Pitfall 3), L7 union (archive P2), method matching (Pitfall 4)
- [Cilium Envoy Proxy](https://docs.cilium.io/en/stable/security/network/proxy/envoy/) — performance impact (Pitfall 17), filter-chain regen (Pitfall 18)
- [Cilium Flow Protobuf API](https://docs.cilium.io/en/stable/_api/v1/flow/README/) — `L7.Http`, `L7.Dns`, `L7FlowType` (Pitfalls 7, 10, 14)
- [Hubble Visibility annotation reference](https://docs.cilium.io/en/stable/observability/visibility/#proxy-visibility) — annotation syntax (Pitfall 1)
- [RFC 9110 §9.1 — HTTP method case sensitivity](https://www.rfc-editor.org/rfc/rfc9110#name-method) (Pitfall 4)
- [Go regexp / RE2 syntax](https://pkg.go.dev/regexp/syntax) — anchoring + QuoteMeta (Pitfall 3)
- [Cilium issue #31197 — FQDN DNS proxy truncation](https://github.com/cilium/cilium/issues/31197) (Pitfall 10)
- [Cilium issue #35525 / #43964 / #30581 — Envoy redirect resets](https://github.com/cilium/cilium/issues/35525) (Pitfall 17 / archive P1)
- [Debug Cilium toFQDN Network Policies (Medium)](https://mcvidanagama.medium.com/debug-cilium-tofqdn-network-policies-b5c4837e3fc4) (Pitfall 5)
- Prior research: `.planning/research/archive-2026-04-25/PITFALLS.md` (P2/P6/P8/P9/P10/P12 confirmed; P1/P4/P11 deferred to v1.3)
- cpg codebase: `pkg/policy/builder.go:109,171` (L4 dispatch), `pkg/policy/merge.go:172` (`mergePortRules`), `pkg/policy/dedup.go:49` (`normalizeRule`), `pkg/hubble/unhandled.go:130` (skip counters)

---
*Pitfalls research for: cpg v1.2 — L7 (HTTP + DNS) policy generation.*
*Researched: 2026-04-25*
