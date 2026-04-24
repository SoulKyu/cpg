package output

import (
	"os"
	"path/filepath"
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)

func buildTestEvent(ns, workload string) policy.PolicyEvent {
	flows := []*flowpb.Flow{
		testdata.IngressTCPFlow(
			[]string{"k8s:app=client"},
			[]string{"k8s:app=server"},
			ns, 80,
		),
	}
	cnp, _ := policy.BuildPolicy(ns, workload, flows, nil, policy.AttributionOptions{})
	return policy.PolicyEvent{
		Namespace: ns,
		Workload:  workload,
		Policy:    cnp,
	}
}

func TestWriter_NewFileCreation(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()
	w := NewWriter(dir, logger)

	event := buildTestEvent("default", "server")
	err := w.Write(event)
	require.NoError(t, err)

	// File should exist at outputDir/namespace/workload.yaml
	path := filepath.Join(dir, "default", "server.yaml")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.False(t, info.IsDir())

	// File should contain valid YAML with apiVersion
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "apiVersion")
	assert.Contains(t, string(content), "cilium.io/v2")
}

func TestWriter_DirectoryCreation(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()
	w := NewWriter(dir, logger)

	event := buildTestEvent("production", "frontend")
	err := w.Write(event)
	require.NoError(t, err)

	// Namespace directory should exist
	nsDir := filepath.Join(dir, "production")
	info, err := os.Stat(nsDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// File should exist
	path := filepath.Join(nsDir, "frontend.yaml")
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestWriter_MergeOnWrite(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()
	w := NewWriter(dir, logger)

	// First write: port 80
	event1 := buildTestEvent("default", "server")
	err := w.Write(event1)
	require.NoError(t, err)

	// Second write: port 443 (different flow, same peer)
	flows2 := []*flowpb.Flow{
		testdata.IngressTCPFlow(
			[]string{"k8s:app=client"},
			[]string{"k8s:app=server"},
			"default", 443,
		),
	}
	cnp2, _ := policy.BuildPolicy("default", "server", flows2, nil, policy.AttributionOptions{})
	event2 := policy.PolicyEvent{
		Namespace: "default",
		Workload:  "server",
		Policy:    cnp2,
	}
	err = w.Write(event2)
	require.NoError(t, err)

	// Read the file and verify it contains both ports
	path := filepath.Join(dir, "default", "server.yaml")
	content, err := os.ReadFile(path)
	require.NoError(t, err)

	contentStr := string(content)
	assert.Contains(t, contentStr, "80")
	assert.Contains(t, contentStr, "443")
}

func TestWriter_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()
	w := NewWriter(dir, logger)

	event := buildTestEvent("default", "server")
	err := w.Write(event)
	require.NoError(t, err)

	path := filepath.Join(dir, "default", "server.yaml")
	info, err := os.Stat(path)
	require.NoError(t, err)
	// File should be 0644
	assert.Equal(t, os.FileMode(0644), info.Mode().Perm())
}

func TestWriter_MultipleNamespaces(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()
	w := NewWriter(dir, logger)

	// Write to two different namespaces
	err := w.Write(buildTestEvent("ns-a", "svc-1"))
	require.NoError(t, err)
	err = w.Write(buildTestEvent("ns-b", "svc-2"))
	require.NoError(t, err)

	// Both files should exist
	_, err = os.Stat(filepath.Join(dir, "ns-a", "svc-1.yaml"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "ns-b", "svc-2.yaml"))
	require.NoError(t, err)
}

func TestWriter_SkipEquivalentPolicy(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()
	w := NewWriter(dir, logger)

	event := buildTestEvent("default", "server")

	// First write
	err := w.Write(event)
	require.NoError(t, err)

	path := filepath.Join(dir, "default", "server.yaml")
	info1, err := os.Stat(path)
	require.NoError(t, err)
	content1, err := os.ReadFile(path)
	require.NoError(t, err)

	// Second write with same policy -- should be skipped (no file change)
	err = w.Write(event)
	require.NoError(t, err)

	info2, err := os.Stat(path)
	require.NoError(t, err)
	content2, err := os.ReadFile(path)
	require.NoError(t, err)

	// Content should be identical (not re-written)
	assert.Equal(t, string(content1), string(content2), "equivalent policy should not change file content")
	assert.Equal(t, info1.ModTime(), info2.ModTime(), "equivalent policy should not update file mtime")
}

func TestWriter_WritesDifferentPolicy(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()
	w := NewWriter(dir, logger)

	// Write initial policy with port 80
	event1 := buildTestEvent("default", "server")
	err := w.Write(event1)
	require.NoError(t, err)

	path := filepath.Join(dir, "default", "server.yaml")
	content1, err := os.ReadFile(path)
	require.NoError(t, err)

	// Write different policy with port 443 (new rules, should merge and write)
	flows2 := []*flowpb.Flow{
		testdata.IngressTCPFlow(
			[]string{"k8s:app=new-client"},
			[]string{"k8s:app=server"},
			"default", 443,
		),
	}
	cnp2, _ := policy.BuildPolicy("default", "server", flows2, nil, policy.AttributionOptions{})
	event2 := policy.PolicyEvent{
		Namespace: "default",
		Workload:  "server",
		Policy:    cnp2,
	}
	err = w.Write(event2)
	require.NoError(t, err)

	content2, err := os.ReadFile(path)
	require.NoError(t, err)

	// Content should have changed (merged with new rules)
	assert.NotEqual(t, string(content1), string(content2), "different policy should update file content")
	assert.Contains(t, string(content2), "443")
	assert.Contains(t, string(content2), "80")
}
