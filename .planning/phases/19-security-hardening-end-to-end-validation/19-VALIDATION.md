---
phase: 19
slug: security-hardening-end-to-end-validation
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-21
---

# Phase 19 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) — 607 existing tests, all `-race` |
| **Config file** | none — go.mod toolchain go1.25.12 |
| **Quick run command** | `rtk proxy go test ./cmd/cpg/ -count=1 -race` |
| **Full suite command** | `rtk proxy go test ./... -count=1 -race` |
| **Estimated runtime** | ~90 seconds full suite (e2e adds one `go build -race` ≈ +10s first run) |

---

## Sampling Rate

- **After every task commit:** Run `rtk proxy go test ./cmd/cpg/ -count=1 -race`
- **After every plan wave:** Run `rtk proxy go test ./... -count=1 -race`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner) | | | SRV-01, SRV-04, SEC-01, SEC-03 | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — `go test -race` is already the suite-wide standard; the audit and e2e tests this phase ADDS are themselves the validation artifacts (SEC-01/SRV-04 deliverables are tests).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| README MCP section reads correctly for a first-time SRE | SEC-03 | Prose quality is human judgment | Read `README.md` §MCP Server; check env block, secrets posture, exec-plugin caveat present and accurate |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
