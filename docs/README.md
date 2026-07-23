# cpg documentation

**cpg** (Cilium Policy Generator) watches dropped flows through Hubble Relay and generates
the CiliumNetworkPolicy YAML that would allow them. This is the full documentation —
for the elevator pitch, see the [project README](../README.md).

## Getting started

| Guide | What's inside |
|-------|---------------|
| [Installation](installation.md) | krew, `go install`, source builds, supported Cilium versions, k9s plugin |
| [Getting started](getting-started.md) | First live capture, offline replay, auto port-forward |
| [Bootstrap runbook](bootstrap-runbook.md) | Onboarding a namespace to default-deny with zero real drops (audit-window workflow) |

## Guides

| Guide | What's inside |
|-------|---------------|
| [Policy generation](policy-generation.md) | The pipeline, generated YAML examples, label selection, deduplication, unhandled flows |
| [Audit mode](audit-mode.md) | How `cpg audit-window`, `cpg bootstrap`, and `--include-audit` fit together; lifecycle guarantees |
| [L7 guide](l7-guide.md) | HTTP/DNS rules with `--l7`: prerequisites, the two-step workflow, visibility bootstrap |
| [Explain & evidence](explain.md) | `cpg explain` — per-rule flow evidence behind every generated rule |
| [MCP server](mcp-server.md) | `cpg mcp` — readonly MCP server for LLM harnesses, tool catalog, harness configuration |
| [Security model](security.md) | Readonly-by-default guarantees, the one mutating command, RBAC requirements |

## Reference

| Reference | What's inside |
|-----------|---------------|
| [CLI reference](cli-reference.md) | All flags, dry-run mode, exit codes, CI/cron integration |
| [Known limitations](KNOWN_LIMITATIONS.md) | Documented limitations and edge cases, with workarounds and tracking IDs |
| [Development](development.md) | Project structure, build and test targets |
