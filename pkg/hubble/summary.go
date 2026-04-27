package hubble

import (
	"fmt"
	"io"
	"sort"
	"strings"

	flowpb "github.com/cilium/cilium/api/v1/flow"

	"github.com/SoulKyu/cpg/pkg/dropclass"
)

const (
	minReasonNameWidth = 38 // historical baseline — short names fit within this
	maxReasonNameWidth = 60 // safety cap to prevent runaway wide output
)

// SummaryPathState describes how cluster-health.json was handled in this run.
// Passed to PrintClusterHealthSummary so the path line renders the correct suffix.
type SummaryPathState int

const (
	// SummaryPathWritten: evidence enabled and not dry-run; file was written.
	SummaryPathWritten SummaryPathState = iota
	// SummaryPathDryRun: dry-run mode; file would have been written but was not.
	SummaryPathDryRun
	// SummaryPathEvidenceOff: --no-evidence; file was never managed by this run.
	// Printing the file path would mislead the operator into looking for a file
	// that does not exist.
	SummaryPathEvidenceOff
)

// PrintClusterHealthSummary writes the end-of-run cluster-health block to out.
// No-op when snapshots is nil or empty (zero infra/transient drops).
// healthPath is the absolute path to cluster-health.json; used by SummaryPathWritten
// and SummaryPathDryRun states.
// state controls the path line rendering (see SummaryPathState).
func PrintClusterHealthSummary(out io.Writer, snapshots []HealthDropSnapshot, stats *SessionStats, healthPath string, state SummaryPathState) {
	if len(snapshots) == 0 {
		return
	}

	// Sort: infra (1) before transient (2) by DropClass value; descending count within same class.
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].Class != snapshots[j].Class {
			return snapshots[i].Class < snapshots[j].Class
		}
		return snapshots[i].Count > snapshots[j].Count
	})

	// M5: compute adaptive frame width based on the longest DropReason name.
	// Keeps short-reason summaries compact while preventing truncation for long names.
	reasonW := minReasonNameWidth
	for _, s := range snapshots {
		if name := flowpb.DropReason_name[int32(s.Reason)]; len(name) > reasonW {
			reasonW = len(name)
		}
	}
	if reasonW > maxReasonNameWidth {
		reasonW = maxReasonNameWidth
	}
	// M-1: frameWidth = reasonW (padded reason name) + 28 chars accounting for
	//   2 ("  " leading indent before reason) +
	//   1 (" "  space after reason) +
	//   11 ([transient] is the widest class label, brackets included) +
	//   2 ("  " spaces after label) +
	//   6 (count column up to 999999) +
	//   6 (" flows" trailing label including leading space)
	//   = 28. Adjust if the rendering format string changes.
	frameWidth := reasonW + 28
	frame := strings.Repeat("━", frameWidth)
	fmt.Fprintln(out, frame)
	fmt.Fprintln(out, "! Cluster-critical drops detected (NOT a policy issue)")
	fmt.Fprintln(out, frame)

	for _, s := range snapshots {
		name := flowpb.DropReason_name[int32(s.Reason)]
		class := s.Class.String()
		fmt.Fprintf(out, "  %-*s [%s]  %d flows\n", reasonW, name, class, s.Count)
		fmt.Fprintf(out, "    Top nodes:     %s\n", top3(s.ByNode))
		fmt.Fprintf(out, "    Top workloads: %s\n", top3(s.ByWorkload))
		if hint := dropclass.RemediationHint(s.Reason); hint != "" {
			fmt.Fprintf(out, "    Hint: %s\n", hint)
		}
		fmt.Fprintln(out)
	}

	switch state {
	case SummaryPathDryRun:
		fmt.Fprintf(out, "cluster-health.json: %s (dry-run, not written)\n", healthPath)
	case SummaryPathEvidenceOff:
		fmt.Fprintln(out, "cluster-health.json: (evidence disabled — file not written)")
	default: // SummaryPathWritten
		fmt.Fprintf(out, "cluster-health.json: %s\n", healthPath)
	}
	fmt.Fprintln(out, frame)
}

// top3 formats the top contributors from a name->count map.
// Shows the top 3 entries and ALL entries tied at the boundary (I8: no hidden entries
// at the tie boundary). Format: "name-a (32), name-b (12), name-c (3) (+N more)"
// Returns "(none)" when map is empty.
func top3(m map[string]uint64) string {
	if len(m) == 0 {
		return "(none)"
	}

	type kv struct {
		name string
		n    uint64
	}
	items := make([]kv, 0, len(m))
	for k, v := range m {
		items = append(items, kv{k, v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].n != items[j].n {
			return items[i].n > items[j].n
		}
		return items[i].name < items[j].name
	})

	limit := 3
	if len(items) < limit {
		limit = len(items)
	}
	// I8: extend limit to include all entries tied at the boundary count.
	// This prevents silently hiding entries that share the same count as the
	// 3rd-place entry.
	if limit > 0 {
		boundaryCount := items[limit-1].n
		for limit < len(items) && items[limit].n == boundaryCount {
			limit++
		}
	}

	parts := make([]string, limit)
	for i := 0; i < limit; i++ {
		parts[i] = fmt.Sprintf("%s (%d)", items[i].name, items[i].n)
	}
	result := strings.Join(parts, ", ")

	extra := len(items) - limit
	if extra > 0 {
		result += fmt.Sprintf(" (+%d more)", extra)
	}
	return result
}
