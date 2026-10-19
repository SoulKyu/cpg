package k8s

import (
	"context"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// dnsDetectTimeout bounds the single Services list call of DetectDNSSelector.
const dnsDetectTimeout = 3 * time.Second

const (
	warnDNSServicesForbidden = "dns companion selector: RBAC denied for list services in kube-system; " +
		"falling back to k8s-app in (kube-dns, coredns). Required permission: services/list in kube-system."

	warnDNSServicesFailed = "dns companion selector: could not list services in kube-system; " +
		"falling back to k8s-app in (kube-dns, coredns)."

	warnDNSServiceNotFound = "dns companion selector: no unambiguous port-53 Service with a selector in kube-system; " +
		"falling back to k8s-app in (kube-dns, coredns)."
)

// DetectDNSSelector returns the pod selector of the cluster DNS Service in
// kube-system, used as the DNS-02 companion rule's toEndpoints selector. A
// Service selector is by definition a label subset every DNS pod carries,
// whatever the distribution or chart override (DNS-FUT-02).
//
// Candidates are kube-system Services exposing port 53 with a non-empty
// selector; the one named kube-dns wins, then coredns, then a lone candidate.
// Never returns an error: nil means "use the policy package fallback", with
// exactly one warning logged.
func DetectDNSSelector(ctx context.Context, client kubernetes.Interface, logger *zap.Logger) map[string]string {
	ctx, cancel := context.WithTimeout(ctx, dnsDetectTimeout)
	defer cancel()

	svcs, err := client.CoreV1().Services(ciliumNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) {
			logger.Warn(warnDNSServicesForbidden, zap.Error(err))
		} else {
			logger.Warn(warnDNSServicesFailed, zap.Error(err))
		}
		return nil
	}

	svc := pickDNSService(svcs.Items)
	if svc == nil {
		logger.Warn(warnDNSServiceNotFound)
		return nil
	}
	logger.Info("dns companion selector detected",
		zap.String("service", svc.Namespace+"/"+svc.Name),
		zap.Any("selector", svc.Spec.Selector))
	return svc.Spec.Selector
}

func pickDNSService(items []corev1.Service) *corev1.Service {
	var candidates []*corev1.Service
	for i := range items {
		if len(items[i].Spec.Selector) > 0 && servesPort53(&items[i]) {
			candidates = append(candidates, &items[i])
		}
	}
	for _, name := range []string{"kube-dns", "coredns"} {
		for _, c := range candidates {
			if c.Name == name {
				return c
			}
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return nil
}

func servesPort53(svc *corev1.Service) bool {
	for _, p := range svc.Spec.Ports {
		if p.Port == 53 {
			return true
		}
	}
	return false
}
