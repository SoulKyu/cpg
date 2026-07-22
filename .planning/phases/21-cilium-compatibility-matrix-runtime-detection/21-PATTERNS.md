# Phase 21: Cilium Compatibility Matrix + Runtime Detection - Pattern Map

**Mapped:** 2026-07-22
**Files analyzed:** 9 (2 new, 7 modified)
**Analogs found:** 9 / 9 (all have at least a role-match; 6 are exact/same-file analogs)

No `CONTEXT.md` exists for this phase — file list extracted entirely from `21-RESEARCH.md`'s Recommended Project Structure, Architecture Patterns, Codebase Privilege Surface, and Phase Requirements → Test Map sections. Two corrections to RESEARCH.md's own Test Map are called out below (see "Corrections to RESEARCH.md" at the end) — the actual analog files differ slightly from what RESEARCH.md guessed.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `pkg/k8s/version.go` (NEW) | service/utility (cluster probe + pure comparison) | request-response (probe) + transform (parse/compare) | `pkg/k8s/preflight.go` (warn-and-proceed shape) + `pkg/k8s/portforward.go` (pod List shape) | exact (composite of two exact same-package analogs) |
| `pkg/k8s/version_test.go` (NEW) | test | unit / fake-clientset | `pkg/k8s/preflight_test.go` | exact |
| `cmd/cpg/generate.go` (MODIFIED — add `maybeRunVersionPreflight` + call site) | controller (CLI composition root) | request-response | itself — `maybeRunL7Preflight` (same file, lines 31-56) | exact, same-file sibling function |
| `pkg/session/manager.go` (MODIFIED — `resolveSetup`) | service (setup orchestration) | request-response | itself — `resolveSetup` (same file, lines 269-316) | exact, same-file |
| `pkg/session/session.go` (MODIFIED — `StartResult`/`StatusResult` fields) | model (MCP result shape) | request-response | itself — `StartResult`/`StatusResult`/`buildSummary` (same file) | exact, same-file |
| `pkg/session/manager_test.go` (MODIFIED — extend `TestManager_Start`/`TestManager_Status`) | test | unit | itself — `TestManager_Start` (line 158), `newTestManager` (line 132) | exact, same-file |
| `cmd/cpg/mcp_session_test.go` (MODIFIED — extend wire-level assertions) | test (MCP wire-level) | request-response | itself — `decodeStructured` (line 47), `TestMCPSessionToolsListed` (line 67) | exact, same-file |
| `cmd/cpg/replay_test.go` (MODIFIED — one regression assertion) | test (regression) | request-response | itself — existing offline-guarantee tests (e.g. `TestReplayCmd_L7DefaultIsFalse`, line 27) | exact, same-file |
| `README.md` (MODIFIED — new section + line-287 fix) | config/docs (static) | — | itself — "L7 Prerequisites" section structure (lines 248-311) | role-match (structural template, no prior "Compat" section exists) |

**No file requires touching `cmd/cpg/mcp_tools.go` or `cmd/cpg/mcp_audit_test.go`** — see Shared Patterns below for why.

## Pattern Assignments

### `pkg/k8s/version.go` (NEW — service/utility, request-response probe + transform)

**Analogs:** `pkg/k8s/preflight.go` (warn-and-proceed three-way branch) + `pkg/k8s/portforward.go` (pod List shape)

**Imports pattern** (from `pkg/k8s/preflight.go:1-10`):
```go
package k8s

import (
	"context"

	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)
```
`version.go` additionally needs `strings` (image-tag parsing), `fmt` (message formatting), and `apiversion "k8s.io/apimachinery/pkg/util/version"` (new import for this package, but not a new `go.mod` dependency — already a direct dep, see RESEARCH.md Standard Stack).

**Pod-List core pattern to mirror** (`pkg/k8s/portforward.go:104-125`, `findRelayPod`):
```go
// findRelayPod finds a running hubble-relay pod in kube-system.
func findRelayPod(ctx context.Context, clientset kubernetes.Interface, logger *zap.Logger) (*corev1.Pod, error) {
	pods, err := clientset.CoreV1().Pods(relayNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: relayLabelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing hubble-relay pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no hubble-relay pod found in %s (selector: %s)", relayNamespace, relayLabelSelector)
	}
	...
}
```
`version.go`'s pod enumeration is **byte-identical in shape** — same `ciliumNamespace` constant (`"kube-system"`, already declared in `preflight.go:13`, reuse it — do not redeclare), same `.List(ctx, metav1.ListOptions{LabelSelector: ...})` call, only the selector value changes to `"k8s-app=cilium"` and the per-item field read is `.Spec.Containers[i].Image` (not `.Status.Phase`).

**Warn-and-proceed three-way branch to reuse verbatim** (`pkg/k8s/preflight.go:75-95`, `getCiliumConfig`):
```go
func getCiliumConfig(ctx context.Context, client kubernetes.Interface, logger *zap.Logger) ciliumConfigData {
	cm, err := client.CoreV1().ConfigMaps(ciliumNamespace).Get(ctx, ciliumConfigMapName, metav1.GetOptions{})
	switch {
	case err == nil:
		...
	case apierrors.IsForbidden(err):
		logger.Warn(warnConfigMapForbidden, zap.Error(err))
		return ciliumConfigData{Forbidden: true}
	case apierrors.IsNotFound(err):
		logger.Warn(warnConfigMapNotFound)
		return ciliumConfigData{NotFound: true}
	default:
		logger.Warn(warnConfigMapNotFound, zap.Error(err))
		return ciliumConfigData{NotFound: true}
	}
}
```
Same switch shape for the new `list pods` call: `err == nil` → tally versions; `apierrors.IsForbidden(err)` → named-permission warning (`"pods/list in kube-system"`) + `Source: "undetermined"`; default (including `IsNotFound`, though List never 404s the way Get does) → generic warn-and-proceed. **Never return an error that blocks the caller** — same rationale as `preflight.go:48-51`'s doc comment (reduced-RBAC CI service accounts must not be locked out).

**Warning-copy style constant** (`pkg/k8s/preflight.go:20-38`) — literal, actionable, remediation-hint strings, declared as package `const` blocks with a doc comment warning "Do not paraphrase." Follow this exact convention for the new below-floor / RBAC-denied version-preflight warning strings.

**Version parse/compare** — no existing cpg analog (new library usage this phase); use RESEARCH.md's own Code Examples verbatim (`parseImageTag`, `apiversion.ParseGeneric` + `.AtLeast`/`.LessThan`) — already vetted against the vendored `k8s.io/apimachinery@v0.35.4` source in RESEARCH.md.

**Error handling:** no `error` return type on the top-level `DetectCiliumVersion` at all — mirrors `RunL7Preflight`'s signature (`func RunL7Preflight(ctx, client, logger)` — no return value beyond the side-effecting warns). `DetectCiliumVersion` returns a plain `CompatInfo` value (warnings are the only "error surface").

---

### `pkg/k8s/version_test.go` (NEW — test, fake-clientset unit)

**Analog:** `pkg/k8s/preflight_test.go` (227 lines, full read)

**Fixture-builder + fake logger pattern** (`pkg/k8s/preflight_test.go:22-47`):
```go
func configMap(data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cilium-config", Namespace: "kube-system"},
		Data: data,
	}
}

func newObservedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.WarnLevel)
	return zap.New(core), logs
}
```
Mirror with a `ciliumAgentPod(image string) *corev1.Pod` builder (namespace `kube-system`, label `k8s-app: cilium`, `.Spec.Containers[0].Image = image`, `.Status.Phase = corev1.PodRunning`) — reuse `newObservedLogger`/`countWarnings`/`containsMessage` as-is (same package, no need to redefine).

**RBAC-forbidden reactor injection** (`pkg/k8s/preflight_test.go:49-57, 149-158`):
```go
func forbiddenReactor(resource, verb string) clienttesting.ReactionFunc {
	return func(action clienttesting.Action) (bool, runtime.Object, error) {
		gr := schema.GroupResource{Resource: resource}
		return true, nil, apierrors.NewForbidden(gr, action.(clienttesting.GetAction).GetName(), &forbiddenErr{...})
	}
}
...
c := fake.NewSimpleClientset(envoyDS())
c.PrependReactor("get", "configmaps", forbiddenReactor("configmaps", "get"))
```
For a `list` verb (not `get`), the reactor's `action` must be type-asserted as `clienttesting.ListAction` (no `.GetName()` on a list action) — a small, necessary deviation from the copy-pasted helper; note this explicitly in the plan so it isn't silently miscompiled.

**Table-driven test-case shape** (`pkg/k8s/preflight_test.go:78-182`) — `[]struct{name, setup func() kubernetes.Interface, wantWarns int, wantContains []string}` — reuse verbatim for `TestDetectCiliumVersion`; add cases for: all-pods-same-version (0 warnings if ≥ floor), mixed-version pods (asserts `VersionsSeen` map populated, minimum taken), forbidden (1 warning, named permission), no pods found (warn, `Source: "undetermined"`).

**Image-tag-parsing sub-tests** (Pitfall 4 in RESEARCH.md) — no existing cpg analog for this specific table-driven string-parsing shape; a fresh `[]struct{name, image string, wantTag string, wantOK bool}` table is the idiomatic Go approach, needs no fixture/clientset (pure function test) — keep it in the same `version_test.go` file per RESEARCH.md's Wave 0 Gaps.

---

### `cmd/cpg/generate.go` (MODIFIED — add `maybeRunVersionPreflight` + call site)

**Analog:** itself — `maybeRunL7Preflight`, same file, lines 31-56 (sibling function, copy-adapt in place)

**Exact function to mirror** (`cmd/cpg/generate.go:31-56`):
```go
// maybeRunL7Preflight runs pkg/k8s.RunL7Preflight when L7 generation is
// requested and pre-flight is not explicitly disabled. Pre-flight is advisory:
// any failure to construct a client is logged as a warning and the pipeline
// proceeds. Pre-flight is invoked AT MOST ONCE per cpg invocation.
//
// Caller contract: invoke from cpg generate ONLY. cpg replay is offline by
// definition and must never call this function regardless of --l7.
func maybeRunL7Preflight(ctx context.Context, kubeConfig *rest.Config, l7Enabled, noPreflight bool, logger *zap.Logger) {
	if !l7Enabled || noPreflight {
		return
	}
	if kubeConfig == nil {
		var err error
		kubeConfig, err = k8s.LoadKubeConfig()
		if err != nil {
			logger.Warn("--l7 preflight skipped: kubeconfig not available", zap.Error(err))
			return
		}
	}
	client, err := l7ClientFactory(kubeConfig)
	if err != nil {
		logger.Warn("--l7 preflight skipped: failed to construct kubernetes client", zap.Error(err))
		return
	}
	k8s.RunL7Preflight(ctx, client, logger)
}
```
`maybeRunVersionPreflight` (RESEARCH.md's own Code Examples section already drafted this — reuse that draft) has no `l7Enabled`/`noPreflight` gate (version detection is always-on, unconditional, unlike opt-in L7) but reuses the exact `l7ClientFactory` package-level var (already test-substitutable, `cmd/cpg/generate.go:27-29`) and the exact nil-kubeConfig fallback shape.

**Call site to mirror** (`cmd/cpg/generate.go:219-223`):
```go
// VIS-04/05/06: run L7 cluster pre-flight ONCE before the pipeline starts.
// Skipped when --l7 is unset, when --no-l7-preflight wins, or when no
// kubeconfig is reachable. Pre-flight is advisory: warnings only, never
// blocking.
maybeRunL7Preflight(ctx, kubeConfig, f.l7, f.noL7Preflight, logger)
```
Add `compat := maybeRunVersionPreflight(ctx, kubeConfig, logger)` immediately alongside this line (before `hubble.RunPipeline(...)`) — `compat` itself has no further CLI consumer today (no `--l7`-style flag reads `BelowFloorFeatures`); the warning is the entire CLI-side surface.

**Pitfall 6 guard (already enforced elsewhere, must NOT be violated):** `cmd/cpg/replay.go` (full file read, 134 lines) has **no** `maybeRunL7Preflight`-style call anywhere in `runReplay` — confirms the existing "replay is offline, preflight is generate-only" contract is a structural absence, not a flag check. Do not add `maybeRunVersionPreflight` to `replay.go`.

---

### `pkg/session/manager.go` (MODIFIED — `resolveSetup`)

**Analog:** itself — `resolveSetup`, same file, lines 269-316

**Exact function to extend** (`pkg/session/manager.go:276-316`):
```go
func (m *Manager) resolveSetup(setupCtx context.Context, args StartArgs) (server string, cleanup func(), clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy, err error) {
	var kubeConfig *rest.Config
	if args.Server != "" {
		// D-07: an explicit server bypasses kubeconfig + auto port-forward.
		server = args.Server
		cleanup = func() {}
	} else {
		kubeConfig, err = k8s.LoadKubeConfig()
		...
		localAddr, pfCleanup, pfErr := k8s.PortForwardToRelay(setupCtx, kubeConfig, m.logger)
		...
		server = localAddr
		cleanup = pfCleanup
	}

	if args.ClusterDedup {
		if kubeConfig == nil {
			kubeConfig, err = k8s.LoadKubeConfig()
			...
		}
		clusterPolicies, err = k8s.LoadClusterPoliciesForNamespaces(setupCtx, kubeConfig, dedupNamespaces(args))
		...
	}

	return server, cleanup, clusterPolicies, nil
}
```
**Design nuance the planner must resolve (flagged, not silently assumed):** `kubeConfig` is `nil` whenever `args.Server != ""` **and** `!args.ClusterDedup` (the pure D-07 bypass — no kubeconfig ever loaded). This is exactly the scenario RESEARCH.md's diagram labels "MCP-only secondary cross-check... works even when `--server` bypasses kubeconfig entirely" — but there is **no already-open observer gRPC connection** at this point in `resolveSetup` either (the pipeline's own `hubble.Client` dials separately, later, inside `RunPipeline`/`StreamDroppedFlows`). A `GetNodes()` secondary check here means dialing a **new**, short-lived gRPC connection to `server` (the just-resolved relay address) specifically for this purpose — see `pkg/hubble/client.go` pattern below. Recommend the planner extend `resolveSetup`'s return tuple with a `compat k8s.CompatInfo` value, computed as: pod-list detection when `kubeConfig != nil`; else a `GetNodes()` attempt against `server`; else `CompatInfo{Source: "undetermined"}`.

**Per-session-value caching pattern to mirror** (how `TmpDir` is set once, under `m.mu`, before the pipeline goroutine launches — `pkg/session/manager.go:199-212`):
```go
m.mu.Lock()
if m.session != s {
	...
}
s.TmpDir = tmpDir
m.mu.Unlock()
```
Cache the detected `CompatInfo` the same way: add a `compat k8s.CompatInfo` field to `Session` (session.go), set it alongside `s.TmpDir = tmpDir` in this exact block (both are "computed once during setup, read-only for the rest of the session's life" — no atomic/mutex-heavy treatment needed beyond the existing `m.mu` critical section, unlike `s.final`/`s.pipelineErr` which are written from a **different** goroutine later and correctly use `atomic.Pointer`).

**Read-side copy-under-lock pattern to mirror** (`pkg/session/manager.go:339-347`, `Status`):
```go
m.mu.Lock()
s := m.session
...
state := s.State
startedAt := s.StartedAt
stoppedAt := s.StoppedAt
tmpDir := s.TmpDir
sid := s.ID
m.mu.Unlock()
```
Add `compat := s.compat` to this same copy block — `Status()` already returns `StatusResult{...}` after unlocking; add `CiliumVersion: compat.ClusterVersion` etc. to that literal (see `session.go` pattern below).

---

### `pkg/session/session.go` (MODIFIED — `StartResult`/`StatusResult` new fields)

**Analog:** itself — `StartResult`/`StatusResult` struct definitions, lines 154-177, plus the `buildSummary` internal-type-to-JSON-friendly-type projection technique, lines 218-264

**Exact structs to extend** (`pkg/session/session.go:154-177`):
```go
// StartResult is the start_session MCP tool's structuredContent shape.
type StartResult struct {
	SessionID string `json:"session_id"`
	DiscardedSession string `json:"discarded_session,omitempty"`
	Server string `json:"server"`
}

// StatusResult is the get_status MCP tool's structuredContent shape.
type StatusResult struct {
	SessionID         string `json:"session_id"`
	State             string `json:"state"`
	Elapsed           string `json:"elapsed"`
	PolicyFileCount   int    `json:"policy_file_count"`
	EvidenceFileCount int    `json:"evidence_file_count"`
	TmpDir            string `json:"tmp_dir"`
	Error string `json:"error,omitempty" jsonschema:"..."`
}
```
Add to **both** structs (per RESEARCH.md Open Question 2 — cache once, surface on every `get_status` call too, matching how `PolicyFileCount`/`EvidenceFileCount` are cheaply recomputed/carried on every `Status()` call):
```go
CiliumVersion      string         `json:"cilium_version,omitempty"`
CiliumVersionsSeen map[string]int `json:"cilium_versions_seen,omitempty" jsonschema:"..."`
BelowFloorFeatures []string       `json:"below_floor_features,omitempty"`
```
`StopResult` (lines 182-212) is **intentionally excluded** — no requirement or research finding calls for compat data on the stop-summary; adding it would be scope creep.

**Internal-type → JSON-friendly-type projection technique to mirror** (`pkg/session/session.go:254-261`, inside `buildSummary`):
```go
for reason, count := range stats.InfraDropsByReason {
	name, ok := flowpb.DropReason_name[int32(reason)]
	if !ok {
		name = fmt.Sprintf("UNKNOWN(%d)", reason)
	}
	result.InfraDropsByReason[name] = count
}
```
If `k8s.CompatInfo`'s internal shape needs any projection before landing in `StartResult`/`StatusResult` (e.g. `*apiversion.Version` → `string`), follow this exact "iterate internal map/value, defensively project, never panic on an unexpected key" style — though per RESEARCH.md's Pattern 3, `CompatInfo.VersionsSeen` is likely already declared as `map[string]int` (string-keyed) at the source, making a straight field copy sufficient (no projection loop needed) — verify this when `pkg/k8s/version.go`'s `CompatInfo` struct is actually authored.

---

### `pkg/session/manager_test.go` (MODIFIED — extend `TestManager_Start`/`TestManager_Status`)

**Analog:** itself — `newTestManager` (line 132), `TestManager_Start` (line 158)

**Test-manager construction seam to reuse** (`pkg/session/manager_test.go:128-141`):
```go
func newTestManager(t *testing.T, source flowsource.FlowSource) *Manager {
	t.Helper()
	m := NewManager(context.Background(), zaptest.NewLogger(t), &bytes.Buffer{}, "vTest")
	m.runPipeline = func(ctx context.Context, cfg hubble.PipelineConfig) error {
		return hubble.RunPipelineWithSource(ctx, cfg, source)
	}
	m.stopWait = 100 * time.Millisecond
	m.removeWait = 100 * time.Millisecond
	return m
}
```
**Important:** every existing `Start` test uses `StartArgs{Server: "bypass:1"}` (D-07) — e.g. `TestManager_Start` at line 162: `m.Start(context.Background(), StartArgs{Server: "bypass:1"})`. Since `"bypass:1"` is not a real gRPC endpoint, if version detection's fallback path attempts a real `GetNodes()` dial against it, these ~20 existing `Start`-family tests would newly hang/fail on a dial they never previously attempted. **The planner must either:** (a) give `resolveSetupFn`/`resolveSetup`'s version-detection step its own injectable test seam (mirroring `resolveSetupFn`'s existing pattern, `manager.go:58-64,92`), or (b) ensure the fallback path fails fast/non-blocking against an unreachable address (bounded by `setupCtx`, which every `resolveSetup` step already observes). Either way, this is a **required**, not optional, consideration — flag prominently in the plan.

**Existing `resolveSetupFn` test-injectable seam to reuse as the template** (`pkg/session/manager.go:58-64, 90-93`):
```go
resolveSetupFn func(setupCtx context.Context, args StartArgs) (server string, cleanup func(), clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy, err error)
...
m.resolveSetupFn = m.resolveSetup
```
If version detection needs its own seam (option (a) above), add a `detectVersionFn func(ctx, kubeConfig, server) k8s.CompatInfo` field to `Manager`, defaulted in `NewManager` the same way, overridable by tests — same "bind as method value after full construction" comment already present at `manager.go:90-93`.

---

### `cmd/cpg/mcp_session_test.go` (MODIFIED — extend wire-level assertions)

**Analog:** itself — `decodeStructured` (line 47), `TestMCPSessionToolsListed` (lines 67-100)

**JSON-round-trip decode helper to reuse verbatim** (`cmd/cpg/mcp_session_test.go:43-52`):
```go
// decodeStructured re-marshals a CallToolResult.StructuredContent value
// (map[string]any on the client side, per the go-sdk's JSON round trip) into
// out, a pointer to a small local struct whose json tags match the relevant
// subset of session.StartResult/StatusResult fields.
func decodeStructured(t *testing.T, structuredContent any, out any) {
	t.Helper()
	raw, err := json.Marshal(structuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, out))
}
```
Add a small local struct (e.g. `struct{ CiliumVersion string; BelowFloorFeatures []string }`) and decode a `start_session`/`get_status` result into it, over the real in-memory MCP transport — proves the new fields actually reach `structuredContent` on the wire, not just the Go struct level (`manager_test.go` already covers that level).

**No `cmd/cpg/mcp_tools.go` change needed:** `registerSessionTools` (`cmd/cpg/mcp_tools.go:95-168`) wires tool handlers via `mcp.AddTool(server, &mcp.Tool{...}, func(...) (*mcp.CallToolResult, session.StartResult, error) {...})` — the go-sdk infers `outputSchema` from the `session.StartResult`/`StatusResult` type via reflection at registration time. Adding fields to those structs (session.go, above) is automatically picked up; **zero lines change in `mcp_tools.go` itself.**

---

### `cmd/cpg/replay_test.go` (MODIFIED — one regression assertion)

**Analog:** itself — existing offline-guarantee test style, e.g. `TestReplayCmd_L7DefaultIsFalse` (line 27) and `TestReplay_IgnoreDropReasonWarnsOnce` (line 38)

RESEARCH.md's Pitfall 6 / Test Map calls for "one assertion, not a new file" proving `cpg replay` never invokes live version detection. Since `runReplay` (`cmd/cpg/replay.go`, full 134-line read) has no kubeconfig/K8s-client code path **at all** today (confirmed: no `k8s.` package import, no `rest.Config`), the most direct regression assertion is structural, not behavioral: keep `replay.go` free of any `k8s.LoadKubeConfig`/`maybeRunVersionPreflight` call, and add a short doc-comment-anchored test (or a `go vet`-adjacent static check) rather than a runtime behavioral test — mirror the existing pattern of asserting on **absence** (e.g., `TestReplayCmd_L7DefaultIsFalse` proves a flag default, not a runtime side effect). A `strings.Contains` scan of `replay.go`'s own source for the forbidden call is one lightweight option if the planner wants an automated guard; a code-comment cross-reference (like `generate.go:36-37`'s own doc comment already states the constraint) may be sufficient given the file's current zero-K8s-dependency shape.

---

### `README.md` (MODIFIED — new "Supported Cilium versions" section + line-287 fix)

**Analog:** itself — "L7 Prerequisites" section structure (lines 248-311) for tone/format; no prior "Compatibility"/"Supported" section exists anywhere in the file (confirmed via full 686-line read)

**Exact line to fix (COMPAT-03)** — `README.md:286-288`:
```
1. **Recommended for ad-hoc bootstrap — proxy-visibility annotation.**
   The legacy but still widely supported (Cilium ≤ 1.19) workload-level
   annotation that triggers Envoy / DNS proxy redirection without
```
Replace "still widely supported (Cilium ≤ 1.19)" with language stating removal at 1.17 (per Version Pin Table: PR #35019, merged 2024-10-01, confirmed zero occurrences of the annotation string in vendored v1.19.4). The surrounding sentence structure/tone should otherwise be preserved.

**Placement for the new section (COMPAT-01)** — insert after "Install" (ends `README.md:62`) and before "Quick start" (`README.md:64`), per RESEARCH.md Assumption A3. Structural template to copy: a top-level `##` heading, a short framing paragraph, then a markdown table — same shape as the existing "Skip reasons" table (`README.md:412-422`) or "Exit codes" table (`README.md:641-644`):
```markdown
| Reason | What it means |
|--------|---------------|
| `no_l4` | Flow has no L4 layer (no port/protocol info) |
```
Use this exact table style for the per-feature floor table (columns: Feature, Cilium Floor, Notes), sourced directly from RESEARCH.md's own "Version Pin Table" (already transcription-ready per that section's own framing).

**Project structure section** (`README.md:614-626`) already documents `pkg/k8s/` as "Kubeconfig loading, port-forward, cluster policy fetching" — a one-line addition ("version detection") keeps this table accurate; not required by any Phase Requirement but low-cost and consistent with existing maintenance style.

## Shared Patterns

### Warn-and-proceed, RBAC-tolerant advisory check (primary shared pattern)
**Source:** `pkg/k8s/preflight.go:75-119` (full three-way branch, all of `getCiliumConfig` + `checkCiliumEnvoy`)
**Apply to:** `pkg/k8s/version.go`'s pod-list detection and any RBAC-touching branch therein.
```go
switch {
case err == nil:
	// success path
case apierrors.IsForbidden(err):
	logger.Warn(warnXForbidden, zap.Error(err))
	return zeroValue, true // or equivalent "forbidden" marker
case apierrors.IsNotFound(err):
	logger.Warn(warnXNotFound)
	return zeroValue, false
default:
	logger.Warn(warnXNotFound, zap.Error(err))
	return zeroValue, false
}
```
**Never return an error that blocks the pipeline** — the load-bearing constraint from `preflight.go:48-51`'s doc comment, applies identically to version detection.

### Passive single-warning-per-run gate (alternative shape, worth knowing)
**Source:** `pkg/hubble/pipeline.go:352-375` (VIS-01 / AUD-01)
```go
if cfg.L7Enabled && stats.FlowsSeen > 0 && agg.L7HTTPCount()+agg.L7DNSCount() == 0 {
	cfg.Logger.Warn("--l7 set but no L7 records observed in window", ...)
}
```
This is a **bare post-computation check, not an RBAC-branch** — relevant if the below-floor warning ends up living in `generate.go`'s call site (a single computed-condition check) rather than inside `pkg/k8s/version.go` itself. Either placement is defensible; RESEARCH.md's own Code Examples section places it in `maybeRunVersionPreflight` (`cmd/cpg/generate.go`), which follows this exact shape.

### Pod-List call shape (RBAC-cheap, same verb/namespace as an existing call)
**Source:** `pkg/k8s/portforward.go:104-125` (`findRelayPod`)
**Apply to:** the new cilium-agent pod enumeration — same `clientset.CoreV1().Pods(ciliumNamespace).List(ctx, metav1.ListOptions{LabelSelector: ...})` call, only the selector string differs (`"k8s-app=cilium"` vs `"k8s-app=hubble-relay"`). Per RESEARCH.md's Codebase Privilege Surface table, this introduces **zero new RBAC class** — an operator's existing `pods/list` grant in `kube-system` already covers any label selector against that resource type.

### gRPC dial + ObserverClient construction (if the GetNodes secondary check is implemented)
**Source:** `pkg/hubble/client.go:53-77` (dial + `waitForConnReady` + `observerpb.NewObserverClient`)
```go
conn, err := grpc.NewClient(c.server, transportCreds)
...
if c.timeout > 0 {
	if err := waitForConnReady(ctx, conn, c.timeout); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
}
client := observerpb.NewObserverClient(conn)
```
A `GetNodes()`-only secondary check needs the same dial + bounded-ready-wait shape but a much shorter-lived connection (one RPC, then close) — do not reuse `pkg/hubble.Client` itself (it is streaming-shaped, not a fit for a single unary call); a small, separate helper in `pkg/k8s/version.go` mirroring just the dial+client-construction lines is the right scope.

### MCP result-struct additive extension + automatic schema inference
**Source:** `pkg/session/session.go:154-177` (structs) + `cmd/cpg/mcp_tools.go:107,152` (generic `mcp.AddTool` registration)
**Apply to:** `StartResult`/`StatusResult` — add fields with `json:"...,omitempty"` (+ `jsonschema:"..."` doc tag matching the existing `Error` field's style at `session.go:176`). No `mcp_tools.go` change needed — schema inference is automatic via reflection over the struct passed as `mcp.AddTool`'s type parameter.

### SEC-01 structural audit — confirmed no-op for this phase
**Source:** `cmd/cpg/mcp_audit_test.go:66-77` (`k8sWriteVerbs`), `91-118` (`fsWriteAllowlist`)
`Get`/`List` are not members of `k8sWriteVerbs` (only `Create/Update/Patch/Delete/Apply/DeleteCollection/UpdateStatus/ApplyStatus`), and this phase writes no new file, so `TestMCPAuditReadonlyReachability` (`cmd/cpg/mcp_audit_test.go:222-336`) requires **zero edits** — it will simply keep passing as long as no code path in this phase calls a write verb or an unallowlisted `os.*` write function. Run it once after implementation as a confirmation, not as a file to modify.

### Fake clientset + zap observed-logger test harness
**Source:** `pkg/k8s/preflight_test.go` (whole file, 227 lines) — `fake.NewSimpleClientset(...)`, `c.PrependReactor(verb, resource, forbiddenReactor(...))`, `observer.New(zapcore.WarnLevel)`.
**Apply to:** `pkg/k8s/version_test.go` in full — this is the single most directly reusable test file in the repo for this phase.

## No Analog Found

| File/Concern | Role | Data Flow | Reason |
|---|---|---|---|
| `k8s.io/apimachinery/pkg/util/version` usage (`ParseGeneric`/`AtLeast`/`LessThan`) | transform | pure computation | First use of this subpackage anywhere in cpg — no prior comparator code to pattern-match; use RESEARCH.md's own Code Examples section directly (already verified against the vendored source at the pinned version) |
| Image-reference tag/digest parsing (`parseImageTag`) | transform | pure computation | No existing Docker/OCI image-reference parser anywhere in cpg; RESEARCH.md's Code Examples section supplies a ready-to-use implementation, grammar-verified against live-cluster samples |
| `GetNodes()` MCP-only secondary cross-check dial | service | request-response (unary RPC) | No existing "single unary RPC, short-lived connection" helper in `pkg/hubble` (the only existing gRPC client code is streaming-shaped, `StreamDroppedFlows`); closest partial analog is `pkg/hubble/client.go`'s dial+client-construction lines only (cited above under Shared Patterns), not a complete pattern — this is new-authorship glue code, small in scope |
| README "Supported Cilium versions" section content (the table itself) | docs | static | Confirmed via full 686-line read: no version-compatibility table, "Supported," or "Compatibility" heading exists anywhere in `README.md` today — this is new authorship, not an edit (RESEARCH.md Pitfall 7); structural formatting borrowed from the "Skip reasons"/"Exit codes" tables (cited above), but the content is wholly new |

## Corrections to RESEARCH.md

RESEARCH.md's own Test Map (§ Phase Requirements -> Test Map) named `pkg/session/session_test.go` as the file to extend for the `StartResult`/`TestStartResult` assertion. Direct inspection shows this is **not accurate**:

- `pkg/session/session_test.go` (110 lines, full read) contains exactly two test functions — `TestState_String` and `TestSession_BuildSummary` — neither constructs nor asserts on a `StartResult` at all.
- The actual `StartResult`-touching test is `pkg/session/manager_test.go`'s `TestManager_Start` (line 158) and its many siblings (`TestManager_Start_RejectsConcurrent`, etc.) — this is the file the planner should point the "extend StartResult test coverage" task at.
- Additionally, a **second**, wire-level analog exists that RESEARCH.md's Test Map did not mention at all: `cmd/cpg/mcp_session_test.go`'s `decodeStructured` + `TestMCPSessionToolsListed` — this is the only place in the repo that proves a `session.StartResult`/`StatusResult` field actually reaches MCP `structuredContent` over the wire (not just the Go struct). Both files are listed above as separate, real analogs.

## Metadata

**Analog search scope:** `pkg/k8s/` (all 4 non-test + 2 test files), `pkg/session/` (all 4 non-test + 3 test files), `pkg/hubble/client.go` + `pipeline.go`, `cmd/cpg/{generate,replay,mcp,mcp_tools,mcp_audit_test,mcp_session_test,replay_test}.go`, `README.md` (full), `docs/KNOWN_LIMITATIONS.md` (partial), `go.mod`, `.claude/skills/desloppify/SKILL.md` (project skill, not directly applicable to pattern extraction)
**Files scanned:** 20
**Pattern extraction date:** 2026-07-22
