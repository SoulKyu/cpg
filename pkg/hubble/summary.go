package hubble

import (
	"fmt"
	"io"
	"sort"
	"strings"

	flowpb "github.com/cilium/cilium/api/v1/flow"

	"github.com/SoulKyu/cpg/pkg/dropclass"
)

const summaryWidth = 52

// PrintClusterHealthSummary writes the end-of-run cluster-health block to out.
// No-op when snapshots is nil or empty (zero infra/transient drops).
// healthPath is the absolute path to cluster-health.json.
// dryRun appends "(dry-run, not written)" to the path line.
func PrintClusterHealthSummary(out io.Writer, snapshots []HealthDropSnapshot, stats *SessionStats, healthPath string, dryRun bool) {
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

	frame := strings.Repeat("━", summaryWidth)
	fmt.Fprintln(out, frame)
	fmt.Fprintln(out, "! Cluster-critical drops detected (NOT a policy issue)")
	fmt.Fprintln(out, frame)

	for _, s := range snapshots {
		name := flowpb.DropReason_name[int32(s.Reason)]
		class := dropClassString(s.Class)
		fmt.Fprintf(out, "  %-38s [%s]  %d flows\n", name, class, s.Count)
		fmt.Fprintf(out, "    Top nodes:     %s\n", top3(s.ByNode))
		fmt.Fprintf(out, "    Top workloads: %s\n", top3(s.ByWorkload))
		if hint := dropclass.RemediationHint(s.Reason); hint != "" {
			fmt.Fprintf(out, "    Hint: %s\n", hint)
		}
		fmt.Fprintln(out)
	}

	pathLine := healthPath
	if dryRun {
		pathLine += " (dry-run, not written)"
	}
	fmt.Fprintf(out, "cluster-health.json: %s\n", pathLine)
	fmt.Fprintln(out, frame)
}

// top3 formats up to the top-3 contributors from a name->count map.
// Format: "name-a (32), name-b (12), name-c (3) (+N more)"
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
