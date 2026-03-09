package labels

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectLabels_PriorityAppKubernetesIOName(t *testing.T) {
	labels := []string{
		"k8s:app.kubernetes.io/name=nginx",
		"k8s:pod-template-hash=abc",
	}
	got := SelectLabels(labels)
	assert.Equal(t, map[string]string{"app.kubernetes.io/name": "nginx"}, got)
}

func TestSelectLabels_PriorityApp(t *testing.T) {
	labels := []string{
		"k8s:app=redis",
		"k8s:pod-template-hash=xyz",
	}
	got := SelectLabels(labels)
	assert.Equal(t, map[string]string{"app": "redis"}, got)
}

func TestSelectLabels_FallbackAllLabels(t *testing.T) {
	labels := []string{
		"k8s:team=platform",
		"k8s:tier=backend",
	}
	got := SelectLabels(labels)
	assert.Equal(t, map[string]string{"team": "platform", "tier": "backend"}, got)
}

func TestSelectLabels_AllDenylisted(t *testing.T) {
	labels := []string{
		"k8s:pod-template-hash=abc",
	}
	got := SelectLabels(labels)
	assert.Empty(t, got)
}

func TestSelectLabels_NonK8sLabelsIgnored(t *testing.T) {
	labels := []string{
		"reserved:world",
	}
	got := SelectLabels(labels)
	assert.Empty(t, got)
}

func TestSelectLabels_DenylistComplete(t *testing.T) {
	// All 7 denylist labels must be excluded
	labels := []string{
		"k8s:pod-template-hash=a",
		"k8s:controller-revision-hash=b",
		"k8s:statefulset.kubernetes.io/pod-name=c",
		"k8s:job-name=d",
		"k8s:batch.kubernetes.io/job-name=e",
		"k8s:batch.kubernetes.io/controller-uid=f",
		"k8s:apps.kubernetes.io/pod-index=g",
		"k8s:team=platform",
	}
	got := SelectLabels(labels)
	assert.Equal(t, map[string]string{"team": "platform"}, got)
}

func TestSelectLabels_AppKubernetesNameTakesPriorityOverApp(t *testing.T) {
	labels := []string{
		"k8s:app.kubernetes.io/name=nginx",
		"k8s:app=web",
		"k8s:tier=frontend",
	}
	got := SelectLabels(labels)
	assert.Equal(t, map[string]string{"app.kubernetes.io/name": "nginx"}, got)
}

func TestWorkloadName_PriorityAppKubernetesIOName(t *testing.T) {
	labels := []string{
		"k8s:app.kubernetes.io/name=nginx",
		"k8s:pod-template-hash=abc",
	}
	got := WorkloadName(labels)
	assert.Equal(t, "nginx", got)
}

func TestWorkloadName_PriorityApp(t *testing.T) {
	labels := []string{
		"k8s:app=redis",
		"k8s:pod-template-hash=xyz",
	}
	got := WorkloadName(labels)
	assert.Equal(t, "redis", got)
}

func TestWorkloadName_FallbackDeterministic(t *testing.T) {
	labels := []string{
		"k8s:team=platform",
		"k8s:tier=backend",
	}
	name1 := WorkloadName(labels)
	name2 := WorkloadName(labels)
	assert.Equal(t, name1, name2, "WorkloadName must be deterministic")
	assert.NotEmpty(t, name1)
}

func TestWorkloadName_Empty(t *testing.T) {
	labels := []string{
		"k8s:pod-template-hash=abc",
	}
	got := WorkloadName(labels)
	assert.Equal(t, "unknown", got)
}

func TestBuildEndpointSelector(t *testing.T) {
	labels := []string{
		"k8s:app=nginx",
		"k8s:pod-template-hash=abc",
	}
	es := BuildEndpointSelector(labels)
	// The endpoint selector should not be zero (it has matchLabels)
	assert.False(t, es.IsZero(), "EndpointSelector should not be zero")
	// Should have the app label (Cilium prefixes with "any." internally)
	assert.True(t, es.HasKey("any.app") || es.HasKey("app"),
		"EndpointSelector should contain app label")
}

func TestBuildPeerSelector_CrossNamespace(t *testing.T) {
	labels := []string{
		"k8s:app=frontend",
	}
	es := BuildPeerSelector(labels, "web", "default")
	assert.False(t, es.IsZero(), "PeerSelector should not be zero")
	// Should include namespace label for cross-namespace traffic
	hasNs := es.HasKey("any.io.kubernetes.pod.namespace") || es.HasKey("io.kubernetes.pod.namespace")
	assert.True(t, hasNs, "PeerSelector should include namespace for cross-namespace traffic")
}

func TestSelectLabels_PriorityAppKubernetesComponent(t *testing.T) {
	labels := []string{
		"k8s:app.kubernetes.io/component=clickhouse",
		"k8s:app.kubernetes.io/managed-by=coroot-operator",
		"k8s:app.kubernetes.io/part-of=coroot",
	}
	got := SelectLabels(labels)
	assert.Equal(t, map[string]string{"app.kubernetes.io/component": "clickhouse"}, got)
}

func TestSelectLabels_AppNameTakesPriorityOverComponent(t *testing.T) {
	labels := []string{
		"k8s:app.kubernetes.io/name=myapp",
		"k8s:app.kubernetes.io/component=frontend",
	}
	got := SelectLabels(labels)
	assert.Equal(t, map[string]string{"app.kubernetes.io/name": "myapp"}, got)
}

func TestSelectLabels_CiliumIdentityLabelsFiltered(t *testing.T) {
	labels := []string{
		"k8s:io.cilium.k8s.namespace.labels.kubernetes.io/metadata.name=coroot",
		"k8s:io.cilium.k8s.policy.cluster=default",
		"k8s:io.cilium.k8s.policy.serviceaccount=coroot",
		"k8s:io.kubernetes.pod.namespace=coroot",
		"k8s:team=platform",
	}
	got := SelectLabels(labels)
	assert.Equal(t, map[string]string{"team": "platform"}, got)
}

func TestWorkloadName_ComponentLabel(t *testing.T) {
	labels := []string{
		"k8s:app.kubernetes.io/component=clickhouse",
		"k8s:app.kubernetes.io/managed-by=coroot-operator",
		"k8s:io.cilium.k8s.policy.cluster=default",
	}
	got := WorkloadName(labels)
	assert.Equal(t, "clickhouse", got)
}

func TestBuildPeerSelector_SameNamespace(t *testing.T) {
	labels := []string{
		"k8s:app=frontend",
	}
	es := BuildPeerSelector(labels, "default", "default")
	// Should NOT include namespace label for same-namespace traffic
	hasNs := es.HasKey("any.io.kubernetes.pod.namespace") || es.HasKey("io.kubernetes.pod.namespace")
	assert.False(t, hasNs, "PeerSelector should not include namespace for same-namespace traffic")
}
