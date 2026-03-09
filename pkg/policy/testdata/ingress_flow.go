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

// WorldEgressTCPFlow builds a dropped egress TCP flow to a world (external) destination.
// The destination endpoint has Identity=2 and reserved:world label.
func WorldEgressTCPFlow(srcLabels []string, srcNs string, dstIP string, dstPort uint32) *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Source: &flowpb.Endpoint{
			Labels:    srcLabels,
			Namespace: srcNs,
		},
		Destination: &flowpb.Endpoint{
			Identity: 2,
			Labels:   []string{"reserved:world"},
		},
		IP: &flowpb.IP{
			Destination: dstIP,
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

// WorldIngressTCPFlow builds a dropped ingress TCP flow from a world (external) source.
// The source endpoint has Identity=2 and reserved:world label.
func WorldIngressTCPFlow(srcIP string, srcPort uint32, dstLabels []string, dstNs string) *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Identity: 2,
			Labels:   []string{"reserved:world"},
		},
		Destination: &flowpb.Endpoint{
			Labels:    dstLabels,
			Namespace: dstNs,
		},
		IP: &flowpb.IP{
			Source: srcIP,
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{
					DestinationPort: srcPort,
				},
			},
		},
	}
}

// WorldFlowNilIP builds a world identity flow with nil IP (edge case).
func WorldFlowNilIP() *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Identity: 2,
			Labels:   []string{"reserved:world"},
		},
		IP: nil,
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{
					DestinationPort: 443,
				},
			},
		},
	}
}

// IngressICMPv4Flow builds a dropped ingress ICMPv4 flow for testing.
func IngressICMPv4Flow(srcLabels []string, srcNs string, dstLabels []string, dstNs string, icmpType uint32) *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    srcLabels,
			Namespace: srcNs,
		},
		Destination: &flowpb.Endpoint{
			Labels:    dstLabels,
			Namespace: dstNs,
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_ICMPv4{
				ICMPv4: &flowpb.ICMPv4{
					Type: icmpType,
				},
			},
		},
	}
}

// EntityIngressFlow builds a dropped ingress flow from a reserved entity.
func EntityIngressFlow(srcLabels []string, dstLabels []string, dstNs string, dstPort uint32) *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels: srcLabels,
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

// EgressICMPv4Flow builds a dropped egress ICMPv4 flow for testing.
func EgressICMPv4Flow(srcLabels []string, srcNs string, dstLabels []string, dstIP string, icmpType uint32) *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Source: &flowpb.Endpoint{
			Labels:    srcLabels,
			Namespace: srcNs,
		},
		Destination: &flowpb.Endpoint{
			Labels: dstLabels,
		},
		IP: &flowpb.IP{
			Destination: dstIP,
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_ICMPv4{
				ICMPv4: &flowpb.ICMPv4{
					Type: icmpType,
				},
			},
		},
	}
}

// EntityEgressFlow builds a dropped egress flow to a reserved entity (e.g., kube-apiserver).
func EntityEgressFlow(srcLabels []string, srcNs string, dstLabels []string, dstIP string, dstPort uint32) *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Source: &flowpb.Endpoint{
			Labels:    srcLabels,
			Namespace: srcNs,
		},
		Destination: &flowpb.Endpoint{
			Labels: dstLabels,
		},
		IP: &flowpb.IP{
			Destination: dstIP,
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
