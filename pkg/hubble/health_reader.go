package hubble

import (
	"encoding/json"
	"fmt"
	"os"
)

// ReadClusterHealth reads and schema-version-gates cluster-health.json,
// mirroring pkg/evidence.Reader.Read's idiom: os.ReadFile wraps a missing
// file's error with fs.ErrNotExist (errors.Is-detectable downstream), then
// json.Unmarshal, then a schema-version gate.
//
// D-13's 3-way branch depends on this: "file absent" (errors.Is(err,
// fs.ErrNotExist)) means "zero infra/transient drops observed" -- NOT
// "session crashed". A malformed or wrong-version file present is a genuine
// error distinct from absence.
func ReadClusterHealth(path string) (*ClusterHealthReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading cluster health %s: %w", path, err)
	}

	var report ClusterHealthReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parsing cluster health %s: %w", path, err)
	}

	if report.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported cluster-health schema_version %d in %s (this cpg understands 1)",
			report.SchemaVersion, path)
	}

	return &report, nil
}
