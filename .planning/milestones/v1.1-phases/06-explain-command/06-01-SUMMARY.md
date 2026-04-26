# Phase 06 Plan 01 — Summary

**Status:** Complete
**Completed:** 2026-04-24

## What Shipped

Tasks 13–19 from the master plan landed on `master`:

- **Task 13** — `commonflags.go` factors shared flags; generate embeds `commonFlags`.
- **Task 14** — `cpg replay <file.jsonl|->`. Uses `flowsource.FileSource`, records session source type `replay` with absolute path.
- **Task 15** — `cpg generate` wires dry-run + evidence flags into `PipelineConfig` with session source type `live`.
- **Task 16** — `explain_target.go` (NAMESPACE/WORKLOAD or YAML path; rejects non-`cpg-` names) + `explain_filter.go` (direction, port, peer label, peer CIDR with proper containment, since).
- **Task 17** — `explain_render.go` with text (ANSI-on-TTY), JSON, and YAML renderers.
- **Task 18** — `cpg explain` subcommand wiring with all filters + actionable error naming the evidence path when missing.
- **Task 19** — README updated with new sections: Quick start (offline replay), Offline replay, Dry-run, Explain policies; Project structure now lists `pkg/flowsource`, `pkg/evidence`, `pkg/diff`.

## Minor Fix Landed Alongside

- `Aggregator.SetMaxSamples(n int)` threads `--evidence-samples` into the builder's `AttributionOptions`. Without this, replay wrote policies but no evidence — caught while writing `TestReplayCommandProducesPoliciesAndEvidence`.

## Verification

- `go build ./...` — success
- `go test ./...` — **180 tests pass** across 9 packages
- Integration tests: replay writes policies + evidence, dry-run writes nothing, explain emits text/JSON for seeded evidence

## Next

Phase 6 closes milestone v1.1. Release-please will detect the `feat:` commits (new `replay` and `explain` subcommands, dry-run, evidence capture) and open a release PR for v1.6.0 automatically.

Task 20 (explicit `Release-As: 1.6.0` empty commit) is **not needed** — we have enough `feat:` conventional commits to trigger a minor bump automatically once the next push lands.
