package hubble

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/flowsource"
	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)

const l7DNSFixture = "../../testdata/flows/l7_dns.jsonl"

// TestPipeline_L7DNS_GenerationAndEvidence drives the full streaming pipeline
// against a synthetic DNS-bearing fixture and asserts the four invariants the
// 09-02 plan calls out:
//
//  1. Generated CNP YAML carries `toFQDNs` (matchName, no trailing dot) +
//     `toPorts.rules.dns` matchName entries.
//  2. The same YAML carries the kube-dns companion egress rule (DNS-02 — the
//     test parses the file and runs testdata.AssertHasKubeDNSCompanion).
//  3. The YAML contains NO `matchPattern:` substring (DNS-03 — v1.2 emits only
//     literal matchName).
//  4. Evidence JSON carries L7Ref{Protocol:"dns", DNSMatchName: <name>} for the
//     DNS-bearing rules, and the SessionStats logged value L7DNSCount > 0.
//  5. VIS-01 empty-records warning does NOT fire (DNS records were observed).
func TestPipeline_L7DNS_GenerationAndEvidence(t *testing.T) {
	outDir := t.TempDir()
	evDir := t.TempDir()
	logger, observed := newObservedLogger()

	src, err := flowsource.NewFileSource(l7DNSFixture, logger)
	require.NoError(t, err)

	absOut, err := filepath.Abs(outDir)
	require.NoError(t, err)

	cfg := PipelineConfig{
		FlushInterval:   50 * time.Millisecond,
		OutputDir:       outDir,
		Logger:          logger,
		L7Enabled:       true,
		EvidenceEnabled: true,
		EvidenceDir:     evDir,
		OutputHash:      evidence.HashOutputDir(absOut),
		EvidenceCaps:    evidence.MergeCaps{MaxSamples: 5, MaxSessions: 5},
		SessionID:       "test-session-dns",
		SessionSource:   evidence.SourceInfo{Type: "replay", File: l7DNSFixture},
		CPGVersion:      "test",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, RunPipelineWithSource(ctx, cfg, src))

	// (1) + (2) + (3): inspect the on-disk YAML.
	yamlPath := filepath.Join(outDir, "production", "api-server.yaml")
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err, "policy YAML must exist at %s", yamlPath)
	yamlStr := string(data)

	assert.Contains(t, yamlStr, "toFQDNs:", "expected toFQDNs block")
	assert.Contains(t, yamlStr, "matchName: api.example.com",
		"expected matchName for observed FQDN (trailing dot stripped)")
	assert.Contains(t, yamlStr, "matchName: www.example.org",
		"expected matchName for second observed FQDN")
	assert.Contains(t, yamlStr, "dns:", "expected dns L7 sub-block paired with toFQDNs")

	// DNS-03: no glob/wildcard inference in v1.2.
	assert.NotContains(t, yamlStr, "matchPattern:",
		"DNS-03: cpg v1.2 must never emit matchPattern")

	// DNS-02: kube-dns companion present. Parse the YAML and run the shared
	// invariant helper from pkg/policy/testdata.
	var cnp ciliumv2.CiliumNetworkPolicy
	require.NoError(t, sigsyaml.Unmarshal(data, &cnp), "YAML must round-trip into CNP")
	testdata.AssertHasKubeDNSCompanion(t, &cnp)

	// (5) VIS-01 must NOT fire — DNS records were observed.
	for _, e := range observed.All() {
		assert.NotContains(t, e.Message, "no L7 records observed",
			"VIS-01 must not fire when DNS records are present")
	}

	// (4) Evidence JSON: locate the policy evidence file and assert at least
	// one rule carries L7Ref{Protocol:"dns", DNSMatchName:"api.example.com"}.
	evPath := evidence.ResolvePolicyPath(evDir, evidence.HashOutputDir(absOut), "production", "api-server")
	raw, err := os.ReadFile(evPath)
	require.NoError(t, err, "evidence file must exist at %s", evPath)
	var pev evidence.PolicyEvidence
	require.NoError(t, json.Unmarshal(raw, &pev))
	assert.Equal(t, evidence.SchemaVersion, pev.SchemaVersion)

	dnsNames := make(map[string]struct{})
	for _, r := range pev.Rules {
		if r.L7 != nil && r.L7.Protocol == "dns" && r.L7.DNSMatchName != "" {
			dnsNames[r.L7.DNSMatchName] = struct{}{}
		}
	}
	assert.Contains(t, dnsNames, "api.example.com",
		"evidence must record dns L7Ref for api.example.com")
	assert.Contains(t, dnsNames, "www.example.org",
		"evidence must record dns L7Ref for www.example.org")

	// (4 cont'd) SessionStats.L7DNSCount > 0 — surfaced via the summary log
	// emitted by stats.Log in RunPipelineWithSource.
	var foundCount bool
	for _, e := range observed.All() {
		if e.Message != "session summary" {
			continue
		}
		fields := e.ContextMap()
		v, ok := fields["l7_dns_count"]
		if !ok {
			continue
		}
		// zap encodes uint64 fields as integer in the observer.
		switch n := v.(type) {
		case uint64:
			assert.Greater(t, n, uint64(0), "l7_dns_count must be > 0")
			foundCount = true
		case int64:
			assert.Greater(t, n, int64(0), "l7_dns_count must be > 0")
			foundCount = true
		default:
			// Fall back to string form check if zap normalized differently.
			s := strings.TrimSpace(strings.Trim(strings.ToLower(strings.ReplaceAll(
				toStringField(v), " ", "")), `"`))
			assert.NotEqual(t, "0", s, "l7_dns_count must be > 0")
			foundCount = true
		}
	}
	assert.True(t, foundCount, "session summary log must include l7_dns_count")
}

// toStringField formats an arbitrary zap field value for diagnostic
// fallback when the runtime type differs from what we typed-asserted on.
func toStringField(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
