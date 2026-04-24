package hubble

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	observerpb "github.com/cilium/cilium/api/v1/observer"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Client wraps a gRPC connection to Hubble Relay for streaming dropped flows.
type Client struct {
	server     string
	tlsEnabled bool
	timeout    time.Duration
	logger     *zap.Logger
}

// NewClient creates a new Hubble Relay client.
func NewClient(server string, tlsEnabled bool, timeout time.Duration, logger *zap.Logger) *Client {
	return &Client{
		server:     server,
		tlsEnabled: tlsEnabled,
		timeout:    timeout,
		logger:     logger,
	}
}

// flowStream abstracts the gRPC streaming interface for testability.
type flowStream interface {
	Recv() (*observerpb.GetFlowsResponse, error)
	Context() context.Context
}

// StreamDroppedFlows connects to Hubble Relay and streams dropped flows into
// typed channels. The caller owns the context; cancelling it stops the stream.
// Both returned channels are closed when the stream ends.
func (c *Client) StreamDroppedFlows(ctx context.Context, namespaces []string, allNS bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	var transportCreds grpc.DialOption
	if c.tlsEnabled {
		transportCreds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	} else {
		transportCreds = grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	conn, err := grpc.NewClient(c.server, transportCreds)
	if err != nil {
		return nil, nil, fmt.Errorf("creating gRPC client: %w", err)
	}

	client := observerpb.NewObserverClient(conn)

	req := &observerpb.GetFlowsRequest{
		Follow:    true,
		Whitelist: buildFilters(namespaces, allNS),
	}

	stream, err := client.GetFlows(ctx, req)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("starting flow stream: %w", err)
	}

	flows, lostEvents := streamFromSource(stream, c.logger, conn)

	return flows, lostEvents, nil
}

// streamFromSource reads from a flowStream and dispatches to typed channels.
// It closes both channels (and onClose if provided) when the stream ends or returns an error.
func streamFromSource(stream flowStream, logger *zap.Logger, onClose io.Closer) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent) {
	flows := make(chan *flowpb.Flow, 256)
	lostEvents := make(chan *flowpb.LostEvent, 16)

	go func() {
		defer close(flows)
		defer close(lostEvents)
		defer func() {
			if onClose != nil {
				onClose.Close()
			}
		}()

		for {
			resp, err := stream.Recv()
			if err != nil {
				if stream.Context().Err() == nil {
					logger.Debug("hubble stream ended", zap.Error(err))
				}
				return
			}

			if f := resp.GetFlow(); f != nil {
				select {
				case flows <- f:
				case <-stream.Context().Done():
					return
				}
			}

			if le := resp.GetLostEvents(); le != nil {
				select {
				case lostEvents <- le:
				case <-stream.Context().Done():
					return
				}
			}
		}
	}()

	return flows, lostEvents
}

// buildFilters constructs FlowFilter whitelist entries to filter dropped flows
// by namespace. Multiple whitelist filters are OR-ed; fields within a single
// filter are AND-ed.
func buildFilters(namespaces []string, allNS bool) []*flowpb.FlowFilter {
	if allNS || len(namespaces) == 0 {
		return []*flowpb.FlowFilter{
			{Verdict: []flowpb.Verdict{flowpb.Verdict_DROPPED}},
		}
	}

	prefixes := make([]string, len(namespaces))
	for i, ns := range namespaces {
		prefixes[i] = ns + "/"
	}

	return []*flowpb.FlowFilter{
		{
			Verdict:   []flowpb.Verdict{flowpb.Verdict_DROPPED},
			SourcePod: prefixes,
		},
		{
			Verdict:        []flowpb.Verdict{flowpb.Verdict_DROPPED},
			DestinationPod: prefixes,
		},
	}
}
