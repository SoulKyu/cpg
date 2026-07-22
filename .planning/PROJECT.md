# CPG — Cilium Policy Generator

## What This Is

A Go CLI tool that connects directly to Hubble Relay via gRPC, observes dropped/denied network flows in real-time, and automatically generates CiliumNetworkPolicy YAML files. Built for SRE/DevOps teams operating Cilium clusters with default-deny network policies, it eliminates the tedious manual process of writing allow rules by turning observed denials into ready-to-apply policies.

## Core Value

Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.

## Current Milestone: v1.5 MCP Integration

**Goal:** Expose cpg as a readonly MCP server (stdio) so an LLM harness can run a live Hubble capture session, analyze dropped flows, and review generated policies — the LLM brings the intelligence, cpg stays deterministic.

**Target features:**
- `cpg mcp` subcommand — MCP server over stdio transport (the harness spawns the process)
- Session tools: start_session / status / stop_session — background Hubble capture
- Ephemeral session tmpdir (`os.MkdirTemp`, respects `$TMPDIR`): policies YAML + evidence + cluster-health.json written via the existing writers with existing FIFO caps — flat memory profile, no parallel in-memory path
- Query tools: dropped flows (dropclass-classified), generated policies, explain/evidence, cluster health — all implemented as readers over the session tmpdir artifacts
- Readonly guarantee: never mutates the cluster, never writes outside the session tmpdir; tmpdir cleaned at stop_session and server shutdown

## Requirements

### Validated

<!-- Shipped and confirmed valuable in v1.0. -->

- ✓ Connect to Hubble Relay via gRPC with auto port-forward — v1.0
- ✓ Override relay address with `--server` flag — v1.0
- ✓ Observe dropped flows filtered by namespace or all-namespaces — v1.0
- ✓ Generate CiliumNetworkPolicy for ingress/egress traffic — v1.0
- ✓ Generate CIDR-based policies for external traffic — v1.0
- ✓ Exact ports (port number + protocol) in generated policies — v1.0
- ✓ Smart label selection (app.kubernetes.io/*, workload name) — v1.0
- ✓ One YAML file per policy in organized directory structure — v1.0
- ✓ Stream policy generation continuously as flows arrive — v1.0
- ✓ Deduplicate against files and live cluster policies — v1.0
- ✓ Structured logging with zap — v1.0
- ✓ Offline replay from Hubble jsonpb captures (`cpg replay <file>`, stdin, gzip) — v1.1
- ✓ Per-rule flow evidence persisted to `$XDG_CACHE_HOME/cpg/evidence` with FIFO caps — v1.1
- ✓ `cpg explain <NS/workload | policy.yaml>` with filters + text/JSON/YAML renderers — v1.1
- ✓ `--dry-run` with unified YAML diff on `generate` and `replay` (ANSI on TTY) — v1.1
- ✓ L7 HTTP policy generation (`toPorts.rules.http`: method + RE2-anchored path) via `--l7` — v1.2
- ✓ L7 DNS policy generation (`toFQDNs.matchName` + paired `dns.matchName`) via `--l7` — v1.2
- ✓ Mandatory companion DNS-53 rule auto-injected for every CNP with `toFQDNs` — v1.2
- ✓ Pre-flight cluster checks (`cilium-config.enable-l7-proxy`, `cilium-envoy` DaemonSet) with warn-and-proceed — v1.2
- ✓ VIS-01 single-warning when `--l7` set but zero L7 records arrive — v1.2
- ✓ Two-step workflow + starter L7-visibility CNP documented in README — v1.2
- ✓ `cpg explain --http-method`/`--http-path`/`--dns-pattern` exact-match filters + L7 rendering — v1.2
- ✓ Drop-reason classifier (`pkg/dropclass/`): O(1) taxonomy across 76 Cilium ≥1.14 DropReason values, Unknown fallback, dedup WARN, semver `ClassifierVersion` — v1.3
- ✓ Aggregator suppresses policy generation for infra/transient drops while preserving `flowsSeen` accuracy — v1.3
- ✓ `cluster-health.json` atomic write (evidence dir): counters by reason × node × workload + Cilium docs remediation URL per reason — v1.3
- ✓ Session summary block to stdout listing infra drops by severity, top-3 nodes/workloads, and the absolute path to `cluster-health.json` — v1.3
- ✓ `--ignore-drop-reason` flag (repeatable, comma-separated, case-insensitive) on `generate` and `replay` with WARN on redundant infra/transient names — v1.3
- ✓ Opt-in `--fail-on-infra-drops` exit code (1) for CI/cron with default behavior unchanged — v1.3
- ✓ All 29 confirmed Fable 5 review findings fixed with regression tests (PR #16, 13 commits, 484 tests) — v1.4
- ✓ CI pipeline running green on `master` for the first time (trigger fix, SHA-pinned actions, pinned tools) — v1.4
- ✓ Zero reachable vulnerabilities: cilium v1.19.4, x/net v0.55.0, `toolchain go1.25.12` (govulncheck clean in CI) — v1.4
- ✓ Genuine stream failures exit non-zero; LostEvents/PoliciesFailed counted; `--timeout` actually applied — v1.4
- ✓ Policy-ref validation (empty/traversal) on evidence and output writers — v1.4
- ✓ `cpg mcp` protocol-safe stdio skeleton: pure JSON-RPC stdout (IOTransport pre-swap capture + global os.Stdout→stderr backstop), zero tools registered (SRV-02) — v1.5 Phase 16
- ✓ Unified stderr logging: zap + go-sdk logs bridged via zapslog (SRV-03) — v1.5 Phase 16
- ✓ Atomic temp+rename policy writes in `pkg/output/writer.go` (torn-read safe for future concurrent MCP readers, SEC-02) — v1.5 Phase 16
- ✓ `pkg/session` single-slot Manager: `capturing → stopped → gone` lifecycle with retention, idempotent stop, autonomous transition on both crash and clean pipeline exit (SESS-02, SESS-03) — v1.5 Phase 17
- ✓ `start_session`/`get_status`/`stop_session` MCP tools wired into `cpg mcp`, opaque session_id, "session not found or expired" contract with D-02 stopped-session queryability (SESS-01, SESS-04, SESS-06) — v1.5 Phase 17
- ✓ Bounded transport-kill cleanup: `Shutdown()` cancels session + setup contexts, per-step deadlines so one wedged cleanup cannot block process exit, tmpdir removal (SESS-05) — v1.5 Phase 17
- ✓ `list_dropped_flows`: paginated composed view (`samples[]` live from evidence + `aggregates[]` from cluster-health.json with `available_after_stop` marker mid-capture), explicit "sampled/aggregated view, not a raw flow log" description (QRY-01) — v1.5 Phase 18
- ✓ `list_policies` (paginated metadata) + `get_policy` (full CNP YAML + absolute tmpdir path, single atomic read) keyed by namespace+workload (QRY-02) — v1.5 Phase 18
- ✓ `get_evidence`: paginated per-rule attribution byte-identical to `cpg explain --output json` via promoted `pkg/explain` (Filter/Output/Render*/ParsePeerLabel exported, flowsource-style promotion) (QRY-03) — v1.5 Phase 18
- ✓ `get_cluster_health`: typed passthrough via `pkg/hubble.ReadClusterHealth`, 4-state branch (capturing → `available_after_stop`; stopped+absent+no-error → "zero drops"; crash → isError citing pipeline error), remediation URLs intact (QRY-04) — v1.5 Phase 18
- ✓ QRY-05 contract on all 8 tools: `structuredContent`+`outputSchema` (explicit `*jsonschema.Schema` dropclass enum — go-sdk has no enum tags), truthful annotations, taxonomy-teaching descriptions, actionable `isError`; opaque fail-closed cursor; `session.DeriveSessionPaths` single source of truth for tmpdir layout — v1.5 Phase 18
- ✓ SEC-01 structural readonly audit: `TestMCPAuditReadonlyReachability` — SSA/RTA callgraph from `runMCPServer`, BFS-filtered to cpg-owned functions, direct-call scan; K8s write verbs (incl. DeleteCollection/UpdateStatus/ApplyStatus) fail unconditionally, fs writes (incl. Chmod/Truncate/Symlink/…) gated by an exact 5-function allowlist; mutation-tested diagnostic (function symbol + call path) — v1.5 Phase 19
- ✓ SRV-01 + SRV-04: real-subprocess stdio e2e — `-race`-built `cpg mcp` driven over real pipes against an in-process fake Hubble observer relay; graceful lifecycle (initialize → 8-tool handshake/schema/annotation proof → session + 5 query tools → stop → clean exit, byte-pure stdout) and ungraceful-disconnect variant (stdin kill → bounded self-exit, tmpdir removed, relay stream cancelled; 5/5 stability under `-race`) — v1.5 Phase 19
- ✓ SEC-03 README `## MCP Server (cpg mcp)` section: harness `env` block (KUBECONFIG/PATH/TMPDIR + why), secrets posture (L7 paths/labels reach the LLM; headers never captured), exec-credential-plugin non-interactive caveat + credential-persistence note, session model — v1.5 Phase 19

### Active

<!-- v1.5 MCP Integration — requirements being defined via /gsd-new-milestone. -->

- _(being defined — see REQUIREMENTS.md once written)_

### Planned

<!-- v1.5 candidates: lint/release debt from the v1.4 audit + feature candidates carried over from earlier deferrals. -->

- [ ] Audit-mode onboarding (default-deny sans casse) — v1.6 candidate: (a) `--include-audit`/`include_audit` — étendre le filtre de verdict à `Verdict_AUDIT` (client.go:198/209/213, file.go:116, aggregator.go:417 — flows AUDIT portent le drop reason, vérifié parser threefour); (b) bootstrap: génération default-deny CNP (`enableDefaultDeny`, Cilium ≥1.15) + activation PolicyAuditMode per-endpoint namespace-scoped; (c) DÉCISION OUVERTE: mutation pilotée par cpg (flag serveur `cpg mcp --enable-audit-bootstrap`, audit = propriété de session, revert lifecycle-bound via fan-out SESS-05, watcher nouveaux pods, revert-only-ours + TTL, SEC-01 évolue en preuve 2-modes, RBAC pods/exec à documenter) vs CLI-only `cpg audit` (MCP reste readonly pur). Discuté 2026-07-22.
- [ ] Lint debt zero: 16 errcheck + 10 staticcheck SA1019 + drop CI `only-new-issues` flag — v1.5 candidate (LINT-01..03, archived in milestones/v1.4-REQUIREMENTS.md)
- [ ] Release hardening: `release.yml` minimal permissions review, govulncheck job pinning follow-through — v1.5 candidate (RELSEC-01..02)
- [ ] `cpg replay` exit-code parity on truncated input (currently logs Error but exits 0; live stream exits non-zero) — v1.5 candidate (conscious call from PR #16 review)
- [ ] `cpg apply` command (dry-run by default, `--force` to apply) — v1.3 candidate
- [ ] Policy consolidation / merging into broader rules — v1.3 candidate
- [ ] Prometheus metrics for long-running instances — v1.3 candidate
- [ ] HTTP path auto-collapse (`--l7-collapse-paths`) + FQDN wildcard inference (`--l7-fqdn-wildcard-depth`) — v1.3 candidate (HTTP-FUT-01, DNS-FUT-01)
- [ ] kube-dns selector autodetection (EKS / GKE / AKS / vanilla) — v1.3 candidate (DNS-FUT-02)
- [ ] ToFQDNs from IP→name correlation when DNS records are missed — v1.3 candidate (DNS-FUT-03)
- [ ] `--include-l7-forwarded` for DNS REFUSED denials surfaced as `Verdict_FORWARDED` — v1.3 candidate (L7-FUT-01)
- [ ] `--min-flows-per-l7-rule N` low-confidence gate — v1.3 candidate (L7-FUT-02)
- [ ] AI-assisted semantic plausibility verdict on `cpg explain` — shelved (see notes below)

<!-- AI feature: design explored on 2026-04-24, spec drafted, then dropped on
     2026-04-25 before implementation. Reasons: hallucination risk on confident
     reasoning, signal quality depends linearly on label hygiene (often poor in
     practice), latency on bulk explain runs, and blast-radius analysis (static,
     deterministic) is more operationally useful than semantic plausibility for
     the same problem space. The design spec lived briefly at
     docs/superpowers/specs/2026-04-24-ai-policy-analysis-design.md
     (commit 7e1e455, removed in v1.2 scoping commit). Recover via git history
     if revisited. -->

### Out of Scope

- Named port resolution — use exact port numbers only
- CiliumClusterwideNetworkPolicy generation — namespace-scoped only
- Web UI or dashboard — CLI tool only

## Context

- Target environment: Kubernetes clusters running Cilium with default-deny policies
- Hubble Relay exposes a gRPC API (protobuf) on port 4245, typically behind a ClusterIP service in kube-system
- Cilium's Go module (`github.com/cilium/cilium`) provides both the observer proto types and the CiliumNetworkPolicy CRD types
- Hubble flow JSON structure includes source/destination identity, labels, namespace, pod name, workload info, traffic direction, ports, and verdict
- The tool replaces the workflow: `hubble observe --verdict DROPPED` → manually read flows → manually write CNP YAML

## Constraints

- **Language**: Go 1.23+ — latest stable, leveraging iterators and rangefunc
- **CLI framework**: cobra — consistent with Cilium/Kubernetes ecosystem tooling
- **Logging**: zap — structured, performant, widely used in K8s ecosystem
- **K8s client**: client-go — direct access to CiliumNetworkPolicy CRDs
- **Hubble integration**: gRPC via cilium/cilium observer proto — no JSON parsing, native types
- **Architecture**: Domain-driven packages — `pkg/{hubble,policy,dedup,k8s,labels}` + `cmd/`

## Key Decisions

<!-- Decisions that constrain future work. Add throughout project lifecycle. -->

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| gRPC only initially | Simplified v1 architecture — offline jsonpb ingestion added in v1.1 for iteration workflow | ✓ Good — v1.1 added `FlowSource` abstraction cleanly |
| Auto port-forward to Hubble Relay | UX parity with hubble CLI, zero manual setup | ✓ Good — shipped v1.0 via SPDY |
| One file per policy output | Easier to review, git-diff friendly, selective apply | ✓ Good |
| Smart label defaults over configurable | Reduces config burden, app.kubernetes.io labels are standard | ✓ Good |
| zap over slog | Team preference, widely used in K8s/Cilium ecosystem | ✓ Good |
| Domain-driven pkg/ structure | Clear separation of concerns, testable packages | ✓ Good — stable through v1.1 (only `flowsource` promoted) |
| Exact ports over named ports | Simpler, no ambiguity, matches flow data directly | ✓ Good |
| Both local + cluster dedup | Comprehensive dedup prevents duplicate policies in all scenarios | ✓ Good — three-layer dedup shipped v1.0 |
| `FlowSource` interface in `pkg/flowsource` | Decouples replay (file) from live (gRPC); testable without Hubble | ✓ Good — shipped v1.1 |
| Evidence under `$XDG_CACHE_HOME/cpg/evidence`, hashed by output dir | Multiple projects don't collide; not committed; XDG-compliant | ✓ Good — shipped v1.1 |
| Evidence JSON schema v1 pinned | Reader rejects unknown versions — safe forward-compat | ✓ Good — shipped v1.1 |
| FIFO caps on samples / sessions | Bounded disk use; `--evidence-samples` / `--evidence-sessions` tunable | ✓ Good — shipped v1.1 |
| Channel fan-out (tee) for policy + evidence writers | Writers independent; neither blocks the other | ✓ Good — shipped v1.1 |
| `--dry-run` covers policies AND evidence | Pure preview semantics — no filesystem side effects | ✓ Good — shipped v1.1 |
| `cpg explain` rejects non-`cpg-` YAML names | Guards against explaining hand-crafted/non-cpg policies | ✓ Good — shipped v1.1 |
| Drop AI-assisted plausibility analysis from v1.2 scope | Signal quality depends on label hygiene; hallucination risk on confident reasoning; blast-radius analysis (static, deterministic) is more operationally useful for the same problem space | — Decided 2026-04-25 |
| v1.2 scoped to L7 policies only | Smaller focused milestone; `cpg apply`, consolidation, metrics deferred to v1.3 | ✓ Good — shipped 2026-04-25 |
| `--l7` opt-in default OFF (no auto-detect) | Preserves v1.1 behavior; avoids silent semantic shift when L7 records appear in flows | ✓ Good — shipped v1.2 |
| HTTP path = `regexp.QuoteMeta` + `^…$` (no inference) | Cilium HTTP path is RE2 regex; under-anchoring matches substrings and broadens allow-list silently | ✓ Good — shipped v1.2 |
| HTTP `headerMatches`/`host`/`hostExact` NEVER emitted (anti-feature) | Risk of leaking `Authorization`/`Cookie`/session tokens into committed YAML | ✓ Good — shipped v1.2 |
| Mandatory companion DNS-53 rule for every `toFQDNs` | Without companion, DNS resolution denied → policy never matches → silent total breakage | ✓ Good — shipped v1.2 (idempotent post-process invariant) |
| Pre-flight checks warn-and-proceed (no abort) | Operators with reduced K8s permissions (CI service accounts) must not be locked out | ✓ Good — shipped v1.2 |
| Evidence schema v2 with no v1 back-compat | v1.1 shipped 2026-04-24 (24h prior); zero production caches; clean cut keeps reader simple | ✓ Good — shipped v1.2 |
| AggKey does NOT extend with L7 fields | L7 is a property of port-rule inside bucket, not of bucket; extending AggKey would shatter buckets | ✓ Good — pipeline structurally unchanged |
| kube-dns companion selector hardcoded `k8s-app=kube-dns` | Auto-detection across CNI distributions adds complexity without v1.2 value | — Deferred to v1.3 (DNS-FUT-02) |
| DROPPED-only verdict filter (kept) | REDIRECTED means Cilium PROXIED; new rules from already-policied traffic would be wrong | ✓ Good — REFUSED gap deferred to v1.3 (L7-FUT-01) |
| CI lint gated `only-new-issues: true` until v1.5 debt cleanup | First-ever CI run exposed 30 pre-existing lint issues; blocking on them would couple the audit PR to a descoped cleanup | — Temporary; drop the flag when LINT-01..03 land (v1.5) |
| `toolchain go1.25.12` directive in go.mod | `setup-go` + `go-version-file` resolves the module minimum (1.25.1) whose stdlib carried 24 fixed CVEs; toolchain directive patches CI and local builds without raising the module floor | ✓ Good — govulncheck green in CI (v1.4) |
| GitHub Actions pinned to release-tag SHAs (verified via `git ls-remote`) | Mutable tags are a supply-chain risk; one agent-suggested pin pointed at an untagged branch commit — verification against real tags is part of the pin | ✓ Good — shipped v1.4 |
| Stream failure ⇒ non-zero exit (behavior change) | Debug-logged clean exits hid mid-capture relay crashes; operators/CI must see failure | ✓ Good — shipped v1.4; replay truncation exit parity deferred (v1.5 candidate) |
| Milestone executed via direct multi-agent workflow (no gsd plans) | Audit remediation with a complete findings inventory doesn't benefit from per-phase planning ceremony; review→verify→fix→PR pipeline replaces it | ✓ Good — v1.4 shipped same-day; keep gsd plans for feature milestones |
| `mcp.IOTransport` with pre-swap stdout capture, never `mcp.StdioTransport{}` | StdioTransport reads package-level os.Stdout lazily inside Connect() — combined with the D-01 global swap it would bind the JSON-RPC wire to stderr and hang the server | ✓ Good — shipped v1.5 Phase 16; acceptance criteria forbid StdioTransport |
| `go.uber.org/zap/exp` v0.3.0 as separate direct dependency | zapslog is NOT bundled in zap v1.27.1 (independently versioned module) — corrected a locked research claim via operator-approved legitimacy gate | ✓ Good — shipped v1.5 Phase 16 |
| Seam-audit identity assertion guarded against test2json aliasing | `go test -json` makes the testing framework alias os.Stderr = os.Stdout in-process; unguarded global-identity assertions flake by mode, not by race | ✓ Good — root-caused and guarded v1.5 Phase 16 |

## Current State

**Shipped:** v1.0 (2026-03-08), v1.1 (2026-04-24), v1.2 (2026-04-25), v1.3 (2026-04-26), and v1.4 (2026-07-20).

**Codebase:** 12 packages (`pkg/{labels,policy,output,hubble,k8s,dedup,flowsource,evidence,diff,dropclass,session,explain}` + `cmd/`). **607 tests passing with `-race`** across 12 packages (up from 539 at Phase 17 close). CI green and operational (build + race tests + lint + govulncheck). Deps: cilium v1.19.4, go-sdk v1.6.1, jsonschema-go v0.4.3 (direct since Phase 18), toolchain go1.25.12. Known debt: 26 lint issues (16 errcheck + 10 SA1019) gated by `only-new-issues`, scoped to v1.5; pre-existing `pkg/session` shared-`/tmp` test flake documented in `phases/18-query-tools/deferred-items.md`. Release-please continues to handle product SemVer tagging.

**Current milestone:** v1.5 MCP Integration — ALL 4 PHASES COMPLETE (16-19, 2026-07-20 → 2026-07-21). Phase 19 (Security Hardening & End-to-End Validation) closed 2026-07-21: SEC-01 audit (mutation-tested), SRV-01/SRV-04 real-stdio e2e both variants green under `-race`, SEC-03 README harness docs; verified 5/5 (one tracking-only gap closed same-day); code review (0 critical, 4 warnings) fixed same-day (audit self-check de-tautologized, verb/fs watchlists extended, subprocess t.Cleanup kill-guard). All 18 v1.5 requirements complete — milestone ready for `/gsd-complete-milestone`. Tests: 610 across 12 packages (607 at Phase 18 close + audit + 2 e2e variants), all `-race`.

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-07-21 — v1.5 Phase 19 (Security Hardening & End-to-End Validation) completed, verified 5/5, review findings fixed; all v1.5 phases (16-19) done.*
