package hubble

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	observerpb "github.com/cilium/cilium/api/v1/observer"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Client wraps a gRPC connection to Hubble Relay for streaming dropped flows.
type Client struct {
	server     string
	tlsEnabled bool
	timeout    time.Duration
	logger     *zap.Logger

	// streamErr surfaces a genuine transport failure (anything other than a
	// clean io.EOF) from the background stream goroutine to the pipeline. It is
	// created per StreamDroppedFlows call and consumed via StreamErr(); the
	// stream goroutine sends at most one error and closes it on exit.
	streamErr chan error
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

	// grpc.NewClient dials lazily, so without an explicit connectivity check an
	// unreachable relay would block indefinitely on the first Recv() rather than
	// honoring --timeout. Bound connection establishment by c.timeout so the
	// caller fails fast (API-DESIGN: the --timeout knob must have an effect).
	if c.timeout > 0 {
		if err := waitForConnReady(ctx, conn, c.timeout); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
	}

	client := observerpb.NewObserverClient(conn)

	req := &observerpb.GetFlowsRequest{
		Follow:    true,
		Whitelist: buildFilters(namespaces, allNS),
	}

	stream, err := client.GetFlows(ctx, req)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("starting flow stream: %w", err)
	}

	c.streamErr = make(chan error, 1)
	flows, lostEvents := streamFromSource(stream, c.logger, conn, c.streamErr)

	return flows, lostEvents, nil
}

// StreamErr returns a channel that yields a single error if the background
// stream goroutine terminated on a genuine transport failure (not a clean
// io.EOF or context cancellation). The channel is closed when the goroutine
// exits, so callers may range over it. Returns nil before StreamDroppedFlows
// has been called.
func (c *Client) StreamErr() <-chan error {
	return c.streamErr
}

// waitForConnReady blocks until conn reaches connectivity.Ready or timeout
// elapses. It applies the configured --timeout to lazy gRPC connection
// establishment so an unreachable relay is reported immediately instead of
// hanging on the first stream Recv().
func waitForConnReady(ctx context.Context, conn *grpc.ClientConn, timeout time.Duration) error {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if !conn.WaitForStateChange(dialCtx, state) {
			return fmt.Errorf("connecting to hubble relay %q: %w", conn.Target(), dialCtx.Err())
		}
	}
}

// streamFromSource reads from a flowStream and dispatches to typed channels.
// It closes both channels (and onClose if provided) when the stream ends or returns an error.
//
// errCh (optional; nil disables error surfacing) receives a single error when
// the stream terminates on a genuine transport failure — anything other than a
// clean io.EOF or a caller-cancelled context. With Follow:true the server never
// sends a clean EOF, so any such Recv() error is a real failure (relay crash,
// TLS/network reset, RST_STREAM) and must not be mistaken for a completed run.
// The channel is closed on goroutine exit so consumers may range over it.
func streamFromSource(stream flowStream, logger *zap.Logger, onClose io.Closer, errCh chan<- error) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent) {
	flows := make(chan *flowpb.Flow, 256)
	lostEvents := make(chan *flowpb.LostEvent, 16)

	go func() {
		defer close(flows)
		defer close(lostEvents)
		if errCh != nil {
			defer close(errCh)
		}
		defer func() {
			if onClose != nil {
				onClose.Close()
			}
		}()

		for {
			resp, err := stream.Recv()
			if err != nil {
				switch {
				case stream.Context().Err() != nil:
					// Caller cancelled the context — expected shutdown.
					logger.Debug("hubble stream stopped: context cancelled", zap.Error(err))
				case errors.Is(err, io.EOF):
					// Clean end-of-stream (not expected under Follow:true, but harmless).
					logger.Debug("hubble stream ended", zap.Error(err))
				default:
					// Genuine transport failure — surface it loudly and to the caller
					// so a mid-capture failure is not reported as a clean exit 0.
					logger.Warn("hubble stream failed", zap.Error(err))
					if errCh != nil {
						errCh <- fmt.Errorf("hubble stream failed: %w", err)
					}
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
