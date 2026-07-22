# Milestones — CPG (Cilium Policy Generator)

## v1.5 MCP Integration (Shipped: 2026-07-22)

**Phases completed:** 4 phases (16-19), 21 plans, 44 tasks
**Delivered:** `cpg mcp` — a readonly MCP server over stdio: one live Hubble capture session at a time, 8 tools (3 session + 5 query), structural readonly proof (RTA/SSA audit, mutation-tested), real-subprocess stdio e2e under `-race` (graceful + ungraceful-disconnect), README harness docs. Tests 484 → 610, all `-race`. Shipped via PR #18 (merge `81ebf2c`), incl. same-day fix of GO-2026-5970 (x/text v0.39.0).
**Timeline:** 2026-07-20 → 2026-07-22 · **Requirements:** 18/18 complete (SRV-01..04, SESS-01..06, QRY-01..05, SEC-01..03)
**Known deferred items at close:** 3 (pre-v1.5 quick-task artifacts lacking closure markers, work shipped in April — see STATE.md Deferred Items; re-acknowledged at this close)

**Key accomplishments:**

- Same-dir temp+rename atomic write for `pkg/output/writer.go` with explicit 0644 chmod, proven torn-read-safe by a race-clean concurrent writer/reader test.
- Pinned github.com/modelcontextprotocol/go-sdk v1.6.1 into go.mod/go.sum behind an operator-approved Package Legitimacy Gate
- `cpg mcp` cobra subcommand wiring a zero-tool go-sdk v1.6.1 server over `mcp.IOTransport` (stdout captured before the `os.Stdout=os.Stderr` backstop swap), with go-sdk logs bridged into cpg's existing stderr zap stream via the newly-added `go.uber.org/zap/exp/zapslog` dependency, verified by a reusable in-memory-transport stdout-purity harness.
- Nil-safe `PipelineConfig.OnFinal func(SessionStats)` hook fired exactly once at end-of-run with fully populated stats, landed and tested as the interface-first foundation for 17-02's session manager.
- `pkg/session`'s state model (capturing/stopped), MCP result shapes (StartResult/StatusResult/StopResult), and `buildPipelineConfig` — a session-tmpdir-scoped port of `generate.go`'s `PipelineConfig` recipe that makes a zero-value `flush_interval`/`timeout` crash structurally impossible.
- Mutex-guarded `pkg/session.Manager` (Start/Status/Stop/Shutdown) implementing the capturing→stopped→gone state machine with a TOCTOU-safe slot claim, copy-under-lock reads, an injectable setup seam, and a 16-test `-race` suite closing both concurrency Blockers identified in research.
- `start_session`/`get_status`/`stop_session` registered with typed schemas in `cmd/cpg/mcp_tools.go`, `runMCPServer` now constructs `pkg/session.Manager` from its own server-root ctx with `mcpModeStdout()` wired and calls `mgr.Shutdown()` after `server.Run` returns — completing the Phase 16 stdout handoff and the SESS-05 cleanup fan-out, proven by 3 cluster-free integration tests over the in-memory transport.
- Session.pipelineErr atomic slot + a guarded autonomous State transition make `get_status` truthfully report a crashed Hubble capture as "stopped" instead of "capturing" forever, closing reopened gap WR-01 (Truth 2 / SESS-03) and WR-04's DropReason key-collapse bug.
- context.AfterFunc(sessionCtx, setupCancel) merges Shutdown's cancellation into an in-flight Start()'s setup phase, and a 24h maxSessionDuration ceiling bounds MCP-supplied timeout/flush_interval — closing reopened gaps WR-02 (Truth 4 / SESS-05) and WR-03.
- Reworded ROADMAP Phase 17 SC5 and REQUIREMENTS SESS-06 to scope the "not found or expired" error to unknown/purged/replaced `session_id` and cite D-02's retained-stopped-session-stays-queryable behavior, closing the 17-VERIFICATION.md Truth 5 documentation gap.
- Reclassified the launch goroutine's genuine-failure guard on `sessionCtx.Err() == nil` (not the returned error's identity) and added `Session.explicitStopSeen atomic.Bool` swapped at both `Stop()` call sites — closing WR-01 (a scoped dial-timeout `DeadlineExceeded` no longer wedges `get_status` at "capturing" forever) and WR-02 (the first `stop_session` after an autonomous crash no longer wrongly reports `already_stopped: true`).
- Broadened `pkg/session/manager.go`'s autonomous-exit guard from `err != nil && sessionCtx.Err() == nil` to `sessionCtx.Err() == nil` alone, so a pipeline that drains cleanly (nil error, e.g. Hubble Relay closing its gRPC stream on io.EOF) now autonomously transitions the session to stopped instead of wedging at "capturing" forever.
- Moved `cpg explain`'s filter/render logic into an importable `pkg/explain` package (exported `Filter.Match`, `Output`, `RenderJSON/RenderText/RenderYAML`, `ParsePeerLabel`), thinning `cmd/cpg/explain.go` to call it — byte-identical CLI output, ready for the get_evidence MCP tool to reuse directly.
- Exported `output.ReadPolicyFile` and `hubble.ReadClusterHealth` with a uniform wrapped-`fs.ErrNotExist` reader contract, plus a regression test proving `hw.finalize()` writes `cluster-health.json` unconditionally even after a mid-capture pipeline error.
- registerQueryTools composition root plus list_policies/get_policy/get_cluster_health MCP tools, with get_cluster_health's D-13 crash-vs-healthy branch logic factored into a directly unit-tested pure function.
- Shared mustQuerySchema/cursor/paginate primitives plus get_evidence (QRY-03): paginated per-rule flow evidence reusing pkg/explain.Filter/Output verbatim, with a direction-enum schema and D-05/D-06 fail-closed cursor handling.
- list_dropped_flows (QRY-01) ships the honest two-section samples[]/aggregates[] composed view with combined pagination, dropclass/direction enum schema, and closes Phase 18 with the final 8-tool integration test (QRY-05 fully satisfied).
- Re-runnable Go test proves zero K8s write verbs and zero unallowlisted filesystem writes are reachable from the MCP composition root, via RTA callgraph + direct SSA call-instruction scan; golang.org/x/tools promoted to a direct test-only dependency with zero new go.sum hashes.
- Real `-race`-built `cpg mcp` subprocess driven over OS stdin/stdout pipes against an in-process fake Hubble gRPC relay, proving the full session lifecycle and the all-8-tool SRV-01 handshake on real stdio (not the in-memory transport).
- New `## MCP Server (cpg mcp)` README section covering the 8-tool table, harness `env` (KUBECONFIG/PATH/TMPDIR) contract, LLM secrets posture, and the exec-credential-plugin non-interactive-hang caveat — written from scratch since README had zero prior MCP mentions.
- TestMCPE2EUngracefulDisconnect proves the SESS-05 bounded cleanup fan-out fires on transport death (no stop_session): bounded self-exit, tmpdir removal, and fake-relay stream cancellation -- stabilized against a real, empirically-reproduced async race by adding an artifact-based synchronization gate beyond the planned relay-reached signal.

---

Historical record of shipped milestones. Each entry links to its archived roadmap and requirements.

---

## v1.4 — Audit Fable5 ✅

**Shipped:** 2026-07-20
**Phases:** 14 → 15 (2 phases, executed via direct multi-agent workflow — no gsd plans)
**Tests:** 484 across 10 packages (up from 418 at v1.3 close)
**Archives:** [roadmap](milestones/v1.4-ROADMAP.md) · [requirements](milestones/v1.4-REQUIREMENTS.md) · [audit](milestones/v1.4-MILESTONE-AUDIT.md)

**Delivered:** Audit-driven hardening from a Fable 5 full-code review (9 section reviewers + 18 adversarial verifiers over ~17.3k lines): all 29 confirmed findings fixed with regression tests and merged as PR #16 (13 commits incl. merge, 44 files, +1163/−204). The repository's CI ran — and went green — for the first time ever: the workflow had always triggered on `main` while the default branch is `master`.

**Highlights:**

- CI bring-up chain: trigger fixed to `master`; actions pinned to release-tag SHAs; golangci-lint v2.12.2 / govulncheck v1.6.0 pinned; `only-new-issues: true` until the v1.5 lint cleanup; two reachable vulns fixed (cilium v1.19.1→v1.19.4 GO-2026-5914, x/net→v0.55.0 GO-2026-5026); `toolchain go1.25.12` for a patched stdlib (24 stdlib CVEs at 1.25.1).
- Correctness: `MergePolicy` nil-Spec panic guard; content-aware dedup sort keys (order-independent `PoliciesEquivalent`); multi-entry ICMP merge; DNS-consumed flows no longer emit a redundant bare `ToCIDR:53` rule.
- Failure visibility: genuine Hubble stream failures surface as non-zero exit (were debug-logged + exit 0); `LostEvents` populated; `PoliciesFailed` counter; truncated replay reported at Error; `--timeout` actually applied via gRPC readiness gate.
- Robustness: policy-ref validation (empty/traversal) on both evidence and output writers; empty-`Direction` render guard; FILTER-03 warned once; `json:` tags where sigs.k8s.io/yaml requires them.
- Process: adversarial verification refuted 2/31 findings pre-fix (incl. one placeholder emitted by a review agent); an independent Opus review of the PR caught `actions/checkout` pinned to an untagged branch-tip commit and one under-scoped fix — both corrected pre-merge. `ClassifierVersion` → `1.0.0-cilium1.19.4` (DropReason audit: no new values).
- Known deferred items at close: 3 v1.3 quick-task artifacts acknowledged (see STATE.md Deferred Items); lint debt 16 errcheck + 10 SA1019 and release hardening scoped to v1.5.

---

## v1.3 — Cluster Health Surfacing ✅

**Shipped:** 2026-04-26
**Phases:** 10 → 13 (4 phases, 8 plans)
**Tests:** 418 across 10 packages (up from 319 at v1.2)
**Archives:** [roadmap](milestones/v1.3-ROADMAP.md) · [requirements](milestones/v1.3-REQUIREMENTS.md) · [audit](milestones/v1.3-MILESTONE-AUDIT.md)

**Delivered:** cpg now distinguishes policy drops from infrastructure-level Hubble drops. Only true policy denials produce a CNP; infra/transient drops (CT_MAP_INSERTION_FAILED, BPF errors, etc.) are surfaced separately via `cluster-health.json` and a session summary block. New `--ignore-drop-reason` flag (parity with `--ignore-protocol`) and opt-in `--fail-on-infra-drops` exit code (1) for CI integration. Default exit behavior unchanged. Closes the v1.2 class-of-bug where `cpg-mmtro-adserver` CNPs were generated for conntrack-map-full drops.

**Highlights:**

- New `pkg/dropclass/` package: O(1) classifier (11.62 ns/op) for all 76 Cilium v1.19.1 `DropReason` values across 4 buckets (Policy / Infra / Transient / Noise) + Unknown fallback. ~94% of values are non-policy.
- Pure-policy reasons (only generate CNPs for these): `POLICY_DENIED`, `POLICY_DENY`, `AUTH_REQUIRED` (with `needs_review` annotation), `DENIED_BY_LB_SRC_RANGE_CHECK`.
- Unknown DropReason values default to `Unknown` (never `Policy`) + dedup WARN once per unique int32 via `sync.Map` — safe for future Cilium proto bumps.
- `cluster-health.json` (schema v1) at `$XDG_CACHE_HOME/cpg/evidence/<hash>/`: counters by reason × node × workload + Cilium docs remediation URL per reason. Atomic write (CreateTemp + Rename).
- Filter precedence in aggregator: `--ignore-drop-reason` → `--ignore-protocol` → classification gate → `keyFromFlow`. Infra/Transient drops STILL increment `flowsSeen` (observed traffic remains accurate; only CNP generation is gated).
- Session summary block to stdout (NOT logger) listing infra drops by severity, top-3 nodes/workloads, and the absolute path to `cluster-health.json`. Empty when zero infra drops.
- `--fail-on-infra-drops` → exit 1 (NOT 2 — avoids Terraform GitOps collision). Default cpg behavior unchanged for backward compat.
- README documents both new flags + dedicated `## Exit codes` section + CI cron pattern.
- 8 plans, all TDD-first commits (failing test → impl), auditable in `git log`. ClassifierVersion semver constant (`1.0.0-cilium1.19.1`) embedded in cluster-health.json for cross-release traceability.

---

## v1.2 — L7 Policies (HTTP + DNS) ✅

**Shipped:** 2026-04-25
**Phases:** 7 → 9 (3 phases, 12 plans)
**Tests:** 319 across 9 packages (up from 180 at v1.1)
**Archives:** [roadmap](milestones/v1.2-ROADMAP.md) · [requirements](milestones/v1.2-REQUIREMENTS.md) · [audit](v1.2-MILESTONE-AUDIT.md)

**Delivered:** cpg generates L7 CiliumNetworkPolicies (HTTP method+path, DNS FQDN matchName) from observed Hubble L7 flows when `--l7` is passed. The two-step workflow (deploy L4 → enable L7 visibility → re-run with `--l7`) is documented end-to-end with pre-flight cluster checks, single-warning empty-records detection, starter visibility CNP, and L7-aware `cpg explain` rendering. Default behavior without `--l7` is byte-identical to v1.1.

**Highlights:**

- HTTP rule generation: `regexp.QuoteMeta` + `^…$` anchored paths, uppercase method normalization, no `headerMatches`/`host`/`hostExact` (anti-feature: secret-leak risk).
- DNS rule generation: `toFQDNs.matchName` (literal, trailing-dot stripped) + `toPorts.rules.dns.matchName` paired; mandatory companion DNS-53 rule auto-injected (idempotent post-process); no `matchPattern` glob in v1.2.
- Pre-flight checks: ConfigMap `kube-system/cilium-config.enable-l7-proxy=true` + `cilium-envoy` DaemonSet (Cilium ≥1.16) or `enable-envoy-config` (1.14–1.15). RBAC denied → warn-and-proceed. `--no-l7-preflight` flag for restricted-RBAC / air-gapped use.
- Evidence schema v1 → v2 (no back-compat layer; reader rejects ≠2 with `$XDG_CACHE_HOME/cpg/evidence/` instruction).
- `cpg explain --http-method`/`--http-path`/`--dns-pattern` (exact-match) + L7 rendering across text/JSON/YAML.
- Latent `mergePortRules` Rules-field-drop bug fixed (would have been silent L7 data loss in production).
- 12 plans, all TDD-first commits (failing test → impl), auditable in `git log`.

---

## v1.1 — Offline Replay & Policy Analysis ✅

**Shipped:** 2026-04-24
**Phases:** 4 → 6 (3 phases, 3 plans)
**Archives:** [roadmap](milestones/v1.1-ROADMAP.md) · [requirements](milestones/v1.1-REQUIREMENTS.md)

**Delivered:** Offline iteration workflow (`cpg replay`), per-rule flow evidence persisted to `$XDG_CACHE_HOME/cpg/evidence`, `cpg explain` with filters and multi-format output, `--dry-run` with unified YAML diff on both `generate` and `replay`.

**Highlights:**

- `FlowSource` interface promoted to `pkg/flowsource`; jsonpb FileSource with gzip + stdin.
- Evidence JSON schema v1 with FIFO caps; atomic writer; schema-version-aware reader.
- `BuildPolicy` returns `[]RuleAttribution` threaded through the pipeline via `PolicyEvent`.
- Channel fan-out: single `policies` channel tees into `policyCh` + `evidenceCh`; writers independent.
- `cpg explain <NS/workload | policy.yaml>` with `--ingress/--egress/--port/--peer/--peer-cidr/--since`, text/JSON/YAML renderers.
- 180 tests pass across 9 packages.

---

## v1.0 — MVP (Core Policy Generator) ✅

**Shipped:** 2026-03-08 (archived retroactively on 2026-04-24)
**Phases:** 1 → 3 (3 phases, 7 plans)
**Archives:** [roadmap](milestones/v1.0-ROADMAP.md) · [requirements](milestones/v1.0-REQUIREMENTS.md)

**Delivered:** Go CLI (`cpg generate`) that connects to Hubble Relay via gRPC, observes dropped flows in real-time, and produces ready-to-apply CiliumNetworkPolicy YAML with smart label selection, CIDR rules for external traffic, file + cluster deduplication, and auto port-forward.

**Highlights:**

- Correct ingress/egress CNP generation with exact port + protocol and `app.kubernetes.io/*` label selectors.
- Live Hubble streaming pipeline with namespace filtering and LostEvents warnings.
- CIDR-based rules for world identity (external traffic).
- Three-layer dedup: file-on-disk, cross-flush in-session, and live cluster via client-go.
- Auto port-forward to hubble-relay via SPDY.
- Domain-driven package structure (`pkg/{labels,policy,output,hubble,k8s,dedup}`) — stable through v1.1.

---

## Notes on tagging

The repository uses release-please for SemVer tagging of product releases (`v1.0.0`, `v1.1.0`, … `v1.6.0`). GSD milestone versions (`v1.0`, `v1.1`) are a **planning concept**, not a git tag — they describe roadmap milestones, not binary releases. No `v1.0` / `v1.1` git tags are created by `/gsd:complete-milestone` to avoid collision with release-please.
