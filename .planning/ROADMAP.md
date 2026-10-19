# Roadmap: CPG (Cilium Policy Generator)

## Overview

CPG delivers a Go CLI tool that turns Hubble dropped flows into ready-to-apply CiliumNetworkPolicies. v1.0 shipped the core live-streaming generator. v1.1 added an offline iteration workflow (`cpg replay`), per-rule flow evidence, `cpg explain`, and `--dry-run` with unified YAML diff. v1.2 extended generation to L7 (HTTP + DNS) with two-step workflow guidance. v1.3 closed the class of bug where infra-level Hubble drops generated bogus CNPs via a static classifier taxonomy. v1.4 landed audit-driven hardening from a Fable 5 full-code review — 29 confirmed findings fixed, two reachable vulnerabilities patched, and the CI pipeline running (green) for the first time. v1.5 exposes cpg as a readonly MCP stdio server so an LLM harness can run a live Hubble capture session, query dropped flows and generated policies, and review cluster health — cpg stays deterministic while the LLM brings the intelligence. v1.6 closes cpg's remaining onboarding gap — ingesting Cilium's AUDIT verdicts, generating a namespaced default-deny bootstrap artifact and runbook, a lifecycle-bound managed audit window, and cpg-dedicated repo-local skills and agent tooling. v1.7 makes `--l7` policies maintainable at scale via confidence-gated HTTP path generalization (`--l7-collapse-paths`, `--min-flows-per-l7-rule`, default-off byte-identical) and surfaces cpg's batch blind spot by reporting namespaces created during a run (NSAW-01 live watcher / NSAW-02 replay heuristic).

## Milestones

- ✅ **v1.0 MVP (Core Policy Generator)** — Phases 1-3 (shipped 2026-03-08) — [archive](milestones/v1.0-ROADMAP.md)
- ✅ **v1.1 Offline Replay & Policy Analysis** — Phases 4-6 (shipped 2026-04-24) — [archive](milestones/v1.1-ROADMAP.md)
- ✅ **v1.2 L7 Policies (HTTP + DNS)** — Phases 7-9 (shipped 2026-04-25) — [archive](milestones/v1.2-ROADMAP.md)
- ✅ **v1.3 Cluster Health Surfacing** — Phases 10-13 (shipped 2026-04-26) — [archive](milestones/v1.3-ROADMAP.md)
- ✅ **v1.4 Audit Fable5** — Phases 14-15 (shipped 2026-07-20) — [archive](milestones/v1.4-ROADMAP.md)
- ✅ **v1.5 MCP Integration** — Phases 16-19 (shipped 2026-07-22) — [archive](milestones/v1.5-ROADMAP.md)
- ✅ **v1.6 Audit-Mode Onboarding & cpg-Dedicated Agent Tooling** — Phases 20-24 (shipped 2026-07-22) — [archive](milestones/v1.6-ROADMAP.md)
- ✅ **v1.7 L7 Path Generalization & Namespace Awareness** — Phases 25-26 (shipped 2026-10-05) — [archive](milestones/v1.7-ROADMAP.md)

## Phases

<details>
<summary>✅ v1.0 MVP (Phases 1-3) — SHIPPED 2026-03-08</summary>

- [x] Phase 1: Core Policy Engine (3/3 plans) — completed 2026-03-08
- [x] Phase 2: Hubble Streaming Pipeline (2/2 plans) — completed 2026-03-08
- [x] Phase 3: Production Hardening (2/2 plans) — completed 2026-03-08

</details>

<details>
<summary>✅ v1.1 Offline Replay & Policy Analysis (Phases 4-6) — SHIPPED 2026-04-24</summary>

- [x] Phase 4: Offline Replay Core (1/1 plan) — completed 2026-04-24
- [x] Phase 5: Dry-Run & Pipeline Integration (1/1 plan) — completed 2026-04-24
- [x] Phase 6: Explain Command (1/1 plan) — completed 2026-04-24

</details>

<details>
<summary>✅ v1.2 L7 Policies — HTTP + DNS (Phases 7-9) — SHIPPED 2026-04-25</summary>

- [x] Phase 7: L7 Infrastructure Prep (4/4 plans) — completed 2026-04-25
- [x] Phase 8: HTTP L7 Generation (4/4 plans) — completed 2026-04-25
- [x] Phase 9: DNS L7 Generation + explain L7 + Docs (4/4 plans) — completed 2026-04-25

</details>

<details>
<summary>✅ v1.3 Cluster Health Surfacing (Phases 10-13) — SHIPPED 2026-04-26</summary>

- [x] Phase 10: Classifier Core (2/2 plans) — completed 2026-04-26
- [x] Phase 11: Aggregator Suppression & Health Writer (2/2 plans) — completed 2026-04-26
- [x] Phase 12: Session Summary Block (1/1 plan) — completed 2026-04-26
- [x] Phase 13: Flags + Exit Code (3/3 plans) — completed 2026-04-26

</details>

<details>
<summary>✅ v1.4 Audit Fable5 (Phases 14-15) — SHIPPED 2026-07-20</summary>

- [x] Phase 14: Fix Verification + Quality Gates (direct multi-agent workflow) — completed 2026-07-20
- [x] Phase 15: CI Trigger Fix + PR Delivery (PR #16, merged) — completed 2026-07-20

</details>

<details>
<summary>✅ v1.5 MCP Integration (Phases 16-19) — SHIPPED 2026-07-22</summary>

- [x] Phase 16: MCP Server Foundation & Write Safety (3/3 plans) — completed 2026-07-20
- [x] Phase 17: Session Lifecycle (9/9 plans) — completed 2026-07-21
- [x] Phase 18: Query Tools (5/5 plans) — completed 2026-07-21
- [x] Phase 19: Security Hardening & End-to-End Validation (4/4 plans) — completed 2026-07-21

</details>

<details>
<summary>✅ v1.6 Audit-Mode Onboarding & cpg-Dedicated Agent Tooling (Phases 20-24) — SHIPPED 2026-07-22</summary>

- [x] Phase 20: `--include-audit` Verdict Ingestion (4/4 plans) — completed 2026-07-22
- [x] Phase 21: Cilium Compatibility Matrix + Runtime Detection (4/4 plans) — completed 2026-07-22
- [x] Phase 22: Bootstrap Artifact Generation (3/3 plans) — completed 2026-07-22
- [x] Phase 23: Managed Audit Window + SEC-01 Evolution (4/4 plans, CLI-only per gate decision) — completed 2026-07-22
- [x] Phase 24: cpg-Dedicated Skills & Agent Tooling (2/2 plans) — completed 2026-07-22

</details>

<details>
<summary>✅ v1.7 L7 Path Generalization & Namespace Awareness (Phases 25-26) — SHIPPED 2026-10-05</summary>

- [x] Phase 25: L7 Path Collapse Engine (L7GEN-01..04) (2/2 plans) — completed 2026-10-04
- [x] Phase 26: Namespace Creation Awareness (NSAW-01..02) (2/2 plans) — completed 2026-10-05
</details>
