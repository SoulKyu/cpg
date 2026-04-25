package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

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
