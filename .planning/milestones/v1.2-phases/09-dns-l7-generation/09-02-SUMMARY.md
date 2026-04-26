---
phase: 09-dns-l7-generation
plan: 02
subsystem: hubble
tags: [dns, l7, aggregator, evidence, vis-01, integration, tdd]

requires:
  - phase: 09-dns-l7-generation
    plan: 01
    provides: DNS codegen at policy layer (toFQDNs + companion + RuleKey.L7 dns discriminator)
  - phase: 08-http-l7-generation
    provides: aggregator HTTP counter pattern, evidence_writer.convert HTTP branch + Phase-9 DNS TODO, VIS-01 gate already summing HTTP+DNS
  - phase: 07-evidence-schema-v2
    provides: evidence.L7Ref{Protocol,DNSMatchName} schema field

provides:
  - Aggregator.l7DNSCount increment site (pkg/hubble/aggregator.go::Run): mirrors HTTP counter, independent of L7Enabled
  - evidenceWriter.convert DNS branch (pkg/hubble/evidence_writer.go): RuleEvidence.L7 = &L7Ref{Protocol:"dns", DNSMatchName} when Key.L7.Protocol == "dns"
  - Phase 8 TODO(phase-9) removed from evidence_writer.go
  - testdata/flows/l7_dns.jsonl: 3-flow DROPPED EGRESS DNS fixture (api.example.com, www.example.org, dup)
  - TestPipeline_L7DNS_GenerationAndEvidence: end-to-end integration test driving the full pipeline against the synthetic fixture
affects: [09-03 cpg explain L7 filter (now has on-disk evidence shape to drive against), 09-04 README L7 prerequisites]

tech-stack:
  added: []
  patterns:
    - "Diagnostic L7 counters increment unconditionally (independent of L7Enabled) so VIS-01 gate stays accurate when codegen is disabled"
    - "Synthesize DNS fixture with proto-correct field casing (IP capital, qtypes []string) for protojson FileSource ingestion"

key-files:
  created:
    - pkg/hubble/pipeline_l7_dns_test.go
    - testdata/flows/l7_dns.jsonl
  modified:
    - pkg/hubble/aggregator.go
    - pkg/hubble/aggregator_test.go
    - pkg/hubble/evidence_writer.go
    - pkg/hubble/evidence_writer_test.go

key-decisions:
  - "DNS counter increments BEFORE keyFromFlow's skip check (parallel to HTTP counter): diagnostic counter must reflect every DNS-bearing observation, including reserved/empty-namespace flows that don't reach a bucket."
  - "Unknown L7 protocol leaves re.L7 nil (defensive — keeps malformed Keys off disk). Validated by TestEvidenceWriter_ConvertUnknownL7Protocol."
  - "Two-flow construction in TestAggregator_L7DNSCount_Increments uses a factory function (not by-value Flow copy) to satisfy go vet's lock-copy check on proto messages."
  - "DNS fixture uses field name 'IP' (not 'ip') and qtypes as []string to satisfy protojson strict casing — matched against `go doc flow.Flow` and `flow.DNS`."
  - "Integration test parses the on-disk YAML into ciliumv2.CiliumNetworkPolicy via sigs.k8s.io/yaml and reuses pkg/policy/testdata.AssertHasKubeDNSCompanion (cross-package helper exposed by 09-01) rather than re-implementing the invariant inline."

patterns-established:
  - "End-to-end pipeline tests should assert on observed log fields (session summary l7_dns_count) for counters that are not otherwise persisted to disk."
  - "Cross-package test invariants (DNS-02 companion check) live in pkg/.../testdata so both internal and external tests share one definition."

metrics:
  duration: ~5 minutes
  tasks-completed: 2/2
  completed: 2026-04-25
---

# Phase 9 Plan 02: Pipeline Integration for DNS Summary

**One-liner:** Aggregator now counts `Flow.L7.Dns` records (VIS-01 gate accurate), `evidenceWriter.convert` emits `L7Ref{Protocol:"dns", DNSMatchName}` (Phase 8 TODO removed), end-to-end integration test drives a synthetic DNS fixture through the full pipeline.

## What Was Built

Two atomic TDD commits closing the integration gap between 09-01 (policy layer DNS codegen) and the on-disk evidence + diagnostic counters:

1. **`Aggregator.l7DNSCount` increment** (`pkg/hubble/aggregator.go::Run`) — symmetric to the HTTP counter shipped in 08-03. Increments on every flow whose `Flow.L7.Dns` is non-nil, regardless of `L7Enabled` (counter is diagnostic). Doc comment updated to say "populated regardless of L7Enabled (Phase 9 wires the increment)."
2. **`evidenceWriter.convert` DNS branch** (`pkg/hubble/evidence_writer.go`) — replaces the explicit `TODO(phase-9)` with `re.L7 = &evidence.L7Ref{Protocol:"dns", DNSMatchName: a.Key.L7.DNSMatchName}`. Unknown L7 protocols still leave `re.L7` nil (defensive).
3. **Aggregator unit tests** — `TestAggregator_L7DNSCount_Increments` (DNS-only flow, mixed HTTP+DNS sequence via factory-built bare flows) and `TestAggregator_L7DNSCount_IndependentOfL7Enabled` (counter contract under `SetL7Enabled(false)`).
4. **Evidence writer unit tests** — `TestEvidenceWriter_ConvertDNSL7` (DNS attribution → DNS L7Ref + JSON round-trip with omitempty preserved) and `TestEvidenceWriter_ConvertUnknownL7Protocol` (defensive nil for unrecognized protocol).
5. **End-to-end integration test** — `TestPipeline_L7DNS_GenerationAndEvidence`. Drives `RunPipelineWithSource` against `testdata/flows/l7_dns.jsonl` (3 DROPPED EGRESS DNS flows, 2 distinct queries) and asserts:
   - On-disk YAML contains `toFQDNs:` with `matchName: api.example.com` + `matchName: www.example.org` (trailing dot stripped) and a `dns:` L7 block.
   - Parsed CNP passes `testdata.AssertHasKubeDNSCompanion` (DNS-02).
   - YAML contains no `matchPattern:` substring (DNS-03).
   - Evidence JSON carries DNS `L7Ref` for both queries.
   - `session summary` log reports `l7_dns_count > 0` (VIS-01 gate truthful).
   - VIS-01 warning does NOT fire (DNS records observed).
6. **DNS fixture** — `testdata/flows/l7_dns.jsonl`. 3 DROPPED EGRESS flows from `production/api-server` → `reserved:world` (8.8.8.8) on UDP/53 carrying DNS queries `api.example.com.`, `www.example.org.`, `api.example.com.` (dup). Field casing matches Hubble's protojson schema (`IP` uppercase, `qtypes` as `[]string`).

## Test Surface

- `pkg/hubble/aggregator_test.go::TestAggregator_L7DNSCount_Increments` — two DNS flows + one HTTP flow → `L7DNSCount==2`, `L7HTTPCount==1`.
- `pkg/hubble/aggregator_test.go::TestAggregator_L7DNSCount_IndependentOfL7Enabled` — DNS counter increments even when `SetL7Enabled(false)`.
- `pkg/hubble/evidence_writer_test.go::TestEvidenceWriter_ConvertDNSL7` — DNS Key.L7 → DNS RuleEvidence.L7 with HTTP fields empty + JSON round-trip preserves `dns_matchname`, omits `http_method`/`http_path`.
- `pkg/hubble/evidence_writer_test.go::TestEvidenceWriter_ConvertUnknownL7Protocol` — unknown protocol leaves `re.L7` nil.
- `pkg/hubble/pipeline_l7_dns_test.go::TestPipeline_L7DNS_GenerationAndEvidence` — full end-to-end driver.

Total tests: 304 across 9 packages, all passing. Phase 7 + 8 byte-stability invariants intact (no regression).

## Decisions Made

- **Counter increment site:** placed at the very top of the flow-receive case in `Aggregator.Run`, BEFORE `keyFromFlow`'s skip check. This mirrors the HTTP counter's existing behavior and ensures VIS-01 surfaces accurate diagnostics even for flows that never make it into a bucket (reserved-identity, empty-namespace, etc.).
- **Defensive nil on unknown L7 protocol:** keeps `re.L7` nil for protocols other than `"http"` or `"dns"`. Future protocols (Kafka, gRPC) would require an explicit branch — fail-closed posture for now.
- **Cross-package companion assertion reuse:** the integration test parses the YAML back into a `*ciliumv2.CiliumNetworkPolicy` via `sigs.k8s.io/yaml` and calls `testdata.AssertHasKubeDNSCompanion` (exposed by 09-01) rather than re-implementing the DNS-02 invariant. Single source of truth across `pkg/policy` internal tests, `pkg/policy_test` external tests, and `pkg/hubble` integration tests.
- **DNS fixture field casing:** the Hubble proto field name for the L3 layer is `IP` (uppercase, per protobuf annotation). Initial fixture used `ip` and protojson's strict mode rejected all 3 lines as malformed. Confirmed via `go doc flow.Flow`. Likewise, `DNS.Qtypes` is `[]string` not `[]int`.

## Deviations from Plan

**[Rule 1 – Bug] Fix `go vet` lock-copy warning in test code**
- **Found during:** Task 2 verification (`go vet ./...`).
- **Issue:** `TestAggregator_L7DNSCount_Increments` initially built two test flows by dereferencing a shared `bothFlow := &flowpb.Flow{...}` — `httpFlow := *bothFlow`. `flowpb.Flow` embeds a `sync.Mutex` (proto generated), so vet flagged the assignment as a lock copy.
- **Fix:** replaced the by-value copy with a `makeBare()` factory that returns a fresh `*flowpb.Flow` per call. Functionally equivalent, vet-clean.
- **Files modified:** `pkg/hubble/aggregator_test.go`.
- **Commit:** rolled into Task 2's commit (`b2be502`) since the fix lived in test scaffolding shared with the integration work.

No other deviations — plan executed as written.

## VIS-01 Gate — Now Truthful

The VIS-01 empty-records warning in `pipeline.go::Finalize` was already wired in Phase 8 to read `agg.L7HTTPCount() + agg.L7DNSCount()`. With Phase 8 the DNS half always returned 0, so the gate effectively only fired for HTTP-disabled-but-observed cases. With this plan's increment site live, the gate now correctly:
- Fires when `--l7` is set, flows are observed, but BOTH HTTP and DNS counters are zero.
- Does NOT fire when only DNS records are observed (covered by integration test's "VIS-01 must not fire" assertion).

## Commits

- `6965192` — `feat(hubble): aggregator DNS counter + evidence DNS branch (DNS-01, VIS-01 gate)`
- `b2be502` — `test(hubble): end-to-end DNS L7 integration test (DNS-01, DNS-02)`

## Verification

- `go test ./pkg/hubble/... -v` — all hubble tests pass (including the new integration test).
- `go test ./...` — 304 passed across 9 packages, no regressions.
- `go vet ./...` — clean.
- `rg -n "TODO\(phase-9\)" pkg/hubble/evidence_writer.go` — zero matches (TODO removed).
- DNS-02 invariant: integration test parses the on-disk YAML and runs `testdata.AssertHasKubeDNSCompanion` — passes.
- DNS-03 invariant: integration test asserts no `matchPattern:` substring in the YAML — passes.

## Self-Check: PASSED

- pkg/hubble/aggregator.go: FOUND (DNS counter increment in Run loop)
- pkg/hubble/aggregator_test.go: FOUND (2 new tests)
- pkg/hubble/evidence_writer.go: FOUND (DNS branch, no TODO)
- pkg/hubble/evidence_writer_test.go: FOUND (2 new tests)
- pkg/hubble/pipeline_l7_dns_test.go: FOUND
- testdata/flows/l7_dns.jsonl: FOUND
- Commit 6965192: FOUND
- Commit b2be502: FOUND
