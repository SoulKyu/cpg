package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"go.uber.org/zap/exp/zapslog"

	"github.com/SoulKyu/cpg/pkg/session"
)

// noopCloseWriter wraps a writer with a no-op Close, mirroring go-sdk's own
// unexported nopCloserWriter (transport.go) used inside StdioTransport.
// Without this, IOTransport's rwc.Close() would really close the real
// stdout fd on session end/ctx-cancel — matching the SDK's own deliberate
// choice to protect stdout (it does NOT protect stdin the same way; os.Stdin
// is passed through directly below, matching StdioTransport's own
// asymmetry).
type noopCloseWriter struct{ io.Writer }

func (noopCloseWriter) Close() error { return nil }

// newMCPCmd builds the `cpg mcp` subcommand: a readonly MCP server over
// stdio. SilenceUsage is set here (D-03) — on this command only, never on
// rootCmd — to suppress cobra's full usage dump on every runtime error,
// which would be noisy for a long-running server. SilenceErrors is
// deliberately NOT set: no production code ever calls cmd.SetOut/SetErr, so
// cobra's own Print*/PrintErrln already fall back to os.Stderr
// (OutOrStderr()/ErrOrStderr()) and never touch the stdout wire — silencing
// it too would delete the only diagnostic text a failed `cpg mcp` produces,
// leaving a supervising MCP host (Claude Desktop, an IDE, etc.) with
// nothing to log on a bad flag or a bad --log-level value.
func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "mcp",
		Short:        "Run cpg as a readonly MCP server over stdio",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Capture the REAL stdin/stdout into the transport's own struct
			// fields BEFORE the global swap below (D-01). Struct-literal
			// field assignment copies the *os.File pointer NOW; the later
			// reassignment of the os.Stdout package variable cannot affect
			// these already-bound fields. Deliberately NOT mcp.StdioTransport{}:
			// that type is a zero-field struct whose Connect() reads the
			// package-level os.Stdout LAZILY (inside server.Run(), after the
			// swap below has already run), which would silently bind the
			// wire to stderr and make the server appear to hang.
			transport := &mcp.IOTransport{
				Reader: os.Stdin,
				Writer: noopCloseWriter{os.Stdout},
			}

			// Global backstop (D-01): any future stray fmt.Print* or
			// third-party write now lands visibly on stderr instead of
			// corrupting the JSON-RPC wire on stdout.
			os.Stdout = os.Stderr

			return runMCPServer(ctx, transport)
		},
	}
}

// runMCPServer builds the MCP server (zapslog-bridged logging, zero tools —
// Phase 17/18 add them) and runs it against transport. Factored out of RunE
// so the stdout-purity test harness can call it directly with an in-memory
// transport, exercising the exact same server-construction and logging
// wiring the real stdio path uses, without touching os.Stdin/os.Stdout at
// all.
func runMCPServer(ctx context.Context, transport mcp.Transport) error {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "cpg", Version: version},
		&mcp.ServerOptions{Logger: bridgedSlogLogger()},
	)

	// Phase 17 registers the 3 session-lifecycle tools (start_session/
	// get_status/stop_session); Phase 18 adds read-side query tools in the
	// same composition-root style. The readonly discipline SEC-01 verifies
	// structurally in Phase 19 continues to hold here: session tools reach
	// only the session tmpdir plus the same K8s read/port-forward verbs
	// generate.go already uses — no K8s write verb is introduced. ctx here
	// MUST be this function's own server-root/signal ctx, never a per-call
	// tool-handler ctx (Pitfall C) — Manager forks every session's
	// background context from it.
	mgr := session.NewManager(ctx, logger, mcpModeStdout(), version)
	registerSessionTools(server, mgr)
	registerQueryTools(server, mgr)
	registerBootstrapTool(server, mgr)

	err := server.Run(ctx, transport)
	// SESS-05: synchronous, bounded cleanup fan-out for BOTH return paths —
	// ctx.Done() (SIGTERM) and the transport session ending (stdin EOF,
	// harness crash). This must run, and be waited on, before runMCPServer
	// itself returns: sessionCtx being a descendant of ctx means SIGTERM
	// *propagates* automatically, but propagation alone does not guarantee
	// the pipeline goroutine has finished unwinding before the process exits.
	mgr.Shutdown()
	return err
}

// bridgedSlogLogger adapts the existing package-level zap logger (already
// stderr-only in every buildLogger branch, see main.go) into a *slog.Logger
// via go.uber.org/zap/exp/zapslog, so go-sdk's internal logs share cpg's one
// unified stderr stream (SRV-03).
//
// Dependency note: zap/exp/zapslog is NOT bundled inside go.uber.org/zap —
// it lives in the independently versioned go.uber.org/zap/exp module
// (go.mod: `go.uber.org/zap/exp v0.3.0`, requiring zap >= v1.26.0). See this
// plan's SUMMARY for the correction record.
func bridgedSlogLogger() *slog.Logger {
	return slog.New(zapslog.NewHandler(logger.Core()))
}

// mcpModeStdout is the MCP-mode human-output seam value (D-02/D-05): never
// nil, never os.Stdout, always os.Stderr. It encodes the contract that
// PipelineConfig.Stdout (pkg/hubble/pipeline.go) must resolve to in MCP
// mode. diffOut (pkg/hubble/writer.go) needs no equivalent wiring this
// phase: it only matters when DryRun == true, and MCP mode never sets
// DryRun: true — Phase 16 registers zero tools and never calls RunPipeline,
// so diffOut is provably dead code this phase. That is the structural
// answer to Open Question #1 in 16-RESEARCH.md: no pkg/hubble change.
//
// Handoff to Phase 17: this helper has no reachable call site yet this
// phase (MCP registers zero tools and never constructs a PipelineConfig),
// so D-02's "explicitly wired to stderr in MCP mode" is only
// contract-pinned here (via TestMCPModeStdoutNeverDefaultsToRealStdout in
// mcp_test.go), not literally connected. Phase 17's session-construction
// code — the first place an MCP-mode PipelineConfig is built, for
// start_session — MUST set PipelineConfig.Stdout = mcpModeStdout() to
// complete this deferred wiring.
func mcpModeStdout() io.Writer {
	return os.Stderr
}
