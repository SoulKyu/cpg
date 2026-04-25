// Package testdata — shared test helpers for pkg/policy.
//
// AssertHasKubeDNSCompanion is the shared invariant test helper enforcing
// DNS-02 across every test that produces a toFQDNs-bearing CNP, regardless of
// whether the test lives in the internal `policy` package or the external
// `policy_test` package.
package testdata

import (
	"testing"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
)

// AssertHasKubeDNSCompanion fails the test when the supplied CNP does not
// carry a kube-dns companion egress rule (selector k8s-app=kube-dns +
// io.kubernetes.pod.namespace=kube-system, ports 53/UDP + 53/TCP, with at
// least one DNS L7 matchName entry). DNS-02 invariant.
//
// Tolerant of the Cilium "any:" / "k8s:" label-source prefix that appears
// after YAML roundtrips.
func AssertHasKubeDNSCompanion(t *testing.T, cnp *ciliumv2.CiliumNetworkPolicy) {
	t.Helper()
	if cnp == nil || cnp.Spec == nil {
		t.Fatalf("AssertHasKubeDNSCompanion: cnp or cnp.Spec is nil")
	}
	for _, eg := range cnp.Spec.Egress {
		if !egressSelectsKubeDNS(eg) {
			continue
		}
		hasUDP, hasTCP, hasDNSRule := false, false, false
		for _, pr := range eg.ToPorts {
			for _, p := range pr.Ports {
				if p.Port != "53" {
					continue
				}
				switch p.Protocol {
				case api.ProtoUDP:
					hasUDP = true
				case api.ProtoTCP:
					hasTCP = true
				case api.ProtoAny:
					hasUDP = true
					hasTCP = true
				}
			}
			if pr.Rules != nil && len(pr.Rules.DNS) > 0 {
				hasDNSRule = true
			}
		}
		if hasUDP && hasTCP && hasDNSRule {
			return
		}
	}
	t.Errorf("kube-dns companion egress rule missing or incomplete (need k8s-app=kube-dns + io.kubernetes.pod.namespace=kube-system + 53/UDP + 53/TCP + DNS rule); got egress=%+v", cnp.Spec.Egress)
}

func egressSelectsKubeDNS(eg api.EgressRule) bool {
	for _, ep := range eg.ToEndpoints {
		if ep.LabelSelector == nil {
			continue
		}
		hasApp, hasNS := false, false
		for k, v := range ep.LabelSelector.MatchLabels {
			bare := stripLabelSourcePrefix(k)
			if bare == "k8s-app" && v == "kube-dns" {
				hasApp = true
			}
			if bare == "io.kubernetes.pod.namespace" && v == "kube-system" {
				hasNS = true
			}
		}
		if hasApp && hasNS {
			return true
		}
	}
	return false
}

func stripLabelSourcePrefix(k string) string {
	for _, prefix := range []string{"any:", "k8s:"} {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			return k[len(prefix):]
		}
	}
	return k
}
