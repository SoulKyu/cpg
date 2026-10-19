---
phase: 26-namespace-creation-awareness
verified: 2026-10-05T09:20:00Z
status: passed
score: 19/19 must-haves verified
behavior_unverified: 0
overrides_applied: 0
covered_files:
  - .planning/phases/26-namespace-creation-awareness/26-01-PLAN.md
  - .planning/phases/26-namespace-creation-awareness/26-02-PLAN.md
  - .planning/phases/26-namespace-creation-awareness/26-01-SUMMARY.md
  - .planning/phases/26-namespace-creation-awareness/26-02-SUMMARY.md
  - pkg/hubble/ns_awareness.go
  - pkg/hubble/ns_awareness_test.go
  - pkg/hubble/pipeline_nsaw_test.go
  - pkg/hubble/pipeline.go
  - pkg/hubble/aggregator.go
  - testdata/flows/ns_staggered.jsonl
  - pkg/k8s/ns_watch.go
  - pkg/k8s/ns_watch_test.go
  - cmd/cpg/generate.go
  - cmd/cpg/generate_test.go
  - cmd/cpg/mcp_session_test.go
  - docs/KNOWN_LIMITATIONS.md
covered_digest: "v2:sha256:d516d0b6fce1d3d3358baf166c12b0e246e204f8799dc75e779c47c93cd41867"
---

# Phase 26: Namespace Creation Awareness Verification Report

**Phase Goal:** cpg's batch blind spot becomes visible — namespaces that appear during a run (live watch) or mid-capture (replay heuristic) are reported at end of run with a re-run hint
**Verified:** 2026-10-05T09:20:00Z
**Status:** passed
**Re-verification:** No — initial verification
**Verifier mode:** goal-backward, adversarial; SUMMARY claims cross-checked against actual code and re-executed tests.

## Goal Achievement

### ROADMAP Success Criteria (contract)

| # | Success Criterion | Status | Evidence |
|---|-------------------|--------|----------|
| SC1 | Namespace created during `cpg generate` listed in end-of-run summary with re-run hint; zero new namespaces ⇒ summary byte-identical to v1.6 | ✓ VERIFIED | Watch path: `pkg/hubble/pipeline.go:473-477` precedence branch → `PrintNewNamespacesSummary(stdout, ...)` with `NewNamespacesFromWatch` phrasing; `TestPipelineNewNamespacesWatch` (pipeline_nsaw_test.go:117) asserts `  - namespace tenant-b appeared after run start — re-run to cover it` and absence of replay phrasing. Byte-identity: `PrintNewNamespacesSummary` first statement is `if len(namespaces) == 0 { return }` (ns_awareness.go:100-102); `TestSummaryByteIdentity` (pipeline_nsaw_test.go:71) proves zero NSAW bytes over small.jsonl — **ran green**. |
| SC2 | Watch failure (RBAC, connection) warns once and proceeds — generation never blocked/degraded, readonly posture intact | ✓ VERIFIED | `StartNamespaceWatcher` never returns an error (ns_watch.go:84-114); single `warnOnce` guard (ns_watch.go:168-176) covers List error, List Forbidden, Watch construction error, Watch Forbidden, `watch.Error` event, channel close; `TestNamespaceWatchFailSafe` (5 table cases, wantWarns: 1, inert non-nil watcher, nil `NewNamespaces()`) — **ran green under -race**. Readonly verbs: `grep -nE '\.(Create\|Update\|Delete\|Patch\|Bind\|Evict)\(' pkg/k8s/ns_watch.go` → 0 matches; only `.List(` / `.Watch(` present. |
| SC3 | `cpg replay` reports namespaces whose first observed flow arrives after capture start (flow-timestamp heuristic, no K8s watch) | ✓ VERIFIED | `pkg/hubble/ns_awareness.go` — `noteFirstSeen` (both endpoints, nil/invalid/zero-time skip, min-keep), `computeNewNamespaces` (capture start = min Flow.Time, strict `After`, sorted); `TestPipelineNewNamespaces` replays `testdata/flows/ns_staggered.jsonl` end-to-end via FileSource and asserts exactly `latecomer` with the pinned replay phrasing — **ran green**. Zero K8s connectivity in the replay path (grep: cmd/cpg/replay.go has no k8s references). |
| SC4 | New-namespace reporting rides the v1.3 session-summary mechanism (stdout in CLI mode; no MCP purity regression) | ✓ VERIFIED | Print site at pipeline.go:477 uses the SAME resolved `stdout` local as `PrintClusterHealthSummary` (immediately after it, before `stats.Log`); no MCP-specific branch. MCP mode routes `cfg.Stdout = mcpModeStdout()` = os.Stderr; `TestMCPSessionLifecycleWiringAndStdoutPurity` — **ran green under -race, assertions unchanged** (comment-only provenance addition at mcp_session_test.go:159-164 naming `mcpModeStdout` and `Phase 26`). |

### Observable Truths — Plan 26-01 (NSAW-02)

| # | Truth (abbrev) | Status | Evidence |
|---|----------------|--------|----------|
| 1 | Staggered-fixture replay lists `latecomer` with exact pinned copy under `New namespaces detected during this run:` | ✓ VERIFIED | ns_awareness.go:99-115 (exact copy strings); TestPipelineNewNamespaces **passed** (integration, FileSource → aggregator → compute → print → cfg.Stdout buffer). |
| 2 | Capture start = min observed Flow.Time; strict `time.After`; first-observed namespace never reported | ✓ VERIFIED | ns_awareness.go:66-86 (min over values, `t.After(captureStart)`); TestFirstSeenNamespaces_CaptureStartEquality subtests **passed**. |
| 3 | Zero new namespaces ⇒ byte-identical v1.6 summary; empty AND nil lists write zero bytes | ✓ VERIFIED | ns_awareness.go:100-102 guard; TestPrintNewNamespacesSummary_EmptyAndNil + TestSummaryByteIdentity **passed**. |
| 4 | Nil/invalid/zero Flow.Time skipped; all-untimestamped capture reports nothing (fail-safe empty) | ✓ VERIFIED | ns_awareness.go:41-49 (nil-check, IsValid, IsZero); TestFirstSeenZeroTime (4 subtests) **passed**. |
| 5 | BOTH source and destination endpoint namespaces recorded; empty-name guard | ✓ VERIFIED | ns_awareness.go:51-58; TestFirstSeenNamespaces (source-only mid-capture reported, destination-only at capture start not reported) + TestFirstSeenNamespaces_EmptyNamespaceName **passed**. |
| 6 | Reported list sorted lexicographically — deterministic output | ✓ VERIFIED | sort.Strings in computeNewNamespaces (ns_awareness.go:83) and defensive sort in print (ns_awareness.go:101); TestFirstSeenNamespaces_SortedDeterministic **passed**. |
| 7 | firstSeenByNamespace mutated only in the Run-loop unconditional bookkeeping block (before every filter `continue`), read only via copy-returning accessor after g.Wait(); -race green | ✓ VERIFIED | aggregator.go:478-481 — `noteFirstSeen(a.firstSeenByNamespace, f)` sits after the AUDIT counter and BEFORE the FILTER-01 `continue` (aggregator.go:483+); accessor at aggregator.go:263-267 returns a copy; doc comment mirrors the M-3 contract (aggregator.go:164-174). Full `pkg/hubble` -race suite **green**. |
| 8 | Print rides the resolved `stdout` local immediately after PrintClusterHealthSummary; no MCP-specific branch | ✓ VERIFIED | pipeline.go:471-477 (single call site, same `stdout` local); grep: exactly one non-test `PrintNewNamespacesSummary` call. |
| 9 | FLAGGED ASSUMPTION: namespaces sharing the minimum timestamp all treated as pre-existing | ✓ VERIFIED | Documented in computeNewNamespaces doc comment (ns_awareness.go:70-72) and asserted by TestFirstSeenNamespaces_CaptureStartEquality/equal_to_capture_start_neither_reported — surfaced, not silently dropped. |

### Observable Truths — Plan 26-02 (NSAW-01)

| # | Truth (abbrev) | Status | Evidence |
|---|----------------|--------|----------|
| 1 | Namespace created after run start during generate listed with watch phrasing | ✓ VERIFIED | Precedence branch (pipeline.go:473-475) + TestPipelineNewNamespacesWatch **passed**. |
| 2 | Watch fail-safe: List/Watch construction/RBAC-403/watch.Error/channel-close each ⇒ at most ONE warn, awareness disabled, never an error into runGenerate, exit code unchanged | ✓ VERIFIED | ns_watch.go:84-114 (Forbidden branches), 122-134 (watch.Error), 148-151 (close); single warnOnce guard; TestNamespaceWatchFailSafe (5 cases) **passed**; StartNamespaceWatcher returns only `*NamespaceWatcher` (no error anywhere in the exported API). |
| 3 | Initial-List namespaces never reported even on synthetic ADDED re-delivery | ✓ VERIFIED | ns_watch.go:128-132 (snapshot check); TestNamespaceWatch_RecordsNewOnly **passed** (re-delivered `default` not recorded, only `tenant-new`). |
| 4 | Watcher list sorted, independent of event arrival order | ✓ VERIFIED | ns_watch.go:190-199 (sorted copy); TestNamespaceWatch_SortedCopy **passed** (zeta/alpha → ["alpha","zeta"], independent copies). |
| 5 | Watcher list authoritative when started (watch phrasing); no watcher ⇒ heuristic fallback (replay phrasing); replay never starts a watcher, never constructs a K8s client for NSAW | ✓ VERIFIED | pipeline.go:471-477 precedence; TestPipelineNewNamespacesHeuristicFallback **passed** (nil source ⇒ small.jsonl zero NSAW bytes); TestReplayNeverStartsNamespaceWatcher **passed**; grep: `StartNamespaceWatcher` in cmd/cpg/replay.go = 0, pkg/session/ = 0. |
| 6 | Watcher lifetime bounded by run signal ctx; defer Stop; mutex-guarded collection; -race green, no leak/hang | ✓ VERIFIED | ns_watch.go:82 (ctx param), 103-104 (defer Stop inside consume — confirmed at ns_watch.go:118), mutex-guarded recorded map (121-131, 168-176, 183-199); full `pkg/k8s` -race suite **green**. |
| 7 | MCP stdout purity structurally preserved (report rides cfg.Stdout seam) | ✓ VERIFIED | TestMCPSessionLifecycleWiringAndStdoutPurity **passed unchanged** (-race); provenance comment present (mcp_session_test.go:159-164). |
| 8 | Empty cluster / zero creations ⇒ zero NSAW bytes | ✓ VERIFIED | NewNamespaces() returns nil when nothing recorded (ns_watch.go:186-188) → print guard returns; TestNamespaceWatch_NilSafeAccessors **passed**. |
| 9 | No new CLI flags — always-on for generate | ✓ VERIFIED | generate.go:97-124 launcher with no flag gating; `NewNamespaces: nsWatcher` wired at generate.go:347; no new flags in generate.go. |
| 10 | KNOWN_LIMITATIONS §9 describes the built-in report instead of the workaround | ✓ VERIFIED | docs/KNOWN_LIMITATIONS.md:112-120 — cron/watcher wrapper workaround sentence gone; contains both the generate-watch description (`appeared after run start`) and the replay heuristic description (`first observed after capture start`); Tracking references v1.7 Phase 26. |

**Score:** 19/19 truths verified (0 present-but-behavior-unverified — every behavior-dependent truth is exercised by a passing named test; see Behavioral Spot-Checks)

### Prohibition Verdicts (deterministic evidence; not silent passes)

| Prohibition | Tier | Verdict | Evidence |
|---|---|---|---|
| 26-01: report MUST NOT be written into evidence JSON, cluster-health.json, or policy YAML | judgment | Grounded by deterministic check — report rides stdout seam only | `PrintNewNamespacesSummary` has exactly ONE non-test call site (pipeline.go:477) writing to the resolved `stdout` local; grep of pkg/output/, pkg/policy/ for ns_awareness/NewNamespaces usage = 0 matches. |
| 26-01: replay MUST NOT gain any K8s API client usage | judgment | Grounded by deterministic check | NSAW-02 is pure flow-timestamp logic in pkg/hubble; cmd/cpg/replay.go has zero `k8s.` references; no pkg/session changes. |
| 26-02: generation MUST NOT be blocked/degraded/non-zero-exit on watch failure | judgment | Test-proven | TestNamespaceWatchFailSafe (5 cases) + TestMaybeStartNamespaceWatcher (fail-safe subtests) **passed**; StartNamespaceWatcher never returns error. |
| 26-02: report MUST NOT be filtered by --namespace flag | judgment | Code-verified | Print site inputs are `agg.FirstSeenByNamespace()` / `cfg.NewNamespaces` only (pipeline.go:471-477) — no `cfg.Namespaces` involvement; watcher records cluster-level namespace ADDED events, not flow-filtered namespaces. |
| 26-02: watcher MUST NOT reconnect, retry, or outlive the run | judgment | Code-verified | ns_watch.go:49-51 doc + grep: zero reconnect/retry/backoff/sleep patterns; ctx-bounded consume loop; deferred Stop. |
| 26-02: MUST NOT use K8s verbs other than namespaces list/watch | judgment | Grep-proven (T-26-06) | `grep -nE '\.(Create\|Update\|Delete\|Patch\|Bind\|Evict)\(' pkg/k8s/ns_watch.go` → 0 matches. |

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `pkg/hubble/ns_awareness.go` | NewNamespaceSource consts, noteFirstSeen, computeNewNamespaces, PrintNewNamespacesSummary | ✓ VERIFIED | All symbols present; substantive (115 lines); wired (pipeline.go:477). |
| `pkg/hubble/ns_awareness_test.go` | Unit tests: first-seen, zero-time, print copy, capture-start equality, sorting, empty-name | ✓ VERIFIED | 8 test functions present; all **passed**. |
| `pkg/hubble/pipeline_nsaw_test.go` | TestPipelineNewNamespaces, TestSummaryByteIdentity, watch/fallback tests | ✓ VERIFIED | 4 test functions present; all **passed**. |
| `pkg/hubble/aggregator.go` | firstSeenByNamespace field + FirstSeenByNamespace() copy accessor + pre-filter recording | ✓ VERIFIED | Field @174, accessor @263, recording @478-481 before FILTER-01 continue @483+. |
| `pkg/hubble/pipeline.go` | NewNamespacesSource interface + PipelineConfig.NewNamespaces + print site | ✓ VERIFIED | Interface @47-49, field @134, precedence branch + print @471-477. |
| `testdata/flows/ns_staggered.jsonl` | 3 lines: default/production at 14:00, latecomer at 14:10 | ✓ VERIFIED | Content matches spec exactly (verdict DROPPED, RFC3339 times, both endpoints). |
| `pkg/k8s/ns_watch.go` | NamespaceWatcher, StartNamespaceWatcher, NewNamespaces, warn consts | ✓ VERIFIED | All symbols present; fail-safe contract doc; substantive (199 lines); readonly verbs only. |
| `pkg/k8s/ns_watch_test.go` | RecordsNewOnly, SortedCopy, FailSafe (5 cases), NilSafeAccessors | ✓ VERIFIED | All 4 test functions present; all **passed** under -race. |
| `cmd/cpg/generate.go` | maybeStartNamespaceWatcher + call site + PipelineConfig wiring | ✓ VERIFIED | Definition @111, call @306 (after version preflight, before RunPipeline), wiring @347. |
| `cmd/cpg/generate_test.go` | TestMaybeStartNamespaceWatcher, TestReplayNeverStartsNamespaceWatcher | ✓ VERIFIED | Both present; **passed**. |
| `cmd/cpg/mcp_session_test.go` | Comment-only Phase 26 provenance, assertions unchanged | ✓ VERIFIED | Provenance comment @159-164 contains `mcpModeStdout` and `Phase 26`; test **passed** unchanged. |
| `docs/KNOWN_LIMITATIONS.md` | §9 rewritten to describe the built-in report | ✓ VERIFIED | §9 @112-120 matches spec. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| aggregator Run loop bookkeeping | `noteFirstSeen(a.firstSeenByNamespace, f)` | recorded before ignoreDropReasons/ignoreProtocols continues | ✓ WIRED | aggregator.go:478-481, FILTER-01 continue at :491. |
| RunPipelineWithSource post-g.Wait() | PrintNewNamespacesSummary + FirstSeenByNamespace accessor | same resolved `stdout` local as PrintClusterHealthSummary | ✓ WIRED | pipeline.go:471-477. |
| runGenerate (after version preflight) | hubble.PipelineConfig.NewNamespaces | `maybeStartNamespaceWatcher(ctx, ...)` + `NewNamespaces: nsWatcher` | ✓ WIRED | generate.go:306, 347. |
| NSAW print site | watcher-precedence branch | `cfg.NewNamespaces != nil` → watch list + watch hint; else heuristic + replay hint | ✓ WIRED | pipeline.go:471-477; both phrasings proven by tests. |
| watcher goroutine | k8s watch result channel | watch.Added, snapshot-gated; watch.Error + close → single warn | ✓ WIRED | ns_watch.go:118-151. |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|--------------|--------|--------------------|--------|
| PrintNewNamespacesSummary (replay) | nsList | `agg.FirstSeenByNamespace()` — populated from actual replayed flows via noteFirstSeen | Yes (FileSource fixture flows) | ✓ FLOWING |
| PrintNewNamespacesSummary (watch) | nsList | `cfg.NewNamespaces.NewNamespaces()` — NamespaceWatcher recorded map from real watch ADDED events | Yes (fake-clientset events in tests; client-go watch in production) | ✓ FLOWING |

No static fallbacks, no hardcoded lists, no hollow props.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| NSAW-02 unit + integration suite (26-01 Task 1/2 filters) | `go test ./pkg/hubble/ -run 'TestFirstSeenNamespaces\|TestFirstSeenZeroTime\|TestPrintNewNamespacesSummary\|TestPipelineNewNamespaces\|TestSummaryByteIdentity' -race -count=1` | ok 1.080s, 16/16 subtests pass, no `no tests to run` | ✓ PASS |
| NSAW-01 watcher suite | `go test ./pkg/k8s/ -run 'TestNamespaceWatch' -race -count=1` | ok 1.206s | ✓ PASS |
| Generate wiring + replay guard + MCP purity | `go test ./cmd/cpg/ -run 'TestMaybeStartNamespaceWatcher\|TestReplayNeverStartsNamespaceWatcher\|TestMCPSessionLifecycleWiringAndStdoutPurity' -race -count=1` | ok 3.115s | ✓ PASS |
| Affected packages full -race | `go test ./pkg/hubble/ ./pkg/k8s/ ./cmd/cpg/ -count=1 -race` (cmd/cpg with the two documented OOM tests skipped) | ok (hubble 4.8s, k8s 2.2s, cmd/cpg 100.3s) | ✓ PASS |
| Documented OOM tests non-race | `GOGC=30 GOMEMLIMIT=1800MiB go test ./cmd/cpg/ -run 'TestMCPAuditReadonlyReachability\|TestAuditWindowNotReachableFromMCP'` | both pass (16.1s / 17.7s) | ✓ PASS |
| Full repo suite (phase gate) | `GOGC=30 GOMEMLIMIT=1800MiB go test ./... -count=1 -p 1` | exit 0, zero FAIL lines | ✓ PASS |

Note: on this memory-constrained host (≈2.4 GB available) the two documented SSA/RTA tests are OOM-killed even non-race at default GC settings; they pass with `GOGC=30 GOMEMLIMIT=1800MiB`, consistent with the documented environmental deviation (pre-existing at baseline 8fc2b68, Phase 23 tests — not a Phase 26 regression).

### Probe Execution

No probe scripts declared by the plans and no conventional `scripts/*/tests/probe-*.sh` files exist for this phase. The plans' verification mechanism is the Go test suite (executed above). SKIPPED (no probes to run).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| NSAW-01 | 26-02 | During `cpg generate`, watch namespace-creation events and list at end of run every namespace created after run start | ✓ SATISFIED | Watcher + wiring + precedence branch + watch-phrasing tests (SC1/SC2 evidence above). |
| NSAW-02 | 26-01 | On `replay`, report namespaces whose first observed flow arrives after capture start (flow-timestamp heuristic, no K8s watch) | ✓ SATISFIED | ns_awareness.go + aggregator recording + staggered-fixture integration test (SC3 evidence above). |

**Orphaned requirements:** none — REQUIREMENTS.md maps exactly NSAW-01 and NSAW-02 to Phase 26; both are claimed (26-02 and 26-01 plans respectively) and both satisfied. No later-phase deferrals.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | — | No TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER markers in any phase-modified file | — | — |
| (none) | — | No stub returns, empty handlers, or hardcoded empty data flowing to output | — | — |

### Human Verification Required

None. All roadmap SCs and plan must-haves are machine-checkable by design and were executed green by this verifier. Consistent with Phase 25 precedent, fake-clientset watch-reactor coverage is the project's accepted verification mechanism for client-go integrations; no cluster-in-the-loop test was required by the roadmap contract.

### Gaps Summary

No gaps. Both plans' must-haves are implemented, wired, and test-proven in the actual codebase; all prohibitions hold with deterministic evidence; both requirement IDs are satisfied; the full repository test suite passes. Residual (informational, not a gap): live behavior against a real Kubernetes API server (real watch delivery semantics, real kubeconfig loading, a genuinely RBAC-restricted service account) is validated only through client-go's own interface contract and fake-clientset tests, per project convention.

---

_Verified: 2026-10-05T09:20:00Z_
_Verifier: Claude (gsd-verifier)_
