package main

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// initLoggerForTesting swaps the package-level logger for a no-op logger so
// subcommand tests don't produce noise and don't require the rootCmd
// PersistentPreRunE logger setup.
func initLoggerForTesting(t *testing.T) {
	t.Helper()
	prev := logger
	logger = zap.NewNop()
	t.Cleanup(func() { logger = prev })
}

// initObservedLoggerForTesting swaps the package-level logger for an observable
// logger and returns its log buffer so tests can assert on emitted entries
// (e.g. VIS-01 warning). The previous logger is restored on test cleanup.
func initObservedLoggerForTesting(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	prev := logger
	logger = zap.New(core)
	t.Cleanup(func() { logger = prev })
	return logs
}
