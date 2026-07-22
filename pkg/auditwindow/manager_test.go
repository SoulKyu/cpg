package auditwindow

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
)

// errBoom is a stable sentinel error for tests asserting on per-endpoint
// revert failure.
var errBoom = errors.New("boom")

// newTestManager builds a Manager against a minimal (never-dialed) rest.Config
// — NewManager only constructs clientsets, it never contacts the API server —
// with bounded deadlines shrunk to run fast, and every seam defaulted to a
// no-op stub the test then overrides as needed. Mirrors
// pkg/session/manager_test.go's newTestManager convention.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return newTestManagerCtx(t, context.Background())
}

// newTestManagerCtx is newTestManager's rootCtx-parameterized sibling, for
// tests that need to cancel rootCtx themselves (TestManager_Shutdown_OnCtxCancel)
// rather than always binding to context.Background().
func newTestManagerCtx(t *testing.T, rootCtx context.Context) *Manager {
	t.Helper()
	m, err := NewManager(rootCtx, zaptest.NewLogger(t), &rest.Config{Host: "https://127.0.0.1:6443"}, "cilium-dbg")
	require.NoError(t, err)
	m.stopWait = 50 * time.Millisecond
	m.removeWait = 50 * time.Millisecond
	m.preconditionFn = func(context.Context) (bool, error) { return false, nil }
	m.listCEFn = func(context.Context, string) ([]ciliumv2.CiliumEndpoint, error) { return nil, nil }
	// Default watchCEFn to a fake watch.Interface whose ResultChan() never
	// emits or closes on its own: without this, Open's watcher goroutine
	// would call through to NewManager's real production watchCEFn (bound
	// against the never-dialed fake rest.Config), dialing 127.0.0.1:6443
	// repeatedly on a bounded-backoff loop for the remainder of the test
	// binary's life in every test that doesn't care about the watcher at
	// all (Task 1/2 tests) — including logging via zaptest.NewLogger(t)
	// after the individual test has already completed.
	m.watchCEFn = func(context.Context, string) (watch.Interface, error) {
		return newFakeWatch(), nil
	}
	return m
}

// fakeWatch is a minimal watch.Interface stand-in: ResultChan() is a plain
// unbuffered channel the test writes watch.Event values to directly (or
// closes, to simulate a dropped connection); Stop() is a no-op (nothing to
// release — the test owns ch's lifecycle directly).
type fakeWatch struct {
	ch chan watch.Event
}

func newFakeWatch() *fakeWatch { return &fakeWatch{ch: make(chan watch.Event)} }

func (f *fakeWatch) Stop()                          {}
func (f *fakeWatch) ResultChan() <-chan watch.Event { return f.ch }

// fakeEndpoint builds a minimal CiliumEndpoint with the fields Open/the
// watcher actually read: UID, node IP, and the per-node integer ID.
func fakeEndpoint(uid types.UID, nodeIP string, id int64) ciliumv2.CiliumEndpoint {
	return ciliumv2.CiliumEndpoint{
		ObjectMeta: metav1.ObjectMeta{UID: uid},
		Status: ciliumv2.EndpointStatus{
			ID:         id,
			Networking: &ciliumv2.EndpointNetworking{NodeIP: nodeIP},
		},
	}
}

// TestManager_Open_RefusesWhenDaemonAuditActive proves the hard-refusal half
// of the daemon-wide precondition (AUD-03): a determined-active verdict
// blocks Open entirely — no flip seam is ever called.
func TestManager_Open_RefusesWhenDaemonAuditActive(t *testing.T) {
	m := newTestManager(t)
	m.preconditionFn = func(context.Context) (bool, error) { return true, nil }

	setCalled := false
	m.setFn = func(context.Context, string, int64, bool) error {
		setCalled = true
		return nil
	}
	m.listCEFn = func(context.Context, string) ([]ciliumv2.CiliumEndpoint, error) {
		return []ciliumv2.CiliumEndpoint{fakeEndpoint("uid-1", "10.0.0.1", 5)}, nil
	}

	err := m.Open(context.Background(), "test-ns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daemon-wide policy-audit-mode is already active")
	assert.False(t, setCalled, "no flip seam may be called on a hard refusal")
}

// TestManager_Open_ProceedsWhenPreconditionUndetermined proves the
// warn-and-proceed half: an RBAC-denied read collapses to preconditionFn
// returning (false, nil) (the same convention k8s.CheckDaemonAuditMode
// already applies) — Open must still proceed and flip.
func TestManager_Open_ProceedsWhenPreconditionUndetermined(t *testing.T) {
	m := newTestManager(t)
	m.preconditionFn = func(context.Context) (bool, error) { return false, nil } // RBAC-denied collapsed by the seam
	m.readFn = func(context.Context, string, int64) (bool, error) { return false, nil }

	var flipped []int64
	m.setFn = func(_ context.Context, _ string, id int64, enable bool) error {
		require.True(t, enable)
		flipped = append(flipped, id)
		return nil
	}
	m.listCEFn = func(context.Context, string) ([]ciliumv2.CiliumEndpoint, error) {
		return []ciliumv2.CiliumEndpoint{fakeEndpoint("uid-1", "10.0.0.1", 5)}, nil
	}

	err := m.Open(context.Background(), "test-ns")
	require.NoError(t, err)
	assert.Equal(t, []int64{5}, flipped, "flips must proceed on an undetermined precondition")
}

// TestManager_Open_SkipsAlreadyAuditedEndpoint proves the revert-only-ours
// half: an endpoint already reporting PolicyAuditMode enabled must never be
// flipped or recorded, while a not-yet-audited sibling endpoint is.
func TestManager_Open_SkipsAlreadyAuditedEndpoint(t *testing.T) {
	m := newTestManager(t)

	const enabledUID types.UID = "uid-already-enabled"
	const freshUID types.UID = "uid-needs-flip"

	m.listCEFn = func(context.Context, string) ([]ciliumv2.CiliumEndpoint, error) {
		return []ciliumv2.CiliumEndpoint{
			fakeEndpoint(enabledUID, "10.0.0.1", 1),
			fakeEndpoint(freshUID, "10.0.0.2", 2),
		}, nil
	}
	m.readFn = func(_ context.Context, _ string, id int64) (bool, error) {
		return id == 1, nil // endpoint 1 already Enabled
	}
	var setCalls []int64
	m.setFn = func(_ context.Context, _ string, id int64, _ bool) error {
		setCalls = append(setCalls, id)
		return nil
	}

	err := m.Open(context.Background(), "test-ns")
	require.NoError(t, err)

	assert.Equal(t, []int64{2}, setCalls, "setFn must be called only for the not-yet-audited endpoint")

	m.mu.Lock()
	defer m.mu.Unlock()
	_, hasEnabled := m.ours[enabledUID]
	_, hasFresh := m.ours[freshUID]
	assert.False(t, hasEnabled, "the already-audited endpoint must never enter the ours map")
	assert.True(t, hasFresh, "the flipped endpoint must enter the ours map")
}

// TestManager_Close proves Close reverts every endpoint cpg flipped,
// returns a nil-error RevertResult per endpoint, and is idempotent on a
// second call (same summary, no double-flip).
func TestManager_Close(t *testing.T) {
	m := newTestManager(t)
	m.readFn = func(context.Context, string, int64) (bool, error) { return false, nil }
	m.listCEFn = func(context.Context, string) ([]ciliumv2.CiliumEndpoint, error) {
		return []ciliumv2.CiliumEndpoint{
			fakeEndpoint("uid-a", "10.0.0.1", 1),
			fakeEndpoint("uid-b", "10.0.0.2", 2),
		}, nil
	}

	var mu sync.Mutex
	var disableCalls []int64
	m.setFn = func(_ context.Context, _ string, id int64, enable bool) error {
		if !enable {
			mu.Lock()
			disableCalls = append(disableCalls, id)
			mu.Unlock()
		}
		return nil
	}

	require.NoError(t, m.Open(context.Background(), "test-ns"))

	result, err := m.Close(context.Background())
	require.NoError(t, err)
	require.Len(t, result.EndpointResults, 2)
	for uid, revertErr := range result.EndpointResults {
		assert.NoError(t, revertErr, "uid %s", uid)
	}
	assert.ElementsMatch(t, []int64{1, 2}, disableCalls)

	second, err := m.Close(context.Background())
	require.NoError(t, err)
	assert.Equal(t, result, second, "a second Close must return the identical cached summary")
	mu.Lock()
	assert.Len(t, disableCalls, 2, "a second Close must not re-flip")
	mu.Unlock()
}

// TestManager_Close_UsesUIDNotReusedEndpointID proves Close re-resolves each
// endpoint's current integer ID fresh from its UID (via resolveCurrentIDFn)
// immediately before reverting, rather than trusting the ID recorded at Open
// time, which may have drifted (T-23-05).
func TestManager_Close_UsesUIDNotReusedEndpointID(t *testing.T) {
	m := newTestManager(t)
	m.readFn = func(context.Context, string, int64) (bool, error) { return false, nil }
	m.listCEFn = func(context.Context, string) ([]ciliumv2.CiliumEndpoint, error) {
		return []ciliumv2.CiliumEndpoint{fakeEndpoint("uid-x", "10.0.0.1", 5)}, nil
	}
	m.setFn = func(context.Context, string, int64, bool) error { return nil }

	require.NoError(t, m.Open(context.Background(), "test-ns"))

	var gotID int64
	m.setFn = func(_ context.Context, _ string, id int64, enable bool) error {
		if !enable {
			gotID = id
		}
		return nil
	}
	// Simulate the ID having drifted since Open (e.g. agent restart) — the
	// same UID now resolves to a different current integer ID.
	m.resolveCurrentIDFn = func(context.Context, string, types.UID) (int64, error) {
		return 99, nil
	}

	_, err := m.Close(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(99), gotID, "revert must target the freshly-resolved ID, not the stale one recorded at Open")
}

// TestManager_Close_ReportsPerEndpointResult proves a single endpoint's
// revert failure is reported by UID in RevertResult while sibling endpoints
// still succeed — a sweep that cannot name its failing endpoint is a
// non-starter (T-23-06).
func TestManager_Close_ReportsPerEndpointResult(t *testing.T) {
	m := newTestManager(t)
	m.readFn = func(context.Context, string, int64) (bool, error) { return false, nil }
	m.listCEFn = func(context.Context, string) ([]ciliumv2.CiliumEndpoint, error) {
		return []ciliumv2.CiliumEndpoint{
			fakeEndpoint("uid-ok", "10.0.0.1", 1),
			fakeEndpoint("uid-fail", "10.0.0.2", 2),
		}, nil
	}
	m.setFn = func(context.Context, string, int64, bool) error { return nil }

	require.NoError(t, m.Open(context.Background(), "test-ns"))

	m.setFn = func(_ context.Context, _ string, id int64, enable bool) error {
		if !enable && id == 2 {
			return errBoom
		}
		return nil
	}

	result, err := m.Close(context.Background())
	require.NoError(t, err)
	require.Len(t, result.EndpointResults, 2)
	assert.NoError(t, result.EndpointResults[types.UID("uid-ok")])
	assert.ErrorIs(t, result.EndpointResults[types.UID("uid-fail")], errBoom)
}

// TestManager_Shutdown_OnCtxCancel proves cancelling rootCtx alone (with no
// explicit Close/Shutdown call from the caller) triggers the same
// closeOnce-guarded revert path exactly once, and that the effect is
// observable promptly.
func TestManager_Shutdown_OnCtxCancel(t *testing.T) {
	rootCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := newTestManagerCtx(t, rootCtx)
	m.readFn = func(context.Context, string, int64) (bool, error) { return false, nil }
	m.listCEFn = func(context.Context, string) ([]ciliumv2.CiliumEndpoint, error) {
		return []ciliumv2.CiliumEndpoint{fakeEndpoint("uid-1", "10.0.0.1", 5)}, nil
	}

	var mu sync.Mutex
	var disableCalls []int64
	m.setFn = func(_ context.Context, _ string, id int64, enable bool) error {
		if !enable {
			mu.Lock()
			disableCalls = append(disableCalls, id)
			mu.Unlock()
		}
		return nil
	}

	require.NoError(t, m.Open(rootCtx, "test-ns"))

	cancel()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(disableCalls) == 1
	}, 4*(m.stopWait+m.removeWait), 5*time.Millisecond,
		"cancelling rootCtx must trigger exactly one revert sweep promptly")

	// A subsequent explicit Close must be idempotent with the auto-triggered
	// one — closeOnce guards both callers identically.
	result, err := m.Close(context.Background())
	require.NoError(t, err)
	assert.Len(t, result.EndpointResults, 1)
	mu.Lock()
	assert.Len(t, disableCalls, 1, "closeOnce must prevent a double revert")
	mu.Unlock()
}

// TestManager_Shutdown_RevertsUnderNonCancelledContext proves the CR-01 fix:
// even when rootCtx is cancelled (a SIGINT/SIGTERM landing), the revert sweep
// runs under a fresh, non-cancelled context — every setFn revert call observes
// ctx.Err() == nil — so the revert cannot fail with context.Canceled for every
// endpoint the way a bare Close(cancelledCtx) would.
func TestManager_Shutdown_RevertsUnderNonCancelledContext(t *testing.T) {
	rootCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := newTestManagerCtx(t, rootCtx)
	m.readFn = func(context.Context, string, int64) (bool, error) { return false, nil }
	m.listCEFn = func(context.Context, string) ([]ciliumv2.CiliumEndpoint, error) {
		return []ciliumv2.CiliumEndpoint{fakeEndpoint("uid-1", "10.0.0.1", 5)}, nil
	}

	reverted := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var revertCtxErr error
	m.setFn = func(ctx context.Context, _ string, _ int64, enable bool) error {
		if !enable {
			mu.Lock()
			revertCtxErr = ctx.Err()
			mu.Unlock()
			once.Do(func() { close(reverted) })
		}
		return nil
	}

	require.NoError(t, m.Open(rootCtx, "test-ns"))

	cancel() // simulate SIGTERM: the signal-bound rootCtx is now Done

	select {
	case <-reverted:
	case <-time.After(4 * (m.stopWait + m.removeWait)):
		t.Fatal("revert never ran after rootCtx cancellation")
	}

	mu.Lock()
	defer mu.Unlock()
	require.NoError(t, revertCtxErr,
		"revert must run under a fresh, non-cancelled context even though rootCtx was cancelled (CR-01)")
}

// TestManager_Shutdown_RevertsWatcherFlippedEndpoint proves the WR-01 fix: an
// endpoint the watcher flips is reverted by Shutdown. Because Shutdown cancels
// and drains the watcher BEFORE Close snapshots m.ours, no endpoint the watcher
// recorded can escape the revert sweep — the snapshot/watcher-flip leak the
// direct-Close path had is closed.
func TestManager_Shutdown_RevertsWatcherFlippedEndpoint(t *testing.T) {
	m := newTestManager(t)
	m.readFn = func(context.Context, string, int64) (bool, error) { return false, nil }

	var mu sync.Mutex
	var disableCalls []int64
	m.setFn = func(_ context.Context, _ string, id int64, enable bool) error {
		if !enable {
			mu.Lock()
			disableCalls = append(disableCalls, id)
			mu.Unlock()
		}
		return nil
	}

	fw := newFakeWatch()
	m.watchCEFn = func(context.Context, string) (watch.Interface, error) { return fw, nil }

	require.NoError(t, m.Open(context.Background(), "test-ns"))

	newEP := fakeEndpoint("uid-watched", "10.0.0.9", 42)
	fw.ch <- watch.Event{Type: watch.Added, Object: &newEP}

	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		_, ok := m.ours[types.UID("uid-watched")]
		return ok
	}, 2*time.Second, 5*time.Millisecond, "watcher must record the flipped endpoint before Shutdown")

	result := m.Shutdown()
	require.Contains(t, result.EndpointResults, types.UID("uid-watched"),
		"a watcher-flipped endpoint must be reverted by Shutdown (WR-01)")
	assert.NoError(t, result.EndpointResults[types.UID("uid-watched")])

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, disableCalls, int64(42), "the watcher-flipped endpoint must be disabled on revert")
}

// TestManager_Shutdown_WedgedExecDoesNotBlock proves the bounded revert
// fan-out: even when setFn never observes ctx cancellation, Shutdown still
// returns within a small multiple of stopWait+removeWait, logging a
// warning rather than blocking process exit forever.
func TestManager_Shutdown_WedgedExecDoesNotBlock(t *testing.T) {
	m := newTestManager(t)
	m.readFn = func(context.Context, string, int64) (bool, error) { return false, nil }
	m.listCEFn = func(context.Context, string) ([]ciliumv2.CiliumEndpoint, error) {
		return []ciliumv2.CiliumEndpoint{fakeEndpoint("uid-1", "10.0.0.1", 5)}, nil
	}
	m.setFn = func(context.Context, string, int64, bool) error { return nil } // initial enable flip in Open

	require.NoError(t, m.Open(context.Background(), "test-ns"))

	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	m.setFn = func(_ context.Context, _ string, _ int64, enable bool) error {
		if !enable {
			<-release // wedged: never observes ctx cancellation
		}
		return nil
	}

	done := make(chan struct{})
	go func() {
		m.Shutdown()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(4 * (m.stopWait + m.removeWait)):
		t.Fatal("Shutdown did not return within the bounded deadline despite a wedged setFn")
	}
}

// TestManager_Watcher_FlipsNewEndpoint proves the watcher applies the same
// read-then-flip-if-needed logic Open's initial sweep uses to a
// newly-observed (ADDED) endpoint, recording its UID in ours.
func TestManager_Watcher_FlipsNewEndpoint(t *testing.T) {
	m := newTestManager(t)
	m.readFn = func(context.Context, string, int64) (bool, error) { return false, nil }

	var mu sync.Mutex
	var enableCalls []int64
	m.setFn = func(_ context.Context, _ string, id int64, enable bool) error {
		if enable {
			mu.Lock()
			enableCalls = append(enableCalls, id)
			mu.Unlock()
		}
		return nil
	}

	fw := newFakeWatch()
	m.watchCEFn = func(context.Context, string) (watch.Interface, error) { return fw, nil }

	require.NoError(t, m.Open(context.Background(), "test-ns"))
	t.Cleanup(func() { m.Shutdown() })

	newEP := fakeEndpoint("uid-new", "10.0.0.9", 42)
	fw.ch <- watch.Event{Type: watch.Added, Object: &newEP}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, id := range enableCalls {
			if id == 42 {
				return true
			}
		}
		return false
	}, 2*time.Second, 5*time.Millisecond, "watcher must flip the newly-observed endpoint")

	m.mu.Lock()
	_, recorded := m.ours[types.UID("uid-new")]
	m.mu.Unlock()
	assert.True(t, recorded, "the new endpoint's UID must be recorded in ours")
}

// TestManager_Watcher_ReconnectsOnChannelClose proves that when the first
// fake watch's ResultChan() closes while ctx is live, the watcher re-Watch()es
// (bounded backoff) and a subsequent ADDED event from the reconnected watch
// is still flipped; Shutdown then exits the loop and returns promptly.
func TestManager_Watcher_ReconnectsOnChannelClose(t *testing.T) {
	m := newTestManager(t)
	m.readFn = func(context.Context, string, int64) (bool, error) { return false, nil }

	var mu sync.Mutex
	var enableCalls []int64
	m.setFn = func(_ context.Context, _ string, id int64, enable bool) error {
		if enable {
			mu.Lock()
			enableCalls = append(enableCalls, id)
			mu.Unlock()
		}
		return nil
	}

	firstWatch := newFakeWatch()
	secondWatch := newFakeWatch()
	var watchCallCount int32
	m.watchCEFn = func(context.Context, string) (watch.Interface, error) {
		if atomic.AddInt32(&watchCallCount, 1) == 1 {
			return firstWatch, nil
		}
		return secondWatch, nil
	}

	require.NoError(t, m.Open(context.Background(), "test-ns"))

	close(firstWatch.ch) // simulate a dropped watch connection

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&watchCallCount) >= 2
	}, 2*time.Second, 5*time.Millisecond, "watcher must re-Watch() after ResultChan() closes while ctx is live")

	newEP := fakeEndpoint("uid-second", "10.0.0.10", 7)
	secondWatch.ch <- watch.Event{Type: watch.Added, Object: &newEP}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, id := range enableCalls {
			if id == 7 {
				return true
			}
		}
		return false
	}, 2*time.Second, 5*time.Millisecond, "the reconnected watch's ADDED event must still be flipped")

	begin := time.Now()
	m.Shutdown()
	assert.Less(t, time.Since(begin), 4*(m.stopWait+m.removeWait), "Shutdown must exit the watcher loop promptly once ctx is done")
}
