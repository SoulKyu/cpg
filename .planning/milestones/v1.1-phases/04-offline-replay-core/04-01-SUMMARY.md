# Phase 04 Plan 01 — Summary

**Status:** Complete
**Completed:** 2026-04-24
**Commits:** 9 (0aba62d, db6f62f, 2c6a58e, 259de0f, a885ccc, 3e00803, + Task 7 + Task 8+9 combined)

## What Shipped

All 9 tasks from the offline-replay master plan landed on `master`:

1. **FlowSource → pkg/flowsource** — interface promoted out of pkg/hubble; pkg/hubble/pipeline.go now consumes `flowsource.FlowSource`.
2. **Evidence schema** — PolicyEvidence, RuleEvidence, FlowSample, PeerRef, SessionInfo, SourceInfo, PolicyRef, FlowEndpoint. Schema version pinned at 1.
3. **Evidence paths** — `HashOutputDir` (SHA-256 first 12 hex, normalized), `DefaultEvidenceDir` (XDG_CACHE_HOME → $HOME/.cache), `ResolvePolicyPath`.
4. **Evidence merge** — FIFO caps on samples (per rule) and sessions (per policy); preserves rules not re-emitted; rules sorted by (direction, key); samples sorted by time.
5. **Evidence writer + reader** — atomic writes via temp-file + rename; reader refuses unknown `schema_version`.
6. **RuleKey + RuleAttribution** — deterministic string encoding regardless of label-map insertion order; supports endpoint/cidr/entity peers.
7. **BuildPolicy returns attribution** — signature now `(cnp, []RuleAttribution)` with trailing `AttributionOptions`; per-bucket `recordAttribution` with FIFO newest-N samples; `PolicyEvent.Attribution` threaded end-to-end; every call site migrated.
8. **Jsonpb FileSource** — line-delimited parser via `protojson.Unmarshal(observer.GetFlowsResponse)`, DROPPED filter, malformed-line counter, stdin support, 10 MiB scanner buffer.
9. **Gzip** — transparent `.gz` decompression via `compress/gzip`.

## Key Files

```
pkg/flowsource/
  source.go               FlowSource interface
  source_test.go
  file.go                 FileSource (jsonpb + gzip + stdin)
  file_test.go            7 tests
pkg/evidence/
  schema.go               JSON schema v1
  schema_test.go
  paths.go                XDG + hash
  paths_test.go
  merge.go                FIFO merge
  merge_test.go           5 tests
  writer.go               Atomic writer
  reader.go               Schema-version-aware reader
  writer_test.go          3 tests
pkg/policy/
  attribution.go          RuleKey, RuleAttribution, Peer types
  attribution_test.go
  builder.go              BuildPolicy returns attribution
  builder_attribution_test.go
pkg/hubble/
  pipeline.go             Consumes flowsource.FlowSource
  aggregator.go           Forwards AttributionOptions
testdata/flows/
  small.jsonl             3 DROPPED flows
  with_non_dropped.jsonl  3 DROPPED + 2 FORWARDED
  malformed.jsonl         2 valid + 1 broken line
  empty.jsonl             0-byte file
  small.jsonl.gz          gzip of small.jsonl
```

## Verification

- `go build ./...` — success
- `go test ./...` — all packages pass
- New test coverage: 7 flowsource, 2 schema, 4 paths, 5 merge, 3 writer, 4 attribution = 25 new tests, all green.

## Notable Decisions

- `protojson` chosen over manual JSON for jsonpb parsing (already transitive via cilium observer proto).
- Gzip support folded into `openReader()`; both file handle and `gzip.Reader` released on cleanup.
- Evidence writes are atomic via temp + rename; schema version mismatch fails fast rather than corrupting user data silently.
- `RuleKey.String()` sorts labels alphabetically — stable across sessions regardless of how the flow was observed.

## Deviations from Master Plan

None — all 9 tasks implemented as specified. Only procedural detour: the spawned executor subagent hit a mid-task rate limit during Task 7, so Tasks 7–9 were finished inline from this context. Code produced is identical to the master-plan content.

## Next

Phase 5 (Dry-Run & Pipeline Integration) — 3 tasks consuming the attribution + evidence surfaces added here.
