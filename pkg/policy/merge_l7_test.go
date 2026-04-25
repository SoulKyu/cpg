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
