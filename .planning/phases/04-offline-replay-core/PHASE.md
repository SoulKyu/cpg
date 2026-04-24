# Phase 4 — Offline Replay Core

**Milestone:** v1.1 Offline Replay & Policy Analysis
**Status:** Planned
**Depends on:** Phase 3 (Production Hardening)
**Requirements:** OFFL-01, OFFL-02, OFFL-03, EVID-01, EVID-02, EVID-03, EVID-04

## Goal

Users can ingest a Hubble jsonpb capture through the existing aggregation
pipeline and receive policies + per-rule evidence on disk.

## Scope

Covers tasks 1–9 of the master plan
(`docs/superpowers/plans/2026-04-24-offline-replay-and-analysis.md`):

| Task | Summary |
|------|---------|
| 1 | Promote `FlowSource` interface to `pkg/flowsource` |
| 2 | Evidence JSON schema |
| 3 | XDG-aware evidence paths + output-dir hash |
| 4 | Evidence merge semantics (FIFO caps) |
| 5 | Evidence atomic writer + reader |
| 6 | `RuleKey` + `RuleAttribution` types |
| 7 | Track attribution during `BuildPolicy` |
| 8 | Jsonpb file source (plain text) |
| 9 | Gzip support on `.gz` replay files |

## Success Criteria

1. `cpg replay <file.jsonl>` produces the same policy YAMLs as
   `cpg generate` would for the same flows.
2. Per-rule evidence files exist under
   `$XDG_CACHE_HOME/cpg/evidence/<hash>/<ns>/<workload>.json`
   with `schema_version=1`.
3. `.gz` extension triggers transparent gzip decompression; `-` reads
   from stdin.
4. Malformed lines and non-DROPPED verdicts are counted and surfaced in
   the session summary.
5. Evidence merges across sessions preserve `first_seen` and cap
   samples/sessions FIFO.

## Plans

- [ ] `04-01-PLAN.md` — generated from master plan tasks 1–9. Use
      `/gsd:plan-phase 04-offline-replay-core` to produce the GSD-formatted
      sub-plan, or execute master-plan tasks inline with
      `superpowers:executing-plans`.

## Master Plan Reference

This phase is sliced from the master plan at
`docs/superpowers/plans/2026-04-24-offline-replay-and-analysis.md`.
The master plan is the source of truth for code snippets, commands, and
commit messages. The phase-level PLAN.md (when generated) will mirror
the selected tasks with GSD's verification-loop conventions.
