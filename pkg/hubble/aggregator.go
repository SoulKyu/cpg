package hubble

import (
	"context"
	"fmt"
	"sort"
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
// flushingTracker is the tracker contract the Aggregator depends on: it both
// receives individual flows (policy.FlowTracker) and is asked to emit the
// periodic summary at each aggregation cycle. Keeping the interface local
// avoids exporting Flush from pkg/policy where it has no use.
type flushingTracker interface {
	policy.FlowTracker
	Flush()
}

type Aggregator struct {
	interval       time.Duration
	logger         *zap.Logger
	warnedReserved map[string]struct{}
	tracker        flushingTracker
	maxSamples     int

	// l7Enabled gates the HTTP L7 codegen branch in BuildPolicy. Forwarded
	// from PipelineConfig.L7Enabled via SetL7Enabled before Run().
	l7Enabled bool

	// l7HTTPCount counts flows carrying a non-nil Flow.L7.Http record observed
	// during the session (independent of l7Enabled — counter is diagnostic and
	// powers the VIS-01 empty-records warning in pipeline.Finalize).
	l7HTTPCount uint64
	// l7DNSCount mirrors l7HTTPCount for DNS records. Declared here so the
	// VIS-01 sum check is one-line ready for Phase 9; no Phase 8 callsite
	// increments it.
	l7DNSCount uint64

	// flowsSeen counts every flow that survived keyFromFlow() (i.e. landed in
	// a bucket). Surfaced via FlowsSeen() so SessionStats reports a real
	// number rather than the always-zero placeholder shipped through v1.1
	// (see v1.0 audit BUG-01).
	flowsSeen uint64

	// seenWorkloads records every (namespace/workload) bucket key observed
	// during the session. Surfaced sorted via ObservedWorkloads() to populate
	// the VIS-01 warning's `workloads` field.
	seenWorkloads map[string]struct{}
}

// NewAggregator creates a new Aggregator with the given flush interval.
// tracker is accepted as an interface so tests can substitute a stub and the
// aggregator can depend on behavior rather than the concrete UnhandledTracker.
func NewAggregator(interval time.Duration, logger *zap.Logger, tracker flushingTracker) *Aggregator {
	return &Aggregator{
		interval:       interval,
		logger:         logger,
		warnedReserved: make(map[string]struct{}),
		tracker:        tracker,
		seenWorkloads:  make(map[string]struct{}),
	}
}

// SetL7Enabled toggles the HTTP L7 codegen branch in BuildPolicy. Safe to
// call before Run().
func (a *Aggregator) SetL7Enabled(enabled bool) {
	a.l7Enabled = enabled
}

// L7HTTPCount returns the number of flows with non-nil Flow.L7.Http observed
// across the session. Independent of L7Enabled; used by VIS-01.
func (a *Aggregator) L7HTTPCount() uint64 {
	return a.l7HTTPCount
}

// L7DNSCount mirrors L7HTTPCount for DNS records. Diagnostic counter;
// populated regardless of L7Enabled (Phase 9 wires the increment).
func (a *Aggregator) L7DNSCount() uint64 {
	return a.l7DNSCount
}

// FlowsSeen returns the count of flows that survived keyFromFlow (i.e.
// landed in an aggregation bucket). Used by SessionStats for the VIS-01
// gate (`flows > 0`).
func (a *Aggregator) FlowsSeen() uint64 {
	return a.flowsSeen
}

// ObservedWorkloads returns every (namespace/workload) the aggregator routed
// flows to during the session, sorted lexicographically for deterministic
// VIS-01 warning output.
func (a *Aggregator) ObservedWorkloads() []string {
	out := make([]string, 0, len(a.seenWorkloads))
	for k := range a.seenWorkloads {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SetMaxSamples configures how many per-rule flow samples the policy builder
// retains when producing attribution. Zero disables attribution entirely.
// Safe to call before Run().
func (a *Aggregator) SetMaxSamples(n int) {
	a.maxSamples = n
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
			// Count L7 HTTP and DNS records on every observed flow, regardless
			// of whether the flow makes it into a bucket and regardless of
			// l7Enabled. Both counters power VIS-01's empty-records gate in
			// pipeline.Finalize (which sums them) — they are purely
			// diagnostic and must remain accurate even when L7 codegen is
			// disabled.
			if f.GetL7().GetHttp() != nil {
				a.l7HTTPCount++
			}
			if f.GetL7().GetDns() != nil {
				a.l7DNSCount++
			}
			key, skip := a.keyFromFlow(f)
			if skip {
				continue
			}
			a.flowsSeen++
			a.seenWorkloads[key.Namespace+"/"+key.Workload] = struct{}{}
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
		a.tracker.Track(f, policy.ReasonUnknownDir)
		return AggKey{}, true
	}

	if ep == nil {
		a.tracker.Track(f, policy.ReasonNilEndpoint)
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
			// Track reserved-identity drops so they appear in the periodic
			// Flush summary alongside other unhandled reasons; the Warn above
			// is one-shot per (labels, port, proto, dir) and does not feed
			// the tracker.
			a.tracker.Track(f, policy.ReasonReservedID)
		} else {
			a.tracker.Track(f, policy.ReasonEmptyNamespace)
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
		cnp, attrib := policy.BuildPolicy(key.Namespace, key.Workload, flows, a.tracker, policy.AttributionOptions{
			MaxSamples: a.maxSamples,
			L7Enabled:  a.l7Enabled,
		})
		out <- policy.PolicyEvent{
			Namespace:   key.Namespace,
			Workload:    key.Workload,
			Policy:      cnp,
			Attribution: attrib,
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
	port, proto := protoFields(f)
	if proto == "unknown" {
		return fmt.Sprintf("%s unknown", f.TrafficDirection.String())
	}
	// ICMP reports its "port" as the ICMP type; keep the historical wording.
	if proto == "ICMPv4" || proto == "ICMPv6" {
		return fmt.Sprintf("%s %s type=%s", f.TrafficDirection.String(), proto, port)
	}
	return fmt.Sprintf("%s %s/%s", f.TrafficDirection.String(), proto, port)
}
