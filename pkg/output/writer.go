package output

import (
	"fmt"

	"go.uber.org/zap"

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
func (w *Writer) Write(event policy.PolicyEvent) error {
	return fmt.Errorf("not implemented")
}
