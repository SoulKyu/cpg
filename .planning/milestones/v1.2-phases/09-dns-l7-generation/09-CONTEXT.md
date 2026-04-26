# Phase 9: DNS L7 Generation + explain L7 + Docs — Context

**Gathered:** 2026-04-25
**Status:** Ready for planning
**Mode:** Auto-generated (`--auto` flag — implementation-focused phase)

<domain>
## Phase Boundary

Final phase of v1.2. Light up the DNS branch of L7 codegen, surface L7 attribution in `cpg explain`, and ship the README two-step workflow + starter visibility CNP. End-of-phase, milestone v1.2 is feature-complete.

Concretely, this phase delivers:
1. DNS L7 rule extraction from `Flow.L7.Dns` records (DNS-01).
2. `toFQDNs.matchName` (literal hostname, trailing-dot stripped) paired with `toPorts.rules.dns.matchName` for the same name.
3. **Mandatory companion DNS-allow rule:** every CNP containing `toFQDNs` MUST also contain a companion egress rule `toEndpoints` with selector `k8s-app=kube-dns` allowing UDP+TCP/53. Atomicity is a unit-test invariant: cpg generator MUST NEVER emit `toFQDNs` without the companion (DNS-02).
4. **No `matchPattern` glob:** v1.2 emits literal `matchName` only. No wildcard inference (DNS-03).
5. **Fallback:** when no `Flow.L7.Dns` records arrive (DNS proxy disabled), egress rules for external traffic stay v1.1 CIDR-based with byte-identical output (DNS-04).
6. `cpg explain` accepts `--http-method`, `--http-path`, `--dns-pattern` exact-match filters (L7CLI-02).
7. `cpg explain` renders L7 attribution per rule when present in evidence: HTTP method+path or DNS matchName, in text/JSON/YAML formats (L7CLI-03).
8. README documents the two-step workflow (L4 deploy → enable L7 visibility → re-run with `--l7`) (VIS-02) and ships a copy-pasteable starter L7-visibility CNP (VIS-03).

Mapped requirements: DNS-01, DNS-02, DNS-03, DNS-04, L7CLI-02, L7CLI-03, VIS-02, VIS-03.

</domain>

<decisions>
## Implementation Decisions

### DNS L7 codegen
- **Same package as HTTP:** extend `pkg/policy/l7.go` with `extractDNSRules(*flowpb.Flow) (matchName string, ok bool)`. Only one DNS query per flow → return single string + presence boolean.
- **Trailing-dot stripping:** `strings.TrimSuffix(query, ".")`. Cilium expects bare names.
- **Empty/malformed query → drop entry, no error.**
- **`Flow.L7.Dns.GetRcode()` filter:** v1.2 generates from queries regardless of rcode (NOERROR vs NXDOMAIN vs REFUSED). The DROPPED verdict already filters in. Document REFUSED/FORWARDED gap in PHASE 9 SUMMARY (deferred to v1.3 per L7-FUT-01).
- **Aggregation:** distinct DNS queries observed for the same workload → multiple `toFQDNs.matchName` entries in one egress rule (or separate rules, designer's choice — keep simple, one egress rule per (src, dst-pattern) tuple).

### Companion DNS-53 rule (DNS-02)
- **Atomic generation:** `BuildPolicy` post-processing checks the result for `toFQDNs` rules. If any present AND companion-DNS-53-rule is absent → AUTO-INSERT it.
- **Selector:** hardcoded `k8s-app=kube-dns` with a YAML comment naming the assumption (`# kube-dns selector hardcoded; autodetection deferred to v1.3 per DNS-FUT-02`).
- **Ports:** `[{port: "53", protocol: UDP}, {port: "53", protocol: TCP}]`.
- **Companion rule sub-section:** `toEndpoints: [{matchLabels: {"k8s-app": "kube-dns", "io.kubernetes.pod.namespace": "kube-system"}}]`.
- **Unit-test invariant:** every test that produces a `toFQDNs`-bearing CNP asserts the companion rule is present. Lint test walks all generated YAML to verify.

### `cpg explain` L7 filters (L7CLI-02)
- Three exact-match filter flags on `cpg explain`:
  - `--http-method <METHOD>` (uppercase normalized when matching)
  - `--http-path <PATH>` (literal exact match against the un-anchored path stored in evidence)
  - `--dns-pattern <NAME>` (literal exact match against `matchName`, trailing dot stripped)
- Filters AND together (multiple flags = all must match).
- L4-only evidence + any L7 filter set → empty result (no rules match).
- No regex/glob — literal exact match. Regex deferred to v1.3.

### `cpg explain` L7 rendering (L7CLI-03)
- **Text:** indent under the rule line. Format: `    L7: HTTP GET /api/v1/users` or `    L7: DNS api.example.com`. Single line per L7 entry.
- **JSON:** add `"l7": {"protocol": "http"|"dns", "http_method": "...", "http_path": "...", "dns_matchname": "..."}` per rule (omitempty fields). Mirror schema v2 `L7Ref`.
- **YAML:** `l7:` block with same fields.

### README (VIS-02 + VIS-03)
- New section `## L7 Prerequisites` (already anchor-reserved by Phase 8) documenting:
  - The two-step workflow: deploy L4 first, enable L7 visibility, re-run cpg with `--l7`.
  - Three ways to enable visibility:
    1. **Recommended:** `policy.cilium.io/proxy-visibility` annotation on the workload (deprecated upstream but widely supported).
    2. **Alternative:** ship a starter L7 CNP that triggers Envoy proxy injection without enforcing rules.
    3. **Cluster-wide:** `enable-l7-proxy: true` in cilium-config (already required for any L7 work).
  - Starter L7-visibility CNP snippet (copy-pasteable):
    ```yaml
    apiVersion: cilium.io/v2
    kind: CiliumNetworkPolicy
    metadata:
      name: cpg-l7-visibility-bootstrap
      namespace: <ns>
    spec:
      endpointSelector: {matchLabels: {"app.kubernetes.io/name": "<workload>"}}
      egress:
        - toPorts:
            - ports: [{port: "80", protocol: TCP}]
              rules: {http: [{}]}  # match-all triggers Envoy without enforcing
    ```
- Update README sections that reference `cpg generate` / `cpg replay` to mention `--l7` flag where appropriate.
- Update `cpg explain` README section to mention new L7 filters and L7 rendering.

### Out of scope for THIS phase (and v1.2)
- DNS `matchPattern` glob inference (deferred to v1.3 per DNS-FUT-01).
- ToFQDNs from IP→name correlation (deferred to v1.3 per DNS-FUT-03).
- kube-dns selector autodetection (deferred to v1.3 per DNS-FUT-02).
- `--include-l7-forwarded` for REFUSED denials (deferred to v1.3 per L7-FUT-01).
- `cpg explain` regex/glob filters (deferred to v1.3).

### Claude's Discretion
File names for the README section (e.g., starter snippet location), exact JSON/YAML field naming (use `http_method`/`http_path`/`dns_matchname` to match schema v2), and test fixture content are at Claude's discretion.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/policy/l7.go` (Phase 8) — has `extractHTTPRules`, `normalizeHTTPMethod`, `anchorPath`. DNS code lives in same file as `extractDNSRules`.
- `pkg/policy/builder.go::BuildPolicy` (Phase 8) — already supports L7 codegen path with `AttributionOptions.L7Enabled`. Extend to call DNS extractor + companion-rule injector.
- `pkg/policy/attribution.go::RuleKey.L7Discriminator` (Phase 7) — extend format: `dns:{matchName}` for DNS rules.
- `pkg/evidence/schema.go::L7Ref` (Phase 7) — already has `DNSMatchName` field. No schema change needed.
- `pkg/hubble/evidence_writer.go::convertAttribution` (Phase 8) — already has TODO for DNS branch (Phase 8 Plan 03 left it explicit). Light it up.
- `cmd/cpg/explain.go` — existing v1.1 explain code with `--ingress`/`--egress`/`--port`/`--peer`/`--peer-cidr`/`--since` filters. Add three L7 filters following the same pattern.
- `cmd/cpg/explain_render.go` — existing renderers (text/JSON/YAML). Extend each to emit L7 block.

### Established Patterns
- TDD-first commits.
- Anti-feature lint tests (per HTTP-05 model in 08-01).
- Companion-rule invariant tests (NEW pattern this phase): every test producing toFQDNs asserts the companion rule.
- Cobra flag declaration in cmd/cpg/explain.go (long flag only, no shorts).
- Cilium api types: `api.FQDNSelector{MatchName: "..."}` for `toFQDNs`, `api.PortRuleDNS` for `dns.matchName`.

### Integration Points
- `pkg/policy/l7.go` — extend with DNS extraction + companion-rule helpers.
- `pkg/policy/builder.go::BuildPolicy` — DNS extraction call + post-process companion-rule injection.
- `pkg/hubble/evidence_writer.go::convertAttribution` — fill DNS branch.
- `cmd/cpg/explain.go` — three new flags + filter logic.
- `cmd/cpg/explain_render.go` — L7 block in all three formats.
- `README.md` — fill the `#l7-prerequisites` section with content + add starter CNP snippet.
- `pkg/hubble/aggregator.go` — increment `L7DNSCount` on DNS records (Phase 8 reserved the counter).

</code_context>

<specifics>
## Specific Ideas

- The companion-rule invariant should be tested via a test helper `assertHasKubeDNSCompanion(t, cnp)` that gets called from every test producing `toFQDNs`. Centralize the rule shape so any future change to selectors propagates.
- The starter L7-visibility CNP in the README must be valid Cilium YAML — copy-pasteable, no `<placeholder>` placeholders that block apply. Use real values with `<workload>` annotated as a fill-in comment.
- For DNS L7 fixtures, capture or synthesize an L7 jsonpb fixture with 2-3 DNS query flows: NOERROR query (api.example.com), NXDOMAIN query (typo.example.com), and one routine kube-dns query that should NOT generate a rule (because internal cluster traffic shouldn't bypass internal DNS).
- The `cpg explain --dns-pattern` filter compares against the literal `matchName` in evidence — no regex. If the user passes `--dns-pattern=*.example.com`, that's an exact-match `*.example.com` (which won't match anything since v1.2 doesn't generate wildcards). Document this in the README to prevent confusion.

</specifics>

<deferred>
## Deferred Ideas

- DNS `matchPattern` glob auto-generation (v1.3).
- `cpg explain` regex/glob filters (v1.3 if requested).
- kube-dns selector autodetection across CNI distributions (v1.3, DNS-FUT-02).
- `--include-l7-forwarded` for REFUSED → FORWARDED denials (v1.3, L7-FUT-01).
- Multi-language reasoning in `cpg explain` (out of scope, never).

</deferred>
