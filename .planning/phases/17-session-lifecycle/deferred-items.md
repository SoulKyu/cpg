# Deferred Items — Phase 17 session-lifecycle

Out-of-scope discoveries logged during plan execution (deviation rules scope
boundary: pre-existing issues unrelated to the current task's changes are not
auto-fixed).

## 17-05: Cross-package `os.TempDir()` glob race between `cmd/cpg` and `pkg/session` test binaries

**Found during:** Task 3 (`go test ./... -race -count=1` acceptance criterion)

**Symptom:** Under Go's default parallel package test execution, `pkg/session`'s
`TestManager_Start_ShutdownRacesSetup` (manager_test.go) occasionally fails its
`assert.Len(t, after, len(before), "no orphaned session tmpdir should remain
after Shutdown raced Start's setup window")` assertion — observed once in ~4
runs as `"[]" should have 1 item(s), but has 0`.

**Root cause:** Both `pkg/session/manager_test.go` (`TestManager_Start_ShutdownRacesSetup`)
and `cmd/cpg/mcp_session_test.go` (`TestMCPSessionLifecycleWiringAndStdoutPurity`)
glob/create real directories matching `os.TempDir()/cpg-session-*`. `cmd/cpg`'s
test drives a real `start_session` (D-07 bypass address `127.0.0.1:1`), which
creates and later removes a genuine `cpg-session-*` tmpdir via `pkg/session.Manager`.
Since `go test ./...` runs independent package test binaries concurrently by
default (not isolated per-package temp namespaces — `os.TempDir()` is a
process-wide, OS-level shared path), the two binaries' tmpdir lifecycles can
interleave and perturb `pkg/session`'s own before/after glob count, which
assumes it is the only writer to that glob pattern during its test.

**Evidence this predates 17-05 and is not caused by this plan's changes:**
- `git diff <base>..HEAD -- pkg/session/manager_test.go` shows `TestManager_Start_ShutdownRacesSetup`
  is byte-identical to the pre-17-05 baseline — untouched by this plan.
  `cmd/cpg/mcp_session_test.go` is not in this plan's `files_modified` and was
  not touched at all.
- `go test ./pkg/session/... -race -count=1` (isolated): reliably green (5+ runs).
- `go test ./... -race -count=1 -p 1` (sequential packages, eliminates the
  cross-binary race): reliably green.
- `go test ./... -race -count=1` (default parallel): green in 3 of 4 runs;
  the one failure was this specific pre-existing race, not a new assertion
  failure introduced by the WR-01 fix.

**Disposition:** Out of scope for 17-05 (WR-01 gap closure). Fixing it would
mean changing test tmpdir isolation strategy (e.g. `t.TempDir()`-scoped
prefixes, or `-p 1` in CI) — a test-infrastructure decision affecting both
`pkg/session` and `cmd/cpg`, not a WR-01 correctness concern. Not fixed here.

**Suggested follow-up (not actioned):** Either serialize `pkg/session` and
`cmd/cpg` in CI (`go test -p 1 ./...`), or give `TestManager_Start_ShutdownRacesSetup`
a collision-resistant glob pattern (e.g. tag the tmpdir with the test's own PID
or a per-run UUID prefix) so its orphan check cannot observe tmpdirs created by
sibling test binaries.
