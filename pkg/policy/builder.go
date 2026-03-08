package policy

import (
	flowpb "github.com/cilium/cilium/api/v1/flow"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
)

// PolicyEvent wraps a generated CiliumNetworkPolicy with its target location.
type PolicyEvent struct {
	Namespace string
	Workload  string
	Policy    *ciliumv2.CiliumNetworkPolicy
}

// BuildPolicy transforms a set of Hubble dropped flows into a CiliumNetworkPolicy.
func BuildPolicy(namespace, workload string, flows []*flowpb.Flow) *ciliumv2.CiliumNetworkPolicy {
	// TODO: implement
	return &ciliumv2.CiliumNetworkPolicy{}
}
