package hubble

import (
	"strconv"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/labels"
	"github.com/SoulKyu/cpg/pkg/policy"
)

// evidenceWriter persists per-rule evidence alongside policy writes. It does
// nothing when evidence capture is disabled for the pipeline run.
type evidenceWriter struct {
	writer  *evidence.Writer
	session evidence.SessionInfo
	logger  *zap.Logger
	seen    map[string]evidence.PolicyRef // workload→ref written at least once this session
}

func newEvidenceWriter(evidenceDir, outputHash string, caps evidence.MergeCaps, session evidence.SessionInfo, logger *zap.Logger) *evidenceWriter {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &evidenceWriter{
		writer:  evidence.NewWriter(evidenceDir, outputHash, caps),
		session: session,
		logger:  logger,
		seen:    make(map[string]evidence.PolicyRef),
	}
}

// handle converts a PolicyEvent's Attribution to evidence.RuleEvidence and
// persists it. Attribution-less events are skipped.
func (ew *evidenceWriter) handle(pe policy.PolicyEvent) {
	if len(pe.Attribution) == 0 {
		return
	}
	ref := evidence.PolicyRef{
		Name:      pe.Policy.Name,
		Namespace: pe.Namespace,
		Workload:  pe.Workload,
	}
	rules := make([]evidence.RuleEvidence, 0, len(pe.Attribution))
	for _, a := range pe.Attribution {
		rules = append(rules, ew.convert(a))
	}
	if err := ew.writer.Write(ref, ew.session, rules); err != nil {
		ew.logger.Warn("writing evidence",
			zap.String("namespace", pe.Namespace),
			zap.String("workload", pe.Workload),
			zap.Error(err),
		)
		return
	}
	ew.seen[pe.Namespace+"/"+pe.Workload] = ref
}

// finalize updates the session's flow counters before the pipeline exits.
// Called once per run. For every workload seen during the session, merge a
// zero-rule update so the session entry reflects final counters.
func (ew *evidenceWriter) finalize(flowsIngested, flowsUnhandled int64) {
	ew.session.FlowsIngested = flowsIngested
	ew.session.FlowsUnhandled = flowsUnhandled
	for _, ref := range ew.seen {
		if err := ew.writer.Write(ref, ew.session, nil); err != nil {
			ew.logger.Warn("updating evidence session counters",
				zap.String("namespace", ref.Namespace),
				zap.String("workload", ref.Workload),
				zap.Error(err),
			)
		}
	}
}

func (ew *evidenceWriter) convert(a policy.RuleAttribution) evidence.RuleEvidence {
	re := evidence.RuleEvidence{
		Key:                  a.Key.String(),
		Direction:            a.Key.Direction,
		Peer:                 convertPeer(a.Key.Peer),
		Port:                 a.Key.Port,
		Protocol:             a.Key.Protocol,
		FlowCount:            a.FlowCount,
		FirstSeen:            a.FirstSeen,
		LastSeen:             a.LastSeen,
		ContributingSessions: []string{ew.session.ID},
	}
	// Populate the L7 attribution ref when the rule key carries an L7
	// discriminator. HTTP wired in Phase 8; DNS wired in Phase 9. Unknown
	// protocols leave re.L7 nil (defensive — keeps malformed Keys off disk).
	if a.Key.L7 != nil {
		switch a.Key.L7.Protocol {
		case "http":
			re.L7 = &evidence.L7Ref{
				Protocol:   "http",
				HTTPMethod: a.Key.L7.HTTPMethod,
				HTTPPath:   a.Key.L7.HTTPPath,
			}
		case "dns":
			re.L7 = &evidence.L7Ref{
				Protocol:     "dns",
				DNSMatchName: a.Key.L7.DNSMatchName,
			}
		}
	}
	for _, f := range a.Samples {
		re.Samples = append(re.Samples, convertSample(f, a.Key))
	}
	return re
}

func convertPeer(p policy.Peer) evidence.PeerRef {
	return evidence.PeerRef{
		Type:   string(p.Type),
		Labels: p.Labels,
		CIDR:   p.CIDR,
		Entity: p.Entity,
	}
}

func convertSample(f *flowpb.Flow, key policy.RuleKey) evidence.FlowSample {
	return evidence.FlowSample{
		Time:       policy.FlowTime(f),
		Src:        endpointFromFlow(f.GetSource()),
		Dst:        endpointFromFlow(f.GetDestination()),
		Port:       portFromKey(key),
		Protocol:   key.Protocol,
		Verdict:    f.GetVerdict().String(),
		DropReason: f.GetDropReasonDesc().String(),
	}
}

func endpointFromFlow(ep *flowpb.Endpoint) evidence.FlowEndpoint {
	if ep == nil {
		return evidence.FlowEndpoint{}
	}
	return evidence.FlowEndpoint{
		Namespace: ep.Namespace,
		Workload:  labels.WorkloadName(ep.Labels),
		Pod:       ep.PodName,
	}
}

func portFromKey(k policy.RuleKey) uint32 {
	p, err := strconv.ParseUint(k.Port, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(p)
}
