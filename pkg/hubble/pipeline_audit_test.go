package hubble

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest/observer"

	"github.com/SoulKyu/cpg/pkg/flowsource"
)

// withAuditFixture is the only new fixture this plan adds. l4OnlyFixture and
// emptyFixture are declared in pipeline_l7_test.go (same package) and are
// reused here as-is — do not redeclare them.
const withAuditFixture = "../../testdata/flows/with_audit.jsonl"

// runReplayPipelineAudit drives the full replay pipeline through
// PipelineConfig.IncludeAudit, returning the output directory and the
// observed log entries for assertion. Mirrors runReplayPipeline
// (pipeline_l7_test.go) but scoped to the AUD-01 surface only — no
// evidence-writer wiring, since none of these tests assert evidence content.
func runReplayPipelineAudit(t *testing.T, fixture string, includeAudit bool) (outDir string, logs *observer.ObservedLogs) {
	t.Helper()
	outDir = t.TempDir()
	logger, observed := newObservedLogger()

	src, err := flowsource.NewFileSource(fixture, logger)
	require.NoError(t, err)

	cfg := PipelineConfig{
		FlushInterval: 50 * time.Millisecond,
		OutputDir:     outDir,
		Logger:        logger,
		IncludeAudit:  includeAudit,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, RunPipelineWithSource(ctx, cfg, src))

	return outDir, observed
}

// TestPipeline_AuditEmpty_FiresWarning asserts that AUD-01 fires exactly once
// when IncludeAudit=true but the fixture carries zero AUDIT-verdict flows
// (l4OnlyFixture: 3 DROPPED, zero AUDIT) — AC-3.
func TestPipeline_AuditEmpty_FiresWarning(t *testing.T) {
	_, logs := runReplayPipelineAudit(t, l4OnlyFixture, true)

	matches := 0
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "no AUDIT-verdict flows observed") {
			matches++
			assert.Contains(t, e.Message, "--include-audit")
			fields := e.ContextMap()
			ws, ok := fields["workloads"].([]interface{})
			if !ok {
				if asStrings, okStr := fields["workloads"].([]string); okStr {
					assert.NotEmpty(t, asStrings)
				}
			} else {
				assert.NotEmpty(t, ws, "workloads must be non-empty")
			}
		}
	}
	assert.Equal(t, 1, matches, "AUD-01 must fire exactly once")
}

// TestPipeline_AuditDisabled_NoWarning asserts that AUD-01 never fires when
// IncludeAudit=false, even though the fixture has zero AUDIT flows.
func TestPipeline_AuditDisabled_NoWarning(t *testing.T) {
	_, logs := runReplayPipelineAudit(t, l4OnlyFixture, false)
	for _, e := range logs.All() {
		assert.NotContains(t, e.Message, "no AUDIT-verdict flows observed",
			"AUD-01 must NOT fire when --include-audit is not set")
	}
}

// TestPipeline_AuditDisabled_AuditFlowsIgnored is the AC-2 end-to-end proof:
// with IncludeAudit=false, a fixture containing an AUDIT flow produces output
// with the AUDIT flow invisible — byte-identical to the DROPPED-only result.
func TestPipeline_AuditDisabled_AuditFlowsIgnored(t *testing.T) {
	outDir, logs := runReplayPipelineAudit(t, withAuditFixture, false)

	// Only the DROPPED flow's policy is generated; the AUDIT flow is invisible.
	yamlPath := filepath.Join(outDir, "production", "api-server.yaml")
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err)
	yaml := string(data)
	assert.Contains(t, yaml, "8080", "the DROPPED flow's port must still generate a rule")
	assert.NotContains(t, yaml, "9090", "the AUDIT flow's port must be absent when flag is unset")

	for _, e := range logs.All() {
		assert.NotContains(t, e.Message, "no AUDIT-verdict flows observed",
			"AUD-01 must NOT fire when --include-audit is not set, regardless of AUDIT flow content")
	}
}

// TestPipeline_AuditEnabled_NoFlows_NoWarning asserts that AUD-01 does not
// fire when IncludeAudit=true but the fixture is empty (zero flows at all).
func TestPipeline_AuditEnabled_NoFlows_NoWarning(t *testing.T) {
	if _, err := os.Stat(emptyFixture); err != nil {
		t.Skipf("empty fixture missing: %v", err)
	}
	_, logs := runReplayPipelineAudit(t, emptyFixture, true)
	for _, e := range logs.All() {
		assert.NotContains(t, e.Message, "no AUDIT-verdict flows observed",
			"AUD-01 must NOT fire when there are no flows at all")
	}
}

// TestPipeline_AuditIngested_GeneratedLikeDropped is the AC-1 proof: with
// IncludeAudit=true, an AUDIT-verdict flow generates a CNP rule exactly like
// a DROPPED flow.
func TestPipeline_AuditIngested_GeneratedLikeDropped(t *testing.T) {
	outDir, _ := runReplayPipelineAudit(t, withAuditFixture, true)

	yamlPath := filepath.Join(outDir, "production", "api-server.yaml")
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err, "policy YAML must exist")
	yaml := string(data)
	assert.Contains(t, yaml, "8080", "DROPPED flow's port")
	assert.Contains(t, yaml, "9090", "AUDIT flow's port must ALSO generate a rule when flag is set")
}
