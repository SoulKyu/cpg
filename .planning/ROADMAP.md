# Roadmap: CPG (Cilium Policy Generator)

## Overview

CPG delivers a Go CLI tool that turns Hubble dropped flows into ready-to-apply CiliumNetworkPolicies. v1.0 shipped the core live-streaming generator. v1.1 added an offline iteration workflow (`cpg replay`), per-rule flow evidence, `cpg explain`, and `--dry-run` with unified YAML diff. v1.2 extended generation to L7 (HTTP + DNS) with two-step workflow guidance. v1.3 closes the class of bug where infra-level Hubble drops (e.g. `CT_MAP_INSERTION_FAILED`) generated bogus CNPs — a static classifier taxonomy gates policy generation and surfaces cluster health separately. v1.4 is an audit-remediation milestone: every confirmed finding from the Fable 5 full-code review lands on `fix/review-findings` as a CI-green, reviewable PR — no new features.

## Milestones

- ✅ **v1.0 MVP (Core Policy Generator)** — Phases 1-3 (shipped 2026-03-08) — [archive](milestones/v1.0-ROADMAP.md)
- ✅ **v1.1 Offline Replay & Policy Analysis** — Phases 4-6 (shipped 2026-04-24) — [archive](milestones/v1.1-ROADMAP.md)
- ✅ **v1.2 L7 Policies (HTTP + DNS)** — Phases 7-9 (shipped 2026-04-25) — [archive](milestones/v1.2-ROADMAP.md)
- ✅ **v1.3 Cluster Health Surfacing** — Phases 10-13 (shipped 2026-04-26) — [archive](milestones/v1.3-ROADMAP.md)
- 📋 **v1.4 Audit Fable5** — Phases 14-15 (in progress)

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

<details>
<summary>✅ v1.3 Cluster Health Surfacing (Phases 10-13) — SHIPPED 2026-04-26</summary>

- [x] Phase 10: Classifier Core (2/2 plans) — completed 2026-04-26
- [x] Phase 11: Aggregator Suppression + Health Writer (2/2 plans) — completed 2026-04-26
- [x] Phase 12: Session Summary Block (1/1 plan) — completed 2026-04-26
- [x] Phase 13: Flags + Exit Code (3/3 plans) — completed 2026-04-26

Full details: [milestones/v1.3-ROADMAP.md](milestones/v1.3-ROADMAP.md)

</details>

### 📋 v1.4 Audit Fable5 (Phases 14-15)

- [ ] **Phase 14: Fix Verification + Quality Gates** - All 29 confirmed findings fixed with regression tests on `fix/review-findings`; build/vet/test-race/lint gates green
- [ ] **Phase 15: CI Trigger Fix + PR Delivery** - CI triggers on `master` with pinned actions/tools; branch pushed with atomic commits and PR open for review

## Phase Details

### Phase 14: Fix Verification + Quality Gates
**Goal**: Every confirmed high/medium/low/info finding from the Fable 5 review is fixed on `fix/review-findings` with regression tests where behavior changed, and the branch passes build/vet/test-race/lint cleanly
**Depends on**: Nothing (first phase of v1.4; verifies work already in progress via a 6-agent package-group fix workflow on `fix/review-findings`)
**Requirements**: AUDIT-01, AUDIT-02, AUDIT-04
**Success Criteria** (what must be TRUE):
  1. `go build ./...` and `go vet ./...` complete with zero errors on `fix/review-findings`
  2. `go test -race ./...` passes with 446+ tests across all packages (up from 418 at v1.3 close), including a regression test for each of the 8 confirmed high/medium findings that fails without the fix and passes with it
  3. `golangci-lint run` reports no NEW issues vs the pre-fix baseline of 30 (17 errcheck + 13 staticcheck SA1019 — deliberately descoped to v1.5, not regressed further)
  4. All 21 confirmed low/info findings show a corresponding code or doc/comment change on the branch (doc/comment-only findings exempt from new tests per AUDIT-02)
**Plans**: TBD

### Phase 15: CI Trigger Fix + PR Delivery
**Goal**: The CI pipeline actually runs on `master` with pinned tooling, and the fix branch is delivered as a reviewable PR with atomic per-package commits
**Depends on**: Phase 14
**Requirements**: AUDIT-03, AUDIT-05
**Success Criteria** (what must be TRUE):
  1. Workflow YAML triggers on `push`/`pull_request` to `master` (not `main`) and a GitHub Actions run is visible for a push — the pipeline executing at all is new behavior (it never ran before this fix)
  2. Every GitHub Action step and installed CLI tool (golangci-lint, etc.) is pinned to an immutable version (commit SHA or exact tag, no floating `@latest`/`@main`/`@master` refs)
  3. `fix/review-findings` history shows atomic, per-package conventional commits (`fix(pkg):`, `test(pkg):`) rather than one squashed commit
  4. `fix/review-findings` is pushed to origin with an open PR targeting `master`, and the PR's own CI run shows all 4 gates green
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
| 13. Flags + Exit Code | v1.3 | 3/3 | Complete    | 2026-04-26 |
| 14. Fix Verification + Quality Gates | v1.4 | 0/TBD | Not started | - |
| 15. CI Trigger Fix + PR Delivery | v1.4 | 0/TBD | Not started | - |

**Milestone status:** v1.0 ✅ shipped · v1.1 ✅ shipped · v1.2 ✅ shipped · v1.3 ✅ shipped · v1.4 📋 in progress
