---
last_mapped_commit: eff36ca37f3c162ebd12c5ddf50350d36a9906b3
last_mapped_at: 2026-10-03
---
# Technology Stack

**Analysis Date:** 2026-10-03

## Languages

**Primary:**
- Go 1.25.1 (toolchain go1.25.12, per `go.mod`) - all application code in `cmd/cpg/` and `pkg/`

**Secondary:**
- YAML - GitHub Actions workflows (`.github/workflows/ci.yml`, `.github/workflows/release.yml`), `.goreleaser.yml`, docs examples
- Shell (via Makefile targets) - build/lint/test orchestration in `Makefile`

## Runtime

**Environment:**
- No runtime VM: cpg compiles to a static native binary (`CGO_ENABLED=0` in `.goreleaser.yml`)
- Kubernetes cluster reachable via kubeconfig for live mode (`pkg/k8s/client.go` loads `KUBECONFIG` env / `~/.kube/config` / in-cluster)
- Hubble Relay must be deployed in the cluster (pod `k8s-app=hubble-relay` in `kube-system`, port 4245 — `pkg/k8s/portforward.go`)

**Package Manager:**
- Go modules (`go.mod`, `go.sum` present)
- Build: `make build` → `go build -ldflags "-X main.version=$(git describe --tags --always --dirty)" -o bin/cpg ./cmd/cpg/`

## Frameworks

**Core:**
- No web/UI framework; CLI built on `github.com/spf13/cobra v1.10.2` (`cmd/cpg/main.go`: commands `generate`, `replay`, `explain`, `mcp`, `bootstrap`, `audit-window`)
- Structured logging: `go.uber.org/zap v1.27.1` + `go.uber.org/zap/exp v0.3.0` (zapslog bridge used by the MCP server, `cmd/cpg/mcp.go`)

**Testing:**
- Stdlib `go test` + `github.com/stretchr/testify v1.11.1`
- `make test` runs `go test ./... -count=1 -race` (race detector is also enforced in CI and by goreleaser's `before` hook)
- Fake gRPC streams via hand-rolled seams (e.g. `flowStream` interface in `pkg/hubble/client.go`); package-var seams like `execCiliumDbgFn` (`pkg/k8s/exec.go`)

**Build/Dev:**
- `golangci-lint` v2 config in `.golangci.yml` (govet, errcheck, staticcheck, unused, errorlint; CI pins v2.12.2 with `only-new-issues: true`)
- GoReleaser v2 (`.goreleaser.yml`): two build IDs (`cpg` and krew-flavored `kubectl-cilium_policy_gen`), linux/darwin × amd64/arm64
- `release-please` (googleapis/release-please-action) drives tagging in `.github/workflows/release.yml`, then goreleaser publishes

## Key Dependencies

**Critical:**
- `github.com/cilium/cilium v1.19.4` - generated Hubble flow/observer protobuf APIs (`api/v1/flow`, `api/v1/observer`, used in `pkg/hubble/client.go`, `pkg/hubble/aggregator.go`) and CiliumNetworkPolicy types + typed clientset (`cilium/pkg/k8s/apis/cilium.io/v2`, `cilium/pkg/k8s/client/clientset/versioned`, used in `pkg/policy/builder.go`, `pkg/k8s/cluster_dedup.go`)
- `k8s.io/client-go v0.35.4` (+ `k8s.io/api`, `k8s.io/apimachinery` v0.35.4) - kubeconfig loading, pod list, SPDY port-forward, WebSocket/SPDY exec, ConfigMap/DaemonSet preflight
- `google.golang.org/grpc v1.79.3` (+ `protobuf`) - Hubble Relay `GetFlows` streaming and `GetNodes` version probe
- `github.com/modelcontextprotocol/go-sdk v1.6.1` - MCP server over stdio (`cmd/cpg/mcp.go`, tool registration in `cmd/cpg/mcp_tools.go`, `mcp_query*.go`, `mcp_bootstrap.go`)
- `github.com/spf13/cobra v1.10.2` - CLI structure (`cmd/cpg/main.go`); also auto-detects kubectl-plugin invocation via `kubectl-` argv0 prefix (`isKubectlPlugin()`)

**Supporting:**
- `github.com/google/jsonschema-go v0.4.3` - hand-built input schemas for MCP tools with manual JSON Schema fields (`cmd/cpg/mcp_query_pagination.go` etc.)
- `github.com/pmezard/go-difflib` - unified diffs of generated vs cluster policies (`pkg/diff/yaml.go`)
- `sigs.k8s.io/yaml v1.6.0` - YAML marshal/unmarshal for policy output (`pkg/output/writer.go`, `pkg/policy/dedup.go`)
- `github.com/google/uuid` - MCP session IDs (`pkg/session/manager.go`: `"sess_" + uuid.NewString()`)
- `golang.org/x/sync v0.21.0` - `errgroup` in the capture pipeline (`pkg/hubble/pipeline.go`)
- `go.uber.org/zap` - sole logging dependency; all warnings/errors go through it

## Configuration

**Environment:**
- No required env vars. kubeconfig resolved by `clientcmd.NewDefaultClientConfigLoadingRules()` (`pkg/k8s/client.go`: `KUBECONFIG` env → `~/.kube/config` → in-cluster)
- CLI flags carry all config: `--server`, `--tls`, `--timeout`, `--l7`, `--no-l7-preflight`, `--cluster-dedup`, `--log-level`, `--json` (`cmd/cpg/generate.go`, `cmd/cpg/commonflags.go`)
- Version injected at link time via `-X main.version=` (`cmd/cpg/main.go`: `var version = "dev"`)

**Build:**
- `go.mod` / `go.sum` - dependency pinning
- `Makefile` - `build`, `test`, `lint`, `clean` targets
- `.goreleaser.yml` - release builds, archives, krew archive naming (`cilium-policy-gen_{{.Os}}_{{.Arch}}`)
- `.golangci.yml` - linter gate
- `.github/workflows/ci.yml` (lint + test on master/PRs), `.github/workflows/release.yml` (release-please → goreleaser)

## Platform Requirements

**Development:**
- Any platform with Go ≥ 1.25; `golangci-lint` for `make lint`
- No Docker/local cluster required for unit tests (gRPC/k8s surfaces are faked via seams)

**Production/Distribution:**
- Single static binary, distributed three ways (`.goreleaser.yml` release footer):
  1. `go install github.com/SoulKyu/cpg/cmd/cpg@<tag>`
  2. krew plugin as `kubectl cilium-policy-gen` (binary named `kubectl-cilium_policy_gen`; krew-release-bot PR flow designed in `docs/plans/2026-03-10-krew-submission-design.md`)
  3. tar.gz from GitHub Releases (linux/darwin, amd64/arm64)
- Target clusters: Cilium ≥ 1.14 baseline, with feature floors `1.15.0` (cilium-dbg naming) and `1.16.0` (enableDefaultDeny) enforced at runtime in `pkg/k8s/version.go` (`featureFloors` table)

---

*Stack analysis: 2026-10-03*
*Update after major dependency changes*
