# Stack Research

**Domain:** Additive milestone on an existing Go CLI + K8s controller-adjacent tool (Cilium/Hubble policy generation)
**Researched:** 2026-07-22
**Confidence:** HIGH — verified by reading the actual vendored module source under `$(go env GOMODCACHE)` for `github.com/cilium/cilium@v1.19.4` and `k8s.io/client-go@v0.35.4`/`k8s.io/apimachinery@v0.35.4` (not just docs), plus a live fetch of `kubernetes/kubectl@master` and current `code.claude.com` docs.

## Headline Finding

**v1.6 needs zero new `go.mod` `require` lines.** Every capability in scope (AUD-01..04, COMPAT-01/02) is reachable through subpackages of modules already direct dependencies: `github.com/cilium/cilium v1.19.4`, `k8s.io/client-go`/`k8s.io/api`/`k8s.io/apimachinery v0.35.4`, `golang.org/x/mod v0.37.0`, `golang.org/x/sync v0.21.0`, `golang.org/x/tools v0.47.0`. This is unusual for a milestone this size and worth stating plainly to the roadmapper: **no dependency-upgrade or new-vendor risk gates any v1.6 phase.** The only "stack" work is wiring already-vendored subpackages that cpg does not currently import.

SKL-01..05 (repo-local skills/agents) need **zero Go dependencies at all** — they are markdown files consumed natively by the Claude Code binary itself, unrelated to `go.mod`.

## Recommended Stack

### Core Technologies (capability → mechanism, all already-vendored)

| Capability | Package(s) | Purpose | Why Recommended |
|------------|-----------|---------|-----------------|
| AUD-01 AUDIT verdict ingestion | *(none — application logic only)* | Widen 5 filter sites to `{DROPPED, AUDIT}` | `Verdict_AUDIT`, `PolicyVerdictNotify` decode, and drop-reason attribution already exist in `github.com/cilium/cilium/api/v1/flow` + `pkg/hubble/parser/threefour` (vendored, verified in draft §2). This is a predicate change in `pkg/hubble/client.go`, `pkg/flowsource/file.go`, `pkg/hubble/aggregator.go` — no new import. |
| AUD-02 bootstrap CNP + runbook | `github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2` (`DefaultDenyConfig`) | Generate `enableDefaultDeny` CNP YAML | Same type already used by `pkg/policy/builder.go` for every CNP cpg emits today; `DefaultDenyConfig` is present in the vendored v1.19.4 API (confirmed by direct grep). Pure `pkg/policy`/`pkg/output` reuse. |
| AUD-03 `pods/exec` to reach `cilium-dbg` | `k8s.io/client-go/tools/remotecommand`, `k8s.io/client-go/transport/spdy` (already imported), `k8s.io/client-go/transport/websocket`, `k8s.io/client-go/kubernetes/scheme`, `k8s.io/api/core/v1` (`PodExecOptions`) | Build the exec stream to `cilium-agent` pods | `remotecommand.NewSPDYExecutor(config, method, url)` calls `spdy.RoundTripperFor(config)` — **the exact same call** `pkg/k8s/portforward.go:51` already makes for port-forward. Confirmed present in `k8s.io/client-go@v0.35.4/tools/remotecommand/{spdy,websocket,fallback}.go`. |
| AUD-03 new-endpoint detection | `github.com/cilium/cilium/pkg/k8s/client/clientset/versioned` (already imported by `pkg/k8s/cluster_dedup.go`), `.../informers/externalversions/cilium.io/v2`, `.../listers/cilium.io/v2`, built on `k8s.io/client-go/tools/cache` | Detect new pods becoming flippable endpoints | Cilium ships a **generated, namespace-filterable `CiliumEndpointInformer`** (`NewFilteredCiliumEndpointInformer`) built directly on `cache.NewSharedIndexInformer` — confirmed by reading the generated file. Zero hand-rolled watch/resync/backoff logic needed. |
| COMPAT-02 Cilium version detection | `github.com/cilium/cilium/api/v1/observer` (`ObserverClient.ServerStatus`/`.GetNodes`, already imported type family) | Detect running Cilium/Hubble version | `ServerStatusResponse.Version` and `Node.Version` are real proto fields ("Version is the version of Cilium/Hubble") reachable on the **same already-open** `observerpb.NewObserverClient(conn)` cpg dials for every invocation (`pkg/hubble/client.go`). Zero new RBAC, zero new connection, works even with digest-pinned images. |
| COMPAT-02 version comparison | `k8s.io/apimachinery/pkg/util/version` (`ParseGeneric`, `AtLeast`) | Parse + compare against the declared floor | Already a direct-dependency subpackage (`k8s.io/apimachinery` v0.35.4). Purpose-built for exactly this: "2+ dot-separated numeric fields... followed by arbitrary uninterpreted data... optionally preceded by v" (official pkg.go.dev docs) — tolerates `-cee.1`/`-eks`/`-rc1` suffixes real Cilium/enterprise images carry. `AtLeast()` is literally the floor check COMPAT-02 needs. |
| SKL-01..05 skills, `cpg-operator` agent | *(none)* | Repo-local LLM-facing procedures | Plain Markdown + YAML frontmatter, natively parsed by the Claude Code binary. No Go, no npm, no runtime. |

### Supporting Libraries (new subpackage imports within already-required modules — zero go.mod diff)

| Import Path | Module (already required) | When to Use |
|---|---|---|
| `k8s.io/client-go/tools/remotecommand` | `k8s.io/client-go v0.35.4` | `NewSPDYExecutor` / `NewWebSocketExecutor` / `NewFallbackExecutor` + `Executor.StreamWithContext(StreamOptions{Stdin,Stdout,Stderr})` to run `cilium-dbg endpoint config <ID> PolicyAuditMode=Enabled` inside the target agent pod. |
| `k8s.io/client-go/transport/websocket` | `k8s.io/client-go v0.35.4` | Only if adopting the WebSocket-primary pattern (see "Stack Patterns by Variant"). Internally uses `github.com/gorilla/websocket` (already an *indirect* dep pulled by client-go itself — cpg never imports gorilla directly). |
| `k8s.io/client-go/kubernetes/scheme` + `k8s.io/api/core/v1.PodExecOptions` | `k8s.io/client-go` / `k8s.io/api v0.35.4` | Build the exec request: `clientset.CoreV1().RESTClient().Post().Resource("pods").Namespace(ns).Name(pod).SubResource("exec").VersionedParams(&corev1.PodExecOptions{Command:[]string{"cilium-dbg","endpoint","config",id,"PolicyAuditMode=Enabled"}, Stdout:true, Stderr:true}, scheme.ParameterCodec)`. Same idiom as the existing `pkg/k8s/portforward.go` URL construction, one line different (`SubResource("exec")` vs `SubResource("portforward")`). |
| `k8s.io/apimachinery/pkg/util/httpstream` (`IsUpgradeFailure`, `IsHTTPSProxyError`) | `k8s.io/apimachinery v0.35.4` | The `shouldFallback` predicate if implementing `NewFallbackExecutor` (see kubectl reference below). Confirmed present in vendored source. |
| `github.com/cilium/cilium/pkg/k8s/client/informers/externalversions` (+ `/cilium.io/v2`) | `github.com/cilium/cilium v1.19.4` | `externalversions.NewSharedInformerFactoryWithOptions(ciliumClientset, resync, externalversions.WithNamespace(ns)).Cilium().V2().CiliumEndpoints().Informer()` — namespace-scoped watch for new/changed endpoints. Reuses the same `ciliumClientset` type already constructed in `pkg/k8s/cluster_dedup.go`. |
| `k8s.io/apimachinery/pkg/util/version` | `k8s.io/apimachinery v0.35.4` | `version.ParseGeneric(imageTag)` / `.AtLeast(floor)` for COMPAT-02's declared-floor check, whichever source (Hubble `ServerStatus` or `ds/cilium` image tag fallback) supplies the raw string. |
| `golang.org/x/sync/errgroup` | `golang.org/x/sync v0.21.0` (already direct, already used in `pkg/hubble/pipeline.go`) | Fan-out per-endpoint audit flips and the lifecycle-bound bulk revert — same concurrency idiom `pkg/session` already established for SESS-05 cleanup. Don't introduce a second concurrency pattern. |
| `golang.org/x/tools/go/callgraph/rta`, `/ssa`, `/ssautil` | `golang.org/x/tools v0.47.0` (already direct, already used in `cmd/cpg/mcp_audit_test.go`) | **Not a new dependency** — flagged here because AUD-04 (SEC-01 two-mode proof) extends the *existing* audit test, it does not add a new static-analysis library. |

### Development Tools

No changes. `golangci-lint`, `govulncheck`, CI pinning, and `go test -race` are unaffected — all new code lives in already-linted packages using already-vetted modules. Sandbox note (repo memory): `make test` is sandbox-denied here; use `rtk proxy go test ./... -count=1 -race` directly.

## Installation

```bash
# No `go get` required. Every needed package is a subpackage of a module
# already in go.mod at a sufficient version. After writing the first import
# of e.g. k8s.io/client-go/tools/remotecommand or the cilium informers
# package, just run:
go mod tidy

# This will at most promote already-indirect entries (e.g. github.com/gorilla/websocket,
# used transitively by k8s.io/client-go/transport/websocket) — it will NOT add a
# new top-level `require`. Verify with:
go mod why k8s.io/client-go/transport/websocket   # sanity check only, not required today
```

## Alternatives Considered

| Recommended | Alternative | Why Not (for v1.6) |
|-------------|-------------|---------------------|
| CiliumEndpoint informer (`cache.SharedIndexInformer` via generated cilium informer) as the "new endpoint" signal | Plain `clientset.CoreV1().Pods(ns).Watch(...)` loop | A Pod ADD event races ahead of Cilium actually creating the endpoint — you'd still need to poll/retry for the endpoint ID before `cilium-dbg endpoint config <ID>` can run. Watching `CiliumEndpoint` directly fires exactly when `Status.ID` becomes available — the natural trigger, one watch instead of two, no hand-rolled retry loop. |
| CiliumEndpoint informer | Raw `Watch()` call without `client-go/tools/cache` | `cache.SharedIndexInformer` gives you resync, reconnect-on-disconnect, delta-FIFO dedup, and an in-memory indexed store for free — a raw `Watch()` loop means reimplementing all of that by hand and testing it under `-race`. The generated `NewFilteredCiliumEndpointInformer` is already built on `cache.NewSharedIndexInformer`, so "informer vs plain Watch" isn't really a choice cpg has to make — the pre-built option is also the zero-effort option. |
| Hubble `ServerStatus`/`GetNodes` RPC as the Cilium version source of truth | `ds/cilium` image tag parsing (kube-system DaemonSet) | Requires **zero new RBAC** (reuses the connection cpg already holds for every invocation) and **survives digest-pinned images** (`image: quay.io/cilium/cilium@sha256:...` has no tag to parse — a real GitOps/Renovate pattern). Image-tag parsing needs `daemonsets/get` in kube-system (same tier as the existing `cilium-envoy` preflight check, so not a new privilege *class*, but still an extra explicit grant) and fails outright on digest pins. Recommend keeping image-tag parsing as an optional cross-check/fallback only, never the primary signal. |
| `k8s.io/apimachinery/pkg/util/version` for parsing/comparing the detected version | `github.com/blang/semver/v4` | Already present, but only as an **indirect** dependency pulled in by `github.com/cilium/cilium/pkg/version` (Cilium's own `ParseKernelVersion` helper, used for *kernel* version checks, not Cilium's own version, and not something cpg calls today — confirmed via `go mod why`). Promoting it to direct adds nothing `apimachinery/util/version` doesn't already give with zero go.mod change, and its parser is stricter (rejects the trailing free-form suffix format apimachinery is explicitly built to tolerate). |
| `k8s.io/apimachinery/pkg/util/version` | `golang.org/x/mod/semver` | Also already a direct dependency (imported today via `golang.org/x/mod/modfile` in `pkg/dropclass/version_test.go`), so also a zero-cost option — but it enforces strict SemVer 2.0 (mandatory `vMAJOR.MINOR.PATCH`, no tolerance for non-conformant suffixes) and is designed for Go *module* versions, not Kubernetes-ecosystem component/image tags. `apimachinery/util/version` is the idiomatic choice the K8s ecosystem itself uses for this exact class of string (client-go's own discovery/version-skew handling). |
| `remotecommand.NewSPDYExecutor` alone (matches existing port-forward pattern 1:1) as the v1.6 MVP | `remotecommand.NewFallbackExecutor(websocketExec, spdyExec, shouldFallback)` (mirrors current `kubectl exec`) | Both already available with zero new deps. SPDY-only is the smaller diff and matches `pkg/k8s/portforward.go` exactly (same `spdy.RoundTripperFor` call, same operational track record since v1.0). Cluster-internal exec to a kube-system agent pod (not through a browser/corporate proxy) is exactly the case where SPDY's known weak spot — intermediary proxies stripping the upgrade — is least likely to bite. Recommend shipping SPDY-only first; the WebSocket+fallback hardening is a documented, low-effort (~10-15 line), zero-new-dependency follow-up, not a blocking v1.6 requirement. |
| Cilium-generated typed clientset (`pkg/k8s/client/clientset/versioned`, already in use) | `k8s.io/client-go/dynamic` (unstructured client) | Not a live question — cpg already uses the generated typed clientset for `CiliumNetworkPolicy` (`pkg/k8s/cluster_dedup.go`). The same clientset's `CiliumV2().CiliumEndpoints(ns)` and the sibling generated informer are the natural, already-consistent choice for `CiliumEndpoint`. Introducing a *second*, untyped client style for one new CRD would be an inconsistency, not a simplification. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|--------------|
| Promoting `github.com/blang/semver/v4` from indirect to direct | Redundant — solves nothing `apimachinery/util/version` doesn't, and it's currently pulled in for an unrelated purpose (Cilium's kernel-version parsing) | `k8s.io/apimachinery/pkg/util/version` |
| A hand-rolled `for { Watch(); reconnect on error }` loop for endpoints or pods | Reinvents `client-go/tools/cache`'s resync/backoff/dedup machinery, needs its own `-race` test suite, duplicates what the generated informer already gives for free | `cache.SharedIndexInformer` via the generated `CiliumEndpointInformer` |
| `ds/cilium` image tag as the *only* version source | Breaks silently on digest-pinned images (no tag) and floating tags (`latest`, `stable`); needs `daemonsets/get` RBAC cpg doesn't strictly need for this purpose | Hubble `ServerStatus`/`GetNodes` (`.Version`) over the already-open observer connection; image tag only as an optional secondary cross-check, parsed leniently and allowed to fail into "version unknown, skipping check" — never abort (matches the existing `pkg/k8s/preflight.go` warn-and-proceed philosophy). |
| `CiliumNode` CRD as a version source | Confirmed by reading `pkg/k8s/apis/cilium.io/v2/types.go`: `CiliumNode`/`NodeSpec` track IPAM/routing state, not agent version. No such field exists. | Hubble `ServerStatus`/`GetNodes`, as above. |
| Assuming the existing SEC-01 verb-name audit (`cmd/cpg/mcp_audit_test.go`) automatically catches the new exec path | `remotecommand.NewSPDYExecutor(...).StreamWithContext(...)` and the WebSocket path are HTTP upgrade calls (`RESTClient().Post()/Get()...SubResource("exec")...Do()`), **not** a typed `.Create(`/`.Update(` clientset call. PROJECT.md's own Key Decision log notes the RTA/SSA scan is verb-*name*-based specifically to avoid false positives — an untyped upgrade call has no such verb name to match, so it can pass through undetected today. This is a direct consequence of the library choice above and should be treated as a required addition to the AUD-04 two-mode proof (an explicit reachability assertion targeting the exec call sites / `remotecommand` symbols), not assumed to fall out of the current test for free. | Extend `cmd/cpg/mcp_audit_test.go`'s allowlist/assertions with an explicit check for reachability of the exec-invoking function(s), mirroring how fs-write functions are already allowlisted by name. |
| A new plugin/runtime framework, package manager, or build step for skills/agents | Not how Claude Code skills or subagents work | Plain `.claude/skills/<name>/SKILL.md` and `.claude/agents/<name>.md` — YAML frontmatter (`name`, `description` required; skills also support `allowed-tools`/`disallowed-tools`; subagents also support `tools`, `model`, `mcpServers`, etc.) + Markdown body, matching the `desloppify` skill already checked into this repo at `.claude/skills/desloppify/SKILL.md`. |
| A typed CRD `Create`/`Update`/`Delete` call for the default-deny CNP itself in v1.6 | Out of scope per the draft (§3.C.4: "v1 mutates ONLY endpoint audit config... The default-deny CNP itself stays human-applied") and per PROJECT.md's existing "No apply_policy MCP tool" constraint | Keep CNP generation write-to-file only (existing `pkg/output` writer); the CNP `Create` path is explicitly a possible *later* extension, not v1.6 stack. |

## Stack Patterns by Variant

**If AUD-03's exec/watch surface ships MCP-flag-gated (`cpg mcp --enable-audit-bootstrap`):**
- Same libraries as the CLI-only variant below. The open decision in the draft (§3.C, "MCP flag-gated vs CLI-only") is a *product-surface* decision, not a stack decision — `remotecommand`, the cilium informer, and `apimachinery/util/version` are needed identically either way.
- Extra requirement: the exec/watch/revert code paths must be reachable only from the gated code path, so AUD-04's SSA/RTA audit can assert "unreachable without the flag" the same way it asserts "zero write verbs reachable" today (same `golang.org/x/tools` machinery, no new library).

**If AUD-03 ships CLI-only (`cpg audit enable|disable -n <ns> --watch --ttl`):**
- Same libraries; the MCP server stays exactly as shipped in v1.5 (zero new tool registration), so SEC-01's *existing* single-mode proof is untouched — only a new CLI command's own tests need coverage. Simplifies AUD-04 to "still nothing new reachable from `runMCPServer`" (cheaper to prove) at the cost of the LLM-driven workflow argument from the draft's rationale (§3.C intro).

**If hardening exec beyond SPDY-only (recommended as a fast-follow, not blocking):**
- Mirror `kubectl`'s current `createExecutor` (verified live against `kubernetes/kubectl@master`, 2026-07-22):
  ```go
  func createExecutor(url *url.URL, config *restclient.Config) (remotecommand.Executor, error) {
      exec, err := remotecommand.NewSPDYExecutor(config, "POST", url)
      if err != nil {
          return nil, err
      }
      websocketExec, err := remotecommand.NewWebSocketExecutor(config, "GET", url.String())
      if err != nil {
          return nil, err
      }
      return remotecommand.NewFallbackExecutor(websocketExec, exec, func(err error) bool {
          return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
      })
  }
  ```
  Note the method asymmetry: WebSocket must use `"GET"` (RFC 6455 §4.1), SPDY uses `"POST"` — same URL, two request objects.

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `github.com/cilium/cilium v1.19.4` | `k8s.io/client-go`/`k8s.io/api`/`k8s.io/apimachinery v0.35.4` | Already proven compatible (current go.mod, 610 passing `-race` tests). All types this research relies on (`DefaultDenyConfig`, `CiliumEndpoint`, generated informers/listers, `ServerStatusResponse.Version`, `Node.Version`) are already present at these exact pinned versions — no bump needed for v1.6. |
| `k8s.io/apimachinery/pkg/util/version` | Any `k8s.io/apimachinery` version cpg will realistically run | Extremely stable, long-lived package (predates client-go's own use of it for server-version handling); no known breaking changes across the k8s.io release train. |
| `k8s.io/client-go/transport/websocket` | `k8s.io/client-go v0.35.4` | Present and used internally by `remotecommand/websocket.go` today — confirmed by reading the vendored file's imports (`gwebsocket "github.com/gorilla/websocket"`, `k8s.io/client-go/transport/websocket`). No separate version pin needed; travels with `k8s.io/client-go`. |
| Runtime cluster Cilium version (the thing COMPAT-02 *detects*) | cpg's own `go.mod` cilium v1.19.4 | **Not the same axis.** The vendored module version governs which Go *types* cpg can compile against (already sufficient for all v1.6 features). The *cluster's* running Cilium version is a runtime data value COMPAT-02 reads and compares — it can be older than v1.19.4 (down to the declared floor) without requiring any go.mod change. Don't conflate "declared floor for cluster compatibility" with "go.mod dependency version" when scoping requirements. |

## Sources

- Direct source inspection (HIGH confidence) — `$(go env GOMODCACHE)/github.com/cilium/cilium@v1.19.4/{api/v1/observer/observer.proto,pkg/k8s/apis/cilium.io/v2/types.go,pkg/k8s/client/informers/externalversions/cilium.io/v2/ciliumendpoint.go,pkg/k8s/client/listers/cilium.io/v2/ciliumendpoint.go,pkg/option/endpoint.go,pkg/version/version.go}` and `$(go env GOMODCACHE)/k8s.io/{client-go,apimachinery}@v0.35.4/{tools/remotecommand/*.go,util/version/version.go,util/httpstream/httpstream.go}`.
- Existing cpg source (HIGH confidence, defines the patterns to mirror) — `pkg/k8s/portforward.go`, `pkg/k8s/preflight.go`, `pkg/k8s/cluster_dedup.go`, `pkg/hubble/client.go`, `pkg/dropclass/version.go`/`version_test.go`, `cmd/cpg/mcp_audit_test.go`, `go.mod`.
- [kubernetes/kubectl `pkg/cmd/exec/exec.go` @ master](https://github.com/kubernetes/kubectl/blob/master/pkg/cmd/exec/exec.go) — live fetch 2026-07-22, HIGH confidence: confirms current production `createExecutor` fallback pattern.
- [k8s.io/apimachinery/pkg/util/version — pkg.go.dev](https://pkg.go.dev/k8s.io/apimachinery/pkg/util/version) — HIGH confidence: official `ParseGeneric`/`AtLeast` semantics.
- [Claude Code — Create custom subagents](https://code.claude.com/docs/en/sub-agents) — live fetch 2026-07-22, HIGH confidence: frontmatter schema (`name`/`description` required, rest optional), `.claude/agents/` project vs `~/.claude/agents/` user scoping.
- [Claude Code — Extend Claude with skills](https://code.claude.com/docs/en/skills) — live fetch 2026-07-22, HIGH confidence: `.claude/skills/<name>/SKILL.md` project scoping, Agent Skills open standard, `allowed-tools`/`disallowed-tools` frontmatter.
- `.planning/drafts/v1.6-audit-onboarding-and-cpg-agent-tooling.md` §2 (verified facts, not re-derived), §5 (research questions this file answers where in scope), §3.E (known floor table — COMPAT-01's exact per-feature introduction versions remain an open item for phase-specific research, not re-verified here since it's a documentation/requirements task, not a stack/dependency one).

---
*Stack research for: cpg v1.6 (Audit-Mode Onboarding & cpg-Dedicated Agent Tooling)*
*Researched: 2026-07-22*
