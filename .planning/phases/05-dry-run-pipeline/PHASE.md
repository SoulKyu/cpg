# Phase 5 — Dry-Run & Pipeline Integration

**Milestone:** v1.1 Offline Replay & Policy Analysis
**Status:** Planned
**Depends on:** Phase 4 (Offline Replay Core)
**Requirements:** DRYR-01, DRYR-02

## Goal

Users can preview generation changes with a unified YAML diff; the pipeline
writes evidence alongside policies via a fan-out from a single PolicyEvent
channel.

## Scope

Covers tasks 10–12 of the master plan:

| Task | Summary |
|------|---------|
| 10 | YAML unified diff (`pkg/diff`) |
| 11 | `policyWriter` dry-run branch |
| 12 | Evidence writer goroutine + channel fan-out |

## Success Criteria

1. `--dry-run` on `generate` and `replay` writes no files (policy or
   evidence) and logs `would write policy` per emitted event.
2. With `--dry-run` and an existing policy on disk, a unified YAML diff
   prints to stdout (colored on a TTY).
3. `--no-diff` disables the diff; the `--dry-run` log line still fires.
4. `policyWriter` and `evidenceWriter` consume a fanned-out
   `PolicyEvent` channel; neither blocks the other.

## Plans

- [ ] `05-01-PLAN.md` — generated from master plan tasks 10–12.

## Master Plan Reference

`docs/superpowers/plans/2026-04-24-offline-replay-and-analysis.md`.
