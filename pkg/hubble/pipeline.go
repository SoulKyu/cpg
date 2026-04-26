package hubble

import (
	"context"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/flowsource"
	"github.com/SoulKyu/cpg/pkg/output"
	"github.com/SoulKyu/cpg/pkg/policy"
)

// PipelineConfig holds all configuration for the streaming pipeline.
type PipelineConfig struct {
	Server        string
	TLSEnabled    bool
	Timeout       time.Duration
	Namespaces    []string
	AllNamespaces bool
	OutputDir     string
	FlushInterval time.Duration
	Logger        *zap.Logger

	// ClusterPolicies is an optional map of existing cluster policies keyed by
	// policy name. When set (via --cluster-dedup), policies matching the cluster
	// state are skipped. This is a startup snapshot; no periodic refresh.
	ClusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy

	// Dry-run mode: skip all filesystem writes (policies and evidence),
	// optionally emit a unified YAML diff against existing files.
	DryRun      bool
	DryRunDiff  bool
	DryRunColor bool

	// Evidence capture (pkg/evidence).
	EvidenceEnabled bool
	EvidenceDir     string
	OutputHash      string
	EvidenceCaps    evidence.MergeCaps
	SessionID       string
	SessionSource   evidence.SourceInfo
	CPGVersion      string

	// L7Enabled: no-op in v1.2 Phase 7; Phase 8 (HTTP) and Phase 9 (DNS) light up codegen.
	L7Enabled bool

	// IgnoreProtocols is the lowercase, already-validated set of L4 protocol
	// names whose flows must be dropped before bucketing (PA5). Caller
	// (cmd/cpg) is responsible for normalization + allowlist validation.
	IgnoreProtocols []string
}

// SessionStats tracks pipeline metrics for the session summary.
type SessionStats struct {
	StartTime          time.Time
	FlowsSeen          uint64
	PoliciesWritten    uint64
	PoliciesSkipped    uint64
	PoliciesWouldWrite uint64 // dry-run counter
	PoliciesWouldSkip  uint64 // dry-run counter
	LostEvents         uint64
	// L7HTTPCount: number of flows whose L7.Http record was observed during
	// the session. Diagnostic counter; populated regardless of L7Enabled.
	L7HTTPCount uint64
	// L7DNSCount mirrors L7HTTPCount for DNS records. Phase 8 leaves this at
	// 0; Phase 9 wires the increment.
	L7DNSCount uint64
	// IgnoredByProtocol is the per-protocol drop counter populated by the
	// aggregator when --ignore-protocol is set (PA5). Logged via zap.Any in
	// the session summary; map iteration order is not pinned.
	IgnoredByProtocol map[string]uint64
	OutputDir         string
}

// Log outputs the session summary to the logger.
func (s *SessionStats) Log(logger *zap.Logger) {
	logger.Info("session summary",
		zap.Duration("duration", time.Since(s.StartTime)),
		zap.Uint64("flows_seen", s.FlowsSeen),
		zap.Uint64("policies_written", s.PoliciesWritten),
		zap.Uint64("policies_skipped", s.PoliciesSkipped),
		zap.Uint64("policies_would_write", s.PoliciesWouldWrite),
		zap.Uint64("policies_would_skip", s.PoliciesWouldSkip),
		zap.Uint64("lost_events", s.LostEvents),
		zap.Uint64("l7_http_count", s.L7HTTPCount),
		zap.Uint64("l7_dns_count", s.L7DNSCount),
		zap.Any("ignored_by_protocol", s.IgnoredByProtocol),
		zap.String("output_dir", s.OutputDir),
	)
}

// RunPipeline connects to Hubble Relay and runs the streaming pipeline.
// It orchestrates three goroutines via errgroup:
//  1. Aggregator: accumulates flows and builds policies
//  2. Writer: writes policies to disk
//  3. LostEvents monitor: aggregates and warns about lost events
func RunPipeline(ctx context.Context, cfg PipelineConfig) error {
	client := NewClient(cfg.Server, cfg.TLSEnabled, cfg.Timeout, cfg.Logger)
	return RunPipelineWithSource(ctx, cfg, client)
}

// RunPipelineWithSource runs the pipeline with an injectable flow source.
// This enables testing without a real gRPC connection.
func RunPipelineWithSource(ctx context.Context, cfg PipelineConfig, source flowsource.FlowSource) error {
	flows, lostEvents, err := source.StreamDroppedFlows(ctx, cfg.Namespaces, cfg.AllNamespaces)
	if err != nil {
		return err
	}

	cfg.Logger.Info("connected to Hubble Relay, streaming dropped flows",
		zap.String("server", cfg.Server),
		zap.Strings("namespaces", cfg.Namespaces),
		zap.Bool("all-namespaces", cfg.AllNamespaces),
	)

	tracker := NewUnhandledTracker(cfg.Logger)
	agg := NewAggregator(cfg.FlushInterval, cfg.Logger, tracker)
	agg.SetL7Enabled(cfg.L7Enabled)
	agg.SetIgnoreProtocols(cfg.IgnoreProtocols)
	if cfg.EvidenceEnabled {
		agg.SetMaxSamples(cfg.EvidenceCaps.MaxSamples)
	}
	writer := output.NewWriter(cfg.OutputDir, cfg.Logger)
	stats := &SessionStats{
		StartTime: time.Now(),
		OutputDir: cfg.OutputDir,
	}

	policies := make(chan policy.PolicyEvent, 64)
	policyCh := make(chan policy.PolicyEvent, 64)
	evidenceCh := make(chan policy.PolicyEvent, 64)

	var ew *evidenceWriter
	if cfg.EvidenceEnabled && !cfg.DryRun {
		session := evidence.SessionInfo{
			ID:         cfg.SessionID,
			StartedAt:  stats.StartTime,
			CPGVersion: cfg.CPGVersion,
			Source:     cfg.SessionSource,
		}
		ew = newEvidenceWriter(cfg.EvidenceDir, cfg.OutputHash, cfg.EvidenceCaps, session, cfg.Logger)
	}

	g, gctx := errgroup.WithContext(ctx)

	// Stage 1: Aggregate flows and build policies.
	g.Go(func() error {
		return agg.Run(gctx, flows, policies)
	})

	// Stage 1b: Fan out PolicyEvent to the policy writer and evidence writer.
	// Neither consumer may block the other.
	g.Go(func() error {
		defer close(policyCh)
		defer close(evidenceCh)
		for pe := range policies {
			policyCh <- pe
			evidenceCh <- pe
		}
		return nil
	})

	// Stage 2: Write policies to disk with dedup checks.
	g.Go(func() error {
		pw := newPolicyWriter(writer, cfg.ClusterPolicies, stats, cfg.Logger)
		pw.dryRun = cfg.DryRun
		pw.dryRunDiff = cfg.DryRunDiff
		pw.dryRunColor = cfg.DryRunColor
		for pe := range policyCh {
			pw.handle(pe)
		}
		return nil
	})

	// Stage 2b: Persist evidence (drained no-op when disabled or dry-run).
	g.Go(func() error {
		for pe := range evidenceCh {
			if ew != nil {
				ew.handle(pe)
			}
		}
		return nil
	})

	// Stage 3: Monitor lost events
	g.Go(func() error {
		return monitorLostEvents(gctx, lostEvents, cfg.Logger)
	})

	err = g.Wait()
	// Final flush for any unhandled flows tracked after the last aggregation cycle
	tracker.Flush()

	// Surface aggregator-side counters on SessionStats so the summary log and
	// VIS-01 gate share the same numbers. This also fixes v1.0 BUG-01 for
	// flows_seen which had been stuck at 0 since v1.0.
	stats.FlowsSeen = agg.FlowsSeen()
	stats.L7HTTPCount = agg.L7HTTPCount()
	stats.L7DNSCount = agg.L7DNSCount()
	stats.IgnoredByProtocol = agg.IgnoredByProtocol()

	// VIS-01: passive empty-L7-records detection. Single warning per pipeline
	// run, fired only when --l7 was requested AND at least one flow was
	// observed AND zero L7 records (HTTP + DNS) materialized. The DNS branch
	// is wired here in advance of Phase 9 — agg.L7DNSCount() returns 0 in
	// Phase 8, so the gate degrades gracefully.
	if cfg.L7Enabled && stats.FlowsSeen > 0 && agg.L7HTTPCount()+agg.L7DNSCount() == 0 {
		cfg.Logger.Warn("--l7 set but no L7 records observed in window",
			zap.Strings("workloads", agg.ObservedWorkloads()),
			zap.Uint64("flows", stats.FlowsSeen),
			zap.String("hint", "see README L7 prerequisites: #l7-prerequisites"),
		)
	}

	if ew != nil {
		ew.session.EndedAt = time.Now()
		ew.finalize(int64(stats.FlowsSeen), int64(stats.LostEvents))
	}
	stats.Log(cfg.Logger)
	return err
}
