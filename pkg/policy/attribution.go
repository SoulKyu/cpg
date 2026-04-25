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

// L7Discriminator carries the optional L7 attributes that distinguish two
// rules sharing the same (direction, peer, port, protocol) tuple. When nil
// on a RuleKey, the key stringifies to the v1.1 4-segment form. Phase 8/9
// populate this; Phase 7 leaves it nil at all callsites.
//
// Protocol is "http" or "dns". HTTPMethod / HTTPPath apply when Protocol ==
// "http"; DNSMatchName applies when Protocol == "dns". All sub-fields are
// omitempty in the string encoding.
type L7Discriminator struct {
	Protocol     string
	HTTPMethod   string
	HTTPPath     string
	DNSMatchName string
}

// RuleKey uniquely identifies a rule within a policy in a stable, sortable form.
type RuleKey struct {
	Direction string // "ingress" | "egress"
	Peer      Peer
	Port      string
	Protocol  string
	// L7 is an optional discriminator. nil → backward-compatible v1.1 key;
	// non-nil → the encoded form appends an ":l7=…" segment so two rules
	// differing only by HTTP method/path do not dedup into the same evidence
	// bucket (EVID2-02).
	L7 *L7Discriminator
}

// String renders a deterministic string representation suitable for use as a
// map key and stable across runs. When L7 is nil, the encoding matches v1.1
// byte-for-byte.
func (k RuleKey) String() string {
	base := fmt.Sprintf("%s:%s:%s:%s", k.Direction, encodePeer(k.Peer), k.Protocol, k.Port)
	if k.L7 == nil {
		return base
	}
	return base + ":l7=" + encodeL7(k.L7)
}

func encodeL7(d *L7Discriminator) string {
	var parts []string
	if d.Protocol != "" {
		parts = append(parts, "proto="+d.Protocol)
	}
	if d.HTTPMethod != "" {
		parts = append(parts, "method="+d.HTTPMethod)
	}
	if d.HTTPPath != "" {
		parts = append(parts, "path="+d.HTTPPath)
	}
	if d.DNSMatchName != "" {
		parts = append(parts, "dns="+d.DNSMatchName)
	}
	return strings.Join(parts, ",")
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
