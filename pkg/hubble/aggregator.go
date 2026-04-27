package hubble

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/dropclass"
	"github.com/SoulKyu/cpg/pkg/labels"
	"github.com/SoulKyu/cpg/pkg/policy"
)

// DropEvent is the minimal record for a flow suppressed by the classification gate.
// Consumed by healthWriter (phase 11-02) to accumulate cluster-health.json counters.
type DropEvent struct {
	Reason    flowpb.DropReason
	Class     dropclass.DropClass
	Namespace string
	Workload  string // formatted as labels.WorkloadName(ep.Labels); "_unknown" if nil
	NodeName  string // f.NodeName; "_unknown" if empty
}

// AggKey identifies a flow aggregation bucket by namespace and workload.
type AggKey struct {
	Namespace string
	Workload  string
}

// validIgnoreProtocols is the single source of truth for the values accepted
// by --ignore-protocol. Kept as a set so cmd/cpg validation can reuse it via
// ValidIgnoreProtocols() without import cycles.
var validIgnoreProtocols = map[string]struct{}{
	"tcp":    {},
	"udp":    {},
	"icmpv4": {},
	"icmpv6": {},
	"sctp":   {},
}

// ValidIgnoreProtocols returns the sorted list of L4 protocol names accepted
// by Aggregator.SetIgnoreProtocols. Used by cmd/cpg to render validation
// error messages with a deterministic allowlist.
func ValidIgnoreProtocols() []string {
	out := make([]string, 0, len(validIgnoreProtocols))
	for k := range validIgnoreProtocols {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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

	// ignoreProtocols is the lowercase set of L4 protocol names whose flows
	// must be dropped before bucketing. Empty/nil = no filtering. Populated
	// via SetIgnoreProtocols (PA5).
	ignoreProtocols map[string]struct{}

	// ignoredByProtocol counts flows dropped via --ignore-protocol per
	// protocol name (lowercase). Surfaced via IgnoredByProtocol() and logged
	// in the session summary.
	ignoredByProtocol map[string]uint64

	// infraDrops accumulates per-reason counts for flows suppressed by the
	// classification gate (Infra and Transient classes). Surfaced via
	// InfraDrops() + InfraDropTotal() for SessionStats and --fail-on-infra-drops (phase 13).
	//
	// M-3: single-goroutine ownership during Run() (the aggregator goroutine).
	// All read accessors (InfraDrops, InfraDropTotal) MUST be called only after
	// errgroup g.Wait() returns — same contract as healthWriter.Snapshot().
	// No mutex needed; concurrent access from outside Run() is a caller bug.
	infraDrops map[flowpb.DropReason]uint64

	// healthChDrops counts DropEvents that could not be sent on healthCh
	// because the channel was full (back-pressure). Non-zero means the consumer
	// (Stage 2c) is slower than the aggregation rate; consider increasing the
	// channel buffer or the flush interval.
	// M-3: atomic.Uint64 so the race detector stays clean if HealthChDrops()
	// is called concurrently with Run() (e.g. in tests or diagnostics).
	healthChDrops atomic.Uint64

	// ignoreDropReasons is the uppercase set of DropReason name strings whose
	// flows must be dropped before the protocol filter and classification gate.
	// Populated via SetIgnoreDropReasons (phase 13 FILTER-01).
	ignoreDropReasons map[string]struct{}

	// ignoredByDropReason counts flows dropped via --ignore-drop-reason per
	// reason name (uppercase canonical form). Surfaced via IgnoredByDropReason().
	ignoredByDropReason map[string]uint64
}

// NewAggregator creates a new Aggregator with the given flush interval.
// tracker is accepted as an interface so tests can substitute a stub and the
// aggregator can depend on behavior rather than the concrete UnhandledTracker.
func NewAggregator(interval time.Duration, logger *zap.Logger, tracker flushingTracker) *Aggregator {
	// Wire the warn logger once per process so Classify() can emit deduplicated
	// WARN logs for unrecognized DropReason values (phase 10 CLASSIFY-02).
	dropclass.SetWarnLogger(logger)
	return &Aggregator{
		interval:          interval,
		logger:            logger,
		warnedReserved:    make(map[string]struct{}),
		tracker:           tracker,
		seenWorkloads:       make(map[string]struct{}),
		ignoredByProtocol:   make(map[string]uint64),
		infraDrops:          make(map[flowpb.DropReason]uint64),
		ignoredByDropReason: make(map[string]uint64),
	}
}

// SetIgnoreProtocols configures the lowercase set of L4 protocol names that
// must be dropped before bucketing. nil/empty disables filtering. Defensive:
// inputs are lowercased again even though cmd/cpg already normalizes.
func (a *Aggregator) SetIgnoreProtocols(protos []string) {
	if len(protos) == 0 {
		a.ignoreProtocols = nil
		return
	}
	set := make(map[string]struct{}, len(protos))
	for _, p := range protos {
		set[strings.ToLower(p)] = struct{}{}
	}
	a.ignoreProtocols = set
}

// IgnoredByProtocol returns a copy of the per-protocol drop counter populated
// during Run(). Map iteration order is not stable; sort at the call site for
// deterministic output.
func (a *Aggregator) IgnoredByProtocol() map[string]uint64 {
	out := make(map[string]uint64, len(a.ignoredByProtocol))
	for k, v := range a.ignoredByProtocol {
		out[k] = v
	}
	return out
}

// SetIgnoreDropReasons configures the uppercase set of DropReason name strings
// that must be dropped before the protocol filter and classification gate.
// nil/empty disables filtering. Inputs are uppercased for case-insensitive
// matching against the flowpb.DropReason_name canonical form.
func (a *Aggregator) SetIgnoreDropReasons(reasons []string) {
	if len(reasons) == 0 {
		a.ignoreDropReasons = nil
		return
	}
	set := make(map[string]struct{}, len(reasons))
	for _, r := range reasons {
		set[strings.ToUpper(r)] = struct{}{}
	}
	a.ignoreDropReasons = set
}

// IgnoredByDropReason returns a copy of the per-reason drop counter populated
// during Run(). Keys are uppercase canonical DropReason names.
// Map iteration order is not stable; sort at the call site for deterministic output.
func (a *Aggregator) IgnoredByDropReason() map[string]uint64 {
	out := make(map[string]uint64, len(a.ignoredByDropReason))
	for k, v := range a.ignoredByDropReason {
		out[k] = v
	}
	return out
}

// InfraDrops returns a copy of the per-reason infra/transient drop counter
// populated during Run(). Map iteration order is not stable; sort at the call
// site for deterministic output.
func (a *Aggregator) InfraDrops() map[flowpb.DropReason]uint64 {
	out := make(map[flowpb.DropReason]uint64, len(a.infraDrops))
	for k, v := range a.infraDrops {
		out[k] = v
	}
	return out
}

// HealthChDrops returns the number of DropEvents dropped due to a full
// healthCh channel (back-pressure). Zero when no back-pressure was observed.
func (a *Aggregator) HealthChDrops() uint64 {
	return a.healthChDrops.Load()
}

// InfraDropTotal returns the total number of flows suppressed by the
// classification gate (sum of all infraDrops values).
func (a *Aggregator) InfraDropTotal() uint64 {
	var total uint64
	for _, v := range a.infraDrops {
		total += v
	}
	return total
}

// policyTargetEndpoint returns the endpoint that policy decisions target for a
// given flow direction: INGRESS targets the destination, EGRESS targets the
// source. Returns nil for unknown or unset directions.
//
// This is the single source of truth for the INGRESS/EGRESS direction switch —
// both buildDropEvent and keyFromFlow delegate to this helper (M3 dedup).
func policyTargetEndpoint(f *flowpb.Flow) *flowpb.Endpoint {
	switch f.GetTrafficDirection() {
	case flowpb.TrafficDirection_INGRESS:
		return f.GetDestination()
	case flowpb.TrafficDirection_EGRESS:
		return f.GetSource()
	default:
		return nil
	}
}

// buildDropEvent constructs a DropEvent for a suppressed Infra/Transient flow.
func buildDropEvent(f *flowpb.Flow, class dropclass.DropClass) DropEvent {
	ns, workload := "_unknown", "_unknown"
	if ep := policyTargetEndpoint(f); ep != nil {
		if ep.Namespace != "" {
			ns = ep.Namespace
		}
		if len(ep.Labels) > 0 {
			workload = labels.WorkloadName(ep.Labels)
		}
	}
	node := f.GetNodeName()
	if node == "" {
		node = "_unknown"
	}
	return DropEvent{
		Reason:    f.GetDropReasonDesc(),
		Class:     class,
		Namespace: ns,
		Workload:  workload,
		NodeName:  node,
	}
}

// flowL4ProtoName returns the lowercase protocol name encoded in a flow's L4
// oneof. Returns "" when L4 is nil or carries an unknown protocol — the
// ignore-protocol filter never matches "" so unknown flows fall through to
// the existing nil-L4 / unknown-protocol tracker paths.
func flowL4ProtoName(f *flowpb.Flow) string {
	if f.L4 == nil {
		return ""
	}
	if f.L4.GetTCP() != nil {
		return "tcp"
	}
	if f.L4.GetUDP() != nil {
		return "udp"
	}
	if f.L4.GetICMPv4() != nil {
		return "icmpv4"
	}
	if f.L4.GetICMPv6() != nil {
		return "icmpv6"
	}
	if f.L4.GetSCTP() != nil {
		return "sctp"
	}
	return ""
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
//
// healthCh receives a DropEvent for every Infra or Transient flow suppressed
// by the classification gate. Pass nil to disable health reporting (no-op).
//
// Counter semantics (Pitfall 6 invariant):
//   - flowsSeen: every flow that reaches the classification gate, regardless of class
//     (except Noise and --ignore-protocol drops which are truly excluded)
//   - ignoredByProtocol: flows dropped BEFORE flowsSeen (intentionally excluded by --ignore-protocol)
//   - infraDrops: Infra/Transient flows counted within flowsSeen but suppressed from CNP generation
func (a *Aggregator) Run(ctx context.Context, in <-chan *flowpb.Flow, out chan<- policy.PolicyEvent, healthCh chan<- DropEvent) error {
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
			// FILTER-01: drop flows matching --ignore-drop-reason BEFORE the
			// protocol filter and classification gate. User-explicit exclusion
			// takes precedence. These flows do NOT increment flowsSeen,
			// infraDrops, or reach healthCh.
			// I7: use the `, ok` form so a future enum value with int32(99999)
			// does not match the "" zero-value key in ignoreDropReasons.
			if len(a.ignoreDropReasons) > 0 {
				if name, ok := flowpb.DropReason_name[int32(f.GetDropReasonDesc())]; ok && name != "" {
					if _, drop := a.ignoreDropReasons[name]; drop {
						a.ignoredByDropReason[name]++
						continue
					}
				}
			}
			// PA5: drop ignored protocols BEFORE bucketing so flowsSeen and
			// the unhandled tracker remain consistent — these flows are
			// intentionally excluded, not "unhandled".
			if len(a.ignoreProtocols) > 0 {
				if name := flowL4ProtoName(f); name != "" {
					if _, drop := a.ignoreProtocols[name]; drop {
						a.ignoredByProtocol[name]++
						continue
					}
				}
			}
			// HEALTH-01/05: Classification gate — applies only to flows with an
			// explicit DROPPED verdict and a non-zero drop reason. Zero-value
			// DropReasonDesc on non-DROPPED flows (e.g. forwarded/unknown) must
			// pass through unmodified (PITFALLS Integration Gotchas: always check
			// Verdict == DROPPED before classifying).
			if f.Verdict == flowpb.Verdict_DROPPED && f.GetDropReasonDesc() != flowpb.DropReason_DROP_REASON_UNKNOWN {
				class := dropclass.Classify(f.GetDropReasonDesc())
				switch class {
				case dropclass.DropClassInfra, dropclass.DropClassTransient:
					a.flowsSeen++
					a.infraDrops[f.GetDropReasonDesc()]++
					if healthCh != nil {
						ev := buildDropEvent(f, class)
						select {
						case healthCh <- ev:
						case <-ctx.Done():
							// Context cancelled while trying to send; drain remaining
							// buckets then return to honour the existing ctx.Done flush
							// contract (returns nil, not ctx.Err()).
							a.flush(buckets, out)
							return nil
						default:
							// Consumer is slow; count the drop and keep going.
							a.healthChDrops.Add(1)
						}
					}
					continue
				case dropclass.DropClassNoise:
					continue
				// DropClassPolicy and DropClassUnknown fall through to keyFromFlow.
				}
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
	// policyTargetEndpoint returns nil for both "unknown direction" and
	// "nil endpoint" cases. Re-check direction to preserve distinct tracker
	// reason codes (ReasonUnknownDir vs ReasonNilEndpoint).
	ep := policyTargetEndpoint(f)
	if ep == nil {
		if f.TrafficDirection != flowpb.TrafficDirection_INGRESS && f.TrafficDirection != flowpb.TrafficDirection_EGRESS {
			a.tracker.Track(f, policy.ReasonUnknownDir)
		} else {
			a.tracker.Track(f, policy.ReasonNilEndpoint)
		}
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
