---
last_mapped_commit: eff36ca37f3c162ebd12c5ddf50350d36a9906b3
last_mapped_at: 2026-10-03
---
# External Integrations

**Analysis Date:** 2026-10-03

## 1. Hubble Relay gRPC (flow streaming)

**What:** Streams dropped/audit flows via the Cilium observer API (`observerpb.ObserverClient.GetFlows`, `Follow: true`, with `Whitelist` filters for verdict/namespace/audit).

**Files:**
- `pkg/hubble/client.go` - `Client.StreamDroppedFlows()` dials with `grpc.NewClient` (TLS or insecure per `--tls`), bounds lazy dial with `waitForConnReady` honoring `--timeout`, then converts the gRPC stream into `<-chan *flowpb.Flow` + `<-chan *flowpb.LostEvent`. `StreamErr()` surfaces transport failures (non-EOF) to the pipeline.
- `pkg/hubble/aggregator.go`, `pkg/hubble/pipeline.go` - consume the channels, aggregate into policies, flush on interval (`errgroup`-coordinated, `golang.org/x/sync`).
- `pkg/flowsource/source.go` - `FlowSource` interface so the same pipeline is fed by gRPC (live) or a file.
- `pkg/flowsource/file.go` - `cpg replay` source reading offline evidence captures (`flowpb.Flow` JSONL).

**Address selection:** explicit `--server` (e.g. `hubble-relay.kube-system.svc:443`), otherwise auto port-forward (see §2). Evidence JSONL also embeds relay/server metadata for replay (`pkg/evidence/schema.go`).

## 2. Kubernetes API (client-go)

**What:** kubeconfig loading, pod discovery, port-forward, exec into cilium-agents, ConfigMap/DaemonSet preflight, CNP list for dedup. All reads; no K8s write verbs anywhere.

**Files & verbs:**
- `pkg/k8s/client.go` - `LoadKubeConfig()` via `clientcmd.NewDefaultClientConfigLoadingRules()` (`KUBECONFIG` → `~/.kube/config` → in-cluster); imports `client-go/plugin/pkg/client/auth` for OIDC/GCP/Azure auth plugins.
- `pkg/k8s/portforward.go` - `PortForwardToRelay()`: `pods/list` in `kube-system` with `k8s-app=hubble-relay`, picks a `Running` pod, builds SPDY tunnel to container port `4245`, local port `0` (auto-assign), returns `localhost:<port>` + cleanup func. Used by `cmd/cpg/generate.go` (`--server` omitted) and `pkg/session/manager.go` (MCP `start_session`).
- `pkg/k8s/exec.go` - `ExecCiliumDbg()`: `pods/exec` into the `cilium-agent` container using kubectl-style WebSocket→SPDY fallback executor (`newFallbackExecutor`, `remotecommand.NewFallbackExecutor` with `httpstream.IsUpgradeFailure`/`IsHTTPSProxyError` predicate). `FindAgentPodForNode()` maps node IP → agent pod via `pods/list` (`k8s-app=cilium`, hostNetwork `Status.HostIP`). `CiliumBinaryName()` picks `cilium-dbg` vs `cilium` based on the 1.15.0 floor.
- `pkg/k8s/preflight.go` - `RunL7Preflight()`: `configmaps/get` (`kube-system/cilium-config`, keys `enable-l7-proxy`, `enable-envoy-config`) and `daemonsets/get` (`kube-system/cilium-envoy`). Advisory only: warnings with fixed remediation strings, never errors; RBAC denials warn-and-proceed.
- `pkg/k8s/cluster_dedup.go` - `LoadClusterPolicies()`: lists `CiliumNetworkPolicies` labeled `app.kubernetes.io/managed-by=cpg` via the Cilium typed clientset (`cilium/pkg/k8s/client/clientset/versioned`) for `--cluster-dedup`.

## 3. Cilium version detection

**What:** Advisory version probe gating feature floors; never blocks.

**Files:**
- `pkg/k8s/version.go` - `CompatInfo` result type (`ClusterVersion`, `VersionsSeen`, `Source`, `BelowFloorFeatures`).
  - Primary: `DetectCiliumVersion()` - `pods/list` in `kube-system` (`k8s-app=cilium`), parses `cilium-agent` container image tags (`parseImageTag` tolerates registry hosts and `@sha256:` digests), takes the minimum version across Running pods. Source: `"pod-images"`.
  - Secondary (MCP-only): `DetectCiliumVersionViaGetNodes()` - direct gRPC `observerpb.GetNodes()` against the relay, reducing per-node `Version` fields, bounded by `versionDetectTimeout` (3s hard ceiling). Source: `"get-nodes"`. Wired in `pkg/session/manager.go` when no kubeconfig exists.
  - Undetermined (`"undetermined"`): nil client, RBAC-denied, or unreachable relay → warn-and-proceed, never an error.
  - `featureFloors` table: baseline `1.14.0`, cilium-dbg naming `1.15.0`, `enableDefaultDeny` `1.16.0`. The 1.17 `proxy-visibility` ceiling is deliberately doc-only (README).
- Consumers: `cmd/cpg/generate.go` (preflight warnings), `pkg/k8s/exec.go` (binary name), `pkg/dropclass/version.go` (5 lines: re-exports floor constant for hint gating).

## 4. MCP server surface (stdio)

**What:** `cpg mcp` runs a readonly-ish MCP server over JSON-RPC stdio using `github.com/modelcontextprotocol/go-sdk` (`mcp.IOTransport` bound to real `os.Stdin`/`os.Stdout` with a no-op close wrapper; stray writes globally redirected to stderr; zap→slog bridge via `zap/exp/zapslog` for SDK logs).

**Files & tools:**
- `cmd/cpg/mcp.go` - server construction, transport, signal handling, `mgr.Shutdown()`.
- `cmd/cpg/mcp_tools.go` - session tools: `start_session` (ReadOnlyHint: false), `get_status` (readonly), `stop_session` (idempotent). Args validated in `package main` (namespace/all_namespaces mutual exclusion, allowlists, `parseOptionalDuration` capped at 24h) before `pkg/session.Manager.Start`.
- `cmd/cpg/mcp_query.go` - read tools: `list_policies`, `get_policy`, `get_cluster_health`.
- `cmd/cpg/mcp_query_flows.go` - `list_dropped_flows` (paginated).
- `cmd/cpg/mcp_query_evidence.go` - `get_evidence`.
- `cmd/cpg/mcp_query_pagination.go` - shared manual JSON Schema via `github.com/google/jsonschema-go`.
- `cmd/cpg/mcp_bootstrap.go` - `get_bootstrap_policy`.
- `pkg/session/manager.go` - one active session; session IDs `"sess_<uuid>"` (`github.com/google/uuid`); background capture + port-forward per session.

## 5. krew / GoReleaser packaging

**What:** Distribution as `kubectl cilium-policy-gen` krew plugin plus plain binaries.

**Files:**
- `.goreleaser.yml` - build ID `kubectl-cilium_policy_gen` (same `./cmd/cpg/` main), archive id `krew` named `cilium-policy-gen_{{.Os}}_{{.Arch}}` (LICENSE included) for krew-release-bot consumption; release footer documents `go install` / `kubectl krew install cilium-policy-gen` / tarball install.
- `cmd/cpg/main.go` - `isKubectlPlugin()` checks `kubectl-` argv0 prefix and rewrites usage to `kubectl cilium-policy-gen`.
- `docs/plans/2026-03-10-krew-submission-design.md` - krew-release-bot auto-PR flow to `kubernetes-sigs/krew-index` (manifest `plugins/cilium-policy-gen.yaml`, planned `.krew.yaml` + `.github/workflows/krew-update.yml`).
- `.github/workflows/release.yml` - release-please creates the tag; goreleaser then builds/publishes (linux/darwin, amd64/arm64, `CGO_ENABLED=0`, `-s -w` ldflags, `go mod verify` + `go test -race` before-hooks).

---

*External integrations analysis: 2026-10-03*
