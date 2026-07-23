# Getting started

Not installed yet? Start with the [installation guide](installation.md).

## Live capture

```bash
# Point at a namespace. cpg auto port-forwards to hubble-relay.
cpg generate -n production

# Explicit relay address
cpg generate --server localhost:4245

# All namespaces, debug logging
cpg --debug generate --all-namespaces

# TLS
cpg generate --server relay.example.com:443 --tls -n production

# Opt-in L7: HTTP method/path + DNS matchName. See the L7 guide.
cpg generate -n production --l7
```

That's it. Leave it running. Go generate some traffic (or wait for someone else to). Ctrl+C when you're done -- cpg flushes remaining flows and prints a session summary before exiting.

Policies show up in `./policies/<namespace>/<workload>.yaml`.

### Auto port-forward

When you omit `--server`, cpg finds the `hubble-relay` pod in `kube-system` using your kubeconfig and sets up a port-forward automatically. One less terminal tab to manage.

## Offline replay

Prefer to iterate on policy generation without reproducing traffic? Capture once, replay many:

```bash
# Capture dropped flows for N minutes
hubble observe --output jsonpb --follow > drops.jsonl
# Ctrl+C when done capturing

# Replay through cpg — reuse the file as many times as you want
cpg replay drops.jsonl -n production
cpg replay drops.jsonl.gz -n production    # gzip transparent
cat drops.jsonl | cpg replay -              # stdin

# Opt-in L7: HTTP method/path + DNS matchName. See the L7 guide.
cpg replay drops.jsonl --l7 -n production
```

`cpg replay <file>` feeds a Hubble jsonpb capture through the same pipeline as the live stream. It is the right tool when you want:

- **Deterministic iteration.** Re-run the same input as you tweak label selection, dedup logic, or flush intervals.
- **Offline workflow.** Capture on a jumphost, replay on your laptop.
- **Post-mortem reproduction.** Keep the capture alongside the policy in your GitOps repo so anyone can reproduce what cpg saw.

Flags shared with `generate` (`--output-dir`, `--cluster-dedup`, `--flush-interval`, `--ignore-protocol`, `--ignore-drop-reason`, `--fail-on-infra-drops`, `--include-audit`) work identically. Non-DROPPED verdicts (unless `--include-audit` also admits AUDIT) and malformed lines are skipped with counters surfaced in the session summary.

## Audit-mode onboarding (default-deny with zero real drops)

The full runbook lives in [bootstrap-runbook.md](bootstrap-runbook.md) -- this is the
short version for a namespace **with live traffic**. The trick: flipping an endpoint's
`PolicyAuditMode` is a no-op until a policy matches, so open the audit window *first* and the
default-deny apply never drops anything -- every would-be drop surfaces as an `AUDIT` verdict
instead.

```bash
# 1. Open the audit window FIRST (dedicated terminal — foreground, auto-reverts on exit/TTL)
cpg audit-window -n production --ttl 30m

# 2. Apply the default-deny bootstrap policy — flows become AUDIT verdicts, not drops
cpg bootstrap -n production | kubectl apply -f -

# 3. Capture the audited traffic and generate the allow policies
cpg generate -n production --include-audit

# 4. Review, then apply (always a human act)
cpg explain production/api-server --json
kubectl apply -f ./policies/production/

# 5. Close the window (Ctrl+C or let --ttl expire) — audit flips revert automatically,
#    default-deny now enforces against fully-covered traffic
```

On a **fresh namespace** (no live traffic) you can invert steps 1 and 2 -- bootstrap first
means the namespace is protected from the very first pod, and there is nothing running to
drop. Both orders, the new-endpoint race window, and the RBAC details are covered in the
[runbook](bootstrap-runbook.md); how the pieces work under the hood is the
[audit mode guide](audit-mode.md).

## Next steps

- [Policy generation](policy-generation.md) — what the generated YAML looks like and why
- [CLI reference](cli-reference.md) — every flag, dry-run mode, exit codes
- [L7 guide](l7-guide.md) — HTTP/DNS rules with `--l7`
- [Explain & evidence](explain.md) — inspect the flows behind every rule
