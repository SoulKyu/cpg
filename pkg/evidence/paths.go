// pkg/evidence/paths.go
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HashOutputDir derives a 12-char hex digest of the canonical output directory
// path. It allows multiple workspaces to share the same evidence-dir without
// collision. The input is normalized via filepath.Clean + filepath.Abs so
// equivalent paths ("foo/bar/", "foo/./bar") hash identically.
func HashOutputDir(outputDir string) string {
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		abs = outputDir
	}
	abs = filepath.Clean(abs)
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:12]
}

// DefaultEvidenceDir returns the evidence storage root, honoring XDG_CACHE_HOME
// with a fallback to $HOME/.cache.
func DefaultEvidenceDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "cpg", "evidence"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "cpg", "evidence"), nil
}

// ResolvePolicyPath returns the absolute JSON path for a policy's evidence file.
func ResolvePolicyPath(evidenceDir, outputHash, namespace, workload string) string {
	return filepath.Join(evidenceDir, outputHash, namespace, workload+".json")
}

// ValidatePolicyRef guards evidence path construction. namespace and workload
// originate from Hubble flow labels and are expected to be k8s-constrained
// (labels cannot contain '/'), but this defensively asserts they are non-empty
// and free of path separators or directory-traversal segments before they are
// joined into a filesystem path — an empty workload would otherwise yield a
// hidden ".json" file.
func ValidatePolicyRef(namespace, workload string) error {
	if err := validatePathComponent("namespace", namespace); err != nil {
		return err
	}
	return validatePathComponent("workload", workload)
}

func validatePathComponent(kind, value string) error {
	switch {
	case value == "":
		return fmt.Errorf("evidence %s must not be empty", kind)
	case value == "." || value == "..":
		return fmt.Errorf("evidence %s must not be a directory-traversal segment: %q", kind, value)
	case strings.ContainsRune(value, '/') || strings.ContainsRune(value, filepath.Separator):
		return fmt.Errorf("evidence %s must not contain a path separator: %q", kind, value)
	}
	return nil
}
