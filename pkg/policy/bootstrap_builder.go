package policy

import (
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/SoulKyu/cpg/pkg/labels"
)

// BuildBootstrapPolicy constructs a namespaced default-deny CiliumNetworkPolicy.
//
// The Ingress/Egress one-element-empty-rule-object pattern ([]api.IngressRule{{}}
// / []api.EgressRule{{}}, NOT []api.IngressRule{}/[]api.EgressRule{} — see
// 22-RESEARCH.md Pitfall 1) is load-bearing: it is what makes the policy pass
// Cilium's own Sanitize() AND actually enforce default-deny, independent of the
// enableDefaultDeny field's own version-gated behavior (cilium/cilium#35558 —
// an empty-slice ingress/egress either fails Sanitize() outright on 1.17+
// agents or is accepted-but-silently-non-enforcing on pre-fix agents at the
// 1.16 floor this phase targets).
//
// The artifact is named "default-deny-<namespace>" by direct string
// concatenation, deliberately independent of this package's per-workload
// naming helper (Pitfall 5) — bootstrap artifacts are not generate's
// per-workload output and must never become merge-target candidates for
// pkg/policy/merge.go's cpg-* dedup logic.
func BuildBootstrapPolicy(namespace string) *ciliumv2.CiliumNetworkPolicy {
	t := true
	return &ciliumv2.CiliumNetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cilium.io/v2",
			Kind:       "CiliumNetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-deny-" + namespace,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "cpg",
			},
		},
		Spec: &api.Rule{
			EndpointSelector: labels.BuildEndpointSelector(nil), // "select all" — proven fallback shape
			Ingress:          []api.IngressRule{{}},
			Egress:           []api.EgressRule{{}},
			EnableDefaultDeny: api.DefaultDenyConfig{
				Ingress: &t,
				Egress:  &t,
			},
		},
	}
}
