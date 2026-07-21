// Package session implements the capturing-to-stopped session state model
// for cpg's MCP session-lifecycle tools (start_session/get_status/stop_session).
//
// This file defines the pure data layer: the State enum, the Session struct
// (including the concurrency primitives the Manager drives), the
// already-validated StartArgs, and the three MCP-tool result shapes plus
// buildSummary. It contains zero orchestration logic — see manager.go
// (plan 17-03) for the mutex-guarded state machine that drives a Session's
// lifecycle end to end.
//
// Layering (Pitfall J): package session must NOT import the CLI
// composition-root package (package main) and must NOT reference its
// unexported stdout-resolution helper or argument validators — those
// symbols are private to that package. The stdout writer and the
// already-validated/normalized StartArgs values arrive as parameters from
// that CLI layer (plan 17-04).
package session

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"

	"github.com/SoulKyu/cpg/pkg/hubble"
)

// State is the coarse session lifecycle state (D-01: capturing -> stopped).
// "gone" is not a State value — a purged/replaced session simply has no
// Manager slot and is reported as not-found (D-02/D-04), never as a State.
type State int

const (
	// StateCapturing means the background pipeline goroutine is actively
	// streaming and writing artifacts under the session tmpdir.
	StateCapturing State = iota
	// StateStopped means the pipeline goroutine has exited and the
	// session's artifacts are finalized and retained (D-01) — a stopped
	// session has zero live resources and get_status/query reads become
	// pure filesystem reads.
	StateStopped
)

// String renders the state the way MCP tool responses expose it
// ("capturing"/"stopped"). An out-of-range value returns "unknown" rather
// than panicking — defensive against a future State constant being added
// without updating this method.
func (s State) String() string {
	switch s {
	case StateCapturing:
		return "capturing"
	case StateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// Session is one live or retained-stopped Hubble capture session.
//
// Exported fields are the coarse, cross-cutting state get_status/stop_session
// summarize. Unexported fields are the concurrency primitives the Manager
// (plan 17-03) drives directly.
type Session struct {
	// ID is the opaque MCP-facing handle, "sess_<uuid>" (D-10). Distinct
	// from the internal evidence SessionID (RFC3339-uuid4 format,
	// untouched — see pipeline_config.go).
	ID string
	// TmpDir is the ephemeral os.MkdirTemp session directory. Retained
	// after stop_session (D-01); removed only at the next start_session's
	// purge, or at server shutdown.
	TmpDir string
	// StartedAt is set once, at session creation.
	StartedAt time.Time
	// StoppedAt is the zero time.Time until the session is stopped.
	StoppedAt time.Time
	// State is the coarse capturing/stopped state (D-01).
	State State

	// cancel cancels the session's background context (a child of the
	// Manager's server-rooted ctx, never the tool-call's request ctx — see
	// plan 17-03 Pattern 2). context.CancelFunc is documented idempotent
	// and safe to call more than once, supporting D-03's idempotent stop.
	// Set and driven by plan 17-03's Manager; this plan writes zero
	// orchestration logic (see package doc), so the field is unread here.
	cancel context.CancelFunc //nolint:unused // consumed by plan 17-03's Manager
	// done receives the pipeline goroutine's terminal error exactly once.
	// Buffered 1 so the goroutine never blocks sending even before anyone
	// has received from it. Set and driven by plan 17-03's Manager.
	done chan error //nolint:unused // consumed by plan 17-03's Manager
	// stopOnce guards the actual cancel+wait+finalize sequence so
	// concurrent stop_session calls for the same session all observe the
	// identical completed teardown, rather than racing on the
	// single-buffered done channel (Pitfall F). Driven by plan 17-03's
	// Manager.
	stopOnce sync.Once //nolint:unused // consumed by plan 17-03's Manager

	// final holds the fully populated hubble.SessionStats captured by the
	// pipeline's OnFinal hook. Written on the pipeline's own goroutine,
	// read by Status/Stop on the tool-handler goroutine — atomic.Pointer
	// (not a bare field) keeps this race-free under `go test -race`.
	final atomic.Pointer[hubble.SessionStats]
}

// StartArgs are the already-validated, already-normalized inputs pkg/session
// receives from the CLI composition-root layer (D-05's argument surface).
// Validation and normalization — namespace/all_namespaces mutual
// exclusivity, IgnoreDropReasons/IgnoreProtocols allowlisting — is the
// caller's responsibility (the MCP tool-handler file, plan 17-04): per
// Pitfall J, the existing CLI validators are unexported package-main
// functions pkg/session cannot import.
type StartArgs struct {
	Namespaces    []string
	AllNamespaces bool
	L7            bool
	// IgnoreDropReasons is the uppercase, pre-validated set (D-06 — same
	// normalization as the CLI's existing drop-reason validator).
	IgnoreDropReasons []string
	// IgnoreProtocols is the lowercase, pre-validated set (D-06 — same
	// normalization as the CLI's existing protocol validator).
	IgnoreProtocols []string
	Server          string
	TLS             bool
	Timeout         time.Duration
	ClusterDedup    bool
	FlushInterval   time.Duration
}

// StartResult is the start_session MCP tool's structuredContent shape.
type StartResult struct {
	SessionID string `json:"session_id"`
	// DiscardedSession names a previous retained-stopped session purged by
	// this start (D-04). Empty unless a purge occurred.
	DiscardedSession string `json:"discarded_session,omitempty"`
	// Server is the resolved Hubble Relay address (the explicit arg, or
	// the auto-port-forward's local address).
	Server string `json:"server"`
}

// StatusResult is the get_status MCP tool's structuredContent shape.
type StatusResult struct {
	SessionID         string `json:"session_id"`
	State             string `json:"state"`
	Elapsed           string `json:"elapsed"`
	PolicyFileCount   int    `json:"policy_file_count"`
	EvidenceFileCount int    `json:"evidence_file_count"`
	TmpDir            string `json:"tmp_dir"`
}

// StopResult is the stop_session MCP tool's structuredContent shape (D-09):
// the OnFinal-captured SessionStats counters plus the absolute
// cluster-health.json path and the session tmpdir.
type StopResult struct {
	SessionID string `json:"session_id"`
	State     string `json:"state"`
	// AlreadyStopped marks a second/idempotent stop_session call (D-03) —
	// this is never an isError response; it carries the same summary as
	// the first stop.
	AlreadyStopped bool   `json:"already_stopped"`
	Duration       string `json:"duration"`

	FlowsSeen       uint64 `json:"flows_seen"`
	PoliciesWritten uint64 `json:"policies_written"`
	PoliciesSkipped uint64 `json:"policies_skipped"`
	PoliciesFailed  uint64 `json:"policies_failed"`
	LostEvents      uint64 `json:"lost_events"`
	L7HTTPCount     uint64 `json:"l7_http_count"`
	L7DNSCount      uint64 `json:"l7_dns_count"`
	InfraDropTotal  uint64 `json:"infra_drop_total"`
	// InfraDropsByReason is string-keyed (flowpb.DropReason_name), not the
	// protobuf-enum-keyed map hubble.SessionStats carries — JSON/LLM
	// friendly.
	InfraDropsByReason map[string]uint64 `json:"infra_drops_by_reason"`

	ClusterHealthPath string `json:"cluster_health_path"`
	TmpDir            string `json:"tmp_dir"`
}

// buildSummary turns the session's OnFinal-captured stats (if any) into a
// StopResult. Safe to call when final was never stored (the pipeline
// errored before OnFinal fired) — returns zeroed counters and a still-valid
// envelope, never panics.
func (s *Session) buildSummary(alreadyStopped bool, clusterHealthPath string) StopResult {
	result := StopResult{
		SessionID:          s.ID,
		State:              StateStopped.String(),
		AlreadyStopped:     alreadyStopped,
		ClusterHealthPath:  clusterHealthPath,
		TmpDir:             s.TmpDir,
		InfraDropsByReason: map[string]uint64{},
	}

	elapsed := time.Since(s.StartedAt)
	if !s.StoppedAt.IsZero() {
		elapsed = s.StoppedAt.Sub(s.StartedAt)
	}
	result.Duration = elapsed.Round(time.Second).String()

	stats := s.final.Load()
	if stats == nil {
		return result
	}

	result.FlowsSeen = stats.FlowsSeen
	result.PoliciesWritten = stats.PoliciesWritten
	result.PoliciesSkipped = stats.PoliciesSkipped
	result.PoliciesFailed = stats.PoliciesFailed
	result.LostEvents = stats.LostEvents
	result.L7HTTPCount = stats.L7HTTPCount
	result.L7DNSCount = stats.L7DNSCount
	result.InfraDropTotal = stats.InfraDropTotal
	for reason, count := range stats.InfraDropsByReason {
		result.InfraDropsByReason[flowpb.DropReason_name[int32(reason)]] = count
	}

	return result
}
