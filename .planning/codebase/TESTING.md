---
last_mapped_commit: eff36ca37f3c162ebd12c5ddf50350d36a9906b3
last_mapped_at: 2026-10-03
---
# Testing Guide

<!-- refreshed: 2026-10-03 -->

**Analysis Date:** 2026-10-03

## Framework & how to run

- `github.com/stretchr/testify` v1.11.1 (`require` for preconditions/lengths, `assert` for value checks). Standard library `testing` + subtests via `t.Run(...)` (21 test files use subtests).
- Race detector is mandatory: `make test` runs `go test ./... -count=1 -race` (`Makefile`). CI (`.github/workflows/ci.yml`) runs `go test -race -coverprofile=coverage.out -count=1 ./...` and uploads coverage on PRs.
- `make lint` (golangci-lint) gates with `only-new-issues: true`; the e2e MCP suite compiles the binary itself with `go build -race` (`cmd/cpg/mcp_e2e_test.go`).
- **Go toolchain is not installed in this environment** — `go`/`golangci-lint` were not found on PATH, so tests could not be executed during this mapping. Use `make test` on a machine with Go 1.25.x.

## Test file layout

- Tests are colocated: every `pkg/<pkg>/x.go` has `x_test.go` beside it (e.g. `pkg/hubble/pipeline.go` + `pkg/hubble/pipeline_test.go`, `pkg/dropclass/classifier.go` + `classifier_test.go`).
- `cmd/cpg/` is itself heavily tested: per-command `*_test.go` files (`generate_test.go`, `replay_test.go`, `explain_test.go`, `commonflags_test.go`, ...) plus `testhelpers_test.go` for shared helpers.
- Mixed internal/external test packages: `pkg/policy/merge_test.go` is `package policy_test` (public API), other files test unexported functions internally (e.g. `pkg/k8s/cluster_dedup_test.go` → `buildClusterPolicyMap`).
- Intra-package test backdoors live in `export_test.go` files: `pkg/dropclass/export_test.go` exposes `ResetWarnStateForTest()` to clear the dedup map + logger between tests.
- `cmd/cpg/mcp_e2e_test.go` is the real-transport layer (subprocess over OS pipes); `mcp_harness_test.go`/`mcp_session_test.go`/`mcp_query_tools_test.go` stay as the in-memory fast-feedback layer (documented as D-11 in the e2e file header).
- Doc-pinning ("golden") tests guard README/runbook content: `cmd/cpg/audit_docs_test.go`, `cmd/cpg/runbook_test.go`, `cmd/cpg/readme_compat_test.go`.

## Mocking approach — interfaces + hand-written stubs, no live cluster

- No mock generator (no mockery/gomock in `go.mod`); stubs are hand-written structs implementing small local interfaces.
- `flowsource.FlowSource` (`pkg/flowsource/source.go`) is the key seam: `pkg/hubble/pipeline_test.go` defines `mockFlowSource{flows, lostEvents}` that pre-loads buffered channels and closes them, so `RunPipeline` is tested end-to-end without gRPC.
- gRPC stream is mocked via the unexported `flowStream` interface (`pkg/hubble/client.go:45`): `mockStream` in `pkg/hubble/client_test.go` returns scripted `GetFlowsResponse`s then `io.EOF`.
- `pkg/hubble/aggregator_test.go` substitutes a stub `flushingTracker` and asserts log output using `zap.New(core)` with an observer core.
- `cmd/cpg/mcp_e2e_test.go` runs an **in-process fake Hubble relay** (`fakeRelay`: minimal `observerpb.ObserverServer` with GetFlows only) plus a once-built `-race` binary; "no cluster, no kubeconfig required (D-05/D-06)". k8s-facing tests use fake clientsets from `k8s.io/client-go` (`pkg/k8s/preflight_test.go`, `pkg/k8s/portforward_test.go`, `pkg/k8s/exec_test.go`, `pkg/k8s/version_test.go`).
- CLI tests execute cobra commands directly: `cmd := newReplayCmd(); cmd.SetOut/ SetErr(bytes.Buffer); cmd.SilenceUsage = true; cmd.SetArgs([]string{...}); require.NoError(t, cmd.Execute())` (see `cmd/cpg/replay_test.go:49-68`).
- Package-level `logger` is swapped via `initLoggerForTesting(t)` (`zap.NewNop()`) or `initObservedLoggerForTesting(t) *observer.ObservedLogs` (`cmd/cpg/testhelpers_test.go`), restored with `t.Cleanup`.

## Shared flow-fixture builders

- `pkg/policy/testdata/` is a real Go package (not go-testdata dir) imported by tests across packages: `testdata.IngressTCPFlow(srcLabels, dstLabels, dstNs, dstPort)`, `EgressUDPFlow`, `WorldEgressTCPFlow` (`pkg/policy/testdata/ingress_flow.go`) build `*flowpb.Flow` values with Hubble `k8s:`-prefixed labels.
- Cross-package invariant helper: `testdata.AssertHasKubeDNSCompanion(t, cnp)` enforces the DNS-02 invariant (kube-dns companion egress rule, ports 53/UDP+53/TCP, L7 matchName) for any toFQDNs-bearing CNP — used by both internal and external test packages (`pkg/policy/testdata/companion_dns_assert.go`).

## testdata/flows/*.jsonl capture fixtures

`testdata/flows/` holds one-flow-per-line jsonpb Hubble captures consumed by `replay`/`generate` CLI tests via relative paths like `"../../testdata/flows/small.jsonl"` (`cmd/cpg/replay_test.go:49`):

| Fixture | Purpose |
|---|---|
| `small.jsonl` | 3 DROPPED TCP flows (ingress 8080/8443, egress 5432) — the default happy-path input |
| `small.jsonl.gz` | gzip variant of the same capture |
| `l7_http.jsonl`, `l7_dns.jsonl` | L7 (HTTP/DNS) flows for `--l7` tests |
| `mixed_tcp_icmp.jsonl` | TCP + ICMP flows for protocol-merge tests |
| `with_audit.jsonl` | Verdict_AUDIT flows for `--include-audit` |
| `with_non_dropped.jsonl` | contains non-DROPPED verdicts (filtering tests) |
| `malformed.jsonl` | interleaves a `this is not json` line — error-handling tests |
| `empty.jsonl` | zero flows — empty-output tests |

Example fixture line (`testdata/flows/small.jsonl:1`): one JSON object per line with `flow.verdict="DROPPED"`, `traffic_direction`, `source/destination.labels` + `namespace`, `l4.TCP.destination_port`.

## Common test patterns

- Temp dirs via `t.TempDir()` for `--output-dir`/`--evidence-dir`; `t.Helper()` in helpers; channel-drain helpers with deadlines (`drain()` in `pkg/flowsource/file_test.go` uses a 5s timer).
- Invariant guardrails are named and commented with their plan ID, e.g. `TestReplay_L7FlagByteStable` ("PRIMARY guardrail for critical correctness invariant 4"), `TestReplay_IgnoreDropReasonWarnsOnce` (FILTER-03: counts exactly one WARN via observer logs), `TestBuildFilters_AllNamespaces_WithAudit` (AC-4 verdict order).
- Table-driven style appears as `t.Run` subtests with per-case expected values; deterministic-output assertions are common (sorted results, exact counts).
- Factored-out pure functions are unit-tested without side effects, e.g. `shouldExitForInfraDrops` in `pkg/hubble/pipeline.go` exists "for unit testing without os.Exit involvement".
