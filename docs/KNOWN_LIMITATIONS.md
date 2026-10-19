# Known Limitations — cpg v1.2

This document lists known limitations and edge cases in the current release. Most are intentional trade-offs; a few are documented gaps awaiting future work. Each entry includes the workaround (if any) and the tracking ID for v1.3+ planning.

For the full list of upcoming features, see [`.planning/PROJECT.md`](../.planning/PROJECT.md) (Planned section) and [`.planning/ROADMAP.md`](../.planning/ROADMAP.md).

---

## 1. L7 visibility prerequisite (the most important)

**Symptom:** `cpg generate --l7` (or `cpg replay --l7`) emits a VIS-01 warning and produces L4-only YAML, even though the user expected HTTP/DNS rules.

**Root cause:** Hubble only emits `Flow.L7` records when traffic is proxied by Cilium Envoy or the DNS proxy. This requires both:
1. `enable-l7-proxy: true` set cluster-wide in `kube-system/cilium-config`, AND
2. A per-workload visibility trigger — either a `policy.cilium.io/proxy-visibility` annotation or an existing L7 `CiliumNetworkPolicy` that already matches the workload.

cpg cannot bootstrap visibility itself. Without these prerequisites, `--l7` has nothing to read.

**Workaround:** Follow the two-step workflow documented in the [L7 guide](l7-guide.md). Deploy the starter L7-visibility CNP first, capture again, then re-run `cpg generate --l7`.

**Tracking:** No issue — by-design constraint of Cilium architecture. Future UX improvement (`cpg setup-visibility <workload>` interactive bootstrap) is on the v1.3 wishlist but not committed.

---

## 2. HTTP path explosion on REST APIs with IDs

**Symptom:** A workload exposing `/api/v1/users/{id}` and observed handling 50,000 distinct requests will produce 50,000 separate `http:` rules in the generated CNP — each path is a literal regex `^/api/v1/users/N$`.

**Root cause:** v1.2 emits one rule per observed `(method, path)` pair. No regex inference or path-template collapse. This is a deliberate trade-off — auto-collapse hallucinates allow-lists broader than what was observed (a security risk).

**Workaround:**
- Manually consolidate the generated YAML before applying (`/api/v1/users/[0-9]+`).
- Limit the observation window (`hubble observe --since 5m`) so fewer paths accumulate.
- Apply L4-only by skipping `--l7`, then iterate.

**Tracking:** `HTTP-FUT-01` (`--l7-collapse-paths` flag) and `L7-FUT-02` (`--min-flows-per-l7-rule N` low-confidence gate) — both deferred to v1.3.

---

## 3. HTTP `Headers`, `Host`, `HostExact` never generated

**Symptom:** Cpg never emits header-based rules even when the user might want them (e.g., enforce `X-Tenant-ID` on a multi-tenant gateway).

**Root cause:** Intentional anti-feature. Authentication/Authorization headers (`Authorization`, `Cookie`, custom session tokens) routinely appear in observed flows. Emitting them into committed policy YAML risks leaking secrets into git history. The risk-vs-value trade-off favors no header generation.

**Workaround:** Hand-craft header rules separately and merge them with cpg-generated YAML. Use Cilium's standard `headerMatches` field syntax.

**Tracking:** Permanent design decision. Not on roadmap.

---

## 4. DNS REFUSED denials surfaced as `Verdict_FORWARDED`

**Symptom:** A pre-existing L7 DNS policy denies a query (returns DNS REFUSED). Hubble may surface this as `Verdict_FORWARDED` rather than `Verdict_DROPPED`. cpg only filters `DROPPED` flows, so these denials are invisible.

**Root cause:** Cilium DNS-proxy denials use a different verdict path than L4 drops. Defensive correctness — we don't want to generate new rules from already-policied traffic — kept the DROPPED-only filter.

**Workaround:** Inspect REFUSED denials manually with `hubble observe --verdict FORWARDED --type l7`.

**Tracking:** `L7-FUT-01` — `--include-l7-forwarded` flag deferred to v1.3 pending live-cluster validation.

---

## 5. DNS query lost but resolved IP captured

**Symptom:** Hubble misses the DNS layer record (ring buffer overflow, DNS proxy timing) but captures the subsequent TCP connection to the resolved IP. cpg falls back to a CIDR rule for the exact IP — brittle if the external service changes IPs (cloud-hosted load balancers do this routinely).

**Root cause:** No IP→FQDN reverse correlation cache in v1.2. Each flow is treated independently.

**Workaround:** Re-run cpg over a longer observation window so the DNS record is more likely to be captured. Manually replace generated CIDR rules with `toFQDNs.matchName` after review.

**Tracking:** `DNS-FUT-03` — ToFQDNs from IP→name correlation deferred to v1.3.

---

## 6. DNS companion selector on non-vanilla clusters

**Symptom:** The auto-injected DNS-53 companion rule must select the cluster DNS pods. CoreDNS labels differ across distributions: `k8s-app: kube-dns` on kubeadm / EKS / GKE / AKS / k3s, `k8s-app: coredns` with the upstream CoreDNS Helm chart defaults (e.g. Outscale OKS). A companion that selects no pod leaves DNS denied and `toFQDNs` never matches.

**Behavior (`DNS-FUT-02`, resolved):** `cpg generate --l7` (and `cpg mcp` L7 sessions) read the selector of the port-53 Service in `kube-system` (`kube-dns` first, then `coredns`, then a lone port-53 Service) and use it verbatim, pinned to `io.kubernetes.pod.namespace: kube-system`. The resolved selector is logged as `dns companion selector detected`. When detection is unavailable — `cpg replay`, the `--server` bypass without kubeconfig, RBAC denied, or no unambiguous Service — cpg logs one warning and falls back to:

```yaml
- matchLabels:
    io.kubernetes.pod.namespace: kube-system
  matchExpressions:
    - {key: k8s-app, operator: In, values: [kube-dns, coredns]}
```

**RBAC:** detection needs `services/list` in `kube-system`; without it cpg uses the fallback.

**Remaining gap:** DNS outside `kube-system` (e.g. OpenShift `openshift-dns`) is not detected. Workaround: edit the companion rule's selector after generation. A companion carrying `k8s-app: kube-dns` or `k8s-app: coredns` (matchLabels or the fallback expression) is recognized on subsequent runs and not duplicated.

**Upgrade note:** policies generated before this change carry `k8s-app: kube-dns` in `matchLabels`. Merging new output appends the new companion next to it; delete the old rule if it selects no pod on your cluster.

**Tracking:** `DNS-FUT-02` — resolved for `kube-system` DNS; non-`kube-system` DNS namespace not yet handled.

---

## 7. DROPPED vs REDIRECTED verdict under L7 visibility

**Symptom:** When a workload already has an L7 CNP that matches, Cilium surfaces flows as `Verdict_REDIRECTED` (proxied by Envoy) rather than `DROPPED`. cpg ignores REDIRECTED. This is correct — we don't want new rules from already-policied traffic — but the user sees fewer denied-flow records than expected and may misread the result as "cpg covered everything".

**Root cause:** DROPPED-only filter is the safest default. No diagnostic surface in the session summary explains the REDIRECTED count.

**Workaround:** Run `hubble observe --verdict REDIRECTED` independently to see what the existing L7 policies are matching.

**Tracking:** No issue. Live-cluster validation prevu en v1.3 to size the actual UX gap.

---

## 8. Unicode and URL encoding in HTTP paths

**Symptom:** A path like `/users/André` or `/users/Andr%C3%A9` may not match consistently across Cilium versions or observation tools.

**Root cause:** `regexp.QuoteMeta` escapes ASCII metacharacters. UTF-8 bytes pass through (RE2 matches byte-for-byte). URL-encoded paths produce a literal regex that matches only the encoded form, not the decoded one. There is no automated cross-Cilium-version test for path normalization.

**Workaround:** Manually verify path matching with `cilium policy validate` after generation. Re-encode paths consistently in source applications.

**Tracking:** No issue. Cilium upstream behavior; cpg follows whatever Hubble exposes.

---

## 9. Dynamic namespace creation

**Symptom:** A new tenant namespace appears after the cpg run; its denied-flows do not generate rules. Re-run required.

**Root cause:** cpg is a batch tool. Each run reads the observation window's flows and produces rules for the workloads seen during that window.

**Workaround:** None needed for awareness — `cpg generate` now reports the blind spot itself. It watches namespace creation in read-only fashion (only the `namespaces/list` and `namespaces/watch` verbs) and, at end of run, lists namespaces that appeared after run start under `New namespaces detected during this run:` with a re-run hint (`namespace <name> appeared after run start — re-run to cover it`). The watch is fail-safe and advisory: if it is unavailable (e.g. an RBAC-restricted service account, an unreachable kubeconfig, or watch expiry) cpg logs exactly one warning and proceeds normally — generation is never blocked or degraded. `cpg replay` is offline by definition and instead reports namespaces first observed after capture start via a flow-timestamp heuristic on the captured flows. The report surfaces the blind spot; it does not close it — re-running cpg is still required to cover the new namespace, and long-running streaming/daemon mode remains out of scope.

**Tracking:** No issue for the awareness report (v1.7, Phase 26: `pkg/k8s` namespace watcher wired into `cpg generate`; heuristic fallback for `cpg replay`). By-design batch tool: covering a newly created namespace still requires a re-run.

---

## 10. Companion DNS-53 collision with existing rules

**Symptom:** If the generated policy already contains an egress rule allowing UDP/53 to DNS pods with a selector cpg does not recognize (anything other than the detected selector, `k8s-app: kube-dns|coredns`, or the fallback expression), `ensureKubeDNSCompanion` adds a second companion rule, producing redundant rules. Similarly, if an existing rule restricts UDP/53 only, the companion still adds a separate UDP+TCP/53 rule.

**Root cause:** The idempotency check looks for an exact match on the companion's selector + ports. Partial overlap is not detected.

**Workaround:** Manually consolidate redundant rules after generation.

**Tracking:** No issue. Rare in practice since `DNS-FUT-02` (see limitation #6).

---

## 11. `cpg explain` filters are exact-match only

**Symptom:** `cpg explain api --http-path=/api/v1/users` matches only the literal path. Passing `--http-path=/api/v1/users.*` or `/api/*` does not work — these are treated as literal strings, not regex/glob patterns.

**Root cause:** v1.2 deliberately ships exact-match filters only. Regex/glob filters add complexity and lock in semantics; deferred until users ask.

**Workaround:** Use multiple `cpg explain` calls with different exact paths, or pipe `cpg explain --json | jq` for ad-hoc filtering.

**Tracking:** No formal issue. Revisit if user feedback demands it.

---

## 12. Evidence schema downgrade (v2 → v1)

**Symptom:** A user upgrades cpg from v1.1 to v1.2, generates evidence in v2 format, then downgrades back to v1.1. The v1.1 reader rejects v2 files with a generic schema-version mismatch error (does not know the v2 wipe instruction).

**Root cause:** v1.1 was released 2026-04-24; v1.2 ships the day after. There was no production cache to migrate. v1.2's reader has the explicit `$XDG_CACHE_HOME/cpg/evidence/` wipe instruction; v1.1's does not.

**Workaround:** Wipe `$XDG_CACHE_HOME/cpg/evidence/` before downgrading to v1.1.

**Tracking:** No issue. Edge case is rare (no real reason to downgrade).

---

## Reporting new limitations

Found a limitation not on this list? Open an issue at [SoulKyu/cpg](https://github.com/SoulKyu/cpg/issues) with:

- The cpg command and flags you ran
- Cilium version + cluster type (vanilla / EKS / GKE / AKS / etc.)
- Expected vs observed behavior
- A minimal reproducer (sanitized Hubble flow JSON or replay fixture)

The fastest fixes ship as v1.3 candidates; harder cases get tracked in the roadmap with clear semver impact.
