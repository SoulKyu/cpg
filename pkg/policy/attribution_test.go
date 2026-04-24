// pkg/policy/attribution_test.go
package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRuleKeyEndpointPeer(t *testing.T) {
	k := RuleKey{
		Direction: "ingress",
		Peer:      Peer{Type: PeerEndpoint, Labels: map[string]string{"app": "x", "env": "prod"}},
		Port:      "8080",
		Protocol:  "TCP",
	}
	assert.Equal(t, "ingress:ep:app=x,env=prod:TCP:8080", k.String())
}

func TestRuleKeyCIDRPeer(t *testing.T) {
	k := RuleKey{
		Direction: "egress",
		Peer:      Peer{Type: PeerCIDR, CIDR: "10.0.0.0/24"},
		Port:      "443",
		Protocol:  "TCP",
	}
	assert.Equal(t, "egress:cidr:10.0.0.0/24:TCP:443", k.String())
}

func TestRuleKeyEntityPeer(t *testing.T) {
	k := RuleKey{
		Direction: "ingress",
		Peer:      Peer{Type: PeerEntity, Entity: "host"},
		Port:      "22",
		Protocol:  "TCP",
	}
	assert.Equal(t, "ingress:entity:host:TCP:22", k.String())
}

func TestRuleKeyLabelsDeterministic(t *testing.T) {
	k1 := RuleKey{
		Direction: "ingress",
		Peer:      Peer{Type: PeerEndpoint, Labels: map[string]string{"b": "2", "a": "1"}},
		Port:      "80", Protocol: "TCP",
	}
	k2 := RuleKey{
		Direction: "ingress",
		Peer:      Peer{Type: PeerEndpoint, Labels: map[string]string{"a": "1", "b": "2"}},
		Port:      "80", Protocol: "TCP",
	}
	assert.Equal(t, k1.String(), k2.String())
}
