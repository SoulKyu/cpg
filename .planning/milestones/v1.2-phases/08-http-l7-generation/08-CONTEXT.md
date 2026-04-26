# Phase 8: HTTP L7 Generation — Context

**Gathered:** 2026-04-25
**Status:** Ready for planning
**Mode:** Auto-generated (`--auto` flag — implementation-focused phase, decisions at Claude's discretion within boundaries below)

<domain>
## Phase Boundary

Light up the L7 codegen branch reserved by Phase 7. Users running `cpg generate --l7` (or `cpg replay --l7`) against captures with L7 visibility enabled see HTTP rules attached to their L4 port entries. Users running without `--l7` get byte-identical YAML to v1.1.

Concretely, this phase delivers:
1. HTTP L7 rule extraction from `Flow.L7.Http` records (HTTP-01).
2. Method casing normalization to uppercase before any merge / dedup (HTTP-02). Required upstream of normalize so byte-stable YAML stays byte-stable.
3. Path emission as Cilium-compatible RE2 regex via `regexp.QuoteMeta(path)` + `^…$` anchoring. No regex inference, no path templating, no auto-collapse — one rule per observed (method, path) pair (HTTP-03).
4. Multi-(method, path) merge into a single `toPorts.rules.http` list — never multiple PortRule blocks for the same (port, protocol) (HTTP-04).
5. Header / Host / HostExact rules NEVER emitted (HTTP-05). Writer-side lint test ensures these fields are absent.
6. Passive empty-records detection (VIS-01): when `--l7` is set but zero `Flow.L7` records arrive in the observation window, emit a single, named, actionable warning.
7. Evidence v2 emission for HTTP rules — `RuleEvidence.L7.Protocol="http"` populated with method + path.

Mapped requirements: HTTP-01, HTTP-02, HTTP-03, HTTP-04, HTTP-05, VIS-01.

</domain>

<decisions>
## Implementation Decisions

### Where the L7 codegen lives
- **`pkg/policy/l7.go`** (new file) — the HTTP extraction + emission logic. Keeps the L4 builder.go uncluttered. `extractL7HTTP(*flowpb.Flow) []api.PortRuleHTTP` returns nil when `Flow.L7.Http == nil`.
- `pkg/policy/builder.go::BuildPolicy` calls into `l7.go` only when the bucket carries L7 records AND the upstream pipeline passed `L7Enabled=true`. Otherwise the L4 codepath is byte-identical to v1.1.
- `pkg/hubble/aggregator.go::AggKey` does NOT extend with HTTP fields. L7 is a property of the port-rule inside the bucket, not of the bucket itself.

### Method normalization
- Done **before** the merge / sort / dedup steps so two flows reporting `get` and `GET` don't dedup into separate rules.
- Helper `normalizeMethod(s string) string` returns `strings.ToUpper(strings.TrimSpace(s))`. Empty input → empty result, caller drops the L7 entry rather than emit an empty-method rule.

### Path emission
- `regexp.QuoteMeta(rawPath)` first, then prefix `^`, suffix `$`. Result example: `/api/v1/users/123` → `^/api/v1/users/123$`.
- Path with `?` or `#` → caller strips fragment + query before regex generation. Hubble flow records expose `Url` and `Path` separately; we use `Path`. Query params are NOT part of Cilium HTTP path matching.
- Empty path → emit `^/$` (root) rather than skipping the rule. Documented in tests.
- `nil`-safe: `Flow.L7.Http.GetPath()` returning empty + GetMethod()=="GET" → `{Method:"GET", Path:"^/$"}`.

### Anti-features (per HTTP-05)
- `headerMatches`, `host`, `hostExact` are NEVER emitted from cpg, even if Hubble flow records carry them.
- Writer-side lint test: walk all generated `*.yaml` for `headerMatches:`, `host:`, `hostExact:` strings via grep — fail the test if any appear.
- Documented as intentional anti-feature in the package godoc and the v1.2 README section (Phase 9 ships the README).

### Multi-rule merge inside a port
- For a given (src, dst, port, protocol) tuple with multiple distinct HTTP entries, all entries land in ONE `PortRule` with a single `Rules.HTTP` slice.
- Sort key for byte-stability (already established in Phase 7 normalizeRule): `(Method, Path)` lexicographic. Phase 7's normalize sort is the source of truth — Phase 8 produces unsorted lists, normalize sorts them.

### Empty-records detection (VIS-01)
- Implemented in `pkg/hubble/pipeline.go` after the stream finalizes. If `L7Enabled && totalL7HTTPRecords + totalL7DNSRecords == 0 && totalFlows > 0`, emit a single warning with the workload list.
- Counter exposed on `SessionStats` (`L7HTTPCount`, `L7DNSCount`) so the warning carries concrete numbers ("0 L7 records observed across N flows from M workloads").
- Warning format: `Logger.Warn("--l7 set but no L7 records observed",
  zap.Strings("workloads", []string{...}), zap.Int("flows", n), zap.String("hint", "see README L7 prerequisites"))`.
- Warning fires once per pipeline run (single `Finalize()` call).
- README link target: anchor `#l7-prerequisites` — Phase 9 ships the actual README content; for Phase 8 we just include the anchor in the hint.

### Evidence v2 emission
- The `RuleAttribution → RuleEvidence` conversion in `pkg/hubble/evidence_writer.go` populates `L7Ref{Protocol: "http", HTTPMethod, HTTPPath}` for HTTP-bearing rules. DNS-bearing rules ship in Phase 9.
- `RuleKey` (Phase 7) discriminator is set: `RuleKey.L7Discriminator = "http:GET:/api/v1/users"`. Format: `{protocol}:{method}:{path}` for HTTP, exact format documented in `attribution.go` godoc.

### Verdict filter under L7 visibility
- Research flagged DROPPED-vs-REDIRECTED as a live-cluster validation gap. Pragmatic decision: KEEP the existing `Verdict_DROPPED` filter; if a live-cluster session shows L7 records arriving with `Verdict_REDIRECTED`, raise a follow-up issue and consider expanding in v1.3 (L7-FUT-01).
- Rationale: defensive correctness — REDIRECTED means Cilium PROXIED the traffic, possibly because an existing L7 policy already matched. We do NOT want to generate new rules from already-policied traffic. DROPPED-only stays correct.

### Out of scope for THIS phase
- DNS L7 generation (Phase 9).
- `cpg explain` L7 filters (Phase 9).
- README two-step workflow doc (Phase 9).
- Path / FQDN auto-collapse heuristics (deferred to v1.3).
- Header generation (anti-feature, never).

### Claude's Discretion
All other implementation choices (helper function names, test fixture filenames, exact zap log field names) are at Claude's discretion. Conform to existing cpg conventions (TDD, table-driven tests, zap logging, errgroup).

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/policy/builder.go::BuildPolicy` — entry point for flow→CNP. Returns `(cnp, []RuleAttribution)`. Phase 7 extended `RuleKey` with `L7Discriminator`.
- `pkg/policy/dedup.go::normalizeRule` — Phase 7 already sorts L7 lists deterministically.
- `pkg/policy/merge.go::mergePortRules` — Phase 7 fixed Rules-field-drop. Multi-(method, path) merge for one port works mechanically.
- `pkg/policy/attribution.go::RuleKey` — Phase 7 added `L7Discriminator string` (omitempty).
- `pkg/evidence/schema.go::RuleEvidence` — Phase 7 added `L7 *L7Ref` (omitempty). `L7Ref{Protocol, HTTPMethod, HTTPPath, DNSMatchName}`.
- `pkg/hubble/evidence_writer.go` — converts RuleAttribution → RuleEvidence; this phase wires the L7 fields.
- `pkg/hubble/pipeline.go::PipelineConfig.L7Enabled` — Phase 7 wired the flag through. This phase reads it.
- `pkg/hubble/pipeline.go::SessionStats` — adds counters for L7 records this phase.

### Established Patterns
- TDD-first commits (test → implementation, separate commits).
- Table-driven tests with descriptive subtests.
- `Flow.L7.Http.GetMethod()` / `GetPath()` accessors (proto getters, nil-safe).
- `regexp.QuoteMeta` from stdlib for safe regex emission (no third-party regex builders).
- Conventional Commits with co-author tag.

### Integration Points
- `pkg/policy/l7.go` (NEW) — HTTP extraction logic.
- `pkg/policy/builder.go::BuildPolicy` — invoke l7.go when L7Enabled + Flow.L7.Http present.
- `pkg/policy/builder.go::AttributionOptions` — variadic option struct already established in v1.1; add `WithL7(bool)` if needed.
- `pkg/hubble/aggregator.go` — receives flows, calls BuildPolicy. No structural change. Possibly increments L7HTTP counter.
- `pkg/hubble/pipeline.go::Finalize` — emits VIS-01 warning when L7Enabled && zero L7 records.
- `pkg/hubble/evidence_writer.go::convertAttribution` — populate `L7Ref` for HTTP rules.
- `cmd/cpg/replay_test.go::TestReplay_L7FlagByteStable` — Phase 7 test asserts byte-identical output. Phase 8 ADDS new tests with L7 fixtures while keeping the existing one (no L7 in fixture → `--l7=true` still produces same output).

</code_context>

<specifics>
## Specific Ideas

- The HTTP-05 anti-feature lint test should be a real test in `pkg/policy/l7_test.go` that walks generated YAML strings and grep-asserts none of `headerMatches:`, `host:`, `hostExact:` appear. This makes the anti-feature self-documenting and CI-enforced.
- Capture a small L7-bearing jsonpb fixture from a real or synthetic Hubble session for replay tests. 3-5 flows is enough: GET /api/v1/users, POST /api/v1/users, GET /healthz from one frontend → backend pair.
- The VIS-01 warning text must include the literal `--l7` flag name verbatim and the README anchor (`#l7-prerequisites`) for grep-discoverability. Even if Phase 9 hasn't shipped the anchor yet, naming it now means the link works once Phase 9 lands.
- Method normalization: write the helper as a package-private `normalizeHTTPMethod` in `pkg/policy/l7.go` rather than a public `Normalize*` — internal contract.

</specifics>

<deferred>
## Deferred Ideas

- Path auto-collapse heuristic (deferred to v1.3 per HTTP-FUT-01).
- Header allowlist (e.g., emit `headerMatches` for non-secret headers like `User-Agent`) — deferred indefinitely; the secret-leakage risk and the user-confusion of "which headers are safe" are not worth the value.
- Verdict filter expansion for REDIRECTED — deferred to v1.3 (L7-FUT-01) pending live-cluster validation.
- `--min-flows-per-l7-rule N` low-confidence gate — deferred to v1.3 (L7-FUT-02).
- DROPPED-but-no-L7-record edge case (HTTP record empty in flow, port=80/443, dst is external) — current behavior emits L4-only; correct behavior. Document as known good in the SUMMARY.

</deferred>
