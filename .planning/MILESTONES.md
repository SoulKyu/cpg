# Milestones — CPG (Cilium Policy Generator)

Historical record of shipped milestones. Each entry links to its archived roadmap and requirements.

---

## v1.3 — Cluster Health Surfacing ✅

**Shipped:** 2026-04-26
**Phases:** 10 → 13 (4 phases, 8 plans)
**Tests:** 418 across 10 packages (up from 319 at v1.2)
**Archives:** [roadmap](milestones/v1.3-ROADMAP.md) · [requirements](milestones/v1.3-REQUIREMENTS.md) · [audit](milestones/v1.3-MILESTONE-AUDIT.md)

**Delivered:** cpg now distinguishes policy drops from infrastructure-level Hubble drops. Only true policy denials produce a CNP; infra/transient drops (CT_MAP_INSERTION_FAILED, BPF errors, etc.) are surfaced separately via `cluster-health.json` and a session summary block. New `--ignore-drop-reason` flag (parity with `--ignore-protocol`) and opt-in `--fail-on-infra-drops` exit code (1) for CI integration. Default exit behavior unchanged. Closes the v1.2 class-of-bug where `cpg-mmtro-adserver` CNPs were generated for conntrack-map-full drops.

**Highlights:**
- New `pkg/dropclass/` package: O(1) classifier (11.62 ns/op) for all 76 Cilium v1.19.1 `DropReason` values across 4 buckets (Policy / Infra / Transient / Noise) + Unknown fallback. ~94% of values are non-policy.
- Pure-policy reasons (only generate CNPs for these): `POLICY_DENIED`, `POLICY_DENY`, `AUTH_REQUIRED` (with `needs_review` annotation), `DENIED_BY_LB_SRC_RANGE_CHECK`.
- Unknown DropReason values default to `Unknown` (never `Policy`) + dedup WARN once per unique int32 via `sync.Map` — safe for future Cilium proto bumps.
- `cluster-health.json` (schema v1) at `$XDG_CACHE_HOME/cpg/evidence/<hash>/`: counters by reason × node × workload + Cilium docs remediation URL per reason. Atomic write (CreateTemp + Rename).
- Filter precedence in aggregator: `--ignore-drop-reason` → `--ignore-protocol` → classification gate → `keyFromFlow`. Infra/Transient drops STILL increment `flowsSeen` (observed traffic remains accurate; only CNP generation is gated).
- Session summary block to stdout (NOT logger) listing infra drops by severity, top-3 nodes/workloads, and the absolute path to `cluster-health.json`. Empty when zero infra drops.
- `--fail-on-infra-drops` → exit 1 (NOT 2 — avoids Terraform GitOps collision). Default cpg behavior unchanged for backward compat.
- README documents both new flags + dedicated `## Exit codes` section + CI cron pattern.
- 8 plans, all TDD-first commits (failing test → impl), auditable in `git log`. ClassifierVersion semver constant (`1.0.0-cilium1.19.1`) embedded in cluster-health.json for cross-release traceability.

---

## v1.2 — L7 Policies (HTTP + DNS) ✅

**Shipped:** 2026-04-25
**Phases:** 7 → 9 (3 phases, 12 plans)
**Tests:** 319 across 9 packages (up from 180 at v1.1)
**Archives:** [roadmap](milestones/v1.2-ROADMAP.md) · [requirements](milestones/v1.2-REQUIREMENTS.md) · [audit](v1.2-MILESTONE-AUDIT.md)

**Delivered:** cpg generates L7 CiliumNetworkPolicies (HTTP method+path, DNS FQDN matchName) from observed Hubble L7 flows when `--l7` is passed. The two-step workflow (deploy L4 → enable L7 visibility → re-run with `--l7`) is documented end-to-end with pre-flight cluster checks, single-warning empty-records detection, starter visibility CNP, and L7-aware `cpg explain` rendering. Default behavior without `--l7` is byte-identical to v1.1.

**Highlights:**

- HTTP rule generation: `regexp.QuoteMeta` + `^…$` anchored paths, uppercase method normalization, no `headerMatches`/`host`/`hostExact` (anti-feature: secret-leak risk).
- DNS rule generation: `toFQDNs.matchName` (literal, trailing-dot stripped) + `toPorts.rules.dns.matchName` paired; mandatory companion DNS-53 rule auto-injected (idempotent post-process); no `matchPattern` glob in v1.2.
- Pre-flight checks: ConfigMap `kube-system/cilium-config.enable-l7-proxy=true` + `cilium-envoy` DaemonSet (Cilium ≥1.16) or `enable-envoy-config` (1.14–1.15). RBAC denied → warn-and-proceed. `--no-l7-preflight` flag for restricted-RBAC / air-gapped use.
- Evidence schema v1 → v2 (no back-compat layer; reader rejects ≠2 with `$XDG_CACHE_HOME/cpg/evidence/` instruction).
- `cpg explain --http-method`/`--http-path`/`--dns-pattern` (exact-match) + L7 rendering across text/JSON/YAML.
- Latent `mergePortRules` Rules-field-drop bug fixed (would have been silent L7 data loss in production).
- 12 plans, all TDD-first commits (failing test → impl), auditable in `git log`.

---

## v1.1 — Offline Replay & Policy Analysis ✅

**Shipped:** 2026-04-24
**Phases:** 4 → 6 (3 phases, 3 plans)
**Archives:** [roadmap](milestones/v1.1-ROADMAP.md) · [requirements](milestones/v1.1-REQUIREMENTS.md)

**Delivered:** Offline iteration workflow (`cpg replay`), per-rule flow evidence persisted to `$XDG_CACHE_HOME/cpg/evidence`, `cpg explain` with filters and multi-format output, `--dry-run` with unified YAML diff on both `generate` and `replay`.

**Highlights:**

- `FlowSource` interface promoted to `pkg/flowsource`; jsonpb FileSource with gzip + stdin.
- Evidence JSON schema v1 with FIFO caps; atomic writer; schema-version-aware reader.
- `BuildPolicy` returns `[]RuleAttribution` threaded through the pipeline via `PolicyEvent`.
- Channel fan-out: single `policies` channel tees into `policyCh` + `evidenceCh`; writers independent.
- `cpg explain <NS/workload | policy.yaml>` with `--ingress/--egress/--port/--peer/--peer-cidr/--since`, text/JSON/YAML renderers.
- 180 tests pass across 9 packages.

---

## v1.0 — MVP (Core Policy Generator) ✅

**Shipped:** 2026-03-08 (archived retroactively on 2026-04-24)
**Phases:** 1 → 3 (3 phases, 7 plans)
**Archives:** [roadmap](milestones/v1.0-ROADMAP.md) · [requirements](milestones/v1.0-REQUIREMENTS.md)

**Delivered:** Go CLI (`cpg generate`) that connects to Hubble Relay via gRPC, observes dropped flows in real-time, and produces ready-to-apply CiliumNetworkPolicy YAML with smart label selection, CIDR rules for external traffic, file + cluster deduplication, and auto port-forward.

**Highlights:**

- Correct ingress/egress CNP generation with exact port + protocol and `app.kubernetes.io/*` label selectors.
- Live Hubble streaming pipeline with namespace filtering and LostEvents warnings.
- CIDR-based rules for world identity (external traffic).
- Three-layer dedup: file-on-disk, cross-flush in-session, and live cluster via client-go.
- Auto port-forward to hubble-relay via SPDY.
- Domain-driven package structure (`pkg/{labels,policy,output,hubble,k8s,dedup}`) — stable through v1.1.

---

## Notes on tagging

The repository uses release-please for SemVer tagging of product releases (`v1.0.0`, `v1.1.0`, … `v1.6.0`). GSD milestone versions (`v1.0`, `v1.1`) are a **planning concept**, not a git tag — they describe roadmap milestones, not binary releases. No `v1.0` / `v1.1` git tags are created by `/gsd:complete-milestone` to avoid collision with release-please.
