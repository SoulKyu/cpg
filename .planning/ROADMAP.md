# Roadmap: CPG (Cilium Policy Generator)

## Overview

CPG delivers a Go CLI tool that turns Hubble dropped flows into ready-to-apply CiliumNetworkPolicies. v1.0 shipped the core live-streaming generator. v1.1 added an offline iteration workflow (`cpg replay`), per-rule flow evidence, `cpg explain`, and `--dry-run` with unified YAML diff. v1.2 extends generation to L7 (HTTP + DNS) — focused, no other scope.

## Milestones

- ✅ **v1.0 MVP (Core Policy Generator)** — Phases 1-3 (shipped 2026-03-08) — [archive](milestones/v1.0-ROADMAP.md)
- ✅ **v1.1 Offline Replay & Policy Analysis** — Phases 4-6 (shipped 2026-04-24) — [archive](milestones/v1.1-ROADMAP.md)
- 🚧 **v1.2 L7 Policies (HTTP + DNS)** — Phases 7-9 (planning) — see below

## Phases

<details>
<summary>✅ v1.0 MVP (Phases 1-3) — SHIPPED 2026-03-08</summary>

- [x] Phase 1: Core Policy Engine (3/3 plans) — completed 2026-03-08
- [x] Phase 2: Hubble Streaming Pipeline (2/2 plans) — completed 2026-03-08
- [x] Phase 3: Production Hardening (2/2 plans) — completed 2026-03-08

Full details: [milestones/v1.0-ROADMAP.md](milestones/v1.0-ROADMAP.md)

</details>

<details>
<summary>✅ v1.1 Offline Replay & Policy Analysis (Phases 4-6) — SHIPPED 2026-04-24</summary>

- [x] Phase 4: Offline Replay Core (1/1 plan) — completed 2026-04-24
- [x] Phase 5: Dry-Run & Pipeline Integration (1/1 plan) — completed 2026-04-24
- [x] Phase 6: Explain Command (1/1 plan) — completed 2026-04-24

Full details: [milestones/v1.1-ROADMAP.md](milestones/v1.1-ROADMAP.md)

</details>

### 🚧 v1.2 L7 Policies (Phases 7-9)

Scope locked to L7 HTTP + DNS generation only. `cpg apply`, policy consolidation, Prometheus metrics, and AI-assisted analysis are explicitly deferred to v1.3+.

- [ ] **Phase 7: L7 Infrastructure Prep** — internal correctness fixes + evidence schema v2 + pre-flight checks + `--l7` flag plumbing. Zero v1.1 user-visible behavior change.
- [ ] **Phase 8: HTTP L7 Generation** — observable HTTP rule emission via `--l7`; passive L7-empty detection; evidence v2 emission for HTTP rules.
- [ ] **Phase 9: DNS L7 Generation + explain L7 + Docs** — `toFQDNs` rules with mandatory companion DNS allow; `cpg explain` L7 filters and rendering; two-step workflow README.

**Deferred to v1.3 (or later):** `cpg apply`, policy consolidation, Prometheus metrics, AI-assisted analysis (shelved before implementation).

## Phase Details

### Phase 7: L7 Infrastructure Prep
**Goal**: Land the foundational fixes (merge bug, schema v2, pre-flight scaffolding, flag plumbing) so subsequent L7 phases ship correctly. End-of-phase output for v1.1 inputs is byte-identical to v1.1.
**Depends on**: Phase 6 (v1.1 shipped)
**Requirements**: EVID2-01, EVID2-02, EVID2-03, EVID2-04, VIS-04, VIS-05, VIS-06, L7CLI-01
**Success Criteria** (what must be TRUE):
  1. `mergePortRules` preserves the `Rules` field of `PortRule` across merges; a regression test fixed today fails on the previous code (EVID2-03).
  2. Evidence files written by v1.2 carry `schema_version: 2` with the new optional `l7` sub-object on `RuleEvidence`; the reader rejects any `schema_version != 2` with an error message that names `$XDG_CACHE_HOME/cpg/evidence/` and instructs the user to wipe it (EVID2-01, EVID2-02).
  3. `normalizeRule` deterministically sorts L7 lists (HTTP by method+path, DNS by matchName); two policies that differ only by L7 ordering compare equivalent (EVID2-04).
  4. Running `cpg generate --l7` against a cluster with `enable-l7-proxy=false` (or missing `cilium-envoy` DaemonSet) emits a named, actionable warning and proceeds; on `kube-system` RBAC denial, it warns-and-proceeds without abort (VIS-04, VIS-05); `--no-l7-preflight` skips both checks entirely for offline / air-gapped use (VIS-06).
  5. The `--l7` flag is parsed on `cpg generate` and `cpg replay`, threaded through `PipelineConfig`, but flipping it ON does not yet alter generated YAML versus v1.1 — L7 codegen lights up in Phase 8 (L7CLI-01).
**Plans**: 4 plans
- [x] 07-01-PLAN.md — merge.Rules preserve + normalizeRule L7 sort + RuleKey L7 discriminator (EVID2-02/03/04)
- [x] 07-02-PLAN.md — evidence schema v1→v2 with optional L7Ref + reader rejection naming $XDG_CACHE_HOME/cpg/evidence/ (EVID2-01)
- [x] 07-03-PLAN.md — pkg/k8s/preflight.go cilium-config + cilium-envoy checks with warn-and-proceed (VIS-04, VIS-05)
- [x] 07-04-PLAN.md — --l7 / --no-l7-preflight flag plumbing + PipelineConfig.L7Enabled + byte-stability integration test (L7CLI-01, VIS-06)

### Phase 8: HTTP L7 Generation
**Goal**: Users running `cpg generate --l7` (or `cpg replay --l7`) against a cluster with L7 visibility see correct, byte-stable HTTP rules emitted alongside L4 port rules in generated CNP YAML, with passive empty-L7 detection when visibility is missing.
**Depends on**: Phase 7
**Requirements**: HTTP-01, HTTP-02, HTTP-03, HTTP-04, HTTP-05, VIS-01
**Success Criteria** (what must be TRUE):
  1. Given a Hubble flow stream containing `Flow.L7.Http` records for a (src, dst, port) tuple, the emitted CNP carries a `toPorts.rules.http` block on the matching L4 port entry — verifiable by diffing the YAML against a fixture (HTTP-01, HTTP-04).
  2. HTTP method casing is normalized to uppercase regardless of input casing; replaying a fixture with mixed-case methods produces the same byte-stable YAML as the all-uppercase fixture (HTTP-02).
  3. HTTP path is emitted as a Cilium-compatible RE2 regex via `regexp.QuoteMeta` + `^…$` anchoring; a property test asserts the generated regex matches only the literal observed path and rejects under-anchored / unescaped variants (HTTP-03).
  4. Generated YAML never contains `headerMatches`, `host`, or `hostExact` fields — even when Hubble flows carry HTTP headers — verified by a writer-side lint test (HTTP-05).
  5. When `--l7` is set but zero `Flow.L7` records arrive in the observation window, cpg emits a single, actionable warning naming the affected workloads with a link to the README L7 prerequisite section, and the warning fires only via the L7 ingestion path (VIS-01).
**Plans**: 4 plans
- [x] 08-01-PLAN.md — pkg/policy/l7.go pure-function HTTP extraction (extractHTTPRules + normalizeHTTPMethod) with HTTP-05 anti-feature lint test (HTTP-02, HTTP-03, HTTP-05)
- [ ] 08-02-PLAN.md — BuildPolicy L7 codegen branch + AttributionOptions.L7Enabled + RuleKey L7 discriminator + byte-stability when L7 disabled (HTTP-01, HTTP-04)
- [ ] 08-03-PLAN.md — pipeline L7Enabled forwarding + SessionStats.L7HTTPCount + VIS-01 warning + evidence L7Ref emission + l7_http.jsonl fixture (VIS-01, HTTP-01, HTTP-04)
- [ ] 08-04-PLAN.md — end-to-end replay tests for HTTP L7 generation + VIS-01 + README #l7-prerequisites anchor (HTTP-01..HTTP-05, VIS-01)

### Phase 9: DNS L7 Generation + explain L7 + Docs
**Goal**: Users running `cpg generate --l7` (or `cpg replay --l7`) against a cluster with DNS proxy see `toFQDNs` egress rules emitted with a mandatory companion DNS-allow rule; `cpg explain` surfaces L7 attribution; the two-step workflow is documented end-to-end.
**Depends on**: Phase 8
**Requirements**: DNS-01, DNS-02, DNS-03, DNS-04, L7CLI-02, L7CLI-03, VIS-02, VIS-03
**Success Criteria** (what must be TRUE):
  1. Given a Hubble DNS query flow on an egress DROPPED tuple, the emitted CNP contains an egress rule with `toFQDNs.matchName` (literal, trailing-dot stripped) paired with `toPorts.rules.dns.matchName` for that name (DNS-01).
  2. Every CNP that contains `toFQDNs` ALSO contains a companion egress rule allowing UDP+TCP/53 to the hardcoded `k8s-app=kube-dns` selector with a YAML comment naming the assumption — a unit-test invariant asserts the generator NEVER emits `toFQDNs` without the companion (DNS-02).
  3. No `matchPattern` glob is auto-generated from observed DNS names in v1.2 — only `matchName` literals appear in emitted YAML (DNS-03); when no `Flow.L7.Dns` records arrive, cpg falls back to v1.1 CIDR-based egress with byte-identical output to v1.1 (DNS-04).
  4. `cpg explain` accepts `--http-method`, `--http-path`, `--dns-pattern` exact-match filters; rendering an evidence v2 record with L7 attribution shows HTTP method+path or DNS matchName per rule across text/JSON/YAML formats (L7CLI-02, L7CLI-03).
  5. The README documents the two-step workflow (L4 deploy → enable L7 visibility → re-run with `--l7`) and ships a copy-pasteable starter L7-visibility CNP snippet for bootstrapping a workload (VIS-02, VIS-03).
**Plans**: 4 plans
- [ ] 07-01-PLAN.md — merge.Rules preserve + normalizeRule L7 sort + RuleKey L7 discriminator (EVID2-02/03/04)
- [ ] 07-02-PLAN.md — evidence schema v1→v2 with optional L7Ref + reader rejection naming $XDG_CACHE_HOME/cpg/evidence/ (EVID2-01)
- [x] 07-03-PLAN.md — pkg/k8s/preflight.go cilium-config + cilium-envoy checks with warn-and-proceed (VIS-04, VIS-05)
- [ ] 07-04-PLAN.md — --l7 / --no-l7-preflight flag plumbing + PipelineConfig.L7Enabled + byte-stability integration test (L7CLI-01, VIS-06)
**UI hint**: no

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Core Policy Engine | v1.0 | 3/3 | Complete | 2026-03-08 |
| 2. Hubble Streaming Pipeline | v1.0 | 2/2 | Complete | 2026-03-08 |
| 3. Production Hardening | v1.0 | 2/2 | Complete | 2026-03-08 |
| 4. Offline Replay Core | v1.1 | 1/1 | Complete | 2026-04-24 |
| 5. Dry-Run & Pipeline Integration | v1.1 | 1/1 | Complete | 2026-04-24 |
| 6. Explain Command | v1.1 | 1/1 | Complete | 2026-04-24 |
| 7. L7 Infrastructure Prep | v1.2 | 0/? | Not started | — |
| 8. HTTP L7 Generation | v1.2 | 0/? | Not started | — |
| 9. DNS L7 Generation + explain L7 + Docs | v1.2 | 0/? | Not started | — |

**Milestone status:** v1.0 ✅ shipped · v1.1 ✅ shipped · v1.2 🚧 phases 7-9 planned, awaiting plan-phase
