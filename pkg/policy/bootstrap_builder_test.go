package policy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/policy"
)

// TestBuildBootstrapPolicy_EnforcesDefaultDeny_Sanitizes is the named
// cilium/cilium#35558 regression guard for AUD-02 criterion 1: a bootstrap
// default-deny CiliumNetworkPolicy must both pass Cilium's own Spec.Sanitize()
// AND actually enforce default-deny once applied. The Sanitize() assertion
// below is the load-bearing check — it is the one that fails on the broken
// empty-slice ([]api.IngressRule{}) construction and passes only on the
// one-element-empty-rule form ([]api.IngressRule{{}}). The marshal and
// struct-level assertions guard the companion omitempty-drop and
// empty-slice regressions independently.
func TestBuildBootstrapPolicy_EnforcesDefaultDeny_Sanitizes(t *testing.T) {
	cnp := policy.BuildBootstrapPolicy("test-ns")
	require.NotNil(t, cnp.Spec)

	// THE #35558 enforcement guard — asserted on the real vendored api.Rule,
	// not a substring check on the marshaled YAML.
	require.NoError(t, cnp.Spec.Sanitize())

	// Naming: default-deny-<ns>, never a cpg-* merge-target candidate.
	assert.Equal(t, "default-deny-test-ns", cnp.Name)
	assert.Equal(t, "test-ns", cnp.Namespace)

	// Struct-level rule presence: one empty rule each, not zero, not nil.
	require.Len(t, cnp.Spec.Ingress, 1)
	require.Len(t, cnp.Spec.Egress, 1)

	require.NotNil(t, cnp.Spec.EnableDefaultDeny.Ingress)
	assert.True(t, *cnp.Spec.EnableDefaultDeny.Ingress)
	require.NotNil(t, cnp.Spec.EnableDefaultDeny.Egress)
	assert.True(t, *cnp.Spec.EnableDefaultDeny.Egress)

	// Endpoint selector select-all form: non-nil LabelSelector.
	require.NotNil(t, cnp.Spec.EndpointSelector.LabelSelector)

	// Marshal via the same sigs.k8s.io/yaml path generate/kubectl use, and
	// assert BOTH the enableDefaultDeny tokens AND the one-element rule
	// tokens survive — guards the omitempty-drop regression specifically
	// (an empty/nil slice would omit the ingress/egress keys entirely).
	out, err := yaml.Marshal(cnp)
	require.NoError(t, err)
	rendered := string(out)

	assert.Contains(t, rendered, "enableDefaultDeny:")
	assert.Contains(t, rendered, "ingress: true")
	assert.Contains(t, rendered, "egress: true")
	assert.Contains(t, rendered, "ingress:")
	assert.Contains(t, rendered, "egress:")
	assert.Contains(t, rendered, "- {}")
	assert.NotContains(t, rendered, "ingress: []")
	assert.NotContains(t, rendered, "egress: []")
}
