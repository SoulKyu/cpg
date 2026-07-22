# Phase 23: Managed Audit Window + SEC-01 Evolution - Pattern Map

**Mapped:** 2026-07-22
**Files analyzed:** 5 new files + documentation updates
**Analogs found:** 8 strong matches (100%)

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `cmd/cpg/audit_window.go` | CLI command (cobra) | request-response, signal-handling | `cmd/cpg/bootstrap.go` + `cmd/cpg/generate.go` | exact |
| `pkg/auditwindow/` (or `pkg/k8s/auditwindow.go`) | service/manager | CRUD + state-machine, bounded shutdown | `pkg/session/manager.go` | exact |
| `pkg/auditwindow/` (SPDY executor) | service | request-response (remote command) | `pkg/k8s/portforward.go` | exact |
| `pkg/auditwindow/` (version detection seam) | utility seam | CRUD (K8s read) | `pkg/k8s/version.go` | exact |
| `cmd/cpg/audit_window_test.go` (SEC-01 tripwire) | test/audit | callgraph analysis | `cmd/cpg/mcp_audit_test.go` | exact |
| `pkg/auditwindow/` (exit-path tests) | test | functional, shutdown lifecycle | `pkg/session/manager_test.go` | exact |
| `README.md` (RBAC + readonly-guarantee sections) | documentation | golden pinning | `cmd/cpg/readme_compat_test.go` | exact |
| `docs/bootstrap-runbook.md` (audit-window step) | documentation + test | golden pinning | `cmd/cpg/runbook_test.go` | exact |

---

## Pattern Assignments

### `cmd/cpg/audit_window.go` (CLI command, foreground lifecycle)

**Analog: `cmd/cpg/bootstrap.go` (constructor, flags, seams pattern)**

**Command Constructor** (bootstrap.go lines 26-51):
```go
// newBootstrapCmd builds the `cpg bootstrap` subcommand
func newBootstrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Generate a namespaced default-deny bootstrap CiliumNetworkPolicy",
		Long: `Emit a namespaced default-deny CiliumNetworkPolicy...`,
		Args: cobra.NoArgs,
		RunE: runBootstrap,
	}
	cmd.Flags().StringP("namespace", "n", "", "target namespace (required)")
	_ = cmd.MarkFlagRequired("namespace")
	return cmd
}
```

Apply pattern: `newAuditWindowCmd()` with `-n <namespace>` (required) and `--ttl <duration>` (with default).

**Package-Level Seam** (bootstrap.go lines 63-77):
```go
// bootstrapDetectVersion is a package-level detection seam so tests can substitute
// a fixed k8s.CompatInfo without a live cluster. Best-effort: yields
// CompatInfo{Source: "undetermined"} on any failure. Never returns an error.
var bootstrapDetectVersion = func(ctx context.Context, logger *zap.Logger) k8s.CompatInfo {
	kubeConfig, err := k8s.LoadKubeConfig()
	if err != nil {
		logger.Warn("bootstrap version detection skipped: kubeconfig not available", zap.Error(err))
		return k8s.CompatInfo{Source: "undetermined"}
	}
	client, err := l7ClientFactory(kubeConfig)
	if err != nil {
		logger.Warn("bootstrap version detection skipped: failed to construct kubernetes client", zap.Error(err))
		return k8s.CompatInfo{Source: "undetermined"}
	}
	detectCtx, cancel := context.WithTimeout(ctx, versionPreflightTimeout)
	defer cancel()
	return k8s.DetectCiliumVersion(detectCtx, client, logger)
}
```

Apply pattern: Create a seam for daemon-wide audit-mode precondition check and version detection gate; tests inject a fixed CompatInfo or error to avoid live cluster.

**Analog: `cmd/cpg/generate.go` (signal handling, foreground lifecycle)**

**Signal Handling + Context** (generate.go lines 202-203):
```go
ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
defer cancel()
```

Apply pattern: Wrap the audit window's watcher and revert fan-out in the same signal-bound context. Use this ctx for all cluster I/O (watch, exec, revert).

**Imports Pattern** (generate.go lines 1-22):
```go
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

Apply pattern: Import `os/signal`, `syscall`, and establish `k8s.CompatInfo` for precondition checks.

**Registration** (main.go line 61):
```go
rootCmd.AddCommand(newAuditWindowCmd())
```

---

### `pkg/auditwindow/manager.go` (or similar) — State machine + bounded shutdown

**Analog: `pkg/session/manager.go` (SESS-05 bounded fan-out pattern)**

**State Machine + Context Lifecycle** (manager.go lines 30-103):
```go
type Manager struct {
	mu sync.Mutex
	session *Session  // single slot, nil means idle
	rootCtx context.Context  // MCP server's long-lived ctx
	logger *zap.Logger
	// ... additional fields
	stopWait time.Duration
	removeWait time.Duration
}

// NewManager constructs a Manager with a long-lived rootCtx
func NewManager(rootCtx context.Context, logger *zap.Logger, ...) *Manager {
	m := &Manager{
		rootCtx:     rootCtx,
		logger:      logger,
		stopWait:    5 * time.Second,
		removeWait:  2 * time.Second,
	}
	// ... bind seams
	return m
}
```

Apply pattern: Create `AuditWindow` (or `Window`) struct holding:
- `mu sync.Mutex` for state guarding
- `endpoints map[string]*CiliumEndpointWindow` keyed by `CiliumEndpoint.UID` (never integer ID)
- `rootCtx context.Context` (not request-scoped)
- `stopWait`, `removeWait` bounded deadlines
- Per-endpoint revert success/failure tracking (atomic counters or sync.Map)

**Stop Method (Bounded Wait + Idempotent)** (manager.go lines 439-498):
```go
func (m *Manager) Stop(id string) (StopResult, error) {
	m.mu.Lock()
	s := m.session
	if s == nil || s.ID != id {
		m.mu.Unlock()
		return StopResult{}, fmt.Errorf("session %q not found or expired", id)
	}
	state := s.State
	tmpDir := s.TmpDir
	m.mu.Unlock()  // Pitfall G — release before the (potentially slow) bounded wait

	if state == StateStopped {
		return s.buildSummary(s.explicitStopSeen.Swap(true), healthPath), nil
	}

	s.stopOnce.Do(func() {  // Pitfall F — only first concurrent caller performs teardown
		s.cancel()
		select {
		case <-s.done:
		case <-time.After(m.stopWait):
			m.logger.Warn("session did not exit within deadline; proceeding", ...)
		}
		// finalize state
	})
	return s.buildSummary(s.explicitStopSeen.Swap(true), healthPath), nil
}
```

Apply pattern: Implement `Close(ctx context.Context) (RevertResult, error)` with:
- `sync.Once` to serialize only-first-caller revert (stopOnce pattern)
- `cancel()` to signal watcher/exec goroutines
- `time.After(m.stopWait)` bounded wait for pending operations
- Per-endpoint result tracking: `RevertResult{ EndpointResults map[string]error }`
- Idempotent: return same summary on second Close

**Shutdown (Unconditional, Independently Bounded)** (manager.go lines 501-545):
```go
func (m *Manager) Shutdown() {
	m.mu.Lock()
	s := m.session
	m.session = nil
	if s == nil {
		m.mu.Unlock()
		return
	}
	state := s.State
	tmpDir := s.TmpDir
	cancel := s.cancel
	done := s.done
	m.mu.Unlock()

	if state == StateCapturing {
		cancel()
		select {
		case <-done:
		case <-time.After(m.stopWait):
			m.logger.Warn("shutdown: session did not exit within deadline; removing tmpdir anyway", ...)
		}
	}

	// Unconditional and independently bounded: wedged filesystem must never prevent exit
	removed := make(chan struct{})
	go func() {
		_ = os.RemoveAll(tmpDir)
		close(removed)
	}()
	select {
	case <-removed:
	case <-time.After(m.removeWait):
		m.logger.Warn("shutdown: tmpdir removal did not complete within deadline", ...)
	}
}
```

Apply pattern: Implement `Shutdown()` (no args) with:
- `m.mu.Lock()` immediately, nil out the window slot, unlock before slow operations (Pitfall G)
- Unconditional `cancel()` on the window's context if capturing
- Independently bounded fan-out: cancel goroutine, bounded-wait select, log warn not error
- Per-endpoint cleanup success tracking (e.g., "reverted N/M endpoints, X errors")

**Error Handling Pattern** (manager.go lines 86-108):
```go
// bootstrapVersionGate is the single decision point
// A determined version below floor -> non-nil, actionable error naming BOTH version and floor
// An undetermined version -> non-empty warning string, nil error
func bootstrapVersionGate(compat k8s.CompatInfo) (warning string, err error) {
	if compat.ClusterVersion != "" {
		for _, f := range compat.BelowFloorFeatures {
			if strings.Contains(f, "enableDefaultDeny") {
				return "", fmt.Errorf(
					"cluster Cilium version %s is below the enableDefaultDeny floor (>= 1.16.0 required); ...",
					compat.ClusterVersion,
				)
			}
		}
		return "", nil
	}
	return "Cilium version undetermined; enableDefaultDeny requires >= 1.16 — proceeding", nil
}
```

Apply pattern: Precondition check before window opens:
```go
func (m *Manager) Open(ctx context.Context, ns string, ttl time.Duration) error {
	// Hard refusal if policy-audit-mode is already active daemon-wide
	isActive, err := m.checkDaemonAuditMode(ctx)  // or k8s.ConfigMapRead + seam
	if err != nil {
		return fmt.Errorf("cannot determine daemon audit mode; refusing to open window: %w", err)
	}
	if isActive {
		return fmt.Errorf(
			"daemon-wide policy-audit-mode is already active; refusing to open a scoped window "+
				"to prevent conflicts. Use `cilium-dbg config PolicyAuditMode=Disabled` on nodes if needed.",
		)
	}
	// proceed
}
```

---

### SPDY Executor Pattern

**Analog: `pkg/k8s/portforward.go` (SPDY RESTClient().Post() shape)**

**SPDY URL Construction + Dialer** (portforward.go lines 44-56):
```go
// Build the port-forward URL
reqURL := clientset.CoreV1().RESTClient().Post().
	Resource("pods").
	Namespace(pod.Namespace).
	Name(pod.Name).
	SubResource("portforward").
	URL()

transport, upgrader, err := spdy.RoundTripperFor(config)
if err != nil {
	return "", nil, fmt.Errorf("creating SPDY round tripper: %w", err)
}

dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, reqURL)
```

Apply pattern for exec (not portforward): substitute `.SubResource("exec")` for `.SubResource("portforward")`. The SPDY construction and dialer shape is identical. The Cilium agent pod is the target pod; the exec command is `cilium-dbg endpoint config <id> PolicyAuditMode=Enabled`.

**Context Cancellation in Exec** (portforward.go lines 71-79):
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
```

Apply pattern: Exec should similarly race `<-ctx.Done()` to return promptly on signal or timeout.

---

### Version Detection & Precondition Seam

**Analog: `pkg/k8s/version.go` (seam pattern for detection, best-effort)**

**Detection Seam** (version.go lines 134-177):
```go
// DetectCiliumVersion detects the cluster's Cilium version
// This NEVER returns an error and NEVER blocks: best-effort with warn-and-proceed
func DetectCiliumVersion(ctx context.Context, client kubernetes.Interface, logger *zap.Logger) CompatInfo {
	if client == nil {
		return finalizeCompat(CompatInfo{Source: "undetermined"}, logger)
	}
	pods, err := client.CoreV1().Pods(ciliumNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: ciliumAgentLabelSelector,
	})
	switch {
	case err == nil:
		versionsSeen, minVersion := tallyPodImageVersions(pods.Items)
		info := CompatInfo{VersionsSeen: versionsSeen, Source: "undetermined"}
		if minVersion != nil {
			info.ClusterVersion = minVersion.String()
			info.Source = "pod-images"
		}
		return finalizeCompat(info, logger)
	case apierrors.IsForbidden(err):
		logger.Warn(warnPodsListForbidden, zap.Error(err))
		return finalizeCompat(CompatInfo{Source: "undetermined"}, logger)
	default:
		if !errors.Is(ctx.Err(), context.Canceled) {
			logger.Warn(warnPodsListFailed, zap.Error(err))
		}
		return finalizeCompat(CompatInfo{Source: "undetermined"}, logger)
	}
}
```

Apply pattern: Create a seam for daemon-audit-mode precondition check:
```go
var checkDaemonAuditMode = func(ctx context.Context, config *rest.Config, logger *zap.Logger) (bool, error) {
	// Production: read cilium-config ConfigMap in kube-system, parse policy-audit-mode field
	// Test: inject stub via seam (return false or deterministic error)
	// Never blocks on this check; RBAC-denied is treated as "undetermined, proceed"
}
```

---

### Exit-Path Tests Pattern

**Analog: `pkg/session/manager_test.go` (exit path testing with fakes)**

**Fake Flow Source + Bounded Waits** (manager_test.go lines 33-80):
```go
type closedFlowSource struct { flows []*flowpb.Flow }
func (c *closedFlowSource) StreamDroppedFlows(...) (<-chan *flowpb.Flow, ...) {
	flowCh := make(chan *flowpb.Flow, len(c.flows))
	lostCh := make(chan *flowpb.LostEvent)
	for _, f := range c.flows { flowCh <- f }
	close(flowCh); close(lostCh)
	return flowCh, lostCh, nil
}

type blockingFlowSource struct { flow *flowpb.Flow }
func (b *blockingFlowSource) StreamDroppedFlows(ctx context.Context, ...) (<-chan *flowpb.Flow, ...) {
	flowCh := make(chan *flowpb.Flow, 1)
	lostCh := make(chan *flowpb.LostEvent)
	if b.flow != nil { flowCh <- b.flow }
	go func() {
		<-ctx.Done()
		close(flowCh); close(lostCh)
	}()
	return flowCh, lostCh, nil
}

// wedgedRunPipeline simulates a step that never observes ctx cancellation
func wedgedRunPipeline(release <-chan struct{}) func(context.Context, ...) error {
	return func(_ context.Context, _ ...) error {
		<-release
		return nil
	}
}
```

Apply pattern for audit window: create fakes for endpoint watcher and exec commands:
```go
type fakeWatcher struct {
	endpoints []ciliumv2.CiliumEndpoint
	ctx context.Context
	cancel context.CancelFunc
}

type fakeExecResult struct {
	err error
	duration time.Duration
}

// wedgedExec simulates an exec that never observes ctx cancellation
func wedgedExec(release <-chan struct{}) func(context.Context, ...) error {
	return func(_ context.Context, _ ...) error {
		<-release
		return nil
	}
}
```

**Test Structure for Exit Paths** (manager_test.go lines 546-591):
```go
// TestManager_Shutdown proves SESS-05: bounded timeout, no block
func TestManager_Shutdown(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})
	res, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)
	
	status, err := m.Status(res.SessionID)
	require.NoError(t, err)
	tmpDir := status.TmpDir
	
	begin := time.Now()
	m.Shutdown()
	elapsed := time.Since(begin)
	
	assert.Less(t, elapsed, 4*(m.stopWait+m.removeWait), "shutdown should return promptly")
	_, statErr := os.Stat(tmpDir)
	assert.True(t, os.IsNotExist(statErr))
}

// TestManager_Shutdown_WedgedStepDoesNotBlock proves bounded fan-out
func TestManager_Shutdown_WedgedStepDoesNotBlock(t *testing.T) {
	m := newTestManager(t, &blockingFlowSource{flow: someFlow()})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	m.runPipeline = wedgedRunPipeline(release)
	
	_, err := m.Start(context.Background(), StartArgs{Server: "bypass:1"})
	require.NoError(t, err)
	
	done := make(chan struct{})
	go func() {
		m.Shutdown()
		close(done)
	}()
	
	select {
	case <-done:
	case <-time.After(4 * (m.stopWait + m.removeWait)):
		t.Fatal("Shutdown did not return within deadline despite wedged step")
	}
}
```

Apply pattern: Test each exit scenario (explicit Close, SIGTERM via signal handler, TTL expiry, exec transport death):
- Close (explicit): blocking Close call, verify revert fires
- SIGTERM: signal in background goroutine, race against Close, verify bounded exit
- TTL expiry: set up a timer trigger, verify revert fires
- Transport death: inject wedged exec stub, verify Shutdown still returns in bounded time

---

## Shared Patterns

### Command-to-Manager Integration

**Analog: bootstrap.go + session/manager.go pattern**

In `cmd/cpg/audit_window.go`, the RunE handler:
```go
func runAuditWindow(cmd *cobra.Command, _ []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	ttl, _ := cmd.Flags().GetDuration("ttl")
	
	// Precondition: hard-refuse on determined dangerous state
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	
	compat := auditWindowDetectVersion(ctx, logger)  // seam, never-error
	if err := auditWindowVersionGate(compat); err != nil {
		return err
	}
	
	// Create the window manager (no MCP context here; pure CLI)
	wm := auditwindow.NewManager(ctx, logger)
	
	// Open the window (precondition check inside)
	revertCh, err := wm.Open(ctx, namespace, ttl)
	if err != nil {
		return err
	}
	
	// Foreground: watcher + exit-path cleanup on every branch
	defer func() {
		// SESS-05 bounded shutdown on every exit
		wm.Shutdown()
	}()
	
	// Block on TTL timer or ctx cancellation
	<-ctx.Done()  // Or watch TTL expiry timer + ctx together
	
	// Close initiates revert (idempotent, bounded)
	result, err := wm.Close(ctx)
	if err != nil {
		logger.Error("close failed", zap.Error(err))
		return err
	}
	
	// Report revert summary per endpoint
	for ep, revErr := range result.EndpointResults {
		if revErr != nil {
			logger.Warn("revert failed for endpoint", zap.String("id", ep), zap.Error(revErr))
		} else {
			logger.Info("reverted endpoint", zap.String("id", ep))
		}
	}
	
	return nil
}
```

---

### Error Handling Conventions

**Analog: bootstrap.go + generate.go error wording**

Hard refusal on determined dangerous state:
```go
return fmt.Errorf(
	"daemon-wide policy-audit-mode is already active; refusing to open a scoped window "+
		"to prevent conflicts. Use `cilium-dbg config PolicyAuditMode=Disabled` on nodes if needed.",
)
```

Warn-and-proceed on undetermined state:
```go
logger.Warn("daemon audit mode undetermined; proceeding with window open", zap.Error(err))
```

Per-item failures (revert summary):
```go
logger.Warn("revert failed for endpoint", zap.String("endpoint_uid", uid), zap.Error(err))
```

---

### Logger Usage

**Analog: bootstrap.go + generate.go conventions**

Always include structured fields:
```go
logger.Info("audit window opened",
	zap.String("namespace", namespace),
	zap.Duration("ttl", ttl),
	zap.String("endpoint_count", len(endpoints)),
)

logger.Warn("endpoint revert timeout", 
	zap.String("endpoint_uid", uid),
	zap.Duration("waited", m.stopWait),
)

logger.Error("watcher failed",
	zap.String("namespace", namespace),
	zap.Error(err),
)
```

---

## SEC-01 Tripwire Test Pattern

**Analog: `cmd/cpg/mcp_audit_test.go` (BFS harness, callPathFrom)**

**Test Structure** (mcp_audit_test.go lines 205-345):
```go
// TestMCPAuditReadonlyReachability is the SEC-01 structural audit
// Stage 1: Load + SSA build
cfg := &packages.Config{Mode: packages.LoadAllSyntax, Tests: false, Dir: "."}
initial, err := packages.Load(cfg, ".")
require.NoError(t, err)

mode := ssa.InstantiateGenerics
prog, pkgs := ssautil.AllPackages(initial, mode)
prog.Build()

// Stage 2: RTA rooted at main+init, BFS from runMCPServer
rtaRes := rta.Analyze([]*ssa.Function{mainFn, initFn}, true)
bfsRes := bfsFromRoot(rtaRes.CallGraph, root)

// Stage 3: restrict to cpg-owned functions
cpgOwned := make(map[*ssa.Function]bool)
for f := range bfsRes.visited {
	if f != nil && f.Pkg != nil && f.Pkg.Pkg != nil && 
	   strings.HasPrefix(f.Pkg.Pkg.Path(), cpgModulePrefix) {
		cpgOwned[f] = true
	}
}

// Scan each function's own call instructions
for f := range cpgOwned {
	for _, b := range f.Blocks {
		for _, instr := range b.Instrs {
			call, ok := instr.(ssa.CallInstruction)
			if !ok { continue }
			// ... Property 1 (K8s writes) and Property 2 (FS writes) checks
		}
	}
}
```

Apply to Phase 23: Add a tripwire assertion (same audit file or sibling `cmd/cpg/audit_window_test.go`):

```go
// TestAuditWindowNotReachableFromMCP is SEC-01's extension: prove that
// remotecommand.NewSPDYExecutor and the audit-window entry point are NOT
// reachable from runMCPServer, AND are reachable ONLY from the audit-window
// command entry point among cpg-owned composition roots.
func TestAuditWindowNotReachableFromMCP(t *testing.T) {
	// Stage 1-2: same setup as TestMCPAuditReadonlyReachability
	// BFS from runMCPServer
	// Stage 3: scan reachable functions
	
	// NEW: Also BFS from runAuditWindow (the audit-window entry point)
	auditRoot := mainPkg.Func("runAuditWindow")
	require.NotNil(t, auditRoot)
	auditBfsRes := bfsFromRoot(rtaRes.CallGraph, auditRoot)
	
	// Property: runMCPServer must NOT reach remotecommand.NewSPDYExecutor
	execConstructor := "k8s.io/client-go/tools/remotecommand.NewSPDYExecutor"
	require.NotContains(t, symbolSet(bfsRes.visited),
		execConstructor,
		"SEC-01: remotecommand.NewSPDYExecutor must not be reachable from runMCPServer")
	
	// Property: runAuditWindow MUST reach remotecommand.NewSPDYExecutor
	require.Contains(t, symbolSet(auditBfsRes.visited),
		execConstructor,
		"SEC-01: remotecommand.NewSPDYExecutor must be reachable from runAuditWindow")
	
	// Positive path: prove the exec constructor is reachable ONLY from audit-window
	// (or other CLI-only commands, never from MCP), using callPathFrom to show the route
	for f := range auditBfsRes.visited {
		// Optional: log the call path for audit clarity
		if strings.Contains(f.String(), "remotecommand") {
			path := callPathFrom(auditBfsRes, auditRoot, f)
			t.Logf("exec reachable via: %s", path)
		}
	}
}
```

---

## Documentation Pinning Tests

### README.md Golden Test

**Analog: `cmd/cpg/readme_compat_test.go` (lines 31-81)**

Add a test (e.g., in `cmd/cpg/readme_compat_test.go` or new `cmd/cpg/audit_window_test.go`):

```go
// TestReadmeAuditWindowSection pins the RBAC + readonly-guarantee statement
// for Phase 23 criterion 5 (README honesty).
func TestReadmeAuditWindowSection(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	require.NoError(t, err)
	readme := string(data)
	
	// AUD-03 criterion 5a: README must state the readonly claim for MCP
	assert.Contains(t, readme, "MCP server is readonly",
		"README must state that the MCP server remains readonly (no mutations)")
	
	// AUD-03 criterion 5b: README must state audit-window is the mutation surface
	assert.Contains(t, readme, "cpg audit-window",
		"README must mention cpg audit-window as the mutation command")
	
	assert.Contains(t, readme, "PolicyAuditMode",
		"README must name the per-endpoint mutation (PolicyAuditMode)")
	
	// AUD-03 criterion 5c: README RBAC section must list the audit-window perms
	assert.Contains(t, readme, "pods/exec",
		"README must list pods/exec as required for audit-window")
	
	assert.Contains(t, readme, "ciliumendpoints",
		"README must list ciliumendpoints (list/watch) as required for audit-window")
	
	// Negative guard: audit-window must be clearly separated from readonly commands
	lines := strings.Split(readme, "\n")
	var auditWindowMentioned bool
	for _, line := range lines {
		if strings.Contains(line, "cpg audit-window") {
			auditWindowMentioned = true
			// The same line or nearby context should clarify lifecycle-bound + revert guarantee
			break
		}
	}
	assert.True(t, auditWindowMentioned, "README must have an explicit audit-window section")
}
```

### Runbook Golden Test

**Analog: `cmd/cpg/runbook_test.go` (lines 32-93)**

Add or extend a test (e.g., in `cmd/cpg/runbook_test.go`):

```go
// TestRunbookAuditWindowStep pins the audit-window runbook step
// (added in Phase 23 to docs/bootstrap-runbook.md).
func TestRunbookAuditWindowStep(t *testing.T) {
	data, err := os.ReadFile("../../docs/bootstrap-runbook.md")
	require.NoError(t, err)
	runbook := string(data)
	
	// Must reference the real command
	assert.Contains(t, runbook, "cpg audit-window",
		"runbook must reference the cpg audit-window command")
	
	// Must document the TTL
	assert.Contains(t, runbook, "--ttl",
		"runbook must document the --ttl flag")
	
	// Must warn about the race window (new endpoints enforced before flip lands)
	assert.Contains(t, runbook, "race",
		"runbook must honestly document the new-endpoint race window")
	
	// Must link to RBAC prerequisites
	assert.Contains(t, runbook, "pods/exec",
		"runbook must reference the pods/exec RBAC requirement")
}
```

---

## No Analog Found

All core patterns have strong existing analogs in the codebase. No file requires RESEARCH.md patterns as fallback.

---

## Metadata

**Analog search scope:** 
- cmd/cpg/*.go (CLI commands, signal handling, integration tests)
- pkg/session/manager.go (state machine, bounded shutdown fan-out, exit paths)
- pkg/k8s/portforward.go (SPDY RESTClient pattern)
- pkg/k8s/version.go (detection seams, best-effort error handling)

**Files scanned:** 8 analog files + 2 documentation files

**Confidence:** HIGH — exact role and data flow matches for all five new/modified files. All patterns have proven precedent in the cpg codebase.

---

## Pattern Extraction Notes

### Key Conventions Observed

1. **Imports:** Use `"github.com/SoulKyu/cpg/pkg/..."` path alias in internal imports; `"go.uber.org/zap"` for logging; `"github.com/spf13/cobra"` for CLI; `"k8s.io/client-go/..."` for K8s client.

2. **Error Wording:** 
   - Hard refusal: "refusing to ... because [condition]; [suggestion]"
   - Warn-and-proceed: "[operation] [reason]; proceeding" at logger.Warn level
   - Per-item failures: structured fields with zap (endpoint_uid, error, duration)

3. **Logger Field Conventions:**
   - `zap.String("field_name", value)` for identifiers
   - `zap.Duration("field_name", value)` for time
   - `zap.Int("field_name", value)` for counts
   - `zap.Error(err)` last in the field list

4. **Testing:**
   - Use `testify/require` for fatal assertions; `testify/assert` for non-fatal
   - Fake/stub dependencies passed as fields (e.g., `Manager.runPipeline`)
   - Bounded waits shrunk in tests (e.g., `m.stopWait = 100 * time.Millisecond`)
   - Golden/pinning tests use `strings.Contains(readFile(...), "expected string")`

5. **Context Cancellation:**
   - CLI: `signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)`
   - Always `defer cancel()` immediately
   - Use `select { case <-ctx.Done(): ... }`  for bounded operations

---
