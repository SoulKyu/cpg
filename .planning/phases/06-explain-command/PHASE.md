# Phase 6 — Explain Command

**Milestone:** v1.1 Offline Replay & Policy Analysis
**Status:** Planned
**Depends on:** Phase 4 (evidence files must exist)
**Requirements:** EXPL-01, EXPL-02, EXPL-03

## Goal

Users can inspect which flows produced each generated rule via
`cpg explain`, with filters and multi-format output.

## Scope

Covers tasks 13–20 of the master plan:

| Task | Summary |
|------|---------|
| 13 | Shared flag helper for `generate` / `replay` |
| 14 | `cpg replay` command wiring |
| 15 | Wire dry-run + evidence flags into `cpg generate` |
| 16 | Explain target resolver + filter matchers |
| 17 | Explain renderers (text, json, yaml) |
| 18 | `cpg explain` command wiring |
| 19 | README additions (replay, explain, dry-run, project structure) |
| 20 | Release trigger (release-please `Release-As: 1.6.0`) |

## Success Criteria

1. `cpg explain NAMESPACE/WORKLOAD` reads evidence and renders per-rule
   attribution in text (default), JSON (`--json`), or YAML (`--format yaml`).
2. `cpg explain path/to/policy.yaml` resolves namespace + workload from
   the YAML and rejects non-`cpg-` names.
3. Filters (`--ingress`, `--egress`, `--port`, `--peer`, `--peer-cidr`,
   `--since`) compose correctly.
4. Missing evidence produces an actionable error naming the exact path
   that was checked.
5. README documents the new `replay` / `explain` / `--dry-run` workflows.

## Plans

- [ ] `06-01-PLAN.md` — generated from master plan tasks 13–20.

## Master Plan Reference

`docs/superpowers/plans/2026-04-24-offline-replay-and-analysis.md`.
