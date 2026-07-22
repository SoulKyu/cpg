---
name: cpg-triage
description: >
  Drive a live cpg MCP session end-to-end: start capture, classify dropped
  flows (policy-actionable vs infra/transient/noise), present each generated
  CiliumNetworkPolicy with its evidence, and recommend what to apply. Use when
  an operator wants to turn live cluster traffic into reviewable network
  policy right now. Do NOT use for offline review of already-generated
  policies (see cpg-policy-review) or for onboarding a brand-new namespace
  into enforced default-deny (see cpg-audit-onboard).
---

# cpg-triage

Router for a single live-session triage pass against a running cluster. This
skill names workflow steps and tool names only — it never restates a tool's
argument schema or result shape. Discover exact semantics live via `tools/list`
before calling anything.

## Step 0: Delegate driving to cpg-operator

Do not call `cpg mcp` tools directly. Spawn the `cpg-operator` agent and hand it
this workflow — it owns the session lifecycle and reports findings back to you.
`cpg-operator` is the one place in this repo that actually drives MCP sessions;
you are the router that decides what it should drive.

## Step 1: Start the capture

Have `cpg-operator` call `start_session`. Confirm the session actually started
before moving on — poll `get_status` until it reports the session is capturing.

## Step 2: Classify dropped flows

Call `list_dropped_flows`. It returns two sections: policy-actionable samples
(worth turning into policy) and infra/transient/noise aggregates (not worth
individual review). Triage each section separately:

- Policy-actionable samples drive Step 3 below, one workload at a time.
- Infra/transient/noise aggregates are folded into the cluster-health summary
  in Step 5 — do not present them as individual policy candidates.

## Step 3: Present each policy candidate with its evidence

For every policy-actionable workload surfaced in Step 2:

1. Call `list_policies` to find the workload's generated CiliumNetworkPolicy.
2. Call `get_policy` for the full CNP YAML.
3. Call `get_evidence` for that policy's per-rule flow evidence, and pair it
   with the YAML when presenting to the operator — evidence is what justifies
   each rule, not just the rule itself.

Present policy + evidence together per workload so the operator can judge each
CNP on its own without cross-referencing a separate view.

## Step 4: Stop the session

Call `stop_session` once every policy-actionable workload from Step 2 has been
presented. `stop_session` finalizes the cluster-health report — call it before
Step 5, not after.

**Decision point:** `get_cluster_health` can be called mid-capture, but it will
report a "not ready yet" marker until the session is stopped — that is a normal,
documented state, not an error. Always sequence `stop_session` before
`get_cluster_health` if a complete infra picture is needed.

## Step 5: Surface the infra/transient side

Call `get_cluster_health` for the finalized per-drop-reason breakdown covering
the infra/transient/noise flows classified in Step 2 (the policy tools never
cover this side — it is `get_cluster_health`'s job).

## Step 6: Recommend

Summarize for the operator: which CNPs look ready to apply as-is, which need a
tighter rule (over-broad peer, missing L7 anchor — route the operator to
`cpg-policy-review` for a deeper offline audit if warranted), and which
cluster-health entries are worth investigating before the next triage pass.
Recommending is as far as this skill goes — applying policy is the operator's
call, made outside this workflow.
