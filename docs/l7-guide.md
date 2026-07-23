# L7 guide (HTTP + DNS rules)

`cpg --l7` translates what Hubble shows it. Hubble only emits `Flow.L7`
records (HTTP method/path, DNS query) when traffic is proxied — by Envoy
for HTTP, by the DNS proxy for DNS. cpg cannot turn that on for you.
If you see the warning

```
--l7 set but no L7 records observed
```

this page is the fix.

## Two-step workflow

Refining L4 policies with L7 rules is a two-step dance, and the order
matters:

1. **Deploy L4 first.** Run `cpg generate -n <namespace>` (no `--l7`).
   Review the generated CiliumNetworkPolicies, commit, apply. You are
   now moving toward default-deny with port/peer-level enforcement.

2. **Enable L7 visibility** on the workloads you want refined. Three
   options below — pick whichever fits your operational model.

3. **Re-run cpg with `--l7`.** With visibility enabled, Hubble starts
   emitting `Flow.L7` records and cpg attaches `rules.http` for HTTP
   and `toFQDNs` + the kube-dns companion for DNS to the relevant
   egress rules.

   ```bash
   cpg generate --l7 -n <namespace>
   # or, on a captured stream
   cpg replay drops.jsonl --l7 -n <namespace>
   ```

## Three ways to enable L7 visibility

1. **Recommended for ad-hoc bootstrap — proxy-visibility annotation.**
   The legacy workload-level annotation that triggers Envoy / DNS proxy
   redirection without enforcing rules. Works only through **Cilium 1.16**
   -- removed from the agent runtime at **1.17** (a no-op on 1.17+). See
   the [Supported Cilium versions](installation.md#supported-cilium-versions)
   table for the full matrix:

   ```bash
   kubectl annotate pod -n <ns> -l app.kubernetes.io/name=<workload> \
     policy.cilium.io/proxy-visibility='<Egress/53/UDP/DNS>,<Ingress/8080/TCP/HTTP>'
   ```

   Easy to apply, easy to remove -- on clusters where it still works
   (<= 1.16). Marked deprecated upstream before removal.

2. **Recommended for permanent enforcement — bootstrap L7 CNP.** Ship
   a starter CiliumNetworkPolicy with a permissive L7 rule. The mere
   *presence* of an L7 rule on a workload triggers Cilium to proxy that
   workload's traffic — match-all `{}` in the HTTP rule and `"*"` in
   the DNS matchPattern lights up visibility without enforcing
   anything. See the snippet below.

3. **Cluster-wide prerequisite — `enable-l7-proxy: true`.** In the
   `kube-system/cilium-config` ConfigMap. Default `true` on most
   installs, but required for any of the above to work. cpg's
   `--l7` pre-flight check (VIS-04) flags it explicitly when missing
   or set to false.

## Starter L7-visibility CNP

Copy-pasteable, valid Cilium YAML. Replace the two placeholders
(namespace + workload label) before applying:

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: cpg-l7-visibility-bootstrap
  namespace: production    # replace with your namespace
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: my-app    # replace with your workload's label
  egress:
    # match-all HTTP rule triggers Envoy without enforcing a path/method.
    - toEndpoints:
        - {}
      toPorts:
        - ports:
            - {port: "80", protocol: TCP}
          rules:
            http:
              - {}
    # match-all DNS rule triggers DNS proxy visibility for kube-dns.
    - toEndpoints:
        - matchLabels:
            k8s-app: kube-dns
            io.kubernetes.pod.namespace: kube-system
      toPorts:
        - ports:
            - {port: "53", protocol: UDP}
            - {port: "53", protocol: TCP}
          rules:
            dns:
              - matchPattern: "*"
```

Apply, observe traffic, then run `cpg --l7`. Once cpg's generated
policy covers everything you need, this bootstrap CNP can be deleted —
its only job was the visibility side-effect of Envoy / DNS proxy
injection.

## Capture-window guidance

Run cpg long enough to capture one full traffic cycle for the
workloads in question. A single observation produces a single rule
with `flow_count=1`, which `cpg explain` surfaces as low-confidence
evidence. For periodic batch jobs, capture across at least one period.

## Known limitations

cpg ships with a documented set of known limitations and edge cases — most are intentional trade-offs (e.g., no HTTP header rules to avoid secret leakage), a few are deferred to v1.3+. Read them **before deploying generated policies** to production:

→ **[KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md)** — full list with workarounds and tracking IDs.

Highlights worth knowing up front:

- L7 visibility prerequisite — `--l7` requires Cilium Envoy proxy + per-workload visibility trigger; cpg cannot bootstrap it (limitation #1).
- HTTP path explosion on REST APIs with IDs — one literal rule per observed `(method, path)`; no auto-collapse (limitation #2, `HTTP-FUT-01`).
- Header-based rules never generated — anti-feature to prevent secret leakage (limitation #3).
- DNS REFUSED denials are missed — `Verdict_FORWARDED` not yet supported (limitation #4, `L7-FUT-01`).
- kube-dns companion selector hardcoded `k8s-app=kube-dns` — autodetect across CNI distributions deferred to v1.3 (limitation #6, `DNS-FUT-02`).
