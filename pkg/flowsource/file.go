package flowsource

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	observerpb "github.com/cilium/cilium/api/v1/observer"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
)

const defaultScannerBufferBytes = 10 * 1024 * 1024 // 10 MiB — Cilium flows with many labels can exceed 64 KiB

// FileSource streams DROPPED flows from a Hubble jsonpb dump. One flow per line,
// non-DROPPED verdicts are skipped with a counter, malformed lines are logged
// and skipped. A ".gz" extension triggers transparent gzip decompression.
type FileSource struct {
	path   string
	logger *zap.Logger
	stats  fileSourceStats
}

// FileSourceStats captures counters populated while streaming a file.
type FileSourceStats struct {
	LinesRead         int64
	FlowsEmitted      int64
	NonDroppedSkipped int64
	Malformed         int64
}

type fileSourceStats struct {
	linesRead         atomic.Int64
	flowsEmitted      atomic.Int64
	nonDroppedSkipped atomic.Int64
	malformed         atomic.Int64
}

// NewFileSource validates the path (unless "-" for stdin) and returns a source.
func NewFileSource(path string, logger *zap.Logger) (*FileSource, error) {
	if path != "-" {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("opening replay file %s: %w", path, err)
		}
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &FileSource{path: path, logger: logger}, nil
}

// Stats returns a snapshot of streaming counters.
func (s *FileSource) Stats() FileSourceStats {
	return FileSourceStats{
		LinesRead:         s.stats.linesRead.Load(),
		FlowsEmitted:      s.stats.flowsEmitted.Load(),
		NonDroppedSkipped: s.stats.nonDroppedSkipped.Load(),
		Malformed:         s.stats.malformed.Load(),
	}
}

// StreamDroppedFlows opens the file and streams DROPPED flows to the returned
// channel. The lost-events channel is pre-closed (file sources have no such
// signal). Both channels are closed when the file is consumed or ctx is canceled.
func (s *FileSource) StreamDroppedFlows(ctx context.Context, _ []string, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	r, cleanup, err := s.openReader()
	if err != nil {
		return nil, nil, err
	}

	flowCh := make(chan *flowpb.Flow, 64)
	lostCh := make(chan *flowpb.LostEvent)
	close(lostCh)

	s.logger.Info("replay starting", zap.String("file", s.path))

	go func() {
		defer close(flowCh)
		defer cleanup()

		scanner := bufio.NewScanner(r)
		buf := make([]byte, defaultScannerBufferBytes)
		scanner.Buffer(buf, defaultScannerBufferBytes)

		lineNum := 0
		for scanner.Scan() {
			lineNum++
			s.stats.linesRead.Add(1)
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var resp observerpb.GetFlowsResponse
			if err := protojson.Unmarshal([]byte(line), &resp); err != nil {
				s.stats.malformed.Add(1)
				s.logger.Warn("malformed flow line", zap.Int("line", lineNum), zap.Error(err))
				continue
			}
			f := resp.GetFlow()
			if f == nil {
				s.stats.malformed.Add(1)
				continue
			}
			if f.Verdict != flowpb.Verdict_DROPPED {
				s.stats.nonDroppedSkipped.Add(1)
				continue
			}
			select {
			case flowCh <- f:
				s.stats.flowsEmitted.Add(1)
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			s.logger.Warn("scanner error", zap.Error(err))
		}
		s.logger.Info("replay complete",
			zap.Int64("lines_read", s.stats.linesRead.Load()),
			zap.Int64("flows_dropped", s.stats.flowsEmitted.Load()),
			zap.Int64("non_dropped_skipped", s.stats.nonDroppedSkipped.Load()),
			zap.Int64("malformed_skipped", s.stats.malformed.Load()),
		)
	}()

	return flowCh, lostCh, nil
}

func (s *FileSource) openReader() (io.Reader, func(), error) {
	if s.path == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(s.path)
	if err != nil {
		return nil, nil, fmt.Errorf("opening replay file %s: %w", s.path, err)
	}
	if filepath.Ext(s.path) == ".gz" {
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, nil, fmt.Errorf("gzip reader: %w", err)
		}
		return gz, func() { gz.Close(); f.Close() }, nil
	}
	return f, func() { f.Close() }, nil
}
