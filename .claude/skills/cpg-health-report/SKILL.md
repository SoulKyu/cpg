---
name: cpg-health-report
description: >
  Turn a captured cluster-health.json into a shareable, self-contained HTML
  report of infra/transient drops broken down by node and workload, with
  Cilium remediation links. Use when an operator wants a readable report of
  the infra side of a session (drops that were not turned into policy). Do
  NOT use for generating or reviewing CiliumNetworkPolicies (see cpg-triage
  or cpg-policy-review).
---

# cpg-health-report

Router for turning `cluster-health.json` into a shareable HTML report. This
skill routes to where the data lives and what to render — it does not
restate the JSON schema field-by-field beyond naming the fields needed to
route the rendering.

## Step 1: Obtain cluster-health.json

Two sources exist — pick the one that matches the invocation context and
STATE which one was used in the report itself:

- **A saved/archived session directory:** read the file directly from disk
  at `<session_tmpdir>/cluster-health.json` (available post-`stop_session`).
  This copy is uncapped.
- **An already-running or already-driven session:** call the
  `get_cluster_health` MCP tool (via `cpg-operator` if a session is already
  being driven for another purpose). This path is server-capped at 100
  node/workload entries per drop.

## Step 2: Render a self-contained HTML file

Build one static HTML file, inline `<style>` only — no external JS/CSS,
no CDN dependency, nothing that requires network access to render correctly
when the operator opens it locally. Structure:

1. **Header:** session summary line built from `flows_seen` and
   `infra_drops_total`.
2. **One table row per drop entry:** reason, class, count.
3. **Expandable breakdown per row:** the by-node and by-workload counts for
   that drop reason.
4. **Remediation link:** wherever a drop entry's remediation field is
   non-empty, render it as a clickable link to the Cilium docs page it
   points at; omit the link entirely when the field is empty rather than
   rendering a dead link.

## Step 3: Escape before embedding

HTML-escape every string field pulled from `cluster-health.json` before
embedding it in the generated file — reason strings, remediation URLs, node
names, workload names. This is a self-XSS guardrail: the report is a local
file the operator opens in a browser, and nothing in this pipeline should
assume those strings are already safe to embed raw.

## Step 4: Hand off

Tell the operator where the HTML file was written and which source (disk
file vs `get_cluster_health` tool, per Step 1) it was built from, so they
know whether they're looking at a capped or uncapped view.
