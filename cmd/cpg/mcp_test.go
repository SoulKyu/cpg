package main

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realProcessStdout is captured at package-var init, before any test runs —
// other tests in this package legitimately swap the os.Stdout global
// (stdout-purity pipe capture), so comparing against the live global is
// order- and timing-sensitive. The seam contract is against the process's
// REAL stdout, which only this init-time capture reliably names.
//
// Caveat discovered the hard way: under `go test -json` (-test.v=test2json)
// the testing framework ITSELF aliases os.Stderr = os.Stdout inside the
// test process so stderr writes get framed into the JSON event stream.
// In that mode "stderr is not the real stdout" is structurally false for
// reasons unrelated to cpg — the identity sub-assertion below guards on it.
var realProcessStdout = os.Stdout

// TestMCPModeStdoutNeverDefaultsToRealStdout is the D-05 seam-audit unit
// test: it pins mcpModeStdout()'s contract (never nil, never the real
// process stdout, always the current os.Stderr) without driving a live
// session or a PipelineConfig. This stands in for the PipelineConfig.Stdout
// seam that Phase 17 will wire this helper into; diffOut is covered
// structurally instead (MCP mode never sets DryRun: true, so
// pkg/hubble/writer.go's diffOut is dead code this phase — see
// mcpModeStdout's doc comment in mcp.go). Pointer-identity assertions
// (Same/NotSame) are deliberate: DeepEqual over *os.File internals is
// meaningless here, and identity is exactly what the D-01 swap manipulates.
func TestMCPModeStdoutNeverDefaultsToRealStdout(t *testing.T) {
	got := mcpModeStdout()
	assert.NotNil(t, got)
	assert.Same(t, os.Stderr, got, "D-02: MCP-mode human-output seams resolve to stderr")
	if os.Stderr != realProcessStdout {
		// Meaningless under test2json mode, where the framework aliased
		// os.Stderr onto the real stdout before any test ran (see the
		// realProcessStdout doc comment). Production `cpg mcp` never runs
		// under -test.v=test2json, so the guard loses nothing real.
		assert.NotSame(t, realProcessStdout, got, "MCP-mode stdout seam must never resolve to the real process stdout")
	}
}

// TestMCPCobraFlagErrorStaysOffStdout is D-04 scenario 4 / D-03: a cobra
// flag-parse error on `cpg mcp` fails before RunE ever runs, so it never
// reaches the transport/swap logic at all — this test proves SilenceUsage
// (set on the mcp command only) keeps cobra's own usage/error text off the
// real stdout, while cobra's default error printer still reaches stderr
// (SilenceErrors is deliberately not set — see newMCPCmd's doc comment), so
// a supervising MCP host always has a diagnostic to surface.
func TestMCPCobraFlagErrorStaysOffStdout(t *testing.T) {
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	realStdout := os.Stdout
	os.Stdout = outW
	t.Cleanup(func() { os.Stdout = realStdout })

	errR, errW, err := os.Pipe()
	require.NoError(t, err)
	realStderr := os.Stderr
	os.Stderr = errW
	t.Cleanup(func() { os.Stderr = realStderr })

	cmd := newMCPCmd()
	cmd.SetArgs([]string{"--totally-unknown-flag"})
	_ = cmd.Execute() // error expected; assertion is about stdout/stderr, not the error itself

	require.NoError(t, outW.Close())
	leaked, err := io.ReadAll(outR)
	require.NoError(t, err)
	assert.Empty(t, leaked, "D-03: SilenceUsage must keep cobra's own usage text off stdout")

	require.NoError(t, errW.Close())
	diagnostic, err := io.ReadAll(errR)
	require.NoError(t, err)
	assert.NotEmpty(t, diagnostic, "cobra's flag-parse error must still reach stderr so a supervising host has something to log")
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
