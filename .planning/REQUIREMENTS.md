# Requirements — Milestone v1.3 Cluster Health Surfacing

**Goal:** Distinguish policy drops from infrastructure-level Hubble drops so cpg only generates CNPs for true policy denials and surfaces cluster-critical issues separately for SRE attention.

**Trigger:** 2026-04-26 prod observation — `mmtro-adserver` ingress drop with reason `CT_MAP_INSERTION_FAILED` (Cilium conntrack map full, infra issue) generated a useless `cpg-mmtro-adserver` CNP. Class of bug: cpg trusts every Hubble DROP as policy-fixable.

---

## v1.3 Requirements

### CLASSIFY — Drop-reason taxonomy

- [ ] **CLASSIFY-01**: User sees Hubble drops classified into one of {policy, infra, transient, unknown} based on a static taxonomy covering all known Cilium ≥1.14 `DropReason` enum values
- [ ] **CLASSIFY-02**: Unknown / unrecognized `DropReason` values default to the `unknown` bucket (never `policy`) and trigger a single deduplicated WARN log per unique value
- [ ] **CLASSIFY-03**: User can read a stable `classifierVersion` constant (semver string) embedded in `cluster-health.json` for cross-release traceability

### HEALTH — Suppression + reporting

- [ ] **HEALTH-01**: cpg does NOT generate a CiliumNetworkPolicy for flows whose drop reason is classified as `infra` or `transient`
- [ ] **HEALTH-02**: User sees infra + transient flows aggregated into `cluster-health.json` (atomic write, in evidence dir `$XDG_CACHE_HOME/cpg/evidence/<hash>/`) with counters keyed by `reason × node × workload` plus a remediation-hint URL per reason
- [ ] **HEALTH-03**: User sees a session-summary block at end of `generate` / `replay` listing infra drops sorted by severity, top-3 nodes / workloads, and the absolute path to `cluster-health.json`
- [ ] **HEALTH-04**: `--dry-run` suppresses the `cluster-health.json` write (parity with policies + evidence)
- [ ] **HEALTH-05**: Infra / transient drops still increment the `flowsSeen` counter (observed traffic count remains accurate; only CNP generation is gated)

### FILTER — User-controlled filtering

- [ ] **FILTER-01**: User can pass `--ignore-drop-reason <reason>` (repeatable, comma-separated, case-insensitive) on both `generate` and `replay` to exclude flows by reason name before classification
- [ ] **FILTER-02**: Validation rejects unknown reason names at flag-parse time, error message lists valid reasons
- [ ] **FILTER-03**: WARN emitted when user passes a reason already classified as `infra` / `transient` (redundant with default suppression — surfaced for clarity)

### EXIT — CI integration

- [ ] **EXIT-01**: User can pass `--fail-on-infra-drops` to make cpg exit with code 1 when ≥ 1 infra drop is observed; default behavior unchanged (exit 0 always)
- [ ] **EXIT-02**: README documents exit-code semantics + a recommended CI cron pattern using the new flag

---

## Future Requirements (deferred)

- OpenMetrics / Prometheus export of drop counters (deferred — gather field feedback first; v1.4+ candidate)
- Semantic policy intersection ("would existing CNP already allow this?") — deferred indefinitely (NP-hard, low ROI vs `cpg apply --diff` workflow)
- CI script to diff `pkg/dropclass/` taxonomy against latest Cilium proto on bump (v1.4 candidate)
- AUTH_REQUIRED dual-path disambiguation via SPIRE status inspection (v1.4 candidate)

---

## Out of Scope (this milestone)

- OpenMetrics / Prometheus export — explicit user decision (defer until field feedback validates need)
- Semantic policy intersection / overlap detection
- `cpg apply` command (v1.4+ candidate)
- Policy consolidation across overlapping rules (v1.4+ candidate)
- L7-FUT-01 (REFUSED verdicts) and L7-FUT-02 (min-flows gate) (carried over to v1.4+)
- Auto-bump of taxonomy from upstream Cilium proto changes (manual bump only this milestone)

---

## Traceability

<!-- Filled by gsd-roadmapper after roadmap creation. Each REQ → Phase mapping. -->

| REQ | Phase | Notes |
|-----|-------|-------|
| _(filled by roadmapper)_ | | |

---

*Created: 2026-04-26 — v1.3 Cluster Health Surfacing scoped via /gsd:new-milestone*
