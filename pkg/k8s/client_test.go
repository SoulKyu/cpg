package k8s

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadKubeConfig_WithKUBECONFIG(t *testing.T) {
	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: fake-token
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "kubeconfig")
	require.NoError(t, os.WriteFile(cfgPath, []byte(kubeconfig), 0600))

	t.Setenv("KUBECONFIG", cfgPath)

	cfg, err := LoadKubeConfig()
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, "https://127.0.0.1:6443", cfg.Host)
}

func TestLoadKubeConfig_InvalidPath(t *testing.T) {
	t.Setenv("KUBECONFIG", "/nonexistent/kubeconfig")

	_, err := LoadKubeConfig()
	assert.Error(t, err)
}
