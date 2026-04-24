// pkg/evidence/writer.go
package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Writer loads existing evidence, folds in a new session, and persists the
// result atomically (temp-file + rename). It is safe for concurrent use from a
// single process only: cross-process concurrency is not expected for cpg.
type Writer struct {
	evidenceDir string
	outputHash  string
	caps        MergeCaps
}

// NewWriter constructs a Writer.
func NewWriter(evidenceDir, outputHash string, caps MergeCaps) *Writer {
	return &Writer{evidenceDir: evidenceDir, outputHash: outputHash, caps: caps}
}

// Write merges the new session and rules into the on-disk evidence for the
// named workload and persists the result.
func (w *Writer) Write(ref PolicyRef, session SessionInfo, newRules []RuleEvidence) error {
	path := ResolvePolicyPath(w.evidenceDir, w.outputHash, ref.Namespace, ref.Workload)

	var existing PolicyEvidence
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("parsing existing evidence %s: %w", path, err)
		}
		if existing.SchemaVersion != SchemaVersion {
			return fmt.Errorf("refusing to merge: existing evidence %s has schema_version %d (this cpg understands %d)", path, existing.SchemaVersion, SchemaVersion)
		}
	case errors.Is(err, fs.ErrNotExist):
		existing = NewSkeleton(ref)
	default:
		return fmt.Errorf("reading existing evidence %s: %w", path, err)
	}

	Merge(&existing, session, newRules, w.caps)

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding evidence: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating evidence dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic rename: %w", err)
	}
	return nil
}
