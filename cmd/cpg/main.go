package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var version = "dev"

// logger is the package-level zap logger, initialized by the root PersistentPreRunE.
var logger *zap.Logger

// isKubectlPlugin returns true when the binary was invoked as a kubectl plugin.
func isKubectlPlugin() bool {
	return strings.HasPrefix(filepath.Base(os.Args[0]), "kubectl-")
}

func main() {
	useName := "cpg"
	if isKubectlPlugin() {
		useName = "kubectl cilium-policy-gen"
	}

	rootCmd := &cobra.Command{
		Use:     useName,
		Short:   "Cilium Policy Generator",
		Long:    "Automatically generate CiliumNetworkPolicies from observed Hubble flow denials.",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			logger, err = buildLogger(cmd)
			if err != nil {
				return err
			}
			return nil
		},
		PersistentPostRun: func(_ *cobra.Command, _ []string) {
			if logger != nil {
				logger.Sync() //nolint:errcheck
			}
		},
	}

	// Global persistent flags for logging
	rootCmd.PersistentFlags().Bool("debug", false, "enable debug logging (shortcut for --log-level debug)")
	rootCmd.PersistentFlags().String("log-level", "info", "log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().Bool("json", false, "output logs in JSON format")

	rootCmd.AddCommand(newGenerateCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// buildLogger constructs a zap.Logger based on CLI flags.
func buildLogger(cmd *cobra.Command) (*zap.Logger, error) {
	debug, _ := cmd.Flags().GetBool("debug")
	logLevel, _ := cmd.Flags().GetString("log-level")
	jsonFormat, _ := cmd.Flags().GetBool("json")

	if jsonFormat {
		cfg := zap.NewProductionConfig()
		if debug {
			cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		} else {
			level, err := zapcore.ParseLevel(logLevel)
			if err != nil {
				return nil, err
			}
			cfg.Level = zap.NewAtomicLevelAt(level)
		}
		return cfg.Build()
	}

	if debug {
		return zap.NewDevelopment()
	}

	// Default: console encoder at configured level
	cfg := zap.NewDevelopmentConfig()
	level, err := zapcore.ParseLevel(logLevel)
	if err != nil {
		return nil, err
	}
	cfg.Level = zap.NewAtomicLevelAt(level)
	return cfg.Build()
}
