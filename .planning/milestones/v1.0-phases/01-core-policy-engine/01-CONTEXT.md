# Phase 1: Core Policy Engine + Hubble Streaming - Context

**Gathered:** 2026-03-08
**Status:** Ready for planning

<domain>
## Phase Boundary

Deliver an end-to-end CLI tool that connects to Hubble Relay via gRPC, streams dropped flows, transforms them into valid CiliumNetworkPolicy YAML files with smart label selection, and writes organized output. This phase merges the originally planned Phase 1 (pure domain logic) and Phase 2 (Hubble streaming) into a single deliverable.

**Requirements covered:** PGEN-01, PGEN-02, PGEN-04, PGEN-05, PGEN-06, OUTP-01, OUTP-02, OUTP-03, CONN-01, CONN-03, CONN-04, CONN-05

</domain>

<decisions>
## Implementation Decisions

### Cilium Dependency Strategy
- Import the full `github.com/cilium/cilium` monorepo for type-safe CRD and proto types
- Target Cilium 1.19 (latest stable)
- No CI gate on binary size for now — verify manually
- Accept the trade-off: larger binary but compile-time type safety and native proto types

### Label Selection Heuristics
- Hierarchical priority for endpointSelector: `app.kubernetes.io/name` > `app` > all labels with denylist filtering
- Denylist (excluded labels): `pod-template-hash`, `controller-revision-hash`, `statefulset.kubernetes.io/pod-name`, `job-name`, `batch.kubernetes.io/job-name`, `batch.kubernetes.io/controller-uid`, `apps.kubernetes.io/pod-index`
- When no priority label found: use ALL labels from the flow after denylist filtering (not skip, not fallback to pod name)
- Same logic applies to both endpointSelector and peer selectors (fromEndpoints/toEndpoints) — consistent everywhere
- Always include `k8s:io.kubernetes.pod.namespace` in peer selectors for cross-namespace traffic

### Output Structure and Naming
- Directory structure: `<output-dir>/<namespace>/<workload>.yaml`
- Default output directory: `./policies` (override with `--output-dir` / `-o`)
- One file per workload containing both ingress and egress rules
- K8s resource name convention: `cpg-<workload>` with label `app.kubernetes.io/managed-by: cpg`
- Merge intelligent: when file exists, read it, add new ports/peers to existing rules, rewrite
- File naming uses workload name derived from label selection (same as endpointSelector)

### gRPC Connection and CLI Flags
- Main command: `cpg generate`
- `--server` / `-s` is required (no auto port-forward in v1)
- Insecure (no TLS) by default — `--tls` flag to enable TLS for direct connections
- Namespace: defaults to current kubeconfig context namespace; `--namespace` / `-n` for override (repeatable for multiple namespaces); `--all-namespaces` / `-A` for cluster-wide
- Connection timeout: 10s default, configurable with `--timeout`
- Streaming only — no one-shot/duration mode; runs until Ctrl+C
- Short flags aligned with kubectl/hubble: `-n`, `-s`, `-o`, `-A`

### Streaming and Aggregation
- Temporal window aggregation: accumulate flows for N seconds, then flush (generate/merge policies)
- Default flush interval: 5s, configurable with `--flush-interval`
- Aggregation key: `(namespace, workload, direction)`
- Graceful shutdown: flush all accumulated flows on SIGINT/SIGTERM before exit, then display summary

### Error Handling and Reconnection
- gRPC disconnect: automatic retry with exponential backoff (1s, 2s, 4s... max 60s), log each attempt
- Malformed/incomplete flows (no L4, no namespace): skip + debug log, do not block pipeline
- File write errors (disk full, permissions): log error, continue streaming, retain flows in memory for retry at next flush

### Logging and User Feedback
- Log level: info by default, `--debug` or `--log-level debug` for verbose output
- Log format: human-friendly colored console by default, `--json` flag for structured JSON output
- Session summary on shutdown (Ctrl+C): duration, flows seen, policies generated/updated/skipped, lost events count, output directory
- LostEvents: aggregated warning log every 30s (not per-event) with total count and recommendation to increase buffer

### Project Structure
- Go 1.23 minimum, golangci-lint v2 (govet, errcheck, staticcheck, unused)
- Package layout: `cmd/` (cobra commands), `pkg/hubble/`, `pkg/policy/`, `pkg/labels/`, `pkg/output/`
- Makefile targets: build (ldflags version), test, lint, clean, all
- Cobra CLI framework for command/flag management
- zap for structured logging

### Claude's Discretion
- Channel buffer sizes for the streaming pipeline
- Exact backoff parameters for gRPC reconnection
- Internal data structures for flow aggregation (maps, LRU, etc.)
- Compression/optimization of generated YAML
- Test fixture design and coverage strategy
- golangci-lint exact configuration

</decisions>

<specifics>
## Specific Ideas

- CLI should feel familiar to kubectl/hubble users (same flag conventions: -n, -A, -o)
- Policy files should be directly usable in GitOps workflows (kubectl apply -f ./policies/)
- Session summary inspired by standard SRE tooling (concise, actionable)
- Labels: user preference to include all available labels (after denylist) rather than picking just one — more restrictive selectors are preferred over potentially ambiguous ones

</specifics>

<code_context>
## Existing Code Insights

### Reusable Assets
- None — greenfield project, no existing code

### Established Patterns
- Cilium ecosystem conventions: `pkg/` prefix, thin `cmd/` layer, interface-based boundaries
- Channel pipeline pattern for streaming (source -> transform -> sink)

### Integration Points
- `github.com/cilium/cilium` v1.19 for CRD types and flow proto
- `github.com/spf13/cobra` for CLI
- `go.uber.org/zap` for structured logging
- `sigs.k8s.io/yaml` for YAML serialization

</code_context>

<deferred>
## Deferred Ideas

- Auto port-forward to hubble-relay (Phase 3 / Production Hardening)
- File-based deduplication (Phase 3)
- Cluster-based deduplication via client-go (Phase 3)
- CIDR-based rules for external traffic / reserved:world identity (Phase 3)
- CI gate on binary size
- TLS certificate configuration flags (--tls-cert, --tls-key, --tls-ca)
- One-shot/duration mode for CI pipelines

</deferred>

---

*Phase: 01-core-policy-engine*
*Context gathered: 2026-03-08*
