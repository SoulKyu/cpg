# Phase 20: `--include-audit` Verdict Ingestion - Pattern Map

**Mapped:** 2026-07-22
**Files analyzed:** 19 (11 production + 6 test + 1 new test file + 1 new fixture, README excluded from count)
**Analogs found:** 19 / 19 — every file has an exact analog, because this phase is a 4th instance of a pattern (`L7Enabled`/`IgnoreProtocols`/`IgnoreDropReasons`) already shipped 3 times in the exact same files. In nearly every case the closest analog is the **same file's own existing sibling-flag code**, not a different file.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `cmd/cpg/commonflags.go` | config (cobra flag registration) | request-response | self — existing `l7` flag (lines 55, 82, 108) | exact (self-pattern) |
| `cmd/cpg/generate.go` | controller (CLI entrypoint) | request-response | self — `L7Enabled` threading (lines 118-119, 223, 251) | exact (self-pattern) |
| `cmd/cpg/replay.go` | controller (CLI entrypoint) | request-response | self — `L7Enabled` threading (lines 123-125) | exact (self-pattern) |
| `cmd/cpg/mcp_tools.go` | route (MCP tool handler) | request-response | self — `L7` arg threading (lines 24, 130) | exact (self-pattern) |
| `pkg/session/session.go` | model (config struct) | CRUD (struct field) | self — `StartArgs.L7` (line 137) | exact (self-pattern) |
| `pkg/session/pipeline_config.go` | service (config transform) | transform | self — `L7Enabled` passthrough (line 101) | exact (self-pattern) |
| `pkg/flowsource/source.go` | interface/model | streaming | self — interface definition, mechanical signature widen | exact |
| `pkg/flowsource/file.go` | service (flow source) | file-I/O, streaming | self — replay gate (line 116) + `StreamDroppedFlows` sig (line 72) | exact (self-pattern) |
| `pkg/hubble/client.go` | service (flow source) | streaming (gRPC) | self — `buildFilters` (lines 195-217) + `StreamDroppedFlows` sig (line 53) | exact (self-pattern) |
| `pkg/hubble/pipeline.go` | service (orchestration) | streaming, event-driven | self — VIS-01 block (lines 345-351) + call site (line 171) | exact (self-pattern) |
| `pkg/hubble/aggregator.go` | service (aggregation) | event-driven, streaming | self — `l7HTTPCount`/`SetL7Enabled` (lines 81-88, 305-321, 381-386) + classification gate (line 417) | exact (self-pattern) |
| `pkg/flowsource/source_test.go` | test | streaming | self — `stubSource` (line 13) | exact — mechanical |
| `pkg/flowsource/file_test.go` | test | file-I/O | self — 8 `StreamDroppedFlows(ctx, nil, false)` call sites | exact — mechanical |
| `pkg/hubble/client_test.go` | test | streaming | self — `TestBuildFilters_*` (lines 41-89) | exact — golden-value extension |
| `pkg/hubble/pipeline_test.go` | test | streaming | self — `mockFlowSource`/`errStreamSource`/`errStreamSourceWithInfraDrop`/`channelFlowSource` | exact — mechanical |
| `pkg/session/manager_test.go` | test | streaming | self — `closedFlowSource`/`blockingFlowSource` | exact — mechanical |
| `pkg/hubble/pipeline_audit_test.go` (**NEW**) | test | streaming, integration | `pkg/hubble/pipeline_l7_test.go` (full file, 200 lines) | exact — byte-for-byte reusable template |
| `testdata/flows/with_audit.jsonl` (**NEW**) | fixture | file-I/O | `testdata/flows/small.jsonl` + `with_non_dropped.jsonl` | exact |
| `README.md` | docs | — | self — existing `l7`/`ignore-protocol` doc rows (Flags table line ~82, 116-124; MCP tool table line 512) | exact (self-pattern) |

**No-analog files:** none. This phase's entire toolkit (flag threading, interface widening, counter+warning, test doubles) already exists in the codebase 3 times over (`L7Enabled`, `IgnoreProtocols`, `IgnoreDropReasons`), per RESEARCH.md's "State of the Art" table.

---

## Pattern Assignments

### Group 1 — CLI + MCP flag threading (`IncludeAudit bool`, 4th instance of the `L7Enabled` shape)

Six files carry the exact same 3-hop shape already proven 3 times: **cobra flag → `commonFlags`/`generateFlags` → `PipelineConfig`** (CLI) and **`startSessionArgs` → `StartArgs` → `buildPipelineConfig` → `PipelineConfig`** (MCP). Copy each hop verbatim, substituting `l7`→`includeAudit`/`IncludeAudit`.

#### `cmd/cpg/commonflags.go` (config, request-response)

**Analog:** this file's own `l7` flag, 3 call sites.

**Struct field** (line 55):
```go
type commonFlags struct {
	...
	l7 bool
	...
}
```
→ add `includeAudit bool` alongside it.

**Flag registration** (`addCommonFlags`, line 82):
```go
f.Bool("l7", false, "enable L7 (HTTP/DNS) policy generation; Phase 7 plumbs the flag, codegen lights up in v1.2 Phase 8/9")
```
→ add immediately after:
```go
f.Bool("include-audit", false, "ingest Verdict_AUDIT flows alongside DROPPED (opt-in; default preserves pre-v1.6 DROPPED-only behavior)")
```

**Flag parse** (`parseCommonFlags`, line 108):
```go
out.l7, _ = f.GetBool("l7")
```
→ add:
```go
out.includeAudit, _ = f.GetBool("include-audit")
```

No new imports required (cobra already imported).

---

#### `cmd/cpg/generate.go` (controller, request-response)

**Analog:** this file's own `L7Enabled: f.l7,` field (line 251) inside the `hubble.PipelineConfig{...}` literal returned by `runGenerate`.

**Core pattern** (lines 225-256, `hubble.RunPipeline(ctx, hubble.PipelineConfig{...})`):
```go
	return hubble.RunPipeline(ctx, hubble.PipelineConfig{
		...
		L7Enabled: f.l7,

		IgnoreProtocols:   ignoreProtocols,
		IgnoreDropReasons: ignoreDropReasons,
		FailOnInfraDrops:  f.failOnInfraDrops,
	})
```
→ add `IncludeAudit: f.includeAudit,` in the same block (no validation needed — cobra bool flags need none, same as `l7`).

No new imports required.

---

#### `cmd/cpg/replay.go` (controller, request-response)

**Analog:** this file's own `L7Enabled: f.l7,` (line 125) inside `runReplay`'s `hubble.PipelineConfig{...}` literal — note this file uses `commonFlags` directly (no `replayFlags` wrapper exists, unlike `generateFlags`), so this is a pure field-copy, no struct addition needed here.

**Core pattern** (lines 100-130):
```go
	cfg := hubble.PipelineConfig{
		...
		// L7Enabled is plumbed through but is a no-op for codegen in v1.2 Phase 7.
		// cpg replay NEVER invokes L7 pre-flight (offline path) regardless of --l7.
		L7Enabled: f.l7,

		IgnoreProtocols:   ignoreProtocols,
		IgnoreDropReasons: ignoreDropReasons,
		FailOnInfraDrops:  f.failOnInfraDrops,
	}

	return hubble.RunPipelineWithSource(ctx, cfg, source)
```
→ add `IncludeAudit: f.includeAudit,` alongside `L7Enabled`.

No new imports required.

---

#### `cmd/cpg/mcp_tools.go` (route/MCP tool, request-response)

**Analog:** this file's own `L7` field in `startSessionArgs` (line 24) and its threading into `session.StartArgs` (line 130).

**Imports** (lines 1-11, unchanged):
```go
import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/SoulKyu/cpg/pkg/session"
)
```

**Struct field pattern** (`startSessionArgs`, line 24):
```go
type startSessionArgs struct {
	Namespace         []string `json:"namespace,omitempty" jsonschema:"namespace filter, repeatable"`
	AllNamespaces     bool     `json:"all_namespaces,omitempty" jsonschema:"observe all namespaces"`
	L7                bool     `json:"l7,omitempty" jsonschema:"enable L7 (HTTP/DNS) policy generation"`
	...
}
```
→ add:
```go
	IncludeAudit bool `json:"include_audit,omitempty" jsonschema:"ingest Verdict_AUDIT flows alongside DROPPED (opt-in; default preserves pre-v1.6 DROPPED-only behavior)"`
```
Per Pitfall 6 (doc drift), the `jsonschema:"..."` string IS the LLM-facing schema description — write it carefully in the same commit as README updates.

**Threading into StartArgs** (`start_session` handler, line 127-138):
```go
		result, err := mgr.Start(ctx, session.StartArgs{
			Namespaces:        args.Namespace,
			AllNamespaces:     args.AllNamespaces,
			L7:                args.L7,
			IgnoreDropReasons: ignoreDropReasons,
			IgnoreProtocols:   ignoreProtocols,
			Server:            args.Server,
			TLS:               args.TLS,
			Timeout:           timeout,
			ClusterDedup:      args.ClusterDedup,
			FlushInterval:     flushInterval,
		})
```
→ add `IncludeAudit: args.IncludeAudit,` alongside `L7: args.L7,`. No new validation call needed — bool passthrough, same as `L7`/`ClusterDedup`/`TLS` today (V5 Input Validation: trivially satisfied per RESEARCH.md Security Domain).

---

#### `pkg/session/session.go` (model, CRUD)

**Analog:** this file's own `L7 bool` field in `StartArgs` (line 137).

**Core pattern** (`StartArgs` struct, lines 134-149):
```go
type StartArgs struct {
	Namespaces    []string
	AllNamespaces bool
	L7            bool
	// IgnoreDropReasons is the uppercase, pre-validated set (D-06 — same
	// normalization as the CLI's existing drop-reason validator).
	IgnoreDropReasons []string
	...
}
```
→ add `IncludeAudit bool` alongside `L7`.

No new imports required.

---

#### `pkg/session/pipeline_config.go` (service, transform)

**Analog:** this file's own `L7Enabled: args.L7,` (line 101) inside `buildPipelineConfig`'s `hubble.PipelineConfig{...}` literal.

**Core pattern** (lines 73-108):
```go
	return hubble.PipelineConfig{
		...
		L7Enabled: args.L7,

		IgnoreProtocols:   args.IgnoreProtocols,
		IgnoreDropReasons: args.IgnoreDropReasons,

		Stdout:  stdout,
		OnFinal: onFinal,
	}
```
→ add `IncludeAudit: args.IncludeAudit,` alongside `L7Enabled`.

No new imports required. (Open Question 2 in RESEARCH.md — whether to also surface `AuditVerdictCount` through `StopResult`/`session.go`'s `buildSummary` — is discretionary; if implemented, mirror `L7HTTPCount`'s exact 4-hop plumbing: `hubble.SessionStats.AuditVerdictCount` → `session.go:buildSummary` (lines 242-249 pattern, e.g. `result.L7HTTPCount = stats.L7HTTPCount`) → `StopResult.AuditVerdictCount uint64 \`json:"audit_verdict_count"\`` (mirrors line 198's `L7HTTPCount`).)

---

### Group 2 — `FlowSource` interface + verdict filter sites (sites 1-4 + interface ripple)

#### `pkg/flowsource/source.go` (interface/model, streaming)

**Full file today** (16 lines):
```go
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
**Widened signature** (add 4th param, per RESEARCH.md's blast-radius table):
```go
	StreamDroppedFlows(ctx context.Context, namespaces []string, allNS bool, includeAudit bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error)
```
This is the root of the 19-location compiler-enforced ripple — land this change LAST (after every implementer/caller is ready) or expect `go build ./...` to fail until all 18 dependents are updated in the same commit/PR.

---

#### `pkg/flowsource/file.go` (service, file-I/O + streaming) — site 4

**Imports** (lines 1-18, unchanged — no new imports needed):
```go
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
```

**Signature widen** (line 72):
```go
func (s *FileSource) StreamDroppedFlows(ctx context.Context, _ []string, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
```
→ `func (s *FileSource) StreamDroppedFlows(ctx context.Context, _ []string, _ bool, includeAudit bool) (...)`

**Replay gate widen — site 4** (current, lines 116-119):
```go
			if f.Verdict != flowpb.Verdict_DROPPED {
				s.stats.nonDroppedSkipped.Add(1)
				continue
			}
```
→ recommended (per RESEARCH.md Code Example + Pitfall 3 — keep the counter increment inside the skip branch of the SAME widened condition, do not split into a separate check):
```go
			if !(f.Verdict == flowpb.Verdict_DROPPED || (includeAudit && f.Verdict == flowpb.Verdict_AUDIT)) {
				s.stats.nonDroppedSkipped.Add(1)
				continue
			}
```
Existing fixture `with_non_dropped.jsonl` (DROPPED/FORWARDED only, zero AUDIT) keeps `TestFileSourceFiltersNonDropped`'s asserted count of `2` unchanged regardless of this edit (Pitfall 3 guard).

---

#### `pkg/hubble/client.go` (service, streaming/gRPC) — sites 1-3

**Imports** (lines 1-18, unchanged):
```go
import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	observerpb "github.com/cilium/cilium/api/v1/observer"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)
```

**Signature widen** (line 53):
```go
func (c *Client) StreamDroppedFlows(ctx context.Context, namespaces []string, allNS bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) {
```
→ add `includeAudit bool` 4th param, pass through to `buildFilters` call (line 81: `Whitelist: buildFilters(namespaces, allNS)` → `buildFilters(namespaces, allNS, includeAudit)`).

**`buildFilters` widen — sites 1-3, current** (lines 192-217):
```go
// buildFilters constructs FlowFilter whitelist entries to filter dropped flows
// by namespace. Multiple whitelist filters are OR-ed; fields within a single
// filter are AND-ed.
func buildFilters(namespaces []string, allNS bool) []*flowpb.FlowFilter {
	if allNS || len(namespaces) == 0 {
		return []*flowpb.FlowFilter{
			{Verdict: []flowpb.Verdict{flowpb.Verdict_DROPPED}},
		}
	}

	prefixes := make([]string, len(namespaces))
	for i, ns := range namespaces {
		prefixes[i] = ns + "/"
	}

	return []*flowpb.FlowFilter{
		{
			Verdict:   []flowpb.Verdict{flowpb.Verdict_DROPPED},
			SourcePod: prefixes,
		},
		{
			Verdict:        []flowpb.Verdict{flowpb.Verdict_DROPPED},
			DestinationPod: prefixes,
		},
	}
}
```
→ recommended widened shape (RESEARCH.md Code Example 1 — single local `verdicts` slice, avoids the 3x-independent-conditional footgun flagged in Anti-Patterns):
```go
func buildFilters(namespaces []string, allNS bool, includeAudit bool) []*flowpb.FlowFilter {
	verdicts := []flowpb.Verdict{flowpb.Verdict_DROPPED}
	if includeAudit {
		verdicts = append(verdicts, flowpb.Verdict_AUDIT)
	}

	if allNS || len(namespaces) == 0 {
		return []*flowpb.FlowFilter{{Verdict: verdicts}}
	}

	prefixes := make([]string, len(namespaces))
	for i, ns := range namespaces {
		prefixes[i] = ns + "/"
	}

	return []*flowpb.FlowFilter{
		{Verdict: verdicts, SourcePod: prefixes},
		{Verdict: verdicts, DestinationPod: prefixes},
	}
}
```

---

#### `pkg/hubble/pipeline.go` (service/orchestration, streaming + event-driven) — production call site + `PipelineConfig` field + AUD-01 warning

**`PipelineConfig` struct — add field** (analog: `L7Enabled bool` at line 74):
```go
	// L7Enabled: no-op in v1.2 Phase 7; Phase 8 (HTTP) and Phase 9 (DNS) light up codegen.
	L7Enabled bool
```
→ add nearby:
```go
	// IncludeAudit: when true, Verdict_AUDIT flows are ingested alongside
	// Verdict_DROPPED at every filter site (sites 1-5). Default false
	// preserves byte-identical pre-v1.6 DROPPED-only behavior.
	IncludeAudit bool
```

**Production call site widen** (line 171, the sole non-test call to the interface method):
```go
func RunPipelineWithSource(ctx context.Context, cfg PipelineConfig, source flowsource.FlowSource) error {
	flows, lostEvents, err := source.StreamDroppedFlows(ctx, cfg.Namespaces, cfg.AllNamespaces)
```
→ `source.StreamDroppedFlows(ctx, cfg.Namespaces, cfg.AllNamespaces, cfg.IncludeAudit)`

**Aggregator wiring** (analog: line 184 `agg.SetL7Enabled(cfg.L7Enabled)`):
```go
	agg.SetL7Enabled(cfg.L7Enabled)
	agg.SetIgnoreProtocols(cfg.IgnoreProtocols)
	agg.SetIgnoreDropReasons(cfg.IgnoreDropReasons)
```
→ add `agg.SetIncludeAudit(cfg.IncludeAudit)` alongside these.

**AUD-01 warning — exact VIS-01 template, add immediately after** (source: lines 340-351, unmodified — copy shape verbatim, do not build a dedup map per Pitfall 2):
```go
	// VIS-01: passive empty-L7-records detection. Single warning per pipeline
	// run, fired only when --l7 was requested AND at least one flow was
	// observed AND zero L7 records (HTTP + DNS) materialized. The DNS branch
	// is wired here in advance of Phase 9 — agg.L7DNSCount() returns 0 in
	// Phase 8, so the gate degrades gracefully.
	if cfg.L7Enabled && stats.FlowsSeen > 0 && agg.L7HTTPCount()+agg.L7DNSCount() == 0 {
		cfg.Logger.Warn("--l7 set but no L7 records observed in window",
			zap.Strings("workloads", agg.ObservedWorkloads()),
			zap.Uint64("flows", stats.FlowsSeen),
			zap.String("hint", "see README L7 prerequisites: #l7-prerequisites"),
		)
	}
```
→ new AUD-01 block, same location, same shape (RESEARCH.md's exact recommended text):
```go
	// AUD-01: passive empty-AUDIT-records detection. Single warning per
	// pipeline run, fired only when --include-audit was requested AND at
	// least one flow was observed AND zero AUDIT-verdict flows materialized.
	// Mirrors VIS-01's shape exactly — a bare post-g.Wait() check, NOT a
	// dedup map (see Pitfall 2 / warnedReserved correction in RESEARCH.md).
	if cfg.IncludeAudit && stats.FlowsSeen > 0 && agg.AuditVerdictCount() == 0 {
		cfg.Logger.Warn("--include-audit set but no AUDIT-verdict flows observed in window",
			zap.Strings("workloads", agg.ObservedWorkloads()),
			zap.Uint64("flows", stats.FlowsSeen),
		)
	}
```
No new imports required (`zap` already imported).

---

### Group 3 — Aggregator classification gate + counter (site 5)

#### `pkg/hubble/aggregator.go` (service, event-driven/streaming)

**Imports** (lines 1-17, unchanged):
```go
import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"go.uber.org/zap"

	"github.com/SoulKyu/cpg/pkg/dropclass"
	"github.com/SoulKyu/cpg/pkg/labels"
	"github.com/SoulKyu/cpg/pkg/policy"
)
```

**Struct field + counter — analog `l7Enabled`/`l7HTTPCount`** (lines 77-88):
```go
	// l7Enabled gates the HTTP L7 codegen branch in BuildPolicy. Forwarded
	// from PipelineConfig.L7Enabled via SetL7Enabled before Run().
	l7Enabled bool

	// l7HTTPCount counts flows carrying a non-nil Flow.L7.Http record observed
	// during the session (independent of l7Enabled — counter is diagnostic and
	// powers the VIS-01 empty-records warning in pipeline.Finalize).
	l7HTTPCount uint64
```
→ add:
```go
	// includeAudit gates whether the classification gate (Run, below) treats
	// Verdict_AUDIT the same as Verdict_DROPPED. Forwarded from
	// PipelineConfig.IncludeAudit via SetIncludeAudit before Run().
	includeAudit bool

	// auditVerdictCount counts flows carrying Verdict_AUDIT observed during
	// the session, incremented unconditionally in Run() (mirrors l7HTTPCount's
	// rationale) — powers the AUD-01 empty-records warning in pipeline.go
	// regardless of whether includeAudit is set.
	auditVerdictCount uint64
```

**Setter + accessor — analog `SetL7Enabled`/`L7HTTPCount`** (lines 305-315):
```go
// SetL7Enabled toggles the HTTP L7 codegen branch in BuildPolicy. Safe to
// call before Run().
func (a *Aggregator) SetL7Enabled(enabled bool) {
	a.l7Enabled = enabled
}

// L7HTTPCount returns the number of flows with non-nil Flow.L7.Http observed
// across the session. Independent of L7Enabled; used by VIS-01.
func (a *Aggregator) L7HTTPCount() uint64 {
	return a.l7HTTPCount
}
```
→ add:
```go
// SetIncludeAudit toggles whether the classification gate treats
// Verdict_AUDIT the same as Verdict_DROPPED. Safe to call before Run().
func (a *Aggregator) SetIncludeAudit(enabled bool) {
	a.includeAudit = enabled
}

// AuditVerdictCount returns the number of Verdict_AUDIT flows observed
// across the session. Populated regardless of includeAudit; used by AUD-01.
func (a *Aggregator) AuditVerdictCount() uint64 {
	return a.auditVerdictCount
}
```

**Counter increment in `Run()` — analog l7HTTPCount/l7DNSCount increment** (lines 375-386):
```go
			// Count L7 HTTP and DNS records on every observed flow, regardless
			// of whether the flow makes it into a bucket and regardless of
			// l7Enabled. Both counters power VIS-01's empty-records gate in
			// pipeline.Finalize (which sums them) — they are purely
			// diagnostic and must remain accurate even when L7 codegen is
			// disabled.
			if f.GetL7().GetHttp() != nil {
				a.l7HTTPCount++
			}
			if f.GetL7().GetDns() != nil {
				a.l7DNSCount++
			}
```
→ add immediately alongside (unconditional, mirrors the L7 counters' "regardless of flag" rationale — structurally guaranteed 0 when `includeAudit=false` because sites 1-4 never deliver an AUDIT flow to this loop in that case, per RESEARCH.md's byte-identical-path analysis):
```go
			if f.Verdict == flowpb.Verdict_AUDIT {
				a.auditVerdictCount++
			}
```

**Classification gate widen — site 5, current** (lines 412-417):
```go
			// HEALTH-01/05: Classification gate — applies only to flows with an
			// explicit DROPPED verdict and a non-zero drop reason. Zero-value
			// DropReasonDesc on non-DROPPED flows (e.g. forwarded/unknown) must
			// pass through unmodified (PITFALLS Integration Gotchas: always check
			// Verdict == DROPPED before classifying).
			if f.Verdict == flowpb.Verdict_DROPPED && f.GetDropReasonDesc() != flowpb.DropReason_DROP_REASON_UNKNOWN {
```
→ widened (RESEARCH.md Code Example 2 — exact recommended text, includes the Pitfall-4 code comment):
```go
			// HEALTH-01/05 + AUD-01: applies to flows with an explicit DROPPED
			// (or, when includeAudit, AUDIT) verdict and a non-zero drop
			// reason. Zero-value DropReasonDesc on non-DROPPED/non-AUDIT flows
			// must pass through unmodified. Note (Pitfall 4, non-blocking): if
			// an AUDIT flow's DropReasonDesc is UNKNOWN on some Cilium
			// version/deployment, this condition is simply false for that flow
			// and it falls through to keyFromFlow() → still bucketed → still
			// generates a policy (same safe fallback as any DROPPED flow with
			// an unknown reason today; no flow is silently lost).
			if (f.Verdict == flowpb.Verdict_DROPPED || (a.includeAudit && f.Verdict == flowpb.Verdict_AUDIT)) &&
				f.GetDropReasonDesc() != flowpb.DropReason_DROP_REASON_UNKNOWN {
```
The rest of the `switch class { ... }` block below (lines 418-443) is unchanged — verified downstream logic keys off `DropReasonDesc`/`class`, never `Verdict`, again.

---

### Group 4 — Test-double mechanical ripple (compile-time only, no new test logic)

All 15 locations below need only the interface's 4th parameter added to compile; the value is ignored (`_ bool`) except where the test explicitly exercises AUDIT behavior. **This is Pitfall 1** — budget every location, not just the 5 conceptual filter sites.

| # | File:line | Type/call | Analog shape (exact copy-paste, change only the receiver) |
|---|-----------|-----------|---|
| 1 | `pkg/flowsource/source_test.go:13` | `stubSource.StreamDroppedFlows` | `func (stubSource) StreamDroppedFlows(_ context.Context, _ []string, _ bool, _ bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error) { return nil, nil, nil }` |
| 2 | `pkg/hubble/pipeline_test.go:32` | `mockFlowSource.StreamDroppedFlows` | add `_ bool` 4th param to `func (m *mockFlowSource) StreamDroppedFlows(_ context.Context, _ []string, _ bool) (...)` — body unchanged |
| 3 | `pkg/hubble/pipeline_test.go:304` | `errStreamSource.StreamDroppedFlows` | same — add `_ bool` 4th param, body unchanged (closes both channels immediately) |
| 4 | `pkg/hubble/pipeline_test.go:362` | `errStreamSourceWithInfraDrop.StreamDroppedFlows` | same — add `_ bool` 4th param, body unchanged |
| 5 | `pkg/hubble/pipeline_test.go:453` | `channelFlowSource.StreamDroppedFlows` | same — add `_ bool` 4th param, body unchanged (`return c.flows, c.lost, nil`) |
| 6 | `pkg/session/manager_test.go:35` | `closedFlowSource.StreamDroppedFlows` | same — add `_ bool` 4th param, body unchanged |
| 7 | `pkg/session/manager_test.go:55` | `blockingFlowSource.StreamDroppedFlows` | same — add `_ bool` 4th param (note: `ctx context.Context` is named here, not `_`, since the body uses `ctx.Done()`) |
| 8-15 | `pkg/flowsource/file_test.go:42,59,70,81,100,122,147,159` | 8 direct calls to `src.StreamDroppedFlows(ctx, nil, false)` | add a 4th arg, `false`, at every call site — e.g. line 42: `src.StreamDroppedFlows(context.Background(), nil, false)` → `src.StreamDroppedFlows(context.Background(), nil, false, false)`. All 8 existing tests assert DROPPED-only/malformed/gzip/cancellation behavior unrelated to AUDIT, so `false` (no widening) is correct for every one of them. |

No new imports required in any of these 8 files — `context`/`flowpb` already imported everywhere the interface is referenced.

---

### Group 5 — Golden/value-pinning test extension (`TestBuildFilters_*`)

#### `pkg/hubble/client_test.go` (test, streaming)

**Analog:** cpg has no golden-file infrastructure (RESEARCH.md "Don't Hand-Roll") — these 4 `testify`-based value-pinning tests ARE the de facto golden test for AC-2.

**Existing pattern to extend** (lines 41-48, repeat for all 4 `TestBuildFilters_*`):
```go
func TestBuildFilters_AllNamespaces(t *testing.T) {
	filters := buildFilters(nil, true)

	require.Len(t, filters, 1, "all-namespaces should produce a single filter")
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict)
	assert.Empty(t, filters[0].SourcePod, "should not filter by source pod")
	assert.Empty(t, filters[0].DestinationPod, "should not filter by destination pod")
}
```
**Widen for AC-2 (byte-identical, flag unset)** — add `false` as a 3rd arg at every one of the 4 call sites (lines 42, 51, 67, 83) and keep every assertion identical (`[]flowpb.Verdict{flowpb.Verdict_DROPPED}` unchanged) — this IS the byte-identical regression proof:
```go
	filters := buildFilters(nil, true, false)
	...
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED}, filters[0].Verdict) // unchanged assertion
```

**New sibling `_WithAudit` variants (widened-behavior proof)** — one per existing test, `true` 3rd arg, asserting the 2-element verdict slice:
```go
func TestBuildFilters_AllNamespaces_WithAudit(t *testing.T) {
	filters := buildFilters(nil, true, true)

	require.Len(t, filters, 1)
	assert.Equal(t, []flowpb.Verdict{flowpb.Verdict_DROPPED, flowpb.Verdict_AUDIT}, filters[0].Verdict)
}
```
Repeat this shape for `SingleNamespace`, `MultipleNamespaces`, `EmptyNamespaces` (analogs at lines 50-89), asserting both `filters[0].Verdict` and `filters[1].Verdict` carry the 2-element slice where the existing test has 2 filters.

No new imports required (`testify`'s `assert`/`require` already imported).

---

### Group 6 — New test file: `pkg/hubble/pipeline_audit_test.go`

**Analog:** `pkg/hubble/pipeline_l7_test.go` (full 200-line file) — RESEARCH.md identifies this as a byte-for-byte reusable template. Copy the file structure exactly; substitute `L7Enabled`→`IncludeAudit`, `L7HTTPCount()+L7DNSCount()`→`AuditVerdictCount()`, `"no L7 records observed"`→`"no AUDIT-verdict flows observed"`.

**Imports** (lines 1-20 of the analog, reuse verbatim):
```go
package hubble

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/SoulKyu/cpg/pkg/evidence"
	"github.com/SoulKyu/cpg/pkg/flowsource"
)
```
(Drop `evidence`/`encoding/json` imports if the AUDIT test file skips the evidence-assertion tests — only needed if mirroring `TestPipeline_L7HTTP_GeneratedAndEvidence`'s evidence-file read.)

**Fixture constants** (analog lines 22-24 — reuse 2 of 3 existing fixtures, add 1 new):
```go
const l4OnlyFixture = "../../testdata/flows/small.jsonl"     // existing, reused: zero-AUDIT-signal case
const emptyFixture = "../../testdata/flows/empty.jsonl"       // existing, reused: zero-flows case
const withAuditFixture = "../../testdata/flows/with_audit.jsonl" // NEW: 1 DROPPED + 1 AUDIT
```

**Helper — analog `runReplayPipeline`** (lines 33-65), add an `includeAudit bool` param in place of/alongside `l7Enabled`:
```go
func runReplayPipelineAudit(t *testing.T, fixture string, includeAudit bool) (outDir string, logs *observer.ObservedLogs) {
	t.Helper()
	outDir = t.TempDir()
	logger, observed := newObservedLogger() // existing helper, pipeline_l7_test.go:28, reused as-is

	src, err := flowsource.NewFileSource(fixture, logger)
	require.NoError(t, err)

	cfg := PipelineConfig{
		FlushInterval: 50 * time.Millisecond,
		OutputDir:     outDir,
		Logger:        logger,
		IncludeAudit:  includeAudit,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, RunPipelineWithSource(ctx, cfg, src))
	return outDir, observed
}
```

**Test 1 — analog `TestPipeline_L7Empty_FiresWarning`** (lines 115-146), mirrors exactly, reuses `l4OnlyFixture` (zero AUDIT flows, 3 DROPPED):
```go
func TestPipeline_AuditEmpty_FiresWarning(t *testing.T) {
	_, logs := runReplayPipelineAudit(t, l4OnlyFixture, true)

	matches := 0
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "no AUDIT-verdict flows observed") {
			matches++
			assert.Contains(t, e.Message, "--include-audit")
			fields := e.ContextMap()
			ws, ok := fields["workloads"].([]interface{})
			if !ok {
				if asStrings, okStr := fields["workloads"].([]string); okStr {
					assert.NotEmpty(t, asStrings)
				}
			} else {
				assert.NotEmpty(t, ws, "workloads must be non-empty")
			}
		}
	}
	assert.Equal(t, 1, matches, "AUD-01 must fire exactly once")
}
```

**Test 2 — analog `TestPipeline_L7Disabled_NoWarning`** (lines 150-156):
```go
func TestPipeline_AuditDisabled_NoWarning(t *testing.T) {
	_, logs := runReplayPipelineAudit(t, l4OnlyFixture, false)
	for _, e := range logs.All() {
		assert.NotContains(t, e.Message, "no AUDIT-verdict flows observed",
			"AUD-01 must NOT fire when --include-audit is not set")
	}
}
```

**Test 3 — analog `TestPipeline_L7Disabled_L7FlowsIgnored`** (lines 158-186) — THE byte-identical AC-2 test, needs the new `withAuditFixture`:
```go
func TestPipeline_AuditDisabled_AuditFlowsIgnored(t *testing.T) {
	outDir, logs := runReplayPipelineAudit(t, withAuditFixture, false)

	// Only the DROPPED flow's policy is generated; the AUDIT flow is invisible.
	yamlPath := filepath.Join(outDir, "production", "api-server.yaml")
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err)
	yaml := string(data)
	assert.Contains(t, yaml, "8080", "the DROPPED flow's port must still generate a rule")
	assert.NotContains(t, yaml, "9090", "the AUDIT flow's port must be absent when flag is unset")

	for _, e := range logs.All() {
		assert.NotContains(t, e.Message, "no AUDIT-verdict flows observed",
			"AUD-01 must NOT fire when --include-audit is not set, regardless of AUDIT flow content")
	}
}
```

**Test 4 — analog `TestPipeline_L7Enabled_NoFlows_NoWarning`** (lines 188-200), reuses `emptyFixture`:
```go
func TestPipeline_AuditEnabled_NoFlows_NoWarning(t *testing.T) {
	if _, err := os.Stat(emptyFixture); err != nil {
		t.Skipf("empty fixture missing: %v", err)
	}
	_, logs := runReplayPipelineAudit(t, emptyFixture, true)
	for _, e := range logs.All() {
		assert.NotContains(t, e.Message, "no AUDIT-verdict flows observed",
			"AUD-01 must NOT fire when there are no flows at all")
	}
}
```

**Test 5 — AC-1, analog `TestPipeline_L7HTTP_GeneratedAndEvidence`'s structure** (lines 71-111) but simpler (no L7/evidence assertions needed — just "AUDIT flows generate a policy like DROPPED"), using `withAuditFixture`:
```go
func TestPipeline_AuditIngested_GeneratedLikeDropped(t *testing.T) {
	outDir, _ := runReplayPipelineAudit(t, withAuditFixture, true)

	yamlPath := filepath.Join(outDir, "production", "api-server.yaml")
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err, "policy YAML must exist")
	yaml := string(data)
	assert.Contains(t, yaml, "8080", "DROPPED flow's port")
	assert.Contains(t, yaml, "9090", "AUDIT flow's port must ALSO generate a rule when flag is set")
}
```

---

### Group 7 — New fixture: `testdata/flows/with_audit.jsonl`

**Analog:** `testdata/flows/small.jsonl` (verified format, 3-DROPPED-flow fixture) + `testdata/flows/with_non_dropped.jsonl` (mixed-verdict fixture, DROPPED+FORWARDED).

**Existing fixture line format** (verified, `small.jsonl` line 1):
```json
{"flow":{"time":"2026-04-24T14:00:00Z","verdict":"DROPPED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":8080}}}}
```

**New fixture content (2 lines: 1 DROPPED + 1 AUDIT, per RESEARCH.md Code Example 4 — verified `protojson.Unmarshal` accepts the enum name string directly)**:
```json
{"flow":{"time":"2026-04-24T14:00:00Z","verdict":"DROPPED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":8080}}}}
{"flow":{"time":"2026-04-24T14:00:01Z","verdict":"AUDIT","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":9090}}}}
```
Both flows target the same `production/api-server` workload (same namespace+labels) so both land in the same output YAML file (`production/api-server.yaml`), letting a single test assert "port 8080 present, port 9090 present-or-absent depending on flag" (Group 6, Tests 3 and 5 above).

---

### Group 8 — Docs (`README.md`)

**Analog:** existing `--l7` and `--ignore-protocol` rows in the same two locations.

**Flags table** (lines 105-144, `## Flags` section) — add a row in the "Filtering" block, alongside the existing `--ignore-protocol`/`--ignore-drop-reason` rows (lines 118-124):
```
      --include-audit        Also ingest Verdict_AUDIT flows alongside DROPPED (opt-in).
                             Default: DROPPED-only (pre-v1.6 behavior unchanged).
```

**MCP tool table** (line 506+, `## MCP Server (cpg mcp)` section) — this phase adds no new *tool*, only a new *argument* to the existing `start_session` tool; no table row addition needed, but the `start_session` description/jsonschema (mirrors `mcp_tools.go`'s `IncludeAudit` field's `jsonschema:"..."` string, Group 1 above) is the LLM-facing doc surface — keep both in sync per Pitfall 6.

---

## Shared Patterns

### Pattern A: Boolean-flag 3-hop threading (CLI + MCP → PipelineConfig)
**Source:** `cmd/cpg/commonflags.go` (lines 55, 82, 108) + `cmd/cpg/generate.go`/`replay.go` (`L7Enabled: f.l7,`) + `cmd/cpg/mcp_tools.go` (lines 24, 130) + `pkg/session/session.go` (line 137) + `pkg/session/pipeline_config.go` (line 101)
**Apply to:** all 6 Group-1 files
**Shape:** `cobra.Bool(...)` → struct field → `PipelineConfig{Field: source,}` literal, no validation logic (bool flags are self-validating).

### Pattern B: `FlowSource` interface widening ripple
**Source:** `pkg/flowsource/source.go:15` (interface) + `pkg/hubble/client.go:53`/`pkg/flowsource/file.go:72` (implementations) + `pkg/hubble/pipeline.go:171` (call site)
**Apply to:** all 15 Group-4 test files/locations plus the 3 Group-2 production files
**Shape:** add `includeAudit bool` as the interface's 4th positional param; every implementer either uses it (2 production types) or ignores it (`_ bool`, all test doubles); every call site gains a 4th argument. Compiler-enforced — land in one commit/PR, not incrementally.

### Pattern C: VIS-01-style single post-run warning (NOT a dedup map)
**Source:** `pkg/hubble/pipeline.go:345-351` (unmodified, exact template)
**Apply to:** the new AUD-01 warning block, same file, added immediately after
**Shape:** bare `if cfg.Flag && stats.FlowsSeen > 0 && counterSum == 0 { cfg.Logger.Warn(...) }`, evaluated exactly once after `g.Wait()` returns. **Do not** build a `map[string]struct{}` dedup mechanism (that is `warnedReserved`'s pattern, `aggregator.go:73,483-489`, solving a different per-flow-warning problem — see RESEARCH.md Pitfall 2).

### Pattern D: Diagnostic counter + setter/accessor pair, incremented unconditionally
**Source:** `pkg/hubble/aggregator.go` — `l7Enabled`/`l7HTTPCount` fields (lines 77-88), `SetL7Enabled`/`L7HTTPCount()` (lines 305-321), increment site in `Run()` (lines 381-386)
**Apply to:** `includeAudit`/`auditVerdictCount` fields, `SetIncludeAudit`/`AuditVerdictCount()`, increment site in `Run()`
**Shape:** counter increments on every observed flow matching the condition, regardless of whether the corresponding feature flag is enabled — this is what makes the counter usable by Pattern C's warning gate even before the flag exists in the fast path.

### Pattern E: Single-build verdict/filter-value slice (avoid N-times-repeated conditional)
**Source:** `pkg/hubble/client.go`'s recommended `buildFilters` widening (RESEARCH.md Code Example 1)
**Apply to:** `pkg/hubble/client.go` only (3 `FlowFilter` literals reuse one `verdicts` slice)
**Shape:** compute the conditional value ONCE per function call, reuse the same slice/variable in every literal that needs it — RESEARCH.md's Anti-Patterns section flags the alternative (3 independent `if includeAudit` blocks) as an actual footgun risk ("empty filter = match everything").

### Pattern F: testify value-pinning as the de facto golden test
**Source:** `pkg/hubble/client_test.go`'s `TestBuildFilters_*` (lines 41-89)
**Apply to:** `client_test.go` (extend 4 existing + add 4 new `_WithAudit`), and by extension `pipeline_audit_test.go`'s byte-identical assertions
**Shape:** `assert.Equal(t, []flowpb.Verdict{...exact expected slice...}, filters[i].Verdict)` — cpg has zero golden-file/snapshot infrastructure; exact-value assertions ARE the regression mechanism (RESEARCH.md "Don't Hand-Roll").

## No Analog Found

None. Every file in this phase's scope has an exact same-file or same-directory analog, per RESEARCH.md's "State of the Art" table (`L7Enabled`/`IgnoreProtocols`/`IgnoreDropReasons` each already shipped this identical shape).

## Metadata

**Analog search scope:** `cmd/cpg/`, `pkg/hubble/`, `pkg/flowsource/`, `pkg/session/`, `pkg/policy/testdata/`, `testdata/flows/`, `README.md` — all read directly (no repo-wide grep needed; RESEARCH.md's file:line citations were exhaustive and independently verified against live file contents in this pass).
**Files scanned/read directly:** 19 production/test files + 2 fixture files + README (2 sections) = 22 file reads, zero re-reads of an already-loaded range.
**Pattern extraction date:** 2026-07-22
