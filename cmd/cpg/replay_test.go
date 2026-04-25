package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

// TestReplayCmd_L7FlagParses confirms `cpg replay --l7` is accepted and the
// commonFlags.l7 field is populated. The flag is plumbing-only in Phase 7.
func TestReplayCmd_L7FlagParses(t *testing.T) {
	cmd := newReplayCmd()
	require.NoError(t, cmd.Flags().Set("l7", "true"))
	f := parseCommonFlags(cmd)
	assert.True(t, f.l7)
}

// TestReplayCmd_L7DefaultIsFalse confirms the default value is OFF.
func TestReplayCmd_L7DefaultIsFalse(t *testing.T) {
	cmd := newReplayCmd()
	f := parseCommonFlags(cmd)
	assert.False(t, f.l7)
}

// TestReplay_L7FlagByteStable is the PRIMARY guardrail for critical
// correctness invariant 4: cpg replay --l7=true and cpg replay --l7=false
// against the SAME v1.1 jsonpb fixture must produce byte-identical YAML
// output (and byte-identical evidence files). Phase 7 plumbs the flag but
// does NOT change codegen — Phase 8/9 light it up. If this test ever fails
// in Phase 7, the foundation for the rest of v1.2 is broken.
func TestReplay_L7FlagByteStable(t *testing.T) {
	initLoggerForTesting(t)

	runReplayOnce := func(l7 bool) (policiesDir, evidenceDir string) {
		t.Helper()
		outDir := t.TempDir()
		evDir := t.TempDir()

		cmd := newReplayCmd()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetErr(new(bytes.Buffer))
		cmd.SilenceUsage = true
		args := []string{
			"../../testdata/flows/small.jsonl",
			"--output-dir", outDir,
			"--evidence-dir", evDir,
			"--flush-interval", "100ms",
		}
		if l7 {
			args = append(args, "--l7")
		}
		cmd.SetArgs(args)
		require.NoError(t, cmd.Execute())
		return outDir, evDir
	}

	offPolicies, offEvidence := runReplayOnce(false)
	onPolicies, onEvidence := runReplayOnce(true)

	assertTreesByteEqual(t, offPolicies, onPolicies, "policy tree")
	// Evidence files contain a session UUID + timestamps that differ run-to-run
	// regardless of --l7. The byte-stability invariant is about CODEGEN output
	// (CNP YAML), not session-stamped evidence. Compare evidence trees by
	// SHAPE (same set of relative paths) only.
	assertTreesSameShape(t, offEvidence, onEvidence, "evidence tree")
}

// assertTreesByteEqual walks two directory trees and asserts that they
// contain the same set of relative paths and that every file is byte-identical.
func assertTreesByteEqual(t *testing.T, a, b, label string) {
	t.Helper()
	aFiles := walkRelFiles(t, a)
	bFiles := walkRelFiles(t, b)
	assert.Equal(t, aFiles, bFiles, "%s: file sets differ", label)
	for _, rel := range aFiles {
		ab, err := os.ReadFile(filepath.Join(a, rel))
		require.NoError(t, err)
		bb, err := os.ReadFile(filepath.Join(b, rel))
		require.NoError(t, err)
		assert.True(t, bytes.Equal(ab, bb), "%s: %s differs", label, rel)
	}
}

// assertTreesSameShape walks two directory trees and asserts only the set of
// relative file paths matches AFTER stripping the leading output-hash dir
// (evidence files live under <hash>/<namespace>/<workload>.json, where <hash>
// is derived from the absolute output dir, which differs across t.TempDir()
// invocations and is therefore expected to differ run-to-run). Used for
// evidence trees that legitimately differ run-to-run.
func assertTreesSameShape(t *testing.T, a, b, label string) {
	t.Helper()
	aFiles := stripFirstSegment(walkRelFiles(t, a))
	bFiles := stripFirstSegment(walkRelFiles(t, b))
	assert.Equal(t, aFiles, bFiles, "%s: file sets differ", label)
}

// stripFirstSegment removes the first path segment (hash dir) from every
// entry so evidence trees rooted at different t.TempDir() invocations can
// be compared by shape.
func stripFirstSegment(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		parts := strings.SplitN(p, string(filepath.Separator), 2)
		if len(parts) < 2 {
			out = append(out, p)
			continue
		}
		out = append(out, parts[1])
	}
	return out
}

// walkRelFiles returns the sorted slice of file paths under root, expressed
// relative to root. Directories are not included.
func walkRelFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, rel)
		return nil
	})
	require.NoError(t, err)
	return out
}

func TestReplayCommandProducesPoliciesAndEvidence(t *testing.T) {
	outDir := t.TempDir()
	evDir := t.TempDir()

	initLoggerForTesting(t)

	cmd := newReplayCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"../../testdata/flows/small.jsonl",
		"--output-dir", outDir,
		"--evidence-dir", evDir,
		"--flush-interval", "100ms",
	})

	require.NoError(t, cmd.Execute())

	// Policies were written (production/ has api-server.yaml and egress produces db.yaml via egress)
	prodEntries, err := os.ReadDir(filepath.Join(outDir, "production"))
	require.NoError(t, err)
	assert.NotEmpty(t, prodEntries)

	// Evidence was written
	absOut, _ := filepath.Abs(outDir)
	hash := evidence.HashOutputDir(absOut)
	evPath := filepath.Join(evDir, hash, "production", "api-server.json")
	data, err := os.ReadFile(evPath)
	require.NoError(t, err)

	var pev evidence.PolicyEvidence
	require.NoError(t, json.Unmarshal(data, &pev))
	assert.Equal(t, 2, pev.SchemaVersion)
	assert.NotEmpty(t, pev.Rules)
	assert.Len(t, pev.Sessions, 1)
	assert.Equal(t, "replay", pev.Sessions[0].Source.Type)
}

// TestReplay_L7HTTPGeneration is the e2e test for HTTP-01..HTTP-05: running
// `cpg replay --l7` against an L7-bearing fixture must emit a CNP YAML with
// the expected http: block, anchored regex paths, normalized methods, and no
// headerMatches/host/hostExact fields. Evidence files must carry L7Ref for at
// least one rule.
func TestReplay_L7HTTPGeneration(t *testing.T) {
	initLoggerForTesting(t)

	outDir := t.TempDir()
	evDir := t.TempDir()

	cmd := newReplayCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"../../testdata/flows/l7_http.jsonl",
		"--output-dir", outDir,
		"--evidence-dir", evDir,
		"--flush-interval", "100ms",
		"--l7",
	})

	require.NoError(t, cmd.Execute())

	yamlPath := filepath.Join(outDir, "production", "api-server.yaml")
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err, "policy YAML must exist at %s", yamlPath)
	yaml := string(data)

	// HTTP-01 / HTTP-04: single port rule carries an http: rules sub-block.
	assert.Contains(t, yaml, "rules:", "rules block expected")
	assert.Contains(t, yaml, "http:", "http sub-block expected")

	// HTTP-02 method casing: lowercase `get` in fixture must be normalized.
	assert.Contains(t, yaml, "method: GET", "GET method must be emitted")
	assert.Contains(t, yaml, "method: POST", "POST method must be emitted")
	assert.NotRegexp(t, `(?m)method:\s+get\b`, yaml, "lowercase methods must not leak")

	// HTTP-03 path anchoring + regex.QuoteMeta.
	assert.Contains(t, yaml, `path: ^/api/v1/users$`, "anchored path expected")
	assert.Contains(t, yaml, `path: ^/healthz$`, "anchored healthz path expected")
	// Query params are stripped before regex emission.
	assert.Contains(t, yaml, `path: ^/api/v1/orders$`, "query params must be stripped")

	// HTTP-05 anti-feature: never emit headerMatches/host/hostExact.
	assert.NotContains(t, yaml, "headerMatches", "headerMatches must never be emitted")
	assert.NotContains(t, yaml, "hostExact", "hostExact must never be emitted")
	// `host:` as an http rule field — be tolerant of the word appearing in
	// other contexts (it shouldn't, but stay specific).
	assert.NotRegexp(t, `(?m)^\s+host:\s`, yaml, "host: http field must never be emitted")

	// Evidence v2: at least one rule carries L7Ref{Protocol:"http",...}.
	absOut, _ := filepath.Abs(outDir)
	hash := evidence.HashOutputDir(absOut)
	evPath := filepath.Join(evDir, hash, "production", "api-server.json")
	raw, err := os.ReadFile(evPath)
	require.NoError(t, err, "evidence file must exist at %s", evPath)

	var pev evidence.PolicyEvidence
	require.NoError(t, json.Unmarshal(raw, &pev))
	hasL7 := false
	for _, r := range pev.Rules {
		if r.L7 != nil && r.L7.Protocol == "http" && r.L7.HTTPMethod != "" && r.L7.HTTPPath != "" {
			hasL7 = true
			break
		}
	}
	assert.True(t, hasL7, "at least one rule evidence must carry L7Ref{Protocol:http,...}")
}

// TestReplay_L7HTTP_DisabledByteStable asserts that running `cpg replay`
// against the L7-bearing fixture WITHOUT --l7 produces byte-identical output
// to running without the flag at all. This is the v1.2 negative invariant:
// L7 codegen is gated on the --l7 flag.
func TestReplay_L7HTTP_DisabledByteStable(t *testing.T) {
	initLoggerForTesting(t)

	runOnce := func(args ...string) string {
		t.Helper()
		outDir := t.TempDir()
		evDir := t.TempDir()
		cmd := newReplayCmd()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetErr(new(bytes.Buffer))
		cmd.SilenceUsage = true
		base := []string{
			"../../testdata/flows/l7_http.jsonl",
			"--output-dir", outDir,
			"--evidence-dir", evDir,
			"--flush-interval", "100ms",
		}
		cmd.SetArgs(append(base, args...))
		require.NoError(t, cmd.Execute())
		return outDir
	}

	noFlag := runOnce()
	withFalse := runOnce("--l7=false")

	files := walkRelFiles(t, noFlag)
	assert.Equal(t, files, walkRelFiles(t, withFalse), "file sets must match")
	for _, rel := range files {
		a, err := os.ReadFile(filepath.Join(noFlag, rel))
		require.NoError(t, err)
		b, err := os.ReadFile(filepath.Join(withFalse, rel))
		require.NoError(t, err)
		assert.True(t, bytes.Equal(a, b), "%s must be byte-identical with --l7=false vs no flag", rel)
		// Defensive: --l7=false must NEVER produce an http: block, even on an
		// L7-bearing fixture.
		assert.NotContains(t, string(a), "http:", "--l7 disabled must not emit http block (%s)", rel)
	}
}

// TestReplay_L7HTTP_EmptyFixtureFiresWarning asserts VIS-01: when --l7 is set
// against an L4-only fixture, the pipeline emits exactly one warning
// referencing #l7-prerequisites and produces no http: block in the output.
func TestReplay_L7HTTP_EmptyFixtureFiresWarning(t *testing.T) {
	logs := initObservedLoggerForTesting(t)

	outDir := t.TempDir()
	evDir := t.TempDir()

	cmd := newReplayCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"../../testdata/flows/small.jsonl",
		"--output-dir", outDir,
		"--evidence-dir", evDir,
		"--flush-interval", "100ms",
		"--l7",
	})
	require.NoError(t, cmd.Execute())

	matches := 0
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "no L7 records observed") {
			matches++
			fields := e.ContextMap()
			hint, ok := fields["hint"].(string)
			assert.True(t, ok, "hint field must be a string")
			assert.Contains(t, hint, "#l7-prerequisites", "hint must reference README anchor")
			assert.Contains(t, e.Message, "--l7", "warning must reference --l7 flag verbatim")
			if ws, ok := fields["workloads"].([]interface{}); ok {
				assert.NotEmpty(t, ws, "workloads must be non-empty")
			} else if ws, ok := fields["workloads"].([]string); ok {
				assert.NotEmpty(t, ws, "workloads must be non-empty")
			}
			if flows, ok := fields["flows"].(uint64); ok {
				assert.Greater(t, flows, uint64(0), "flows count must be > 0")
			}
		}
	}
	assert.Equal(t, 1, matches, "VIS-01 warning must fire exactly once")

	// No http: block must appear in any generated YAML.
	for _, rel := range walkRelFiles(t, outDir) {
		data, err := os.ReadFile(filepath.Join(outDir, rel))
		require.NoError(t, err)
		assert.NotContains(t, string(data), "http:", "%s must not contain http block", rel)
	}
}

// TestReplay_L7DNSGeneration is the e2e acceptance test for DNS-01..DNS-03:
// running `cpg replay --l7` against an L7 DNS-bearing fixture must emit a CNP
// YAML with a toFQDNs block (literal matchName entries, lexicographically
// sorted), the matching toPorts.rules.dns matchName entries, the mandatory
// kube-dns companion egress rule (DNS-02), and NEVER a matchPattern (DNS-03)
// or HTTP header/host fields (HTTP-05 invariant survives DNS-only fixtures).
// Evidence files must carry L7Ref{Protocol:"dns", DNSMatchName:...} for the
// observed queries.
func TestReplay_L7DNSGeneration(t *testing.T) {
	initLoggerForTesting(t)

	outDir := t.TempDir()
	evDir := t.TempDir()

	cmd := newReplayCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"../../testdata/flows/l7_dns.jsonl",
		"--output-dir", outDir,
		"--evidence-dir", evDir,
		"--namespace", "production",
		"--flush-interval", "100ms",
		"--l7",
	})
	require.NoError(t, cmd.Execute())

	yamlPath := filepath.Join(outDir, "production", "api-server.yaml")
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err, "policy YAML must exist at %s", yamlPath)
	yaml := string(data)

	// DNS-01: toFQDNs with literal matchName for each observed query
	// (lexicographically sorted per EVID2-04).
	assert.Contains(t, yaml, "toFQDNs:", "toFQDNs block expected")
	assert.Contains(t, yaml, "matchName: api.example.com", "api.example.com matchName expected")
	assert.Contains(t, yaml, "matchName: www.example.org", "www.example.org matchName expected")

	// DNS-01: matching dns rules sub-block on the toPorts.
	assert.Contains(t, yaml, "rules:", "rules block expected")
	assert.Contains(t, yaml, "dns:", "dns sub-block expected")

	// DNS-02: kube-dns companion rule with both UDP+TCP/53.
	assert.Contains(t, yaml, "k8s-app: kube-dns", "kube-dns companion selector expected")
	assert.Contains(t, yaml, "io.kubernetes.pod.namespace: kube-system", "kube-system namespace selector expected")
	// Both UDP and TCP must be present (companion must allow both).
	assert.Regexp(t, `(?s)protocol:\s+UDP.*protocol:\s+TCP`, yaml, "companion must allow UDP and TCP")

	// DNS-03: NO matchPattern glob anywhere in the YAML.
	assert.NotContains(t, yaml, "matchPattern", "matchPattern must never be auto-generated in v1.2 (DNS-03)")

	// HTTP-05 invariant: no HTTP header/host fields, even on a DNS-only fixture.
	assert.NotContains(t, yaml, "headerMatches", "headerMatches must never be emitted")
	assert.NotContains(t, yaml, "hostExact", "hostExact must never be emitted")
	assert.NotRegexp(t, `(?m)^\s+host:\s`, yaml, "host: http field must never be emitted")

	// Evidence v2: at least one rule carries L7Ref{Protocol:"dns",...} for each
	// observed name.
	absOut, _ := filepath.Abs(outDir)
	hash := evidence.HashOutputDir(absOut)
	evPath := filepath.Join(evDir, hash, "production", "api-server.json")
	raw, err := os.ReadFile(evPath)
	require.NoError(t, err, "evidence file must exist at %s", evPath)

	var pev evidence.PolicyEvidence
	require.NoError(t, json.Unmarshal(raw, &pev))
	seenAPI, seenWWW := false, false
	for _, r := range pev.Rules {
		if r.L7 == nil || r.L7.Protocol != "dns" {
			continue
		}
		switch r.L7.DNSMatchName {
		case "api.example.com":
			seenAPI = true
		case "www.example.org":
			seenWWW = true
		}
	}
	assert.True(t, seenAPI, "evidence must carry L7Ref{Protocol:dns, DNSMatchName:api.example.com}")
	assert.True(t, seenWWW, "evidence must carry L7Ref{Protocol:dns, DNSMatchName:www.example.org}")
}

// TestReplay_L7DNSDisabled_FallbackByteStable locks DNS-04: running the same
// DNS-bearing fixture WITHOUT --l7 must produce a v1.1-shape CIDR-based
// egress (no toFQDNs, no companion, no dns: rules) byte-identical to running
// with --l7=false.
func TestReplay_L7DNSDisabled_FallbackByteStable(t *testing.T) {
	initLoggerForTesting(t)

	runOnce := func(args ...string) string {
		t.Helper()
		outDir := t.TempDir()
		evDir := t.TempDir()
		cmd := newReplayCmd()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetErr(new(bytes.Buffer))
		cmd.SilenceUsage = true
		base := []string{
			"../../testdata/flows/l7_dns.jsonl",
			"--output-dir", outDir,
			"--evidence-dir", evDir,
			"--namespace", "production",
			"--flush-interval", "100ms",
		}
		cmd.SetArgs(append(base, args...))
		require.NoError(t, cmd.Execute())
		return outDir
	}

	noFlag := runOnce()
	withFalse := runOnce("--l7=false")

	files := walkRelFiles(t, noFlag)
	assert.Equal(t, files, walkRelFiles(t, withFalse), "file sets must match")
	for _, rel := range files {
		a, err := os.ReadFile(filepath.Join(noFlag, rel))
		require.NoError(t, err)
		b, err := os.ReadFile(filepath.Join(withFalse, rel))
		require.NoError(t, err)
		assert.True(t, bytes.Equal(a, b), "%s must be byte-identical with --l7=false vs no flag (DNS-04)", rel)

		yaml := string(a)
		// DNS-04 fallback must be CIDR-based v1.1 shape — no L7 artifacts.
		assert.NotContains(t, yaml, "toFQDNs", "%s must not contain toFQDNs without --l7 (DNS-04)", rel)
		assert.NotContains(t, yaml, "k8s-app: kube-dns", "%s must not contain kube-dns companion without --l7 (DNS-04)", rel)
		assert.NotContains(t, yaml, "matchName", "%s must not contain dns matchName without --l7 (DNS-04)", rel)
		assert.NotContains(t, yaml, "matchPattern", "matchPattern must never appear (DNS-03)")
		// CIDR fallback path is preserved.
		assert.Contains(t, yaml, "toCIDR", "%s must contain CIDR-based egress (DNS-04 fallback)", rel)
		assert.Contains(t, yaml, "8.8.8.8/32", "%s must contain destination CIDR /32 (DNS-04 fallback)", rel)
	}
}

func TestReplayDryRunWritesNothing(t *testing.T) {
	outDir := t.TempDir()
	evDir := t.TempDir()

	initLoggerForTesting(t)

	cmd := newReplayCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"../../testdata/flows/small.jsonl",
		"--output-dir", outDir,
		"--evidence-dir", evDir,
		"--flush-interval", "100ms",
		"--dry-run",
		"--no-diff",
	})

	require.NoError(t, cmd.Execute())

	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "no files must be written in dry-run")

	evEntries, _ := os.ReadDir(evDir)
	assert.Empty(t, evEntries, "evidence dir must stay empty in dry-run")
}
