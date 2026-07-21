package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"k8s.io/client-go/rest"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/hubble"
	"github.com/SoulKyu/cpg/pkg/k8s"
)

// Manager is the mutex-guarded, single-active-session state machine that
// drives a Session end to end: Start spawns hubble.RunPipeline in a
// background goroutine on a server-rooted (not request-scoped) context,
// Status/Stop read and finalize it, and Shutdown bounds cleanup on every
// process-exit path. Exactly one session may be capturing at a time
// (SESS-02); a stopped session is retained until the next Start's purge or
// Shutdown (D-01).
type Manager struct {
	mu sync.Mutex
	// session is the single slot: nil means idle. A multi-session registry
	// (map[string]*Session) is deliberately rejected — RESEARCH.md /
	// ARCHITECTURE.md Anti-Pattern 4 and REQUIREMENTS.md's explicit
	// single-session scope.
	session *Session

	// rootCtx is the MCP server's own long-lived ctx (e.g. the
	// signal.NotifyContext-derived ctx cmd/cpg/mcp.go's runMCPServer
	// receives), captured once at construction. Storing a ctx as a struct
	// field is normally a Go anti-pattern for per-call contexts; the
	// accepted exception is exactly this shape — a long-lived component's
	// own lifecycle boundary, not a per-call scope (RESEARCH.md Pattern 2).
	// Every session's background pipeline ctx forks from THIS field, never
	// from a tool-handler's per-call request ctx (Pitfall C).
	rootCtx context.Context

	logger *zap.Logger
	// stdout is the writer for the pipeline's human-readable summary block
	// (plan 17-04 passes mcpModeStdout() — never resolved by name here).
	stdout     io.Writer
	cpgVersion string

	// runPipeline defaults to hubble.RunPipeline; swappable in tests to
	// hubble.RunPipelineWithSource plus a fake flowsource.FlowSource, so
	// pkg/session's suite needs no real cluster and no MCP SDK.
	runPipeline func(ctx context.Context, cfg hubble.PipelineConfig) error

	// resolveSetupFn is a test-only injectable seam over resolveSetup
	// (kubeconfig/port-forward/cluster-dedup). NewManager defaults it to
	// m.resolveSetup (the production implementation); same-package tests
	// swap it to gate the setup window or inject a deterministic setup
	// failure with no cluster. Unexported and NOT a NewManager parameter —
	// the exported constructor signature plan 17-04 calls is unaffected.
	resolveSetupFn func(setupCtx context.Context, args StartArgs) (server string, cleanup func(), clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy, err error)

	// stopWait bounds Stop/Shutdown's wait for the pipeline goroutine to
	// observe ctx cancellation and exit. removeWait independently bounds
	// os.RemoveAll so a wedged filesystem step can never block process exit
	// (SESS-05). NewManager sets sensible defaults; same-package tests
	// shrink both so bounded-wait paths run fast.
	stopWait   time.Duration
	removeWait time.Duration
}

// NewManager constructs a Manager. rootCtx MUST be the MCP server's own
// long-lived ctx (never a per-tool-call request ctx — Pitfall C); stdout is
// the writer plan 17-04 wires every session's PipelineConfig.Stdout to;
// cpgVersion is the CLI's build-time version string, which pkg/session
// cannot see on its own.
func NewManager(rootCtx context.Context, logger *zap.Logger, stdout io.Writer, cpgVersion string) *Manager {
	m := &Manager{
		rootCtx:     rootCtx,
		logger:      logger,
		stdout:      stdout,
		cpgVersion:  cpgVersion,
		runPipeline: hubble.RunPipeline,
		stopWait:    5 * time.Second,
		removeWait:  2 * time.Second,
	}
	// Bind the seam as a method value AFTER m is fully constructed, so the
	// default target is the finished Manager, not a partially-built one.
	m.resolveSetupFn = m.resolveSetup
	return m
}

// Start creates a new capturing session, or rejects/purges per the state
// machine (SESS-02/D-04). The single slot is claimed under m.mu BEFORE the
// slow synchronous setup (kubeconfig/port-forward/cluster-dedup) runs, so
// two concurrent Start calls can never both succeed — exactly one owns the
// slot, the loser is rejected at the SESS-02 check below, and a setup
// failure (or a Shutdown racing the setup window) rolls the slot back to
// nil with no orphaned goroutine/tmpdir.
func (m *Manager) Start(reqCtx context.Context, args StartArgs) (StartResult, error) {
	m.mu.Lock()
	if m.session != nil && m.session.State == StateCapturing {
		active := m.session
		m.mu.Unlock()
		return StartResult{}, fmt.Errorf(
			"session %s already running (started %s ago); call stop_session first",
			active.ID, time.Since(active.StartedAt).Round(time.Second))
	}

	var discarded string
	if m.session != nil {
		// Retained stopped session — D-04 silent purge: this start wins,
		// the old tmpdir is removed, the response notes what was discarded.
		discarded = m.session.ID
		_ = os.RemoveAll(m.session.TmpDir)
		m.session = nil
	}

	// Fork the background ctx and publish the placeholder slot NOW, still
	// holding m.mu — before the slow setup, not after (Blocker fix: the
	// idle/stopped slot would otherwise be a TOCTOU hazard). The
	// StateCapturing placeholder means a concurrent Start immediately hits
	// the SESS-02 branch above; exactly one Start can own the slot. The ctx
	// forks from m.rootCtx (Pattern 2), NEVER reqCtx, so it survives this
	// specific tool call returning.
	sessionCtx, sessionCancel := context.WithCancel(m.rootCtx)
	s := &Session{
		ID:        "sess_" + uuid.New().String(),
		StartedAt: time.Now(),
		State:     StateCapturing,
		cancel:    sessionCancel,
		done:      make(chan error, 1),
	}
	m.session = s
	m.mu.Unlock()

	// fail releases the claimed slot and cancels the forked ctx on any
	// synchronous-setup failure. The m.session == s guard avoids clobbering
	// a concurrent Shutdown that already nil'd the slot out from under us.
	fail := func(err error) (StartResult, error) {
		sessionCancel()
		m.mu.Lock()
		if m.session == s {
			m.session = nil
		}
		m.mu.Unlock()
		return StartResult{}, err
	}

	tmpDir, err := os.MkdirTemp("", "cpg-session-*")
	if err != nil {
		return fail(fmt.Errorf("creating session tmpdir: %w", err))
	}

	timeout := defaultDuration(args.Timeout, 10*time.Second)
	setupCtx, setupCancel := context.WithTimeout(reqCtx, timeout) // Pitfall H — bounds the WHOLE setup, not just the gRPC dial
	defer setupCancel()
	// WR-02: setupCtx is now bounded by THREE independent signals — its own
	// timeout above, the per-call reqCtx it forks from, and sessionCtx via
	// this AfterFunc merge. Without this third signal, Shutdown's
	// s.cancel() (== sessionCancel, which only cancels sessionCtx) had no
	// way to reach a mid-setup resolveSetupFn call: setupCtx and sessionCtx
	// shared no parent Shutdown could reach. Merging them means a
	// transport-kill/SIGTERM landing while resolveSetupFn is still running
	// now cancels setupCtx too, so any setup step that observes its ctx
	// (PortForwardToRelay, LoadClusterPoliciesForNamespaces below) returns
	// promptly instead of running to setupCtx's own timeout — Start's
	// existing os.RemoveAll(tmpDir) error path a few lines down then
	// reclaims the tmpdir instead of orphaning it. Accepted residual:
	// k8s.LoadKubeConfig() below takes no ctx parameter at all, so a hang
	// specifically inside kubeconfig load is reachable by neither the
	// timeout nor this cancellation — a pre-existing upstream helper
	// limitation left unaddressed this phase (T-17-06-03). It does not
	// block process exit: Shutdown's own fan-out is independently bounded
	// and returns regardless of this goroutine.
	stopSetupOnShutdown := context.AfterFunc(sessionCtx, setupCancel)
	defer stopSetupOnShutdown()

	server, portForwardCleanup, clusterPolicies, err := m.resolveSetupFn(setupCtx, args)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return fail(err)
	}

	cfg := buildPipelineConfig(args, tmpDir, server, m.logger, m.cpgVersion, m.stdout, clusterPolicies, func(st hubble.SessionStats) {
		s.final.Store(&st)
	})

	// D-10: correlate the opaque MCP handle with the internal evidence
	// SessionID on a single log line.
	m.logger.Info("session started",
		zap.String("session_id", s.ID),
		zap.String("evidence_session_id", cfg.SessionID),
	)

	m.mu.Lock()
	if m.session != s {
		// A concurrent Shutdown nil'd the slot while this Start was still
		// mid-setup (Shutdown never touches m.mu until it runs, so this
		// Start cannot observe the change until now) — abort cleanly: no
		// orphaned goroutine, no orphaned tmpdir, no orphaned port-forward.
		m.mu.Unlock()
		sessionCancel()
		portForwardCleanup()
		_ = os.RemoveAll(tmpDir)
		return StartResult{}, fmt.Errorf("server shutting down; session %s aborted", s.ID)
	}
	s.TmpDir = tmpDir
	m.mu.Unlock()

	go func() {
		err := m.runPipeline(sessionCtx, cfg)
		portForwardCleanup() // non-blocking (close(stopCh)) — before signaling done, so an

		// WR-01 (17-08): a GENUINE failure is classified on whether the
		// session's OWN ctx (sessionCtx) was cancelled, not on the kind of
		// error runPipeline returned. sessionCtx.Err() is non-nil ONLY when
		// Stop/Shutdown (or m.rootCtx) actually cancelled this session —
		// the sole "this was on purpose" signal — so any other non-nil err
		// is a genuine failure: relay connection reset, auth expiry, an
		// unreachable/typo'd --server address, INCLUDING a
		// context.DeadlineExceeded produced by an unrelated SCOPED timeout
		// such as pkg/hubble/client.go's dial timeout, which derives its
		// own child ctx from this still-healthy sessionCtx. Autonomously
		// transition the session to stopped so get_status stops reporting
		// "capturing" forever for a dead session. A clean drain (nil) or a
		// genuine sessionCtx cancellation (Stop/Shutdown) is deliberately
		// left untouched — Stop/Shutdown remain the sole state drivers for
		// those paths, so every pre-existing test keeps passing unchanged.
		if err != nil && sessionCtx.Err() == nil {
			s.pipelineErr.Store(&err)
			m.logger.Warn("session pipeline exited with error", zap.String("session_id", s.ID), zap.Error(err))
			m.mu.Lock()
			// Guard against clobbering a slot a concurrent Shutdown already
			// nil'd, or a State a concurrent Stop already transitioned.
			if m.session == s && s.State == StateCapturing {
				s.State = StateStopped
				s.StoppedAt = time.Now()
			}
			m.mu.Unlock()
			// INFO (17-08): release sessionCtx's registration on m.rootCtx
			// now that the pipeline has autonomously exited — s.cancel is
			// idempotent and a no-op for the already-exited pipeline, and
			// safe w.r.t. Start's context.AfterFunc(sessionCtx,
			// setupCancel): Start's own deferred stopSetupOnShutdown()
			// already un-registered that AfterFunc before this goroutine's
			// runPipeline call returned.
			s.cancel()
		}

		s.done <- err // observer of done also knows the port-forward is already closing
	}()

	return StartResult{SessionID: s.ID, DiscardedSession: discarded, Server: server}, nil
}

// resolveSetup is the production implementation bound to the
// resolveSetupFn seam by NewManager. It ports cmd/cpg/generate.go:165-212
// under setupCtx (Pitfall H — the entire synchronous setup is bounded, not
// just the gRPC dial): D-07's server bypass skips kubeconfig/port-forward
// entirely; otherwise it auto-port-forwards to hubble-relay. cluster_dedup
// independently (re-)loads a kubeconfig even when the server bypass was
// used, mirroring generate.go's own independent-of-server nuance.
func (m *Manager) resolveSetup(setupCtx context.Context, args StartArgs) (server string, cleanup func(), clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy, err error) {
	var kubeConfig *rest.Config
	if args.Server != "" {
		// D-07: an explicit server bypasses kubeconfig + auto port-forward.
		server = args.Server
		cleanup = func() {}
	} else {
		kubeConfig, err = k8s.LoadKubeConfig()
		if err != nil {
			return "", nil, nil, fmt.Errorf("--server not provided and kubeconfig not available: %w", err)
		}

		localAddr, pfCleanup, pfErr := k8s.PortForwardToRelay(setupCtx, kubeConfig, m.logger)
		if pfErr != nil {
			if errors.Is(pfErr, context.DeadlineExceeded) {
				return "", nil, nil, fmt.Errorf(
					"kubeconfig auth did not complete within the setup timeout; re-authenticate outside the MCP session (e.g. run `kubectl get pods` once in a real shell) and retry: %w", pfErr)
			}
			return "", nil, nil, fmt.Errorf("auto port-forward to hubble-relay failed: %w", pfErr)
		}
		server = localAddr
		cleanup = pfCleanup
	}

	if args.ClusterDedup {
		if kubeConfig == nil {
			kubeConfig, err = k8s.LoadKubeConfig()
			if err != nil {
				cleanup()
				return "", nil, nil, fmt.Errorf("cluster_dedup requires kubeconfig: %w", err)
			}
		}
		clusterPolicies, err = k8s.LoadClusterPoliciesForNamespaces(setupCtx, kubeConfig, dedupNamespaces(args))
		if err != nil {
			cleanup()
			return "", nil, nil, fmt.Errorf("loading cluster policies for dedup: %w", err)
		}
	}

	return server, cleanup, clusterPolicies, nil
}

// dedupNamespaces mirrors generate.go's clusterDedupNamespaces: an
// all-namespaces or empty selection maps to the single "" sentinel (list
// across every namespace); otherwise the explicit namespace list is used.
func dedupNamespaces(args StartArgs) []string {
	if args.AllNamespaces || len(args.Namespaces) == 0 {
		return []string{""}
	}
	return args.Namespaces
}

// Status returns coarse state for a capturing or retained-stopped session.
// A stopped session stays queryable (D-02/SESS-03) — this is a pure
// filesystem read plus an in-memory snapshot, never touching pipeline
// internals directly.
func (m *Manager) Status(id string) (StatusResult, error) {
	m.mu.Lock()
	s := m.session
	if s == nil || s.ID != id {
		m.mu.Unlock()
		return StatusResult{}, fmt.Errorf("session %q not found or expired", id) // SESS-06 (D-02: never for a retained stopped id)
	}
	// Copy every field this method needs WHILE STILL HOLDING m.mu — Stop
	// writes State/StoppedAt under this same lock, so an unlocked read here
	// would be a data race.
	state := s.State
	startedAt := s.StartedAt
	stoppedAt := s.StoppedAt
	tmpDir := s.TmpDir
	sid := s.ID
	m.mu.Unlock()

	elapsed := time.Since(startedAt)
	if state == StateStopped {
		elapsed = stoppedAt.Sub(startedAt) // frozen, not still ticking
	}

	// Filesystem globbing runs OUTSIDE the lock, on the copied tmpDir —
	// never hold m.mu across I/O. A glob error is non-fatal: log and zero.
	policyCount, err := countGlob(filepath.Join(tmpDir, "policies", "*", "*.yaml"))
	if err != nil {
		m.logger.Warn("status: policy file glob failed", zap.String("session_id", sid), zap.Error(err))
		policyCount = 0
	}
	evidenceCount, err := countGlob(filepath.Join(tmpDir, "evidence", "*", "*", "*.json"))
	if err != nil {
		m.logger.Warn("status: evidence file glob failed", zap.String("session_id", sid), zap.Error(err))
		evidenceCount = 0
	}

	// WR-01: pipelineErr is atomic — safe to load outside m.mu. The
	// StateStopped elapsed-freeze above already handles the frozen-elapsed
	// case for a crashed session, since the launch goroutine set StoppedAt
	// alongside pipelineErr.
	var statusErr string
	if p := s.pipelineErr.Load(); p != nil && *p != nil {
		statusErr = (*p).Error()
	}

	return StatusResult{
		SessionID:         sid,
		State:             state.String(),
		Elapsed:           elapsed.Round(time.Second).String(),
		PolicyFileCount:   policyCount,
		EvidenceFileCount: evidenceCount,
		TmpDir:            tmpDir,
		Error:             statusErr,
	}, nil
}

// Stop cancels the session's ctx, bounded-waits for the pipeline goroutine
// to exit, finalizes State/StoppedAt, and returns the final summary. Never
// removes the tmpdir (D-01 retention — see Manager.Start's purge and
// Manager.Shutdown for the only two removal points). Idempotent: a second
// Stop on the same id returns the identical summary with an
// already-stopped marker, never an error (D-03).
func (m *Manager) Stop(id string) (StopResult, error) {
	m.mu.Lock()
	s := m.session
	if s == nil || s.ID != id {
		m.mu.Unlock()
		return StopResult{}, fmt.Errorf("session %q not found or expired", id) // SESS-06
	}
	state := s.State
	tmpDir := s.TmpDir
	m.mu.Unlock() // Pitfall G — release before the (potentially slow) bounded wait

	// Inline recompute: Session carries no outputHash field (session.go
	// belongs to plan 17-02, not this plan's files_modified). This is the
	// exact formula buildPipelineConfig uses for PipelineConfig.OutputHash.
	outputHash := evidence.HashOutputDir(filepath.Join(tmpDir, "policies"))
	healthPath := filepath.Join(tmpDir, "evidence", outputHash, "cluster-health.json")

	if state == StateStopped {
		return s.buildSummary(true, healthPath), nil // D-03: idempotent, already-stopped marker, never isError
	}

	s.stopOnce.Do(func() { // Pitfall F — only the first concurrent caller performs the real teardown
		s.cancel()
		select {
		case <-s.done:
		case <-time.After(m.stopWait):
			m.logger.Warn("session did not exit within deadline; proceeding", zap.String("session_id", id))
		}
		m.mu.Lock()
		s.State = StateStopped
		s.StoppedAt = time.Now()
		m.mu.Unlock()
	})

	// s.StoppedAt was written under m.mu inside the Do closure above. Every
	// caller of Stop — including one that merely observed state ==
	// StateStopped above and returned early — only reaches a buildSummary
	// call after its own m.mu Lock/Unlock cycle, which is ordered after
	// whichever call last wrote StoppedAt by the mutex's happens-before
	// guarantee. No additional synchronization is needed for this read.
	return s.buildSummary(false, healthPath), nil
}

// Shutdown synchronously, boundedly tears down the active session (if any)
// on every process-exit path (SESS-05): cancel, bounded-wait for the
// pipeline to exit, then an UNCONDITIONAL, independently-bounded tmpdir
// removal — a single wedged step can never block process exit. Removes
// both a just-capturing and a retained-stopped session's tmpdir (D-01
// "...or at server shutdown").
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
		cancel() // context.CancelFunc: idempotent, instant, non-nil (Start sets it at slot-claim time)
		select {
		case <-done:
		case <-time.After(m.stopWait):
			m.logger.Warn("shutdown: session did not exit within deadline; removing tmpdir anyway", zap.String("tmpdir", tmpDir))
		}
	}

	// Unconditional and independently bounded: a wedged filesystem must
	// never prevent process exit. If Shutdown races a Start still mid-setup,
	// the copied tmpDir is "" here (Start only sets s.TmpDir at finalize),
	// so os.RemoveAll("") is a safe no-op — Start's own finalize guard
	// (m.session != s) is what removes that session's real tmpdir.
	removed := make(chan struct{})
	go func() {
		_ = os.RemoveAll(tmpDir)
		close(removed)
	}()
	select {
	case <-removed:
	case <-time.After(m.removeWait):
		m.logger.Warn("shutdown: tmpdir removal did not complete within deadline", zap.String("tmpdir", tmpDir))
	}
}

// countGlob returns the number of filesystem matches for pattern.
func countGlob(pattern string) (int, error) {
	matches, err := filepath.Glob(pattern)
	return len(matches), err
}
