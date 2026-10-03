# Codebase map

> Orientation map for agents and contributors. Structure snapshot, not a design doc — update when layout changes.

## Overview

- **What:** CLI that streams dropped flows from Hubble Relay and generates the CiliumNetworkPolicy YAML that would allow them (default-deny onboarding). Readonly by default, never auto-applies.
- **Stack:** Go 1.25, cobra, client-go, Cilium/Hubble gRPC, zap, MCP go-sdk.
- **Module:** `github.com/SoulKyu/cpg`

## Entrypoint — `cmd/cpg/`

| Command | Role |
|---|---|
| `generate` | Live: stream Hubble → aggregate → YAML in `./policies/<ns>/<workload>.yaml` (cluster dedup, auto port-forward) |
| `replay <file.jsonl\|->` | Offline: replay a jsonpb capture through the same pipeline |
| `bootstrap` | Default-deny onboarding with zero real drops |
| `audit-window` | Only command that writes to the cluster: flips per-endpoint `PolicyAuditMode`, reverts every flip on exit |
| `explain <NS/WORKLOAD \| policy.yaml>` | Per-rule flow evidence behind generated rules |
| `mcp` | MCP server (query/evidence/flows tools) for LLM harnesses |

Support: `main.go` (cobra wiring), `commonflags.go`, `mcp_*.go` (tools/session/evidence, ~15 files).

## Packages — `pkg/`

| Package | Role | Key files |
|---|---|---|
| `hubble` | Live gRPC client, flow aggregator, pipeline orchestration, summary, unhandled flows, evidence/health writers | `pipeline.go`, `aggregator.go` |
| `policy` | Flow → CiliumNetworkPolicy: builder, merge with existing YAML, semantic dedup, attribution, L7 HTTP/DNS, bootstrap builder | `builder.go`, `merge.go` |
| `k8s` | Kubeconfig loading, port-forward, preflight, cluster policy fetch, Cilium version detection | `client.go`, `version.go` |
| `session` | Session state and pipeline config | `manager.go` |
| `flowsource` | Flow stream abstraction: live gRPC or jsonpb file source | `source.go` |
| `evidence` | Per-rule flow attribution (for `explain`/MCP), schema + reader/writer | `schema.go` |
| `labels` | Label selection, denylist, endpoint/peer selector builders | `selector.go` |
| `output` | Directory-organized YAML writer, merge-on-write, annotations | `writer.go` |
| `diff` | Unified YAML diff for `--dry-run` | `yaml.go` |
| `dropclass` | Drop classification + hints + versions | `classifier.go` |
| `auditwindow` | Audit mode management (lifecycle-bound) | `manager.go` |

## Data flow (generate/replay)

```
flowsource (gRPC live | jsonl replay)
  → hubble.aggregator (aggregate by workload)
  → policy.builder (+ l7, companion_dns)
  → policy.dedup / policy.merge (fuse with existing YAML)
  → output.writer → ./policies/<ns>/<workload>.yaml
  → evidence.* (attribution for explain/mcp)
```

## Tests & quality

- Tests colocated as `_test.go` next to every file; no live cluster required (Hubble gRPC client mocked via interfaces).
- `testdata/flows/`: jsonl fixtures (small, l7_dns, l7_http, mixed_tcp_icmp, malformed, with_audit…).
- `make all` = lint (golangci-lint) + test (race detector) + build to `./bin/cpg`.
- Desloppify scorecard in `scorecard.png`; package-level `CLAUDE.md` in `cmd/cpg`, `pkg/hubble`, `pkg/labels`, `pkg/output`, `pkg/policy`.

## Docs — `docs/`

User guides: installation, getting-started, bootstrap-runbook, audit-mode, policy-generation, l7-guide, explain, mcp-server, security, cli-reference, KNOWN_LIMITATIONS (with tracking IDs). Design artifacts: `plans/` (krew submission, unhandled-flows tracker), `superpowers/` (offline replay).

## Watch points

- `policy/builder.go` (~740 lines) and `session/manager.go` (~550 lines) are the largest units.
- Security: readonly guarantee everywhere except `audit-window` — see `docs/security.md`.
