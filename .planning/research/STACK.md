# Stack Research — v1.2 L7 Policies

**Domain:** Go CLI tool — Extending CPG with L7 HTTP and DNS CiliumNetworkPolicy generation
**Researched:** 2026-04-25
**Confidence:** HIGH (types verified directly against vendored `cilium/cilium` v1.19.1 source)

## Scope

Stack additions/changes needed for v1.2 L7-only milestone:
1. L7 HTTP policy generation (method, path, headers) from Hubble L7 flows
2. L7 DNS policy generation (FQDN matchPattern / matchName) from Hubble DNS flows

Validated and **NOT re-researched**: existing core stack (Go 1.25.1, cilium/cilium v1.19.1, cobra, zap, client-go v0.35.0, grpc v1.79.2, sigs.k8s.io/yaml v1.6.0). The `cpg apply` command is **out of scope** for v1.2 (deferred to v1.3); apply-related research has been removed.

## Headline: Zero New Module Dependencies

All required types are already present via `github.com/cilium/cilium v1.19.1` (transitively in `go.mod`). No `go get` required. The work is integration, not stack expansion.

---

## Recommended Stack (Additions Only)

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `github.com/cilium/cilium/pkg/policy/api` | v1.19.1 | Provides `L7Rules`, `PortRuleHTTP`, `PortRulesDNS`, `FQDNSelector`, `FQDNSelectorSlice` — the typed structs that `pkg/policy/builder.go` already uses for L4 (`PortProtocol`). Stays consistent with current code path. | Authoritative CRD types maintained by Cilium upstream; same package already imported for L4. No alternative typed package exists. |
| `github.com/cilium/cilium/api/v1/flow` | v1.19.1 | Adds usage of `Flow.L7` (`*Layer7`), with `Layer7.GetHttp()`, `Layer7.GetDns()` accessors and the `HTTP`/`DNS` proto messages. | Same import already used in `pkg/hubble`. The L7 oneof is part of every flow Hubble emits — we just stop ignoring it. |

### Supporting Libraries

None. The L7 work uses only types from packages already in `go.mod` plus the Go stdlib (`regexp` for path/method sanitization if needed).

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `hubble observe --output jsonpb -t l7` | Generate L7 fixture data for `pkg/flowsource` replay tests | Capture against a real cluster with an existing L7 CNP; redact tokens before checking into `testdata/`. |
| `go doc github.com/cilium/cilium/pkg/policy/api L7Rules` | Verify type signatures during implementation | Authoritative — uses the actual vendored version. Prefer over web docs. |

## Installation

No new modules to add. `go.mod` already pins:

```
github.com/cilium/cilium v1.19.1
```

If a future bump is desired, verify on `pkg.go.dev/github.com/cilium/cilium/pkg/policy/api` first — see Version Compatibility section.

---

## L7 Type Inventory (Authoritative — verified via `go doc` against vendored v1.19.1)

### Policy CRD types — `github.com/cilium/cilium/pkg/policy/api`

| Type | Kind | Notes for cpg integration |
|------|------|---------------------------|
| `L7Rules` | struct | Container hung off `PortRule.Rules`. Fields: `HTTP PortRulesHTTP`, `DNS PortRulesDNS`, `Kafka []kafka.PortRule` (deprecated upstream — skip), `L7Proto string` + `L7 PortRulesL7` (generic key/value — skip). |
| `PortRulesHTTP` | `[]PortRuleHTTP` | Order-agnostic equality. Use as the value of `L7Rules.HTTP`. |
| `PortRuleHTTP` | struct | Fields: `Path string` (extended POSIX regex), `Method string` (regex), `Host string` (regex, IDN-validated), `Headers []HeaderMatch`. All optional — empty matches everything. |
| `HeaderMatch` | struct | Cilium-validated key+value matcher. Use only when Hubble flow exposes a meaningful header (rare — see Pitfalls). |
| `PortRulesDNS` | `[]PortRuleDNS` | Order-agnostic equality. Value of `L7Rules.DNS`. |
| `PortRuleDNS` | **`type PortRuleDNS FQDNSelector`** | **CORRECTION vs prior research:** this is a type alias of `FQDNSelector`, not a separate struct. Same fields: `MatchName`, `MatchPattern`. |
| `FQDNSelector` | struct | Fields: `MatchName string`, `MatchPattern string`. Trailing `.` auto-added. `**.` prefix matches multilevel subdomains. |
| `FQDNSelectorSlice` | `[]FQDNSelector` | Value of `EgressRule.ToFQDNs`. |
| `EgressRule.ToFQDNs` | field | DNS-based egress whitelisting. Per upstream docstring: **"ToFQDN cannot occur in the same policy as other To* rules"** — must live in its own EgressRule entry. |

### Hubble flow proto — `github.com/cilium/cilium/api/v1/flow`

| Type | Kind | Notes for cpg integration |
|------|------|---------------------------|
| `Flow.L7` | `*Layer7` | Optional; non-nil iff Hubble proxy observed an L7 record for this flow. |
| `Layer7` | struct | `Type L7FlowType` (REQUEST/RESPONSE/SAMPLE — **not** the protocol), `LatencyNs uint64`, oneof `Record` accessed via `GetHttp()`, `GetDns()`, `GetKafka()`. |
| `L7FlowType` | enum | `UNKNOWN=0`, `REQUEST=1`, `RESPONSE=2`, `SAMPLE=3`. The protocol discriminator is which `Get*` returns non-nil. |
| `HTTP` | struct | `Code uint32` (0 for requests), `Method`, `Url` (full path or URL), `Protocol` (HTTP/1.1, HTTP/2), `Headers []*HTTPHeader`. |
| `HTTPHeader` | struct | `Key`, `Value`. |
| `DNS` | struct | `Query string` (e.g. `"isovalent.com."`), `Ips []string`, `Ttl uint32`, `Cnames []string`, `Rcode uint32` (0=NOERROR, 3=NXDOMAIN, 5=REFUSED — Cilium policy denial), `Qtypes []string` (A, AAAA, CNAME...), `Rrtypes []string`, `ObservationSource string`. |

### Verification commands used

```bash
go doc -short github.com/cilium/cilium/pkg/policy/api L7Rules
go doc -short github.com/cilium/cilium/pkg/policy/api PortRuleHTTP
go doc -short github.com/cilium/cilium/pkg/policy/api PortRuleDNS    # confirms type alias to FQDNSelector
go doc -short github.com/cilium/cilium/pkg/policy/api FQDNSelector
go doc -short github.com/cilium/cilium/pkg/policy/api EgressRule     # confirms ToFQDN exclusivity note
go doc -short github.com/cilium/cilium/api/v1/flow Layer7
go doc -short github.com/cilium/cilium/api/v1/flow HTTP
go doc -short github.com/cilium/cilium/api/v1/flow DNS
```

---

## Question-by-Question Findings (Downstream Consumer)

### 1. Does Hubble's flow protobuf expose L7 fields in dropped-flow records? Schema across cilium/cilium v1.18+?

**YES, but conditionally.** `Flow.L7` (`*Layer7`) is part of the proto and has been stable across v1.16 → v1.19. Field availability:

- **HTTP:** `method`, `url` (path), `protocol`, `code`, `headers[]` — all populated by Cilium's Envoy access log. No request body, no client-cert SANs.
- **DNS:** `query`, `rcode`, `ips[]`, `cnames[]`, `qtypes[]`, `rrtypes[]`, `ttl`, `observation_source` — populated by the DNS proxy.

The conditional part: `Flow.L7` is non-nil **only if traffic transited the Cilium proxy**. That requires either (a) an existing L7 CNP that selects the workload, or (b) the legacy `policy.cilium.io/proxy-visibility` annotation. See Question 2.

For DROPPED verdict specifically: an L7-policy violation produces `Verdict_DROPPED` with `L7.HTTP` populated and a meaningful HTTP method/path — this is the primary signal cpg will turn into rules. For DNS, Cilium denies a DNS query by returning `REFUSED` (rcode=5) in the response; the flow may still show `Verdict_FORWARDED` (the proxy answered) — see Pitfalls.

Schema is stable in v1.19.x. v1.20 (in development per docs.cilium.io/en/latest) does not break these fields.

### 2. enable-l7-proxy + L7 visibility — does cpg need to detect?

**Two-tier requirement on the cluster side:**

1. **Cilium agent config:** `enable-l7-proxy: true` (Helm: `l7Proxy=true`, default true on most installs but explicitly disabled in some hardened setups).
2. **Per-workload visibility trigger:** EITHER an existing L7 CNP that already selects the pod, OR the legacy `policy.cilium.io/proxy-visibility="<Egress/53/UDP/DNS>,<Egress/80/TCP/HTTP>"` annotation. Per upstream docs (and confirmed via web search 2026-04-25), the annotation is "historically supported but no longer recommended" — Cilium pushes users toward L7 policies as the visibility trigger.

**Implication for cpg — chicken-and-egg.** This is the dominant operational pitfall and the v1.2 README must surface it:

> To generate L7 policies with cpg, the workload must already produce L7 flows. That requires either a starter L7 CNP (any rule, even `{}`) or the proxy-visibility annotation. cpg cannot bootstrap L7 visibility from L4-only flows.

**Recommended detection in cpg:** when running with `--l7` (proposed flag), warn if **no flow with non-nil `L7` field is seen within N seconds / N flows**. Suggested log:

> "No L7 flows observed. Verify enable-l7-proxy is true and that workloads have L7 policies or `policy.cilium.io/proxy-visibility` annotations. See `cpg --help l7` for setup."

Stronger detection (querying agent config) requires an additional API call to the cilium-agent — out of scope; rely on flow-presence heuristic.

### 3. Cilium API package — exact types, required vs optional, version compat

Already enumerated in the L7 Type Inventory table above. Required-vs-optional summary:

- `PortRuleHTTP`: **all fields optional**. Empty struct = "any HTTP". Missing `Path` and `Method` together would generate an unhelpful policy — cpg should require at least one of method or path before emitting a rule.
- `PortRuleDNS` / `FQDNSelector`: **OneOf** (`MatchName` XOR `MatchPattern`) per kubebuilder annotations. cpg must pick one; recommendation: literal queries → `MatchName`, wildcards → `MatchPattern`.
- `EgressRule.ToFQDNs`: cannot coexist with other `To*` selectors in the same EgressRule. This forces `pkg/policy` merge logic to **emit a separate EgressRule** for FQDN traffic.

Version-compat: types are stable v1.16 → v1.19. v1.19 added the `L7Proto`/`L7` generic key-value mechanism (out of scope for cpg).

### 4. ToFQDNs vs L7 DNS rules — when use which?

These are **complementary, not alternatives**:

| Construct | What it controls | Where in CRD | When cpg uses it |
|-----------|-----------------|--------------|------------------|
| `ToFQDNs` (egress selector) | "This pod may send L3/L4 traffic to IPs that resolved from these FQDNs." Cilium agent does the DNS-to-IP mapping internally. | `EgressRule.ToFQDNs` | When the DENIED flow is **L4 traffic to an external IP** that previously resolved from a domain (e.g. pod tries TCP/443 to `1.2.3.4` and we know that IP came from `api.example.com` via a prior allowed DNS lookup). |
| `PortRuleDNS` inside `L7Rules.DNS` | "This pod may make **DNS queries** matching these names via the DNS proxy on port 53." Restricts which names the resolver will answer. | `EgressRule.ToPorts[].Rules.DNS` (typically on port 53/UDP+TCP to kube-dns) | When the DENIED flow is **a DNS query** itself (port 53, L7.DNS populated, rcode=REFUSED or DROPPED verdict). |

**Hubble does not give us enough to populate `ToFQDNs` directly from a single dropped flow.** A dropped TCP flow shows the destination IP, not the original DNS query. To map IPs → FQDNs we would need DNS-response correlation across flows (track allowed DNS responses, build an IP→name cache, look up dropped IPs there). This correlation is non-trivial.

**Recommendation for v1.2:**
- For **DNS query denials** (port 53 with `L7.DNS` populated): emit `PortRuleDNS` rules under `L7Rules.DNS` on the DNS port. This is unambiguous and directly populated from `flow.L7.GetDns().Query`.
- For **L4 denials to external IPs**: keep current v1.0 behavior (CIDR rule). Do **not** attempt FQDN correlation in v1.2 — it's a v1.3+ feature requiring a DNS cache. Document this gap in PITFALLS.md.

### 5. New go module dependency required?

**No.** All types live in `github.com/cilium/cilium v1.19.1` (already in go.mod, line 6). Specifically:

- `pkg/policy/api` — already imported by `pkg/policy/builder.go` for L4
- `api/v1/flow` — already imported by `pkg/hubble/client.go` and `pkg/hubble/aggregator.go`

`client-go v0.35.0` and `apimachinery v0.35.0` are unaffected (no apply work in v1.2). `sigs.k8s.io/yaml v1.6.0` continues to handle marshaling.

### 6. L7 protocols cpg should NOT handle

Confirmed scope: **HTTP + DNS only**. Skip:

| Protocol | Why skip |
|----------|----------|
| **Kafka** (`PortRulesKafka`) | Marked deprecated in upstream `L7Rules` struct: `// Deprecated: This beta feature is deprecated and will be removed in a future release.` Hubble proto's `Layer7_Kafka` oneof remains but no point generating policy for a deprecated rule type. |
| **gRPC** | No first-class type. gRPC over HTTP/2 surfaces in Hubble as HTTP flows with `:method=POST` and a path like `/pkg.Service/Method` — already covered by HTTP rules. There is no separate `PortRulesGRPC` in v1.19.1. |
| **Generic L7** (`L7Rules.L7Proto` + `L7Rules.L7`) | Requires a registered Envoy L7 parser by name and key/value rules. Niche; not exposed in standard Hubble flows in a structured way. |
| **TLS SNI / Authentication / mTLS** | `EgressRule.Authentication` is a separate concern, not a generation target. Hubble flow does not expose SNI in a stable structured field. |

Reasonable scope: HTTP + DNS only. This matches the v1.2 PROJECT.md scope statement.

---

## Integration Points with Existing Codebase

### `pkg/policy/builder.go` (~13.7K, primary L7 work)

- Add an `extractL7Rules(f *flowpb.Flow) *api.L7Rules` helper.
- Inside the existing `PortRule` construction, attach `Rules: extractL7Rules(f)` when non-nil.
- HTTP path: `flow.L7.GetHttp()` → `&api.L7Rules{HTTP: api.PortRulesHTTP{{Method: m, Path: p}}}`. Sanitize `Path` (Cilium expects regex; cpg should regex-quote literal paths).
- DNS path: `flow.L7.GetDns()` → `&api.L7Rules{DNS: api.PortRulesDNS{{MatchName: q}}}` (literal) or `MatchPattern` if cpg later generalizes (e.g. version-suffix collapse).

### `pkg/policy/merge.go` (~6.7K)

- Extend `mergePortRules()` to dedup L7 rules when keys (port+proto) match. HTTP dedup key: `Method|Path|Host|sortedHeaders`. DNS dedup key: `MatchName|MatchPattern`.
- New rule: when an EgressRule has `ToFQDNs`, it must be split out into its own EgressRule (constraint surfaced in Question 4). This affects merge ordering — keep existing L4 EgressRule, append a sibling for FQDNs. (FQDNs aren't a v1.2 emission target per Question 4 recommendation, but the merge code should be future-proofed against accidentally combining them.)

### `pkg/hubble/aggregator.go` (~7.5K)

- No structural change. The L7 data is on the `*flowpb.Flow` already flowing through. Aggregation key currently: identity+port+protocol+direction. **Decision needed (downstream of this research):** does the L7 dimension belong in the aggregation key? Recommendation: yes for HTTP method/path coarseness (otherwise every URL becomes its own rule) — but that's a builder-design question, not a stack one.

### `pkg/hubble/client.go` (~3.7K)

- Current filter is `Verdict_DROPPED` only. For DNS proxy denials returning REFUSED with `Verdict_FORWARDED`, cpg may miss the signal. **Two options** (decision deferred to roadmap):
  1. Keep DROPPED-only — simple, may miss DNS REFUSED.
  2. Add a second filter for `EventType == L7` regardless of verdict — pulls more data; needs filtering downstream.
- Recommendation for v1.2: ship with DROPPED-only and document the limitation. Add `--include-l7-forwarded` flag if user demand emerges.

### `pkg/flowsource` (replay)

- No code changes required: replay reads jsonpb flows and L7 fields are already part of the proto. **Test fixture work:** add `testdata/flows-l7-http.jsonpb` and `flows-l7-dns.jsonpb` captures.

### `pkg/evidence`

- No schema bump strictly required (evidence stores the originating flow). Consider whether the per-rule evidence needs an `l7_summary` denormalized field for `cpg explain` rendering. Schema v1 is pinned ("Reader rejects unknown versions" per PROJECT.md) — adding a field would require a v2 schema bump. Decision: keep schema v1, render L7 in `explain` by re-reading the flow.

---

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| `cilium/cilium/pkg/policy/api` typed structs | Build CNP YAML via `unstructured.Unstructured` + manual maps | Never for v1.2 — typed structs catch field-name typos at compile time. The `unstructured` path was relevant only for `cpg apply` (now deferred). |
| `flow.L7.GetHttp()` / `GetDns()` accessors | Type-switch on `Layer7.Record` (the underlying oneof) | Either works; accessors are idiomatic protobuf-go and return nil-safe. Use accessors. |
| Skip Kafka / gRPC / generic L7 | Implement Kafka rules | Only if a user explicitly requests Kafka and accepts the upstream deprecation risk. Not v1.2. |
| DNS `MatchName` for literal queries | Always `MatchPattern` (e.g. exact-match pattern) | `MatchName` is canonical for literals (per CRD docs). Use `MatchPattern` only when cpg generalizes (e.g. wildcard inference) — not v1.2. |
| Emit `L7Rules.DNS` for DNS denials | Emit `ToFQDNs` for DNS denials | `ToFQDNs` controls which IPs the pod may reach **after** DNS resolution; not the right primitive for "this DNS query was denied." Use `L7Rules.DNS`. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `api.PortRulesKafka` / Kafka-specific rules | Upstream comment: "Deprecated: This beta feature is deprecated and will be removed in a future release." | Skip Kafka entirely. |
| `api.L7Rules.L7Proto` + `L7Rules.L7` (generic) | Requires a registered Envoy parser by name; niche; no Hubble field maps cleanly. | Skip. |
| `policy.cilium.io/proxy-visibility` annotations as a cpg-managed feature | Upstream considers this legacy. cpg should not write these annotations on user workloads. | Document the annotation in user-facing setup docs as a fallback. |
| `FQDN` correlation in v1.2 | Requires DNS-response → IP cache, multi-flow correlation, TTL handling. Significant complexity for marginal v1.2 value. | Defer to v1.3. Emit CIDR rules for L4-to-external denials. |
| Importing `cilium/cilium/pkg/proxy/accesslog` to "enrich" L7 data | Internal cilium-agent package, not designed for external consumers. Pulls hive DI framework. | The `flowpb.HTTP` / `flowpb.DNS` types already mirror accesslog fields — use them. |
| HTTP `Headers` rule emission from observed headers | Hubble exposes headers, but emitting `HeaderMatch` rules from observed `Authorization`/`Cookie` values would leak credentials into policy YAML. | For v1.2: skip headers. Method+Path only. Headers can come later behind an explicit `--include-headers` flag with allowlist. |

## Stack Patterns by Variant

**If user has existing L7 CNPs in cluster:**
- Hubble naturally produces L7 flows for those workloads.
- cpg dedup must compare L7 rule sets against the live cluster (extending `pkg/dedup` to walk into `Rules.HTTP[]` and `Rules.DNS[]`).

**If user has zero L7 CNPs and no proxy-visibility annotations:**
- `Flow.L7` will always be nil. cpg's `--l7` output will be empty.
- Surface a clear warning (see Question 2 detection heuristic).
- README should provide a "starter L7 visibility CNP" snippet users can apply first.

**If user is in a hardened env with `enable-l7-proxy: false`:**
- Same outcome as above — no L7 flows. Warning suffices; no other code path needed.

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `cilium/cilium@v1.19.1 / pkg/policy/api` | Cilium agent v1.16+ on cluster | CNP CRD schema for L7 rules has been stable since v1.13. Generated YAML applies cleanly to any v1.16+ cluster. |
| `cilium/cilium@v1.19.1 / api/v1/flow` | Hubble Relay v1.16+ | `Layer7` oneof (HTTP/DNS/Kafka) stable since v1.13. v1.19 added `L7Proto`/`L7` generic — opt-in only. |
| `PortRuleDNS` (alias for `FQDNSelector`) | All v1.16+ | Type alias predates v1.16. Be aware: DeepEqual treats them identically. |

**No version bumps recommended for v1.2.** A future bump to v1.20 (when released) should be re-verified — the v1.20-dev docs are public but the proto/api packages haven't been audited against it here.

---

## Honest Gaps and Caveats

1. **HTTP request bodies, response bodies, gRPC trailers, JWT claims:** Hubble proto does not expose any of these. cpg cannot derive policy from them.
2. **HTTP headers in observed flows:** exposed in `flowpb.HTTPHeader` but emitting them as `HeaderMatch` rules is a security smell (leaks tokens). Skip in v1.2.
3. **TLS SNI:** not in the standard `flowpb.Flow` proto in a stable structured field. Cannot generate SNI-based rules.
4. **DNS REFUSED via FORWARDED verdict:** if Cilium denies via DNS proxy with REFUSED rcode, the flow's verdict may be FORWARDED (not DROPPED). v1.2 with a DROPPED-only filter will miss this signal. Document; consider `--include-l7-forwarded` in v1.3.
5. **FQDN correlation:** Cannot derive `ToFQDNs` from L4 dropped flows in v1.2 (requires DNS cache). Documented limitation.
6. **L7 visibility chicken-and-egg:** cpg cannot generate L7 rules for a workload that doesn't already have either an L7 CNP or the legacy proxy-visibility annotation. README must call this out.
7. **PortRuleDNS is a type alias, not a separate struct.** Prior research (archived) treated it as struct with `MatchName`/`MatchPattern` fields; that is correct *content-wise* but it's literally `type PortRuleDNS FQDNSelector`. Casting / DeepEqual semantics matter for dedup code.

---

## Sources

- Vendored source via `go doc` on `github.com/cilium/cilium@v1.19.1` (HIGH confidence — authoritative against the actually-pinned version):
  - `pkg/policy/api`: `L7Rules`, `PortRuleHTTP`, `PortRulesHTTP`, `PortRuleDNS` (type alias), `PortRulesDNS`, `FQDNSelector`, `EgressRule.ToFQDNs` (with exclusivity note in upstream docstring).
  - `api/v1/flow`: `Layer7`, `L7FlowType`, `HTTP`, `HTTPHeader`, `DNS`.
- [Cilium L7 Visibility — stable docs](https://docs.cilium.io/en/stable/observability/visibility/) — confirms L7 CNPs trigger visibility (HIGH).
- [Cilium L7 Visibility — v1.20-dev docs](https://docs.cilium.io/en/latest/observability/visibility/) — schema unchanged in current main (MEDIUM, dev branch).
- [OneUptime — Cilium L7 Network Policies (2026-03-13)](https://oneuptime.com/blog/post/2026-03-13-cilium-l7-network-policies/view) — community confirmation of method/path/header model (MEDIUM).
- WebSearch 2026-04-25 — confirmed `policy.cilium.io/proxy-visibility` is "historically supported but no longer recommended" (MEDIUM, multi-source).
- Prior cpg research at `.planning/research/archive-2026-04-25/STACK.md` — superseded; this document re-verified types directly. Notable correction: `PortRuleDNS` is a type alias of `FQDNSelector`, not a parallel struct.

---
*Stack research for: CPG v1.2 — L7 Policies (HTTP + DNS) only*
*Researched: 2026-04-25*
