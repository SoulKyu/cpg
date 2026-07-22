---
phase: 18-query-tools
plan: 04
subsystem: api
tags: [go, mcp, go-sdk, jsonschema, pagination, cursor, explain, evidence]

# Dependency graph
requires:
  - phase: 18-query-tools (18-01)
    provides: pkg/explain package (exported Filter/Match, Output, RenderJSON/RenderText/RenderYAML, ParsePeerLabel) — reused verbatim, not re-implemented
  - phase: 18-query-tools (18-03)
    provides: cmd/cpg/mcp_query.go composition root (registerQueryTools), resolveSession helper, D-16 error-return convention this plan extends
provides:
  - "mustQuerySchema[T] — the only mechanism (explicit *jsonschema.Schema construction + Properties[field].Enum patch) to add an Enum constraint to a go-sdk v1.6.1 tool schema; struct tags cannot express it"
  - "Opaque base64 boundary-key cursor (encodeCursor/decodeCursor) + generic paginate() helper, fail-closed on malformed input, never an absolute offset"
  - "get_evidence (QRY-03): paginated per-rule evidence identical in per-record shape to `cpg explain --output json`, mirroring the real explain.Filter fields (direction/port/peer/peer_cidr/http_method/http_path/dns_pattern — no protocol)"
  - "jsonschema-go v0.4.3 promoted from an indirect to a direct go.mod dependency (same audited version, no new download)"
affects: [18-05 (list_dropped_flows — consumes mustQuerySchema/cursor/paginate/defaultFlowLimit/maxFlowLimit verbatim), 19-security-hardening-e2e]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Enum-constrained MCP tool schema: build via jsonschema.For[T](nil), then patch schema.Properties[field].Enum before assigning Tool.InputSchema — bypasses go-sdk's struct-tag reflection entirely (D-14)"
    - "Boundary-key pagination cursor: opaque base64 token encoding (namespace, workload, stable-index), resumed by scanning for the first item whose key sorts after the decoded cursor — never an absolute offset, so it degrades gracefully (occasional skip/dup at the boundary) rather than catastrophically under mid-capture file-set drift (D-05/D-06)"
    - "Pagination wraps the promoted renderer, never baked into it (Pattern 2): get_evidence calls pkg/explain.Filter.Match to get the full matched set, then paginate() slices it — pkg/explain itself stays unpaginated and shared with the CLI unchanged"

key-files:
  created:
    - cmd/cpg/mcp_query_pagination.go
    - cmd/cpg/mcp_query_pagination_test.go
    - cmd/cpg/mcp_query_evidence.go
    - cmd/cpg/mcp_query_evidence_test.go
  modified:
    - cmd/cpg/mcp_query.go
    - go.mod

key-decisions:
  - "indexedRuleEvidence{rule, idx} pairs each matched RuleEvidence with its position in the evidence file's own (unfiltered) Rules array as the cursor's 'stable in-file index' — pkg/evidence.Merge keeps Rules sorted by (Direction, Key) after every write, so this index is stable across re-scans unless a concurrent insert shifts it, which paginate's boundary-scan resumption tolerates gracefully per D-06"
  - "Default/max pagination-limit consts (defaultFlowLimit/maxFlowLimit/defaultEvidenceLimit/maxEvidenceLimit) are defined once in mcp_query_pagination.go so 18-05's list_dropped_flows references the same D-07 numbers instead of hand-copying them; the two flow-scale consts have no consumer until 18-05 lands, so they carry an explained //nolint:unused rather than sitting as an unexplained lint gap or being deferred to a later plan"
  - "registerGetEvidenceTool is its own function (mcp_query_evidence.go), unlike the 3 tools registered inline in registerQueryTools (mcp_query.go) — because its InputSchema needs the mustQuerySchema enum-patching mechanism; keeping the schema construction next to the args struct it describes avoids splitting one tool's definition across two files"
  - "get_evidence's argument surface deliberately omits a protocol field: explain.Filter has no Protocol field (Direction/Port/PeerLabel/PeerCIDR/Since/Now/HTTPMethod/HTTPPath/DNSPattern only), and the plan's own D-10 correction is explicit that a schema field with nothing behind it is a dead, misleading surface (QRY-05 truthful-behavior requirement)"

patterns-established:
  - "mustQuerySchema[T](enumFields) is the one and only path to an Enum-constrained tool schema for the rest of this milestone — 18-05's direction/dropclass enums reuse it verbatim, not a second hand-rolled schema-patching helper"
  - "paginate[T any](items, keyOf, after, limit, defaultLimit, maxLimit) takes a keyOf extraction closure rather than requiring T to implement an interface — necessary because paginate's callers slice types (evidence.RuleEvidence) owned by another package, which cannot have methods added to them from cmd/cpg"

requirements-completed: [QRY-03]

duration: ~25min
completed: 2026-07-21
---

# Phase 18 Plan 04: Pagination + Enum-Schema Infra, get_evidence Summary

**Shared mustQuerySchema/cursor/paginate primitives plus get_evidence (QRY-03): paginated per-rule flow evidence reusing pkg/explain.Filter/Output verbatim, with a direction-enum schema and D-05/D-06 fail-closed cursor handling.**

## Performance

- **Duration:** ~25 min (approx.)
- **Started:** 2026-07-21T16:25:00+02:00 (approx., immediately after 18-03's completion)
- **Completed:** 2026-07-21T16:50:51Z
- **Tasks:** 2 completed
- **Files modified:** 6 (4 created, 2 modified)

## Accomplishments

- `cmd/cpg/mcp_query_pagination.go`: `mustQuerySchema[T]` (the only way to add an `Enum` constraint to a go-sdk v1.6.1 tool schema — struct tags carry free-text description only and reject `WORD=`-shaped values), an opaque base64 boundary-key cursor codec (`encodeCursor`/`decodeCursor`, fail-closed on malformed input), and a generic `paginate()` helper with D-07 default/max limit clamping — all unit-tested independent of any real tool
- `get_evidence` (QRY-03/D-10): returns paginated per-rule evidence whose per-record shape is byte-identical to `cpg explain --output json`, by reusing the promoted `pkg/explain.Filter`/`Output` verbatim (Pattern 2 — pagination wraps the renderer's full matched set, never baked into it)
- Argument surface mirrors the real `explain.Filter` fields exactly (`direction`/`port`/`peer`/`peer_cidr`/`http_method`/`http_path`/`dns_pattern`) with deliberately no `protocol` field (D-10 plan-checker correction — `explain.Filter` has none)
- `direction` is schema-enum-constrained to `[ingress, egress]` via `mustQuerySchema[getEvidenceArgs]` (D-14); `ReadOnlyHint`/`IdempotentHint`/`OpenWorldHint(false)` annotations (D-16)
- Path-traversal guard (`evidence.ValidatePolicyRef`) before any `filepath.Join`; not-found detection via `evidence.IsNotExist` (wrapped error, never a string/type check), surfaced as an actionable error naming `list_policies`; malformed peer/peer_cidr and an invalid cursor return actionable `isError` text, never a panic (T-18-04-02)
- `go mod tidy` promotes `github.com/google/jsonschema-go v0.4.3` from `// indirect` to a direct require line — same already-audited version, no new download

## Task Commits

Each task was committed atomically (TDD RED/GREEN pairs):

1. **Task 1: Shared pagination + enum-schema infra (mustQuerySchema, cursor, paginate) + go mod tidy**
   - `9b8db7b` (test) - failing tests for mustQuerySchema/cursor codec/paginate
   - `2b722b0` (feat) - implementation + `go mod tidy` (jsonschema-go indirect → direct)
2. **Task 2: Implement get_evidence — paginated pkg/explain output (QRY-03/D-10)**
   - `4af5513` (test) - failing tests for get_evidence (shape, pagination, per-field filters, error texts)
   - `dd6d39f` (feat) - implementation (handler, args/result structs, registration wired into `registerQueryTools`)

**Additional deviation commit:**
- `3fb35d7` (style) - `//nolint:unused` on the two flow-scale pagination consts with no consumer until 18-05 (Rule 1-adjacent code-quality fix)

**Plan metadata:** (this commit, docs)

## Files Created/Modified

- `cmd/cpg/mcp_query_pagination.go` - `mustQuerySchema[T]`, `paginateBoundaryKey`/`compareBoundaryKey`, `encodeCursor`/`decodeCursor`, `clampLimit`, generic `paginate[T any]`, and the 4 default/max limit consts (2 evidence-scale used here, 2 flow-scale reserved for 18-05)
- `cmd/cpg/mcp_query_pagination_test.go` - unit tests: Enum-patching (+ unknown-field panic), cursor round-trip, fail-closed decode (invalid base64, truncated JSON, empty token), paginate across first/middle/last/empty pages, limit clamping
- `cmd/cpg/mcp_query_evidence.go` - `getEvidenceArgs`/`getEvidenceResult`, `registerGetEvidenceTool`, `indexedRuleEvidence`, `handleGetEvidence`, `buildEvidenceFilter`
- `cmd/cpg/mcp_query_evidence_test.go` - `TestMCPQueryGetEvidence` (11 subtests: shape/unfiltered, pagination, one narrowing assertion per filter field, malformed peer, unknown target, invalid cursor) + `TestMCPQueryGetEvidenceInputSchema` (required fields, direction enum, no protocol property); fixture helpers `buildEvidenceFixture`/`writeEvidenceFixture`/`seedEvidenceQuerySession`/`callGetEvidence`
- `cmd/cpg/mcp_query.go` - `registerGetEvidenceTool(server, mgr)` wired into `registerQueryTools`; doc comment updated to reflect get_evidence's inclusion
- `go.mod` - `github.com/google/jsonschema-go v0.4.3` moved from `// indirect` to a direct require line (via `go mod tidy`); `go.sum` unchanged

## Decisions Made

- `paginate[T any]` takes a `keyOf func(T) paginateBoundaryKey` extraction closure rather than requiring an interface method on `T`, since Go forbids adding methods to types (like `evidence.RuleEvidence`) owned by another package — this keeps `paginate` genuinely generic across get_evidence's `indexedRuleEvidence` wrapper today and 18-05's own item type tomorrow.
- The cursor's "stable in-file index" for get_evidence is each matched rule's position in the evidence file's own **unfiltered** `Rules` array (captured via `indexedRuleEvidence`), not its position in the filtered `matched` slice — `pkg/evidence.Merge` keeps `Rules` sorted by `(Direction, Key)` after every write, so this index survives re-scans as long as the rule set itself is stable, and degrades gracefully (not catastrophically) if a concurrent insert shifts it by one, per D-06's accepted trade-off.
- Default/max limit consts for both flow-scale (18-05) and evidence-scale (this plan) tools live together in `mcp_query_pagination.go` per D-07's "expose as named consts so both tools reference them" — the two flow-scale consts (`defaultFlowLimit`/`maxFlowLimit`) have no consumer until 18-05 lands; rather than leave them as a silent lint gap or defer the shared-const design to a later plan (contradicting the plan's own explicit instruction), they carry an explained `//nolint:unused` naming 18-05 as the resolver.
- No `Since`/`Now` population in `buildEvidenceFilter`: get_evidence's argument surface has no time-range filter (D-03 excludes it from the milestone), so `explain.Filter`'s `Since`/`Now` stay at their zero value, which `Filter.Match` already treats as "unset" — no dead-but-populated field.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - code-quality] Suppressed `unused` lint on `defaultFlowLimit`/`maxFlowLimit`**
- **Found during:** post-implementation lint pass (`golangci-lint run ./cmd/cpg/... --tests`, via `rtk proxy` — the direct binary's `--out-format` flag is broken under this environment's hook)
- **Issue:** Task 1's action text directs defining both flow-scale and evidence-scale limit consts now so 18-05 can reference the same D-07 numbers later, but `defaultFlowLimit`/`maxFlowLimit` have zero call sites until 18-05 lands — golangci-lint's `unused` check (enabled in `.golangci.yml`) correctly flags this as dead code today.
- **Fix:** Added a `//nolint:unused` directive on each const with a comment naming 18-05 as the consumer, rather than leaving an unexplained lint suppression, silently ignoring the flag, or deferring the shared-const design (which would contradict the plan's explicit D-07 instruction).
- **Files modified:** `cmd/cpg/mcp_query_pagination.go`
- **Verification:** `rtk proxy golangci-lint run ./cmd/cpg/... --tests` reports 0 issues; `go build ./...` and `go test ./cmd/cpg/... -race -count=1` still pass.
- **Committed in:** `3fb35d7`

---

**Total deviations:** 1 auto-fixed (code-quality, Rule 1-adjacent)
**Impact on plan:** No scope creep — no new tools, no new fields beyond what the plan specified. The fix only adds an explanatory suppression comment for a transient, plan-directed condition that self-resolves once 18-05 lands.

## Issues Encountered

A full-repository `go test ./... -race -count=1` diligence run (beyond this plan's own required `go test ./cmd/cpg/... -race -count=1` gate) surfaced 3 failing `pkg/session` tests (`TestManager_Start_ShutdownRacesSetup`, `TestManager_Start_SetupFailureRollsBackSlot`, `TestManager_Start_ShutdownCancelsSetupCtx`). Investigated and confirmed out of scope: all 3 assert an unchanged `filepath.Glob(os.TempDir(), "cpg-session-*")` count before/after, which is inherently racy against any other concurrent process creating/removing session tmpdirs on the same machine (this environment runs parallel worktree-agent test suites). Re-running the same 3 tests in isolation passed cleanly on the first try, confirming this plan's `cmd/cpg`/`go.mod`-only changes are not the cause — `pkg/session` is untouched by this plan. Logged as a recurrence in `.planning/phases/18-query-tools/deferred-items.md` (an entry from 18-01 already documents the same flake); not fixed, per the scope-boundary rule.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `mustQuerySchema`, the boundary-key cursor codec (`encodeCursor`/`decodeCursor`), `paginate`, and the flow-scale limit consts (`defaultFlowLimit`/`maxFlowLimit`) are all in place exactly as 18-05's `list_dropped_flows` expects to consume them verbatim — no new primitives should be needed there.
- `get_evidence` (QRY-03) is fully wired and tested; `registerQueryTools` now registers 4 of the milestone's 5 query tools (`list_policies`, `get_policy`, `get_cluster_health`, `get_evidence`) — only `list_dropped_flows` (18-05) remains before Phase 18's tool table is complete.
- `go build ./...`, `go test ./cmd/cpg/... -race -count=1` (129 tests), `go vet ./...`, `gofmt -l` (clean), `rtk proxy golangci-lint run ./cmd/cpg/... --tests` (0 issues), and `govulncheck ./...` (no vulnerabilities) are all green.
- REQUIREMENTS.md: QRY-03 marked complete. QRY-05 deliberately left pending per this plan's explicit instruction — it stays open until 18-05 closes the full 5-tool table (the orchestrator previously reverted a premature QRY-05 marking after 18-03 for exactly this reason).
- No blockers for 18-05.

---
*Phase: 18-query-tools*
*Completed: 2026-07-21*

## Self-Check: PASSED

- FOUND: cmd/cpg/mcp_query_pagination.go
- FOUND: cmd/cpg/mcp_query_pagination_test.go
- FOUND: cmd/cpg/mcp_query_evidence.go
- FOUND: cmd/cpg/mcp_query_evidence_test.go
- FOUND: cmd/cpg/mcp_query.go
- FOUND: go.mod
- FOUND: .planning/phases/18-query-tools/deferred-items.md
- FOUND commit: 9b8db7b (test: failing tests for pagination + enum-schema infra)
- FOUND commit: 2b722b0 (feat: implement pagination + enum-schema infra + go mod tidy)
- FOUND commit: 4af5513 (test: failing tests for get_evidence)
- FOUND commit: dd6d39f (feat: implement get_evidence)
- FOUND commit: 3fb35d7 (style: nolint fix for unused flow-scale consts)
