# Phase 12: Session Summary Block - Context

**Gathered:** 2026-04-26
**Status:** Ready for planning
**Mode:** Auto-generated (autonomous mode, decisions locked in REQUIREMENTS + research SUMMARY)

<domain>
## Phase Boundary

After every `cpg generate` and `cpg replay` run, if the session observed ≥1 infra-class drop, print a clearly-visible summary block to **stdout** (NOT logger) listing:
- Each observed infra/transient drop reason sorted by severity (critical first)
- Top-3 nodes by infra-drop volume (per reason)
- Top-3 workloads by infra-drop volume (per reason)
- The absolute path to `cluster-health.json`

When zero infra drops occurred → no block printed (zero noise on healthy clusters).

Out of scope: classifier (phase 10), aggregator/writer (phase 11), filter flag + exit code (phase 13).
</domain>

<decisions>
## Implementation Decisions

### Output destination
- `fmt.Fprintf(os.Stdout, ...)` — NOT logger.Warn (stderr split would hide block when stderr is redirected per Pitfall P4)
- Block printed at session end, AFTER pipeline.RunPipelineWithSource returns and AFTER any policy-write summary

### Where it lives
- New function `printClusterHealthSummary(out io.Writer, stats SessionStats, healthPath string)` in `pkg/hubble/pipeline.go` (or `pkg/hubble/summary.go` if cleaner separation)
- Called from `cmd/cpg/generate.go` and `cmd/cpg/replay.go` after pipeline run, gated on `stats.InfraDropTotal > 0`
- Reads accumulator data from healthWriter — easiest path: healthWriter.Snapshot() returns the in-memory clusterHealthReport so summary can format top-3 lists without re-reading the JSON file

### Format (cromagnon-style, copy-pasteable)
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠ Cluster-critical drops detected (NOT a policy issue)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  CT_MAP_INSERTION_FAILED      [infra]    47 flows
    Top nodes:     node-a-1 (32), node-b-2 (12), node-c-3 (3)
    Top workloads: team-trading/mmtro-adserver (28), team-data/x (15), team-foo/y (4)
    Hint: https://docs.cilium.io/en/stable/operations/troubleshooting/#ct-map-full

  POLICY_DENIED_REVERSE         [transient] 5 flows
    Top nodes:     node-a-1 (5)
    Top workloads: team-trading/butler (5)

cluster-health.json: /home/gule/.cache/cpg/evidence/<hash>/cluster-health.json
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### Severity ordering
- Use the DropClass enum value as proxy: Infra > Transient (critical → less critical)
- Within same class, sort by descending count

### Top-3 truncation
- If more than 3 nodes/workloads contributed, show "(+N more)" suffix
- If only 1 contributor, show just "node-x (N)"

### Dry-run behavior
- Block IS printed even with `--dry-run` (it's diagnostic, not an artifact)
- Path line shows where file WOULD be written + `(dry-run, not written)` suffix

### Testing strategy
- TDD-first
- Unit test: synthetic SessionStats + clusterHealthReport → assert printed text matches expected format
- Edge case: zero infra drops → assert empty output
- Edge case: single contributor → assert no "(+N more)"
- Edge case: dry-run → assert "(dry-run)" suffix present

### Anti-features
- NO color codes (would break grep/log capture; let terminal interpret if connected)
- NO JSON output of summary (cluster-health.json IS the structured artifact; summary is human-readable)
- NO truncation of reason names
- NO --quiet flag this phase (user already has --json log mode at logger level)
</decisions>

<code_context>
## Existing Code Insights

### Reusable assets
- `pkg/hubble/pipeline.go` — `RunPipelineWithSource` returns the SessionStats now; healthWriter is private to package
- `cmd/cpg/generate.go` — already prints policy summary at end of run (existing pattern to mirror)
- `pkg/dropclass.RemediationHint(reason)` — for Hint: line

### Established patterns
- Logger via zap (do NOT use for this — stdout direct)
- Cromagnon style: telegraphic, ASCII frames, no emojis
- Existing session summary already uses Fprintln for clean output
</code_context>

<specifics>
## Specific Ideas

- Use `fmt.Fprintln(out, "━━━...")` for frames (76 wide max)
- Reason name padded to 30 chars left-aligned, then `[class]`, then count
- Use `strings.Builder` for performance if formatting is non-trivial
- Pipe-friendly: no ANSI in this block (let users `cpg replay foo.jsonl | tee log.txt`)
</specifics>

<deferred>
## Deferred Ideas

- ANSI color when stdout is a TTY (v1.4 polish)
- `--summary-format=json|text` (over-engineering — JSON IS the file)
- Per-namespace breakdown (not requested)
</deferred>
