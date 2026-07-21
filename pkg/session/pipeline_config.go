package session

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/hubble"
)

// defaultDuration returns fallback when d <= 0, else d.
//
// This is the Pitfall A crash guard: an MCP client that omits an optional
// duration argument (timeout/flush_interval, D-05) sends nothing, which
// unmarshals to Go's zero value — unlike a cobra flag, whose default is
// baked into flag registration at parse time. Forwarding a zero
// FlushInterval straight into hubble.PipelineConfig panics inside
// pkg/hubble/aggregator.go's time.NewTicker (time.NewTicker panics for
// d <= 0); that runs inside an errgroup goroutine, so an unrecovered panic
// crashes the whole cpg mcp process, not just the one tool call. A zero
// Timeout instead silently removes the gRPC dial's bound. A negative
// duration (syntactically valid from time.ParseDuration, e.g. "-5s") is
// treated the same as zero — both fall back to the caller-supplied default.
func defaultDuration(d, fallback time.Duration) time.Duration {
	if d <= 0 {
		return fallback
	}
	return d
}

// buildPipelineConfig ports the CLI's live-generate hubble.PipelineConfig
// construction recipe (RESEARCH.md Pattern 5) to an ephemeral session
// tmpdir. cpgVersion is threaded through as a parameter (rather than read
// from a package-level var) because pkg/session cannot see the CLI
// composition root's build-time version string — the future Manager (plan
// 17-03) holds it as a field, set once at construction.
//
// Differences from the CLI's own recipe, all deliberate (D-05):
//   - OutputDir/EvidenceDir live under tmpDir, not a user-chosen path.
//   - Stdout is the caller-injected writer (the MCP tool-handler layer
//     passes its stdout-resolution helper in plan 17-04) — never resolved
//     by name inside this package.
//   - OnFinal is wired by the caller to the session's atomic stats store.
//   - The CLI's preview-mode fields and its opt-in infra-drop exit-code
//     field are never set — nonsensical in MCP mode (D-05).
//   - Timeout/FlushInterval are passed through defaultDuration first, so a
//     zero-value MCP arg can never reach hubble.PipelineConfig (Pitfall A).
//
// L7 cluster pre-flight (the CLI's maybeRunL7Preflight) is intentionally
// NOT invoked here. It is CLI-only/advisory, and its "at most once per
// process invocation" contract does not fit a long-lived MCP server that
// may run many sessions per process (RESEARCH.md Pattern 5 / Open Q1).
func buildPipelineConfig(
	args StartArgs,
	tmpDir string,
	server string,
	logger *zap.Logger,
	cpgVersion string,
	stdout io.Writer,
	clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy,
	onFinal func(hubble.SessionStats),
) hubble.PipelineConfig {
	outputDir := filepath.Join(tmpDir, "policies")
	evidenceDir := filepath.Join(tmpDir, "evidence")
	outputHash := evidence.HashOutputDir(outputDir)

	return hubble.PipelineConfig{
		Server:          server,
		TLSEnabled:      args.TLS,
		Timeout:         defaultDuration(args.Timeout, 10*time.Second),
		Namespaces:      args.Namespaces,
		AllNamespaces:   args.AllNamespaces,
		OutputDir:       outputDir,
		FlushInterval:   defaultDuration(args.FlushInterval, 5*time.Second),
		Logger:          logger,
		ClusterPolicies: clusterPolicies,

		// The CLI's preview-mode fields (diff/color included) and its
		// opt-in infra-drop exit-code field are left unset here — D-05
		// excludes them from MCP mode entirely; they stay at Go's zero
		// value (false).

		EvidenceEnabled: true,
		EvidenceDir:     evidenceDir,
		OutputHash:      outputHash,
		EvidenceCaps:    evidence.MergeCaps{MaxSamples: 10, MaxSessions: 10},
		// SessionID keeps the CLI's exact existing formula — the internal
		// evidence schema v2 is untouched by this phase (D-10). This is NOT
		// the "sess_<uuid>" MCP-facing handle (Session.ID); the two are
		// deliberately different formats, correlated by a log line.
		SessionID:     fmt.Sprintf("%s-%s", time.Now().UTC().Format(time.RFC3339), uuid.New().String()[:4]),
		SessionSource: evidence.SourceInfo{Type: "live", Server: server},
		CPGVersion:    cpgVersion,

		L7Enabled: args.L7,

		IgnoreProtocols:   args.IgnoreProtocols,
		IgnoreDropReasons: args.IgnoreDropReasons,

		Stdout:  stdout,
		OnFinal: onFinal,
	}
}
