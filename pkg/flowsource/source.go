// Package flowsource decouples the streaming source of Hubble flows from the
// streaming pipeline, so the same pipeline can be fed by a live gRPC client
// or by an offline capture file.
package flowsource

import (
	"context"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

// FlowSource abstracts the streaming source for testability and offline replay.
// Implementations MUST close both returned channels when the stream ends.
type FlowSource interface {
	StreamDroppedFlows(ctx context.Context, namespaces []string, allNS bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error)
}
