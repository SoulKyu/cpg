# Feature Research

**Domain:** Kubernetes/Cilium network-policy onboarding tooling + LLM-agent-facing ops tooling (repo-local Claude Code skills/agents on top of a readonly MCP server)
**Researched:** 2026-07-22
**Confidence:** MEDIUM-HIGH (audit-mode mechanics and Calico/Istio/JIT comparables are HIGH confidence, official-doc-backed; two specific version-floor facts are flagged LOW and need phase-level verification before being frozen into a compat table)

Scope note: this file covers ONLY the five v1.6 feature areas (`--include-audit` ingestion, bootstrap artifact generation, managed audit window, cpg-dedicated skills/agents, Cilium version compat). Existing v1.0–v1.5 features are not re-researched.

---

## Feature Landscape

### Table Stakes (Users Expect These)

Features an SRE evaluating this milestone will consider non-negotiable — mostly because the upstream ecosystem (Cilium itself, Calico, Istio) already sets the expectation.

| Feature | Area | Why Expected | Complexity | Notes |
|---------|------|--------------|------------|-------|
| Ingest `Verdict_AUDIT` flows through the same pipeline as `DROPPED` | AUD-01 | Cilium's own "[Creating Policies from Verdicts](https://docs.cilium.io/en/stable/security/policy-creation/)" guide *is* the documented onboarding path — a policy generator that can't see audit verdicts can't participate in the sanctioned upstream workflow at all | LOW-MEDIUM | Draft already pinpoints the 5 filter sites; drop-reason decoding is verified unchanged on AUDIT flows (`decodeDropReason`/`decodeVerdict` in vendored parser) |
| Single warning when a diagnostic mode yields zero expected signal | AUD-01 | Direct precedent already shipped in cpg: VIS-01 (v1.2) warns once when `--l7` is set but zero L7 records arrive. Operators expect the same courtesy for `--include-audit` — "you turned this on, nothing showed up, you probably forgot the enable step" | LOW | Reuse the exact VIS-01 single-warning pattern; do not invent a new UX for this |
| Generated default-deny CNP actually enforces default-deny | AUD-02 | Table stakes at the level of "the artifact does what it says." **Load-bearing correctness gotcha found in research:** as of the versions checked, `enableDefaultDeny: {ingress: true, egress: true}` alone does **not** enforce default-deny — Cilium requires at least one (even empty) rule stanza present (`ingress: []` / `egress: []`) or the policy silently no-ops. This is tracked as an open upstream CFP (["Clarify the intent for policies with default deny specified with no rules", cilium/cilium#35558](https://github.com/cilium/cilium/issues/35558)), not yet fixed in shipping releases | LOW-MEDIUM | cpg's generator MUST emit the empty-rule-stanza form until/unless the CFP ships and the declared floor is raised past it. This is a correctness requirement, not a style choice — silent non-enforcement in a "safety" bootstrap artifact is the worst possible failure mode |
| A written, ordered runbook accompanies the bootstrp policy | AUD-02 | Upstream's own guide is exactly this shape: enable audit → apply default-deny → observe verdicts (`hubble observe -t policy-verdict`) → write allow rules → disable audit/enforce. Operators already expect a checklist; upstream provides shell-command templates but explicitly **no automation tooling** — that gap is what cpg fills | LOW | Model the runbook's phase order 1:1 on the upstream guide so operators already familiar with Cilium docs recognize it immediately |
| Warn-and-proceed on missing/incompatible cluster capability, never hard-abort | COMPAT-02 | Direct internal precedent: v1.2 pre-flight checks (`enable-l7-proxy`, `cilium-envoy` DaemonSet) already warn-and-proceed rather than abort, specifically because CI service accounts and reduced-permission operators must not be locked out. External precedent: `cilium status --verbose` and `cilium version` themselves report mismatches without refusing to run | LOW | Reuse the v1.2 pre-flight pattern verbatim for version-floor checks — same phrasing conventions, same "read-only verb, never abort" posture |
| A declared, single-document compatibility floor | COMPAT-01 | Cilium itself ships a [Kubernetes Compatibility table](https://docs.cilium.io/en/stable/network/kubernetes/compatibility/) as "the authoritative reference," refreshed every release, precisely because — per the ecosystem's own framing — "ignoring this table before an installation or upgrade is one of the most common causes of Cilium deployment failures." A tool that itself has version-dependent features (bootstrap CNP form, `cilium-dbg` binary name) and doesn't declare its own floor repeats that failure mode one layer up | LOW | Documentation-only artifact; the cost is the verification legwork (see Gaps), not the writing |
| Repo-local, versioned skill files discoverable by Claude Code without manual invocation | SKL-01..05 | Matches [official Claude Code skill-authoring guidance](https://docs.claude.com/en/docs/agents-and-tools/agent-skills/best-practices): skills committed to `.claude/skills/` in the repo are picked up by every clone, with live change detection — this is the documented, expected mechanism for "team conventions enforced in every session," not a novel pattern cpg invents | LOW | Constraint is entirely about following the platform's own conventions (concise SKILL.md, <500 lines, clear name+description for triggering), not new product code |

### Differentiators (Competitive Advantage)

Where cpg goes further than both the upstream manual workflow and adjacent generic tooling.

| Feature | Area | Value Proposition | Complexity | Notes |
|---------|------|--------------------|------------|-------|
| Automated end-to-end onboarding (bootstrap → observe → generate → enforce) | AUD-01/02 | Upstream's own guide is explicit that it offers **"shell command templates but no automation tooling recommendations"** for going from verdicts to policies. cpg closing that gap — generation is already cpg's core value prop — is the single clearest differentiator in this milestone | MEDIUM | Builds directly on existing `generate`/`replay` pipeline; the new work is ingestion widening + bootstrap generation, not new policy logic |
| cpg-managed, lifecycle-bound audit window (flip → watch → auto-revert on stop/crash/TTL) | AUD-03 | This is a stronger safety guarantee than **every** comparable pattern found: Cilium's own per-endpoint audit flip is manual and "restarting the Cilium pod resets it" (i.e., reset is accidental, not managed); Calico's staged policies require a manual `kind:` edit to promote (no auto-expiry at all); shell-script rollback approaches have exactly the failure modes the draft names (forgotten rollback, mis-ordering, terminal death stranding the window). A session-scoped mutation that structurally cannot outlive its owning process (v1.5's SESS-05 bounded fan-out, e2e-proven under both graceful stop and ungraceful disconnect) is closer to [Teleport-style JIT access](https://goteleport.com/learn/just-in-time-access-for-amazon-eks/) (time-boxed, auto-revoked, no standing privilege) than to anything in the network-policy space specifically | HIGH | Highest complexity and highest differentiation in the milestone — matches the draft's own framing as the item needing a discuss-phase decision. New-pod-watcher auto-flip in particular has **no upstream or comparable-tool equivalent found**: neither Cilium's manual guide nor Calico's staged-policy docs address "pod created mid-window" at all |
| Typed, taxonomy-teaching MCP tools vs. generic command pass-through | SKL-01..05 / all MCP surface | Directly contrasted against [Azure's `mcp-kubernetes`](https://github.com/Azure/mcp-kubernetes), which exposes Cilium/Hubble as raw `call_cilium`/`call_hubble` command-execution tools ("straightforward command pass-through rather than domain-specific operations"). cpg's existing QRY-01..05 tools (paginated, schema'd, `dropclass`-aware) are already a structural step up; extending that discipline to bootstrap/compat tools (rather than adding a raw exec escape hatch) is the differentiator to protect | LOW (discipline, not code) | This is as much an anti-pattern-to-avoid as a feature — see Anti-Features below |
| Structural, mutation-tested readonly proof extended to a "readonly by default, provably scoped mutation behind an explicit flag" two-mode proof | AUD-04 | No comparable tool found does this. Generic K8s/Cilium MCP wrappers (Azure `mcp-kubernetes`, `containers/kubernetes-mcp-server`) rely on RBAC and documentation for their safety story. cpg's SEC-01 (RTA callgraph reachability, mutation-tested) proving the claim in CI is a genuine differentiator worth carrying forward rather than diluting | MEDIUM | Conditional on the AUD-03 surface decision (see Dependencies) — if CLI-only is chosen, this shrinks to "re-confirm zero write verbs still holds with new readonly tools added," not a real two-mode proof |
| Cilium version detection + feature gating exposed to the LLM (not just a README table) | COMPAT-02 | Comparable tools stop at documentation (Cilium's own compat table is static/manual). Surfacing "this cluster is Cilium X.Y, feature Z unavailable" inside `start_session`/`get_status` lets the LLM reason about it without guessing or hallucinating a feature that isn't there — directly relevant given the project's own prior decision to drop AI-assisted semantic analysis over hallucination risk | MEDIUM | Reuses `ClassifierVersion` semver precedent (`pkg/dropclass`) as an existing in-repo pattern for "surface a version pairing" |
| Purpose-built, lightweight repo-local skills vs. a general cloud-native agent framework | SKL-01..05 | [kagent](https://github.com/kagent-dev/kagent) (CNCF sandbox) is the closest "AI agents for cloud-native ops" comparable, but it's a whole additional system: a Kubernetes controller + CRDs for agents/tools + a separate Python reasoning engine + OTel pipeline, aimed at being a general framework across Istio/Argo/Prometheus. cpg's approach — versioned markdown skills consumed directly by Claude Code, zero extra infrastructure — is deliberately a much lighter tier, appropriate for a single-purpose CLI+MCP tool rather than a platform | LOW | Confirms the repo-local-only constraint (§3.D of the draft) is the right call, not under-ambition — running a kagent-style controller to support 5 skills for one CLI tool would be a scope explosion |

### Anti-Features (Commonly Requested, Often Problematic)

| Feature | Why it looks appealing | Why problematic | Alternative |
|---------|------------------------|------------------|-------------|
| Daemon-wide `policy-audit-mode=true` convenience command/flag | "One flag, whole cluster in audit — simplest possible onboarding" | Confirmed independently by both the draft's verified code facts and [official Cilium docs](https://docs.cilium.io/en/stable/security/policy-creation/): it **"is not recommended for production deployment"** and suspends enforcement of ALL existing policies cluster-wide, not just the namespace being onboarded — a single onboarding action would blind the whole cluster's enforcement | Namespace scoping via the synthesized approach already chosen: namespaced default-deny CNP + per-endpoint audit flips scoped to that namespace's pods only |
| Free-floating `enable_audit`/`disable_audit` MCP tools (no lifecycle binding) | Feels more flexible — LLM or operator can flip audit on/off at will, any time | This is exactly the shape [MCP security guidance](https://stytch.com/blog/mcp-security/) warns about — a state-mutating tool with no automatic cleanup path is a standing privilege, not a JIT one; an LLM hallucination, dropped connection, or forgotten follow-up call leaves a namespace silently unenforced indefinitely. Contrast with JIT/break-glass conventions ([Teleport](https://goteleport.com/learn/just-in-time-access-for-amazon-eks/), [k8s-breakglass](https://github.com/telekom/k8s-breakglass)) where time-boxing and auto-revoke are the entire point | Session-scoped property as already designed in the draft: `start_session{audit_bootstrap, audit_ttl}` → revert bound to the existing SESS-05 fan-out (stop/crash/SIGTERM/TTL), not a separate mutable toggle |
| Generic "exec arbitrary `cilium-dbg`/`kubectl` command" pass-through tool | Simplest implementation — one tool covers audit flips, version checks, anything else that comes up later | This is literally what [Azure `mcp-kubernetes`](https://github.com/Azure/mcp-kubernetes)'s `call_cilium`/`call_hubble` tools do, and it defeats the entire SEC-01 structural-readonly value proposition: a reachability audit can prove "no typed write-verb call is reachable," but it cannot bound what an arbitrary shelled-out command string does. It also reintroduces exactly the "undetectable by any gateway tool" scope-enforcement gap MCP security writeups flag as the core problem with current MCP practice | Keep every mutation a named, typed, single-purpose tool (or CLI subcommand) with a fixed, auditable set of side effects — exactly the SEC-01 discipline already applied to the 8 existing tools |
| Custom `cpg.io/audit-mode: enabled`-style CNP annotation to fake per-policy audit scoping | Would make audit scope feel declarative/GitOps-friendly, matching how the rest of the CNP is authored | **There is no native per-policy or per-namespace audit scope in Cilium** (verified fact, §2 of the draft) — Cilium would simply ignore such an annotation. Shipping it would be a silently-inert feature that misleads operators into thinking they've scoped something they haven't | Keep the namespace-scoping synthesis explicit and mechanical (namespaced CNP selection + per-endpoint flips), never implied via an annotation Cilium doesn't understand |
| Full CI cluster test matrix across every supported Cilium version (kind + cilium install per version) | Highest-confidence way to *prove* the compat table is correct | Explicitly heavy — draft already calls this out as disproportionate, and it matches the ecosystem norm: even Cilium's own K8s compatibility table is a **published, manually-curated reference**, not a live per-PR matrix test across every historical version pair for downstream consumers | Static, dependency-derived matrix (COMPAT-01) + runtime warn-and-proceed detection (COMPAT-02) — verification happens against the live cluster at connect-time, not against a synthetic matrix in CI |
| A single mega-skill/agent file that inlines all 5 workflows (triage + onboarding + review + health-report + smoke) | Fewer files, "one skill to rule them all," less to maintain | Directly contradicts [official skill-authoring guidance](https://docs.claude.com/en/docs/agents-and-tools/agent-skills/best-practices): "multiple small Skills are preferable to one large Skill (composition over monoliths)." A 5-in-1 skill also blows past the recommended <500-line SKILL.md size and makes Claude's triggering-on-description mechanism far less precise (five different intents behind one description) | Keep the 5 skills separate per the draft's table; a dedicated `cpg-operator` **subagent** (a different architectural layer — context isolation / tool-scoping, not a mega-skill) is a legitimate, distinct option and is not this anti-pattern — see Dependency Notes |
| `apply_policy`/CNP-create-or-delete MCP tool for allow rules | "Why generate a YAML the human still has to apply by hand" | Explicitly out of scope, carried forward unchanged from v1.5 (§4 of the draft) — applying generated allow-rule policies stays a human act. Folding this into v1.6 would also collide with the SEC-01 two-mode proof's stated boundary ("v1 mutates ONLY endpoint audit config... CNP apply/delete = possible later extension, one mutation tier above") | Keep policy application entirely manual; if ever revisited, it's a distinct, later, more-privileged milestone — not bundled into audit-window scope |

---

## Feature Dependencies

```
AUD-01 (--include-audit ingestion)
    └──gates the whole onboarding loop being observable──> AUD-02 (bootstrap artifact generation)
                                                                └──namespace target for──> AUD-03 (managed audit window)
                                                                                                └──surface choice determines scope of──> AUD-04 (SEC-01 two-mode proof)

COMPAT-01 (declared matrix) ──must exist before──> COMPAT-02 (runtime detection compares against it)
COMPAT-02 ──feature-gates──> AUD-02 (bootstrap YAML form: enableDefaultDeny vs legacy, by version)
COMPAT-02 ──feature-gates──> AUD-03 (cilium-dbg vs cilium binary selection, by version)

SKL-01 (cpg-triage)        ──requires only──> v1.5 MCP (no v1.6 dependency — shippable first, independently)
SKL-03 (cpg-policy-review) ──requires only──> existing CLI (`cpg explain`) (no v1.6 dependency)
SKL-04 (cpg-health-report) ──requires only──> existing cluster-health.json (no v1.6 dependency)
SKL-05 (cpg-mcp-smoke)     ──requires only──> existing mcp_e2e_test.go infra (no v1.6 dependency)
SKL-02 (cpg-audit-onboard) ──requires──> AUD-01 + AUD-02 (+ AUD-03 IF the MCP-driven surface is chosen)

cpg-operator agent (optional) ──enhances──> SKL-01, SKL-02 (tool-scoping/context-isolation layer, not a functional dependency)
```

### Dependency Notes

- **AUD-01 gates AUD-02/03's *value*, not their buildability.** Bootstrap artifact generation (CNP + runbook text) is pure generation with no runtime AUDIT-flow dependency and could technically be built in isolation — but per the draft, "without this piece, any bootstrap tooling opens a window cpg cannot see," so shipping AUD-01 first is the correct sequencing regardless of technical independence. This is a value-sequencing dependency, not a code dependency.
- **AUD-04 is conditional on the AUD-03 surface decision, not a fixed-scope item.** If the CLI-only surface is chosen (MCP stays pure-readonly, bootstrap tool returns the command for a human to run), AUD-04 shrinks to "reconfirm the existing SEC-01 zero-write-verb guarantee still holds with the new readonly tools added" — a much smaller task than a genuine two-mode structural proof. Requirements definition should size AUD-04 only after AUD-03's surface is picked, or scope it as two explicit sub-variants.
- **SKL-02 is the one skill with a hard v1.6 dependency; the other four are independent and could ship in an earlier phase than the audit-mode features themselves.** This matters for phase ordering — SKL-01/03/04/05 have zero technical reason to wait for AUD-01..04 and could be sequenced as an early, low-risk phase that also serves as validation/dogfooding for the existing v1.5 MCP surface before the higher-complexity audit-window work begins.
- **`cpg-operator` agent vs. per-skill invocation is an architecture choice, not a functional dependency** — per official guidance, subagents earn their keep specifically for context isolation and *tool-scoping* (a subagent's allowed toolset can be restricted independently of the main conversation's). A dedicated `cpg-operator` agent would matter most as a defense-in-depth measure for AUD-03/SKL-02 specifically: an agent whose tool allowlist is limited to the MCP surface reduces blast radius if a triage/onboarding session is ever steered off-task, more than it matters for the simpler, already-readonly skills (SKL-01/03/04/05). This is evidence for the discuss-phase decision, not a resolution of it.
- **COMPAT-01 has no code dependency and should be sequenced early** — it is pure documentation derived from already-known/vendored dependencies, and COMPAT-02's runtime checks need the matrix to exist first to have something to compare against.

---

## MVP Definition

Framed as v1.6 phase-scoping guidance (this is a subsequent milestone on a shipped product, not a 0-to-1 launch).

### Ship First (load-bearing, low-risk)

- [ ] **AUD-01** `--include-audit`/`include_audit` ingestion + zero-signal warning — everything else in the milestone is inert without it; draft explicitly says "ship first"
- [ ] **AUD-02** Bootstrap artifact generation (CNP + runbook) — readonly, uncontroversial, but must get the `enableDefaultDeny` + empty-rule-stanza correctness right (see Table Stakes)
- [ ] **COMPAT-01** Declared compatibility matrix — pure documentation, cheap, and needed as the reference point for COMPAT-02
- [ ] **SKL-01, SKL-03, SKL-04, SKL-05** — zero v1.6 feature dependency, buildable and shippable independently of the audit-mode work; low complexity, good early validation of the v1.5 MCP surface

### Add After the Audit-Window Decision (medium-to-high complexity, dependent on discuss-phase resolution)

- [ ] **AUD-03** Managed audit window — highest complexity and the one item with an open surface decision (MCP flag-gated vs. CLI-only); do not start implementation until that decision is made
- [ ] **AUD-04** SEC-01 two-mode evolution — size depends directly on AUD-03's outcome
- [ ] **SKL-02** cpg-audit-onboard — depends on AUD-01 + AUD-02, and on AUD-03 if the MCP-driven surface is chosen
- [ ] **COMPAT-02** Runtime detection + feature gating — depends on COMPAT-01; feeds AUD-02's version-gated YAML form and AUD-03's `cilium-dbg`/`cilium` binary selection

### Explicitly Not This Milestone (already correctly deferred)

- [ ] CNP apply/delete via MCP (any tier) — stays out of scope per carried-forward v1.5 constraint
- [ ] Full CI cluster test matrix across Cilium versions — static matrix + runtime detection substitutes for it
- [ ] A generic/raw command-passthrough MCP tool — never add one, regardless of convenience pressure

---

## Feature Prioritization Matrix

| Feature (REQ ID) | User Value | Implementation Cost | Priority |
|-------------------|------------|----------------------|----------|
| AUD-01 `--include-audit` ingestion | HIGH | LOW-MEDIUM | P1 |
| AUD-02 Bootstrap artifact generation | HIGH | LOW-MEDIUM | P1 |
| COMPAT-01 Declared matrix | MEDIUM | LOW | P1 |
| SKL-01 cpg-triage | MEDIUM-HIGH | LOW | P1 |
| SKL-03 cpg-policy-review | MEDIUM | LOW | P1 |
| SKL-04 cpg-health-report | MEDIUM | LOW-MEDIUM | P1 |
| SKL-05 cpg-mcp-smoke | MEDIUM (release-quality signal) | LOW | P1 |
| AUD-03 Managed audit window | HIGH (biggest differentiator) | HIGH | P2 (blocked on discuss-phase surface decision) |
| AUD-04 SEC-01 two-mode proof | HIGH (trust/safety claim) | MEDIUM (conditional) | P2 |
| SKL-02 cpg-audit-onboard | HIGH | MEDIUM | P2 |
| COMPAT-02 Runtime detection + gating | MEDIUM-HIGH | MEDIUM | P2 |
| `cpg-operator` agent (optional) | MEDIUM (defense-in-depth) | MEDIUM | P3 |

**Priority key:**
- P1: Ship-first, low-risk, no blocking open decisions
- P2: Valuable, but sized/sequenced by the AUD-03 surface decision
- P3: Nice to have, genuinely optional per the draft's own framing

---

## Comparable-Tool / Pattern Analysis

| Pattern | How it works there | cpg's approach |
|---------|--------------------|-----------------|
| **Cilium's own "Creating Policies from Verdicts"** ([docs](https://docs.cilium.io/en/stable/security/policy-creation/)) | 5 fully-manual phases: enable audit (daemon-wide or per-endpoint) → apply default-deny CNP → `hubble observe -t policy-verdict` → hand-write allow policies → disable audit/enforce. Shell-command templates only, "no automation tooling recommendations" | Automates phases 2-4 end-to-end (bootstrap generation + AUDIT ingestion + existing policy-generation pipeline); phase 1 (enable) and 5 (enforce) are the contested "who mutates" surface (AUD-03 open decision) |
| **Calico staged network policies** ([Tigera](https://www.tigera.io/blog/dry-run-your-kubernetes-network-policies-with-calico-staged-network-policies/)) | A first-class CRD tier (`StagedNetworkPolicy`/`StagedKubernetesNetworkPolicy`/`StagedGlobalNetworkPolicy`) that logs match verdicts without enforcing; promoted to real enforcement by manually changing the resource `kind:`. No time-based expiry — promotion/rollback is entirely a human, manual step | cpg has no equivalent first-class "staged CNP" resource (Cilium has no such native tier — confirmed: only daemon-wide or per-endpoint audit flags exist, not a policy-kind distinction) — cpg instead layers a managed, TTL-bound *process* (the audit window) on top of Cilium's coarser primitives, which is a stronger automatic-safety story than Calico's manual-promotion model, at the cost of not being a native CRD-level feature |
| **Istio mTLS PERMISSIVE → STRICT migration** ([istio.io](https://istio.io/latest/docs/tasks/security/authentication/mtls-migration/)) | Namespace-by-namespace phased rollout (e.g., staging → dev → internal-tools → production), confirming stability before moving to the next scope, then a final mesh-wide STRICT policy | Validates the namespace-scoped, phased-rollout shape of cpg's onboarding design (one namespace's audit window at a time) as the industry-standard way to limit blast radius during a "lock down gradually" migration |
| **JIT / break-glass privileged access** ([Teleport](https://goteleport.com/learn/just-in-time-access-for-amazon-eks/), [k8s-breakglass](https://github.com/telekom/k8s-breakglass)) | Time-boxed, task-scoped elevated access issued on request, auto-revoked at TTL expiry or task completion, no standing privilege | Direct structural analogue for AUD-03: the audit window is a JIT elevation of Cilium's audit-mode primitive, scoped to a session and a TTL, with the v1.5 SESS-05 bounded-cleanup fan-out as the "auto-revoke" mechanism |
| **Generic K8s/Cilium MCP wrappers** ([Azure mcp-kubernetes](https://github.com/Azure/mcp-kubernetes), `containers/kubernetes-mcp-server`) | Expose `call_cilium`/`call_hubble` as raw command-execution tools; safety story relies on RBAC + documentation, not structural proof | cpg's typed, paginated, schema'd tools (and SEC-01's mutation-tested reachability proof) are a materially stronger safety posture than the generic-passthrough norm — worth explicitly protecting as new tools are added in this milestone |
| **kagent** ([CNCF sandbox](https://github.com/kagent-dev/kagent)) | Full framework: K8s controller + CRDs for agents/tools + separate Python (Google ADK) reasoning engine + OTel tracing, aimed at general cross-tool (Istio/Argo/Prometheus) agentic ops | Confirms the draft's repo-local-only constraint is proportionate — a kagent-style standing controller would be a scope explosion for 5 skills serving one CLI tool; cpg's "just markdown files Claude Code already knows how to load" is the right tier for this milestone |
| **MCP zero-permission-by-default posture** (Microsoft guidance via [Stytch MCP security writeup](https://stytch.com/blog/mcp-security/); "dangerous"-namespace gating pattern in community MCP servers) | Recommends granting zero permissions by default, deliberate opt-in for anything sensitive; some servers gate a whole class of tools behind a single explicit flag so the tools are entirely absent from `tools/list` unless opted in | Directly validates the draft's Design point 1 for AUD-03 ("consent = server launch flag... tool absent from `tools/list` without it") as matching current best practice, regardless of which surface (MCP vs CLI-only) is ultimately chosen |

---

## Gaps to Flag for Phase-Level Research (do not treat as resolved)

- **`cilium-dbg` rename version:** the draft's working assumption is "≥1.14 (`cilium-dbg`), `cilium` before." Multiple community sources found in this research (Cilium cheat sheet, blog secondary sources) instead place the rename at **Cilium 1.15**, not 1.14 — sources conflict and I could not locate the authoritative PR/changelog entry to settle it. This is a load-bearing fact for both the compat floor table and the AUD-03 binary-selection logic — verify against the actual `cilium/cilium` changelog/PR before freezing it into COMPAT-01's table (LOW confidence as researched here).
- **`policy.cilium.io`/`io.cilium.proxy-visibility` annotation status:** current stable docs (`/en/stable/observability/visibility/`) describe only the CiliumNetworkPolicy-based L7 visibility method and make no mention of the annotation-based approach that appears in docs through 1.13. I could not find an explicit deprecation notice (absence of documentation is suggestive, not proof of removal) — this affects the README's existing L7 two-step section independently of this milestone, exactly as flagged in the draft's research question #9 (MEDIUM-LOW confidence; needs a direct check against the current Helm/CRD schema or a changelog entry, not just doc-page absence).
- **Exact introduction versions for the rest of the §3.E floor table** (Verdict_AUDIT/PolicyVerdictNotify encoding, observer gRPC API stability window) were not independently re-derived here — the draft's code-verified facts (§2) are the authoritative source for those and should be trusted over any web search on the topic.
- **Version-detection source of truth** (image tag vs. `CiliumNode` CRD vs. Hubble Relay `ServerStatus` vs. `cilium-dbg version` exec) is an implementation decision, not an ecosystem-feature-landscape question — flagged in the draft's own research questions (§5.7) and correctly left for phase-level/architecture research rather than resolved here.

---

## Sources

- [Cilium — Creating Policies from Verdicts (stable docs)](https://docs.cilium.io/en/stable/security/policy-creation/) — HIGH confidence, official docs, fetched directly
- [Cilium — Deny Policies (stable docs)](https://docs.cilium.io/en/latest/security/policy/deny/) — HIGH confidence, official docs
- [cilium/cilium#35558 — CFP: Clarify default-deny-with-no-rules intent](https://github.com/cilium/cilium/issues/35558) — HIGH confidence, live upstream issue, fetched directly
- [Tigera/Calico — Dry Run: staged network policies](https://www.tigera.io/blog/dry-run-your-kubernetes-network-policies-with-calico-staged-network-policies/) and [Calico Whisker + staged policies](https://www.tigera.io/blog/calico-whisker-staged-network-policies-secure-kubernetes-workloads-without-downtime/) — HIGH confidence, vendor-official blog
- [CNCF — Safely managing Cilium network policies: testing and simulation techniques](https://www.cncf.io/blog/2025/11/06/safely-managing-cilium-network-policies-in-kubernetes-testing-and-simulation-techniques/) — MEDIUM-HIGH confidence, CNCF-published
- [Istio — Mutual TLS Migration](https://istio.io/latest/docs/tasks/security/authentication/mtls-migration/) — HIGH confidence, official docs (via search summary)
- [Cilium — Kubernetes Compatibility](https://docs.cilium.io/en/stable/network/kubernetes/compatibility/) — HIGH confidence, official docs
- [Cilium — cilium-dbg command reference](https://docs.cilium.io/en/stable/cmdref/cilium-dbg/) — HIGH confidence, official docs; rename-version specifics MEDIUM-LOW (see Gaps)
- [Cilium — Observability/Visibility (stable docs)](https://docs.cilium.io/en/stable/observability/visibility/) — MEDIUM confidence (fetched; absence of annotation-based method is suggestive, not a confirmed deprecation notice)
- [Azure mcp-kubernetes](https://github.com/Azure/mcp-kubernetes) — MEDIUM-HIGH confidence, fetched directly, confirms generic-passthrough design
- [kagent (CNCF sandbox)](https://github.com/kagent-dev/kagent) / [CNCF blog](https://www.cncf.io/blog/2025/04/15/kagent-bringing-agentic-ai-to-cloud-native/) — MEDIUM confidence
- [Claude Code — Skill authoring best practices](https://docs.claude.com/en/docs/agents-and-tools/agent-skills/best-practices) — HIGH confidence, official docs
- Skills-vs-subagents decision framework (aggregated from multiple 2026 community writeups: buildthisnow.com, totalum.app) — MEDIUM confidence (community sources, internally consistent with official skill docs)
- [Model Context Protocol Blog — Tool Annotations as Risk Vocabulary](https://blog.modelcontextprotocol.io/posts/2026-03-16-tool-annotations/) — MEDIUM-HIGH confidence
- [Stytch — Securing MCP: Threats & Defenses](https://stytch.com/blog/mcp-security/) — MEDIUM confidence, vendor security blog, corroborated by Microsoft zero-permission guidance cited within
- [Teleport — Just-in-Time Access for Amazon EKS](https://goteleport.com/learn/just-in-time-access-for-amazon-eks/), [telekom/k8s-breakglass](https://github.com/telekom/k8s-breakglass) — MEDIUM-HIGH confidence, vendor/OSS-maintainer sources
- Internal verified facts (Cilium audit-mode mechanics, filter sites, v1.5 assets) — from `.planning/drafts/v1.6-audit-onboarding-and-cpg-agent-tooling.md` §2, treated as ground truth, not re-derived

---
*Feature research for: cpg v1.6 (Audit-Mode Onboarding & cpg-Dedicated Agent Tooling)*
*Researched: 2026-07-22*
