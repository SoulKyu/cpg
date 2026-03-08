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

	"github.com/gule/cpg/pkg/hubble"
	"github.com/gule/cpg/pkg/k8s"
)

func newGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate CiliumNetworkPolicies from Hubble flow observations",
		Long: `Connect to Hubble Relay via gRPC, stream dropped flows, and generate
CiliumNetworkPolicy YAML files organized by namespace and workload.

When --server is omitted, cpg automatically port-forwards to the
hubble-relay service in kube-system using your current kubeconfig.

Runs continuously until interrupted (Ctrl+C). On shutdown, flushes all
accumulated flows and displays a session summary.

Output files are organized as <output-dir>/<namespace>/<workload>.yaml.
When a policy file already exists, new rules are merged into the existing
policy (ports are deduplicated, new peers are appended).

Examples:
  # Generate policies (auto port-forward to hubble-relay)
  cpg generate -n production

  # Generate policies from explicit Hubble Relay
  cpg generate --server localhost:4245

  # Skip policies that already exist in the cluster
  cpg generate --cluster-dedup -n production

  # Filter to specific namespaces
  cpg generate --server relay.example.com:443 --tls -n production -n staging

  # All namespaces with debug logging
  cpg --debug generate --server localhost:4245 --all-namespaces`,
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

func runGenerate(cmd *cobra.Command, _ []string) error {
	server, _ := cmd.Flags().GetString("server")
	namespaces, _ := cmd.Flags().GetStringSlice("namespace")
	allNamespaces, _ := cmd.Flags().GetBool("all-namespaces")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	tlsEnabled, _ := cmd.Flags().GetBool("tls")
	flushInterval, _ := cmd.Flags().GetDuration("flush-interval")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	clusterDedup, _ := cmd.Flags().GetBool("cluster-dedup")

	// Validate mutually exclusive flags
	if len(namespaces) > 0 && allNamespaces {
		return fmt.Errorf("--namespace and --all-namespaces are mutually exclusive")
	}

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

		clusterPolicies = make(map[string]*ciliumv2.CiliumNetworkPolicy)
		targetNamespaces := namespaces
		if allNamespaces || len(targetNamespaces) == 0 {
			// When all namespaces or no filter, pass empty string to list across all namespaces
			targetNamespaces = []string{""}
		}

		for _, ns := range targetNamespaces {
			policies, err := k8s.LoadClusterPolicies(ctx, kubeConfig, ns)
			if err != nil {
				return fmt.Errorf("loading cluster policies for dedup: %w", err)
			}
			for name, pol := range policies {
				clusterPolicies[name] = pol
			}
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
