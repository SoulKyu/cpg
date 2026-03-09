package output

import (
	"strings"
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/gule/cpg/pkg/policy"
)

func TestAnnotateRules_IngressAndEgress(t *testing.T) {
	flows := []*flowpb.Flow{
		{
			TrafficDirection: flowpb.TrafficDirection_INGRESS,
			Source:           &flowpb.Endpoint{Labels: []string{"k8s:app.kubernetes.io/component=coroot-node-agent"}, Namespace: "coroot"},
			Destination:      &flowpb.Endpoint{Labels: []string{"k8s:app.kubernetes.io/component=clickhouse"}, Namespace: "coroot"},
			L4:               &flowpb.Layer4{Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{DestinationPort: 9000}}},
		},
		{
			TrafficDirection: flowpb.TrafficDirection_INGRESS,
			Source:           &flowpb.Endpoint{Labels: []string{"k8s:app.kubernetes.io/component=coroot-node-agent"}, Namespace: "coroot"},
			Destination:      &flowpb.Endpoint{Labels: []string{"k8s:app.kubernetes.io/component=clickhouse"}, Namespace: "coroot"},
			L4:               &flowpb.Layer4{Protocol: &flowpb.Layer4_ICMPv4{ICMPv4: &flowpb.ICMPv4{Type: 8}}},
		},
		{
			TrafficDirection: flowpb.TrafficDirection_EGRESS,
			Source:           &flowpb.Endpoint{Labels: []string{"k8s:app.kubernetes.io/component=clickhouse"}, Namespace: "coroot"},
			Destination:      &flowpb.Endpoint{Labels: []string{"reserved:kube-apiserver"}},
			IP:               &flowpb.IP{Destination: "10.0.0.1"},
			L4:               &flowpb.Layer4{Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{DestinationPort: 6443}}},
		},
	}

	cnp := policy.BuildPolicy("coroot", "clickhouse", flows)
	data, err := yaml.Marshal(cnp)
	require.NoError(t, err)

	annotated := string(annotateRules(data, cnp.Spec))
	t.Log("Annotated YAML:\n" + annotated)

	// Ingress TCP rule should have a comment
	assert.Contains(t, annotated, "# TCP/9000 from app.kubernetes.io/component=coroot-node-agent")
	// Ingress ICMP rule should have a comment
	assert.Contains(t, annotated, "# IPv4(type=8) from app.kubernetes.io/component=coroot-node-agent")
	// Egress entity rule should have a comment
	assert.Contains(t, annotated, "# TCP/6443 to entity kube-apiserver")

	// Comments should appear before the rule they describe
	tcpIdx := strings.Index(annotated, "# TCP/9000")
	fromIdx := strings.Index(annotated, "- fromEndpoints:")
	assert.Less(t, tcpIdx, fromIdx, "comment should appear before rule")
}

func TestAnnotateRules_NilSpec(t *testing.T) {
	data := []byte("apiVersion: cilium.io/v2\nkind: CiliumNetworkPolicy\n")
	result := annotateRules(data, nil)
	assert.Equal(t, data, result)
}

func TestStripComments(t *testing.T) {
	input := "  # TCP/9000 from app=foo\n  - fromEndpoints:\n"
	stripped := stripComments(input)
	assert.NotContains(t, stripped, "# TCP/9000")
	assert.Contains(t, stripped, "- fromEndpoints:")
}
