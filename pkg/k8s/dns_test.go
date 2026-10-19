package k8s

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func dnsService(name string, port int32, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kube-system"},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports:    []corev1.ServicePort{{Port: port}},
		},
	}
}

func TestDetectDNSSelector(t *testing.T) {
	oksSelector := map[string]string{
		"app.kubernetes.io/instance": "coredns",
		"app.kubernetes.io/name":     "coredns",
		"k8s-app":                    "coredns",
	}
	tests := []struct {
		name     string
		services []*corev1.Service
		want     map[string]string
		warns    int
	}{
		{
			name:     "vanilla kube-dns service",
			services: []*corev1.Service{dnsService("kube-dns", 53, map[string]string{"k8s-app": "kube-dns"})},
			want:     map[string]string{"k8s-app": "kube-dns"},
		},
		{
			name:     "upstream chart coredns service (Outscale OKS)",
			services: []*corev1.Service{dnsService("coredns", 53, oksSelector)},
			want:     oksSelector,
		},
		{
			name: "kube-dns name wins over other port-53 services",
			services: []*corev1.Service{
				dnsService("kube-dns-upstream", 53, map[string]string{"k8s-app": "node-local-dns"}),
				dnsService("kube-dns", 53, map[string]string{"k8s-app": "kube-dns"}),
			},
			want: map[string]string{"k8s-app": "kube-dns"},
		},
		{
			name:     "single custom-named port-53 service",
			services: []*corev1.Service{dnsService("dns", 53, map[string]string{"app": "dns"})},
			want:     map[string]string{"app": "dns"},
		},
		{
			name: "ambiguous custom-named services fall back",
			services: []*corev1.Service{
				dnsService("dns-a", 53, map[string]string{"app": "a"}),
				dnsService("dns-b", 53, map[string]string{"app": "b"}),
			},
			warns: 1,
		},
		{
			name:     "no port-53 service falls back",
			services: []*corev1.Service{dnsService("metrics-server", 443, map[string]string{"k8s-app": "metrics-server"})},
			warns:    1,
		},
		{
			name:     "selectorless service falls back",
			services: []*corev1.Service{dnsService("kube-dns", 53, nil)},
			warns:    1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			for _, s := range tt.services {
				if _, err := client.CoreV1().Services("kube-system").Create(context.Background(), s, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			logger, logs := newObservedLogger()

			got := DetectDNSSelector(context.Background(), client, logger)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("selector = %v, want %v", got, tt.want)
			}
			if n := countWarnings(logs); n != tt.warns {
				t.Errorf("warning count = %d, want %d; entries: %+v", n, tt.warns, logs.All())
			}
		})
	}
}

func TestDetectDNSSelector_Forbidden(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "services", forbiddenListReactor("services"))
	logger, logs := newObservedLogger()

	if got := DetectDNSSelector(context.Background(), client, logger); got != nil {
		t.Errorf("selector = %v, want nil (fallback)", got)
	}
	if !containsMessage(logs, "services/list in kube-system") {
		t.Errorf("expected RBAC warning naming services/list in kube-system; entries: %+v", logs.All())
	}
}
