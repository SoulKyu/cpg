# cpg Bootstrap & Audit-Mode Onboarding Runbook

> **Do not enable daemon-wide `policy-audit-mode`.** Setting `policy-audit-mode: "true"` in the
> cluster-wide `cilium-config` ConfigMap and restarting the `cilium` DaemonSet disables policy
> enforcement for **every endpoint on every node** for as long as it stays set -- not just the
> workload you are onboarding. On a cluster already running default-deny, that is a fleet-wide
> hole: anything already inside the mesh gets unrestricted network access to everything the
> daemon manages until someone remembers to flip it back. Never do this on a production cluster,
> and avoid it even in staging unless the entire cluster is isolated for the duration. This
> runbook mentions `policy-audit-mode` here, in this warning, and nowhere else -- every
> actionable step below uses the **per-endpoint** form instead, which scopes the audit window to
> a single `CiliumEndpoint` and leaves every other endpoint's enforcement untouched.

This runbook mirrors the phase order of Cilium's own
["Creating Policies from Verdicts"](https://docs.cilium.io/en/stable/security/policy-creation/)
guide, adapted to `cpg`'s bootstrap + generate workflow: bootstrap a namespace to default-deny
first, then use per-endpoint audit mode plus `cpg generate --include-audit` to grow the policy
from observed traffic instead of hand-writing it.

## Prerequisites

- `cpg` installed and on `PATH` (see the main [README](../README.md#install)).
- A working `kubeconfig` pointed at the target cluster, with the same RBAC `cpg generate` already
  needs (`pods` list/get, `ciliumnetworkpolicies` read, and -- for this runbook -- `create`/
  `apply` once you're ready to land the generated policy).
- Cilium **>= 1.16** on the target cluster. `cpg bootstrap` detects the cluster's Cilium version
  and refuses to emit an artifact below that floor, because the `enableDefaultDeny` field it
  relies on is silently pruned by the CRD schema on older clusters (see the
  [Supported Cilium versions](../README.md#supported-cilium-versions) table). If version
  detection fails (no reachable cluster), `cpg bootstrap` warns and proceeds -- useful for
  offline/CI artifact generation, but confirm the target cluster's version yourself before
  applying anything it produces.

## Choose Your Order: Live Traffic vs Fresh Namespace

The two first steps below -- applying the default-deny bootstrap policy and opening the
per-endpoint audit window -- are order-independent commands, and which one you run first
matters a lot on a namespace with live traffic:

- **Namespace with live traffic (recommended order: audit window FIRST).** Open
  `cpg audit-window` *before* applying the bootstrap policy. Flipping an endpoint's
  `PolicyAuditMode` is a no-op until a policy actually matches, so the flips are harmless --
  and the moment the default-deny policy lands, every would-be-dropped flow becomes an `AUDIT`
  verdict instead of a real drop. **Zero real drops, end to end.** This mirrors the order of
  Cilium's own "Creating Policies from Verdicts" guide (audit mode first, then default-deny).
  Read the sections below in this order: [Enable Per-Endpoint Audit Mode](#enable-per-endpoint-audit-mode)
  first, then come back to [Bootstrap the Namespace](#bootstrap-the-namespace).
- **Fresh or scaled-down namespace (bootstrap first, the order written below).** With no live
  traffic there is nothing to drop, and applying the policy first means the namespace is
  enforced from the very first pod. This is the conservative default: if you walk away
  mid-runbook, the namespace converges to *protected*, not to *open*.

Either way, remember that **while the audit window is open the namespace is not enforced** --
traffic the default-deny would block is allowed (and logged as `AUDIT`). Keep the window as
short as your capture needs (`--ttl` bounds it), whichever order you chose.

## Bootstrap the Namespace

Generate the namespaced default-deny `CiliumNetworkPolicy` and apply it directly:

```bash
cpg bootstrap -n <namespace> | kubectl apply -f -
```

This emits a single CNP (`default-deny-<namespace>`) carrying `spec.enableDefaultDeny` **and**
explicit empty-rule `ingress`/`egress` stanzas -- both are required for the policy to actually
enforce default-deny (an `enableDefaultDeny` field with no rule stanzas at all is a known no-op
footgun, cilium/cilium#35558). Once applied, every pod in `<namespace>` starts from zero implicit
access: exactly the state the rest of this runbook safely fills in.

Prefer to review before applying? Redirect the artifact to a file instead of piping it
(`cpg bootstrap -n <namespace> > default-deny.yaml`), inspect it, then `kubectl apply -f` it
yourself. The
[MCP](../README.md#mcp-server-cpg-mcp) `get_bootstrap_policy` tool returns the same YAML as
read-only tool-result content, for harnesses that want to inspect it programmatically before an
operator applies it.

## Deploy / Scale Considerations

Bootstrapping default-deny on a namespace with live traffic immediately blocks anything not yet
covered by a policy -- **unless the audit window is already open** (the recommended live-traffic
order above), in which case those flows surface as `AUDIT` verdicts instead of drops. Before
applying:

- On live traffic, open the audit window first (see
  [Choose Your Order](#choose-your-order-live-traffic-vs-fresh-namespace)) -- that is what makes
  the apply drop-free.
- If you bootstrap first anyway and the namespace runs a workload you can safely scale down (a
  canary replica, a low-traffic background job), do that -- it shrinks the blast radius of the
  initial default-deny window while you build up policies from observed drops.
- If you bootstrap first on anything user-facing, expect drops immediately after
  `kubectl apply` until the audit window below is open.
- One namespace per `cpg bootstrap` invocation -- loop your shell over namespaces if you're
  onboarding several. There is no cluster-wide bootstrap mode on any code path.

## Enable Per-Endpoint Audit Mode

Run `cpg audit-window` to enable `PolicyAuditMode` on the endpoints you're onboarding -- this
reports policy-verdict violations without dropping traffic, so you can observe what the
default-deny namespace needs before it starts actually blocking. On a live-traffic namespace,
run this *before* applying the bootstrap policy (see
[Choose Your Order](#choose-your-order-live-traffic-vs-fresh-namespace)) -- the flips are
no-ops until the policy lands, and the apply then produces `AUDIT` verdicts instead of drops:

```bash
cpg audit-window -n <namespace> --ttl 30m
```

This is a foreground, supervised command -- leave it running in its own terminal for the
duration of your onboarding capture (the next two sections). It discovers every
`CiliumEndpoint` already in `<namespace>`, flips `PolicyAuditMode` on each one via `pods/exec`
into that endpoint's node's cilium-agent pod (the same passthrough `kubectl exec` uses), and
keeps watching for newly-created endpoints in the namespace so they get flipped too as they
appear. Stop it -- Ctrl+C, SIGTERM, or let `--ttl` expire -- and it reverts every endpoint it
flipped, on every exit path, before it exits: there is no separate manual disable step to
forget (see below). It never touches an endpoint that was already in audit mode before it
started, and it never touches the daemon-wide audit mode setting.

**NEW-ENDPOINT RACE (documented, not solved):** a pod whose `CiliumEndpoint` object `cpg
audit-window` has not yet observed and flipped is still in enforcing mode for that brief
interval -- traffic it generates during the race window can be dropped instead of audited. The
watch typically reacts within the same second the `CiliumEndpoint` appears, but there is no hard
upper bound on that lag (apiserver load, watch reconnects after a transient disconnect). If you
need airtight audit coverage for a bursty scale-up, wait a few seconds after scaling before
generating the traffic you want captured.

**RBAC:** `cpg audit-window` needs `pods/exec` (create, in `kube-system`) to reach each node's
cilium-agent, and `ciliumendpoints` (list/watch) to discover endpoints -- a step-up beyond
anything else this runbook or `cpg generate` needs. See the main README's
[Readonly by default](../README.md#readonly-by-default) section for the full RBAC posture and
its scoping limitation.

If you need the underlying `$ENDPOINT`/`$CILIUM_POD` values for the Hubble observation step
below (independent of `cpg audit-window`'s own internal exec calls):

```bash
ENDPOINT=$(kubectl get cep -n <namespace> <pod-name> -o jsonpath='{.status.id}')
CILIUM_POD=$(kubectl -n kube-system get pod -l k8s-app=cilium \
  --field-selector spec.nodeName=<node-name> -o jsonpath='{.items[0].metadata.name}')
```

## Observe Policy Verdicts

With per-endpoint audit mode on, watch policy verdicts for the endpoint via Hubble:

```bash
kubectl -n kube-system exec "$CILIUM_POD" -c cilium-agent -- \
  hubble observe flows -t policy-verdict --pod <namespace>/<pod-name> --last 20
```

Verdicts show as `AUDIT` while the endpoint is in audit mode -- this is traffic that the
bootstrapped default-deny policy *would* have dropped in enforcing mode. Confirm every audited
flow is expected traffic before moving on; anything unexpected is worth investigating rather than
blindly allow-listing.

## Capture with cpg generate --include-audit

Point `cpg` at the same cluster and capture with `--include-audit` so it ingests these `AUDIT`
verdicts alongside any hard `DROPPED` flows from endpoints not yet in audit mode:

```bash
cpg generate -n <namespace> --include-audit
```

In short: `cpg generate --include-audit` (add `-n <namespace>` / `--all-namespaces` as usual).
`--include-audit` is opt-in -- the default stays `DROPPED`-only, matching pre-audit-onboarding
behavior -- so set it explicitly whenever you're growing policy from an audit-mode window. `cpg
replay <file> --include-audit` works the same way against a saved capture. Either form produces
the same per-workload `CiliumNetworkPolicy` YAML `cpg generate` always writes.

## Create and Apply Generated Policies

Review the generated YAML in `./policies/<namespace>/` (or wherever `-o/--output-dir` pointed),
then apply it alongside the bootstrap CNP from step one:

```bash
kubectl apply -f ./policies/<namespace>/
```

The generated per-workload policies and the `default-deny-<namespace>` bootstrap policy coexist
-- Cilium computes the union of all matching CNPs for a given endpoint, so the generated allow
rules now widen exactly the paths that were observed, while the bootstrap policy keeps everything
else denied by default.

## Disable Per-Endpoint Audit Mode

There is no separate disable step to remember. Once the generated policy covers the traffic you
observed, stop `cpg audit-window` -- Ctrl+C in its terminal, SIGTERM, or let `--ttl` expire --
and it reverts `PolicyAuditMode` on every endpoint it flipped, on every exit path, before it
exits. The endpoint starts enforcing again as soon as the revert lands.

## Verify Enforcement

Confirm the endpoint is out of audit mode and traffic covered by the generated policy is being
allowed (not just audited):

```bash
kubectl -n kube-system exec "$CILIUM_POD" -c cilium-agent -- \
  cilium-dbg endpoint get "$ENDPOINT" -o jsonpath='{[*].spec.options.PolicyAuditMode}'
# expect: Disabled

kubectl -n kube-system exec "$CILIUM_POD" -c cilium-agent -- \
  hubble observe flows -t policy-verdict --pod <namespace>/<pod-name> --last 5
# expect: ALLOW/DROP verdicts, no more AUDIT
```

Traffic your generated policy covers should show `ALLOWED`; anything genuinely unexpected should
now show `DROPPED` -- exactly the enforcing behavior the bootstrap CNP promised in step one.

## Clean-up

If this was a one-off exercise (a demo namespace, a throwaway cluster), remove what you applied:

```bash
kubectl delete -f ./policies/<namespace>/
kubectl delete cnp -n <namespace> default-deny-<namespace>
```

For a real onboarding, leave both the bootstrap CNP and the generated policies in place -- they
are now your namespace's default-deny baseline and its GitOps-tracked allow rules, respectively.
Commit the generated YAML to your policy repo the same way you would after any other `cpg
generate` run.
