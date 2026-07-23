---
name: cpg-audit-onboard
description: >
  Guide an operator through onboarding a brand-new namespace into enforced
  default-deny network policy: bootstrap, per-endpoint audit mode, capture,
  and the human-applied enforce checklist. Use when a namespace has no
  default-deny CiliumNetworkPolicy yet and needs to be brought onto one
  safely. Do NOT use for triaging an already-running capture on a namespace
  that is already onboarded (see cpg-triage) or for offline review of
  existing policies (see cpg-policy-review).
---

# cpg-audit-onboard

Router for the full onboarding workflow documented in
[docs/bootstrap-runbook.md](../../../docs/bootstrap-runbook.md). This skill
GUIDES the human through the CLI steps that are human-run, DRIVES the capture
step via the `cpg-operator` agent, and ENDS with the human applying policy —
it never runs `cpg bootstrap`/`cpg audit-window` itself and never invokes an
apply tool, because none exists and none should be invented.

## Step 0: Check prerequisites

Point the operator at the runbook's
[Prerequisites](../../../docs/bootstrap-runbook.md#prerequisites) section:
`cpg` on `PATH`, a working `kubeconfig` with `create`/`apply` RBAC for the
target namespace, and Cilium >= 1.16 on the cluster. Confirm these before
proceeding — a stale Cilium version silently degrades `cpg bootstrap`'s
output rather than failing loudly.

## Step 0.5: Pick the order (live traffic vs fresh namespace)

Ask the operator one question before anything else: **does the namespace
carry live traffic?**

- **Live traffic → audit window FIRST, bootstrap second** (swap Steps 1
  and 2 below). Flipping `PolicyAuditMode` is a no-op until a policy
  matches, so opening the window first is harmless — and the bootstrap
  apply then produces `AUDIT` verdicts instead of real drops. Zero real
  drops, end to end.
- **Fresh or scaled-down namespace → bootstrap first** (the order as
  written). Nothing is running to drop, and the namespace is enforced from
  the very first pod.

See the runbook's
[Choose Your Order](../../../docs/bootstrap-runbook.md#choose-your-order-live-traffic-vs-fresh-namespace)
section. Either way, remind the operator the namespace is NOT enforced
while the window is open — keep the `--ttl` as short as the capture needs.

## Step 1: Guide bootstrap (human-run)

Instruct the operator to run, in their own terminal:

```bash
cpg bootstrap -n <namespace> | kubectl apply -f -
```

This is a human-run CLI step — this skill does not execute it. If the
operator wants to review before applying, tell them they can redirect to a
file first and `kubectl apply -f` it themselves; see the runbook's
[Bootstrap the Namespace](../../../docs/bootstrap-runbook.md#bootstrap-the-namespace)
section for both paths, plus the note on
[Deploy / Scale Considerations](../../../docs/bootstrap-runbook.md#deploy--scale-considerations)
if the namespace carries live traffic (in which case Step 2's window should
already be open per Step 0.5).

## Step 2: Guide per-endpoint audit mode (human-run)

Instruct the operator to run `cpg audit-window` in its own supervised
terminal for the duration of the onboarding capture:

```bash
cpg audit-window -n <namespace> --ttl <duration>
```

This skill does not run this command either — it is foreground and
supervised by design, and it self-reverts on every exit path (Ctrl+C,
SIGTERM, or `--ttl` expiry). Point the operator at the runbook's
[Enable Per-Endpoint Audit Mode](../../../docs/bootstrap-runbook.md#enable-per-endpoint-audit-mode)
section for the RBAC it needs and the documented new-endpoint race window.
Use the per-endpoint form only — never suggest the daemon-wide audit-mode
setting; that ConfigMap knob disables enforcement cluster-wide and the
runbook's own leading warning confines it to a single paragraph for exactly
that reason.

## Step 3: Drive the capture

Once the operator confirms audit mode is active, delegate to the
`cpg-operator` agent to call `start_session` with audit-inclusion enabled —
discover the exact argument name for that via `tools/list` at call time; do
not assume it from this document (Router Principle). Let the capture run
long enough to observe the traffic the operator cares about, then have
`cpg-operator` call `stop_session`.

For a wider verdict view alongside the MCP-driven capture, the operator may
also want the runbook's
[Observe Policy Verdicts](../../../docs/bootstrap-runbook.md#observe-policy-verdicts)
Hubble command — that stays a human/CLI step, not something this skill runs.

## Step 4: Enforce checklist (ends with the human applying)

1. Review the generated policy YAML the capture produced.
2. Cross-check against the runbook's
   [Create and Apply Generated Policies](../../../docs/bootstrap-runbook.md#create-and-apply-generated-policies)
   section.
3. The HUMAN applies the reviewed policy:

   ```bash
   kubectl apply -f ./policies/<namespace>/
   ```

There is no apply tool in this workflow and none should be invoked or
suggested — this step is intentionally the operator's own action, taken
outside this skill, after they have reviewed the YAML.

## Step 5: Verify and clean up (human-run)

Point the operator at the runbook's
[Verify Enforcement](../../../docs/bootstrap-runbook.md#verify-enforcement) and
[Clean-up](../../../docs/bootstrap-runbook.md#clean-up) sections to confirm the
endpoint left audit mode and, if this was a one-off exercise, to tear down
what was applied. Per-endpoint audit mode has no separate disable step —
stopping `cpg audit-window` in Step 2 already reverted it.

## After onboarding

Once a namespace is onboarded and enforcing, route follow-up live-capture
work to `cpg-triage` and offline policy audits to `cpg-policy-review` — this
skill's job ends at a verified, enforcing namespace.
