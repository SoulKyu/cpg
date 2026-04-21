package labels

import (
	"sort"
	"strings"

	"github.com/cilium/cilium/pkg/labels"
	slim_metav1 "github.com/cilium/cilium/pkg/k8s/slim/k8s/apis/meta/v1"
	"github.com/cilium/cilium/pkg/policy/api"
)

// Denylist contains label keys that should never appear in selectors.
// These are auto-generated or ephemeral labels that would produce overly
// specific or unstable selectors.
var Denylist = map[string]struct{}{
	"pod-template-hash":                  {},
	"controller-revision-hash":           {},
	"statefulset.kubernetes.io/pod-name": {},
	"job-name":                           {},
	"batch.kubernetes.io/job-name":       {},
	"batch.kubernetes.io/controller-uid": {},
	"apps.kubernetes.io/pod-index":       {},
}

// denylistPrefixes contains label key prefixes that should never appear in
// selectors. Cilium identity labels (io.cilium.k8s.*) are internal metadata,
// not pod labels — using them produces overly specific and fragile selectors.
var denylistPrefixes = []string{
	"io.cilium.k8s.",
	"io.kubernetes.pod.namespace",
	"io.cilium.k8s.policy.",
}

// priorityKeys defines the label selection hierarchy in order of preference.
// Uses standard Kubernetes recommended labels per
// https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
var priorityKeys = []string{
	"app.kubernetes.io/name",
	"app.kubernetes.io/component",
	"app",
}

// filterK8sLabels parses raw Hubble labels and returns only k8s-sourced labels
// that are not in the denylist or denylist prefixes.
func filterK8sLabels(endpointLabels []string) []labels.Label {
	var filtered []labels.Label
	for _, raw := range endpointLabels {
		l := labels.ParseLabel(raw)
		if l.Source != labels.LabelSourceK8s {
			continue
		}
		if _, denied := Denylist[l.Key]; denied {
			continue
		}
		if hasDenylistedPrefix(l.Key) {
			continue
		}
		filtered = append(filtered, l)
	}
	return filtered
}

// hasDenylistedPrefix returns true if the key starts with any denylisted prefix.
func hasDenylistedPrefix(key string) bool {
	for _, prefix := range denylistPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// SelectLabels extracts the most relevant labels from Hubble flow endpoint labels.
// It implements a 3-tier hierarchy:
//  1. If app.kubernetes.io/name is present, return only that label
//  2. If app is present, return only that label
//  3. Otherwise return all k8s labels after denylist filtering
//
// Returns an empty map if all labels are denylisted or non-k8s.
func SelectLabels(endpointLabels []string) map[string]string {
	filtered := filterK8sLabels(endpointLabels)
	if len(filtered) == 0 {
		return map[string]string{}
	}

	// Check priority labels in order
	for _, key := range priorityKeys {
		for _, l := range filtered {
			if l.Key == key {
				return map[string]string{l.Key: l.Value}
			}
		}
	}

	// Fallback: all remaining labels
	result := make(map[string]string, len(filtered))
	for _, l := range filtered {
		result[l.Key] = l.Value
	}
	return result
}

// WorkloadName derives a deterministic workload name from endpoint labels.
// It follows the same priority hierarchy as SelectLabels:
//  1. Value of app.kubernetes.io/name
//  2. Value of app
//  3. Sorted label values joined with "-", truncated to 63 chars (K8s name limit)
//  4. "unknown" if no usable labels remain
func WorkloadName(endpointLabels []string) string {
	filtered := filterK8sLabels(endpointLabels)
	if len(filtered) == 0 {
		return "unknown"
	}

	// Check priority labels in order
	for _, key := range priorityKeys {
		for _, l := range filtered {
			if l.Key == key {
				return l.Value
			}
		}
	}

	// Fallback: sort values deterministically and join
	values := make([]string, 0, len(filtered))
	for _, l := range filtered {
		values = append(values, l.Value)
	}
	sort.Strings(values)
	name := strings.Join(values, "-")

	// Truncate to K8s name limit (63 chars)
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// BuildEndpointSelector creates an EndpointSelector from flow endpoint labels.
// Uses plain keys without the sanitized flag to prevent MarshalJSON from
// applying GetCiliumKeyFrom which corrupts label keys (e.g. app.kubernetes.io/name
// becomes app:kubernetes.io/name where "app" is misinterpreted as a label source).
func BuildEndpointSelector(endpointLabels []string) api.EndpointSelector {
	selected := SelectLabels(endpointLabels)
	if len(selected) == 0 {
		return api.EndpointSelector{
			LabelSelector: &slim_metav1.LabelSelector{},
		}
	}
	return api.EndpointSelector{
		LabelSelector: &slim_metav1.LabelSelector{
			MatchLabels: selected,
		},
	}
}

// BuildPeerSelector creates an EndpointSelector for peer (from/to) endpoints.
// It includes the k8s:io.kubernetes.pod.namespace label when peerNamespace
// differs from policyNamespace to ensure cross-namespace traffic is scoped
// correctly.
// Uses unsanitized EndpointSelector to prevent MarshalJSON from corrupting
// label keys via GetCiliumKeyFrom.
func BuildPeerSelector(peerLabels []string, peerNamespace, policyNamespace string) api.EndpointSelector {
	selected := SelectLabels(peerLabels)

	// Add namespace label for cross-namespace peers
	if peerNamespace != policyNamespace {
		selected["io.kubernetes.pod.namespace"] = peerNamespace
	}

	if len(selected) == 0 {
		return api.EndpointSelector{
			LabelSelector: &slim_metav1.LabelSelector{},
		}
	}
	return api.EndpointSelector{
		LabelSelector: &slim_metav1.LabelSelector{
			MatchLabels: selected,
		},
	}
}
