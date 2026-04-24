package main

import (
	"testing"

	"go.uber.org/zap"
)

// initLoggerForTesting swaps the package-level logger for a no-op logger so
// subcommand tests don't produce noise and don't require the rootCmd
// PersistentPreRunE logger setup.
func initLoggerForTesting(t *testing.T) {
	t.Helper()
	logger = zap.NewNop()
}
