# Requirements: CPG — Cilium Policy Generator

**Defined:** 2026-07-22 (milestone v1.6)
**Core Value:** Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.
**Milestone:** v1.6 — Audit-Mode Onboarding & cpg-Dedicated Agent Tooling
**Ideation source:** [drafts/v1.6-audit-onboarding-and-cpg-agent-tooling.md](drafts/v1.6-audit-onboarding-and-cpg-agent-tooling.md) · **Research:** [research/SUMMARY.md](research/SUMMARY.md)

## v1.6 Requirements

Requirements for this milestone. Each maps to roadmap phases.

### Audit-Mode Onboarding

- [x] **AUD-01**: Operator can ingest `Verdict_AUDIT` flows into policy generation via `--include-audit` (on `generate` and `replay`) and `include_audit` (MCP `start_session` arg); default behavior without the flag stays byte-identical (regression-tested); a single VIS-01-style warning fires when the flag is set but zero AUDIT flows arrive
- [x] **AUD-02**: Operator can generate a namespaced default-deny bootstrap artifact via `cpg bootstrap -n <ns>` and a readonly MCP tool — CNP carrying `enableDefaultDeny` **and** explicit empty-rule stanzas `ingress: [{}]`/`egress: [{}]` (one-element empty-rule form; the literal `ingress: []` IS the cilium/cilium#35558 bug — see 22-RESEARCH.md Pitfall 1; named, tested acceptance criterion), version-gated on Cilium ≥ 1.16 (never a silently-pruned field), plus an audit-window runbook modeled on Cilium's "Creating Policies from Verdicts" with an active warning against daemon-wide `policy-audit-mode`
- [x] **AUD-03**: Operator can open a managed audit window on a namespace — per-endpoint `PolicyAuditMode` flips via `pods/exec`, new-endpoint watcher (`CiliumEndpoint`-based), revert-only-ours bookkeeping keyed on UID (never raw endpoint ID), TTL auto-revert — with revert riding the existing SESS-05 bounded cleanup fan-out on every exit path; the surface (MCP flag-gated session property vs CLI-only command) is an explicit discuss-phase decision before the phase is planned
- [x] **AUD-04**: SEC-01's structural proof truthfully covers the audit-window mutation — mechanism (build-tag split vs path-scoped reachability assertion) recorded as a PROJECT.md Key Decision **before** any mutation code lands — and the README readonly guarantee is reworded to "readonly by default; scoped, lifecycle-bound mutations behind an explicit launch flag"

### cpg-Dedicated Agent Tooling (repo-local only)

- [x] **SKL-01**: `cpg-triage` skill drives a live MCP session end-to-end: start → classify drops (policy vs infra) → present each CNP with its evidence → recommend what to apply
- [x] **SKL-02**: `cpg-audit-onboard` skill guides/drives the full onboarding workflow: bootstrap → audit window → `include_audit` capture → enforce checklist
- [x] **SKL-03**: `cpg-policy-review` skill audits generated CNPs offline (over-broad rules, L7 anchoring, missing DNS-53 companions, dedup sanity) via `cpg explain` + evidence
- [x] **SKL-04**: `cpg-health-report` skill turns `cluster-health.json` into an HTML report of infra drops by node/workload with Cilium remediation links
- [x] **SKL-05**: `cpg-mcp-smoke` skill runs a post-release smoke of the tagged binary against the e2e fake relay: 8-tool handshake + full session lifecycle
- [x] **SKL-06**: `cpg-operator` subagent (single repo-local agent driving MCP sessions) is used by `cpg-triage`/`cpg-audit-onboard` instead of per-skill agents

All SKL artifacts live in this repo (`.claude/skills/cpg-*/SKILL.md`, `.claude/agents/cpg-operator.md`), are written as workflow routers pointing at live `tools/list` discovery (never a third copy of tool semantics), and carry a consistency tripwire tying skill/README prose back to the Go `Description:` strings.

### Cilium Compatibility

- [ ] **COMPAT-01**: README "Supported Cilium versions" section declares one documented floor + a per-feature table using the PR-verified numbers (`cilium-dbg` rename = 1.15, `enableDefaultDeny` = 1.16, `proxy-visibility` removed = 1.17), with the same merged-PR + release-tag verification completed for the remaining unpinned entries (`Verdict_AUDIT`/`PolicyVerdictNotify` vintage, observer gRPC API window)
- [ ] **COMPAT-02**: cpg detects the cluster's Cilium version at connect (source-of-truth: Hubble `ServerStatus`/`GetNodes` vs DaemonSet image tag — decided during phase planning), warns-and-proceeds below floor naming the affected features (never aborts, stays privilege-neutral — no `pods/exec`), gates version-dependent behavior (bootstrap CNP form, `cilium-dbg` vs `cilium`), and surfaces version + compat verdict via MCP
- [ ] **COMPAT-03**: README's proxy-visibility L7 section is corrected to state the ≤ 1.16 boundary explicitly (mechanism removed from the agent at 1.17) — fixes a live, shipped doc bug claiming support through 1.19

## Future Requirements

Deferred. Tracked but not in the v1.6 roadmap.

### Audit & Exec Hardening

- **AUD-FUT-01**: WebSocket exec with SPDY fallback (`NewFallbackExecutor`) matching current `kubectl exec` — fast-follow hardening once SPDY-only ships
- **AUD-FUT-02**: cpg-managed CNP apply/delete for the bootstrap policy behind the same launch flag (one mutation tier above endpoint config — draft §3.C.4)

Pre-v1.6 candidates (lint debt LINT-01..03, release hardening RELSEC-01..02, replay exit parity, `cpg apply`, consolidation, metrics, L7/DNS futures) remain tracked in PROJECT.md § Planned.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Daemon-wide `policy-audit-mode` (flag, tool, or runbook suggestion) | Suspends enforcement of ALL policies cluster-wide + agent restart; Cilium docs: not for production. Every generated artifact actively warns against it |
| Free-floating `enable_audit`/`disable_audit` MCP tools | Standing privilege without lifecycle binding — the JIT/break-glass anti-pattern AUD-03's session-bound design exists to avoid |
| Generic exec/command pass-through MCP tool | Azure `mcp-kubernetes` anti-pattern; would defeat SEC-01's structural readonly proof entirely |
| `apply_policy` MCP tool for generated allow rules | Applying policies stays a human act — carried forward unchanged from v1.5 |
| Global/user-level skills or agents | Hard operator constraint: everything repo-local, namespaced `cpg-*`, this milestone |
| Multi-version kind/Cilium CI matrix | Heavy; compat matrix is derived statically (COMPAT-01) + covered by runtime detection (COMPAT-02) |
| L7 policy refinement under audit mode | Audit operates at the L3/L4 datapath; L7 stays the existing two-step `--l7` workflow after onboarding |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| AUD-01 | Phase 20 | Complete |
| AUD-02 | Phase 22 | Complete |
| AUD-03 | Phase 23 | Complete |
| AUD-04 | Phase 23 | Complete |
| SKL-01 | Phase 24 | Complete |
| SKL-02 | Phase 24 | Complete |
| SKL-03 | Phase 24 | Complete |
| SKL-04 | Phase 24 | Complete |
| SKL-05 | Phase 24 | Complete |
| SKL-06 | Phase 24 | Complete |
| COMPAT-01 | Phase 21 | Complete |
| COMPAT-02 | Phase 21 | Complete |
| COMPAT-03 | Phase 21 | Complete |

**Coverage:**
- v1.6 requirements: 13 total
- Mapped to phases: 13
- Unmapped: 0 ✓

---
*Requirements defined: 2026-07-22*
*Last updated: 2026-07-22 after roadmap creation (Phases 20-24, full coverage)*
