package main

import (
	"net"
	"strings"
	"time"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

type explainFilter struct {
	Direction string
	Port      string
	PeerLabel struct {
		Key   string
		Value string
		Set   bool
	}
	PeerCIDR *net.IPNet
	Since    time.Duration
	Now      time.Time

	// L7 filters. Empty string = unset. Inputs are normalized in buildFilter
	// (HTTPMethod uppercased, DNSPattern trailing dot stripped). When ANY of
	// these is set, rules without an L7Ref are dropped from the matched set.
	HTTPMethod string
	HTTPPath   string
	DNSPattern string
}

func (f explainFilter) match(r evidence.RuleEvidence) bool {
	if f.Direction != "" && r.Direction != f.Direction {
		return false
	}
	if f.Port != "" && r.Port != f.Port {
		return false
	}
	if f.PeerLabel.Set {
		if r.Peer.Type != "endpoint" {
			return false
		}
		v, ok := r.Peer.Labels[f.PeerLabel.Key]
		if !ok || v != f.PeerLabel.Value {
			return false
		}
	}
	if f.PeerCIDR != nil {
		if r.Peer.Type != "cidr" {
			return false
		}
		ruleIP, ruleNet, err := net.ParseCIDR(r.Peer.CIDR)
		if err != nil || !f.PeerCIDR.Contains(ruleIP) {
			return false
		}
		fOnes, _ := f.PeerCIDR.Mask.Size()
		rOnes, _ := ruleNet.Mask.Size()
		if rOnes < fOnes {
			return false
		}
	}
	if f.Since > 0 && !r.LastSeen.IsZero() && r.LastSeen.Before(f.Now.Add(-f.Since)) {
		return false
	}
	if f.HTTPMethod != "" || f.HTTPPath != "" || f.DNSPattern != "" {
		if r.L7 == nil {
			return false
		}
		if f.HTTPMethod != "" {
			if r.L7.Protocol != "http" || r.L7.HTTPMethod != f.HTTPMethod {
				return false
			}
		}
		if f.HTTPPath != "" {
			if r.L7.Protocol != "http" || r.L7.HTTPPath != f.HTTPPath {
				return false
			}
		}
		if f.DNSPattern != "" {
			if r.L7.Protocol != "dns" || r.L7.DNSMatchName != f.DNSPattern {
				return false
			}
		}
	}
	return true
}

func parsePeerLabel(s string) (key, value string, ok bool) {
	if s == "" {
		return "", "", false
	}
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
