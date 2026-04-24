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
