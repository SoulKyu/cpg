# MCP server (`cpg mcp`)

`cpg mcp` runs a readonly [MCP](https://modelcontextprotocol.io) server over stdio: your LLM harness spawns `cpg mcp` as a subprocess, and it exposes a single live Hubble capture session at a time. The LLM reads dropped flows, the CiliumNetworkPolicy YAML cpg generates, per-rule flow evidence, and cluster health through the tools below — cpg never mutates your cluster and never writes outside its own session tmpdir.

## Tool catalog

| Tool | Description |
|------|-------------|
| `start_session` | Start a live Hubble capture session in the background; returns an opaque `session_id` immediately. Only one session at a time — stop the current one before starting another. Accepts `include_audit` to also ingest `Verdict_AUDIT` flows alongside DROPPED (opt-in; default preserves pre-v1.6 DROPPED-only behavior). |
| `get_status` | Coarse session state (capturing/stopped), elapsed time, and on-disk artifact counts. Works for a stopped-but-retained session too. |
| `stop_session` | Cancel the capture, finalize `cluster-health.json` and session stats, and return the final summary. Idempotent — a second call returns the same summary, never an error. |
| `list_dropped_flows` | Paginated, two-section view of dropped flows: policy-actionable samples plus infra/transient/noise aggregate counts. Sampled/aggregated, not a raw flow log. |
| `list_policies` | Paginated metadata for every generated CiliumNetworkPolicy in the session — namespace, workload, rule counts, YAML path. |
| `get_policy` | Full CiliumNetworkPolicy YAML plus metadata for one namespace/workload pair (discover pairs via `list_policies`). |
| `get_evidence` | Paginated per-rule flow evidence for one policy, byte-identical in shape to `cpg explain --json`. |
| `get_cluster_health` | The session's finalized cluster-health report: per-drop-reason counts by node/workload, plus Cilium-docs remediation URLs. |
| `get_bootstrap_policy` | A namespaced default-deny CiliumNetworkPolicy as YAML (same artifact as `cpg bootstrap`), returned as read-only tool content with the detected Cilium version and compat verdict — no filesystem writes. |

## Harness configuration

```json
{
  "mcpServers": {
    "cpg": {
      "command": "cpg",
      "args": ["mcp"],
      "env": {
        "KUBECONFIG": "/home/you/.kube/config",
        "PATH": "/usr/local/bin:/usr/bin:/bin",
        "TMPDIR": "/tmp"
      }
    }
  }
}
```

MCP hosts do not inherit your shell environment — the `env` block above is not optional decoration, it is the only environment `cpg mcp` will ever see. Set each key explicitly:

- **`KUBECONFIG`** — cpg's client-go loader resolves clusters the same way `kubectl` does (`KUBECONFIG` env, then `~/.kube/config`, then in-cluster config). Omit it and the process silently falls back to whatever exists at the default path — which may not exist, or may point at the wrong cluster.
- **`PATH`** — if your kubeconfig authenticates via an `exec` credential plugin (see the caveat below), client-go shells out to a binary on `PATH`. Without it, that lookup fails.
- **`TMPDIR`** — the session's `os.MkdirTemp`-rooted working directory (policies, evidence, `cluster-health.json`) honors `$TMPDIR`. Leave it unset and the process falls back to the platform default, which is usually fine but worth setting explicitly if your host sandboxes `/tmp`.

## Secrets posture

With `--l7`-style visibility enabled on a session (see the [L7 guide](l7-guide.md)), HTTP paths and methods, FQDNs, and workload labels reach the LLM context through tool results — that's the query tools doing their job. `Authorization`, `Cookie`, and other headers are **never** captured; cpg has never generated or stored header-based rules, an anti-feature carried forward from v1.2 specifically to avoid this kind of leakage. There is no redaction pass in v1.5 — whatever the pipeline observes is what the LLM sees, unfiltered (a dedicated redaction feature is deferred to v2). If you're pointing an MCP session at a cluster carrying sensitive path, FQDN, or label data, know what crosses the boundary before you start the session.

## Exec-credential-plugin caveat

Kubeconfigs authenticating via an `exec` plugin — `aws eks get-token`, `gke-gcloud-auth-plugin`, `azure kubelogin`, and similar — expect an interactive terminal for re-auth (an expired SSO session, a first-time device-code flow). Under an MCP host, `cpg mcp`'s real stdin is the JSON-RPC channel, not a keyboard: an interactive prompt has nowhere to go, and `start_session` hangs instead of failing fast. Before wiring cpg into a harness, verify headless auth actually works — running `kubectl get pods` from a non-interactive shell (no TTY) is the fastest check — or point at a static, already-authenticated kubeconfig instead. `start_session`'s bounded setup timeout turns the common exec-plugin re-auth hang (during dial/port-forward) into an actionable error rather than a silent wait — the one known exception is a hang specifically inside kubeconfig load itself (`k8s.LoadKubeConfig()` takes no `ctx`), which neither this timeout nor cancellation can bound. One more thing worth knowing: some `exec`/OIDC plugins refresh and persist credentials back to the kubeconfig file on disk as a side effect of successful auth — expected client-go behavior, not something cpg itself does, but worth knowing if you otherwise treat that file as read-only.

## Session model

One capture session at a time — `start_session` returns an error if a session is already running. A stopped session isn't discarded: `get_status` and `stop_session` keep returning its final state, and the query tools keep serving its artifacts, until the next `start_session` call or the server process exits.

## Agent tooling

Repo-local Claude Code skills and agent under `.claude/` route an LLM operator through cpg-specific workflows on top of `cpg mcp`'s tool surface. Skills name workflow steps and tool names only — argument schemas and result shapes are always discovered live via `tools/list`, never restated in skill prose.

| Skill | Purpose |
|-------|---------|
| `cpg-triage` | Drive a live MCP session end-to-end: start capture, classify dropped flows, present each generated policy with its evidence, recommend what to apply. |
| `cpg-audit-onboard` | Guide onboarding a new namespace: bootstrap and audit-window (human-run CLI), drive capture via MCP, end with the human applying policy. |
| `cpg-policy-review` | Audit already-generated policies offline via `cpg explain` and evidence — over-broad rules, L7 anchoring, DNS-53 companions, dedup. |
| `cpg-health-report` | Turn a session's `cluster-health.json` into a self-contained HTML report of infra drops by node/workload with remediation links. |
| `cpg-mcp-smoke` | Post-release smoke test of a tagged binary's MCP server against the fake-relay e2e harness. |

`cpg-operator` (`.claude/agents/cpg-operator.md`) is the single repo-local agent that drives live MCP session lifecycles on behalf of `cpg-triage` and `cpg-audit-onboard` — it is not invoked directly.
