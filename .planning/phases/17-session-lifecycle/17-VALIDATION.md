---
phase: 17
slug: session-lifecycle
status: planned
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-20
---

# Phase 17 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + testify (assert/require) + go.uber.org/zap/zaptest/observer; fakes implement `pkg/flowsource.FlowSource`, no cluster, no MCP SDK for pkg/session |
| **Config file** | none — standard `go.mod` toolchain (go1.25.12) |
| **Quick run command** | `go test ./pkg/session/... ./pkg/hubble/... ./cmd/cpg/... -race -count=1` |
| **Full suite command** | `go test -race ./... -count=1` |
| **Estimated runtime** | ~90 seconds (~490 tests, 10+ packages; bounded-wait deadlines shrunk to 100ms in pkg/session tests) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/session/... ./pkg/hubble/... ./cmd/cpg/... -race -count=1`
- **After every plan wave:** Run `go test -race ./... -count=1`
- **Before `/gsd-verify-work`:** Full suite green + `rtk proxy golangci-lint run`
- **Max feedback latency:** 90 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 17-01-T1 | 17-01 | 1 | SESS-04 | T-17-01-02 | Nil OnFinal is a no-op — full pkg/hubble suite unchanged (nil-safe for every CLI path) | unit + race | `go build ./... && go test ./pkg/hubble/... -race -count=1` | ✅ existing (extend) | ⬜ pending |
| 17-01-T2 | 17-01 | 1 | SESS-04 | T-17-01-01 | Hook fires exactly once with populated stats, passed by value (no shared pointer) | unit + race | `go test ./pkg/hubble/... -run 'TestRunPipeline_OnFinal' -race -count=1 -v` | ❌ new | ⬜ pending |
| 17-02-T1 | 17-02 | 2 | SESS-01, SESS-03, SESS-04 | T-17-02-03 | Cross-goroutine stats via atomic.Pointer; buildSummary nil-`final` safe | unit + race | `go test ./pkg/session/... -race -run 'TestState_String\|TestSession_BuildSummary' -v` | ❌ new | ⬜ pending |
| 17-02-T2 | 17-02 | 2 | SESS-01, SESS-04 | T-17-02-01, T-17-02-02, T-17-02-04 | Zero/negative duration → CLI default (ticker never sees 0; dial bound restored); D-05 write-fields never set | unit + race | `go test ./pkg/session/... -race -run 'TestDefaultDuration\|TestBuildPipelineConfig' -v` | ❌ new | ⬜ pending |
| 17-03-T1 | 17-03 | 3 | SESS-01..06 | T-17-03-01, T-17-03-02, T-17-03-05 | Server-rooted ctx fork; bounded shutdown; setup under one timeout; no write verb | build + vet | `go build ./... && go vet ./pkg/session/...` | ✅ existing tooling | ⬜ pending |
| 17-03-T2 | 17-03 | 3 | SESS-01, SESS-02, SESS-03, SESS-04, SESS-05, SESS-06 | T-17-03-01, T-17-03-03, T-17-03-06 | Single-active enforced; idempotent stop; retention; wedged-step Shutdown still bounded; -race clean | unit + race | `go test ./pkg/session/... -race -count=1 -v` | ❌ new | ⬜ pending |
| 17-04-T1 | 17-04 | 4 | SESS-01..06 | T-17-04-01, T-17-04-02, T-17-04-05 | Args validated at boundary (durations, validators, mutual-exclusivity); mcpModeStdout() wired; Shutdown after Run | build + CLI | `go mod tidy && go build ./... && go run ./cmd/cpg mcp --help 2>&1 \| rg -q 'readonly MCP server over stdio'` | ❌ new | ⬜ pending |
| 17-04-T2 | 17-04 | 4 | SESS-02, SESS-05, SESS-06 | T-17-04-02, T-17-04-03 | 3 tools listed; unknown id → isError "not found or expired"; tmpdir removed after ctx-cancel; zero stdout leak | integration + race | `go test ./cmd/cpg/... -run 'TestMCPSession' -race -count=1 -v` | ❌ new | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- Existing infrastructure covers all phase requirements. Every net-new test file (`pkg/hubble/pipeline_test.go` extensions, `pkg/session/session_test.go`, `pkg/session/pipeline_config_test.go`, `pkg/session/manager_test.go`, `cmd/cpg/mcp_session_test.go`) is authored inside the same task/plan that needs it — no task's `<automated>` verify references a test a later task creates. The one build/vet-only task (17-03-T1) is immediately followed by its behavioral race suite (17-03-T2) in the same plan, so sampling continuity holds (no 3 consecutive tasks without an automated behavioral verify). `go test` + `-race` discipline already exists project-wide (~484 tests, 10+ packages, `Makefile:9`). No framework install required.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `start_session` against a real Hubble Relay (live capture → policies/evidence/cluster-health on disk) | SESS-01, SESS-04 | No reachable cluster in CI (RESEARCH Environment Availability); automated pkg/session tests use a fake `FlowSource` + `server`-bypass by design | Against a real cluster: `cpg mcp` in a harness, call `start_session {"namespace":"..."}`, poll `get_status`, then `stop_session`; confirm the returned `cluster_health_path` file exists and the summary counts are non-zero |
| Actionable kubeconfig/exec-plugin timeout message (`resolveSetup` DeadlineExceeded) | SESS-01 | A deterministic hang of a real exec-credential plugin cannot be reliably simulated in unit tests | Code-asserted: the wrapping exists (`rg -n 'DeadlineExceeded\|re-authenticate' pkg/session/manager.go`). Optional live check: point `start_session` at a cluster whose kubeconfig uses an interactive exec plugin and confirm the timeout message names re-authentication |

*Deferred by design (NOT manual, NOT Phase 17): the full capturing golden-sequence over a real/fake gRPC Hubble source + the ungraceful-disconnect variant driven end-to-end under `-race` is Phase 19 / SRV-04 — mirroring how 16-VALIDATION deferred the real-stdio subprocess e2e. Phase 17's deep SESS-05 cleanup semantics are proven at unit level in `pkg/session/manager_test.go` (graceful + wedged Shutdown); the cmd-level integration test proves only the composition-root WIRING calls Shutdown.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (every task has an automated command; no checkpoints this phase — fully autonomous)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (the single build/vet task 17-03-T1 is immediately followed by the race suite 17-03-T2)
- [x] Wave 0 covers all MISSING references (none needed — see Wave 0 Requirements)
- [x] No watch-mode flags
- [x] Feedback latency < 90s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planned — ready for `/gsd-execute-phase 17`
