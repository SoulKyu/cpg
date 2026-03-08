package hubble

import (
	"context"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"go.uber.org/zap"

	"github.com/gule/cpg/pkg/labels"
	"github.com/gule/cpg/pkg/policy"
)

// AggKey identifies a flow aggregation bucket by namespace and workload.
type AggKey struct {
	Namespace string
	Workload  string
}

// Aggregator accumulates flows by (namespace, workload) and flushes them as
// PolicyEvents on a configurable ticker interval. It also flushes remaining
// flows when the input channel closes or the context is cancelled.
type Aggregator struct {
	interval time.Duration
	logger   *zap.Logger
}

// NewAggregator creates a new Aggregator with the given flush interval.
func NewAggregator(interval time.Duration, logger *zap.Logger) *Aggregator {
	return &Aggregator{
		interval: interval,
		logger:   logger,
	}
}

// Run reads flows from in, accumulates them by AggKey, and sends PolicyEvents
// to out on each ticker flush, channel close, or context cancellation.
// It closes the out channel when it returns.
func (a *Aggregator) Run(ctx context.Context, in <-chan *flowpb.Flow, out chan<- policy.PolicyEvent) error {
	defer close(out)

	buckets := make(map[AggKey][]*flowpb.Flow)
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case f, ok := <-in:
			if !ok {
				a.flush(buckets, out)
				return nil
			}
			key, skip := a.keyFromFlow(f)
			if skip {
				continue
			}
			buckets[key] = append(buckets[key], f)

		case <-ticker.C:
			a.flush(buckets, out)

		case <-ctx.Done():
			a.flush(buckets, out)
			return nil
		}
	}
}

// keyFromFlow derives the aggregation key from a flow.
// For INGRESS flows, the destination endpoint is the policy target.
// For EGRESS flows, the source endpoint is the policy target.
// Returns skip=true if the target endpoint has an empty namespace.
func (a *Aggregator) keyFromFlow(f *flowpb.Flow) (key AggKey, skip bool) {
	var ep *flowpb.Endpoint
	switch f.TrafficDirection {
	case flowpb.TrafficDirection_INGRESS:
		ep = f.Destination
	case flowpb.TrafficDirection_EGRESS:
		ep = f.Source
	default:
		ep = f.Destination
	}

	if ep == nil {
		a.logger.Debug("skipping flow with nil endpoint")
		return AggKey{}, true
	}

	if ep.Namespace == "" {
		a.logger.Debug("skipping flow with empty namespace",
			zap.String("workload", labels.WorkloadName(ep.Labels)),
		)
		return AggKey{}, true
	}

	return AggKey{
		Namespace: ep.Namespace,
		Workload:  labels.WorkloadName(ep.Labels),
	}, false
}

// flush sends PolicyEvents for all accumulated buckets and clears them.
func (a *Aggregator) flush(buckets map[AggKey][]*flowpb.Flow, out chan<- policy.PolicyEvent) {
	for key, flows := range buckets {
		cnp := policy.BuildPolicy(key.Namespace, key.Workload, flows)
		out <- policy.PolicyEvent{
			Namespace: key.Namespace,
			Workload:  key.Workload,
			Policy:    cnp,
		}
	}
	for k := range buckets {
		delete(buckets, k)
	}
}

// monitorLostEvents accumulates LostEvents and logs an aggregated warning
// every 30 seconds instead of per-event to avoid log spam. On context
// cancellation, logs a final summary if any events were lost.
func monitorLostEvents(ctx context.Context, ch <-chan *flowpb.LostEvent, logger *zap.Logger) error {
	var totalLost uint64
	var periodLost uint64

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case le, ok := <-ch:
			if !ok {
				if totalLost > 0 {
					logger.Warn("total hubble events lost during session",
						zap.Uint64("total_lost", totalLost),
					)
				}
				return nil
			}
			periodLost += le.NumEventsLost
			totalLost += le.NumEventsLost

		case <-ticker.C:
			if periodLost > 0 {
				logger.Warn("hubble events lost -- consider increasing ring buffer size",
					zap.Uint64("lost_this_period", periodLost),
					zap.Uint64("total_lost", totalLost),
				)
				periodLost = 0
			}

		case <-ctx.Done():
			if totalLost > 0 {
				logger.Warn("total hubble events lost during session",
					zap.Uint64("total_lost", totalLost),
				)
			}
			return nil
		}
	}
}
