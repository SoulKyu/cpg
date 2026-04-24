// pkg/flowsource/source_test.go
package flowsource

import (
	"context"
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

type stubSource struct{}

func (stubSource) StreamDroppedFlows(_ context.Context, _ []string, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	return nil, nil, nil
}

func TestFlowSourceInterfaceSatisfied(t *testing.T) {
	var _ FlowSource = stubSource{}
}
