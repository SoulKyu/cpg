---
phase: 09-dns-l7-generation
plan: 01
subsystem: policy
tags: [dns, l7, fqdn, kube-dns, cilium, codegen, tdd]

requires:
  - phase: 07-evidence-schema-v2
    provides: L7Discriminator{Protocol,DNSMatchName} on RuleKey, AttributionOptions.L7Enabled gate
  - phase: 08-http-l7-generation
    provides: recordL7 dispatch site in BuildPolicy, peerRules.httpRules pattern reused for dnsOrder
provides:
  - extractDNSQuery (pkg/policy/l7.go): nil-safe DNS query extractor with trailing-dot + whitespace stripping
  - ensureKubeDNSCompanion (pkg/policy/companion_dns.go): idempotent post-process companion-rule injector
  - kubeDNSSelector (pkg/policy/companion_dns.go): hardcoded kube-dns EndpointSelector for v1.2
  - testdata.AssertHasKubeDNSCompanion (pkg/policy/testdata/companion_dns_assert.go): shared DNS-02 invariant helper
  - peerRules.dnsNames + dnsOrder + addDNSName (pkg/policy/builder.go)
  - BuildPolicy now calls ensureKubeDNSCompanion before returning
  - buildEgressRules emits one dedicated FQDN egress rule per bucket with dnsOrder non-empty
affects: [09-02 hubble pipeline DNS branch, 09-03 cpg explain L7 filters/render, 09-04 README + visibility CNP]

tech-stack:
  added: []
  patterns:
    - "Companion-rule auto-injection at BuildPolicy post-process (idempotent, label-source-prefix tolerant)"
    - "DNS rules live in dedicated EgressRule (Cilium API mutual exclusivity with ToEndpoints/ToCIDR/ToEntities)"

key-files:
  created:
    - pkg/policy/companion_dns.go
    - pkg/policy/companion_dns_test.go
    - pkg/policy/testdata/companion_dns_assert.go
  modified:
    - pkg/policy/l7.go
    - pkg/policy/l7_test.go
    - pkg/policy/builder.go
    - pkg/policy/builder_l7_test.go

key-decisions:
  - "Companion's rules.dns lists each observed FQDN as literal matchName (not matchPattern:'*'): preserves DNS-03 globally"
  - "ToFQDNs lives in its OWN EgressRule (mutually exclusive with ToEndpoints/ToCIDR/ToEntities per Cilium API)"
  - "DNS branch only fires for direction=='egress' — ingress DNS records are ignored"
  - "DNS-04 byte-stability achieved by gating recordL7 DNS branch on opts.L7Enabled at the top of recordL7"
  - "ensureKubeDNSCompanion is label-source-prefix tolerant ('any:', 'k8s:') so YAML roundtrips do not break idempotency"
  - "AssertHasKubeDNSCompanion exposed via testdata package so external (policy_test) test files can call it"

patterns-established:
  - "Single global post-process call (ensureKubeDNSCompanion) is the simplest enforcement point for cross-cutting CNP invariants"
  - "Per-bucket dnsOrder + sorted emission keeps YAML byte-stable across runs without normalizeRule fix-up"

metrics:
  duration: ~25 minutes
  tasks-completed: 2/2
  completed: 2026-04-25
---

# Phase 9 Plan 01: DNS L7 Generation + kube-dns Companion Injector Summary

**One-liner:** DNS query extraction wired into BuildPolicy with mandatory kube-dns companion rule auto-injection (literal matchName only, no glob, idempotent post-process).

## What Was Built

Two atomic TDD commits delivering the DNS branch of L7 codegen at the policy layer (DNS-01, DNS-02, DNS-03 closed; DNS-04 byte-stability validated):

1. **`extractDNSQuery(*flowpb.Flow) (string, bool)`** — nil-safe extractor. Strips canonical trailing dot from `Flow.L7.Dns.Query` and surrounding whitespace; returns `("", false)` on empty/malformed/missing-record. Direct drop-in for Cilium `FQDNSelector.MatchName`.
2. **`ensureKubeDNSCompanion(*ciliumv2.CiliumNetworkPolicy)`** — idempotent post-process injector enforcing DNS-02. Walks every `ToFQDNs` slice, collects unique sorted matchNames, and appends a single companion egress rule with selector `k8s-app=kube-dns + io.kubernetes.pod.namespace=kube-system` opening 53/UDP+TCP with literal `rules.dns` matchName entries (never matchPattern). Detects existing companion via label-source-prefix-tolerant comparison and skips appending.
3. **`peerRules.dnsNames + dnsOrder`** + **`addDNSName`** — per-bucket DNS observation aggregation with first-observation order preservation and dedup.
4. **`recordL7` extended** — dispatches HTTP OR DNS based on which `Flow.L7` sub-record is non-nil. DNS branch only fires for `direction == "egress"` and emits attribution with `L7Discriminator{Protocol:"dns", DNSMatchName: <stripped>}`.
5. **`buildEgressRules` extended** — appends one dedicated FQDN `EgressRule` per bucket whose `dnsOrder` is non-empty. ToFQDNs lives alone (no ToEndpoints/ToCIDR/ToEntities) per Cilium API mutual exclusivity. Names sorted before emission for byte-stable YAML.
6. **`BuildPolicy` post-processed** — calls `ensureKubeDNSCompanion(cnp)` once before returning. Single global enforcement point.
7. **`testdata.AssertHasKubeDNSCompanion`** — shared invariant helper used by both internal (`pkg/policy`) and external (`pkg/policy_test`) test files. Tolerant of `any:` / `k8s:` label-source prefixes.

## Test Surface

- `pkg/policy/l7_test.go::TestExtractDNSQuery` — table-driven: nil flow, nil L7, nil DNS record, trailing dot, no dot, empty, whitespace-only, leading/trailing whitespace.
- `pkg/policy/companion_dns_test.go` — empty CNP no-op, no-FQDN no-op, single-FQDN injection + assertHasKubeDNSCompanion, multi-FQDN sorted listing, idempotency, DNS-03 (no MatchPattern emitted anywhere).
- `pkg/policy/builder_l7_test.go` — DNS single-query end-to-end (toFQDNs + paired rules.dns + companion), multi-query aggregation (single FQDN rule + exactly one companion), DNS-04 byte-stability (L7Enabled=false on DNS-bearing input is byte-identical to L7-stripped input), empty query drop, RuleKey discriminator format `dns:<matchName>`.

Total tests: 124 → 129 in pkg/policy (+5 DNS-bearing builder cases). All 299 tests across 9 packages pass.

## Decisions Made

- **Companion uses literal matchName, not matchPattern:"*"**. v1.2 honors DNS-03 globally; the companion's `rules.dns` lists each observed FQDN explicitly. Future glob support (DNS-FUT-01) lands in v1.3.
- **ToFQDNs in dedicated EgressRule**. Cilium API forbids ToFQDNs in the same EgressRule as ToEndpoints/ToCIDR/ToEntities. Splitting keeps the L4 egress rule (e.g., to `10.96.0.10/32`) and the FQDN egress rule independent.
- **DNS branch egress-only**. Ingress DNS records are ignored — they cannot produce policy.
- **`ensureKubeDNSCompanion` single global post-process**. Easier to reason about than per-bucket inline injection; idempotency check makes it safe to call multiple times.
- **Label-source-prefix tolerance**. Comparing `k8s-app`, `any:k8s-app`, and `k8s:k8s-app` as equivalent keeps idempotency intact across YAML roundtrips.

## Deviations from Plan

None — plan executed exactly as written. The plan suggested keeping the `assertHasKubeDNSCompanion` helper internal to `pkg/policy`; I exposed an exported variant `testdata.AssertHasKubeDNSCompanion` because `builder_l7_test.go` is in `package policy_test` (external) and cannot reach internal helpers. Both forms exist; the external form is the one downstream plans should reference.

## DNS-04 Byte-Stability Verification

`TestBuildPolicy_L7Disabled_DNSFlow_NoFQDN_NoCompanion` locks the invariant: a flow with `Flow.L7.Dns.Query="api.example.com."` under `L7Enabled=false` produces output byte-identical to the same flow with `L7=nil`. Verified via `policy.PoliciesEquivalent`.

## Commits

- `6d72ac9`: feat(policy): extractDNSQuery + kube-dns companion injector (DNS-01, DNS-02)
- `80a9b86`: feat(policy): wire DNS L7 codegen into BuildPolicy with companion injector (DNS-01, DNS-02, DNS-03)

## Verification

- `go test ./pkg/policy/... -v` — 129 passed, 0 failed
- `go vet ./pkg/policy/...` — clean
- `go test ./...` — 299 passed across 9 packages (Phase 7 + 8 byte-stability invariants intact)
- `rg "MatchPattern" pkg/policy/companion_dns.go pkg/policy/builder.go pkg/policy/l7.go` — only a comment reference; DNS-03 honored

## Self-Check: PASSED

- pkg/policy/companion_dns.go: FOUND
- pkg/policy/companion_dns_test.go: FOUND
- pkg/policy/testdata/companion_dns_assert.go: FOUND
- pkg/policy/l7.go: FOUND (extractDNSQuery added)
- pkg/policy/builder.go: FOUND (peerRules extended, recordL7 dispatch, buildEgressRules FQDN emission, BuildPolicy companion call)
- pkg/policy/builder_l7_test.go: FOUND (DNS test cases added)
- Commit 6d72ac9: FOUND
- Commit 80a9b86: FOUND
