package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"k8s.io/client-go/rest"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/hubble"
	"github.com/SoulKyu/cpg/pkg/k8s"
)

func newGenerateCmd() *cobra.Command {
	bin := "cpg"
	if isKubectlPlugin() {
		bin = "kubectl cilium-policy-gen"
	}

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate CiliumNetworkPolicies from Hubble flow observations",
		Long: fmt.Sprintf(`Connect to Hubble Relay via gRPC, stream dropped flows, and generate
CiliumNetworkPolicy YAML files organized by namespace and workload.

When --server is omitted, %[1]s automatically port-forwards to the
hubble-relay service in kube-system using your current kubeconfig.

Runs continuously until interrupted (Ctrl+C). On shutdown, flushes all
accumulated flows and displays a session summary.

Output files are organized as <output-dir>/<namespace>/<workload>.yaml.
When a policy file already exists, new rules are merged into the existing
policy (ports are deduplicated, new peers are appended).

Examples:
  # Generate policies (auto port-forward to hubble-relay)
  %[1]s generate -n production

  # Generate policies from explicit Hubble Relay
  %[1]s generate --server localhost:4245

  # Skip policies that already exist in the cluster
  %[1]s generate --cluster-dedup -n production

  # Dry-run: preview changes without writing
  %[1]s generate -n production --dry-run

  # All namespaces with debug logging
  %[1]s --debug generate --server localhost:4245 --all-namespaces`, bin),
		RunE: runGenerate,
	}

	addCommonFlags(cmd)

	// generate-specific (connection) flags
	cmd.Flags().StringP("server", "s", "", "Hubble Relay address (auto port-forward if omitted)")
	cmd.Flags().BoolP("tls", "", false, "enable TLS for gRPC connection")
	cmd.Flags().Duration("timeout", 10*time.Second, "connection timeout")

	return cmd
}

// generateFlags extends commonFlags with the connection flags specific to live streaming.
type generateFlags struct {
	commonFlags
	server     string
	tlsEnabled bool
	timeout    time.Duration
}

func (f generateFlags) validate() error {
	if len(f.namespaces) > 0 && f.allNamespaces {
		return fmt.Errorf("--namespace and --all-namespaces are mutually exclusive")
	}
	return nil
}

func (f generateFlags) clusterDedupNamespaces() []string {
	if f.allNamespaces || len(f.namespaces) == 0 {
		return []string{""}
	}
	return f.namespaces
}

func parseGenerateFlags(cmd *cobra.Command) generateFlags {
	f := generateFlags{commonFlags: parseCommonFlags(cmd)}
	f.server, _ = cmd.Flags().GetString("server")
	f.tlsEnabled, _ = cmd.Flags().GetBool("tls")
	f.timeout, _ = cmd.Flags().GetDuration("timeout")
	return f
}

func runGenerate(cmd *cobra.Command, _ []string) error {
	f := parseGenerateFlags(cmd)
	if err := f.validate(); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server := f.server
	var kubeConfig *rest.Config
	if server == "" {
		var err error
		kubeConfig, err = k8s.LoadKubeConfig()
		if err != nil {
			return fmt.Errorf("--server not provided and kubeconfig not available: %w", err)
		}

		localAddr, cleanup, err := k8s.PortForwardToRelay(ctx, kubeConfig, logger)
		if err != nil {
			return fmt.Errorf("auto port-forward to hubble-relay failed: %w", err)
		}
		defer cleanup()

		server = localAddr
		logger.Info("auto port-forward established", zap.String("local_addr", localAddr))
	}

	logger.Info("cpg generate configuration",
		zap.String("server", server),
		zap.Strings("namespaces", f.namespaces),
		zap.Bool("all-namespaces", f.allNamespaces),
		zap.String("output-dir", f.outputDir),
		zap.Bool("tls", f.tlsEnabled),
		zap.Duration("flush-interval", f.flushInterval),
		zap.Duration("timeout", f.timeout),
		zap.Bool("cluster-dedup", f.clusterDedup),
		zap.Bool("dry-run", f.dryRun),
		zap.Bool("evidence", !f.noEvidence),
	)

	var clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy
	if f.clusterDedup {
		if kubeConfig == nil {
			var err error
			kubeConfig, err = k8s.LoadKubeConfig()
			if err != nil {
				return fmt.Errorf("--cluster-dedup requires kubeconfig: %w", err)
			}
		}
		var err error
		clusterPolicies, err = k8s.LoadClusterPoliciesForNamespaces(ctx, kubeConfig, f.clusterDedupNamespaces())
		if err != nil {
			return fmt.Errorf("loading cluster policies for dedup: %w", err)
		}
		logger.Info("loaded cluster policies for dedup", zap.Int("count", len(clusterPolicies)))
	}

	absOutDir, err := filepath.Abs(f.outputDir)
	if err != nil {
		absOutDir = f.outputDir
	}

	return hubble.RunPipeline(ctx, hubble.PipelineConfig{
		Server:          server,
		TLSEnabled:      f.tlsEnabled,
		Timeout:         f.timeout,
		Namespaces:      f.namespaces,
		AllNamespaces:   f.allNamespaces,
		OutputDir:       f.outputDir,
		FlushInterval:   f.flushInterval,
		Logger:          logger,
		ClusterPolicies: clusterPolicies,

		DryRun:      f.dryRun,
		DryRunDiff:  !f.dryRunNoDiff,
		DryRunColor: isTerminal(os.Stdout),

		EvidenceEnabled: !f.noEvidence,
		EvidenceDir:     resolveEvidenceDir(f.evidenceDir),
		OutputHash:      evidence.HashOutputDir(absOutDir),
		EvidenceCaps: evidence.MergeCaps{
			MaxSamples:  f.evidenceSamples,
			MaxSessions: f.evidenceSessions,
		},
		SessionID:     fmt.Sprintf("%s-%s", time.Now().UTC().Format(time.RFC3339), uuid.New().String()[:4]),
		SessionSource: evidence.SourceInfo{Type: "live", Server: server},
		CPGVersion:    version,
	})
}

func resolveEvidenceDir(override string) string {
	if override != "" {
		return override
	}
	dir, err := evidence.DefaultEvidenceDir()
	if err != nil {
		logger.Warn("falling back to in-repo .cpg-evidence", zap.Error(err))
		return ".cpg-evidence"
	}
	return dir
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
