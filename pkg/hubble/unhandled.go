package hubble

import (
	"fmt"
	"sync"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"go.uber.org/zap"
)

// UnhandledTracker tracks flows that CPG cannot convert to policy rules.
// It deduplicates by flow identity + reason, emits DEBUG logs for first
// occurrence of each unique flow, and emits periodic INFO summaries.
type UnhandledTracker struct {
	mu       sync.Mutex
	seen     map[string]struct{}
	counters map[string]int64
	logger   *zap.Logger
}

// NewUnhandledTracker creates a new tracker with the given logger.
func NewUnhandledTracker(logger *zap.Logger) *UnhandledTracker {
	return &UnhandledTracker{
		seen:     make(map[string]struct{}),
		counters: make(map[string]int64),
		logger:   logger,
	}
}

// Track records an unhandled flow. On first occurrence (unique src+dst+port+proto+reason),
// it emits a DEBUG log with flow details and destination labels. It always increments the
// reason counter for the periodic summary.
func (t *UnhandledTracker) Track(f *flowpb.Flow, reason string) {
	key := t.dedupKey(f, reason)

	t.mu.Lock()
	defer t.mu.Unlock()

	t.counters[reason]++

	if _, seen := t.seen[key]; seen {
		return
	}
	t.seen[key] = struct{}{}

	src, dst, port, proto, dstLabels := t.extractFields(f)
	t.logger.Debug("unhandled flow",
		zap.String("src", src),
		zap.String("dst", dst),
		zap.String("port", port),
		zap.String("proto", proto),
		zap.String("reason", reason),
		zap.Strings("dst_labels", dstLabels),
	)
}

// Flush emits a structured INFO log summarizing unhandled flow counts by reason,
// then resets the counters. The seen map is NOT reset — each unique flow only
// produces one DEBUG log for the entire process lifetime.
// If all counters are zero (no new tracks since last flush), no log is emitted.
func (t *UnhandledTracker) Flush() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Collect non-zero counters
	var fields []zap.Field
	var total int64
	for reason, count := range t.counters {
		if count > 0 {
			fields = append(fields, zap.Int64(reason, count))
			total += count
		}
	}

	if total == 0 {
		return
	}

	t.logger.Info("unhandled flows summary", fields...)

	// Reset counters but keep seen map
	for k := range t.counters {
		t.counters[k] = 0
	}
}

// dedupKey builds a unique key from the flow's identity fields and reason.
func (t *UnhandledTracker) dedupKey(f *flowpb.Flow, reason string) string {
	src := endpointID(f.Source)
	dst := endpointID(f.Destination)
	port, proto := protoFields(f)
	dir := f.TrafficDirection.String()
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s", src, dst, port, proto, dir, reason)
}

// extractFields pulls human-readable fields from a flow for DEBUG logging.
func (t *UnhandledTracker) extractFields(f *flowpb.Flow) (src, dst, port, proto string, dstLabels []string) {
	src = endpointID(f.Source)
	dst = endpointID(f.Destination)
	port, proto = protoFields(f)
	if f.Destination != nil {
		dstLabels = f.Destination.Labels
	}
	return
}

// endpointID returns "namespace/workload" or a label-based fallback for an endpoint.
func endpointID(ep *flowpb.Endpoint) string {
	if ep == nil {
		return "<nil>"
	}
	if ep.Namespace != "" {
		workload := workloadFromLabels(ep.Labels)
		if workload != "" {
			return ep.Namespace + "/" + workload
		}
		return ep.Namespace + "/<unknown>"
	}
	// No namespace -- use first label as identifier
	if len(ep.Labels) > 0 {
		return ep.Labels[0]
	}
	return "<unknown>"
}

// workloadFromLabels extracts a workload name from endpoint labels.
// Mirrors the logic in pkg/labels.WorkloadName but avoids a cross-package dependency.
func workloadFromLabels(lbls []string) string {
	for _, l := range lbls {
		if len(l) > 27 && l[:27] == "k8s:app.kubernetes.io/name=" {
			return l[27:]
		}
	}
	for _, l := range lbls {
		if len(l) > 8 && l[:8] == "k8s:app=" {
			return l[8:]
		}
	}
	return ""
}

// protoFields extracts port and protocol from a flow's L4 layer.
func protoFields(f *flowpb.Flow) (port, proto string) {
	if f.L4 == nil {
		return "0", "unknown"
	}
	if tcp := f.L4.GetTCP(); tcp != nil {
		return fmt.Sprintf("%d", tcp.DestinationPort), "TCP"
	}
	if udp := f.L4.GetUDP(); udp != nil {
		return fmt.Sprintf("%d", udp.DestinationPort), "UDP"
	}
	if icmp4 := f.L4.GetICMPv4(); icmp4 != nil {
		return fmt.Sprintf("%d", icmp4.Type), "ICMPv4"
	}
	if icmp6 := f.L4.GetICMPv6(); icmp6 != nil {
		return fmt.Sprintf("%d", icmp6.Type), "ICMPv6"
	}
	return "0", "unknown"
}
