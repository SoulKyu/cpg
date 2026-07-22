package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"
)

// policyAuditModeCanonical* are the canonical, lowercase argv literals cpg's
// own exec calls pass for the PolicyAuditMode option. cilium-dbg's
// NormalizeBool parser tolerates several case-insensitive spellings
// ("Enabled", "TRUE", "1", ...), but cpg deliberately hard-codes the exact
// documented cmdref form (23-RESEARCH.md Pitfall 5) rather than relying on
// that tolerance, so any reader cross-referencing cpg's exec calls against
// the upstream docs sees the canonical spelling.
const (
	policyAuditModeCanonicalEnable  = "enable"
	policyAuditModeCanonicalDisable = "disable"

	// policyAuditModeEnabledLiteral is the exact literal cilium-dbg's own
	// IntOptions.GetMutableModel() formats a Realized/Spec option value as
	// when PolicyAuditMode is on ("Enabled"/"Disabled", title case — NOT
	// "1"/"0"). Used only when reading state back, never when writing it.
	policyAuditModeEnabledLiteral = "Enabled"

	// ciliumDbgBinaryFloor is the Cilium version at and above which the
	// cilium-dbg binary name applies (pkg/k8s/version.go's "cilium-dbg
	// binary naming" feature floor, PR #28085).
	ciliumDbgBinaryFloor = "1.15.0"
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

// CiliumBinaryName returns the cilium-dbg CLI binary name to exec, gated on
// the detected cluster version's "cilium-dbg binary naming" floor
// (pkg/k8s/version.go's featureFloors, PR #28085 @ Cilium 1.15). An
// undetermined ClusterVersion (compat.Source == "undetermined", or any
// unparseable version) defaults to "cilium-dbg" — >= 1.15 is the
// overwhelmingly common floor across supported clusters, and the older bare
// "cilium" name is legacy.
func CiliumBinaryName(compat CompatInfo) string {
	if compat.ClusterVersion == "" {
		return "cilium-dbg"
	}
	cluster, err := apiversion.ParseGeneric(compat.ClusterVersion)
	if err != nil {
		return "cilium-dbg"
	}
	floor, err := apiversion.ParseGeneric(ciliumDbgBinaryFloor)
	if err != nil {
		return "cilium-dbg"
	}
	if cluster.AtLeast(floor) {
		return "cilium-dbg"
	}
	return "cilium"
}

// endpointGetJSON matches ONE element of `cilium-dbg endpoint get <id> -o
// json`'s top-level output, which is always a JSON ARRAY of models.Endpoint
// (never a bare object) — a different, incompatible shape from `cilium-dbg
// endpoint config <id>`'s models.EndpointConfigurationStatus (23-RESEARCH.md
// Pitfall 2). ReadPolicyAuditMode standardizes on the `endpoint get` form.
type endpointGetJSON struct {
	Spec struct {
		Options map[string]string `json:"options"`
	} `json:"spec"`
}

// ReadPolicyAuditMode reads an endpoint's current PolicyAuditMode via
// `<binary> endpoint get <id> -o json`. It errors (never silently returns
// false) when the response is not a well-formed, non-empty JSON array, so
// callers can never mistake "couldn't determine state" for "confirmed
// disabled."
func ReadPolicyAuditMode(ctx context.Context, config *rest.Config, clientset kubernetes.Interface, podName, binary string, endpointID int64) (enabled bool, err error) {
	stdout, _, err := execCiliumDbgFn(ctx, config, clientset, podName, binary,
		[]string{"endpoint", "get", strconv.FormatInt(endpointID, 10), "-o", "json"})
	if err != nil {
		return false, err
	}
	var parsed []endpointGetJSON
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return false, fmt.Errorf("parsing endpoint %d get output: %w", endpointID, err)
	}
	if len(parsed) == 0 {
		return false, fmt.Errorf("endpoint %d: empty response from endpoint get", endpointID)
	}
	return parsed[0].Spec.Options["PolicyAuditMode"] == policyAuditModeEnabledLiteral, nil
}

// SetPolicyAuditMode flips an endpoint's PolicyAuditMode via `<binary>
// endpoint config <id> PolicyAuditMode=<enable|disable>`, always passing the
// canonical lowercase literal rather than relying on cilium-dbg's
// NormalizeBool tolerance for other spellings (23-RESEARCH.md Pitfall 5).
func SetPolicyAuditMode(ctx context.Context, config *rest.Config, clientset kubernetes.Interface, podName, binary string, endpointID int64, enable bool) error {
	value := policyAuditModeCanonicalDisable
	if enable {
		value = policyAuditModeCanonicalEnable
	}
	_, _, err := execCiliumDbgFn(ctx, config, clientset, podName, binary,
		[]string{"endpoint", "config", strconv.FormatInt(endpointID, 10), "PolicyAuditMode=" + value})
	if err != nil {
		return fmt.Errorf("setting endpoint %d PolicyAuditMode=%s: %w", endpointID, value, err)
	}
	return nil
}

// CheckDaemonAuditMode reads the daemon-wide policy-audit-mode setting from
// the cilium-config ConfigMap (kube-system). RBAC-forbidden reads are
// treated as undetermined (false, nil) — warn-and-proceed, matching
// version.go's own convention for every other advisory precondition; any
// other error is returned so the caller can hard-refuse on a genuine read
// failure rather than silently proceeding. The ConfigMap key is absent
// entirely unless an operator explicitly set policyAuditMode in Helm values,
// so cm.Data's zero-value "" correctly reads as inactive.
func CheckDaemonAuditMode(ctx context.Context, clientset kubernetes.Interface) (active bool, err error) {
	cm, err := clientset.CoreV1().ConfigMaps(ciliumNamespace).Get(ctx, ciliumConfigMapName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s/%s ConfigMap: %w", ciliumNamespace, ciliumConfigMapName, err)
	}
	return cm.Data["policy-audit-mode"] == "true", nil
}
