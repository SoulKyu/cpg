# Phase 17: Session Lifecycle - Pattern Map

**Mapped:** 2026-07-20
**Files analyzed:** 9 (4 new in `pkg/session`, 1 new in `cmd/cpg`, 1 new/extended test file, 3 modified existing files)
**Analogs found:** 7 / 9 with a usable analog (2 exact-self, 3 exact/role-match, 2 composite/partial) — 2 files have genuinely no analog anywhere in the codebase (flagged below, fall back to RESEARCH.md's verified Code Examples)

Module path: `github.com/SoulKyu/cpg`. Grep sweep confirmed zero existing production `os.MkdirTemp` calls and only two `sync.{Mutex,Once}` sites in the entire `pkg/` tree (`pkg/hubble/health_writer.go:38`, `pkg/hubble/unhandled.go:18`) — `pkg/session` is genuinely new orchestration territory, not a copy-paste of an existing subsystem.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|-----------------|----------------|
| `pkg/session/pipeline_config.go` (NEW) | utility (config-builder) | transform | `cmd/cpg/generate.go:145-257` (`runGenerate`) | exact |
| `pkg/session/manager.go` (NEW) | service (orchestrator) | event-driven, CRUD-shaped API (Start/Status/Stop) | composite: `pkg/hubble/pipeline.go` (ctx/errgroup lifecycle) + `pkg/hubble/health_writer.go` (sync.Once idempotency) + `cmd/cpg/generate.go` (ctx-cancel shutdown) + `pkg/k8s/portforward.go` (non-blocking cleanup) | role-match (composite — no single existing "Manager" type) |
| `pkg/session/session.go` (NEW) | model | CRUD (state machine) | `pkg/hubble/pipeline.go:43-128` (`PipelineConfig`/`SessionStats` struct style) | role-match |
| `pkg/session/manager_test.go` (NEW) | test | event-driven | `pkg/hubble/pipeline_test.go` (`mockFlowSource`, `TestRunPipeline_GracefulShutdown`) | exact |
| `cmd/cpg/mcp_tools.go` (NEW) | controller | request-response | partial: `cmd/cpg/mcp.go` (composition root) + `cmd/cpg/commonflags.go` (validators) + `cmd/cpg/generate.go` (validate-then-build glue) | **no analog** for the `AddTool`/`ToolHandlerFor` shape itself — see No Analog Found |
| `cmd/cpg/mcp.go` (MODIFIED) | config / composition-root | request-response + event-driven shutdown fan-out | itself (`runMCPServer`, lines 77-87) | exact (self, extend in place) |
| `cmd/cpg/mcp_harness_test.go` (EXTENDED) or new `cmd/cpg/mcp_session_test.go` | test | request-response | `cmd/cpg/mcp_harness_test.go` (`startInMemoryMCPSession`) + `cmd/cpg/mcp_test.go` | exact |
| `pkg/hubble/pipeline.go` (MODIFIED — additive `OnFinal` hook) | service | streaming | itself (exact insertion point verified: struct field near line 93, call site after line 322) | exact (self, additive) |
| `pkg/hubble/pipeline_test.go` (MODIFIED) | test | streaming | itself (`TestSessionStats_Log`, `TestRunPipeline_EndToEnd`) | exact (self, additive) |

## Pattern Assignments

### `pkg/session/pipeline_config.go` (utility/config-builder, transform)

**Analog:** `cmd/cpg/generate.go:145-257` (`runGenerate`) — this is literally the recipe RESEARCH.md Pattern 5 says to port to a session tmpdir.

**Imports pattern** (`cmd/cpg/generate.go:1-22`):
```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/hubble"
	"github.com/SoulKyu/cpg/pkg/k8s"
)
```
`pkg/session` is a new `package session` (not `main`), so it cannot import `cmd/cpg`'s cobra-flag machinery — only the four project packages (`evidence`, `hubble`, `k8s`) plus `github.com/google/uuid`/`go.uber.org/zap` carry over.

**Server resolution / port-forward pattern** (`cmd/cpg/generate.go:165-182`):
```go
server := f.server
var kubeConfig *rest.Config
if server == "" {
	var err error
	kubeConfig, err = k8s.LoadKubeConfig()
	if err != nil {
		return fmt.Errorf("--server not provided and kubeconfig not available: %w", err)
	}

	localAddr, cleanup, err := k8s.PortForwardToRelay(ctx, kubeConfig, logger)
	if err != nil {
		return fmt.Errorf("auto port-forward to hubble-relay failed: %w", err)
	}
	defer cleanup()

	server = localAddr
	logger.Info("auto port-forward established", zap.String("local_addr", localAddr))
}
```
D-07's `server` arg bypass is this exact `if server == ""` branch, unmodified — `k8s.LoadKubeConfig`/`k8s.PortForwardToRelay` are called verbatim, only the caller (`Manager.Start`'s setup phase) changes.

**Cluster-dedup pattern — independent of `server`** (`cmd/cpg/generate.go:197-212`):
```go
var clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy
if f.clusterDedup {
	if kubeConfig == nil {
		var err error
		kubeConfig, err = k8s.LoadKubeConfig()
		if err != nil {
			return fmt.Errorf("--cluster-dedup requires kubeconfig: %w", err)
		}
	}
	var err error
	clusterPolicies, err = k8s.LoadClusterPoliciesForNamespaces(ctx, kubeConfig, f.clusterDedupNamespaces())
	if err != nil {
		return fmt.Errorf("loading cluster policies for dedup: %w", err)
	}
	logger.Info("loaded cluster policies for dedup", zap.Int("count", len(clusterPolicies)))
}
```
Note the independent `kubeConfig == nil` re-load — `cluster_dedup` needs a kubeconfig even when D-07's `server` bypass skipped the port-forward-triggered load. `k8s.LoadClusterPoliciesForNamespaces` signature (`pkg/k8s/cluster_dedup.go:40`): `func LoadClusterPoliciesForNamespaces(ctx context.Context, config *rest.Config, namespaces []string) (map[string]*ciliumv2.CiliumNetworkPolicy, error)`.

**Core `PipelineConfig` construction** (`cmd/cpg/generate.go:225-256`):
```go
return hubble.RunPipeline(ctx, hubble.PipelineConfig{
	Server:          server,
	TLSEnabled:      f.tlsEnabled,
	Timeout:         f.timeout,
	Namespaces:      f.namespaces,
	AllNamespaces:   f.allNamespaces,
	OutputDir:       f.outputDir,
	FlushInterval:   f.flushInterval,
	Logger:          logger,
	ClusterPolicies: clusterPolicies,

	DryRun:      f.dryRun,
	DryRunDiff:  !f.dryRunNoDiff,
	DryRunColor: isTerminal(os.Stdout),

	EvidenceEnabled: !f.noEvidence,
	EvidenceDir:     resolveEvidenceDir(f.evidenceDir),
	OutputHash:      evidence.HashOutputDir(absOutDir),
	EvidenceCaps: evidence.MergeCaps{
		MaxSamples:  f.evidenceSamples,
		MaxSessions: f.evidenceSessions,
	},
	SessionID:     fmt.Sprintf("%s-%s", time.Now().UTC().Format(time.RFC3339), uuid.New().String()[:4]),
	SessionSource: evidence.SourceInfo{Type: "live", Server: server},
	CPGVersion:    version,

	L7Enabled: f.l7,

	IgnoreProtocols:   ignoreProtocols,
	IgnoreDropReasons: ignoreDropReasons,
	FailOnInfraDrops:  f.failOnInfraDrops,
})
```
For the session build: `OutputDir`/`EvidenceDir` move under the session tmpdir (`filepath.Join(tmpDir, "policies")` / `filepath.Join(tmpDir, "evidence")`), `SessionID` formula is reused **verbatim** (D-10 — internal evidence ID format untouched), `DryRun*`/`FailOnInfraDrops` are never set (D-05 excludes them from MCP mode), and `Stdout` must be added — see mcpModeStdout wiring below (absent from this CLI recipe because the CLI defaults `Stdout` to nil → `os.Stdout`, which is correct for a terminal but wrong for MCP mode).

**`HashOutputDir` signature** (`pkg/evidence/paths.go:17-25`):
```go
func HashOutputDir(outputDir string) string {
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		abs = outputDir
	}
	abs = filepath.Clean(abs)
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:12]
}
```
Deterministic 12-hex-char hash of the (already-unique `MkdirTemp`) tmpdir path — no collision risk across sessions.

**`mcpModeStdout()` wiring — the Phase 16 handoff this phase completes** (`cmd/cpg/mcp.go:119-121`):
```go
func mcpModeStdout() io.Writer {
	return os.Stderr
}
```
`cfg.Stdout = mcpModeStdout()` MUST be set in every `PipelineConfig` this file builds — see `mcp.go`'s own doc comment at lines 111-118 pinning this exact requirement, and `cmd/cpg/mcp_test.go:35-46`'s `TestMCPModeStdoutNeverDefaultsToRealStdout` contract test that already exists to catch a regression.

---

### `pkg/session/manager.go` (service, event-driven with CRUD-shaped API)

**No single analog exists** — this is a composite of four distinct existing patterns; RESEARCH.md's Code Examples 2/3/4 (already fully derived from these same four sources) are the primary reference. Read this section together with RESEARCH.md Pattern 1-3.

**1. ctx-cancel shutdown pattern** (`cmd/cpg/generate.go:162-163`):
```go
ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
defer cancel()
```
Already shipped and exercised on `cpg generate` (Ctrl+C). `stop_session`'s `s.cancel()` call reuses this exact mechanism — `context.CancelFunc` is documented idempotent, safe to call twice (D-03's idempotent stop).

**2. `sync.Once` idempotency guard** (`pkg/hubble/health_writer.go:32-40, 193-210`):
```go
type healthWriter struct {
	evidenceDir    string
	outputHash     string
	logger         *zap.Logger
	drops          map[flowpb.DropReason]*healthDropEntry
	startedAt      time.Time
	snapshotOnce   sync.Once
	cachedSnapshot []HealthDropSnapshot
}
// ...
func (hw *healthWriter) Snapshot() []HealthDropSnapshot {
	if hw == nil {
		return nil
	}
	hw.snapshotOnce.Do(func() {
		cache := make([]HealthDropSnapshot, 0, len(hw.drops))
		for _, e := range hw.drops {
			cache = append(cache, HealthDropSnapshot{ /* ... */ })
		}
		hw.cachedSnapshot = cache
	})
	// deep-copy on every call so callers cannot mutate cached state
	out := make([]HealthDropSnapshot, len(hw.cachedSnapshot))
	for i, e := range hw.cachedSnapshot {
		out[i] = HealthDropSnapshot{ /* fresh copy */ }
	}
	return out
}
```
This is the exact `sync.Once` idempotency shape `Session.stopOnce` needs (Pitfall F: concurrent `stop_session` calls must not double-teardown, and every caller must observe the same final result) — `hw.Snapshot()`'s "first call wins, every call gets an independent copy" discipline maps directly onto `Manager.Stop`'s "first caller does the real teardown, every caller gets the same summary."

**3. Non-blocking cleanup + bounded `select` wait** (`pkg/k8s/portforward.go:71-79, 91-94`):
```go
select {
case <-readyCh:
	// Port forward is ready
case err := <-errCh:
	return "", nil, fmt.Errorf("port forwarding failed: %w", err)
case <-ctx.Done():
	close(stopCh)
	return "", nil, ctx.Err()
}
// ...
cleanup := func() {
	close(stopCh)
}
```
`close(stopCh)` never blocks — the underlying SPDY goroutines finish asynchronously. This is the reference shape for `Manager.Shutdown()`'s bounded `select { case <-s.done: ...; case <-time.After(deadline): ... }` step, and for why cancel/close steps in the fan-out need no deadline of their own (Pattern 3).

**4. Goroutine + errgroup lifecycle, single source of `PipelineConfig`/error return** (`pkg/hubble/pipeline.go:150-166`):
```go
func RunPipeline(ctx context.Context, cfg PipelineConfig) error {
	client := NewClient(cfg.Server, cfg.TLSEnabled, cfg.Timeout, cfg.Logger)
	return RunPipelineWithSource(ctx, cfg, client)
}

func RunPipelineWithSource(ctx context.Context, cfg PipelineConfig, source flowsource.FlowSource) error {
	flows, lostEvents, err := source.StreamDroppedFlows(ctx, cfg.Namespaces, cfg.AllNamespaces)
	// ...
}
```
`Manager.Start`'s background goroutine calls exactly this function (injectable as `m.runPipeline func(ctx context.Context, cfg hubble.PipelineConfig) error`, defaulting to `hubble.RunPipeline`, swappable to `RunPipelineWithSource` + a fake source in tests) — `RunPipeline` itself is **completely unmodified** by this phase except the additive `OnFinal` field.

**Error handling pattern — plain wrapped errors, no custom error type needed:**
Every analog above uses `fmt.Errorf("...: %w", err)` — no `AppError`-style centralized error type exists anywhere in this codebase. SESS-02/SESS-06's error paths follow the same convention: `return nil, StartResult{}, fmt.Errorf("session %s already running (started %s ago); call stop_session first", active.ID, time.Since(active.StartedAt).Round(time.Second))`. The go-sdk auto-converts a returned `error` into `CallToolResult{IsError: true, ...}` (verified against pinned `go-sdk` v1.6.1 in RESEARCH.md Pattern 0) — never hand-construct `CallToolResult{IsError: true}`.

---

### `pkg/session/session.go` (model, CRUD/state-machine)

**Analog:** `pkg/hubble/pipeline.go:43-94, 97-128` — `PipelineConfig`/`SessionStats` struct style: plain exported struct, doc comment on the struct and on every non-obvious field, no getters/setters, grouped by concern with blank-line separators and inline `// SECTION:` style comments.

```go
// PipelineConfig holds all configuration for the streaming pipeline.
type PipelineConfig struct {
	Server        string
	TLSEnabled    bool
	Timeout       time.Duration
	// ...
	// Stdout is the writer for human-readable output (session summary block).
	// Nil defaults to os.Stdout. Use bytes.Buffer in tests.
	Stdout io.Writer
}

// SessionStats tracks pipeline metrics for the session summary.
type SessionStats struct {
	StartTime       time.Time
	FlowsSeen       uint64
	PoliciesWritten uint64
	// PoliciesFailed counts policies that could not be persisted (e.g. disk
	// full, permission denied). ...
	PoliciesFailed uint64
	// ...
}
```
`session.State`, `session.Session`, `StartArgs`/`StatusResult`/`StopResult` in the new file should follow this exact convention — plain structs, doc-commented fields, no encapsulation ceremony. Cross-goroutine field access (the `OnFinal`-populated stats, read by a different goroutine than the one that wrote them) needs `atomic.Pointer[hubble.SessionStats]`, not a bare field — see RESEARCH.md Pattern 4's rationale (project-wide `-race` convention, `Makefile:9`, would catch a bare-field violation).

---

### `pkg/session/manager_test.go` (test, event-driven)

**Analog:** `pkg/hubble/pipeline_test.go`

**`FlowSource` fake-injection pattern** (`pkg/hubble/pipeline_test.go:25-46`):
```go
// mockFlowSource implements FlowSource for testing.
type mockFlowSource struct {
	flows      []*flowpb.Flow
	lostEvents []*flowpb.LostEvent
}

func (m *mockFlowSource) StreamDroppedFlows(_ context.Context, _ []string, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	flowCh := make(chan *flowpb.Flow, len(m.flows))
	lostCh := make(chan *flowpb.LostEvent, len(m.lostEvents))
	for _, f := range m.flows {
		flowCh <- f
	}
	close(flowCh)
	for _, le := range m.lostEvents {
		lostCh <- le
	}
	close(lostCh)
	return flowCh, lostCh, nil
}
```
`FlowSource` interface being implemented (`pkg/flowsource/source.go:14-16`):
```go
type FlowSource interface {
	StreamDroppedFlows(ctx context.Context, namespaces []string, allNS bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error)
}
```
`pkg/session/manager_test.go` injects `m.runPipeline = func(ctx context.Context, cfg hubble.PipelineConfig) error { return hubble.RunPipelineWithSource(ctx, cfg, &mockFlowSource{...}) }` — no real cluster, no MCP SDK, exactly matching RESEARCH.md's stated test strategy.

**Ctx-cancel + deterministic wait pattern** (`pkg/hubble/pipeline_test.go:106-134`):
```go
ctx, cancel := context.WithCancel(context.Background())
// ... send a flow ...
done := make(chan error, 1)
go func() {
	done <- RunPipelineWithSource(ctx, cfg, source)
}()

require.Eventually(t, func() bool {
	_, err := os.Stat(serverPolicy)
	return err == nil
}, 5*time.Second, 5*time.Millisecond)
cancel()

select {
case err := <-done:
	assert.NoError(t, err, "graceful shutdown should not return error")
case <-time.After(5 * time.Second):
	t.Fatal("timed out waiting for graceful shutdown")
}
```
This is the reference shape for `TestManager_Stop`/`TestManager_ConcurrentStop`: launch in a goroutine, `require.Eventually` to reach a deterministic state, then assert the bounded `select`/`time.After` teardown completes.

**Observed-logger assertion pattern** (`pkg/hubble/pipeline_test.go:139-160`, `cmd/cpg/testhelpers_test.go:21-31`):
```go
core, logs := observer.New(zapcore.InfoLevel)
logger := zap.New(core)
stats := &SessionStats{ /* ... */ }
stats.Log(logger)
require.GreaterOrEqual(t, logs.Len(), 1, "should log session summary")
entry := logs.All()[0]
assert.Equal(t, "session summary", entry.Message)
```
Use for asserting `Manager`'s warning logs (e.g. "session did not exit within deadline") fire correctly under `-race`.

---

### `cmd/cpg/mcp_tools.go` (controller, request-response) — NEW, partial analog only

**No existing MCP tool registration exists anywhere in this codebase** — Phase 16's `cmd/cpg/mcp.go:82-84` explicitly registers zero tools ("Zero tools registered this phase... Phase 17 adds session tools"). The `mcp.AddTool[In, Out]`/`ToolHandlerFor` shape itself has no codebase analog; use RESEARCH.md's Code Example 1 (independently verified against the pinned `go-sdk` v1.6.1 source this research session) as the primary reference. What DOES have analogs:

**Composition-root conventions to match** (`cmd/cpg/mcp.go:77-87`):
```go
func runMCPServer(ctx context.Context, transport mcp.Transport) error {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "cpg", Version: version},
		&mcp.ServerOptions{Logger: bridgedSlogLogger()},
	)
	// Zero tools registered this phase: this is the composition root
	// establishing the readonly discipline SEC-01 verifies structurally in
	// Phase 19. Phase 17 adds session tools, Phase 18 adds query tools.

	return server.Run(ctx, transport)
}
```
`mcp_tools.go` exports `registerSessionTools(server *mcp.Server, mgr *session.Manager)`, called from `runMCPServer` right where that comment currently sits.

**Validation reuse — verbatim, D-06** (`cmd/cpg/commonflags.go:20-37`):
```go
func validateIgnoreProtocols(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	allow := make(map[string]struct{}, len(hubble.ValidIgnoreProtocols()))
	for _, p := range hubble.ValidIgnoreProtocols() {
		allow[p] = struct{}{}
	}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.ToLower(raw)
		if _, ok := allow[v]; !ok {
			return nil, fmt.Errorf("unknown protocol %q: valid values are %s", raw, strings.Join(hubble.ValidIgnoreProtocols(), ", "))
		}
		out = append(out, v)
	}
	return out, nil
}
```
And `validateIgnoreDropReasons(in []string, logger *zap.Logger) ([]string, error)` (`cmd/cpg/commonflags.go:199-246`, uppercase normalization + Levenshtein-suggestion error path). Both are **unexported, `package main`** — per Pitfall J, `mcp_tools.go` (also `package main`) can call them directly; `pkg/session` cannot and must never reimplement them.

**Validate-then-build glue shape** (`cmd/cpg/generate.go:145-161`):
```go
func runGenerate(cmd *cobra.Command, _ []string) error {
	f := parseGenerateFlags(cmd)
	if err := f.validate(); err != nil {
		return err
	}
	ignoreProtocols, err := validateIgnoreProtocols(f.ignoreProtocols)
	if err != nil {
		return err
	}
	ignoreDropReasons, err := validateIgnoreDropReasons(f.ignoreDropReasons, nil)
	if err != nil {
		return err
	}
	// ...
}
```
`start_session`'s handler mirrors this exact "validate all args, bail early on first error, then call into the domain layer" shape, including the mutual-exclusivity check (`generateFlags.validate()`, `cmd/cpg/generate.go:122-127`: `if len(f.namespaces) > 0 && f.allNamespaces { return fmt.Errorf("--namespace and --all-namespaces are mutually exclusive") }` — the `namespace`/`all_namespaces` MCP args need the identical inline check, since this two-line validation is not a standalone reusable function).

---

### `cmd/cpg/mcp.go` (MODIFIED — config/composition-root)

**Self-analog** — extend `runMCPServer` in place, signature **unchanged** (`(ctx context.Context, transport mcp.Transport) error`) so `mcp_harness_test.go`'s `startInMemoryMCPSession` helper keeps working unmodified:

Current full function (`cmd/cpg/mcp.go:77-87`):
```go
func runMCPServer(ctx context.Context, transport mcp.Transport) error {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "cpg", Version: version},
		&mcp.ServerOptions{Logger: bridgedSlogLogger()},
	)
	return server.Run(ctx, transport)
}
```
Extend to: construct `mgr := session.NewManager(ctx, logger)` before `server.Run`, call `registerSessionTools(server, mgr)`, and call `mgr.Shutdown()` synchronously **after** `server.Run` returns, before `runMCPServer` itself returns (SESS-05, Pattern 3) — `ctx` passed to `NewManager` must be this function's own `ctx` parameter (the server's root/signal ctx), never a per-tool-call ctx (Pitfall C).

---

### `cmd/cpg/mcp_harness_test.go` (EXTENDED) / new `cmd/cpg/mcp_session_test.go` (test, request-response)

**Analog:** `cmd/cpg/mcp_harness_test.go` — reuse `startInMemoryMCPSession` unchanged.

**Session helper** (`cmd/cpg/mcp_harness_test.go:28-35`):
```go
func startInMemoryMCPSession(ctx context.Context) (client *mcp.InMemoryTransport, drain func()) {
	serverT, clientT := mcp.NewInMemoryTransports()

	errCh := make(chan error, 1)
	go func() { errCh <- runMCPServer(ctx, serverT) }()

	return clientT, func() { <-errCh }
}
```
Golden-sequence test (`initialize → start_session → get_status → stop_session`) and the ungraceful-disconnect test (SESS-05) both call this helper once per scenario — each `InMemoryTransport` half may be `Connect`-ed at most once (doc comment, lines 21-24), matching `TestMCPStdoutPurity`'s "Session A" / "Session B" two-independent-pairs pattern (`cmd/cpg/mcp_harness_test.go:52-111`).

**Stdout-purity contract test to extend** (`cmd/cpg/mcp_test.go:35-46`):
```go
func TestMCPModeStdoutNeverDefaultsToRealStdout(t *testing.T) {
	got := mcpModeStdout()
	assert.NotNil(t, got)
	assert.Same(t, os.Stderr, got, "D-02: MCP-mode human-output seams resolve to stderr")
	// ...
}
```
Extend this file's stdout-purity harness with a live `start_session`/`stop_session` scenario over the in-memory transport to prove `PipelineConfig.Stdout` actually resolves through `mcpModeStdout()` end-to-end (Pitfall I) — this is the first phase where a real `PipelineConfig` gets built in MCP mode, so the existing unit-level contract test alone cannot catch a wiring regression.

**Logger test helpers** (`cmd/cpg/testhelpers_test.go:11-31`):
```go
func initLoggerForTesting(t *testing.T) {
	t.Helper()
	prev := logger
	logger = zap.NewNop()
	t.Cleanup(func() { logger = prev })
}

func initObservedLoggerForTesting(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	prev := logger
	logger = zap.New(core)
	t.Cleanup(func() { logger = prev })
	return logs
}
```
Reuse both verbatim — the package-level `logger`/`version` vars (`cmd/cpg/main.go:16`: `var version = "dev"`) are shared globals every `cmd/cpg` file already depends on.

---

### `pkg/hubble/pipeline.go` (MODIFIED — additive `OnFinal` hook, service/streaming)

**Self-analog** — exact, independently-verified insertion points (read directly this session, not merely carried from RESEARCH.md):

**Field insertion point** — `Stdout` field, the existing seam it sits next to (`pkg/hubble/pipeline.go:91-93`):
```go
	// Stdout is the writer for human-readable output (session summary block).
	// Nil defaults to os.Stdout. Use bytes.Buffer in tests.
	Stdout io.Writer
}
```
Add `OnFinal func(SessionStats)` immediately after, nil-safe, doc-commented per D-08.

**Call-site insertion point** — immediately after the stats-population block (`pkg/hubble/pipeline.go:316-323`):
```go
	stats.FlowsSeen = agg.FlowsSeen()
	stats.LostEvents = lostTotal.Load()
	stats.L7HTTPCount = agg.L7HTTPCount()
	stats.L7DNSCount = agg.L7DNSCount()
	stats.IgnoredByProtocol = agg.IgnoredByProtocol()
	stats.InfraDropTotal = agg.InfraDropTotal()
	stats.InfraDropsByReason = agg.InfraDrops()

	// VIS-01: passive empty-L7-records detection. ...
```
Insert `if cfg.OnFinal != nil { cfg.OnFinal(*stats) }` between the `stats.InfraDropsByReason = ...` line and the `// VIS-01` comment — this is the exact point where every `SessionStats` field is fully populated but before `ew.finalize`/`hw.finalize`/the stdout summary print run (all of which are unaffected by this addition).

**Nil-safety convention already established in this same file** — mirror `hw`'s nil-safe pattern (`pkg/hubble/pipeline.go:344`: `if err := hw.finalize(stats); err != nil { ... }` where `hw` may be nil and `finalize` itself checks `if hw == nil { return nil }`) — `cfg.OnFinal` follows the same "nil = no-op, caller doesn't need to branch" discipline, just checked at the call site since it's a func field, not a method receiver.

---

### `pkg/hubble/pipeline_test.go` (MODIFIED, test)

**Self-analog** — extend using the file's own two established patterns:

**End-to-end pattern to model the "fires once" assertion on** (`pkg/hubble/pipeline_test.go:48-88`, `TestRunPipeline_EndToEnd`) — build a `PipelineConfig` with `OnFinal: func(s SessionStats) { called++; captured = s }`, run via `RunPipelineWithSource` + `mockFlowSource`, assert `called == 1` and `captured.FlowsSeen` matches the expected count.

**Nil-safety assertion** — a second test with `OnFinal` left nil (zero value), asserting `RunPipelineWithSource` still completes without panicking — mirrors the existing nil-safety precedent for `hw`/`ew` (both may be nil, e.g. `!cfg.EvidenceEnabled`) already exercised implicitly by every existing test that doesn't set `EvidenceEnabled: true`.

---

## Shared Patterns

### Ctx-cancel shutdown (SIGTERM / stop)
**Source:** `cmd/cpg/generate.go:162-163`
```go
ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
defer cancel()
```
**Apply to:** `cmd/cpg/mcp.go`'s `newMCPCmd` (already has this, Phase 16, lines 44-45 — unchanged this phase); `pkg/session.Manager`'s `sessionCtx := context.WithCancel(m.rootCtx)` forks from this same root ctx (Pattern 2 — never from a per-call handler ctx).

### `PipelineConfig` construction recipe
**Source:** `cmd/cpg/generate.go:225-256`
**Apply to:** `pkg/session/pipeline_config.go` — see full excerpt above. Only diffs: `OutputDir`/`EvidenceDir` under the session tmpdir, `Stdout: mcpModeStdout()` added, `DryRun*`/`FailOnInfraDrops` never set, `OnFinal` wired to `s.final.Store(&stats)`.

### Verbatim validator reuse (D-06)
**Source:** `cmd/cpg/commonflags.go:20-37` (`validateIgnoreProtocols`), `:199-246` (`validateIgnoreDropReasons`)
**Apply to:** `cmd/cpg/mcp_tools.go`'s `start_session` handler, called before `Manager.Start` — never reimplemented inside `pkg/session` (Pitfall J: these are unexported `package main` functions, invisible outside `cmd/cpg`).

### `mcpModeStdout()` handoff
**Source:** `cmd/cpg/mcp.go:119-121`
```go
func mcpModeStdout() io.Writer {
	return os.Stderr
}
```
**Apply to:** `pkg/session/pipeline_config.go`'s `cfg.Stdout` field — this is the FIRST phase where this helper has a live call site (Phase 16 built zero `PipelineConfig`s); a regression here reintroduces the exact stdout-corruption class `TestMCPStdoutPurity`/`TestMCPModeStdoutNeverDefaultsToRealStdout` exist to prevent.

### `sync.Once` idempotency guard
**Source:** `pkg/hubble/health_writer.go:32-40, 193-210` (`snapshotOnce`/`Snapshot()`)
**Apply to:** `pkg/session.Session.stopOnce` — guards the real cancel+wait+finalize sequence so concurrent `stop_session` calls for the same ID all observe the identical completed teardown (Pitfall F).

### Atomic temp+rename writes (context only — `pkg/session` writes zero files itself)
**Source:** `pkg/hubble/health_writer.go:144-162`
```go
tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
// ... write, Close, then:
if err := os.Rename(tmpPath, path); err != nil {
	os.Remove(tmpPath)
	return fmt.Errorf("health writer: atomic rename: %w", err)
}
```
**Apply to:** nothing directly — `pkg/session` never writes artifact files (every write goes through the already-atomic `pkg/evidence`/`pkg/hubble/health_writer.go` writers per the Don't-Hand-Roll table). Relevant only as the reason `get_status`'s filesystem reads (artifact file counts) are torn-safe against a concurrently-writing pipeline goroutine — no read-side locking needed.

### Explicit zero-value defaulting before reaching `PipelineConfig` (Pitfall A — DoS-class bug if skipped)
**Source of the CLI defaults being mirrored:** `cmd/cpg/generate.go:104` (`cmd.Flags().Duration("timeout", 10*time.Second, ...)`), `cmd/cpg/commonflags.go:71` (`f.Duration("flush-interval", 5*time.Second, ...)`)
**Source of the crash this prevents:** `pkg/hubble/aggregator.go:365` (`ticker := time.NewTicker(a.interval)` — no validation upstream; confirmed no defensive check exists anywhere in this file via direct read)
**Apply to:** `pkg/session/manager.go`'s `Start` — an MCP client omitting the optional `timeout`/`flush_interval` JSON args sends nothing, which unmarshals to Go's zero value (`0`), unlike a cobra flag whose default is baked into flag registration. `flush_interval: 0` panics the ticker inside an `errgroup.Go` goroutine, crashing the whole `cpg mcp` process. Default explicitly (10s / 5s) and reject `<= 0` after `time.ParseDuration`, before either value reaches `PipelineConfig`.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `cmd/cpg/mcp_tools.go` — the `mcp.AddTool[In, Out]`/`ToolHandlerFor` registration call itself | controller | request-response | Zero MCP tools are registered anywhere in this codebase today — `cmd/cpg/mcp.go:82-84`'s own comment states this explicitly ("Zero tools registered this phase... Phase 17 adds session tools"). Fall back to RESEARCH.md's Code Example 1 (independently verified against the pinned `go-sdk` v1.6.1 source this session via `go doc`) rather than a codebase analog. |
| `pkg/session/manager.go` — the mutex-guarded single-slot goroutine-lifecycle `Manager` shape | service | event-driven | Grep-confirmed: only two `sync.{Mutex,Once}` sites exist in the entire `pkg/` tree (`pkg/hubble/health_writer.go:38`, `pkg/hubble/unhandled.go:18`), neither is a start/stop lifecycle manager — this codebase has never had a "supervise one long-running background job with an external Start/Stop API" component before. Also: zero existing production `os.MkdirTemp` calls anywhere (only `t.TempDir()` in tests) — the ephemeral-tmpdir-per-session idiom is new. Fall back to RESEARCH.md Pattern 1/2/3 and Code Examples 2-3 as the primary reference, informed by the four partial analogs listed in the Pattern Assignments section above (ctx-cancel shutdown, `sync.Once` idempotency, non-blocking cleanup, errgroup goroutine lifecycle) — each solves one facet of the new component, none solves the whole shape. |

## Metadata

**Analog search scope:** `cmd/cpg/*.go` (all 18 files), `pkg/hubble/{pipeline,pipeline_test,health_writer,aggregator}.go`, `pkg/k8s/{portforward,client,cluster_dedup}.go`, `pkg/evidence/paths.go`, `pkg/flowsource/source.go`, plus a full-tree grep sweep of `pkg/` and `cmd/` for `sync.Mutex|sync.Once|atomic.Pointer|atomic.Value` and `os.MkdirTemp`.
**Files read in depth:** 14
**Pattern extraction date:** 2026-07-20
