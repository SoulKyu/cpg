package k8s

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// configMap builds a kube-system/cilium-config ConfigMap with the given data.
func configMap(data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cilium-config",
			Namespace: "kube-system",
		},
		Data: data,
	}
}

// envoyDS builds a kube-system/cilium-envoy DaemonSet.
func envoyDS() *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cilium-envoy",
			Namespace: "kube-system",
		},
	}
}

// newObservedLogger returns a zap logger that captures all entries at WarnLevel+.
func newObservedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.WarnLevel)
	return zap.New(core), logs
}

// forbiddenReactor returns a reactor that produces a 403 Forbidden error for
// the given resource (configmaps or daemonsets).
func forbiddenReactor(resource, verb string) clienttesting.ReactionFunc {
	return func(action clienttesting.Action) (bool, runtime.Object, error) {
		gr := schema.GroupResource{Resource: resource}
		return true, nil, apierrors.NewForbidden(gr, action.(clienttesting.GetAction).GetName(),
			&forbiddenErr{msg: "user lacks " + verb + " " + resource + " permission"})
	}
}

type forbiddenErr struct{ msg string }

func (e *forbiddenErr) Error() string { return e.msg }

// countWarnings returns the number of warn-level entries in the observed log.
func countWarnings(logs *observer.ObservedLogs) int {
	return len(logs.FilterLevelExact(zapcore.WarnLevel).All())
}

// containsMessage returns true if any logged entry's message contains substr.
func containsMessage(logs *observer.ObservedLogs, substr string) bool {
	for _, e := range logs.All() {
		if strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}

func TestRunL7Preflight(t *testing.T) {
	tests := []struct {
		name         string
		setup        func() kubernetes.Interface
		wantWarns    int
		wantContains []string // each substring expected in some warning message
	}{
		{
			name: "1_l7_proxy_true_envoy_ds_present_silent_pass",
			setup: func() kubernetes.Interface {
				return fake.NewSimpleClientset(
					configMap(map[string]string{"enable-l7-proxy": "true"}),
					envoyDS(),
				)
			},
			wantWarns: 0,
		},
		{
			name: "2_l7_proxy_false_warns",
			setup: func() kubernetes.Interface {
				return fake.NewSimpleClientset(
					configMap(map[string]string{"enable-l7-proxy": "false"}),
					envoyDS(),
				)
			},
			wantWarns:    1,
			wantContains: []string{"enable-l7-proxy", "roll the cilium-agent"},
		},
		{
			name: "3_l7_proxy_missing_key_warns",
			setup: func() kubernetes.Interface {
				return fake.NewSimpleClientset(
					configMap(map[string]string{}),
					envoyDS(),
				)
			},
			wantWarns:    1,
			wantContains: []string{"enable-l7-proxy"},
		},
		{
			name: "4_configmap_not_found_warns",
			setup: func() kubernetes.Interface {
				// No ConfigMap; envoy DS present so VIS-05 silent.
				return fake.NewSimpleClientset(envoyDS())
			},
			wantWarns:    1,
			wantContains: []string{"ConfigMap kube-system/cilium-config not found"},
		},
		{
			name: "5_envoy_ds_absent_but_enable_envoy_config_true_silent",
			setup: func() kubernetes.Interface {
				return fake.NewSimpleClientset(
					configMap(map[string]string{
						"enable-l7-proxy":      "true",
						"enable-envoy-config":  "true",
					}),
				)
			},
			wantWarns: 0,
		},
		{
			name: "6_envoy_ds_absent_no_enable_envoy_config_warns",
			setup: func() kubernetes.Interface {
				return fake.NewSimpleClientset(
					configMap(map[string]string{"enable-l7-proxy": "true"}),
				)
			},
			wantWarns:    1,
			wantContains: []string{"cilium-envoy", "enable-envoy-config"},
		},
		{
			name: "7_configmap_rbac_403_warn_and_proceed",
			setup: func() kubernetes.Interface {
				c := fake.NewSimpleClientset(envoyDS())
				c.PrependReactor("get", "configmaps", forbiddenReactor("configmaps", "get"))
				return c
			},
			// Only configmap call is denied; envoy DS check still succeeds (DS present) → silent.
			wantWarns:    1,
			wantContains: []string{"RBAC denied", "configmaps/get"},
		},
		{
			name: "8_daemonset_rbac_403_warn_and_proceed",
			setup: func() kubernetes.Interface {
				c := fake.NewSimpleClientset(
					configMap(map[string]string{"enable-l7-proxy": "true"}),
				)
				c.PrependReactor("get", "daemonsets", forbiddenReactor("daemonsets", "get"))
				return c
			},
			wantWarns:    1,
			wantContains: []string{"RBAC denied", "daemonsets/get"},
		},
		{
			name: "9_both_rbac_403_two_warnings",
			setup: func() kubernetes.Interface {
				c := fake.NewSimpleClientset()
				c.PrependReactor("get", "configmaps", forbiddenReactor("configmaps", "get"))
				c.PrependReactor("get", "daemonsets", forbiddenReactor("daemonsets", "get"))
				return c
			},
			wantWarns:    2,
			wantContains: []string{"configmaps/get", "daemonsets/get"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := tc.setup()
			logger, logs := newObservedLogger()

			RunL7Preflight(context.Background(), client, logger)

			if got := countWarnings(logs); got != tc.wantWarns {
				t.Errorf("warning count: got %d, want %d; entries: %+v",
					got, tc.wantWarns, logs.All())
			}
			for _, sub := range tc.wantContains {
				if !containsMessage(logs, sub) {
					t.Errorf("expected a warning containing %q; entries: %+v", sub, logs.All())
				}
			}
		})
	}
}

// Single-warning-per-invocation invariant: each call to RunL7Preflight emits at
// most one warning per check, regardless of how many issues exist for that
// check. (A misconfigured cluster with both VIS-04 and VIS-05 problems = 2
// warnings total — one per check, not one per workload.)
func TestRunL7Preflight_SingleWarningPerInvocation(t *testing.T) {
	// ConfigMap missing entirely: triggers VIS-04 (configmap not found) AND
	// VIS-05 (envoy DS not found, fallback to cilium-config which is also
	// missing). Expect exactly 2 warnings: one per check.
	client := fake.NewSimpleClientset()
	logger, logs := newObservedLogger()

	RunL7Preflight(context.Background(), client, logger)

	if got := countWarnings(logs); got != 2 {
		t.Errorf("expected 2 warnings (one per check), got %d; entries: %+v",
			got, logs.All())
	}
	if !containsMessage(logs, "cilium-config") {
		t.Errorf("expected a VIS-04 warning naming cilium-config")
	}
	if !containsMessage(logs, "cilium-envoy") {
		t.Errorf("expected a VIS-05 warning naming cilium-envoy")
	}
}
