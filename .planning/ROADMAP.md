# Roadmap: CPG (Cilium Policy Generator)

## Overview

CPG delivers a Go CLI tool that turns Hubble dropped flows into ready-to-apply CiliumNetworkPolicies. v1.0 shipped the core live-streaming generator. v1.1 added an offline iteration workflow (`cpg replay`), per-rule flow evidence, `cpg explain`, and `--dry-run` with unified YAML diff. v1.2 extended generation to L7 (HTTP + DNS) with two-step workflow guidance. v1.3 closes the class of bug where infra-level Hubble drops (e.g. `CT_MAP_INSERTION_FAILED`) generated bogus CNPs — a static classifier taxonomy gates policy generation and surfaces cluster health separately.

## Milestones

- ✅ **v1.0 MVP (Core Policy Generator)** — Phases 1-3 (shipped 2026-03-08) — [archive](milestones/v1.0-ROADMAP.md)
- ✅ **v1.1 Offline Replay & Policy Analysis** — Phases 4-6 (shipped 2026-04-24) — [archive](milestones/v1.1-ROADMAP.md)
- ✅ **v1.2 L7 Policies (HTTP + DNS)** — Phases 7-9 (shipped 2026-04-25) — [archive](milestones/v1.2-ROADMAP.md)
- 📋 **v1.3 Cluster Health Surfacing** — Phases 10-13 (in progress)

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

<details>
<summary>✅ v1.2 L7 Policies — HTTP + DNS (Phases 7-9) — SHIPPED 2026-04-25</summary>

- [x] Phase 7: L7 Infrastructure Prep (4/4 plans) — completed 2026-04-25
- [x] Phase 8: HTTP L7 Generation (4/4 plans) — completed 2026-04-25
- [x] Phase 9: DNS L7 Generation + explain L7 + Docs (4/4 plans) — completed 2026-04-25

Full details: [milestones/v1.2-ROADMAP.md](milestones/v1.2-ROADMAP.md)

</details>

### 📋 v1.3 Cluster Health Surfacing (Phases 10-13)

- [x] **Phase 10: Classifier Core** - Drop-reason taxonomy in `pkg/dropclass/` covering all Cilium ≥1.14 DropReason values with stable versioning (completed 2026-04-26)
- [x] **Phase 11: Aggregator Suppression + Health Writer** - Aggregator gates CNP generation on drop class; infra/transient flows written to `cluster-health.json` (completed 2026-04-26)
- [x] **Phase 12: Session Summary Block** - End-of-run summary listing infra drops by severity with top-3 nodes/workloads and path to `cluster-health.json` (completed 2026-04-26)
- [ ] **Phase 13: Flags + Exit Code** - `--ignore-drop-reason`, `--fail-on-infra-drops`, `--dry-run` parity, and README CI/cron documentation

## Phase Details

### Phase 10: Classifier Core
**Goal**: Every Hubble drop reason is classified into a stable taxonomy (policy / infra / transient / unknown) with a versioned, auditable mapping embedded in `pkg/dropclass/`
**Depends on**: Nothing (first phase of v1.3)
**Requirements**: CLASSIFY-01, CLASSIFY-02, CLASSIFY-03
**Success Criteria** (what must be TRUE):
  1. Every Cilium ≥1.14 DropReason enum value maps to exactly one of {policy, infra, transient, unknown} — no value returns an unclassified result at runtime
  2. An unrecognized DropReason (e.g. from a future Cilium version) resolves to `unknown` (never `policy`) and emits a single deduplicated WARN log per unique unrecognized value across the session
  3. `cluster-health.json` carries a `classifierVersion` semver string that identifies the taxonomy version, enabling operators to detect when reason mappings changed between cpg releases
**Plans**: 2 plans

Plans:
- [x] 10-01-taxonomy-and-hints-PLAN.md — DropClass enum, O(1) taxonomy map (76 reasons), RemediationHint URL table, ClassifierVersion, ValidReasonNames()
- [x] 10-02-unknown-dedup-warn-PLAN.md — SetWarnLogger + sync.Map dedup WARN for unrecognized reasons

### Phase 11: Aggregator Suppression + Health Writer
**Goal**: cpg never generates a CiliumNetworkPolicy for infra or transient drops, and all non-policy drops are aggregated into `cluster-health.json` with per-reason counters and remediation hints
**Depends on**: Phase 10
**Requirements**: HEALTH-01, HEALTH-02, HEALTH-04, HEALTH-05
**Success Criteria** (what must be TRUE):
  1. A flow with drop reason `CT_MAP_INSERTION_FAILED` (or any other infra/transient reason) produces zero CNPs — no `cpg-*` YAML file is written or modified
  2. `cluster-health.json` is written atomically to `$XDG_CACHE_HOME/cpg/evidence/<hash>/` with counters keyed by `reason × node × workload` and a remediation-hint URL for each reason
  3. Infra and transient drops still increment `flowsSeen` — the observed traffic count remains accurate and complete
  4. Running with `--dry-run` leaves `cluster-health.json` unwritten (filesystem untouched, matching policy and evidence dry-run behavior)
**Plans**: 2 plans

Plans:
- [x] 11-01-aggregator-classification-gate-PLAN.md — Classification gate in Run(): Infra/Transient suppressed, infraDrops counter, DropEvent channel, flowsSeen invariant
- [x] 11-02-health-writer-and-pipeline-wiring-PLAN.md — healthWriter atomic JSON write + pipeline third-channel wiring + dry-run gate

### Phase 12: Session Summary Block
**Goal**: Users see a concise cluster-health summary at the end of every `generate` and `replay` run so infra-level drop events are never silently lost
**Depends on**: Phase 11
**Requirements**: HEALTH-03
**Success Criteria** (what must be TRUE):
  1. At the end of every `generate` and `replay` run that observed ≥1 infra drop, a summary block is printed listing each observed infra drop reason sorted by severity (critical first)
  2. The summary shows the top-3 nodes and top-3 workloads by infra drop volume
  3. The summary includes the absolute path to `cluster-health.json` so operators can open it directly
  4. When zero infra drops were observed, the summary block is omitted entirely (no noise on healthy-cluster runs)
**Plans**: 1 plan

Plans:
- [x] 12-01-session-summary-block-PLAN.md — PrintClusterHealthSummary + healthWriter.Snapshot() + PipelineConfig.Stdout + pipeline wiring

### Phase 13: Flags + Exit Code
**Goal**: Users can exclude specific drop reasons from processing and wire cpg into CI/cron pipelines with a deterministic non-zero exit when infra drops are detected
**Depends on**: Phase 12
**Requirements**: FILTER-01, FILTER-02, FILTER-03, EXIT-01, EXIT-02
**Success Criteria** (what must be TRUE):
  1. `--ignore-drop-reason REASON` (repeatable, comma-separated, case-insensitive) excludes matching flows before classification on both `generate` and `replay` subcommands
  2. Passing an unrecognized reason name fails at flag-parse time with an error message that lists all valid reason names
  3. Passing a reason already classified as `infra` or `transient` to `--ignore-drop-reason` emits a WARN noting the redundancy rather than silently accepting or rejecting the flag
  4. `--fail-on-infra-drops` exits cpg with code 1 when ≥1 infra drop is observed; without the flag, cpg always exits 0 regardless of infra drop count
  5. README documents exit-code semantics and includes a copy-pasteable CI cron pattern using `--fail-on-infra-drops`
**Plans**: TBD

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Core Policy Engine | v1.0 | 3/3 | Complete | 2026-03-08 |
| 2. Hubble Streaming Pipeline | v1.0 | 2/2 | Complete | 2026-03-08 |
| 3. Production Hardening | v1.0 | 2/2 | Complete | 2026-03-08 |
| 4. Offline Replay Core | v1.1 | 1/1 | Complete | 2026-04-24 |
| 5. Dry-Run & Pipeline Integration | v1.1 | 1/1 | Complete | 2026-04-24 |
| 6. Explain Command | v1.1 | 1/1 | Complete | 2026-04-24 |
| 7. L7 Infrastructure Prep | v1.2 | 4/4 | Complete | 2026-04-25 |
| 8. HTTP L7 Generation | v1.2 | 4/4 | Complete | 2026-04-25 |
| 9. DNS L7 Generation + explain L7 + Docs | v1.2 | 4/4 | Complete | 2026-04-25 |
| 10. Classifier Core | v1.3 | 2/2 | Complete    | 2026-04-26 |
| 11. Aggregator Suppression + Health Writer | v1.3 | 2/2 | Complete    | 2026-04-26 |
| 12. Session Summary Block | v1.3 | 1/1 | Complete    | 2026-04-26 |
| 13. Flags + Exit Code | v1.3 | 0/? | Not started | - |

**Milestone status:** v1.0 ✅ shipped · v1.1 ✅ shipped · v1.2 ✅ shipped · v1.3 📋 in progress
