package hubble

import (
	"strings"
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/SoulKyu/cpg/pkg/policy"
)

func TestTrack_Dedup(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "production",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 8080},
			},
		},
	}

	tracker.Track(flow, policy.ReasonNoL4)
	tracker.Track(flow, policy.ReasonNoL4) // duplicate

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 1, "duplicate flow should only produce one DEBUG log")
}

func TestTrack_DifferentFlows(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow1 := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "production",
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 8080},
			},
		},
	}

	flow2 := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=api"},
			Namespace: "staging",
		},
		Destination: &flowpb.Endpoint{
			Labels: []string{"reserved:world"},
		},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{
				TCP: &flowpb.TCP{DestinationPort: 443},
			},
		},
	}

	tracker.Track(flow1, policy.ReasonNoL4)
	tracker.Track(flow2, policy.ReasonWorldNoIP)

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 2, "different flows should produce separate DEBUG logs")
}

func TestTrack_SameFlowDifferentReason(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=client"},
			Namespace: "default",
		},
		Destination: &flowpb.Endpoint{
			Labels:    []string{"k8s:app=server"},
			Namespace: "production",
		},
	}

	tracker.Track(flow, policy.ReasonNoL4)
	tracker.Track(flow, policy.ReasonUnknownProtocol)

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 2, "same flow with different reasons should produce separate DEBUG logs")
}

// filterLogs returns log entries matching the given level and message substring.
func filterLogs(logs *observer.ObservedLogs, level zapcore.Level, msgSubstring string) []observer.LoggedEntry {
	var result []observer.LoggedEntry
	for _, entry := range logs.All() {
		if entry.Level == level && strings.Contains(entry.Message, msgSubstring) {
			result = append(result, entry)
		}
	}
	return result
}


func TestFlush_EmitsSummary(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow1 := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Labels: []string{"k8s:app=a"}, Namespace: "default"},
		Destination:      &flowpb.Endpoint{Labels: []string{"k8s:app=b"}, Namespace: "prod"},
	}
	flow2 := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_EGRESS,
		Source:           &flowpb.Endpoint{Labels: []string{"k8s:app=c"}, Namespace: "staging"},
		Destination:      &flowpb.Endpoint{Labels: []string{"reserved:world"}},
		L4: &flowpb.Layer4{
			Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{DestinationPort: 443}},
		},
	}

	tracker.Track(flow1, policy.ReasonNoL4)
	tracker.Track(flow1, policy.ReasonNoL4) // dup — counter still increments
	tracker.Track(flow2, policy.ReasonWorldNoIP)

	tracker.Flush()

	infoLogs := filterLogs(logs, zapcore.InfoLevel, "unhandled flows summary")
	require.Len(t, infoLogs, 1)

	fields := fieldMap(infoLogs[0])
	assert.Equal(t, int64(2), fields["no_l4"], "no_l4 counter should be 2 (tracked twice)")
	assert.Equal(t, int64(1), fields["world_no_ip"])
}

func TestFlush_ResetsCountersNotSeen(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Labels: []string{"k8s:app=a"}, Namespace: "default"},
		Destination:      &flowpb.Endpoint{Labels: []string{"k8s:app=b"}, Namespace: "prod"},
	}

	tracker.Track(flow, policy.ReasonNoL4)
	tracker.Flush()

	// Track same flow again — counter increments but no new DEBUG log
	tracker.Track(flow, policy.ReasonNoL4)
	tracker.Flush()

	debugLogs := filterLogs(logs, zapcore.DebugLevel, "unhandled flow")
	assert.Len(t, debugLogs, 1, "seen map should persist — no second DEBUG log")

	infoLogs := filterLogs(logs, zapcore.InfoLevel, "unhandled flows summary")
	require.Len(t, infoLogs, 2, "should have two flush summaries")

	assert.Equal(t, int64(1), fieldMap(infoLogs[0])["no_l4"])
	assert.Equal(t, int64(1), fieldMap(infoLogs[1])["no_l4"])
}

func TestFlush_SkipsZeroCounters(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	flow := &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Labels: []string{"k8s:app=a"}, Namespace: "default"},
		Destination:      &flowpb.Endpoint{Labels: []string{"k8s:app=b"}, Namespace: "prod"},
	}

	tracker.Track(flow, policy.ReasonNoL4)
	tracker.Flush()

	// No new tracks — flush should emit nothing
	tracker.Flush()

	infoLogs := filterLogs(logs, zapcore.InfoLevel, "unhandled flows summary")
	assert.Len(t, infoLogs, 1, "flush with zero counters should not emit INFO log")
}

func TestFlush_NoTracksNoLog(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	tracker := NewUnhandledTracker(logger)

	tracker.Flush()

	infoLogs := filterLogs(logs, zapcore.InfoLevel, "unhandled flows summary")
	assert.Empty(t, infoLogs, "flush with no tracks should not emit INFO log")
}

// fieldMap extracts int64 fields from a log entry into a map.
func fieldMap(entry observer.LoggedEntry) map[string]int64 {
	m := make(map[string]int64)
	for _, f := range entry.Context {
		if f.Type == zapcore.Int64Type {
			m[f.Key] = f.Integer
		}
	}
	return m
}
