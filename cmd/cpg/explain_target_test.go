package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTargetNamespaceWorkloadForm(t *testing.T) {
	tgt, err := resolveExplainTarget("production/api-server")
	require.NoError(t, err)
	assert.Equal(t, "production", tgt.Namespace)
	assert.Equal(t, "api-server", tgt.Workload)
}

func TestResolveTargetRejectsInvalidForm(t *testing.T) {
	_, err := resolveExplainTarget("just-one-segment")
	assert.Error(t, err)
}

func TestResolveTargetYAMLPath(t *testing.T) {
	tmp := t.TempDir()
	yamlPath := filepath.Join(tmp, "api-server.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("apiVersion: cilium.io/v2\nkind: CiliumNetworkPolicy\nmetadata:\n  name: cpg-api-server\n  namespace: production\n"), 0o644))

	tgt, err := resolveExplainTarget(yamlPath)
	require.NoError(t, err)
	assert.Equal(t, "production", tgt.Namespace)
	assert.Equal(t, "api-server", tgt.Workload)
}

func TestResolveTargetRejectsYAMLWithoutCPGPrefix(t *testing.T) {
	tmp := t.TempDir()
	yamlPath := filepath.Join(tmp, "other.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("apiVersion: cilium.io/v2\nkind: CiliumNetworkPolicy\nmetadata:\n  name: not-a-cpg-policy\n  namespace: production\n"), 0o644))

	_, err := resolveExplainTarget(yamlPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cpg-")
}
