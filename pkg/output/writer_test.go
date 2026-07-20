package output

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sigs.k8s.io/yaml"

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

// TestWriter_RejectsInvalidPolicyRef pins the path-validation guard: a
// namespace/workload that is empty or a traversal segment must be refused
// before any directory or file is created under the output root.
func TestWriter_RejectsInvalidPolicyRef(t *testing.T) {
	tests := []struct {
		name     string
		ns       string
		workload string
	}{
		{"empty namespace", "", "api"},
		{"empty workload", "prod", ""},
		{"traversal namespace", "..", "api"},
		{"traversal workload", "prod", ".."},
		{"separator in workload", "prod", "a/b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			w := NewWriter(dir, zap.NewNop())

			err := w.Write(buildTestEvent(tt.ns, tt.workload))

			require.Error(t, err, "invalid ref must be refused")
			entries, readErr := os.ReadDir(dir)
			require.NoError(t, readErr)
			assert.Empty(t, entries, "nothing may be created under the output root")
		})
	}
}

// TestWriter_AtomicNoLeftoverTempFiles verifies that after a successful
// write, no leftover ".tmp-*" file remains in the namespace directory --
// proving the temp file was renamed into place rather than left behind.
func TestWriter_AtomicNoLeftoverTempFiles(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()
	w := NewWriter(dir, logger)

	event := buildTestEvent("default", "server")
	err := w.Write(event)
	require.NoError(t, err)

	nsDir := filepath.Join(dir, "default")
	entries, err := os.ReadDir(nsDir)
	require.NoError(t, err)

	for _, entry := range entries {
		assert.False(t, strings.Contains(entry.Name(), ".tmp-"), "leftover temp file found: %s", entry.Name())
	}

	path := filepath.Join(nsDir, "server.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var cnp ciliumv2.CiliumNetworkPolicy
	require.NoError(t, yaml.Unmarshal(data, &cnp), "written file must be valid CNP YAML")
}

// TestWriter_ConcurrentReaderNeverSeesPartialFile drives a writer goroutine
// that repeatedly rewrites the same policy file -- varying the destination
// port each iteration so every write is a genuine content change, forcing a
// real temp+rename cycle every time instead of hitting the
// equivalent-policy skip path -- concurrently with a reader goroutine that
// repeatedly reads the same path. Atomic rename guarantees the reader
// observes either the previous complete file or the new complete file,
// never a partial one. Run under -race.
func TestWriter_ConcurrentReaderNeverSeesPartialFile(t *testing.T) {
	dir := t.TempDir()
	logger := zap.NewNop()
	w := NewWriter(dir, logger)

	const (
		ns       = "default"
		workload = "server"
		iters    = 100
	)
	path := filepath.Join(dir, ns, workload+".yaml")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			flows := []*flowpb.Flow{
				testdata.IngressTCPFlow(
					[]string{"k8s:app=client"},
					[]string{"k8s:app=server"},
					ns, uint32(8000+i),
				),
			}
			cnp, _ := policy.BuildPolicy(ns, workload, flows, nil, policy.AttributionOptions{})
			event := policy.PolicyEvent{
				Namespace: ns,
				Workload:  workload,
				Policy:    cnp,
			}
			if err := w.Write(event); err != nil {
				t.Errorf("writer goroutine: unexpected error: %v", err)
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue // valid before the first rename
				}
				t.Errorf("reader goroutine: unexpected read error: %v", err)
				continue
			}
			var cnp ciliumv2.CiliumNetworkPolicy
			if err := yaml.Unmarshal(data, &cnp); err != nil {
				t.Errorf("reader observed a partial/corrupt file: %v", err)
			}
		}
	}()

	wg.Wait()
}
