# Phase 23: Managed Audit Window + SEC-01 Evolution - Research

**Researched:** 2026-07-22
**Domain:** Kubernetes `pods/exec` (SPDY) + CiliumEndpoint CRD watch, wired into cpg's existing SESS-05 bounded-cleanup fan-out; SEC-01 static-reachability audit evolution
**Confidence:** HIGH

## Summary

Every mechanism this phase needs is a subpackage of a module cpg already directly depends on — confirmed by reading the vendored `github.com/cilium/cilium@v1.19.4` and `k8s.io/client-go@v0.35.4` source trees, not by inference from docs. Zero new `go.mod` lines. The exec primitive is a one-line variant of `pkg/k8s/portforward.go` (`SubResource("exec")` instead of `"portforward"`, `remotecommand.NewSPDYExecutor` instead of `portforward.New`). The CiliumEndpoint CRD is already reachable through the same typed clientset `pkg/k8s/cluster_dedup.go` already constructs (`ciliumclient.NewForConfig(config).CiliumV2().CiliumEndpoints(ns)`), so the new-endpoint watcher needs no new client construction path, only a new namespace-scoped call.

Three facts materially correct or sharpen the CONTEXT.md/PATTERNS.md draft, all found by reading vendored source directly:

1. **Exec argument syntax uses lowercase `enable`/`disable`, not `Enabled`/`Disabled`** in `cilium-dbg`'s own documented example — but `NormalizeBool` (the parser actually invoked) lowercases its input first, so `PolicyAuditMode=Enabled` (CONTEXT.md's phrasing) parses identically to `PolicyAuditMode=enable`. Both forms work; the canonical/documented form is lowercase. [VERIFIED: vendored `pkg/option/option.go` `NormalizeBool`]
2. **Reading current per-endpoint audit state requires a *different* subcommand and a *different* JSON shape than the mutating one.** `cilium-dbg endpoint config <id>` (no value args) prints `models.EndpointConfigurationStatus{Realized: {Options: map[string]string}}`; `cilium-dbg endpoint get <id> -o json` prints `[]models.Endpoint{{Spec: {Options: map[string]string}}}` (a **JSON array**, one element) — the runbook already uses the `get` form (`-o jsonpath='{[*].spec.options.PolicyAuditMode}'`). Both code paths ultimately serialize the setting via `IntOptions.GetMutableModel()`, which formats the value as the literal string **`"Enabled"`** or **`"Disabled"`** (title case, not `"1"/"0"`). [VERIFIED: vendored `pkg/option/option.go:198-213`, `pkg/endpoint/api.go:248-263`]
3. **The RTA SEC-01 audit's `reflect.Value.Call` over-approximation is real and already proven to affect a cobra `RunE` target in this exact codebase** (`cmd/cpg/bootstrap.go:153-157`'s own comment). A naive "assert `runAuditWindow` is absent from `bfsFromRoot(rtaRes.CallGraph, runMCPServerNode).visited`" tripwire **will false-positive**, because every address-taken function (which includes every `RunE:` value assigned to a cobra `*cobra.Command` field, including the not-yet-written `runAuditWindow`) gains a synthetic edge from `(*reflect.Value).Call` the instant that intrinsic is itself reachable — and it almost certainly is, transitively, from `runMCPServer` (the MCP SDK's tool-handler binding is a very plausible real call site, though this session did not trace the exact one). `golang.org/x/tools/go/callgraph.Edge.Site == nil` is the load-bearing, **officially documented** signal that distinguishes this synthetic edge from a genuine call site (`callgraph.go:86-87`: *"Site is nil for edges originating in synthetic or intrinsic functions, e.g. reflect.Value.Call or the root of the call graph"*). See Common Pitfall 1 and the SEC-01 Tripwire Design section below for the fix.

**Primary recommendation:** build `pkg/auditwindow` as a new package (SESS-05-shaped `Manager`, `sync.Once`-guarded `Close`, unconditionally-bounded `Shutdown`) driving two already-vendored primitives — `pkg/k8s/exec.go` (new, SPDY sibling of `portforward.go`) and the existing `ciliumclient.NewForConfig(...).CiliumV2().CiliumEndpoints(ns)` typed client (watch, not a new informer) — with `cmd/cpg/audit_window.go` as a thin, foreground, signal-bound cobra command exactly mirroring `generate.go`'s signal-handling shape. The SEC-01 tripwire must scan **genuine-edge-only** reachable sets (filtering `Edge.Site == nil`) rather than raw BFS-visited-set membership, or it will misfire in both directions.

## User Constraints (from CONTEXT.md)

### Locked Decisions
- CLI-only command (Variant B). No MCP tool, no MCP flag, no session property — zero diffs under `cmd/cpg/mcp*.go` except the SEC-01 tripwire test. The `pods/exec` privileged mutation stays a human act.
- Foreground, supervised command: `cpg audit-window -n <ns>` opens the window, watches, and reverts on EVERY exit (explicit Ctrl+C/SIGTERM, TTL expiry, transport death). A foreground process is the structural can't-be-left-open guarantee — no detached/daemon mode.
- `--ttl <duration>` with a sane default (e.g. 30m): the window is ALWAYS time-bounded; TTL expiry reverts and exits. Exact default at planner's discretion.
- Per-endpoint `PolicyAuditMode` flips via `pods/exec` on the Cilium agent pod of each endpoint's node (`cilium-dbg endpoint config <id> PolicyAuditMode=Enabled` — binary name `cilium-dbg` vs `cilium` gated on the Phase 21 detected version, ≥1.15 = `cilium-dbg`).
- SPDY executor (`remotecommand.NewSPDYExecutor`, `StreamWithContext`) — matches current kubectl exec; WebSocket fallback is deferred (AUD-FUT-01).
- Never touch an endpoint already in audit before cpg started (revert-only-ours); bookkeeping keyed on `CiliumEndpoint` UID, never the raw per-node integer endpoint ID (ID reuse hazard).
- New-endpoint watcher: namespace-scoped `CiliumEndpoint` watch; flips new endpoints as they appear; the race window for brand-new endpoints (enforced before the flip lands) is documented honestly in the runbook, not papered over.
- Revert rides the exact SESS-05 bounded cleanup fan-out pattern (same shape as `pkg/session` Manager shutdown) — no parallel construct, no independent `time.AfterFunc` racing an explicit stop, per-endpoint revert success/failure tracked and reported.
- Precondition check: refuse to open a window if daemon-wide `policy-audit-mode` is already active — hard refusal naming the reason, not a silent proceed. Detection source at planner's discretion (cilium-config ConfigMap read preferred over exec if RBAC-clean).
- The existing `TestMCPAuditReadonlyReachability` stays byte-identical in what it proves. Add a tripwire assertion: the exec-executor constructor / audit-window entry function is NOT reachable from `runMCPServer`, and IS reachable ONLY from the audit-window command entry point among cpg-owned roots — failing loudly if any other path grows one.
- RBAC step-up (`pods/exec` create, `ciliumendpoints` list/watch) documented in README + runbook as exclusive to `cpg audit-window` — readonly commands need none of it.
- README: MCP "readonly, period" claim STAYS. CLI section states plainly that `cpg audit-window` is the one mutating command.

### Claude's Discretion
- Exact TTL default/bounds, flag names beyond `-n`/`--ttl`, package layout (`pkg/auditwindow` vs under `pkg/k8s`), watcher implementation details, error wording, runbook integration (extend docs/bootstrap-runbook.md audit-window step vs separate doc).

### Deferred Ideas (OUT OF SCOPE)
- MCP flag-gated audit window (Variant A) — future milestone if LLM-driven onboarding proves needed.
- WebSocket exec with SPDY fallback (AUD-FUT-01).
- cpg-managed CNP apply/delete (AUD-FUT-02).

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| AUD-03 | Managed, lifecycle-bound per-endpoint audit window (CLI-only) | Exec Mechanics, CiliumEndpoint CRD, Daemon-Wide Precondition, Architecture Patterns, Code Examples sections below cover the full flip→watch→revert path with exact verified types/functions |
| AUD-04 | SEC-01 structural evolution proving the mutation stays unreachable from `runMCPServer` | SEC-01 Tripwire Design section — the `Edge.Site == nil` filter is the load-bearing mechanism; Code Examples gives a compiling sketch |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| CLI flag parsing / signal handling | CLI (cobra command) | — | `cmd/cpg/audit_window.go`, mirrors `generate.go`/`bootstrap.go` |
| Daemon-wide precondition check | API/Backend (K8s client read) | — | ConfigMap read against the K8s API server, no agent involved |
| Endpoint discovery (namespace → CiliumEndpoint list) | API/Backend (K8s client read) | — | Typed clientset `CiliumV2().CiliumEndpoints(ns)` |
| New-endpoint watch | API/Backend (K8s watch) | — | Same typed clientset, `Watch()` not a new informer |
| Per-endpoint audit-mode flip/read | API/Backend → Agent (pods/exec passthrough) | — | SPDY exec into the cilium-agent pod is a K8s-API-mediated RPC to a specific node's agent process; cpg never talks to the agent directly (no node access) |
| Revert bookkeeping / bounded shutdown | API/Backend (in-process state machine) | — | `pkg/auditwindow.Manager`, same tier as `pkg/session.Manager` |
| SEC-01 tripwire | Build-time / CI (static analysis) | — | `go test` at build/CI time, not a runtime tier at all |

No browser, CDN, or persistent-storage tier is involved anywhere in this phase — it is exclusively CLI + Kubernetes-API-mediated backend.

## Package Legitimacy Audit

No new external packages are introduced by this phase. Every capability (SPDY exec, CiliumEndpoint typed client, ConfigMap read) is a subpackage of `github.com/cilium/cilium@v1.19.4`, `k8s.io/client-go@v0.35.4`, `k8s.io/api@v0.35.4`, and `k8s.io/apimachinery@v0.35.4` — all four already direct requires in `go.mod` (confirmed: `grep -n "cilium/cilium\|k8s.io/api \|k8s.io/apimachinery\|k8s.io/client-go" go.mod`). No `go get`, no `go mod tidy` promotion, no slopcheck run needed — there is nothing new to audit.

**Packages removed due to slopcheck [SLOP] verdict:** none (N/A — no new packages).
**Packages flagged as suspicious [SUS]:** none (N/A).

## Standard Stack

### Core (all already-vendored subpackages — no `go.mod` changes)

| Package | Version (pinned) | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `k8s.io/client-go/tools/remotecommand` | v0.35.4 | `NewSPDYExecutor` + `StreamWithContext` — exec into the cilium-agent pod | Identical mechanism `kubectl exec` uses; cpg's own `pkg/k8s/portforward.go` already uses the sibling SPDY dialer shape one line away |
| `k8s.io/client-go/kubernetes/scheme` | v0.35.4 | `scheme.ParameterCodec` for `.VersionedParams(&corev1.PodExecOptions{...}, scheme.ParameterCodec)` | The only supported way to serialize `PodExecOptions` into the request's query string; already registered in the vendored `kubernetes/scheme/register.go` (`var ParameterCodec = runtime.NewParameterCodec(Scheme)`) |
| `k8s.io/api/core/v1` (`PodExecOptions`) | v0.35.4 | Typed request body for the `pods/exec` subresource (`Stdin`/`Stdout`/`Stderr`/`TTY`/`Container`/`Command`) | Confirmed struct at `k8s.io/api@v0.35.4/core/v1/types.go:7273` |
| `k8s.io/client-go/util/exec` (`exec.CodeExitError`) | v0.35.4 | Exit-code-aware error returned by `StreamWithContext` when the remote command exits non-zero (protocol v4+) | Confirmed at `remotecommand/v4.go:111-114`'s `errorDecoderV4.decode` |
| `github.com/cilium/cilium/pkg/k8s/client/clientset/versioned` (`ciliumclient`) | v1.19.4 | Typed clientset already imported by `pkg/k8s/cluster_dedup.go`; exposes `.CiliumV2().CiliumEndpoints(ns)` with `List`/`Watch`/`Get` | Zero new client construction path — literally the same `ciliumclient.NewForConfig(config)` call cluster_dedup.go already makes |
| `github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2` (`ciliumv2.CiliumEndpoint`) | v1.19.4 | Typed CRD struct: `ObjectMeta.UID`, `Status.ID` (int64 endpoint ID), `Status.Networking.NodeIP` (string) | Confirmed fields at vendored `pkg/k8s/apis/cilium.io/v2/types.go:36-105,335-344` |
| `k8s.io/apimachinery/pkg/watch` | v0.35.4 | `watch.Interface`/`.ResultChan()` returned by `CiliumEndpoints(ns).Watch(ctx, ...)` | Standard generated-client-go watch contract; ctx-bound, closes on cancel |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `k8s.io/apimachinery/pkg/apis/meta/v1` (`metav1.ListOptions`) | v0.35.4 | `List`/`Watch` options for `CiliumEndpoints(ns)` calls | Every list/watch call |
| `k8s.io/client-go/transport/spdy` (`spdy.RoundTripperFor`, `spdy.NewDialer`) | v0.35.4 | Underlying SPDY transport — but `remotecommand.NewSPDYExecutor(config, method, url)` wraps this internally; only needed directly if bypassing `NewSPDYExecutor` (not recommended) | Not directly needed — `NewSPDYExecutor` is the public entry point |
| `encoding/json` (stdlib) | — | Parse `cilium-dbg endpoint get <id> -o json` stdout to check current `PolicyAuditMode` before flipping (never-touch-already-in-audit check) | Every "read before flip" call |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Raw `watch.Interface` on the typed client | `github.com/cilium/cilium/pkg/k8s/client/informers/externalversions/cilium.io/v2` `CiliumEndpointInformer` (`cache.NewSharedIndexInformer`) | Informer gives automatic relist/resync/backoff robustness against watch drops, at the cost of its own goroutine lifecycle to fold into SESS-05's bounded shutdown. For a foreground, TTL-bounded (default likely ≤ a few hours) CLI command, a plain ctx-bound `watch.Interface` is simpler to reason about and bound; recommend the plain watch unless a spike shows watch-drop is a real problem in target environments. **[ASSUMED]** — not empirically tested against a live cluster this session. |
| SPDY-only exec | `remotecommand.NewFallbackExecutor(websocket, spdy, shouldFallback)` | Explicitly deferred (AUD-FUT-01) per locked decision — do not build this phase. |

**Installation:** none — no new `go.mod` `require` lines. `go build ./...` alone proves this; no `go get` step exists in the plan.

**Version verification:** confirmed via `grep -n "cilium/cilium\|k8s.io/api \|k8s.io/apimachinery\|k8s.io/client-go" go.mod` against the repo's actual `go.mod` — `github.com/cilium/cilium v1.19.4`, `k8s.io/api v0.35.4`, `k8s.io/apimachinery v0.35.4`, `k8s.io/client-go v0.35.4`. All four subpackages above were then read directly from `$(go env GOMODCACHE)/<module>@<version>` at these exact pinned versions — not from documentation or training-data recollection.

## Architecture Patterns

### System Architecture Diagram

```
 operator shell
      │
      │ cpg audit-window -n <ns> --ttl 30m
      ▼
 ┌─────────────────────────────────────────────────────────────┐
 │ cmd/cpg/audit_window.go : runAuditWindow                    │
 │  1. signal.NotifyContext(cmd.Context(), SIGINT, SIGTERM)     │
 │  2. auditWindowDetectVersion(ctx) ── seam, warn-and-proceed  │
 │  3. checkDaemonAuditMode(ctx) ─────┐                         │
 │     (ConfigMap read, kube-system)  │ hard-refuse if "true"   │
 │  4. wm := auditwindow.NewManager(ctx, logger)                │
 │  5. wm.Open(ctx, ns) ───────────────────────────────────┐    │
 └──────────────────────────────────────────────────────────┼──┘
                                                              │
   ┌──────────────────────────────────────────────────────────▼───┐
   │ pkg/auditwindow.Manager                                       │
   │                                                                │
   │  Open(ctx, ns):                                                │
   │   a. List CiliumEndpoints(ns)          ── cilium.io v2 CRD    │
   │   b. for each: read current state ──── pods/exec (read-only) │
   │      if already "Enabled" → SKIP (never touch pre-existing)  │
   │      else → flip to Enabled ──────────── pods/exec (mutate)  │
   │      record {UID: endpointID} in "ours" map                   │
   │   c. spawn watcher goroutine: Watch(ns) CiliumEndpoints       │
   │      on ADDED event → same read-then-flip-if-needed logic     │
   │                                                                │
   │  Close(ctx)  [sync.Once]:                                      │
   │   for each UID in "ours" map:                                 │
   │      flip back to Disabled ──────────── pods/exec (mutate)   │
   │      record success/failure per endpoint                      │
   │                                                                │
   │  Shutdown() [unconditional, bounded]:                          │
   │   cancel watcher ctx; bounded-wait; Close() if not already run │
   └────────────────────────────────────────────────────────────────┘
                    │                              │
                    ▼                              ▼
         ┌────────────────────┐         ┌────────────────────────┐
         │ pkg/k8s/exec.go     │         │ K8s API server          │
         │ ExecPolicyAudit-    │◄───────►│ pods/exec subresource   │
         │ Mode(ctx, config,   │  SPDY   │ (kube-system,           │
         │ nodeIP, id, enable) │  POST   │  cilium-agent pod)      │
         └────────────────────┘         └───────────┬─────────────┘
                                                      │ kubelet proxies
                                                      ▼
                                          cilium-agent container:
                                          cilium-dbg endpoint config <id> \
                                            PolicyAuditMode=enable|disable
```

Every exit path (`<-ctx.Done()` from SIGINT/SIGTERM, TTL timer fire, or the watcher goroutine observing a transport failure) funnels into the same `wm.Shutdown()` call exactly once — the SESS-05 `sync.Once` + bounded-wait pattern, never a second independent teardown path.

### Recommended Project Structure
```
cmd/cpg/
├── audit_window.go        # cobra command: flags, signal handling, seams (mirrors bootstrap.go)
└── audit_window_test.go   # SEC-01 tripwire (Property 3) + CLI-level flag/seam tests

pkg/auditwindow/
├── manager.go             # Manager: Open/Close/Shutdown, "ours" bookkeeping keyed on UID
├── manager_test.go        # exit-path tests (explicit Close, SIGTERM race, TTL, wedged exec)
└── daemon_precondition.go # checkDaemonAuditMode (ConfigMap read) — or fold into manager.go

pkg/k8s/
├── exec.go                # ExecPolicyAuditMode / ReadPolicyAuditMode (SPDY sibling of portforward.go)
└── exec_test.go           # fake-clientset-based pod-selection tests (node → agent pod mapping)
```

### Pattern 1: SPDY Exec Request Construction (sibling of `portforward.go`)
**What:** Build a `pods/exec` SPDY request against a specific cilium-agent pod, targeting its `cilium-agent` container.
**When to use:** Every per-endpoint flip or read.
**Example:**
```go
// Source: verified against k8s.io/client-go@v0.35.4 vendored source
// (remotecommand/spdy.go, api/core/v1/types.go:7273, kubernetes/scheme/register.go:86)
// and pkg/k8s/portforward.go's proven SPDY dialer shape.
package k8s

import (
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"
)

const ciliumAgentContainerName = "cilium-agent" // already declared in version.go — reuse, don't redeclare

// execCiliumDbg runs `cilium-dbg <args...>` inside the cilium-agent container
// of podName (kube-system), returning combined stdout/stderr and any error.
// A non-nil error wrapping exec.CodeExitError means the remote command itself
// exited non-zero (bad endpoint ID, bad option, agent socket unreachable) —
// never a transport failure, which surfaces as a different error type.
func execCiliumDbg(ctx context.Context, config *rest.Config, clientset kubernetes.Interface, podName string, args []string) (stdout, stderr string, err error) {
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(ciliumNamespace). // "kube-system", declared in preflight.go
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: ciliumAgentContainerName,
			Command:   append([]string{"cilium-dbg"}, args...),
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", "", fmt.Errorf("creating SPDY executor: %w", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
	})
	if err != nil {
		var codeErr exec.CodeExitError
		if ok := extractCodeExitError(err, &codeErr); ok {
			return stdoutBuf.String(), stderrBuf.String(), fmt.Errorf(
				"cilium-dbg exited %d in pod %s: %s (stderr: %s)", codeErr.Code, podName, err, stderrBuf.String())
		}
		return stdoutBuf.String(), stderrBuf.String(), fmt.Errorf("exec transport failure in pod %s: %w", podName, err)
	}
	return stdoutBuf.String(), stderrBuf.String(), nil
}
```
Note: `errors.As(err, &codeErr)` (stdlib `errors`) is the correct way to detect `exec.CodeExitError`, since `exec.CodeExitError` implements `error` and `Unwrap`-style matching works through `errors.As` — the sketch above names an `extractCodeExitError` helper as a stand-in for `errors.As(err, &codeErr)` to keep the snippet self-contained; use `errors.As` directly in the real implementation.

### Pattern 2: Endpoint ID + Node Mapping (namespace CiliumEndpoint → cilium-agent pod)
**What:** For each `CiliumEndpoint` in the target namespace, resolve which cilium-agent pod (in kube-system) to exec into.
**When to use:** Both the initial namespace sweep (`Open`) and every watcher-observed new endpoint.
**Example:**
```go
// Source: verified against pkg/k8s/apis/cilium.io/v2/types.go:335-344 (EndpointNetworking.NodeIP)
// and the existing ciliumAgentLabelSelector const (version.go) / findRelayPod pattern (portforward.go).
func findAgentPodForNode(ctx context.Context, clientset kubernetes.Interface, nodeIP string) (*corev1.Pod, error) {
	pods, err := clientset.CoreV1().Pods(ciliumNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: ciliumAgentLabelSelector, // "k8s-app=cilium", declared in version.go
	})
	if err != nil {
		return nil, fmt.Errorf("listing cilium-agent pods: %w", err)
	}
	for i := range pods.Items {
		// cilium-agent runs hostNetwork; Status.HostIP == the node's IP, which
		// is exactly what CiliumEndpoint.Status.Networking.NodeIP reports.
		if pods.Items[i].Status.Phase == corev1.PodRunning && pods.Items[i].Status.HostIP == nodeIP {
			return &pods.Items[i], nil
		}
	}
	return nil, fmt.Errorf("no running cilium-agent pod found on node with IP %s", nodeIP)
}
```
This avoids any `nodes/get` RBAC — it reuses the same `pods/list` verb in kube-system that `findRelayPod`/`DetectCiliumVersion` already require unconditionally, matching PITFALLS.md's "no new RBAC class" finding.

### Pattern 3: Read-Then-Flip (never-touch-already-in-audit)
**What:** Before flipping an endpoint, read its current `PolicyAuditMode` and skip if it is already `"Enabled"`.
**When to use:** Every endpoint touched by `Open` and by the watcher.
**Example:**
```go
// Source: verified against pkg/option/option.go:198-213 (GetMutableModel formats
// as literal "Enabled"/"Disabled") and pkg/endpoint/api.go:248-263 (models.Endpoint.Spec.Options).
type endpointGetJSON struct {
	Spec struct {
		Options map[string]string `json:"options"`
	} `json:"spec"`
}

func readPolicyAuditMode(ctx context.Context, config *rest.Config, clientset kubernetes.Interface, podName string, endpointID int64) (enabled bool, err error) {
	stdout, _, err := execCiliumDbg(ctx, config, clientset, podName,
		[]string{"endpoint", "get", fmt.Sprint(endpointID), "-o", "json"})
	if err != nil {
		return false, err
	}
	var parsed []endpointGetJSON // top-level JSON is an ARRAY (cilium-dbg always wraps in []models.Endpoint)
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return false, fmt.Errorf("parsing endpoint get output: %w", err)
	}
	if len(parsed) == 0 {
		return false, fmt.Errorf("endpoint %d: empty response", endpointID)
	}
	return parsed[0].Spec.Options["PolicyAuditMode"] == "Enabled", nil
}
```

### Pattern 4: Daemon-Wide Precondition (ConfigMap read, RBAC-cheapest)
**What:** Refuse to open a window if the daemon-wide `policy-audit-mode` ConfigMap key is `"true"`.
**When to use:** Once, at `Open()` time, before any exec call.
**Example:**
```go
// Source: verified against the vendored Helm chart template itself —
// install/kubernetes/cilium/templates/cilium-configmap.yaml:1031-1032:
//   {{- if hasKey .Values "policyAuditMode" }}
//   policy-audit-mode: {{ .Values.policyAuditMode | quote }}
// and docs.cilium.io/en/stable/security/policy-creation.rst:48 (kubectl patch example),
// and pkg/option/config.go:863 (PolicyAuditModeArg = "policy-audit-mode").
func checkDaemonAuditMode(ctx context.Context, clientset kubernetes.Interface) (active bool, err error) {
	cm, err := clientset.CoreV1().ConfigMaps(ciliumNamespace).Get(ctx, "cilium-config", metav1.GetOptions{})
	if apierrors.IsForbidden(err) {
		return false, nil // RBAC-denied: undetermined, warn-and-proceed like every other precondition (matches version.go's own convention)
	}
	if err != nil {
		return false, fmt.Errorf("reading cilium-config ConfigMap: %w", err)
	}
	return cm.Data["policy-audit-mode"] == "true", nil
}
```
The ConfigMap key is absent entirely unless an operator explicitly set `policyAuditMode` in Helm values (`{{- if hasKey .Values "policyAuditMode" }}` — the key is conditionally rendered), so `cm.Data["policy-audit-mode"]` correctly defaults to `""` (not `"true"`) when unset — no special-casing needed for the common case.

### Anti-Patterns to Avoid
- **A second independent watcher/timer construct racing `Manager.Close`:** PITFALLS.md Pitfall 5 — fold the watcher's stop signal and the TTL timer into the exact same ctx-cancellation + `sync.Once` shape `pkg/session/manager.go` already uses. Do not write a second `time.AfterFunc`.
- **Bookkeeping keyed on raw `Status.ID` (int64):** endpoint IDs are per-node, small integers, and get reused after an endpoint is destroyed and a new one created on the same node — key every "ours" map strictly on `ObjectMeta.UID` (a `types.UID`, globally unique), looking up the current `Status.ID` fresh at flip/revert time.
- **A file-write path anywhere in `runAuditWindow`'s command:** would force a new `fsWriteAllowlist` entry in `TestMCPAuditReadonlyReachability` for the exact reason `bootstrap.go:153-157` already documents (the `reflect.Value.Call` sweep pollutes `cpgOwned`, the audit's restricted-scan set, with every RunE target regardless of real reachability) — keep all output on stdout/stderr, like `bootstrap.go` does.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| SPDY exec transport, protocol negotiation, exit-code decoding | A custom HTTP/SPDY client and stdout/stderr multiplexer | `k8s.io/client-go/tools/remotecommand.NewSPDYExecutor` + `StreamWithContext` | Handles protocol v1-v5 negotiation, exit-code-aware error stream decoding (`exec.CodeExitError`), and stdin/stdout/stderr multiplexing — the exact code path `kubectl exec` itself uses |
| CiliumEndpoint list/watch, JSON (un)marshaling | Raw `dynamic.Interface` + manual unstructured parsing | The typed `ciliumclient.NewForConfig(config).CiliumV2().CiliumEndpoints(ns)` clientset (already imported elsewhere in cpg) | Generated, type-safe, zero manual JSON handling; `pkg/k8s/cluster_dedup.go` already proves the construction path works in this exact codebase |
| Version comparison for the `cilium-dbg` vs `cilium` binary gate | String-splitting a version tag | `k8s.io/apimachinery/pkg/util/version.ParseGeneric`/`.AtLeast` (already used in `pkg/k8s/version.go`'s `featureFloors`/`belowFloorFeatures`) | Tolerant of `-cee.1`/`-eks`/`-rc1` suffixes; already the established pattern in this codebase, don't reinvent |

**Key insight:** every "don't hand-roll" item here is not merely "a library exists" — it is "cpg's own codebase already calls this exact library function for a sibling purpose," so the audit-window feature introduces zero new client-construction or parsing idioms, only zero new call sites of already-proven patterns.

## SEC-01 Tripwire Design

This is the phase's highest-risk correctness surface and deserves its own section, since a naively-designed tripwire either false-positives (blocking every future PR) or false-negatives (never actually proving anything).

### The problem, precisely

`golang.org/x/tools/go/callgraph/rta` (vendored at `go.mod`'s `golang.org/x/tools v0.47.0`) adds a synthetic edge from `(*reflect.Value).Call` to **every address-taken function in the whole program**, the instant `reflect.Value.Call` is itself reachable (`rta.go:181-207`, `visitAddrTakenFunc`). Assigning any function as a `cobra.Command.RunE` field value takes its address — so `runBootstrap`, and the not-yet-written `runAuditWindow`, are both address-taken. `cmd/cpg/bootstrap.go:153-157`'s own comment already documents this concretely: `runBootstrap` **is** present in `TestMCPAuditReadonlyReachability`'s `bfsRes.visited`/`cpgOwned` set today, reached via this synthetic path, not via any real call from `runMCPServer`.

Any tripwire written as `assert.NotContains(t, symbolSet(bfsRes.visited), "cmd/cpg.runAuditWindow")` will **fail** the moment `runAuditWindow` exists, for the same reason `runBootstrap` is already in that set — a false positive with zero relationship to whether the MCP server can actually reach the audit-window code.

### The fix: filter on `callgraph.Edge.Site == nil`

`golang.org/x/tools/go/callgraph.Edge`'s own doc comment (`callgraph.go:86-87`) states: *"Site is nil for edges originating in synthetic or intrinsic functions, e.g. reflect.Value.Call or the root of the call graph."* This is the exact, officially-documented signal needed to distinguish a real call edge from a reflect-sweep edge. `bfsFromRoot` (mcp_audit_test.go:144-168) currently traverses every `n.Out` edge unconditionally; a genuine-only variant simply skips edges where `edge.Site == nil`:

```go
// bfsFromRootGenuine is bfsFromRoot's SEC-01-tripwire-safe sibling: it skips
// any edge with a nil Site, which per callgraph.Edge's own doc comment marks
// a synthetic edge (reflect.Value.Call's sweep, or the graph root) rather
// than a real call site. Reachability computed this way cannot be inflated
// by any cobra RunE value merely existing somewhere in the program — a
// concrete, provable improvement over raw bfsFromRoot for any assertion that
// depends on "is this specific function really called from here," as opposed
// to Stage-3's existing direct-call-instruction scan (which never relied on
// visited-set membership meaning "genuinely called" in the first place).
func bfsFromRootGenuine(cg *callgraph.Graph, root *ssa.Function) bfsResult {
	res := bfsResult{visited: map[*ssa.Function]bool{root: true}, parent: map[*ssa.Function]*ssa.Function{}}
	rootNode := cg.Nodes[root]
	if rootNode == nil {
		return res
	}
	queue := []*callgraph.Node{rootNode}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, edge := range n.Out {
			if edge.Site == nil {
				continue // synthetic (reflect.Value.Call sweep or graph root) — not a real call
			}
			callee := edge.Callee
			if callee == nil || callee.Func == nil || res.visited[callee.Func] {
				continue
			}
			res.visited[callee.Func] = true
			res.parent[callee.Func] = n.Func
			queue = append(queue, callee)
		}
	}
	return res
}
```

### The tripwire itself (Property 3)

Add a **new Property 3**, following the exact style of the existing Property 1 (K8s write verbs) / Property 2 (fs writes) direct-call-instruction scan — NOT a bare `visited`-set membership check:

```go
// Property 3 (SEC-01 evolution, AUD-04): no cpg-owned function genuinely
// reachable from runMCPServer may contain a static call instruction to
// remotecommand.NewSPDYExecutor. "Genuinely reachable" uses
// bfsFromRootGenuine (Edge.Site != nil only) specifically so a future
// audit-window-shaped RunE value existing in the binary can never, by mere
// existence, satisfy or violate this property — only a REAL call chain can.
execConstructor := "k8s.io/client-go/tools/remotecommand.NewSPDYExecutor"

genuineFromMCP := bfsFromRootGenuine(rtaRes.CallGraph, mcpRoot)
genuineCpgOwnedFromMCP := restrictToCpgOwned(genuineFromMCP.visited) // same filter as existing Stage 3
for f := range genuineCpgOwnedFromMCP {
	for _, b := range f.Blocks {
		for _, instr := range b.Instrs {
			call, ok := instr.(ssa.CallInstruction)
			if !ok {
				continue
			}
			if callee := call.Common().StaticCallee(); callee != nil && callee.String() == execConstructor {
				t.Errorf("SEC-01/AUD-04: %s genuinely (non-reflect) reaches %s from runMCPServer\n  call path: %s",
					f.String(), execConstructor, callPathFrom(genuineFromMCP, mcpRoot, f))
			}
		}
	}
}

// Positive path (AUD-04's "reachable ONLY from audit-window" half): the exec
// constructor must be genuinely reachable from runAuditWindow.
auditRoot := mainPkg.Func("runAuditWindow")
require.NotNil(t, auditRoot, "cmd/cpg must declare func runAuditWindow")
genuineFromAudit := bfsFromRootGenuine(rtaRes.CallGraph, auditRoot)
foundExecCaller := false
for f := range restrictToCpgOwned(genuineFromAudit.visited) {
	for _, b := range f.Blocks {
		for _, instr := range b.Instrs {
			if call, ok := instr.(ssa.CallInstruction); ok {
				if callee := call.Common().StaticCallee(); callee != nil && callee.String() == execConstructor {
					foundExecCaller = true
					t.Logf("SEC-01/AUD-04: exec constructor genuinely reachable via %s", callPathFrom(genuineFromAudit, auditRoot, f))
				}
			}
		}
	}
}
require.True(t, foundExecCaller, "SEC-01/AUD-04: remotecommand.NewSPDYExecutor must be genuinely reachable from runAuditWindow — audit is vacuous otherwise")
```

`restrictToCpgOwned` is the existing `cpgOwned`-building loop (mcp_audit_test.go:288-293) factored into a small helper so both the existing test and this new one share it.

This design satisfies ARCHITECTURE.md's Tension-4 recommendation (a path-scoped Property 3, materially stronger than a flat allowlist) **without** needing PITFALLS.md's build-tag fork at all, because Variant B (CLI-only, locked) already means `runAuditWindow` lives in a sibling command tree with no runtime flag involved — the only remaining subtlety was RTA's reflect sweep, now handled by the `Edge.Site` filter above.

## Common Pitfalls

### Pitfall 1: The `TestRunbookNeverSuggestsDaemonWideAudit` golden test scopes the hyphenated token, not the per-endpoint one
**What goes wrong:** `cmd/cpg/runbook_test.go`'s existing test asserts the literal string `"policy-audit-mode"` (hyphenated, lowercase — the daemon-wide flag/ConfigMap-key form) appears ONLY in the runbook's leading warning block (before the first `## ` heading). If Phase 23's runbook update documents the new precondition check (which reads the `cilium-config` ConfigMap's `policy-audit-mode` key) anywhere in the body, this existing golden test breaks.
**Why it happens:** `PolicyAuditMode` (the per-endpoint option name, no hyphen, title-case) is a **different token** already used freely throughout the runbook body — easy to conflate the two when writing new prose about the daemon-wide precondition check.
**How to avoid:** When documenting the new precondition refusal in the runbook or README, refer to it as "the daemon-wide audit mode setting" or similar prose, OR deliberately widen `firstSectionHeadingIndex`'s scope in the test as part of this phase's plan (a conscious golden-test edit, not an accidental regression).
**Warning signs:** `go test ./cmd/cpg/... -run TestRunbookNeverSuggestsDaemonWideAudit` failing after a runbook edit that looks unrelated to the daemon-wide warning.

### Pitfall 2: `cilium-dbg endpoint config`'s read path and `endpoint get`'s read path return different JSON shapes
**What goes wrong:** `cilium-dbg endpoint config <id>` (no value args) returns `models.EndpointConfigurationStatus{Realized: {Options}}`; `cilium-dbg endpoint get <id> -o json` returns a JSON **array** of `models.Endpoint{Spec: {Options}}`. Code written against one shape will silently fail to parse (or worse, silently zero-value) against the other.
**Why it happens:** Both ultimately format the option value the same way (`"Enabled"`/`"Disabled"` via `IntOptions.GetMutableModel()`), so a quick manual test against one subcommand can look correct while the implementation actually targets the other subcommand's shape.
**How to avoid:** Standardize on `endpoint get <id> -o json` (already the form the existing runbook uses) and always unmarshal into `[]struct{ Spec struct{ Options map[string]string } }`, never a bare object.
**Warning signs:** `json.Unmarshal` returning a "cannot unmarshal object into Go value of type []T" error, or (worse) silently succeeding into an always-empty struct if the target type is too permissive (e.g. `map[string]any`).

### Pitfall 3: RTA's `reflect.Value.Call` sweep (see SEC-01 Tripwire Design)
Already covered in detail above — repeated here as a pitfall entry because it is the single highest-risk item in this phase's own test suite, not just its production code.

### Pitfall 4: `pods/exec` RBAC cannot be scoped to "only cilium-agent pods" by name
**What goes wrong:** Cilium agent pod names are DaemonSet-generated (`cilium-<random-suffix>`), so a `Role`'s `resourceNames` field cannot pin `pods/exec` to "only cilium-agent pods" — the grant is effectively "exec into any pod in kube-system" unless combined with an external admission controller (Kyverno/OPA Gatekeeper).
**Why it happens:** Kubernetes RBAC `resourceNames` requires exact, static names; DaemonSet pod names are neither exact-known in advance nor stable across restarts.
**How to avoid:** Document this limitation explicitly in the runbook/README RBAC section (per the locked decision's "documented completely separately from base RBAC" requirement) rather than implying a tighter scope than RBAC can actually enforce. If a cluster has an admission controller, recommend policy-based scoping there as a defense-in-depth note, not a cpg-enforced guarantee.
**Warning signs:** A security review assuming the granted Role is narrower than it actually is.

### Pitfall 5: `NormalizeBool`'s tolerance masks a real input-validation gap
**What goes wrong:** `cilium-dbg`'s `NormalizeBool` accepts `"true"/"on"/"enable"/"enabled"/"1"` case-insensitively — convenient, but means a typo like `PolicyAuditMode=enalbed` is REJECTED (good), while `PolicyAuditMode=TRUE` is silently accepted even though it isn't the documented canonical form. cpg's own Go code constructing the exec command args should use one canonical literal (`"enable"`/`"disable"`, matching the cmdref example) rather than relying on the parser's tolerance, so a future reader of cpg's source sees the exact documented form.
**How to avoid:** Hard-code `"enable"`/`"disable"` (lowercase) as cpg's own constants for the values it passes, even though `"Enabled"`/`"Disabled"` would also work — reduces cognitive load for anyone cross-referencing cpg's exec calls against the upstream cmdref docs.

## Code Examples

### Reading `go.mod`-confirmed direct dependencies (verification command used this session)
```bash
grep -n "cilium/cilium\|k8s.io/api \|k8s.io/apimachinery\|k8s.io/client-go" go.mod
# github.com/cilium/cilium v1.19.4
# k8s.io/api v0.35.4
# k8s.io/apimachinery v0.35.4
# k8s.io/client-go v0.35.4
```

### Manager shape (mirrors `pkg/session/manager.go`'s SESS-05 fields exactly)
```go
// pkg/auditwindow/manager.go
package auditwindow

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/rest"
	"github.com/SoulKyu/cpg/pkg/k8s"
)

type endpointRecord struct {
	uid       string // types.UID as string — never the raw integer endpoint ID
	currentID int64  // re-read fresh at revert time; the ID may have drifted since Open (rare, but possible on agent restart)
}

type Manager struct {
	mu       sync.Mutex
	rootCtx  context.Context
	logger   *zap.Logger
	config   *rest.Config
	ours     map[string]*endpointRecord // keyed by CiliumEndpoint UID
	watchCancel context.CancelFunc
	watchDone   chan struct{}

	closeOnce sync.Once
	stopWait  time.Duration
	removeWait time.Duration

	// seams, mirroring session.Manager's detectVersionFn/resolveSetupFn pattern
	execFn func(ctx context.Context, config *rest.Config, podName string, args []string) (stdout, stderr string, err error)
}

func NewManager(rootCtx context.Context, logger *zap.Logger, config *rest.Config) *Manager {
	m := &Manager{
		rootCtx:    rootCtx,
		logger:     logger,
		config:     config,
		ours:       make(map[string]*endpointRecord),
		stopWait:   5 * time.Second,
		removeWait: 2 * time.Second,
	}
	m.execFn = k8s.ExecCiliumDbg // production binding; tests substitute a stub
	return m
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| `cilium endpoint config` (binary name) | `cilium-dbg endpoint config` | Cilium 1.15 (PR #28085) | Already gated correctly by the Phase 21 `DetectCiliumVersion` floor table (`pkg/k8s/version.go`'s `"cilium-dbg binary naming", "1.15.0"` entry) — no new work needed here, just reuse |
| Manual `kubectl exec ... cilium-dbg endpoint config` per the runbook's current steps | `cpg audit-window -n <ns> --ttl <d>` (this phase) | This phase | The runbook's "Enable Per-Endpoint Audit Mode" / "Disable Per-Endpoint Audit Mode" sections (lines 69-87, 132-143 of `docs/bootstrap-runbook.md`) should be rewritten to reference the new command, replacing the manual `kubectl exec` + manual revert-reminder steps — this is a real, in-scope doc change, not optional polish |

**Deprecated/outdated:** none newly deprecated by this phase; the runbook's manual-exec steps become superseded (not deprecated by upstream) once `cpg audit-window` exists.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `runMCPServer`'s transitive closure genuinely (not merely via reflect-sweep) reaches a real call to `(*reflect.Value).Call` somewhere (plausibly inside the MCP SDK's tool-dispatch binding) | SEC-01 Tripwire Design, Summary point 3 | If false, the reflect-sweep pollution described may not actually affect `bfsFromRoot(..., runMCPServerNode)` today — but the `Edge.Site == nil` filter fix is correct and harmless regardless of whether this specific pollution is currently active; it protects against the pollution existing NOW or being introduced by a future MCP SDK upgrade. Low risk either way — worth a quick `t.Logf` in the tripwire test confirming whether `reflectValueCall` was actually reached, so the team knows which case they're in. |
| A2 | A plain ctx-bound `watch.Interface` (not a `SharedIndexInformer`) is sufficiently robust for a foreground, TTL-bounded CLI command's watch-drop tolerance | Standard Stack, Alternatives Considered | If wrong (i.e., watch drops are common enough in target clusters to matter), new endpoints could go briefly unobserved until the watch is manually re-established — mitigated by documenting this as part of the already-locked "race window is documented honestly, not solved" decision, but worth a cluster-empirical spike if the team has doubts |
| A3 | The Phase 21 `DetectCiliumVersion`/`bootstrapDetectVersion`-style seam (package-level function var) is the right shape for `auditWindowDetectVersion` too, reusing `versionPreflightTimeout` unmodified | Architecture Patterns | Low risk — this is an established, already-reviewed pattern in this exact codebase (three prior instances), not a novel design choice |

**If this table is empty:** N/A — see entries above; none block planning, all are low-risk and mitigated by existing patterns or explicit documentation.

## Open Questions

1. **Exact TTL default value**
   - What we know: CONTEXT.md's example is `30m`; "Claude's Discretion" explicitly defers the exact value to the planner.
   - What's unclear: nothing left to resolve here — this is a planner-owned discretion item, not a research gap.
   - RESOLVED: Not a research question — recommend `30m` as the default (matches CONTEXT.md's own example, a reasonable onboarding-session length), planner may adjust.

2. **Does `runMCPServer`'s real (non-reflect-swept) call graph reach `(*reflect.Value).Call`?**
   - What we know: The MCP SDK (`github.com/modelcontextprotocol/go-sdk/mcp`) plausibly uses reflection to bind tool-handler functions to JSON schemas, which is a common pattern for this class of library; `bootstrap.go`'s own comment confirms the sweep pollutes `cpgOwned` today for `runBootstrap`.
   - What's unclear: This session did not trace the exact call site inside the vendored `go-sdk/mcp` package that invokes `reflect.Value.Call` (if any) — confirming it precisely was out of scope given the phase's time budget, and is not load-bearing for the fix.
   - RESOLVED: Irrelevant to the fix's correctness — the `Edge.Site == nil` filter in `bfsFromRootGenuine` is correct and safe whether or not this specific pollution is currently active; the planner does not need to resolve this before writing the tripwire.

3. **Watch-drop robustness (plain `watch.Interface` vs `SharedIndexInformer`)**
   - What we know: A plain `Watch()` call is simpler to fold into the SESS-05 bounded-shutdown shape; an informer adds relist/resync robustness at the cost of a second goroutine-lifecycle shape to reconcile with `Manager.Shutdown()`.
   - What's unclear: Whether watch drops are common enough in the team's actual target clusters (network policy, apiserver load-shedding, etc.) to justify the added complexity — this is empirical, not resolvable by source reading.
   - RESOLVED: Recommend the plain `watch.Interface` for this phase (simpler, matches the "no parallel construct" locked decision more naturally), with the informer noted as a documented future upgrade path if watch-drop proves to be a real operational problem — not a blocker for planning.

4. **Does `CiliumEndpointList.Watch` reconnect automatically on a transient apiserver disconnect, or does cpg need to re-`Watch()` in a loop?**
   - What we know: Standard client-go generated `Watch()` methods return a single `watch.Interface` whose `ResultChan()` closes when the underlying HTTP connection ends (including transient disconnects) — client-go does NOT auto-reconnect a raw `Watch()` call (that reconnect logic lives inside `cache.Reflector`/`SharedIndexInformer`, which this phase is deliberately not using per Open Question 3's resolution).
   - What's unclear: Nothing — this follows directly from client-go's documented `Watch()` contract (a bare `watch.Interface`, no reflector wrapping) once Alternatives Considered's choice (plain watch, not informer) is made.
   - RESOLVED: `pkg/auditwindow.Manager`'s watcher goroutine must re-`Watch()` (with a bounded backoff, e.g. matching `stopWait`'s scale) whenever `ResultChan()` closes AND ctx is not yet done — a one-line reconnect loop, not a full reflector — else a transient apiserver blip silently ends new-endpoint detection for the rest of the window. This must be an explicit task in the plan, not an assumed detail.

5. **README RBAC section wording — does documenting `pods/exec` scope collide with any existing golden test beyond `TestReadmeCompatSection`?**
   - What we know: `TestReadmeCompatSection` (readme_compat_test.go) pins specific tokens (`1.14`-`1.17`, PR numbers, `cpg bootstrap`, `docs/bootstrap-runbook.md`) but does not currently constrain where RBAC prose may appear.
   - What's unclear: Nothing outstanding — confirmed by reading the full test file this session; no additional golden constraint exists beyond the one already documented in Pitfall 1 for the runbook.
   - RESOLVED: No collision beyond the one already flagged (Pitfall 1, runbook only). The README RBAC addition is unconstrained by any existing golden test.

## Environment Availability

No external tool/service dependency beyond what `cpg` already requires (a `kubeconfig` and a reachable Kubernetes API server) — this phase adds no new environment prerequisite. `cilium-dbg`/`cilium` itself runs INSIDE the cilium-agent container (accessed via `pods/exec`), never on the machine running `cpg` — so there is nothing new to probe on the local environment.

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** none — the existing Phase 21 version-detection warn-and-proceed pattern already covers "Cilium version undetermined" gracefully; this phase's daemon-wide precondition check has its own RBAC-denied warn-and-proceed branch (Pattern 4 above).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `testify` (`require`/`assert`), matching every existing cpg test file |
| Config file | none — `go test ./...` via the repo `Makefile`'s `test:` target |
| Quick run command | `go test ./pkg/auditwindow/... ./pkg/k8s/... -run <TestName> -count=1` |
| Full suite command | `go test ./... -count=1 -race` (per `Makefile`; SEC-01's own tests carry a documented ~45-76s budget under `-race`, do not lower below ~120s timeout for `cmd/cpg` package tests) |

Per the project's own sandbox note (session memory): the sandbox may deny `go` under `make` — use `go test ./... -count=1 -race` directly (or via the project's `rtk proxy go test ...` wrapper) rather than `make test` if that denial recurs.

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| AUD-03 | Daemon-wide precondition hard-refuses when ConfigMap key is `"true"` | unit | `go test ./pkg/auditwindow/... -run TestManager_Open_RefusesWhenDaemonAuditActive -count=1` | ❌ Wave 0 |
| AUD-03 | RBAC-denied ConfigMap read warns and proceeds (not a hard failure) | unit | `go test ./pkg/auditwindow/... -run TestManager_Open_ProceedsWhenPreconditionUndetermined -count=1` | ❌ Wave 0 |
| AUD-03 | Never touch an endpoint already in audit (`skip` on pre-existing `"Enabled"`) | unit | `go test ./pkg/auditwindow/... -run TestManager_Open_SkipsAlreadyAuditedEndpoint -count=1` | ❌ Wave 0 |
| AUD-03 | Revert-only-ours keyed on UID, not raw endpoint ID (ID reuse safe) | unit | `go test ./pkg/auditwindow/... -run TestManager_Close_UsesUIDNotReusedEndpointID -count=1` | ❌ Wave 0 |
| AUD-03 | Revert fires on explicit `Close()` call | unit | `go test ./pkg/auditwindow/... -run TestManager_Close -count=1` | ❌ Wave 0 |
| AUD-03 | Revert fires on SIGTERM (races an in-progress watch) | unit (fake signal via ctx cancel) | `go test ./pkg/auditwindow/... -run TestManager_Shutdown_OnCtxCancel -count=1` | ❌ Wave 0 |
| AUD-03 | Revert fires on TTL expiry | unit | `go test ./cmd/cpg/... -run TestAuditWindow_TTLExpiryTriggersRevert -count=1` | ❌ Wave 0 |
| AUD-03 | Revert still completes (bounded) when the exec transport is wedged | unit (wedged exec stub, mirrors `manager_test.go`'s `wedgedRunPipeline`) | `go test ./pkg/auditwindow/... -run TestManager_Shutdown_WedgedExecDoesNotBlock -count=1` | ❌ Wave 0 |
| AUD-03 | Per-endpoint revert success/failure individually tracked and reported | unit | `go test ./pkg/auditwindow/... -run TestManager_Close_ReportsPerEndpointResult -count=1` | ❌ Wave 0 |
| AUD-03 | New-endpoint watcher flips newly-observed endpoints | unit (fake watch.Interface) | `go test ./pkg/auditwindow/... -run TestManager_Watcher_FlipsNewEndpoint -count=1` | ❌ Wave 0 |
| AUD-03 | Watch reconnects (bounded backoff) after `ResultChan()` closes mid-window | unit | `go test ./pkg/auditwindow/... -run TestManager_Watcher_ReconnectsOnChannelClose -count=1` | ❌ Wave 0 |
| AUD-04 | `remotecommand.NewSPDYExecutor` NOT genuinely reachable from `runMCPServer` | SSA/RTA structural | `go test ./cmd/cpg/... -run TestAuditWindowNotReachableFromMCP -count=1` (budget: same ~45-76s as `TestMCPAuditReadonlyReachability`) | ❌ Wave 0 |
| AUD-04 | `remotecommand.NewSPDYExecutor` IS genuinely reachable from `runAuditWindow` (non-vacuous) | SSA/RTA structural | same test file, second assertion | ❌ Wave 0 |
| criterion 5 | README RBAC section documents `pods/exec` + `ciliumendpoints` scoped exclusively to `cpg audit-window` | golden pinning | `go test ./cmd/cpg/... -run TestReadmeAuditWindowSection -count=1` | ❌ Wave 0 |
| criterion 5 | Runbook references the real `cpg audit-window` command, honestly documents the race window | golden pinning | `go test ./cmd/cpg/... -run TestRunbookAuditWindowStep -count=1` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** the specific unit test(s) for that task's behavior (quick run command above)
- **Per wave merge:** `go test ./pkg/auditwindow/... ./pkg/k8s/... ./cmd/cpg/... -count=1 -race`
- **Phase gate:** full suite (`go test ./... -count=1 -race`) green before `/gsd-verify-work`, including the existing `TestMCPAuditReadonlyReachability` (must remain byte-identical pass) and the new `TestAuditWindowNotReachableFromMCP`

### Wave 0 Gaps
- [ ] `pkg/auditwindow/manager_test.go` — new file, no existing test infrastructure for this package (package doesn't exist yet)
- [ ] `pkg/k8s/exec_test.go` — new file; needs a `fake.NewSimpleClientset(...)`-based node→agent-pod mapping test (mirrors `portforward_test.go`'s `findRelayPod` fake-clientset pattern) plus an injectable `execFn` seam for the SPDY call itself (no fake-SPDY-server infrastructure exists in cpg today — recommend the seam approach over building one, matching `session.Manager`'s `detectVersionFn` precedent)
- [ ] `cmd/cpg/audit_window.go` + `cmd/cpg/audit_window_test.go` — new files; `audit_window_test.go` needs the `bfsFromRootGenuine`/Property-3 additions described in SEC-01 Tripwire Design, most naturally added to the existing `mcp_audit_test.go` (shares `bfsResult`, `callPathFrom`, `cpgModulePrefix`) rather than duplicating those helpers in a new file
- [ ] Framework install: none — `testing`/`testify` already used everywhere in this repo

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | cpg delegates entirely to the operator's own kubeconfig; no new auth surface introduced |
| V3 Session Management | no | N/A — CLI-only, no session concept beyond the process's own lifetime |
| V4 Access Control | yes | Kubernetes RBAC (`pods/exec` create, `ciliumendpoints` list/watch, `configmaps` get) is the sole access-control mechanism; cpg itself performs no additional authorization — it inherits whatever the operator's kubeconfig grants |
| V5 Input Validation | yes | Namespace name validated via `k8s.io/apimachinery/pkg/util/validation.IsDNS1123Label` (existing `validateBootstrapNamespace` pattern, reuse verbatim for `-n`); endpoint IDs are always sourced from `CiliumEndpoint.Status.ID` (never user-supplied text), so no injection surface into the `cilium-dbg endpoint config <id> ...` argv exists as long as the ID is always machine-derived, never operator-typed |
| V6 Cryptography | no | No new cryptographic operation; SPDY/TLS transport security is entirely delegated to `k8s.io/client-go`'s existing `rest.Config`/kubeconfig TLS handling, never hand-rolled |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Command injection into the `cilium-dbg` argv via a maliciously-crafted namespace or endpoint identifier | Tampering | Namespace validated via `IsDNS1123Label` before any exec call; the `Command []string` field of `PodExecOptions` is passed as a real argv array (not shell-interpolated — `PodExecOptions.Command` is `[]string`, executed "not within a shell" per its own doc comment at `types.go:7298`), so there is no shell-metacharacter injection surface even without additional escaping; endpoint IDs are always `int64` values read from the CRD, never operator-supplied strings |
| Privilege escalation via over-broad `pods/exec` RBAC (Pitfall 4) | Elevation of Privilege | Document the RBAC scoping limitation explicitly (cannot be scoped to "only cilium-agent pods" by RBAC `resourceNames` alone); recommend admission-controller-based tightening as defense-in-depth, not a cpg-enforced guarantee |
| Leaving a namespace permanently in audit mode (structural DoS-adjacent risk: silently-disabled enforcement) | Denial of Service (of the *policy*, not the process) | This is the entire point of the SESS-05-shaped bounded revert fan-out — every exit path (explicit stop, SIGTERM, TTL, transport death) funnels into one `sync.Once`-guarded, independently-bounded `Close`/`Shutdown` pair, exactly like `pkg/session/manager.go`'s proven pattern |
| A future PR accidentally wiring the exec-executor into an MCP tool handler | Elevation of Privilege (readonly guarantee regression) | The SEC-01 Tripwire (Property 3, this document) fails loudly and specifically the moment any cpg-owned function genuinely reachable from `runMCPServer` calls `remotecommand.NewSPDYExecutor` |

## Sources

### Primary (HIGH confidence)
- Direct source inspection of `$(go env GOMODCACHE)/github.com/cilium/cilium@v1.19.4` — `pkg/option/{option,runtime_options,config}.go`, `pkg/endpoint/api.go`, `pkg/k8s/apis/cilium.io/v2/types.go`, `pkg/k8s/client/clientset/versioned/typed/cilium.io/v2/ciliumendpoint.go`, `cilium-dbg/cmd/{endpoint_config,endpoint_get,helpers}.go`, `Documentation/cmdref/cilium-dbg_endpoint_config.md`, `install/kubernetes/cilium/templates/{cilium-configmap.yaml,cilium-agent/daemonset.yaml}`, `Documentation/security/policy-creation.rst`
- Direct source inspection of `$(go env GOMODCACHE)/k8s.io/client-go@v0.35.4` — `tools/remotecommand/{remotecommand,spdy,v4,errorstream}.go`, `kubernetes/scheme/register.go`
- Direct source inspection of `$(go env GOMODCACHE)/k8s.io/api@v0.35.4` — `core/v1/types.go` (`PodExecOptions`)
- Direct source inspection of `$(go env GOMODCACHE)/golang.org/x/tools@v0.47.0` — `go/callgraph/{callgraph,rta/rta}.go` (the `Edge.Site == nil` synthetic-edge documentation and the `reflect.Value.Call` sweep mechanism)
- cpg's own repo source at HEAD — `pkg/k8s/{portforward,version,cluster_dedup}.go`, `pkg/session/manager.go`, `cmd/cpg/{main,mcp,bootstrap,bootstrap_test,mcp_audit_test,readme_compat_test,runbook_test}.go`, `docs/bootstrap-runbook.md`, `.planning/research/SUMMARY.md`
- WebSearch (verified against the vendored Helm chart template directly, not trusted standalone): `cilium-config` ConfigMap key `policy-audit-mode` / Helm value `policyAuditMode`

### Secondary (MEDIUM confidence)
- None used standalone without primary-source cross-verification this session.

### Tertiary (LOW confidence)
- None retained — every finding in this document was cross-checked against vendored source before being stated as fact.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — every type/function cited was read directly from the vendored module at the exact `go.mod`-pinned version, not recalled from training data
- Architecture: HIGH — every pattern mirrors an existing, working cpg file cited by path
- SEC-01 tripwire design: HIGH — the `Edge.Site == nil` mechanism is officially documented in the vendored `x/tools` source itself, not inferred
- Pitfalls: HIGH for the golden-test-collision and JSON-shape pitfalls (directly read source); MEDIUM for the reflect-sweep-origin claim in Assumption A1 (the sweep mechanism is HIGH confidence; which specific call site in the MCP SDK triggers it, if any, was not traced this session — see Open Question 2)

**Research date:** 2026-07-22
**Valid until:** 30 days (stable — pinned dependency versions, no fast-moving ecosystem risk)
