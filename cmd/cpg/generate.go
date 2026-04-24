package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"k8s.io/client-go/rest"

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

  # Filter to specific namespaces
  %[1]s generate --server relay.example.com:443 --tls -n production -n staging

  # All namespaces with debug logging
  %[1]s --debug generate --server localhost:4245 --all-namespaces`, bin),
		RunE: runGenerate,
	}

	// Connection flags
	cmd.Flags().StringP("server", "s", "", "Hubble Relay address (auto port-forward if omitted)")
	cmd.Flags().BoolP("tls", "", false, "enable TLS for gRPC connection")
	cmd.Flags().Duration("timeout", 10*time.Second, "connection timeout")

	// Namespace filtering
	cmd.Flags().StringSliceP("namespace", "n", nil, "namespace filter (repeatable)")
	cmd.Flags().BoolP("all-namespaces", "A", false, "observe all namespaces")

	// Output
	cmd.Flags().StringP("output-dir", "o", "./policies", "output directory for generated policies")

	// Aggregation
	cmd.Flags().Duration("flush-interval", 5*time.Second, "aggregation flush interval")

	// Dedup
	cmd.Flags().Bool("cluster-dedup", false, "skip policies that already exist in cluster (requires RBAC for CiliumNetworkPolicy list)")

	return cmd
}

// generateFlags holds the parsed flag values for the generate command. It
// exists so flag-level validation (mutual exclusion, target namespace
// resolution) can be unit tested without constructing a cobra.Command.
type generateFlags struct {
	server        string
	namespaces    []string
	allNamespaces bool
	outputDir     string
	tlsEnabled    bool
	flushInterval time.Duration
	timeout       time.Duration
	clusterDedup  bool
}

// validate enforces flag-level invariants. Returns an error when the user
// combined flags that cannot be honored simultaneously.
func (f generateFlags) validate() error {
	if len(f.namespaces) > 0 && f.allNamespaces {
		return fmt.Errorf("--namespace and --all-namespaces are mutually exclusive")
	}
	return nil
}

// clusterDedupNamespaces returns the list of namespaces to query when
// loading cluster policies for dedup. An empty-string element means "list
// across all namespaces" (the k8s client contract).
func (f generateFlags) clusterDedupNamespaces() []string {
	if f.allNamespaces || len(f.namespaces) == 0 {
		return []string{""}
	}
	return f.namespaces
}

func parseGenerateFlags(cmd *cobra.Command) generateFlags {
	f := generateFlags{}
	f.server, _ = cmd.Flags().GetString("server")
	f.namespaces, _ = cmd.Flags().GetStringSlice("namespace")
	f.allNamespaces, _ = cmd.Flags().GetBool("all-namespaces")
	f.outputDir, _ = cmd.Flags().GetString("output-dir")
	f.tlsEnabled, _ = cmd.Flags().GetBool("tls")
	f.flushInterval, _ = cmd.Flags().GetDuration("flush-interval")
	f.timeout, _ = cmd.Flags().GetDuration("timeout")
	f.clusterDedup, _ = cmd.Flags().GetBool("cluster-dedup")
	return f
}

func runGenerate(cmd *cobra.Command, _ []string) error {
	f := parseGenerateFlags(cmd)
	if err := f.validate(); err != nil {
		return err
	}
	server := f.server
	namespaces := f.namespaces
	allNamespaces := f.allNamespaces
	outputDir := f.outputDir
	tlsEnabled := f.tlsEnabled
	flushInterval := f.flushInterval
	timeout := f.timeout
	clusterDedup := f.clusterDedup

	// Setup signal-aware context for graceful shutdown
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Auto port-forward when --server is not provided
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
		zap.Strings("namespaces", namespaces),
		zap.Bool("all-namespaces", allNamespaces),
		zap.String("output-dir", outputDir),
		zap.Bool("tls", tlsEnabled),
		zap.Duration("flush-interval", flushInterval),
		zap.Duration("timeout", timeout),
		zap.Bool("cluster-dedup", clusterDedup),
	)

	// Load cluster policies for dedup if requested
	var clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy
	if clusterDedup {
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

		logger.Info("loaded cluster policies for dedup",
			zap.Int("count", len(clusterPolicies)),
		)
	}

	return hubble.RunPipeline(ctx, hubble.PipelineConfig{
		Server:          server,
		TLSEnabled:      tlsEnabled,
		Timeout:         timeout,
		Namespaces:      namespaces,
		AllNamespaces:   allNamespaces,
		OutputDir:       outputDir,
		FlushInterval:   flushInterval,
		Logger:          logger,
		ClusterPolicies: clusterPolicies,
	})
}
