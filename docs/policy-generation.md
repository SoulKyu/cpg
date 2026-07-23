# Policy generation

## How it works

```
         Hubble Relay (gRPC)
              |
         [cpg generate]
              |
     stream dropped flows
              |
     aggregate by workload
              |
     build CiliumNetworkPolicy
              |
     merge with existing files
              |
     write YAML to disk
              |
     you review & git push
```

Flows are aggregated by namespace and workload on a configurable interval (default 5s), so you get one policy per workload -- not one per packet. Existing files are read, merged (new ports and peers appended), and only rewritten if something actually changed.

## What it generates

Given a dropped ingress flow to a pod labeled `app.kubernetes.io/name: api-server` on port 8080/TCP from a pod with `app: frontend`:

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: cpg-api-server
  namespace: production
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: api-server
  ingress:
    - fromEndpoints:
        - matchLabels:
            app: frontend
      toPorts:
        - ports:
            - port: "8080"
              protocol: TCP
```

External traffic (world identity) gets CIDR-based rules (`fromCIDR` / `toCIDR`) with /32 addresses instead of endpoint selectors, because you can't exactly match a label on the internet.

### With `--l7` (opt-in HTTP + DNS)

When `--l7` is set and Hubble is producing L7 flow records (see the [L7 guide](l7-guide.md)), cpg attaches HTTP method/path and DNS `toFQDNs` to the relevant rules. Same fixture as above plus an observed `GET /api/v1/users` and a DNS query for `api.example.com`:

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: cpg-api-server
  namespace: production
spec:
  endpointSelector:
    matchLabels:
      app.kubernetes.io/name: api-server
  ingress:
    - fromEndpoints:
        - matchLabels:
            app: frontend
      toPorts:
        - ports:
            - {port: "8080", protocol: TCP}
          rules:
            http:
              - {method: GET, path: ^/api/v1/users$}
  egress:
    - toFQDNs:
        - matchName: api.example.com
      toPorts:
        - ports:
            - {port: "53", protocol: UDP}
            - {port: "53", protocol: TCP}
          rules:
            dns:
              - matchName: api.example.com
    # Companion kube-dns rule auto-injected for every CNP with toFQDNs (DNS-02).
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
              - matchName: api.example.com
```

HTTP paths are emitted as anchored, `regexp.QuoteMeta`'d RE2 regexes (`^/api/v1/users$`). Methods are uppercase-normalized. Header / Host rules are never generated (anti-feature, see [Limitations](#limitations)).

## Label selection

Labels are chosen with a priority hierarchy:

1. `app.kubernetes.io/name` if present (Kubernetes standard)
2. `app` if present (common convention)
3. All labels minus a denylist (pod-template-hash, controller-revision-hash, etc.)

This means generated policies survive rolling updates and don't accidentally pin to a specific ReplicaSet.

## Deduplication

cpg tries hard not to waste your time:

- **File dedup**: if the merged result is identical to what's already on disk, it skips the write.
- **Cross-flush dedup**: if the same policy was written in a previous flush cycle, it's not rewritten.
- **Cluster dedup** (`--cluster-dedup`): fetches live CiliumNetworkPolicies from the cluster and skips policies that already match. Needs `list` RBAC on `ciliumnetworkpolicies.cilium.io`.

## Unhandled flows

Not every dropped flow can become a policy rule. cpg reports what it skips so you can investigate:

- **INFO summary** at each flush cycle -- structured counters by skip reason
- **DEBUG detail** per unique flow -- logged once, with source, destination, port, protocol, and destination labels

Enable debug logging to see individual flows:

```bash
cpg --debug generate -n production
# or
cpg --log-level debug generate -n production
```

### Skip reasons

| Reason | What it means |
|--------|---------------|
| `no_l4` | Flow has no L4 layer (no port/protocol info) |
| `nil_endpoint` | Source or destination endpoint is nil |
| `empty_namespace` | Target endpoint has no namespace (non-reserved identity) |
| `nil_source` | Ingress flow with nil source endpoint |
| `nil_destination` | Egress flow with nil destination endpoint |
| `unknown_protocol` | L4 layer present but protocol not TCP/UDP/ICMP |
| `world_no_ip` | World (external) traffic without an IP address |

### Example output

At INFO level (default):

```
INFO  Unhandled flows summary  {"no_l4": 42, "nil_endpoint": 8, "world_no_ip": 3}
```

At DEBUG level:

```
DEBUG Unhandled flow  {"src": "default/nginx", "dst": "kube-system/coredns", "port": "53", "proto": "UDP", "reason": "no_l4", "dst_labels": ["k8s:app=coredns"]}
```

Reserved identity flows (like `reserved:host` or `reserved:kube-apiserver`) are reported separately as WARN logs with guidance to use CiliumClusterwideNetworkPolicy instead.

## Limitations

Honest ones:

- **L4 by default; L7 opt-in via `--l7`.** Without the flag, cpg generates port-level policies (v1.1 byte-stable). With `--l7`, cpg attaches HTTP `method` + anchored regex `path` and DNS `toFQDNs` (literal `matchName`) to the matching rules. Several L7 features are intentionally deferred to v1.3:
  - No HTTP `Headers` / `Host` / `HostExact` rules (anti-feature: secret leakage into committed YAML).
  - No HTTP path templating / auto-collapse — one rule per observed `(method, path)` pair (`--l7-collapse-paths`, HTTP-FUT-01).
  - No DNS `matchPattern` glob inference — only literal `matchName` (`--l7-fqdn-wildcard-depth`, DNS-FUT-01).
  - No FQDN inference from L4-to-IP correlation (DNS-FUT-03).
  - REFUSED DNS denials surface as `Verdict_FORWARDED` and are missed (`--include-l7-forwarded`, L7-FUT-01).

  See the [L7 guide](l7-guide.md) for the two-step workflow and the starter visibility CNP.
- **No auto-apply.** cpg writes YAML files. Applying them is your job, presumably through whatever GitOps tooling you already have. This is intentional -- auto-applying network policies in production is how you get paged at 3am.
- **Namespace-scoped only.** It generates CiliumNetworkPolicy, not CiliumClusterwideNetworkPolicy. Cluster-wide policies are typically hand-crafted by platform teams who know what they're doing (allegedly).
- **Named ports aren't resolved.** You get port numbers, not service port names. Port 8080 is port 8080. Less ambiguity, more grep-ability.

The full list with workarounds and tracking IDs lives in [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md).
