package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/k8s"
)

// withFakeBootstrapDetectVersion swaps the package-level bootstrapDetectVersion
// seam for the duration of a test so no live cluster is needed. Restored via
// t.Cleanup.
func withFakeBootstrapDetectVersion(t *testing.T, compat k8s.CompatInfo) {
	t.Helper()
	prev := bootstrapDetectVersion
	bootstrapDetectVersion = func(context.Context, *zap.Logger) k8s.CompatInfo {
		return compat
	}
	t.Cleanup(func() { bootstrapDetectVersion = prev })
}

// newBootstrapTestCmd builds the command with namespace set and stdout
// captured into the returned buffer.
func newBootstrapTestCmd(t *testing.T, namespace string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	cmd := newBootstrapCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(&buf)
	require.NoError(t, cmd.Flags().Set("namespace", namespace))
	return cmd, &buf
}

// TestBootstrapMissingNamespace asserts -n is required before any cluster
// access — cobra's MarkFlagRequired fires on Execute(), so this drives the
// command through Execute() rather than calling runBootstrap directly.
func TestBootstrapMissingNamespace(t *testing.T) {
	initLoggerForTesting(t)

	cmd := newBootstrapCmd()
	cmd.SetArgs([]string{})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
}

// TestBootstrapVersionGate proves the hard-refusal branch: a determined,
// below-floor CompatInfo causes runBootstrap to return an error naming both
// the detected version and the 1.16 floor, and asserts nothing is emitted on
// stdout.
func TestBootstrapVersionGate(t *testing.T) {
	initLoggerForTesting(t)
	withFakeBootstrapDetectVersion(t, k8s.CompatInfo{
		ClusterVersion:     "1.15.0",
		BelowFloorFeatures: []string{"enableDefaultDeny CNP field (requires >= 1.16.0)"},
	})

	cmd, buf := newBootstrapTestCmd(t, "demo")

	err := runBootstrap(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1.15.0")
	assert.Contains(t, err.Error(), "1.16")
	assert.Empty(t, buf.String(), "no artifact should be emitted on hard refusal")
}

// TestBootstrapUndeterminedVersion proves the warn-and-proceed branch: an
// undetermined CompatInfo produces no error, the artifact is still emitted,
// and a warning is observed on the logger.
func TestBootstrapUndeterminedVersion(t *testing.T) {
	logs := initObservedLoggerForTesting(t)
	withFakeBootstrapDetectVersion(t, k8s.CompatInfo{Source: "undetermined"})

	cmd, buf := newBootstrapTestCmd(t, "demo")

	err := runBootstrap(cmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "enableDefaultDeny")

	found := false
	for _, entry := range logs.All() {
		if entry.Level.String() == "warn" {
			found = true
		}
	}
	assert.True(t, found, "expected a warn-level log entry for the undetermined version")
}

// TestBootstrapDeterminedOK proves the silent-proceed branch: a determined
// version at or above every floor produces no warning and the artifact is
// emitted.
func TestBootstrapDeterminedOK(t *testing.T) {
	logs := initObservedLoggerForTesting(t)
	withFakeBootstrapDetectVersion(t, k8s.CompatInfo{ClusterVersion: "1.19.4"})

	cmd, buf := newBootstrapTestCmd(t, "demo")

	err := runBootstrap(cmd, nil)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "enableDefaultDeny")

	for _, entry := range logs.All() {
		assert.NotEqual(t, "warn", entry.Level.String(), "no warning expected when the cluster is at/above every floor")
	}
}
