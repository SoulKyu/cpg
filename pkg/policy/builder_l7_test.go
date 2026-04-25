package policy_test

import (
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/policy"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)

// withHTTP returns a copy-style mutation of f attaching an L7 HTTP record
// (method + url). f is mutated in place for compactness — callers build a
// fresh flow per test case.
func withHTTP(f *flowpb.Flow, method, url string) *flowpb.Flow {
	f.L7 = &flowpb.Layer7{
		Record: &flowpb.Layer7_Http{
			Http: &flowpb.HTTP{
				Method: method,
				Url:    url,
			},
		},
	}
	return f
}

// withEmptyL7 attaches an L7 wrapper with nil HTTP record (DNS-only L7 shape).
func withEmptyL7(f *flowpb.Flow) *flowpb.Flow {
	f.L7 = &flowpb.Layer7{}
	return f
}

// mkHTTPIngressFlow builds an ingress TCP flow on the given port, carrying
// the supplied HTTP method+url.
func mkHTTPIngressFlow(port uint32, method, url string) *flowpb.Flow {
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		port,
	)
	return withHTTP(f, method, url)
}

// normalizedYAML marshals the policy spec via the package's normalizeRule
// indirectly — by going through PoliciesEquivalent's path. We re-use yaml
// directly here for byte comparison, but rely on normalizeRule applied by
// the caller to ensure determinism.
func normalizedYAML(t *testing.T, p interface{}) []byte {
	t.Helper()
	data, err := yaml.Marshal(p)
	require.NoError(t, err)
	return data
}

// TestBuildPolicy_L7Disabled_ByteIdentical asserts that with L7Enabled=false,
// the presence of L7 records on flows must NOT change YAML output vs input
// flows with no L7 at all. This is the byte-stability invariant for v1.1
// inputs flowing through a v1.2 binary.
func TestBuildPolicy_L7Disabled_ByteIdentical(t *testing.T) {
	l4Only := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		8080,
	)
	withL7 := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		80,
	)
	withL7 = withHTTP(withL7, "GET", "/api/users")

	// Stripped variant: same flows, but the L7-bearing flow has L7 cleared.
	l4OnlyStripped := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		8080,
	)
	stripped := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		80,
	)
	// stripped has no L7

	pWith, _ := policy.BuildPolicy("default", "server",
		[]*flowpb.Flow{l4Only, withL7}, nil,
		policy.AttributionOptions{L7Enabled: false})
	pStripped, _ := policy.BuildPolicy("default", "server",
		[]*flowpb.Flow{l4OnlyStripped, stripped}, nil,
		policy.AttributionOptions{L7Enabled: false})

	eq, err := policy.PoliciesEquivalent(pWith, pStripped)
	require.NoError(t, err)
	assert.True(t, eq, "L7Enabled=false output must be byte-identical regardless of L7 presence on input flows")
}

func TestBuildPolicy_L7Enabled_SingleHTTPRule(t *testing.T) {
	f := mkHTTPIngressFlow(80, "GET", "/api/v1/users")

	p, _ := policy.BuildPolicy("default", "server",
		[]*flowpb.Flow{f}, nil,
		policy.AttributionOptions{L7Enabled: true})
	require.NotNil(t, p)
	require.Len(t, p.Spec.Ingress, 1)

	rule := p.Spec.Ingress[0]
	require.Len(t, rule.ToPorts, 1)
	pr := rule.ToPorts[0]
	require.Len(t, pr.Ports, 1)
	assert.Equal(t, "80", pr.Ports[0].Port)
	assert.Equal(t, api.ProtoTCP, pr.Ports[0].Protocol)

	require.NotNil(t, pr.Rules, "L7Enabled + HTTP record must attach Rules to PortRule")
	require.Len(t, pr.Rules.HTTP, 1)
	assert.Equal(t, "GET", pr.Rules.HTTP[0].Method)
	assert.Equal(t, "^/api/v1/users$", pr.Rules.HTTP[0].Path)
}

// TestBuildPolicy_L7Enabled_MultiHTTPRule_SamePort_SinglePortRule asserts the
// HTTP-04 invariant: 3 distinct (method, path) entries on the same
// (src, dst, port) tuple merge into ONE PortRule with 3 HTTP entries.
func TestBuildPolicy_L7Enabled_MultiHTTPRule_SamePort_SinglePortRule(t *testing.T) {
	f1 := mkHTTPIngressFlow(80, "GET", "/api/users")
	f2 := mkHTTPIngressFlow(80, "POST", "/api/users")
	f3 := mkHTTPIngressFlow(80, "GET", "/healthz")

	p, _ := policy.BuildPolicy("default", "server",
		[]*flowpb.Flow{f1, f2, f3}, nil,
		policy.AttributionOptions{L7Enabled: true})
	require.NotNil(t, p)
	require.Len(t, p.Spec.Ingress, 1, "single peer + single port must produce one IngressRule")

	rule := p.Spec.Ingress[0]
	require.Len(t, rule.ToPorts, 1, "HTTP-04: same (port, proto) must collapse into ONE PortRule")
	pr := rule.ToPorts[0]
	require.NotNil(t, pr.Rules)
	require.Len(t, pr.Rules.HTTP, 3, "all 3 (method, path) observations must land in the single PortRule.Rules.HTTP slice")

	// Collect (method, path) tuples and assert presence regardless of order
	// (Phase 7 normalizeRule sorts these — but BuildPolicy itself doesn't).
	got := map[string]bool{}
	for _, h := range pr.Rules.HTTP {
		got[h.Method+" "+h.Path] = true
	}
	assert.True(t, got["GET ^/api/users$"], "missing GET /api/users")
	assert.True(t, got["POST ^/api/users$"], "missing POST /api/users")
	assert.True(t, got["GET ^/healthz$"], "missing GET /healthz")
}

// TestBuildPolicy_L7Enabled_MultiPort_SeparatePortRules asserts that distinct
// ports keep their own PortRule entries and that HTTP rules attach only to
// the matching port.
func TestBuildPolicy_L7Enabled_MultiPort_SeparatePortRules(t *testing.T) {
	f80 := mkHTTPIngressFlow(80, "GET", "/api/users")
	f443 := mkHTTPIngressFlow(443, "POST", "/login")

	p, _ := policy.BuildPolicy("default", "server",
		[]*flowpb.Flow{f80, f443}, nil,
		policy.AttributionOptions{L7Enabled: true})
	require.NotNil(t, p)
	require.Len(t, p.Spec.Ingress, 1)
	rule := p.Spec.Ingress[0]

	// Both ports must be present with each carrying their own HTTP rules.
	// Implementation note: builder emits one PortRule per (port, proto) when
	// L7 is attached, OR one PortRule with multiple ports when not — verify
	// the structural invariant: each port has its HTTP block and rules don't
	// bleed across.

	// Walk the structure: there may be one PortRule per port (when each
	// carries Rules) — accept either shape but assert HTTP attaches to the
	// correct port.
	type entry struct {
		port  string
		http  []api.PortRuleHTTP
	}
	var entries []entry
	for _, pr := range rule.ToPorts {
		for _, port := range pr.Ports {
			e := entry{port: port.Port}
			if pr.Rules != nil {
				e.http = pr.Rules.HTTP
			}
			entries = append(entries, e)
		}
	}
	require.Len(t, entries, 2)
	byPort := map[string][]api.PortRuleHTTP{}
	for _, e := range entries {
		byPort[e.port] = e.http
	}
	require.Contains(t, byPort, "80")
	require.Contains(t, byPort, "443")
	require.Len(t, byPort["80"], 1)
	require.Len(t, byPort["443"], 1)
	assert.Equal(t, "GET", byPort["80"][0].Method)
	assert.Equal(t, "^/api/users$", byPort["80"][0].Path)
	assert.Equal(t, "POST", byPort["443"][0].Method)
	assert.Equal(t, "^/login$", byPort["443"][0].Path)
}

// TestBuildPolicy_L7Enabled_RuleKeyDiscriminator asserts EVID2-02: two rules
// differing only by HTTP method/path produce distinct RuleAttribution entries
// with distinct RuleKey.L7 discriminators.
func TestBuildPolicy_L7Enabled_RuleKeyDiscriminator(t *testing.T) {
	fGet := mkHTTPIngressFlow(80, "GET", "/a")
	fPost := mkHTTPIngressFlow(80, "POST", "/a")

	_, attrib := policy.BuildPolicy("default", "server",
		[]*flowpb.Flow{fGet, fPost}, nil,
		policy.AttributionOptions{L7Enabled: true, MaxSamples: 1})
	require.Len(t, attrib, 2, "two flows differing only in HTTP method must produce 2 attribution entries")

	// Both must have non-nil L7 discriminator with Protocol=http and
	// distinct method/path tuples.
	seen := map[string]bool{}
	for _, a := range attrib {
		require.NotNil(t, a.Key.L7, "RuleKey.L7 must be populated for HTTP-bearing rules")
		assert.Equal(t, "http", a.Key.L7.Protocol)
		key := a.Key.L7.HTTPMethod + " " + a.Key.L7.HTTPPath
		seen[key] = true
	}
	assert.True(t, seen["GET ^/a$"])
	assert.True(t, seen["POST ^/a$"])
}

// TestBuildPolicy_L7Enabled_NoL7RecordsOnFlow_NoHTTPBlock asserts that with
// L7Enabled=true, flows carrying NO L7 record produce no HTTP block at all
// (PortRule.Rules stays nil; no empty rules.http: [] noise in YAML).
func TestBuildPolicy_L7Enabled_NoL7RecordsOnFlow_NoHTTPBlock(t *testing.T) {
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		80,
	)

	p, _ := policy.BuildPolicy("default", "server",
		[]*flowpb.Flow{f}, nil,
		policy.AttributionOptions{L7Enabled: true})
	require.NotNil(t, p)
	require.Len(t, p.Spec.Ingress, 1)
	require.Len(t, p.Spec.Ingress[0].ToPorts, 1)
	assert.Nil(t, p.Spec.Ingress[0].ToPorts[0].Rules,
		"L7Enabled=true but no Flow.L7 → no Rules attached")
}

// TestBuildPolicy_L7Enabled_PartialL7_EmptyMethodSkipped asserts that an L7
// record with empty method is dropped by extractHTTPRules — no HTTP block
// emitted.
func TestBuildPolicy_L7Enabled_PartialL7_EmptyMethodSkipped(t *testing.T) {
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		80,
	)
	f = withHTTP(f, "", "/api/users")

	p, _ := policy.BuildPolicy("default", "server",
		[]*flowpb.Flow{f}, nil,
		policy.AttributionOptions{L7Enabled: true})
	require.NotNil(t, p)
	require.Len(t, p.Spec.Ingress, 1)
	require.Len(t, p.Spec.Ingress[0].ToPorts, 1)
	assert.Nil(t, p.Spec.Ingress[0].ToPorts[0].Rules,
		"empty method → extractHTTPRules drops entry → no Rules")
}

// TestBuildPolicy_L7Enabled_NilHttpRecord asserts an L7 wrapper with nil HTTP
// (e.g., DNS-only L7 record shape) produces no HTTP block.
func TestBuildPolicy_L7Enabled_NilHttpRecord(t *testing.T) {
	f := testdata.IngressTCPFlow(
		[]string{"k8s:app=client"},
		[]string{"k8s:app=server"},
		"default",
		80,
	)
	f = withEmptyL7(f)

	p, _ := policy.BuildPolicy("default", "server",
		[]*flowpb.Flow{f}, nil,
		policy.AttributionOptions{L7Enabled: true})
	require.NotNil(t, p)
	require.Len(t, p.Spec.Ingress, 1)
	require.Len(t, p.Spec.Ingress[0].ToPorts, 1)
	assert.Nil(t, p.Spec.Ingress[0].ToPorts[0].Rules)
}

// TestBuildPolicy_L7Enabled_NoL7RecordsAcrossAllFlows_ByteIdenticalToL7Disabled
// asserts that when ZERO flows carry L7 records, the L7Enabled=true and
// L7Enabled=false codepaths produce byte-identical YAML. This is the
// unit-level mirror of cmd/cpg/replay_test.go::TestReplay_L7FlagByteStable.
func TestBuildPolicy_L7Enabled_NoL7RecordsAcrossAllFlows_ByteIdenticalToL7Disabled(t *testing.T) {
	flows := []*flowpb.Flow{
		testdata.IngressTCPFlow(
			[]string{"k8s:app=client"},
			[]string{"k8s:app=server"},
			"default",
			80,
		),
		testdata.IngressTCPFlow(
			[]string{"k8s:app=other"},
			[]string{"k8s:app=server"},
			"default",
			443,
		),
		testdata.EgressUDPFlow(
			[]string{"k8s:app=server"},
			[]string{"k8s:app=dns"},
			"default",
			53,
		),
	}

	pOn, _ := policy.BuildPolicy("default", "server", flows, nil,
		policy.AttributionOptions{L7Enabled: true})
	pOff, _ := policy.BuildPolicy("default", "server", flows, nil,
		policy.AttributionOptions{L7Enabled: false})

	eq, err := policy.PoliciesEquivalent(pOn, pOff)
	require.NoError(t, err)
	assert.True(t, eq, "L7-empty inputs must produce byte-identical YAML across L7Enabled toggle")

	// And direct YAML byte comparison after marshal — Phase 7's invariant.
	on := normalizedYAML(t, pOn.Spec)
	off := normalizedYAML(t, pOff.Spec)
	assert.Equal(t, string(off), string(on))
}

// withDNS attaches a Flow.L7.Dns record carrying the supplied query.
func withDNS(f *flowpb.Flow, query string) *flowpb.Flow {
	f.L7 = &flowpb.Layer7{
		Record: &flowpb.Layer7_Dns{
			Dns: &flowpb.DNS{Query: query},
		},
	}
	return f
}

// mkDNSEgressFlow builds an egress UDP/53 flow from k8s:app=client to a
// world-identity destination IP, carrying a DNS query record. Mirrors the
// shape Hubble produces for DNS proxy denials.
func mkDNSEgressFlow(query string) *flowpb.Flow {
	f := testdata.WorldEgressTCPFlow(
		[]string{"k8s:app=client"},
		"default",
		"10.96.0.10",
		53,
	)
	// switch protocol to UDP/53 — DNS lookups are predominantly UDP
	f.L4 = &flowpb.Layer4{
		Protocol: &flowpb.Layer4_UDP{
			UDP: &flowpb.UDP{DestinationPort: 53},
		},
	}
	return withDNS(f, query)
}

// findEgressByFQDN locates the (first) egress rule whose ToFQDNs contains the
// supplied matchName, returning nil when none match.
func findEgressByFQDN(rules []api.EgressRule, name string) *api.EgressRule {
	for i := range rules {
		for _, sel := range rules[i].ToFQDNs {
			if sel.MatchName == name {
				return &rules[i]
			}
		}
	}
	return nil
}

// TestBuildPolicy_L7Enabled_DNS_SingleQuery asserts DNS-01 + DNS-02 + DNS-03:
// one DNS-bearing egress flow produces a dedicated egress rule with toFQDNs
// + paired toPorts.rules.dns.matchName, and the kube-dns companion lands in
// the same CNP. No matchPattern is emitted anywhere.
func TestBuildPolicy_L7Enabled_DNS_SingleQuery(t *testing.T) {
	f := mkDNSEgressFlow("api.example.com.")

	p, _ := policy.BuildPolicy("default", "client",
		[]*flowpb.Flow{f}, nil,
		policy.AttributionOptions{L7Enabled: true})
	require.NotNil(t, p)

	// Locate the FQDN-bearing rule.
	fqdnRule := findEgressByFQDN(p.Spec.Egress, "api.example.com")
	require.NotNil(t, fqdnRule, "expected egress rule with toFQDNs.matchName=api.example.com (trailing dot stripped); got egress=%+v", p.Spec.Egress)

	// ToFQDNs entries must use literal MatchName only (DNS-03).
	for _, sel := range fqdnRule.ToFQDNs {
		assert.Empty(t, sel.MatchPattern, "DNS-03: ToFQDNs.MatchPattern must be empty")
	}

	// Paired toPorts.rules.dns.matchName mirrors the FQDN.
	require.Len(t, fqdnRule.ToPorts, 1)
	pr := fqdnRule.ToPorts[0]
	require.NotNil(t, pr.Rules)
	require.NotEmpty(t, pr.Rules.DNS)
	dnsNames := make(map[string]bool)
	for _, d := range pr.Rules.DNS {
		assert.Empty(t, d.MatchPattern, "DNS-03: rules.dns.matchPattern must be empty")
		dnsNames[d.MatchName] = true
	}
	assert.True(t, dnsNames["api.example.com"], "rules.dns must include matchName=api.example.com")

	// DNS-02: companion rule present.
	testdata.AssertHasKubeDNSCompanion(t, p)
}

// TestBuildPolicy_L7Enabled_DNS_MultipleQueries asserts that two distinct
// DNS queries on the same source workload merge into a single FQDN-bearing
// egress rule with both names listed (sorted), and the companion lands once.
func TestBuildPolicy_L7Enabled_DNS_MultipleQueries(t *testing.T) {
	f1 := mkDNSEgressFlow("api.example.com.")
	f2 := mkDNSEgressFlow("www.example.org")

	p, _ := policy.BuildPolicy("default", "client",
		[]*flowpb.Flow{f1, f2}, nil,
		policy.AttributionOptions{L7Enabled: true})
	require.NotNil(t, p)

	// Find the FQDN-bearing rule (either name should locate it).
	fqdnRule := findEgressByFQDN(p.Spec.Egress, "api.example.com")
	require.NotNil(t, fqdnRule)

	got := make(map[string]bool)
	for _, sel := range fqdnRule.ToFQDNs {
		got[sel.MatchName] = true
	}
	assert.True(t, got["api.example.com"])
	assert.True(t, got["www.example.org"])

	// Companion present exactly once.
	testdata.AssertHasKubeDNSCompanion(t, p)
	companions := 0
	for _, eg := range p.Spec.Egress {
		for _, ep := range eg.ToEndpoints {
			if ep.LabelSelector == nil {
				continue
			}
			ml := ep.LabelSelector.MatchLabels
			if ml["k8s-app"] == "kube-dns" || ml["any:k8s-app"] == "kube-dns" || ml["k8s:k8s-app"] == "kube-dns" {
				companions++
			}
		}
	}
	assert.Equal(t, 1, companions, "exactly one kube-dns companion egress rule must be present")
}

// TestBuildPolicy_L7Disabled_DNSFlow_NoFQDN_NoCompanion locks DNS-04: a
// DNS-bearing flow under L7Enabled=false must NOT produce toFQDNs and must
// NOT trigger companion injection. Output is byte-identical to the same
// flow with no L7 record at all.
func TestBuildPolicy_L7Disabled_DNSFlow_NoFQDN_NoCompanion(t *testing.T) {
	withDNS := mkDNSEgressFlow("api.example.com.")
	withoutDNS := mkDNSEgressFlow("api.example.com.")
	withoutDNS.L7 = nil

	pWith, _ := policy.BuildPolicy("default", "client",
		[]*flowpb.Flow{withDNS}, nil,
		policy.AttributionOptions{L7Enabled: false})
	pWithout, _ := policy.BuildPolicy("default", "client",
		[]*flowpb.Flow{withoutDNS}, nil,
		policy.AttributionOptions{L7Enabled: false})

	// No toFQDNs and no kube-dns companion under L7Enabled=false.
	for _, eg := range pWith.Spec.Egress {
		assert.Empty(t, eg.ToFQDNs, "L7Enabled=false must not emit toFQDNs")
		for _, ep := range eg.ToEndpoints {
			if ep.LabelSelector == nil {
				continue
			}
			for k, v := range ep.LabelSelector.MatchLabels {
				if v == "kube-dns" {
					t.Errorf("L7Enabled=false must not inject kube-dns companion; got label %s=%s", k, v)
				}
			}
		}
	}

	// Byte-identical output.
	eq, err := policy.PoliciesEquivalent(pWith, pWithout)
	require.NoError(t, err)
	assert.True(t, eq, "L7Enabled=false: DNS-bearing input must be byte-identical to L7-stripped input (DNS-04)")
}

// TestBuildPolicy_L7Enabled_DNS_EmptyQuery asserts that an empty DNS query
// drops the entry — no toFQDNs, no companion injection from that flow alone.
func TestBuildPolicy_L7Enabled_DNS_EmptyQuery(t *testing.T) {
	f := mkDNSEgressFlow("")

	p, _ := policy.BuildPolicy("default", "client",
		[]*flowpb.Flow{f}, nil,
		policy.AttributionOptions{L7Enabled: true})
	require.NotNil(t, p)

	for _, eg := range p.Spec.Egress {
		assert.Empty(t, eg.ToFQDNs, "empty query must not produce toFQDNs")
	}
	// No FQDN → no companion rule injected.
	for _, eg := range p.Spec.Egress {
		for _, ep := range eg.ToEndpoints {
			if ep.LabelSelector == nil {
				continue
			}
			for _, v := range ep.LabelSelector.MatchLabels {
				assert.NotEqual(t, "kube-dns", v, "no FQDN observed → no kube-dns companion")
			}
		}
	}
}

// TestBuildPolicy_L7Enabled_DNS_RuleKeyDiscriminator asserts EVID2-02 for DNS:
// two distinct DNS queries on the same egress peer/port produce 2 distinct
// attribution entries with distinct L7Discriminator values.
func TestBuildPolicy_L7Enabled_DNS_RuleKeyDiscriminator(t *testing.T) {
	fA := mkDNSEgressFlow("a.example.com")
	fB := mkDNSEgressFlow("b.example.com")

	_, attrib := policy.BuildPolicy("default", "client",
		[]*flowpb.Flow{fA, fB}, nil,
		policy.AttributionOptions{L7Enabled: true, MaxSamples: 1})

	dnsAttribs := 0
	seen := map[string]bool{}
	for _, a := range attrib {
		if a.Key.L7 == nil || a.Key.L7.Protocol != "dns" {
			continue
		}
		dnsAttribs++
		seen[a.Key.L7.DNSMatchName] = true
		// String form should embed "dns=<name>" (EVID2-02 + dns discriminator format).
		assert.Contains(t, a.Key.String(), "dns="+a.Key.L7.DNSMatchName)
	}
	assert.Equal(t, 2, dnsAttribs, "two distinct DNS queries must produce 2 DNS attribution entries")
	assert.True(t, seen["a.example.com"])
	assert.True(t, seen["b.example.com"])
}
