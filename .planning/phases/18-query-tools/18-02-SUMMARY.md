---
phase: 18-query-tools
plan: 02
subsystem: api
tags: [go, mcp, cilium, hubble, cluster-health, yaml, json, query-tools, read-model]

# Dependency graph
requires:
  - phase: 11-aggregator-suppression-and-health-writer
    provides: cluster-health.json atomic writer (healthWriter.finalize) and the unexported report structs this plan exports
  - phase: 17-session-lifecycle
    provides: session tmpdir convention (policies YAML + evidence + cluster-health.json) these readers operate over
provides:
  - "output.ReadPolicyFile(path) -- exported CNP-YAML parser with wrapped-fs.ErrNotExist not-found contract"
  - "hubble.ReadClusterHealth(path) -- schema-version-gated cluster-health.json reader mirroring evidence.Reader.Read"
  - "Exported ClusterHealthReport/HealthSession/HealthDropJSON types for MCP outputSchema reflection (QRY-05)"
  - "TestRunPipeline_FinalizesHealthOnStreamError -- regression proof that hw.finalize() runs unconditionally after a pipeline error, closing the Pitfall-1 gap"
affects: [18-03, 18-05, mcp-query-tools, mcp-outputschema]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Wrapped-fs.ErrNotExist reader convention: os.ReadFile error wrapped with %w so errors.Is(err, fs.ErrNotExist) works downstream -- now applied uniformly across evidence.Reader.Read, output.ReadPolicyFile, and hubble.ReadClusterHealth"
    - "Schema-version gate idiom (os.ReadFile -> json.Unmarshal -> version check -> wrapped errors) mirrored from pkg/evidence/reader.go into pkg/hubble/health_reader.go"

key-files:
  created:
    - pkg/hubble/health_reader.go
    - pkg/hubble/health_reader_test.go
  modified:
    - pkg/output/writer.go
    - pkg/output/writer_test.go
    - pkg/hubble/health_writer.go
    - pkg/hubble/health_writer_test.go
    - pkg/hubble/pipeline_test.go

key-decisions:
  - "ReadPolicyFile adopts the wrapped-fs.ErrNotExist contract (matching evidence.Reader.Read / ReadClusterHealth), NOT readExistingPolicy's silent (nil, nil) -- gives get_policy/list_policies a uniform not-found signal; readExistingPolicy/ReadExisting left untouched for Write's merge path"
  - "clusterHealthReport/healthSession/healthDropJSON renamed in place to exported ClusterHealthReport/HealthSession/HealthDropJSON with byte-identical JSON tags (D-12 passthrough, zero derived fields)"
  - "New pipeline test delays its injected stream error by 150ms rather than firing it synchronously -- removes a two-point select race (outer flow-select and healthCh-send-select both compete with gctx cancellation against already-ready channel operations) that would otherwise make the accumulated-drop assertion flaky depending on goroutine scheduling"

patterns-established:
  - "All three session-tmpdir readers (evidence.Reader.Read, output.ReadPolicyFile, hubble.ReadClusterHealth) now share one not-found convention: wrap with fs.ErrNotExist, let callers errors.Is/IsNotExist"

requirements-completed: [QRY-02, QRY-04]

# Metrics
duration: 20min
completed: 2026-07-21
---

# Phase 18 Plan 02: Query Tool Read-Side Exports Summary

**Exported `output.ReadPolicyFile` and `hubble.ReadClusterHealth` with a uniform wrapped-`fs.ErrNotExist` reader contract, plus a regression test proving `hw.finalize()` writes `cluster-health.json` unconditionally even after a mid-capture pipeline error.**

## Performance

- **Duration:** 20 min
- **Started:** 2026-07-21T13:29:00Z (approx.)
- **Completed:** 2026-07-21T13:49:10Z
- **Tasks:** 3 completed
- **Files modified:** 7 (2 created, 5 modified)

## Accomplishments

- `pkg/output.ReadPolicyFile(path)` reuses `readExistingPolicy`'s CNP-YAML unmarshal logic but wraps a missing file's error with `fs.ErrNotExist` instead of `readExistingPolicy`'s silent `(nil, nil)` -- the query tools (`get_policy`/`list_policies`, plans 18-03/18-05) now get a caller-detectable not-found signal via `errors.Is`.
- `pkg/hubble`'s three unexported cluster-health marshaling structs are now exported (`ClusterHealthReport`, `HealthSession`, `HealthDropJSON`) with byte-identical JSON tags, including the plural `infra_drops_total` tag -- ready for the MCP `outputSchema` (QRY-05) to reflect over.
- `pkg/hubble.ReadClusterHealth(path)` mirrors `evidence.Reader.Read`'s exact idiom: read, unmarshal, schema-version gate (`SchemaVersion != 1` rejected by name+path).
- `TestRunPipeline_FinalizesHealthOnStreamError` closes the Pitfall-1 gap: proves `hw.finalize(stats)` runs after `g.Wait()` with no `if err == nil` gate, so a pipeline that accumulates an infra drop before a genuine stream error still leaves a valid `cluster-health.json` on disk (D-13's 3-way branch: "file absent" means "zero drops," not "crashed").

## Task Commits

Each task was committed atomically:

1. **Task 1: Export output.ReadPolicyFile with a wrapped-not-exist contract** - `7ace14c` (feat)
2. **Task 2: Export cluster-health report types and add hubble.ReadClusterHealth** - `7dabea1` (feat)
3. **Task 3: Prove finalize() writes cluster-health.json on a pipeline error** - `41923ff` (test)

Additional fixup commit (lint cleanup on new code, see Deviations): `fb36fc3` (style)

_Note: tasks were marked `tdd="true"` but this plan's frontmatter is `type: execute` (not `type: tdd`), and each task's `<action>` combines implementation and tests as one unit rather than a separate `<behavior>`/`<implementation>` split -- see "TDD task interpretation" under Deviations for why commits are combined `feat`/`test` per task rather than split RED/GREEN pairs._

## Files Created/Modified

- `pkg/output/writer.go` - Added exported `ReadPolicyFile(path)`, reusing `readExistingPolicy`'s unmarshal logic with a wrapped-`fs.ErrNotExist` not-found contract
- `pkg/output/writer_test.go` - Added `TestReadPolicyFile_RoundTrips`, `TestReadPolicyFile_MissingFile`, `TestReadPolicyFile_MalformedYAML`
- `pkg/hubble/health_writer.go` - Renamed `clusterHealthReport`/`healthSession`/`healthDropJSON` to exported `ClusterHealthReport`/`HealthSession`/`HealthDropJSON`; updated `finalize()`'s sole call site
- `pkg/hubble/health_writer_test.go` - Updated 6 unqualified `clusterHealthReport` references to `ClusterHealthReport` (required for compilation after the rename; not in the plan's file list -- see Deviations)
- `pkg/hubble/health_reader.go` (new) - `ReadClusterHealth(path)`: read + schema-version-gate cluster-health.json, mirroring `evidence.Reader.Read`
- `pkg/hubble/health_reader_test.go` (new) - Happy path (incl. Remediation URL round-trip), missing file, malformed JSON, wrong schema_version
- `pkg/hubble/pipeline_test.go` - Added `errStreamSourceWithInfraDrop` fixture and `TestRunPipeline_FinalizesHealthOnStreamError`

## Decisions Made

- **Not-found convention resolved consistently across all 3 readers:** PATTERNS.md flagged an inconsistency between `readExistingPolicy`'s silent `(nil, nil)` and the wrapped-error convention used by `evidence.Reader.Read`/`ReadClusterHealth`. Resolved by giving `ReadPolicyFile` the wrapped-error contract (matching the other two) while leaving `readExistingPolicy`/`ReadExisting` untouched, since `Write()`'s internal merge check still needs the silent-nil sibling.
- **Test-only delay in the new pipeline test:** `errStreamSourceWithInfraDrop.StreamErr()` delivers its error after a 150ms delay rather than immediately. Analysis showed the naive immediate-delivery design races `gctx` cancellation against two already-ready channel operations inside the aggregator (the outer flow-select and the healthCh-send-select), each of which Go's `select` resolves via uniform-random choice when multiple cases are ready -- an immediate error could in principle win either race and cause the accumulated-drop assertion to fail intermittently. The delay separates the two operations by several orders of magnitude versus the in-memory work it waits out. Verified with 20 consecutive `-race` runs, all passing.
- **TDD task interpretation:** Tasks 1-3 carry `tdd="true"` but this plan's frontmatter is `type: execute`, and each task uses the standard `<action>`/`<behavior>` structure rather than the `<feature><behavior>/<implementation></feature>` split that drives literal RED-then-GREEN commit sequencing. Task 3 in particular tests pre-existing production behavior (finalize()'s unconditional call was already correct), so there is no "GREEN" implementation step to separate from "RED." Combined implementation+test commits (`feat`) for Tasks 1-2 and a test-only commit (`test`) for Task 3 were used instead of split RED/GREEN pairs. This plan is not `type: tdd`, so the Plan-Level TDD Gate Enforcement's mandatory gate-sequence validation does not apply.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated health_writer_test.go's unqualified type references**
- **Found during:** Task 2
- **Issue:** The plan's `files_modified` frontmatter lists only `pkg/hubble/health_writer.go`, `health_reader.go`, `health_reader_test.go`, and `pipeline_test.go` -- but `pkg/hubble/health_writer_test.go` (same package, not listed) references the unexported `clusterHealthReport` type by name in 6 places. Renaming the type without updating this file would break compilation.
- **Fix:** Replaced all 6 occurrences of `clusterHealthReport` with `ClusterHealthReport` in `health_writer_test.go`. Purely mechanical (type name only, same fields, same package).
- **Files modified:** pkg/hubble/health_writer_test.go
- **Verification:** `go build ./pkg/hubble/...` and `go test ./pkg/hubble/... -race -count=1` both pass
- **Committed in:** 7dabea1 (Task 2 commit)

**2. [Rule 1 - Bug/robustness] Delayed stream-error delivery in the new pipeline test fixture**
- **Found during:** Task 3
- **Issue:** PATTERNS.md's suggested `errStreamSource` variant delivers the stream error synchronously (channel already populated and closed before the goroutine starts). Tracing the aggregator's select statements showed this design races `gctx` cancellation against two already-ready channel operations, each resolved by Go's uniform-random `select` semantics when multiple cases are ready -- a latent source of intermittent test failure.
- **Fix:** `errStreamSourceWithInfraDrop.StreamErr()` sends the error from a goroutine after a 150ms `time.Sleep`, giving the aggregator's synchronous in-memory processing of the single buffered flow (map bookkeeping + one buffered channel send) ample headroom to complete before `gctx` is cancelled.
- **Files modified:** pkg/hubble/pipeline_test.go
- **Verification:** 20 consecutive `-race -count=20` runs, all passing; existing `TestRunPipeline_SurfacesStreamError` still green
- **Committed in:** 41923ff (Task 3 commit)

**3. [Rule 1 - Bug] Simplified embedded-field selector flagged by staticcheck**
- **Found during:** post-Task-1 lint pass
- **Issue:** `golangci-lint` (staticcheck QF1008) flagged `cnp.ObjectMeta.Name` in `TestReadPolicyFile_RoundTrips` as a redundant selector through the anonymously-embedded `metav1.ObjectMeta` field (`cnp.Name` is equivalent and idiomatic).
- **Fix:** Changed `event.Policy.ObjectMeta.Name, cnp.ObjectMeta.Name` to `event.Policy.Name, cnp.Name`. No behavior change.
- **Files modified:** pkg/output/writer_test.go
- **Verification:** `golangci-lint run ./pkg/output/... ./pkg/hubble/...` shows zero new issues (11 remaining issues are pre-existing debt in files/lines this plan did not touch -- `pkg/hubble/client.go`, `pkg/hubble/summary.go`, and the pre-existing `finalize()` error-cleanup lines in `health_writer.go` -- tracked per PROJECT.md as "26 lint issues... gated by only-new-issues, scoped to v1.5")
- **Committed in:** fb36fc3 (separate style commit)

---

**Total deviations:** 3 auto-fixed (1 blocking/Rule 3, 2 bug-robustness/Rule 1)
**Impact on plan:** All three were necessary for correctness (compilation, test determinism) or cleanliness (lint). No scope creep -- no files outside the plan's touched packages were modified, and no unrelated pre-existing lint debt was fixed.

## Issues Encountered

None beyond the deviations documented above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `output.ReadPolicyFile`, `hubble.ReadClusterHealth`, and the exported `ClusterHealthReport`/`HealthSession`/`HealthDropJSON` types are ready for plans 18-03 (`get_policy`/`list_policies`) and 18-05 (`get_cluster_health`) to consume directly -- no parse-logic duplication needed in the MCP handlers.
- The finalize-on-error regression test pins the evidence D-13's 3-way branch depends on: query-tool handlers can safely treat `errors.Is(err, fs.ErrNotExist)` from `ReadClusterHealth` as "zero infra/transient drops," distinct from a genuine error.
- This plan touched only `pkg/output` and `pkg/hubble` (zero `cmd/cpg` files), consistent with running in Wave 1 parallel to 18-01's `pkg/explain` promotion -- no merge conflicts expected.
- No blockers for the next plans in this phase.

## Self-Check: PASSED

- Created/modified files exist on disk: FOUND pkg/output/writer.go, pkg/output/writer_test.go, pkg/hubble/health_writer.go, pkg/hubble/health_reader.go, pkg/hubble/health_reader_test.go, pkg/hubble/pipeline_test.go, pkg/hubble/health_writer_test.go
- Commit hashes exist in git log: FOUND 7ace14c, 7dabea1, 41923ff, fb36fc3
- Task 1 acceptance criteria: PASS (ReadPolicyFile exported, readExistingPolicy/ReadExisting unchanged, go test ./pkg/output/... -race -count=1 green)
- Task 2 acceptance criteria: PASS (ReadClusterHealth exists, all 3 types exported, plural infra_drops_total tag preserved, go test ./pkg/hubble/... -race -count=1 green)
- Task 3 acceptance criteria: PASS (test exists, asserts both require.Error and require.FileExists, existing TestRunPipeline_SurfacesStreamError still green, 20x -race stress run clean)
- Plan-level verification: `go test ./pkg/output/... ./pkg/hubble/... -race -count=1` PASS; `go build ./...` PASS; `go vet ./...` clean
- `golangci-lint run ./pkg/output/... ./pkg/hubble/...`: 0 new issues (11 remaining are pre-existing debt, files/lines untouched by this plan)

---
*Phase: 18-query-tools*
*Completed: 2026-07-21*
