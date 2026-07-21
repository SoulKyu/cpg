package explain

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

func TestFilterDirectionAndPort(t *testing.T) {
	rule := evidence.RuleEvidence{Direction: "ingress", Port: "8080"}
	f := Filter{Direction: "ingress", Port: "8080"}
	assert.True(t, f.Match(rule))

	f.Port = "9090"
	assert.False(t, f.Match(rule))
}

func TestFilterPeerLabel(t *testing.T) {
	rule := evidence.RuleEvidence{Peer: evidence.PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}}}
	f := Filter{}
	f.PeerLabel.Set = true
	f.PeerLabel.Key, f.PeerLabel.Value = "app", "x"
	assert.True(t, f.Match(rule))

	f.PeerLabel.Value = "y"
	assert.False(t, f.Match(rule))
}

func TestFilterPeerCIDRContainment(t *testing.T) {
	_, filterNet, _ := net.ParseCIDR("10.0.0.0/8")
	rule := evidence.RuleEvidence{Peer: evidence.PeerRef{Type: "cidr", CIDR: "10.0.1.0/24"}}
	f := Filter{PeerCIDR: filterNet}
	assert.True(t, f.Match(rule))

	rule.Peer.CIDR = "192.168.0.0/16"
	assert.False(t, f.Match(rule))

	rule.Peer.CIDR = "10.0.0.0/4" // broader than filter — should not match
	assert.False(t, f.Match(rule))
}

func TestFilterSince(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	rule := evidence.RuleEvidence{LastSeen: now.Add(-5 * time.Minute)}
	f := Filter{Since: 10 * time.Minute, Now: now}
	assert.True(t, f.Match(rule))

	f.Since = 1 * time.Minute
	assert.False(t, f.Match(rule))
}

func httpRule() evidence.RuleEvidence {
	return evidence.RuleEvidence{
		Direction: "egress",
		L7: &evidence.L7Ref{
			Protocol:   "http",
			HTTPMethod: "GET",
			HTTPPath:   "^/foo$",
		},
	}
}

func dnsRule() evidence.RuleEvidence {
	return evidence.RuleEvidence{
		Direction: "egress",
		L7: &evidence.L7Ref{
			Protocol:     "dns",
			DNSMatchName: "api.example.com",
		},
	}
}

func TestFilterHTTPMethod(t *testing.T) {
	r := httpRule()
	assert.True(t, Filter{HTTPMethod: "GET"}.Match(r))
	// L4-only rule with any L7 filter set → drop.
	assert.False(t, Filter{HTTPMethod: "GET"}.Match(evidence.RuleEvidence{Direction: "egress"}))
	// Non-matching method.
	assert.False(t, Filter{HTTPMethod: "POST"}.Match(r))
	// DNS rule with HTTP method filter → drop (Protocol mismatch).
	assert.False(t, Filter{HTTPMethod: "GET"}.Match(dnsRule()))
}

func TestFilterHTTPPath(t *testing.T) {
	r := httpRule()
	assert.True(t, Filter{HTTPPath: "^/foo$"}.Match(r))
	// Literal exact: substring/unanchored does not match.
	assert.False(t, Filter{HTTPPath: "/foo"}.Match(r))
	// L4-only → drop.
	assert.False(t, Filter{HTTPPath: "^/foo$"}.Match(evidence.RuleEvidence{}))
}

func TestFilterDNSPattern(t *testing.T) {
	r := dnsRule()
	assert.True(t, Filter{DNSPattern: "api.example.com"}.Match(r))
	// Wildcard literal exact match (v1.2 doesn't generate them, but filter is exact).
	wild := evidence.RuleEvidence{L7: &evidence.L7Ref{Protocol: "dns", DNSMatchName: "*.example.com"}}
	assert.True(t, Filter{DNSPattern: "*.example.com"}.Match(wild))
	// HTTP rule with DNS filter → drop.
	assert.False(t, Filter{DNSPattern: "api.example.com"}.Match(httpRule()))
}

func TestFilterAndCombination(t *testing.T) {
	r := httpRule()
	assert.True(t, Filter{HTTPMethod: "GET", HTTPPath: "^/foo$"}.Match(r))
	// AND requires both — wrong path → false.
	assert.False(t, Filter{HTTPMethod: "GET", HTTPPath: "^/bar$"}.Match(r))
	// HTTP method + DNS pattern on HTTP rule → false (DNS branch fails).
	assert.False(t, Filter{HTTPMethod: "GET", DNSPattern: "x.com"}.Match(r))
}

func TestFilterL4OnlyNoL7Filters(t *testing.T) {
	// No L7 filters set → existing v1.1 behavior preserved (L4-only rule matches).
	r := evidence.RuleEvidence{Direction: "egress", Port: "80"}
	assert.True(t, Filter{}.Match(r))
}
