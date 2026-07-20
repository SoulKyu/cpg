package main

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPModeStdoutNeverDefaultsToRealStdout is the D-05 seam-audit unit
// test: it pins mcpModeStdout()'s contract (never nil, never os.Stdout,
// always os.Stderr) without driving a live session or a PipelineConfig.
// This stands in for the PipelineConfig.Stdout seam that Phase 17 will wire
// this helper into; diffOut is covered structurally instead (MCP mode
// never sets DryRun: true, so pkg/hubble/writer.go's diffOut is dead code
// this phase — see mcpModeStdout's doc comment in mcp.go).
func TestMCPModeStdoutNeverDefaultsToRealStdout(t *testing.T) {
	got := mcpModeStdout()
	assert.NotNil(t, got)
	assert.NotEqual(t, os.Stdout, got, "MCP-mode stdout seam must never resolve to the real os.Stdout")
	assert.Equal(t, os.Stderr, got, "D-02: MCP-mode human-output seams resolve to stderr")
}

// TestMCPCobraFlagErrorStaysOffStdout is D-04 scenario 4 / D-03: a cobra
// flag-parse error on `cpg mcp` fails before RunE ever runs, so it never
// reaches the transport/swap logic at all — this test proves
// SilenceUsage/SilenceErrors (set on the mcp command only) keep cobra's own
// usage/error text off the real stdout.
func TestMCPCobraFlagErrorStaysOffStdout(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	realStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = realStdout })

	cmd := newMCPCmd()
	cmd.SetArgs([]string{"--totally-unknown-flag"})
	_ = cmd.Execute() // error expected; assertion is about stdout, not the error itself

	require.NoError(t, w.Close())
	leaked, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Empty(t, leaked, "D-03: SilenceUsage/SilenceErrors must keep cobra's own error/usage text off stdout")
}

// TestMCPLoggingBridgesToZapStderr is the SRV-03 bridge test: it proves a
// message logged through bridgedSlogLogger() (the *slog.Logger go-sdk's
// ServerOptions.Logger receives) lands in cpg's existing zap stream.
// buildLogger already pins that stream to stderr in every branch, so
// unified-stderr logging is satisfied by construction once the bridge
// itself is proven here.
func TestMCPLoggingBridgesToZapStderr(t *testing.T) {
	logs := initObservedLoggerForTesting(t)

	l := bridgedSlogLogger()
	l.Info("probe-msg")

	assert.Equal(t, 1, logs.FilterMessage("probe-msg").Len())
}
