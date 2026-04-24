// pkg/evidence/merge_test.go
package evidence

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func ts(sec int) time.Time {
	return time.Date(2026, 4, 24, 14, 0, sec, 0, time.UTC)
}

func sample(sec int, src string) FlowSample {
	return FlowSample{
		Time: ts(sec),
		Src:  FlowEndpoint{Namespace: "default", Workload: src},
		Dst:  FlowEndpoint{Namespace: "production", Workload: "api-server"},
		Port: 8080, Protocol: "TCP", Verdict: "DROPPED",
	}
}

func TestMergeAppendsNewRule(t *testing.T) {
	existing := NewSkeleton(PolicyRef{Name: "p", Namespace: "ns", Workload: "w"})

	newSession := SessionInfo{ID: "s1", StartedAt: ts(0), EndedAt: ts(10)}
	newRule := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP",
		FlowCount: 3, FirstSeen: ts(1), LastSeen: ts(5),
		ContributingSessions: []string{"s1"},
		Samples:              []FlowSample{sample(1, "a"), sample(2, "b"), sample(5, "c")},
	}

	Merge(&existing, newSession, []RuleEvidence{newRule}, MergeCaps{MaxSamples: 10, MaxSessions: 10})

	assert.Len(t, existing.Sessions, 1)
	assert.Len(t, existing.Rules, 1)
	assert.Equal(t, int64(3), existing.Rules[0].FlowCount)
	assert.Len(t, existing.Rules[0].Samples, 3)
}

func TestMergeExtendsExistingRule(t *testing.T) {
	existing := NewSkeleton(PolicyRef{Name: "p", Namespace: "ns", Workload: "w"})
	existing.Sessions = []SessionInfo{{ID: "s1", StartedAt: ts(0), EndedAt: ts(10)}}
	existing.Rules = []RuleEvidence{{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP",
		FlowCount: 3, FirstSeen: ts(1), LastSeen: ts(5),
		ContributingSessions: []string{"s1"},
		Samples:              []FlowSample{sample(1, "a"), sample(2, "b"), sample(5, "c")},
	}}

	newSession := SessionInfo{ID: "s2", StartedAt: ts(20), EndedAt: ts(30)}
	newRule := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP",
		FlowCount: 2, FirstSeen: ts(21), LastSeen: ts(25),
		ContributingSessions: []string{"s2"},
		Samples:              []FlowSample{sample(21, "d"), sample(25, "e")},
	}

	Merge(&existing, newSession, []RuleEvidence{newRule}, MergeCaps{MaxSamples: 10, MaxSessions: 10})

	assert.Len(t, existing.Rules, 1)
	r := existing.Rules[0]
	assert.Equal(t, int64(5), r.FlowCount)
	assert.Equal(t, ts(1), r.FirstSeen)
	assert.Equal(t, ts(25), r.LastSeen)
	assert.Equal(t, []string{"s1", "s2"}, r.ContributingSessions)
	assert.Len(t, r.Samples, 5)
}

func TestMergeCapsSamplesFIFO(t *testing.T) {
	existing := NewSkeleton(PolicyRef{Name: "p"})
	existing.Sessions = []SessionInfo{{ID: "s1", StartedAt: ts(0), EndedAt: ts(10)}}
	existing.Rules = []RuleEvidence{{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP",
		Samples: []FlowSample{sample(1, "a"), sample(2, "b"), sample(3, "c")},
	}}

	newSession := SessionInfo{ID: "s2", StartedAt: ts(20), EndedAt: ts(30)}
	newRule := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP",
		Samples: []FlowSample{sample(21, "d"), sample(22, "e")},
	}

	Merge(&existing, newSession, []RuleEvidence{newRule}, MergeCaps{MaxSamples: 3, MaxSessions: 10})

	r := existing.Rules[0]
	assert.Len(t, r.Samples, 3)
	// Kept newest 3 (by time): c, d, e
	assert.Equal(t, ts(3), r.Samples[0].Time)
	assert.Equal(t, ts(21), r.Samples[1].Time)
	assert.Equal(t, ts(22), r.Samples[2].Time)
}

func TestMergeCapsSessionsFIFO(t *testing.T) {
	existing := NewSkeleton(PolicyRef{Name: "p"})
	existing.Sessions = []SessionInfo{
		{ID: "s1", StartedAt: ts(0)},
		{ID: "s2", StartedAt: ts(10)},
	}

	Merge(&existing, SessionInfo{ID: "s3", StartedAt: ts(20)}, nil, MergeCaps{MaxSamples: 10, MaxSessions: 2})

	assert.Len(t, existing.Sessions, 2)
	assert.Equal(t, "s2", existing.Sessions[0].ID)
	assert.Equal(t, "s3", existing.Sessions[1].ID)
}

func TestMergePreservesRulesNotInNewSession(t *testing.T) {
	existing := NewSkeleton(PolicyRef{Name: "p"})
	existing.Sessions = []SessionInfo{{ID: "s1"}}
	existing.Rules = []RuleEvidence{
		{Key: "ingress:ep:app=x:TCP:80", Direction: "ingress", Port: "80", Protocol: "TCP"},
		{Key: "ingress:ep:app=y:TCP:443", Direction: "ingress", Port: "443", Protocol: "TCP"},
	}

	newRule := RuleEvidence{Key: "ingress:ep:app=x:TCP:80", Direction: "ingress", Port: "80", Protocol: "TCP"}
	Merge(&existing, SessionInfo{ID: "s2"}, []RuleEvidence{newRule}, MergeCaps{MaxSamples: 10, MaxSessions: 10})

	assert.Len(t, existing.Rules, 2)
	// Rules are sorted by (direction, key)
	assert.Equal(t, "ingress:ep:app=x:TCP:80", existing.Rules[0].Key)
	assert.Equal(t, "ingress:ep:app=y:TCP:443", existing.Rules[1].Key)
}
