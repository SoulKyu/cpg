---
phase: 19-security-hardening-end-to-end-validation
verified: 2026-07-21T21:45:00Z
reverified: 2026-07-21T22:05:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
gaps: []
resolved_gaps:
  - truth: "All requirement IDs claimed by this phase's plans (SRV-01, SRV-04, SEC-01, SEC-03) are correctly tracked as complete in .planning/REQUIREMENTS.md"
    status: resolved
    resolution: "Orchestrator closed the gap in commit 99e08f3 + footer refresh: SEC-03 checkbox flipped to [x], traceability row set to Complete, 'Last updated' footer now cites Phase 19. All three 'missing' items done. Deliverable itself was already verified correct (Truth 4); no code changed."
    reason: "SEC-03's actual deliverable (README's new '## MCP Server (cpg mcp)' section) is complete, accurate, and independently verified in this report — but .planning/REQUIREMENTS.md was never updated to reflect it. The checkbox at line 39 is still '- [ ] **SEC-03**' and the Traceability table at line 94 still reads '| SEC-03 | Phase 19 | Pending |', inconsistent with its three sibling requirements from the SAME phase (SRV-01, SRV-04, SEC-01), which are all correctly marked '[x]'/'Complete'. Plans 19-01, 19-02, and 19-04's own SUMMARY.md files explicitly document updating REQUIREMENTS.md as a completion step (19-01-SUMMARY.md: \"Marked requirement SEC-01 complete in .planning/REQUIREMENTS.md\"; 19-04-SUMMARY.md: \"requirements.mark-complete SRV-04 confirmed already-complete\"). 19-03-SUMMARY.md (the README/SEC-03 plan) makes no such mention in its Accomplishments, Files Created/Modified, or Self-Check sections, and the current file confirms the step never ran. This is a documentation/tracking gap only — the underlying SEC-03 work itself is verified complete and correct (see Success Criterion 4 below); it does not indicate missing or broken functionality."
    artifacts:
      - path: ".planning/REQUIREMENTS.md"
        issue: "Line 39 '- [ ] **SEC-03**: ...' should be '- [x]'; line 94 '| SEC-03 | Phase 19 | Pending |' should read 'Complete'; the trailing 'Last updated' note (line 103) still says 'after Phase 18' and should mention Phase 19"
    missing:
      - "Flip the SEC-03 checkbox to [x] in the '### Security & Hardening' section of .planning/REQUIREMENTS.md"
      - "Change the SEC-03 row in the Traceability table from 'Pending' to 'Complete'"
      - "Refresh the file's trailing 'Last updated' note to reflect Phase 19 completion"
---

# Phase 19: Security Hardening & End-to-End Validation Verification Report

**Phase Goal:** The readonly guarantee is structurally proven and documented, and the complete session lifecycle is verified end-to-end under race detection
**Verified:** 2026-07-21T21:45:00Z
**Status:** passed (re-verified after gap closure — commit 99e08f3)
**Re-verification:** Yes — the single tracking-only gap (SEC-03 ledger) was closed by the orchestrator; Truth 5 now VERIFIED. No code changed between verification runs; test evidence carries over.

## Goal Achievement

**Adversarial note on method:** This verification did not stop at reading source and trusting the four SUMMARY.md files. It independently re-executed every test this phase claims (SEC-01 audit, both e2e variants, full package, full module — all under `-race` where specified), and additionally ran a **mutation test** against the SEC-01 audit: one of the five allowlisted functions was temporarily removed from the allowlist, the audit was re-run to confirm it genuinely fails with the exact function-symbol + call-path diagnostic the plan requires, and the file was then reverted via `git checkout` and re-verified clean and passing. This is the single highest-value check for this phase, since D-04's entire "re-runnable, catches future leaks" contract is otherwise just a docstring claim.

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Initialize handshake lists all 8 tools with correct schemas (SRV-01) | VERIFIED | `cmd/cpg/mcp_e2e_test.go:402-476` (`TestMCPE2EGracefulLifecycle`) asserts `serverInfo.Name=="cpg"`, exactly 8 named tools, non-empty description/inputSchema per tool, non-empty outputSchema per data tool, correct `ReadOnlyHint`/`IdempotentHint`/`OpenWorldHint` per tool, and the `dropclass` enum scoped to `list_dropped_flows` only. Independently re-run: `PASS (7.54s, -race)`. Cross-checked every assertion against the actual registration code (`cmd/cpg/mcp_tools.go`, `mcp_query.go`, `mcp_query_evidence.go`, `mcp_query_flows.go`) — annotations and the absence of a `Dropclass` field on `getEvidenceArgs` match exactly. |
| 2 | Audit test proves no K8s write verb and no filesystem write outside the session tmpdir is reachable from the MCP composition root — re-runnable (SEC-01) | VERIFIED | `cmd/cpg/mcp_audit_test.go` (336 lines): RTA rooted at `main`+`init` (line 261, not at `runMCPServer`), BFS restricted to `github.com/SoulKyu/cpg/` prefix (line 281), Stage-3 direct-call scan against a 3-verb-extended `k8sWriteVerbs` set (unconditional fail, no allowlist, lines 66-77/323-331) and a 7-mutator-extended `disallowedFSWrite` set with an exact 5-function `fsWriteAllowlist` (lines 29-118). Independently re-run 3x (non-race, ~13-14s each): `PASS`. **Mutation test performed:** removed `(*session.Manager).Start` from the allowlist — audit correctly `FAIL`ed, naming the exact offending function, its disallowed calls (`os.RemoveAll`, `os.MkdirTemp`), and a full BFS call path from `runMCPServer` — reverted cleanly via `git checkout`, re-confirmed `PASS`. This proves the D-04 re-runnability/diagnostic contract is real, not just documented. |
| 3 | End-to-end stdio test drives the full lifecycle under `-race`, plus an ungraceful-disconnect variant proving cleanup within a bounded deadline (SRV-04) | VERIFIED | `cmd/cpg/mcp_e2e_test.go` (815 lines): `TestMCPE2EGracefulLifecycle` drives `initialize → tools/list → start_session → get_status → 5 query tools mid-capture → stop_session → get_cluster_health post-stop → close stdin → exit 0` against a real `-race`-built subprocess and a real in-process fake gRPC relay; `TestMCPE2EUngracefulDisconnect` drives `start_session → get_status` then abruptly closes stdin with **no** `stop_session`, asserting bounded self-exit (~10s cap), `NoDirExists(tmp_dir)`, and `relay.snapshot().cancelled==true`. Independently re-run: both `PASS` under `-race` (7.54s / 2.32s). Stability re-check: `TestMCPE2EUngracefulDisconnect -race -count=5` → **5/5 PASS** (the historically flake-prone test per its own SUMMARY). Full `cmd/cpg` package under `-race`: `PASS` (82.9s, matches SUMMARY's claimed ~81s). Full module (12 packages) under `-race`: `PASS`. `go build ./...`: clean. |
| 4 | README's MCP section documents harness `env` config, secrets posture, and the exec-credential-plugin caveat (SEC-03) | VERIFIED | `README.md:506-555`, correctly placed between `## Explain policies` (438) and `## Label selection` (557). All 6 D-13 blocks present in order: intro, 8-tool table, `### Harness configuration` (KUBECONFIG/PATH/TMPDIR + WHY, "do not inherit your shell environment"), `### Secrets posture` (Authorization/Cookie never captured, no v1.5 redaction, `[L7 Prerequisites](#l7-prerequisites)` cross-link to the real anchor at line 246), `### Exec-credential-plugin caveat` (names `aws eks get-token`/`gke-gcloud-auth-plugin`/`azure kubelogin`, headless-auth verification, bounded-timeout note, credential-persistence one-liner), `### Session model`. No aspirational content (`rg "HTTP transport\|multi-session\|SSE transport" README.md` → 0 matches). **Two factual claims independently fact-checked against source** (Confirmation Bias Counter): the KUBECONFIG resolution-order claim matches `pkg/k8s/client.go:13-15`'s doc comment verbatim ("KUBECONFIG env, ~/.kube/config, in-cluster"); the IN-02-corrected kubeconfig-load timeout exception matches `pkg/session/manager.go:172-173`'s own comment verbatim. Tool-table one-liners cross-checked against the real `Description` strings in `mcp_tools.go`/`mcp_query.go` — not invented. |
| 5 | All requirement IDs claimed by this phase's plans (SRV-01, SRV-04, SEC-01, SEC-03) are correctly tracked as complete in `.planning/REQUIREMENTS.md` | VERIFIED (after gap closure) | Initially FAILED: `.planning/REQUIREMENTS.md:39` read `- [ ] **SEC-03**` / traceability row `Pending` (Plan 19-03 skipped the ledger step its siblings ran). Closed by orchestrator commit `99e08f3`: checkbox `[x]`, row `Complete`, footer refreshed to cite Phase 19 — all three `missing` items from the original gap done. Underlying deliverable was already verified correct (Truth 4). |

**Score:** 5/5 truths verified (Truth 5 resolved post-initial-run — commit 99e08f3)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `cmd/cpg/mcp_audit_test.go` | SEC-01 RTA reachability + BFS filter + direct-call scan + 5-function allowlist + K8s-verb property, ≥120 lines | VERIFIED | 336 lines. Contains `runMCPServer` (line 252), `TestMCPAuditReadonlyReachability` (line 222). Substantive (no stubs); wired (compiles and runs as part of `cmd/cpg` package); re-run passes; mutation-tested for real failure behavior. |
| `go.mod` | `golang.org/x/tools` promoted indirect→direct | VERIFIED | Line 19: `golang.org/x/tools v0.44.0` in the direct `require (...)` block (lines 7-26), no `// indirect` suffix. |
| `go.sum` | Zero new hashes from the promotion | VERIFIED | `git diff` across the entire phase-19 commit range (`6119fc2^..bfe69e7`) shows `go.sum` untouched — only `go.mod` changed (1 insertion, 1 deletion). Last commit to touch `go.sum` before Phase 19 was Phase 16 (`93e7b3e`). |
| `cmd/cpg/mcp_e2e_test.go` | Fake relay + `-race` build helper + subprocess/tee harness + fixtures + graceful test, ≥200 lines; ungraceful variant added by 19-04 | VERIFIED | 815 lines. Contains `TestMCPE2EGracefulLifecycle` (line 389) and `TestMCPE2EUngracefulDisconnect` (line 694), each defined exactly once. Substantive, wired (compiles, runs, independently re-executed and passing including a 5x race-stability check). |
| `README.md` | New `## MCP Server (cpg mcp)` section with all 6 D-13 blocks | VERIFIED | Section at lines 506-555, correctly placed, all sub-headings present, content fact-checked against source (see Truth 4). |
| `.planning/REQUIREMENTS.md` | SRV-01/SRV-04/SEC-01/SEC-03 tracked as complete | VERIFIED | All four tracked complete after gap closure (commit `99e08f3`); footer cites Phase 19. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `mcp_audit_test.go` | `cmd/cpg/mcp.go runMCPServer` | `mainPkg.Func("runMCPServer")` as BFS root | WIRED | Line 252: `root := mainPkg.Func("runMCPServer")` + `require.NotNil`. |
| `mcp_audit_test.go` | `golang.org/x/tools/go/callgraph/rta` | `rta.Analyze` rooted at main+init | WIRED | Line 261: `rta.Analyze([]*ssa.Function{mainFn, initFn}, true)` — confirmed NOT rooted at `runMCPServer` directly, matching the anti-pattern guard. |
| `mcp_audit_test.go` | `pkg/session/paths.go DeriveSessionPaths` | allowlist WHY-safe rationale | WIRED | Lines 85-117: every one of the 5 allowlist entries' doc comments cites `os.MkdirTemp`/`session.DeriveSessionPaths`. |
| `mcp_e2e_test.go` | `cmd/cpg/mcp.go runMCPServer` (via `cpg mcp`) | `exec.Command(bin, "mcp")` | WIRED | Line 305: `exec.Command(binPath, "mcp")`, built via `go build -race` (line 228). |
| `mcp_e2e_test.go` | `pkg/hubble/client.go` | fake relay implements `GetFlows` only | WIRED | Lines 96-122: `(*fakeRelay).GetFlows` is the sole implemented RPC; embeds `UnimplementedObserverServer` by value (not pointer) per SDK guidance. |
| `mcp_e2e_test.go` | `github.com/cilium/cilium/api/v1/observer` | `RegisterObserverServer` on `127.0.0.1:0` | WIRED | Line 165: `observerpb.RegisterObserverServer(grpcServer, relay)` on `net.Listen("tcp", "127.0.0.1:0")` (line 160). |
| `mcp_e2e_test.go` | raw stdout tee buffer | `io.TeeReader` byte-purity loop | WIRED | Line 330: `io.TeeReader(stdoutR, &rawTee)`; `assertStdoutPurity` (line 371) called at line 673. |
| `TestMCPE2EUngracefulDisconnect` | fake relay `started` signal | `relay.waitStarted` before stdin close | WIRED | Line 752: `relay.waitStarted(t, 5*time.Second)` — plus a second, stronger artifact-based gate (poll for the real policy file on disk, lines 773-778) added as a documented Rule-1 auto-fix for a race the plan's literal guidance alone did not fully close. |
| `TestMCPE2EUngracefulDisconnect` | `pkg/session/manager.go Shutdown` | `NoDirExists` + `cancelled` assertions | WIRED | Lines 797, 811-813: `require.NoDirExists(t, statusOut.TmpDir)` and `require.Eventually(... relay.snapshot().cancelled ...)`. D-09 mapping documented in a test comment (lines 686-693). |
| `README.md` MCP section | `README.md #l7-prerequisites` anchor | secrets-posture cross-link | WIRED | Line 547: `[L7 Prerequisites](#l7-prerequisites)`; anchor confirmed present at `README.md:246` (`## L7 Prerequisites <a id="l7-prerequisites"></a>`). |

### Behavioral Spot-Checks / Independent Re-execution

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| SEC-01 audit passes standalone | `go test ./cmd/cpg/ -run TestMCPAuditReadonlyReachability -count=1` | `PASS` (13.4-14.3s, 3 runs) | PASS |
| SEC-01 audit genuinely fails on a real violation (mutation test) | Removed `(*session.Manager).Start` from `fsWriteAllowlist`, re-ran, reverted | `FAIL` with function symbol + disallowed calls (`os.RemoveAll`, `os.MkdirTemp`) + full call path from `runMCPServer`; reverted cleanly, `PASS` confirmed after | PASS |
| Both e2e variants pass under `-race` | `go test ./cmd/cpg/ -run 'TestMCPE2E' -count=1 -race` | `PASS` (10.95s total: graceful 7.54s, ungraceful 2.32s) | PASS |
| Ungraceful variant is stable (flake-resistance gate) | `go test ./cmd/cpg/ -run TestMCPE2EUngracefulDisconnect -race -count=5` | `PASS` 5/5 (17.5s total) | PASS |
| Full `cmd/cpg` package regression (D-11) | `go test ./cmd/cpg/ -count=1 -race` | `PASS` (82.9s) | PASS |
| Full module regression | `go test ./... -count=1 -race` | `PASS`, 12 packages (cmd/cpg + 11 pkg/*) | PASS |
| Build integrity | `go build ./...` | clean, exit 0 | PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| SRV-01 | 19-02 | SRE can register `cpg mcp` and initialize handshake lists all tools | SATISFIED | Verified in Truth 1; `REQUIREMENTS.md` correctly shows `[x]`/Complete. |
| SRV-04 | 19-02, 19-04 | E2e stdio test drives full lifecycle under `-race` + ungraceful-disconnect variant | SATISFIED | Verified in Truth 3; `REQUIREMENTS.md` correctly shows `[x]`/Complete. |
| SEC-01 | 19-01 | Readonly guarantee is structural, verified by re-runnable audit | SATISFIED | Verified in Truth 2 (including mutation test); `REQUIREMENTS.md` correctly shows `[x]`/Complete. |
| SEC-03 | 19-03 | README documents harness config, secrets posture, exec-credential caveat | SATISFIED (deliverable) / BLOCKED (tracking) | README content verified in Truth 4; `REQUIREMENTS.md` traceability NOT updated — see Gaps. |

No orphaned requirements: all 4 IDs declared across the 4 plans' `requirements:` frontmatter match exactly ROADMAP.md's Phase 19 `Requirements: SRV-01, SRV-04, SEC-01, SEC-03`, and REQUIREMENTS.md has an entry (description + traceability row) for each.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | Zero `TBD`/`FIXME`/`XXX`/`TODO`/`HACK`/`PLACEHOLDER` markers found in any of the 4 phase-modified files (`mcp_audit_test.go`, `mcp_e2e_test.go`, `go.mod`, `README.md`) | — | No debt-marker gate triggered. |
| `cmd/cpg/mcp_e2e_test.go` | 308-334, 659 (per 19-REVIEW.md IN-03) | `cmd.Wait()` runs concurrently with the `StdoutPipe` read loop — the documented `os/exec` ordering footgun | INFO (accepted) | Explicitly reviewed and triaged by `19-REVIEW-FIX.md`: classified by the reviewer itself as "benign in practice" (every assertion reads synchronously via `CallTool` before stdin closes, so `assertStdoutPurity` only ever sees whole frames); the proposed fix is a genuine concurrency restructure, reasonably deferred rather than mechanically applied. Not re-flagged as a new gap — properly triaged, documented rationale present. |

Code review (`19-REVIEW.md`) found 4 WARNING + 3 INFO issues; `19-REVIEW-FIX.md` shows 6/7 fixed (WR-01..04, IN-01, IN-02) with commits, and the 1 skip (IN-03) is justified and low-severity. All fixes were independently confirmed present in the current source during this verification (WR-01's `require.NotNil`/`require.Greater`/`require.Contains` floor checks, WR-02's extended `disallowedFSWrite`, WR-03's extended `k8sWriteVerbs`, WR-04's `t.Cleanup` kill-guard, IN-01's docstring comment, IN-02's softened README claim).

### Human Verification Required

None. Every roadmap success criterion and every plan must-have is programmatically verifiable and was independently verified in this report (including re-executing all specified test commands, a full-module regression run, and a mutation test of the audit's failure path). The README's prose-accuracy check — which the plan itself assigns to "the phase verifier reading the rendered README, no automated test for prose is possible" — was performed here via direct read plus fact-checking two specific claims (KUBECONFIG resolution order, kubeconfig-load timeout exception) against the actual `pkg/k8s`/`pkg/session` source; both are accurate.

### Gaps Summary

**RESOLVED (2026-07-21, commit `99e08f3`):** the gap below was closed same-day by the orchestrator — SEC-03 checkbox `[x]`, traceability row `Complete`, footer refreshed. Phase status is now **passed, 5/5**. Original finding preserved below for the audit trail.

**One gap found, and it is a documentation-tracking issue, not a functional or design defect.** All four ROADMAP.md success criteria for Phase 19 are independently verified as implemented and working: the SEC-01 audit genuinely detects violations (proven via mutation test, not just read), both e2e lifecycle variants pass under `-race` including a 5x stability re-run of the historically flake-prone ungraceful path, the full 12-package module regression is green, and the README's new MCP section is structurally complete and factually accurate against source.

The single gap is that `.planning/REQUIREMENTS.md` was never updated to mark **SEC-03** complete, even though its actual deliverable (the README section) is done and verified correct. Its three sibling requirements from the same phase (SRV-01, SRV-04, SEC-01) were correctly marked complete by their respective plans' executors, but Plan 19-03's SUMMARY.md shows no evidence this same bookkeeping step ran. This is a two-line fix in a single markdown file (flip a checkbox, flip one table cell) with zero code risk — flagged here specifically because leaving it unfixed would cause this phase (the v1.5 milestone's closing phase) to permanently misreport 17/18 requirements complete in the canonical requirements ledger, even though ROADMAP.md's own phase-progress table already shows Phase 19 as 4/4 plans complete.

**Recommendation:** fix directly (no formal closure plan needed) — update `.planning/REQUIREMENTS.md` line 39 (`- [ ]` → `- [x]`) and line 94 (`Pending` → `Complete`), then re-verify.

---

_Verified: 2026-07-21T21:45:00Z_
_Verifier: Claude (gsd-verifier)_
