---
phase: 09-dns-l7-generation
milestone: v1.2
status: complete
completed: 2026-04-25
plans: [09-01, 09-02, 09-03, 09-04]
requirements-completed: [DNS-01, DNS-02, DNS-03, DNS-04, L7CLI-02, L7CLI-03, VIS-02, VIS-03]
---

# Phase 9: DNS L7 Generation + explain L7 + Docs — Phase Summary

**Final phase of v1.2. DNS L7 codegen lit up (literal toFQDNs.matchName + mandatory kube-dns companion), `cpg explain` gained three exact-match L7 filter flags + L7 attribution rendering across text/JSON/YAML, README ships the two-step workflow + a copy-pasteable starter visibility CNP, and an end-to-end replay test locks DNS-01..DNS-04 on disk. v1.2 milestone feature-complete.**

## Plans

| Plan | Title | Closes |
|------|-------|--------|
| 09-01 | DNS L7 extraction + kube-dns companion injector + builder integration | DNS-01, DNS-02, DNS-03 (codegen) |
| 09-02 | Aggregator DNS counter + evidence_writer DNS branch + pipeline integration test | DNS-01..DNS-03 (telemetry + evidence) |
| 09-03 | `cpg explain` L7 filters (--http-method/--http-path/--dns-pattern) + L7 rendering | L7CLI-02, L7CLI-03 |
| 09-04 | README two-step workflow + starter visibility CNP + e2e DNS replay tests | VIS-02, VIS-03, DNS-04 (e2e lock) |

## Requirements Closed

- **DNS-01** — `--l7` + `Flow.L7.Dns` ⇒ egress with `toFQDNs.matchName` + matching `toPorts.rules.dns.matchName`. Trailing-dot stripped. Lexicographically sorted (EVID2-04). Closed via `extractDNSQuery` + `BuildPolicy` integration + e2e test.
- **DNS-02** — Every CNP with `toFQDNs` carries a companion egress rule allowing UDP+TCP/53 to `k8s-app=kube-dns` in `kube-system`. Enforced via `ensureKubeDNSCompanion(cnp)` post-process in `BuildPolicy`. Idempotent (no duplicate companion on repeated calls). Asserted in unit tests + e2e test.
- **DNS-03** — No `matchPattern` glob auto-generated in v1.2. Asserted via negative substring check across all DNS-related tests. Wildcard inference deferred to v1.3 (DNS-FUT-01).
- **DNS-04** — When `--l7=false` or absent, DNS-bearing fixtures produce v1.1-shape CIDR-only egress, byte-identical to no-flag run. Locked by `TestReplay_L7DNSDisabled_FallbackByteStable`.
- **L7CLI-02** — `cpg explain` accepts `--http-method`, `--http-path`, `--dns-pattern` exact-match (literal) filters. AND-combined. L4-only rules filtered out when any L7 filter is set.
- **L7CLI-03** — `cpg explain` renders L7 attribution per rule when present in evidence: text format prints a single indented line per rule (`L7: HTTP GET /api/v1/users` or `L7: DNS api.example.com`); JSON / YAML formats include an `l7` sub-object with `protocol` / `http_method` / `http_path` / `dns_matchname` (omitempty).
- **VIS-02** — README documents the two-step workflow (deploy L4 → enable L7 visibility via 3 options → re-run with `--l7`).
- **VIS-03** — Starter L7-visibility CiliumNetworkPolicy snippet ships in README, copy-pasteable, valid Cilium YAML (verified by `yaml.safe_load_all` smoke check).

## v1.2 Milestone Status: feature-complete

All 22 v1.2 requirements satisfied across phases 7, 8, 9:

| Bucket | Reqs | Phase | Status |
|--------|------|-------|--------|
| Visibility & Workflow | VIS-01..06 | 7+8+9 | complete |
| HTTP L7 Generation | HTTP-01..05 | 8 | complete |
| DNS L7 Generation | DNS-01..04 | 9 | complete |
| Evidence Schema (v2) | EVID2-01..04 | 7 | complete |
| CLI Surface | L7CLI-01..03 | 7+9 | complete |

## Carried forward to v1.3

Per CONTEXT.md deferred list and accumulated requirements backlog:

- **DNS-FUT-01**: DNS `matchPattern` glob auto-generation (`--l7-fqdn-wildcard-depth`).
- **DNS-FUT-02**: kube-dns selector autodetection across CNI distributions (EKS / GKE / AKS / vanilla).
- **DNS-FUT-03**: ToFQDNs from IP→name correlation when DNS records are missed.
- **HTTP-FUT-01**: Auto-collapse of similar paths into a regex (`--l7-collapse-paths`).
- **L7-FUT-01**: `--include-l7-forwarded` for DNS REFUSED denials surfaced as `Verdict_FORWARDED`.
- **L7-FUT-02**: `--min-flows-per-l7-rule N` low-confidence gate.
- **L7-FUT-03**: kube-dns selector autodetection at runtime via `kubectl get pods`.
- **cpg explain regex/glob L7 filters** (deferred from L7CLI-02 — v1.2 ships literal exact match only).

## Test Coverage at Phase Close

- 319 tests across 9 packages pass.
- `go vet ./...` clean.
- New e2e tests in this phase:
  - `TestReplay_L7DNSGeneration` (cmd/cpg) — end-to-end DNS L7 acceptance.
  - `TestReplay_L7DNSDisabled_FallbackByteStable` (cmd/cpg) — DNS-04 byte-stability lock.
- HTTP-05 anti-feature invariant continues to hold across DNS-only fixtures (asserted in DNS e2e test).

## Cross-references

- VIS-01 warning hint string (`pkg/hubble/pipeline.go:203`) now points at authoritative README content.
- README anchor `<a id="l7-prerequisites"></a>` preserved exactly once.

---
*Milestone: v1.2 L7 Policies (HTTP + DNS)*
*Phase: 09-dns-l7-generation*
*Completed: 2026-04-25*
