package session

import (
	"path/filepath"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

// SessionPaths is the pipeline's on-disk output-layout contract (WR-04): the
// policies/evidence/cluster-health.json paths derived from a session's
// tmpDir. Before this type existed, the formula
//
//	outputHash  = evidence.HashOutputDir(join(tmpDir, "policies"))
//	evidenceDir = join(tmpDir, "evidence")
//	healthPath  = join(evidenceDir, outputHash, "cluster-health.json")
//
// was hand-copied at 5 sites: buildPipelineConfig and Manager.Stop (the
// writer side, this package) plus get_policy/get_cluster_health,
// get_evidence, and list_dropped_flows (the 3 cmd/cpg query-tool readers).
// It was consistent everywhere, but exactly the kind of invariant that
// breaks silently: change the "policies" subdir name or the hash input in
// one place and the readers keep looking where the writer no longer
// writes, with no compile error and only a subtle "no data" symptom.
// DeriveSessionPaths is now the one place that formula is defined — every
// site above calls it instead of re-deriving the formula locally.
type SessionPaths struct {
	// OutputDir is where generated CiliumNetworkPolicy YAML files live:
	// <tmpDir>/policies.
	OutputDir string
	// EvidenceDir is where per-(namespace,workload) evidence JSON and
	// cluster-health.json live: <tmpDir>/evidence.
	EvidenceDir string
	// OutputHash is evidence.HashOutputDir(OutputDir) — the directory
	// component both EvidenceDir's namespace/workload subtree and
	// ClusterHealthPath nest under.
	OutputHash string
	// ClusterHealthPath is the finalized cluster-health report path:
	// <EvidenceDir>/<OutputHash>/cluster-health.json.
	ClusterHealthPath string
}

// DeriveSessionPaths computes SessionPaths for tmpDir. Pure and
// side-effect-free — path/hash arithmetic only, no filesystem access — so
// it is safe to call from the writer side (buildPipelineConfig, before the
// pipeline has written anything) as well as every reader side (query-tool
// handlers, typically called well after).
func DeriveSessionPaths(tmpDir string) SessionPaths {
	outputDir := filepath.Join(tmpDir, "policies")
	evidenceDir := filepath.Join(tmpDir, "evidence")
	outputHash := evidence.HashOutputDir(outputDir)
	return SessionPaths{
		OutputDir:         outputDir,
		EvidenceDir:       evidenceDir,
		OutputHash:        outputHash,
		ClusterHealthPath: filepath.Join(evidenceDir, outputHash, "cluster-health.json"),
	}
}
