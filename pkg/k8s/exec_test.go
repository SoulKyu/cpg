package k8s

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
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

// stubExecCiliumDbgFn substitutes the execCiliumDbgFn seam for the duration
// of a test, restoring the production binding via t.Cleanup — no fake SPDY
// server is ever stood up for the read/flip tests below.
func stubExecCiliumDbgFn(t *testing.T, stub func(ctx context.Context, config *rest.Config, clientset kubernetes.Interface, podName, binary string, args []string) (string, string, error)) {
	t.Helper()
	orig := execCiliumDbgFn
	execCiliumDbgFn = stub
	t.Cleanup(func() { execCiliumDbgFn = orig })
}

func TestReadPolicyAuditMode_ParsesEnabledFromArray(t *testing.T) {
	tests := []struct {
		name       string
		stdout     string
		wantEnable bool
		wantErr    bool
	}{
		{
			name:       "enabled_element",
			stdout:     `[{"spec":{"options":{"PolicyAuditMode":"Enabled"}}}]`,
			wantEnable: true,
		},
		{
			name:       "disabled_element",
			stdout:     `[{"spec":{"options":{"PolicyAuditMode":"Disabled"}}}]`,
			wantEnable: false,
		},
		{
			name:    "empty_array_errors",
			stdout:  `[]`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubExecCiliumDbgFn(t, func(ctx context.Context, config *rest.Config, clientset kubernetes.Interface, podName, binary string, args []string) (string, string, error) {
				return tc.stdout, "", nil
			})

			enabled, err := ReadPolicyAuditMode(context.Background(), nil, nil, "pod", "cilium-dbg", 42)
			if tc.wantErr {
				if err == nil {
					t.Fatal("ReadPolicyAuditMode returned nil error, want non-nil for empty array")
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadPolicyAuditMode returned error: %v", err)
			}
			if enabled != tc.wantEnable {
				t.Errorf("enabled = %v, want %v", enabled, tc.wantEnable)
			}
		})
	}
}

func TestSetPolicyAuditMode_UsesCanonicalLowercase(t *testing.T) {
	tests := []struct {
		name     string
		enable   bool
		wantArgv string
	}{
		{name: "enabling_uses_lowercase_enable", enable: true, wantArgv: "PolicyAuditMode=enable"},
		{name: "disabling_uses_lowercase_disable", enable: false, wantArgv: "PolicyAuditMode=disable"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var capturedArgs []string
			stubExecCiliumDbgFn(t, func(ctx context.Context, config *rest.Config, clientset kubernetes.Interface, podName, binary string, args []string) (string, string, error) {
				capturedArgs = args
				return "", "", nil
			})

			if err := SetPolicyAuditMode(context.Background(), nil, nil, "pod", "cilium-dbg", 7, tc.enable); err != nil {
				t.Fatalf("SetPolicyAuditMode returned error: %v", err)
			}

			found := false
			for _, a := range capturedArgs {
				if a == tc.wantArgv {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("captured args %v does not contain %q", capturedArgs, tc.wantArgv)
			}
		})
	}
}

func TestCheckDaemonAuditMode_ActiveWhenConfigTrue(t *testing.T) {
	t.Run("config_true_is_active", func(t *testing.T) {
		client := fake.NewSimpleClientset(configMap(map[string]string{"policy-audit-mode": "true"}))

		active, err := CheckDaemonAuditMode(context.Background(), client)
		if err != nil {
			t.Fatalf("CheckDaemonAuditMode returned error: %v", err)
		}
		if !active {
			t.Error("active = false, want true when ConfigMap policy-audit-mode=true")
		}
	})

	t.Run("key_absent_is_inactive", func(t *testing.T) {
		client := fake.NewSimpleClientset(configMap(map[string]string{}))

		active, err := CheckDaemonAuditMode(context.Background(), client)
		if err != nil {
			t.Fatalf("CheckDaemonAuditMode returned error: %v", err)
		}
		if active {
			t.Error("active = true, want false when policy-audit-mode key absent")
		}
	})

	t.Run("forbidden_is_undetermined_false_nil", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		client.PrependReactor("get", "configmaps", forbiddenReactor("configmaps", "get"))

		active, err := CheckDaemonAuditMode(context.Background(), client)
		if err != nil {
			t.Fatalf("CheckDaemonAuditMode returned error on forbidden read, want nil (warn-and-proceed): %v", err)
		}
		if active {
			t.Error("active = true, want false on forbidden read")
		}
	})

	t.Run("not_found_returns_error", func(t *testing.T) {
		client := fake.NewSimpleClientset() // no ConfigMap at all -> NotFound

		_, err := CheckDaemonAuditMode(context.Background(), client)
		if err == nil {
			t.Fatal("CheckDaemonAuditMode returned nil error on NotFound, want non-nil")
		}
	})
}

func TestCiliumBinaryName_Gate(t *testing.T) {
	tests := []struct {
		name   string
		compat CompatInfo
		want   string
	}{
		{name: "above_floor", compat: CompatInfo{ClusterVersion: "1.19.2", Source: "pod-images"}, want: "cilium-dbg"},
		{name: "at_floor", compat: CompatInfo{ClusterVersion: "1.15.0", Source: "pod-images"}, want: "cilium-dbg"},
		{name: "below_floor", compat: CompatInfo{ClusterVersion: "1.14.2", Source: "pod-images"}, want: "cilium"},
		{name: "undetermined_defaults_to_dbg", compat: CompatInfo{Source: "undetermined"}, want: "cilium-dbg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CiliumBinaryName(tc.compat); got != tc.want {
				t.Errorf("CiliumBinaryName(%+v) = %q, want %q", tc.compat, got, tc.want)
			}
		})
	}
}
