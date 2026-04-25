package k8s

import (
	"context"

	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	ciliumNamespace      = "kube-system"
	ciliumConfigMapName  = "cilium-config"
	ciliumEnvoyDSName    = "cilium-envoy"
	enableL7ProxyKey     = "enable-l7-proxy"
	enableEnvoyConfigKey = "enable-envoy-config"
)

// Warning copy is intentionally explicit so operators grep'ing logs during
// incident response find a literal, actionable string. Do not paraphrase.
const (
	warnL7ProxyDisabled = "L7 preflight: enable-l7-proxy is not set to 'true' in ConfigMap kube-system/cilium-config. " +
		"L7 policy generation requires the Cilium L7 proxy. " +
		"Remediation: set 'enable-l7-proxy: \"true\"' in the ConfigMap and roll the cilium-agent DaemonSet."

	warnConfigMapNotFound = "L7 preflight: ConfigMap kube-system/cilium-config not found. " +
		"L7 policy generation requires Cilium with L7 proxy enabled. Verify Cilium is installed in this cluster."

	warnConfigMapForbidden = "L7 preflight: RBAC denied for get configmaps in kube-system (cilium-config). " +
		"Skipping enable-l7-proxy check; proceeding. Required permission: configmaps/get in kube-system."

	warnEnvoyMissing = "L7 preflight: DaemonSet kube-system/cilium-envoy not found and enable-envoy-config is not 'true' in cilium-config. " +
		"On Cilium >= 1.16 the envoy DaemonSet is required for L7 visibility; " +
		"on Cilium 1.14-1.15 set enable-envoy-config: \"true\" in cilium-config."

	warnEnvoyForbidden = "L7 preflight: RBAC denied for get daemonsets in kube-system (cilium-envoy). " +
		"Skipping cilium-envoy check; proceeding. Required permission: daemonsets/get in kube-system."
)

// RunL7Preflight verifies cluster prerequisites for L7 policy generation:
//
//   - VIS-04: kube-system/cilium-config ConfigMap has enable-l7-proxy="true".
//   - VIS-05: kube-system/cilium-envoy DaemonSet exists (Cilium >= 1.16) OR
//     cilium-config has enable-envoy-config="true" (Cilium 1.14-1.15 fallback,
//     where envoy is embedded in the cilium-agent).
//
// Failures are reported as zap.Warn calls with remediation hints and DO NOT
// return errors — pre-flight is advisory. RBAC denials (apierrors.IsForbidden)
// are also warned and continued: cluster admins running cpg with reduced
// permissions (CI service accounts) must not be locked out.
//
// Contract: invoke ONCE per cpg invocation. Each warning fires at most once
// per call (one per check, two checks → up to two warnings). Callers gating
// on --no-l7-preflight (VIS-06) MUST simply skip invoking this function
// entirely; this function does not itself read the flag (Plan 07-04 owns the
// flag wiring).
func RunL7Preflight(ctx context.Context, client kubernetes.Interface, logger *zap.Logger) {
	cfg := getCiliumConfig(ctx, client, logger) // VIS-04 (also caches data for VIS-05 fallback)
	checkCiliumEnvoy(ctx, client, logger, cfg)   // VIS-05
}

// ciliumConfigData is the result of fetching cilium-config: either Data is
// non-nil (success), or Forbidden/NotFound flags are set, or both are false
// when an unexpected error occurred (still treated as missing for VIS-05).
type ciliumConfigData struct {
	Data      map[string]string
	Forbidden bool
	NotFound  bool
}

// getCiliumConfig fetches the cilium-config ConfigMap, emits the VIS-04
// warning if appropriate, and returns the data (or error markers) for the
// VIS-05 fallback to consume.
func getCiliumConfig(ctx context.Context, client kubernetes.Interface, logger *zap.Logger) ciliumConfigData {
	cm, err := client.CoreV1().ConfigMaps(ciliumNamespace).Get(ctx, ciliumConfigMapName, metav1.GetOptions{})
	switch {
	case err == nil:
		if cm.Data[enableL7ProxyKey] != "true" {
			logger.Warn(warnL7ProxyDisabled)
		}
		return ciliumConfigData{Data: cm.Data}
	case apierrors.IsForbidden(err):
		logger.Warn(warnConfigMapForbidden, zap.Error(err))
		return ciliumConfigData{Forbidden: true}
	case apierrors.IsNotFound(err):
		logger.Warn(warnConfigMapNotFound)
		return ciliumConfigData{NotFound: true}
	default:
		// Unexpected error (network, decode, etc.). Treat as missing — warn
		// like NotFound so the operator sees something, then proceed.
		logger.Warn(warnConfigMapNotFound, zap.Error(err))
		return ciliumConfigData{NotFound: true}
	}
}

// checkCiliumEnvoy verifies the cilium-envoy DaemonSet exists. If it doesn't,
// the Cilium 1.14-1.15 embedded-envoy fallback applies: a silent pass when
// cilium-config has enable-envoy-config="true". Anything else → one warning.
func checkCiliumEnvoy(ctx context.Context, client kubernetes.Interface, logger *zap.Logger, cfg ciliumConfigData) {
	_, err := client.AppsV1().DaemonSets(ciliumNamespace).Get(ctx, ciliumEnvoyDSName, metav1.GetOptions{})
	switch {
	case err == nil:
		// DS present → VIS-05 silent pass.
		return
	case apierrors.IsForbidden(err):
		logger.Warn(warnEnvoyForbidden, zap.Error(err))
		return
	case apierrors.IsNotFound(err):
		// Fallback: enable-envoy-config="true" → embedded envoy (1.14-1.15).
		if cfg.Data[enableEnvoyConfigKey] == "true" {
			return
		}
		logger.Warn(warnEnvoyMissing)
	default:
		// Unexpected error — surface the same warning.
		logger.Warn(warnEnvoyMissing, zap.Error(err))
	}
}
