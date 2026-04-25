package policy

import (
	"testing"

	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergePortRules_PreservesRules is a regression test for the latent bug in
// mergePortRules where the Rules (L7) field of api.PortRule was silently
// dropped during merge. Before Phase 7, no caller populated Rules so the bug
// was harmless; the moment Phase 8 attaches HTTP rules, this becomes silent
// L7 data loss. The test must FAIL on the pre-fix implementation and PASS
// after the fix.
//
// Sub-cases mirror the ordering in the plan (07-01) verification block:
//
//	a) existing has Rules, incoming has none on same port  → preserve existing Rules.
//	b) incoming has Rules, existing has none on same port  → pick up incoming Rules.
//	c) both have Rules on the same port                    → union-merge HTTP by (Method, Path).
//	d) different ports, each with Rules                    → both PortRules preserved with their Rules intact.
//	e) L4-only fast path (no Rules anywhere)               → behavior unchanged from v1.1.
func TestMergePortRules_PreservesRules(t *testing.T) {
	httpGetA := api.PortRuleHTTP{Method: "GET", Path: "/a"}
	httpPostB := api.PortRuleHTTP{Method: "POST", Path: "/b"}

	tcp80 := api.PortProtocol{Port: "80", Protocol: api.ProtoTCP}
	tcp443 := api.PortProtocol{Port: "443", Protocol: api.ProtoTCP}

	t.Run("a) existing has Rules, incoming has none on same port", func(t *testing.T) {
		existing := api.PortRules{
			{
				Ports: []api.PortProtocol{tcp80},
				Rules: &api.L7Rules{HTTP: []api.PortRuleHTTP{httpGetA}},
			},
		}
		incoming := api.PortRules{
			{Ports: []api.PortProtocol{tcp80}},
		}
		merged := mergePortRules(existing, incoming)
		require.Len(t, merged, 1, "same (port, proto) collapses to a single PortRule")
		require.Len(t, merged[0].Ports, 1)
		assert.Equal(t, "80", merged[0].Ports[0].Port)
		require.NotNil(t, merged[0].Rules, "existing Rules must be preserved")
		require.Len(t, merged[0].Rules.HTTP, 1)
		assert.Equal(t, "GET", merged[0].Rules.HTTP[0].Method)
		assert.Equal(t, "/a", merged[0].Rules.HTTP[0].Path)
	})

	t.Run("b) incoming has Rules, existing has none on same port", func(t *testing.T) {
		existing := api.PortRules{
			{Ports: []api.PortProtocol{tcp80}},
		}
		incoming := api.PortRules{
			{
				Ports: []api.PortProtocol{tcp80},
				Rules: &api.L7Rules{HTTP: []api.PortRuleHTTP{httpGetA}},
			},
		}
		merged := mergePortRules(existing, incoming)
		require.Len(t, merged, 1)
		require.NotNil(t, merged[0].Rules, "incoming Rules must be picked up")
		require.Len(t, merged[0].Rules.HTTP, 1)
		assert.Equal(t, "GET", merged[0].Rules.HTTP[0].Method)
		assert.Equal(t, "/a", merged[0].Rules.HTTP[0].Path)
	})

	t.Run("c) both have Rules on the same port — HTTP union-merge by (Method, Path)", func(t *testing.T) {
		existing := api.PortRules{
			{
				Ports: []api.PortProtocol{tcp80},
				Rules: &api.L7Rules{HTTP: []api.PortRuleHTTP{httpGetA}},
			},
		}
		incoming := api.PortRules{
			{
				Ports: []api.PortProtocol{tcp80},
				Rules: &api.L7Rules{HTTP: []api.PortRuleHTTP{httpGetA, httpPostB}},
			},
		}
		merged := mergePortRules(existing, incoming)
		require.Len(t, merged, 1)
		require.NotNil(t, merged[0].Rules)
		require.Len(t, merged[0].Rules.HTTP, 2, "duplicates dedup, new entry appended")
		// Order: existing observation first (httpGetA), new entry next (httpPostB).
		assert.Equal(t, "GET", merged[0].Rules.HTTP[0].Method)
		assert.Equal(t, "/a", merged[0].Rules.HTTP[0].Path)
		assert.Equal(t, "POST", merged[0].Rules.HTTP[1].Method)
		assert.Equal(t, "/b", merged[0].Rules.HTTP[1].Path)
	})

	t.Run("d) different ports, each with Rules → both preserved", func(t *testing.T) {
		existing := api.PortRules{
			{
				Ports: []api.PortProtocol{tcp80},
				Rules: &api.L7Rules{HTTP: []api.PortRuleHTTP{httpGetA}},
			},
		}
		incoming := api.PortRules{
			{
				Ports: []api.PortProtocol{tcp443},
				Rules: &api.L7Rules{HTTP: []api.PortRuleHTTP{httpPostB}},
			},
		}
		merged := mergePortRules(existing, incoming)
		require.Len(t, merged, 2, "distinct (port, proto) keys produce distinct PortRules")

		// Find each by port — order is determined by the merge implementation but
		// each PortRule MUST carry its own Rules.
		var foundA, foundB bool
		for _, pr := range merged {
			require.Len(t, pr.Ports, 1)
			require.NotNil(t, pr.Rules, "each PortRule must keep its Rules")
			require.Len(t, pr.Rules.HTTP, 1)
			switch pr.Ports[0].Port {
			case "80":
				foundA = true
				assert.Equal(t, "GET", pr.Rules.HTTP[0].Method)
				assert.Equal(t, "/a", pr.Rules.HTTP[0].Path)
			case "443":
				foundB = true
				assert.Equal(t, "POST", pr.Rules.HTTP[0].Method)
				assert.Equal(t, "/b", pr.Rules.HTTP[0].Path)
			default:
				t.Fatalf("unexpected port: %s", pr.Ports[0].Port)
			}
		}
		assert.True(t, foundA, "port 80 PortRule with Rules must be present")
		assert.True(t, foundB, "port 443 PortRule with Rules must be present")
	})

	t.Run("e) L4-only fast path remains byte-stable", func(t *testing.T) {
		// Two existing ports + one incoming port on a brand new port: result must
		// keep all three, no Rules anywhere, identical to v1.1 behavior.
		existing := api.PortRules{
			{Ports: []api.PortProtocol{tcp80, tcp443}},
		}
		incoming := api.PortRules{
			{Ports: []api.PortProtocol{{Port: "8080", Protocol: api.ProtoTCP}}},
		}
		merged := mergePortRules(existing, incoming)
		require.Len(t, merged, 1, "L4-only path collapses to a single PortRule (v1.1 behavior)")
		assert.Nil(t, merged[0].Rules, "no Rules on either input → result.Rules stays nil")
		require.Len(t, merged[0].Ports, 3)
		// Port order: existing first (preserve v1.1 ordering), incoming appended.
		assert.Equal(t, "80", merged[0].Ports[0].Port)
		assert.Equal(t, "443", merged[0].Ports[1].Port)
		assert.Equal(t, "8080", merged[0].Ports[2].Port)
	})
}

// TestMergePortRules_L4OnlyByteIdentical asserts the L4-only merge path
// produces output identical to the v1.1 implementation: a single PortRule
// with merged Ports list, Rules == nil, in first-observation order.
func TestMergePortRules_L4OnlyByteIdentical(t *testing.T) {
	existing := api.PortRules{
		{Ports: []api.PortProtocol{
			{Port: "80", Protocol: api.ProtoTCP},
			{Port: "443", Protocol: api.ProtoTCP},
		}},
	}
	incoming := api.PortRules{
		{Ports: []api.PortProtocol{
			{Port: "443", Protocol: api.ProtoTCP}, // duplicate
			{Port: "8080", Protocol: api.ProtoTCP},
		}},
	}
	merged := mergePortRules(existing, incoming)
	require.Len(t, merged, 1)
	assert.Nil(t, merged[0].Rules)
	require.Len(t, merged[0].Ports, 3)
	assert.Equal(t, "80", merged[0].Ports[0].Port)
	assert.Equal(t, "443", merged[0].Ports[1].Port)
	assert.Equal(t, "8080", merged[0].Ports[2].Port)
}

// TestNormalizeRule_SortsL7Lists asserts normalizeRule deterministically
// sorts Rules.HTTP by (Method, Path) and Rules.DNS by MatchName, leaves
// nil/empty Rules untouched, and preserves the nil-vs-empty distinction
// (Pitfall 12).
func TestNormalizeRule_SortsL7Lists(t *testing.T) {
	t.Run("HTTP sorted by (Method, Path)", func(t *testing.T) {
		rule := &api.Rule{
			Egress: []api.EgressRule{
				{
					ToPorts: api.PortRules{
						{
							Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}},
							Rules: &api.L7Rules{HTTP: []api.PortRuleHTTP{
								{Method: "POST", Path: "/b"},
								{Method: "GET", Path: "/a"},
								{Method: "GET", Path: "/z"},
							}},
						},
					},
				},
			},
		}
		normalizeRule(rule)
		got := rule.Egress[0].ToPorts[0].Rules.HTTP
		require.Len(t, got, 3)
		assert.Equal(t, "GET", got[0].Method)
		assert.Equal(t, "/a", got[0].Path)
		assert.Equal(t, "GET", got[1].Method)
		assert.Equal(t, "/z", got[1].Path)
		assert.Equal(t, "POST", got[2].Method)
		assert.Equal(t, "/b", got[2].Path)
	})

	t.Run("DNS sorted by MatchName", func(t *testing.T) {
		rule := &api.Rule{
			Egress: []api.EgressRule{
				{
					ToPorts: api.PortRules{
						{
							Ports: []api.PortProtocol{{Port: "53", Protocol: api.ProtoUDP}},
							Rules: &api.L7Rules{DNS: []api.PortRuleDNS{
								{MatchName: "b.example.com"},
								{MatchName: "a.example.com"},
							}},
						},
					},
				},
			},
		}
		normalizeRule(rule)
		got := rule.Egress[0].ToPorts[0].Rules.DNS
		require.Len(t, got, 2)
		assert.Equal(t, "a.example.com", got[0].MatchName)
		assert.Equal(t, "b.example.com", got[1].MatchName)
	})

	t.Run("nil Rules is a no-op", func(t *testing.T) {
		rule := &api.Rule{
			Egress: []api.EgressRule{
				{ToPorts: api.PortRules{{Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}}}}},
			},
		}
		normalizeRule(rule)
		assert.Nil(t, rule.Egress[0].ToPorts[0].Rules)
	})

	t.Run("empty L7 lists stay empty (not nil)", func(t *testing.T) {
		empty := &api.L7Rules{HTTP: []api.PortRuleHTTP{}, DNS: []api.PortRuleDNS{}}
		rule := &api.Rule{
			Egress: []api.EgressRule{
				{ToPorts: api.PortRules{{
					Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}},
					Rules: empty,
				}}},
			},
		}
		normalizeRule(rule)
		got := rule.Egress[0].ToPorts[0].Rules
		require.NotNil(t, got)
		assert.NotNil(t, got.HTTP, "empty (non-nil) HTTP list must stay non-nil")
		assert.NotNil(t, got.DNS, "empty (non-nil) DNS list must stay non-nil")
		assert.Len(t, got.HTTP, 0)
		assert.Len(t, got.DNS, 0)
	})

	t.Run("single-element list is untouched (no spurious sort)", func(t *testing.T) {
		rule := &api.Rule{
			Egress: []api.EgressRule{
				{ToPorts: api.PortRules{{
					Ports: []api.PortProtocol{{Port: "80", Protocol: api.ProtoTCP}},
					Rules: &api.L7Rules{HTTP: []api.PortRuleHTTP{{Method: "GET", Path: "/only"}}},
				}}},
			},
		}
		normalizeRule(rule)
		got := rule.Egress[0].ToPorts[0].Rules.HTTP
		require.Len(t, got, 1)
		assert.Equal(t, "/only", got[0].Path)
	})
}

// TestRuleKey_L7Discriminator asserts:
//   - nil L7 stringifies to the v1.1 4-segment form (byte-identical).
//   - populated L7 appends an ":l7=…" segment containing the L7 fields.
//   - two keys differing only by HTTPPath produce different strings.
func TestRuleKey_L7Discriminator(t *testing.T) {
	base := RuleKey{
		Direction: "egress",
		Peer:      Peer{Type: PeerEndpoint, Labels: map[string]string{"app": "x"}},
		Port:      "80",
		Protocol:  "TCP",
	}

	t.Run("nil L7 → v1.1 byte-identical encoding", func(t *testing.T) {
		assert.Equal(t, "egress:ep:app=x:TCP:80", base.String())
	})

	t.Run("HTTP discriminator appended", func(t *testing.T) {
		k := base
		k.L7 = &L7Discriminator{Protocol: "http", HTTPMethod: "GET", HTTPPath: "/api"}
		s := k.String()
		assert.Contains(t, s, "egress:ep:app=x:TCP:80")
		assert.Contains(t, s, "l7=")
		assert.Contains(t, s, "proto=http")
		assert.Contains(t, s, "method=GET")
		assert.Contains(t, s, "path=/api")
	})

	t.Run("two keys differing only by HTTPPath are distinct", func(t *testing.T) {
		k1 := base
		k1.L7 = &L7Discriminator{Protocol: "http", HTTPMethod: "GET", HTTPPath: "/a"}
		k2 := base
		k2.L7 = &L7Discriminator{Protocol: "http", HTTPMethod: "GET", HTTPPath: "/b"}
		assert.NotEqual(t, k1.String(), k2.String())
	})

	t.Run("DNS discriminator appended", func(t *testing.T) {
		k := base
		k.L7 = &L7Discriminator{Protocol: "dns", DNSMatchName: "api.example.com"}
		s := k.String()
		assert.Contains(t, s, "proto=dns")
		assert.Contains(t, s, "dns=api.example.com")
	})
}
