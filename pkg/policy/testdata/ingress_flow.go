package testdata

import (
	flowpb "github.com/cilium/cilium/api/v1/flow"
)

// IngressTCPFlow builds a dropped ingress TCP flow for testing.
// srcLabels and dstLabels are in "source:key=value" Hubble format.
func IngressTCPFlow(srcLabels, dstLabels []string, dstNs string, dstPort uint32) *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    srcLabels,
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    dstLabels,
			Namespace: dstNs,
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{
					DestinationPort: dstPort,
				},
			},
		},
	}
}

// EgressUDPFlow builds a dropped egress UDP flow for testing.
func EgressUDPFlow(srcLabels, dstLabels []string, srcNs string, dstPort uint32) *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Source: &flowpb.Endpoint{
			Labels:    srcLabels,
			Namespace: srcNs,
		},
		Destination: &flowpb.Endpoint{
			Labels:    dstLabels,
			Namespace: "default",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_UDP{
				UDP: &flowpb.UDP{
					DestinationPort: dstPort,
				},
			},
		},
	}
}

// NilL4Flow builds a flow with nil L4 layer (edge case).
func NilL4Flow() *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "default",
		},
		L4: nil,
	}
}
