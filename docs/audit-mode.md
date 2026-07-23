# Audit mode

This page explains how cpg's audit-mode support works and how the pieces fit together.
For the step-by-step onboarding procedure, use the [bootstrap runbook](bootstrap-runbook.md).

## What audit mode is

Cilium's `PolicyAuditMode` is a per-endpoint switch: when enabled, policy violations are
**reported instead of enforced**. Traffic that a matching policy would drop keeps flowing,
and Hubble surfaces it as an `AUDIT` policy verdict instead of `DROPPED`.

That is exactly the signal cpg wants when onboarding a namespace to default-deny: apply the
deny policy, let real traffic run, and read the `AUDIT` verdicts as the list of allow rules
you still need — with **zero real drops** along the way.

> **Never enable daemon-wide `policy-audit-mode`.** The cluster-wide ConfigMap flag disables
> enforcement for every endpoint on every node until someone flips it back. Everything cpg
> does uses the **per-endpoint** form, scoped to the endpoints being onboarded. The
> [runbook](bootstrap-runbook.md) opens with the full warning.

## The three building blocks

Milestone v1.6 shipped audit-mode support as three composable pieces:

| Piece | Role |
|-------|------|
| `cpg bootstrap -n <ns>` | Emits the namespaced default-deny CiliumNetworkPolicy (stdout-only, you apply it) |
| `cpg audit-window -n <ns> --ttl 30m` | Opens a managed, TTL-bounded per-endpoint audit window — the one mutating command |
| `--include-audit` | Makes `cpg generate` / `cpg replay` ingest `Verdict_AUDIT` flows alongside `DROPPED` |

Combined: open the window, apply default-deny, capture with `--include-audit`, review and
apply the generated policies, close the window. The [runbook](bootstrap-runbook.md) walks
each step in order (and explains why the order differs between a live-traffic namespace and
a fresh one).

## `cpg audit-window` lifecycle

`cpg audit-window` is a foreground, supervised command:

1. **Discover** every `CiliumEndpoint` in the target namespace.
2. **Flip** `PolicyAuditMode` on each one, via `pods/exec` into that endpoint's node's
   cilium-agent pod (the same passthrough `kubectl exec` uses; WebSocket transport with
   SPDY fallback, matching kubectl 1.30+).
3. **Watch** for newly-created endpoints in the namespace and flip those too as they appear.
4. **Revert** every flip it made — on Ctrl+C, on SIGTERM, on `--ttl` expiry, on any exit
   path — before the process exits. There is no separate disable step to forget.

Guarantees worth knowing:

- **Always bounded.** The window has a TTL on every run (default 30m); there is no
  unbounded mode. When it expires, the reverts run and the command exits.
- **Never touches pre-existing audit state.** An endpoint already in audit mode before the
  window opened is left exactly as found.
- **Never touches the daemon-wide setting.** Only per-endpoint flips, only in the target
  namespace.
- **Reverts are reported.** If a revert cannot land before the shutdown deadline, the
  affected endpoints are reported as possibly stuck rather than silently abandoned —
  verify those manually (the runbook's [Verify Enforcement](bootstrap-runbook.md#verify-enforcement)
  section shows the `cilium-dbg` check).

### The new-endpoint race (documented, not solved)

A pod whose `CiliumEndpoint` the watcher has not yet observed and flipped is still enforcing
for that brief interval — its traffic can be dropped instead of audited. The watch typically
reacts within the same second, but there is no hard upper bound (apiserver load, watch
reconnects). For bursty scale-ups, wait a few seconds after scaling before generating the
traffic you want captured.

### RBAC

`cpg audit-window` is the one command needing a step-up beyond readonly: `pods/exec`
(create, in `kube-system`) and `ciliumendpoints` (list/watch). The
[security model](security.md#rbac-requirements) covers the grants and the scoping
limitation that comes with `pods/exec`.

## Capturing audit verdicts

`--include-audit` is **opt-in** on both capture surfaces — the default stays `DROPPED`-only,
matching pre-v1.6 behavior:

```bash
cpg generate -n <namespace> --include-audit
cpg replay drops.jsonl --include-audit -n <namespace>
```

On the MCP server, `start_session` accepts the equivalent `include_audit` parameter, and the
session's final summary surfaces the audit-verdict count alongside the other session stats —
see the [MCP server guide](mcp-server.md).

If `--include-audit` is set but zero `AUDIT` flows materialize while flows were observed,
cpg emits a single warning at the end of the run (AUD-01) — the usual cause is capturing
before the default-deny policy was applied (flips are no-ops until a policy matches) or
after the audit window closed.

## Version requirements

| Capability | Cilium version |
|------------|----------------|
| `PolicyVerdictNotify` audit-action bit | >= 1.8 |
| `Verdict_AUDIT` via the Hubble flow API (`--include-audit`) | >= 1.10 |
| `enableDefaultDeny` CNP field (`cpg bootstrap`) | >= 1.16 |

Full matrix with PR citations in the
[Supported Cilium versions](installation.md#supported-cilium-versions) table.

## Related

- [Bootstrap runbook](bootstrap-runbook.md) — the step-by-step onboarding procedure
- [Security model](security.md) — why audit-window is the single mutating command
- [Getting started](getting-started.md#audit-mode-onboarding-default-deny-with-zero-real-drops) — the five-command short version
