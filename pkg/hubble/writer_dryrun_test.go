package hubble

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/SoulKyu/cpg/pkg/output"
	"github.com/SoulKyu/cpg/pkg/policy"
)

func simplePolicy(name, namespace string) *ciliumv2.CiliumNetworkPolicy {
	return &ciliumv2.CiliumNetworkPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: "cilium.io/v2", Kind: "CiliumNetworkPolicy"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       &api.Rule{},
	}
}

func TestDryRunDoesNotWriteToDisk(t *testing.T) {
	tmp := t.TempDir()
	logger := zap.NewNop()

	stats := &SessionStats{}
	pw := newPolicyWriter(output.NewWriter(tmp, logger), nil, stats, logger)
	pw.dryRun = true
	pw.dryRunDiff = false

	pe := policy.PolicyEvent{
		Namespace: "prod", Workload: "api", Policy: simplePolicy("cpg-api", "prod"),
	}
	pw.handle(pe)

	_, err := os.Stat(filepath.Join(tmp, "prod", "api.yaml"))
	assert.True(t, os.IsNotExist(err), "no file must be written in dry-run")

	assert.Equal(t, uint64(1), stats.PoliciesWouldWrite)
	assert.Equal(t, uint64(0), stats.PoliciesWritten)
}

func TestDryRunEmitsDiffWhenExistingChanges(t *testing.T) {
	tmp := t.TempDir()

	existingPath := filepath.Join(tmp, "prod", "api.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(existingPath), 0o755))
	require.NoError(t, os.WriteFile(existingPath, []byte("apiVersion: cilium.io/v2\nkind: CiliumNetworkPolicy\nmetadata:\n  name: cpg-api\n  namespace: prod\nspec:\n  endpointSelector: {}\n  ingress:\n  - fromEndpoints: []\n"), 0o644))

	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	stats := &SessionStats{}
	pw := newPolicyWriter(output.NewWriter(tmp, logger), nil, stats, logger)
	pw.dryRun = true
	pw.dryRunDiff = true
	buf := new(bytes.Buffer)
	pw.diffOut = buf

	pe := policy.PolicyEvent{
		Namespace: "prod", Workload: "api", Policy: simplePolicy("cpg-api", "prod"),
	}
	pw.handle(pe)

	data, err := os.ReadFile(existingPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "ingress")

	assert.Contains(t, buf.String(), "--- ")
	assert.Contains(t, buf.String(), "+++ ")

	entries := logs.FilterMessage("would write policy").All()
	assert.Len(t, entries, 1)
}
