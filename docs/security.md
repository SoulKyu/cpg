# Security model

## Readonly by default

cpg is readonly by default. Every command -- `cpg generate`, `cpg replay`, `cpg bootstrap`,
`cpg explain`, and `cpg mcp` (see the [MCP server guide](mcp-server.md)) -- only lists,
watches, and reads Kubernetes and Hubble data. None of them writes to the cluster.

The one exception is `cpg audit-window`, the single scoped, lifecycle-bound mutating command:
it flips per-endpoint `PolicyAuditMode` on for the endpoints in a target namespace, watches for
newly-created endpoints and flips those too, then reverts every flip it made -- on Ctrl+C, on
`--ttl` expiry, on any exit path -- the moment the foreground command stops. It never touches an
endpoint that was already in audit mode before it started, and it never touches the daemon-wide
audit mode setting. See the [bootstrap runbook](bootstrap-runbook.md) for the full
`cpg audit-window` workflow, including its new-endpoint race window.

## RBAC requirements

`cpg audit-window` requires an RBAC step-up that no readonly command needs: `pods/exec` (create,
scoped to `kube-system`) to reach each node's cilium-agent pod, and `ciliumendpoints`
(list/watch) to discover and track endpoints. Grant both verbs only to whatever principal
actually runs `cpg audit-window`. The exec transport is WebSocket with SPDY fallback (same as
kubectl since 1.30+).

**RBAC scoping limitation:** Kubernetes RBAC `resourceNames` requires exact, static pod names,
but Cilium agent pod names are DaemonSet-generated (`cilium-<random-suffix>`) and not stable
across restarts -- there is no way to scope `pods/exec` to "only cilium-agent pods" by name.
The grant is effectively "exec into any pod in `kube-system`" for whatever role carries it. If
your cluster runs an admission controller (Kyverno, OPA Gatekeeper), consider policy-based
scoping there as defense-in-depth -- this is not something cpg itself enforces or guarantees.

Readonly commands need much less: the only documented extra grant is `--cluster-dedup`,
which needs `list` on `ciliumnetworkpolicies.cilium.io`.

## What never happens

- **No auto-apply.** cpg writes YAML files to disk; applying them is always a human act,
  presumably through your GitOps pipeline.
- **No HTTP header rules.** `Authorization`, `Cookie`, and other headers are never captured
  or turned into rules -- an anti-feature to prevent secret leakage into committed YAML.

## Related

- [MCP server guide](mcp-server.md) — secrets posture when exposing sessions to an LLM
- [Bootstrap runbook](bootstrap-runbook.md) — the full audit-window workflow and its RBAC details
