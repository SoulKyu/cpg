package k8s

import (
	"context"

	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

// Warning copy is intentionally explicit so operators grep'ing logs during
// incident response find a literal, actionable string. Do not paraphrase.
// (TDD RED stub: the warn-once wiring that emits these lands with the GREEN
// implementation.)
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

// NamespaceWatcher records namespaces created after run start (NSAW-01) for
// the end-of-run new-namespace report. RED compile stub — inert placeholder;
// the List-snapshot + Watch implementation lands with the GREEN commit.
type NamespaceWatcher struct{}

// StartNamespaceWatcher RED compile stub: always returns a non-nil inert
// watcher, never an error.
func StartNamespaceWatcher(_ context.Context, _ kubernetes.Interface, _ *zap.Logger) *NamespaceWatcher {
	return &NamespaceWatcher{}
}

// NewNamespaces RED compile stub: inert watchers report nothing.
func (w *NamespaceWatcher) NewNamespaces() []string { return nil }
