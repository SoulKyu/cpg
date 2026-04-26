package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SoulKyu/cpg/pkg/hubble"
)

// validateIgnoreProtocols normalizes the --ignore-protocol input (lowercase,
// preserves order) and rejects any value not in hubble.ValidIgnoreProtocols.
// nil/empty input is a no-op (returns nil, nil).
func validateIgnoreProtocols(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	allow := make(map[string]struct{}, len(hubble.ValidIgnoreProtocols()))
	for _, p := range hubble.ValidIgnoreProtocols() {
		allow[p] = struct{}{}
	}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.ToLower(raw)
		if _, ok := allow[v]; !ok {
			return nil, fmt.Errorf("unknown protocol %q: valid values are %s", raw, strings.Join(hubble.ValidIgnoreProtocols(), ", "))
		}
		out = append(out, v)
	}
	return out, nil
}

// commonFlags hold the flags shared by `generate` and `replay`.
type commonFlags struct {
	namespaces    []string
	allNamespaces bool
	outputDir     string
	flushInterval time.Duration
	clusterDedup  bool

	dryRun       bool
	dryRunNoDiff bool

	noEvidence       bool
	evidenceDir      string
	evidenceSamples  int
	evidenceSessions int

	l7 bool

	ignoreProtocols []string
}

// addCommonFlags wires the shared flags onto the given command.
func addCommonFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringSliceP("namespace", "n", nil, "namespace filter (repeatable)")
	f.BoolP("all-namespaces", "A", false, "observe all namespaces")

	f.StringP("output-dir", "o", "./policies", "output directory for generated policies")

	f.Duration("flush-interval", 5*time.Second, "aggregation flush interval")
	f.Bool("cluster-dedup", false, "skip policies that already exist in cluster (requires RBAC for CiliumNetworkPolicy list)")

	f.Bool("dry-run", false, "preview changes without writing to disk")
	f.Bool("no-diff", false, "with --dry-run, skip the unified diff output")

	f.Bool("no-evidence", false, "disable per-rule evidence capture")
	f.String("evidence-dir", "", "override evidence storage path (default: XDG_CACHE_HOME/cpg/evidence)")
	f.Int("evidence-samples", 10, "samples kept per rule in evidence files")
	f.Int("evidence-sessions", 10, "sessions kept per policy in evidence files")

	f.Bool("l7", false, "enable L7 (HTTP/DNS) policy generation; Phase 7 plumbs the flag, codegen lights up in v1.2 Phase 8/9")

	f.StringSlice("ignore-protocol", nil, "drop flows whose L4 protocol matches (repeatable, comma-separated). Valid: tcp, udp, icmpv4, icmpv6, sctp")
}

func parseCommonFlags(cmd *cobra.Command) commonFlags {
	f := cmd.Flags()
	out := commonFlags{}
	out.namespaces, _ = f.GetStringSlice("namespace")
	out.allNamespaces, _ = f.GetBool("all-namespaces")
	out.outputDir, _ = f.GetString("output-dir")
	out.flushInterval, _ = f.GetDuration("flush-interval")
	out.clusterDedup, _ = f.GetBool("cluster-dedup")
	out.dryRun, _ = f.GetBool("dry-run")
	out.dryRunNoDiff, _ = f.GetBool("no-diff")
	out.noEvidence, _ = f.GetBool("no-evidence")
	out.evidenceDir, _ = f.GetString("evidence-dir")
	out.evidenceSamples, _ = f.GetInt("evidence-samples")
	out.evidenceSessions, _ = f.GetInt("evidence-sessions")
	out.l7, _ = f.GetBool("l7")
	out.ignoreProtocols, _ = f.GetStringSlice("ignore-protocol")
	return out
}
