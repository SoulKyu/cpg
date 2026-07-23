# cpg

**Cilium Policy Generator** -- because writing CiliumNetworkPolicies by hand in a default-deny cluster is nobody's idea of a good Friday night.

`cpg` connects to Hubble Relay, watches dropped flows in real time, and generates the CiliumNetworkPolicy YAML files that would allow them. You run it, wait for traffic to get denied, and it writes the fix. Then you review, commit, and apply through your GitOps pipeline like a responsible adult.

```
         Hubble Relay (gRPC)
              |
         [cpg generate]
              |
     stream dropped flows
              |
     aggregate by workload
              |
     build CiliumNetworkPolicy
              |
     merge with existing files
              |
     write YAML to disk
              |
     you review & git push
```

## Install

```bash
# kubectl krew
kubectl krew install cilium-policy-gen

# go install
go install github.com/SoulKyu/cpg/cmd/cpg@latest
```

More options (source builds, supported Cilium versions, k9s plugin) in the [installation guide](docs/installation.md).

## Quick start

```bash
# Live: point at a namespace, cpg auto port-forwards to hubble-relay
cpg generate -n production

# Offline: capture once, replay many
hubble observe --output jsonpb --follow > drops.jsonl
cpg replay drops.jsonl -n production

# Opt-in L7 (HTTP method/path + DNS)
cpg generate -n production --l7
```

Leave it running, generate some traffic, Ctrl+C when done. Policies land in `./policies/<namespace>/<workload>.yaml` -- reviewing and applying them stays your job (no auto-apply, ever).

Onboarding a namespace to default-deny with zero real drops? That's the [bootstrap runbook](docs/bootstrap-runbook.md).

## Readonly by default

Every command only lists, watches, and reads Kubernetes and Hubble data -- none writes to the cluster. The single exception is `cpg audit-window`, a scoped, lifecycle-bound command that flips per-endpoint `PolicyAuditMode` and reverts every flip on exit. Full guarantees and RBAC details in the [security model](docs/security.md).

## Documentation

Full documentation lives in [`docs/`](docs/README.md):

| | |
|---|---|
| [Installation](docs/installation.md) | krew, `go install`, source builds, supported Cilium versions, k9s plugin |
| [Getting started](docs/getting-started.md) | First live capture, offline replay, audit-mode onboarding |
| [Bootstrap runbook](docs/bootstrap-runbook.md) | Default-deny onboarding with `cpg bootstrap` + `cpg audit-window` |
| [Policy generation](docs/policy-generation.md) | Generated YAML examples, label selection, dedup, unhandled flows |
| [L7 guide](docs/l7-guide.md) | HTTP/DNS rules with `--l7`: prerequisites and visibility bootstrap |
| [Explain & evidence](docs/explain.md) | `cpg explain` -- the flow evidence behind every rule |
| [MCP server](docs/mcp-server.md) | `cpg mcp` for LLM harnesses: tools, configuration, secrets posture |
| [Security model](docs/security.md) | Readonly guarantees and RBAC requirements |
| [CLI reference](docs/cli-reference.md) | All flags, dry-run, exit codes, CI/cron integration |
| [Known limitations](docs/KNOWN_LIMITATIONS.md) | Honest list with workarounds and tracking IDs |
| [Development](docs/development.md) | Project structure, build/test targets, code quality scorecard |

## License

Apache 2.0
