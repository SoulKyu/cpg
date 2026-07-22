package k8s

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// ciliumAgentPodCounter gives each ciliumAgentPod a unique name so multiple
// pods with the same (or different) image can coexist in one fake
// clientset without a name collision.
var ciliumAgentPodCounter atomic.Int64

// ciliumAgentPod builds a Running cilium-agent pod in kube-system
// (k8s-app=cilium) with a single container named cilium-agent whose image is
// set to the given reference.
func ciliumAgentPod(image string) *corev1.Pod {
	n := ciliumAgentPodCounter.Add(1)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cilium-agent-%d", n),
			Namespace: "kube-system",
			Labels:    map[string]string{"k8s-app": "cilium"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "cilium-agent", Image: image},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
}

// forbiddenListReactor returns a reactor that produces a 403 Forbidden error
// for a "list" verb action. Unlike this package's forbiddenReactor
// (preflight_test.go), which type-asserts clienttesting.GetAction and would
// panic on a list action, this asserts clienttesting.ListAction — list
// actions carry no GetName() (see 21-PATTERNS.md).
func forbiddenListReactor(resource string) clienttesting.ReactionFunc {
	return func(action clienttesting.Action) (bool, runtime.Object, error) {
		if _, ok := action.(clienttesting.ListAction); !ok {
			return false, nil, nil
		}
		gr := schema.GroupResource{Resource: resource}
		return true, nil, apierrors.NewForbidden(gr, "",
			&forbiddenErr{msg: "user lacks list " + resource + " permission"})
	}
}

func TestParseImageTag(t *testing.T) {
	tests := []struct {
		name    string
		image   string
		wantTag string
		wantOK  bool
	}{
		{
			name:    "tag_with_digest_suffix",
			image:   "quay.io/cilium/cilium:v1.19.2@sha256:7bc7e0abcdef0123456789",
			wantTag: "v1.19.2",
			wantOK:  true,
		},
		{
			name:    "registry_host_port_not_misparsed",
			image:   "registry.internal:5000/cilium/cilium:v1.16.0",
			wantTag: "v1.16.0",
			wantOK:  true,
		},
		{
			name:    "digest_only_no_tag",
			image:   "cilium/cilium@sha256:7bc7e0abcdef0123456789",
			wantTag: "",
			wantOK:  false,
		},
		{
			name:    "bare_digest_id_rejected",
			image:   "sha256:5051a679",
			wantTag: "",
			wantOK:  false,
		},
		{
			name:    "no_tag_at_all",
			image:   "quay.io/cilium/cilium",
			wantTag: "",
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTag, gotOK := parseImageTag(tc.image)
			if gotTag != tc.wantTag || gotOK != tc.wantOK {
				t.Errorf("parseImageTag(%q) = (%q, %v), want (%q, %v)",
					tc.image, gotTag, gotOK, tc.wantTag, tc.wantOK)
			}
		})
	}
}

func TestDetectCiliumVersion(t *testing.T) {
	tests := []struct {
		name                   string
		setup                  func() kubernetes.Interface
		wantSource             string
		wantClusterVersion     string
		wantVersionsSeen       map[string]int
		wantBelowFloorEmpty    bool
		wantBelowFloorContains []string
		wantWarns              int
	}{
		{
			name: "all_pods_same_version_above_all_floors",
			setup: func() kubernetes.Interface {
				return fake.NewSimpleClientset(
					ciliumAgentPod("quay.io/cilium/cilium:v1.16.0"),
					ciliumAgentPod("quay.io/cilium/cilium:v1.16.0"),
				)
			},
			wantSource:          "pod-images",
			wantClusterVersion:  "1.16.0",
			wantVersionsSeen:    map[string]int{"1.16.0": 2},
			wantBelowFloorEmpty: true,
			wantWarns:           0,
		},
		{
			name: "mixed_versions_reduce_to_minimum",
			setup: func() kubernetes.Interface {
				objs := make([]runtime.Object, 0, 83)
				for i := 0; i < 53; i++ {
					objs = append(objs, ciliumAgentPod("quay.io/cilium/cilium:v1.19.2"))
				}
				for i := 0; i < 30; i++ {
					objs = append(objs, ciliumAgentPod("quay.io/cilium/cilium:v1.19.3"))
				}
				return fake.NewSimpleClientset(objs...)
			},
			wantSource:          "pod-images",
			wantClusterVersion:  "1.19.2",
			wantVersionsSeen:    map[string]int{"1.19.2": 53, "1.19.3": 30},
			wantBelowFloorEmpty: true,
			wantWarns:           0,
		},
		{
			name: "below_floor_names_affected_features",
			setup: func() kubernetes.Interface {
				return fake.NewSimpleClientset(ciliumAgentPod("quay.io/cilium/cilium:v1.14.2"))
			},
			wantSource:             "pod-images",
			wantClusterVersion:     "1.14.2",
			wantVersionsSeen:       map[string]int{"1.14.2": 1},
			wantBelowFloorContains: []string{"cilium-dbg", "enableDefaultDeny"},
			wantWarns:              1,
		},
		{
			name: "no_cilium_agent_pods_found",
			setup: func() kubernetes.Interface {
				return fake.NewSimpleClientset()
			},
			wantSource: "undetermined",
			wantWarns:  0,
		},
		{
			name: "nil_client",
			setup: func() kubernetes.Interface {
				return nil
			},
			wantSource: "undetermined",
			wantWarns:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := tc.setup()
			logger, logs := newObservedLogger()

			info := DetectCiliumVersion(context.Background(), client, logger)

			if info.Source != tc.wantSource {
				t.Errorf("Source = %q, want %q", info.Source, tc.wantSource)
			}
			if tc.wantClusterVersion != "" && info.ClusterVersion != tc.wantClusterVersion {
				t.Errorf("ClusterVersion = %q, want %q", info.ClusterVersion, tc.wantClusterVersion)
			}
			for v, wantCount := range tc.wantVersionsSeen {
				if gotCount := info.VersionsSeen[v]; gotCount != wantCount {
					t.Errorf("VersionsSeen[%q] = %d, want %d (full map: %+v)",
						v, gotCount, wantCount, info.VersionsSeen)
				}
			}
			if tc.wantBelowFloorEmpty && len(info.BelowFloorFeatures) != 0 {
				t.Errorf("BelowFloorFeatures = %v, want empty", info.BelowFloorFeatures)
			}
			for _, sub := range tc.wantBelowFloorContains {
				found := false
				for _, f := range info.BelowFloorFeatures {
					if strings.Contains(f, sub) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("BelowFloorFeatures %v does not contain substring %q", info.BelowFloorFeatures, sub)
				}
			}
			if got := countWarnings(logs); got != tc.wantWarns {
				t.Errorf("warning count: got %d, want %d; entries: %+v", got, tc.wantWarns, logs.All())
			}
		})
	}
}

// TestDetectCiliumVersion_Forbidden covers the RBAC-denied pods/list path
// separately from the main table: it needs a list-verb-specific forbidden
// reactor (forbiddenListReactor), not the get-verb one preflight_test.go
// already defines.
func TestDetectCiliumVersion_Forbidden(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "pods", forbiddenListReactor("pods"))
	logger, logs := newObservedLogger()

	info := DetectCiliumVersion(context.Background(), client, logger)

	if got := countWarnings(logs); got != 1 {
		t.Errorf("warning count: got %d, want 1; entries: %+v", got, logs.All())
	}
	if !containsMessage(logs, "pods/list in kube-system") {
		t.Errorf("expected a warning containing %q; entries: %+v", "pods/list in kube-system", logs.All())
	}
	if info.Source != "undetermined" {
		t.Errorf("Source = %q, want %q", info.Source, "undetermined")
	}
}

// TestDetectCiliumVersionViaGetNodes_UnreachableIsBoundedUndetermined proves
// the secondary GetNodes probe never hangs against an unreachable relay: it
// must return Source == "undetermined" well within the test's own generous
// deadline, bounded by the caller-supplied (tightening) timeout.
func TestDetectCiliumVersionViaGetNodes_UnreachableIsBoundedUndetermined(t *testing.T) {
	logger, _ := newObservedLogger()

	start := time.Now()
	resultCh := make(chan CompatInfo, 1)
	go func() {
		resultCh <- DetectCiliumVersionViaGetNodes(context.Background(), "127.0.0.1:1", false, 1*time.Second, logger)
	}()

	select {
	case info := <-resultCh:
		elapsed := time.Since(start)
		if info.Source != "undetermined" {
			t.Errorf("Source = %q, want %q", info.Source, "undetermined")
		}
		if elapsed >= 3*time.Second {
			t.Errorf("DetectCiliumVersionViaGetNodes took %v against an unreachable relay, want well under 3s (bounded dial)", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DetectCiliumVersionViaGetNodes did not return within 5s against an unreachable relay — bounded-dial guarantee violated")
	}
}

// TestParseAgentVersionString directly covers the component-prefix-stripping
// fix in parseAgentVersionString: GetNodes' per-node Version field is
// formatted "<component> v<version>" by cilium's own
// pkg/hubble/build.Version.String() (verified against the vendored
// github.com/cilium/cilium@v1.19.4 source), e.g. "cilium v1.19.2+g3977f6a1"
// — never a bare version string. A direct apiversion.ParseGeneric call on
// the raw field would fail on every real value; this test proves the
// stripped-and-parsed result is correct.
func TestParseAgentVersionString(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantStr string
		wantOK  bool
	}{
		{
			name:    "component_prefixed_with_build_metadata",
			raw:     "cilium v1.19.2+g3977f6a1",
			wantStr: "1.19.2",
			wantOK:  true,
		},
		{
			name:    "component_prefixed_no_build_metadata",
			raw:     "cilium v1.19.3",
			wantStr: "1.19.3",
			wantOK:  true,
		},
		{
			name:   "unparseable_garbage",
			raw:    "garbage",
			wantOK: false,
		},
		{
			name:   "empty_string",
			raw:    "",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := parseAgentVersionString(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("parseAgentVersionString(%q) ok = %v, want %v", tc.raw, ok, tc.wantOK)
			}
			if ok && v.String() != tc.wantStr {
				t.Errorf("parseAgentVersionString(%q) = %q, want %q", tc.raw, v.String(), tc.wantStr)
			}
		})
	}
}
