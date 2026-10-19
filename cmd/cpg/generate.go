package main

import (
	"context"
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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/hubble"
	"github.com/SoulKyu/cpg/pkg/k8s"
)

// l7ClientFactory builds a kubernetes.Interface from a rest.Config. Extracted
// as a package-level variable so tests can substitute a fake client without
// touching the production code path.
var l7ClientFactory = func(cfg *rest.Config) (kubernetes.Interface, error) {
	return kubernetes.NewForConfig(cfg)
}

// versionPreflightTimeout hard-bounds the always-on CLI version preflight so a
// kubeconfig pointing at an unreachable or wedged apiserver can never stall
// cpg startup (WR-01). Mirrors pkg/k8s.versionDetectTimeout (unexported there,
// hence duplicated here): "warn-and-proceed, never blocking" must survive a
// hung apiserver, so the probe carries its own deadline even when the caller
// ctx has none.
const versionPreflightTimeout = 3 * time.Second

// maybeRunL7Preflight runs pkg/k8s.RunL7Preflight when L7 generation is
// requested and pre-flight is not explicitly disabled. Pre-flight is advisory:
// any failure to construct a client is logged as a warning and the pipeline
// proceeds. Pre-flight is invoked AT MOST ONCE per cpg invocation.
//
// Caller contract: invoke from cpg generate ONLY. cpg replay is offline by
// definition and must never call this function regardless of --l7.
func maybeRunL7Preflight(ctx context.Context, kubeConfig *rest.Config, l7Enabled, noPreflight bool, logger *zap.Logger) {
	if !l7Enabled || noPreflight {
		return
	}
	if kubeConfig == nil {
		var err error
		kubeConfig, err = k8s.LoadKubeConfig()
		if err != nil {
			logger.Warn("--l7 preflight skipped: kubeconfig not available", zap.Error(err))
			return
		}
	}
	client, err := l7ClientFactory(kubeConfig)
	if err != nil {
		logger.Warn("--l7 preflight skipped: failed to construct kubernetes client", zap.Error(err))
		return
	}
	k8s.RunL7Preflight(ctx, client, logger)
}

// maybeRunVersionPreflight runs pkg/k8s.DetectCiliumVersion once before the
// pipeline starts. Unlike maybeRunL7Preflight, version detection has no
// opt-in gate: it is always-on, because feature-floor awareness benefits
// every invocation, not only L7 users. Detection is advisory: any failure to
// load a kubeconfig or construct a client is logged as a warning and the
// pipeline proceeds. DetectCiliumVersion itself emits the below-floor / RBAC
// warnings internally, so this call site never re-warns on its result.
//
// Caller contract: invoke from cpg generate ONLY. cpg replay is offline by
// definition and must never call this function.
func maybeRunVersionPreflight(ctx context.Context, kubeConfig *rest.Config, logger *zap.Logger) {	if kubeConfig == nil {
		var err error
		kubeConfig, err = k8s.LoadKubeConfig()
		if err != nil {
			logger.Warn("version preflight skipped: kubeconfig not available", zap.Error(err))
			return
		}
	}
	client, err := l7ClientFactory(kubeConfig)
	if err != nil {
		logger.Warn("version preflight skipped: failed to construct kubernetes client", zap.Error(err))
		return
	}
	// WR-01: bound the probe with its own deadline. The signal ctx carries no
	// deadline and LoadKubeConfig sets no rest.Config.Timeout, so without this
	// an unreachable apiserver would stall startup indefinitely.
	detectCtx, cancel := context.WithTimeout(ctx, versionPreflightTimeout)
	defer cancel()
	k8s.DetectCiliumVersion(detectCtx, client, logger)
}

// maybeStartNamespaceWatcher starts the NSAW-01 namespace-creation watcher
// (pkg/k8s.StartNamespaceWatcher) once, before the pipeline runs. Always-on
// (no opt-in flag — the report is advisory, like version preflight). The
// launcher itself is fail-safe: a nil kubeconfig (the --server bypass,
// Pitfall 6) or a client construction failure logs exactly one skip warning
// and returns nil — generation proceeds without namespace awareness, and the
// pipeline falls back to the replay heuristic. StartNamespaceWatcher never
// returns an error and warns at most once itself, so this call site adds no
// error handling of its own. The returned watcher's lifetime is bounded by
// ctx (the run's signal context); wire the (possibly nil) result into
// PipelineConfig.NewNamespaces — the interface field is nil-safe.
//
// Caller contract: invoke from cpg generate ONLY. cpg replay is offline by
// definition and must never start a watcher.
func maybeStartNamespaceWatcher(ctx context.Context, kubeConfig *rest.Config, logger *zap.Logger) *k8s.NamespaceWatcher {
	if kubeConfig == nil {
		var err error
		kubeConfig, err = k8s.LoadKubeConfig()
		if err != nil {
			logger.Warn("namespace watch skipped: kubeconfig not available", zap.Error(err))
			return nil
		}
	}
	client, err := l7ClientFactory(kubeConfig)
	if err != nil {
		logger.Warn("namespace watch skipped: failed to construct kubernetes client", zap.Error(err))
		return nil
	}
	return k8s.StartNamespaceWatcher(ctx, client, logger)
}

// maybeDetectDNSSelector reads the DNS companion selector from the
// kube-system DNS Service (DNS-FUT-02). Only --l7 emits the companion, so
// detection is skipped otherwise. Fail-safe: any failure logs one warning and
// returns nil, which selects the policy fallback.
func maybeDetectDNSSelector(ctx context.Context, kubeConfig *rest.Config, l7 bool, logger *zap.Logger) map[string]string {
	if !l7 {
		return nil
	}
	if kubeConfig == nil {
		var err error
		kubeConfig, err = k8s.LoadKubeConfig()
		if err != nil {
			logger.Warn("dns companion selector: kubeconfig not available, falling back to k8s-app in (kube-dns, coredns)", zap.Error(err))
			return nil
		}
	}
	client, err := l7ClientFactory(kubeConfig)
	if err != nil {
		logger.Warn("dns companion selector: failed to construct kubernetes client, falling back to k8s-app in (kube-dns, coredns)", zap.Error(err))
		return nil
	}
	return k8s.DetectDNSSelector(ctx, client, logger)
}

func newGenerateCmd() *cobra.Command {
	bin := "cpg"
	if isKubectlPlugin() {
		bin = "kubectl cilium-policy-gen"
	}

	cmd := &cobra.Command{
		Use:     "generate",
		PreRunE: validateCommonFlags,
		Short:   "Generate CiliumNetworkPolicies from Hubble flow observations",
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

	// generate-specific L7 pre-flight skip flag (VIS-06). Not part of replay
	// because replay is offline by definition and never invokes pre-flight.
	cmd.Flags().Bool("no-l7-preflight", false, "skip L7 cluster pre-flight checks (VIS-06); useful for restricted-RBAC CI or air-gapped use")

	return cmd
}

// generateFlags extends commonFlags with the connection flags specific to live streaming.
type generateFlags struct {
	commonFlags
	server        string
	tlsEnabled    bool
	timeout       time.Duration
	noL7Preflight bool
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
	f.noL7Preflight, _ = cmd.Flags().GetBool("no-l7-preflight")
	return f
}

func runGenerate(cmd *cobra.Command, _ []string) error {
	f := parseGenerateFlags(cmd)
	if err := f.validate(); err != nil {
		return err
	}
	ignoreProtocols, err := validateIgnoreProtocols(f.ignoreProtocols)
	if err != nil {
		return err
	}
	// FILTER-03: PreRunE (validateCommonFlags) already validated these flags and
	// emitted any redundancy warning. Pass a nil logger here so this re-validation
	// stays silent and the warning is not duplicated per invocation.
	ignoreDropReasons, err := validateIgnoreDropReasons(f.ignoreDropReasons, nil)
	if err != nil {
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

	// VIS-04/05/06: run L7 cluster pre-flight ONCE before the pipeline starts.
	// Skipped when --l7 is unset, when --no-l7-preflight wins, or when no
	// kubeconfig is reachable. Pre-flight is advisory: warnings only, never
	// blocking.
	maybeRunL7Preflight(ctx, kubeConfig, f.l7, f.noL7Preflight, logger)

	// COMPAT-02: run Cilium version preflight ONCE before the pipeline
	// starts. Always-on (no opt-in flag, unlike L7 pre-flight above) and
	// advisory: warnings only, never blocking. DetectCiliumVersion itself
	// warns when the connected cluster is below one or more feature floors.
	maybeRunVersionPreflight(ctx, kubeConfig, logger)

	// NSAW-01: start the namespace-creation watcher ONCE before the pipeline
	// runs. Always-on and advisory like version preflight; nil kubeConfig
	// (--server bypass) or any launcher failure degrades to a single skip
	// warning and the pipeline proceeds. The watcher (when non-nil) is the
	// authoritative new-namespace source for the run's end-of-run report.
	nsWatcher := maybeStartNamespaceWatcher(ctx, kubeConfig, logger)

	dnsSelector := maybeDetectDNSSelector(ctx, kubeConfig, f.l7, logger)

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

		L7Enabled:    f.l7,
		IncludeAudit: f.includeAudit,

		CollapseL7Paths:   f.l7CollapsePaths,
		MinFlowsPerL7Rule: f.minFlowsPerL7Rule,
		DNSSelector:       dnsSelector,

		IgnoreProtocols:   ignoreProtocols,
		IgnoreDropReasons: ignoreDropReasons,
		FailOnInfraDrops:  f.failOnInfraDrops,

		// NSAW-01: nil when no watcher could be started (no kubeconfig,
		// restricted RBAC at the cmd layer) — the pipeline then falls back to
		// the replay heuristic in its watcher-precedence branch.
		NewNamespaces: nsWatcher,
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
