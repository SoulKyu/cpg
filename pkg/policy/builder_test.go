package policy_test

import (
	"testing"

	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

func TestBuildPolicy_IngressTCP(t *testing.T) {
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		8080,
	)

	p, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
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

	p, _ := policy.BuildPolicy("default", "client", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
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

	p, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{ingress, egress}, nil, policy.AttributionOptions{})
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

	p, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
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

	p, _ := policy.BuildPolicy("production", "server", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
	assert.Equal(t, "cpg-server", p.Name)
	assert.Equal(t, "production", p.Namespace)
	assert.Equal(t, "cpg", p.Labels["app.kubernetes.io/managed-by"])
}

func TestBuildPolicy_NilL4(t *testing.T) {
	f := testdata.NilL4Flow()

	// Should not panic
	p, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
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

	p, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{f1, f2}, nil, policy.AttributionOptions{})
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

	p, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{f1, f2}, nil, policy.AttributionOptions{})
	require.NotNil(t, p.Spec)

	// Different peers -> separate rules
	assert.Len(t, p.Spec.Ingress, 2)
}

func TestBuildPolicy_WorldEgressCIDR(t *testing.T) {
	f := testdata.WorldEgressTCPFlow(
		[]string{"k8s:app=client"},
		"default",
		"93.184.216.34",
		443,
	)

	p, _ := policy.BuildPolicy("default", "client", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)

	// World egress -> EgressRule with ToCIDR, no ToEndpoints
	require.Len(t, p.Spec.Egress, 1)
	assert.Empty(t, p.Spec.Ingress)

	rule := p.Spec.Egress[0]
	assert.Empty(t, rule.ToEndpoints, "world identity should not produce endpoint selectors")
	require.Len(t, rule.ToCIDR, 1)
	assert.Equal(t, api.CIDR("93.184.216.34/32"), rule.ToCIDR[0])

	require.Len(t, rule.ToPorts, 1)
	require.Len(t, rule.ToPorts[0].Ports, 1)
	assert.Equal(t, "443", rule.ToPorts[0].Ports[0].Port)
	assert.Equal(t, api.ProtoTCP, rule.ToPorts[0].Ports[0].Protocol)
}

func TestBuildPolicy_WorldIngressCIDR(t *testing.T) {
	f := testdata.WorldIngressTCPFlow(
		"198.51.100.1",
		8080,
		[]string{"k8s:app=server"},
		"default",
	)

	p, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)

	// World ingress -> IngressRule with FromCIDR, no FromEndpoints
	require.Len(t, p.Spec.Ingress, 1)
	assert.Empty(t, p.Spec.Egress)

	rule := p.Spec.Ingress[0]
	assert.Empty(t, rule.FromEndpoints, "world identity should not produce endpoint selectors")
	require.Len(t, rule.FromCIDR, 1)
	assert.Equal(t, api.CIDR("198.51.100.1/32"), rule.FromCIDR[0])

	require.Len(t, rule.ToPorts, 1)
	require.Len(t, rule.ToPorts[0].Ports, 1)
	assert.Equal(t, "8080", rule.ToPorts[0].Ports[0].Port)
	assert.Equal(t, api.ProtoTCP, rule.ToPorts[0].Ports[0].Protocol)
}

func TestBuildPolicy_MixedWorldAndManaged(t *testing.T) {
	worldFlow := testdata.WorldEgressTCPFlow(
		[]string{"k8s:app=client"},
		"default",
		"93.184.216.34",
		443,
	)
	managedFlow := testdata.EgressUDPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=dns"},
		"default",
		53,
	)

	p, _ := policy.BuildPolicy("default", "client", []*flowpb.Flow{worldFlow, managedFlow}, nil, policy.AttributionOptions{})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)

	// Should have 2 egress rules: one CIDR, one endpoint selector
	require.Len(t, p.Spec.Egress, 2)

	var hasCIDR, hasEndpoint bool
	for _, rule := range p.Spec.Egress {
		if len(rule.ToCIDR) > 0 {
			hasCIDR = true
			assert.Empty(t, rule.ToEndpoints)
		}
		if len(rule.ToEndpoints) > 0 {
			hasEndpoint = true
			assert.Empty(t, rule.ToCIDR)
		}
	}
	assert.True(t, hasCIDR, "should have CIDR rule for world traffic")
	assert.True(t, hasEndpoint, "should have endpoint selector rule for managed traffic")
}

func TestBuildPolicy_WorldNilIP(t *testing.T) {
	f := testdata.WorldFlowNilIP()

	// Should not panic, world flow with nil IP is skipped
	p, _ := policy.BuildPolicy("default", "client", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)
	assert.Empty(t, p.Spec.Egress, "world flow with nil IP should be skipped")
}

func TestBuildPolicy_EgressICMPv4(t *testing.T) {
	f := testdata.EgressICMPv4Flow(
		[]string{"k8s:app.kubernetes.io/name=external-dns"},
		"external-dns",
		[]string{"reserved:kube-apiserver", "reserved:remote-node"},
		"10.6.46.11",
		8, // EchoRequest
	)

	p, _ := policy.BuildPolicy("external-dns", "external-dns", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)

	// Entity egress with ICMP
	require.Len(t, p.Spec.Egress, 1)
	rule := p.Spec.Egress[0]

	// Should use toEntities for kube-apiserver
	require.Len(t, rule.ToEntities, 1)
	assert.Equal(t, api.EntityKubeAPIServer, rule.ToEntities[0])

	// Should have ICMPs, not ToPorts
	assert.Empty(t, rule.ToPorts, "ICMP should not produce ToPorts")
	require.Len(t, rule.ICMPs, 1)
	require.Len(t, rule.ICMPs[0].Fields, 1)
	assert.Equal(t, "8", rule.ICMPs[0].Fields[0].Type.String())
	assert.Equal(t, api.IPv4Family, rule.ICMPs[0].Fields[0].Family)
}

func TestBuildPolicy_EntityEgressTCP(t *testing.T) {
	f := testdata.EntityEgressFlow(
		[]string{"k8s:app.kubernetes.io/name=external-dns"},
		"external-dns",
		[]string{"reserved:kube-apiserver", "reserved:remote-node"},
		"10.6.46.11",
		6443,
	)

	p, _ := policy.BuildPolicy("external-dns", "external-dns", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)

	require.Len(t, p.Spec.Egress, 1)
	rule := p.Spec.Egress[0]

	// Should use toEntities
	require.Len(t, rule.ToEntities, 1)
	assert.Equal(t, api.EntityKubeAPIServer, rule.ToEntities[0])

	// Should have ToPorts for TCP, not ICMPs
	require.Len(t, rule.ToPorts, 1)
	assert.Equal(t, "6443", rule.ToPorts[0].Ports[0].Port)
	assert.Equal(t, api.ProtoTCP, rule.ToPorts[0].Ports[0].Protocol)
	assert.Empty(t, rule.ICMPs)
}

func TestBuildPolicy_MixedTCPAndICMP(t *testing.T) {
	icmpFlow := testdata.EgressICMPv4Flow(
		[]string{"k8s:app.kubernetes.io/name=external-dns"},
		"external-dns",
		[]string{"reserved:kube-apiserver", "reserved:remote-node"},
		"10.6.46.11",
		8,
	)
	tcpFlow := testdata.EntityEgressFlow(
		[]string{"k8s:app.kubernetes.io/name=external-dns"},
		"external-dns",
		[]string{"reserved:kube-apiserver", "reserved:remote-node"},
		"10.6.46.11",
		6443,
	)

	p, _ := policy.BuildPolicy("external-dns", "external-dns", []*flowpb.Flow{icmpFlow, tcpFlow}, nil, policy.AttributionOptions{})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)

	// Same entity → two separate rules (ToPorts and ICMPs cannot coexist)
	require.Len(t, p.Spec.Egress, 2)

	// First rule: TCP ports
	assert.Equal(t, api.EntityKubeAPIServer, p.Spec.Egress[0].ToEntities[0])
	require.Len(t, p.Spec.Egress[0].ToPorts, 1)
	assert.Equal(t, "6443", p.Spec.Egress[0].ToPorts[0].Ports[0].Port)
	assert.Empty(t, p.Spec.Egress[0].ICMPs)

	// Second rule: ICMP
	assert.Equal(t, api.EntityKubeAPIServer, p.Spec.Egress[1].ToEntities[0])
	require.Len(t, p.Spec.Egress[1].ICMPs, 1)
	assert.Equal(t, "8", p.Spec.Egress[1].ICMPs[0].Fields[0].Type.String())
	assert.Empty(t, p.Spec.Egress[1].ToPorts)
}

func TestBuildPolicy_WorldICMP(t *testing.T) {
	f := testdata.EgressICMPv4Flow(
		[]string{"k8s:app.kubernetes.io/name=external-dns"},
		"external-dns",
		[]string{"cidr:10.6.31.11/32", "fqdn:pdns.example.com", "reserved:world"},
		"10.6.31.11",
		8,
	)

	p, _ := policy.BuildPolicy("external-dns", "external-dns", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
	require.NotNil(t, p)
	require.NotNil(t, p.Spec)

	require.Len(t, p.Spec.Egress, 1)
	rule := p.Spec.Egress[0]

	// World → CIDR rule with ICMP
	require.Len(t, rule.ToCIDR, 1)
	assert.Equal(t, api.CIDR("10.6.31.11/32"), rule.ToCIDR[0])

	assert.Empty(t, rule.ToPorts)
	require.Len(t, rule.ICMPs, 1)
	assert.Equal(t, "8", rule.ICMPs[0].Fields[0].Type.String())
}

func TestBuildPolicy_EntityICMPYAMLRoundtrip(t *testing.T) {
	f := testdata.EgressICMPv4Flow(
		[]string{"k8s:app.kubernetes.io/name=external-dns"},
		"external-dns",
		[]string{"reserved:kube-apiserver", "reserved:remote-node"},
		"10.6.46.11",
		8,
	)

	p, _ := policy.BuildPolicy("external-dns", "external-dns", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})
	data, err := yaml.Marshal(p)
	require.NoError(t, err)

	yamlStr := string(data)
	assert.Contains(t, yamlStr, "toEntities")
	assert.Contains(t, yamlStr, "kube-apiserver")
	assert.Contains(t, yamlStr, "icmps")
	assert.Contains(t, yamlStr, "type: 8")
	assert.Contains(t, yamlStr, "family: IPv4")
	assert.NotContains(t, yamlStr, "toPorts")
}

func TestBuildPolicy_YAMLRoundtrip(t *testing.T) {
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		8080,
	)

	p, _ := policy.BuildPolicy("default", "server", []*flowpb.Flow{f}, nil, policy.AttributionOptions{})

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
