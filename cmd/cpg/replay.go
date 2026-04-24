package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/flowsource"
	"github.com/SoulKyu/cpg/pkg/hubble"
)

func newReplayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay <file.jsonl|->",
		Short: "Generate policies from a captured Hubble jsonpb dump",
		Long: `Replay a Hubble jsonpb capture through the same pipeline as the live stream,
generating (or updating) policies on disk. Use this for deterministic iteration
on policy logic without having to reproduce traffic.

Capture a stream with:

	hubble observe --output jsonpb --follow > drops.jsonl

Then:

	cpg replay drops.jsonl -n production

Pipe through stdin with:

	cat drops.jsonl | cpg replay - -n production

'.gz' extensions are decompressed transparently.`,
		Args: cobra.ExactArgs(1),
		RunE: runReplay,
	}
	addCommonFlags(cmd)
	return cmd
}

func runReplay(cmd *cobra.Command, args []string) error {
	f := parseCommonFlags(cmd)
	if len(f.namespaces) > 0 && f.allNamespaces {
		return fmt.Errorf("--namespace and --all-namespaces are mutually exclusive")
	}

	path := args[0]
	source, err := flowsource.NewFileSource(path, logger)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	evidenceDir := resolveEvidenceDir(f.evidenceDir)

	sessionSource := evidence.SourceInfo{Type: "replay"}
	if path != "-" {
		if abs, err := filepath.Abs(path); err == nil {
			sessionSource.File = abs
		} else {
			sessionSource.File = path
		}
	}

	absOutDir, err := filepath.Abs(f.outputDir)
	if err != nil {
		absOutDir = f.outputDir
	}

	logger.Info("cpg replay configuration",
		zap.String("file", path),
		zap.Strings("namespaces", f.namespaces),
		zap.Bool("all-namespaces", f.allNamespaces),
		zap.String("output-dir", f.outputDir),
		zap.Bool("dry-run", f.dryRun),
		zap.Bool("evidence", !f.noEvidence),
	)

	cfg := hubble.PipelineConfig{
		Server:        "replay:" + path,
		Namespaces:    f.namespaces,
		AllNamespaces: f.allNamespaces,
		OutputDir:     f.outputDir,
		FlushInterval: f.flushInterval,
		Logger:        logger,

		DryRun:      f.dryRun,
		DryRunDiff:  !f.dryRunNoDiff,
		DryRunColor: isTerminal(os.Stdout),

		EvidenceEnabled: !f.noEvidence,
		EvidenceDir:     evidenceDir,
		OutputHash:      evidence.HashOutputDir(absOutDir),
		EvidenceCaps: evidence.MergeCaps{
			MaxSamples:  f.evidenceSamples,
			MaxSessions: f.evidenceSessions,
		},
		SessionID:     fmt.Sprintf("%s-%s", time.Now().UTC().Format(time.RFC3339), uuid.New().String()[:4]),
		SessionSource: sessionSource,
		CPGVersion:    version,
	}

	return hubble.RunPipelineWithSource(ctx, cfg, source)
}
