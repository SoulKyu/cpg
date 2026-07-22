package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/auditwindow"
	"github.com/SoulKyu/cpg/pkg/k8s"
)

// auditWindowDefaultTTL is the default, always-applied bound on the audit
// window's lifetime (23-RESEARCH.md locked decision: the window is ALWAYS
// time-bounded, default at planner's discretion — 30m matches CONTEXT.md's
// own example, a reasonable onboarding-session length).
const auditWindowDefaultTTL = 30 * time.Minute

// auditWindowManager is the subset of (*pkg/auditwindow.Manager)'s exported
// surface runAuditWindow depends on, extracted as an interface so tests can
// substitute a stub Manager via the auditWindowNewManager seam below without
// standing up a live cluster (mirrors bootstrapDetectVersion/l7ClientFactory's
// seam convention).
type auditWindowManager interface {
	Open(ctx context.Context, ns string) error
	Close(ctx context.Context) (auditwindow.RevertResult, error)
	Shutdown() auditwindow.RevertResult
}

// auditWindowDetectVersion mirrors bootstrapDetectVersion's best-effort,
// never-error shape exactly (23-RESEARCH.md Assumption A3): kubeconfig load
// -> l7ClientFactory(cfg) -> k8s.DetectCiliumVersion, bounded by the shared
// versionPreflightTimeout (generate.go's constant, reused unmodified). Any
// load/construct failure yields CompatInfo{Source: "undetermined"}. This
// reaches ONLY already-audited read functions — introduces no K8s write verb
// and no filesystem write.
var auditWindowDetectVersion = func(ctx context.Context, logger *zap.Logger) k8s.CompatInfo {
	kubeConfig, err := k8s.LoadKubeConfig()
	if err != nil {
		logger.Warn("audit-window version detection skipped: kubeconfig not available", zap.Error(err))
		return k8s.CompatInfo{Source: "undetermined"}
	}
	client, err := l7ClientFactory(kubeConfig)
	if err != nil {
		logger.Warn("audit-window version detection skipped: failed to construct kubernetes client", zap.Error(err))
		return k8s.CompatInfo{Source: "undetermined"}
	}
	detectCtx, cancel := context.WithTimeout(ctx, versionPreflightTimeout)
	defer cancel()
	return k8s.DetectCiliumVersion(detectCtx, client, logger)
}

// auditWindowNewManager constructs the Manager runAuditWindow drives,
// loading the kubeconfig and binding pkg/auditwindow.NewManager to it.
// Extracted as a package-level seam (mirroring l7ClientFactory /
// bootstrapDetectVersion) so tests substitute a stub auditWindowManager
// without any kubeconfig or live cluster involved at all.
var auditWindowNewManager = func(ctx context.Context, logger *zap.Logger, binary string) (auditWindowManager, error) {
	kubeConfig, err := k8s.LoadKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	return auditwindow.NewManager(ctx, logger, kubeConfig, binary)
}

// newAuditWindowCmd builds the `cpg audit-window` subcommand: a foreground,
// signal-bound, always-TTL-bounded command that is the sole sanctioned
// mutating (pods/exec) surface in cpg (23-RESEARCH.md locked decision,
// Variant B — CLI-only, zero MCP surface change).
func newAuditWindowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit-window",
		Short: "Open a managed, TTL-bounded per-endpoint policy-audit-mode window",
		Long: `Flip PolicyAuditMode to Enabled on every CiliumEndpoint in a namespace,
watch for newly-created endpoints and flip those too, then revert every
flip cpg made on exit — Ctrl+C, SIGTERM, or TTL expiry, whichever comes
first. Runs in the foreground for as long as the window is open: this is
the structural guarantee against a forgotten, indefinitely-open window.

Refuses to open a window if the cluster's daemon-wide policy-audit-mode
setting is already active (see the cilium-config ConfigMap in
kube-system) — disable it first.

Examples:
  # Open a 30-minute (default) audit window in namespace "production"
  cpg audit-window -n production

  # Open a 2-hour window
  cpg audit-window -n production --ttl 2h`,
		Args: cobra.NoArgs,
		RunE: runAuditWindow,
	}
	cmd.Flags().StringP("namespace", "n", "", "target namespace (required)")
	_ = cmd.MarkFlagRequired("namespace")
	cmd.Flags().Duration("ttl", auditWindowDefaultTTL, "maximum window lifetime before automatic revert (always bounded)")
	return cmd
}

// runAuditWindow is cpg's one mutating command's entry point. Every exit
// path — an explicit Ctrl+C/SIGTERM, TTL expiry, or an early validation
// error — funnels into wm.Shutdown() (deferred immediately once wm exists),
// which is sync.Once-guarded in pkg/auditwindow so the real revert sweep
// runs exactly once regardless of how many paths call it.
func runAuditWindow(cmd *cobra.Command, _ []string) error {
	namespace, err := cmd.Flags().GetString("namespace")
	if err != nil {
		return err
	}
	if namespace == "" {
		return fmt.Errorf("--namespace is required")
	}
	if err := validateBootstrapNamespace(namespace); err != nil {
		return err
	}

	ttl, err := cmd.Flags().GetDuration("ttl")
	if err != nil {
		return err
	}
	if ttl <= 0 {
		// The window is ALWAYS time-bounded — an operator passing --ttl 0 (or
		// a negative duration) never gets an unbounded window.
		ttl = auditWindowDefaultTTL
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	compat := auditWindowDetectVersion(ctx, logger)
	binary := k8s.CiliumBinaryName(compat)

	wm, err := auditWindowNewManager(ctx, logger, binary)
	if err != nil {
		return fmt.Errorf("constructing audit-window manager: %w", err)
	}
	// Deferred immediately after wm exists so EVERY exit path below —
	// including a returned error from Open — reverts.
	defer wm.Shutdown()

	if err := wm.Open(ctx, namespace); err != nil {
		return fmt.Errorf("opening audit window: %w", err)
	}

	logger.Info("audit window open",
		zap.String("namespace", namespace),
		zap.Duration("ttl", ttl))

	ttlTimer := time.NewTimer(ttl)
	defer ttlTimer.Stop()

	select {
	case <-ctx.Done():
		logger.Info("audit window: signal received, reverting")
	case <-ttlTimer.C:
		logger.Info("audit window: TTL expired, reverting")
	}

	// Revert through the bounded Shutdown path — NEVER a bare Close under the
	// signal ctx (which is already cancelled on the SIGINT/SIGTERM branch).
	// Shutdown runs the revert sweep under context.Background() with its own
	// internal bounded deadline, so an already-cancelled ctx can never fail
	// the revert for every endpoint (CR-01) and a wedged exec transport can
	// never block process exit (CR-02); it also cancels the watcher before
	// snapshotting, closing the TTL-path snapshot/watcher-flip leak (WR-01).
	result := wm.Shutdown()
	for uid, revertErr := range result.EndpointResults {
		if revertErr != nil {
			logger.Warn("audit window: revert failed for endpoint",
				zap.String("uid", string(uid)), zap.Error(revertErr))
		} else {
			logger.Info("audit window: reverted endpoint", zap.String("uid", string(uid)))
		}
	}
	return nil
}
