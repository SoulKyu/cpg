# Milestone v1.7 Requirements: L7 Path Generalization & Namespace Awareness

**Status:** Complete — all 6 v1.7 requirements delivered and verified (Phase 25 passed 2026-10-04, Phase 26 passed 19/19 2026-10-05)
**Created:** 2026-10-03

## v1.7 Requirements

### L7GEN — L7 Path Generalization

- [x] **L7GEN-01**: User can pass `--l7-collapse-paths` on `generate` and `replay` and get HTTP rules grouped by inferred path template instead of one literal `^/api/v1/users/N$` rule per observed `(method, path)` pair
- [x] **L7GEN-02**: Path templates stay anchored (`^…$`) and only generalize segments with observed variance (numeric, hex, UUID → character classes); literal segments remain literal
- [x] **L7GEN-03**: User can enable path collapse from an MCP session (`collapse_l7_paths` session arg, mirroring the `include_audit` pattern)
- [x] **L7GEN-04**: User can set `--min-flows-per-l7-rule N`: a template is only emitted when ≥ N distinct observed paths confirm it; below the threshold the literal rules are kept

### NSAW — Namespace Awareness

- [x] **NSAW-01**: During `cpg generate`, cpg watches namespace-creation events and lists at end of run every namespace created after run start ("namespace X appeared after run start — re-run to cover it")
- [x] **NSAW-02**: On `replay`, cpg reports namespaces whose first observed flow arrives after capture start (flow-timestamp heuristic, no K8s watch)

## Future Requirements

- [ ] FQDN wildcard inference (`--l7-fqdn-wildcard-depth`) — DNS-FUT-01, v1.3 carryover
- [ ] kube-dns selector autodetection (EKS / GKE / AKS / vanilla) — DNS-FUT-02, v1.3 carryover

## Out of Scope

- **Long-running daemon mode** (#9 heavy): cpg stays a batch tool with a run-finished contract; NSAW reports the blind spot instead of closing it
- **Unanchored `matchPattern` globs**: generalization stays within anchored regex, per the v1.2 anti-hallucination stance
- **`Headers`/`Host`/`HostExact` generation**: remains a deliberate anti-feature (secret-leak risk, KNOWN_LIMITATIONS #3)
- **Auto-collapse without a gate**: collapse is opt-in and always bounded by `--min-flows-per-l7-rule`; silent generalization is a security regression

## Traceability

<!-- Filled by the roadmapper: REQ-ID → phase mapping -->

| REQ-ID | Phase |
|--------|-------|
| L7GEN-01 | 25 |
| L7GEN-02 | 25 |
| L7GEN-03 | 25 |
| L7GEN-04 | 25 |
| NSAW-01 | 26 |
| NSAW-02 | 26 |
