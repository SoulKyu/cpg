package k8s

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// ciliumAgentPodWithHostIP builds a cilium-agent pod (kube-system,
// k8s-app=cilium) with the given HostIP and phase, reusing the naming
// counter version_test.go's ciliumAgentPod already established so pods never
// collide by name in the same fake clientset.
func ciliumAgentPodWithHostIP(hostIP string, phase corev1.PodPhase) *corev1.Pod {
	pod := ciliumAgentPod("quay.io/cilium/cilium:v1.19.2")
	pod.Status.HostIP = hostIP
	pod.Status.Phase = phase
	return pod
}

func TestFindAgentPodForNode_MatchesHostIP(t *testing.T) {
	podA := ciliumAgentPodWithHostIP("10.0.0.1", corev1.PodRunning)
	podB := ciliumAgentPodWithHostIP("10.0.0.2", corev1.PodRunning)
	client := fake.NewSimpleClientset(podA, podB)

	got, err := FindAgentPodForNode(context.Background(), client, "10.0.0.2")
	if err != nil {
		t.Fatalf("FindAgentPodForNode returned error: %v", err)
	}
	if got.Name != podB.Name {
		t.Errorf("FindAgentPodForNode returned pod %q, want %q (HostIP match)", got.Name, podB.Name)
	}
}

func TestFindAgentPodForNode_NoMatchErrors(t *testing.T) {
	// A non-Running pod whose HostIP would otherwise match must be ignored.
	nonRunning := ciliumAgentPodWithHostIP("10.0.0.9", corev1.PodPending)
	running := ciliumAgentPodWithHostIP("10.0.0.1", corev1.PodRunning)
	client := fake.NewSimpleClientset(nonRunning, running)

	_, err := FindAgentPodForNode(context.Background(), client, "10.0.0.9")
	if err == nil {
		t.Fatal("FindAgentPodForNode returned nil error, want error naming the unmatched node IP")
	}
	if !strings.Contains(err.Error(), "10.0.0.9") {
		t.Errorf("error %q does not name the node IP %q", err.Error(), "10.0.0.9")
	}
}

// TestFindAgentPodForNode_NoPodsAtAll covers the base case: an empty
// clientset (no cilium-agent pods at all) must still error naming the
// requested node IP, not panic on an empty Items slice.
func TestFindAgentPodForNode_NoPodsAtAll(t *testing.T) {
	client := fake.NewSimpleClientset()

	_, err := FindAgentPodForNode(context.Background(), client, "10.0.0.5")
	if err == nil {
		t.Fatal("FindAgentPodForNode returned nil error on empty clientset, want error")
	}
	if !strings.Contains(err.Error(), "10.0.0.5") {
		t.Errorf("error %q does not name the node IP %q", err.Error(), "10.0.0.5")
	}
}
