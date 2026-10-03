---
last_mapped_commit: eff36ca37f3c162ebd12c5ddf50350d36a9906b3
last_mapped_at: 2026-10-03
---
# Codebase Concerns — Tech Debt, Fragile Areas, Security Posture

<!-- refreshed: 2026-10-03 -->

**Analysis Date:** 2026-10-03
Scope: `github.com/SoulKyu/cpg` at current `master` (post-v1.6). Every claim below was verified in code or project docs; tracking IDs come from `docs/KNOWN_LIMITATIONS.md` and `.planning/PROJECT.md`.

---

## 1. Tech debt (tracked, deliberate)

| ID | What | Where | Severity | Why it matters |
|---|---|---|---|---|
| LINT-01..03 | 16 errcheck + 10 staticcheck SA1019 pre-existing issues; CI lint only gates new issues (`only-new-issues: true` in `.github/workflows/ci.yml:29`) | CI config; backlog in `.planning/PROJECT.md` (Planned) | Medium | Legacy lint debt is invisible to CI; the flag is a named temporary gate ("drop when LINT-01..03 land"). New code touching those files can hide regressions. |
| RELSEC-01..02 | `release.yml` minimal-permissions review + govulncheck pinning follow-through | `.planning/PROJECT.md` (Planned) | Medium | Release workflow permissions not yet hardened. |
| replay exit-code parity | `cpg replay` on truncated input logs an Error but exits 0; live stream exits non-zero | `.planning/PROJECT.md` (Planned, "conscious call from PR #16 review") | Low | Scripts keying on exit status silently swallow truncated-replay failures. |
| T-17-06-03 | `k8s.LoadKubeConfig()` takes no `context.Context`; a hang inside kubeconfig load is cancellable by neither `setupCtx` nor `sessionCtx` | `pkg/session/manager.go` (WR-02 comment block) | Medium | Documented residual: the one setup step no bounded wait can reach. Does not block process exit (Shutdown's fan-out is independently bounded), but a hung auth plugin stalls a session Start until the caller kills the process. |

Deferred feature debt (all in `.planning/PROJECT.md` Planned, with IDs `HTTP-FUT-01`, `L7-FUT-01/02`, `DNS-FUT-01/02/03`, `AUD-FUT-02`): HTTP path collapse, kube-dns selector autodetection, ToFQDNs IP→name correlation, `--include-l7-forwarded`, `--min-flows-per-l7-rule`, flag-gated bootstrap CNP apply.

## 2. Large / complex files (hot spots)

- `pkg/policy/builder.go` — 737 lines, 29 functions (`BuildPolicy`, `groupFlows`, `buildIngressRules`/`buildEgressRules`, `fqdnEgressRulesFrom`). The single highest-churn policy-logic file; L7 concerns are split into `pkg/policy/l7.go` and `pkg/policy/companion_dns.go`, but the L4/L7 dispatch all funnels through `peerRules.addFlow` here. Changes to rule attribution (`RuleKey`, `RuleAttribution`) ripple into `pkg/explain` and evidence hashing.
- `pkg/session/manager.go` — 551 lines, one state machine with heavy concurrency invariants (SESS-02 single slot, SESS-05 bounded shutdown, WR-01/02/04 corrections documented inline). The comments ARE the spec; a refactor that drops them loses the safety argument. Test-only seams (`resolveSetupFn`, `detectVersionFn`) are struct fields, not constructor params — touching `NewManager` affects the plan-17 contract.
- `pkg/hubble/aggregator.go` — 661 lines; `pkg/hubble/pipeline.go` — 444 lines of errgroup stage wiring. `pkg/session/manager_test.go` (967) and `pkg/hubble/aggregator_test.go` (1175) are the largest tests — regressions here are expensive to localize.
- `cmd/cpg/mcp_query_flows.go` (497) and `cmd/cpg/mcp_query.go` (489) — pagination/fail-closed cursor logic duplicated in shape across five query tools; a cursor-semantics fix must be applied consistently.

## 3. Fragile areas

### 3.1 Port-forward lifecycle (`pkg/k8s/portforward.go`, 126 lines)

- Cleanup is `close(stopCh)` on a channel created after the SPDY dialer; if `fw.GetPorts()` fails, `stopCh` is closed manually on each of three early-return paths — a fourth return path added later must remember to do the same or the forwarder goroutine leaks.
- `PortForwardToRelay` forwards port `0` (OS-assigned); the local address is only correct for the process lifetime. Session's pipeline goroutine runs `portForwardCleanup()` **before** signaling `s.done` (`pkg/session/manager.go`, launch goroutine) — ordering is load-bearing; do not reorder.
- The relay pod is resolved once by label `k8s-app=hubble-relay` in `kube-system` (`findRelayPod`); a relay restart mid-session kills the gRPC stream — handled as an autonomous session exit (WR-01), not a reconnect. Non-custom-port relays only: `relayPort = 4245` is a const.

### 3.2 Audit-window revert guarantees (`pkg/auditwindow/manager.go`, 485 lines)

- The "cannot structurally be left open" guarantee rests on: `closeOnce`-guarded revert sweep, a goroutine watching `rootCtx.Done()` started **inside `Open`** (manager.go ~line 216), and independently bounded `stopWait`/`removeWait`. If a hard process kill (SIGKILL/OOM) lands between a successful `setFn(enable)` and the `m.ours[uid]` insert (~4 lines later), the flip is recorded nowhere and survives the process — the only window where the guarantee fails. Documented semantics are "every *exit path* reverts", not "every instruction".
- Revert fan-out timeout (`RevertResult.TimedOut` + `PossiblyStuck`, T-23-06): on deadline expiry the map is unreadable race-free and callers must manually verify every owned UID. `runAuditWindow` (`cmd/cpg/audit_window.go`) must surface this — worth re-verifying when editing that command.
- Watcher reconnect is a hand-rolled bounded-backoff loop (deliberately no informer): a newly-created endpoint can be **policy-enforced for the reconnect interval** before its flip lands — an honest, documented race (`docs/bootstrap-runbook.md:121`, `docs/audit-mode.md:66`).
- `flipIfNeeded` skips endpoints whose `readFn`/`setFn` exec fails — a transiently unreachable node stays enforced, only Warn-logged; no retry queue.
- Revert uses `resolveCurrentIDFn` to re-resolve the integer endpoint ID fresh from UID (T-23-05) because Cilium reuses `Status.ID`; any new code reverting by cached ID reintroduces that bug class.

### 3.3 MCP stdout wire (`cmd/cpg/mcp.go`)

- The JSON-RPC transport binds to `os.Stdout` **after** a global swap `os.Stdout = os.Stderr` (D-01 backstop). Any future code capturing `os.Stdout` *before* `server.Run()` binds the wire to stderr and the server silently hangs — the exact failure the comment at the top of the file warns about. Fragile by construction; protected only by stdout-purity tests.

### 3.4 Session stats via v1.0 BUG-01 pattern

- `SessionStats` counters (`flows_seen`, `lost_events`) were stuck at 0 through v1.1 because aggregation happened in goroutines the stats copy outran. Fixed in `pkg/hubble/pipeline.go:321-343` by copying counters only after `g.Wait()`. Regression guard: `TestRunPipeline_PopulatesLostEvents` (`pkg/hubble/pipeline_test.go:256`). Any new async counter must follow the same post-Wait copy discipline.

### 3.5 Classifier drift sentinel

- `pkg/dropclass/version_test.go` (`TestClassifierVersionMatchesGoMod`) forces a manual DropReason audit on every Cilium bump (`ClassifierVersion` suffix must match go.mod). Forgetting the audit silently misclassifies drops on new Cilium versions — the test catches the *version*, not the *audit content*.

## 4. Security posture

- **Readonly default** (`docs/security.md`): `generate`, `replay`, `bootstrap`, `explain`, `mcp` only list/watch/read. No auto-apply — cpg writes YAML to disk only. Phase 19 verified this structurally (RTA/SSA reachability audit: 0 K8s write verbs; 5-function fs allowlist) per `.planning/RETROSPECTIVE.md` v1.5.
- **The one exception: `cpg audit-window`** (`cmd/cpg/audit_window.go` → `pkg/auditwindow`): mutates per-endpoint `PolicyAuditMode` via `pods/exec` into cilium-agent pods (`pkg/k8s/exec.go`). Requires `pods/exec create` scoped to `kube-system` + `ciliumendpoints list/watch`. RBAC scoping limitation (documented, not fixable by cpg): cilium-agent pod names are DaemonSet-generated, so `resourceNames` cannot scope exec to cilium-agent only — the grant is effectively "exec into any pod in kube-system" (`docs/security.md`).
  - Revert-only-ours discipline: endpoints already in audit mode before Open are never touched (`flipIfNeeded`, manager.go ~line 322); daemon-wide audit mode is never modified.
  - Never reverts by cached integer ID (T-23-05); re-resolves via UID.
- **Anti-feature by design:** HTTP header rules (`Authorization`, `Cookie`, …) are never captured or emitted — secret-leakage prevention (`docs/KNOWN_LIMITATIONS.md` §3).
- **Evidence cache:** `$XDG_CACHE_HOME/cpg/evidence/` keyed by SHA-256 of output dir (`pkg/evidence/paths.go`); `ValidatePolicyRef` defends against path traversal from flow-label-derived namespace/workload. Downgrade v1.2→v1.1 requires a manual cache wipe (limitations §12).

## 5. Documented limitations (summarized from `docs/KNOWN_LIMITATIONS.md`, 12 entries)

1. L7 visibility prerequisite — `--l7` yields L4-only without cluster-wide `enable-l7-proxy` + per-workload annotation (by design; `cpg setup-visibility` is a v1.3 wishlist item).
2. HTTP path explosion — one literal regex rule per observed (method, path); no collapse (HTTP-FUT-01, L7-FUT-02).
3. HTTP headers never generated — permanent anti-feature.
4. DNS REFUSED denials surface as `Verdict_FORWARDED`, invisible to the DROPPED-only filter (L7-FUT-01).
5. DNS record lost but IP captured → brittle CIDR rule for cloud LBs (DNS-FUT-03).
6. Hardcoded kube-dns companion selector `k8s-app=kube-dns` in `kube-system` (`pkg/policy/companion_dns.go:27-30`) — wrong on non-vanilla clusters (DNS-FUT-02).
7. `Verdict_REDIRECTED` flows (already-L7-policied traffic) ignored with no summary diagnostic.
8. Unicode/URL-encoding path-matching inconsistency across Cilium versions — `regexp.QuoteMeta`-escaped literals in `pkg/policy/l7.go:98` match only the observed encoding.
9. Dynamic namespace creation after a run produces nothing until re-run (batch tool by design).
10. Companion DNS-53 rule idempotency check is exact-match; partial-overlap selectors produce redundant rules (`ensureKubeDNSCompanion`).
11. `cpg explain` filters are exact-match only (no regex/glob).
12. Evidence schema v2→v1 downgrade needs a manual cache wipe.

## 6. TODO/FIXME/HACK comments

**None.** `rg "TODO|FIXME|HACK|XXX|BUG|WORKAROUND"` over `*.go` finds only test-assertion strings and historical-ID references (`BUG-01` in comments naming the *fixed* v1.0 stats bug, e.g. `pkg/hubble/aggregator.go:104`). Debt is tracked via named IDs (SESS-xx, WR-xx, T-17-06-03, T-23-05/06, LINT-xx, FUT-xx) in doc comments and `.planning/` instead of inline markers — search those IDs, not TODO, when planning.

---

*Concerns analysis: 2026-10-03*
