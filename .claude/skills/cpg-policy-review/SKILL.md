---
name: cpg-policy-review
description: >
  Audit already-generated CiliumNetworkPolicies offline: over-broad rules,
  missing/incorrect L7 anchoring, missing DNS-53 companion rules, and
  dedup sanity, routed through cpg explain and per-rule flow evidence. Use
  when reviewing policy YAML that already exists on disk or in git, without
  a live cluster session. Do NOT use for driving a live capture (see
  cpg-triage) or for the initial namespace onboarding workflow (see
  cpg-audit-onboard).
---

# cpg-policy-review

Router for an offline policy audit. This skill routes to `cpg explain` and
the `get_evidence` tool for evidence, and points at the README rather than
restating flag lists or field shapes.

## Step 1: Locate the target

Identify the namespace/workload pair or the policy YAML path to review
(`<NAMESPACE/WORKLOAD>` or a path under the output directory, e.g.
`./policies/<namespace>/<workload>.yaml`).

## Step 2: Explain the policy

Run `cpg explain <NAMESPACE/WORKLOAD | path/to/policy.yaml>` for the target.
Use its filter flags (`--ingress`/`--egress`/`--port`/`--peer`/`--peer-cidr`/
`--http-method`/`--http-path`/`--dns-pattern`/`--since`/`--samples-limit`) to
narrow the review to the rules under question, and `--json`/`--format` to
get a structured view when comparing against evidence programmatically.
Do not restate the flag semantics beyond naming them — run `cpg explain
--help` for the exact current flag surface if anything is unclear.

If reviewing against a live-ish source (an active or recently-stopped MCP
session) rather than files on disk, use the `get_evidence` tool instead —
it returns evidence in the same shape `cpg explain --json` does.

## Step 3: Checklist — walk each rule

For each rule `cpg explain` surfaces, check:

- **Over-broad rules:** does the peer selector or CIDR match more than the
  evidence actually shows traffic from? A rule matching a whole namespace
  when evidence shows only one workload talking is a candidate to tighten.
- **L7 anchoring:** for HTTP/DNS rules, is the L7 detail (method, path,
  matchName) actually present and correctly anchored? See the README's
  [L7 Prerequisites](../../../README.md#l7-prerequisites) section for the
  capture-time requirements a rule needs before L7 attribution is even
  possible — do not re-derive those requirements here, route to it.
- **Missing DNS-53 companions:** an egress rule permitting a `toFQDNs` peer
  needs a paired DNS/port-53 rule for the FQDN to resolve at all; flag any
  FQDN-based rule missing its DNS companion.
- **Dedup sanity:** do near-duplicate rules exist (same peer/port pair
  expressed twice with slightly different selectors)? Evidence should
  justify each distinct rule; if two rules are backed by the same evidence
  set, they are a dedup candidate.

## Step 4: Cross-reference the evidence

For every rule flagged in Step 3, pull its evidence via `get_evidence` (or
the JSON output of `cpg explain` from Step 2) and confirm the flagged
concern is real — a rule that looks over-broad in isolation may be backed by
genuinely varied evidence samples justifying the breadth.

## Step 5: Report

Summarize findings per rule: keep as-is, tighten (with the specific narrower
selector/port/L7 filter suggested), or flag as a dedup candidate. Point the
operator at [Explain policies](../../../README.md#explain-policies) in the
README for the full worked examples of `cpg explain` usage if they want to
reproduce a finding themselves. This skill reports; the operator decides
whether and how to edit the policy YAML.
