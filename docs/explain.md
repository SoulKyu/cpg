# Explain & evidence

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
literal `matchName` stored in evidence (trailing dot stripped); cpg emits
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

## Where is evidence stored?

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
