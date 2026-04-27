package main

import (
	"fmt"
	"strings"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/dropclass"
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

	ignoreProtocols   []string
	ignoreDropReasons []string
	failOnInfraDrops  bool
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

	f.StringSlice("ignore-drop-reason", nil,
		"exclude flows by drop reason name before classification "+
			"(repeatable, comma-separated, case-insensitive). "+
			"Passing a reason already classified as infra/transient emits a warning.")
	f.Bool("fail-on-infra-drops", false,
		"exit with code 1 when ≥1 infra drop is observed (default: always exit 0)")
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
	out.ignoreDropReasons, _ = f.GetStringSlice("ignore-drop-reason")
	out.failOnInfraDrops, _ = f.GetBool("fail-on-infra-drops")
	return out
}

// levenshtein computes the edit distance between strings a and b using the
// standard 2-row DP algorithm. O(len(a)*len(b)) time, O(len(b)) space.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// suggestClosest returns up to n candidates from candidates that are closest
// (by Levenshtein distance) to input. Ties broken lexicographically for
// deterministic output.
func suggestClosest(input string, candidates []string, n int) []string {
	type scored struct {
		name string
		dist int
	}
	scored_ := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		scored_ = append(scored_, scored{name: c, dist: levenshtein(input, c)})
	}
	// Stable sort: ascending distance, then lexicographic on name.
	for i := 1; i < len(scored_); i++ {
		for j := i; j > 0 && (scored_[j].dist < scored_[j-1].dist ||
			(scored_[j].dist == scored_[j-1].dist && scored_[j].name < scored_[j-1].name)); j-- {
			scored_[j], scored_[j-1] = scored_[j-1], scored_[j]
		}
	}
	if n > len(scored_) {
		n = len(scored_)
	}
	out := make([]string, n)
	for i := range out {
		out[i] = scored_[i].name
	}
	return out
}

// validateCommonFlags is the cobra PreRunE handler shared by generate and replay.
// Runs BEFORE RunE so flag errors abort before kubeconfig load / port-forward.
// logger may be nil during PreRunE if main() hasn't initialized it yet;
// validateIgnoreDropReasons handles nil logger gracefully.
func validateCommonFlags(cmd *cobra.Command, _ []string) error {
	f := parseCommonFlags(cmd)
	if _, err := validateIgnoreProtocols(f.ignoreProtocols); err != nil {
		return err
	}
	if _, err := validateIgnoreDropReasons(f.ignoreDropReasons, logger); err != nil {
		return err
	}
	return nil
}

// validateIgnoreDropReasons normalizes --ignore-drop-reason input (uppercase),
// rejects unknown reason names (FILTER-02), and warns when a name is already
// classified Infra/Transient by default suppression (FILTER-03).
// nil/empty input is a no-op (returns nil, nil).
func validateIgnoreDropReasons(in []string, logger *zap.Logger) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}

	// Build allowlist from canonical protobuf enum names (UPPERCASE).
	all := dropclass.ValidReasonNames()
	allow := make(map[string]struct{}, len(all))
	for _, n := range all {
		allow[n] = struct{}{}
	}

	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.ToUpper(raw)
		if _, ok := allow[v]; !ok {
			suggestions := suggestClosest(v, all, 5)
			return nil, fmt.Errorf(
				"unknown drop reason %q: did you mean any of: %s? See https://docs.cilium.io/en/stable/observability/hubble/#dropreason for the full list",
				raw, strings.Join(suggestions, ", "),
			)
		}
		// FILTER-03: warn when reason is already suppressed by default.
		if reasonVal, exists := flowpb.DropReason_value[v]; exists {
			class := dropclass.Classify(flowpb.DropReason(reasonVal))
			if class == dropclass.DropClassInfra || class == dropclass.DropClassTransient {
				if logger != nil {
					logger.Warn("--ignore-drop-reason is redundant: reason is already classified and suppressed by default",
						zap.String("reason", v),
						zap.String("class", class.String()),
					)
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

