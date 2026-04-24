// pkg/evidence/paths_test.go
package evidence

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashOutputDirStable(t *testing.T) {
	h1 := HashOutputDir("/home/gule/work/cpg/policies")
	h2 := HashOutputDir("/home/gule/work/cpg/policies/") // trailing slash
	h3 := HashOutputDir("/home/gule/work/cpg/policies/./")
	assert.Equal(t, h1, h2, "trailing slash must not affect hash")
	assert.Equal(t, h1, h3, "./ segments must not affect hash")
	assert.Len(t, h1, 12)
}

func TestHashOutputDirDifferent(t *testing.T) {
	h1 := HashOutputDir("/a/b")
	h2 := HashOutputDir("/a/c")
	assert.NotEqual(t, h1, h2)
}

func TestDefaultEvidenceDirRespectsXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-test")
	got, err := DefaultEvidenceDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/xdg-test", "cpg", "evidence"), got)
}

func TestDefaultEvidenceDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/tmp/home-test")
	got, err := DefaultEvidenceDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/home-test", ".cache", "cpg", "evidence"), got)
}

func TestResolvePolicyPath(t *testing.T) {
	p := ResolvePolicyPath("/base", "a3f2b1", "production", "api-server")
	assert.Equal(t, "/base/a3f2b1/production/api-server.json", p)
}
