package output

import (
	"strings"
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/policy"
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

	cnp, _ := policy.BuildPolicy("coroot", "clickhouse", flows, nil, policy.AttributionOptions{})
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

func TestDescribePeer_Endpoints(t *testing.T) {
	tests := []struct {
		name      string
		endpoints []api.EndpointSelector
		want      string
	}{
		{
			name: "single matchLabels",
			endpoints: []api.EndpointSelector{
				api.NewESFromMatchRequirements(map[string]string{"app": "x"}, nil),
			},
			want: "from app=x",
		},
		{
			name: "multiple selectors all described",
			endpoints: []api.EndpointSelector{
				api.NewESFromMatchRequirements(map[string]string{"app": "x"}, nil),
				api.NewESFromMatchRequirements(map[string]string{"app": "y"}, nil),
			},
			want: "from app=x or app=y",
		},
		{
			name: "matchExpressions-only selector not reported as any",
			endpoints: []api.EndpointSelector{
				api.NewESFromMatchRequirements(nil, []slim_metav1.LabelSelectorRequirement{
					{Key: "tier", Operator: slim_metav1.LabelSelectorOpIn, Values: []string{"frontend", "backend"}},
				}),
			},
			want: "from tier in (frontend, backend)",
		},
		{
			name: "matchLabels and matchExpressions combined",
			endpoints: []api.EndpointSelector{
				api.NewESFromMatchRequirements(map[string]string{"app": "x"}, []slim_metav1.LabelSelectorRequirement{
					{Key: "env", Operator: slim_metav1.LabelSelectorOpExists},
				}),
			},
			want: "from app=x, env exists",
		},
		{
			name: "empty selector falls back to any",
			endpoints: []api.EndpointSelector{
				api.NewESFromMatchRequirements(nil, nil),
			},
			want: "from any",
		},
		{
			name:      "no endpoints falls back to any",
			endpoints: nil,
			want:      "from any",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describePeer(tt.endpoints, nil, nil, "from")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStripComments(t *testing.T) {
	input := "  # TCP/9000 from app=foo\n  - fromEndpoints:\n"
	stripped := stripComments(input)
	assert.NotContains(t, stripped, "# TCP/9000")
	assert.Contains(t, stripped, "- fromEndpoints:")
}
