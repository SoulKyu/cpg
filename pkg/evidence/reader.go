// pkg/evidence/reader.go
package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Reader loads PolicyEvidence from the filesystem.
type Reader struct {
	evidenceDir string
	outputHash  string
}

// NewReader constructs a Reader scoped to an evidence directory and output-dir hash.
func NewReader(evidenceDir, outputHash string) *Reader {
	return &Reader{evidenceDir: evidenceDir, outputHash: outputHash}
}

// Read returns the PolicyEvidence for the given workload. It returns an error
// wrapping fs.ErrNotExist when the file is absent; callers can detect that via
// errors.Is.
func (r *Reader) Read(namespace, workload string) (PolicyEvidence, error) {
	path := ResolvePolicyPath(r.evidenceDir, r.outputHash, namespace, workload)
	data, err := os.ReadFile(path)
	if err != nil {
		return PolicyEvidence{}, fmt.Errorf("reading evidence %s: %w", path, err)
	}
	var pe PolicyEvidence
	if err := json.Unmarshal(data, &pe); err != nil {
		return PolicyEvidence{}, fmt.Errorf("parsing evidence %s: %w", path, err)
	}
	if pe.SchemaVersion != SchemaVersion {
		return PolicyEvidence{}, fmt.Errorf(
			"unsupported schema_version %d in %s (this cpg understands %d). "+
				"v1.2 evidence schema is incompatible with previous versions. "+
				"Wipe the evidence cache and re-run cpg: rm -rf $XDG_CACHE_HOME/cpg/evidence/",
			pe.SchemaVersion, path, SchemaVersion)
	}
	return pe, nil
}

// IsNotExist reports whether err is the not-found variant returned by Read.
func IsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
