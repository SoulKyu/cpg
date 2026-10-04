package hubble

// TestL7NoCollapseByteIdentity is the ROADMAP SC2 / L7GEN-01 criterion-2
// regression: with L7Enabled and every plan-25 collapse field left at their
// zero values, the pipeline's emitted YAML tree must be byte-identical to
// the goldens produced by the PRE-PHASE-25 build (the additive
// CollapseL7Paths/MinFlowsPerL7Rule config fields must never perturb
// default-path emission — plan 25 threat T-25-04).
//
// Golden provenance (backstop — a reviewer must be able to re-derive the
// goldens). The plan 25-02 pinned procedure names the v1.6.0 tag; executed
// verification showed tag v1.6.0 predates the L7 work entirely (no --l7
// flag, no fixtures), so the plan's documented fallback applied: the
// goldens were generated from a worktree of the PRE-PHASE-25 commit
// e4159a8 (parent of the first phase-25 commit 9ef0028), which is exactly
// the "pre-phase golden files" source ROADMAP SC2 language pins:
//
//	git worktree add /tmp/cpg-prephase e4159a8
//	(cd /tmp/cpg-prephase && go build -o /tmp/cpg-prephase/cpg ./cmd/cpg)
//	/tmp/cpg-prephase/cpg replay testdata/flows/l7_http.jsonl --l7 \
//	  --output-dir /tmp/goldens/l7_http --evidence-dir /tmp/goldens/ev_l7_http
//	/tmp/cpg-prephase/cpg replay testdata/flows/mixed_tcp_icmp.jsonl --l7 \
//	  --output-dir /tmp/goldens/mixed --evidence-dir /tmp/goldens/ev_mixed
//	cp /tmp/goldens/l7_http/production/*.yaml \
//	  testdata/golden/l7-collapse-regression/l7_http/production/
//	cp /tmp/goldens/mixed/production/*.yaml \
//	  testdata/golden/l7-collapse-regression/mixed_tcp_icmp/production/
//	git worktree remove /tmp/cpg-prephase
//
// NEVER edit a golden to make this test pass — a byte diff is a regression
// in the emission code, not in the golden (plan 25-02 prohibition).
import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/flowsource"
)

const l7CollapseGoldenRoot = "../../testdata/golden/l7-collapse-regression"

// runNoCollapsePipeline replays fixture with L7 codegen enabled and ALL
// collapse fields at their zero values (runReplayPipeline's shape — the
// evidence dir is deliberately left unused; emission bytes are the only
// concern here).
func runNoCollapsePipeline(t *testing.T, fixture string) string {
	t.Helper()
	outDir := t.TempDir()
	logger, _ := newObservedLogger()

	src, err := flowsource.NewFileSource(fixture, logger)
	require.NoError(t, err)

	cfg := PipelineConfig{
		FlushInterval: 50 * time.Millisecond,
		OutputDir:     outDir,
		Logger:        logger,
		L7Enabled:     true,
		// CollapseL7Paths / MinFlowsPerL7Rule deliberately zero: this is
		// the default-path byte-identity proof.
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, RunPipelineWithSource(ctx, cfg, src))

	return outDir
}

// emittedYAMLTree returns the sorted relative paths of every emitted .yaml
// under root (the <ns>/<workload>.yaml tree the writer produces).
func emittedYAMLTree(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		paths = append(paths, rel)
		return nil
	})
	require.NoError(t, err)
	return paths
}

func TestL7NoCollapseByteIdentity(t *testing.T) {
	// Per-fixture golden DIRECTORY + pinned emission-count asymmetry:
	// l7_http emits exactly ONE policy (production/api-server.yaml);
	// mixed_tcp_icmp replayed WITHOUT --ignore-protocol emits TWO —
	// aggregator policyTargetEndpoint routes INGRESS to the destination
	// (server) and EGRESS to the source (client), both under production/.
	cases := []struct {
		fixture      string
		goldenDir    string
		wantPolicies int
	}{
		{fixture: "../../testdata/flows/l7_http.jsonl", goldenDir: l7CollapseGoldenRoot + "/l7_http", wantPolicies: 1},
		{fixture: "../../testdata/flows/mixed_tcp_icmp.jsonl", goldenDir: l7CollapseGoldenRoot + "/mixed_tcp_icmp", wantPolicies: 2},
	}

	for _, tc := range cases {
		t.Run(filepath.Base(tc.fixture), func(t *testing.T) {
			outDir := runNoCollapsePipeline(t, tc.fixture)

			emitted := emittedYAMLTree(t, outDir)
			require.Len(t, emitted, tc.wantPolicies,
				"emission count pinned for %s: got %v", tc.fixture, emitted)

			golden := emittedYAMLTree(t, tc.goldenDir)
			require.ElementsMatch(t, golden, emitted,
				"emitted tree must match the golden tree file-for-file (fixture %s)", tc.fixture)

			for _, rel := range emitted {
				want, err := os.ReadFile(filepath.Join(tc.goldenDir, rel))
				require.NoError(t, err, "reading golden %s", rel)
				got, err := os.ReadFile(filepath.Join(outDir, rel))
				require.NoError(t, err, "reading emitted %s", rel)
				require.Equal(t, string(want), string(got),
					"default-path emission must stay byte-identical to the pre-phase-25 build (%s)", rel)
			}
		})
	}
}
