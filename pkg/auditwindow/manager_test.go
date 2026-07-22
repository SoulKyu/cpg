package auditwindow

import (
	"context"
	"testing"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

// newTestManager builds a Manager against a minimal (never-dialed) rest.Config
// — NewManager only constructs clientsets, it never contacts the API server —
// with bounded deadlines shrunk to run fast, and every seam defaulted to a
// no-op stub the test then overrides as needed. Mirrors
// pkg/session/manager_test.go's newTestManager convention.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(context.Background(), zaptest.NewLogger(t), &rest.Config{Host: "https://127.0.0.1:6443"}, "cilium-dbg")
	require.NoError(t, err)
	m.stopWait = 50 * time.Millisecond
	m.removeWait = 50 * time.Millisecond
	m.preconditionFn = func(context.Context) (bool, error) { return false, nil }
	m.listCEFn = func(context.Context, string) ([]ciliumv2.CiliumEndpoint, error) { return nil, nil }
	return m
}

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
