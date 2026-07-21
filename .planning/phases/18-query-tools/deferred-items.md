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
