// pkg/policy/attribution.go
package policy

import (
	"fmt"
	"sort"
	"strings"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

// PeerType identifies the kind of peer a rule addresses.
type PeerType string

const (
	PeerEndpoint PeerType = "endpoint"
	PeerCIDR     PeerType = "cidr"
	PeerEntity   PeerType = "entity"
)

// Peer is a uniform description of a rule peer used for attribution.
type Peer struct {
	Type   PeerType
	Labels map[string]string
	CIDR   string
	Entity string
}

// RuleKey uniquely identifies a rule within a policy in a stable, sortable form.
type RuleKey struct {
	Direction string // "ingress" | "egress"
	Peer      Peer
	Port      string
	Protocol  string
}

// String renders a deterministic string representation suitable for use as a
// map key and stable across runs.
func (k RuleKey) String() string {
	return fmt.Sprintf("%s:%s:%s:%s", k.Direction, encodePeer(k.Peer), k.Protocol, k.Port)
}

func encodePeer(p Peer) string {
	switch p.Type {
	case PeerEndpoint:
		keys := make([]string, 0, len(p.Labels))
		for k := range p.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + "=" + p.Labels[k]
		}
		return "ep:" + strings.Join(parts, ",")
	case PeerCIDR:
		return "cidr:" + p.CIDR
	case PeerEntity:
		return "entity:" + p.Entity
	default:
		return "unknown:"
	}
}

// RuleAttribution records, for each rule emitted in a policy, the flows that
// contributed to its creation during the current session.
type RuleAttribution struct {
	Key       RuleKey
	FlowCount int64
	FirstSeen time.Time
	LastSeen  time.Time
	Samples   []*flowpb.Flow // capped by caller
}

// AttributionOptions controls how much per-rule flow evidence is retained
// during BuildPolicy. When MaxSamples is 0, no attribution is tracked — the
// returned slice is nil.
type AttributionOptions struct {
	MaxSamples int
}
