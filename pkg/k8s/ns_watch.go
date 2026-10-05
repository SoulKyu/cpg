package k8s

import (
	"context"
	"sort"
	"sync"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// Warning copy is intentionally explicit so operators grep'ing logs during
// incident response find a literal, actionable string. Do not paraphrase.
const (
	warnNsWatchListForbidden = "namespace watch: RBAC denied for namespaces/list. " +
		"Skipping namespace-creation awareness for this run; generation proceeds unaffected. " +
		"Required permission: namespaces/list (cluster-scoped)."

	warnNsWatchListFailed = "namespace watch: could not list namespaces to snapshot the pre-run state; " +
		"skipping namespace-creation awareness for this run; generation proceeds unaffected. " +
		"Required permission: namespaces/list (cluster-scoped)."

	warnNsWatchStartForbidden = "namespace watch: RBAC denied for namespaces/watch. " +
		"Skipping namespace-creation awareness for this run; generation proceeds unaffected. " +
		"Required permission: namespaces/watch (cluster-scoped)."

	warnNsWatchStartFailed = "namespace watch: could not start the namespace watch; " +
		"skipping namespace-creation awareness for this run; generation proceeds unaffected. " +
		"Required permission: namespaces/watch (cluster-scoped)."

	warnNsWatchEndedEarly = "namespace watch ended early — new-namespace awareness stopped for this run"
)

// NamespaceWatcher records namespaces that appear after run start (NSAW-01)
// so the end-of-run new-namespace report can list them. Detection is
// List-snapshot + Watch: the snapshot of namespaces present at start makes
// synthetic initial ADDED events harmless (research Pitfall 1 / T-26-05) —
// only ADDED names absent from the snapshot are recorded.
//
// Failures are reported as zap.Warn calls and DO NOT return errors — the
// namespace watch is advisory, exactly like the L7 pre-flight: generation is
// never blocked, degraded, or exited non-zero because of it. RBAC denials
// (apierrors.IsForbidden) are also warned and continued: cluster admins
// running cpg with reduced permissions (CI service accounts) must not be
// locked out. There is no reconnect, relist, or retry — a closed result
// channel (apiserver watch expiry) ends awareness for the run with the single
// ended-early warning (Don't Hand-Roll: reconnect machinery would add failure
// modes to a purely advisory feature).
//
// Contract: invoke ONCE per cpg invocation. At most one warning fires per
// watcher lifetime. Watcher lifetime is bounded by ctx — pass the run's
// signal context; when ctx is cancelled the consume goroutine exits silently
// (a normal run shutdown is not a failure) and Stop has been deferred on the
// underlying watch.
type NamespaceWatcher struct {
	mu sync.Mutex
	// snapshot holds the namespaces present at run start (from List).
	snapshot map[string]struct{}
	// recorded holds post-snapshot ADDED names; guarded by mu because the
	// consume goroutine writes while the pipeline reads after g.Wait()
	// (T-26-04 — no channel sends, so nothing can block shutdown).
	recorded map[string]struct{}
	// warned is the single warn-once guard for the whole watcher lifetime.
	warned bool
	// wi is the underlying watch; kept for Stop symmetry with the deferred
	// Stop inside the consume goroutine.
	wi watch.Interface
}

// StartNamespaceWatcher snapshots the cluster's namespaces, starts a
// namespaces watch, and returns a non-nil watcher — NEVER an error. On a List
// or Watch construction failure it emits exactly one warning (Forbidden gets
// its own branch, mirroring the RunL7Preflight error switch) and returns an
// inert watcher whose NewNamespaces() stays nil for the whole run.
//
// Contract: invoke ONCE per cpg invocation. At most one warning fires per
// watcher lifetime. Watcher lifetime is bounded by ctx.
func StartNamespaceWatcher(ctx context.Context, client kubernetes.Interface, logger *zap.Logger) *NamespaceWatcher {
	w := &NamespaceWatcher{snapshot: map[string]struct{}{}}

	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) {
			w.warnOnce(logger, warnNsWatchListForbidden, zap.Error(err))
		} else {
			w.warnOnce(logger, warnNsWatchListFailed, zap.Error(err))
		}
		return w
	}
	for i := range list.Items {
		if list.Items[i].Name != "" {
			w.snapshot[list.Items[i].Name] = struct{}{}
		}
	}

	wi, err := client.CoreV1().Namespaces().Watch(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) {
			w.warnOnce(logger, warnNsWatchStartForbidden, zap.Error(err))
		} else {
			w.warnOnce(logger, warnNsWatchStartFailed, zap.Error(err))
		}
		return w
	}
	w.wi = wi
	w.recorded = map[string]struct{}{}

	go w.consume(ctx, wi, logger)
	return w
}

// consume drains the watch result channel until it closes (watch expiry or
// ctx cancellation closing the watch) or a watch.Error event arrives. ADDED
// names absent from the start snapshot are recorded; everything else is
// ignored. Exactly one warning fires per watcher lifetime on any failure path.
func (w *NamespaceWatcher) consume(ctx context.Context, wi watch.Interface, logger *zap.Logger) {
	defer wi.Stop()
	for ev := range wi.ResultChan() {
		if ctx.Err() != nil {
			// Run shutdown — normal lifecycle end, not a failure; exit
			// silently so no spurious warning fires at every run end.
			return
		}
		switch ev.Type {
		case watch.Error:
			// Apiserver signalled an error (e.g. expired resourceVersion).
			// Same single warn-once path as channel close: the operator
			// learns awareness stopped, exactly once (research Pitfall 8).
			w.warnOnce(logger, warnNsWatchEndedEarly)
			return
		case watch.Added:
			ns, ok := ev.Object.(*corev1.Namespace)
			if !ok || ns.Name == "" {
				continue
			}
			w.mu.Lock()
			if _, seen := w.snapshot[ns.Name]; !seen {
				w.recorded[ns.Name] = struct{}{}
			}
			w.mu.Unlock()
		default:
			// MODIFIED/DELETED/etc. are irrelevant to creation awareness.
			continue
		}
	}
	// Channel closed without error events: either clean watch termination or
	// apiserver watch expiry mid-run. Warn once so the operator knows the
	// rest of the run is unwatched — but stay silent on a cancelled ctx
	// (normal shutdown above already returned; this is the unexpected-close
	// path with a still-live run context).
	if ctx.Err() == nil {
		w.warnOnce(logger, warnNsWatchEndedEarly)
	}
}

// warnOnce emits msg at most once per watcher lifetime (T-26-03: every
// failure path degrades to a single warning, never repeated noise).
func (w *NamespaceWatcher) warnOnce(logger *zap.Logger, msg string, fields ...zap.Field) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.warned {
		return
	}
	w.warned = true
	logger.Warn(msg, fields...)
}

// NewNamespaces returns a sorted copy of the namespaces recorded as created
// after run start, or nil when nothing was recorded (inert watcher, no
// creations). Nil-safe on inert watchers and safe to call concurrently with
// the consume goroutine (mutex-guarded). The returned slice is an independent
// copy — mutating it does not affect the watcher. Structurally satisfies
// hubble.NewNamespacesSource (no import of pkg/hubble — structural typing).
func (w *NamespaceWatcher) NewNamespaces() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.recorded) == 0 {
		return nil
	}
	out := make([]string, 0, len(w.recorded))
	for ns := range w.recorded {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}
