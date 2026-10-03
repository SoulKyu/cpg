---
last_mapped_commit: eff36ca37f3c162ebd12c5ddf50350d36a9906b3
last_mapped_at: 2026-10-03
---
# Structure

**Analysis Date:** 2026-10-03

## Directory Layout

```
cpg/
├── cmd/cpg/            # All cobra commands + MCP server (package main)
├── pkg/                # Library packages, one concern per package
├── docs/               # User guides + design artifacts
├── testdata/flows/     # jsonpb jsonl fixtures for replay/tests
├── .github/            # CI workflows
├── .krew.yaml          # kubectl plugin manifest
├── .golangci.yml       # Lint config
├── .goreleaser.yml     # Release config
├── Makefile            # build / test / lint / all
└── go.mod              # module github.com/SoulKyu/cpg, go 1.25
```

## `cmd/cpg/` — command layer (package `main`)

| File | ~LOC | Role |
|---|---|---|
| `main.go` | 105 | Root cobra command, logger setup, command registration, exit-code mapping |
| `commonflags.go` | 251 | Shared flags (`-n`, `-A`, `-o`, `--evidence-dir`, `--ignore-protocol`, `--ignore-drop-reason`...), `validateCommonFlags`, Levenshtein suggestions |
| `generate.go` | 324 | Live command: preflight, port-forward, cluster dedup, `hubble.RunPipeline` |
| `replay.go` | 134 | Offline command: `flowsource.NewFileSource` → same pipeline |
| `explain.go` | 163 | Evidence reader/renderer command (`pkg/explain`) |
| `explain_target.go` | 57 | Resolves `NS/WORKLOAD` vs policy-yaml target |
| `bootstrap.go` | 160 | Default-deny bootstrap policy to stdout |
| `audit_window.go` | 187 | Audit-mode window command (only cluster writer) |
| `mcp.go` | 142 | MCP stdio server bootstrap (`mcp.IOTransport`, stdio protection) |
| `mcp_tools.go` | 168 | MCP tool registration (`session start/status/stop`) |
| `mcp_query.go` | 489 | MCP query tool: policies/rules over evidence |
| `mcp_query_flows.go` | 497 | MCP flows tool: replay-backed flow queries |
| `mcp_query_evidence.go` | 216 | MCP evidence tool |
| `mcp_query_pagination.go` | 198 | Cursor pagination for MCP tools |
| `mcp_bootstrap.go` | 91 | MCP bootstrap tool |
| `CLAUDE.md` | — | Package conventions for agents |
| `*_test.go` (24 files) | — | Colocated tests incl. e2e/MCP harness (`mcp_harness_test.go`, `mcp_e2e_test.go`) |

## `pkg/` — one package per concern

| Package | ~LOC (non-test) | Role | Key files |
|---|---|---|---|
| `pkg/policy` | ~2100 | Flow → CiliumNetworkPolicy synthesis | `builder.go` (737), `merge.go` (410), `dedup.go` (189), `companion_dns.go` (189), `attribution.go` (129), `l7.go` (122), `bootstrap_builder.go` (51), `reasons.go` (18); `testdata/ingress_flow.go`, `testdata/companion_dns_assert.go` |
| `pkg/hubble` | ~2200 | gRPC client + streaming pipeline orchestration | `aggregator.go` (661), `pipeline.go` (444), `health_writer.go` (270), `client.go` (223), `writer.go` (175), `evidence_writer.go` (152), `summary.go` (149), `unhandled.go` (146), `health_reader.go` (35); `CLAUDE.md` |
| `pkg/session` | ~460 | MCP single-session state machine + pipeline config | `manager.go` (551), `pipeline_config.go` (110), `paths.go` (57) |
| `pkg/auditwindow` | 485 | Audit-mode lifecycle: open/close/shutdown with guaranteed revert | `manager.go` |
| `pkg/k8s` | ~1150 | Cluster integration (readonly except auditwindow's use) | `version.go` (389), `exec.go` (240), `portforward.go` (126), `preflight.go` (119), `cluster_dedup.go` (61), `client.go` (25) |
| `pkg/evidence` | ~430 | Per-rule evidence schema (v2) + reader/writer/merge | `schema.go` (111), `merge.go` (107), `writer.go` (89), `paths.go` (68), `reader.go` (49) |
| `pkg/flowsource` | ~210 | `FlowSource` interface + jsonpb file source | `source.go` (interface), `file.go` (171, gzip + stdin) |
| `pkg/explain` | ~290 | Evidence rendering (text/json/yaml) + filters | `render.go` (183), `filter.go` (101) |
| `pkg/output` | ~350 | YAML output writer, merge-on-write, annotations | `writer.go` (194), `annotate.go` (160); `CLAUDE.md` |
| `pkg/labels` | 187 | K8s label filtering, workload naming, selectors | `selector.go`; `CLAUDE.md` |
| `pkg/dropclass` | ~380 | DropReason → Infra/Transient/App/L7 classification | `classifier.go` (329), `hints.go` (56) |
| `pkg/diff` | 62 | Unified YAML diff for `--dry-run` | `yaml.go` |

## Supporting directories

- `testdata/flows/` — replay fixtures: `empty.jsonl`, `small.jsonl` (+ `.gz`), `l7_dns.jsonl`, `l7_http.jsonl`, `mixed_tcp_icmp.jsonl`, `malformed.jsonl`, `with_audit.jsonl`, `with_non_dropped.jsonl`
- `docs/` — user docs: `cli-reference.md`, `bootstrap-runbook.md`, `audit-mode.md`, `explain.md`, `security.md`, `KNOWN_LIMITATIONS.md`, `CODEBASE_MAP.md`; design artifacts under `docs/plans/`, `docs/superpowers/`
- `.planning/` — planning workspace (PROJECT.md, ROADMAP.md, STATE.md, milestones, this `codebase/` dir)
- `scorecard.png`, `CHANGELOG.md`, `README.md` at root

## Where new code goes

- **New CLI command:** `cmd/cpg/<name>.go` with `newXCmd() *cobra.Command` + `runX` runner; register in `main.go` via `rootCmd.AddCommand(newXCmd())`; add flags to `commonflags.go` only if shared
- **New pipeline behavior:** aggregation/filtering in `pkg/hubble/aggregator.go` (flags plumbed via `PipelineConfig` in `pkg/hubble/pipeline.go`, CLI flag in `cmd/cpg`); policy-rule synthesis in `pkg/policy/builder.go`
- **New evidence field:** bump/extend `pkg/evidence/schema.go` (schema versioned, readers refuse unknown versions), then `pkg/hubble/evidence_writer.go` conversion
- **New k8s interaction:** `pkg/k8s/` behind a small function or seam var in `cmd/cpg` (pattern: `l7ClientFactory`, `auditWindowNewManager`)
- **New MCP tool:** register in `cmd/cpg/mcp_tools.go` (`registerSessionTools` pattern), implementation in `mcp_query*.go`
- **Tests:** colocated `_test.go` next to the file under test; replay-driven tests use `testdata/flows/` fixtures via `flowsource.FileSource` + `RunPipelineWithSource`

## Naming conventions observed

- **Packages:** short, singular-concept lowercase names (`flowsource`, `dropclass`, `auditwindow`)
- **Constructors:** `New<Type>` exported (`output.NewWriter`, `evidence.NewWriter`), `new<Type>` unexported (`hubble.newPolicyWriter`, `hubble.newEvidenceWriter`)
- **Command wiring:** `newXCmd()` / `runX(cmd, args) error` per command file
- **Test seams:** package-level `var <name>Factory` or `<name>NewManager` func vars, overridden in tests
- **Interfaces:** defined at the consumer, often unexported (`flushingTracker`, `flowStream`, `auditWindowManager`); exported only at true boundaries (`FlowSource`, `FlowTracker`)
- **CLAUDE.md files** in `cmd/cpg/`, `pkg/hubble/`, `pkg/labels/`, `pkg/output/`, `pkg/policy/`, `pkg/policy/testdata/` document agent-facing conventions per package
- **Comment discipline:** decision IDs in comments (`EXIT-01`, `FILTER-03`, `M7`, `D-08`, `SESS-02`) trace to plans; constraints documented at definition sites
- **Config value comments:** magic numbers named and justified inline (`defaultScannerBufferBytes = 10 MiB`, `auditWindowDefaultTTL = 30m`)

---

*Structure analysis: 2026-10-03*
*Update when files or directories are added/removed/moved*
