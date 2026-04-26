---
phase: 09-dns-l7-generation
plan: 04
subsystem: docs
tags: [readme, l7, dns, http, cilium, ciliumnetworkpolicy, e2e, byte-stability]

requires:
  - phase: 09-dns-l7-generation
    provides: DNS L7 codegen (extractDNSQuery, ensureKubeDNSCompanion), aggregator DNS counter, evidence_writer DNS branch, cpg explain L7 filters + rendering
  - phase: 08-vis-warning-http-l7
    provides: VIS-01 warning + #l7-prerequisites anchor reservation, HTTP-05 anti-feature invariant, --l7 flag plumbing
provides:
  - README #l7-prerequisites authoritative content (two-step workflow + 3 ways to enable visibility + starter visibility CNP)
  - End-to-end DNS L7 acceptance test (TestReplay_L7DNSGeneration)
  - DNS-04 byte-stability lock (TestReplay_L7DNSDisabled_FallbackByteStable)
  - --l7 + explain L7 filter mentions across Quick start / replay / explain / Limitations
  - "What it generates with --l7" example showing toFQDNs + companion shape
affects: [v1.3 wildcard inference, --include-l7-forwarded, kube-dns autodetection, --l7-collapse-paths]

tech-stack:
  added: []
  patterns:
    - "End-to-end test pattern: cobra cmd + temp output + temp evidence dir + YAML substring + structured evidence assertions"
    - "DNS-04 byte-stability assertion: same fixture, no flag vs --l7=false produces byte-identical CIDR-shape egress"
    - "README YAML-block validation via python yaml.safe_load_all in CI-style smoke check"

key-files:
  created:
    - .planning/phases/09-dns-l7-generation/09-04-SUMMARY.md
  modified:
    - README.md
    - cmd/cpg/replay_test.go

key-decisions:
  - "Test fixture testdata/flows/l7_dns.jsonl was already created by an earlier 09-* plan; reused as-is (3 EGRESS DROPPED DNS flows: api.example.com x2 + www.example.org x1 to reserved:world / 8.8.8.8). No new fixture needed."
  - "DNS-04 byte-stability test compares no-flag vs --l7=false (not vs a separate snapshot) to mirror the HTTP byte-stability pattern from Phase 8."
  - "Starter L7-visibility CNP in README uses match-all HTTP {} and DNS matchPattern \"*\" — the matchPattern wildcard is ONLY for visibility bootstrap, NOT cpg's generated output (DNS-03 invariant intact: cpg never auto-generates matchPattern)."
  - "README anchor exact-once invariant verified: grep -c 'id=\"l7-prerequisites\"' README.md returns 1; pkg/hubble/pipeline.go:203 hint string still resolves."

patterns-established:
  - "v1.2 milestone wrap-up pattern: replace placeholder anchor content with authoritative docs in the final phase"
  - "DNS-04 byte-stability: --l7=false and no-flag must produce identical YAML on a DNS-bearing fixture, locked by an e2e test"

requirements-completed: [VIS-02, VIS-03, DNS-01, DNS-02, DNS-03, DNS-04]

duration: 12min
completed: 2026-04-25
---

# Phase 9 Plan 04: README two-step workflow + DNS L7 e2e tests Summary

**README #l7-prerequisites filled with the two-step workflow + copy-pasteable starter visibility CNP, plus end-to-end DNS L7 generation and DNS-04 byte-stability tests on testdata/flows/l7_dns.jsonl. v1.2 milestone feature-complete.**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-04-25T13:48:00Z
- **Completed:** 2026-04-25T14:00:00Z
- **Tasks:** 3
- **Files modified:** 2 (1 created summary)

## Accomplishments

- Replaced the Phase 8 placeholder #l7-prerequisites section with authoritative v1.2 content: two-step workflow (deploy L4 → enable visibility → re-run --l7), three ways to enable visibility (annotation / bootstrap CNP / cluster-wide enable-l7-proxy), copy-pasteable starter L7-visibility CiliumNetworkPolicy (match-all HTTP {} + DNS matchPattern "*" — for visibility bootstrap only), capture-window guidance, honest v1.2 limitations with deferred-flag traceability (HTTP-05, DNS-03, REFUSED-via-FORWARDED, hardcoded kube-dns selector, no auto-collapse).
- Added "With --l7 (opt-in HTTP + DNS)" subsection under "What it generates" showing the expected CNP shape, including the auto-injected kube-dns companion (DNS-02).
- Updated Quick start / Quick start (offline replay) sections with --l7 examples; updated Explain section with --http-method / --http-path / --dns-pattern usage notes; refreshed Limitations to v1.2-honest.
- Added `TestReplay_L7DNSGeneration` end-to-end acceptance test: replay --l7 against l7_dns.jsonl asserts toFQDNs (sorted matchName), companion kube-dns 53/UDP+TCP rule (DNS-02), no matchPattern (DNS-03), no HTTP header/host (HTTP-05 invariant survives DNS-only fixtures), evidence L7Ref{Protocol:dns, DNSMatchName} for each query.
- Added `TestReplay_L7DNSDisabled_FallbackByteStable`: replay without --l7 and with --l7=false produce byte-identical v1.1-shape CIDR-only egress (DNS-04 lock).
- All 319 tests pass across 9 packages; go vet clean.

## Task Commits

1. **Task 3: e2e DNS L7 + DNS-04 byte-stability tests (TDD)** — `f99e37f` (test)
2. **Task 1: README two-step workflow + starter visibility CNP** — `a7cfd73` (docs)

Task 2 (testdata/flows/l7_dns.jsonl) was already created by an earlier 09-* plan execution and validated in place (3 lines, EGRESS DROPPED DNS flows, valid jsonpb shape) — no separate commit required.

## Files Created/Modified

- `README.md` — #l7-prerequisites authoritative content + --l7 + explain L7 filter mentions across Quick start / replay / explain / Limitations / "What it generates"
- `cmd/cpg/replay_test.go` — TestReplay_L7DNSGeneration + TestReplay_L7DNSDisabled_FallbackByteStable

## Cross-references

- VIS-01 warning hint string in `pkg/hubble/pipeline.go:203` ("see README L7 prerequisites: #l7-prerequisites") now points at real authoritative content. Anchor preserved exactly once: `grep -c 'id="l7-prerequisites"' README.md` returns 1.
- HTTP-05 anti-feature invariant remains enforced even on DNS-only fixtures: TestReplay_L7DNSGeneration asserts no `headerMatches` / `host:` / `hostExact` in the generated YAML.

## Decisions Made

- Reused the pre-existing `testdata/flows/l7_dns.jsonl` fixture rather than recreating it — the file was committed to the tree by a prior 09-* plan execution and matched the contract specified in this plan (3 DNS-bearing EGRESS DROPPED flows with reserved:world destination, 8.8.8.8/32 IP, UDP/53, Flow.L7.Dns.Query).
- DNS-04 byte-stability test compares `noFlag` vs `--l7=false` (mirrors Phase 8 HTTP byte-stability) rather than vs a checked-in snapshot — keeps tests resilient to non-semantic YAML formatting changes.

## Deviations from Plan

None — plan executed exactly as written. Task 2 fixture creation was a no-op because the file already existed and met the spec.

## Issues Encountered

None.

## Next Phase Readiness

v1.2 milestone is feature-complete. All 8 v1.2 requirements claimed by Phase 9 (DNS-01..DNS-04, L7CLI-02, L7CLI-03, VIS-02, VIS-03) are now satisfied with on-disk e2e coverage. v1.3 backlog: DNS matchPattern wildcard inference (DNS-FUT-01), kube-dns selector autodetection (DNS-FUT-02), FQDN-from-IP correlation (DNS-FUT-03), --include-l7-forwarded (L7-FUT-01), --l7-collapse-paths (HTTP-FUT-01), cpg explain regex/glob filters.

## Self-Check: PASSED

- README.md exists and contains `id="l7-prerequisites"` exactly once (verified)
- README.md contains `cpg-l7-visibility-bootstrap` (verified)
- README.md mentions `--l7`, `--http-method`, `--http-path`, `--dns-pattern` (verified)
- All 5 README YAML fenced blocks parse via yaml.safe_load_all (verified)
- testdata/flows/l7_dns.jsonl exists with 3 lines, contains `dns` and `api.example.com` (verified)
- cmd/cpg/replay_test.go contains `TestReplay_L7DNSGeneration` (verified)
- Commit f99e37f exists (test 09-04 e2e tests)
- Commit a7cfd73 exists (docs readme L7 prerequisites)
- `go test ./...` clean: 319 passed across 9 packages
- `go vet ./...` clean

---
*Phase: 09-dns-l7-generation*
*Completed: 2026-04-25*
