---
phase: 16
slug: mcp-server-foundation-write-safety
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-20
---

# Phase 16 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) |
| **Config file** | none — standard `go.mod` toolchain (go1.25.12) |
| **Quick run command** | `go test ./cmd/cpg/... ./pkg/output/...` |
| **Full suite command** | `go test -race ./...` |
| **Estimated runtime** | ~60 seconds (484 existing tests, 10 packages) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./cmd/cpg/... ./pkg/output/...`
- **After every plan wave:** Run `go test -race ./...`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 90 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| _(populated by planner from RESEARCH.md §Validation Architecture)_ | | | SRV-02, SRV-03, SEC-02 | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] _(populated by planner — expected: none; `go test` infrastructure already exists and covers all phase requirements)_

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| _(expected none — all three requirements have automated verification per RESEARCH.md)_ | | | |

*If none: "All phase behaviors have automated verification."*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
