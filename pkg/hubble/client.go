package hubble

import (
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"go.uber.org/zap"
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
