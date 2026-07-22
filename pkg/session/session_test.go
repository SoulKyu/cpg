package session

import (
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/hubble"
)

func TestState_String(t *testing.T) {
	tests := []struct {
		name  string
		state State
		want  string
	}{
		{"capturing", StateCapturing, "capturing"},
		{"stopped", StateStopped, "stopped"},
		{"out of range", State(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.state.String())
		})
	}
}

func TestSession_BuildSummary(t *testing.T) {
	t.Run("final stats stored", func(t *testing.T) {
		start := time.Now().Add(-2 * time.Minute)
		stop := start.Add(90 * time.Second)
		s := &Session{
			ID:        "sess_abc123",
			TmpDir:    "/tmp/cpg-session-abc123",
			StartedAt: start,
			StoppedAt: stop,
			State:     StateStopped,
		}
		s.final.Store(&hubble.SessionStats{
			FlowsSeen:         7,
			PoliciesWritten:   3,
			PoliciesSkipped:   1,
			PoliciesFailed:    0,
			LostEvents:        2,
			L7HTTPCount:       4,
			L7DNSCount:        1,
			AuditVerdictCount: 6,
			InfraDropTotal:    5,
			InfraDropsByReason: map[flowpb.DropReason]uint64{
				flowpb.DropReason_POLICY_DENIED: 5,
			},
		})

		result := s.buildSummary(false, "/abs/cluster-health.json")

		assert.Equal(t, "sess_abc123", result.SessionID)
		assert.Equal(t, "stopped", result.State)
		assert.False(t, result.AlreadyStopped)
		assert.Equal(t, "1m30s", result.Duration)
		assert.Equal(t, uint64(7), result.FlowsSeen)
		assert.Equal(t, uint64(3), result.PoliciesWritten)
		assert.Equal(t, uint64(1), result.PoliciesSkipped)
		assert.Equal(t, uint64(0), result.PoliciesFailed)
		assert.Equal(t, uint64(2), result.LostEvents)
		assert.Equal(t, uint64(4), result.L7HTTPCount)
		assert.Equal(t, uint64(1), result.L7DNSCount)
		assert.Equal(t, uint64(6), result.AuditVerdictCount)
		assert.Equal(t, uint64(5), result.InfraDropTotal)
		require.Contains(t, result.InfraDropsByReason, "POLICY_DENIED")
		assert.Equal(t, uint64(5), result.InfraDropsByReason["POLICY_DENIED"])
		assert.Equal(t, "/abs/cluster-health.json", result.ClusterHealthPath)
		assert.Equal(t, "/tmp/cpg-session-abc123", result.TmpDir)
	})

	t.Run("final never stored — no panic, zeroed counters", func(t *testing.T) {
		start := time.Now().Add(-30 * time.Second)
		s := &Session{
			ID:        "sess_neverfired",
			TmpDir:    "/tmp/cpg-session-neverfired",
			StartedAt: start,
			StoppedAt: start.Add(10 * time.Second),
			State:     StateStopped,
		}

		var result StopResult
		require.NotPanics(t, func() {
			result = s.buildSummary(true, "/abs/cluster-health.json")
		})

		assert.Equal(t, "sess_neverfired", result.SessionID)
		assert.Equal(t, "stopped", result.State)
		assert.True(t, result.AlreadyStopped)
		assert.Equal(t, "10s", result.Duration)
		assert.Equal(t, uint64(0), result.FlowsSeen)
		assert.Equal(t, uint64(0), result.PoliciesWritten)
		assert.Equal(t, uint64(0), result.PoliciesSkipped)
		assert.Equal(t, uint64(0), result.PoliciesFailed)
		assert.Equal(t, uint64(0), result.LostEvents)
		assert.Equal(t, uint64(0), result.L7HTTPCount)
		assert.Equal(t, uint64(0), result.L7DNSCount)
		assert.Equal(t, uint64(0), result.AuditVerdictCount)
		assert.Equal(t, uint64(0), result.InfraDropTotal)
		assert.Empty(t, result.InfraDropsByReason)
		assert.Equal(t, "/abs/cluster-health.json", result.ClusterHealthPath)
		assert.Equal(t, "/tmp/cpg-session-neverfired", result.TmpDir)
	})
}
