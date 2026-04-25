# Requirements: CPG (Cilium Policy Generator) — v1.2 L7 Policies

**Defined:** 2026-04-25
**Milestone:** v1.2 L7 Policies (HTTP + DNS)
**Core Value:** Extend automatic CiliumNetworkPolicy generation from L4 to L7 (HTTP method/path, DNS FQDN matchPattern) so SREs do not have to hand-craft L7 rules after observing L7 traffic in Hubble.

## v1.2 Requirements

Each requirement maps to exactly one phase. Traceability table at bottom.

### Visibility & Workflow

- [ ] **VIS-01**: cpg detects when `--l7` is requested but no `Flow.L7` records appear in the observed window, and emits a single, actionable warning that names the workloads missing visibility and links to the README L7 prerequisite section.
- [ ] **VIS-02**: cpg documents the two-step workflow in the README: (1) deploy L4 policies first, (2) enable L7 visibility (proxy-visibility annotation OR an L7 CNP that triggers Envoy proxy injection), (3) re-run cpg with `--l7` to refine.
- [ ] **VIS-03**: A starter L7-visibility-trigger CNP snippet ships in the README so users can bootstrap visibility for a workload they want to observe.
- [x] **VIS-04**: Pre-flight check (`cpg generate --l7` only) reads ConfigMap `kube-system/cilium-config`, verifies `enable-l7-proxy: "true"`. If false or missing, emit a warning naming the offending field with a remediation hint (set in ConfigMap and roll the cilium-agent DaemonSet). On RBAC denied: warn-and-proceed.
- [x] **VIS-05**: Pre-flight check (`cpg generate --l7` only) verifies presence of cilium-envoy: DaemonSet `kube-system/cilium-envoy` (Cilium ≥ 1.16) OR detection by the `enable-envoy-config` flag in cilium-config (Cilium 1.14–1.15 embeds envoy in cilium-agent — check passes silently). On RBAC denied: warn-and-proceed.
- [ ] **VIS-06**: A `--no-l7-preflight` flag skips VIS-04 and VIS-05 cluster checks, for restricted-RBAC CI, kubeconfig without `kube-system` access, or air-gapped use. VIS-01 (passive empty-records warning) still fires.

### HTTP L7 Generation

- [ ] **HTTP-01**: When `--l7` is set and `Flow.L7.Http` records are present for a (src, dst, port) tuple, cpg generates a `toPorts.rules.http` block alongside the L4 port rule.
- [ ] **HTTP-02**: HTTP method is normalized to uppercase (`GET`, `POST`, `PUT`, `DELETE`, …) before emission, regardless of how Hubble reports the method casing.
- [ ] **HTTP-03**: HTTP path is emitted as a Cilium-compatible RE2 regex: `regexp.QuoteMeta` applied to the literal path, anchored with `^…$`. No regex inference, no path templating, no auto-collapse — one rule per observed (method, path) pair.
- [ ] **HTTP-04**: Multiple distinct (method, path) observations for the same (src, dst, port) tuple merge into a single `toPorts.rules.http` list — not into multiple PortRule blocks.
- [ ] **HTTP-05**: HTTP `Headers`, `Host`, and `HostExact` rules are NOT generated. Header generation is explicitly out of scope (see Out of Scope, anti-feature: secret leakage into committed YAML).

### DNS L7 Generation

- [ ] **DNS-01**: When `--l7` is set and `Flow.L7.Dns` records are present for an egress dropped flow, cpg generates an egress rule with `toFQDNs.matchName` (literal hostname) for the queried name, paired with `toPorts.rules.dns.matchName`.
- [ ] **DNS-02**: For every CNP that contains `toFQDNs`, cpg automatically emits a companion egress rule allowing UDP+TCP/53 to kube-dns (selector hardcoded for v1.2 with a YAML comment naming the assumption; autodetection deferred to v1.3).
- [ ] **DNS-03**: DNS `matchPattern` (glob) is NOT auto-generated from observed names in v1.2 — only `matchName` (literal). Wildcard inference is deferred to v1.3.
- [ ] **DNS-04**: When `Flow.L7.Dns` is absent (DNS proxy disabled or no DNS denials in window), cpg falls back to the existing v1.1 CIDR-based egress rule for external traffic, with no behavior change vs v1.1.

### Evidence Schema (Internal Contract)

- [ ] **EVID2-01**: The evidence JSON schema bumps from `schema_version: 1` to `schema_version: 2`. The v2 schema adds an optional `l7` field per `RuleEvidence` (sub-fields: `protocol` ∈ {http, dns}, `http_method`, `http_path`, `dns_matchname`). The existing reader behavior is preserved: any non-`2` value is rejected with a clear error directing the user to wipe `$XDG_CACHE_HOME/cpg/evidence/` (no v1 back-compat layer — v1.1 shipped 2026-04-24, no caches in production).
- [ ] **EVID2-02**: `RuleKey` extends with an optional L7 discriminator so that two rules differing only by HTTP method or path are not deduplicated into the same evidence bucket.
- [ ] **EVID2-03**: `mergePortRules` (`pkg/policy/merge.go`) preserves the `Rules` field of `PortRule` across merge operations. Today it silently drops it (latent bug; harmless before L7 codegen, silent data loss after).
- [ ] **EVID2-04**: `normalizeRule` extends to deterministically sort L7 lists (HTTP method+path lexicographic, DNS matchName lexicographic) so YAML output stays byte-stable across runs and dedup file-comparison stays correct.

### CLI Surface

- [ ] **L7CLI-01**: `cpg generate` and `cpg replay` accept `--l7` (default OFF). When unset, behavior is byte-identical to v1.1 for the same input.
- [ ] **L7CLI-02**: `cpg explain` accepts three new exact-match filter flags: `--http-method`, `--http-path`, `--dns-pattern`. Regex / glob filters are deferred.
- [ ] **L7CLI-03**: `cpg explain` renders L7 attribution per rule when present in the evidence: HTTP method + path, or DNS matchName. JSON / YAML formats include the L7 sub-object; text format prints a single indented line per L7 entry.

## Future Requirements (deferred)

Captured here for traceability; not in v1.2 scope.

- [ ] **HTTP-FUT-01**: Auto-collapse of similar paths into a regex (`/api/v1/users/[0-9]+`) — `--l7-collapse-paths` flag.
- [ ] **DNS-FUT-01**: Wildcard FQDN inference (`*.amazonaws.com` from observed `s3.amazonaws.com`, `ec2.amazonaws.com`) — `--l7-fqdn-wildcard-depth` flag.
- [ ] **DNS-FUT-02**: kube-dns selector autodetection across Cilium distributions (EKS / GKE / AKS / vanilla).
- [ ] **DNS-FUT-03**: ToFQDNs from IP→name correlation when DNS records are missed but the resolved IP is present.
- [ ] **L7-FUT-01**: `--include-l7-forwarded` to capture DNS REFUSED denials surfaced as `Verdict_FORWARDED` rather than `DROPPED`.
- [ ] **L7-FUT-02**: `--min-flows-per-l7-rule N` gate for low-confidence rules (single-observation paths emitted as YAML comments).
- [ ] **L7-FUT-03**: kube-dns selector autodetection at runtime via `kubectl get pods -l <candidate-labels>`.

## Out of Scope (v1.2)

| Feature | Reason |
|---------|--------|
| HTTP `Headers` rules | Risk of leaking `Authorization`, `Cookie`, or other secrets into committed policy YAML. Operators who genuinely need header-based rules write them by hand. |
| HTTP `Host` / `HostExact` rules | Authority is captured by FQDN egress + path; Host adds complexity without observed value. |
| Kafka L7 rules | Deprecated upstream; no demand in current cpg user base. |
| gRPC-as-distinct-protocol L7 rules | Already covered by HTTP `POST /<package>.<service>/<method>`. |
| Generic `L7Proto` (Cilium custom proxy plugins) | Niche; no path to test without custom Envoy filters. |
| `cpg apply` | Deferred to v1.3. Operators apply via `kubectl apply` for v1.2. |
| Policy consolidation across overlapping rules | Deferred to v1.3. |
| Prometheus metrics | Deferred to v1.3. |
| AI-assisted plausibility analysis | Shelved — see PROJECT.md decisions table (2026-04-25). |
| ToFQDNs from IP correlation | Deferred to v1.3 (DNS-FUT-03). |
| Path / FQDN auto-collapse heuristics | Deferred to v1.3 (HTTP-FUT-01, DNS-FUT-01). |

## Traceability

Filled by the roadmapper after phase definition.

| Requirement | Phase | Status |
|-------------|-------|--------|
| VIS-01 | Phase 8 | Planned |
| VIS-02 | Phase 9 | Planned |
| VIS-03 | Phase 9 | Planned |
| VIS-04 | Phase 7 | Planned |
| VIS-05 | Phase 7 | Planned |
| VIS-06 | Phase 7 | Planned |
| HTTP-01 | Phase 8 | Planned |
| HTTP-02 | Phase 8 | Planned |
| HTTP-03 | Phase 8 | Planned |
| HTTP-04 | Phase 8 | Planned |
| HTTP-05 | Phase 8 | Planned |
| DNS-01 | Phase 9 | Planned |
| DNS-02 | Phase 9 | Planned |
| DNS-03 | Phase 9 | Planned |
| DNS-04 | Phase 9 | Planned |
| EVID2-01 | Phase 7 | Planned |
| EVID2-02 | Phase 7 | Planned |
| EVID2-03 | Phase 7 | Planned |
| EVID2-04 | Phase 7 | Planned |
| L7CLI-01 | Phase 7 | Planned |
| L7CLI-02 | Phase 9 | Planned |
| L7CLI-03 | Phase 9 | Planned |

**Coverage:** 22 v1.2 requirements, all to be mapped by the roadmapper.

---
*Requirements defined: 2026-04-25 — based on parallel research findings (STACK / FEATURES / ARCHITECTURE / PITFALLS / SUMMARY).*
