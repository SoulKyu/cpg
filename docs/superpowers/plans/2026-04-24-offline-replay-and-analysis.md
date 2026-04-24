# Offline Replay and Analysis — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `cpg replay <file>` for offline jsonpb replay, `cpg explain <target>` for per-rule flow evidence, and `--dry-run` with unified-diff preview on both commands.

**Architecture:** Introduce `pkg/flowsource` promoting the existing `FlowSource` interface out of `pkg/hubble`; add a file-based source parsing Hubble jsonpb. Introduce `pkg/evidence` to persist per-rule attribution (samples, counts, sessions) under XDG cache, keyed by a hash of the output-dir. Extend `pkg/policy/builder.go` to emit a `RuleAttribution` alongside each CNP, then fan-out from the aggregator to both the policy writer and a new evidence writer. Add `pkg/diff` for YAML unified diff in dry-run mode. New cobra subcommands `replay` and `explain` reuse shared flags via a common helper.

**Tech Stack:** Go 1.25, cobra, `google.golang.org/protobuf/encoding/protojson`, `github.com/cilium/cilium/api/v1/flow`, `github.com/cilium/cilium/api/v1/observer`, `github.com/pmezard/go-difflib`, `sigs.k8s.io/yaml`, `zap`, `testify`.

**Spec reference:** `docs/superpowers/specs/2026-04-24-offline-replay-and-analysis-design.md`

---

## Phase 1 — Foundation (refactors with no behavior change)

### Task 1: Promote FlowSource interface to pkg/flowsource

**Files:**
- Create: `pkg/flowsource/source.go`
- Modify: `pkg/hubble/pipeline.go` (remove the local `FlowSource` interface, import the new package)
- Modify: `pkg/hubble/client.go` (ensure `*Client` still satisfies the interface from its new home)
- Test: `pkg/flowsource/source_test.go`

- [ ] **Step 1: Write an interface-contract test**

```go
// pkg/flowsource/source_test.go
package flowsource

import (
	"context"
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

type stubSource struct{}

func (stubSource) StreamDroppedFlows(_ context.Context, _ []string, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	return nil, nil, nil
}

func TestFlowSourceInterfaceSatisfied(t *testing.T) {
	var _ FlowSource = stubSource{}
}
```

- [ ] **Step 2: Run the test — it must fail to compile**

Run: `go test ./pkg/flowsource/...`
Expected: build error `package pkg/flowsource: no Go files` or `undefined: FlowSource`.

- [ ] **Step 3: Create the interface**

```go
// pkg/flowsource/source.go
// Package flowsource decouples the streaming source of Hubble flows from the
// streaming pipeline, so the same pipeline can be fed by a live gRPC client
// or by an offline capture file.
package flowsource

import (
	"context"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

// FlowSource abstracts the streaming source for testability and offline replay.
// Implementations MUST close both returned channels when the stream ends.
type FlowSource interface {
	StreamDroppedFlows(ctx context.Context, namespaces []string, allNS bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error)
}
```

- [ ] **Step 4: Run the test — it must pass**

Run: `go test ./pkg/flowsource/...`
Expected: `ok   github.com/SoulKyu/cpg/pkg/flowsource`.

- [ ] **Step 5: Migrate pkg/hubble to consume the new interface**

Edit `pkg/hubble/pipeline.go`:

Remove the local interface:

```go
// FlowSource abstracts the streaming source for testability.
type FlowSource interface {
	StreamDroppedFlows(ctx context.Context, namespaces []string, allNS bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error)
}
```

Replace the imports block with:

```go
import (
	"context"
	"time"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/SoulKyu/cpg/pkg/flowsource"
	"github.com/SoulKyu/cpg/pkg/output"
	"github.com/SoulKyu/cpg/pkg/policy"
)
```

Replace the `RunPipelineWithSource` signature to use the new package type:

```go
func RunPipelineWithSource(ctx context.Context, cfg PipelineConfig, source flowsource.FlowSource) error {
```

Remove the `flowpb` import from `pkg/hubble/pipeline.go` (it is no longer referenced there since the interface moved).

- [ ] **Step 6: Run the full build and test suite**

Run: `go build ./...`
Expected: success.

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 7: Commit**

```bash
git add pkg/flowsource/ pkg/hubble/pipeline.go
git commit -m "refactor: promote FlowSource interface to pkg/flowsource"
```

---

### Task 2: Evidence JSON schema

**Files:**
- Create: `pkg/evidence/schema.go`
- Test: `pkg/evidence/schema_test.go`

- [ ] **Step 1: Write a roundtrip test**

```go
// pkg/evidence/schema_test.go
package evidence

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaRoundtrip(t *testing.T) {
	original := PolicyEvidence{
		SchemaVersion: 1,
		Policy: PolicyRef{
			Name:      "cpg-api-server",
			Namespace: "production",
			Workload:  "api-server",
		},
		Sessions: []SessionInfo{{
			ID:             "2026-04-24T14:02:11Z-a3f2",
			StartedAt:      time.Date(2026, 4, 24, 14, 2, 11, 0, time.UTC),
			EndedAt:        time.Date(2026, 4, 24, 14, 15, 48, 0, time.UTC),
			CPGVersion:     "1.6.0",
			Source:         SourceInfo{Type: "replay", File: "/tmp/flows.jsonl"},
			FlowsIngested:  12847,
			FlowsUnhandled: 42,
		}},
		Rules: []RuleEvidence{{
			Key:       "ingress:ep:app=weird-thing:TCP:8080",
			Direction: "ingress",
			Peer: PeerRef{
				Type:   "endpoint",
				Labels: map[string]string{"app": "weird-thing"},
			},
			Port:                 "8080",
			Protocol:             "TCP",
			FlowCount:            23,
			FirstSeen:            time.Date(2026, 4, 24, 14, 2, 11, 0, time.UTC),
			LastSeen:             time.Date(2026, 4, 24, 14, 15, 48, 0, time.UTC),
			ContributingSessions: []string{"2026-04-24T14:02:11Z-a3f2"},
			Samples: []FlowSample{{
				Time:     time.Date(2026, 4, 24, 14, 2, 11, 0, time.UTC),
				Src:      FlowEndpoint{Namespace: "default", Workload: "weird-thing", Pod: "weird-thing-5d4f"},
				Dst:      FlowEndpoint{Namespace: "production", Workload: "api-server", Pod: "api-server-abc"},
				Port:     8080,
				Protocol: "TCP",
				Verdict:  "DROPPED",
			}},
		}},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded PolicyEvidence
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original, decoded)
}

func TestSchemaEmptySkeleton(t *testing.T) {
	skel := NewSkeleton(PolicyRef{Name: "cpg-x", Namespace: "ns", Workload: "x"})
	assert.Equal(t, 1, skel.SchemaVersion)
	assert.Equal(t, "cpg-x", skel.Policy.Name)
	assert.Empty(t, skel.Sessions)
	assert.Empty(t, skel.Rules)
}
```

- [ ] **Step 2: Run test — must fail to build**

Run: `go test ./pkg/evidence/...`
Expected: build error (`undefined: PolicyEvidence` etc.).

- [ ] **Step 3: Create the schema**

```go
// pkg/evidence/schema.go
// Package evidence stores per-rule attribution for policies produced by cpg.
// It answers "which flows caused this rule?" for `cpg explain`.
package evidence

import "time"

// SchemaVersion is bumped whenever the on-disk format is not backwards
// compatible. Readers must refuse unknown versions.
const SchemaVersion = 1

// PolicyEvidence is the root document persisted to
// <evidence-dir>/<output-dir-hash>/<namespace>/<workload>.json.
type PolicyEvidence struct {
	SchemaVersion int            `json:"schema_version"`
	Policy        PolicyRef      `json:"policy"`
	Sessions      []SessionInfo  `json:"sessions"`
	Rules         []RuleEvidence `json:"rules"`
}

// PolicyRef identifies the CiliumNetworkPolicy this evidence file documents.
type PolicyRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
}

// SessionInfo records one invocation of generate or replay.
type SessionInfo struct {
	ID             string     `json:"id"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        time.Time  `json:"ended_at"`
	CPGVersion     string     `json:"cpg_version"`
	Source         SourceInfo `json:"source"`
	FlowsIngested  int64      `json:"flows_ingested"`
	FlowsUnhandled int64      `json:"flows_unhandled"`
}

// SourceInfo describes where flows came from for a session.
type SourceInfo struct {
	Type   string `json:"type"` // "live" | "replay"
	File   string `json:"file,omitempty"`
	Server string `json:"server,omitempty"`
}

// RuleEvidence attributes a single rule emitted in the policy YAML.
type RuleEvidence struct {
	Key                  string       `json:"key"`
	Direction            string       `json:"direction"` // "ingress" | "egress"
	Peer                 PeerRef      `json:"peer"`
	Port                 string       `json:"port"`
	Protocol             string       `json:"protocol"`
	FlowCount            int64        `json:"flow_count"`
	FirstSeen            time.Time    `json:"first_seen"`
	LastSeen             time.Time    `json:"last_seen"`
	ContributingSessions []string     `json:"contributing_sessions"`
	Samples              []FlowSample `json:"samples"`
}

// PeerRef encodes the rule peer in a uniform shape across endpoint, CIDR, and
// entity peers. Only the field corresponding to Type is populated.
type PeerRef struct {
	Type   string            `json:"type"` // "endpoint" | "cidr" | "entity"
	Labels map[string]string `json:"labels,omitempty"`
	CIDR   string            `json:"cidr,omitempty"`
	Entity string            `json:"entity,omitempty"`
}

// FlowSample is a compact record of one contributing flow.
type FlowSample struct {
	Time       time.Time    `json:"time"`
	Src        FlowEndpoint `json:"src"`
	Dst        FlowEndpoint `json:"dst"`
	Port       uint32       `json:"port"`
	Protocol   string       `json:"protocol"`
	Verdict    string       `json:"verdict"`
	DropReason string       `json:"drop_reason,omitempty"`
}

// FlowEndpoint identifies a participant in a flow sample.
type FlowEndpoint struct {
	Namespace string `json:"namespace,omitempty"`
	Workload  string `json:"workload,omitempty"`
	Pod       string `json:"pod,omitempty"`
	IP        string `json:"ip,omitempty"`
}

// NewSkeleton returns an empty evidence document for a freshly observed policy.
func NewSkeleton(ref PolicyRef) PolicyEvidence {
	return PolicyEvidence{
		SchemaVersion: SchemaVersion,
		Policy:        ref,
	}
}
```

- [ ] **Step 4: Run test — must pass**

Run: `go test ./pkg/evidence/...`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add pkg/evidence/schema.go pkg/evidence/schema_test.go
git commit -m "feat(evidence): add JSON schema for per-rule attribution"
```

---

### Task 3: Evidence paths (XDG + output-dir hash)

**Files:**
- Create: `pkg/evidence/paths.go`
- Test: `pkg/evidence/paths_test.go`

- [ ] **Step 1: Write tests for path resolution**

```go
// pkg/evidence/paths_test.go
package evidence

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashOutputDirStable(t *testing.T) {
	h1 := HashOutputDir("/home/gule/work/cpg/policies")
	h2 := HashOutputDir("/home/gule/work/cpg/policies/") // trailing slash
	h3 := HashOutputDir("/home/gule/work/cpg/policies/./")
	assert.Equal(t, h1, h2, "trailing slash must not affect hash")
	assert.Equal(t, h1, h3, "./ segments must not affect hash")
	assert.Len(t, h1, 12)
}

func TestHashOutputDirDifferent(t *testing.T) {
	h1 := HashOutputDir("/a/b")
	h2 := HashOutputDir("/a/c")
	assert.NotEqual(t, h1, h2)
}

func TestDefaultEvidenceDirRespectsXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-test")
	got, err := DefaultEvidenceDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/xdg-test", "cpg", "evidence"), got)
}

func TestDefaultEvidenceDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/tmp/home-test")
	got, err := DefaultEvidenceDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/home-test", ".cache", "cpg", "evidence"), got)
}

func TestResolvePolicyPath(t *testing.T) {
	p := ResolvePolicyPath("/base", "a3f2b1", "production", "api-server")
	assert.Equal(t, "/base/a3f2b1/production/api-server.json", p)
}
```

- [ ] **Step 2: Run test — must fail to build**

Run: `go test ./pkg/evidence/... -run Path`
Expected: build errors (`undefined: HashOutputDir` etc.).

- [ ] **Step 3: Implement paths**

```go
// pkg/evidence/paths.go
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// HashOutputDir derives a 12-char hex digest of the canonical output directory
// path. It allows multiple workspaces to share the same evidence-dir without
// collision. The input is normalized via filepath.Clean + filepath.Abs so
// equivalent paths ("foo/bar/", "foo/./bar") hash identically.
func HashOutputDir(outputDir string) string {
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		abs = outputDir
	}
	abs = filepath.Clean(abs)
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:12]
}

// DefaultEvidenceDir returns the evidence storage root, honoring XDG_CACHE_HOME
// with a fallback to $HOME/.cache.
func DefaultEvidenceDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "cpg", "evidence"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "cpg", "evidence"), nil
}

// ResolvePolicyPath returns the absolute JSON path for a policy's evidence file.
func ResolvePolicyPath(evidenceDir, outputHash, namespace, workload string) string {
	return filepath.Join(evidenceDir, outputHash, namespace, workload+".json")
}
```

- [ ] **Step 4: Run test — must pass**

Run: `go test ./pkg/evidence/...`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/evidence/paths.go pkg/evidence/paths_test.go
git commit -m "feat(evidence): XDG-aware path resolver with output-dir hash"
```

---

## Phase 2 — Evidence merge + IO

### Task 4: Merge semantics

**Files:**
- Create: `pkg/evidence/merge.go`
- Test: `pkg/evidence/merge_test.go`

- [ ] **Step 1: Write tests for all merge branches**

```go
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
```

- [ ] **Step 2: Run tests — must fail to build**

Run: `go test ./pkg/evidence/... -run Merge`
Expected: `undefined: Merge`, `undefined: MergeCaps`.

- [ ] **Step 3: Implement merge**

```go
// pkg/evidence/merge.go
package evidence

import "sort"

// MergeCaps bounds the size of the merged evidence file.
type MergeCaps struct {
	MaxSamples  int // samples kept per rule (newest by time)
	MaxSessions int // sessions kept in total (newest by StartedAt)
}

// Merge folds a new session and its rules into an existing PolicyEvidence
// document in place. Rule identity is the Key field. Samples and sessions are
// capped FIFO by time. first_seen is the earliest time ever recorded for a
// rule; last_seen is the latest.
func Merge(existing *PolicyEvidence, session SessionInfo, newRules []RuleEvidence, caps MergeCaps) {
	// Append session, cap newest-first by StartedAt.
	existing.Sessions = append(existing.Sessions, session)
	if caps.MaxSessions > 0 && len(existing.Sessions) > caps.MaxSessions {
		sort.SliceStable(existing.Sessions, func(i, j int) bool {
			return existing.Sessions[i].StartedAt.Before(existing.Sessions[j].StartedAt)
		})
		drop := len(existing.Sessions) - caps.MaxSessions
		existing.Sessions = existing.Sessions[drop:]
	}

	byKey := make(map[string]int, len(existing.Rules))
	for i, r := range existing.Rules {
		byKey[r.Key] = i
	}

	for _, nr := range newRules {
		if idx, ok := byKey[nr.Key]; ok {
			merged := mergeRule(existing.Rules[idx], nr, caps.MaxSamples)
			existing.Rules[idx] = merged
			continue
		}
		// New rule: ensure samples are capped and sorted as well.
		nr.Samples = capSamples(nr.Samples, caps.MaxSamples)
		existing.Rules = append(existing.Rules, nr)
		byKey[nr.Key] = len(existing.Rules) - 1
	}

	sort.Slice(existing.Rules, func(i, j int) bool {
		if existing.Rules[i].Direction != existing.Rules[j].Direction {
			return existing.Rules[i].Direction < existing.Rules[j].Direction
		}
		return existing.Rules[i].Key < existing.Rules[j].Key
	})
}

func mergeRule(a, b RuleEvidence, maxSamples int) RuleEvidence {
	out := a
	out.FlowCount += b.FlowCount
	if !b.FirstSeen.IsZero() && (out.FirstSeen.IsZero() || b.FirstSeen.Before(out.FirstSeen)) {
		out.FirstSeen = b.FirstSeen
	}
	if b.LastSeen.After(out.LastSeen) {
		out.LastSeen = b.LastSeen
	}
	out.ContributingSessions = append(out.ContributingSessions, b.ContributingSessions...)
	out.Samples = capSamples(append(append([]FlowSample{}, a.Samples...), b.Samples...), maxSamples)
	return out
}

// capSamples sorts samples by time ascending and keeps the newest maxSamples.
// A non-positive maxSamples keeps all samples.
func capSamples(s []FlowSample, maxSamples int) []FlowSample {
	sort.SliceStable(s, func(i, j int) bool {
		return s[i].Time.Before(s[j].Time)
	})
	if maxSamples > 0 && len(s) > maxSamples {
		s = s[len(s)-maxSamples:]
	}
	return s
}
```

- [ ] **Step 4: Run tests — must pass**

Run: `go test ./pkg/evidence/... -run Merge -v`
Expected: all 5 tests pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/evidence/merge.go pkg/evidence/merge_test.go
git commit -m "feat(evidence): merge semantics with FIFO sample/session caps"
```

---

### Task 5: Writer (atomic write + load-merge-write)

**Files:**
- Create: `pkg/evidence/writer.go`
- Test: `pkg/evidence/writer_test.go`

- [ ] **Step 1: Write tests**

```go
// pkg/evidence/writer_test.go
package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriterCreatesFile(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, "hash0", MergeCaps{MaxSamples: 10, MaxSessions: 10})

	ref := PolicyRef{Name: "cpg-api", Namespace: "prod", Workload: "api"}
	session := SessionInfo{ID: "s1", StartedAt: ts(0), EndedAt: ts(10)}
	rule := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP", FlowCount: 1,
		Samples: []FlowSample{sample(1, "a")},
	}

	require.NoError(t, w.Write(ref, session, []RuleEvidence{rule}))

	path := filepath.Join(dir, "hash0", "prod", "api.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var got PolicyEvidence
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, 1, got.SchemaVersion)
	assert.Equal(t, ref, got.Policy)
	assert.Len(t, got.Sessions, 1)
	assert.Len(t, got.Rules, 1)
}

func TestWriterMergesWithExisting(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir, "hash0", MergeCaps{MaxSamples: 10, MaxSessions: 10})

	ref := PolicyRef{Name: "cpg-api", Namespace: "prod", Workload: "api"}
	session1 := SessionInfo{ID: "s1", StartedAt: ts(0), EndedAt: ts(10)}
	rule := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP", FlowCount: 1,
		FirstSeen: ts(1), LastSeen: ts(5),
		ContributingSessions: []string{"s1"},
		Samples:              []FlowSample{sample(1, "a")},
	}
	require.NoError(t, w.Write(ref, session1, []RuleEvidence{rule}))

	// Second run, same rule
	session2 := SessionInfo{ID: "s2", StartedAt: ts(20), EndedAt: ts(30)}
	rule2 := RuleEvidence{
		Key: "ingress:ep:app=x:TCP:80", Direction: "ingress",
		Peer: PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
		Port: "80", Protocol: "TCP", FlowCount: 2,
		FirstSeen: ts(21), LastSeen: ts(25),
		ContributingSessions: []string{"s2"},
		Samples:              []FlowSample{sample(21, "d"), sample(25, "e")},
	}
	require.NoError(t, w.Write(ref, session2, []RuleEvidence{rule2}))

	got, err := NewReader(dir, "hash0").Read("prod", "api")
	require.NoError(t, err)
	assert.Len(t, got.Sessions, 2)
	assert.Len(t, got.Rules, 1)
	assert.Equal(t, int64(3), got.Rules[0].FlowCount)
	assert.Equal(t, ts(1), got.Rules[0].FirstSeen)
	assert.Equal(t, ts(25), got.Rules[0].LastSeen)
}

func TestWriterIgnoresUnknownSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hash0", "prod", "api.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`{"schema_version": 999}`), 0o644))

	w := NewWriter(dir, "hash0", MergeCaps{MaxSamples: 10, MaxSessions: 10})
	ref := PolicyRef{Name: "cpg-api", Namespace: "prod", Workload: "api"}
	session := SessionInfo{ID: "s1", StartedAt: time.Now()}

	err := w.Write(ref, session, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema_version")
}
```

- [ ] **Step 2: Run tests — must fail to build**

Run: `go test ./pkg/evidence/... -run Writer`
Expected: `undefined: NewWriter`, `undefined: NewReader`.

- [ ] **Step 3: Implement reader**

```go
// pkg/evidence/reader.go
package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Reader loads PolicyEvidence from the filesystem.
type Reader struct {
	evidenceDir string
	outputHash  string
}

// NewReader constructs a Reader scoped to an evidence directory and output-dir hash.
func NewReader(evidenceDir, outputHash string) *Reader {
	return &Reader{evidenceDir: evidenceDir, outputHash: outputHash}
}

// Read returns the PolicyEvidence for the given workload. It returns an error
// wrapping fs.ErrNotExist when the file is absent; callers can detect that via
// errors.Is.
func (r *Reader) Read(namespace, workload string) (PolicyEvidence, error) {
	path := ResolvePolicyPath(r.evidenceDir, r.outputHash, namespace, workload)
	data, err := os.ReadFile(path)
	if err != nil {
		return PolicyEvidence{}, fmt.Errorf("reading evidence %s: %w", path, err)
	}
	var pe PolicyEvidence
	if err := json.Unmarshal(data, &pe); err != nil {
		return PolicyEvidence{}, fmt.Errorf("parsing evidence %s: %w", path, err)
	}
	if pe.SchemaVersion != SchemaVersion {
		return PolicyEvidence{}, fmt.Errorf("unsupported schema_version %d in %s (this cpg understands %d)", pe.SchemaVersion, path, SchemaVersion)
	}
	return pe, nil
}

// IsNotExist reports whether err is the not-found variant returned by Read.
func IsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
```

- [ ] **Step 4: Implement writer**

```go
// pkg/evidence/writer.go
package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Writer loads existing evidence, folds in a new session, and persists the
// result atomically (temp-file + rename). It is safe for concurrent use from a
// single process only: cross-process concurrency is not expected for cpg.
type Writer struct {
	evidenceDir string
	outputHash  string
	caps        MergeCaps
}

// NewWriter constructs a Writer.
func NewWriter(evidenceDir, outputHash string, caps MergeCaps) *Writer {
	return &Writer{evidenceDir: evidenceDir, outputHash: outputHash, caps: caps}
}

// Write merges the new session and rules into the on-disk evidence for the
// named workload and persists the result.
func (w *Writer) Write(ref PolicyRef, session SessionInfo, newRules []RuleEvidence) error {
	path := ResolvePolicyPath(w.evidenceDir, w.outputHash, ref.Namespace, ref.Workload)

	var existing PolicyEvidence
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("parsing existing evidence %s: %w", path, err)
		}
		if existing.SchemaVersion != SchemaVersion {
			return fmt.Errorf("refusing to merge: existing evidence %s has schema_version %d (this cpg understands %d)", path, existing.SchemaVersion, SchemaVersion)
		}
	case errors.Is(err, fs.ErrNotExist):
		existing = NewSkeleton(ref)
	default:
		return fmt.Errorf("reading existing evidence %s: %w", path, err)
	}

	Merge(&existing, session, newRules, w.caps)

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding evidence: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating evidence dir: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic rename: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run tests — must pass**

Run: `go test ./pkg/evidence/... -v`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add pkg/evidence/writer.go pkg/evidence/reader.go pkg/evidence/writer_test.go
git commit -m "feat(evidence): atomic writer + reader with schema version check"
```

---

## Phase 3 — Attribution from the policy builder

### Task 6: Attribution type and rule keys

**Files:**
- Create: `pkg/policy/attribution.go`
- Test: `pkg/policy/attribution_test.go`

- [ ] **Step 1: Write tests for the rule-key encoder**

```go
// pkg/policy/attribution_test.go
package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRuleKeyEndpointPeer(t *testing.T) {
	k := RuleKey{
		Direction: "ingress",
		Peer:      Peer{Type: PeerEndpoint, Labels: map[string]string{"app": "x", "env": "prod"}},
		Port:      "8080",
		Protocol:  "TCP",
	}
	assert.Equal(t, "ingress:ep:app=x,env=prod:TCP:8080", k.String())
}

func TestRuleKeyCIDRPeer(t *testing.T) {
	k := RuleKey{
		Direction: "egress",
		Peer:      Peer{Type: PeerCIDR, CIDR: "10.0.0.0/24"},
		Port:      "443",
		Protocol:  "TCP",
	}
	assert.Equal(t, "egress:cidr:10.0.0.0/24:TCP:443", k.String())
}

func TestRuleKeyEntityPeer(t *testing.T) {
	k := RuleKey{
		Direction: "ingress",
		Peer:      Peer{Type: PeerEntity, Entity: "host"},
		Port:      "22",
		Protocol:  "TCP",
	}
	assert.Equal(t, "ingress:entity:host:TCP:22", k.String())
}

func TestRuleKeyLabelsDeterministic(t *testing.T) {
	k1 := RuleKey{
		Direction: "ingress",
		Peer:      Peer{Type: PeerEndpoint, Labels: map[string]string{"b": "2", "a": "1"}},
		Port:      "80", Protocol: "TCP",
	}
	k2 := RuleKey{
		Direction: "ingress",
		Peer:      Peer{Type: PeerEndpoint, Labels: map[string]string{"a": "1", "b": "2"}},
		Port:      "80", Protocol: "TCP",
	}
	assert.Equal(t, k1.String(), k2.String())
}
```

- [ ] **Step 2: Run test — must fail to build**

Run: `go test ./pkg/policy/... -run RuleKey`
Expected: `undefined: RuleKey`.

- [ ] **Step 3: Implement RuleKey + Attribution**

```go
// pkg/policy/attribution.go
package policy

import (
	"fmt"
	"sort"
	"strings"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
)

// PeerType identifies the kind of peer a rule addresses.
type PeerType string

const (
	PeerEndpoint PeerType = "endpoint"
	PeerCIDR     PeerType = "cidr"
	PeerEntity   PeerType = "entity"
)

// Peer is a uniform description of a rule peer used for attribution.
type Peer struct {
	Type   PeerType
	Labels map[string]string
	CIDR   string
	Entity string
}

// RuleKey uniquely identifies a rule within a policy in a stable, sortable form.
type RuleKey struct {
	Direction string // "ingress" | "egress"
	Peer      Peer
	Port      string
	Protocol  string
}

// String renders a deterministic string representation suitable for use as a
// map key and stable across runs.
func (k RuleKey) String() string {
	return fmt.Sprintf("%s:%s:%s:%s", k.Direction, encodePeer(k.Peer), k.Protocol, k.Port)
}

func encodePeer(p Peer) string {
	switch p.Type {
	case PeerEndpoint:
		keys := make([]string, 0, len(p.Labels))
		for k := range p.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + "=" + p.Labels[k]
		}
		return "ep:" + strings.Join(parts, ",")
	case PeerCIDR:
		return "cidr:" + p.CIDR
	case PeerEntity:
		return "entity:" + p.Entity
	default:
		return "unknown:"
	}
}

// RuleAttribution records, for each rule emitted in a policy, the flows that
// contributed to its creation during the current session.
type RuleAttribution struct {
	Key       RuleKey
	FlowCount int64
	FirstSeen time.Time
	LastSeen  time.Time
	Samples   []*flowpb.Flow // capped by caller
}
```

- [ ] **Step 4: Run tests — must pass**

Run: `go test ./pkg/policy/... -run RuleKey -v`
Expected: 4 tests pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/policy/attribution.go pkg/policy/attribution_test.go
git commit -m "feat(policy): add RuleKey and RuleAttribution types"
```

---

### Task 7: Track attribution during BuildPolicy

**Files:**
- Modify: `pkg/policy/builder.go` (all `peerRules`, `endpointBucket`, `cidrBucket`, entity bucket paths)
- Modify: `pkg/policy/event.go` (add `Attribution` field to `PolicyEvent`)
- Test: `pkg/policy/builder_attribution_test.go`

- [ ] **Step 1: Write test asserting BuildPolicy returns attribution matching emitted rules**

```go
// pkg/policy/builder_attribution_test.go
package policy

import (
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ingressTCPFlow(srcLabels, dstLabels []string, namespace string, port uint32) *flowpb.Flow {
	return &flowpb.Flow{
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Labels: srcLabels, Namespace: "default"},
		Destination:      &flowpb.Endpoint{Labels: dstLabels, Namespace: namespace},
		L4:               &flowpb.Layer4{Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{DestinationPort: port}}},
	}
}

func TestBuildPolicyEmitsAttribution(t *testing.T) {
	flows := []*flowpb.Flow{
		ingressTCPFlow([]string{"k8s:app=frontend"}, []string{"k8s:app=api"}, "prod", 8080),
		ingressTCPFlow([]string{"k8s:app=frontend"}, []string{"k8s:app=api"}, "prod", 8080),
		ingressTCPFlow([]string{"k8s:app=worker"}, []string{"k8s:app=api"}, "prod", 8443),
	}

	cnp, attrib := BuildPolicy("prod", "api", flows, nil, AttributionOptions{MaxSamples: 10})

	require.NotNil(t, cnp)
	require.Len(t, cnp.Spec.Ingress, 2)

	ruleKeys := make(map[string]*RuleAttribution)
	for i := range attrib {
		ruleKeys[attrib[i].Key.String()] = &attrib[i]
	}

	k1 := RuleKey{Direction: "ingress", Peer: Peer{Type: PeerEndpoint, Labels: map[string]string{"app": "frontend"}}, Port: "8080", Protocol: "TCP"}
	k2 := RuleKey{Direction: "ingress", Peer: Peer{Type: PeerEndpoint, Labels: map[string]string{"app": "worker"}}, Port: "8443", Protocol: "TCP"}

	a1, ok := ruleKeys[k1.String()]
	require.True(t, ok, "attribution missing for %s", k1.String())
	assert.Equal(t, int64(2), a1.FlowCount)
	assert.Len(t, a1.Samples, 2)

	a2, ok := ruleKeys[k2.String()]
	require.True(t, ok)
	assert.Equal(t, int64(1), a2.FlowCount)
}
```

- [ ] **Step 2: Run test — must fail to build**

Run: `go test ./pkg/policy/... -run BuildPolicyEmitsAttribution`
Expected: build error (`BuildPolicy` signature mismatch, `AttributionOptions` undefined).

- [ ] **Step 3: Extend the builder**

Edit `pkg/policy/builder.go`.

Add a new type near the top (after `policyNamePrefix`):

```go
// AttributionOptions controls how much per-rule flow evidence is retained
// during BuildPolicy. When MaxSamples is 0, no attribution is tracked — the
// returned slice is nil.
type AttributionOptions struct {
	MaxSamples int
}
```

Extend `peerRules` (around line 186) to carry attribution state:

```go
type peerRules struct {
	ports      []api.PortProtocol
	icmpFields []api.ICMPField
	seen       map[string]struct{}
	// attribution: one entry per rule key produced from this bucket
	attrib     map[string]*RuleAttribution
}

func newPeerRules() *peerRules {
	return &peerRules{seen: make(map[string]struct{}), attrib: make(map[string]*RuleAttribution)}
}
```

Add a helper on `peerRules` to record attribution (add near `addFlow`):

```go
func (pr *peerRules) recordAttribution(key RuleKey, f *flowpb.Flow, maxSamples int) {
	if maxSamples <= 0 {
		return
	}
	k := key.String()
	entry, ok := pr.attrib[k]
	if !ok {
		entry = &RuleAttribution{Key: key}
		pr.attrib[k] = entry
	}
	entry.FlowCount++
	if ts := flowTime(f); !ts.IsZero() {
		if entry.FirstSeen.IsZero() || ts.Before(entry.FirstSeen) {
			entry.FirstSeen = ts
		}
		if ts.After(entry.LastSeen) {
			entry.LastSeen = ts
		}
	}
	if len(entry.Samples) < maxSamples {
		entry.Samples = append(entry.Samples, f)
	} else {
		// FIFO newest: drop oldest, append new
		entry.Samples = append(entry.Samples[1:], f)
	}
}
```

Add a time extractor at the bottom of `builder.go`:

```go
// flowTime extracts a timestamp from a Hubble flow, falling back to zero when
// absent (Hubble always populates this in practice but tests may omit it).
func flowTime(f *flowpb.Flow) time.Time {
	if f == nil || f.Time == nil {
		return time.Time{}
	}
	return f.Time.AsTime()
}
```

Add the import at top of `builder.go` if not already present:

```go
	"time"
```

Change the signature of `BuildPolicy` to:

```go
func BuildPolicy(namespace, workload string, flows []*flowpb.Flow, tracker FlowTracker, opts AttributionOptions) (*ciliumv2.CiliumNetworkPolicy, []RuleAttribution) {
```

Inside `BuildPolicy`, thread `opts` down to `buildIngressRules` / `buildEgressRules`. Their signatures change to accept `opts` and return `([]api.IngressRule, []RuleAttribution)` / `([]api.EgressRule, []RuleAttribution)`.

In each rule-emission site (`endpointBucket` → `toPorts`, `cidrBucket` → `toPorts`, entity bucket → `toPorts`), after the rule is appended to the output slice, iterate over the bucket's `attrib` map and append each `*RuleAttribution` (value copy) to the returned attribution slice. Fix peer type/labels/CIDR/entity fields on the recorded `RuleKey` at the moment the bucket is flushed (they are known at that point from the bucket's selector or cidr field).

At the flow-ingest site inside `groupFlows`, after `pr.addFlow(fp)`, call
`pr.recordAttribution(ruleKeyFor(direction, peerFromBucketContext, fp), f, opts.MaxSamples)` using a newly-introduced helper `ruleKeyFor(direction string, peer Peer, fp *flowProto) RuleKey`.

Also add `ruleKeyFor`:

```go
func ruleKeyFor(direction string, peer Peer, fp *flowProto) RuleKey {
	return RuleKey{
		Direction: direction,
		Peer:      peer,
		Port:      strconv.FormatUint(uint64(fp.port), 10),
		Protocol:  protoDisplayName(fp.proto, fp.icmp),
	}
}

func protoDisplayName(p api.L4Proto, isICMP bool) string {
	switch {
	case p == api.ProtoTCP:
		return "TCP"
	case p == api.ProtoUDP:
		return "UDP"
	case p == api.ProtoICMP:
		return "ICMPv4"
	case p == api.ProtoICMPv6:
		return "ICMPv6"
	default:
		return "UNKNOWN"
	}
}
```

In the grouping code, compute `peerFromBucketContext` for each flow direction:

- Endpoint bucket: `Peer{Type: PeerEndpoint, Labels: selectedLabelsFromFlow(peer)}`
- CIDR bucket: `Peer{Type: PeerCIDR, CIDR: "<ip>/32"}`
- Entity bucket: `Peer{Type: PeerEntity, Entity: string(entity)}`

Maintain a helper `selectedLabelsFromFlow(ep *flowpb.Endpoint) map[string]string` that wraps the existing `labels.SelectLabels(ep.Labels)` and returns the map directly.

Update every internal call site of `BuildPolicy` (there should only be `pkg/hubble/aggregator.go` and tests).

Edit `pkg/hubble/aggregator.go`, in `flush()` (around line 121):

```go
cnp, attrib := policy.BuildPolicy(key.Namespace, key.Workload, flows, a.tracker, policy.AttributionOptions{MaxSamples: a.maxSamples})
out <- policy.PolicyEvent{
	Namespace:   key.Namespace,
	Workload:    key.Workload,
	Policy:      cnp,
	Attribution: attrib,
}
```

Extend `Aggregator` struct and `NewAggregator` to carry a `maxSamples int` field (default 0 when unset = no attribution tracked).

Edit `pkg/policy/event.go` (likely in `builder.go` or `pipeline.go` — grep for `type PolicyEvent`):

```go
type PolicyEvent struct {
	Namespace   string
	Workload    string
	Policy      *ciliumv2.CiliumNetworkPolicy
	Attribution []RuleAttribution // nil when AttributionOptions.MaxSamples == 0
}
```

- [ ] **Step 4: Update existing tests that call BuildPolicy**

Search for `BuildPolicy(` across the repo and add `policy.AttributionOptions{}` as the last argument. Use `_` to ignore the second return value in tests that don't care about attribution.

Run: `grep -rn "BuildPolicy(" --include="*.go"`
Update each call site.

- [ ] **Step 5: Run all tests — must pass**

Run: `go build ./... && go test ./...`
Expected: success.

- [ ] **Step 6: Commit**

```bash
git add pkg/policy/ pkg/hubble/
git commit -m "feat(policy): BuildPolicy returns per-rule attribution"
```

---

## Phase 4 — File source

### Task 8: Jsonpb file source (plain text)

**Files:**
- Create: `pkg/flowsource/file.go`
- Test: `pkg/flowsource/file_test.go`
- Create: `testdata/flows/small.jsonl`
- Create: `testdata/flows/with_non_dropped.jsonl`
- Create: `testdata/flows/malformed.jsonl`
- Create: `testdata/flows/empty.jsonl`

- [ ] **Step 1: Create the fixture files**

Create `testdata/flows/small.jsonl` with 3 lines of valid DROPPED flows (namespaces `default` → `production`, ports 8080/8443/9090):

```jsonl
{"flow":{"time":"2026-04-24T14:00:00Z","verdict":"DROPPED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":8080}}}}
{"flow":{"time":"2026-04-24T14:00:01Z","verdict":"DROPPED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":8443}}}}
{"flow":{"time":"2026-04-24T14:00:02Z","verdict":"DROPPED","traffic_direction":"EGRESS","source":{"labels":["k8s:app=api-server"],"namespace":"production"},"destination":{"labels":["k8s:app=db"],"namespace":"production"},"l4":{"TCP":{"destination_port":5432}}}}
```

Create `testdata/flows/with_non_dropped.jsonl` with 3 DROPPED + 2 FORWARDED:

```jsonl
{"flow":{"time":"2026-04-24T14:00:00Z","verdict":"DROPPED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":8080}}}}
{"flow":{"time":"2026-04-24T14:00:01Z","verdict":"FORWARDED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":80}}}}
{"flow":{"time":"2026-04-24T14:00:02Z","verdict":"DROPPED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":8443}}}}
{"flow":{"time":"2026-04-24T14:00:03Z","verdict":"FORWARDED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":443}}}}
{"flow":{"time":"2026-04-24T14:00:04Z","verdict":"DROPPED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":9090}}}}
```

Create `testdata/flows/malformed.jsonl` with 2 valid flanking a malformed line:

```jsonl
{"flow":{"time":"2026-04-24T14:00:00Z","verdict":"DROPPED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":8080}}}}
this is not json
{"flow":{"time":"2026-04-24T14:00:02Z","verdict":"DROPPED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":8443}}}}
```

Create `testdata/flows/empty.jsonl` as an empty file:

```bash
: > testdata/flows/empty.jsonl
```

- [ ] **Step 2: Write tests for the file source**

```go
// pkg/flowsource/file_test.go
package flowsource

import (
	"context"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func drain(t *testing.T, flows <-chan *flowpb.Flow) []*flowpb.Flow {
	t.Helper()
	var out []*flowpb.Flow
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case f, ok := <-flows:
			if !ok {
				return out
			}
			out = append(out, f)
		case <-deadline.C:
			t.Fatalf("timed out waiting for channel close")
			return out
		}
	}
}

func TestFileSourceHappyPath(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/small.jsonl", zap.NewNop())
	require.NoError(t, err)

	flows, lost, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	got := drain(t, flows)
	assert.Len(t, got, 3)

	// Lost events channel must also be closed.
	_, ok := <-lost
	assert.False(t, ok, "lost channel must be closed")

	assert.Equal(t, int64(3), src.Stats().FlowsEmitted)
	assert.Equal(t, int64(0), src.Stats().NonDroppedSkipped)
	assert.Equal(t, int64(0), src.Stats().Malformed)
}

func TestFileSourceFiltersNonDropped(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/with_non_dropped.jsonl", zap.NewNop())
	require.NoError(t, err)
	flows, _, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	got := drain(t, flows)
	assert.Len(t, got, 3)
	assert.Equal(t, int64(2), src.Stats().NonDroppedSkipped)
}

func TestFileSourceSkipsMalformed(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/malformed.jsonl", zap.NewNop())
	require.NoError(t, err)
	flows, _, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	got := drain(t, flows)
	assert.Len(t, got, 2)
	assert.Equal(t, int64(1), src.Stats().Malformed)
}

func TestFileSourceEmptyFile(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/empty.jsonl", zap.NewNop())
	require.NoError(t, err)
	flows, _, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	got := drain(t, flows)
	assert.Empty(t, got)
}

func TestFileSourceMissingFile(t *testing.T) {
	_, err := NewFileSource("/nonexistent/file.jsonl", zap.NewNop())
	require.Error(t, err)
}

func TestFileSourceContextCancellation(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/small.jsonl", zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	flows, _, err := src.StreamDroppedFlows(ctx, nil, false)
	require.NoError(t, err)

	// Drain must terminate cleanly.
	_ = drain(t, flows)
}
```

- [ ] **Step 3: Run tests — must fail to build**

Run: `go test ./pkg/flowsource/...`
Expected: `undefined: NewFileSource`.

- [ ] **Step 4: Implement FileSource**

```go
// pkg/flowsource/file.go
package flowsource

import (
	"bufio"
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

const defaultScannerBufferBytes = 10 * 1024 * 1024 // 10 MiB, Cilium flows with many labels can exceed 64 KiB

// FileSource streams DROPPED flows from a Hubble jsonpb dump. One flow per line,
// non-DROPPED verdicts are skipped with a counter, malformed lines are logged
// and skipped.
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

// NewFileSource opens the file for stat checks and returns a source. The file
// is re-opened for streaming inside StreamDroppedFlows.
func NewFileSource(path string, logger *zap.Logger) (*FileSource, error) {
	if path != "-" {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("opening replay file %s: %w", path, err)
		}
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
// signal). Both channels are closed when the file is fully consumed or ctx is
// canceled.
func (s *FileSource) StreamDroppedFlows(ctx context.Context, _ []string, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
	r, cleanup, err := s.openReader()
	if err != nil {
		return nil, nil, err
	}

	flowCh := make(chan *flowpb.Flow, 64)
	lostCh := make(chan *flowpb.LostEvent)
	close(lostCh) // file source has no lost-event notion

	s.logger.Info("replay starting",
		zap.String("file", s.path),
	)

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
		// Gzip support is added in the next task. Fail fast for now.
		f.Close()
		return nil, nil, fmt.Errorf("gzip not yet supported; decompress first")
	}
	return f, func() { f.Close() }, nil
}
```

- [ ] **Step 5: Run tests — must pass**

Run: `go test ./pkg/flowsource/... -v`
Expected: 6 file-source tests pass.

- [ ] **Step 6: Commit**

```bash
git add pkg/flowsource/file.go pkg/flowsource/file_test.go testdata/flows/
git commit -m "feat(flowsource): jsonpb file source with DROPPED filter and error counters"
```

---

### Task 9: Gzip support

**Files:**
- Modify: `pkg/flowsource/file.go`
- Modify: `pkg/flowsource/file_test.go`
- Create: `testdata/flows/small.jsonl.gz` (generated)

- [ ] **Step 1: Write a test for gzip input**

Add to `pkg/flowsource/file_test.go`:

```go
func TestFileSourceGzip(t *testing.T) {
	src, err := NewFileSource("../../testdata/flows/small.jsonl.gz", zap.NewNop())
	require.NoError(t, err)

	flows, _, err := src.StreamDroppedFlows(context.Background(), nil, false)
	require.NoError(t, err)

	got := drain(t, flows)
	assert.Len(t, got, 3)
}
```

- [ ] **Step 2: Create the gzipped fixture**

Run:

```bash
gzip -k testdata/flows/small.jsonl
ls testdata/flows/small.jsonl.gz
```

Expected: file exists.

- [ ] **Step 3: Run test — must fail**

Run: `go test ./pkg/flowsource/... -run Gzip`
Expected: `gzip not yet supported`.

- [ ] **Step 4: Implement gzip branch**

Replace `openReader` in `pkg/flowsource/file.go`:

```go
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
```

Add the import:

```go
	"compress/gzip"
```

- [ ] **Step 5: Run all file-source tests**

Run: `go test ./pkg/flowsource/... -v`
Expected: all pass including gzip.

- [ ] **Step 6: Commit**

```bash
git add pkg/flowsource/file.go pkg/flowsource/file_test.go testdata/flows/small.jsonl.gz
git commit -m "feat(flowsource): transparent gzip decompression for .gz replay files"
```

---

## Phase 5 — Dry-run with unified diff

### Task 10: YAML unified diff

**Files:**
- Create: `pkg/diff/yaml.go`
- Test: `pkg/diff/yaml_test.go`

- [ ] **Step 1: Write tests**

```go
// pkg/diff/yaml_test.go
package diff

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestYAMLDiffIdentical(t *testing.T) {
	d, err := UnifiedYAML("a.yaml", "b.yaml", []byte("key: value\n"), []byte("key: value\n"), false)
	assert.NoError(t, err)
	assert.Empty(t, d)
}

func TestYAMLDiffShowsAddition(t *testing.T) {
	old := []byte("foo: 1\n")
	new := []byte("foo: 1\nbar: 2\n")
	d, err := UnifiedYAML("old", "new", old, new, false)
	assert.NoError(t, err)
	assert.Contains(t, d, "+bar: 2")
}

func TestYAMLDiffShowsDeletion(t *testing.T) {
	old := []byte("foo: 1\nbar: 2\n")
	new := []byte("foo: 1\n")
	d, err := UnifiedYAML("old", "new", old, new, false)
	assert.NoError(t, err)
	assert.Contains(t, d, "-bar: 2")
}

func TestYAMLDiffHeaders(t *testing.T) {
	d, err := UnifiedYAML("a.yaml", "b.yaml", []byte("x: 1\n"), []byte("x: 2\n"), false)
	assert.NoError(t, err)
	assert.Contains(t, d, "--- a.yaml")
	assert.Contains(t, d, "+++ b.yaml")
}
```

- [ ] **Step 2: Run tests — must fail to build**

Run: `go test ./pkg/diff/...`
Expected: `undefined: UnifiedYAML`.

- [ ] **Step 3: Add dependency and implement**

Run:

```bash
go get github.com/pmezard/go-difflib/difflib
```

Create `pkg/diff/yaml.go`:

```go
// Package diff renders unified diffs for YAML documents. It is used by the
// dry-run mode of cpg generate and cpg replay to preview what would change
// on disk without writing.
package diff

import (
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// UnifiedYAML returns a unified diff between a and b, labeled with aName and
// bName. When color is true, lines starting with '+' are wrapped in green ANSI,
// '-' in red. Returns the empty string when a and b are identical.
func UnifiedYAML(aName, bName string, a, b []byte, color bool) (string, error) {
	if string(a) == string(b) {
		return "", nil
	}
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(a)),
		B:        difflib.SplitLines(string(b)),
		FromFile: aName,
		ToFile:   bName,
		Context:  3,
	}
	out, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return "", err
	}
	if !color {
		return out, nil
	}
	return colorize(out), nil
}

func colorize(s string) string {
	const (
		red   = "\x1b[31m"
		green = "\x1b[32m"
		reset = "\x1b[0m"
	)
	var b strings.Builder
	b.Grow(len(s))
	for _, line := range strings.SplitAfter(s, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			b.WriteString(line)
		case strings.HasPrefix(line, "+"):
			b.WriteString(green)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "-"):
			b.WriteString(red)
			b.WriteString(line)
			b.WriteString(reset)
		default:
			b.WriteString(line)
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests — must pass**

Run: `go test ./pkg/diff/... -v`
Expected: all 4 pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/diff/ go.mod go.sum
git commit -m "feat(diff): unified YAML diff with optional ANSI colors"
```

---

### Task 11: policyWriter dry-run branch

**Files:**
- Modify: `pkg/hubble/pipeline.go` (PipelineConfig fields)
- Modify: `pkg/hubble/writer.go` (policyWriter.handle branches on dry-run)
- Test: `pkg/hubble/writer_dryrun_test.go`

- [ ] **Step 1: Extend PipelineConfig**

In `pkg/hubble/pipeline.go`, add the following fields to `PipelineConfig`:

```go
	// DryRun disables filesystem writes for both policies and evidence. All
	// upstream stages still run; the writer logs "would write" and optionally
	// prints a unified diff.
	DryRun bool
	// DryRunDiff enables the unified diff output in dry-run mode.
	DryRunDiff bool
	// DryRunColor enables ANSI colors on the diff (tty detection is the
	// caller's responsibility).
	DryRunColor bool
```

- [ ] **Step 2: Write a test for dry-run behavior**

```go
// pkg/hubble/writer_dryrun_test.go
package hubble

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/SoulKyu/cpg/pkg/output"
	"github.com/SoulKyu/cpg/pkg/policy"
)

func simplePolicy(name, namespace string) *ciliumv2.CiliumNetworkPolicy {
	return &ciliumv2.CiliumNetworkPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: "cilium.io/v2", Kind: "CiliumNetworkPolicy"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       &api.Rule{},
	}
}

func TestDryRunDoesNotWriteToDisk(t *testing.T) {
	tmp := t.TempDir()
	logger := zap.NewNop()

	stats := &SessionStats{}
	pw := newPolicyWriter(output.NewWriter(tmp, logger), nil, stats, logger)
	pw.dryRun = true
	pw.dryRunDiff = false

	pe := policy.PolicyEvent{
		Namespace: "prod", Workload: "api", Policy: simplePolicy("cpg-api", "prod"),
	}
	pw.handle(pe)

	// No file should exist
	_, err := os.Stat(filepath.Join(tmp, "prod", "api.yaml"))
	assert.True(t, os.IsNotExist(err), "no file must be written in dry-run")

	assert.Equal(t, uint64(1), stats.PoliciesWouldWrite)
	assert.Equal(t, uint64(0), stats.PoliciesWritten)
}

func TestDryRunEmitsDiffWhenExistingChanges(t *testing.T) {
	tmp := t.TempDir()

	// Pre-populate an existing file with different content
	existingPath := filepath.Join(tmp, "prod", "api.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(existingPath), 0o755))
	require.NoError(t, os.WriteFile(existingPath, []byte("apiVersion: cilium.io/v2\nkind: CiliumNetworkPolicy\nmetadata:\n  name: cpg-api\n  namespace: prod\nspec:\n  endpointSelector: {}\n  ingress:\n  - fromEndpoints: []\n"), 0o644))

	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	stats := &SessionStats{}
	pw := newPolicyWriter(output.NewWriter(tmp, logger), nil, stats, logger)
	pw.dryRun = true
	pw.dryRunDiff = true
	pw.diffOut = new(bytes.Buffer) // inject sink for test

	pe := policy.PolicyEvent{
		Namespace: "prod", Workload: "api", Policy: simplePolicy("cpg-api", "prod"),
	}
	pw.handle(pe)

	// Existing file untouched
	data, err := os.ReadFile(existingPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "ingress")

	// Diff printed to injected writer
	assert.Contains(t, pw.diffOut.(*bytes.Buffer).String(), "--- ")
	assert.Contains(t, pw.diffOut.(*bytes.Buffer).String(), "+++ ")

	// Log line present
	entries := logs.FilterMessage("would write policy").All()
	assert.Len(t, entries, 1)
}
```

- [ ] **Step 3: Run tests — must fail to build**

Run: `go test ./pkg/hubble/... -run DryRun`
Expected: build error (`pw.dryRun` undefined, `PoliciesWouldWrite` undefined).

- [ ] **Step 4: Update SessionStats**

In `pkg/hubble/pipeline.go`, extend `SessionStats`:

```go
type SessionStats struct {
	StartTime         time.Time
	FlowsSeen         uint64
	PoliciesWritten   uint64
	PoliciesSkipped   uint64
	PoliciesWouldWrite uint64 // dry-run counter
	PoliciesWouldSkip  uint64 // dry-run counter
	LostEvents        uint64
	OutputDir         string
}
```

And the Log method:

```go
func (s *SessionStats) Log(logger *zap.Logger) {
	logger.Info("session summary",
		zap.Duration("duration", time.Since(s.StartTime)),
		zap.Uint64("flows_seen", s.FlowsSeen),
		zap.Uint64("policies_written", s.PoliciesWritten),
		zap.Uint64("policies_skipped", s.PoliciesSkipped),
		zap.Uint64("policies_would_write", s.PoliciesWouldWrite),
		zap.Uint64("policies_would_skip", s.PoliciesWouldSkip),
		zap.Uint64("lost_events", s.LostEvents),
		zap.String("output_dir", s.OutputDir),
	)
}
```

- [ ] **Step 5: Extend policyWriter**

In `pkg/hubble/writer.go`, add fields and dry-run handling:

```go
type policyWriter struct {
	writer          *output.Writer
	clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy
	written         map[string]*ciliumv2.CiliumNetworkPolicy
	stats           *SessionStats
	logger          *zap.Logger

	// Dry-run mode
	dryRun     bool
	dryRunDiff bool
	dryRunColor bool
	diffOut    io.Writer // test injection; defaults to os.Stdout when nil
}

func newPolicyWriter(w *output.Writer, clusterPolicies map[string]*ciliumv2.CiliumNetworkPolicy, stats *SessionStats, logger *zap.Logger) *policyWriter {
	return &policyWriter{
		writer:          w,
		clusterPolicies: clusterPolicies,
		written:         make(map[string]*ciliumv2.CiliumNetworkPolicy),
		stats:           stats,
		logger:          logger,
	}
}
```

Replace `handle` to branch on dryRun:

```go
func (w *policyWriter) handle(pe policy.PolicyEvent) {
	if w.skipForClusterMatch(pe) {
		w.bumpSkip()
		return
	}
	dedupKey := fmt.Sprintf("%s/%s", pe.Namespace, pe.Workload)
	if w.skipForCrossFlushMatch(pe, dedupKey) {
		w.bumpSkip()
		return
	}
	if w.dryRun {
		w.dryRunEmit(pe)
		w.stats.PoliciesWouldWrite++
		w.written[dedupKey] = pe.Policy
		return
	}
	if err := w.writer.Write(pe); err != nil {
		w.logger.Error("failed to write policy",
			zap.String("namespace", pe.Namespace),
			zap.String("workload", pe.Workload),
			zap.Error(err),
		)
		return
	}
	w.stats.PoliciesWritten++
	w.written[dedupKey] = pe.Policy
}

func (w *policyWriter) bumpSkip() {
	if w.dryRun {
		w.stats.PoliciesWouldSkip++
	} else {
		w.stats.PoliciesSkipped++
	}
}

func (w *policyWriter) dryRunEmit(pe policy.PolicyEvent) {
	w.logger.Info("would write policy",
		zap.String("namespace", pe.Namespace),
		zap.String("workload", pe.Workload),
	)
	if !w.dryRunDiff {
		return
	}

	rendered, err := sigyaml.Marshal(pe.Policy)
	if err != nil {
		w.logger.Warn("dry-run render failed", zap.Error(err))
		return
	}

	existing, err := w.writer.ReadExisting(pe.Namespace, pe.Workload)
	if err != nil {
		existing = nil
	}

	target := filepath.Join(w.writer.OutputDir(), pe.Namespace, pe.Workload+".yaml")
	d, err := diff.UnifiedYAML(target, target+" (in memory)", existing, rendered, w.dryRunColor)
	if err != nil {
		w.logger.Warn("diff failed", zap.Error(err))
		return
	}
	if d == "" {
		return
	}
	out := w.diffOut
	if out == nil {
		out = os.Stdout
	}
	io.WriteString(out, d)
}
```

Add imports to `pkg/hubble/writer.go`:

```go
	"io"
	"os"
	"path/filepath"

	"github.com/SoulKyu/cpg/pkg/diff"
	sigyaml "sigs.k8s.io/yaml"
```

- [ ] **Step 6: Add ReadExisting / OutputDir to pkg/output.Writer**

Open `pkg/output/writer.go`. Expose two helpers (add near the end):

```go
// OutputDir returns the root directory this writer targets.
func (w *Writer) OutputDir() string {
	return w.dir
}

// ReadExisting returns the raw YAML bytes of the file on disk for the given
// namespace/workload, or nil if no such file exists. Errors other than "not
// exist" are returned as-is.
func (w *Writer) ReadExisting(namespace, workload string) ([]byte, error) {
	path := filepath.Join(w.dir, namespace, workload+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}
```

Inspect existing imports in `pkg/output/writer.go`; add `"errors"`, `"io/fs"`, `"os"`, `"path/filepath"` if not already there.

- [ ] **Step 7: Run dry-run tests**

Run: `go test ./pkg/hubble/... -run DryRun -v`
Expected: both tests pass.

- [ ] **Step 8: Commit**

```bash
git add pkg/hubble/ pkg/output/
git commit -m "feat(hubble): dry-run mode in policyWriter with YAML unified diff"
```

---

## Phase 6 — Evidence wiring in the pipeline

### Task 12: Evidence writer goroutine

**Files:**
- Create: `pkg/hubble/evidence_writer.go`
- Modify: `pkg/hubble/pipeline.go` (fan-out PolicyEvent, start evidenceWriter)

- [ ] **Step 1: Write a test asserting evidence is written alongside policy**

Add `pkg/hubble/evidence_writer_test.go`:

```go
package hubble

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/policy"
)

func TestEvidenceWriterPersistsAttribution(t *testing.T) {
	tmp := t.TempDir()
	sessionID := "test-session-1"
	session := evidence.SessionInfo{
		ID: sessionID, StartedAt: time.Now(), EndedAt: time.Now(),
		CPGVersion: "test", Source: evidence.SourceInfo{Type: "replay", File: "x.jsonl"},
	}

	ew := newEvidenceWriter(tmp, "hash0", evidence.MergeCaps{MaxSamples: 10, MaxSessions: 10}, session, zap.NewNop())

	now := timestamppb.New(time.Unix(1700000000, 0))
	flow := &flowpb.Flow{
		Time:             now,
		TrafficDirection: flowpb.TrafficDirection_INGRESS,
		Source:           &flowpb.Endpoint{Labels: []string{"k8s:app=client"}, Namespace: "default", PodName: "client-abc"},
		Destination:      &flowpb.Endpoint{Labels: []string{"k8s:app=api"}, Namespace: "prod", PodName: "api-xyz"},
		Verdict:          flowpb.Verdict_DROPPED,
	}

	pe := policy.PolicyEvent{
		Namespace: "prod", Workload: "api",
		Policy: &ciliumv2.CiliumNetworkPolicy{
			TypeMeta:   metav1.TypeMeta{APIVersion: "cilium.io/v2", Kind: "CiliumNetworkPolicy"},
			ObjectMeta: metav1.ObjectMeta{Name: "cpg-api", Namespace: "prod"},
			Spec:       &api.Rule{},
		},
		Attribution: []policy.RuleAttribution{{
			Key: policy.RuleKey{
				Direction: "ingress",
				Peer:      policy.Peer{Type: policy.PeerEndpoint, Labels: map[string]string{"app": "client"}},
				Port:      "8080", Protocol: "TCP",
			},
			FlowCount: 1,
			FirstSeen: flow.GetTime().AsTime(),
			LastSeen:  flow.GetTime().AsTime(),
			Samples:   []*flowpb.Flow{flow},
		}},
	}

	ew.handle(pe)
	ew.finalize(3, 0)

	path := filepath.Join(tmp, "hash0", "prod", "api.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var pev evidence.PolicyEvidence
	require.NoError(t, json.Unmarshal(data, &pev))
	assert.Len(t, pev.Sessions, 1)
	assert.Equal(t, sessionID, pev.Sessions[0].ID)
	require.Len(t, pev.Rules, 1)
	assert.Equal(t, "ingress:ep:app=client:TCP:8080", pev.Rules[0].Key)
	assert.Len(t, pev.Rules[0].Samples, 1)
	assert.Equal(t, "default", pev.Rules[0].Samples[0].Src.Namespace)
}
```

- [ ] **Step 2: Run test — must fail to build**

Run: `go test ./pkg/hubble/... -run Evidence`
Expected: `undefined: newEvidenceWriter`.

- [ ] **Step 3: Implement evidenceWriter**

```go
// pkg/hubble/evidence_writer.go
package hubble

import (
	flowpb "github.com/cilium/cilium/api/v1/flow"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/labels"
	"github.com/SoulKyu/cpg/pkg/policy"
)

// evidenceWriter persists per-rule evidence alongside policy writes. It does
// nothing when evidence capture is disabled for the pipeline run.
type evidenceWriter struct {
	writer  *evidence.Writer
	session evidence.SessionInfo
	logger  *zap.Logger
	seen    map[string]struct{} // workloads already written at least once this session
}

func newEvidenceWriter(evidenceDir, outputHash string, caps evidence.MergeCaps, session evidence.SessionInfo, logger *zap.Logger) *evidenceWriter {
	return &evidenceWriter{
		writer:  evidence.NewWriter(evidenceDir, outputHash, caps),
		session: session,
		logger:  logger,
		seen:    make(map[string]struct{}),
	}
}

// handle converts a PolicyEvent's Attribution to evidence.RuleEvidence and
// persists it. Attribution-less events are skipped.
func (ew *evidenceWriter) handle(pe policy.PolicyEvent) {
	if len(pe.Attribution) == 0 {
		return
	}
	ref := evidence.PolicyRef{
		Name:      pe.Policy.Name,
		Namespace: pe.Namespace,
		Workload:  pe.Workload,
	}
	rules := make([]evidence.RuleEvidence, 0, len(pe.Attribution))
	for _, a := range pe.Attribution {
		rules = append(rules, ew.convert(a))
	}
	if err := ew.writer.Write(ref, ew.session, rules); err != nil {
		ew.logger.Warn("writing evidence",
			zap.String("namespace", pe.Namespace),
			zap.String("workload", pe.Workload),
			zap.Error(err),
		)
		return
	}
	ew.seen[pe.Namespace+"/"+pe.Workload] = struct{}{}
}

// finalize updates the session's flow counters before the pipeline exits.
// It is called once per run.
func (ew *evidenceWriter) finalize(flowsIngested, flowsUnhandled int64) {
	ew.session.FlowsIngested = flowsIngested
	ew.session.FlowsUnhandled = flowsUnhandled
	// Rewrite session info on each seen workload so final counters are
	// recorded. We do this by issuing a zero-rule merge, which only updates
	// the session entry.
	for key := range ew.seen {
		ns, wl := splitKey(key)
		ref := evidence.PolicyRef{Namespace: ns, Workload: wl}
		if err := ew.writer.Write(ref, ew.session, nil); err != nil {
			ew.logger.Warn("updating evidence session counters",
				zap.String("namespace", ns),
				zap.String("workload", wl),
				zap.Error(err),
			)
		}
	}
}

func splitKey(key string) (namespace, workload string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

func (ew *evidenceWriter) convert(a policy.RuleAttribution) evidence.RuleEvidence {
	re := evidence.RuleEvidence{
		Key:                  a.Key.String(),
		Direction:            a.Key.Direction,
		Peer:                 convertPeer(a.Key.Peer),
		Port:                 a.Key.Port,
		Protocol:             a.Key.Protocol,
		FlowCount:            a.FlowCount,
		FirstSeen:            a.FirstSeen,
		LastSeen:             a.LastSeen,
		ContributingSessions: []string{ew.session.ID},
	}
	for _, f := range a.Samples {
		re.Samples = append(re.Samples, convertSample(f, a.Key))
	}
	return re
}

func convertPeer(p policy.Peer) evidence.PeerRef {
	return evidence.PeerRef{
		Type:   string(p.Type),
		Labels: p.Labels,
		CIDR:   p.CIDR,
		Entity: p.Entity,
	}
}

func convertSample(f *flowpb.Flow, key policy.RuleKey) evidence.FlowSample {
	return evidence.FlowSample{
		Time:       flowTime(f),
		Src:        endpointFromFlow(f.Source),
		Dst:        endpointFromFlow(f.Destination),
		Port:       portFromKey(key),
		Protocol:   key.Protocol,
		Verdict:    f.Verdict.String(),
		DropReason: f.DropReasonDesc.String(),
	}
}

func endpointFromFlow(ep *flowpb.Endpoint) evidence.FlowEndpoint {
	if ep == nil {
		return evidence.FlowEndpoint{}
	}
	return evidence.FlowEndpoint{
		Namespace: ep.Namespace,
		Workload:  labels.WorkloadName(ep.Labels),
		Pod:       ep.PodName,
	}
}

func portFromKey(k policy.RuleKey) uint32 {
	var p uint32
	for _, c := range k.Port {
		if c < '0' || c > '9' {
			return 0
		}
		p = p*10 + uint32(c-'0')
	}
	return p
}

// flowTime mirrors the helper in pkg/policy/builder.go to avoid a cross-package
// reference just for this.
func flowTime(f *flowpb.Flow) (t timeT) {
	if f == nil || f.Time == nil {
		return timeT{}
	}
	return timeT{T: f.Time.AsTime()}
}
```

Wait — `flowTime` exists already in `pkg/policy`. Import it instead:

Remove the local helpers at the bottom of `evidence_writer.go` and import `policy.FlowTime` (you'll need to export the existing function). Go to `pkg/policy/builder.go`, rename `flowTime` to `FlowTime` (exported) and update its two local callers.

Replace the sample-conversion line with:

```go
Time: policy.FlowTime(f),
```

- [ ] **Step 4: Export policy.FlowTime**

Edit `pkg/policy/builder.go`. Rename `flowTime` → `FlowTime` and update call sites within the same file.

- [ ] **Step 5: Run evidence writer test**

Run: `go test ./pkg/hubble/... -run Evidence -v`
Expected: test passes.

- [ ] **Step 6: Fan out PolicyEvent in the pipeline**

In `pkg/hubble/pipeline.go`, update `RunPipelineWithSource` so it forks the `policies` channel to both the policy writer and evidence writer:

```go
policies := make(chan policy.PolicyEvent, 64)

// Fan out: each downstream consumer has its own channel. The aggregator
// emits to 'policies'; a tee goroutine dispatches each event to both
// consumers so slow I/O on one does not stall the other.
policyCh := make(chan policy.PolicyEvent, 64)
evidenceCh := make(chan policy.PolicyEvent, 64)

g, gctx := errgroup.WithContext(ctx)

g.Go(func() error {
	return agg.Run(gctx, flows, policies)
})

g.Go(func() error {
	defer close(policyCh)
	defer close(evidenceCh)
	for pe := range policies {
		policyCh <- pe
		evidenceCh <- pe
	}
	return nil
})
```

Add the policy-writer goroutine consuming `policyCh` (existing body with `pw := newPolicyWriter(...)` now reads `policyCh`). Wire `dryRun`, `dryRunDiff`, `dryRunColor` from `cfg`:

```go
g.Go(func() error {
	pw := newPolicyWriter(writer, cfg.ClusterPolicies, stats, cfg.Logger)
	pw.dryRun = cfg.DryRun
	pw.dryRunDiff = cfg.DryRunDiff
	pw.dryRunColor = cfg.DryRunColor
	for pe := range policyCh {
		pw.handle(pe)
	}
	return nil
})
```

Add the evidence-writer goroutine:

```go
var ew *evidenceWriter
if cfg.EvidenceEnabled {
	session := evidence.SessionInfo{
		ID:         cfg.SessionID,
		StartedAt:  time.Now(),
		CPGVersion: cfg.CPGVersion,
		Source:     cfg.SessionSource,
	}
	ew = newEvidenceWriter(cfg.EvidenceDir, cfg.OutputHash, cfg.EvidenceCaps, session, cfg.Logger)
}

g.Go(func() error {
	for pe := range evidenceCh {
		if ew != nil && !cfg.DryRun {
			ew.handle(pe)
		}
	}
	return nil
})
```

At the end of `RunPipelineWithSource`, after `g.Wait()`:

```go
// Close the evidence session with final counters.
if ew != nil && !cfg.DryRun {
	ew.session.EndedAt = time.Now()
	ew.finalize(int64(stats.FlowsSeen), int64(stats.LostEvents))
}
```

Add these fields to `PipelineConfig`:

```go
// Evidence capture (see pkg/evidence)
EvidenceEnabled bool
EvidenceDir     string
OutputHash      string
EvidenceCaps    evidence.MergeCaps
SessionID       string
SessionSource   evidence.SourceInfo
CPGVersion      string
```

Import `github.com/SoulKyu/cpg/pkg/evidence` at the top.

- [ ] **Step 7: Run full test suite**

Run: `go test ./... -v`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add pkg/hubble/
git commit -m "feat(hubble): evidence writer goroutine fanned out from policy channel"
```

---

## Phase 7 — CLI wiring

### Task 13: Shared flag helper

**Files:**
- Create: `cmd/cpg/commonflags.go`
- Modify: `cmd/cpg/generate.go` (consume the helper)

- [ ] **Step 1: Define the common-flags helper**

```go
// cmd/cpg/commonflags.go
package main

import (
	"time"

	"github.com/spf13/cobra"
)

// commonFlags hold the flags shared by `generate` and `replay`.
type commonFlags struct {
	namespaces    []string
	allNamespaces bool
	outputDir     string
	flushInterval time.Duration
	clusterDedup  bool

	dryRun       bool
	dryRunNoDiff bool

	noEvidence       bool
	evidenceDir      string
	evidenceSamples  int
	evidenceSessions int
}

// addCommonFlags wires the shared flags onto the given command.
func addCommonFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringSliceP("namespace", "n", nil, "namespace filter (repeatable)")
	f.BoolP("all-namespaces", "A", false, "observe all namespaces")

	f.StringP("output-dir", "o", "./policies", "output directory for generated policies")

	f.Duration("flush-interval", 5*time.Second, "aggregation flush interval")
	f.Bool("cluster-dedup", false, "skip policies that already exist in cluster (requires RBAC for CiliumNetworkPolicy list)")

	f.Bool("dry-run", false, "preview changes without writing to disk")
	f.Bool("no-diff", false, "with --dry-run, skip the unified diff output")

	f.Bool("no-evidence", false, "disable per-rule evidence capture")
	f.String("evidence-dir", "", "override evidence storage path (default: XDG_CACHE_HOME/cpg/evidence)")
	f.Int("evidence-samples", 10, "samples kept per rule in evidence files")
	f.Int("evidence-sessions", 10, "sessions kept per policy in evidence files")
}

func parseCommonFlags(cmd *cobra.Command) commonFlags {
	f := cmd.Flags()
	out := commonFlags{}
	out.namespaces, _ = f.GetStringSlice("namespace")
	out.allNamespaces, _ = f.GetBool("all-namespaces")
	out.outputDir, _ = f.GetString("output-dir")
	out.flushInterval, _ = f.GetDuration("flush-interval")
	out.clusterDedup, _ = f.GetBool("cluster-dedup")
	out.dryRun, _ = f.GetBool("dry-run")
	out.dryRunNoDiff, _ = f.GetBool("no-diff")
	out.noEvidence, _ = f.GetBool("no-evidence")
	out.evidenceDir, _ = f.GetString("evidence-dir")
	out.evidenceSamples, _ = f.GetInt("evidence-samples")
	out.evidenceSessions, _ = f.GetInt("evidence-sessions")
	return out
}
```

- [ ] **Step 2: Refactor generate.go to use the helper**

Rewrite `newGenerateCmd` so it calls `addCommonFlags(cmd)` in place of every flag that now lives in the helper. Keep the existing generate-specific flags (`--server`, `--tls`, `--timeout`) as-is.

Extend `generateFlags` to embed `commonFlags`:

```go
type generateFlags struct {
	commonFlags
	server     string
	tlsEnabled bool
	timeout    time.Duration
}

func parseGenerateFlags(cmd *cobra.Command) generateFlags {
	return generateFlags{
		commonFlags: parseCommonFlags(cmd),
		server:      mustString(cmd, "server"),
		tlsEnabled:  mustBool(cmd, "tls"),
		timeout:     mustDuration(cmd, "timeout"),
	}
}

func mustString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func mustBool(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}

func mustDuration(cmd *cobra.Command, name string) time.Duration {
	v, _ := cmd.Flags().GetDuration(name)
	return v
}
```

Update `runGenerate` to use `f.outputDir`, `f.flushInterval`, etc., instead of the scattered local variables previously read directly off `cmd.Flags()`.

Remove the now-duplicated flag definitions from `newGenerateCmd`.

- [ ] **Step 3: Update existing generate tests**

Open `cmd/cpg/generate_test.go`. The existing tests should keep passing because flag semantics are unchanged. If any assertions reference a flag path that moved, update them.

- [ ] **Step 4: Run all tests**

Run: `go test ./cmd/cpg/... -v`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add cmd/cpg/commonflags.go cmd/cpg/generate.go cmd/cpg/generate_test.go
git commit -m "refactor(cmd): factor common flags into helper for generate and replay"
```

---

### Task 14: `cpg replay` command

**Files:**
- Create: `cmd/cpg/replay.go`
- Modify: `cmd/cpg/main.go` (register replay command)
- Test: `cmd/cpg/replay_test.go`

- [ ] **Step 1: Write an integration test**

```go
// cmd/cpg/replay_test.go
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

func TestReplayCommandProducesPoliciesAndEvidence(t *testing.T) {
	outDir := t.TempDir()
	evDir := t.TempDir()

	cmd := newReplayCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{
		"../../testdata/flows/small.jsonl",
		"--output-dir", outDir,
		"--evidence-dir", evDir,
		"--flush-interval", "100ms",
	})

	// Wire the global logger for the test
	initLoggerForTesting(t)

	require.NoError(t, cmd.Execute())

	// Policies were written
	entries, err := os.ReadDir(filepath.Join(outDir, "production"))
	require.NoError(t, err)
	assert.NotEmpty(t, entries)

	// Evidence was written
	hash := evidence.HashOutputDir(outDir)
	evPath := filepath.Join(evDir, hash, "production", "api-server.json")
	data, err := os.ReadFile(evPath)
	require.NoError(t, err)

	var pev evidence.PolicyEvidence
	require.NoError(t, json.Unmarshal(data, &pev))
	assert.Equal(t, 1, pev.SchemaVersion)
	assert.NotEmpty(t, pev.Rules)
	assert.Len(t, pev.Sessions, 1)
	assert.Equal(t, "replay", pev.Sessions[0].Source.Type)
}
```

- [ ] **Step 2: Introduce `initLoggerForTesting` helper**

In `cmd/cpg/main.go`, wherever the global `logger` is initialized, extract a small helper:

```go
func initLoggerForTesting(t *testing.T) {
	t.Helper()
	logger = zap.NewNop()
}
```

(Place it behind a `_test.go` suffix so it isn't in production binary: create `cmd/cpg/testhelpers_test.go`.)

- [ ] **Step 3: Run test — must fail to build**

Run: `go test ./cmd/cpg/... -run Replay`
Expected: `undefined: newReplayCmd`.

- [ ] **Step 4: Implement replay command**

```go
// cmd/cpg/replay.go
package main

import (
	"fmt"
	"os"
	"os/signal"
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
`,
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

	evidenceDir := f.evidenceDir
	if evidenceDir == "" {
		evidenceDir, err = evidence.DefaultEvidenceDir()
		if err != nil {
			return fmt.Errorf("resolving evidence dir: %w", err)
		}
	}

	sessionSource := evidence.SourceInfo{Type: "replay"}
	if path != "-" {
		abs, err := absolutePath(path)
		if err == nil {
			sessionSource.File = abs
		} else {
			sessionSource.File = path
		}
	}

	cfg := hubble.PipelineConfig{
		Server:          "replay:" + path,
		Namespaces:      f.namespaces,
		AllNamespaces:   f.allNamespaces,
		OutputDir:       f.outputDir,
		FlushInterval:   f.flushInterval,
		Logger:          logger,
		DryRun:          f.dryRun,
		DryRunDiff:      !f.dryRunNoDiff,
		DryRunColor:     isTerminal(os.Stdout),
		EvidenceEnabled: !f.noEvidence,
		EvidenceDir:     evidenceDir,
		OutputHash:      evidence.HashOutputDir(f.outputDir),
		EvidenceCaps: evidence.MergeCaps{
			MaxSamples:  f.evidenceSamples,
			MaxSessions: f.evidenceSessions,
		},
		SessionID:     fmt.Sprintf("%s-%s", time.Now().UTC().Format(time.RFC3339), uuid.New().String()[:4]),
		SessionSource: sessionSource,
		CPGVersion:    version,
	}

	logger.Info("cpg replay configuration",
		zap.String("file", path),
		zap.Strings("namespaces", f.namespaces),
		zap.Bool("all-namespaces", f.allNamespaces),
		zap.String("output-dir", f.outputDir),
		zap.Bool("dry-run", f.dryRun),
		zap.Bool("evidence", !f.noEvidence),
	)

	return hubble.RunPipelineWithSource(ctx, cfg, source)
}

func absolutePath(p string) (string, error) {
	if p == "-" {
		return p, nil
	}
	return filepath.Abs(p)
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
```

Add these imports at the top:

```go
	"path/filepath"
```

Add helper in `cmd/cpg/main.go`:

```go
// version is the cpg version reported in session metadata. Injected at build
// time via -ldflags when available, otherwise "dev".
var version = "dev"
```

(If a version variable already exists elsewhere, skip this block.)

- [ ] **Step 5: Register the command in main.go**

Edit `cmd/cpg/main.go`:

```go
rootCmd.AddCommand(newGenerateCmd())
rootCmd.AddCommand(newReplayCmd()) // NEW
```

- [ ] **Step 6: Add google/uuid dependency**

Run:

```bash
go get github.com/google/uuid
```

- [ ] **Step 7: Run the test**

Run: `go test ./cmd/cpg/... -run Replay -v`
Expected: test passes, evidence + policies present on disk.

- [ ] **Step 8: Commit**

```bash
git add cmd/cpg/replay.go cmd/cpg/main.go cmd/cpg/testhelpers_test.go cmd/cpg/replay_test.go go.mod go.sum
git commit -m "feat(cli): cpg replay subcommand for offline jsonpb playback"
```

---

### Task 15: Wire dry-run + evidence into `cpg generate`

**Files:**
- Modify: `cmd/cpg/generate.go`
- Test: `cmd/cpg/dryrun_test.go`

- [ ] **Step 1: Write a dry-run integration test exercising generate via file replay**

Add `cmd/cpg/dryrun_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplayDryRunWritesNothing(t *testing.T) {
	outDir := t.TempDir()
	evDir := t.TempDir()

	initLoggerForTesting(t)

	cmd := newReplayCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{
		"../../testdata/flows/small.jsonl",
		"--output-dir", outDir,
		"--evidence-dir", evDir,
		"--flush-interval", "100ms",
		"--dry-run",
		"--no-diff",
	})

	require.NoError(t, cmd.Execute())

	// Output dir must still be empty
	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "no files must be written in dry-run")

	// Evidence dir must also be empty
	assert.NoFileExists(t, filepath.Join(evDir, "anyhash"))
}
```

- [ ] **Step 2: Run test — it should pass immediately**

Run: `go test ./cmd/cpg/... -run DryRun -v`
Expected: pass. (The dry-run plumbing added in Task 11 + wiring in Task 12 should already handle this.)

- [ ] **Step 3: Now do the same for `cpg generate --dry-run`**

The existing `generate` command currently does not thread the dry-run flags into PipelineConfig. Update `runGenerate` to mirror `runReplay`:

```go
cfg := hubble.PipelineConfig{
	Server:          server,
	TLSEnabled:      tlsEnabled,
	Timeout:         timeout,
	Namespaces:      namespaces,
	AllNamespaces:   allNamespaces,
	OutputDir:       outputDir,
	FlushInterval:   flushInterval,
	Logger:          logger,
	ClusterPolicies: clusterPolicies,

	DryRun:      f.dryRun,
	DryRunDiff:  !f.dryRunNoDiff,
	DryRunColor: isTerminal(os.Stdout),

	EvidenceEnabled: !f.noEvidence,
	EvidenceDir:     resolveEvidenceDir(f.evidenceDir),
	OutputHash:      evidence.HashOutputDir(outputDir),
	EvidenceCaps: evidence.MergeCaps{
		MaxSamples:  f.evidenceSamples,
		MaxSessions: f.evidenceSessions,
	},
	SessionID:     fmt.Sprintf("%s-%s", time.Now().UTC().Format(time.RFC3339), uuid.New().String()[:4]),
	SessionSource: evidence.SourceInfo{Type: "live", Server: server},
	CPGVersion:    version,
}
```

Add:

```go
func resolveEvidenceDir(override string) string {
	if override != "" {
		return override
	}
	dir, err := evidence.DefaultEvidenceDir()
	if err != nil {
		logger.Warn("falling back to in-repo .cpg-evidence", zap.Error(err))
		return ".cpg-evidence"
	}
	return dir
}
```

- [ ] **Step 4: Run full test suite**

Run: `go test ./... -v`
Expected: success.

- [ ] **Step 5: Commit**

```bash
git add cmd/cpg/generate.go cmd/cpg/dryrun_test.go
git commit -m "feat(cli): wire dry-run and evidence flags into generate subcommand"
```

---

## Phase 8 — Explain command

### Task 16: Target resolver + filters

**Files:**
- Create: `cmd/cpg/explain_target.go`
- Test: `cmd/cpg/explain_target_test.go`

- [ ] **Step 1: Write tests**

```go
// cmd/cpg/explain_target_test.go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTargetNamespaceWorkloadForm(t *testing.T) {
	tgt, err := resolveExplainTarget("production/api-server")
	require.NoError(t, err)
	assert.Equal(t, "production", tgt.Namespace)
	assert.Equal(t, "api-server", tgt.Workload)
}

func TestResolveTargetRejectsInvalidForm(t *testing.T) {
	_, err := resolveExplainTarget("just-one-segment")
	assert.Error(t, err)
}

func TestResolveTargetYAMLPath(t *testing.T) {
	tmp := t.TempDir()
	yamlPath := filepath.Join(tmp, "api-server.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("apiVersion: cilium.io/v2\nkind: CiliumNetworkPolicy\nmetadata:\n  name: cpg-api-server\n  namespace: production\n"), 0o644))

	tgt, err := resolveExplainTarget(yamlPath)
	require.NoError(t, err)
	assert.Equal(t, "production", tgt.Namespace)
	assert.Equal(t, "api-server", tgt.Workload)
}

func TestResolveTargetRejectsYAMLWithoutCPGPrefix(t *testing.T) {
	tmp := t.TempDir()
	yamlPath := filepath.Join(tmp, "other.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("apiVersion: cilium.io/v2\nkind: CiliumNetworkPolicy\nmetadata:\n  name: not-a-cpg-policy\n  namespace: production\n"), 0o644))

	_, err := resolveExplainTarget(yamlPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cpg-")
}
```

- [ ] **Step 2: Run test — must fail to build**

Run: `go test ./cmd/cpg/... -run Target`
Expected: undefined symbols.

- [ ] **Step 3: Implement the resolver**

```go
// cmd/cpg/explain_target.go
package main

import (
	"fmt"
	"os"
	"strings"

	sigyaml "sigs.k8s.io/yaml"
)

type explainTarget struct {
	Namespace string
	Workload  string
}

// resolveExplainTarget accepts "NAMESPACE/WORKLOAD" or a path to a YAML policy
// file and returns the target. YAML files must carry a `cpg-` prefix on the
// policy name — `cpg explain` only documents cpg-generated policies.
func resolveExplainTarget(arg string) (explainTarget, error) {
	if strings.HasSuffix(arg, ".yaml") || strings.HasSuffix(arg, ".yml") {
		return resolveFromYAML(arg)
	}
	parts := strings.SplitN(arg, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return explainTarget{}, fmt.Errorf("invalid target %q: expected NAMESPACE/WORKLOAD or a policy YAML path", arg)
	}
	return explainTarget{Namespace: parts[0], Workload: parts[1]}, nil
}

func resolveFromYAML(path string) (explainTarget, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return explainTarget{}, fmt.Errorf("reading %s: %w", path, err)
	}
	type meta struct {
		Metadata struct {
			Name      string `yaml:"name"`
			Namespace string `yaml:"namespace"`
		} `yaml:"metadata"`
	}
	var m meta
	if err := sigyaml.Unmarshal(data, &m); err != nil {
		return explainTarget{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if m.Metadata.Name == "" || m.Metadata.Namespace == "" {
		return explainTarget{}, fmt.Errorf("%s: missing metadata.name or metadata.namespace", path)
	}
	if !strings.HasPrefix(m.Metadata.Name, "cpg-") {
		return explainTarget{}, fmt.Errorf("%s: policy name %q does not start with 'cpg-' — explain is scoped to cpg-generated policies", path, m.Metadata.Name)
	}
	return explainTarget{
		Namespace: m.Metadata.Namespace,
		Workload:  strings.TrimPrefix(m.Metadata.Name, "cpg-"),
	}, nil
}
```

- [ ] **Step 4: Add filter plumbing**

```go
// cmd/cpg/explain_filter.go
package main

import (
	"net"
	"strings"
	"time"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

type explainFilter struct {
	Direction string
	Port      string
	PeerLabel struct {
		Key   string
		Value string
		Set   bool
	}
	PeerCIDR  *net.IPNet
	Since     time.Duration
	Now       time.Time
}

func (f explainFilter) match(r evidence.RuleEvidence) bool {
	if f.Direction != "" && r.Direction != f.Direction {
		return false
	}
	if f.Port != "" && r.Port != f.Port {
		return false
	}
	if f.PeerLabel.Set {
		if r.Peer.Type != "endpoint" {
			return false
		}
		if v, ok := r.Peer.Labels[f.PeerLabel.Key]; !ok || v != f.PeerLabel.Value {
			return false
		}
	}
	if f.PeerCIDR != nil {
		if r.Peer.Type != "cidr" {
			return false
		}
		ruleIP, ruleNet, err := net.ParseCIDR(r.Peer.CIDR)
		if err != nil || !f.PeerCIDR.Contains(ruleIP) {
			return false
		}
		fOnes, _ := f.PeerCIDR.Mask.Size()
		rOnes, _ := ruleNet.Mask.Size()
		if rOnes < fOnes {
			return false
		}
	}
	if f.Since > 0 && r.LastSeen.Before(f.Now.Add(-f.Since)) {
		return false
	}
	return true
}

func parsePeerLabel(s string) (key, value string, ok bool) {
	if s == "" {
		return "", "", false
	}
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
```

Write matching tests in `cmd/cpg/explain_filter_test.go`:

```go
package main

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

func TestFilterDirectionAndPort(t *testing.T) {
	rule := evidence.RuleEvidence{Direction: "ingress", Port: "8080"}
	f := explainFilter{Direction: "ingress", Port: "8080"}
	assert.True(t, f.match(rule))

	f.Port = "9090"
	assert.False(t, f.match(rule))
}

func TestFilterPeerLabel(t *testing.T) {
	rule := evidence.RuleEvidence{Peer: evidence.PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}}}
	f := explainFilter{}
	f.PeerLabel.Set = true
	f.PeerLabel.Key, f.PeerLabel.Value = "app", "x"
	assert.True(t, f.match(rule))

	f.PeerLabel.Value = "y"
	assert.False(t, f.match(rule))
}

func TestFilterPeerCIDRContainment(t *testing.T) {
	_, filterNet, _ := net.ParseCIDR("10.0.0.0/8")
	rule := evidence.RuleEvidence{Peer: evidence.PeerRef{Type: "cidr", CIDR: "10.0.1.0/24"}}
	f := explainFilter{PeerCIDR: filterNet}
	assert.True(t, f.match(rule))

	rule.Peer.CIDR = "192.168.0.0/16"
	assert.False(t, f.match(rule))

	rule.Peer.CIDR = "10.0.0.0/4" // broader than filter — should not match
	assert.False(t, f.match(rule))
}

func TestFilterSince(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	rule := evidence.RuleEvidence{LastSeen: now.Add(-5 * time.Minute)}
	f := explainFilter{Since: 10 * time.Minute, Now: now}
	assert.True(t, f.match(rule))

	f.Since = 1 * time.Minute
	assert.False(t, f.match(rule))
}
```

- [ ] **Step 5: Run tests — must pass**

Run: `go test ./cmd/cpg/... -run "Target|Filter" -v`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/cpg/explain_target.go cmd/cpg/explain_target_test.go cmd/cpg/explain_filter.go cmd/cpg/explain_filter_test.go
git commit -m "feat(cli): explain target resolver and filter matchers"
```

---

### Task 17: Explain renderers (text, JSON, YAML)

**Files:**
- Create: `cmd/cpg/explain_render.go`
- Test: `cmd/cpg/explain_render_test.go`

- [ ] **Step 1: Write tests for each renderer**

```go
// cmd/cpg/explain_render_test.go
package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

func sampleEvidence() evidence.PolicyEvidence {
	return evidence.PolicyEvidence{
		SchemaVersion: 1,
		Policy:        evidence.PolicyRef{Name: "cpg-api", Namespace: "prod", Workload: "api"},
		Sessions: []evidence.SessionInfo{{
			ID:        "s1",
			StartedAt: time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC),
			EndedAt:   time.Date(2026, 4, 24, 14, 15, 0, 0, time.UTC),
			Source:    evidence.SourceInfo{Type: "replay", File: "f.jsonl"},
		}},
		Rules: []evidence.RuleEvidence{{
			Key: "ingress:ep:app=x:TCP:8080", Direction: "ingress",
			Peer: evidence.PeerRef{Type: "endpoint", Labels: map[string]string{"app": "x"}},
			Port: "8080", Protocol: "TCP",
			FlowCount: 3, FirstSeen: time.Date(2026, 4, 24, 14, 0, 1, 0, time.UTC), LastSeen: time.Date(2026, 4, 24, 14, 2, 5, 0, time.UTC),
			Samples: []evidence.FlowSample{{
				Time: time.Date(2026, 4, 24, 14, 0, 1, 0, time.UTC),
				Src:  evidence.FlowEndpoint{Namespace: "default", Workload: "client"},
				Dst:  evidence.FlowEndpoint{Namespace: "prod", Workload: "api"},
				Port: 8080, Protocol: "TCP", Verdict: "DROPPED",
			}},
		}},
	}
}

func TestRenderTextShowsRuleMeta(t *testing.T) {
	buf := new(bytes.Buffer)
	require.NoError(t, renderText(buf, sampleEvidence(), sampleEvidence().Rules, 10, false))

	out := buf.String()
	assert.Contains(t, out, "Policy: cpg-api (prod)")
	assert.Contains(t, out, "Ingress rule")
	assert.Contains(t, out, "app=x")
	assert.Contains(t, out, "8080/TCP")
	assert.Contains(t, out, "Flow count:  3")
	assert.Contains(t, out, "default/client")
}

func TestRenderJSON(t *testing.T) {
	buf := new(bytes.Buffer)
	require.NoError(t, renderJSON(buf, sampleEvidence(), sampleEvidence().Rules))

	var got struct {
		Policy       evidence.PolicyRef       `json:"policy"`
		Sessions     []evidence.SessionInfo   `json:"sessions"`
		MatchedRules []evidence.RuleEvidence  `json:"matched_rules"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, "cpg-api", got.Policy.Name)
	assert.Len(t, got.MatchedRules, 1)
}

func TestRenderYAML(t *testing.T) {
	buf := new(bytes.Buffer)
	require.NoError(t, renderYAML(buf, sampleEvidence(), sampleEvidence().Rules))
	assert.Contains(t, buf.String(), "policy:")
	assert.Contains(t, buf.String(), "matched_rules:")
}

func TestRenderTextEmptyMatchListsAvailable(t *testing.T) {
	buf := new(bytes.Buffer)
	err := renderText(buf, sampleEvidence(), nil, 10, false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No rules matched")
	assert.Contains(t, buf.String(), "Available rules:")
	assert.Contains(t, buf.String(), "app=x")
}
```

- [ ] **Step 2: Run test — must fail to build**

Run: `go test ./cmd/cpg/... -run Render`
Expected: undefined `renderText`, `renderJSON`, `renderYAML`.

- [ ] **Step 3: Implement the renderers**

```go
// cmd/cpg/explain_render.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	sigyaml "sigs.k8s.io/yaml"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
)

func renderText(w io.Writer, pe evidence.PolicyEvidence, matched []evidence.RuleEvidence, samplesLimit int, color bool) error {
	c := colorizer{enabled: color}
	fmt.Fprintf(w, "%sPolicy:%s %s (%s)\n", c.bold(), c.reset(), pe.Policy.Name, pe.Policy.Namespace)
	if len(pe.Sessions) > 0 {
		last := pe.Sessions[len(pe.Sessions)-1]
		fmt.Fprintf(w, "%sLatest session:%s %s → %s (source: %s)\n\n",
			c.dim(), c.reset(),
			last.StartedAt.Format("2006-01-02 15:04"),
			last.EndedAt.Format("15:04"),
			last.Source.Type,
		)
	}

	if len(matched) == 0 {
		fmt.Fprintln(w, "No rules matched the given filters.")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Available rules:")
		for _, r := range pe.Rules {
			fmt.Fprintf(w, "  - %s %s %s/%s\n", r.Direction, peerSummary(r.Peer), r.Port, r.Protocol)
		}
		return nil
	}

	for _, r := range matched {
		writeRule(w, c, r, samplesLimit)
	}
	return nil
}

func writeRule(w io.Writer, c colorizer, r evidence.RuleEvidence, limit int) {
	title := fmt.Sprintf("%s rule", strings.Title(r.Direction))
	fmt.Fprintf(w, "%s%s%s\n", c.green(), title, c.reset())
	fmt.Fprintf(w, "  Peer:        %s\n", peerSummary(r.Peer))
	fmt.Fprintf(w, "  Port:        %s/%s\n", r.Port, r.Protocol)
	fmt.Fprintf(w, "  Flow count:  %d\n", r.FlowCount)
	fmt.Fprintf(w, "  First seen:  %s\n", r.FirstSeen.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "  Last seen:   %s\n", r.LastSeen.Format("2006-01-02 15:04:05"))

	samples := r.Samples
	if limit > 0 && len(samples) > limit {
		samples = samples[len(samples)-limit:]
	}

	if len(samples) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "  Sample flows:")
		for _, s := range samples {
			fmt.Fprintf(w, "    %s  %s → %s  %s/%d\n",
				s.Time.Format("15:04:05"),
				fmtEndpoint(s.Src), fmtEndpoint(s.Dst),
				s.Protocol, s.Port,
			)
		}
	}
	fmt.Fprintln(w, "")
}

func peerSummary(p evidence.PeerRef) string {
	switch p.Type {
	case "endpoint":
		keys := make([]string, 0, len(p.Labels))
		for k := range p.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("%s=%s", k, p.Labels[k])
		}
		return strings.Join(parts, ",") + " (endpoint)"
	case "cidr":
		return p.CIDR + " (cidr)"
	case "entity":
		return p.Entity + " (entity)"
	default:
		return "unknown"
	}
}

func fmtEndpoint(e evidence.FlowEndpoint) string {
	if e.Namespace != "" && e.Workload != "" {
		return e.Namespace + "/" + e.Workload
	}
	if e.IP != "" {
		return e.IP
	}
	return "<unknown>"
}

func renderJSON(w io.Writer, pe evidence.PolicyEvidence, matched []evidence.RuleEvidence) error {
	out := struct {
		Policy       evidence.PolicyRef      `json:"policy"`
		Sessions     []evidence.SessionInfo  `json:"sessions"`
		MatchedRules []evidence.RuleEvidence `json:"matched_rules"`
	}{
		Policy:       pe.Policy,
		Sessions:     pe.Sessions,
		MatchedRules: matched,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func renderYAML(w io.Writer, pe evidence.PolicyEvidence, matched []evidence.RuleEvidence) error {
	out := struct {
		Policy       evidence.PolicyRef      `json:"policy"`
		Sessions     []evidence.SessionInfo  `json:"sessions"`
		MatchedRules []evidence.RuleEvidence `json:"matched_rules"`
	}{
		Policy:       pe.Policy,
		Sessions:     pe.Sessions,
		MatchedRules: matched,
	}
	data, err := sigyaml.Marshal(out)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

type colorizer struct{ enabled bool }

func (c colorizer) bold() string  { if c.enabled { return ansiBold }; return "" }
func (c colorizer) dim() string   { if c.enabled { return ansiDim }; return "" }
func (c colorizer) green() string { if c.enabled { return ansiGreen }; return "" }
func (c colorizer) reset() string { if c.enabled { return ansiReset }; return "" }
```

- [ ] **Step 4: Run renderer tests**

Run: `go test ./cmd/cpg/... -run Render -v`
Expected: 4 tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/cpg/explain_render.go cmd/cpg/explain_render_test.go
git commit -m "feat(cli): text/json/yaml renderers for cpg explain"
```

---

### Task 18: `cpg explain` command wiring

**Files:**
- Create: `cmd/cpg/explain.go`
- Modify: `cmd/cpg/main.go`
- Test: `cmd/cpg/explain_test.go`

- [ ] **Step 1: Write an end-to-end test**

```go
// cmd/cpg/explain_test.go
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

func seedEvidence(t *testing.T, evDir, outDir string) {
	t.Helper()
	hash := evidence.HashOutputDir(outDir)
	p := filepath.Join(evDir, hash, "prod", "api.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	data, err := json.Marshal(sampleEvidence())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, data, 0o644))
}

func TestExplainFindsEvidenceAndPrintsText(t *testing.T) {
	outDir := t.TempDir()
	evDir := t.TempDir()
	seedEvidence(t, evDir, outDir)

	initLoggerForTesting(t)

	cmd := newExplainCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"prod/api",
		"--output-dir", outDir,
		"--evidence-dir", evDir,
	})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "Policy: cpg-api")
	assert.Contains(t, out, "app=x")
}

func TestExplainReturnsClearErrorWhenEvidenceMissing(t *testing.T) {
	outDir := t.TempDir()
	evDir := t.TempDir()
	initLoggerForTesting(t)

	cmd := newExplainCmd()
	cmd.SetOut(new(bytes.Buffer))
	errBuf := new(bytes.Buffer)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"prod/api",
		"--output-dir", outDir,
		"--evidence-dir", evDir,
	})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no evidence found")
}

func TestExplainJSONOutput(t *testing.T) {
	outDir := t.TempDir()
	evDir := t.TempDir()
	seedEvidence(t, evDir, outDir)
	initLoggerForTesting(t)

	cmd := newExplainCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"prod/api",
		"--output-dir", outDir,
		"--evidence-dir", evDir,
		"--json",
	})
	require.NoError(t, cmd.Execute())

	var got struct {
		MatchedRules []evidence.RuleEvidence `json:"matched_rules"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Len(t, got.MatchedRules, 1)
}
```

- [ ] **Step 2: Run — must fail to build**

Run: `go test ./cmd/cpg/... -run Explain`
Expected: `undefined: newExplainCmd`.

- [ ] **Step 3: Implement the command**

```go
// cmd/cpg/explain.go
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"time"

	"github.com/spf13/cobra"

	"github.com/SoulKyu/cpg/pkg/evidence"
)

func newExplainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain <NAMESPACE/WORKLOAD | path/to/policy.yaml>",
		Short: "Explain which flows produced a policy's rules",
		Long: `Inspect the per-rule evidence for a generated policy. Evidence is captured
automatically by 'cpg generate' and 'cpg replay' and stored under
$XDG_CACHE_HOME/cpg/evidence (Linux: ~/.cache/cpg/evidence).

Examples:

  cpg explain production/api-server
  cpg explain ./policies/production/api-server.yaml
  cpg explain production/api-server --ingress --port 8080
  cpg explain production/api-server --peer app=frontend --json
`,
		Args: cobra.ExactArgs(1),
		RunE: runExplain,
	}
	f := cmd.Flags()
	f.StringP("output-dir", "o", "./policies", "output directory the policy was generated into (used to locate evidence)")
	f.String("evidence-dir", "", "override evidence storage lookup path")
	f.Bool("ingress", false, "filter: ingress rules only")
	f.Bool("egress", false, "filter: egress rules only")
	f.String("port", "", "filter: rules using this port")
	f.String("peer", "", "filter: endpoint peer with KEY=VAL")
	f.String("peer-cidr", "", "filter: CIDR peer containing this CIDR")
	f.Duration("since", 0, "filter: flows last seen within this duration")
	f.Int("samples-limit", 10, "max samples to display per rule")
	f.Bool("json", false, "output JSON instead of formatted text")
	f.String("format", "text", "output format: text | json | yaml")
	return cmd
}

func runExplain(cmd *cobra.Command, args []string) error {
	target, err := resolveExplainTarget(args[0])
	if err != nil {
		return err
	}

	outputDir, _ := cmd.Flags().GetString("output-dir")
	evDir, _ := cmd.Flags().GetString("evidence-dir")
	if evDir == "" {
		evDir, err = evidence.DefaultEvidenceDir()
		if err != nil {
			return err
		}
	}

	hash := evidence.HashOutputDir(outputDir)
	reader := evidence.NewReader(evDir, hash)
	pe, err := reader.Read(target.Namespace, target.Workload)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			path := evidence.ResolvePolicyPath(evDir, hash, target.Namespace, target.Workload)
			return fmt.Errorf("no evidence found for %s/%s at %s (run `cpg generate` or `cpg replay` with the same --output-dir first)", target.Namespace, target.Workload, path)
		}
		return err
	}

	filter, err := buildFilter(cmd)
	if err != nil {
		return err
	}
	matched := make([]evidence.RuleEvidence, 0, len(pe.Rules))
	for _, r := range pe.Rules {
		if filter.match(r) {
			matched = append(matched, r)
		}
	}

	samplesLimit, _ := cmd.Flags().GetInt("samples-limit")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	format, _ := cmd.Flags().GetString("format")
	if jsonFlag {
		format = "json"
	}

	out := cmd.OutOrStdout()
	switch format {
	case "json":
		return renderJSON(out, pe, matched)
	case "yaml":
		return renderYAML(out, pe, matched)
	case "text":
		return renderText(out, pe, matched, samplesLimit, isTerminal(asFile(out)))
	default:
		return fmt.Errorf("unknown format %q: expected text | json | yaml", format)
	}
}

func buildFilter(cmd *cobra.Command) (explainFilter, error) {
	f := explainFilter{Now: time.Now()}
	ing, _ := cmd.Flags().GetBool("ingress")
	eg, _ := cmd.Flags().GetBool("egress")
	if ing && eg {
		return f, fmt.Errorf("--ingress and --egress are mutually exclusive")
	}
	if ing {
		f.Direction = "ingress"
	}
	if eg {
		f.Direction = "egress"
	}
	f.Port, _ = cmd.Flags().GetString("port")

	peer, _ := cmd.Flags().GetString("peer")
	if peer != "" {
		k, v, ok := parsePeerLabel(peer)
		if !ok {
			return f, fmt.Errorf("--peer must be KEY=VAL")
		}
		f.PeerLabel.Set = true
		f.PeerLabel.Key = k
		f.PeerLabel.Value = v
	}

	peerCIDR, _ := cmd.Flags().GetString("peer-cidr")
	if peerCIDR != "" {
		_, ipnet, err := net.ParseCIDR(peerCIDR)
		if err != nil {
			return f, fmt.Errorf("--peer-cidr %q: %w", peerCIDR, err)
		}
		f.PeerCIDR = ipnet
	}

	f.Since, _ = cmd.Flags().GetDuration("since")
	return f, nil
}

// asFile best-effort tries to turn the cobra command output into an *os.File
// for TTY detection. When out is not a file (e.g. in tests), it returns os.Stdout.
func asFile(w interface{}) *os.File {
	if f, ok := w.(*os.File); ok {
		return f
	}
	return os.Stdout
}
```

Add import `"os"` at the top.

- [ ] **Step 4: Register the command**

Edit `cmd/cpg/main.go`:

```go
rootCmd.AddCommand(newExplainCmd())
```

- [ ] **Step 5: Run all explain tests**

Run: `go test ./cmd/cpg/... -run Explain -v`
Expected: 3 tests pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/cpg/explain.go cmd/cpg/main.go cmd/cpg/explain_test.go
git commit -m "feat(cli): cpg explain subcommand with filters and multi-format output"
```

---

## Phase 9 — Documentation and polish

### Task 19: README additions

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add offline-replay section after the existing "Quick start"**

Insert after the existing "Quick start" block in `README.md`:

```markdown
## Quick start (offline replay)

Prefer to iterate on policy generation without reproducing traffic? Capture once, replay many:

```bash
# Capture dropped flows for N minutes
hubble observe --output jsonpb --follow > drops.jsonl
# Ctrl+C when done capturing

# Replay through cpg — reuse the file as many times as you want
cpg replay drops.jsonl -n production
```

`cpg replay` accepts `-` to read from stdin and transparently
decompresses `.gz` files.
```

- [ ] **Step 2: Add an "Offline replay (UC1)" section after the "What it generates" block**

Insert:

```markdown
## Offline replay

`cpg replay <file>` feeds a Hubble jsonpb capture through the same pipeline
as the live stream. It is the right tool when you want:

- **Deterministic iteration.** Re-run the same input as you tweak label
  selection, dedup logic, or flush intervals.
- **Offline workflow.** Capture on a jumphost, replay on your laptop.
- **Post-mortem reproduction.** Keep the capture alongside the policy in
  your GitOps repo so anyone can reproduce what cpg saw.

Capture:

```bash
hubble observe --output jsonpb --follow > drops.jsonl
```

Replay:

```bash
cpg replay drops.jsonl -n production
cpg replay drops.jsonl.gz -n production    # gzip transparent
cat drops.jsonl | cpg replay -              # stdin
```

Flags shared with `generate` (e.g. `--output-dir`, `--cluster-dedup`)
work identically.
```

- [ ] **Step 3: Add an "Explain policies (UC3)" section**

Insert after the "Unhandled flows" block:

```markdown
## Explain policies

After a run, every emitted rule has per-flow evidence recorded alongside
the YAML. Inspect it with `cpg explain`:

```bash
cpg explain production/api-server
cpg explain production/api-server --peer app=frontend
cpg explain production/api-server --ingress --port 8080
cpg explain ./policies/production/api-server.yaml --since 1h --json
```

Example output:

```
Policy: cpg-api-server (production)
Latest session: 2026-04-24 14:02 → 14:15 (source: replay)

Ingress rule
  Peer:        app=frontend (endpoint)
  Port:        8080/TCP
  Flow count:  23
  First seen:  2026-04-24 14:02:11
  Last seen:   2026-04-24 14:15:48

  Sample flows:
    14:02:11  default/frontend-5d4f → production/api-server-abc  TCP/8080
    14:02:13  default/frontend-5d4f → production/api-server-abc  TCP/8080
    ...
```

### Where is evidence stored?

Evidence lives outside the output directory to keep GitOps clean:

- **Linux:** `$XDG_CACHE_HOME/cpg/evidence` (defaults to `~/.cache/cpg/evidence`)
- **macOS:** `~/Library/Caches/cpg/evidence`

The path is keyed by a hash of the absolute output directory, so multiple
workspaces coexist without collision.

To share evidence with a colleague or archive it:

```bash
cpg replay drops.jsonl -n production --evidence-dir ./evidence
# ... ship ./evidence alongside the policies
cpg explain production/api-server --evidence-dir ./evidence
```

Disable capture entirely with `--no-evidence`. Tune retention per rule
with `--evidence-samples` (default 10) and per policy with
`--evidence-sessions` (default 10).
```

- [ ] **Step 4: Add a "Dry-run (UC4)" section**

Insert under the "Deduplication" section:

```markdown
## Dry-run

Preview what `generate` or `replay` would write without touching any
file:

```bash
cpg replay drops.jsonl --dry-run           # with unified diff
cpg replay drops.jsonl --dry-run --no-diff # log-only
cpg generate -n production --dry-run
```

In `--dry-run` mode, all stages of the pipeline run normally: you still
see unhandled-flow warnings, cluster-dedup hits, and aggregation logs.
Only the filesystem write step is suppressed. When an existing file
would change, a unified diff is printed to stdout (colored on a tty,
plain otherwise).

Useful for validating a refactor of label-selection logic against a
historical capture:

```bash
# Before the refactor
cpg replay drops.jsonl -o /tmp/baseline

# After the refactor
cpg replay drops.jsonl --dry-run --output-dir /tmp/baseline
# read the diff — verify only the changes you expected appear
```
```

- [ ] **Step 5: Update the Flags block**

Append these lines to the existing `## Flags` code block, in the appropriate section:

```
Dry-run:
      --dry-run              Preview changes without writing to disk
      --no-diff              With --dry-run, skip unified diff

Evidence:
      --no-evidence          Disable per-rule evidence capture
      --evidence-dir string  Override evidence storage path
      --evidence-samples int Samples per rule (default 10)
      --evidence-sessions int Sessions per policy (default 10)
```

Add a new command reference after the existing `cpg generate [flags]` block:

```
cpg replay <file.jsonl|-> [flags]

Positional:
  <file.jsonl|->             Path to Hubble jsonpb dump, or "-" for stdin
                             .gz extension triggers transparent gzip

All of generate's output/filtering/aggregation/dedup/dry-run/evidence
flags apply.

cpg explain <NAMESPACE/WORKLOAD | path/to/policy.yaml> [flags]

Flags:
  -o, --output-dir string    Same output-dir used when generating
      --evidence-dir string  Override evidence storage lookup path
      --ingress / --egress   Direction filter
      --port string          Port filter
      --peer string          Endpoint peer filter KEY=VAL
      --peer-cidr string     CIDR peer filter
      --since duration       Flows last seen within this duration
      --samples-limit int    Samples displayed per rule (default 10)
      --json                 Shortcut for --format json
      --format string        text | json | yaml (default text)
```

- [ ] **Step 6: Update the Project structure block**

Replace the current block with:

```
cmd/cpg/           CLI entrypoint (cobra): generate, replay, explain
pkg/labels/        Label selection, denylist, endpoint/peer selector builders
pkg/policy/        Flow-to-CiliumNetworkPolicy builder, merge, dedup, attribution
pkg/output/        Directory-organized YAML writer with merge-on-write
pkg/hubble/        Live gRPC client, aggregator, pipeline orchestration
pkg/k8s/           Kubeconfig loading, port-forward, cluster policy fetching
pkg/flowsource/    Flow stream abstraction: live gRPC or jsonpb file source
pkg/evidence/      Per-rule flow attribution (cpg explain)
pkg/diff/          Unified YAML diff (cpg generate/replay --dry-run)
```

- [ ] **Step 7: Commit**

```bash
git add README.md
git commit -m "docs: document cpg replay, explain, and dry-run workflows"
```

---

### Task 20: CHANGELOG + version bump

**Files:**
- Modify: `CHANGELOG.md` (entries are driven by release-please, but an explicit Release-As trigger is used for this minor bump)

- [ ] **Step 1: Verify the release-please PR will open**

The cumulative commits on this branch include `feat:` conventional commits (new features) — release-please will compute a minor bump automatically once merged to master.

No manual CHANGELOG edit is required.

- [ ] **Step 2: Final full-suite run**

Run: `go build ./... && go test ./... -v`
Expected: all tests pass.

- [ ] **Step 3: Rebase onto master before opening the PR**

Run:

```bash
git fetch origin
git rebase origin/master
```

Resolve any conflicts (there should be none since the feature work is additive).

- [ ] **Step 4: Push the feature branch**

```bash
git push -u origin offline-replay-and-analysis
```

- [ ] **Step 5: Open the PR**

Title: `feat: offline replay, per-rule evidence, and dry-run`

Body (copy from the design doc Section 1 "Goals" and Section 11
"Documentation"). Reference the spec:

> Spec: `docs/superpowers/specs/2026-04-24-offline-replay-and-analysis-design.md`

Once merged, release-please will produce a v1.6.0 release PR. Merge that
release PR to trigger goreleaser.

---

## Self-review

Spec coverage sweep done:
- UC1 offline replay → Tasks 8, 9, 14.
- UC3 explain → Tasks 2, 3, 4, 5, 6, 7, 12, 16, 17, 18.
- UC4-light dry-run → Tasks 10, 11, 15.
- Shared refactor (FlowSource) → Task 1.
- Shared flags → Task 13.
- README → Task 19.
- Release → Task 20.

Signature consistency: `BuildPolicy` new signature appears in Task 7 and is consumed in Task 7 (Aggregator) and Task 14 (via PipelineConfig). `RuleKey.String()` used by evidence writer in Task 12 matches definition in Task 6. `evidenceWriter.handle` / `finalize` names are stable from Task 12 through Task 15.

No `TODO`, no "later", no unexplained placeholders. Every code step contains full source.
