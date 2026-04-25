// pkg/evidence/reader_test.go
package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReader_RejectsNonV2SchemaWithWipeInstruction asserts that the v1.2 reader
// refuses any evidence file whose schema_version is not 2 with an error message
// that names `$XDG_CACHE_HOME/cpg/evidence/` verbatim (incident-response
// grep-bait) and instructs the user to wipe that directory.
//
// Per CONTEXT.md decision: NO back-compat reader path. v1.1 shipped 2026-04-24,
// no v1 caches in production. This rejection is the upgrade UX.
func TestReader_RejectsNonV2SchemaWithWipeInstruction(t *testing.T) {
	cases := []struct {
		name          string
		schemaVersion int
	}{
		{name: "rejects v1 (pre-v1.2)", schemaVersion: 1},
		{name: "rejects v3 (future)", schemaVersion: 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			outputHash := "hash0"
			ns := "prod"
			workload := "api"

			path := ResolvePolicyPath(dir, outputHash, ns, workload)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

			// Well-formed JSON, only the schema_version is off.
			doc := map[string]any{
				"schema_version": tc.schemaVersion,
				"policy": map[string]any{
					"name":      "cpg-api",
					"namespace": ns,
					"workload":  workload,
				},
				"sessions": []any{},
				"rules":    []any{},
			}
			raw, err := json.Marshal(doc)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, raw, 0o644))

			r := NewReader(dir, outputHash)
			got, err := r.Read(ns, workload)

			require.Error(t, err, "reader must reject non-v2 schema")
			assert.Equal(t, PolicyEvidence{}, got, "reader returns zero PolicyEvidence on error")

			msg := err.Error()
			assert.True(t,
				strings.Contains(msg, "$XDG_CACHE_HOME/cpg/evidence/"),
				"error must contain literal `$XDG_CACHE_HOME/cpg/evidence/` for incident-response grep-bait; got: %q", msg)

			// Wipe instruction: accept any of the canonical phrasings.
			lower := strings.ToLower(msg)
			hasWipeHint := strings.Contains(lower, "wipe") ||
				strings.Contains(lower, "remove") ||
				strings.Contains(lower, "rm -rf")
			assert.True(t, hasWipeHint,
				"error must instruct user to wipe/remove the evidence cache; got: %q", msg)
		})
	}
}
