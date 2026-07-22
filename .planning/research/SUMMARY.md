# Project Research Summary

**Project:** CPG — Cilium Policy Generator
**Domain:** Milestone-scoped addition to an existing Go CLI + readonly MCP stdio server (Kubernetes/Cilium network-policy generation) — audit-mode onboarding (ingestion, bootstrap generation, a managed mutation window), Cilium version-compatibility detection, and cpg-dedicated LLM agent tooling (repo-local skills/subagent)
**Researched:** 2026-07-22
**Confidence:** HIGH

## Executive Summary

v1.6 closes cpg's one remaining gap in its value chain: everything today assumes default-deny already exists and is already breaking things (it reacts to `Verdict_DROPPED`); the missing chapter is *onboarding* a namespace from "no policies" to "enforced default-deny" with near-zero real drops, using Cilium's own official but currently-manual audit-mode workflow. The headline fact from STACK.md, worth stating plainly because it changes how risky this milestone actually is: **v1.6 needs zero new `go.mod` `require` lines.** Every capability (`--include-audit` ingestion, bootstrap CNP generation, `pods/exec` to flip per-endpoint audit mode, `CiliumEndpoint` watch, Cilium version detection) is reachable through subpackages of modules cpg already depends on directly — `github.com/cilium/cilium@v1.19.4`, `k8s.io/client-go`/`k8s.io/api`/`k8s.io/apimachinery@v0.35.4`. No dependency-upgrade or new-vendor risk gates any phase; the entire "stack" question is wiring already-vendored code, verified by direct source inspection (not docs alone) against the exact pinned versions in cpg's `go.mod`. The five cpg-dedicated skills/agent tooling items are pure Markdown + YAML frontmatter, orthogonal to Go entirely.

All four research files converge on the same shape for the safe path: widen the verdict filter first (`AUD-01`, mechanically identical to three prior threaded features), generate the bootstrap default-deny CNP + runbook second (`AUD-02`, readonly, reusing existing CNP-construction conventions in a new package rather than bending the flow-driven `pkg/policy/builder.go`), and treat the actual audit-window mutation (`AUD-03`) as the milestone's one genuinely hard, genuinely undecided problem. That problem has two layers, both flagged as blocking, both requiring an explicit recorded decision rather than an implicit one: (1) **which surface** the mutation ships on — an MCP session property gated by a launch flag (Variant A, richer LLM-driven UX, real SEC-01 engineering cost) vs. a CLI-only command with the MCP server staying byte-for-byte v1.5 (Variant B, zero SEC-01 cost, but a colder hand-off to a human); and (2), if Variant A is chosen, **how** SEC-01's structural readonly proof can honestly claim "two modes" when static SSA/RTA analysis has no concept of a runtime flag and the SPDY `pods/exec` mechanism is structurally invisible to the existing verb-name matcher (proven, not hypothesized — cpg's own existing port-forward code uses the identical call shape today and is already invisible to it, harmlessly, since it isn't a mutation). Both decisions must land before AUD-03/AUD-04 are scoped into file-level work.

The chief risk this research surfaced is not architectural, it's factual: the ideation draft's own version-floor table (§3.E) contains at least two wrong numbers that PITFALLS.md corrected against actual merged PRs and release-tag dates — `cilium-dbg`'s rename is Cilium 1.15, not 1.14, and the `enableDefaultDeny` CNP knob is Cilium 1.16, not 1.15 — plus a live, already-shipped README bug (the `proxy-visibility` annotation section claims support through Cilium 1.19 when the mechanism was actually removed from the agent's runtime code at 1.17). These corrected numbers must be carried forward into COMPAT-01 verbatim, because getting the `enableDefaultDeny` floor wrong in the bootstrap generator produces the worst possible failure mode for a safety artifact: Kubernetes' structural-schema field-pruning silently drops the unrecognized field, `kubectl apply` succeeds, `kubectl get` shows the policy present, and the namespace is not actually in default-deny — with no error, anywhere. A second, independent correctness fact compounds this: `enableDefaultDeny` alone does not enforce even on a version where the field exists — Cilium requires an accompanying empty `ingress: []`/`egress: []` rule stanza (open upstream CFP, cilium/cilium#35558) or the policy silently no-ops. Both facts are must-carry, testable acceptance criteria for AUD-02, not implementation details to rediscover.

## Key Findings

### Recommended Stack

Full detail: `.planning/research/STACK.md`

Every v1.6 capability is a subpackage of an already-direct dependency — no `go get`, `go mod tidy` only promotes indirect entries already present (e.g. `github.com/gorilla/websocket`, pulled in transitively by `client-go`'s own websocket transport). This was verified by reading the actual vendored module source under `$(go env GOMODCACHE)`, not by trusting documentation.

**Core technologies:**
- `k8s.io/client-go/tools/remotecommand` (`NewSPDYExecutor`) — reach `cilium-dbg` inside agent pods — the exact same `spdy.RoundTripperFor` call `pkg/k8s/portforward.go` already makes for port-forward; one line different (`SubResource("exec")` vs `"portforward"`)
- `github.com/cilium/cilium/pkg/k8s/client/informers/externalversions/cilium.io/v2` — generated, namespace-filterable `CiliumEndpointInformer` built on `cache.NewSharedIndexInformer` — zero hand-rolled watch/resync/backoff logic needed for "new endpoint appeared" detection
- `github.com/cilium/cilium/api/v1/observer` (`ServerStatus`/`GetNodes`) — Cilium/Hubble version over the **already-open** observer gRPC connection cpg dials every invocation — zero new RBAC, survives digest-pinned images
- `k8s.io/apimachinery/pkg/util/version` (`ParseGeneric`/`AtLeast`) — the idiomatic K8s-ecosystem version comparator, tolerant of `-cee.1`/`-eks`/`-rc1` suffixes real Cilium images carry (rejected alternatives: `blang/semver` is only an indirect dep pulled in for an unrelated Cilium kernel-version helper; `x/mod/semver` enforces strict SemVer with no suffix tolerance)
- `pkg/k8s/apis/cilium.io/v2.DefaultDenyConfig` (already-vendored type) — the `enableDefaultDeny` CNP field, reusing the same `ciliumv2.CiliumNetworkPolicy` construction shape `pkg/policy/builder.go` already uses
- **Zero dependencies:** SKL-01..05 skills and the optional `cpg-operator` subagent are plain `.claude/skills/*/SKILL.md` and `.claude/agents/*.md` — Markdown + YAML frontmatter, natively parsed by the Claude Code binary

One explicit product-surface decision STACK.md defers rather than resolves: SPDY-only exec (matches `portforward.go` 1:1, smaller diff, recommended MVP) vs. `NewFallbackExecutor(websocket, spdy, shouldFallback)` hardening (mirrors current `kubectl exec`, ~10-15 line follow-up, not blocking).

### Expected Features

Full detail: `.planning/research/FEATURES.md`

**Must have (table stakes):**
- Ingest `Verdict_AUDIT` through the same pipeline as `DROPPED` — this *is* Cilium's own documented onboarding path; a generator blind to it can't participate at all
- A VIS-01-style single warning when `--include-audit` yields zero AUDIT signal — direct existing precedent, don't invent new UX
- A default-deny CNP that **actually** enforces default-deny — the empty-rule-stanza correctness requirement above
- A written runbook modeled on Cilium's own upstream guide phase order — upstream explicitly offers no automation, that's the gap cpg fills
- Warn-and-proceed on missing/incompatible version, never hard-abort — direct precedent already shipped (v1.2 pre-flight)
- A declared, single-document compatibility floor — Cilium's own published compat table is the ecosystem norm

**Should have (differentiators):**
- Automated end-to-end onboarding (bootstrap → observe → generate → enforce) — upstream itself says it offers "shell command templates but no automation tooling recommendations"; closing that gap is cpg's clearest differentiator this milestone
- A cpg-managed, lifecycle-bound audit window (flip → watch → auto-revert) — a *stronger* safety guarantee than every comparable found: Cilium's own flip is manual and reset-on-restart, Calico's staged policies need a manual `kind:` edit to promote with no auto-expiry at all; closer to Teleport-style JIT access than anything native to the network-policy space
- Cilium version + feature-gating surfaced to the LLM directly (not just a README table)
- Purpose-built, lightweight repo-local skills vs. a general agent framework (contrast: kagent, a full K8s-controller-plus-CRDs system — confirms the repo-local-only constraint is proportionate, not under-ambitious)

**Defer / explicitly reject:**
- Daemon-wide `policy-audit-mode` convenience flag — confirmed "not recommended for production" by Cilium's own docs; blinds cluster-wide enforcement, not just the onboarding namespace
- Free-floating `enable_audit`/`disable_audit` MCP tools with no lifecycle binding — a standing privilege dressed as convenience, exactly what JIT/break-glass conventions exist to avoid
- Generic exec-arbitrary-command pass-through tool — this is literally what Azure's `mcp-kubernetes` does and it defeats SEC-01's entire structural-readonly value proposition
- `apply_policy`/CNP-create-or-delete MCP tool — stays out of scope, carried forward unchanged from v1.5

MVP framing (FEATURES.md): AUD-01, AUD-02, COMPAT-01, and SKL-01/03/04/05 are ship-first, low-risk, no blocking open decisions. AUD-03, AUD-04, SKL-02, and COMPAT-02 are valuable but sized/sequenced by the AUD-03 surface decision.

### Architecture Approach

Full detail: `.planning/research/ARCHITECTURE.md`

Purely additive against the verified v1.5 baseline (`runMCPServer` composition root, `pkg/session.Manager`'s SESS-05 bounded-cleanup fan-out, SEC-01's SSA/RTA reachability audit). Verdict-widening threads through the exact plumbing shape already used for `L7Enabled`/`IgnoreProtocols` (no new pattern to invent). Bootstrap generation is a new package, not an extension of the flow-driven policy builder. The audit window is genuinely new orchestration built from already-vendored primitives, with an explicit, unresolved surface-variant fork.

**Major components:**
1. `pkg/flowsource`/`pkg/hubble` (modified) — `FlowSource` interface gains a 4th parameter; 5 verdict-filter sites widen to `{DROPPED, AUDIT}`; classifier/dedup/evidence/policy-builder layers beneath are verified unchanged (AUDIT flows carry the same drop reason)
2. `pkg/bootstrap` (new) — static, flow-independent default-deny CNP + runbook text generator; reuses `pkg/policy`'s CNP-construction conventions only, never its flow-driven `BuildPolicy` function itself; recommended to return content directly rather than write a new file (zero new SEC-01 allowlist entry)
3. `pkg/k8s/version.go` + `pkg/k8s/exec.go` (new) — Cilium version detection (v1.2 pre-flight pattern) and the `pods/exec` primitive (SPDY sibling of `portforward.go`, but genuinely new node-scoped pod-selection logic, not a copy of `findRelayPod`'s label-selector lookup)
4. `pkg/auditwindow` (new) — flip/track/watch/revert orchestration, consuming the `pkg/k8s` primitives; must fold into the *same* SESS-05 fan-out `Shutdown()`/`Stop()` already perform, not a second, independent goroutine/timer

### Critical Pitfalls

Full detail: `.planning/research/PITFALLS.md`

1. **The two-mode SEC-01 proof cannot be built the way the single-mode proof was.** Static SSA/RTA reachability is a build-time property; a `if flag { registerTool() }` guard does not remove the guarded function from the callgraph. Fix requires an explicit, recorded mechanism decision (build-tag split vs. reviewed allowlist) before any audit-window mutation code is written — see Cross-File Tension 4.
2. **SPDY `pods/exec` is invisible to the existing K8s-write-verb detector — proven, not hypothetical.** The call shape (`RESTClient().Post()...SubResource("exec")`, then `remotecommand.NewSPDYExecutor`) matches none of `k8sWriteVerbs`' method names; cpg's own existing port-forward code already uses this exact shape and is already invisible to the audit today, harmlessly. The audit-window feature inherits the same blind spot, but now hiding a genuine privileged mutation.
3. **Daemon-wide `policy-audit-mode` is a cluster-wide enforcement kill switch dressed up as a debugging flag.** It suspends enforcement of *every* existing policy cluster-wide (not just the onboarding namespace) and requires an agent restart to toggle either direction. It is also the more heavily blogged-about mechanism, making it the "obvious" wrong answer an LLM is more likely to suggest than the correct scoped one — every generated artifact (runbook, skill, README) must actively warn against it, never merely omit it.
4. **New and regenerated endpoints always start outside the audit window — the watcher narrows the race, it cannot close it.** `PolicyAuditMode` is non-persistent and per-node; every new pod's endpoint starts at daemon-default (audit-off, correctly) and is immediately enforced under the bootstrap default-deny CNP before cpg's watcher can observe and flip it. Compounded by endpoint-ID reuse (per-node, non-globally-unique small integers) — "revert-only-ours" bookkeeping must key on `CiliumEndpoint` UID/resourceVersion, never the raw integer ID.
5. **Revert must ride the exact existing SESS-05 fan-out, not a parallel construct.** A second un-fenced watcher goroutine, an independent `time.AfterFunc` TTL timer racing an explicit stop, or a revert sweep that reports "reverted" without per-endpoint success/failure tracking each independently break the safety argument the whole audit-window feature rests on ("the LLM structurally cannot leave a cluster in audit"). None of these fail loudly — they compile, pass happy-path tests, and surface later as an intermittent "endpoint stuck in audit" bug report.
6. **The milestone's own starting version-floor table has at least three wrong numbers.** See Cross-File Tension 2 — carry the PR-verified corrections forward, not the draft's original figures.

## Cross-File Tensions & Reconciliation

Four places where the research files' recommendations don't trivially line up, or where one file corrected another's working assumption, surfaced explicitly per this synthesis's brief rather than papered over. All four need an explicit decision recorded during requirements definition or discuss-phase — none should be resolved by silent default during implementation.

### Tension 1: Version-detection source of truth (open — STACK.md vs. ARCHITECTURE.md disagree on which signal is primary)

STACK.md recommends Hubble Relay's `ServerStatus`/`GetNodes` gRPC (`ObserverClient`, over the connection cpg already holds open every invocation) as the **primary** signal, with `ds/cilium` DaemonSet image-tag parsing kept as an optional secondary cross-check only. Its case: `ServerStatusResponse.Version`/`Node.Version` are real, verified proto fields (confirmed by direct grep of the vendored module, not inference), the mechanism needs zero new RBAC, and it survives digest-pinned images (`image: quay.io/cilium/cilium@sha256:...` has no tag to parse at all — an increasingly common GitOps pattern PITFALLS.md independently flags as "not an edge case").

ARCHITECTURE.md recommends the `ds/cilium` image-tag route as primary instead, arguing it piggybacks on the **same RBAC tier cpg already needs** for the unrelated `cilium-envoy` L7 pre-flight check (`daemonsets/get` in `kube-system`) — i.e., no new privilege *class*, just reuse of an existing grant — with Hubble `ServerStatus` and other candidates as fallback/cross-check. It explicitly self-rates this MEDIUM confidence ("a reasoned recommendation from verified RBAC-tier facts, not itself independently verified against a running cluster").

**These do not reconcile automatically — they are a genuine either/or.** Both claims "needs zero new RBAC" are true under different reasoning (Hubble: reuses an already-open connection with no RBAC surface at all; image tag: reuses an already-granted permission for a different purpose), so RBAC cost is not the deciding factor either way. What *does* differentiate them: PITFALLS.md independently documents real image-tag failure modes that don't apply to the Hubble path — digest pins (no tag), custom/mirrored registries with distro-specific suffixes, and non-upstream/enterprise builds whose tag scheme may not map 1:1 to upstream release numbers. PITFALLS also flags that COMPAT-02 must stay privilege-neutral and must **not** reach for `cilium-dbg version` via exec as a shortcut (that would smuggle the audit-window's `pods/exec` RBAC step-up into a feature meant to work in plain readonly mode) — both STACK's and ARCHITECTURE's candidates satisfy that constraint, so it doesn't break the tie.

**Recommendation (opinionated, not a resolution):** lean Hubble `ServerStatus`/`GetNodes` as primary per STACK's stronger evidentiary footing (directly verified proto fields, HIGH confidence; digest-safety is a real, not theoretical, robustness win) with the DaemonSet image tag retained as a secondary cross-check exactly as STACK's "What NOT to Use" table recommends. **Who must decide:** this is a COMPAT-02 design step, owned by whoever plans Phase 21 (see Roadmap below) — confirm the exact `observerpb` field semantics against a real cluster first (does `Node.Version` report the Cilium *agent's* version specifically, or Hubble's own build version, when they could in principle diverge? STACK.md read the proto comment — "Version is the version of Cilium/Hubble" — but this has not been confirmed against live version-skew behavior).

### Tension 2: The ideation draft's §3.E version floors are wrong — PITFALLS.md's PR-verified numbers win, carry them forward

FEATURES.md, working from the draft's assumptions, correctly flagged two of these as uncertain rather than freezing them ("Gaps to Flag for Phase-Level Research") — it did not have the authoritative source. PITFALLS.md did the archaeology directly against merged PRs and release-tag publish dates, and the result **overturns** the draft's §3.E table on three points, all HIGH confidence:

| Claim | Draft said | Verified fact | Evidence |
|---|---|---|---|
| `cilium-dbg` binary rename | greater-or-equal 1.14 | **greater-or-equal 1.15** (released 2024-01-31) | PR #28085 merged 2023-10-11, after the 1.14 branch point; v1.14 cmdref still shows `cilium endpoint config`, v1.15 cmdref shows `cilium-dbg endpoint config`; PR #29187's backport-conflict note confirms the rename did not land in v1.14 |
| `enableDefaultDeny` CNP knob | greater-or-equal 1.15 | **greater-or-equal 1.16** (released 2024-07-24) | PR #30572 merged 2024-03-14 — after v1.15.0 GA'd, before v1.16.0 GA'd; no backport found |
| `policy.cilium.io/proxy-visibility` annotation | "deprecated upstream in recent releases," README may already be a bug | **Removed from agent runtime code at 1.17** (released 2025-02-04); docs-deprecated since 1.15 only | PR #35019 merged 2024-10-01 ("deprecated since Cilium 1.15... no longer supported"); vendored `cilium@v1.19.4` module tree confirmed to contain zero occurrences of the string anywhere |

(PR references: cilium/cilium#28085, #29187, #30572, #35019, #28449 — all on github.com/cilium/cilium/pull/<number>.)

The third row is not merely a milestone-scoping correction — cpg's **already-shipped** README (lines 284-295) currently states this mechanism is "still widely supported (Cilium <= 1.19)," which is materially wrong for any cluster on Cilium greater-or-equal 1.17: the operator annotates a pod, nothing happens, no error surfaces anywhere (Kubernetes accepts arbitrary pod annotations unconditionally). This is a live, shipped, user-facing bug independent of v1.6 scope and should be fixed as part of COMPAT-01/AUD-02, not deferred to "a docs pass."

**Action:** COMPAT-01's declared matrix must use 1.15/1.16/1.17 (not the draft's 1.14/1.15/deprecated-unquantified), cite the merged-PR-plus-release-tag pairing for every entry (the verification method PITFALLS used, not a remembered number), and rewrite the README's proxy-visibility section to state the <=1.16 boundary explicitly and demote it to a legacy/last-resort footnote. AUD-02's bootstrap generator must gate `enableDefaultDeny` emission on **1.16**, not 1.15 — using the wrong number here produces the field-pruning failure mode described in the Executive Summary.

### Tension 3: `enableDefaultDeny` alone does not enforce — correctness-critical for the bootstrap generator, not yet a tested acceptance criterion anywhere

FEATURES.md found (Table Stakes) that `enableDefaultDeny: {ingress: true, egress: true}` alone does **not** enforce default-deny in any shipping Cilium release checked — Cilium requires at least one, even empty, rule stanza (`ingress: []`/`egress: []`) present, or the policy silently no-ops. This is tracked as an open, unresolved upstream CFP (cilium/cilium#35558), not a fixed bug — cpg cannot wait for it to land and must emit the empty-stanza form unconditionally until/unless the floor is raised past whatever release fixes it.

ARCHITECTURE.md's proposed bootstrap CNP skeleton shape (Integration Point 2) already describes `EnableDefaultDeny: DefaultDenyConfig{&ingress, &egress}` "with empty `Ingress`/`Egress` rule lists" — which happens to match FEATURES' requirement — but ARCHITECTURE does not cite the CFP or explain *why* the empty stanzas are present; as written, this reads as an accidental match rather than a deliberately-tested correctness requirement. Neither STACK.md nor ARCHITECTURE.md's discussion of `DefaultDenyConfig` flags the no-rules-means-no-op failure mode as a hazard in its own right.

**Action:** treat this as a named, explicit, tested acceptance criterion for AUD-02 — not an artifact of one particular implementation's struct-literal shape. A test asserting the generated CNP's YAML contains both `enableDefaultDeny` **and** non-nil empty `ingress`/`egress` rule arrays (not merely that the Go struct happens to zero-value that way) should be part of AUD-02's definition of done, with a code comment citing cilium/cilium#35558 so a future maintainer doesn't "simplify" the generator by dropping the seemingly-redundant empty stanzas.

### Tension 4: The SEC-01 "two-mode proof" needs an explicit mechanism decision — build-tag split vs. path-scoped allowlist (all three files converge on the diagnosis, diverge on the fix)

STACK.md, ARCHITECTURE.md, and PITFALLS.md independently arrive at the same two-part diagnosis, which should be treated as settled, HIGH-confidence fact: (1) SSA/RTA reachability is a **static, whole-program** property — it proves what code *can* be called, not what a runtime boolean gates, so a `--enable-audit-bootstrap` flag check inside `runMCPServer` does not remove the guarded subtree from the callgraph; and (2) the SPDY `pods/exec` mechanism (`RESTClient().Post()...SubResource("exec")`, `remotecommand.NewSPDYExecutor`, `Executor.StreamWithContext`) matches none of SEC-01's existing verb-name or filesystem-write detection rules — proven today by the fact that cpg's own existing port-forward code uses the identical shape and is already invisible to the audit, harmlessly.

Where the files diverge is the **fix**, and this is a real fork, not a wording difference:
- **PITFALLS.md** frames it as two named, mutually exclusive options: **Option A** — a Go build tag (`//go:build auditmutation`) producing two build modes, keeping the existing zero-tolerance test literally, structurally unmodified against the default (no-tag) build, with a *second*, tag-gated test asserting the audit-mutation build's new reachable calls are confined to the expected subtree; or **Option B** — stay single-binary and consciously downgrade the existing zero-tolerance property to a reviewed, SSA-symbol-keyed allowlist (the same shape `fsWriteAllowlist` already uses), recorded as an explicit PROJECT.md Key Decision so nobody discovers years later that "zero write verbs reachable" quietly became "an allowlist of write verbs reachable" without anyone deciding that on purpose.
- **ARCHITECTURE.md**'s recommended shape is single-binary, structurally closer to Option B but materially stronger: add a new Property 3 that asserts the exec-executor constructor **is** reachable (the feature exists) and, using the audit's own existing `callPathFrom` helper, that the call path passes **only** through the expected entry point — failing loudly if any *other* cpg-owned function also reaches it. This is a path-scoped assertion, not a flat symbol allowlist, and arguably closes the gap Option B's "an allowlist of write verbs reachable" leaves open (a bare allowlist doesn't prove *only* the intended caller reaches the symbol; a call-path assertion does).

**This decision is also conditional on the separate AUD-03 surface decision** (Variant A, MCP flag-gated, vs. Variant B, CLI-only — see ARCHITECTURE Integration Point 3). If Variant B wins, this entire tension evaporates: the mutating code lives in a sibling cobra command never called from `runMCPServer`, SEC-01's existing BFS never even loads it, and the existing single-mode test continues to prove exactly what it proves today, unmodified. The build-tag-vs-allowlist choice only has to be made **if** Variant A is chosen.

**Recommendation (opinionated):** if Variant A is chosen, prefer PITFALLS' Option A (build-tag split) for its literal, unambiguous preservation of the "zero write verbs reachable, no exceptions" claim for the default build — it is the interpretation that actually matches the draft's own "bit-identical to v1.5" language rather than merely gesturing at it. If a single shipping binary is a hard product requirement that rules out the build-tag split, ARCHITECTURE's path-scoped Property 3 is the correct fallback — a materially stronger single-binary design than a flat allowlist, and should be described to stakeholders as such, not as "the same kind of allowlist we already have for filesystem writes." **Who must decide:** this must be a recorded PROJECT.md Key Decision, made *before* the first line of audit-window mutation code is written (per PITFALLS' explicit warning against letting it be decided implicitly by whatever the first PR happens to do to make the test pass) — owned by discuss-phase ahead of Phase 23 below, sequenced after the Variant A/B surface decision itself.

## Implications for Roadmap

Phase numbering continues from v1.5's final phase (19) per `ROADMAP.md` — v1.6 begins at **Phase 20**.

Based on combined research — ARCHITECTURE.md's explicit "Suggested Build Order" and PITFALLS.md's "Pitfall-to-Phase Mapping," which independently converge on nearly the same groupings, reconciled against FEATURES.md's MVP tiering where they differ (see Phase Ordering Rationale) — suggested phase structure:

### Phase 20: `--include-audit` Verdict Ingestion (AUD-01)
**Rationale:** Zero dependency on anything else in the milestone; every later artifact (bootstrap runbook, audit-window value proposition) has nothing to observe without it. Mirrors an already-proven plumbing pattern (`L7Enabled`/`IgnoreProtocols` threaded identically through `StartArgs -> PipelineConfig -> Aggregator setter`) — no new pattern to invent. Ship first, per the draft's own framing and unanimous agreement across all four research files.
**Delivers:** the 5 known filter sites (plus any additional sites found via an exhaustive, repo-wide re-grep for `Verdict ==`/`Verdict_DROPPED` — a phase-0 verification step, not an assumption that the pre-enumerated list is exhaustive) widened to `{DROPPED, AUDIT}`; the `FlowSource` interface's 4th-parameter signature change threaded through both implementations; a VIS-01-style single warning when the flag is set but zero AUDIT verdicts arrive; a golden/snapshot regression test proving byte-identical `buildFilters` (and sibling sites) output when the flag is unset; an end-to-end test injecting a synthetic AUDIT flow through the full pipeline with the flag unset, asserting zero output artifacts.
**Addresses:** AUD-01.
**Avoids:** Pitfall 8 (an undiscovered 6th filter site, or an "empty filter = match everything" footgun silently widening default behavior).

### Phase 21: Cilium Compatibility Matrix + Runtime Detection (COMPAT-01, COMPAT-02)
**Rationale:** No code dependency on AUD-02/03 (can run in parallel with Phase 20 if the roadmapper wants to compress the schedule), but AUD-02 **needs** its output (the capability gate for `enableDefaultDeny` vs. legacy CNP form) — ARCHITECTURE.md explicitly recommends sequencing this before or alongside AUD-02, not after. This overrides FEATURES.md's priority-tier bucketing (which groups COMPAT-02 with the audit-window-decision-dependent P2 items); there is no actual technical dependency forcing that grouping, only a complexity/value heuristic, and ARCHITECTURE's code-dependency read is the more actionable ordering signal.
**Delivers:** README "Supported Cilium versions" section with one documented floor + the per-feature table, carrying forward Tension 2's corrected numbers (cilium-dbg = 1.15, enableDefaultDeny = 1.16, proxy-visibility removed = 1.17) and completing the same PR-plus-release-tag verification for the still-unpinned entries (`Verdict_AUDIT`/`PolicyVerdictNotify` introduction version, observer gRPC API stability window); the live README proxy-visibility bug fixed as part of this phase, independent of any scope debate; new `pkg/k8s/version.go` implementing the v1.2 warn-and-proceed pre-flight pattern exactly; the version-detection source-of-truth decision from Tension 1 resolved and implemented; MCP/CLI exposure of the detected version + compat verdict.
**Addresses:** COMPAT-01, COMPAT-02.
**Avoids:** Pitfall 6 (wrong version floors), Pitfall 7 (COMPAT-02 must stay privilege-neutral — must never require `pods/exec`, must not bundle its RBAC ask with the audit-window's).

### Phase 22: Bootstrap Artifact Generation (AUD-02)
**Rationale:** Depends on Phase 21's capability-gate output and conceptually on Phase 20 (the runbook text references `cpg generate --include-audit`). Readonly and uncontroversial per FEATURES.md — but only if the persistence choice and the Tension 3 correctness requirement below are both followed exactly. Independent of the AUD-03 surface decision entirely; can ship before or in parallel with it.
**Delivers:** new `pkg/bootstrap` package (explicitly *not* an extension of `pkg/policy/builder.go` — reuses only its CNP-construction conventions); generated CNP carries `enableDefaultDeny` **and** explicit empty `ingress: []`/`egress: []` stanzas, tested as a named acceptance criterion citing cilium/cilium#35558 (Tension 3); version-gated on the corrected 1.16 floor with a hard refusal or documented legacy form below it — never a silently-pruned field; a runbook modeled 1:1 on Cilium's own "Creating Policies from Verdicts" phase order, with an explicit, first-lines warning against daemon-wide `policy-audit-mode` (never included as a copy-pasteable snippet, even as a "don't do this" example); CLI (`cpg bootstrap -n <ns>`) + a readonly MCP tool, content returned directly with no new filesystem-write call site (zero new SEC-01 allowlist entry).
**Addresses:** AUD-02.
**Avoids:** Pitfall 3 (daemon-wide audit as an attractive nuisance), the "extend `builder.go`" and "invent a new fs-write function" anti-patterns from ARCHITECTURE.md, the silently-pruned-CRD-field UX failure mode.

**-- Decision gate before Phase 23 --** The AUD-03 surface decision (Variant A: MCP flag-gated session property vs. Variant B: CLI-only command, MCP stays pure-readonly) is an explicit discuss-phase blocker per ARCHITECTURE.md's own framing: the two variants touch almost entirely different files and carry a materially different SEC-01 engineering cost (see Tension 4). This should resolve via `/gsd-discuss-phase` before Phase 23 is planned at file-level detail — including, if Variant A wins, the compounding build-tag-split-vs-allowlist decision from Tension 4, recorded as its own PROJECT.md Key Decision.

### Phase 23: Managed Audit Window + SEC-01 Evolution (AUD-03, AUD-04)
**Rationale:** Highest complexity and highest differentiation item in the milestone (FEATURES.md: HIGH value, HIGH cost, P2). Cannot be scoped into file-level work until the decision gate above resolves. Shared substrate first, then orchestration, then the chosen surface's wiring — per ARCHITECTURE.md's build order.
**Delivers:** new `pkg/k8s/exec.go` (SPDY sibling of `portforward.go`, but genuinely new node-scoped pod-selection logic — not a copy of `findRelayPod`); `pods/exec` RBAC scoped narrowly to `kube-system` + cilium-agent pods, documented completely separately from base RBAC (never bundled into one example Role); a new-endpoint watcher preferring `CiliumEndpoint` CRD watch over a plain Pod watch (gives endpoint ID + node in one object; namespace-scoped, never cluster-wide); new `pkg/auditwindow` package with "ours" bookkeeping keyed on `CiliumEndpoint` UID/resourceVersion or pod UID (never a raw integer endpoint ID alone — ID-reuse risk, Pitfall 4); revert wired into the *same* SESS-05 fan-out (watcher goroutine joins the existing completion signal; TTL timer reuses the `stopOnce`/`explicitStopSeen` atomic-swap idiom; per-endpoint revert success/failure tracked and surfaced, never a blanket "reverted"; watcher stops only after/atomically with the final revert sweep); a precondition refusal if daemon-wide audit is already on; tool-annotation honesty cross-checked automatically against SEC-01's own reachability findings (Pitfall 9); an early cluster-empirical spike resolving whether endpoint regeneration alone resets `PolicyAuditMode` (documented only for full agent restart — genuinely unresolved by docs) before finalizing watcher scope; e2e coverage extended for the TTL-vs-explicit-stop race, a watcher goroutine still running at process exit, and a partial-failure revert sweep; if Variant A, the SEC-01 mechanism from Tension 4 built alongside the feature, not after; README rewording from "readonly, period" to "readonly by default; scoped, lifecycle-bound mutations behind an explicit launch flag."
**Addresses:** AUD-03, AUD-04.
**Avoids:** Pitfalls 1, 2, 4, 5, 9 — nearly the entire Critical Pitfalls list converges on this one phase.

### Phase 24: cpg-Dedicated Skills & Agent Tooling (SKL-01..05, optional `cpg-operator`)
**Rationale:** Zero `pkg/`/`cmd/` integration, no compile dependency on anything above. `cpg-triage`, `cpg-policy-review`, `cpg-health-report`, and `cpg-mcp-smoke` need only the existing v1.5 MCP surface / CLI and, per FEATURES.md's dependency notes, could ship as early as right after Phase 20 for dogfooding value — ARCHITECTURE.md instead sequences all skills last so they can reference real, working workflows. This is genuine scheduling freedom for the roadmapper, not a technical dependency either way. `cpg-audit-onboard` (SKL-02) is the one skill with a hard dependency (AUD-01 + AUD-02, plus AUD-03 if Variant A is chosen) and must trail Phase 22 (and Phase 23 if applicable) regardless of where the other four are scheduled.
**Delivers:** five `.claude/skills/cpg-*/SKILL.md` files (+ optional `.claude/agents/cpg-operator.md`) written as workflow routers pointing at live tool discovery (`tools/list`) rather than restating tool mechanics inline; a golden-file/consistency-check tripwire tying skill and README prose back to the Go source `Description:` strings; `cpg-mcp-smoke` asserting against live tool schemas, not just tool-name presence.
**Addresses:** SKL-01..05.
**Avoids:** Pitfall 10 (skills becoming a third, drifting copy of tool semantics), the single-mega-skill anti-feature.

### Phase Ordering Rationale

- Phase 20 and Phase 21 have no code dependency on each other and can run in parallel if the roadmapper wants to compress the schedule — both files agree on this independence.
- Phase 21 is deliberately sequenced *before* Phase 22 on ARCHITECTURE.md's technical-dependency read (AUD-02 needs COMPAT-02's capability gate), overriding FEATURES.md's priority-tier grouping, which had no stated code-level blocker forcing the later placement.
- The decision gate between Phase 22 and Phase 23 is a hard requirement, not a suggestion — ARCHITECTURE.md states plainly that AUD-03 "cannot be scoped into phases until variant A vs B is chosen," since the variants touch almost entirely different files.
- Phase 24 is sequenced last by ARCHITECTURE's reasoning (skills read better against finished workflows) but FEATURES' dependency analysis shows most of it (SKL-01/03/04/05) has no technical reason to wait — flagged as scheduling flexibility, not a fixed position.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 21 (COMPAT-01/02):** Tension 1 (version-detection source of truth) is unresolved between STACK.md and ARCHITECTURE.md and needs a decision; several §3.E floor entries still need the same PR-plus-release-tag verification rigor PITFALLS.md applied to only 3 of them (Verdict_AUDIT/PolicyVerdictNotify vintage, observer API stability window).
- **Phase 23 (AUD-03/AUD-04):** needs `/gsd-discuss-phase` before it can be planned at all (the Variant A/B surface decision), plus a genuine research/spike need once scoped (the cluster-empirical endpoint-regeneration-persistence question, Tension 4's SEC-01 mechanism decision, RBAC minimal-scope verification, watch-latency characteristics under HPA-burst conditions).

Phases with standard patterns (skip research-phase — shapes are already fully specified by this research round):
- **Phase 20:** exact plumbing pattern already given in ARCHITECTURE.md Integration Point 1, mirrors 3 prior shipped features threaded identically.
- **Phase 22:** every open question already answered at HIGH confidence across the four files (package boundary, persistence choice, the empty-rule-stanza requirement, the corrected 1.16 floor) — implementation work, not research work.
- **Phase 24:** plain Markdown/YAML frontmatter, official Claude Code skill-authoring guidance already fully specifies the shape; the one open item (SKL-02's exact scope) resolves automatically once Phase 23's surface decision is known, not a research gap in its own right.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Verified by reading actual vendored module source (`github.com/cilium/cilium@v1.19.4`, `k8s.io/client-go`/`k8s.io/apimachinery@v0.35.4`) at the exact pinned versions, plus a live fetch of `kubernetes/kubectl@master` and current Claude Code docs — not documentation-only inference. |
| Features | MEDIUM-HIGH -> effectively HIGH after synthesis | Audit-mode mechanics and Calico/Istio/JIT comparables are HIGH, official-doc-backed. FEATURES.md itself flagged the `cilium-dbg` rename version and `proxy-visibility` deprecation status as LOW/MEDIUM-LOW gaps needing phase-level verification — PITFALLS.md's PR-verified corrections (Tension 2) close both gaps, so the residual uncertainty FEATURES.md correctly declined to resolve is no longer open. |
| Architecture | HIGH | Every integration point grounded in source read at repo HEAD + the same vendored dependencies, file:line cited throughout. MEDIUM/LOW called out explicitly and only for design-synthesis recommendations (package naming, which watch primitive to prefer) rather than verified facts. |
| Pitfalls | HIGH for version/mechanics claims (PR + release-tag citations); MEDIUM/LOW explicitly marked inline for the handful of genuinely cluster-empirical questions (endpoint-regeneration persistence semantics) that no documentation search can settle. |

**Overall confidence:** HIGH — an unusually well-verified milestone research pass. All four files performed direct source verification (vendored module reads, live PR/release-tag lookups, actual repo HEAD reads) rather than relying on general knowledge or blog-post synthesis, and the one file (FEATURES.md) that flagged genuine uncertainty had that uncertainty independently resolved by a sibling file's follow-up archaeology (Tension 2) — a real synthesis win, not a coincidence to note in passing. The residual gaps below are concentrated in decisions the research correctly declined to make unilaterally (the two surface/mechanism forks) and questions that are irreducibly empirical (cluster behavior under endpoint regeneration).

### Gaps to Address

- **Tension 1** (version-detection source of truth): STACK.md and ARCHITECTURE.md disagree on primary vs. fallback signal; this synthesis leans Hubble `ServerStatus`/`GetNodes` per STACK's stronger evidentiary footing, but needs an explicit COMPAT-02 design decision during Phase 21 planning, plus a live-cluster confirmation of the exact field semantics under agent/Hubble version skew.
- **Tension 4, part 1** (AUD-03 surface: Variant A vs. Variant B): the single largest open decision in the milestone — gates Phase 23's entire scope and file list. Needs `/gsd-discuss-phase` before Phase 23 can be planned.
- **Tension 4, part 2** (SEC-01 two-mode mechanism: build-tag split vs. path-scoped allowlist): conditional on the above resolving to Variant A; needs its own recorded PROJECT.md Key Decision before any audit-window mutation code is written, per PITFALLS.md's explicit warning against letting this be decided implicitly.
- **Endpoint regeneration vs. restart vs. deletion persistence semantics** for `PolicyAuditMode`: documented only for full agent restart (resets to daemon default); regeneration-alone behavior is genuinely unresolved by any documentation found and requires an empirical spike on a real cluster early in Phase 23, before the watcher's event scope (pod-create only, or also regeneration events on already-flipped endpoints) can be finalized.
- **Remaining unpinned §3.E floor entries**: `Verdict_AUDIT`/`PolicyVerdictNotify` introduction version and the observer gRPC API stability window are still stated as "roughly greater-or-equal 1.8/1.9" in the draft — need the same merged-PR-plus-release-tag verification Tension 2's three corrections used, not a re-guess.
- **Live README proxy-visibility bug**: already fixed in scope by Phase 21's plan above, but flagged here as a genuine pre-existing shipped defect independent of v1.6 — should not silently fall out of scope if Phase 21 is ever descoped or delayed.
- **`enableDefaultDeny`'s empty-rule-stanza requirement** (Tension 3): correctness-critical and must become a named, tested acceptance criterion for AUD-02 rather than an artifact of whichever struct literal an implementer happens to write first.

## Sources

### Primary (HIGH confidence)
- Direct source inspection of `$(go env GOMODCACHE)/github.com/cilium/cilium@v1.19.4` and `k8s.io/{client-go,apimachinery}@v0.35.4` — proto fields, `DefaultDenyConfig`, generated informers/listers, `remotecommand`/`httpstream` internals
- cpg's own repo source at HEAD (post-v1.5) — every file:line citation in ARCHITECTURE.md and PITFALLS.md (`pkg/hubble/{client,aggregator,pipeline}.go`, `pkg/flowsource/{source,file}.go`, `pkg/k8s/{portforward,preflight,cluster_dedup}.go`, `pkg/session/{manager,session}.go`, `pkg/policy/builder.go`, `pkg/output/writer.go`, `cmd/cpg/{main,generate,replay,mcp,mcp_tools,mcp_audit_test,mcp_e2e_test}.go`, `README.md`)
- Cilium official docs: "Creating Policies from Verdicts", "Deny Policies", "Kubernetes Compatibility", "Terminology" (endpoint ID uniqueness) — docs.cilium.io
- GitHub PRs (merged, dated): cilium/cilium#28085 (cilium-dbg rename), #29187 (v1.14 backport-conflict confirmation), #30572 (EnableDefaultDeny), #35019 + #28449 (proxy-visibility removal/deprecation); release tags v1.15.0/v1.16.0/v1.17.0; open CFP cilium/cilium#35558 (empty-rule-stanza requirement)
- kubernetes/kubectl `pkg/cmd/exec/exec.go` @ master — live fetch, current `createExecutor` fallback pattern
- k8s.io/apimachinery/pkg/util/version (pkg.go.dev); standard Kubernetes RBAC docs (`pods/exec` distinct subresource verb)
- Claude Code official docs: "Create custom subagents", "Extend Claude with skills", "Skill authoring best practices" — code.claude.com / docs.claude.com, live fetch
- MCP Blog — "Tool Annotations as Risk Vocabulary" (blog.modelcontextprotocol.io) — annotations as untrusted hints, not enforced guarantees

### Secondary (MEDIUM confidence)
- Tigera/Calico staged network policies blog, Istio mTLS migration docs, Teleport JIT access, telekom/k8s-breakglass — comparable-pattern analysis
- Azure mcp-kubernetes, kagent (CNCF sandbox) — anti-pattern/scope-proportionality comparables
- Stytch "Securing MCP", OWASP MCP Security Cheat Sheet, Invariant Labs tool-poisoning notice — MCP security posture corroboration
- CNCF blog — "Safely managing Cilium network policies: testing and simulation techniques"

### Tertiary (LOW confidence / superseded)
- Community cheat-sheets/blog posts asserting `cilium-dbg`'s rename at 1.14 and treating `proxy-visibility` as merely "deprecated" without a removal date — explicitly superseded by Tension 2's PR-verified corrections; do not use these for COMPAT-01.

---
*Research completed: 2026-07-22*
*Ready for roadmap: yes*
