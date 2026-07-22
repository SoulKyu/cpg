---
phase: 21-cilium-compatibility-matrix-runtime-detection
fixed_at: 2026-07-22T13:52:15Z
review_path: .planning/phases/21-cilium-compatibility-matrix-runtime-detection/21-REVIEW.md
iteration: 1
findings_in_scope: 3
fixed: 3
skipped: 0
status: all_fixed
---

# Phase 21: Code Review Fix Report

**Fixed at:** 2026-07-22T13:52:15Z
**Source review:** .planning/phases/21-cilium-compatibility-matrix-runtime-detection/21-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 3 (Warnings WR-01, WR-02, WR-03; the 6 Info findings are out of scope)
- Fixed: 3
- Skipped: 0

## Fixed Issues

### WR-01: Always-on CLI version preflight is unbounded and has no opt-out

**Files modified:** `cmd/cpg/generate.go`, `pkg/k8s/version.go`
**Commit:** c4db965
**Applied fix:** Added `versionPreflightTimeout = 3 * time.Second` and wrapped the CLI
probe in `context.WithTimeout(ctx, versionPreflightTimeout)` inside
`maybeRunVersionPreflight`, so an unreachable/wedged apiserver can no longer stall
`cpg generate` startup (mirrors the unexported `pkg/k8s.versionDetectTimeout`). In
`DetectCiliumVersion`'s `default:` branch, guarded the `warnPodsListFailed` warning
with `!errors.Is(ctx.Err(), context.Canceled)` so a SIGINT during the preflight no
longer emits a spurious failure warning about the operator's own shutdown.

Scope note (deviation from the full "Additionally consider" guidance): the two
optional suggestions in WR-01's Fix section were NOT applied, as each is a design-sized
change beyond a targeted review fix:
- (a) skipping the kubeconfig probe on the explicit `--server` path, or reusing the
  GetNodes secondary there — this is the `--server` behavioral-regression item and
  requires threading `f.server`/`f.tlsEnabled`/`f.timeout` into the preflight call site.
- (b) a `--no-version-preflight` opt-out flag mirroring VIS-06.
Both remain valid follow-ups; the applied fix resolves the two acute runtime hazards
(unbounded stall on a hung apiserver, and the spurious Ctrl+C warning).

Adaptation note: the reviewer's literal fix used `if ctx.Err() == nil`, which would also
suppress the warning on a genuine budget timeout. This fix uses
`!errors.Is(ctx.Err(), context.Canceled)` instead so a real `DeadlineExceeded`/list
failure still warns (operator visibility) while only shutdown-cancellation is silenced —
addressing WR-01 consequence 3 precisely without hiding legitimate timeouts.

### WR-02: GetNodes RPC escapes the versionDetectTimeout hard ceiling

**Files modified:** `pkg/k8s/version.go`
**Commit:** 0144ae0
**Applied fix:** Wrapped the unary `client.GetNodes(...)` call in
`context.WithTimeout(ctx, effective)` (the same `min(timeout, versionDetectTimeout)`
budget that already bounds the dial via `waitForVersionConnReady`). A relay whose
channel reaches Ready but never answers the RPC (half-open middlebox, wedged relay) can
no longer block until the caller ctx (`setupCtx`, up to the 24h `maxSessionDuration`)
expires — the whole probe is now bounded, not just the connection.

### WR-03: featureFloors table cannot express the proxy-visibility <= 1.16 ceiling

**Files modified:** `pkg/k8s/version.go`
**Commit:** b8a389d
**Applied fix:** Amended the `featureFloors` doc comment (option (b), the "at minimum"
fix from the review) to state explicitly that the table models lower bounds only, that
the proxy-visibility ceiling (removed at Cilium 1.17) is intentionally left as doc-only
enforcement with VIS-01's "no L7 records observed" warning as the downstream signal, and
that the "keep in sync with README" instruction covers floors only and is not a claim of
full README/table parity.

Scope note: the review's option (a) — adding an optional `ceiling` field to the table
and emitting an above-ceiling no-op warning when `--l7` is set on a detected >= 1.17
cluster — was NOT applied. It is a feature-sized change (new struct field, evaluation
logic, wiring into the L7 preflight, and new tests) rather than a targeted fix, and the
review explicitly offers (b) as the acceptable minimum. Option (a) remains a valid
follow-up if runtime enforcement of the ceiling is later desired.

## Skipped Issues

None — all 3 in-scope warnings were fixed.

---

_Fixed: 2026-07-22T13:52:15Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
</content>
</invoke>
