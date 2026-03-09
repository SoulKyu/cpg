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

	"github.com/gule/cpg/pkg/policy"
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

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing policy file %s: %w", path, err)
	}

	return nil
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
