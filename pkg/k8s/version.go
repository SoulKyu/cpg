package k8s

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	observerpb "github.com/cilium/cilium/api/v1/observer"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
)

// ciliumAgentLabelSelector/ciliumAgentContainerName identify the cilium-agent
// DaemonSet's pods. ciliumNamespace itself is declared in preflight.go
// (kube-system) and reused here unchanged — cilium-config, cilium-envoy, and
// the cilium-agent DaemonSet all live in the same namespace.
const (
	ciliumAgentLabelSelector = "k8s-app=cilium"
	ciliumAgentContainerName = "cilium-agent"

	// versionDetectTimeout bounds DetectCiliumVersionViaGetNodes's dial +
	// ready-wait. It is a hard ceiling, never merely a suggestion: a
	// caller-supplied timeout may only tighten it (see
	// DetectCiliumVersionViaGetNodes), and an omitted or zero timeout must
	// never produce an unbounded dial against an unreachable relay.
	versionDetectTimeout = 3 * time.Second
)

// Warning copy is intentionally explicit so operators grep'ing logs during
// incident response find a literal, actionable string. Do not paraphrase.
const (
	warnPodsListForbidden = "version preflight: RBAC denied for list pods in kube-system (k8s-app=cilium). " +
		"Skipping Cilium version detection; version-gated features proceed without a floor check. " +
		"Required permission: pods/list in kube-system."

	warnPodsListFailed = "version preflight: could not list cilium-agent pods in kube-system; proceeding without version detection. " +
		"Cilium version detection is advisory only and does not block cpg's pipeline."

	warnGetNodesFailed = "version preflight: GetNodes probe against the Hubble Relay observer API failed; " +
		"proceeding without version detection. This is the MCP-only secondary detection source; " +
		"its failure never blocks session setup."

	warnBelowFloorPrefix = "version preflight: cluster Cilium version is below one or more feature floors; proceeding without blocking. "
)

// CompatInfo is the result of a Cilium version-detection probe. It is never
// an error condition in itself: Source == "undetermined" signals that no
// reliable version signal was available (nil client, RBAC-denied pods/list,
// no matching pods, or an unreachable Hubble Relay) and callers MUST treat
// that as "proceed without a floor check," never as a failure to propagate.
type CompatInfo struct {
	// ClusterVersion is the minimum Cilium version parsed across every
	// detected source (e.g. "1.19.2"). Empty when undetermined.
	ClusterVersion string
	// VersionsSeen maps each distinct parsed version string to the number of
	// pods (Source "pod-images") or nodes (Source "get-nodes") reporting
	// that version. A mixed result usually means a rolling upgrade is in
	// progress. Nil when the underlying probe was never attempted or failed
	// outright (RBAC-denied, unreachable); non-nil (possibly empty) when the
	// probe ran but matched nothing.
	VersionsSeen map[string]int
	// Source names how ClusterVersion was obtained: "pod-images" (primary,
	// CLI+MCP), "get-nodes" (MCP-only secondary), or "undetermined".
	Source string
	// BelowFloorFeatures names each declared feature floor the detected
	// ClusterVersion does not meet, e.g. "cilium-dbg binary naming (requires
	// >= 1.15.0)". Empty when ClusterVersion is undetermined or meets every
	// floor.
	BelowFloorFeatures []string
}

// featureFloors is the declared per-feature Cilium version floor table,
// sourced from 21-RESEARCH.md's Version Pin Table (merged-PR + release-tag
// verified). Keep in sync with README.md's "Supported Cilium versions"
// section (COMPAT-01) — this table is the runtime-enforcement side of that
// documentation.
var featureFloors = []struct {
	name  string
	floor string // fed to apiversion.ParseGeneric
}{
	{"baseline cpg operation", "1.14.0"},
	{"cilium-dbg binary naming", "1.15.0"},
	{"enableDefaultDeny CNP field", "1.16.0"},
}

// parseImageTag extracts the tag from a container image reference,
// tolerating a registry host:port prefix and an "@sha256:..." digest
// suffix. It returns ("", false) — never panics — on any input it cannot
// confidently parse a tag from, including:
//   - a digest-only reference with no tag at all (e.g.
//     "cilium/cilium@sha256:...")
//   - a bare content digest with no repository path (e.g.
//     "sha256:5051a679...", the shape Kubernetes reports in a pod's
//     .status.containerStatuses[].image field — never a real image
//     reference; a colon here is never a tag separator)
//
// Per Docker/OCI image-reference grammar, a tag can never itself contain a
// "/", so the correct tag separator is the LAST ":" occurring AFTER the LAST
// "/" — and only when the reference has a "/" at all. A colon in a
// slash-free string is always a registry-port-style or bare-digest-style
// separator, never a tag.
func parseImageTag(image string) (tag string, ok bool) {
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	lastColon := strings.LastIndex(image, ":")
	lastSlash := strings.LastIndex(image, "/")
	if lastColon == -1 || lastSlash == -1 || lastColon < lastSlash {
		return "", false
	}
	return image[lastColon+1:], true
}

// DetectCiliumVersion detects the cluster's Cilium version by enumerating
// cilium-agent pods in kube-system (k8s-app=cilium) and reducing their
// .spec.containers[].image tags to the minimum parsed version across all
// Running pods. This is the PRIMARY detection source: it works identically
// from CLI and MCP, needs no already-open gRPC connection, and reuses the
// exact pods/list-in-kube-system verb findRelayPod (portforward.go) already
// requires unconditionally — no new RBAC class.
//
// This NEVER returns an error and NEVER blocks: a nil client, an
// RBAC-forbidden pods/list, or any other list failure all warn-and-proceed
// with CompatInfo{Source: "undetermined"} — reduced-RBAC service accounts
// (CI, etc.) must not be locked out, mirroring RunL7Preflight's own
// rationale (preflight.go).
func DetectCiliumVersion(ctx context.Context, client kubernetes.Interface, logger *zap.Logger) CompatInfo {
	if client == nil {
		return finalizeCompat(CompatInfo{Source: "undetermined"}, logger)
	}

	pods, err := client.CoreV1().Pods(ciliumNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: ciliumAgentLabelSelector,
	})
	switch {
	case err == nil:
		versionsSeen, minVersion := tallyPodImageVersions(pods.Items)
		info := CompatInfo{VersionsSeen: versionsSeen, Source: "undetermined"}
		if minVersion != nil {
			info.ClusterVersion = minVersion.String()
			info.Source = "pod-images"
		}
		return finalizeCompat(info, logger)
	case apierrors.IsForbidden(err):
		logger.Warn(warnPodsListForbidden, zap.Error(err))
		return finalizeCompat(CompatInfo{Source: "undetermined"}, logger)
	default:
		// WR-01: a SIGINT during the CLI preflight cancels ctx and surfaces
		// here as context.Canceled — that is the operator's own shutdown, not
		// a detection failure, so suppress the alarming warning. A genuine
		// deadline (our own budget) or list error still warns.
		if !errors.Is(ctx.Err(), context.Canceled) {
			logger.Warn(warnPodsListFailed, zap.Error(err))
		}
		return finalizeCompat(CompatInfo{Source: "undetermined"}, logger)
	}
}

// tallyPodImageVersions tallies the parsed version of each Running pod's
// cilium-agent container image (falling back to the pod's first container if
// none is named ciliumAgentContainerName), returning a per-version pod count
// and the minimum parsed version across every pod whose tag parsed
// successfully. A pod with an unparseable or absent image tag is skipped
// entirely — a malformed tag must never poison the cluster-wide minimum
// (Threat T-21-01-01).
func tallyPodImageVersions(pods []corev1.Pod) (versionsSeen map[string]int, minVersion *apiversion.Version) {
	versionsSeen = make(map[string]int)
	for i := range pods {
		if pods[i].Status.Phase != corev1.PodRunning {
			continue
		}

		image := ""
		for _, c := range pods[i].Spec.Containers {
			if c.Name == ciliumAgentContainerName {
				image = c.Image
				break
			}
		}
		if image == "" && len(pods[i].Spec.Containers) > 0 {
			image = pods[i].Spec.Containers[0].Image
		}
		if image == "" {
			continue
		}

		tag, ok := parseImageTag(image)
		if !ok {
			continue
		}
		v, err := apiversion.ParseGeneric(tag)
		if err != nil {
			continue
		}

		versionsSeen[v.String()]++
		if minVersion == nil || v.LessThan(minVersion) {
			minVersion = v
		}
	}
	return versionsSeen, minVersion
}

// DetectCiliumVersionViaGetNodes is the MCP-only secondary detection source:
// it dials the Hubble Relay observer gRPC API directly and calls GetNodes(),
// tallying each node's own reported Version. Its only advantage over
// DetectCiliumVersion is that it works even when an explicit --server (D-07)
// bypasses kubeconfig entirely, leaving no Kubernetes client available at
// all.
//
// This function NEVER calls bare ServerStatus() — that RPC reports Hubble
// Relay's OWN build version, not any cilium-agent's, and the two can diverge
// during a rollout where Relay and the agent DaemonSet are upgraded
// independently (verified against a live cluster and cpg's own vendored
// cilium source — 21-RESEARCH.md Tension 1 Resolution). It also NEVER execs
// into a pod: detection stays privilege-neutral, always.
//
// The dial + ready-wait is always bounded: effective is
// min(timeout, versionDetectTimeout), with versionDetectTimeout itself a
// never-0 floor, so an omitted or zero caller timeout can never produce an
// unbounded dial against an unreachable relay. The connection is always
// closed before returning, on every path.
func DetectCiliumVersionViaGetNodes(ctx context.Context, server string, tlsEnabled bool, timeout time.Duration, logger *zap.Logger) CompatInfo {
	effective := versionDetectTimeout
	if timeout > 0 && timeout < versionDetectTimeout {
		effective = timeout
	}

	var transportCreds grpc.DialOption
	if tlsEnabled {
		transportCreds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	} else {
		transportCreds = grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	conn, err := grpc.NewClient(server, transportCreds)
	if err != nil {
		logger.Warn(warnGetNodesFailed, zap.Error(err))
		return finalizeCompat(CompatInfo{Source: "undetermined"}, logger)
	}
	defer func() { _ = conn.Close() }()

	if err := waitForVersionConnReady(ctx, conn, effective); err != nil {
		logger.Warn(warnGetNodesFailed, zap.Error(err))
		return finalizeCompat(CompatInfo{Source: "undetermined"}, logger)
	}

	client := observerpb.NewObserverClient(conn)
	resp, err := client.GetNodes(ctx, &observerpb.GetNodesRequest{})
	if err != nil {
		logger.Warn(warnGetNodesFailed, zap.Error(err))
		return finalizeCompat(CompatInfo{Source: "undetermined"}, logger)
	}

	versionsSeen := make(map[string]int)
	var minVersion *apiversion.Version
	for _, node := range resp.GetNodes() {
		v, ok := parseAgentVersionString(node.GetVersion())
		if !ok {
			continue
		}
		versionsSeen[v.String()]++
		if minVersion == nil || v.LessThan(minVersion) {
			minVersion = v
		}
	}

	info := CompatInfo{VersionsSeen: versionsSeen, Source: "undetermined"}
	if minVersion != nil {
		info.ClusterVersion = minVersion.String()
		info.Source = "get-nodes"
	}
	return finalizeCompat(info, logger)
}

// parseAgentVersionString parses a cilium-agent version string as reported
// by GetNodes' per-node Version field. cpg's own vendored source
// (pkg/hubble/build.Version.String(), github.com/cilium/cilium@v1.19.4)
// formats this as "<component> v<version>" (e.g. "cilium v1.19.2+g3977f6a1")
// — the "<component> " prefix is stripped (split on the last space) before
// parsing, since apiversion.ParseGeneric expects the string to start with an
// optional "v" then digits, not a component name. Falls back to parsing the
// raw string unchanged if no space is present, tolerating a possible
// bare-version format.
func parseAgentVersionString(raw string) (*apiversion.Version, bool) {
	s := raw
	if i := strings.LastIndex(s, " "); i >= 0 {
		s = s[i+1:]
	}
	v, err := apiversion.ParseGeneric(s)
	if err != nil {
		return nil, false
	}
	return v, true
}

// waitForVersionConnReady blocks until conn reaches connectivity.Ready or
// timeout elapses. Mirrors pkg/hubble/client.go's waitForConnReady exactly
// (that helper is unexported in a different package, hence this local
// reimplementation); pkg/hubble.Client itself is not reused because it is
// streaming-shaped and not a fit for this single unary GetNodes call.
func waitForVersionConnReady(ctx context.Context, conn *grpc.ClientConn, timeout time.Duration) error {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if !conn.WaitForStateChange(dialCtx, state) {
			return fmt.Errorf("connecting to hubble relay observer %q: %w", conn.Target(), dialCtx.Err())
		}
	}
}

// finalizeCompat computes BelowFloorFeatures for a detected ClusterVersion
// and, when any feature is below its floor, emits a single warning naming
// the affected feature(s) and the detected version (COMPAT-02 success
// criterion 3). Undetermined verdicts (empty ClusterVersion) are a no-op —
// the pods/list-forbidden, list-failure, or GetNodes-failure warning already
// fired at the call site, and there is no version to gate on.
func finalizeCompat(info CompatInfo, logger *zap.Logger) CompatInfo {
	if info.ClusterVersion == "" {
		return info
	}
	parsed, err := apiversion.ParseGeneric(info.ClusterVersion)
	if err != nil {
		return info
	}

	info.BelowFloorFeatures = belowFloorFeatures(parsed)
	if len(info.BelowFloorFeatures) > 0 {
		logger.Warn(warnBelowFloorPrefix+fmt.Sprintf(
			"Detected version %s; affected features: %s.",
			info.ClusterVersion, strings.Join(info.BelowFloorFeatures, "; "),
		),
			zap.String("cluster_version", info.ClusterVersion),
			zap.Strings("below_floor_features", info.BelowFloorFeatures),
		)
	}
	return info
}

// belowFloorFeatures returns the featureFloors entries cluster does not
// meet, each formatted as "<name> (requires >= <floor>)". A nil cluster
// (undetermined version) is treated as failing every floor.
func belowFloorFeatures(cluster *apiversion.Version) []string {
	var below []string
	for _, f := range featureFloors {
		floor, err := apiversion.ParseGeneric(f.floor)
		if err != nil {
			continue // programmer error in the table itself, not a runtime condition
		}
		if cluster == nil || !cluster.AtLeast(floor) {
			below = append(below, fmt.Sprintf("%s (requires >= %s)", f.name, f.floor))
		}
	}
	return below
}
