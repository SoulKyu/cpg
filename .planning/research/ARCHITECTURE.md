# Architecture Research — v1.2 L7 Policies (HTTP + DNS)

**Domain:** Extend existing CPG (Cilium Policy Generator) to emit L7 HTTP and DNS rules
**Researched:** 2026-04-25
**Confidence:** HIGH (codebase analysis) / MEDIUM (Cilium API surface — verified in archived research)
**Scope discipline:** L7-only. `cpg apply` deferred to v1.3 (excluded here).

## Guiding Principle: Extend, Don't Restructure

The v1.1 codebase is well-factored: the streaming pipeline (`hubble.client → aggregator → BuildPolicy → Writer/EvidenceWriter`) is layer-agnostic. L7 enrichment lives where flow→rule conversion happens (`pkg/policy/builder.go`). Pipeline, aggregation, output, and dedup remain structurally unchanged — they only learn to carry/compare richer `PortRule` payloads.

The previous archived research (2026-03-09) bundled L7 + apply. Re-evaluating for L7-only: the build order shrinks from 13 steps to 9, FQDN-related work splits cleanly from HTTP, and the `Apply` phase disappears. No architectural decision from the archive is invalidated.

---

## System Overview (Where L7 Plugs In)

```
                       Hubble gRPC / jsonpb replay
                                    │
                                    ▼
                ┌───────────────────────────────────┐
                │ pkg/hubble/client (UNCHANGED)     │
                │ StreamDroppedFlows                │
                │   → flow filter already requests  │
                │     full Flow proto, L7 already   │
                │     present when emitted          │
                └─────────────┬─────────────────────┘
                              │ *flowpb.Flow
                              ▼
                ┌───────────────────────────────────┐
                │ pkg/hubble/aggregator (UNCHANGED) │
                │ AggKey{Namespace, Workload}       │
                │   → L7 flows hit the same bucket  │
                │     as their L4 counterparts      │
                └─────────────┬─────────────────────┘
                              │ flush → []*Flow
                              ▼
                ┌───────────────────────────────────┐
                │ pkg/policy/BuildPolicy  (MODIFY)  │
                │  ├── extractProto (UNCHANGED)     │
                │  ├── extractL7 (NEW, l7.go)       │
                │  ├── peerRules (EXTENDED)         │
                │  │     +httpByPort                │
                │  │     +dnsByPort                 │
                │  └── fqdnEgress (NEW path)        │
                │      isolated EgressRule(s)       │
                └─────────────┬─────────────────────┘
                              │ PolicyEvent
                              ▼
                  ┌───────────┴───────────┐
                  ▼                       ▼
        ┌──────────────────┐   ┌────────────────────┐
        │ output.Writer    │   │ evidence.Writer    │
        │ (modify dedup    │   │ (schema bump v1→v2 │
        │  comparator;     │   │  additive L7 fields)│
        │  YAML marshal    │   └────────────────────┘
        │  works as-is)    │
        └──────────────────┘
                  │
                  ▼
        ┌──────────────────┐
        │ pkg/policy/dedup │  PoliciesEquivalent
        │   normalizeRule  │  (MODIFY: sort L7 lists)
        ├──────────────────┤
        │ pkg/policy/merge │  mergePortRules
        │   (MODIFY: merge │  (extend to merge Rules)
        │   L7 + match     │
        │   FQDN egress)   │
        ├──────────────────┤
        │ pkg/k8s/cluster  │  cluster dedup uses
        │   (UNCHANGED —   │  PoliciesEquivalent →
        │   gets L7 dedup  │  inherits fix
        │   for free)      │
        └──────────────────┘
```

Legend: UNCHANGED, MODIFY (in-place edits), NEW (new file/path).

---

## Q1. Where L7 Rule Extraction Lives

**Decision:** New file `pkg/policy/l7.go` — same package, dedicated translation unit.

**Rationale:**
- `BuildPolicy` signature stays untouched (still `(namespace, workload, flows, tracker, opts) → (*CNP, []RuleAttribution)`).
- `builder.go` is already 494 lines; HTTP path extraction, header selection, DNS query normalization, and FQDN egress assembly add ~200 lines. A separate file improves readability without crossing a package boundary (avoids exporting internals like `flowProto`, `peerRules`).
- Same package = unexported helpers stay private; `peerRules` can grow new fields without making them public.

**File contents (`pkg/policy/l7.go`):**
- `extractHTTP(f *flowpb.Flow) *api.PortRuleHTTP` — pure function (nil if no HTTP).
- `extractDNSQuery(f *flowpb.Flow) string` — request-only, trimmed.
- `httpRuleKey(r api.PortRuleHTTP) string` — dedup key (`method|path|headers-hash`).
- `extractPathFromURL(raw string) string` — URL → regex-escaped path.
- `buildFQDNEgressRules(flows []*flowpb.Flow, policyNS string, opts) ([]api.EgressRule, []RuleAttribution)` — separate codepath because `ToFQDNs` is mutually exclusive with `ToEndpoints`/`ToCIDR` in a single rule.

**Touched in `builder.go`:**
- `peerRules` struct gains two maps (see Q2).
- `groupFlows` calls `extractHTTP` / `extractDNSQuery` after `extractProto` succeeds and stores results on the bucket.
- `ingressRulesFrom` / `egressRulesFrom` learn to attach `*api.L7Rules` to `PortRule` when the bucket carries L7 data for that port.
- `buildEgressRules` post-processes DNS-only flows to produce `buildFQDNEgressRules` output appended to the regular egress slice.

---

## Q2. Aggregation Key — Stays the Same

**Decision:** Do NOT extend `AggKey`. L7 lives one level deeper, inside the bucket.

**Why:** The aggregator's job is "which CNP does this flow belong to?" — answered by `(namespace, workload)`. L7 is a property of a *rule* inside that CNP, not of the policy itself. Adding `port` or `protocol` to `AggKey` would shatter buckets and produce one CNP per port — the opposite of what we want.

**Where L7 keying lives instead:** inside `peerRules` (per-bucket), keyed by port+proto.

```go
// peerRules (extended). All new fields zero-valued for non-L7 flows → backward compat.
type peerRules struct {
    ports       []api.PortProtocol
    icmpFields  []api.ICMPField
    seen        map[string]struct{}              // port/proto dedup (existing)
    attrib      map[string]*RuleAttribution      // existing

    // NEW — L7 enrichment, indexed by port/proto string ("80/TCP", "53/UDP")
    httpRules   map[string][]api.PortRuleHTTP    // port/proto → HTTP rules
    httpSeen    map[string]map[string]struct{}   // port/proto → method+path dedup
    dnsRules    map[string][]api.PortRuleDNS     // port/proto → DNS visibility rules
    dnsSeen     map[string]map[string]struct{}   // port/proto → matchPattern dedup
}
```

When `addFlow` runs, it now also receives optional `*api.PortRuleHTTP` / DNS query. If present and the port/proto matches, it appends to `httpRules[key]` (dedup via `httpSeen[key]`).

**FQDN egress rules** are separate: they are not enrichments of an existing port-rule, they are *new EgressRule* entries (`ToFQDNs` is incompatible with `ToEndpoints`/`ToCIDR`). They live in a parallel slice on `peerBuckets` (or as a return value from `buildFQDNEgressRules`) and are appended after the loop in `buildEgressRules`.

---

## Q3. Deduplication of L7 Rules

| Layer | File | Status | Required change |
|-------|------|--------|-----------------|
| File dedup (writer) | `pkg/output/writer.go` | Works as-is | YAML byte comparison still correct. Marshaling is deterministic when builder produces sorted L7 lists. |
| Cross-flush / merge dedup | `pkg/policy/merge.go` `mergePortRules` | **BROKEN today** | Currently flattens `PortRules` into `result[0].Ports` and **drops `Rules` field entirely**. Must merge `Rules.HTTP` and `Rules.DNS` per port/proto match. |
| Semantic equivalence | `pkg/policy/dedup.go` `PoliciesEquivalent` / `normalizeRule` | Partially works | Compares via YAML marshal of full Spec, so L7 IS captured — but order is non-deterministic. Must sort `Rules.HTTP` (by method+path) and `Rules.DNS` (by matchPattern) inside each `PortRule`. |
| Cluster dedup | `pkg/k8s/cluster_dedup.go` | Works once `PoliciesEquivalent` is fixed | Uses `PoliciesEquivalent` directly → inherits the fix. |

**Critical detail in `mergePortRules`:** today's implementation collapses everything into `result[0].Ports` with no `Rules` field. This means when an L7-bearing policy is merged with an existing one, **L7 rules are silently lost**. This MUST be the first dedup change shipped — without it, every L7 policy that goes through file-merge or cluster-dedup loses its L7 layer.

**Refactor target for `mergePortRules`:**
- Group by `port/proto` key (already the dedup key).
- For each group, merge `Rules.HTTP` (dedup by `method+path+sorted-headers`) and `Rules.DNS` (dedup by `matchPattern`/`matchName`).
- Mixing HTTP and DNS rules on the same port/proto is structurally illegal in Cilium (`L7Rules` is a oneof-style union); merge must refuse and log (anti-pattern enforcement).

**Refactor target for `normalizeRule`:**
- After `sortPorts`, also `sortL7HTTP(pr.Rules)` and `sortL7DNS(pr.Rules)` per `PortRule`.
- Update `ingressRuleKey`/`egressRuleKey` only if FQDN selectors are introduced (see Q5).

---

## Q4. Evidence Schema — Bump to v2 (Backward-Incompatible by Design)

**Current state (`pkg/evidence/schema.go`):**
- `SchemaVersion = 1` and the reader **rejects unknown versions** (per archived decision).
- `RuleEvidence.Key` derives from `RuleKey{Direction, Peer, Port, Protocol}` → no L7 field.
- Two L7 rules on the same `(direction, peer, port, proto)` would collide on the same key, overwriting each other's flow counts and samples.

**Decision:** **Bump `SchemaVersion` to 2.**

**Why a bump (not additive):**
1. The reader in v1.1 explicitly refuses unknown versions — this means *any* old reader will refuse a new file regardless of additivity. A bump is required by the existing forward-compat contract.
2. `RuleKey.String()` must change to incorporate L7 selectors, otherwise rule attribution collides. This is a behavior change in evidence semantics, not just a structure addition.
3. JSON files written by v1.1 remain readable by v1.2 (we keep a `v1` decode path) but newly written files use v2.

**Schema v2 additions:**
```go
type RuleEvidence struct {
    Key                  string       `json:"key"`
    Direction            string       `json:"direction"`
    Peer                 PeerRef      `json:"peer"`
    Port                 string       `json:"port"`
    Protocol             string       `json:"protocol"`

    // NEW (omitempty → unset means L4-only rule)
    L7                   *L7Ref       `json:"l7,omitempty"`

    FlowCount            int64        `json:"flow_count"`
    FirstSeen            time.Time    `json:"first_seen"`
    LastSeen             time.Time    `json:"last_seen"`
    ContributingSessions []string     `json:"contributing_sessions"`
    Samples              []FlowSample `json:"samples"`
}

type L7Ref struct {
    Type        string `json:"type"`                  // "http" | "dns"
    HTTPMethod  string `json:"http_method,omitempty"`
    HTTPPath    string `json:"http_path,omitempty"`
    DNSPattern  string `json:"dns_pattern,omitempty"` // matchPattern OR matchName
}
```

`FlowSample` may also gain optional `L7Type` / `L7Summary` for `cpg explain` rendering — additive within the v2 schema, no further bump.

**Reader strategy:** keep a v1-decode fallback for ~one minor cycle (read-only), so users with existing `~/.cache/cpg/evidence` v1 files don't lose history. New writes are v2. Deletion of v1 path can land in v1.3.

---

## Q5. Two-Step Workflow vs Single Run

**Decision: same run produces L4 + L7 when records contain both layers. No mode flag.**

**Reasoning:**
- A Hubble flow either has `f.L7 != nil` or it doesn't. The builder branches on the field; this is data-driven, not mode-driven.
- The "two-step workflow" described in `PROJECT.md` is operational, not architectural: the *user* deploys L4 first because L7 visibility requires Cilium proxy redirect (which itself requires a CNP with L7 rules in place). That's a deployment ordering, not a code path. Once L7 visibility is enabled in the cluster, dropped/redirected flows naturally arrive with `L7` populated and the same `cpg generate` run handles both.
- Inverse case: if a user runs with no L7 visibility, no flows carry `L7`, output is L4-only — same code path, no failure.
- An optional `--l7=off` flag (suppresses extraction even when present) is a future affordance, not a v1.2 requirement. Recommend deferring.

**Natural boundary inside the builder:**
- L4 rule production = always.
- L7 rule attachment = conditional on `extractHTTP` / `extractDNSQuery` returning non-nil.
- FQDN egress rule = conditional on at least one DNS request flow with non-empty query (egress only).

**Implication for `cpg replay`:** unchanged. Replay over a jsonpb capture that contains L7 fields will produce L7 policies; over an L4-only capture, it produces L4 policies. Same binary, same flags.

---

## Q6. Suggested Build Order (L7-Only Scope)

The order minimizes broken intermediate states by fixing the silent-data-loss bug (merge) early and shipping HTTP before DNS (DNS adds the FQDN-egress-rule complication).

| # | Step | Package | Purpose | Verifiable by |
|---|------|---------|---------|---------------|
| 1 | Fix `mergePortRules` to preserve `Rules` | `pkg/policy/merge.go` | Stop silently dropping L7 fields in cross-flush merge. Required *before* L7 generation, otherwise generation tests pass but writer/cluster dedup break. | New unit tests with hand-built L7 PortRules merging cleanly. No L7 builder code yet — only proves the merge layer is L7-safe. |
| 2 | Extend `normalizeRule` to sort L7 lists | `pkg/policy/dedup.go` | Make `PoliciesEquivalent` deterministic for L7. | Round-trip equivalence tests with shuffled L7 rule order. |
| 3 | Bump evidence schema to v2 + L7Ref | `pkg/evidence/schema.go` | Reserve the on-disk shape before writers populate it. Keep v1 read path. | Schema test: read v1 fixture OK, write v2, refuse v3. |
| 4 | Add `pkg/policy/l7.go` (HTTP only) + extend `peerRules` | `pkg/policy/{l7.go,builder.go}` | Pure-extraction layer. Wire HTTP into `groupFlows` and rule emission. | Unit tests with synthetic flows carrying `f.L7.Http`. |
| 5 | Wire L7 attribution into `RuleAttribution` | `pkg/policy/{attribution.go,builder.go}` | Extend `RuleKey` with optional `L7` discriminator so attribution doesn't collide. | Builder attribution tests with two HTTP rules on same port. |
| 6 | Evidence writer: emit `RuleEvidence.L7` | `pkg/evidence/` (writer files) | Persist the new field for HTTP rules. | End-to-end: generate → read evidence → see L7 entries. |
| 7 | Add DNS extraction + FQDN egress builder | `pkg/policy/l7.go` (DNS section), `builder.go` (egress post-processing) | Add DNS visibility rule on port 53 *and* paired FQDN egress rule. | Builder tests with DNS request flows producing two egress rules per FQDN. |
| 8 | Extend `mergePortRules` + matching for FQDN egress rules | `pkg/policy/merge.go` | `ToFQDNs` rules need their own matcher (`matchFQDNs`) and L7 DNS list merge on port-53 rule. | Merge tests with FQDN egress rules. |
| 9 | `cpg explain` — render L7 in text/json/yaml output | `cmd/cpg/explain_render.go` | Show HTTP method/path or DNS pattern in evidence rendering. | Snapshot tests on evidence with L7 records. |

**Steps 1-3 are infrastructure prep, no behavior change.** Anyone pulling the branch at step 3 still produces v1.1-compatible L4 output. Steps 4-7 ship the actual L7 generation. Steps 8-9 are the FQDN/UX layer.

**Hubble client modification:** *not in this list*. The current filter already streams full `Flow` records including the `L7` field — verified via flow proto. No changes needed unless the verdict filter excludes `REDIRECTED` (it does today; depending on cluster L7-policy config, L7 visibility flows may arrive as `REDIRECTED` rather than `DROPPED`). Validate during step 4 testing; add filter expansion only if needed and document the trade-off.

---

## Q7. Renderer Impact (YAML Marshaling)

**Verdict: works as-is, no renderer changes.**

`pkg/output/writer.go` uses `sigs.k8s.io/yaml` to marshal `*ciliumv2.CiliumNetworkPolicy`. That library marshals via JSON tags first, then converts to YAML — so anything Cilium's `api` types annotate with `json:` tags renders correctly. The `PortRule.Rules *L7Rules` field, plus nested `[]api.PortRuleHTTP` and `[]api.PortRuleDNS`, all carry `json:` tags in upstream Cilium and round-trip through `sigs.k8s.io/yaml` cleanly (verified in archived STACK research).

**`api.FQDNSelector` and `api.EgressRule.ToFQDNs`** likewise marshal cleanly.

**Risk to watch (NOT a blocker):** `annotateRules` in `pkg/output/annotate.go` adds human-readable comments next to rules — it works on raw YAML byte ranges, not the typed object. New L7 rule kinds need annotator awareness if we want them annotated; otherwise they render with no comment (acceptable for v1.2). Track as a polish item, not a blocker.

**`stripComments` in writer.go** is YAML-line-based and handles arbitrary new fields without code change.

---

## Q8. `cpg explain` Integration

**Decision: minimal change — add `--http-method`, `--http-path`, `--dns-pattern` filters; out-of-scope for substring/regex matching of paths.**

**Rationale:**
- The skeleton (`explainFilter`, `match`, evidence reader) is direction/port/peer/CIDR/since aware. Adding three optional string fields is ~30 lines: parse flag → if set, exclude rules whose `r.L7` doesn't match.
- Path matching: exact-match only in v1.2 (the path stored in evidence is already the regex-escaped pattern; substring matching against an escaped pattern is confusing). Document "exact-match" in flag help. Defer regex/glob to v1.3.
- DNS pattern: exact match against `RuleEvidence.L7.DNSPattern`.
- The renderer (`explain_render.go`) gains a new line per rule when `r.L7 != nil`: e.g. `  HTTP GET /api/health` or `  DNS *.example.com`. Same in JSON/YAML — `L7Ref` is just a nested object.

**Out of scope for v1.2:**
- Regex/glob matching on path (would need careful escaping and clear UX).
- Filter combinators (`--http-method GET OR POST`) — single-value flags suffice; users compose via shell loops.

---

## Component-Change Ledger

| Package / File | Status | Why |
|----------------|--------|-----|
| `pkg/hubble/client.go` | UNCHANGED | Streams full `Flow` proto already. |
| `pkg/hubble/aggregator.go` | UNCHANGED | `AggKey` semantics unchanged. |
| `pkg/hubble/pipeline.go` | UNCHANGED | Layer-agnostic fan-out. |
| `pkg/flowsource/` | UNCHANGED | Replay path is shape-agnostic. |
| `pkg/policy/builder.go` | MODIFY | Extend `peerRules`; wire L7 extraction into `groupFlows`; attach `L7Rules` in `*RulesFrom` helpers. |
| `pkg/policy/l7.go` | NEW | All L7 extraction + FQDN egress assembly. |
| `pkg/policy/attribution.go` | MODIFY | `RuleKey` gains optional L7 discriminator so attribution doesn't collide. |
| `pkg/policy/merge.go` | MODIFY | Preserve `Rules` in `mergePortRules` (currently dropped — silent bug under L7); add `matchFQDNs` for FQDN egress matching. |
| `pkg/policy/dedup.go` | MODIFY | Sort L7 lists in `normalizeRule`; extend `egressRuleKey` if FQDN selectors are added. |
| `pkg/output/writer.go` | UNCHANGED | YAML marshaling carries L7 fields automatically. |
| `pkg/output/annotate.go` | OPTIONAL POLISH | Annotate L7 rules with comments (non-blocking). |
| `pkg/evidence/schema.go` | MODIFY | Bump `SchemaVersion = 2`; add `L7Ref`. |
| `pkg/evidence/writer.go` (or equivalent) | MODIFY | Populate `RuleEvidence.L7`. |
| `pkg/evidence/reader.go` | MODIFY | Accept v1 (read-only legacy) and v2; reject v3+. |
| `pkg/k8s/cluster_dedup.go` | UNCHANGED | Inherits L7 dedup correctness via `PoliciesEquivalent`. |
| `pkg/diff/` | UNCHANGED | Operates on YAML bytes — captures L7 changes for free. |
| `cmd/cpg/generate.go` | UNCHANGED | No new flags required for v1.2. |
| `cmd/cpg/replay.go` | UNCHANGED | Same. |
| `cmd/cpg/explain.go` | MODIFY | Three new filter flags + render path for `RuleEvidence.L7`. |
| `cmd/cpg/explain_filter.go` | MODIFY | Filter struct gains 3 fields and `match` clauses. |
| `cmd/cpg/explain_render.go` | MODIFY | Add an L7 line to per-rule output. |

**Surface area:** 9 packages touched, 2 new files (`pkg/policy/l7.go`, plus tests), 1 schema bump. No package added; no public API broken (`BuildPolicy` signature preserved).

---

## Anti-Patterns to Avoid (carried from archived research, still apply)

1. **Two `PortRule` entries for the same port (L4 + L7)** — Cilium evaluates each independently; the L4-only entry would whitelist everything, defeating the L7 restriction. Always attach L7 to the same `PortRule` as the L4 port.
2. **FQDN rule without DNS-allow companion** — `ToFQDNs` silently fails without port-53 allow with `Rules.DNS`. The FQDN builder MUST emit both rules atomically.
3. **Mixing HTTP and DNS in one `L7Rules`** — Cilium rejects. Keep HTTP rules on the workload's HTTP port (e.g. 80/TCP) and DNS rules on port 53/UDP.
4. **Path explosion** — `/users/123`, `/users/124`, … blow up Envoy memory. Out of scope for v1.2 (note path normalization as v1.3 candidate); document in v1.2 release notes as a known limitation.

---

## Risk Hotspots to Flag for Roadmap

- **`mergePortRules` silent data loss (HIGH).** This is a latent bug today (no L7 input in v1.1, so harmless) that becomes a correctness break the moment L7 builder ships. Step 1 in build order — non-negotiable.
- **Evidence schema v1→v2 migration (MEDIUM).** Existing users have v1 cache files. The reader must keep v1 decode for at least one minor cycle. Test fixture for v1 read-only path required.
- **REDIRECTED verdict (MEDIUM, validation only).** L7-visible flows may arrive as `REDIRECTED` rather than `DROPPED` depending on cluster config. Verify against a live cluster during step 4. Filter expansion is a one-line change if needed; document the trade-off (REDIRECTED includes successful proxy traffic — needs verdict-aware handling so we don't generate policies *from allowed flows*).
- **Hubble L7 visibility prerequisite (LOW, doc).** Users without L7 visibility get L4-only output. Document in release notes; no code change.

---

## Sources

- Existing codebase (`pkg/policy/builder.go`, `pkg/policy/merge.go`, `pkg/policy/dedup.go`, `pkg/policy/attribution.go`, `pkg/hubble/aggregator.go`, `pkg/output/writer.go`, `pkg/evidence/schema.go`, `cmd/cpg/explain*.go`) — direct read, HIGH confidence.
- `.planning/research/archive-2026-04-25/ARCHITECTURE.md` — prior L7+apply design, retained where applicable.
- `.planning/research/archive-2026-04-25/SUMMARY.md` — pitfalls and stack confirmations carried forward.

---
*Architecture research for: CPG v1.2 L7 policies (HTTP + DNS) — apply deferred*
*Researched: 2026-04-25*
