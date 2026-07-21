# Deferred Items — Phase 18 (query-tools)

Out-of-scope discoveries logged during plan execution, per the executor's scope-boundary
rule: only auto-fix issues directly caused by the current task's changes.

## From 18-01 (pkg/explain promotion)

### Flaky test: `TestManager_Start_ShutdownRacesSetup` (pkg/session)

- **Package:** `pkg/session` (Phase 17, session lifecycle — unrelated to 18-01's
  `pkg/explain`/`cmd/cpg/explain*.go` scope)
- **Symptom:** `go test ./... -race -count=1` intermittently fails with:
  ```
  Error: "[...4 tmpdirs...]" should have 5 item(s), but has 4
  Test: TestManager_Start_ShutdownRacesSetup
  Message: no orphaned session tmpdir should remain after Shutdown raced Start's setup window
  ```
  preceded by `WARN shutdown: session did not exit within deadline; removing tmpdir anyway`.
- **Observed:** Passed in a whole-module run immediately after 18-01's Task 2 GREEN commit,
  then failed on a later whole-module run with no `pkg/session` changes in between. Re-run
  in isolation (`go test ./pkg/session/... -race -count=1 -run TestManager_Start_ShutdownRacesSetup`)
  passed immediately. This is a timing-sensitive race between `Shutdown()`'s deadline and
  `Start()`'s setup goroutine, not a regression introduced by 18-01.
- **Action taken:** None — out of scope for 18-01 (no files under `pkg/session` were touched
  by this plan). Not fixed, per the executor's scope-boundary rule.
- **Recommendation:** Track as a known-flaky test if it recurs; investigate the deadline/
  setup-window race in `pkg/session/manager.go`'s `Shutdown()` path if it becomes disruptive
  to CI. Not blocking for Phase 18's remaining plans (18-02..18-05), none of which touch
  `pkg/session`'s shutdown-race window.

## From 18-04 (get_evidence + pagination infra)

### Recurrence: 3 `pkg/session` tmpdir-count tests failed under `go test ./... -race -count=1`

- **Tests:** `TestManager_Start_ShutdownRacesSetup`, `TestManager_Start_SetupFailureRollsBackSlot`,
  `TestManager_Start_ShutdownCancelsSetupCtx` (all `pkg/session/manager_test.go`, unrelated to
  18-04's `cmd/cpg`/`go.mod` scope).
- **Root cause confirmed:** each test snapshots `filepath.Glob(filepath.Join(os.TempDir(),
  "cpg-session-*"))` before/after and asserts the count is unchanged. This is inherently racy
  under concurrent execution — any other process on the same machine creating/removing
  `/tmp/cpg-session-*` at the same moment (another package's test binary in the same `go test
  ./...` run, or another worktree-agent's `pkg/session` tests running in parallel as part of
  this same wave) shifts the glob count out from under the assertion.
- **Verified unrelated to this plan:** re-running the same 3 tests in isolation
  (`go test ./pkg/session/... -race -count=1 -run '<3 test names>'`) passed cleanly on the
  first try — confirms a shared-`/tmp` concurrency artifact, not a regression from 18-04's
  `cmd/cpg`/pagination changes (this plan touches zero files under `pkg/session`).
  `go test ./cmd/cpg/... -race -count=1` (this plan's actual required verification gate)
  passed cleanly on every run.
- **Action taken:** None — out of scope for 18-04 per the executor's scope-boundary rule.
- **Recommendation:** Same as 18-01's entry above — if this keeps recurring across future
  plans/waves, the fix belongs in `pkg/session/manager_test.go`: scope the before/after glob
  to tmpdirs this specific test created (e.g. record the Manager's own tmpdir path instead of
  diffing a shared, unscoped `/tmp` glob), not a change to `pkg/session/manager.go` itself.
