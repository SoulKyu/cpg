# Requirements — Milestone v1.3 Cluster Health Surfacing

**Goal:** Distinguish policy drops from infrastructure-level Hubble drops so cpg only generates CNPs for true policy denials and surfaces cluster-critical issues separately for SRE attention.

**Trigger:** 2026-04-26 prod observation — `mmtro-adserver` ingress drop with reason `CT_MAP_INSERTION_FAILED` (Cilium conntrack map full, infra issue) generated a useless `cpg-mmtro-adserver` CNP. Class of bug: cpg trusts every Hubble DROP as policy-fixable.

---

## v1.3 Requirements

### CLASSIFY — Drop-reason taxonomy

- [x] **CLASSIFY-01**: User sees Hubble drops classified into one of {policy, infra, transient, unknown} based on a static taxonomy covering all known Cilium ≥1.14 `DropReason` enum values
- [x] **CLASSIFY-02**: Unknown / unrecognized `DropReason` values default to the `unknown` bucket (never `policy`) and trigger a single deduplicated WARN log per unique value
- [x] **CLASSIFY-03**: User can read a stable `classifierVersion` constant (semver string) embedded in `cluster-health.json` for cross-release traceability

### HEALTH — Suppression + reporting

- [x] **HEALTH-01**: cpg does NOT generate a CiliumNetworkPolicy for flows whose drop reason is classified as `infra` or `transient`
- [x] **HEALTH-02**: User sees infra + transient flows aggregated into `cluster-health.json` (atomic write, in evidence dir `$XDG_CACHE_HOME/cpg/evidence/<hash>/`) with counters keyed by `reason × node × workload` plus a remediation-hint URL per reason
- [x] **HEALTH-03**: User sees a session-summary block at end of `generate` / `replay` listing infra drops sorted by severity, top-3 nodes / workloads, and the absolute path to `cluster-health.json`
- [x] **HEALTH-04**: `--dry-run` suppresses the `cluster-health.json` write (parity with policies + evidence)
- [x] **HEALTH-05**: Infra / transient drops still increment the `flowsSeen` counter (observed traffic count remains accurate; only CNP generation is gated)

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

| REQ | Phase | Notes |
|-----|-------|-------|
| CLASSIFY-01 | Phase 10 | Core taxonomy in `pkg/dropclass/` |
| CLASSIFY-02 | Phase 10 | Unknown-reason fallback + dedup WARN |
| CLASSIFY-03 | Phase 10 | `classifierVersion` semver in output |
| HEALTH-01 | Phase 11 | Aggregator suppression gate |
| HEALTH-02 | Phase 11 | `cluster-health.json` atomic write |
| HEALTH-04 | Phase 11 | `--dry-run` parity for health file |
| HEALTH-05 | Phase 11 | `flowsSeen` counter still incremented |
| HEALTH-03 | Phase 12 | Session summary block + severity sort |
| FILTER-01 | Phase 13 | `--ignore-drop-reason` flag |
| FILTER-02 | Phase 13 | Unknown-reason validation at parse time |
| FILTER-03 | Phase 13 | WARN on redundant infra/transient filter |
| EXIT-01 | Phase 13 | `--fail-on-infra-drops` exit code |
| EXIT-02 | Phase 13 | README CI/cron documentation |

---

*Created: 2026-04-26 — v1.3 Cluster Health Surfacing scoped via /gsd:new-milestone*
*Traceability filled: 2026-04-26 — roadmap phases 10-13 assigned*
