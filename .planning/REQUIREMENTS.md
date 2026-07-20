# Requirements: CPG — Cilium Policy Generator

**Defined:** 2026-07-20
**Core Value:** Automatically generate correct CiliumNetworkPolicies from observed Hubble denials so that SREs spend zero time manually writing network policies in default-deny environments.
**Milestone:** v1.4 Audit Fable5 — land every confirmed finding from the Fable 5 full-code review (2026-07-20, commit 427e047) as a CI-green, reviewable PR.

## v1.4 Requirements

Requirements for this milestone. Each maps to roadmap phases.

### Audit remediation

- [ ] **AUDIT-01**: Every confirmed high/medium review finding (1 high, 7 medium) is fixed with a regression test proving the behavior change
- [ ] **AUDIT-02**: Every confirmed low/info review finding (21) is fixed; doc/comment-only findings exempt from new tests
- [ ] **AUDIT-03**: CI pipeline triggers on `master` (push + pull_request) with GitHub Actions and installed tools pinned to immutable versions
- [ ] **AUDIT-04**: Full gates are green on the fix branch: `go build ./...`, `go vet ./...`, `go test -race ./...`, `golangci-lint run` with no new issues vs the pre-fix baseline (30)
- [ ] **AUDIT-05**: Branch `fix/review-findings` is pushed with atomic per-package conventional commits and a PR is open for user review

**Finding inventory (fixed set, from `code-review-report.html` @ 427e047):**
1 high (CI trigger `main` vs `master`) · 7 medium (MergePolicy nil-Spec panic, order-dependent dedup sort keys, redundant ToCIDR:53 egress rule, LostEvents never populated, stream failures swallowed, replay partial failure reported complete, evidence session-ID duplication) · 11 low · 10 info. Refuted findings (2) are excluded by design.

## Future Requirements

Deferred — tracked but not in the current roadmap.

### Lint debt (v1.5 candidate)

- **LINT-01**: errcheck clean (17 current hits: unchecked `Close`/`Remove`/`Fprintln` returns)
- **LINT-02**: staticcheck SA1019 clean — migrate `fake.NewSimpleClientset` → `NewClientset`; explicit strategy for deprecated Cilium `DropReason_*` enum values (fix or documented lint exceptions)
- **LINT-03**: errorlint enabled in `.golangci.yml` and clean

### Release hardening (v1.5 candidate)

- **RELSEC-01**: `release.yml` actions pinned to commit SHAs with minimal permissions
- **RELSEC-02**: govulncheck job in CI, pinned and green

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Lint debt cleanup (errcheck/staticcheck/errorlint) | User descoped to keep milestone tight — deferred to v1.5 (LINT-01..03) |
| Release workflow hardening beyond confirmed findings | Only findings from the review are in scope — deferred (RELSEC-01..02) |
| v1.3 deferred feature candidates (`cpg apply`, consolidation, metrics, L7-FUT/DNS-FUT) | Feature work does not belong in an audit-remediation milestone — remain Planned in PROJECT.md |
| Fixing the 2 refuted findings | Adversarial verification proved them false positives |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| AUDIT-01 | Phase 14 | Pending |
| AUDIT-02 | Phase 14 | Pending |
| AUDIT-03 | Phase 15 | Pending |
| AUDIT-04 | Phase 14 | Pending |
| AUDIT-05 | Phase 15 | Pending |

**Coverage:**
- v1.4 requirements: 5 total
- Mapped to phases: 5
- Unmapped: 0 ✓

---
*Requirements defined: 2026-07-20*
*Last updated: 2026-07-20 — roadmap created, traceability mapped (5/5)*
