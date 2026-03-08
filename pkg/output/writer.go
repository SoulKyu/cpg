package output

import (
	"fmt"
	"os"
	"path/filepath"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
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

	if existing != nil {
		merged := policy.MergePolicy(existing, event.Policy)
		data, err = yaml.Marshal(merged)
		if err != nil {
			return fmt.Errorf("marshaling merged policy: %w", err)
		}
		w.logger.Info("policy updated", zap.String("path", path))
	} else {
		data, err = yaml.Marshal(event.Policy)
		if err != nil {
			return fmt.Errorf("marshaling policy: %w", err)
		}
		w.logger.Info("policy written", zap.String("path", path))
	}

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
