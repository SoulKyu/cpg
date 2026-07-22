package main

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/auditwindow"
	"github.com/SoulKyu/cpg/pkg/k8s"
)

// stubAuditWindowManager is a minimal auditWindowManager stand-in. It
// mirrors pkg/auditwindow.Manager's own sync.Once-guarded revert contract
// (revertOnce) so tests can assert "the revert path was invoked exactly
// once" regardless of whether Close, Shutdown, or both are called along a
// given exit path — exactly the guarantee the real Manager provides.
type stubAuditWindowManager struct {
	openErr     error
	closeErr    error
	closeResult auditwindow.RevertResult

	mu          sync.Mutex
	revertOnce  sync.Once
	revertCount int
	openCalled  bool
}

func (s *stubAuditWindowManager) Open(_ context.Context, _ string) error {
	s.mu.Lock()
	s.openCalled = true
	s.mu.Unlock()
	return s.openErr
}

func (s *stubAuditWindowManager) Close(_ context.Context) (auditwindow.RevertResult, error) {
	s.revertOnce.Do(func() {
		s.mu.Lock()
		s.revertCount++
		s.mu.Unlock()
	})
	return s.closeResult, s.closeErr
}

func (s *stubAuditWindowManager) Shutdown() {
	s.revertOnce.Do(func() {
		s.mu.Lock()
		s.revertCount++
		s.mu.Unlock()
	})
}

func (s *stubAuditWindowManager) reverts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revertCount
}

func (s *stubAuditWindowManager) wasOpened() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.openCalled
}

// withFakeAuditWindowSeams swaps both the version-detection and
// Manager-construction seams for the duration of a test so no live cluster
// or kubeconfig is ever required. Restored via t.Cleanup.
func withFakeAuditWindowSeams(t *testing.T, stub *stubAuditWindowManager) {
	t.Helper()
	prevDetect := auditWindowDetectVersion
	prevNewManager := auditWindowNewManager
	auditWindowDetectVersion = func(context.Context, *zap.Logger) k8s.CompatInfo {
		return k8s.CompatInfo{Source: "undetermined"}
	}
	auditWindowNewManager = func(context.Context, *zap.Logger, string) (auditWindowManager, error) {
		return stub, nil
	}
	t.Cleanup(func() {
		auditWindowDetectVersion = prevDetect
		auditWindowNewManager = prevNewManager
	})
}

// TestAuditWindow_RequiresNamespace asserts -n is required before any
// cluster access — cobra's MarkFlagRequired fires on Execute(), so this
// drives the command through Execute() rather than calling runAuditWindow
// directly (mirrors TestBootstrapMissingNamespace).
func TestAuditWindow_RequiresNamespace(t *testing.T) {
	initLoggerForTesting(t)

	cmd := newAuditWindowCmd()
	cmd.SetArgs([]string{})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
}

// TestAuditWindow_ValidatesNamespace asserts an invalid DNS-1123 namespace is
// rejected before any cluster call — the version-detection and
// Manager-construction seams must never be reached.
func TestAuditWindow_ValidatesNamespace(t *testing.T) {
	initLoggerForTesting(t)

	stub := &stubAuditWindowManager{}
	withFakeAuditWindowSeams(t, stub)

	for _, ns := range []string{"Foo Bar", "../etc", "UPPER", "trailing-"} {
		cmd := newAuditWindowCmd()
		require.NoError(t, cmd.Flags().Set("namespace", ns))
		cmd.SetContext(context.Background())

		err := runAuditWindow(cmd, nil)
		require.Error(t, err, "namespace %q must be rejected", ns)
		assert.Contains(t, err.Error(), "invalid namespace")
		assert.False(t, stub.wasOpened(), "namespace %q must be rejected before any cluster call", ns)
	}
}

// TestAuditWindow_DefaultTTLApplies asserts the effective TTL (when --ttl is
// omitted) equals the documented default — the window is ALWAYS
// time-bounded, never left to an implicit/unbounded zero value.
func TestAuditWindow_DefaultTTLApplies(t *testing.T) {
	cmd := newAuditWindowCmd()
	f := cmd.Flags().Lookup("ttl")
	require.NotNil(t, f, "expected a --ttl flag")
	assert.Equal(t, auditWindowDefaultTTL.String(), f.DefValue)

	ttl, err := cmd.Flags().GetDuration("ttl")
	require.NoError(t, err)
	assert.Equal(t, auditWindowDefaultTTL, ttl)
}

// TestAuditWindow_TTLExpiryTriggersRevert proves the TTL-expiry exit path:
// with a stubbed Manager and a short TTL, the timer fires, the revert path
// (Manager.Close/Shutdown) is invoked exactly once (revertOnce-guarded, same
// contract the real Manager provides), and the command returns nil.
func TestAuditWindow_TTLExpiryTriggersRevert(t *testing.T) {
	initLoggerForTesting(t)

	stub := &stubAuditWindowManager{}
	withFakeAuditWindowSeams(t, stub)

	cmd := newAuditWindowCmd()
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"-n", "test-ns", "--ttl", "5ms"})

	err := cmd.Execute()
	require.NoError(t, err)

	assert.True(t, stub.wasOpened())
	assert.Equal(t, 1, stub.reverts(), "expected the revert path to run exactly once on TTL expiry")
}
