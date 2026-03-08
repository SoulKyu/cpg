package policy_test

import (
	"testing"

	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/gule/cpg/pkg/policy"
	"github.com/gule/cpg/pkg/policy/testdata"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

func TestBuildPolicy_IngressTCP(t *testing.T) {
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		8080,
	)

	p := policy.BuildPolicy("default", "server", []*flowpb.Flow{f})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)

	// Ingress flow on destination -> IngressRule
	require.Len(t, p.Spec.Ingress, 1)
	assert.Empty(t, p.Spec.Egress)

	rule := p.Spec.Ingress[0]
	// FromEndpoints should have the source selector
	require.Len(t, rule.FromEndpoints, 1)

	// ToPorts should have port 8080/TCP
	require.Len(t, rule.ToPorts, 1)
	require.Len(t, rule.ToPorts[0].Ports, 1)
	assert.Equal(t, "8080", rule.ToPorts[0].Ports[0].Port)
	assert.Equal(t, api.ProtoTCP, rule.ToPorts[0].Ports[0].Protocol)
}

func TestBuildPolicy_EgressUDP(t *testing.T) {
	f := testdata.EgressUDPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=dns"},
		"default",
		53,
	)

	p := policy.BuildPolicy("default", "client", []*flowpb.Flow{f})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)

	// Egress flow on source -> EgressRule
	require.Len(t, p.Spec.Egress, 1)
	assert.Empty(t, p.Spec.Ingress)

	rule := p.Spec.Egress[0]
	require.Len(t, rule.ToEndpoints, 1)

	require.Len(t, rule.ToPorts, 1)
	require.Len(t, rule.ToPorts[0].Ports, 1)
	assert.Equal(t, "53", rule.ToPorts[0].Ports[0].Port)
	assert.Equal(t, api.ProtoUDP, rule.ToPorts[0].Ports[0].Protocol)
}

func TestBuildPolicy_MixedDirections(t *testing.T) {
	ingress := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		8080,
	)
	egress := testdata.EgressUDPFlow(
		[]string{"k8s:app=server"},
		[]string{"k8s:app=dns"},
		"default",
		53,
	)

	p := policy.BuildPolicy("default", "server", []*flowpb.Flow{ingress, egress})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)

	assert.Len(t, p.Spec.Ingress, 1, "should have 1 ingress rule")
	assert.Len(t, p.Spec.Egress, 1, "should have 1 egress rule")
}

func TestBuildPolicy_TypeMeta(t *testing.T) {
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		80,
	)

	p := policy.BuildPolicy("default", "server", []*flowpb.Flow{f})
	assert.Equal(t, "cilium.io/v2", p.APIVersion)
	assert.Equal(t, "CiliumNetworkPolicy", p.Kind)
}

func TestBuildPolicy_ObjectMeta(t *testing.T) {
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"production",
		443,
	)

	p := policy.BuildPolicy("production", "server", []*flowpb.Flow{f})
	assert.Equal(t, "cpg-server", p.Name)
	assert.Equal(t, "production", p.Namespace)
	assert.Equal(t, "cpg", p.Labels["app.kubernetes.io/managed-by"])
}

func TestBuildPolicy_NilL4(t *testing.T) {
	f := testdata.NilL4Flow()

	// Should not panic
	p := policy.BuildPolicy("default", "server", []*flowpb.Flow{f})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)
	assert.Empty(t, p.Spec.Ingress)
	assert.Empty(t, p.Spec.Egress)
}

func TestBuildPolicy_SamePeerDifferentPorts(t *testing.T) {
	f1 := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		80,
	)
	f2 := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		443,
	)

	p := policy.BuildPolicy("default", "server", []*flowpb.Flow{f1, f2})
	require.NotNil(t, p.Spec)

	// Same peer -> single rule with multiple ports
	require.Len(t, p.Spec.Ingress, 1)
	require.Len(t, p.Spec.Ingress[0].ToPorts, 1)
	ports := p.Spec.Ingress[0].ToPorts[0].Ports
	require.Len(t, ports, 2)

	portStrings := []string{ports[0].Port, ports[1].Port}
	assert.Contains(t, portStrings, "80")
	assert.Contains(t, portStrings, "443")
}

func TestBuildPolicy_DifferentPeers(t *testing.T) {
	f1 := testdata.IngressTCPFlow(
		[]string{"k8s:app=client-a"},
		[]string{"k8s:app=server"},
		"default",
		80,
	)
	f2 := testdata.IngressTCPFlow(
		[]string{"k8s:app=client-b"},
		[]string{"k8s:app=server"},
		"default",
		80,
	)

	p := policy.BuildPolicy("default", "server", []*flowpb.Flow{f1, f2})
	require.NotNil(t, p.Spec)

	// Different peers -> separate rules
	assert.Len(t, p.Spec.Ingress, 2)
}

func TestBuildPolicy_YAMLRoundtrip(t *testing.T) {
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		8080,
	)

	p := policy.BuildPolicy("default", "server", []*flowpb.Flow{f})

	data, err := yaml.Marshal(p)
	require.NoError(t, err)

	yamlStr := string(data)
	assert.Contains(t, yamlStr, "apiVersion: cilium.io/v2")
	assert.Contains(t, yamlStr, "kind: CiliumNetworkPolicy")
	assert.Contains(t, yamlStr, "cpg-server")
	assert.Contains(t, yamlStr, "endpointSelector")
	assert.Contains(t, yamlStr, "ingress")
	assert.Contains(t, yamlStr, "fromEndpoints")
	assert.Contains(t, yamlStr, "toPorts")
	assert.Contains(t, yamlStr, `port: "8080"`)
	assert.Contains(t, yamlStr, "protocol: TCP")
}
