---
name: cpg-mcp-smoke
description: >
  Smoke-test a tagged cpg binary's MCP server against the fake-relay e2e
  harness: full 9-tool handshake plus a graceful session lifecycle. Use
  after a release build, before shipping a new cpg binary, to confirm the
  MCP surface still works end-to-end. Do NOT use this for a live-cluster
  workflow — it targets the fake relay, not a real cluster (see cpg-triage
  or cpg-audit-onboard for live work).
---

# cpg-mcp-smoke

Router for post-release MCP smoke testing. This skill routes to the
EXISTING e2e harness in `cmd/cpg/mcp_e2e_test.go` — it does not rebuild any
test infrastructure.

## Step 1: Run the graceful lifecycle test WITHOUT -short

```bash
rtk proxy go test ./cmd/cpg/... -run TestMCPE2EGracefulLifecycle -v
```

This must run WITHOUT `-short` — the test explicitly skips itself under
short mode. A "pass" that completes in under a second means the test was
skipped, not that the smoke actually ran; the real run does a one-time
`-race` build of the binary plus a real subprocess round-trip, so expect it
to take several seconds minimum.

## Step 2: What the test covers

`TestMCPE2EGracefulLifecycle` exercises the full handshake and session
lifecycle against an in-process fake Hubble relay: initialize, `tools/list`
(schema + annotation checks), `start_session`, `get_status`, all five query
tools mid-capture, `stop_session`, `get_cluster_health` post-stop, and a
graceful stdin-close exit. This is the smoke skill's primary target.

## Step 3: Assert the tool count

The handshake must report exactly 9 tools. This skill validates the current
full real tool surface, not the stale "8-tool" figure some older docs still
carry — `get_bootstrap_policy` shipped after that figure was written.

## Step 4: Coverage floor — the 9 tools

Every registered MCP tool must be exercised or at minimum named as part of
this smoke's scope: `start_session`, `get_status`, `stop_session`,
`list_dropped_flows`, `list_policies`, `get_policy`, `get_evidence`,
`get_cluster_health`, `get_bootstrap_policy`.

## Step 5: Companion scenario — abrupt disconnect

`TestMCPE2EUngracefulDisconnect` is the companion scenario for a killed
transport (an abrupt disconnect instead of a graceful stdin close). Run it
alongside the graceful lifecycle test for a fuller smoke pass:

```bash
rtk proxy go test ./cmd/cpg/... -run TestMCPE2EUngracefulDisconnect -v
```

The graceful lifecycle test in Step 1 remains the primary smoke target;
this companion is worth running but is not a substitute for it.
