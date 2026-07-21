---
phase: 17-session-lifecycle
verified: 2026-07-21T08:10:00Z
status: gaps_found
score: 3/5 must-haves verified
overrides_applied: 0
gaps:
  - truth: "LLM calls get_status(session_id) at any point and receives coarse state — capturing/stopped, elapsed time, artifact file counts on disk"
    status: partial
    reason: "Session.State is only ever transitioned to StateStopped inside Manager.Stop's stopOnce.Do closure (pkg/session/manager.go:339-350). The background pipeline's terminal error, returned by m.runPipeline in Start's launch goroutine (manager.go:195-199), is sent onto s.done and then silently discarded by every consumer (Stop's bounded wait at manager.go:341-345, Shutdown's bounded wait at manager.go:383-387 both only use the channel receive to unblock a select, never inspect the value). If the pipeline exits on its own before stop_session is ever called (relay connection reset, auth expiry, an unreachable/typo'd --server address, network blip), Session.State never leaves StateCapturing, so get_status reports \"capturing\" indefinitely for a session whose background goroutine has already exited — directly contradicting the tool's own description (\"Poll get_status to check progress\", cmd/cpg/mcp_tools.go:82), since there is no progress and no signal available from get_status to detect it. Independently re-derived from source (not just taken from .planning/phases/17-session-lifecycle/17-REVIEW.md WR-01, though that report reaches the same conclusion): confirmed no test in manager_test.go exercises a real (non-context-cancellation) pipeline error, and StopResult (session.go:155-179) has no error/failure field, so even an eventual stop_session call cannot distinguish a crashed session from a clean one beyond smaller counters."
    artifacts:
      - path: "pkg/session/manager.go"
        issue: "Start's launch goroutine (~195-199) discards the pipeline's terminal error; the only writer of Session.State = StateStopped is Stop()'s stopOnce.Do (~339-350) — an autonomous pipeline exit never updates State, so a dead session still reports \"capturing\"."
    missing:
      - "Capture the pipeline's terminal error (e.g. atomic.Pointer[error] on Session, alongside the existing `final` field) instead of discarding it in the launch goroutine."
      - "Transition State (or add a third state distinct from capturing/stopped) when the pipeline goroutine exits on its own, so get_status reflects reality without requiring an explicit stop_session call, and surface the captured error on StatusResult/StopResult so a crashed session is distinguishable from a clean one."
  - truth: "Killing the transport for any reason (stdin EOF, harness crash) during an active session cancels the session context, closes the port-forward, and removes the tmpdir — each step bounded by its own deadline so one wedged cleanup cannot block process exit"
    status: partial
    reason: "Manager.Shutdown() only cancels sessionCtx := context.WithCancel(m.rootCtx) (manager.go:130, via s.cancel at manager.go:382). Start's synchronous setup phase (resolveSetupFn — kubeconfig load, port-forward, cluster-dedup) is bounded by a structurally SEPARATE context tree, setupCtx := context.WithTimeout(reqCtx, timeout) (manager.go:160), which Shutdown never touches — cancelling sessionCtx has zero effect on setupCtx since they share no parent Shutdown can reach. Independently confirmed by re-deriving both context trees directly from source (not just trusting 17-REVIEW.md WR-02's claim) and by reading TestManager_Start_ShutdownRacesSetup (manager_test.go:563-625): its fake resolveSetupFn blocks on a plain, ctx-independent channel (`<-release`, line 574) that ONLY the test manually closes (closeRelease() at line 605) after already asserting Shutdown returned — the test structurally cannot prove setupCtx is cancelled by Shutdown, because the fake never inspects ctx at all. Compounding: k8s.LoadKubeConfig() (called inside resolveSetup at manager.go:218 and again at manager.go:237 for cluster-dedup) accepts no context parameter whatsoever, so it is not bounded by setupCtx's own timeout either — an existing upstream helper limitation, not introduced by this phase, but load-bearing here. Net effect: if a transport-kill/SIGTERM lands while a Start() call is genuinely stuck inside resolveSetupFn (e.g. a hung kubeconfig exec-credential plugin — a scenario this same codebase's own SEC-03 requirement text explicitly names as a real operational risk), the already-created (os.MkdirTemp'd) session tmpdir and any already-opened port-forward are reached by neither Shutdown's cleanup (tmpDir is still \"\" at that point — Start only assigns s.TmpDir at finalize) nor by Start's own finalize-guard cleanup (that code only runs once resolveSetupFn eventually returns, which is not guaranteed to happen before the process exits, since Shutdown/process-exit does not wait for that goroutine). This can leave an orphaned, empty cpg-session-* directory — and potentially a detached port-forward/exec-plugin subprocess — past process exit, contradicting this criterion's literal \"removes the tmpdir... bounded... one wedged cleanup cannot block process exit\" promise for this specific, realistic race window. Pre-existing, already-documented, unaddressed finding (17-REVIEW.md WR-02), independently re-confirmed here with additional detail (the unbounded k8s.LoadKubeConfig call)."
    artifacts:
      - path: "pkg/session/manager.go"
        issue: "setupCtx (line 160) is derived from reqCtx, not from sessionCtx/m.rootCtx, so Shutdown's s.cancel() (which only cancels sessionCtx) never reaches an in-flight Start's synchronous setup phase; k8s.LoadKubeConfig (lines 218, 237) additionally accepts no ctx at all."
    missing:
      - "Derive setupCtx from sessionCtx (or merge both, e.g. context.AfterFunc(sessionCtx, setupCancel)) so Shutdown's cancellation actually aborts a mid-setup resolveSetupFn call — see 17-REVIEW.md WR-02 for a concrete fix sketch."
      - "Consider an upper bound on MCP-supplied timeout/flush_interval (17-REVIEW.md WR-03) — compounds WR-02's severity, since setupCtx's own timeout is otherwise the only backstop and is currently unbounded above zero."
human_verification: []
---

# Phase 17: Session Lifecycle Verification Report

**Phase Goal:** An LLM can start, monitor, and stop a live Hubble capture session through MCP tools, with the process robustly cleaning up on every exit path.
**Verified:** 2026-07-21T08:10:00Z
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth (ROADMAP Success Criterion) | Status | Evidence |
|---|---|---|---|
| 1 | `start_session` returns opaque `session_id`; background goroutine on detached cancellable ctx; ephemeral `os.MkdirTemp` tmpdir; second `start_session` while active is rejected (actionable, names active session), never queued/silently replaced | ✓ VERIFIED | `pkg/session/manager.go` `Start()` (104-202): slot claimed under `m.mu` before slow setup (TOCTOU-safe), `sessionCtx, sessionCancel := context.WithCancel(m.rootCtx)` (line 130, never `reqCtx`), `os.MkdirTemp("", "cpg-session-*")` (154), reject with `"session %s already running (started %s ago); call stop_session first"` (109-112, names `active.ID`). Independently re-ran `go test ./pkg/session/... -race -count=1 -v`: 28/28 pass. `TestManager_Start_ConcurrentStartRejectsSecond` stress-tested by me 25× under `-race` (125 sub-runs, 0 failures). `TestMCPSessionLifecycleWiringAndStdoutPurity` (cmd/cpg) independently re-run: proves a real `session_id` + on-disk tmpdir over the actual MCP wire. |
| 2 | `get_status(session_id)` returns coarse state — capturing/stopped, elapsed time, artifact file counts on disk | ✗ FAILED (partial) | Mechanism works for the designed capturing→stopped-via-stop_session path (`Status()`, manager.go:267-310, copy-under-lock, independently re-verified `-race` clean). **Gap:** `Session.State` is only ever set to `StateStopped` inside `Stop()`'s `stopOnce.Do` (339-350) — a pipeline that exits **on its own** (not via `stop_session`) never transitions the state, so `get_status` reports `"capturing"` forever for a dead session. See gaps YAML for full evidence trail (independently re-derived, corroborates `17-REVIEW.md` WR-01). |
| 3 | `stop_session(session_id)`: pipeline ctx cancelled, artifacts finalized (`cluster-health.json`, session stats), final summary returned | ✓ VERIFIED | `Stop()` (manager.go:318-359): `s.cancel()` inside `stopOnce.Do`, bounded wait on `s.done` (Pitfall F/G honored — lock released before wait), `s.buildSummary(...)` returns real `SessionStats`-derived counters. Independently re-ran `TestManager_Stop`: `FlowsSeen == 2` (proves `OnFinal` fed the summary through a *real* `RunPipelineWithSource` run, not a mock), tmpdir retained after stop (D-01), idempotent second stop (`TestManager_Stop_Idempotent`) returns identical summary + `AlreadyStopped: true`. Minor info-tier note: `cluster-health.json` is conditionally skipped by the pre-existing (pre-Phase-17) health writer when zero infra/transient drops are observed — `ClusterHealthPath` is always a valid computed path, but the file itself may legitimately not exist; this is inherited, tested, unrelated-to-Phase-17 behavior (`pkg/hubble/health_writer.go`), not a regression. Also note `17-REVIEW.md` WR-04 (unrecognized future `DropReason` values collapse to one map key) — real but low-severity, does not block goal achievement. |
| 4 | Killing the transport (stdin EOF, harness crash) cancels the session ctx, closes the port-forward, removes the tmpdir — each step bounded so one wedged cleanup cannot block process exit | ✗ FAILED (partial) | Fully proven for the primary/designed scenario — an **active capturing session** killed via transport death: `Shutdown()` (manager.go:367-405) bounded two-stage fan-out, independently re-verified via `TestManager_Shutdown_WedgedStepDoesNotBlock` (a wedged pipeline that never observes ctx still exits bounded) and `TestMCPSessionLifecycleWiringAndStdoutPurity` (live MCP-wire test: real tmpdir created, then removed after ctx-cancel+drain, zero stdout leakage). **Gap:** `Shutdown()`'s cancellation cannot reach a `Start()` call still inside its *synchronous setup phase* (kubeconfig/port-forward/cluster-dedup) — `setupCtx` is rooted in `reqCtx`, not `sessionCtx`/`m.rootCtx`, so a transport-kill landing in that window can orphan an empty tmpdir (and possibly a port-forward/exec-plugin subprocess) past process exit. See gaps YAML for full evidence trail (independently re-derived, corroborates `17-REVIEW.md` WR-02, plus an additional finding: `k8s.LoadKubeConfig` takes no ctx at all). |
| 5 | Any session-scoped tool called with an unknown or already-stopped `session_id` returns a crisp "session not found or expired" error, never a generic failure | ✓ VERIFIED (with documented wording supersession) | "Unknown id" fully proven at both unit level (`TestManager_UnknownSessionID`) and MCP-wire level (`TestMCPSessionUnknownIDReturnsIsError`, independently re-run: `IsError == true`, content contains "not found or expired" for both `get_status` and `stop_session`). **"Already-stopped" clause:** the implementation deliberately does NOT error for a retained-stopped session — `get_status`/`stop_session` succeed normally (`TestManager_Status_StoppedSessionStaysQueryable`). This is a documented, **human-approved** architecture decision (D-02 in `17-CONTEXT.md`, resolved via an explicit "User's choice" Q&A recorded in `17-DISCUSSION-LOG.md` predating planning) made to resolve a genuine tension between this criterion's literal wording and QRY-04 (Phase 18, needs to read a stopped session's artifacts). ROADMAP.md/REQUIREMENTS.md text was not updated to reflect D-02 — recommend reconciling the wording (see Gaps Summary), but the actual behavior is intentional, tested, and coherent, not a functional defect. |

**Score:** 3/5 truths fully verified (Truths 2 and 4 FAILED — partial, see gaps)

### Required Artifacts

| Artifact | Expected | Status | Details |
|---|---|---|---|
| `pkg/hubble/pipeline.go` | `OnFinal func(SessionStats)` nil-safe hook (D-08) | ✓ VERIFIED | Field at line 101 (after `Stdout`, line 93); call site `if cfg.OnFinal != nil { cfg.OnFinal(*stats) }` at lines 336-338, after stats population (317-330), before VIS-01 gate — value-copy dereference confirmed. |
| `pkg/hubble/pipeline_test.go` | Fires-once + nil-safe coverage | ✓ VERIFIED | `TestRunPipeline_OnFinalFiresOnce`, `TestRunPipeline_OnFinalNilSafe` present; re-ran `-race`: 2/2 pass. |
| `pkg/session/session.go` | State model, result shapes, `buildSummary` | ✓ VERIFIED | 219 lines; `StateCapturing`/`StateStopped`, `Session`, `StartArgs`, `StartResult`/`StatusResult`/`StopResult`, `buildSummary` all present and match spec exactly. |
| `pkg/session/pipeline_config.go` | `defaultDuration` + `buildPipelineConfig` | ✓ VERIFIED | 109 lines; crash guard (`d <= 0` → fallback) and session-tmpdir-scoped recipe confirmed field-for-field against `cmd/cpg/generate.go`'s recipe, D-05 exclusions (`DryRun`/`FailOnInfraDrops`) absent (`rg` confirms zero matches). |
| `pkg/session/session_test.go` / `pipeline_config_test.go` | Coverage | ✓ VERIFIED | `TestState_String`, `TestSession_BuildSummary`, `TestDefaultDuration`, `TestBuildPipelineConfig` present; re-ran: all pass. |
| `pkg/session/manager.go` | `Manager` (Start/Status/Stop/Shutdown/resolveSetup/countGlob) | ✓ VERIFIED (with the two logic gaps above) | 411 lines. All 6 required functions present. TOCTOU-safe slot claim, copy-under-lock reads, D-10 correlation log, `sync.Once` idempotent stop, D-01 retention all independently confirmed via direct code reading. |
| `pkg/session/manager_test.go` | `-race` suite, SESS-01..06 | ✓ VERIFIED | 657 lines, 16 test functions — all present and independently re-run `-race`, including a 25× stress run of the 5 highest-risk concurrency tests (125 sub-runs, 0 failures, 0 data races). |
| `cmd/cpg/mcp_tools.go` | `registerSessionTools`, 3 handlers, arg validation | ✓ VERIFIED | 148 lines. `validateIgnoreProtocols`/`validateIgnoreDropReasons` reused verbatim (D-06); `rg -n 'IsError'` → 0 matches (no hand-built error results); duration parsing rejects `<= 0`. |
| `cmd/cpg/mcp.go` | Manager wiring + `Shutdown()` after `Run` | ✓ VERIFIED | `session.NewManager(ctx, logger, mcpModeStdout(), version)` (line 94) uses `runMCPServer`'s own ctx (never a per-call ctx); `mgr.Shutdown()` (line 104) runs unconditionally after `server.Run` returns, before the function returns, for both return paths. |
| `cmd/cpg/mcp_session_test.go` | Tools-listed, SESS-06, SESS-05 wiring + stdout purity | ✓ VERIFIED | 220 lines, 3 tests, independently re-run: `TestMCPSessionToolsListed`, `TestMCPSessionUnknownIDReturnsIsError`, `TestMCPSessionLifecycleWiringAndStdoutPurity` — all pass `-race`. |

**gsd-sdk verify.artifacts:** 11/11 artifacts pass across all 4 plans (all `exists: true`, `issues: []`).

### Key Link Verification

`gsd-sdk query verify.key-links` returned `"Source file not found"` for all 14 declared links across the 4 plans — a tooling/path-parsing artifact (the plans' `from:` fields embed a function name after the file path, e.g. `"pkg/hubble/pipeline.go RunPipelineWithSource"`, which the verb does not resolve as a bare path). This is **not** evidence of missing wiring. Every link was independently verified by direct source reading instead:

| From | To | Via | Status | Details |
|---|---|---|---|---|
| `pipeline.go RunPipelineWithSource` | `cfg.OnFinal` | nil-checked call after stats population | ✓ WIRED | Confirmed lines 336-338. |
| `pipeline_config.go buildPipelineConfig` | `hubble.PipelineConfig.OnFinal` | closure param passthrough | ✓ WIRED | `OnFinal: onFinal` (pipeline_config.go:107); caller wires `func(st hubble.SessionStats){ s.final.Store(&st) }` (manager.go:169-171). |
| `pipeline_config.go buildPipelineConfig` | `tmpDir/policies` + `tmpDir/evidence` | `filepath.Join` | ✓ WIRED | Confirmed lines 69-70; also confirmed `true` by the automated tool. |
| `pipeline_config.go` | `defaultDuration(...)` zero-value guard | pre-construction | ✓ WIRED | Confirmed by automated tool + manual read (lines 76, 80). |
| `manager.go Start` | `context.WithCancel(m.rootCtx)` | server-rooted fork | ✓ WIRED | Line 130; `rg -n 'context.WithCancel\(m.rootCtx'` matches, zero matches for `reqCtx`/`setupCtx` reaching the goroutine. |
| `manager.go Start` | `go m.runPipeline(sessionCtx, cfg)` | background goroutine | ✓ WIRED | Lines 195-199. |
| `manager.go Start` | `m.session = s` (claimed before setup) | TOCTOU-safe slot claim | ✓ WIRED | Line 138, before `os.MkdirTemp` (154) and `resolveSetupFn` (163). |
| `manager.go Start` | `m.logger.Info evidence_session_id` | D-10 correlation | ✓ WIRED | Lines 175-178. |
| `manager.go Stop` | `s.stopOnce.Do` | idempotency guard | ✓ WIRED | Line 339. |
| `manager.go Shutdown` | `os.RemoveAll(tmpDir)` | unconditional bounded removal | ✓ WIRED (see Truth 4 gap for the mid-setup exception) | Line 397. |
| `mcp.go runMCPServer` | `session.NewManager(...)` | composition root | ✓ WIRED | Line 94. |
| `mcp.go runMCPServer` | `mgr.Shutdown()` | after `server.Run` returns | ✓ WIRED | Line 104. |
| `mcp_tools.go start_session` | `validateIgnoreProtocols`/`validateIgnoreDropReasons` | verbatim reuse | ✓ WIRED | Lines 92, 96; `rg -n 'validateIgnore' pkg/session/'` confirms zero reimplementation. |
| `mcp_tools.go handlers` | `mgr.Start`/`Status`/`Stop` | one-line forward | ✓ WIRED | Lines 109-124, 133-134, 145-146. |

### Data-Flow Trace (Level 4)

Backend Go MCP server — no UI rendering. Closest analog: does `structuredContent` returned to the LLM carry real data, or hardcoded/stubbed values?

| Artifact | Data Variable | Source | Produces Real Data | Status |
|---|---|---|---|---|
| `start_session` → `StartResult` | `session_id`, `server` | `mgr.Start()` → `Manager.Start` (real `uuid.New()`, real `resolveSetup`) | Yes | ✓ FLOWING — `TestMCPSessionLifecycleWiringAndStdoutPurity` extracts a real `session_id` from the actual wire `StructuredContent` and confirms a real on-disk tmpdir exists. |
| `get_status` → `StatusResult` | `state`, `elapsed`, `policy_file_count`, `evidence_file_count`, `tmp_dir` | `mgr.Status()` → real `filepath.Glob` counts on disk | Yes | ✓ FLOWING — `TestManager_Status` asserts `PolicyFileCount > 0` after a real `RunPipelineWithSource` run writes an actual policy file. |
| `stop_session` → `StopResult` | `flows_seen`, `policies_written`, etc. | `mgr.Stop()` → `buildSummary()` ← `Session.final` (populated by the real `OnFinal` hook) | Yes | ✓ FLOWING — `TestManager_Stop` confirms `FlowsSeen == 2` from a genuine pipeline run, not a mocked/hardcoded value. |

No hollow props or static fallbacks found in the request path.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|---|---|---|---|
| `cpg mcp` subcommand registered | `go run ./cmd/cpg --help` | Lists `mcp   Run cpg as a readonly MCP server over stdio` | ✓ PASS |
| `cpg mcp --help` does not hang | `timeout 10 go run ./cmd/cpg mcp --help` | Returns immediately, prints Short description | ✓ PASS |
| `pkg/session` full `-race` suite | `go test ./pkg/session/... -race -count=1 -v` | 28/28 tests pass | ✓ PASS |
| `cmd/cpg` MCP session integration tests | `go test ./cmd/cpg/... -run 'TestMCPSession' -race -count=1 -v` | 3/3 tests pass | ✓ PASS |
| Highest-risk concurrency tests, stress | `go test ./pkg/session/... -race -count=25 -run 'TestManager_Start_ConcurrentStartRejectsSecond\|_ConcurrentStatusAndStop\|_ConcurrentShutdownAndStop\|_Start_ShutdownRacesSetup\|_Start_SetupFailureRollsBackSlot'` | 125/125 sub-runs pass, 0 data races | ✓ PASS |
| Full repo suite (regression check) | `go test ./... -race -count=1` | 11/11 packages green | ✓ PASS |
| `go vet` | `go vet ./pkg/session/... ./cmd/cpg/... ./pkg/hubble/...` | No issues | ✓ PASS |
| `golangci-lint`, diff-scoped | `golangci-lint run --new-from-rev=83b6890` | 0 new issues since phase start | ✓ PASS |

### Probe Execution

No `scripts/*/tests/probe-*.sh` convention in this repository and none declared in the PLAN/SUMMARY files. **SKIPPED** (no probe-based verification applies to this Go-native test-suite project).

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|---|---|---|---|---|
| SESS-01 | 17-02, 17-03, 17-04 | `start_session` returns opaque `session_id`, background goroutine, detached ctx, ephemeral tmpdir | ✓ SATISFIED | Truth 1, VERIFIED. |
| SESS-02 | 17-03, 17-04 | Exactly one concurrent session, actionable rejection | ✓ SATISFIED | Truth 1, VERIFIED — including true-concurrency race coverage. |
| SESS-03 | 17-02, 17-03, 17-04 | `get_status` coarse state/elapsed/file counts | ⚠ PARTIAL | Truth 2, FAILED (partial) — mechanism correct for the stop_session-driven path; stale-state gap for autonomous pipeline exit (WR-01). |
| SESS-04 | 17-01, 17-02, 17-03, 17-04 | `stop_session` cancels, finalizes, returns summary | ✓ SATISFIED | Truth 3, VERIFIED. |
| SESS-05 | 17-03, 17-04 | Transport-termination bounded cleanup fan-out | ⚠ PARTIAL | Truth 4, FAILED (partial) — bounded and correct for an active/capturing session; mid-setup race can orphan a tmpdir/port-forward (WR-02). |
| SESS-06 | 17-03, 17-04 | Unknown/stopped `session_id` → crisp error | ✓ SATISFIED (documented wording deviation) | Truth 5, VERIFIED — "unknown" fully proven; "already-stopped" deliberately superseded by human-approved D-02 (see Gaps Summary). |

No orphaned requirements: REQUIREMENTS.md's traceability table maps exactly SESS-01..06 to Phase 17, and all 6 appear in at least one plan's `requirements:` frontmatter (17-03 and 17-04 both declare all six). REQUIREMENTS.md currently marks all six `Complete` — this verification finds SESS-03 and SESS-05 should be reopened pending the gaps above.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|---|---|---|---|---|
| — | — | TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER scan across all 12 phase-modified files | — | ℹ️ INFO — zero matches. No debt markers, no stub patterns, no hardcoded-empty-return stubs found. |
| `pkg/session/manager.go` | 195-199, 341-345, 383-387 | Pipeline terminal error read off `s.done` and discarded (never logged, stored, or surfaced) | 🛑 Blocker-tier (see Truth 2 gap) | `get_status` cannot detect an autonomously-dead session. |
| `pkg/session/manager.go` | 130, 160 | `setupCtx` derived from `reqCtx`, not `sessionCtx`/`m.rootCtx` | 🛑 Blocker-tier (see Truth 4 gap) | `Shutdown()` cannot abort a mid-setup `Start()`; possible orphaned tmpdir/port-forward. |
| `cmd/cpg/mcp_tools.go` | 50-62 | `parseOptionalDuration` rejects `<= 0` but has no upper bound | ⚠️ Warning | An MCP client can supply an unbounded `timeout`/`flush_interval`; compounds the Truth 4 gap since `setupCtx`'s own timeout is otherwise the only backstop. Reported as `17-REVIEW.md` WR-03. |
| `pkg/session/session.go` | 214-216 | `flowpb.DropReason_name[int32(reason)]` lookup has no `ok`-check; unrecognized future reason values collapse into one `""`-keyed map entry, silently undercounting | ⚠️ Warning | Low-probability (requires cluster running a newer Cilium than `cpg` was built against) but real data-integrity gap in `StopResult.InfraDropsByReason`. Reported as `17-REVIEW.md` WR-04. |
| `pkg/session/manager.go` / `pipeline_config.go` | manager.go:329-333, pipeline_config.go:69-71 | `outputHash`/`healthPath` formula duplicated in two places (deliberate, per comment) | ℹ️ Info | No functional bug today (pure function of a deterministic path); nothing enforces the two stay in sync if either changes independently. Reported as `17-REVIEW.md` IN-01. |

**Note:** These findings were independently re-derived by this verifier directly from source; they also happen to be documented in `.planning/phases/17-session-lifecycle/17-REVIEW.md` (a code-review artifact committed after all 4 plan SUMMARYs, `status: issues_found`, 0 critical / 4 warning / 1 info). That review remains unaddressed as of the current HEAD (no follow-up commit).

### Human Verification Required

None. All findings in this report are either fully automatically verified (build/vet/lint/tests, independently re-run — not just SUMMARY claims) or represent concrete code-level gaps (WR-01, WR-02) that a closure plan can fix without human judgment. The one genuinely human-originated item (the D-02 architecture decision superseding SC5's literal "already-stopped" wording) is already resolved — it was made explicitly by the user during the phase's discussion step (`17-DISCUSSION-LOG.md`), not something this verification needs to ask about again. It only needs a documentation reconciliation (ROADMAP.md/REQUIREMENTS.md wording), not a functional decision.

### Gaps Summary

Phase 17 delivers a genuinely solid, extensively and honestly tested implementation for its primary designed lifecycle: `start_session` → background capture → `get_status` polling → `stop_session` (or transport-death) → bounded cleanup. All 11 required artifacts exist, are substantive, and are wired; all 4 plans' own test suites pass, independently re-run by this verifier (not just trusted from SUMMARY.md), including a 25× stress run of the highest-risk concurrency tests with zero data races. `go build`, `go vet`, and `golangci-lint --new-from-rev` are all clean. No stub/debt-marker anti-patterns exist anywhere in the 12 phase-modified files.

However, two real, independently-confirmed logic gaps prevent a clean pass, both already surfaced (but left unaddressed) by the project's own `17-REVIEW.md` code review:

1. **WR-01 (Truth 2 / SESS-03):** `get_status` can report `"capturing"` forever for a session whose pipeline has already died on its own — the terminal error is captured and then thrown away, and `Session.State` is only ever advanced by an explicit `stop_session` call. This directly undermines the phase's own goal language ("...monitor... with the process robustly cleaning up").
2. **WR-02 (Truth 4 / SESS-05):** `Shutdown()`'s cancellation cannot reach a `Start()` call that is still inside its synchronous setup phase (`setupCtx` is rooted in the per-call `reqCtx`, not the server-rooted `sessionCtx`), so a transport-kill landing in that narrow-but-real window can leave an orphaned tmpdir (and possibly a leaked port-forward/exec-plugin subprocess) past process exit — contradicting SC4's literal "removes the tmpdir... bounded" promise for that window. Compounded by `k8s.LoadKubeConfig` accepting no context at all, and by no upper bound on the MCP-supplied `timeout` (WR-03).

Neither gap is deferred by a later phase's explicit scope: Phase 19's SRV-04 ("ungraceful-disconnect variant proving port-forward and tmpdir are cleaned up within a bounded deadline") is a *test* of the general disconnect scenario, not a guaranteed fix for this specific mid-setup race, and Phase 19's SEC-01 is about readonly-guarantee auditing, not cleanup-context wiring — so both gaps are kept as active, actionable items for this phase rather than deferred.

A third finding is **not** treated as a gap: Truth 5's "already-stopped session_id" clause is deliberately, correctly implemented differently from ROADMAP.md/REQUIREMENTS.md's literal wording, per a documented, human-approved decision (D-02, `17-CONTEXT.md`) made explicitly during the phase's discussion step (`17-DISCUSSION-LOG.md`, "User's choice: Retenu après stop") specifically to resolve a genuine tension with Phase 18's QRY-04. Recommend reconciling ROADMAP.md/REQUIREMENTS.md wording to reference D-02 explicitly so future readers/verifiers aren't misled by the stale "or already-stopped" phrasing — this is a documentation action, not a code gap.

**This looks intentional (Truth 5 only).** To formally accept the D-02 wording deviation, add to a future VERIFICATION.md frontmatter:

```yaml
overrides:
  - must_have: "Any session-scoped tool called with an unknown or already-stopped session_id returns a crisp 'session not found or expired' error"
    reason: "D-02 (17-CONTEXT.md): a retained stopped session must stay queryable to resolve the SESS-06 <-> QRY-04 tension (Phase 18 needs to read a stopped session's artifacts cold). Decided explicitly by the user during the phase discussion step (17-DISCUSSION-LOG.md, 'Retenu apres stop'), predating planning. SESS-06 applies only to unknown/purged ids."
    accepted_by: "<developer to confirm>"
    accepted_at: "<ISO timestamp>"
```

WR-01 and WR-02, by contrast, are not intentional deviations — they are unaddressed logic gaps the project's own review already flagged. Recommend a closure plan for both before considering Phase 17 fully hardened.

---

_Verified: 2026-07-21T08:10:00Z_
_Verifier: Claude (gsd-verifier)_
