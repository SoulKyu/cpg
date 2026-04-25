package main

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

func TestFilterDirectionAndPort(t *testing.T) {
	rule := evidence.RuleEvidence{Direction: "ingress", Port: "8080"}
	f := explainFilter{Direction: "ingress", Port: "8080"}
	assert.True(t, f.match(rule))

	f.Port = "9090"
	assert.False(t, f.match(rule))
}

func TestFilterPeerLabel(t *testing.T) {
	rule := evidence.RuleEvidence{Peer: evidence.PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}}}
	f := explainFilter{}
	f.PeerLabel.Set = true
	f.PeerLabel.Key, f.PeerLabel.Value = "app", "x"
	assert.True(t, f.match(rule))

	f.PeerLabel.Value = "y"
	assert.False(t, f.match(rule))
}

func TestFilterPeerCIDRContainment(t *testing.T) {
	_, filterNet, _ := net.ParseCIDR("10.0.0.0/8")
	rule := evidence.RuleEvidence{Peer: evidence.PeerRef{Type: "cidr", CIDR: "10.0.1.0/24"}}
	f := explainFilter{PeerCIDR: filterNet}
	assert.True(t, f.match(rule))

	rule.Peer.CIDR = "192.168.0.0/16"
	assert.False(t, f.match(rule))

	rule.Peer.CIDR = "10.0.0.0/4" // broader than filter — should not match
	assert.False(t, f.match(rule))
}

func TestFilterSince(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	rule := evidence.RuleEvidence{LastSeen: now.Add(-5 * time.Minute)}
	f := explainFilter{Since: 10 * time.Minute, Now: now}
	assert.True(t, f.match(rule))

	f.Since = 1 * time.Minute
	assert.False(t, f.match(rule))
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
	assert.True(t, explainFilter{HTTPMethod: "GET"}.match(r))
	// L4-only rule with any L7 filter set → drop.
	assert.False(t, explainFilter{HTTPMethod: "GET"}.match(evidence.RuleEvidence{Direction: "egress"}))
	// Non-matching method.
	assert.False(t, explainFilter{HTTPMethod: "POST"}.match(r))
	// DNS rule with HTTP method filter → drop (Protocol mismatch).
	assert.False(t, explainFilter{HTTPMethod: "GET"}.match(dnsRule()))
}

func TestFilterHTTPPath(t *testing.T) {
	r := httpRule()
	assert.True(t, explainFilter{HTTPPath: "^/foo$"}.match(r))
	// Literal exact: substring/unanchored does not match.
	assert.False(t, explainFilter{HTTPPath: "/foo"}.match(r))
	// L4-only → drop.
	assert.False(t, explainFilter{HTTPPath: "^/foo$"}.match(evidence.RuleEvidence{}))
}

func TestFilterDNSPattern(t *testing.T) {
	r := dnsRule()
	assert.True(t, explainFilter{DNSPattern: "api.example.com"}.match(r))
	// Wildcard literal exact match (v1.2 doesn't generate them, but filter is exact).
	wild := evidence.RuleEvidence{L7: &evidence.L7Ref{Protocol: "dns", DNSMatchName: "*.example.com"}}
	assert.True(t, explainFilter{DNSPattern: "*.example.com"}.match(wild))
	// HTTP rule with DNS filter → drop.
	assert.False(t, explainFilter{DNSPattern: "api.example.com"}.match(httpRule()))
}

func TestFilterAndCombination(t *testing.T) {
	r := httpRule()
	assert.True(t, explainFilter{HTTPMethod: "GET", HTTPPath: "^/foo$"}.match(r))
	// AND requires both — wrong path → false.
	assert.False(t, explainFilter{HTTPMethod: "GET", HTTPPath: "^/bar$"}.match(r))
	// HTTP method + DNS pattern on HTTP rule → false (DNS branch fails).
	assert.False(t, explainFilter{HTTPMethod: "GET", DNSPattern: "x.com"}.match(r))
}

func TestFilterL4OnlyNoL7Filters(t *testing.T) {
	// No L7 filters set → existing v1.1 behavior preserved (L4-only rule matches).
	r := evidence.RuleEvidence{Direction: "egress", Port: "80"}
	assert.True(t, explainFilter{}.match(r))
}
