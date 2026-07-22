package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/k8s"
	"github.com/SoulKyu/cpg/pkg/policy"
)

// newBootstrapCmd builds the `cpg bootstrap` subcommand: a single-purpose,
// non-streaming command that emits a namespaced default-deny
// CiliumNetworkPolicy (cilium/cilium#35558-safe, see pkg/policy.BuildBootstrapPolicy).
// Mirrors replay.go's constructor simplicity — deliberately does NOT call
// addCommonFlags (22-RESEARCH Pattern 2): bootstrap needs only -n, none of
// generate/replay's streaming-pipeline flag set.
func newBootstrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Generate a namespaced default-deny bootstrap CiliumNetworkPolicy",
		Long: `Emit a namespaced default-deny CiliumNetworkPolicy (enableDefaultDeny +
explicit ingress/egress presence, cilium/cilium#35558-safe) to stdout.
Detects the cluster's Cilium version and hard-refuses when a determined
version is below the enableDefaultDeny floor (>= 1.16); proceeds with a
warning when the version cannot be determined.

Examples:
  # Print the bootstrap policy for namespace "production" to stdout
  cpg bootstrap -n production

  # Save it to a file
  cpg bootstrap -n production > default-deny-production.yaml

  # Apply it directly
  cpg bootstrap -n production | kubectl apply -f -`,
		Args: cobra.NoArgs,
		RunE: runBootstrap,
	}
	cmd.Flags().StringP("namespace", "n", "", "target namespace (required)")
	_ = cmd.MarkFlagRequired("namespace")
	return cmd
}

// bootstrapDetectVersion is a package-level detection seam (mirroring
// generate.go's l7ClientFactory seam pattern) so tests can substitute a
// fixed k8s.CompatInfo without a live cluster. Best-effort:
// k8s.LoadKubeConfig -> l7ClientFactory(cfg) -> k8s.DetectCiliumVersion,
// bounded by versionPreflightTimeout (generate.go's own constant, reused
// unmodified). Never returns an error — any load/construct failure yields
// CompatInfo{Source: "undetermined"}, exactly like maybeRunVersionPreflight.
// This reaches ONLY already-audited read functions (LoadKubeConfig,
// NewForConfig via l7ClientFactory, DetectCiliumVersion -> pods/list) —
// introduces no K8s write verb and no filesystem write.
var bootstrapDetectVersion = func(ctx context.Context, logger *zap.Logger) k8s.CompatInfo {
	kubeConfig, err := k8s.LoadKubeConfig()
	if err != nil {
		logger.Warn("bootstrap version detection skipped: kubeconfig not available", zap.Error(err))
		return k8s.CompatInfo{Source: "undetermined"}
	}
	client, err := l7ClientFactory(kubeConfig)
	if err != nil {
		logger.Warn("bootstrap version detection skipped: failed to construct kubernetes client", zap.Error(err))
		return k8s.CompatInfo{Source: "undetermined"}
	}
	detectCtx, cancel := context.WithTimeout(ctx, versionPreflightTimeout)
	defer cancel()
	return k8s.DetectCiliumVersion(detectCtx, client, logger)
}

// bootstrapVersionGate is the single decision point reused verbatim by the
// MCP handler (mcp_bootstrap.go) so the hard-refuse / warn-and-proceed
// behavior can never drift between the CLI and MCP surfaces.
//
//   - A determined version (compat.ClusterVersion != "") below the
//     enableDefaultDeny floor -> a non-nil, actionable error naming BOTH the
//     detected version and the >= 1.16 floor. No artifact should be
//     constructed after this branch fires.
//   - An undetermined version (compat.ClusterVersion == "") -> a non-empty
//     warning string, nil error. Caller logs the warning and proceeds
//     (warn-and-proceed).
//   - A determined version at or above every floor -> ("", nil): proceed
//     silently.
func bootstrapVersionGate(compat k8s.CompatInfo) (warning string, err error) {
	if compat.ClusterVersion != "" {
		for _, f := range compat.BelowFloorFeatures {
			if strings.Contains(f, "enableDefaultDeny") {
				return "", fmt.Errorf(
					"cluster Cilium version %s is below the enableDefaultDeny floor (>= 1.16.0 required); "+
						"refusing to generate a bootstrap artifact that would be silently pruned by the CRD "+
						"schema on this cluster",
					compat.ClusterVersion,
				)
			}
		}
		return "", nil
	}

	return "Cilium version undetermined; enableDefaultDeny requires >= 1.16 — proceeding", nil
}

// validateBootstrapNamespace rejects namespaces that are not valid DNS-1123
// labels (the Kubernetes namespace-name rule) before they flow into
// metadata.namespace. Typed-struct marshal already prevents YAML injection;
// this guards against emitting a well-formed artifact the apiserver would
// reject anyway (e.g. "Foo Bar", "../etc"). Shared by the CLI and the MCP
// handler so the two surfaces cannot drift.
func validateBootstrapNamespace(namespace string) error {
	if errs := validation.IsDNS1123Label(namespace); len(errs) > 0 {
		return fmt.Errorf("invalid namespace %q: %s", namespace, strings.Join(errs, "; "))
	}
	return nil
}

func runBootstrap(cmd *cobra.Command, _ []string) error {
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

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	compat := bootstrapDetectVersion(ctx, logger)
	warning, err := bootstrapVersionGate(compat)
	if err != nil {
		return err
	}
	if warning != "" {
		logger.Warn(warning)
	}

	cnp := policy.BuildBootstrapPolicy(namespace)
	data, err := yaml.Marshal(cnp)
	if err != nil {
		return fmt.Errorf("marshaling bootstrap policy: %w", err)
	}

	// ponytail: stdout-only — no -o flag, no file write path anywhere in this
	// command. A file write here would force an fsWriteAllowlist entry in the
	// SEC-01 audit (RTA's reflect.Value.Call edge sweeps every address-taken
	// function, including cobra RunE targets); shell redirection covers the
	// file use case with zero write call sites.
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
