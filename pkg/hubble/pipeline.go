package hubble

import (
	"context"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

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
}

// SessionStats tracks pipeline metrics for the session summary.
type SessionStats struct {
	StartTime       time.Time
	FlowsSeen       uint64
	PoliciesWritten uint64
	PoliciesSkipped uint64
	LostEvents      uint64
	OutputDir       string
}

// Log outputs the session summary to the logger.
func (s *SessionStats) Log(logger *zap.Logger) {
	logger.Info("session summary",
		zap.Duration("duration", time.Since(s.StartTime)),
		zap.Uint64("flows_seen", s.FlowsSeen),
		zap.Uint64("policies_written", s.PoliciesWritten),
		zap.Uint64("policies_skipped", s.PoliciesSkipped),
		zap.Uint64("lost_events", s.LostEvents),
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
	writer := output.NewWriter(cfg.OutputDir, cfg.Logger)
	stats := &SessionStats{
		StartTime: time.Now(),
		OutputDir: cfg.OutputDir,
	}

	policies := make(chan policy.PolicyEvent, 64)

	g, gctx := errgroup.WithContext(ctx)

	// Stage 1: Aggregate flows and build policies
	g.Go(func() error {
		return agg.Run(gctx, flows, policies)
	})

	// Stage 2: Write policies to disk with dedup checks
	g.Go(func() error {
		pw := newPolicyWriter(writer, cfg.ClusterPolicies, stats, cfg.Logger)
		for pe := range policies {
			pw.handle(pe)
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
	stats.Log(cfg.Logger)
	return err
}
