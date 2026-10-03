---
last_mapped_commit: eff36ca37f3c162ebd12c5ddf50350d36a9906b3
last_mapped_at: 2026-10-03
---
# Architecture

**Analysis Date:** 2026-10-03

## Pattern Overview

**Overall:** Streaming pipeline CLI (single Go binary, cobra commands driving a multi-goroutine flow→policy pipeline)

**Key Characteristics:**
- Single executable `cpg` (`cmd/cpg/`), also installable as kubectl plugin (`kubectl-cilium-policy-gen` — detected via `isKubectlPlugin()` in `cmd/cpg/main.go`)
- Continuous streaming: Hubble Relay gRPC flow stream (or offline jsonpb replay) consumed until Ctrl+C, then flush + summary
- Readonly by default; only `audit-window` writes to the cluster
- Offline-capable: the same pipeline is fed by a live gRPC source or a capture file via the `flowsource.FlowSource` interface
- Side-channel outputs: policy YAML is the main product; evidence JSON (`pkg/evidence`) and cluster-health JSON are parallel side effects of the same events

## Layers

**Command Layer (`cmd/cpg/`, package `main`):**
- Purpose: cobra command wiring, flag parsing/validation, signal handling (SIGINT/SIGTERM), preflight orchestration, exit-code policy
- Contains: one file per command (`generate.go`, `replay.go`, `explain.go`, `bootstrap.go`, `audit_window.go`, `mcp.go`), shared flag code (`commonflags.go`), MCP server internals (`mcp_*.go`)
- Depends on: all `pkg/` layers; injectable seams are package-level vars (`l7ClientFactory`, `bootstrapDetectVersion`, `auditWindowNewManager` — see `cmd/cpg/generate.go`, `cmd/cpg/audit_window.go`)
- Used by: the user / kubectl plugin / MCP host

**Pipeline Orchestration Layer (`pkg/hubble`):**
- Purpose: owns the streaming lifecycle: connect, aggregate, fan out, write, finalize
- Contains: `RunPipeline`/`RunPipelineWithSource` (`pkg/hubble/pipeline.go`), `Aggregator` (`pkg/hubble/aggregator.go`), gRPC `Client` (`pkg/hubble/client.go`), `policyWriter` (`pkg/hubble/writer.go`), `evidenceWriter`/`healthWriter` (`pkg/hubble/evidence_writer.go`, `pkg/hubble/health_writer.go`), `UnhandledTracker` (`pkg/hubble/unhandled.go`)
- Depends on: `flowsource`, `policy`, `output`, `evidence`, `dropclass`, `labels`
- Used by: `cmd/cpg/generate.go`, `cmd/cpg/replay.go`, `pkg/session/manager.go` (MCP sessions)

**Policy Synthesis Layer (`pkg/policy`):**
- Purpose: pure conversion of flows into CiliumNetworkPolicy rules
- Contains: `builder.go` (flow → `PolicyEvent`), `merge.go` (merge with existing YAML), `dedup.go` (`PoliciesEquivalent`), `l7.go` (HTTP/DNS rules), `companion_dns.go` (kube-dns companion egress), `attribution.go` (`RuleKey`/`RuleAttribution`), `bootstrap_builder.go`
- Depends on: `labels` only — no I/O, fully testable
- Used by: pipeline orchestration layer

**I/O & Integration Layer (`pkg/output`, `pkg/k8s`, `pkg/flowsource`, `pkg/evidence`):**
- Purpose: filesystem writes, cluster reads, flow ingestion, evidence persistence
- Contains: `pkg/output/writer.go` (`<dir>/<ns>/<workload>.yaml`, merge-on-write), `pkg/k8s/*` (kubeconfig, port-forward, version detection, preflight, cluster policy list, exec), `pkg/flowsource/source.go` + `pkg/flowsource/file.go`, `pkg/evidence/*` (schema v2, reader/writer/merge)
- Depends on: client-go, Cilium gRPC APIs, `pkg/policy`
- Used by: pipeline orchestration and command layers

**Support Layer (`pkg/labels`, `pkg/dropclass`, `pkg/diff`, `pkg/explain`, `pkg/auditwindow`, `pkg/session`):**
- Purpose: label→selector logic, drop-reason classification, YAML diff, evidence rendering, audit-mode lifecycle, MCP session state machine
- Depends on: minimal, leaf-like (`dropclass` depends only on flowpb; `session` depends on `hubble`+`k8s`)

## Data Flow

**`cpg generate` (live streaming):**

1. `cmd/cpg/main.go` builds root cobra command, registers `generate`; `PersistentPreRunE` builds zap logger
2. `runGenerate` (`cmd/cpg/generate.go`) parses/validates flags, sets up signal ctx, runs version + optional L7 preflight (`pkg/k8s`), optionally port-forwards to hubble-relay (`pkg/k8s/portforward.go`), loads cluster policies for `--cluster-dedup` (`pkg/k8s/cluster_dedup.go`)
3. Calls `hubble.RunPipeline` (`pkg/hubble/pipeline.go`) with a `PipelineConfig`; the default source is `hubble.Client` (`pkg/hubble/client.go`), which implements `flowsource.FlowSource`
4. `RunPipelineWithSource` runs 5 errgroup stages: (0) stream-error relay via `source.StreamErr()`, (1) `Aggregator.Run` — flows bucketed by `(namespace, workload)` (`AggKey`), classified via `dropclass.Classify`, converted to `policy.PolicyEvent`; (1b) fan-out to policy + evidence channels; (2) `policyWriter.handle` — skips cluster/equivalent duplicates; (2b) `evidenceWriter`; (2c) `healthWriter` drains `DropEvent`s
5. On flush interval or ctx cancel, `Aggregator.Flush()` emits policies; `output.Writer.Write` (`pkg/output/writer.go`) merges into existing YAML at `./policies/<ns>/<workload>.yaml`
6. On shutdown: `SessionStats.Log` prints summary; `ExitCodeError` (`pkg/hubble/pipeline.go`) propagates `--fail-on-infra-drops` exit codes through `cmd/cpg/main.go`

**`cpg replay <file.jsonl|->` (offline):**
Identical from step 4 on; the source is `flowsource.NewFileSource` (`pkg/flowsource/file.go`) reading jsonpb lines (gzip-transparent, stdin via `-`). No k8s preflight — offline by contract (`cmd/cpg/generate.go` comments enforce "generate ONLY" for preflight calls).

**Evidence side-channel:**
Every `PolicyEvent` is duplicated to `evidenceWriter` → `pkg/evidence.Writer` persists `PolicyEvidence` (schema v2, `pkg/evidence/schema.go`) to `<evidence-dir>/<output-hash>/<ns>/<workload>.json`. `cpg explain` (`cmd/cpg/explain.go` → `pkg/explain/render.go`, `pkg/explain/filter.go`) and `cpg mcp` query tools read it back. Cluster-health counters follow the same hash-rooted layout (`pkg/hubble/health_writer.go`, read by `pkg/hubble/health_reader.go`).

**State Management:**
- No persistent process state; each invocation is independent
- Disk artifacts: policy YAML under `--output-dir` (default `./policies`), evidence under `$XDG_CACHE_HOME/cpg/evidence` (`pkg/evidence/paths.go`, `pkg/session/paths.go`)
- MCP mode: `pkg/session.Manager` holds a single-session slot (`session *Session`, mutex-guarded) driving pipelines on a server-rooted ctx

## Key Abstractions

**`flowsource.FlowSource` (`pkg/flowsource/source.go:14`):**
- Purpose: decouple the flow stream from the pipeline; live gRPC vs file replay vs test stubs
- Signature: `StreamDroppedFlows(ctx, namespaces, allNS, includeAudit) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error)`; implementations must close both channels
- Implementations: `hubble.Client` (`pkg/hubble/client.go`), `flowsource.FileSource` (`pkg/flowsource/file.go`); optional `StreamErr() <-chan error` interface detected at `pkg/hubble/pipeline.go:242`
- Pattern: interface at consumer boundary; construction happens in `cmd/cpg` (client) or `hubble.RunPipeline` default

**`policy.FlowTracker` (`pkg/policy/builder.go:19`):**
- Purpose: receive flows that cannot become policy rules; `pkg/hubble.UnhandledTracker` implements it; `Aggregator` wraps it in a local `flushingTracker` interface (`pkg/hubble/aggregator.go:65`) adding `Flush()`
- Pattern: consumer-side interface, kept unexported/local to avoid API bloat

**`PolicyEvent` + `RuleAttribution` (`pkg/policy/attribution.go`):**
- Purpose: the unit flowing through channels — one workload policy per flush plus per-rule flow attribution (peer, L7 discriminator, samples)
- Pattern: value type passed over buffered channels (buf 64), fanned out in pipeline stage 1b

**Command seams (package-level vars in `cmd/cpg`):**
- Purpose: substitute k8s interactions in tests without a live cluster
- Examples: `l7ClientFactory` (`cmd/cpg/generate.go`), `auditWindowManager` interface + `auditWindowNewManager` (`cmd/cpg/audit_window.go`)
- Pattern: `var factory = func(...) ...` overridable in tests

**`session.Manager` (`pkg/session/manager.go`):**
- Purpose: single-active-session state machine for MCP mode (Start/Status/Stop/Shutdown), pipelines run on a server-rooted ctx, not per-request
- Pattern: mutex-guarded single slot; multi-session registry deliberately rejected

## Entry Points

**Root CLI:** `cmd/cpg/main.go` — builds logger, registers commands, maps `hubble.ExitCodeError` → `os.Exit(code)`, all other errors → exit 1

Per cobra command (each has `newXCmd()` + `runX`):

| Command | Constructor | Runner | Core call |
|---|---|---|---|
| `generate` | `newGenerateCmd` (`cmd/cpg/generate.go`) | `runGenerate` | `hubble.RunPipeline` |
| `replay` | `newReplayCmd` (`cmd/cpg/replay.go`) | `runReplay` | `hubble.RunPipelineWithSource` + `flowsource.NewFileSource` |
| `explain` | `newExplainCmd` (`cmd/cpg/explain.go`) | `runExplain` | `pkg/evidence` reader + `pkg/explain` render |
| `bootstrap` | `newBootstrapCmd` (`cmd/cpg/bootstrap.go`) | `runBootstrap` | `policy.BuildBootstrapPolicy` → stdout |
| `audit-window` | `newAuditWindowCmd` (`cmd/cpg/audit_window.go`) | `runAuditWindow` | `auditwindow.Manager.Open/Close/Shutdown` |
| `mcp` | `newMCPCmd` (`cmd/cpg/mcp.go`) | `runMCPServer` | MCP go-sdk stdio server, tools in `cmd/cpg/mcp_tools.go`, `mcp_query*.go` |

## Error Handling

**Strategy:** errors bubble from `RunE` to cobra; `cmd/cpg/main.go` converts typed `hubble.ExitCodeError` to a specific exit code, everything else exits 1. Pipeline-internal write failures are logged, never fatal (e.g. `policyWriter.handle` "never returns an error").

**Patterns:**
- Transport vs EOF distinction: stream goroutine sends only non-EOF errors over `streamErr` (`pkg/hubble/client.go`)
- Advisory preflight: kubeconfig/client failures logged as warnings, pipeline proceeds (`maybeRunVersionPreflight`, `maybeRunL7Preflight` in `cmd/cpg/generate.go`)
- Explicit close ordering in the fan-out stage (no `defer`; see M7 comment in `pkg/hubble/pipeline.go`)
- Flag validation fail-fast in `PreRunE: validateCommonFlags` (`cmd/cpg/commonflags.go`) with Levenshtein suggestions

## Cross-Cutting Concerns

**Logging:** zap, package-level `logger` built in root `PersistentPreRunE`; `--debug`/`--log-level`/`--json` flags; structured fields throughout; `zap/exp/zapslog` bridges zap into the MCP server's slog

**Signals:** every long-running command wraps ctx with `signal.NotifyContext(os.Interrupt, syscall.SIGTERM)`; audit-window reverts all flips on any exit path

**Validation:** allowlists live beside their consumers — `hubble.ValidIgnoreProtocols()` (`pkg/hubble/aggregator.go`), `dropclass.ValidReasonNames()` (`pkg/dropclass/classifier.go`), both enforced in `cmd/cpg/commonflags.go`

**Readonly guarantee:** all commands are cluster-readonly except `audit-window` (flips `PolicyAuditMode` per endpoint, reverts every flip on exit — see `pkg/auditwindow/manager.go`, `docs/security.md`)

**Testing conventions:** tests colocated as `_test.go`; interfaces at k8s/gRPC boundaries enable mock-only tests; jsonl fixtures in `testdata/flows/` and `pkg/policy/testdata/`; `make all` = lint + `go test -race` + build

---

*Architecture analysis: 2026-10-03*
*Update when major patterns change*
