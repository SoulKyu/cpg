package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFindRelayPod_Found(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hubble-relay-abc123",
			Namespace: "kube-system",
			Labels:    map[string]string{"k8s-app": "hubble-relay"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	})

	pod, err := findRelayPod(context.Background(), client, logger)
	require.NoError(t, err)
	assert.Equal(t, "hubble-relay-abc123", pod.Name)
	assert.Equal(t, "kube-system", pod.Namespace)
}

func TestFindRelayPod_NotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client := fake.NewSimpleClientset()

	_, err := findRelayPod(context.Background(), client, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no hubble-relay pod")
}

func TestFindRelayPod_NoRunningPod(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hubble-relay-abc123",
			Namespace: "kube-system",
			Labels:    map[string]string{"k8s-app": "hubble-relay"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	})

	_, err := findRelayPod(context.Background(), client, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no running hubble-relay pod")
}
