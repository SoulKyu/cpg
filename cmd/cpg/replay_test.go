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
