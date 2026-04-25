# Phase 7: L7 Infrastructure Prep — Context

**Gathered:** 2026-04-25
**Status:** Ready for planning
**Mode:** Auto-generated (`--auto` flag — pure infrastructure phase, decisions at Claude's discretion within boundaries below)

<domain>
## Phase Boundary

Land foundational fixes so the v1.2 L7 phases (8 and 9) can ship correctly. This phase produces NO user-visible behavior change in CNP YAML output: end-of-phase, the same v1.1 inputs produce byte-identical YAML to v1.1.

Concretely, this phase delivers:
1. Fix to `pkg/policy/merge.go::mergePortRules` so the `Rules` field of `PortRule` is preserved across merges (latent bug today, silent data loss the moment Phase 8 lights up).
2. Evidence schema bump from v1 → v2: new optional `l7` sub-field on `RuleEvidence`, reader rejects `schema_version != 2` with explicit instruction to wipe `$XDG_CACHE_HOME/cpg/evidence/`. No back-compat layer (v1.1 shipped 2026-04-24, no caches in production).
3. `RuleKey` extended with optional L7 discriminator (HTTP method+path or DNS matchName) so two rules differing only in L7 do not dedup into the same evidence bucket.
4. `normalizeRule` extended to deterministically sort L7 lists.
5. Pre-flight cluster checks: ConfigMap `kube-system/cilium-config.enable-l7-proxy=true`, presence of `cilium-envoy` DaemonSet (Cilium ≥ 1.16) or `enable-envoy-config` flag (Cilium 1.14–1.15). RBAC denied → warn-and-proceed (NOT abort).
6. `--no-l7-preflight` flag to skip the cluster pre-flight checks (CI restricted-RBAC, air-gapped use).
7. `--l7` flag plumbed through `cmd/generate.go`, `cmd/replay.go`, and `pkg/hubble/PipelineConfig` — flag is parsed, threaded, but flipping it ON does not yet alter generated YAML. Phase 8 lights up the codegen.

Mapped requirements: EVID2-01, EVID2-02, EVID2-03, EVID2-04, VIS-04, VIS-05, VIS-06, L7CLI-01.

</domain>

<decisions>
## Implementation Decisions

### Schema & dedup
- **Evidence schema v1 → v2 with no back-compat reader.** Reader rejects `schema_version != 2` with a clear error message that names `$XDG_CACHE_HOME/cpg/evidence/` and tells the user to wipe it. v1.1 shipped 2026-04-24, no caches in production.
- **`RuleKey` L7 discriminator is optional** (omitempty). L4 rules continue to produce identical keys to v1.1 — only L7 rules carry the discriminator.
- **`mergePortRules` Rules-preservation lands BEFORE schema v2** so the regression test for the latent bug uses the existing v1 schema and clearly demonstrates the fix in isolation. Schema v2 lands second, in a separate plan.
- **`normalizeRule` sort keys:** HTTP entries sorted by `(Method, Path)` lexicographic, DNS entries sorted by `MatchName` lexicographic. Stable order across runs for byte-stable YAML output.

### Pre-flight checks
- **VIS-04 (cilium-config check):** read ConfigMap `kube-system/cilium-config`, look for `enable-l7-proxy: "true"`. If missing or false → warn with remediation (set in ConfigMap, roll cilium-agent DaemonSet). The check uses the existing `pkg/k8s` clientset and kubeconfig.
- **VIS-05 (cilium-envoy presence):** look for DaemonSet `kube-system/cilium-envoy`. If absent, fall back to checking `enable-envoy-config` in `cilium-config`. Cilium 1.14–1.15 (envoy embedded in cilium agent) → check passes silently.
- **RBAC failure mode:** any 403 on the pre-flight calls → warn with the RBAC permission required, then proceed. Pre-flight is advisory, never blocking. Cluster admins running cpg with reduced permissions (CI service accounts) must not be locked out.
- **`--no-l7-preflight` flag** wins over both VIS-04 and VIS-05; useful for offline replay (no cluster to query) and air-gapped environments. VIS-01 (passive empty-records detection) still fires later in Phase 8.
- **Pre-flight only applies to `cpg generate --l7`.** `cpg replay` runs offline; pre-flight does not run there even when `--l7` is set.

### Flag plumbing
- **`--l7` is a Cobra `Bool` flag**, default `false`, on both `cpg generate` and `cpg replay`. Long flag only — no short alias.
- **Plumbed through `PipelineConfig`** (new `L7Enabled bool` field). Aggregator and builder receive it but use it as a no-op in Phase 7. Phase 8 wires the actual codegen branch.
- **Behavior with `--l7` set in Phase 7:** identical to `--l7` unset — byte-stable YAML. The pre-flight checks DO run when `--l7` is set on `cpg generate` even in Phase 7, so VIS-04/05/06 are testable end-to-end immediately.

### Test strategy
- **Regression test for the merge bug** must be checked-in BEFORE the fix (TDD): a test that asserts `Rules` survives a merge round-trip, failing on current `master`, passing after the fix.
- **Schema v2 reader rejection test:** write an evidence file with `schema_version: 1`, attempt to read, assert error message names `$XDG_CACHE_HOME/cpg/evidence/`.
- **Pre-flight tests use `k8s.io/client-go/kubernetes/fake`** (already a transitive dep) — no real cluster needed in unit tests.
- **Flag plumbing test:** spawn `cpg generate --l7` against the existing pkg/hubble integration test fixture (live or replay), assert output matches the same fixture's `--l7=false` output byte-for-byte.

### Out of scope for THIS phase
- HTTP rule generation (Phase 8).
- DNS rule generation (Phase 9).
- VIS-01 passive empty-records detection (Phase 8 — needs the L7 ingestion path running).
- README updates (Phase 9 — bundled with two-step workflow doc).
- kube-dns selector autodetection (deferred to v1.3 per DNS-FUT-02).

### Claude's Discretion
All other implementation choices (file/struct names, package layout for new pre-flight code, exact Cobra flag declaration ordering, test fixture format) are at Claude's discretion. Conform to existing cpg conventions (`pkg/`-based domain split, TDD with table-driven tests, zap logging, errgroup-style goroutines).

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/k8s/` — already has clientset bootstrapping, kubeconfig loading, port-forward (SPDY), cluster CNP listing. Pre-flight reuses the clientset; no new K8s connection logic.
- `pkg/evidence/` — has Writer (atomic), Reader (schema-version-checked), schema.go (schema_version=1 pinned), merge.go (FIFO, session upsert by ID), paths.go (XDG hashing). Schema bump replaces the version constant + extends the struct; the rest of the package stays.
- `pkg/policy/` — has `BuildPolicy`, `MergePolicy`, `PoliciesEquivalent`, `RuleKey`, `RuleAttribution`, `normalizeRule` (private). Modify `merge.go` and `normalizeRule`; extend `attribution.go` for L7 RuleKey.
- `pkg/hubble/PipelineConfig` — extends with `L7Enabled bool`. Aggregator and pipeline already accept config struct, no signature breakage.
- `cmd/commonflags.go` (v1.1) — already factors shared flags between `generate` and `replay`. Add `--l7` and `--no-l7-preflight` here.

### Established Patterns
- TDD throughout (test files land before implementation in commits).
- Table-driven tests with descriptive `name` fields.
- Cobra subcommands in `cmd/`, no business logic at this layer.
- Zap structured logging — `logger.Warn` / `logger.Info` are the patterns; use them for pre-flight warnings.
- `errgroup`-style goroutines with context cancellation in pipeline code.
- File dedup via YAML byte comparison (file-on-disk); semantic dedup via `PoliciesEquivalent` (cross-flush + cluster).

### Integration Points
- `pkg/hubble/pipeline.go::PipelineConfig` — gains `L7Enabled bool` and (probably) `L7PreflightDisabled bool` mirroring `--no-l7-preflight` for the generate path.
- `cmd/commonflags.go` — declares the new flags.
- `cmd/generate.go` — runs pre-flight (when `L7Enabled && !L7PreflightDisabled`) before starting the pipeline.
- `cmd/replay.go` — pre-flight is skipped here regardless of flags (offline by definition).
- `pkg/k8s/preflight.go` (new file) — houses the cilium-config + cilium-envoy checks with the warn-and-proceed semantics.
- `pkg/policy/merge.go::mergePortRules` — the bug fix.
- `pkg/policy/builder.go::normalizeRule` — extends with L7 sort keys.
- `pkg/policy/attribution.go::RuleKey` — extends with optional L7 discriminator.
- `pkg/evidence/schema.go` — `SchemaVersion = 2`; `RuleEvidence` gains `L7 *L7Ref` field with omitempty.

</code_context>

<specifics>
## Specific Ideas

- The merge-bug regression test must literally land in a commit before the fix, mirroring v1.1's TDD-first approach (the v1.1 phase summaries explicitly call out "test files land in failing commits before implementation"). This makes the bug self-documenting in git history.
- The schema v2 commit message should explicitly call out that wiping `$XDG_CACHE_HOME/cpg/evidence/` is required after upgrade, so downstream `git log` greppers find it during incident response.
- Pre-flight warnings should fire EXACTLY ONCE per run — not once per workload. A single warning at startup is the right UX. Use a `sync.Once` if needed to dedup.

</specifics>

<deferred>
## Deferred Ideas

- kube-dns selector autodetection across CNI distributions (EKS / GKE / AKS) — deferred to v1.3 (DNS-FUT-02). v1.2 hardcodes `k8s-app=kube-dns` with a YAML comment.
- DROPPED-vs-REDIRECTED verdict filter expansion under L7 visibility — research flagged this for live-cluster validation in Phase 8, not Phase 7.
- `--include-l7-forwarded` for DNS REFUSED denials surfaced as `Verdict_FORWARDED` — deferred to v1.3 (L7-FUT-01).
- `--min-flows-per-l7-rule N` low-confidence gate — deferred to v1.3 (L7-FUT-02).

</deferred>
