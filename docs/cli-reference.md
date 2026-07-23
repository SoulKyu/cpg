# CLI reference

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
      --include-audit        Also ingest Verdict_AUDIT flows alongside DROPPED (opt-in).
                             Default: DROPPED-only (pre-v1.6 behavior unchanged).

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

`cpg replay` shares `--output-dir`, `--cluster-dedup`, `--flush-interval`, `--ignore-protocol`,
`--ignore-drop-reason`, `--fail-on-infra-drops`, and `--include-audit` with `generate` — they
work identically. It also accepts `-` to read from stdin and transparently decompresses `.gz` files.

Evidence flags (`--evidence-dir`, `--no-evidence`, `--evidence-samples`, `--evidence-sessions`)
and the `cpg explain` filters are covered in [Explain & evidence](explain.md).

## Dry-run

Preview what `generate` or `replay` would write without touching any file:

```bash
cpg replay drops.jsonl --dry-run           # with unified diff
cpg replay drops.jsonl --dry-run --no-diff # log-only
cpg generate -n production --dry-run
```

In `--dry-run` mode, all stages of the pipeline run normally: you still see unhandled-flow warnings, cluster-dedup hits, and aggregation logs. Only the filesystem write step is suppressed. When an existing file would change, a unified diff is printed to stdout (colored on a tty, plain otherwise).

## Exit codes

| Code | Meaning |
|------|-------------------------------------------------------------------------|
| 0    | Success — policies generated (or previewed). Default even with infra drops. |
| 1    | `--fail-on-infra-drops` was set **and** ≥1 infra drop was observed. |

Any other non-zero exit means cpg encountered a fatal error (connection
failure, bad flag, etc.).

## CI / cron integration

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
