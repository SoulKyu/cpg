package hubble

import (
	"fmt"
	"io"
	"sort"
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
// attribution: a brand-new namespace acting purely as a flow source is still
// reported). Empty endpoint namespace names are skipped, as are flows with
// nil, invalid, or zero-value Flow.Time (Pitfall 3). Keeps the MINIMUM
// timestamp per namespace. Nil-map-safe: records nothing when the map is nil
// (the aggregator lazily allocates the map before its first call).
func noteFirstSeen(firstSeen map[string]time.Time, f *flowpb.Flow) {
	if firstSeen == nil || f == nil {
		return
	}
	ts := f.Time
	if ts == nil || !ts.IsValid() {
		return
	}
	t := ts.AsTime()
	if t.IsZero() {
		return
	}
	for _, ns := range []string{f.GetSource().GetNamespace(), f.GetDestination().GetNamespace()} {
		if ns == "" {
			continue
		}
		if cur, ok := firstSeen[ns]; !ok || t.Before(cur) {
			firstSeen[ns] = t
		}
	}
}

// computeNewNamespaces returns the sorted namespace names whose first-seen
// timestamp is STRICTLY after the capture start, defined as the minimum
// observed Flow.Time across the capture (planner decision A4 — never file
// mtime or wall-clock replay time). Strictly-after means a namespace whose
// first-seen EQUALS capture start is never reported: by design the
// first-observed namespace can never be flagged, and when multiple namespaces
// share the minimum timestamp they are all treated as pre-existing (flagged
// assumption — the heuristic cannot distinguish a namespace that existed at
// capture start from one created in the same instant as the first flow).
// Returns nil for empty, single-namespace, and no-valid-timestamp inputs
// (fail-safe empty).
func computeNewNamespaces(firstSeen map[string]time.Time) []string {
	if len(firstSeen) == 0 {
		return nil
	}
	captureStart, first := time.Time{}, true
	for _, t := range firstSeen {
		if first || t.Before(captureStart) {
			captureStart, first = t, false
		}
	}
	var out []string
	for ns, t := range firstSeen {
		if t.After(captureStart) {
			out = append(out, ns)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// PrintNewNamespacesSummary writes the end-of-run new-namespace report block
// to out. No-op when namespaces is nil or empty — the byte-identity mechanism
// mirroring PrintClusterHealthSummary's zero-drop guard: a run that detects no
// new namespaces leaves the session summary byte-identical to a build without
// this feature (zero bytes written). The report rides the cfg.Stdout summary
// seam ONLY — it is never written into evidence JSON, cluster-health.json, or
// generated policy YAML. source selects the per-namespace hint copy (watch vs
// replay). The list is defensively sorted for deterministic output.
func PrintNewNamespacesSummary(out io.Writer, namespaces []string, source NewNamespaceSource) {
	if len(namespaces) == 0 {
		return
	}
	sort.Strings(namespaces)
	fmt.Fprintln(out, "New namespaces detected during this run:")
	for _, ns := range namespaces {
		switch source {
		case NewNamespacesFromWatch:
			fmt.Fprintf(out, "  - namespace %s appeared after run start — re-run to cover it\n", ns)
		default: // NewNamespacesFromReplay
			fmt.Fprintf(out, "  - namespace %s first observed after capture start — re-run to cover it\n", ns)
		}
	}
}
