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

	return nil
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
