package hubble

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/dropclass"
)

// TestReadClusterHealth_HappyPath writes a well-formed report matching
// health_writer.go's exact write format (json.MarshalIndent) and asserts the
// round-tripped Drops/Session -- including the per-reason Remediation URL --
// match what was written.
func TestReadClusterHealth_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cluster-health.json")

	started := time.Now().Add(-5 * time.Minute).UTC().Truncate(time.Second)
	ended := time.Now().UTC().Truncate(time.Second)

	want := ClusterHealthReport{
		SchemaVersion:     1,
		ClassifierVersion: dropclass.ClassifierVersion,
		Session: HealthSession{
			Started:        started,
			Ended:          ended,
			FlowsSeen:      42,
			InfraDropTotal: 3,
		},
		Drops: []HealthDropJSON{
			{
				Reason:      "CT_MAP_INSERTION_FAILED",
				Class:       dropclass.DropClassInfra.String(),
				Count:       3,
				Remediation: dropclass.RemediationHint(flowpb.DropReason_CT_MAP_INSERTION_FAILED),
				ByNode:      map[string]uint64{"node-1": 3},
				ByWorkload:  map[string]uint64{"prod/adserver": 3},
			},
		},
	}
	require.NotEmpty(t, want.Drops[0].Remediation, "fixture must carry a real remediation URL to prove round-trip")

	data, err := json.MarshalIndent(want, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))

	got, err := ReadClusterHealth(path)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, want.SchemaVersion, got.SchemaVersion)
	assert.Equal(t, want.ClassifierVersion, got.ClassifierVersion)
	assert.True(t, got.Session.Started.Equal(started), "session.started must round-trip")
	assert.True(t, got.Session.Ended.Equal(ended), "session.ended must round-trip")
	assert.Equal(t, want.Session.FlowsSeen, got.Session.FlowsSeen)
	assert.Equal(t, want.Session.InfraDropTotal, got.Session.InfraDropTotal)
	require.Len(t, got.Drops, 1)
	assert.Equal(t, want.Drops[0].Reason, got.Drops[0].Reason)
	assert.Equal(t, want.Drops[0].Class, got.Drops[0].Class)
	assert.Equal(t, want.Drops[0].Count, got.Drops[0].Count)
	assert.Equal(t, want.Drops[0].Remediation, got.Drops[0].Remediation, "remediation URL must round-trip")
	assert.Equal(t, want.Drops[0].ByNode, got.Drops[0].ByNode)
	assert.Equal(t, want.Drops[0].ByWorkload, got.Drops[0].ByWorkload)
}

// TestReadClusterHealth_MissingFile asserts a missing path returns a wrapped
// fs.ErrNotExist error -- the D-13 3-way branch signal for "zero infra
// drops observed", not a crash.
func TestReadClusterHealth_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing", "cluster-health.json")

	report, err := ReadClusterHealth(path)
	require.Error(t, err)
	assert.Nil(t, report)
	assert.True(t, errors.Is(err, fs.ErrNotExist), "expected wrapped fs.ErrNotExist, got: %v", err)
}

// TestReadClusterHealth_MalformedJSON asserts a non-nil, non-not-exist error
// for a corrupt file -- distinguishable from the missing-file case.
func TestReadClusterHealth_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cluster-health.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid json"), 0o644))

	report, err := ReadClusterHealth(path)
	require.Error(t, err)
	assert.Nil(t, report)
	assert.False(t, errors.Is(err, fs.ErrNotExist), "malformed JSON must not present as not-exist")
}

// TestReadClusterHealth_RejectsWrongSchemaVersion asserts the schema-version
// gate rejects any report.SchemaVersion != 1, and that the error names both
// the bad version number and the file path.
func TestReadClusterHealth_RejectsWrongSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cluster-health.json")

	doc := ClusterHealthReport{
		SchemaVersion:     99,
		ClassifierVersion: dropclass.ClassifierVersion,
		Session:           HealthSession{Started: time.Now(), Ended: time.Now()},
		Drops:             []HealthDropJSON{},
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))

	report, err := ReadClusterHealth(path)
	require.Error(t, err)
	assert.Nil(t, report)
	assert.Contains(t, err.Error(), "99", "error must name the bad schema_version")
	assert.Contains(t, err.Error(), path, "error must name the file path")
}
