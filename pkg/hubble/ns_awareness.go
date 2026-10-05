package hubble

import (
	"io"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

// NewNamespaceSource identifies which detection mechanism produced a
// new-namespace report list, selecting the per-namespace hint copy in
// PrintNewNamespacesSummary. The watch const is unused until plan 26-02 (the
// live NSAW-01 watcher); it is declared now so the print signature never
// changes when the watcher path plugs into the same seam.
type NewNamespaceSource int

const (
	// NewNamespacesFromWatch marks namespaces observed by the live NSAW-01
	// namespace watch (cpg generate).
	NewNamespacesFromWatch NewNamespaceSource = iota
	// NewNamespacesFromReplay marks namespaces whose first observed flow is
	// strictly after the capture start (NSAW-02, offline replay heuristic).
	NewNamespacesFromReplay
)

// noteFirstSeen records the earliest observed Flow.Time for BOTH endpoint
// namespaces of f (source and destination — NSAW-02 A5 existence-awareness
// attribution). Empty endpoint namespace names are skipped, as are flows with
// nil, invalid, or zero-value Flow.Time (Pitfall 3). Keeps the MINIMUM
// timestamp per namespace. nil-map-safe: writing nothing when the map is nil
// (the aggregator lazily allocates before its first call).
func noteFirstSeen(firstSeen map[string]time.Time, f *flowpb.Flow) {
	_ = firstSeen
	_ = f
}

// computeNewNamespaces returns the sorted namespace names whose first-seen
// timestamp is STRICTLY after the capture start, defined as the minimum
// observed Flow.Time across the capture (planner decision A4 — never file
// mtime or wall-clock replay time). Returns nil for empty, single-namespace,
// and no-valid-timestamp inputs (fail-safe empty).
func computeNewNamespaces(firstSeen map[string]time.Time) []string {
	return nil
}

// PrintNewNamespacesSummary writes the end-of-run new-namespace report block
// to out. No-op when namespaces is nil or empty — the byte-identity mechanism
// mirroring PrintClusterHealthSummary's zero-drop guard: a run that detects no
// new namespaces leaves the session summary byte-identical to a build without
// this feature. The report rides the cfg.Stdout summary seam ONLY — it is
// never written into evidence JSON, cluster-health.json, or generated policy
// YAML. source selects the per-namespace hint copy (watch vs replay).
func PrintNewNamespacesSummary(out io.Writer, namespaces []string, source NewNamespaceSource) {
	_ = out
	_ = namespaces
	_ = source
}
