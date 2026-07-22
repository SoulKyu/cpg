# Architecture Research

**Domain:** Go CLI + readonly MCP stdio server — Cilium/Kubernetes network-policy generation from Hubble flows. This is an **integration architecture** for v1.6 (audit-mode onboarding + cpg-agent tooling) against the real, existing cpg codebase — not a greenfield domain survey.
**Researched:** 2026-07-22
**Confidence:** HIGH for all integration points (every claim below is grounded in source read at repo HEAD `2fbef25` + vendored `github.com/cilium/cilium@v1.19.4` + `k8s.io/client-go@v0.35.4`, file:line cited). MEDIUM/LOW called out explicitly where the underlying fact is a design recommendation rather than a verified code fact (marked inline).

---

## System Overview

### Current (v1.5) — verified baseline

```
┌─────────────────────────────── CLI entrypoints (cmd/cpg/main.go) ─────────────────────────────────┐
│  generate.go          replay.go           explain.go            mcp.go                             │
│  (live gRPC)          (offline jsonpb)    (evidence render)      (readonly MCP server)               │
└──────┬───────────────────┬──────────────────────────────────────────┬──────────────────────────────┘
       │                   │                                          │
       ▼                   ▼                                          ▼
┌─────────────────────────────────────┐            ┌──────────────────────────────────────────────┐
│  pkg/flowsource.FlowSource interface │            │  cmd/cpg/mcp_tools.go (registerSessionTools)  │
│   - hubble.Client   (client.go)      │            │  cmd/cpg/mcp_query.go (registerQueryTools)     │
│   - flowsource.FileSource (file.go)  │            │        │                                       │
└──────────────┬───────────────────────┘            │        ▼                                       │
               │ flows                              │  pkg/session.Manager (single-slot lifecycle)   │
               ▼                                    │   capturing → stopped → gone                   │
┌─────────────────────────────────────┐            │   Start / Status / Stop / Shutdown (SESS-05)   │
│ pkg/hubble.Aggregator (classifier    │◄───────────┘        │                                        │
│ gate @ aggregator.go:417) → buckets  │                     ▼ (calls hubble.RunPipeline in goroutine)│
└──────────────┬───────────────────────┘            └──────────────────────────────────────────────┘
               │ policy.PolicyEvent
               ▼
┌─────────────────────────────────────┐
│ pkg/policy.BuildPolicy (flow-driven) │
└──────────────┬───────────────────────┘
               │ fan-out (tee)
      ┌────────┴─────────┬───────────────┐
      ▼                  ▼               ▼
pkg/output.Writer   pkg/evidence.Writer  pkg/hubble.healthWriter
(CNP YAML, atomic)  (per-rule evidence)  (cluster-health.json, atomic)
```

The MCP composition root is `runMCPServer` (`cmd/cpg/mcp.go:79`). **SEC-01** (`cmd/cpg/mcp_audit_test.go`) is a whole-program SSA/RTA reachability proof rooted there: zero K8s write verbs, filesystem writes allowlisted to exactly 5 functions.

### v1.6 additions overlaid (this research's subject)

```
                                   [NEW/MODIFIED — v1.6]
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ cmd/cpg/generate.go, replay.go        --include-audit flag        [MODIFIED]          │
│ cmd/cpg/mcp_tools.go                  include_audit session arg   [MODIFIED]          │
│         │                                                                             │
│         ▼                                                                             │
│ pkg/flowsource.FlowSource interface   3rd bool param               [MODIFIED — sig]   │
│   hubble.Client.StreamDroppedFlows    buildFilters widened         [MODIFIED]         │
│   flowsource.FileSource...            per-line verdict check       [MODIFIED]         │
│         │                                                                             │
│         ▼                                                                             │
│ pkg/hubble.Aggregator                 gate widened + AUD counter   [MODIFIED]          │
│ pkg/hubble.PipelineConfig/RunPipeline IncludeAudit field + VIS-01  [MODIFIED]          │
│         (classifier/dedup/evidence/policy builder: UNCHANGED)                          │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ pkg/bootstrap                (NEW)    default-deny CNP + runbook text generator        │
│         reuses pkg/policy CNP-construction conventions; consumed by:                   │
│         cmd/cpg/bootstrap.go (NEW CLI) + cmd/cpg/mcp_bootstrap.go (NEW readonly tool)  │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ pkg/k8s/version.go            (NEW)   Cilium version detection — v1.2 preflight shape  │
│ pkg/k8s/exec.go                (NEW)  ExecInAgentPod — SPDY sibling of portforward.go  │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ pkg/auditwindow                (NEW)  flip/track/watch/revert orchestration            │
│   consumes: pkg/k8s (exec + version + watch primitive), pkg/policy (naming conventions)│
│   consumed by EITHER:                                                                  │
│     (A) pkg/session.Manager (StartArgs+Stop+Shutdown extension)   [MCP session prop]  │
│     (B) cmd/cpg/audit.go (NEW cobra command, own lifecycle)       [CLI-only]           │
│   — OPEN DECISION, both mapped below, not resolved here                                │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ cmd/cpg/mcp_audit_test.go     SEC-01 gains Property 3 (exec reachability)  [MODIFIED — │
│                                 variant (A) only; variant (B) needs NO SEC-01 change]  │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

---

## Component Responsibilities

| Component | Responsibility today | v1.6 change |
|-----------|----------------------|--------------|
| `pkg/flowsource` (`source.go`, `file.go`) | `FlowSource` interface + offline jsonpb replay source | **Interface signature change** — every implementation touched |
| `pkg/hubble` (`client.go`, `aggregator.go`, `pipeline.go`) | gRPC client, flow aggregation, classifier gate, pipeline orchestration | **Modified** — verdict widening, new counter, new VIS-01-style warning |
| `pkg/policy` (`builder.go`) | Flow-driven CNP construction (`BuildPolicy(ns, workload, flows, ...)`) | **Unchanged** — bootstrap does NOT extend this (see Integration Point 2) |
| `pkg/output` (`writer.go`) | Atomic CNP YAML persistence, SEC-01-allowlisted | **Reused, not extended** — recommended path for bootstrap persistence |
| `pkg/k8s` (`client.go`, `portforward.go`, `preflight.go`, `cluster_dedup.go`) | Kubeconfig load, SPDY port-forward to hubble-relay, L7 pre-flight (ConfigMap/DaemonSet, warn-and-proceed), typed CNP list | **New files added**: `version.go` (COMPAT-02), `exec.go` (pods/exec primitive) |
| `pkg/session` (`manager.go`, `session.go`, `pipeline_config.go`, `paths.go`) | Single-slot `capturing→stopped` lifecycle, SESS-05 bounded cleanup fan-out | **Modified IF variant (A)** — new `StartArgs` fields, revert hook in `Stop`+`Shutdown`, TTL trigger |
| `pkg/dropclass` | O(1) drop-reason taxonomy, `ClassifierVersion` semver constant | **Unchanged** — AUDIT flows carry real drop reasons, classifier already handles them (verified, see Integration Point 1) |
| `cmd/cpg` (`mcp.go`, `mcp_tools.go`, `mcp_query.go`, `mcp_audit_test.go`, `generate.go`, `replay.go`, `main.go`) | Composition root, CLI flags, MCP tool registration, SEC-01 audit | **Modified** — new flags, new tool(s) or new command, SEC-01 evolution |
| `pkg/bootstrap` | — does not exist | **NEW package** — pure, flow-independent CNP skeleton + runbook text generation |
| `pkg/auditwindow` | — does not exist | **NEW package** — audit-window domain orchestration (flip tracking, watch consumption, TTL, revert) |

---

## Integration Point 1 — Verdict-filter widening (`--include-audit` / `include_audit`)

**Confidence: HIGH.** All five sites read directly; line numbers match the draft's verified-facts table exactly.

| # | Site | File:line (verified) | Current behavior | v1.6 change |
|---|------|----------------------|-------------------|-------------|
| 1 | gRPC filter, all-namespaces | `pkg/hubble/client.go:198` | `Verdict: []flowpb.Verdict{flowpb.Verdict_DROPPED}` | Append `Verdict_AUDIT` when flag set |
| 2 | gRPC filter, namespace SourcePod | `pkg/hubble/client.go:209` | same | same |
| 3 | gRPC filter, namespace DestinationPod | `pkg/hubble/client.go:213` | same | same |
| 4 | Replay per-line gate | `pkg/flowsource/file.go:116` | `if f.Verdict != flowpb.Verdict_DROPPED { skip }` | `if !(f.Verdict == DROPPED \|\| (includeAudit && f.Verdict == AUDIT)) { skip }` |
| 5 | Aggregator classifier gate | `pkg/hubble/aggregator.go:417` | `f.Verdict == flowpb.Verdict_DROPPED && f.GetDropReasonDesc() != DROP_REASON_UNKNOWN` | widen the `Verdict ==` half of the condition the same way |

**Choke point not in the draft's table but load-bearing:** `pkg/flowsource.FlowSource` is an **interface** (`source.go:15`):
```go
StreamDroppedFlows(ctx context.Context, namespaces []string, allNS bool) (<-chan *flowpb.Flow, <-chan *flowpb.LostEvent, error)
```
Both implementations (`hubble.Client`, `flowsource.FileSource`) satisfy it. Widening the filter requires **changing this signature** (a 4th parameter, e.g. `includeAudit bool`) — every implementation and the single call site (`pkg/hubble/pipeline.go:171`, `RunPipelineWithSource`) must update in lockstep. This is a small, single-PR, mechanical change (Go compiler enforces completeness), but it is the one part of "5 filter sites" that is actually an **interface contract change**, not a local conditional edit.

**Threading path** (new `PipelineConfig.IncludeAudit bool` field, `pkg/hubble/pipeline.go:43-102`):
```
cmd/cpg/generate.go, replay.go (--include-audit flag)  ─┐
cmd/cpg/mcp_tools.go (startSessionArgs.IncludeAudit)    ─┼─► pkg/session.StartArgs (session.go:134)
                                                          │      ▼ buildPipelineConfig (pipeline_config.go)
                                                          └─► hubble.PipelineConfig.IncludeAudit
                                                                 ▼
                                                          RunPipelineWithSource (pipeline.go:171)
                                                            → source.StreamDroppedFlows(ctx, ns, allNS, cfg.IncludeAudit)
                                                            → agg.SetIncludeAudit(cfg.IncludeAudit)  [new setter,
                                                                                                        mirrors SetL7Enabled
                                                                                                        pattern, pipeline.go:184]
```
This is the **exact existing plumbing shape** already used for `L7Enabled`, `IgnoreProtocols`, `IgnoreDropReasons` (three prior features threaded identically through `StartArgs → PipelineConfig → Aggregator setter`). No new plumbing pattern needs inventing.

**VIS-01-style single warning** (draft §3.A): the existing empty-L7-records warning is a direct template (`pkg/hubble/pipeline.go:345-351`):
```go
if cfg.L7Enabled && stats.FlowsSeen > 0 && agg.L7HTTPCount()+agg.L7DNSCount() == 0 {
    cfg.Logger.Warn("--l7 set but no L7 records observed in window", ...)
}
```
Recommended: add `Aggregator.auditVerdictCount uint64` (incremented in `Run()` alongside the existing `Verdict == DROPPED` branch, mirroring `l7HTTPCount`'s counter shape at `aggregator.go:81-88`), expose via `AuditVerdictCount()`, and add the parallel block: `if cfg.IncludeAudit && stats.FlowsSeen > 0 && agg.AuditVerdictCount() == 0 { cfg.Logger.Warn(...) }`.

**Verified unchanged (per draft, independently confirmed by reading `aggregator.go`):** `dropclass.Classify()`, dedup (`pkg/policy/dedup.go`), evidence writer, and `policy.BuildPolicy` all key off `DropReasonDesc`, which `buildDropEvent` (`aggregator.go:256-277`) and the bucket key derivation read identically regardless of `Verdict` value — no changes needed downstream of the gate itself.

---

## Integration Point 2 — Bootstrap artifact generation: new package, not a `pkg/policy` extension

**Recommendation: new `pkg/bootstrap` package. Confidence: HIGH on the "don't extend builder.go" half, MEDIUM on the exact package name/shape (design synthesis).**

**Why not `pkg/policy`:** `policy.BuildPolicy(namespace, workload string, flows []*flowpb.Flow, tracker FlowTracker, opts AttributionOptions)` (`builder.go:91`) is **flow-driven** end to end — every downstream helper (`groupFlows`, `buildIngressRules`, `recordL7`, attribution) exists to derive rules FROM observed flows. Bootstrap's default-deny CNP has **zero flow input** — it is a static skeleton (`EnableDefaultDeny: DefaultDenyConfig{&ingress, &egress}` with empty `Ingress`/`Egress` rule lists). Cramming a flow-independent generator into `builder.go` conflates two different generation contracts and drags in `FlowTracker`/`AttributionOptions` parameters that don't apply.

**What `pkg/bootstrap` should reuse from `pkg/policy` (verified conventions, `builder.go:90-105`):**
- `ciliumv2.CiliumNetworkPolicy{TypeMeta{APIVersion: "cilium.io/v2", Kind: "CiliumNetworkPolicy"}, ObjectMeta{Name: ..., Namespace: ..., Labels: ...}}` construction shape.
- `policy.PolicyName(workload string) string` naming convention is workload-scoped (`"cpg-" + workload`); bootstrap is namespace-scoped, so it needs its **own** name function (e.g. `"cpg-bootstrap-default-deny"`) — do not force-fit `PolicyName`.
- `EnableDefaultDeny DefaultDenyConfig` is a real, vendored field — verified at `github.com/cilium/cilium@v1.19.4/pkg/policy/api/rule.go:33,138,200` (`type DefaultDenyConfig struct { Ingress, Egress *bool }`, embedded as `Rule.EnableDefaultDeny`). No new Cilium API surface to learn; it's already reachable via the same `ciliumv2`/`api` imports `builder.go` already uses.

**Persistence — the SEC-01-relevant decision:** the draft says the MCP tool writes "only into the session tmpdir" and calls this "SEC-01-compatible as-is (auto-covered by the reachability audit)." Verified this is **only true if bootstrap does not add a NEW filesystem-write call site.** SEC-01's Property 2 (`mcp_audit_test.go:79-118`) is an **exact 5-function allowlist** keyed by SSA symbol — a brand-new writer function (even one performing the identical atomic temp+rename shape as `pkg/output/writer.go:81-102`) is **not automatically covered**; a human must add a 6th allowlist entry for the audit to keep passing, and that entry only proves the write is *contained*, not that the mechanism required zero changes.

Two options, both structurally sound, different SEC-01 cost:
1. **(Recommended) Pure in-memory return, no new write at all.** `pkg/bootstrap.BuildDefaultDenyCNP(namespace string, gate CompatGate) *ciliumv2.CiliumNetworkPolicy` + `BuildRunbook(namespace string, cnp *ciliumv2.CiliumNetworkPolicy, includeAuditCmd string) string` marshal via `sigs.k8s.io/yaml` (already imported by `pkg/output/writer.go:12`) and return the YAML/text as MCP `structuredContent`/CLI stdout — **zero new filesystem write, zero new SEC-01 allowlist entry, exactly matching how `get_policy`/`get_evidence` already return content (QRY-02/QRY-03 in PROJECT.md) without themselves writing anything new.**
2. Persist into the tmpdir by **reusing** the existing allowlisted `(*output.Writer).Write` — construct a synthetic `policy.PolicyEvent{Namespace: ns, Workload: "bootstrap-default-deny", Policy: cnp}` and call the writer that's already in `fsWriteAllowlist` (`mcp_audit_test.go:108`). This still requires zero new allowlist entries but produces a real on-disk artifact under the session tmpdir (consistent with the draft's literal wording).

Either is compatible with the readonly claim; option 1 is the cleanest "auto-covered" story and should be the default recommendation carried into requirements.

**CLI + MCP surface (both, per draft — not an open decision, this half is settled):**
- `cmd/cpg/bootstrap.go` (new file, parallel structure to `generate.go`/`replay.go`): `cpg bootstrap -n <ns>` prints CNP YAML + runbook text (or writes to `--output`).
- `cmd/cpg/mcp_bootstrap.go` (new file, parallel structure to `mcp_query.go`'s per-concern file split — e.g. `mcp_query_flows.go`, `mcp_query_evidence.go`): a new **readonly** tool (e.g. `get_bootstrap_policy`), `Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}` — genuinely truthful under option 1 above.

**Depends on Integration Point 5 (version detection)** for the `enableDefaultDeny` vs legacy-form branch (draft §3.E design point 3) — `pkg/bootstrap` needs a capability input, not a hardcoded assumption.

---

## Integration Point 3 — Managed audit window: both surface variants mapped

**This is an explicit OPEN DECISION in the draft (§3.C). Both variants integrate cleanly; they are NOT equally cheap — see the SEC-01 cost delta at the end of this section, which is itself decision-relevant information.**

### Shared substrate (needed by both variants)

- **New `pkg/k8s/exec.go`**: `ExecInAgentPod(ctx context.Context, config *rest.Config, nodeName string, command []string, logger *zap.Logger) (stdout string, err error)`. Verified building blocks, all already-vendored dependencies (**zero new external dependency**):
  - Same SPDY plumbing `portforward.go` already uses: `k8s.io/client-go/transport/spdy.RoundTripperFor` (`portforward.go:51`), fluent REST builder `clientset.CoreV1().RESTClient().Post().Resource("pods").Namespace(...).Name(...).SubResource(...)` (`portforward.go:44-49`, swap `"portforward"` for `"exec"`).
  - Executor: `k8s.io/client-go/tools/remotecommand.NewSPDYExecutor(config, "POST", url) (Executor, error)`, then `Executor.StreamWithContext(ctx, StreamOptions{...})` — confirmed present at the exact pinned version (`k8s.io/client-go v0.35.4`, `go.mod:24`; `StreamWithContext` verified in `tools/remotecommand/remotecommand.go` of that cached module version — ctx-cancellable, fits this codebase's ctx-everywhere convention).
  - **Pod selection is NOT a copy of `findRelayPod`.** `findRelayPod` (`portforward.go:105-125`) finds *one* pod by label selector across a namespace. Reaching `cilium-dbg` requires finding the cilium-agent pod running on **the specific node** hosting the target workload's pod — i.e. list the `cilium` DaemonSet's pods (`kube-system`, label e.g. `k8s-app=cilium`) and match `pod.Spec.NodeName` against the target endpoint's node. This is genuinely new selection logic, not a drop-in reuse, even though the SPDY/exec transport underneath is a sibling of the port-forward code.
- **RBAC step-up (verified via Kubernetes RBAC docs, not training-data guess):** `pods/exec` is a **distinct subresource** requiring its own rule — `get`/`list` on `pods` does **not** imply exec access:
  ```yaml
  rules:
  - apiGroups: [""]
    resources: ["pods/exec"]
    verbs: ["create"]
  ```
  This is the exact "real privilege step-up" the draft flags (§3.C design point 3) — README/RBAC documentation needs this literal rule, distinct from the existing read/portforward verbs `cpg` already documents.
- **New `pkg/auditwindow` package** — domain orchestration, consuming `pkg/k8s` primitives:
  - Tracks the "ours" set (endpoint IDs this session flipped — draft's "revert-only-ours bookkeeping").
  - Watch-and-flip-new-pods loop (Integration Point 4).
  - TTL timer.
  - `Revert(ctx) error` — the single function both surface variants call on every exit path.

### Variant A — MCP session property (`start_session {audit_bootstrap, audit_ttl}`)

| Touch point | File | Change |
|---|---|---|
| Server launch consent gate | `cmd/cpg/mcp.go` (`newMCPCmd`) | new `--enable-audit-bootstrap` cobra flag, threaded into `runMCPServer` |
| Session args | `cmd/cpg/mcp_tools.go` (`startSessionArgs`) | new `AuditBootstrap bool`, `AuditTTL string` fields |
| Validated args | `pkg/session/session.go` (`StartArgs`) | new `AuditBootstrap bool`, `AuditTTL time.Duration` fields (same `parseOptionalDuration` validation shape already used for `Timeout`/`FlushInterval`, `mcp_tools.go:65-80`) |
| Setup orchestration | `pkg/session/manager.go` (`resolveSetup`, `Start`) | new step alongside `PortForwardToRelay`/`LoadClusterPoliciesForNamespaces` (`manager.go:288-313`) — calls into `pkg/auditwindow` to flip the namespace's current endpoints and start the watch loop. Recommend a new injectable seam field on `Manager` (`auditWindowFn`), mirroring the existing `resolveSetupFn`/`runPipeline` test-seam pattern (`manager.go:58-64`) already used for testability. |
| **Revert — the SESS-05 fan-out extension** | `pkg/session/manager.go` (`Stop`, `Shutdown`) | **Both** functions need the hook — they are genuinely separate code paths today (`Stop` at `manager.go:393-447` is the explicit `stop_session` tool call; `Shutdown` at `manager.go:455-493` is the process-exit fan-out called once from `cmd/cpg/mcp.go:105`). A shared `revert()` helper must be called from both, plus a **third, independent trigger**: the TTL timer firing on its own goroutine (calling the same revert path, most simply by invoking `Manager.Stop(id)` internally). This is a 3-trigger convergence, not a 1-line hook — worth flagging precisely since "hook into SESS-05" undersells the actual fan-in required. |
| Result surface | `pkg/session/session.go` (`StartResult`, `StatusResult`, `StopResult`) | new fields: audit-window state, flipped-endpoint count, revert outcome |

**Schema-gating nuance (draft internal tension, not resolved here — flag for discuss-phase):** draft design point 1 says "without [the flag], the server is bit-identical to v1.5 (tool absent from `tools/list`)," but design point 2 says the mutation is "a session property" on the **existing** `start_session` tool, not a new tool. Since `mcp.AddTool` registration happens once at `runMCPServer` startup (`mcp.go:94-96`), "tool absent from `tools/list`" cannot apply to `start_session` itself (it must always exist — v1.5 baseline). The two readings that reconcile this: (i) `registerSessionTools` takes the launch flag and registers a **different `startSessionArgs` schema variant** when the flag is present (fields only appear in the tool's declared schema when enabled), or (ii) the fields are always in the schema but the **handler** rejects `audit_bootstrap: true` with an error unless the server was launched with the flag (schema shape is identical either way; only accepted values differ, which is arguably not "bit-identical tools/list" in spirit but is in literal schema bytes). Both are implementable; this is a small, contained design question for `/gsd-discuss-phase`, not an architecture blocker — flagging it here because it does NOT change the SEC-01 analysis below (reachability is compiled-in either way, see Integration Point 6).

### Variant B — CLI-only (`cpg audit enable|disable -n <ns> --watch --ttl`)

| Touch point | File | Change |
|---|---|---|
| New command | `cmd/cpg/audit.go` (new file, parallel structure to `generate.go`) | `cpg audit enable -n <ns> --watch --ttl 30m`, `cpg audit disable -n <ns>`; own `signal.NotifyContext` + deferred revert, mirroring `generate.go`'s existing Ctrl-C handling shape (`generate.go:162-163`) |
| Root wiring | `cmd/cpg/main.go` | `rootCmd.AddCommand(newAuditCmd())` alongside the existing four (`main.go:57-60`) |
| MCP surface | **none** | `pkg/bootstrap`'s runbook (Integration Point 2) prints the exact CLI command for the human to run — the MCP server never touches `pkg/auditwindow` |
| `pkg/session` | **untouched** | no `StartArgs`/`Manager` changes at all |

### The decision-relevant asymmetry (verified, not a guess)

**Variant B requires zero SEC-01 changes.** The mutating code lives in a sibling cobra command wired at `main()` (`main.go:57-60`), never called from `runMCPServer`. SEC-01's RTA/BFS walk (`mcp_audit_test.go:222-336`) is rooted at `runMCPServer` and never even loads `audit.go` into its reachable set — the existing test continues to prove exactly what it proves today, unchanged, because the new code is structurally outside its scan.

**Variant A requires a new SEC-01 property** (Integration Point 6) because the mutation code becomes part of `runMCPServer`'s transitive closure regardless of the runtime flag. This is real, non-trivial engineering (not a "just re-run the existing audit" task) — see below.

This asymmetry is exactly the kind of fact the draft's own framing ("the choice is about what the flagship 'readonly MCP' claim should remain") is gesturing at, now made concrete: variant B keeps the MCP composition root **literally unchanged and re-provably readonly with the existing test**; variant A keeps everything inside one coherent LLM-driven session lifecycle (arguably better UX, matches the "structurally cannot leave a cluster in audit" safety argument in the draft) but spends real audit-engineering effort to keep the "provable" claim honest.

---

## Integration Point 4 — Watcher placement ("new endpoint in namespace")

**Confidence: HIGH on both primitives being already-available dependencies; MEDIUM on which one to prefer (explicitly an open research question in the draft, §5.2 — not resolved here, but the trade-off is now verified rather than assumed).**

Both candidate primitives are **already vendored, zero new dependency**:
- Plain pod watch: `k8s.io/client-go/informers/core/v1` (pinned `client-go v0.35.4`) — standard `SharedInformerFactory`, gives `pod.Spec.NodeName` directly.
- `CiliumEndpoint` CRD watch: `github.com/cilium/cilium/pkg/k8s/client/informers/externalversions/cilium.io/v2` (`ciliumendpoint.go`) — already reachable since `ciliumclient "github.com/cilium/cilium/pkg/k8s/client/clientset/versioned"` is already a direct dependency (`pkg/k8s/cluster_dedup.go:8`).

**Verified field-level difference that matters for this specific use case:** `CiliumEndpoint.Status` (`github.com/cilium/cilium@v1.19.4/pkg/k8s/apis/cilium.io/v2/types.go:36-56,335-343`) carries **both**:
- `Status.ID int64` — "the cilium-agent-local ID of the endpoint" — this is the exact identifier `cilium-dbg endpoint config <ID> PolicyAuditMode=Enabled` needs.
- `Status.Networking.NodeIP string` — the node the endpoint runs on.

A plain Pod watch gives `pod.Spec.NodeName` (node mapping) but **not** the cilium-agent-local endpoint ID — that would require a *second* round trip (e.g. `cilium-dbg endpoint list` inside the agent pod, matched by pod IP) to resolve the ID the flip command actually needs. A `CiliumEndpoint` watch gives **both pieces of information the flip operation needs in one object**, at the cost of depending on Cilium's own CRD lifecycle (an endpoint's `CiliumEndpoint` object may lag actual pod readiness during startup — the latency characteristic the draft's research question 2 explicitly asks about).

**Placement recommendation (design synthesis, MEDIUM confidence):** the watch **primitive** (informer setup, "here's a channel of endpoint-appeared events") belongs in `pkg/k8s` — parallel to the existing precedent that `pkg/k8s` already hardcodes feature-specific pod-finding logic for a single consumer (`findRelayPod`, `portforward.go:105-125`, hardcoded to `hubble-relay`). The **orchestration** (deciding to flip a newly observed endpoint, updating the "ours" bookkeeping, respecting TTL) belongs in `pkg/auditwindow`, which subscribes to the `pkg/k8s` primitive. This mirrors the existing separation where `pkg/k8s` provides connectivity and `pkg/session.Manager` (the existing analogous orchestrator) drives it.

---

## Integration Point 5 — Cilium version detection

**Confidence: HIGH on placement and pattern reuse; LOW on the exact per-feature introduction versions (the draft itself flags these as researcher-pinned in §3.E, and no code in this repo currently encodes them — genuinely new territory, confirmed by `ls pkg/k8s/ | grep -i version` returning nothing).**

**Placement: new `pkg/k8s/version.go`**, not a new package — this is the *same domain* (K8s connectivity + advisory cluster checks) as the existing `pkg/k8s/preflight.go`, just reading a different resource. The v1.2 pattern to replicate exactly (verified, `preflight.go:58-119`):
- Read-only client call (ConfigMap/DaemonSet `Get`), never a `List`+heavy call.
- Three-way branch: success → check value; `apierrors.IsForbidden` → warn with the exact required permission named, proceed; `apierrors.IsNotFound` / any other error → warn, proceed.
- **Never returns an error that blocks the pipeline** — advisory only, "warn-and-proceed," explicitly to avoid locking out reduced-RBAC CI service accounts (this rationale is stated verbatim in `preflight.go:51`, and the draft explicitly asks for "the exact v1.2 pre-flight pattern").
- Invoked once per invocation from the CLI composition root, exactly like `maybeRunL7Preflight` is invoked once from `generate.go:223` (and deliberately never from `replay.go`, since replay is offline).

**Candidate source (from draft §5.7, now narrowed by what's already available without new privilege):** `kube-system` `ds/cilium` DaemonSet image tag is reachable with the **same RBAC tier `cpg` already needs** for `preflight.go` (read `daemonsets` in `kube-system` — `RunL7Preflight` already calls `client.AppsV1().DaemonSets(ciliumNamespace).Get(...)` at `preflight.go:101` for the unrelated `cilium-envoy` check) — i.e. version detection can piggyback on a **permission tier already granted for L7 pre-flight**, not a new privilege ask. The other candidates the draft lists (Hubble Relay `ServerStatus`, `CiliumNode` CRD, `cilium-dbg version` via exec) all require either a new gRPC call shape or the exec privilege from Integration Point 3 — the DaemonSet-image-tag route is the only one that needs **zero new RBAC**, which is a strong practical argument for it as the primary source, with the others as fallback/cross-check. (This preference is a reasoned recommendation from verified RBAC-tier facts, not itself independently verified against a running cluster — MEDIUM confidence.)

**Consumption points (threading, once detected):**
- `pkg/session.Manager.resolveSetup` (`manager.go:276-316`) is the natural call site — parallel to where it already calls `k8s.LoadKubeConfig()` / `k8s.PortForwardToRelay` — and can thread the result into `StartResult` (session.go:152-160) alongside `dropclass.ClassifierVersion` (already a plain exported string constant, `pkg/dropclass/version.go:5`) per draft design point 3 ("surface the pairing").
- `pkg/bootstrap`'s CNP builder (Integration Point 2) needs the detected version/capability as an input parameter to choose `enableDefaultDeny` vs. a documented legacy form.
- `pkg/auditwindow`'s exec-command construction needs it to pick `cilium-dbg` vs `cilium` as the binary name.
- CLI equivalent: `generate.go`/`bootstrap.go`/`audit.go` would each call the same `pkg/k8s` function directly, mirroring how `maybeRunL7Preflight` is called from `generate.go` today.

---

## Integration Point 6 — SEC-01 evolution: the reachability-is-static nuance

**Confidence: HIGH on the gap analysis (directly derived from reading `mcp_audit_test.go`'s exact matching rules against the exact `client-go` exec API shape). MEDIUM on the recommended fix shape (design synthesis for the phase implementer to validate).** This is the least obvious and most load-bearing finding in this document, and it directly answers draft research question 5.

### The gap, precisely

SEC-01 today has exactly two detection properties (`mcp_audit_test.go:55-118`, `296-335`):
- **Property 1** — matches SSA calls where `common.IsInvoke() && common.Method != nil`, method name in `{Create, Update, Patch, Delete, Apply, DeleteCollection, UpdateStatus, ApplyStatus}` — i.e. **interface-dispatch calls on a typed clientset's named write verbs**.
- **Property 2** — matches SSA calls where `common.StaticCallee()` is non-nil and its symbol is in the `disallowedFSWrite` set of `os.*` functions, gated by an exact function-name allowlist.

The `pods/exec` mechanism (verified shape, Integration Point 3) is built from:
1. A fluent REST builder chain (`.Post().Resource("pods").Namespace().Name().SubResource("exec")...`) — none of these method names are in the K8s-write-verb set. **This is not a hypothetical blind spot: the identical fluent-builder shape already exists today for the `"portforward"` subresource (`portforward.go:44-49`) and is reachable from `runMCPServer` right now, yet SEC-01 passes with zero K8s-write hits** — direct, already-verified proof that this call shape is invisible to Property 1.
2. `remotecommand.NewSPDYExecutor(...)` — a static top-level function call, so `common.StaticCallee()` would be non-nil — but its symbol is not in `disallowedFSWrite` (that set is FS-write-specific, not K8s-mutation-specific), so **Property 2 doesn't fire either** (it isn't designed to; it was never meant to catch this).
3. `Executor.StreamWithContext(...)` — an interface-dispatch call, so Property 1's matcher looks at it — but `"StreamWithContext"`/`"Stream"` are not in `k8sWriteVerbs`, so it evades Property 1 too.

**Conclusion: the entire exec mechanism is structurally invisible to both existing properties, regardless of which audit-window surface variant is chosen and regardless of runtime flag state.** Simply re-running `TestMCPAuditReadonlyReachability` against a build that includes variant A's code would report "PASS, zero K8s writes" even though a real mutation path exists — a false sense of security, not a proof.

### A second, independent structural fact worth stating plainly

SSA/RTA reachability is a **static, whole-program property** — it answers "can this function possibly be called," not "is this runtime flag true right now." A `if opts.EnableAuditBootstrap { registerAuditTools(server, mgr) }` guard inside `runMCPServer` does **not** remove `registerAuditTools` (or anything it calls) from the RTA callgraph — Go's static analysis sees the call site regardless of the boolean. This means **mode (a)'s "zero write verbs reachable" claim, if compiled in variant A, is trivially and permanently true today only because Property 1/2 can't see the exec path at all** — it is not evidence that the flag-off mode is actually safer than flag-on at the reachability level. Any "two-mode" proof that relies on re-running the *existing* test twice would prove nothing new.

### Recommended shape (design synthesis — for the phase implementer to refine, not gospel)

Add a **Property 3**, independent of the flag-off/flag-on framing entirely: an explicit reachability + call-path assertion targeting the exec construction (matching `common.StaticCallee()` against the exec-executor constructor symbol, e.g. `k8s.io/client-go/tools/remotecommand.NewSPDYExecutor`, exactly the same static-callee-matching technique Property 2 already uses — no new SSA technique needed, just a new watched symbol). Then:
- Assert the exec-executor constructor **is** reachable from `runMCPServer` (if it weren't, the feature doesn't exist).
- Reuse the existing `callPathFrom` helper (`mcp_audit_test.go:167-181`, already built for exactly this diagnostic) to reconstruct the call path and assert it passes **only** through the expected entry point (the `audit_bootstrap`-handling branch of `start_session`, or a dedicated tool handler) — and fail loudly if any *other* cpg-owned function (e.g. `get_status`, `list_policies`) also reaches it.
- Keep Properties 1 and 2 exactly as they are today (they continue to prove what they've always proven, and continue to hold at zero) — the "two modes" the draft describes are best understood as: **(a) the two existing properties, unchanged, still zero** + **(b) the new Property 3, an allowlist-of-one-path rather than a zero-tolerance check**, together forming the "two-mode" story — not two separately-compiled binaries.

Under **variant B (CLI-only)**, none of this is needed: SEC-01 requires **zero modification**, because the mutating code never enters `runMCPServer`'s transitive closure at all (a sibling cobra command, wired at `main()`, is invisible to a BFS rooted at `runMCPServer` — this is the same reason today's audit never scans `generate.go`'s or `replay.go`'s code paths).

### A note on the "one mutation tier above" future extension

The draft mentions CNP `Create`/`Delete` by cpg itself as a possible later extension "one mutation tier above" endpoint-config flips. Verified: `ciliumclient "github.com/cilium/cilium/pkg/k8s/client/clientset/versioned"` is **already a direct dependency** (`pkg/k8s/cluster_dedup.go:8`, used today for read-only `List`). A future typed `Create`/`Delete` call on that clientset **would** trip Property 1 immediately (it's a named write verb on a typed interface) — confirming the draft's own instinct that this tier is meaningfully more dangerous *and* (reassuringly) that SEC-01 would actually catch it without modification, unlike the exec tier.

---

## Suggested Build Order

Dependency-aware, keyed to the draft's own candidate REQ IDs. AUD-01 first is correct and here is why, plus what follows:

1. **AUD-01 (`--include-audit` ingestion).** Zero new packages, touches only existing files in their existing plumbing pattern (Integration Point 1). No dependency on anything else in this milestone. **Ship first** — without it, every later artifact (bootstrap runbook telling the operator to run `cpg generate --include-audit`, any audit-window value proposition) has nothing to observe.
2. **AUD-04 part (a) confirmation.** Re-run `TestMCPAuditReadonlyReachability` after AUD-01 lands — should stay green with zero modification (pure verdict-filter logic introduces no new K8s/FS write call). Cheap to confirm immediately, establishes the regression baseline before touching SEC-01 for real in step 5/6.
3. **COMPAT-02 (runtime version detection, `pkg/k8s/version.go`).** No dependency on AUD-02/03's code, but AUD-02 and AUD-03 both **need its output** (capability gate) to be honest about `enableDefaultDeny` vs legacy form and `cilium-dbg` vs `cilium`. Sequence it before or alongside AUD-02, not after. COMPAT-01 (the README matrix) is a documentation deliverable that can trail once the per-feature floors are pinned by whoever answers draft research question 8 — no code dependency either way.
4. **AUD-02 (bootstrap artifact generation).** Depends on COMPAT-02 (for gating) and conceptually on AUD-01 (the runbook text references it). New `pkg/bootstrap`, reusing `pkg/policy` conventions and (recommended) returning content rather than writing new files — genuinely "readonly, uncontroversial" as the draft claims, **if** the persistence choice in Integration Point 2 is followed. Independent of AUD-03's mutation logic entirely — can ship before or in parallel with it.
5. **Surface decision (discuss-phase blocker).** AUD-03 cannot be scoped into phases until variant A vs B is chosen — the two variants touch almost entirely different files (`pkg/session/*` vs a new `cmd/cpg/audit.go`) and, per Integration Point 6, carry a **materially different SEC-01 cost**. This decision should land before phase planning assigns file-level work for AUD-03/AUD-04(b).
6. **AUD-03 (managed audit window)** + **AUD-04 part (b) (SEC-01 Property 3, variant A only).** Shared substrate first (`pkg/k8s/exec.go`, watcher primitive — Integration Points 3/4), then `pkg/auditwindow` orchestration, then the chosen surface's wiring. If variant A: build Property 3 test infrastructure *alongside* the feature, not after — the draft's own e2e harness (`cmd/cpg/mcp_e2e_test.go`, already exercising graceful/ungraceful lifecycle) is the natural place to add revert-path e2e cases (TTL expiry, ungraceful disconnect mid-audit-window), reusing its existing fake-relay/D-07-bypass infrastructure rather than inventing new test scaffolding.
7. **SKL-01..05 (cpg-local skills/agents).** Pure `.claude/skills/cpg-*/SKILL.md` (+ optional `.claude/agents/cpg-*.md`) additions — zero `pkg/`/`cmd/` integration, no compile dependency on anything above. Can be authored any time AUD-01 (for `cpg-triage`) or the full workflow (for `cpg-audit-onboard`) exists to script against. Sequence last only because the skills need real, working commands/tools to reference — not because of a technical dependency.

---

## Anti-Patterns to Avoid (specific to this milestone)

### Anti-Pattern 1: Assuming a runtime flag prunes static reachability
**What people do:** gate a new MCP mutation tool behind `if enableFlag { registerTool(...) }` and assume a reachability-based security test "sees" the flag.
**Why it's wrong:** RTA/SSA analysis (as SEC-01 already implements) proves what code *can* be called, not what runs at a given moment — verified in Integration Point 6.
**Instead:** either keep the mutation code entirely outside the audited composition root (variant B), or add an explicit, path-specific Property 3 assertion that doesn't depend on the flag's runtime value at all.

### Anti-Pattern 2: Treating "sibling of existing SPDY code" as "sibling of existing pod-selection logic"
**What people do:** assume `pods/exec` is a copy-paste of `PortForwardToRelay`'s pod lookup because both use SPDY.
**Why it's wrong:** `findRelayPod` selects one pod by label selector across a namespace; reaching `cilium-dbg` requires selecting the cilium-agent pod **on a specific node**, a different selection axis entirely (verified, Integration Point 3).
**Instead:** write new node-scoped pod-selection logic; reuse only the SPDY transport/executor plumbing.

### Anti-Pattern 3: Extending `policy.BuildPolicy` to handle a "no flows" bootstrap case
**What people do:** add an `if len(flows) == 0` branch or optional flows parameter to the existing flow-driven builder to also emit a default-deny skeleton.
**Why it's wrong:** conflates two different generation contracts (flow-derived allow rules vs. static default-deny skeleton) inside one function whose entire existing test suite (`builder_test.go`, `builder_l7_test.go`, `builder_attribution_test.go`) assumes flow-driven behavior.
**Instead:** new `pkg/bootstrap` package, reusing only construction *conventions* (verified in Integration Point 2), not the function itself.

### Anti-Pattern 4: Letting the bootstrap tool invent a new filesystem-write function
**What people do:** write a new `(*bootstrap.Generator).WriteRunbook` that does its own `os.MkdirAll`/`os.WriteFile`/rename into the session tmpdir.
**Why it's wrong:** SEC-01's Property 2 is an exact 5-function allowlist — this silently becomes a 6th entry a human must remember to add, and undermines the "auto-covered" claim in the draft.
**Instead:** return content directly (no write), or reuse the already-allowlisted `(*output.Writer).Write` (verified, Integration Point 2).

---

## Scaling Considerations

This is an operator-driven CLI/MCP tool, not a multi-tenant service — "scale" here means "size of the audit-onboarding blast radius," not concurrent users.

| Concern | Single namespace, few pods | Namespace with many pods/rapid churn | Cluster-wide audit rollout (out of scope this milestone) |
|---------|------------------------------|----------------------------------------|-------------------------------------------------------------|
| Endpoint flip fan-out | One-shot list + flip, trivial | Watch loop must keep up with pod churn during the window — TTL bounds worst case | Not attempted — draft explicitly scopes to per-namespace synthesis, no native cluster-wide audit scope exists in Cilium itself |
| Exec calls | Sequential is fine | Consider bounding concurrency (a semaphore, matching the existing bounded-fan-out style already used in `pkg/session`'s `stopWait`/`removeWait`) so a slow/unreachable node doesn't stall the whole window | N/A |
| Revert bookkeeping | Small in-memory set | Same set, larger — no different data structure needed, just correctness under concurrent watch-driven appends (needs the same mutex discipline `pkg/session.Manager` already models) | N/A |

### Scaling priorities
1. **First real risk:** a node's cilium-agent pod being unreachable (network partition, pod restarting) mid-flip — the revert path must not block on it indefinitely; apply the same bounded-wait pattern already proven in `pkg/session.Manager.Shutdown` (`manager.go:483-492`, independent `removeWait` timeout so one wedged step can't block the rest).
2. **Second:** watch latency (draft research question 2) — a pod that goes ready before its flip lands is briefly enforced cold; this is a correctness/UX concern, not a scale concern, and is exactly why the draft frames TTL as "belt-and-suspenders" rather than the primary safety mechanism.

---

## Sources

**cpg source (this repo, verified at HEAD `2fbef25`, post-v1.5):**
- `pkg/flowsource/source.go:15` (FlowSource interface), `file.go:72-152` (replay gate)
- `pkg/hubble/client.go:53-217` (gRPC client, buildFilters), `aggregator.go:70-460` (Aggregator, classifier gate, counters), `pipeline.go:42-420` (PipelineConfig, RunPipeline/RunPipelineWithSource, VIS-01 pattern)
- `pkg/policy/builder.go:1-105` (BuildPolicy, CNP construction shape, PolicyName), `pkg/policy/CLAUDE.md`
- `pkg/output/writer.go:1-105` (atomic write shape, SEC-01-allowlisted)
- `pkg/k8s/client.go`, `portforward.go:1-126` (SPDY pattern, findRelayPod), `preflight.go:1-119` (v1.2 warn-and-proceed pattern), `cluster_dedup.go:1-62` (typed clientset already a dependency)
- `pkg/session/manager.go:1-500` (Start/Status/Stop/Shutdown, SESS-05 fan-out, resolveSetupFn seam pattern), `session.go:1-259` (StartArgs/StartResult/StopResult), `pipeline_config.go:1-109` (buildPipelineConfig threading), `paths.go:1-57` (DeriveSessionPaths)
- `pkg/dropclass/version.go` (ClassifierVersion)
- `cmd/cpg/main.go:1-104` (rootCmd assembly), `generate.go:1-278`, `replay.go:1-134` (CLI plumbing, maybeRunL7Preflight), `mcp.go:1-141` (runMCPServer composition root), `mcp_tools.go:1-167` (registerSessionTools, startSessionArgs, parseOptionalDuration), `mcp_query.go` (registerQueryTools structure), `mcp_audit_test.go:1-337` (SEC-01 full mechanism — Properties 1/2, allowlists, BFS/RTA machinery, callPathFrom), `mcp_e2e_test.go`/`mcp_harness_test.go` (e2e harness, D-07 server bypass)

**Vendored dependencies (confirmed present at the exact pinned versions in `go.mod`):**
- `github.com/cilium/cilium@v1.19.4`: `pkg/policy/api/rule.go:31-33,138,200` (DefaultDenyConfig/EnableDefaultDeny), `pkg/option/config.go:862-863,1672-1675,2470` (PolicyAuditMode/PolicyAuditModeArg), `pkg/option/endpoint.go` (endpoint-mutable option library incl. PolicyAuditMode), `pkg/k8s/apis/cilium.io/v2/types.go:36-56,335-343` (CiliumEndpoint.Status.ID + Networking.NodeIP), `pkg/k8s/client/informers/externalversions/cilium.io/v2/ciliumendpoint.go` (CiliumEndpoint informer already available), `pkg/k8s/client/clientset/versioned` (typed clientset already a direct dependency)
- `k8s.io/client-go@v0.35.4` (`go.mod:24`): `tools/remotecommand/remotecommand.go` (NewSPDYExecutor, Executor interface, StreamWithContext), `informers/core/v1/pod.go` (Pod informer already available)

**External verification:**
- [Using RBAC Authorization | Kubernetes](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) — confirms `pods/exec` is a distinct subresource requiring its own `resources: ["pods/exec"], verbs: ["create"]` rule, independent of `pods` read verbs.

---
*Architecture research for: cpg v1.6 (Audit-Mode Onboarding & cpg-Dedicated Agent Tooling)*
*Researched: 2026-07-22*
