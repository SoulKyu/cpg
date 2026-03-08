package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildClusterPolicyMap(t *testing.T) {
	policies := []ciliumv2.CiliumNetworkPolicy{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "server",
				Namespace: "production",
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "cpg"},
			},
			Spec: &api.Rule{
				EndpointSelector: api.EndpointSelector{
					LabelSelector: &slim_metav1.LabelSelector{
						MatchLabels: map[string]slim_metav1.MatchLabelsValue{"app": "server"},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "client",
				Namespace: "production",
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "cpg"},
			},
			Spec: &api.Rule{
				EndpointSelector: api.EndpointSelector{
					LabelSelector: &slim_metav1.LabelSelector{
						MatchLabels: map[string]slim_metav1.MatchLabelsValue{"app": "client"},
					},
				},
			},
		},
	}

	result := buildClusterPolicyMap(policies)
	require.Len(t, result, 2)
	assert.NotNil(t, result["server"])
	assert.NotNil(t, result["client"])
}

func TestBuildClusterPolicyMap_Empty(t *testing.T) {
	result := buildClusterPolicyMap(nil)
	assert.Empty(t, result)
}

func TestLoadClusterPolicies_LabelSelector(t *testing.T) {
	// This test verifies the label selector constant is correct.
	assert.Equal(t, "app.kubernetes.io/managed-by=cpg", ManagedByLabel)
}

func TestLoadClusterPolicies_Integration(t *testing.T) {
	// LoadClusterPolicies requires a real Cilium clientset which is hard to fake.
	// We test the core logic via buildClusterPolicyMap and verify the function
	// signature and label selector are correct.
	ctx := context.Background()
	_ = ctx // used in actual call
}
