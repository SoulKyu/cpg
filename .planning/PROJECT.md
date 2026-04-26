# CPG — Cilium Policy Generator

## What This Is

A Go CLI tool that connects directly to Hubble Relay via gRPC, observes dropped/denied network flows in real-time, and automatically generates CiliumNetworkPolicy YAML files. Built for SRE/DevOps teams operating Cilium clusters with default-deny network policies, it eliminates the tedious manual process of writing allow rules by turning observed denials into ready-to-apply policies.

## Core Value

Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.

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

### Active

<!-- Awaiting v1.4 scoping via /gsd:new-milestone. -->

- _(defined during `/gsd:new-milestone`)_

### Planned

<!-- v1.3 candidates carried over from v1.2 deferrals. -->

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

## Current State

**Shipped:** v1.0 (2026-03-08), v1.1 (2026-04-24), v1.2 (2026-04-25), and v1.3 (2026-04-26).

**Codebase:** 10 packages (`pkg/{labels,policy,output,hubble,k8s,dedup,flowsource,evidence,diff,dropclass}` + `cmd/`). New in v1.3: `pkg/dropclass/` (classifier + hints + version) and `pkg/hubble/{health_writer,summary}.go`. **418 tests passing** across 10 packages (up from 319 at v1.2 close). Release-please continues to handle product SemVer tagging.

**Next milestone:** v1.4 — awaiting scoping. See Planned section above for candidates carried over from v1.3 deferrals.

---
*Last updated: 2026-04-26 — v1.3 Cluster Health Surfacing milestone shipped (8 plans, 13 REQs, 418 tests).*
