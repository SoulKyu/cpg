package hubble

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/dropclass"
)

// healthDropEntry accumulates counters for a single drop reason.
type healthDropEntry struct {
	reason     flowpb.DropReason
	class      dropclass.DropClass
	count      uint64
	byNode     map[string]uint64
	byWorkload map[string]uint64
}

// healthWriter accumulates DropEvents from the healthCh channel and writes
// cluster-health.json atomically when finalize() is called.
// Owned by a single goroutine (Stage 2c) — no mutex required.
type healthWriter struct {
	evidenceDir string
	outputHash  string
	logger      *zap.Logger
	drops       map[flowpb.DropReason]*healthDropEntry
	startedAt   time.Time
}

// newHealthWriter constructs a healthWriter.
func newHealthWriter(evidenceDir, outputHash string, logger *zap.Logger, startedAt time.Time) *healthWriter {
	return &healthWriter{
		evidenceDir: evidenceDir,
		outputHash:  outputHash,
		logger:      logger,
		drops:       make(map[flowpb.DropReason]*healthDropEntry),
		startedAt:   startedAt,
	}
}

// accumulate folds a DropEvent into the in-memory counters.
// Must be called from a single goroutine only (Stage 2c).
func (hw *healthWriter) accumulate(e DropEvent) {
	entry, ok := hw.drops[e.Reason]
	if !ok {
		entry = &healthDropEntry{
			reason:     e.Reason,
			class:      e.Class,
			byNode:     make(map[string]uint64),
			byWorkload: make(map[string]uint64),
		}
		hw.drops[e.Reason] = entry
	}
	entry.count++

	node := e.NodeName
	if node == "" {
		node = "_unknown"
	}
	entry.byNode[node]++

	workload := e.Workload
	if workload == "" {
		workload = "_unknown"
	}
	// Qualify workload with namespace for the by_workload key.
	wkey := e.Namespace + "/" + workload
	if e.Namespace == "" {
		wkey = "_unknown/" + workload
	}
	entry.byWorkload[wkey]++
}

// finalize writes cluster-health.json atomically.
// No-op when hw is nil (dry-run) or when zero drops were accumulated.
func (hw *healthWriter) finalize(stats *SessionStats) error {
	if hw == nil {
		return nil
	}
	if len(hw.drops) == 0 {
		hw.logger.Info("health writer: no infra/transient drops observed — skipping cluster-health.json")
		return nil
	}

	// Build sorted drops array for deterministic output.
	entries := make([]*healthDropEntry, 0, len(hw.drops))
	for _, e := range hw.drops {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		nameI := flowpb.DropReason_name[int32(entries[i].reason)]
		nameJ := flowpb.DropReason_name[int32(entries[j].reason)]
		return nameI < nameJ
	})

	dropsJSON := make([]healthDropJSON, 0, len(entries))
	for _, e := range entries {
		dropsJSON = append(dropsJSON, healthDropJSON{
			Reason:      flowpb.DropReason_name[int32(e.reason)],
			Class:       dropClassString(e.class),
			Count:       e.count,
			Remediation: dropclass.RemediationHint(e.reason),
			ByNode:      e.byNode,
			ByWorkload:  e.byWorkload,
		})
	}

	endedAt := time.Now()
	report := clusterHealthReport{
		SchemaVersion:     1,
		ClassifierVersion: dropclass.ClassifierVersion,
		Session: healthSession{
			Started:        hw.startedAt,
			Ended:          endedAt,
			FlowsSeen:      stats.FlowsSeen,
			InfraDropTotal: stats.InfraDropTotal,
		},
		Drops: dropsJSON,
	}

	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("health writer: encoding report: %w", err)
	}

	path := filepath.Join(hw.evidenceDir, hw.outputHash, "cluster-health.json")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("health writer: creating dir: %w", err)
	}

	// Atomic write: CreateTemp → write → Close → Rename (mirrors pkg/evidence/writer.go).
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("health writer: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("health writer: writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("health writer: closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("health writer: atomic rename: %w", err)
	}

	hw.logger.Info("cluster-health.json written",
		zap.String("path", path),
		zap.Int("drop_reasons", len(entries)),
		zap.Uint64("infra_drop_total", stats.InfraDropTotal),
	)
	return nil
}

// HealthDropSnapshot is the per-reason view used by the summary formatter.
// Unexported beyond pkg/hubble; used only by PrintClusterHealthSummary.
type HealthDropSnapshot struct {
	Reason     flowpb.DropReason
	Class      dropclass.DropClass
	Count      uint64
	ByNode     map[string]uint64 // shallow copy
	ByWorkload map[string]uint64 // shallow copy
}

// Snapshot returns a copy of the accumulated drop entries.
// Returns nil when hw is nil (dry-run / evidence disabled).
func (hw *healthWriter) Snapshot() []HealthDropSnapshot {
	if hw == nil {
		return nil
	}
	result := make([]HealthDropSnapshot, 0, len(hw.drops))
	for _, e := range hw.drops {
		result = append(result, HealthDropSnapshot{
			Reason:     e.reason,
			Class:      e.class,
			Count:      e.count,
			ByNode:     shallowCopyMap(e.byNode),
			ByWorkload: shallowCopyMap(e.byWorkload),
		})
	}
	return result
}

// shallowCopyMap returns a shallow copy of a string->uint64 map.
func shallowCopyMap(m map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// dropClassString converts a DropClass to its lowercase string representation
// for JSON output. Fallback to "unknown" for unrecognized values.
func dropClassString(c dropclass.DropClass) string {
	switch c {
	case dropclass.DropClassPolicy:
		return "policy"
	case dropclass.DropClassInfra:
		return "infra"
	case dropclass.DropClassTransient:
		return "transient"
	case dropclass.DropClassNoise:
		return "noise"
	default:
		return "unknown"
	}
}

// JSON output structs — unexported, used only for marshaling.

type clusterHealthReport struct {
	SchemaVersion     int              `json:"schema_version"`
	ClassifierVersion string           `json:"classifier_version"`
	Session           healthSession    `json:"session"`
	Drops             []healthDropJSON `json:"drops"`
}

type healthSession struct {
	Started        time.Time `json:"started"`
	Ended          time.Time `json:"ended"`
	FlowsSeen      uint64    `json:"flows_seen"`
	InfraDropTotal uint64    `json:"infra_drops_total"`
}

type healthDropJSON struct {
	Reason      string            `json:"reason"`
	Class       string            `json:"class"`
	Count       uint64            `json:"count"`
	Remediation string            `json:"remediation"`
	ByNode      map[string]uint64 `json:"by_node"`
	ByWorkload  map[string]uint64 `json:"by_workload"`
}
