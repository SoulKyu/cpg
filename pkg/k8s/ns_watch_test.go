package k8s

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zaptest/observer"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// namespace builds a core/v1 Namespace object with the given name.
func namespace(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

// listErrorReactor returns a reactor that fails every namespaces list with err.
func listErrorReactor(err error) clienttesting.ReactionFunc {
	return func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, err
	}
}

// listForbiddenReactor returns a reactor that produces a 403 Forbidden for a
// namespaces list (mirrors the forbiddenReactor shape in preflight_test.go,
// adapted for list actions, which carry no single object name).
func listForbiddenReactor() clienttesting.ReactionFunc {
	return func(action clienttesting.Action) (bool, runtime.Object, error) {
		gr := schema.GroupResource{Resource: "namespaces"}
		return true, nil, apierrors.NewForbidden(gr, "", errors.New("user lacks namespaces/list permission"))
	}
}

// watchErrorReactor returns a watch reactor that fails the namespaces watch
// construction with err.
func watchErrorReactor(err error) clienttesting.WatchReactionFunc {
	return func(action clienttesting.Action) (bool, watch.Interface, error) {
		return true, nil, err
	}
}

// staticWatchReactor returns a watch reactor that always returns the given
// (pre-created) watch.Interface.
func staticWatchReactor(wi watch.Interface) clienttesting.WatchReactionFunc {
	return func(action clienttesting.Action) (bool, watch.Interface, error) {
		return true, wi, nil
	}
}

// waitForWarns polls until the observed log holds at least want warn entries
// or the deadline expires, then returns the current count. Failure paths that
// fire from the consume goroutine (watch.Error event, channel close) are
// asynchronous relative to StartNamespaceWatcher's return; failure paths taken
// inside StartNamespaceWatcher (list, watch construction) warn synchronously —
// polling covers both uniformly.
func waitForWarns(t *testing.T, logs *observer.ObservedLogs, want int) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countWarnings(logs) >= want {
			return countWarnings(logs)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return countWarnings(logs)
}

// TestNamespaceWatch_RecordsNewOnly proves the List-snapshot safety (T-26-05):
// a fake clientset seeded with `default` and `kube-system` delivers synthetic
// ADDED events for pre-existing namespaces on watch start; only ADDED names
// ABSENT from the snapshot may be recorded (research Pitfall 1).
func TestNamespaceWatch_RecordsNewOnly(t *testing.T) {
	fw := watch.NewFakeWithChanSize(8, false)
	cs := fake.NewSimpleClientset(namespace("default"), namespace("kube-system"))
	cs.PrependWatchReactor("namespaces", staticWatchReactor(fw))
	logger, logs := newObservedLogger()

	w := StartNamespaceWatcher(context.Background(), cs, logger)
	requireNotNilWatcher(t, w)

	fw.Add(namespace("tenant-new"))
	// Synthetic re-delivery of a snapshot namespace as ADDED must NOT be recorded.
	fw.Add(namespace("default"))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(w.NewNamespaces()) < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := countWarnings(logs); got != 0 {
		t.Errorf("healthy recording path must not warn; got %d warnings; entries: %+v", got, logs.All())
	}
	if got, want := w.NewNamespaces(), []string{"tenant-new"}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("NewNamespaces() = %v, want %v (only post-snapshot ADDED names recorded)", got, want)
	}
}

// TestNamespaceWatch_SortedCopy proves deterministic output independent of
// event arrival order, and copy semantics (mutating one returned slice must
// not affect subsequent calls).
func TestNamespaceWatch_SortedCopy(t *testing.T) {
	fw := watch.NewFakeWithChanSize(8, false)
	cs := fake.NewSimpleClientset()
	cs.PrependWatchReactor("namespaces", staticWatchReactor(fw))

	logger, logs := newObservedLogger()
	w := StartNamespaceWatcher(context.Background(), cs, logger)
	requireNotNilWatcher(t, w)

	fw.Add(namespace("zeta"))
	fw.Add(namespace("alpha"))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(w.NewNamespaces()) < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	if len(w.NewNamespaces()) != 2 {
		t.Fatalf("watcher did not record both namespaces; got %v", w.NewNamespaces())
	}
	got := w.NewNamespaces()
	if got[0] != "alpha" || got[1] != "zeta" {
		t.Errorf("NewNamespaces() = %v, want [alpha zeta] (sorted lexicographically regardless of event order)", got)
	}
	first := w.NewNamespaces()
	first[0] = "mutated"
	second := w.NewNamespaces()
	if second[0] != "alpha" || second[1] != "zeta" {
		t.Errorf("NewNamespaces() after mutating a previous copy = %v, want independent copy [alpha zeta]", second)
	}
	if got := countWarnings(logs); got != 0 {
		t.Errorf("healthy recording path must not warn; got %d warnings; entries: %+v", got, logs.All())
	}
}

// TestNamespaceWatchFailSafe is the T-26-03 table: EVERY failure path — List
// error (generic and RBAC 403), Watch construction error, a watch.Error event,
// and result-channel close (apiserver watch expiry) — emits at most ONE
// zap.Warn for the whole watcher lifetime, disables awareness (NewNamespaces()
// stays nil), returns a non-nil inert watcher, and never an error. Mirrors the
// preflight_test.go table shape.
func TestNamespaceWatchFailSafe(t *testing.T) {
	tests := []struct {
		name         string
		setup        func() (kubernetes.Interface, func())
		wantContains []string
	}{
		{
			name: "1_list_forbidden",
			setup: func() (kubernetes.Interface, func()) {
				cs := fake.NewSimpleClientset()
				cs.PrependReactor("list", "namespaces", listForbiddenReactor())
				return cs, func() {}
			},
			wantContains: []string{warnNsWatchListForbidden, "namespaces/list"},
		},
		{
			name: "2_list_generic_error",
			setup: func() (kubernetes.Interface, func()) {
				cs := fake.NewSimpleClientset()
				cs.PrependReactor("list", "namespaces", listErrorReactor(errors.New("etcd unavailable")))
				return cs, func() {}
			},
			wantContains: []string{warnNsWatchListFailed},
		},
		{
			name: "3_watch_construction_error",
			setup: func() (kubernetes.Interface, func()) {
				cs := fake.NewSimpleClientset()
				cs.PrependWatchReactor("namespaces", watchErrorReactor(errors.New("watch refused")))
				return cs, func() {}
			},
			wantContains: []string{warnNsWatchStartFailed, "namespaces/watch"},
		},
		{
			name: "4_watch_error_event",
			setup: func() (kubernetes.Interface, func()) {
				// A watch.Error event followed by channel close shares the
				// single warn-once path — the operator learns awareness
				// stopped exactly once (research Pitfall 8).
				fw := watch.NewFakeWithChanSize(8, false)
				cs := fake.NewSimpleClientset()
				cs.PrependWatchReactor("namespaces", staticWatchReactor(fw))
				return cs, func() {
					fw.Error(&metav1.Status{Status: "Failure"})
					fw.Stop()
				}
			},
			wantContains: []string{warnNsWatchEndedEarly},
		},
		{
			name: "5_channel_close_clean",
			setup: func() (kubernetes.Interface, func()) {
				fw := watch.NewFakeWithChanSize(8, false)
				cs := fake.NewSimpleClientset()
				cs.PrependWatchReactor("namespaces", staticWatchReactor(fw))
				return cs, func() { fw.Stop() }
			},
			wantContains: []string{warnNsWatchEndedEarly},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cs, fire := tc.setup()
			logger, logs := newObservedLogger()

			w := StartNamespaceWatcher(context.Background(), cs, logger)
			requireNotNilWatcher(t, w, "fail-safe watcher is inert but NEVER nil, and never an error")
			fire()

			if got := waitForWarns(t, logs, 1); got != 1 {
				t.Errorf("warning count: got %d, want exactly 1; entries: %+v", got, logs.All())
			}
			// Settle window: prove warn-ONCE (no second warning after the first).
			time.Sleep(20 * time.Millisecond)
			if got := countWarnings(logs); got > 1 {
				t.Errorf("warn-once violated: got %d warnings, want at most 1; entries: %+v", got, logs.All())
			}
			if got := w.NewNamespaces(); got != nil {
				t.Errorf("inert watcher must report no namespaces; got %v", got)
			}
			for _, sub := range tc.wantContains {
				if !containsMessage(logs, sub) {
					t.Errorf("expected a warning containing %q; entries: %+v", sub, logs.All())
				}
			}
		})
	}
}

// TestNamespaceWatch_NilSafeAccessors proves NewNamespaces() is nil-safe on an
// inert (List-failed) watcher AND safe to call concurrently with the live
// watcher goroutine — verified by the -race detector over the whole suite
// (T-26-04: mutex-guarded collection, no channel sends after pipeline g.Wait()).
func TestNamespaceWatch_NilSafeAccessors(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "namespaces", listForbiddenReactor())
	logger, logs := newObservedLogger()

	w := StartNamespaceWatcher(context.Background(), cs, logger)
	requireNotNilWatcher(t, w)

	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_ = w.NewNamespaces() // must never panic, always nil-safe
				}
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(done)
	wg.Wait()

	if got := w.NewNamespaces(); got != nil {
		t.Errorf("inert watcher's NewNamespaces() = %v, want nil", got)
	}
	if got := countWarnings(logs); got != 1 {
		t.Errorf("warning count: got %d, want 1 (list failure path); entries: %+v", got, logs.All())
	}
}

// requireNotNilWatcher asserts w is non-nil (plain-testing helper; the pkg/k8s
// suite avoids testify per its existing convention).
func requireNotNilWatcher(t *testing.T, w *NamespaceWatcher, msgAndArgs ...string) {
	t.Helper()
	if w == nil {
		if len(msgAndArgs) > 0 {
			t.Fatalf("watcher is nil: %s", msgAndArgs[0])
		}
		t.Fatal("watcher is nil")
	}
}
