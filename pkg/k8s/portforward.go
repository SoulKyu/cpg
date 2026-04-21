package k8s

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const (
	relayNamespace    = "kube-system"
	relayLabelSelector = "k8s-app=hubble-relay"
	relayPort         = 4245
)

// PortForwardToRelay establishes a port-forward tunnel to a hubble-relay pod
// in kube-system. It returns the local address (localhost:port), a cleanup
// function that closes the tunnel, and any error.
func PortForwardToRelay(ctx context.Context, config *rest.Config, logger *zap.Logger) (string, func(), error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	pod, err := findRelayPod(ctx, clientset, logger)
	if err != nil {
		return "", nil, err
	}

	logger.Info("found hubble-relay pod",
		zap.String("pod", pod.Name),
		zap.String("namespace", pod.Namespace),
	)

	// Build the port-forward URL
	reqURL := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("portforward").
		URL()

	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return "", nil, fmt.Errorf("creating SPDY round tripper: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, reqURL)

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})

	fw, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", relayPort)}, stopCh, readyCh, io.Discard, io.Discard)
	if err != nil {
		return "", nil, fmt.Errorf("creating port forwarder: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- fw.ForwardPorts()
	}()

	select {
	case <-readyCh:
		// Port forward is ready
	case err := <-errCh:
		return "", nil, fmt.Errorf("port forwarding failed: %w", err)
	case <-ctx.Done():
		close(stopCh)
		return "", nil, ctx.Err()
	}

	ports, err := fw.GetPorts()
	if err != nil {
		close(stopCh)
		return "", nil, fmt.Errorf("getting forwarded ports: %w", err)
	}
	if len(ports) == 0 {
		close(stopCh)
		return "", nil, fmt.Errorf("no ports forwarded")
	}

	localAddr := fmt.Sprintf("localhost:%d", ports[0].Local)
	cleanup := func() {
		close(stopCh)
	}

	logger.Info("port-forward established",
		zap.String("local_addr", localAddr),
		zap.String("pod", pod.Name),
	)

	return localAddr, cleanup, nil
}

// findRelayPod finds a running hubble-relay pod in kube-system.
func findRelayPod(ctx context.Context, clientset kubernetes.Interface, logger *zap.Logger) (*corev1.Pod, error) {
	pods, err := clientset.CoreV1().Pods(relayNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: relayLabelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing hubble-relay pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no hubble-relay pod found in %s (selector: %s)", relayNamespace, relayLabelSelector)
	}

	// Find a running pod
	for i := range pods.Items {
		if pods.Items[i].Status.Phase == corev1.PodRunning {
			return &pods.Items[i], nil
		}
	}

	return nil, fmt.Errorf("no running hubble-relay pod found in %s (%d pods found, none running)", relayNamespace, len(pods.Items))
}

