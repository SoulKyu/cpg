# Phase 21: Cilium Compatibility Matrix + Runtime Detection - Research

**Researched:** 2026-07-22
**Domain:** Cilium/Kubernetes version-compatibility documentation + privilege-neutral runtime version detection, in an existing Go CLI + readonly MCP stdio server
**Confidence:** HIGH

## Summary

No `CONTEXT.md` exists for this phase — `/gsd-discuss-phase` has not run, so there are no locked user decisions to carry forward. Every recommendation below is a research finding or a design synthesis; product-level calls (the exact declared floor number, the mixed-version-cluster reduction strategy) are flagged explicitly for the planner or discuss-phase to confirm.

The milestone-level research (`.planning/research/{SUMMARY,PITFALLS,FEATURES,ARCHITECTURE}.md`) already pre-verified three version pins via merged-PR-plus-release-tag archaeology (`cilium-dbg` rename = 1.15, `enableDefaultDeny` = 1.16, `policy.cilium.io/proxy-visibility` removed = 1.17) and left two items explicitly open: the `Verdict_AUDIT`/`PolicyVerdictNotify` introduction vintage, and the observer gRPC API's compatibility window. This research closes both using the identical verification method (`gh pr view`, `gh api .../releases/tags/...`), plus goes one step further than the milestone research on Tension 1 (which signals of the version-detection source of truth are trustworthy): rather than leaving "confirm the exact field semantics against a real cluster first" as an open action item, this session had a live, reachable cluster available and used it — read-only `kubectl get` plus a bounded, cleanly-torn-down `kubectl port-forward` + `grpcurl` session against the target repo's own dev/test cluster (Cilium v1.19.2/v1.19.3, 83 nodes, an **actively in-progress rolling upgrade** at the time of research). That live data, cross-checked against cpg's own vendored source for `ObserverClient.ServerStatus`/`GetNodes`, produces a materially sharper, evidence-backed recommendation than either milestone research file proposed on its own (see Tension 1 resolution below).

**Primary recommendation:** Detect the Cilium version by enumerating cilium-agent **pod** `.spec.containers[].image` tags (kube-system, `k8s-app=cilium` — the exact RBAC verb/namespace/list-shape cpg's own `findRelayPod` already requires unconditionally), reducing to the **minimum** parsed version across all Ready pods, with Hubble's `GetNodes()` RPC as an MCP-only secondary cross-check (works even when `--server`/D-07 bypasses kubeconfig entirely) — and never `ServerStatus()` alone or `cilium-dbg version` via exec. Reuse `pkg/k8s/preflight.go`'s exact warn-and-proceed, three-way-branch shape for the below-floor warning; add a small new `pkg/k8s/version.go`. Zero new `go.mod` dependencies, zero new SEC-01 surface (read-only K8s verbs only).

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-------------------|
| COMPAT-01 | README "Supported Cilium versions" section: one floor + per-feature table, PR-verified numbers, including the two previously-unpinned entries | This research supplies full merged-PR + release-tag citations for both remaining entries (`Verdict_AUDIT`/`PolicyVerdictNotify` vintage; observer gRPC API window), spot-checks the three milestone-pre-verified entries against the actual vendored source, and confirms no such README section exists today (full 686-line read) — this is new-authorship, not an edit |
| COMPAT-02 | Runtime detection at connect, privilege-neutral, warn-and-proceed below floor, gates version-dependent behavior, surfaced via MCP | Full RBAC-surface enumeration (file:line) of everything cpg touches today; three candidate detection sources evaluated and ranked with live-cluster evidence; exact `preflight.go` pattern to reuse; exact `StartResult`/`StatusResult` extension points identified; confirmed zero SEC-01 impact |
| COMPAT-03 | README proxy-visibility section: state the ≤1.16 boundary explicitly, remove the "through 1.19" claim | Exact current line numbers confirmed via full README read at HEAD (lines 286-297, "Cilium ≤ 1.19" literally at line 287) |
</phase_requirements>

## Architectural Responsibility Map

cpg is a CLI + MCP-stdio tool, not a web app — the tier vocabulary below is adapted to its actual architecture (composition root / core logic / external cluster services / static docs) rather than forced into browser/SSR/CDN terms.

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Cilium version detection (read-only cluster probe) | `pkg/k8s` (core logic, new `version.go`) | External cluster (K8s API server pods/list; Hubble Relay gRPC `GetNodes`) | Detection logic belongs alongside `preflight.go`'s existing cluster-probing code; the version DATA itself lives in the external cluster, cpg only reads it |
| Compat-verdict computation (version vs. floor table) | `pkg/k8s` (core logic) | — | Pure in-process comparison (`apimachinery/util/version.AtLeast`) once a version string is obtained; no external dependency of its own |
| Warn-and-proceed surfacing (CLI) | `cmd/cpg` (composition root) | `pkg/k8s` (owns the actual probe + log-call pattern) | Mirrors `maybeRunL7Preflight`'s exact call-site placement in `generate.go:223` |
| Compat-verdict surfacing (MCP) | `pkg/session` (`StartResult`/`StatusResult`) + `cmd/cpg/mcp_tools.go` | `pkg/k8s` (supplies the detected value via `resolveSetup`) | `StartResult`/`StatusResult` are the existing MCP "response" shape; `Manager.resolveSetup` is the existing setup-orchestration call site (`manager.go:276-316`) |
| Declared compat matrix | README.md (static documentation) | — | No runtime tier — a hand-authored table using the same floor constants the code enforces |

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `k8s.io/apimachinery/pkg/util/version` | v0.35.4 — **already a direct dependency** [VERIFIED: go.mod line 23; confirmed present at `$(go env GOMODCACHE)/k8s.io/apimachinery@v0.35.4/pkg/util/version/version.go`] | `ParseGeneric`/`AtLeast` — semver-tolerant version comparison | Idiomatic K8s-ecosystem comparator; `ParseGeneric`'s own doc comment: "the version string must consist of two or more dot-separated numeric fields... followed by arbitrary uninterpreted data... the version can be preceded by the letter 'v'" — exactly the shape Cilium's own version strings take (see Code Examples) |
| `github.com/cilium/cilium/api/v1/observer` | v1.19.4 — **already a direct dependency, already imported** [VERIFIED: `pkg/hubble/client.go:12,77` already does `observerpb.NewObserverClient(conn)` for `GetFlows`] | `ObserverClient.GetNodes`/`ServerStatus` RPCs | Reuses the exact client type cpg already constructs; adding a `GetNodes`/`ServerStatus` call is a same-package, same-client addition, not a new dependency |
| `k8s.io/client-go` (`CoreV1().Pods(...).List`) | v0.35.4 — already a direct dependency | Enumerate cilium-agent pods for image-tag detection | Byte-identical call shape to `pkg/k8s/portforward.go:106`'s existing `findRelayPod` (same namespace, same `List` verb, different label selector) |

### Supporting

None new — every candidate signal is reachable through a subpackage of an existing direct dependency, confirmed by direct `go.mod`/`GOMODCACHE` inspection [VERIFIED], consistent with the milestone research's own STACK.md finding ("v1.6 needs zero new `go.mod` `require` lines").

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `k8s.io/apimachinery/pkg/util/version` | `github.com/blang/semver/v4` | Already vendored, but only as an **indirect** dependency (`go.mod` line 32, `// indirect`) pulled in for an unrelated Cilium kernel-version helper — not a direct dep, and enforces strict SemVer with no tolerance for Cilium's own `v1.19.4+gabc123` build-metadata suffix format |
| `k8s.io/apimachinery/pkg/util/version` | `golang.org/x/mod/semver` | Already a direct dependency (`go.mod` line 17), but enforces strict SemVer 2.0 syntax — would reject a leading bare `1.19.2` (no `v`) or a malformed/truncated tag without the same graceful-degradation posture `ParseGeneric` documents |
| Live per-pod enumeration | `cilium-dbg version` via `pods/exec` | **Rejected outright** — see Common Pitfalls. Would smuggle Phase 23's privileged RBAC step-up into a capability that must stay privilege-neutral (COMPAT-02's own success criterion) |

**Installation:**
No new packages. Both `k8s.io/apimachinery` and `github.com/cilium/cilium` subpackages used here are already `require`d in `go.mod` (lines 8, 22-24).

**Version verification:** [VERIFIED: direct `go.mod` read + `go env GOMODCACHE` inspection, 2026-07-22] — `github.com/cilium/cilium v1.19.4` and `k8s.io/apimachinery v0.35.4` are the exact pinned versions; both packages used by this phase's recommendation were confirmed present and API-compatible by reading the actual extracted module source, not by trusting documentation.

## Package Legitimacy Audit

**Not applicable — this phase installs zero new external packages.** Every capability (semver comparison, `GetNodes`/`ServerStatus` RPCs, Pod/DaemonSet `Get`/`List`) is a subpackage of a module already present in `go.mod`'s direct-require block, verified by reading the actual vendored source at the exact pinned version (not inferred from training data or a registry lookup). The `slopcheck`/registry-verification steps in the Package Legitimacy Gate protocol do not apply — there is nothing to install.

## Architecture Patterns

### System Architecture Diagram

```
CLI entry (cpg generate)                    MCP entry (start_session tool)
        |                                            |
        v                                            v
maybeRunVersionPreflight(ctx,                resolveSetup(setupCtx, args)  [existing, manager.go:276]
  kubeConfig, logger)  [NEW,                         |  |- k8s.LoadKubeConfig()        [existing]
  mirrors maybeRunL7Preflight                        |  |- k8s.PortForwardToRelay(...) [existing]
  generate.go:38-56]                                 |  '- k8s.DetectCiliumVersion(setupCtx,
        |                                            |        kubeConfig, observerConn) [NEW]
        '----------------------.                     |
                                v                     v
                    +-----------------------------------------------------+
                    |         pkg/k8s/version.go (NEW)                    |
                    |  DetectCiliumVersion(ctx, k8sClient, observerClient) |
                    +-----------------------------------------------------+
                                |
       .------------------------+-------------------------------------.
       v                                                               v
  1. PRIMARY: list cilium-agent pods                      2. SECONDARY cross-check
     (kube-system, k8s-app=cilium)                            (MCP only, when the observer
     read POD SPEC .spec.containers[].image                   gRPC conn is already open):
     (never .status — loses the tag, see Pitfalls)             ObserverClient.GetNodes()
     strip @sha256:digest, split on LAST ':'                   -> per-node Version field
     after LAST '/', feed to version.ParseGeneric               (the AGENT's own build,
     reduce: MIN parsed version across all                      verified — see Pitfalls)
     Ready pods; keep a {version: count} map
       v                                                               v
       '------------------------+-------------------------------------'
                                 v
                    3. NEVER: bare ServerStatus() alone (reports the
                       RELAY's own build, not the agent's — verified
                       live-divergent during a real rollout) or
                       `cilium-dbg version` via exec (privilege
                       smuggling — Pitfall 7 / this phase's own
                       privilege-neutral constraint)
                                 v
                    CompatInfo{ ClusterVersion (min),
                                VersionsSeen map[string]int,
                                Source string,
                                BelowFloorFeatures []string }
                                 |
              .------------------+-------------------------------.
              v                                                   v
   CLI: logger.Warn(...) iff BelowFloorFeatures         MCP: new fields on StartResult /
   non-empty (mirrors preflight.go's three-way           StatusResult (session.go) — reaches
   warn-and-proceed branch, NEVER aborts)                 the LLM via structuredContent, zero
                                                            SEC-01 impact (Get/List only, no
                                                            new write verb, no new fs write)

   README.md "Supported Cilium versions" (static) -- hand-authored from the
   same per-feature floor constants k8s.DetectCiliumVersion compares against.
```

### Recommended Project Structure

```
pkg/k8s/
├── version.go          # NEW — DetectCiliumVersion + CompatInfo + floor table
├── version_test.go     # NEW — fake clientset, mirrors preflight_test.go's pattern exactly
├── preflight.go         # existing — warn-and-proceed template to reuse
└── portforward.go       # existing — findRelayPod is the List-call shape to mirror
```

### Pattern 1: Warn-and-proceed, three-way branch (reuse verbatim)

**What:** `pkg/k8s/preflight.go:75-119`'s exact shape — attempt the read; on success check the value; on `apierrors.IsForbidden` warn with the exact missing permission named and proceed; on `apierrors.IsNotFound` or any other error, warn and proceed. **Never return an error that blocks the pipeline** (`preflight.go:49-51`'s own rationale comment: reduced-RBAC CI service accounts must not be locked out).
**When to use:** The below-floor / undetermined-version warning at connect time.
**Example (adapted from the existing pattern, `pkg/k8s/preflight.go:75-95`):**
```go
// Source: pkg/k8s/preflight.go (existing pattern), adapted
func detectViaPodImages(ctx context.Context, client kubernetes.Interface, logger *zap.Logger) (versionsSeen map[string]int, forbidden bool) {
    pods, err := client.CoreV1().Pods(ciliumNamespace).List(ctx, metav1.ListOptions{
        LabelSelector: "k8s-app=cilium",
    })
    switch {
    case err == nil:
        return tallyPodImageVersions(pods.Items), false
    case apierrors.IsForbidden(err):
        logger.Warn("version preflight: RBAC denied for list pods in kube-system (k8s-app=cilium). "+
            "Skipping Cilium version detection; version-gated features proceed without a floor check. "+
            "Required permission: pods/list in kube-system.", zap.Error(err))
        return nil, true
    default:
        logger.Warn("version preflight: could not list cilium-agent pods; proceeding without version detection", zap.Error(err))
        return nil, false
    }
}
```

### Pattern 2: Per-feature floor table + `AtLeast` comparison (new, small)

**What:** A package-level table of `(feature name, floor *version.Version)` pairs, compared against the detected `ClusterVersion` via `AtLeast`.
**When to use:** Building `CompatInfo.BelowFloorFeatures`.
**Example:**
```go
// Source: k8s.io/apimachinery/pkg/util/version (vendored, v0.35.4) + this research's verified floor table
var featureFloors = []struct {
    name  string
    floor string // fed to version.ParseGeneric
}{
    {"cilium-dbg binary naming", "1.15.0"},
    {"enableDefaultDeny CNP field", "1.16.0"},
}

func belowFloorFeatures(cluster *apiversion.Version) []string {
    var below []string
    for _, f := range featureFloors {
        floor, err := apiversion.ParseGeneric(f.floor)
        if err != nil {
            continue // programmer error in the table itself, not a runtime condition
        }
        if cluster == nil || !cluster.AtLeast(floor) {
            below = append(below, fmt.Sprintf("%s (requires >= %s)", f.name, f.floor))
        }
    }
    return below
}
```

### Pattern 3: Additive MCP result-field surfacing

**What:** Add fields to an existing result struct rather than a new tool, mirroring how `Server`/`DiscardedSession` already ride on `StartResult` (`pkg/session/session.go:154-163`).
**When to use:** Surfacing the detected version + compat verdict via MCP (success criterion 4).
**Example:**
```go
// Source: pkg/session/session.go (existing StartResult), extended
type StartResult struct {
    SessionID        string `json:"session_id"`
    DiscardedSession string `json:"discarded_session,omitempty"`
    Server           string `json:"server"`
    // NEW:
    CiliumVersion      string         `json:"cilium_version,omitempty"`
    CiliumVersionsSeen map[string]int `json:"cilium_versions_seen,omitempty" jsonschema:"distinct Cilium versions observed across connected agent nodes, keyed by version string, valued by node count — a mixed result usually means a rolling upgrade is in progress"`
    BelowFloorFeatures []string       `json:"below_floor_features,omitempty"`
}
```

### Anti-Patterns to Avoid

- **Reading the DaemonSet's `.spec.template.spec.containers[].image` as the sole source:** [VERIFIED: live cluster, 2026-07-22] This reports the rollout **target**, not current fleet reality. On this repo's own dev/test cluster, `kubectl get daemonset cilium -n kube-system` showed `UP-TO-DATE: 30` against `DESIRED: 83` — an active rolling upgrade with 53/83 nodes still on the prior release. Reading only the DaemonSet object would have reported the newer target version as "the cluster's version" while ~64% of nodes were still on the older one.
- **Reading pod `.status.containerStatuses[].image` or `.imageID` expecting a tag:** [VERIFIED: live cluster, 2026-07-22] On this cluster's container runtime, both fields are digest-normalized (`sha256:...` / `repo@sha256:...`) with **zero human-readable tag**, even though the same pod's `.spec.containers[].image` field retains the tag (`quay.io/cilium/cilium:v1.19.2@sha256:...`). Use the pod **spec**, not status, for tag extraction. This generalizes to standard containerd/CRI image-reference normalization behavior, but was directly confirmed only on this one cluster/runtime — treat as high-confidence guidance, not a hard cross-runtime guarantee.
- **Treating `ServerStatus().Version` as "the agent's version":** [VERIFIED: cpg's own vendored source + live cluster] — see Tension 1 Resolution below. It is Hubble Relay's own build version, not any agent's.
- **Naive `strings.SplitN(image, ":", 2)` tag extraction:** breaks on `host:port/repo:tag` registry references (a real, documented Docker/OCI image-reference form) — must split on the **last** `:` occurring **after** the last `/`, and split off any `@sha256:...` suffix first.
- **Reaching for `cilium-dbg version` via `pods/exec`:** rejected per the milestone research's Pitfall 7 and this phase's own success criterion 3 ("never `pods/exec`") — would smuggle Phase 23's privileged step-up into a capability that must remain usable by an operator who never grants that RBAC.
- **Invoking version detection from `cpg replay`:** `replay` is fully offline by design — `pkg/k8s/preflight.go`'s own doc comment states the equivalent constraint verbatim for L7 pre-flight ("cpg replay is offline by definition and must never call this function regardless of --l7"); the same reasoning applies here with no live cluster to detect against.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Semver comparison tolerant of build suffixes | A custom string-split/int-compare version parser | `k8s.io/apimachinery/pkg/util/version` (`ParseGeneric` + `AtLeast`) | Already vendored; documented tolerance for a leading `v` and arbitrary trailing data (Cilium's own version strings carry a `+g<sha>` build-metadata suffix that changed format across Cilium's history — see State of the Art) |
| Docker/OCI image-reference tag/digest parsing | An ad hoc regex assuming no registry-port colon | Reference-grammar-aware split: strip `@sha256:...` first, then split on the last `:` occurring after the last `/` | Image refs legally contain `host:port/repo:tag` — a naive first-colon split misparses the registry port as a tag separator |
| Cluster-capability warning UX | A new warning/logging convention | `pkg/k8s/preflight.go`'s existing three-way branch + `zap.Warn` house style | Already reviewed, already tested (`preflight_test.go`), already the established convention for "advisory, RBAC-tolerant, never-abort" checks |

**Key insight:** Every piece of this phase's detection logic is a *read* of data that already exists in objects/RPCs cpg (or its immediate sibling `preflight.go`) already touches. The actual engineering risk is not "how do we get a version string" but "which version string is actually true, and for whom" — three different fields (`DaemonSet.spec` image tag, `Pod.status` image/imageID, `ServerStatus.Version`) all *look* like plausible answers and are all wrong or misleading in at least one common, non-theoretical scenario, empirically confirmed this session.

## Tension 1 Resolution — Version-Detection Source of Truth (previously open, now resolved)

The milestone `SUMMARY.md` left this as an explicit, unresolved either/or between `STACK.md` (Hubble `ServerStatus`/`GetNodes` primary) and `ARCHITECTURE.md` (DaemonSet image tag primary), with an action item: "confirm the exact `observerpb` field semantics against a real cluster first." This research did that confirmation, both by reading cpg's own exact vendored implementation and by querying a live, reachable cluster.

**Verified from cpg's own vendored source (`github.com/cilium/cilium@v1.19.4`):**
- `pkg/hubble/relay/observer/server.go:309-311` — Hubble Relay's `ServerStatus` handler literally does `resp := &observerpb.ServerStatusResponse{Version: build.RelayVersion.String()}` **before** the per-peer aggregation loop, and `Version` is **never touched again** by that loop (only flow counts/uptime are aggregated). [VERIFIED]
- `pkg/hubble/build/version.go:19-31` — `build.RelayVersion` and `build.ServerVersion` are both derived from the *same* `version.GetCiliumVersion()` call, differing only by a `component` label (`"hubble-relay"` vs `"cilium"`) baked in at compile time of *that specific binary*. [VERIFIED]
- `pkg/hubble/relay/observer/server.go:172-185` — Relay's `GetNodes` handler dials **each peer** (each cilium-agent's own local Hubble endpoint) and calls **that peer's** `ServerStatus()`, copying `n.Version = status.GetVersion()` — i.e. `Node.Version` is each node's own `build.ServerVersion.String()` (`pkg/hubble/observer/local_observer.go:239-240`). [VERIFIED]

**Conclusion: `ServerStatus().Version` reports the Relay's own build; `GetNodes()[i].Version` reports that specific node's cilium-agent build.** They are populated by genuinely independent code paths and can diverge.

**Verified live** (this repo's own dev/test cluster, read-only `kubectl get` + a bounded `kubectl port-forward` + `grpcurl -plaintext -use-reflection` session against `hubble-relay`, port-forward torn down immediately after, 2026-07-22):
- `ServerStatus` → `"version": "hubble-relay v1.19.3+gf5eb641b"`
- `GetNodes` → per-node versions actually split: `"cilium v1.19.2+g3977f6a1"` on some nodes, `"cilium v1.19.3+gf5eb641b"` on others — **confirmed to match exactly** the same v1.19.2/v1.19.3 split independently observed via `kubectl get pods -n kube-system -l k8s-app=cilium` (30 nodes updated, 53 not yet, per the DaemonSet's own `updatedNumberScheduled: 30` / `desiredNumberScheduled: 83` status).
- `ServerStatus`'s single version number (1.19.3) happened to match the *newer* value only because Hubble Relay (a separate Deployment) had already fully rolled forward while the DaemonSet rollout lagged — a coincidence of this specific rollout's timing, not a structural guarantee.

**Refined recommendation (supersedes both prior files' framing):** neither RPC alone is the best CLI+MCP-consistent primary. Enumerate cilium-agent **pod** images directly (RBAC-cheaper than either RPC path — reuses the exact `pods.list`-in-`kube-system` verb cpg's own `findRelayPod` already requires unconditionally, works identically from CLI and MCP, needs no already-open gRPC connection) as primary; use `GetNodes()` as an MCP-only secondary cross-check (its only advantage: works even when `--server` bypasses kubeconfig entirely, D-07); never use bare `ServerStatus()` for this purpose at all.

## Codebase Privilege Surface (enumerated, file:line)

Every K8s-touching call site in cpg today — the baseline COMPAT-02 must not expand without an explicit, separately-documented RBAC ask:

| Call | Verb | Namespace/Scope | File:line |
|------|------|------------------|-----------|
| `client.CoreV1().ConfigMaps(...).Get` | `get` configmaps | `kube-system` (`cilium-config`) | `pkg/k8s/preflight.go:76` |
| `client.AppsV1().DaemonSets(...).Get` | `get` daemonsets | `kube-system` (`cilium-envoy`) | `pkg/k8s/preflight.go:101` |
| `clientset.CoreV1().Pods(...).List` | `list` pods | `kube-system` (`k8s-app=hubble-relay`) | `pkg/k8s/portforward.go:106` |
| `clientset.CoreV1().RESTClient().Post()...SubResource("portforward")` | `create` pods/portforward | one resolved hubble-relay pod | `pkg/k8s/portforward.go:44-49` |
| `cs.CiliumV2().CiliumNetworkPolicies(...).List` | `list` ciliumnetworkpolicies.cilium.io | operator-chosen namespace(s), opt-in `--cluster-dedup` | `pkg/k8s/cluster_dedup.go:25` |

**This phase's recommended addition:** `clientset.CoreV1().Pods(...).List` — `list` pods, `kube-system`, `k8s-app=cilium` selector. This is the **same verb, same namespace, same resource type** as the existing hubble-relay pod lookup — only the label selector value differs. No new RBAC *class* is introduced; an operator's existing grant for `pods/list` in `kube-system` already covers it. [VERIFIED: RBAC verbs are resource-scoped in Kubernetes, not label-selector-scoped — a `pods` `list` grant in a namespace covers any label selector against that resource type]

**Confirmed zero SEC-01 impact:** `Get`/`List` are not in `k8sWriteVerbs` (`cmd/cpg/mcp_audit_test.go:66-77` — only `Create/Update/Patch/Delete/Apply/DeleteCollection/UpdateStatus/ApplyStatus`), and this phase's recommendation writes no file (no new `disallowedFSWrite`/`fsWriteAllowlist` entry needed). SEC-01's existing structural audit requires zero modification for COMPAT-02, unlike Phase 23's exec-based mutation.

## Common Pitfalls

### Pitfall 1: DaemonSet spec image tag reports the rollout target, not current fleet state

**What goes wrong:** A COMPAT-02 implementation that reads only `DaemonSet.spec.template.spec.containers[].image` reports whatever the *next* rollout target is, even while most nodes are still running the previous version.
**Why it happens:** `.spec` is the desired state; `updatedNumberScheduled` can legitimately lag `desiredNumberScheduled` for the DaemonSet's entire rollout window (which can be days on a large, cautiously-configured cluster — 83 nodes, 140-day-old DaemonSet, still not fully rolled out at the time of this research).
**How to avoid:** Enumerate the actual **pods** (or use `GetNodes()`), not the DaemonSet object.
**Warning signs:** A `CompatInfo` that reports exactly one version with no visibility into per-node spread.
**Evidence:** [VERIFIED: live cluster, 2026-07-22 — `kubectl get daemonset cilium -n kube-system` showed `desiredNumberScheduled: 83`, `updatedNumberScheduled: 30`; distinct pod images confirmed via `kubectl get pods ... -o jsonpath` showed exactly two values, `v1.19.2` (53 pods) and `v1.19.3` (30 pods)]

### Pitfall 2: Pod status image fields lose the tag entirely

**What goes wrong:** `.status.containerStatuses[].image` and `.imageID` are digest-normalized by the container runtime — on this cluster, `.image` returned a bare `sha256:...` local image ID with no repo or tag at all, and `.imageID` returned `repo@sha256:digest` (repo name + digest, still no tag).
**Why it happens:** Kubelet/CRI normalizes the *resolved* image reference for status reporting; the human-readable tag is only preserved in the pod **spec** (what was requested), not status (what's running, expressed as a content-addressed digest).
**How to avoid:** Parse `.spec.containers[].image`, never `.status.containerStatuses[]`.
**Evidence:** [VERIFIED: live cluster, 2026-07-22 — same pod, `spec.image` = `quay.io/cilium/cilium:v1.19.2@sha256:7bc7e0...`; `status.containerStatuses[].image` = `sha256:5051a679...` (bare digest, no tag, no repo); `status.containerStatuses[].imageID` = `quay.io/cilium/cilium@sha256:7bc7e0...` (repo + digest, still no tag)]

### Pitfall 3: `ServerStatus().Version` is not the agent's version

**What goes wrong:** Using bare `ServerStatus()` as "the cluster's Cilium version" reports Hubble Relay's own build, which can diverge from the actual cilium-agent fleet during any rollout where Relay and the agent DaemonSet are upgraded at different times (a Deployment and a DaemonSet, upgraded independently — not an edge case).
**Why it happens:** See Tension 1 Resolution above — `ServerStatus.Version` is set once, unconditionally, to `build.RelayVersion.String()`, never derived from peer data.
**How to avoid:** Use `GetNodes()`'s per-node `Version` (each node's own `ServerVersion`) or pod-image enumeration instead; if `ServerStatus` is used at all, label it explicitly as "Hubble Relay version" in any surfaced output, never "Cilium version."
**Evidence:** [VERIFIED: live cluster + source code, both cited in Tension 1 Resolution]

### Pitfall 4: Image-reference parsing must handle registry `host:port` prefixes

**What goes wrong:** `strings.SplitN(image, ":", 2)` on `registry.internal.corp:5000/cilium/cilium:v1.19.2` incorrectly isolates `"registry.internal.corp"` as the "repo" and `"5000/cilium/cilium:v1.19.2"` as the "tag."
**Why it happens:** Docker/OCI image-reference grammar permits a `host:port` prefix before the first `/`; a tag can never itself contain a `/`, so the correct split point is the last `:` that occurs *after* the last `/`.
**How to avoid:** Strip `@sha256:...` first (split on `@`), then find `strings.LastIndex(rest, ":")` and confirm it is greater than `strings.LastIndex(rest, "/")` before treating it as a tag separator; if not (or absent entirely), the image has no tag — warn "version undetermined," never crash.
**Evidence:** Standard, documented Docker/OCI image-reference grammar; not itself re-derived this session but a well-known, easily-missed parsing hazard directly relevant to this phase's own recommended implementation.

### Pitfall 5: Version detection must stay privilege-neutral (carried from milestone research, reinforced)

**What goes wrong:** Reaching for `cilium-dbg version` via `pods/exec` as a "more authoritative" fallback smuggles Phase 23's privileged RBAC step-up into what must remain a plain, always-on, readonly capability.
**Why it happens:** Exec-based detection genuinely is the most direct single source (asks the running binary), which makes it attractive without noticing the privilege-boundary crossing.
**How to avoid:** Never call exec from `pkg/k8s/version.go`. If an operator has already granted `pods/exec` for the (separate, opt-in) audit-window feature, that is Phase 23's concern, not this phase's.
**Phase to address:** COMPAT-02 (this phase) — already-verified reasoning, milestone PITFALLS.md Pitfall 7.

### Pitfall 6: `replay` must never invoke live version detection

**What goes wrong:** Copy-pasting `maybeRunVersionPreflight`'s call site into `replay.go` alongside `generate.go` would attempt a live cluster read for a fully offline command.
**Why it happens:** `generate.go` and `replay.go` share almost all other flags/flows (both accept `--include-audit`, `--cluster-dedup`, etc.) — a superficial pattern-match invites adding this one too.
**How to avoid:** Mirror `pkg/k8s/preflight.go`'s own doc-comment contract for `RunL7Preflight` verbatim: "invoke from `cpg generate` ONLY... `cpg replay` is offline by definition and must never call this function."
**Evidence:** [VERIFIED: `cmd/cpg/generate.go:36-37`'s existing doc comment states this exact constraint for the analogous L7 pre-flight function]

### Pitfall 7: README currently has no "Supported Cilium versions" section at all

**What goes wrong:** Treating COMPAT-01 as an *edit* to existing content (as if fixing a table) rather than new authorship.
**Why it happens:** The phase description's phrasing ("README's ... section states...") reads like an edit; the milestone research also discusses "the draft's §3.E table" as if version-floor content already exists somewhere user-facing.
**How to avoid:** [VERIFIED: full 686-line README read, 2026-07-22] — no "Supported," "Compatibility," or per-feature version table exists anywhere in `README.md` today. The only existing version signal is `pkg/k8s/preflight.go`'s embedded comments ("Cilium 1.14-1.15... Cilium >= 1.16") — code comments, not user-facing docs. This section must be written from scratch.
**Related, not required:** `docs/KNOWN_LIMITATIONS.md:15` mentions the `policy.cilium.io/proxy-visibility` annotation by name in passing (no version-boundary claim attached — not itself wrong, no fix required for COMPAT-03's stated scope, but worth a glance during review for consistency).

## Code Examples

### Extracting a version from a pod's spec image reference

```go
// Source: this research — grammar per standard Docker/OCI image-reference
// rules, tag format confirmed against live cluster samples, 2026-07-22
func parseImageTag(image string) (tag string, ok bool) {
    // Strip an optional @sha256:... digest suffix first.
    if i := strings.Index(image, "@"); i >= 0 {
        image = image[:i]
    }
    lastColon := strings.LastIndex(image, ":")
    lastSlash := strings.LastIndex(image, "/")
    if lastColon == -1 || lastColon < lastSlash {
        return "", false // no tag (registry host:port prefix, or bare digest ref)
    }
    return image[lastColon+1:], true
}
```

### Parsing + comparing against a floor

```go
// Source: k8s.io/apimachinery/pkg/util/version (vendored, v0.35.4)
tag, ok := parseImageTag(pod.Spec.Containers[i].Image) // e.g. "v1.19.2"
if !ok {
    continue // this pod's version is undetermined; don't let it poison the min
}
v, err := apiversion.ParseGeneric(tag) // tolerates leading "v", trailing "+g<sha>"
if err != nil {
    continue
}
if clusterMin == nil || v.LessThan(clusterMin) {
    clusterMin = v
}
```

### Reusing `preflight.go`'s warn-and-proceed call-site pattern in `generate.go`

```go
// Source: cmd/cpg/generate.go:38-56 (maybeRunL7Preflight), adapted
func maybeRunVersionPreflight(ctx context.Context, kubeConfig *rest.Config, logger *zap.Logger) k8s.CompatInfo {
    if kubeConfig == nil {
        var err error
        kubeConfig, err = k8s.LoadKubeConfig()
        if err != nil {
            logger.Warn("version preflight skipped: kubeconfig not available", zap.Error(err))
            return k8s.CompatInfo{Source: "undetermined"}
        }
    }
    client, err := l7ClientFactory(kubeConfig) // reuse the existing factory var
    if err != nil {
        logger.Warn("version preflight skipped: failed to construct kubernetes client", zap.Error(err))
        return k8s.CompatInfo{Source: "undetermined"}
    }
    info := k8s.DetectCiliumVersion(ctx, client, nil /* no observer conn in CLI mode */, logger)
    if len(info.BelowFloorFeatures) > 0 {
        logger.Warn("cluster Cilium version is below one or more feature floors; proceeding",
            zap.String("cluster_version", info.ClusterVersion),
            zap.Strings("below_floor_features", info.BelowFloorFeatures))
    }
    return info
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| No declared Cilium compatibility floor anywhere in cpg | Explicit README section + runtime detection | This phase | Matches Cilium's own documented framing that its Kubernetes Compatibility table is "the authoritative reference" and that ignoring it "is one of the most common causes of Cilium deployment failures" (per milestone FEATURES.md, citing docs.cilium.io) |
| README claims proxy-visibility "still widely supported (Cilium ≤ 1.19)" | Corrected to state removal at 1.17 | This phase | Fixes a live, already-shipped, user-facing documentation bug — verified: the annotation string is **absent from the entire vendored `cilium@v1.19.4` module tree** [VERIFIED] |
| Milestone research's Tension 1 (open, "confirm against a real cluster first") | Resolved: pod-image enumeration primary, `GetNodes()` MCP-only secondary, `ServerStatus()` alone never used for this purpose | This research, 2026-07-22 | De-risks COMPAT-02's implementation choice with both source-code and live-cluster evidence rather than leaving it to be discovered mid-implementation |
| Cilium's own version-string format | `<component> v<version>+g<git-sha>` (build-metadata `+` separator) | Somewhere between the PR #13979-era example (2020, showed a `-g<sha>` **dash** separator) and the current vendored v1.19.4 source (confirmed `+g<sha>` **plus** separator, live-cluster-observed) | Reinforces using a suffix-tolerant parser (`ParseGeneric`) rather than assuming either separator form is permanent |

**Deprecated/outdated:** Community cheat-sheets asserting the `cilium-dbg` rename at 1.14 (rather than 1.15) and treating `proxy-visibility` as merely "deprecated" without a removal version — already flagged as superseded by the milestone PITFALLS.md; not re-cited here.

## Version Pin Table (COMPAT-01's declared matrix, ready to transcribe)

| Feature | Cilium Floor | Verification |
|---------|-------------|---------------|
| Baseline cpg operation (existing, implicit) | **>= 1.14** [ASSUMED as the recommended documented floor — see Assumptions Log A1] | `pkg/k8s/preflight.go:34-35,45,98,110`'s own "Cilium 1.14-1.15" embedded-envoy fallback comment — the lowest version any shipped cpg code path already assumes |
| `PolicyVerdictNotify` audit-action bit (monitor/datapath level — prerequisite for AUDIT flows to exist at all) | **>= 1.8** | [VERIFIED, this research] PR [#11843](https://github.com/cilium/cilium/pull/11843) "Add audit action to the policy verdict log," merged to `main` 2020-06-04 — before v1.8.0 GA'd (2020-06-22) — and explicitly backported into the `v1.8` branch the following day, PR [#11893](https://github.com/cilium/cilium/pull/11893) "v1.8 backports 2020-06-04," merged 2020-06-05 |
| `Verdict_AUDIT` observable via Hubble's flow API (what `--include-audit`/`include_audit` actually checks) | **>= 1.10** | [VERIFIED, this research] PR [#14785](https://github.com/cilium/cilium/pull/14785) "api/hubble: add AUDIT policy verdict," merged 2021-02-09, plus PR [#14923](https://github.com/cilium/cilium/pull/14923) "hubble: distinguish AUDIT policy verdict from FORWARDED," merged 2021-03-09 — both to `main`, no v1.9.x backport found (`gh search prs --repo cilium/cilium "14785"`/`"14923"` return only the originals; neither PR carries a backport-tracking label). v1.9.0 (GA 2020-11-10) predates both; v1.10.0 (GA 2021-05-20) postdates both. **Already satisfied by the recommended 1.14 base floor — no new gating code needed for the already-shipped AUD-01.** |
| `cilium-dbg` binary rename (was `cilium`) | **>= 1.15** | [Milestone-pre-verified, spot-checked this session] PR [#28085](https://github.com/cilium/cilium/pull/28085) merged 2023-10-11; `cilium-dbg` directory confirmed present in vendored v1.19.4 source tree [VERIFIED] |
| `enableDefaultDeny` CNP field | **>= 1.16** | [Milestone-pre-verified, spot-checked this session] PR [#30572](https://github.com/cilium/cilium/pull/30572) merged 2024-03-14; `DefaultDenyConfig`/`EnableDefaultDeny` confirmed present at `pkg/policy/api/rule.go:33,138` in vendored v1.19.4 [VERIFIED] |
| `policy.cilium.io/proxy-visibility` annotation | **<= 1.16** (removed from agent runtime at 1.17) | [Milestone-pre-verified, spot-checked this session] PR [#35019](https://github.com/cilium/cilium/pull/35019) merged 2024-10-01; confirmed **zero occurrences** of the string anywhere in the vendored v1.19.4 module tree [VERIFIED] |
| Observer gRPC `GetNodes()` RPC + `version` field on `ServerStatusResponse`/`Node` (COMPAT-02's own detection mechanism) | **>= 1.10** | [VERIFIED, this research] PR [#13979](https://github.com/cilium/cilium/pull/13979) "hubble[/relay]: add version to observer.ServerStatus and add and implement observer.GetNodes," merged 2020-11-27. Raw source fetched directly at exact release tags: **absent** in `v1.9.0` and `v1.9.18` (latest v1.9.x patch — checked both, zero matches for `rpc GetNodes` or `string version`); **present** in `v1.10.0`. Already satisfied by the recommended 1.14 base floor. |

**Release tags used for dating** (`gh api repos/cilium/cilium/releases/tags/<tag> --jq .published_at`): v1.8.0 = 2020-06-22, v1.9.0 = 2020-11-10, v1.9.1 = 2020-12-04, v1.10.0 = 2021-05-20, v1.10.1 = 2021-06-16, v1.15.0 = 2024-01-31, v1.16.0 = 2024-07-24, v1.17.0 = 2025-02-04.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|----------------|
| A1 | The recommended single documented floor is Cilium **1.14** (derived from `preflight.go`'s existing comment, not an explicit prior product decision) | Version Pin Table / Standard Stack | LOW — if the planner/operator wants a different floor (e.g. matching cpg's own tested vendored version, 1.19.4, more conservatively), the README table and any code constant are trivially adjustable; no architectural rework needed |
| A2 | Reducing multiple detected node versions to their **minimum** (rather than e.g. majority, or only nodes hosting the target namespace) is the correct default for floor-gating | Architecture Patterns / Tension 1 Resolution | MEDIUM — a majority-vote or namespace-scoped reduction could reduce false-positive warnings during a rollout, at the cost of missing a real gap on a lagging node; recommend starting conservative (min) and revisiting only if false-positive warning noise becomes a real operator complaint |
| A3 | Placing the new "Supported Cilium versions" README section immediately after "Install" (before "Quick start") is the best location | Common Pitfalls (Pitfall 7) | LOW — purely a documentation-structure preference; any placement satisfies COMPAT-01's literal requirement |
| A4 | Pod `.status.containerStatuses[].image`/`.imageID` losing the tag (digest-normalized) generalizes across container runtimes beyond the one live-verified this session | Common Pitfalls (Pitfall 2) | LOW-MEDIUM — this is standard, widely-documented containerd/CRI behavior, but was only directly observed on one cluster/runtime this session; if a target runtime behaves differently, the recommendation to prefer `.spec` over `.status` still holds regardless (spec always preserves the tag as requested) |

## Open Questions (RESOLVED)

1. **Which pods count toward "the cluster's version" for gating purposes — all cilium-agent pods cluster-wide, or only those on nodes hosting the target session's namespace(s)?**
   - What we know: both `GetNodes()` and a cluster-wide pod list return cluster-wide info by default; namespace-scoping to "only relevant nodes" would require an extra Pod→Node join (list target-namespace pods, extract `.spec.nodeName`, cross-reference against cilium-agent pods on those specific nodes).
   - What's unclear: whether the added complexity is worth it for a warn-and-proceed (non-blocking) signal.
   - Recommendation: start cluster-wide (simplest, matches this phase's own "no live cluster test matrix, static + runtime detection" scope framing); namespace-scope only if real operator feedback says the cluster-wide view produces confusing/irrelevant warnings.

2. **Should `StatusResult` (not just `StartResult`) re-surface the cached compat verdict on every `get_status` call?**
   - What we know: detection happens once at `resolveSetup` time; caching the result on the `Session` struct and copying it into both result types is cheap (no re-detection).
   - Recommendation: yes — mirrors how `PolicyFileCount`/`EvidenceFileCount` are already recomputed cheaply per `Status()` call; carrying a cached compat verdict forward costs nothing and saves the LLM from needing to remember the original `start_session` response.

3. **Exact wording for "one documented floor" vs. per-feature table** — is 1.14 the right number, or should it be raised now that this milestone adds features gated at 1.15/1.16? See Assumption A1 — this is a product decision, not a technical constraint; either choice is implementable without rework.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Building/testing this phase's code | check-mark | go1.25.12 | — |
| `k8s.io/client-go` fake clientset (`k8s.io/client-go/kubernetes/fake`) | Unit tests (no live cluster needed) | check-mark, already vendored and already used identically in `pkg/k8s/preflight_test.go` | — | — |
| Live Cilium/Hubble cluster | Manual/integration verification only — **not required** for the unit-test suite | check-mark, confirmed reachable during this research session (Cilium v1.19.2/v1.19.3, 83 nodes, mixed-version rollout in progress) | v1.19.2 / v1.19.3 | Unit tests use the fake clientset exclusively, mirroring `preflight_test.go` — no live cluster dependency for CI |
| `grpcurl` | Optional live-verification tooling only, not a build/runtime dependency | check-mark, used for this research's own live confirmation | — | — |
| `golangci-lint` | `make lint` | Present, but the bare CLI invocation used by this session's tooling hook rejects `--out-format` (a known, pre-existing project-tooling quirk, unrelated to this phase) | — | `rtk proxy golangci-lint run` (documented workaround already in this user's session memory) |

**Missing dependencies with no fallback:** none — this phase has no execution-blocking environment gaps.
**Missing dependencies with fallback:** the `golangci-lint`/rtk-hook quirk noted above; pre-existing, not introduced by this phase.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `github.com/stretchr/testify` (require/assert) — already used throughout, e.g. `pkg/k8s/preflight_test.go` |
| Config file | none — driven via `Makefile`'s `test:` target |
| Quick run command | `go test ./pkg/k8s/... -run TestDetectCiliumVersion -v` |
| Full suite command | `go test ./... -count=1 -race` (Makefile `make test`) |

### Phase Requirements -> Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|---------------------|-------------|
| COMPAT-02 | Detects cluster version from pod images, reduces to minimum, builds `BelowFloorFeatures` | unit | `go test ./pkg/k8s/... -run TestDetectCiliumVersion -v` | Wave 0 (new — `pkg/k8s/version_test.go`, fake clientset pattern identical to `preflight_test.go`) |
| COMPAT-02 | RBAC-Forbidden on pods/list warns-and-proceeds, never errors | unit | `go test ./pkg/k8s/... -run TestDetectCiliumVersion_Forbidden -v` | Wave 0 (new, same file) |
| COMPAT-02 | `StartResult`/`StatusResult` carry the detected version + verdict | unit | `go test ./pkg/session/... -run TestStartResult -v` | check-mark existing test file (`pkg/session/session_test.go`) extended, not new |
| COMPAT-02 | `replay` never invokes version detection | unit (regression) | `go test ./cmd/cpg/... -run TestReplay -v` | check-mark existing test file (`cmd/cpg/replay_test.go`) — add one assertion, not a new file |
| COMPAT-01 | README declares one floor + per-feature table with the corrected numbers | manual/doc-review; optionally a lightweight golden test | `go test ./cmd/cpg/... -run TestReadmeCompatSection -v` (recommend, does not exist yet) | Wave 0 gap — no precedent in this repo for testing README prose; planner should decide whether to add this or accept manual review only |
| COMPAT-03 | README's proxy-visibility section states the <=1.16 boundary, "through 1.19" claim is gone | manual/doc-review; optionally a lightweight golden test | Could be folded into the same `TestReadmeCompatSection` above (assert the literal string "1.19" no longer appears adjacent to "proxy-visibility") | Wave 0 gap — same decision as COMPAT-01 |

### Sampling Rate

- **Per task commit:** `go test ./pkg/k8s/... ./pkg/session/... -run "TestDetectCiliumVersion|TestStartResult" -v`
- **Per wave merge:** `make test` (full suite, `-race`)
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps

- [ ] `pkg/k8s/version_test.go` — new file, fake-clientset pattern identical to `preflight_test.go` (success / Forbidden / NotFound-or-error three-way branch, plus a mixed-version-pods case asserting the minimum is taken and `VersionsSeen` is populated)
- [ ] `pkg/k8s/version.go`'s image-tag-parsing helper needs its own focused unit tests for the `host:port/repo:tag` and `repo@sha256:digest`-only edge cases (Pitfall 4) — table-driven, no clientset needed
- [ ] A decision needed from the planner: whether to add an automated README-prose consistency test (`TestReadmeCompatSection`) or accept manual review for COMPAT-01/COMPAT-03 — no existing precedent in this repo either way

*(No framework install gap — `testify`/`client-go/fake` are already vendored and already used identically elsewhere in this package.)*

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V2 Authentication | No | Unchanged — cpg's own kubeconfig/client-go auth is untouched by this phase |
| V3 Session Management | No | This phase adds fields to existing MCP session results; it does not touch session lifecycle/auth |
| V4 Access Control | **Yes** | This phase's entire success criterion 3 is an access-control constraint: detection must be privilege-neutral, reusing exactly the existing `pods/list`/`daemonsets/get`-in-`kube-system` RBAC tier, never introducing `pods/exec` |
| V5 Input Validation | **Yes** | The version string returned by the cluster (a DaemonSet/pod image field, or a gRPC response field) is cluster-supplied, effectively untrusted-adjacent data (a compromised/misconfigured agent or a spoofed image tag could return an arbitrary string) that cpg parses and uses for gating decisions; `version.ParseGeneric` returns an `error` rather than panicking on malformed input — verified, matches Go's memory-safe string-handling guarantees |
| V6 Cryptography | No | No crypto in this phase |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|------------------------|
| A compromised/misconfigured cluster component returns a spoofed or malformed version string to influence cpg's feature-gating decision | Tampering | Warn-and-proceed only (never hard-gate into a MORE privileged code path based on a version claim) — this phase's own design keeps the blast radius of a wrong/spoofed version at "wrong warning," never "unlocked capability," since the version-gated FEATURES themselves (bootstrap CNP form, `cilium-dbg` naming) live in later phases and each independently validates its own preconditions |
| Detected Cilium version reaching MCP tool results / LLM context | Information Disclosure | Same class of exposure cpg's existing secrets-posture README section already documents for workload labels/FQDNs (README.md's MCP section) — a Cilium version number is not itself sensitive, but the phase should note it crosses the same trust boundary, consistent with existing disclosure |
| Reaching for `pods/exec`-based detection as a "more reliable" shortcut | Elevation of Privilege | The core constraint this phase enforces: detection must never smuggle Phase 23's privileged RBAC step-up into an always-on, readonly capability (Pitfall 5 / milestone Pitfall 7) |
| Unbounded/malformed image-tag string causing a parsing panic | Denial of Service (minor) | `version.ParseGeneric` is documented to return an error, not panic, on malformed input; the recommended `parseImageTag` helper above returns `(string, bool)`, never panics on any input shape |

## Sources

### Primary (HIGH confidence)

- cpg repo source, read directly at HEAD, 2026-07-22: `README.md` (full 686 lines), `pkg/k8s/{preflight,portforward,client,cluster_dedup}.go` (+ tests), `pkg/hubble/client.go`, `pkg/session/{manager,session,pipeline_config}.go`, `cmd/cpg/{mcp,mcp_tools,mcp_audit_test,generate}.go`, `go.mod`, `Makefile`, `docs/KNOWN_LIMITATIONS.md`
- Vendored `github.com/cilium/cilium@v1.19.4` (read directly via `$(go env GOMODCACHE)`): `api/v1/observer/{observer.proto,observer_grpc.pb.go}`, `pkg/hubble/relay/observer/server.go`, `pkg/hubble/observer/local_observer.go`, `pkg/hubble/build/version.go`, `pkg/version/version.go`, `pkg/policy/api/rule.go`, `api/v1/flow/flow.proto`
- Vendored `k8s.io/apimachinery@v0.35.4/pkg/util/version/version.go` — read directly
- GitHub PRs (via `gh pr view`/`gh search prs`, merged + dated, this session's own archaeology): [#11843](https://github.com/cilium/cilium/pull/11843), [#11893](https://github.com/cilium/cilium/pull/11893), [#14785](https://github.com/cilium/cilium/pull/14785), [#14923](https://github.com/cilium/cilium/pull/14923), [#13979](https://github.com/cilium/cilium/pull/13979)
- GitHub release tags (via `gh api repos/cilium/cilium/releases/tags/<tag>`): v1.8.0, v1.9.0, v1.9.1, v1.10.0, v1.10.1
- Raw source fetched directly at exact release tags (`raw.githubusercontent.com/cilium/cilium/<tag>/api/v1/observer/observer.proto`): v1.9.0, v1.9.18, v1.10.0 — confirming `GetNodes`/`version` field absence/presence directly, not inferred
- **Live cluster** (read-only `kubectl get`, plus one bounded `kubectl port-forward` + `grpcurl -plaintext -use-reflection` session against this repo's own reachable dev/test cluster, port-forward process force-killed and confirmed torn down immediately after, 2026-07-22): `cilium` DaemonSet/pods (kube-system), `hubble-relay` pod/service, `cilium-config` ConfigMap
- Milestone research files `.planning/research/{SUMMARY,PITFALLS,FEATURES,ARCHITECTURE}.md` (2026-07-22) — used as a starting point; several claims independently re-verified, refined, or corrected by this session (see Tension 1 Resolution, Version Pin Table)

### Secondary (MEDIUM confidence)

- WebFetch of `docs.cilium.io/en/v1.8/hubble/` and `docs.cilium.io/en/v1.9/hubble/` (versioned docs pages) — used only for initial orientation; **superseded** by the raw-tag source fetch above where they seemed to disagree (the v1.9 docs page's apparent mention of `GetNodes` did not match the raw v1.9.0/v1.9.18 tag source, which is the higher-authority, ground-truth check and is what this research relies on)

### Tertiary (LOW confidence)

- None carried forward — the milestone research's own "superseded" tertiary sources (community cheat-sheets on `cilium-dbg`/proxy-visibility) are not re-cited here.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new dependencies; both packages' exact APIs confirmed by reading vendored source at the pinned version
- Architecture: HIGH — every integration point grounded in file:line source reads plus live-cluster empirical confirmation (Tension 1)
- Pitfalls: HIGH — the two most consequential pitfalls (DaemonSet-spec-vs-reality skew; pod-status-loses-tag) were directly, empirically observed on a live cluster during this research, not merely hypothesized
- Version pins: HIGH — every entry in the Version Pin Table carries a merged-PR-plus-release-tag citation; the two previously-unpinned entries were resolved this session using the identical method the milestone research used for the other three, plus a raw-source-at-tag cross-check for the observer API window specifically

**Research date:** 2026-07-22
**Valid until:** The version-pin facts (PR merge dates, release-tag dates, proto field history) are historical and effectively permanent — no revalidation needed. The specific live-cluster snapshot (83 nodes, the exact v1.19.2/v1.19.3 split) is a point-in-time example of real-world version skew, not an ongoing state to re-check — treat it as illustrative evidence, not a live fact to depend on. Recommend standard 30-day validity for the design-synthesis recommendations (detection-source ordering, floor number).
