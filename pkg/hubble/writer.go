package hubble

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"go.uber.org/zap"
	sigyaml "sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/diff"
	"github.com/SoulKyu/cpg/pkg/output"
	"github.com/SoulKyu/cpg/pkg/policy"
)

// policyWriter serializes policy events from the aggregator, skipping those
// equivalent to a cluster snapshot (opt-in via --cluster-dedup) or to the
// last version this process wrote for the same workload. Extracted from
// RunPipelineWithSource so the dedup logic can be reasoned about independently
// from stream orchestration.
type policyWriter struct {
	writer          *output.Writer
	clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy
	written         map[string]*ciliumv2.CiliumNetworkPolicy
	stats           *SessionStats
	logger          *zap.Logger

	// Dry-run knobs. When dryRun is true, handle() logs and optionally prints
	// a unified diff instead of writing to disk.
	dryRun      bool
	dryRunDiff  bool
	dryRunColor bool
	diffOut     io.Writer // test injection; defaults to os.Stdout when nil
}

func newPolicyWriter(w *output.Writer, clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy, stats *SessionStats, logger *zap.Logger) *policyWriter {
	return &policyWriter{
		writer:          w,
		clusterPolicies: clusterPolicies,
		written:         make(map[string]*ciliumv2.CiliumNetworkPolicy),
		stats:           stats,
		logger:          logger,
	}
}

// handle decides whether to write pe and performs the write when necessary,
// updating the stats and written-history side state. It never returns an
// error: individual write failures are logged but do not kill the pipeline.
func (w *policyWriter) handle(pe policy.PolicyEvent) {
	if w.skipForClusterMatch(pe) {
		w.bumpSkip()
		return
	}

	dedupKey := fmt.Sprintf("%s/%s", pe.Namespace, pe.Workload)
	if w.skipForCrossFlushMatch(pe, dedupKey) {
		w.bumpSkip()
		return
	}

	if w.dryRun {
		w.dryRunEmit(pe)
		w.stats.PoliciesWouldWrite++
		w.written[dedupKey] = pe.Policy
		return
	}

	if err := w.writer.Write(pe); err != nil {
		w.logger.Error("failed to write policy",
			zap.String("namespace", pe.Namespace),
			zap.String("workload", pe.Workload),
			zap.Error(err),
		)
		return
	}
	w.stats.PoliciesWritten++
	w.written[dedupKey] = pe.Policy
}

func (w *policyWriter) bumpSkip() {
	if w.dryRun {
		w.stats.PoliciesWouldSkip++
	} else {
		w.stats.PoliciesSkipped++
	}
}

func (w *policyWriter) dryRunEmit(pe policy.PolicyEvent) {
	w.logger.Info("would write policy",
		zap.String("namespace", pe.Namespace),
		zap.String("workload", pe.Workload),
	)
	if !w.dryRunDiff {
		return
	}

	rendered, err := sigyaml.Marshal(pe.Policy)
	if err != nil {
		w.logger.Warn("dry-run render failed", zap.Error(err))
		return
	}

	existing, err := w.writer.ReadExisting(pe.Namespace, pe.Workload)
	if err != nil {
		existing = nil
	}

	target := filepath.Join(w.writer.OutputDir(), pe.Namespace, pe.Workload+".yaml")
	d, err := diff.UnifiedYAML(target, target+" (in memory)", existing, rendered, w.dryRunColor)
	if err != nil {
		w.logger.Warn("diff failed", zap.Error(err))
		return
	}
	if d == "" {
		return
	}
	out := w.diffOut
	if out == nil {
		out = os.Stdout
	}
	_, _ = io.WriteString(out, d)
}

func (w *policyWriter) skipForClusterMatch(pe policy.PolicyEvent) bool {
	if w.clusterPolicies == nil {
		return false
	}
	existing, ok := w.clusterPolicies[policy.PolicyName(pe.Workload)]
	if !ok {
		return false
	}
	return w.equivalent(existing, pe, "policy already exists in cluster, skipping")
}

func (w *policyWriter) skipForCrossFlushMatch(pe policy.PolicyEvent, dedupKey string) bool {
	lastWritten, ok := w.written[dedupKey]
	if !ok {
		return false
	}
	return w.equivalent(lastWritten, pe, "policy unchanged since last flush, skipping")
}

// equivalent returns true when a is equivalent to pe.Policy. Equivalence-check
// errors log a warning and return false so the pipeline writes the policy to
// be safe rather than silently dropping it.
func (w *policyWriter) equivalent(a *ciliumv2.CiliumNetworkPolicy, pe policy.PolicyEvent, skipMsg string) bool {
	equiv, err := policy.PoliciesEquivalent(a, pe.Policy)
	if err != nil {
		w.logger.Warn("policy equivalence check failed; writing to be safe",
			zap.String("namespace", pe.Namespace),
			zap.String("workload", pe.Workload),
			zap.Error(err),
		)
		return false
	}
	if equiv {
		w.logger.Debug(skipMsg,
			zap.String("namespace", pe.Namespace),
			zap.String("workload", pe.Workload),
		)
	}
	return equiv
}
