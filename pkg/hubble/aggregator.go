package hubble

import (
	"context"
	"fmt"
	"strings"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/labels"
	"github.com/SoulKyu/cpg/pkg/policy"
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
	interval       time.Duration
	logger         *zap.Logger
	warnedReserved map[string]struct{}
	tracker        *UnhandledTracker
}

// NewAggregator creates a new Aggregator with the given flush interval.
func NewAggregator(interval time.Duration, logger *zap.Logger, tracker *UnhandledTracker) *Aggregator {
	return &Aggregator{
		interval:       interval,
		logger:         logger,
		warnedReserved: make(map[string]struct{}),
		tracker:        tracker,
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
		a.tracker.Track(f, string(ReasonUnknownDir))
		return AggKey{}, true
	}

	if ep == nil {
		a.tracker.Track(f, string(ReasonNilEndpoint))
		return AggKey{}, true
	}

	if ep.Namespace == "" {
		if isActionableReserved(ep.Labels) {
			warnKey := reservedWarnKey(ep.Labels, f)
			if _, seen := a.warnedReserved[warnKey]; !seen {
				a.warnedReserved[warnKey] = struct{}{}
				a.logger.Warn("dropped flow targets a reserved identity — cpg generates namespace-scoped CiliumNetworkPolicy and cannot handle reserved endpoints; use a CiliumClusterwideNetworkPolicy instead",
					zap.Strings("labels", ep.Labels),
					zap.String("summary", flowSummary(f)),
				)
			}
		} else {
			a.tracker.Track(f, "empty_namespace")
		}
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
		cnp := policy.BuildPolicy(key.Namespace, key.Workload, flows, a.tracker)
		out <- policy.PolicyEvent{
			Namespace: key.Namespace,
			Workload:  key.Workload,
			Policy:    cnp,
		}
	}
	for k := range buckets {
		delete(buckets, k)
	}
	// Flush unhandled flow summary after each aggregation cycle
	a.tracker.Flush()
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

// reservedWarnKey builds a dedup key from reserved labels and traffic direction
// so the same warning is only logged once per identity+direction combination.
func reservedWarnKey(epLabels []string, f *flowpb.Flow) string {
	var reserved string
	for _, l := range epLabels {
		if strings.HasPrefix(l, "reserved:") {
			reserved = l
			break
		}
	}
	return fmt.Sprintf("%s/%s", reserved, f.TrafficDirection)
}

// actionableReserved lists reserved identities where a CiliumClusterwideNetworkPolicy
// can actually fix the drop. "reserved:unknown" is not actionable — it represents
// unidentified traffic (pre-identity resolution, non-IP protocols like ARP).
var actionableReserved = map[string]struct{}{
	"reserved:health":         {},
	"reserved:host":           {},
	"reserved:remote-node":    {},
	"reserved:kube-apiserver": {},
	"reserved:ingress":        {},
}

// isActionableReserved returns true if the endpoint is a reserved identity
// that can be addressed with a CiliumClusterwideNetworkPolicy.
func isActionableReserved(epLabels []string) bool {
	for _, l := range epLabels {
		if _, ok := actionableReserved[l]; ok {
			return true
		}
	}
	return false
}

// flowSummary returns a short human-readable description of a flow
// for use in log messages.
func flowSummary(f *flowpb.Flow) string {
	dir := f.TrafficDirection.String()
	proto := "unknown"
	if f.L4 != nil {
		if tcp := f.L4.GetTCP(); tcp != nil {
			proto = fmt.Sprintf("TCP/%d", tcp.DestinationPort)
		} else if udp := f.L4.GetUDP(); udp != nil {
			proto = fmt.Sprintf("UDP/%d", udp.DestinationPort)
		} else if icmp4 := f.L4.GetICMPv4(); icmp4 != nil {
			proto = fmt.Sprintf("ICMPv4 type=%d", icmp4.Type)
		} else if icmp6 := f.L4.GetICMPv6(); icmp6 != nil {
			proto = fmt.Sprintf("ICMPv6 type=%d", icmp6.Type)
		}
	}
	return fmt.Sprintf("%s %s", dir, proto)
}
