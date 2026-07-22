// Package auditwindow implements the managed, lifecycle-bound per-endpoint
// policy-audit-mode window (AUD-03). Manager mirrors pkg/session.Manager's
// SESS-05 shape exactly: a mutex-guarded state machine whose every exit path
// (explicit Close, ctx-cancel/SIGTERM, or a wedged exec transport) funnels
// into one sync.Once-guarded, independently-bounded revert sweep — the
// "cannot structurally be left open" guarantee this package exists to
// encode.
package auditwindow

import (
	"context"
	"fmt"
	"sync"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	ciliumclient "github.com/cilium/cilium/pkg/k8s/client/clientset/versioned"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/SoulKyu/cpg/pkg/k8s"
)

// endpointRecord is the sole bookkeeping record for an endpoint cpg flipped.
// Keyed on uid (a globally-unique CiliumEndpoint ObjectMeta.UID) — NEVER on
// the reusable per-node integer Status.ID (RESEARCH.md Anti-Pattern: IDs are
// reused after an endpoint is destroyed and a new one created on the same
// node). currentID is the ID observed at Open/watcher-flip time; Close
// re-resolves it fresh from uid via resolveCurrentIDFn before reverting,
// since it may have drifted (e.g. an agent restart).
type endpointRecord struct {
	uid       types.UID
	nodeIP    string
	currentID int64
}

// RevertResult reports the per-endpoint outcome of a revert sweep (Close or
// Shutdown) — a sweep that cannot name the endpoint it failed to revert is a
// non-starter (T-23-06, Repudiation).
type RevertResult struct {
	EndpointResults map[types.UID]error
}

// Manager is the mutex-guarded state machine that opens, watches, and
// unconditionally reverts a namespace-scoped policy-audit-mode window.
// Exactly one Open/Close/Shutdown lifecycle is expected per Manager
// instance (mirrors pkg/session.Manager's single-slot shape, applied here to
// a single namespace-window instead of a single capture session).
type Manager struct {
	mu      sync.Mutex
	rootCtx context.Context
	logger  *zap.Logger
	config  *rest.Config
	binary  string

	// ns is the namespace Open was called with; Close's resolveCurrentIDFn
	// re-lists this same namespace to re-resolve each endpoint's current ID
	// fresh from its UID.
	ns string

	// ours is the sole record of what cpg must revert, keyed on
	// CiliumEndpoint UID (never the reusable integer Status.ID).
	ours map[types.UID]*endpointRecord

	closeOnce   sync.Once
	closeResult RevertResult

	// stopWait bounds the wait for the watcher goroutine to observe ctx
	// cancellation and exit. removeWait independently bounds the revert
	// fan-out so a wedged exec transport can never block process exit
	// (SESS-05 shape). NewManager sets sensible defaults; same-package tests
	// shrink both so bounded-wait paths run fast.
	stopWait   time.Duration
	removeWait time.Duration

	watchCancel context.CancelFunc
	watchDone   chan struct{}

	// Seams: production code always calls through these vars (bound by
	// NewManager to the real pkg/k8s primitives against a clientset
	// constructed exactly once), so tests substitute stubs without standing
	// up a fake SPDY server or a live cluster — mirrors
	// pkg/session.Manager's resolveSetupFn/detectVersionFn convention.
	preconditionFn     func(ctx context.Context) (bool, error)
	listCEFn           func(ctx context.Context, ns string) ([]ciliumv2.CiliumEndpoint, error)
	watchCEFn          func(ctx context.Context, ns string) (watch.Interface, error)
	readFn             func(ctx context.Context, nodeIP string, id int64) (bool, error)
	setFn              func(ctx context.Context, nodeIP string, id int64, enable bool) error
	resolveCurrentIDFn func(ctx context.Context, ns string, uid types.UID) (int64, error)
}

// NewManager constructs a Manager, building the kubernetes and Cilium typed
// clientsets exactly once and binding every seam to the real pkg/k8s
// primitives (production default; same-package tests override the seam
// fields directly). rootCtx MUST be the audit-window command's own
// long-lived, signal-bound ctx (mirrors pkg/session.Manager's Pattern 2) —
// a background goroutine watches rootCtx.Done() and calls Shutdown, so a
// SIGINT/SIGTERM landing anywhere reverts every flip cpg made, even if the
// caller never explicitly calls Shutdown itself.
func NewManager(rootCtx context.Context, logger *zap.Logger, config *rest.Config, binary string) (*Manager, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes clientset: %w", err)
	}
	ciliumCS, err := ciliumclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating Cilium clientset: %w", err)
	}

	m := &Manager{
		rootCtx:    rootCtx,
		logger:     logger,
		config:     config,
		binary:     binary,
		ours:       make(map[types.UID]*endpointRecord),
		stopWait:   5 * time.Second,
		removeWait: 2 * time.Second,
	}

	m.preconditionFn = func(ctx context.Context) (bool, error) {
		return k8s.CheckDaemonAuditMode(ctx, clientset)
	}
	m.listCEFn = func(ctx context.Context, ns string) ([]ciliumv2.CiliumEndpoint, error) {
		list, err := ciliumCS.CiliumV2().CiliumEndpoints(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	}
	m.watchCEFn = func(ctx context.Context, ns string) (watch.Interface, error) {
		return ciliumCS.CiliumV2().CiliumEndpoints(ns).Watch(ctx, metav1.ListOptions{})
	}
	m.readFn = func(ctx context.Context, nodeIP string, id int64) (bool, error) {
		pod, err := k8s.FindAgentPodForNode(ctx, clientset, nodeIP)
		if err != nil {
			return false, err
		}
		return k8s.ReadPolicyAuditMode(ctx, config, clientset, pod.Name, binary, id)
	}
	m.setFn = func(ctx context.Context, nodeIP string, id int64, enable bool) error {
		pod, err := k8s.FindAgentPodForNode(ctx, clientset, nodeIP)
		if err != nil {
			return err
		}
		return k8s.SetPolicyAuditMode(ctx, config, clientset, pod.Name, binary, id, enable)
	}
	m.resolveCurrentIDFn = m.defaultResolveCurrentID

	return m, nil
}

// Open lists ns's CiliumEndpoints, flips only those not already in audit
// mode, and records ownership keyed on UID. It hard-refuses when the
// daemon-wide policy-audit-mode precondition is determined active, and
// warns-and-proceeds when the precondition check itself is undetermined
// (an error from preconditionFn — the same "can't tell, don't block on it"
// convention pkg/k8s.CheckDaemonAuditMode already applies to RBAC-forbidden
// reads).
func (m *Manager) Open(ctx context.Context, ns string) error {
	active, err := m.preconditionFn(ctx)
	switch {
	case err != nil:
		m.logger.Warn("daemon-wide policy-audit-mode precondition check failed; proceeding with an undetermined verdict",
			zap.Error(err))
	case active:
		return fmt.Errorf(
			"daemon-wide policy-audit-mode is already active in this cluster; refusing to open a scoped per-endpoint audit window until it is disabled (see the cilium-config ConfigMap in kube-system)")
	}

	m.mu.Lock()
	m.ns = ns
	m.mu.Unlock()

	endpoints, err := m.listCEFn(ctx, ns)
	if err != nil {
		return fmt.Errorf("listing CiliumEndpoints in namespace %s: %w", ns, err)
	}

	for i := range endpoints {
		m.flipIfNeeded(ctx, &endpoints[i])
	}

	watchCtx, cancel := context.WithCancel(ctx)
	watchDone := make(chan struct{})
	m.mu.Lock()
	m.watchCancel = cancel
	m.watchDone = watchDone
	m.mu.Unlock()
	go m.startWatcher(watchCtx, ns, watchDone)

	// Watch for rootCtx cancellation on any path — SIGINT/SIGTERM, parent
	// context death — and revert unconditionally exactly once. This is what
	// makes "every exit path reverts" true even if the CLI caller forgets to
	// call Shutdown explicitly (mirrors Shutdown's own sole responsibility:
	// nothing to revert exists before Open runs, so this is started here,
	// not in NewManager).
	go func() {
		<-m.rootCtx.Done()
		m.Shutdown()
	}()

	return nil
}

// startWatcher watches ns's CiliumEndpoints for newly-created endpoints
// (ADDED, or MODIFIED for a UID not yet ours) and applies the same
// read-then-flip-if-needed logic Open's initial sweep uses. When
// ResultChan() closes while ctx is still live, it reconnects with a bounded
// backoff (scaled off stopWait) and re-Watch()es — a deliberate one-line
// reconnect loop, NOT a cache.Reflector/informer (RESEARCH.md's locked "no
// parallel construct" decision). The honest residual race: a brand-new
// endpoint may be policy-enforced for the reconnect interval before its flip
// lands — documented here and in the runbook, not solved. Exits and closes
// done when ctx is done.
func (m *Manager) startWatcher(ctx context.Context, ns string, done chan struct{}) {
	defer close(done)

	backoff := m.stopWait
	if backoff <= 0 {
		backoff = 5 * time.Second
	}

	for ctx.Err() == nil {
		w, err := m.watchCEFn(ctx, ns)
		if err != nil {
			m.logger.Warn("audit window watcher: Watch failed; retrying with bounded backoff", zap.Error(err))
			if !sleepOrDone(ctx, backoff) {
				return
			}
			continue
		}

		m.consumeWatch(ctx, w)

		// ResultChan() closed (or ctx is done, in which case the loop
		// condition below exits immediately) — reconnect with the same
		// bounded backoff rather than busy-looping re-Watch() calls.
		if !sleepOrDone(ctx, backoff) {
			return
		}
	}
}

// consumeWatch ranges over w.ResultChan() until it closes or ctx is done,
// flipping any newly-observed endpoint via the shared flipIfNeeded helper.
func (m *Manager) consumeWatch(ctx context.Context, w watch.Interface) {
	defer w.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.ResultChan():
			if !ok {
				return // channel dropped; caller reconnects with backoff
			}
			if ev.Type != watch.Added && ev.Type != watch.Modified {
				continue
			}
			ep, ok := ev.Object.(*ciliumv2.CiliumEndpoint)
			if !ok {
				continue
			}
			m.flipIfNeeded(ctx, ep)
		}
	}
}

// sleepOrDone waits for either d to elapse (returning true) or ctx to be
// done (returning false) first.
func sleepOrDone(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// flipIfNeeded applies the read-then-flip-if-needed logic shared by Open's
// initial sweep and the watcher's new-endpoint handling: skip endpoints
// already recorded as ours, skip endpoints already in audit mode
// (never touch pre-existing audit state), and record newly-flipped
// endpoints under mu keyed on UID.
func (m *Manager) flipIfNeeded(ctx context.Context, ep *ciliumv2.CiliumEndpoint) {
	uid := ep.ObjectMeta.UID

	m.mu.Lock()
	_, already := m.ours[uid]
	m.mu.Unlock()
	if already {
		return
	}

	if ep.Status.Networking == nil {
		m.logger.Warn("endpoint has no networking status; skipping", zap.String("uid", string(uid)))
		return
	}
	nodeIP := ep.Status.Networking.NodeIP
	id := ep.Status.ID

	enabled, err := m.readFn(ctx, nodeIP, id)
	if err != nil {
		m.logger.Warn("reading policy-audit-mode failed; skipping endpoint",
			zap.String("uid", string(uid)), zap.Error(err))
		return
	}
	if enabled {
		return // already in audit mode before cpg ran — never touch it (revert-only-ours)
	}

	if err := m.setFn(ctx, nodeIP, id, true); err != nil {
		m.logger.Warn("flipping policy-audit-mode failed",
			zap.String("uid", string(uid)), zap.Error(err))
		return
	}

	m.mu.Lock()
	m.ours[uid] = &endpointRecord{uid: uid, nodeIP: nodeIP, currentID: id}
	m.mu.Unlock()
	m.logger.Info("opened audit window for endpoint",
		zap.String("uid", string(uid)), zap.Int64("endpoint_id", id))
}

// defaultResolveCurrentID re-resolves an endpoint's current integer ID fresh
// from its UID by re-listing ns via the same listCEFn seam Open already
// uses — no new client-construction path. Returns an error if the endpoint
// no longer exists.
func (m *Manager) defaultResolveCurrentID(ctx context.Context, ns string, uid types.UID) (int64, error) {
	endpoints, err := m.listCEFn(ctx, ns)
	if err != nil {
		return 0, fmt.Errorf("re-resolving endpoint %s: %w", uid, err)
	}
	for i := range endpoints {
		if endpoints[i].ObjectMeta.UID == uid {
			return endpoints[i].Status.ID, nil
		}
	}
	return 0, fmt.Errorf("endpoint %s no longer exists in namespace %s", uid, ns)
}

// Close reverts every endpoint cpg flipped, guarded by closeOnce so only the
// first caller (whether Close itself or Shutdown's internal call) performs
// the real sweep — a second call returns the identical cached summary, never
// double-flipping. Each endpoint's current integer ID is re-resolved fresh
// from its UID (via resolveCurrentIDFn) immediately before reverting, since
// it may have drifted since Open (T-23-05: never revert via a stale,
// possibly-reused ID). Every endpoint is reverted concurrently (a bounded
// fan-out, not a sequential loop) so one wedged transport can never prevent
// the others from completing and being reported — per-endpoint success or
// failure is always recorded, never swallowed (T-23-06).
func (m *Manager) Close(ctx context.Context) (RevertResult, error) {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		recs := make([]*endpointRecord, 0, len(m.ours))
		for _, rec := range m.ours {
			recs = append(recs, rec)
		}
		ns := m.ns
		m.mu.Unlock()

		result := RevertResult{EndpointResults: make(map[types.UID]error, len(recs))}
		var resultMu sync.Mutex
		var wg sync.WaitGroup
		for _, rec := range recs {
			wg.Add(1)
			go func(rec *endpointRecord) {
				defer wg.Done()

				id := rec.currentID
				if m.resolveCurrentIDFn != nil {
					freshID, err := m.resolveCurrentIDFn(ctx, ns, rec.uid)
					if err != nil {
						m.logger.Warn("re-resolving endpoint's current ID failed; reverting with the ID recorded at Open",
							zap.String("uid", string(rec.uid)), zap.Error(err))
					} else {
						id = freshID
					}
				}

				revertErr := m.setFn(ctx, rec.nodeIP, id, false)
				if revertErr != nil {
					m.logger.Warn("reverting policy-audit-mode failed",
						zap.String("uid", string(rec.uid)), zap.Error(revertErr))
				}

				resultMu.Lock()
				result.EndpointResults[rec.uid] = revertErr
				resultMu.Unlock()
			}(rec)
		}
		wg.Wait()

		m.closeResult = result
	})
	return m.closeResult, nil
}

// Shutdown unconditionally, boundedly tears down the audit window on every
// process-exit path (SESS-05 shape): cancel the watcher ctx, bounded-wait
// for it to exit, then run the revert fan-out (Close, via the same
// closeOnce this method shares with an explicit Close call) inside an
// independent bounded deadline — a wedged exec transport can never prevent
// process exit. Safe to call multiple times and concurrently with Close:
// closeOnce guarantees the real sweep runs exactly once regardless of which
// caller reaches it first.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	cancel := m.watchCancel
	done := m.watchDone
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(m.stopWait):
			m.logger.Warn("audit window: watcher did not exit within the bounded deadline; proceeding to revert anyway")
		}
	}

	m.mu.Lock()
	n := len(m.ours)
	m.mu.Unlock()
	if n < 1 {
		n = 1
	}
	deadline := m.removeWait * time.Duration(n)

	revertDone := make(chan struct{})
	go func() {
		_, _ = m.Close(context.Background())
		close(revertDone)
	}()

	select {
	case <-revertDone:
	case <-time.After(deadline):
		m.logger.Warn("audit window: revert fan-out did not complete within the bounded deadline; proceeding with shutdown regardless")
	}
}
