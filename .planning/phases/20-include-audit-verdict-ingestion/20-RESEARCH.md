# Phase 20: `--include-audit` Verdict Ingestion - Research

**Researched:** 2026-07-22
**Domain:** Go CLI + readonly MCP stdio server — widening an existing Hubble verdict filter/classification pipeline (cpg, Cilium Policy Generator) to opt-in ingest `Verdict_AUDIT` flows alongside `Verdict_DROPPED`
**Confidence:** HIGH — every claim below is either a direct read of cpg's own repo at HEAD (`6894007`, zero Go-file diff since the milestone research commit `2fbef25`) or a direct read of the vendored `github.com/cilium/cilium@v1.19.4` module source. No `[ASSUMED]` claims were needed for this phase's core scope (see Assumptions Log).

## Summary

This phase is a mechanical, well-precedented widening of an existing filter/classification pipeline, not new architecture. cpg already threads three boolean-ish flags (`L7Enabled`, `IgnoreProtocols`, `IgnoreDropReasons`) through the identical path — `cobra flag → commonFlags/generateFlags struct → PipelineConfig field` (CLI) and `startSessionArgs → session.StartArgs → buildPipelineConfig → PipelineConfig field` (MCP) — and `--include-audit`/`include_audit` is a fourth instance of that exact shape. The milestone-level research (`.planning/research/{ARCHITECTURE,PITFALLS,STACK}.md`) correctly identified this and pre-enumerated 5 verdict-filter sites; this phase-level pass re-verified all 5 against the live repo (unchanged, file:line exact) and ran the exhaustive re-grep AC-04 demands. Result: **no 6th filter site exists**, but there is a materially larger blast radius hiding inside "5 sites" that the milestone research undercounted — `pkg/flowsource.FlowSource` is an interface, and widening its signature to carry an `includeAudit bool` ripples through **19 distinct locations** (2 production implementations, 1 interface definition, 1 production call site, 7 test-double implementations across 4 test files, and 8 direct test call-sites) before a single line of filtering logic is written. This is not a red flag — it's a purely mechanical, compiler-enforced ripple (Go will not compile until every implementation matches) — but it means the phase's true diff size is larger than "5 sites" implies, and the planner should size tasks accordingly.

The single most valuable finding of this research pass is a pre-existing, byte-for-byte-reusable test template: `pkg/hubble/pipeline_l7_test.go` implements the *exact* pattern AC-02/AC-03 require — a `runReplayPipeline` helper driving `RunPipelineWithSource` against a fixture file with an observed zap logger, then asserting (a) a feature-specific CNP is generated, (b) a one-shot warning fires exactly once when the flag is set and the expected signal is absent, (c) the warning never fires when the flag is unset, and (d) flag-unset output is byte-identical regardless of fixture content (`TestPipeline_L7Disabled_L7FlowsIgnored`). This is the literal, file-and-line template for AC-02's "byte-identical" and AC-03's "exactly one warning" tests — no new test infrastructure needs inventing, and two of the five recommended AUDIT tests can reuse *existing* fixtures (`small.jsonl` for the zero-signal-warning case, `empty.jsonl` for the zero-flows case) with only one new fixture (`with_audit.jsonl`) required.

One precision correction against the milestone-level research: `.planning/research/PITFALLS.md` (Pitfall 8) recommends composing the new warning with "the existing VIS-01 one-shot-warning machinery/pattern (e.g. `warnedReserved`-style dedup-by-key map in `aggregator.go`)". Direct code reading shows this conflates two *different* existing mechanisms: `warnedReserved` is a per-key dedup map used because a *per-flow* warning could otherwise fire many times per session; the actual VIS-01 L7 warning (`pipeline.go:345-351`) is not a dedup map at all — it's a single aggregate check performed exactly once, after `g.Wait()` returns, over session-total counters. The AUDIT warning must mirror the *latter* (single post-run check), not the former (per-key map) — building a dedup map here would be unnecessary and would diverge from the actual VIS-01 precedent it claims to follow.

**Primary recommendation:** Thread `IncludeAudit bool` through the exact `L7Enabled` plumbing shape (CLI flag → `commonFlags`/`generateFlags` → `PipelineConfig`; MCP arg → `StartArgs` → `buildPipelineConfig` → `PipelineConfig`), widen the 5 pre-enumerated verdict-filter sites plus update the `FlowSource` interface signature and its 18 dependent locations, add an `Aggregator.auditVerdictCount` counter mirroring `l7HTTPCount`, and add a VIS-01-shaped single post-run warning block immediately after the existing L7 one in `pipeline.go`. Test using the exact `pipeline_l7_test.go` template with a new sibling file `pipeline_audit_test.go`.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| AUD-01 | Operator can ingest `Verdict_AUDIT` flows into policy generation via `--include-audit` (`generate`/`replay`) and `include_audit` (MCP `start_session`); default behavior stays byte-identical (regression-tested); a single VIS-01-style warning fires when the flag is set but zero AUDIT flows arrive | Full flag-plumbing path traced file:line (Integration Point 1 below); all 5 verdict-filter sites + the `FlowSource` interface ripple enumerated exhaustively; existing `pipeline_l7_test.go` identified as the exact reusable test template for byte-identical + single-warning requirements; `Verdict_AUDIT` existence and semantics VERIFIED directly against vendored `flow.pb.go` |

**Sub-criteria traceability** (phase description's 4 numbered Success Criteria, all under AUD-01):

| Sub-AC | Behavior | Research Support |
|--------|----------|------------------|
| AC-1 | AUDIT flows classified/aggregated/generate policies exactly like DROPPED | Integration Point 1 (gate widening); "Verified unchanged" list (dropclass, dedup, policy builder all key off `DropReasonDesc`, not `Verdict`) |
| AC-2 | Flag-unset output byte-identical to pre-v1.6, regression-tested | `TestPipeline_L7Disabled_L7FlowsIgnored` template (exact reusable pattern); existing `TestBuildFilters_*` tests already pin `Verdict` values and only need a `false`-arg extension |
| AC-3 | Exactly one warning when flag set + zero AUDIT arrive, never a storm | VIS-01 single post-`g.Wait()` check pattern (pipeline.go:345-351), corrected against PITFALLS.md's imprecise `warnedReserved` cross-reference |
| AC-4 | All 5 sites + any additional site found by exhaustive re-grep, widened | Exhaustive re-grep performed this session (see Verdict-Filter Site Inventory) — confirms exactly 5 filtering sites, 2 additional non-filtering pass-through sites, no 6th filter site |
</phase_requirements>

## Architectural Responsibility Map

cpg is a single-binary Go CLI + MCP stdio server, not a multi-tier web app — the generic Browser/SSR/API/CDN/DB tiers don't apply. Mapping instead to cpg's own established layers (verified via `ARCHITECTURE.md`'s System Overview + this session's direct reads):

| Capability | Primary Layer | Secondary Layer | Rationale |
|------------|---------------|-----------------|-----------|
| `--include-audit` / `include_audit` flag surface | CLI entrypoint (`cmd/cpg/{generate,replay,mcp_tools}.go`) | — | User-facing input parsing; owns validation-free bool passthrough (cobra `bool` flags need no validation) |
| Verdict filter widening (gRPC + replay) | Flow source (`pkg/hubble/client.go`, `pkg/flowsource/file.go`) | `pkg/flowsource.FlowSource` interface | Owns "what flows even reach the pipeline" — the only layer that can exclude AUDIT flows before they're seen at all |
| Classification gate widening + AUDIT counter | Aggregation (`pkg/hubble/aggregator.go`) | — | Owns per-flow classification (Infra/Transient/Noise/Policy) and session-level diagnostic counters; already verified to key off `DropReasonDesc`, not `Verdict`, for everything downstream of the gate |
| Zero-signal warning | Pipeline orchestration (`pkg/hubble/pipeline.go`) | — | Owns session-level post-run aggregate checks (VIS-01 precedent lives here, not in the aggregator) |
| Config threading (CLI + MCP → pipeline) | `pkg/session` (MCP) / `cmd/cpg` (CLI) | `pkg/hubble.PipelineConfig` | Both entrypoints converge on one config struct — no divergent code path to keep in sync |
| Policy generation, dedup, evidence, output | `pkg/policy`, `pkg/evidence`, `pkg/output` | — | **Unchanged** — verified these layers key off `DropReasonDesc`/labels, never `Verdict`, so AUDIT flows flow through identically to DROPPED once past the gate |
| MCP structural readonly proof (SEC-01) | `cmd/cpg/mcp_audit_test.go` | — | **Unaffected** — this phase adds zero new K8s write verbs and zero new filesystem-write call sites (verified: `k8sWriteVerbs`/`fsWriteAllowlist` maps are keyed by verb/function name, untouched by a struct-field addition) |

## Standard Stack

**This phase requires zero new dependencies.** Every capability is already reachable through `github.com/cilium/cilium@v1.19.4`, a direct dependency since v1.0. Confirmed by direct read of `go.mod` (below) — agrees with `.planning/research/STACK.md`'s milestone-level finding ("v1.6 needs zero new `go.mod` require lines").

### Core (existing, reused — no version changes)
| Library | Version | Purpose in this phase | Provenance |
|---------|---------|------------------------|------------|
| `github.com/cilium/cilium/api/v1/flow` (flowpb) | v1.19.4 (pinned in `go.mod:8`) | `flowpb.Verdict_AUDIT` (=4) and `flowpb.Verdict_DROPPED` (=2) enum constants | `[VERIFIED: direct read of $(go env GOMODCACHE)/github.com/cilium/cilium@v1.19.4/api/v1/flow/flow.pb.go:417-461]` |
| `go.uber.org/zap` | v1.27.1 (`go.mod:15`) | Structured warning log (mirrors VIS-01 exactly) | `[VERIFIED: go.mod]` |
| `github.com/spf13/cobra` | v1.10.2 (`go.mod:13`) | `--include-audit` flag registration (mirrors `--l7`) | `[VERIFIED: go.mod]` |
| `github.com/modelcontextprotocol/go-sdk` | v1.6.1 (`go.mod:11`) | `include_audit` MCP arg via existing `startSessionArgs` struct + jsonschema tag | `[VERIFIED: go.mod]` |
| `github.com/stretchr/testify` | v1.11.1 (`go.mod:14`) | `assert`/`require` — the "golden test" mechanism this repo actually uses (see below) | `[VERIFIED: go.mod]` |

### Alternatives Considered
None applicable — there is no library decision to make in this phase; it is pure application-logic wiring of an already-vendored enum value.

**Installation:** None. `go build`/`go test` against the existing `go.mod` — no `go get`, no `go mod tidy` changes expected.

## Package Legitimacy Audit

**N/A — this phase installs zero new external packages.** No `go.mod` `require` additions, no new indirect promotions. The Package Legitimacy Gate protocol is not triggered; `slopcheck`/registry verification were not run because there is nothing to verify. If a future planning pass discovers a need for a new package, re-run this gate before proceeding — but as researched, no such need exists for AUD-01.

## Architecture Patterns

### System Architecture Diagram

```
                    ┌─────────────────────┐        ┌──────────────────────────┐
                    │ CLI: cpg generate/  │        │ MCP: start_session       │
                    │ replay --include-   │        │ {include_audit: true}    │
                    │ audit               │        │ (cmd/cpg/mcp_tools.go)   │
                    └──────────┬──────────┘        └────────────┬─────────────┘
                               │                                │
                    commonFlags/generateFlags          startSessionArgs.IncludeAudit
                               │                                │
                               ▼                                ▼
                               │                     session.StartArgs.IncludeAudit
                               │                     (pkg/session/session.go)
                               │                                │
                               │                     buildPipelineConfig()
                               │                     (pkg/session/pipeline_config.go)
                               │                                │
                               └───────────┬────────────────────┘
                                           ▼
                          hubble.PipelineConfig.IncludeAudit  [NEW FIELD]
                                           │
                                           ▼
                          RunPipelineWithSource(ctx, cfg, source)
                          (pkg/hubble/pipeline.go:170)
                                           │
                    ┌──────────────────────┼───────────────────────────┐
                    ▼ (site 1-3: gRPC)                                 ▼ (site 4: replay)
        source.StreamDroppedFlows(                          source.StreamDroppedFlows(
          ctx, ns, allNS,                                      ctx, ns, allNS,
          cfg.IncludeAudit)  [4th param — INTERFACE CHANGE]     cfg.IncludeAudit)
                    │                                                 │
        pkg/hubble/client.go:198,209,213                  pkg/flowsource/file.go:116
        buildFilters(): Verdict:                           if !(Verdict==DROPPED ||
          [DROPPED] or [DROPPED,AUDIT]                        (includeAudit && Verdict==AUDIT))
                    │                                                 │ { skip, count NonDroppedSkipped }
                    └──────────────────────┬──────────────────────────┘
                                           ▼
                              flows chan *flowpb.Flow
                                           │
                                           ▼
                          agg.Run(ctx, flows, policies, healthCh)
                          (pkg/hubble/aggregator.go:361)
                              │
                              ├─ auditVerdictCount++ if Verdict==AUDIT  [NEW, diagnostic,
                              │                                          unconditional — mirrors
                              │                                          l7HTTPCount/l7DNSCount]
                              │
                              ▼ site 5: classification gate (aggregator.go:417)
                    if (Verdict==DROPPED || (includeAudit && Verdict==AUDIT))
                       && DropReasonDesc != UNKNOWN {
                          dropclass.Classify() → Infra/Transient/Noise: suppress + healthCh
                                                → Policy/Unknown: fall through
                    }
                              │
                              ▼ (UNCHANGED downstream — verified keys off DropReasonDesc, not Verdict)
                    keyFromFlow() → buckets → policy.BuildPolicy() → PolicyEvent
                              │
                    ┌─────────┼──────────────────┐
                    ▼         ▼                  ▼
              output.Writer  evidence.Writer  healthWriter
              (CNP YAML)     (per-rule)       (cluster-health.json)
                              │
                              ▼ (post g.Wait(), pipeline.go:340+)
              VIS-01 (existing, unchanged): L7Enabled && FlowsSeen>0 && L7Count==0 → warn
              AUD-01 (NEW, same shape):     IncludeAudit && FlowsSeen>0 && AuditVerdictCount==0 → warn ONCE
```

**Flag-unset path (the byte-identical requirement):** with `IncludeAudit=false`, sites 1-4 never deliver an AUDIT-verdict flow to the aggregator at all — the classification gate's widened condition is never even exercised for a real AUDIT flow, `auditVerdictCount` stays 0, the new warning block's `IncludeAudit` guard is false so it never evaluates the rest of the condition, and every line downstream is byte-for-byte the pre-v1.6 code path. The byte-identical property is enforced structurally by the upstream filter, not by careful downstream neutrality — this is the important thing the regression test must prove.

### Verdict-Filter Site Inventory (exhaustive re-grep performed this session, AC-04)

Ran `grep -rn "Verdict"` (and separately `"DROPPED"`, `"Verdict_AUDIT"`) across all non-vendor `pkg/` and `cmd/` Go files (test and non-test). Result: **exactly 5 filtering sites, matching the milestone research's pre-enumeration precisely — no 6th site exists.** Two additional non-filtering pass-through sites were found and are documented below for completeness (AC-04 requires the re-grep to be exhaustive, not that every hit be a filter).

| # | Site | File:line | Current (v1.5) | v1.6 widening |
|---|------|-----------|------------------|----------------|
| 1 | gRPC filter, all-namespaces | `pkg/hubble/client.go:198` | `{Verdict: []flowpb.Verdict{flowpb.Verdict_DROPPED}}` | Append `Verdict_AUDIT` when `includeAudit` |
| 2 | gRPC filter, SourcePod | `pkg/hubble/client.go:209` | same | same |
| 3 | gRPC filter, DestinationPod | `pkg/hubble/client.go:213` | same | same |
| 4 | Replay per-line gate | `pkg/flowsource/file.go:116` | `if f.Verdict != flowpb.Verdict_DROPPED { skip }` | `if !(f.Verdict == DROPPED \|\| (includeAudit && f.Verdict == AUDIT)) { skip }` |
| 5 | Aggregator classifier gate | `pkg/hubble/aggregator.go:417` | `if f.Verdict == flowpb.Verdict_DROPPED && f.GetDropReasonDesc() != DROP_REASON_UNKNOWN` | widen the `Verdict ==` half identically |

**Non-filtering pass-through sites found (no change required, documented for completeness):**

| Site | File:line | What it does | Why no change needed |
|------|-----------|---------------|------------------------|
| Evidence schema field | `pkg/evidence/schema.go:93` | `Verdict string` struct field | Generic string capture; will correctly show `"AUDIT"` once ingested — a downstream UX benefit, not a required edit |
| Evidence writer capture | `pkg/hubble/evidence_writer.go:130` | `Verdict: f.GetVerdict().String()` | Same — generic `.String()` call, verdict-agnostic |
| MCP query tool field | `cmd/cpg/mcp_query_flows.go:54,345` | `Verdict string` in `DroppedFlowSample` (list_dropped_flows tool) | Same — an LLM querying a session post-AUD-01 will correctly see `"verdict": "AUDIT"` in results with zero code change |

**Confirmed clean (checked explicitly per PITFALLS.md's own worry list, zero Verdict coupling found):** `pkg/policy/dedup.go` (`PoliciesEquivalent` — compares built CNP specs, no flow/Verdict input at all), `pkg/explain/filter.go` (`Filter` struct has Direction/Port/PeerLabel/PeerCIDR/Since/L7 fields — no Verdict field), `cmd/cpg/commonflags.go` (flag validation — no verdict-related flag exists today), `pkg/output/annotate.go`.

### The hidden ripple: `FlowSource` interface signature change (AC-04's "including any additional site")

The milestone research flagged this as "not in the draft's table but load-bearing" — this session's exhaustive grep confirms and fully enumerates it. `pkg/flowsource.FlowSource` (`source.go:14-16`) is an **interface**:
```go
type FlowSource interface {
	StreamDroppedFlows(ctx context.Context, namespaces []string, allNS bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error)
}
```
Widening filtering requires a 4th parameter (`includeAudit bool`). Because Go enforces interface satisfaction at compile time, this signature change has a **mechanical, unavoidable, all-or-nothing** blast radius of 19 locations:

| Category | Count | Locations |
|----------|-------|-----------|
| Interface definition | 1 | `pkg/flowsource/source.go:15` |
| Production implementations | 2 | `pkg/hubble/client.go:53` (`Client`), `pkg/flowsource/file.go:72` (`FileSource`) |
| Production call site | 1 | `pkg/hubble/pipeline.go:171` (`RunPipelineWithSource`) |
| Test-double implementations (must add the 4th param to compile) | 7 | `pkg/hubble/pipeline_test.go:32` (`mockFlowSource`), `:304` (`errStreamSource`), `:362` (`errStreamSourceWithInfraDrop`), `:453` (`channelFlowSource`); `pkg/session/manager_test.go:35` (`closedFlowSource`), `:55` (`blockingFlowSource`); `pkg/flowsource/source_test.go:13` (`stubSource`) |
| Direct test call-sites (must add a 4th arg) | 8 | `pkg/flowsource/file_test.go:42,59,70,81,100,122,147,159` |

This is a compiler-enforced, low-risk-of-silent-error ripple (the build fails until every site is updated) — but it means the phase's real diff touches roughly a dozen files beyond the 5 conceptual filter sites, and task/plan sizing should reflect that. None of these 19 locations require new logic beyond adding the parameter (all but the 2 production implementations simply ignore it, `_ bool`).

### Threading path (verified, mirrors 3 prior shipped features exactly)

Both entrypoints converge on one `hubble.PipelineConfig` struct — confirmed by direct reads of `cmd/cpg/generate.go:225-256`, `cmd/cpg/replay.go:100-130`, and `pkg/session/pipeline_config.go:58-109`. `L7Enabled`/`IgnoreProtocols`/`IgnoreDropReasons` all thread through the identical shape; `IncludeAudit` is a fourth instance:

```
CLI:  cobra flag (commonflags.go: f.Bool("include-audit", false, "..."))
        → commonFlags.includeAudit (parseCommonFlags)
        → hubble.PipelineConfig{ IncludeAudit: f.includeAudit, ... }   (generate.go / replay.go)

MCP:  startSessionArgs.IncludeAudit `json:"include_audit,omitempty"`   (mcp_tools.go)
        → session.StartArgs{ IncludeAudit: args.IncludeAudit, ... }   (mgr.Start call)
        → buildPipelineConfig(): hubble.PipelineConfig{ IncludeAudit: args.IncludeAudit, ... }
```

No new plumbing pattern needs inventing — this is the same 3-hop shape as `L7`/`IgnoreProtocols`/`IgnoreDropReasons`, verified at every hop.

### Pattern: VIS-01-style single post-run warning (verified template, `pipeline.go:340-351`)

```go
// Source: pkg/hubble/pipeline.go:345-351 (existing, unmodified — the exact template)
if cfg.L7Enabled && stats.FlowsSeen > 0 && agg.L7HTTPCount()+agg.L7DNSCount() == 0 {
    cfg.Logger.Warn("--l7 set but no L7 records observed in window",
        zap.Strings("workloads", agg.ObservedWorkloads()),
        zap.Uint64("flows", stats.FlowsSeen),
        zap.String("hint", "see README L7 prerequisites: #l7-prerequisites"),
    )
}
```
This check runs exactly once, after `g.Wait()` returns — it is inherently single-fire by construction (no per-flow loop, no dedup map needed). The AUDIT warning should be added immediately after this block, in the identical shape:
```go
// Recommended addition, same location, same shape:
if cfg.IncludeAudit && stats.FlowsSeen > 0 && agg.AuditVerdictCount() == 0 {
    cfg.Logger.Warn("--include-audit set but no AUDIT-verdict flows observed in window",
        zap.Strings("workloads", agg.ObservedWorkloads()),
        zap.Uint64("flows", stats.FlowsSeen),
    )
}
```
**Correction against milestone-level research:** `.planning/research/PITFALLS.md` (Pitfall 8) suggests composing this with "the existing... `warnedReserved`-style dedup-by-key map (`aggregator.go`)". Direct reading shows `warnedReserved` (`aggregator.go:73,483-489`) solves a *different* problem — deduplicating a warning that could otherwise fire once per matching flow within a single `Run()` (many reserved-identity flows could arrive in one session). VIS-01's L7 warning is not per-flow at all; it is a single aggregate check. The AUDIT warning is also a single aggregate check (session-total `AuditVerdictCount`), so it must mirror VIS-01's actual mechanism (a bare `if` after `g.Wait()`), not `warnedReserved`'s per-key map. Using a dedup map here would be both unnecessary and a structural mismatch with the stated precedent.

### Pattern: Aggregator counter + setter (verified template, `aggregator.go:76-88,305-321`)

```go
// Existing L7 counter pattern (aggregator.go:81-88, 305-321) — the exact template:
// l7HTTPCount counts flows carrying a non-nil Flow.L7.Http record, incremented
// unconditionally in Run() (aggregator.go:381-386), independent of l7Enabled —
// "diagnostic counter, powers VIS-01 regardless of whether L7 codegen is on."
func (a *Aggregator) SetL7Enabled(enabled bool) { a.l7Enabled = enabled }
func (a *Aggregator) L7HTTPCount() uint64 { return a.l7HTTPCount }
```
Recommended: `SetIncludeAudit(bool)`, `auditVerdictCount uint64` field, `AuditVerdictCount() uint64` accessor, incremented unconditionally in `Run()` right alongside the existing `l7HTTPCount`/`l7DNSCount` increments (`aggregator.go:381-386`) — i.e. on every flow observed from the channel, before any ignore-protocol/ignore-drop-reason filtering, mirroring the L7 counters' documented rationale exactly ("regardless of whether the flow makes it into a bucket"). Because sites 1-4 already exclude AUDIT flows entirely when `includeAudit=false`, this counter is structurally guaranteed to stay 0 when the flag is unset — no additional guard needed inside `Run()` itself.

### Anti-Patterns to Avoid
- **A generic `[]Verdict` allowlist config.** AUD-01 and the existing PROJECT.md Key Decision ("DROPPED-only verdict filter (kept)... REFUSED gap deferred") both scope this narrowly to `{DROPPED, AUDIT}`. Do not build a general verdict-inclusion list that also invites FORWARDED/ERROR/REDIRECTED/TRACED/TRANSLATED — there is no requirement for them and `REFUSED` handling is a *separately tracked, still-deferred* item (L7-FUT-01); conflating the two in one generalized mechanism would silently expand scope beyond AUD-01.
- **A new dedup-map warning mechanism.** See the VIS-01 correction above — mirror the single post-run check, not `warnedReserved`.
- **Extending `buildFilters` with 3 independent conditional literals.** Building the `Verdict` slice 3 separate times (once per `FlowFilter` in `client.go:195-217`) triples the chance one call site's conditional is written slightly differently than the other two — the exact "empty filter = match everything" footgun PITFALLS.md warns about (Pitfall 8). Build one local `verdicts []flowpb.Verdict` slice once per `buildFilters` call and reuse it in all 3 filter literals.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Golden/snapshot regression testing | A new golden-file-diffing framework or `testdata/*.golden` convention | Direct `assert.Equal`/`assert.Contains` value-pinning tests (testify) | cpg has **no existing golden-file infrastructure** anywhere in the repo (confirmed by grep — the only "golden" hits are code comments about *test ordering*, not a file-diffing mechanism). The 4 existing `TestBuildFilters_*` tests in `client_test.go` already pin exact `Verdict` slice values — extending them with a `false` 4th arg **is** cpg's de facto golden test for AC-2; no new mechanism needed |
| Verdict-to-string mapping | A custom `map[Verdict]string` or switch | `flowpb.Verdict(...).String()` / `f.GetVerdict().String()` (already used at `pkg/hubble/evidence_writer.go:130`) | Protobuf-generated `.String()` already exists and is already the pattern in use |
| One-shot session warning | A new dedup mechanism | The exact VIS-01 post-`g.Wait()` check shape (`pipeline.go:345-351`) | Already proven, already single-fire by construction, zero new state needed |
| Test flow fixtures | Hand-rolled `*flowpb.Flow{}` literals from scratch | `pkg/policy/testdata.IngressTCPFlow`/`EgressUDPFlow` helpers, then explicitly set `.Verdict = flowpb.Verdict_AUDIT` (mirrors `aggregator_test.go`'s `makePolicyFlow()` pattern, which does the identical override for DROPPED) | Established helpers already produce well-formed flows for the label/policy layer; only `Verdict`/`DropReasonDesc` need explicit override since these helpers deliberately leave them unset |

**Key insight:** This phase's entire toolkit already exists in the codebase, proven by 3 prior shipped features (`L7Enabled`, `IgnoreProtocols`, `IgnoreDropReasons`) and one prior test suite (`pipeline_l7_test.go`) that solves a structurally identical problem (opt-in flag + one-shot zero-signal warning + byte-identical-when-off guarantee). The risk in this phase is under-using precedent, not needing new tools.

## Common Pitfalls

### Pitfall 1: "5 sites" undersells the true diff — the `FlowSource` interface ripple
**What goes wrong:** A task/plan sized around "5 verdict checks to widen" will surprise-fail CI when 7 test-double types and 8 direct test call-sites don't compile.
**Why it happens:** The interface signature change is a single, correct, necessary edit (`source.go:15`) but Go's static typing propagates it everywhere the interface is implemented or called with positional args, including test-only code that has no filtering logic of its own.
**How to avoid:** Budget the task list explicitly for all 19 locations enumerated above, not just the 5 conceptual gates. Most of the 19 are one-line, `_ bool`-style mechanical edits.
**Warning signs:** `go build ./...` failing on `does not implement flowsource.FlowSource` for a test type after only editing `client.go`/`file.go`.

### Pitfall 2: Conflating the VIS-01 warning mechanism with `warnedReserved`
**What goes wrong:** Building a per-key dedup map for the AUDIT warning (as the milestone-level PITFALLS.md's wording could be read to suggest) adds unneeded state and, if built incorrectly, could actually let the warning fire more than once per session (a map is only "single-fire" if the key is chosen so all AUDIT-related warnings collide into a fixed number of keys, which is the more complex, less obviously-correct design here).
**Why it happens:** The milestone-level research cross-referenced two structurally different existing mechanisms as if they were the same "VIS-01 one-shot machinery."
**How to avoid:** Copy `pipeline.go:345-351`'s exact shape — a bare `if` after `g.Wait()`, no map, no per-flow hook. See Architecture Patterns above for the corrected recommendation.
**Warning signs:** A new `map[string]struct{}` field on `Aggregator` for tracking "have I warned about audit yet" — unnecessary; the check only ever runs once regardless.

### Pitfall 3: `FileSourceStats.NonDroppedSkipped` semantic drift
**What goes wrong:** `pkg/flowsource/file.go`'s replay gate (site 4) currently counts every non-DROPPED verdict (including what will become AUDIT-when-flag-set) under `stats.nonDroppedSkipped`. If the widened gate isn't written carefully, an AUDIT flow could either (a) still increment `NonDroppedSkipped` even when actually emitted (double-counting confusion in the replay summary log), or (b) never increment it even when correctly skipped (flag unset) — either breaks the existing `TestFileSourceFiltersNonDropped` invariant.
**Why it happens:** The counter's positioning (`file.go:116-119`) sits directly inside the same `if` that decides skip-vs-emit — a straightforward widen keeps this correct automatically, but a careless refactor (e.g. moving the counter increment before checking `includeAudit`) would not.
**How to avoid:** Keep the counter increment inside the `else`/skip branch of the widened condition, never in a separate check. Verified: `with_non_dropped.jsonl` (the existing fixture for this test) contains only `DROPPED`/`FORWARDED` verdicts — zero AUDIT — so this existing test is unaffected either way and remains a valid regression guard.
**Warning signs:** `TestFileSourceFiltersNonDropped`'s asserted count (`2`) changing without a fixture change.

### Pitfall 4: AUDIT flows might not always carry a populated `DropReasonDesc` on every Cilium version/deployment — genuinely open, not disqualifying
**What goes wrong:** The aggregator's widened classification gate (site 5) only applies Infra/Transient/Noise suppression when `DropReasonDesc != DROP_REASON_UNKNOWN`. The vendored proto comment (`flow.pb.go:425-427`) explicitly documents this fidelity for `DROPPED` ("the exact drop reason may be found in drop_reason_desc") but the `AUDIT` comment (`flow.pb.go:431-433`, "used... to denominate flows that would have been dropped by policy if audit mode was turned off") does not make the same explicit guarantee for `drop_reason_desc` population.
**Why it happens:** This is a datapath/runtime behavior question, not a Go-source-reading question — it cannot be fully settled by reading vendored Go code, only by observing a real cluster's AUDIT-mode traffic. `[CITED: github.com/cilium/cilium/issues/42044]` documents a related, confirmed metadata-population gap for policy-log fields on DROPPED/AUDIT flows in Cilium v1.18.2-v1.19.0 (cpg's pinned dependency, v1.19.4, sits just past this range) — a different field (`policy_log`/derived-from labels, not `drop_reason_desc`), but it corroborates that non-FORWARDED verdict metadata population has had real, version-specific gaps in this exact Cilium era.
**How to avoid:** This is **not a blocking risk** — if `DropReasonDesc` is `UNKNOWN` on some AUDIT flow, the widened gate condition is simply false for that flow, which means it skips classification and falls straight through to `keyFromFlow()` → gets bucketed → generates a policy anyway (the existing, safe fallback behavior for any DROPPED flow with an unknown reason today). No flow is silently lost either way. Flag this as a documented, low-severity edge case (code comment near the widened gate) rather than something requiring a design change.
**Warning signs:** N/A for this phase — only relevant if a future phase needs Infra/Transient suppression to be *guaranteed* accurate for AUDIT flows specifically.

### Pitfall 5: Scope creep into `REFUSED`/other verdicts
**What goes wrong:** `pkg/hubble/client_test.go` and PROJECT.md's own Key Decisions table record a deliberate, still-standing decision: "DROPPED-only verdict filter (kept) | REDIRECTED means Cilium PROXIED; new rules from already-policied traffic would be wrong | Good — REFUSED gap deferred to v1.3 (L7-FUT-01)". `REFUSED` handling remains a separately-tracked, still-open future item, not part of AUD-01.
**Why it happens:** Once a bool-flag pattern for one extra verdict exists, it's tempting to generalize to "any verdict" in the same PR.
**How to avoid:** Keep the change scoped to exactly `{DROPPED, AUDIT}` per the requirement text; do not build a general verdict allowlist (see Anti-Patterns above).
**Warning signs:** A PR touching `REFUSED`/`REDIRECTED`/`ERROR` handling under an AUD-01 commit message.

### Pitfall 6: Doc drift — README + jsonschema strings not updated in the same change
**What goes wrong:** README.md has a dedicated `## Flags` reference table (line 105) and a `## MCP Server (cpg mcp)` section (line 506) documenting every existing flag/arg including `--l7`; `startSessionArgs`' jsonschema tags (`mcp_tools.go:21-32`) are the LLM-facing tool-schema description. Landing `--include-audit`/`include_audit` without updating both is the same "third copy drifts" problem PITFALLS.md's Pitfall 10 warns about for a different phase (SKL work) — it applies here too, at a smaller scale.
**How to avoid:** Update the README `## Flags` table, the MCP tool section, and the `jsonschema:"..."` tag on the new `startSessionArgs.IncludeAudit` field in the same task/commit as the code change.
**Warning signs:** `--include-audit` working but absent from `cpg generate --help` docs comparison or the README table.

## Code Examples

### 1. `buildFilters` widening (single local variable, avoids the 3x-conditional footgun)
```go
// Source: pkg/hubble/client.go:192-217 (current) — recommended widened shape:
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
One `verdicts` slice built once, reused in all filter literals — structurally impossible for one call site to drift from the others.

### 2. Aggregator classification gate widening (site 5)
```go
// Source: pkg/hubble/aggregator.go:412-417 (current) — recommended widened condition:
// HEALTH-01/05 + AUD-01: applies to flows with an explicit DROPPED (or, when
// includeAudit, AUDIT) verdict and a non-zero drop reason. Zero-value
// DropReasonDesc on non-DROPPED/non-AUDIT flows must pass through unmodified.
if (f.Verdict == flowpb.Verdict_DROPPED || (a.includeAudit && f.Verdict == flowpb.Verdict_AUDIT)) &&
	f.GetDropReasonDesc() != flowpb.DropReason_DROP_REASON_UNKNOWN {
	class := dropclass.Classify(f.GetDropReasonDesc())
	// ... unchanged switch below
}
```

### 3. Test template — mirror `pipeline_l7_test.go` exactly (new file `pipeline_audit_test.go`)
```go
// Source pattern: pkg/hubble/pipeline_l7_test.go:33-65 (runReplayPipeline helper)
// and :113-146 (TestPipeline_L7Empty_FiresWarning) — extend the helper with an
// includeAudit param, then mirror each of the 5 L7 tests for AUDIT:
func runReplayPipelineAudit(t *testing.T, fixture string, includeAudit bool) (outDir string, logs *observer.ObservedLogs) {
	t.Helper()
	outDir = t.TempDir()
	logger, observed := newObservedLogger() // existing helper, pipeline_l7_test.go:28

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

// TestPipeline_AuditEmpty_FiresWarning: reuses the EXISTING l4OnlyFixture
// (testdata/flows/small.jsonl, 3 DROPPED-only flows, zero AUDIT) — no new
// fixture needed for this test.
//
// TestPipeline_AuditDisabled_NoWarning / _AuditFlowsIgnored: needs ONE new
// fixture, testdata/flows/with_audit.jsonl (1 DROPPED + 1 AUDIT flow,
// same minimal shape as small.jsonl), asserting flag-off output is byte-
// identical to what the DROPPED flow alone would produce.
//
// TestPipeline_AuditEnabled_NoFlows_NoWarning: reuses EXISTING emptyFixture.
```

### 4. New fixture format (matches existing `testdata/flows/*.jsonl` convention exactly)
```json
{"flow":{"time":"2026-04-24T14:00:00Z","verdict":"DROPPED","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":8080}}}}
{"flow":{"time":"2026-04-24T14:00:01Z","verdict":"AUDIT","traffic_direction":"INGRESS","source":{"labels":["k8s:app=client"],"namespace":"default"},"destination":{"labels":["k8s:app=api-server"],"namespace":"production"},"l4":{"TCP":{"destination_port":9090}}}}
```
Verified: `protojson.Unmarshal` (used by `pkg/flowsource/file.go:106`) accepts the enum name string (`"DROPPED"`, `"AUDIT"`) directly, matching every existing fixture's convention.

## State of the Art

Not applicable in the usual "industry evolved" sense — this is an internal, incremental widening of cpg's own established pattern, not an external-ecosystem shift. The relevant "evolution" is cpg's own feature history:

| Feature | Shipped | Threading shape |
|---------|---------|-------------------|
| `L7Enabled` | v1.2 (Phase 7-9) | `--l7` → `commonFlags.l7` → `PipelineConfig.L7Enabled` → `agg.SetL7Enabled` |
| `IgnoreProtocols` | v1.3 (Phase 13, "PA5") | `--ignore-protocol` → same shape |
| `IgnoreDropReasons` | v1.3 (Phase 13, "FILTER-01") | `--ignore-drop-reason` → same shape |
| `IncludeAudit` (this phase) | v1.6 (Phase 20) | `--include-audit` → same shape (4th instance) |

**No deprecated/outdated concerns** — the pattern being extended is the current, actively-used one, not a legacy path being replaced.

## Assumptions Log

No claims in this research required an `[ASSUMED]` (training-data, unverified-in-session) tag — every substantive finding was verified either by direct repo/vendored-module source reads this session, or is drawn verbatim from the phase's own requirement text (e.g. the `--include-audit`/`include_audit` naming is specified in AUD-01 itself, not inferred). The one genuine open question (AUDIT flows' `DropReasonDesc` fidelity on a real cluster) is investigated with citations (vendored proto comment + a real GitHub issue) rather than assumed from training data, and is tracked in Open Questions below, not here.

**This table is empty:** all claims in this research were verified or cited — no user confirmation needed before planning.

## Open Questions (RESOLVED)

1. **Does `DropReasonDesc` populate with the same fidelity on `Verdict_AUDIT` flows as on `Verdict_DROPPED` flows, across the Cilium versions cpg targets?**
   - RESOLVED: operationalized as a mandated code comment near the widened gate in 20-01-PLAN.md Task 2's `<action>`, and tracked as a Manual-Only Verification in 20-VALIDATION.md (live-cluster spot-check, non-blocking).
   - What we know: The vendored proto explicitly documents this fidelity for `DROPPED` only (`flow.pb.go:427`); the `AUDIT` comment describes semantics ("would have been dropped if audit mode was off") but not an explicit metadata guarantee. `[CITED: github.com/cilium/cilium/issues/42044]` confirms a related (not identical) metadata-population gap for policy-log fields affecting both DROPPED and AUDIT flows in Cilium v1.18.2-v1.19.0.
   - What's unclear: Whether `drop_reason_desc` specifically (not `policy_log`) has ever had a similar gap for AUDIT flows on any version in cpg's supported range.
   - Recommendation: Not blocking — see Pitfall 4. The fallback behavior (gate condition false → flow still bucketed and generates a policy) is safe. Worth a one-line code comment near the widened gate; no design change needed. If a real `hubble observe --output jsonpb` capture against a cluster in audit mode is available during phase execution, a quick manual spot-check of `drop_reason_desc` population would upgrade this from "open" to "confirmed," but is not required to ship AUD-01.

2. **Should `Aggregator.AuditVerdictCount()` be fully surfaced through `SessionStats`/`StopResult` (MCP-visible), mirroring `L7HTTPCount`/`L7DNSCount` exactly?**
   - RESOLVED: deliberately deferred at planning — discretionary, not required by any AC. Documented in 20-PATTERNS.md Group 1; a follow-up plan can mirror the L7 plumbing exactly if MCP-visible symmetry is wanted later.
   - What we know: `L7HTTPCount`/`L7DNSCount` are plumbed all the way to `SessionStats` (`pipeline.go:118-123`), `SessionStats.Log()` (`pipeline.go:149-150`), and `session.StopResult` (`session.go:198-199`) — full symmetry with the counter this phase adds would suggest doing the same for `AuditVerdictCount`.
   - What's unclear: None of the 4 numbered Success Criteria explicitly require MCP/CLI visibility into the count itself (only that the warning fires correctly) — this is a "nice symmetry" choice, not a requirement.
   - Recommendation: Left to planner/implementer discretion. Full plumbing costs ~4 small, low-risk edits (mirroring an established pattern exactly) and improves observability for an LLM operator inspecting `stop_session` results; omitting it does not violate any stated AC. Given the pattern is trivial to mirror and the codebase's own convention strongly favors symmetry (every other diagnostic counter added alongside a VIS-01-style warning has been fully surfaced), recommend doing it — but flag as discretionary, not mandatory.

## Environment Availability

Step 2.6: SKIPPED (no external dependencies identified). This phase is a pure application-logic change to an already-running Hubble gRPC pipeline and an already-vendored dependency (`github.com/cilium/cilium`) — it introduces no new tool, service, runtime, or CLI dependency. The existing Hubble Relay connectivity requirement is unchanged from pre-v1.6 behavior.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` + `github.com/stretchr/testify` v1.11.1 (`assert`/`require`) + `go.uber.org/zap/zaptest/observer` for log-content assertions |
| Config file | None — plain `go test`, no separate test-runner config |
| Quick run command | `rtk proxy go test ./pkg/hubble/... ./pkg/flowsource/... ./pkg/session/... ./cmd/cpg/... -count=1 -race` |
| Full suite command | `rtk proxy go test ./... -count=1 -race` (matches `Makefile:8-9`'s `test` target exactly) |

Note (environment-specific, from prior session memory): the sandbox denies `go` invoked via `make`; invoke `go test` directly (optionally via `rtk proxy`) rather than through `make test` in this environment.

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|--------------------|--------------|
| AUD-01 (AC-1) | AUDIT flows classified/aggregated/generate policies like DROPPED | integration | `go test ./pkg/hubble/... -run TestPipeline_AuditIngested_GeneratedLikeDropped -race` | ❌ Wave 0 — new file `pipeline_audit_test.go`, mirrors `TestPipeline_L7HTTP_GeneratedAndEvidence` |
| AUD-01 (AC-2) | Flag-unset output byte-identical (regression) | unit + integration | `go test ./pkg/hubble/... -run 'TestBuildFilters_|TestPipeline_AuditDisabled_AuditFlowsIgnored' -race` | ⚠️ Partial — `TestBuildFilters_*` (4 tests) exist in `client_test.go` and need a `false` 4th-arg extension (not new tests, extended signatures); the byte-identical E2E test is ❌ Wave 0 |
| AUD-01 (AC-3) | Exactly one warning, zero AUDIT arrived | unit | `go test ./pkg/hubble/... -run TestPipeline_AuditEmpty_FiresWarning -race` | ❌ Wave 0 — mirrors `TestPipeline_L7Empty_FiresWarning` (`pipeline_l7_test.go:115-146`), reuses existing `l4OnlyFixture` |
| AUD-01 (AC-4) | All 5 sites + any additional found by re-grep, widened | unit (compile-time) | `go build ./...` (interface satisfaction) + `go test ./pkg/hubble/... ./pkg/flowsource/... -run TestFlowSourceInterfaceSatisfied -race` | ⚠️ Partial — `TestFlowSourceInterfaceSatisfied` exists (`source_test.go:17-19`) and will fail to compile until `stubSource` gets the 4th param; the re-grep itself is a research/verification step, not a test (already performed this session — see Verdict-Filter Site Inventory) |

### Sampling Rate
- **Per task commit:** `rtk proxy go test ./pkg/hubble/... ./pkg/flowsource/... ./pkg/session/... ./cmd/cpg/... -count=1 -race`
- **Per wave merge:** `rtk proxy go test ./... -count=1 -race`
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `pkg/hubble/pipeline_audit_test.go` (new file) — covers AC-1, AC-3; mirrors `pipeline_l7_test.go` structure 1:1
- [ ] `testdata/flows/with_audit.jsonl` (new fixture, 2 lines: 1 DROPPED + 1 AUDIT) — covers AC-2's byte-identical assertion
- [ ] Extend `pkg/hubble/client_test.go`'s 4 existing `TestBuildFilters_*` functions with a `false` 4th arg (byte-identical proof) + 4 new sibling `_WithAudit` variants (widened-behavior proof)
- [ ] Extend all 7 test-double `StreamDroppedFlows` implementations + 8 `file_test.go` call-sites with the 4th param (compile-time requirement, not new test logic)

*(No framework install needed — `testify`/`zaptest` already a direct dependency.)*

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V2 Authentication | No | Phase adds no auth surface |
| V3 Session Management | No | No new session semantics — reuses existing `pkg/session` lifecycle unchanged |
| V4 Access Control | No | No new RBAC/permission surface — reuses the existing Hubble Relay connection |
| V5 Input Validation | Trivially yes | `--include-audit` is a cobra `bool` flag (no malformed-input surface beyond true/false); `include_audit` MCP arg is validated by the go-sdk's existing JSON-schema type coercion (same mechanism as `L7`, `TLS`, `ClusterDedup` bools today) — no new validation code needed |
| V6 Cryptography | No | Not applicable |

### Known Threat Patterns for this stack
None apply specifically to this phase. This is an additive, opt-in boolean toggle over an already-open, already-authenticated gRPC stream (Hubble Relay) and an already-open replay file path — it introduces no new deserialization surface (the same `protojson.Unmarshal`/gRPC `Recv()` paths already handle arbitrary `Verdict` enum values today; nothing new is parsed), no new K8s RBAC verb, and no new filesystem-write call site.

**SEC-01 structural readonly proof — explicitly unaffected.** Verified by reading `cmd/cpg/mcp_audit_test.go`: `TestMCPAuditReadonlyReachability` (line 222) checks reachability against two static maps, `k8sWriteVerbs` (line 66) and `fsWriteAllowlist` (line 91), both keyed by function/verb identity. Adding a `bool` field to `StartArgs`/`PipelineConfig` and widening existing conditionals introduces zero new function calls into either map's domain — this phase requires **no changes** to `mcp_audit_test.go` and should not touch it.

## Sources

### Primary (HIGH confidence — direct source reads this session)
- cpg repo at HEAD `6894007` (zero Go-file diff vs. milestone-research commit `2fbef25`, confirmed via `git diff --stat`) — every file:line citation above: `pkg/hubble/{client,aggregator,pipeline}.go`, `pkg/flowsource/{source,file}.go`, `pkg/session/{session,pipeline_config,manager}.go`, `cmd/cpg/{generate,replay,mcp_tools,commonflags,mcp_query_flows,mcp_audit_test}.go`, all corresponding `*_test.go` files, `testdata/flows/*.jsonl`, `README.md`, `Makefile`, `go.mod`, `.planning/PROJECT.md`
- `$(go env GOMODCACHE)/github.com/cilium/cilium@v1.19.4/api/v1/flow/flow.pb.go:417-461` — `Verdict` enum definition, `Verdict_AUDIT = 4`, `Verdict_DROPPED = 2`, and their doc comments, read directly

### Secondary (MEDIUM confidence)
- `[CITED: github.com/cilium/cilium/issues/42044]` — "Policy log does not work for DROPPED/AUDIT flow", confirms a real (different-field) metadata-population gap for non-FORWARDED verdicts in Cilium v1.18.2-v1.19.0, used only to corroborate Pitfall 4's caution, not as a blocking finding
- `.planning/research/{SUMMARY,ARCHITECTURE,PITFALLS,STACK,FEATURES}.md` — milestone-level research, cross-checked and re-verified against live code this session; one precision correction applied (VIS-01 vs. `warnedReserved` mechanism, see Summary)

### Tertiary (LOW confidence)
None used for this phase's core findings.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new dependencies, verified directly against `go.mod`
- Architecture: HIGH — every integration point and every one of the 19 `FlowSource` blast-radius locations verified by direct grep + read against live repo HEAD
- Pitfalls: HIGH for all code-level claims (direct reads); MEDIUM for the one genuinely empirical question (AUDIT `DropReasonDesc` fidelity on a live cluster), which is correctly flagged as open rather than asserted

**Research date:** 2026-07-22
**Valid until:** Stable — this research is tied to cpg's own repo state (verified zero-diff at HEAD) and the pinned `cilium@v1.19.4` module; re-verify only if either changes before planning executes (30-day nominal validity for a fast-moving repo, but the underlying facts are code-verified, not time-sensitive external claims).
