# cpg

**Cilium Policy Generator** -- because writing CiliumNetworkPolicies by hand in a default-deny cluster is nobody's idea of a good Friday night.

## Code quality

![desloppify scorecard](docs/scorecard.png)

Tracked with [desloppify](https://github.com/peteromallet/desloppify) — strict score, regenerated on each code-health pass.

`cpg` connects to Hubble Relay, watches dropped flows in real time, and generates the CiliumNetworkPolicy YAML files that would allow them. You run it, wait for traffic to get denied, and it writes the fix. Then you review, commit, and apply through your GitOps pipeline like a responsible adult.

## The problem

You've deployed Cilium with default-deny. Good for you. Now every new service, every port change, every cross-namespace call gets blocked until someone writes the right policy YAML by hand. You stare at `hubble observe --verdict DROPPED`, translate flow fields into Cilium API objects in your head, and pray you got the label selectors right.

Or you let `cpg` do it.

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

## Install

```bash
# kubectl krew
kubectl krew install cilium-policy-gen

# go install
go install github.com/SoulKyu/cpg/cmd/cpg@latest
```

Or build from source:

```bash
git clone https://github.com/SoulKyu/cpg.git
cd cpg
make build
# binary lands in ./bin/cpg
```

When installed via krew, use `kubectl cilium-policy-gen` instead of `cpg`. Same flags, same behavior.

Requires Go 1.25+ for source builds.

## Quick start

```bash
# Point at a namespace. cpg auto port-forwards to hubble-relay.
cpg generate -n production

# Explicit relay address
cpg generate --server localhost:4245

# All namespaces, debug logging
cpg --debug generate --all-namespaces

# TLS
cpg generate --server relay.example.com:443 --tls -n production

# Opt-in L7: HTTP method/path + DNS matchName. See "L7 Prerequisites".
cpg generate -n production --l7
```

That's it. Leave it running. Go generate some traffic (or wait for someone else to). Ctrl+C when you're done -- cpg flushes remaining flows and prints a session summary before exiting.

Policies show up in `./policies/<namespace>/<workload>.yaml`.

## Quick start (offline replay)

Prefer to iterate on policy generation without reproducing traffic? Capture once, replay many:

```bash
# Capture dropped flows for N minutes
hubble observe --output jsonpb --follow > drops.jsonl
# Ctrl+C when done capturing

# Replay through cpg — reuse the file as many times as you want
cpg replay drops.jsonl -n production

# Opt-in L7: HTTP method/path + DNS matchName. See "L7 Prerequisites".
cpg replay drops.jsonl --l7 -n production
```

`cpg replay` accepts `-` to read from stdin and transparently decompresses `.gz` files.

## Flags

```
cpg generate [flags]

Connection:
  -s, --server string        Hubble Relay address (auto port-forward if omitted)
      --tls                  Enable TLS for gRPC connection
      --timeout duration     Connection timeout (default 10s)

Filtering:
  -n, --namespace strings    Namespace filter (repeatable)
  -A, --all-namespaces       Observe all namespaces
      --ignore-protocol strs Drop flows whose L4 protocol matches; repeatable / comma-separated.
                             Valid: tcp, udp, icmpv4, icmpv6, sctp (case-insensitive)
      --ignore-drop-reason strs
                             Exclude flows by Cilium drop reason name before classification;
                             repeatable / comma-separated / case-insensitive.
                             Passing a reason already classified as infra or transient emits
                             a warning (it is already suppressed by default).

CI integration:
      --fail-on-infra-drops  Exit with code 1 when ≥1 infra drop is observed (default:
                             always exit 0). Use in CI/cron pipelines to alert on cluster
                             health issues.

Output:
  -o, --output-dir string    Output directory (default "./policies")

Aggregation:
      --flush-interval dur   Aggregation flush interval (default 5s)

Deduplication:
      --cluster-dedup        Skip policies matching live cluster state (needs RBAC)

Global:
      --debug                Debug logging
      --log-level string     Log level: debug, info, warn, error (default "info")
      --json                 JSON log format
```

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

When `--l7` is set and Hubble is producing L7 flow records (see [L7 Prerequisites](#l7-prerequisites)), cpg attaches HTTP method/path and DNS `toFQDNs` to the relevant rules. Same fixture as above plus an observed `GET /api/v1/users` and a DNS query for `api.example.com`:

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

HTTP paths are emitted as anchored, `regexp.QuoteMeta`'d RE2 regexes (`^/api/v1/users$`). Methods are uppercase-normalized. Header / Host rules are never generated (anti-feature, see Limitations).

## Offline replay

`cpg replay <file>` feeds a Hubble jsonpb capture through the same pipeline as the live stream. It is the right tool when you want:

- **Deterministic iteration.** Re-run the same input as you tweak label selection, dedup logic, or flush intervals.
- **Offline workflow.** Capture on a jumphost, replay on your laptop.
- **Post-mortem reproduction.** Keep the capture alongside the policy in your GitOps repo so anyone can reproduce what cpg saw.

Capture:

```bash
hubble observe --output jsonpb --follow > drops.jsonl
```

Replay:

```bash
cpg replay drops.jsonl -n production
cpg replay drops.jsonl.gz -n production    # gzip transparent
cat drops.jsonl | cpg replay -              # stdin
```

Flags shared with `generate` (`--output-dir`, `--cluster-dedup`, `--flush-interval`, `--ignore-protocol`, `--ignore-drop-reason`, `--fail-on-infra-drops`) work identically. Non-DROPPED verdicts and malformed lines are skipped with counters surfaced in the session summary.

## L7 Prerequisites <a id="l7-prerequisites"></a>

`cpg --l7` translates what Hubble shows it. Hubble only emits `Flow.L7`
records (HTTP method/path, DNS query) when traffic is proxied — by Envoy
for HTTP, by the DNS proxy for DNS. cpg cannot turn that on for you.
If you see the warning

```
--l7 set but no L7 records observed
```

this section is the fix.

### Two-step workflow

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

### Three ways to enable L7 visibility

1. **Recommended for ad-hoc bootstrap — proxy-visibility annotation.**
   The legacy but still widely supported (Cilium ≤ 1.19) workload-level
   annotation that triggers Envoy / DNS proxy redirection without
   enforcing rules:

   ```bash
   kubectl annotate pod -n <ns> -l app.kubernetes.io/name=<workload> \
     policy.cilium.io/proxy-visibility='<Egress/53/UDP/DNS>,<Ingress/8080/TCP/HTTP>'
   ```

   Easy to apply, easy to remove. Marked deprecated upstream — track its
   deprecation if you build long-term tooling on it.

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

### Starter L7-visibility CNP

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

### Capture-window guidance

Run cpg long enough to capture one full traffic cycle for the
workloads in question. A single observation produces a single rule
with `flow_count=1`, which `cpg explain` surfaces as low-confidence
evidence. For periodic batch jobs, capture across at least one period.

### Known v1.2 limitations

cpg v1.2 ships with a documented set of known limitations and edge cases — most are intentional trade-offs (e.g., no HTTP header rules to avoid secret leakage), a few are deferred to v1.3+. Read them **before deploying generated policies** to production:

→ **[docs/KNOWN_LIMITATIONS.md](docs/KNOWN_LIMITATIONS.md)** — full list with workarounds and tracking IDs.

Highlights worth knowing up front:

- L7 visibility prerequisite — `--l7` requires Cilium Envoy proxy + per-workload visibility trigger; cpg cannot bootstrap it (limitation #1).
- HTTP path explosion on REST APIs with IDs — one literal rule per observed `(method, path)`; no auto-collapse in v1.2 (limitation #2, `HTTP-FUT-01`).
- Header-based rules never generated — anti-feature to prevent secret leakage (limitation #3).
- DNS REFUSED denials are missed — `Verdict_FORWARDED` not yet supported (limitation #4, `L7-FUT-01`).
- kube-dns companion selector hardcoded `k8s-app=kube-dns` — autodetect across CNI distributions deferred to v1.3 (limitation #6, `DNS-FUT-02`).

## Dry-run

Preview what `generate` or `replay` would write without touching any file:

```bash
cpg replay drops.jsonl --dry-run           # with unified diff
cpg replay drops.jsonl --dry-run --no-diff # log-only
cpg generate -n production --dry-run
```

In `--dry-run` mode, all stages of the pipeline run normally: you still see unhandled-flow warnings, cluster-dedup hits, and aggregation logs. Only the filesystem write step is suppressed. When an existing file would change, a unified diff is printed to stdout (colored on a tty, plain otherwise).

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

## Explain policies

After a run, every emitted rule has per-flow evidence recorded alongside the YAML. Inspect it with `cpg explain`:

```bash
cpg explain production/api-server
cpg explain production/api-server --peer app=frontend
cpg explain production/api-server --ingress --port 8080
cpg explain ./policies/production/api-server.yaml --since 1h --json

# L7 filters (literal exact match, AND-combined). Require evidence captured with --l7.
cpg explain production/api-server --http-method GET
cpg explain production/api-server --http-method GET --http-path '^/api/v1/users$'
cpg explain production/api-server --dns-pattern api.example.com
```

`--http-path` matches the literal regex stored in evidence — that is, the
anchored, `regexp.QuoteMeta`'d form produced by the builder
(`^/api/v1/users$`), not the raw observed path. `--dns-pattern` matches the
literal `matchName` stored in evidence (trailing dot stripped); v1.2 emits
no wildcards, so passing `*.example.com` will simply not match anything.
When any L7 filter is set, L4-only rules (no L7Ref in evidence) are
excluded from the result.

When the evidence carries L7 attribution, `cpg explain` renders it
inline. Text format prints a single indented line per rule
(`L7: HTTP GET /api/v1/users` or `L7: DNS api.example.com`); JSON and
YAML formats include an `l7` sub-object on each rule with the relevant
fields (`protocol`, `http_method`, `http_path`, `dns_matchname`).

Example output:

```
Policy: cpg-api-server (production)
Latest session: 2026-04-24 14:02 → 14:15 (source: replay)

Ingress rule
  Peer:        app=frontend (endpoint)
  Port:        8080/TCP
  Flow count:  23
  First seen:  2026-04-24 14:02:11
  Last seen:   2026-04-24 14:15:48

  Sample flows:
    14:02:11  default/frontend → production/api-server  TCP/8080
    14:02:13  default/frontend → production/api-server  TCP/8080
    ...
```

### Where is evidence stored?

Evidence lives outside the output directory to keep GitOps clean:

- **Linux:** `$XDG_CACHE_HOME/cpg/evidence` (defaults to `~/.cache/cpg/evidence`)
- **macOS:** `~/Library/Caches/cpg/evidence`

The path is keyed by a hash of the absolute output directory, so multiple workspaces coexist without collision.

To share evidence with a colleague or archive it:

```bash
cpg replay drops.jsonl -n production --evidence-dir ./evidence
# ... ship ./evidence alongside the policies
cpg explain production/api-server --evidence-dir ./evidence
```

Disable capture with `--no-evidence`. Tune retention per rule with `--evidence-samples` (default 10) and per policy with `--evidence-sessions` (default 10).

## Label selection

Labels are chosen with a priority hierarchy:

1. `app.kubernetes.io/name` if present (Kubernetes standard)
2. `app` if present (common convention)
3. All labels minus a denylist (pod-template-hash, controller-revision-hash, etc.)

This means generated policies survive rolling updates and don't accidentally pin to a specific ReplicaSet.

## Auto port-forward

When you omit `--server`, cpg finds the `hubble-relay` pod in `kube-system` using your kubeconfig and sets up a port-forward automatically. One less terminal tab to manage.

## k9s plugin

You can trigger cpg directly from k9s on a namespace. Drop this into `$XDG_CONFIG_HOME/k9s/plugins.yaml` (usually `~/.config/k9s/plugins.yaml`):

```yaml
plugins:
  cpg:
    shortCut: Shift-G
    description: Generate Cilium policies from dropped flows
    scopes:
    - namespace
    command: cpg
    background: false
    args:
    - generate
    - -n
    - $NAME
    - --cluster-dedup
```

Navigate to a namespace in k9s, press `Shift-G`, and cpg starts streaming dropped flows for that namespace. Ctrl+C to stop -- policies land in `./policies/<namespace>/`.

If you installed via krew instead of `go install`, replace `command: cpg` with `command: kubectl` and prepend `cilium-policy-gen` to the args:

```yaml
plugins:
  cpg:
    shortCut: Shift-G
    description: Generate Cilium policies from dropped flows
    scopes:
    - namespace
    command: kubectl
    background: false
    args:
    - cilium-policy-gen
    - generate
    - -n
    - $NAME
    - --cluster-dedup
```

## Project structure

```
cmd/cpg/           CLI entrypoint (cobra): generate, replay, explain
pkg/labels/        Label selection, denylist, endpoint/peer selector builders
pkg/policy/        Flow-to-CiliumNetworkPolicy builder, merge, semantic dedup, attribution
pkg/output/        Directory-organized YAML writer with merge-on-write
pkg/hubble/        Live gRPC client, aggregator, pipeline orchestration
pkg/k8s/           Kubeconfig loading, port-forward, cluster policy fetching
pkg/flowsource/    Flow stream abstraction: live gRPC or jsonpb file source
pkg/evidence/      Per-rule flow attribution (cpg explain)
pkg/diff/          Unified YAML diff (cpg generate/replay --dry-run)
```

## Development

```bash
make test          # run tests with race detector
make lint          # golangci-lint
make build         # build binary to ./bin/cpg
make all           # lint + test + build
```

The test suite covers label selection, policy building, merging, output writing, flow aggregation, pipeline orchestration, and dedup logic. No live cluster required -- the Hubble gRPC client is mocked via interfaces.

## Exit codes

| Code | Meaning |
|------|-------------------------------------------------------------------------|
| 0    | Success — policies generated (or previewed). Default even with infra drops. |
| 1    | `--fail-on-infra-drops` was set **and** ≥1 infra drop was observed. |

Any other non-zero exit means cpg encountered a fatal error (connection
failure, bad flag, etc.).

### CI / cron example

```bash
# Alert when infra drops appear in a captured window
cpg replay /tmp/last-hour.jsonl --fail-on-infra-drops \
  || alert-team "cpg detected infra drops — check cluster-health.json"
```

With `cpg generate` (live stream — run for a fixed window with timeout):

> Note: `--preserve-status` ensures `timeout` propagates `cpg`'s exit code (0 vs 1) instead of
> returning 124 when the deadline is reached. Without it, a CI job that hits the timeout would
> mask whether infra drops were detected.

```bash
timeout --preserve-status 300 cpg generate -n production --fail-on-infra-drops \
  || alert-team "infra drops in production — see cluster-health.json"
```

## Limitations

Honest ones:

- **L4 by default; L7 opt-in via `--l7`.** Without the flag, cpg generates port-level policies (v1.1 byte-stable). With `--l7`, cpg attaches HTTP `method` + anchored regex `path` and DNS `toFQDNs` (literal `matchName`) to the matching rules. Several L7 features are intentionally deferred to v1.3:
  - No HTTP `Headers` / `Host` / `HostExact` rules (anti-feature: secret leakage into committed YAML).
  - No HTTP path templating / auto-collapse — one rule per observed `(method, path)` pair (`--l7-collapse-paths`, HTTP-FUT-01).
  - No DNS `matchPattern` glob inference — only literal `matchName` (`--l7-fqdn-wildcard-depth`, DNS-FUT-01).
  - No FQDN inference from L4-to-IP correlation (DNS-FUT-03).
  - REFUSED DNS denials surface as `Verdict_FORWARDED` and are missed (`--include-l7-forwarded`, L7-FUT-01).

  See [L7 Prerequisites](#l7-prerequisites) for the two-step workflow and the starter visibility CNP.
- **No auto-apply.** cpg writes YAML files. Applying them is your job, presumably through whatever GitOps tooling you already have. This is intentional -- auto-applying network policies in production is how you get paged at 3am.
- **Namespace-scoped only.** It generates CiliumNetworkPolicy, not CiliumClusterwideNetworkPolicy. Cluster-wide policies are typically hand-crafted by platform teams who know what they're doing (allegedly).
- **Named ports aren't resolved.** You get port numbers, not service port names. Port 8080 is port 8080. Less ambiguity, more grep-ability.

## License

Apache 2.0
