---
phase: 16
slug: mcp-server-foundation-write-safety
status: planned
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-20
---

# Phase 16 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + testify + zap/zaptest/observer |
| **Config file** | none — standard `go.mod` toolchain (go1.25.12) |
| **Quick run command** | `go test ./cmd/cpg/... ./pkg/output/...` |
| **Full suite command** | `go test -race ./...` |
| **Estimated runtime** | ~60 seconds (484 existing tests, 10 packages) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./cmd/cpg/... ./pkg/output/...`
- **After every plan wave:** Run `go test -race ./...`
- **Before `/gsd-verify-work`:** Full suite must be green + `golangci-lint run`
- **Max feedback latency:** 90 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 16-01-T1 | 16-01 | 1 | SEC-02 | T-16-01, T-16-01b | Policy YAML written via temp+rename with 0644 preserved; existing suite green | unit | `go build ./... && go test ./pkg/output/... -v` | ✅ existing | ⬜ pending |
| 16-01-T2 | 16-01 | 1 | SEC-02 | T-16-01 | No leftover `.tmp-*`; concurrent reader never sees a partial file | unit + race | `go test ./pkg/output/... -race -run 'TestWriter_AtomicNoLeftoverTempFiles\|TestWriter_ConcurrentReaderNeverSeesPartialFile' -v` | ❌ new | ⬜ pending |
| 16-02-T1 | 16-02 | 1 | SRV-02, SRV-03 | T-16-SC | Blocking-human legitimacy gate before go-sdk install | checkpoint:human-verify | (manual — operator approval) | n/a | ⬜ pending |
| 16-02-T2 | 16-02 | 1 | SRV-02, SRV-03 | T-16-SC | go-sdk pinned at exactly v1.6.1 in go.mod/go.sum | unit | `rg -q 'github.com/modelcontextprotocol/go-sdk v1.6.1' go.mod && rg -q 'github.com/modelcontextprotocol/go-sdk' go.sum` | ❌ new | ⬜ pending |
| 16-03-T1 | 16-03 | 2 | SRV-02, SRV-03 | T-16-02, T-16-03, T-16-EoP | `cpg mcp` builds; IOTransport captured before swap; zapslog wired; zero tools | build + CLI | `go mod tidy && go build ./... && go run ./cmd/cpg --help 2>&1 \| rg -q '(^\|\s)mcp(\s\|$)' && go run ./cmd/cpg mcp --help 2>&1 \| rg -q 'readonly MCP server over stdio'` | ❌ new | ⬜ pending |
| 16-03-T2 | 16-03 | 2 | SRV-02 | T-16-02 | Zero bytes leak to real os.Stdout across a full in-memory session | integration + race | `go test ./cmd/cpg/... -run TestMCPStdoutPurity -race -v` | ❌ new | ⬜ pending |
| 16-03-T3 | 16-03 | 2 | SRV-02, SRV-03 | T-16-02 | Seam audit (→stderr), cobra Silence* off stdout, zapslog→zap bridge | unit | `go test ./cmd/cpg/... -run 'TestMCPModeStdoutNeverDefaultsToRealStdout\|TestMCPCobraFlagErrorStaysOffStdout\|TestMCPLoggingBridgesToZapStderr' -v` | ❌ new | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- Existing infrastructure covers all phase requirements. All net-new test files (`cmd/cpg/mcp_test.go`, `cmd/cpg/mcp_harness_test.go`, and the new `pkg/output/writer_test.go` tests) are authored inside the same task/plan that needs them — no task's `<automated>` verify references a test that a later task creates, so no Wave 0 scaffold is required. `go test` + `-race` discipline already exists project-wide (484 tests, 10 packages).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| go-sdk v1.6.1 supply-chain legitimacy | SRV-02, SRV-03 | slopcheck `[SUS]` verdict (rebutted); Package Legitimacy Gate mandates operator awareness before pulling a new module into the build graph — never auto-approvable | Review https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk@v1.6.1 and https://github.com/modelcontextprotocol/go-sdk (tag v1.6.1); confirm the exact pin; type "approved" (16-02 Task 1) |

*Note: the real-stdio subprocess e2e (every stdout line parses as a JSON-RPC frame) is Phase 19 / SRV-04 scope (D-06) — deliberately NOT duplicated here.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (checkpoint 16-02-T1 is a human gate by design; every code task has an automated command)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (the single checkpoint is immediately followed by an automated task)
- [x] Wave 0 covers all MISSING references (none needed — see Wave 0 Requirements)
- [x] No watch-mode flags
- [x] Feedback latency < 90s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planned — ready for `/gsd-execute-phase 16`
