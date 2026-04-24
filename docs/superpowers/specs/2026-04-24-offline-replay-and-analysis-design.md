# Offline replay and analysis — design

Status: draft
Date: 2026-04-24
Target release: 1.6.0 (tentative)

## 1. Goals

Extend `cpg` with an offline-first workflow and per-rule traceability so
users can iterate on policy generation without re-producing traffic.

Three user stories frame the design:

1. **UC1 — Offline iteration**
   *"I captured one hour of dropped flows in staging. I want to re-run
   `cpg` against that capture as many times as I want — to iterate on
   label-selection logic or validate a refactor — without reproducing
   traffic."*

2. **UC3 — Explain a generated rule**
   *"I see a rule in the generated YAML I don't recognize. I want to ask
   'where did this rule come from?' and get the contributing flows with
   timestamps, source/destination, and ports."*

3. **UC4 — Dry-run preview (light)**
   *"I want to run generation against a live stream or a capture and
   preview what would change on disk, without touching any file."*

Use-case 2 (gap analysis) explored during brainstorming was dropped:
cpg's baseline behavior already answers it, and an additional framing
would have been redundant sugar.

## 2. Non-goals

- L7 support (HTTP/DNS rules). Tracked as a separate effort.
- Seeding policies from cluster state (deployments/services) without
  flow evidence. Explicitly out of scope.
- Auto-apply of generated policies to a cluster.
- Non-Cilium NetworkPolicy emission.

## 3. CLI surface

### 3.1 Commands

```
cpg generate [flags]                       # existing — unchanged behavior
cpg replay <file.jsonl|-> [flags]          # NEW
cpg explain <target> [flags]               # NEW
```

`generate` remains the live-streaming command. `replay` is a dedicated
offline command — explicit naming was preferred over piling `--from-file`
onto `generate`. Flags shared by both are built from a common helper to
avoid duplication.

### 3.2 Shared flags (generate and replay)

```
-n, --namespace strings      Namespace filter (repeatable)
-A, --all-namespaces         Observe all namespaces
-o, --output-dir string      Output directory (default "./policies")
    --flush-interval dur     Aggregation flush interval (default 5s)
    --cluster-dedup          Skip policies matching live cluster state

    --dry-run                Do not touch disk — log what would be written
    --no-diff                With --dry-run, skip unified diff output
    --no-evidence            Disable evidence capture for this run

    --evidence-dir string    Override evidence storage path
                             (default: XDG_CACHE_HOME/cpg/evidence)
    --evidence-samples int   Samples kept per rule (default 10)
    --evidence-sessions int  Sessions kept per policy (default 10)
```

### 3.3 Generate-specific flags (unchanged)

```
-s, --server string        Hubble Relay address (auto port-forward if omitted)
    --tls                  Enable TLS for gRPC connection
    --timeout duration     Connection timeout (default 10s)
```

### 3.4 Replay-specific flags

```
(positional) <file.jsonl|->  Path to jsonpb dump, or `-` for stdin
```

Extension-based autodetection of compression: `.gz` → gzip, otherwise
plain text.

### 3.5 Explain-specific flags

```
(positional) <target>        Either NAMESPACE/WORKLOAD or a YAML path

    --ingress                Filter: ingress rules only
    --egress                 Filter: egress rules only
    --port string            Filter: rules using this port
    --peer string            Filter: endpoint peer with KEY=VAL
    --peer-cidr string       Filter: CIDR peer containing this CIDR
    --since duration         Filter: flows seen within the last N

    --samples-limit int      Cap samples displayed per rule (default 10)
    --json                   Emit JSON instead of formatted text
    --format string          Output format: text (default) | json | yaml
    --evidence-dir string    Override evidence storage lookup path
```

Filters are AND-combined. Empty match → explanatory message listing
the available rules.

## 4. Architecture

### 4.1 Package layout

New packages:

```
pkg/flowsource/     Promoted FlowSource interface (was internal to pkg/hubble)
  source.go           interface + contract
  live.go             adapts existing *hubble.Client
  file.go             NEW — jsonpb file source
pkg/evidence/       Per-rule attribution persistence
  schema.go           JSON structs (SessionEvidence, RuleEvidence, FlowSample)
  writer.go           merges with existing evidence on disk, writes atomically
  reader.go           loads for explain
  merge.go            FIFO caps for samples and sessions
  paths.go            XDG resolution + hash-of-output-dir
pkg/diff/           YAML unified-diff for --dry-run
  yaml.go             render + unified diff
```

Modified packages:

```
pkg/hubble/         FlowSource moved out. PipelineConfig gains DryRun,
                    DryRunDiff, EvidenceEnabled, EvidenceWriter.
                    policyWriter branches on DryRun: log + optional diff
                    instead of filesystem writes.
                    New evidenceWriter goroutine consumes PolicyEvent.
pkg/policy/         BuildPolicy returns (cnp, attribution). Existing
                    callers are migrated. RuleAttribution keys rules by
                    direction + peer + port + protocol.
cmd/cpg/            New files: replay.go, explain.go, commonflags.go.
                    generate.go refactored to consume commonflags.
```

### 4.2 Data flow

```
FlowSource (live | file)
  |  <-chan *flowpb.Flow
  v
Aggregator  (accumulates by AggKey, ticks on flush interval)
  |  <-chan PolicyEvent{ CNP, Attribution }
  v
  +------------------+-----------------------+
  v                  v
policyWriter    evidenceWriter
  (writes CNP     (merges evidence
   YAML, or        with existing on-disk
   dry-runs)       state, writes JSON)
```

The evidenceWriter reads before writing: it loads the existing
`<evidence-dir>/<ns>/<workload>.json`, merges the new session into it,
writes the result atomically. This mirrors the existing merge-on-write
behavior of the policy YAML writer.

### 4.3 Flow-to-rule attribution

During `BuildPolicy`, each peer bucket (`endpointBucket`, `cidrBucket`,
entity bucket) accumulates:

- `flow_count int64`
- `first_seen`, `last_seen time.Time`
- `samples []FlowSample` (capped FIFO at `EvidenceSamples` size)

Only the compact form (samples + counts) crosses the `PolicyEvent`
channel — raw `*flowpb.Flow` references are not held past the builder.
This keeps pipeline memory bounded even for long runs.

Rule key format: `<direction>:<peer-encoding>:<protocol>:<port>`
- `direction` ∈ `ingress | egress`
- `peer-encoding`:
  - `ep:<sorted-labels>` for endpoint peers (e.g. `ep:app=api`)
  - `cidr:<cidr>` for CIDR peers
  - `entity:<entity-name>` for reserved entities
- `protocol` is the Cilium display name (`TCP`, `UDP`, `ICMPv4`,
  `ICMPv6`).

This key is stable across sessions and deterministic (no ordering
dependency), which is required for cross-session merge.

## 5. Evidence format

Path template:
`<evidence-dir>/<output-dir-hash>/<namespace>/<workload>.json`

Default evidence-dir: `$XDG_CACHE_HOME/cpg/evidence` (Linux
`~/.cache/cpg/evidence`, macOS `~/Library/Caches/cpg/evidence`).

Output-dir hash: first 12 hex chars of SHA-256 of the absolute,
cleaned output-dir path. This lets multiple workspaces coexist without
collision and allows explain to resolve evidence automatically from
`--output-dir`.

### 5.1 Schema (v1)

```json
{
  "schema_version": 1,
  "policy": {
    "name": "cpg-api-server",
    "namespace": "production",
    "workload": "api-server"
  },
  "sessions": [
    {
      "id": "2026-04-24T14:02:11Z-a3f2",
      "started_at": "2026-04-24T14:02:11Z",
      "ended_at": "2026-04-24T14:15:48Z",
      "cpg_version": "1.6.0",
      "source": {
        "type": "replay",
        "file": "/abs/path/flows.jsonl",
        "server": ""
      },
      "flows_ingested": 12847,
      "flows_unhandled": 42
    }
  ],
  "rules": [
    {
      "key": "ingress:ep:app=weird-thing:TCP:8080",
      "direction": "ingress",
      "peer": {
        "type": "endpoint",
        "labels": {"app": "weird-thing"},
        "cidr": "",
        "entity": ""
      },
      "port": "8080",
      "protocol": "TCP",
      "flow_count": 23,
      "first_seen": "2026-04-24T14:02:11Z",
      "last_seen": "2026-04-24T14:15:48Z",
      "contributing_sessions": ["2026-04-24T14:02:11Z-a3f2"],
      "samples": [
        {
          "time": "2026-04-24T14:02:11Z",
          "src": {"namespace": "default", "workload": "weird-thing", "pod": "weird-thing-5d4f"},
          "dst": {"namespace": "production", "workload": "api-server", "pod": "api-server-abc"},
          "port": 8080,
          "protocol": "TCP",
          "verdict": "DROPPED",
          "drop_reason": "Policy denied by denylist"
        }
      ]
    }
  ]
}
```

### 5.2 Merge semantics

On each new session:

1. Load existing evidence file (absent → empty skeleton).
2. For every rule produced in the new session:
   - Look up existing rule by `key`.
   - Existing rule found:
     - `flow_count += new_flows`
     - `last_seen = max(last_seen, new_last_seen)`
     - `samples = (old_samples ++ new_samples)` trimmed to
       `--evidence-samples` newest entries
     - `contributing_sessions += new_session_id`
   - No existing rule → append as-is.
3. `sessions = (sessions ++ new_session)` trimmed to
   `--evidence-sessions` newest entries.
4. Sort `rules` by `(direction, key)` and `samples` by `time` ascending.
5. Serialize and write atomically (write to temp + rename).

Rules no longer produced by the current session are preserved (they
still exist in the policy YAML via the same merge semantics).

## 6. Input parsing (replay)

Expected input: output of `hubble observe --output jsonpb --follow`.
Each line is a JSON object of type `observer.GetFlowsResponse`
containing a `.flow` field.

Parser pipeline:

1. Open the file (`-` selects stdin).
2. Autodetect compression via extension:
   - `.gz` → `gzip.Reader`
   - otherwise → raw bytes
3. `bufio.Scanner` with buffer grown to 10 MiB (Cilium flows with
   many labels can exceed the default 64 KiB).
4. Per line:
   - Skip empty lines.
   - `protojson.Unmarshal` into `observer.GetFlowsResponse`.
   - On error: log a `WARN` with line number, increment
     `malformed_skipped`, continue.
   - Extract `.flow`. Nil → increment `malformed_skipped`, continue.
   - If `verdict != DROPPED` → increment `non_dropped_skipped`,
     continue (robust to captures taken without `--verdict DROPPED`).
   - Push on the outbound channel.
5. At EOF: close the channel, let the aggregator drain, exit
   gracefully.

Startup log:

```
INFO replay starting  {"file":"flows.jsonl","size_bytes":105382924,"compression":"none"}
```

Completion log (in addition to the standard session summary):

```
INFO replay complete  {"lines_read":12889,"flows_dropped":12847,"non_dropped_skipped":40,"malformed_skipped":2}
```

## 7. Dry-run

`--dry-run` is an orthogonal modifier usable on both `generate` and
`replay`. Behavior:

- All upstream stages (source, aggregator, builder, unhandled tracker)
  run normally. No logs or warnings are suppressed.
- `policyWriter.handle()` branches on `DryRun`:
  - Skips `writer.Write(pe)`.
  - Skips evidence update.
  - Logs `INFO would write policy` with `namespace`, `workload`, and
    a `rule_deltas` map describing added/removed/unchanged rules
    relative to disk.
  - If `DryRunDiff` is true and the target YAML already exists:
    renders the in-memory CNP as YAML, reads the existing file,
    computes a unified diff via `pmezard/go-difflib`, prints it with
    ANSI colors if stdout is a terminal.
- Session stats expose separate counters `PoliciesWouldWrite` and
  `PoliciesWouldSkip`, logged as part of the final summary.

`--no-diff` disables the diff output only, the "would write" log line
still fires.

## 8. Explain

Resolution:

1. Target is `NAMESPACE/WORKLOAD` or a path ending in `.yaml`.
2. Path form: read the YAML, derive namespace from `metadata.namespace`,
   derive workload by stripping the `cpg-` prefix from `metadata.name`.
   If the name does not start with `cpg-`, fail fast with a clear
   error — explain is scoped to cpg-generated policies.
3. Compute `<output-dir-hash>` from `--output-dir` (default
   `./policies/` absolutized).
4. Look up
   `<evidence-dir>/<output-dir-hash>/<namespace>/<workload>.json`.
5. Absent → clear error with the path that was checked and a hint to
   run `cpg generate` / `cpg replay` first with the same output-dir.

Filtering (AND-combined):

- `--ingress` / `--egress` on `rule.direction`
- `--port` exact match on `rule.port`
- `--peer KEY=VAL` requires `rule.peer.type == "endpoint"` and
  `rule.peer.labels[KEY] == VAL`
- `--peer-cidr CIDR` requires `rule.peer.type == "cidr"` and the
  rule's CIDR to be fully contained within the filter CIDR (parse
  both with `net.ParseCIDR`, require `filter.Contains(ruleIP)` and
  `ruleMaskBits >= filterMaskBits`)
- `--since DURATION` keeps rules whose `last_seen >= now-duration`

Rendering:

- Text (default): header block with policy metadata + latest session
  context, followed by one block per matched rule showing direction,
  peer, port/protocol, counts, timestamps, and up to
  `--samples-limit` sample flows. Color: green headers, dimmed
  timestamps, only if stdout is a TTY.
- `--json` or `--format json`: emit `{"policy": {...}, "sessions":
  [...], "rules": [matched...]}` with the full evidence schema for
  matched rules.
- `--format yaml`: same payload as JSON, rendered as YAML via
  `sigs.k8s.io/yaml`.

Empty match: list available rules compactly (direction + peer +
port/protocol) so the user can refine.

## 9. Behavior contracts

- **Existing `cpg generate` invocations are unchanged.** Every new flag
  has a default equivalent to previous behavior. No breaking changes
  to output paths, YAML schema, or log format.
- **Evidence capture is on by default**, with opt-out (`--no-evidence`).
  Disk overhead is bounded by `--evidence-samples` and
  `--evidence-sessions`; typical sessions produce sub-megabyte
  evidence files.
- **Evidence lives outside the output-dir** by default. Users GitOps'ing
  `./policies/` do not accidentally commit evidence.
- **Dry-run touches no filesystem state**, including the evidence
  store. Pure observability of what the run would have produced.

## 10. Testing strategy

Unit coverage (new):

- `pkg/flowsource/file_test.go`:
  - happy-path parse (3 DROPPED flows → 3 emitted)
  - non-DROPPED filtered out (mixed fixture)
  - malformed line skipped, counter incremented
  - gzip file
  - stdin source
  - empty file (clean termination)
  - very long line (>64 KiB labels) parsed
- `pkg/evidence/schema_test.go`:
  - roundtrip (unmarshal → marshal → byte-equal after sort)
- `pkg/evidence/merge_test.go`:
  - new rule appended
  - existing rule merged (counts, samples, sessions)
  - `EvidenceSamples` cap enforced (FIFO oldest dropped)
  - `EvidenceSessions` cap enforced
  - rules from previous sessions preserved when not re-produced
- `pkg/evidence/paths_test.go`:
  - `XDG_CACHE_HOME` override honored
  - hash is stable and output-dir-sensitive
- `pkg/diff/yaml_test.go`:
  - identical inputs → empty diff
  - addition-only diff
  - deletion-only diff
  - mixed diff
- `pkg/policy/builder_test.go` (extended):
  - `BuildPolicy` returns an attribution whose rule keys match the
    emitted ingress/egress rules 1:1
  - endpoint, CIDR, entity peers all represented in the attribution

Integration (`cmd/cpg/`):

- `replay_test.go`: replay a `testdata/flows/small.jsonl` fixture into
  `t.TempDir()`, assert on disk YAML files and evidence files.
- `replay_test.go` (case): malformed + non-DROPPED mixed in, counters
  match.
- `dryrun_test.go`: replay with `--dry-run`, assert no files written,
  captured stdout contains the expected "would write" line and diff.
- `explain_test.go`: seed a known evidence file in `t.TempDir()`,
  invoke `cpg explain`, assert on stdout for text/json/yaml. Check
  filter combinations and empty-match branch.

Fixtures (new):

- `testdata/flows/small.jsonl`: 20 flows — ingress/egress,
  endpoint/CIDR/entity, multiple ports/protocols, stable timestamps
  (2026-04-24T14:00:00Z + n×1s).
- `testdata/flows/with_non_dropped.jsonl`: 10 DROPPED + 5 ALLOWED.
- `testdata/flows/malformed.jsonl`: 5 flows with 1 broken line in the
  middle.
- `testdata/evidence/api-server.json`: pre-populated evidence for
  explain tests.

## 11. Documentation

README additions (on top of existing content):

1. New quickstart paragraph, directly after the current "Quick start",
   introducing `cpg replay` with a one-liner using `hubble observe`.
2. "Offline replay" section: when to use it, the capture workflow,
   iteration loop, interaction with `--cluster-dedup`.
3. "Explain policies" section: what `cpg explain` answers, example
   session, evidence storage location, how to share evidence with
   `--evidence-dir`.
4. "Dry-run" section: preview vs write, composition with replay,
   interpreting the diff.
5. Updated "Project structure" block listing the new packages.
6. Updated "Flags" reference including the new flags.

Limitations section: keep the auto-apply disclaimer (dry-run does not
change that position), update the L7 note to reference its own
upcoming spec, drop UC2-related wording.

## 12. Release and rollout

- Target version: `1.6.0` (minor bump — new features, no breaking
  changes). Release via the release-please PR-driven workflow already
  in place. No manual tag.
- No migration step: the new evidence directory is created on demand
  under XDG cache.
- Behavior of every existing command and flag stays identical for
  users who do not adopt the new features.

## 13. Open items / future work (out of scope here)

- L7 rule support (separate spec).
- Aggregated analytics on evidence (top-N peers, time-series, etc.).
- Protobuf binary replay format (deferred unless requested).
- Remote evidence storage (S3, etc.) — not needed for v1.
