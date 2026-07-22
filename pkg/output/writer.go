package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	"go.uber.org/zap"
	"sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/policy"
)

// Writer writes CiliumNetworkPolicy YAML files to an organized directory structure.
type Writer struct {
	outputDir string
	logger    *zap.Logger
}

// NewWriter creates a Writer that writes policies to the given output directory.
func NewWriter(outputDir string, logger *zap.Logger) *Writer {
	return &Writer{
		outputDir: outputDir,
		logger:    logger,
	}
}

// Write writes a PolicyEvent to disk as a YAML file.
// If the file already exists, it reads the existing policy, merges it with the
// incoming policy using MergePolicy, and writes the merged result.
func (w *Writer) Write(event policy.PolicyEvent) error {
	if err := evidence.ValidatePolicyRef(event.Namespace, event.Workload); err != nil {
		return fmt.Errorf("refusing to write policy YAML: %w", err)
	}
	nsDir := filepath.Join(w.outputDir, event.Namespace)
	if err := os.MkdirAll(nsDir, 0755); err != nil {
		return fmt.Errorf("creating namespace directory %s: %w", nsDir, err)
	}

	path := filepath.Join(nsDir, event.Workload+".yaml")

	var data []byte
	existing, err := readExistingPolicy(path)
	if err != nil {
		return fmt.Errorf("reading existing policy %s: %w", path, err)
	}

	var spec *api.Rule
	if existing != nil {
		merged := policy.MergePolicy(existing, event.Policy)
		spec = merged.Spec
		data, err = yaml.Marshal(merged)
		if err != nil {
			return fmt.Errorf("marshaling merged policy: %w", err)
		}
		// Compare serialized YAML to detect semantic equivalence after roundtrip.
		// In-memory comparison is unreliable due to label prefix normalization (any:).
		// Strip comments from existing file before comparing since annotations may differ.
		existingData, readErr := os.ReadFile(path)
		if readErr == nil && stripComments(string(existingData)) == string(data) {
			w.logger.Debug("policy unchanged, skipping write", zap.String("path", path))
			return nil
		}
		w.logger.Info("policy updated", zap.String("path", path))
	} else {
		spec = event.Policy.Spec
		data, err = yaml.Marshal(event.Policy)
		if err != nil {
			return fmt.Errorf("marshaling policy: %w", err)
		}
		w.logger.Info("policy written", zap.String("path", path))
	}

	// Annotate rules with human-readable comments
	data = annotateRules(data, spec)

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("setting temp file permissions: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic rename: %w", err)
	}

	return nil
}

// OutputDir returns the root directory this writer targets.
func (w *Writer) OutputDir() string {
	return w.outputDir
}

// ReadExisting returns the raw YAML bytes of the file on disk for the given
// namespace/workload, or nil if no such file exists. Errors other than "not
// exist" are returned as-is.
func (w *Writer) ReadExisting(namespace, workload string) ([]byte, error) {
	path := filepath.Join(w.outputDir, namespace, workload+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// ReadPolicyFile reads and unmarshals a CiliumNetworkPolicy from a YAML file
// on disk, delegating the parse step to UnmarshalPolicy. Unlike
// readExistingPolicy's silent (nil, nil) contract (appropriate for Write's
// internal "is there something to merge?" check), ReadPolicyFile wraps a
// missing file's error with fs.ErrNotExist so callers can detect it via
// errors.Is — matching the evidence.Reader.Read / hubble.ReadClusterHealth
// not-found convention the query tools (get_policy/list_policies) depend on
// to distinguish "no such policy" from a genuine read/parse error.
func ReadPolicyFile(path string) (*ciliumv2.CiliumNetworkPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading policy %s: %w", path, err)
	}

	cnp, err := UnmarshalPolicy(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cnp, nil
}

// UnmarshalPolicy parses a CiliumNetworkPolicy from raw YAML bytes already
// read off disk. Factored out of ReadPolicyFile (WR-03) so a caller that
// also needs the raw bytes alongside the parsed struct (e.g. cmd/cpg's
// get_policy, which returns both metadata AND the verbatim YAML) can read
// the file exactly once via its own os.ReadFile and reuse this same parse
// logic — instead of a second, independent os.ReadFile that risks observing
// a different on-disk version if Writer.Write's atomic temp+rename lands in
// between the two reads during an active capture.
func UnmarshalPolicy(data []byte) (*ciliumv2.CiliumNetworkPolicy, error) {
	var cnp ciliumv2.CiliumNetworkPolicy
	if err := yaml.Unmarshal(data, &cnp); err != nil {
		return nil, fmt.Errorf("unmarshaling policy: %w", err)
	}
	return &cnp, nil
}

// readExistingPolicy reads and unmarshals a CiliumNetworkPolicy from disk.
// Returns nil, nil if the file does not exist.
func readExistingPolicy(path string) (*ciliumv2.CiliumNetworkPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cnp ciliumv2.CiliumNetworkPolicy
	if err := yaml.Unmarshal(data, &cnp); err != nil {
		return nil, fmt.Errorf("unmarshaling existing policy: %w", err)
	}

	return &cnp, nil
}

// stripComments removes YAML comment lines (starting with optional whitespace + #)
// so that semantic comparison ignores annotation differences.
func stripComments(yamlStr string) string {
	lines := strings.Split(yamlStr, "\n")
	var filtered []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" || !strings.HasPrefix(strings.TrimSpace(line), "#") {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}
