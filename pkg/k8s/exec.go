package k8s

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"
)

// execCiliumDbgFn is a package-level seam over ExecCiliumDbg: production code
// always calls through this var, so callers (Task 2's read/flip helpers)
// substitute a stub in tests without standing up a fake SPDY server (mirrors
// DetectCiliumVersion's own seam-by-package-var convention already
// established in this package).
var execCiliumDbgFn = ExecCiliumDbg

// ExecCiliumDbg runs `<binary> <args...>` inside the cilium-agent container
// of podName (kube-system), returning combined stdout/stderr and any error.
//
// A non-nil error wrapping exec.CodeExitError (detected via errors.As) means
// the remote command itself exited non-zero (bad endpoint ID, bad option,
// agent socket unreachable) — this is always distinguished from a transport
// failure (SPDY dial/stream error), which wraps a different message so
// callers and operators can tell the two apart at a glance.
func ExecCiliumDbg(ctx context.Context, config *rest.Config, clientset kubernetes.Interface, podName, binary string, args []string) (stdout, stderr string, err error) {
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(ciliumNamespace).
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: ciliumAgentContainerName,
			Command:   append([]string{binary}, args...),
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", "", fmt.Errorf("creating SPDY executor for pod %s: %w", podName, err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
	})
	if err != nil {
		var codeErr exec.CodeExitError
		if errors.As(err, &codeErr) {
			return stdoutBuf.String(), stderrBuf.String(), fmt.Errorf(
				"%s exited %d in pod %s: %w (stderr: %s)", binary, codeErr.Code, podName, err, stderrBuf.String())
		}
		return stdoutBuf.String(), stderrBuf.String(), fmt.Errorf("exec transport failure in pod %s: %w", podName, err)
	}
	return stdoutBuf.String(), stderrBuf.String(), nil
}

// FindAgentPodForNode resolves the running cilium-agent pod (kube-system,
// k8s-app=cilium) whose Status.HostIP matches nodeIP. cilium-agent runs
// hostNetwork, so Status.HostIP is exactly the node's IP — the same value
// CiliumEndpoint.Status.Networking.NodeIP reports — letting this mapping
// avoid any nodes/get RBAC entirely: it reuses the same pods/list verb in
// kube-system that DetectCiliumVersion/findRelayPod already require
// unconditionally.
func FindAgentPodForNode(ctx context.Context, clientset kubernetes.Interface, nodeIP string) (*corev1.Pod, error) {
	pods, err := clientset.CoreV1().Pods(ciliumNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: ciliumAgentLabelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing cilium-agent pods: %w", err)
	}
	for i := range pods.Items {
		if pods.Items[i].Status.Phase == corev1.PodRunning && pods.Items[i].Status.HostIP == nodeIP {
			return &pods.Items[i], nil
		}
	}
	return nil, fmt.Errorf("no running cilium-agent pod found on node with IP %s", nodeIP)
}
