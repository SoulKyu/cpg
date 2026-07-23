---
phase: 21-cilium-compatibility-matrix-runtime-detection
reviewed: 2026-07-22T13:23:51Z
depth: standard
files_reviewed: 11
files_reviewed_list:
  - README.md
  - cmd/cpg/generate.go
  - cmd/cpg/generate_test.go
  - cmd/cpg/mcp_session_test.go
  - cmd/cpg/readme_compat_test.go
  - cmd/cpg/replay_test.go
  - pkg/k8s/version.go
  - pkg/k8s/version_test.go
  - pkg/session/manager.go
  - pkg/session/manager_test.go
  - pkg/session/session.go
findings:
  critical: 0
  warning: 3
  info: 6
  total: 9
status: issues_found
---

# Phase 21: Code Review Report

**Reviewed:** 2026-07-22T13:23:51Z
**Depth:** standard
**Files Reviewed:** 11
**Status:** issues_found

## Summary

Reviewed the Phase 21 Cilium compatibility-matrix + runtime version-detection change set (diff base `c054713`) at standard depth, with cross-references into the non-diffed collaborators (`pkg/k8s/client.go`, `pkg/k8s/preflight.go`, `pkg/k8s/portforward.go`, `pkg/hubble/client.go`, `cmd/cpg/mcp.go`, `cmd/cpg/mcp_tools.go`, `pkg/session/pipeline_config.go`).

**Phase invariants verified as holding:**

- SEC-01: `cmd/cpg/mcp_audit_test.go` is byte-identical to the diff base (`git diff --shortstat` empty).
- Zero new `go.mod`/`go.sum` dependencies (`k8s.io/apimachinery/pkg/util/version` and `observerpb` were already direct deps).
- Privilege-neutral detection: only `pods/list` in kube-system (same verb class as `findRelayPod`, portforward.go:106) plus the GetNodes read RPC; no `pods/exec`, no bare `ServerStatus()` call anywhere in version.go.
- Warn-and-proceed: every detection failure path (nil client, RBAC-forbidden, list failure, unreachable relay, unparseable tags) returns `Source: "undetermined"` and never propagates an error into `Start`/`runGenerate` (manager.go:340 discards no error; DetectCiliumVersion returns no error type at all).
- Cluster-wide min-version reduction over Running pods, malformed tags skipped without poisoning the minimum (T-21-01-01 honored).
- `detectVersionFn` seam correctly prevents every `pkg/session` unit test from dialing the fake bypass address (manager_test.go:146-148).
- COMPAT-03: the shipped "proxy-visibility through 1.19" bug is fixed to the correct 1.16/1.17 boundary, and `readme_compat_test.go` pins both the negative (no proxy-visibility+1.19 pairing) and positive (boundary stated in-paragraph) guards.
- `go build ./...`, `go vet`, and `go test -count=1 -race` on `pkg/k8s`, `pkg/session`, and `cmd/cpg` all green.

**Key concerns:** three warnings. The always-on CLI version preflight introduces an unbounded, un-opt-outable kubeconfig/apiserver probe — a behavioral regression on the explicit `--server` path (WR-01). The GetNodes secondary's unary RPC escapes the `versionDetectTimeout` hard ceiling and is bounded only by the caller ctx (WR-02). The runtime floor table structurally cannot express the documented proxy-visibility ≤ 1.16 ceiling, so 1.17+ clusters get no runtime signal that README's recommended annotation path is a no-op (WR-03).

## Narrative Findings (AI reviewer)

No structural pre-pass was provided; all findings below are from direct adversarial review.

## Warnings

### WR-01: Always-on CLI version preflight is unbounded and has no opt-out; regression on the explicit `--server` path

**File:** `cmd/cpg/generate.go:68-83, 256` (and `pkg/k8s/version.go:142-144, 157-159`)
**Issue:** `maybeRunVersionPreflight` runs unconditionally on every `cpg generate` invocation and, unlike the L7 preflight, has no `--no-version-preflight` escape hatch (VIS-06's `--no-l7-preflight` covers only L7). Three consequences:

1. **No time bound.** `DetectCiliumVersion`'s `Pods(...).List(ctx, ...)` runs under the signal ctx, which carries no deadline, and `LoadKubeConfig()` (pkg/k8s/client.go:15) sets no `rest.Config.Timeout`. A kubeconfig pointing at an unreachable private endpoint (VPN down — a daily occurrence) stalls startup ~30-40s on client-go's default dial/TLS timeouts; an accepted-but-stalled apiserver connection (control-plane outage behind a live TCP LB) stalls it indefinitely until Ctrl+C. The MCP path bounds the same call with `setupCtx`'s deadline; the CLI path has no analog to `versionDetectTimeout` at all. "Warn-and-proceed, never blocking" does not survive a hung apiserver.
2. **Regression on `--server`.** Before this phase, `cpg generate --server X` never touched kubeconfig unless `--cluster-dedup` was set. Now every explicit-server run attempts `LoadKubeConfig()` + an apiserver round trip the user deliberately bypassed — and, when no kubeconfig exists (air-gapped relay tunnel, the exact use case `--server` serves), emits a WARN (`version preflight skipped: kubeconfig not available`) on every run for a perfectly legitimate setup.
3. **Spurious warning on Ctrl+C.** A SIGINT during the preflight cancels ctx, the List returns `context.Canceled`, and version.go's `default:` branch (line 157) logs `warnPodsListFailed` — alarming an operator about a failure that was their own shutdown.

**Fix:** Bound the CLI preflight and special-case cancellation:
```go
// generate.go — maybeRunVersionPreflight
detectCtx, cancel := context.WithTimeout(ctx, versionDetectBudget) // e.g. 3s, mirroring versionDetectTimeout
defer cancel()
k8s.DetectCiliumVersion(detectCtx, client, logger)

// version.go — default branch of DetectCiliumVersion
default:
    if ctx.Err() == nil { // don't warn when the caller was cancelled/shut down
        logger.Warn(warnPodsListFailed, zap.Error(err))
    }
    return finalizeCompat(CompatInfo{Source: "undetermined"}, logger)
```
Additionally consider: (a) on the explicit `--server` path, either skip the kubeconfig probe or reuse the GetNodes secondary the binary already ships (the CLI has `f.server`/`f.tlsEnabled`/`f.timeout` in hand at the call site — the "MCP-only" restriction leaves CLI `--server` users permanently at "undetermined" for no technical reason); (b) a `--no-version-preflight` flag for restricted-RBAC CI, mirroring VIS-06.

### WR-02: `DetectCiliumVersionViaGetNodes` — the GetNodes RPC escapes the `versionDetectTimeout` hard ceiling

**File:** `pkg/k8s/version.go:252-257` (constant contract at 31-37)
**Issue:** `effective = min(timeout, versionDetectTimeout)` bounds only the dial/ready-wait via `waitForVersionConnReady` (line 247). The subsequent unary call runs on the raw caller ctx:
```go
resp, err := client.GetNodes(ctx, &observerpb.GetNodesRequest{})
```
A relay that completes the gRPC handshake (channel reaches Ready) but never answers the RPC — a half-open connection through a middlebox, or a wedged relay — blocks here until the caller ctx expires. In the one production call path (`resolveSetup`, manager.go:340) that is `setupCtx`, whose deadline is the MCP-supplied `timeout` up to the 24h `maxSessionDuration` ceiling: `start_session` with `timeout: "24h"` against a ready-but-stalled relay hangs the tool call for 24 hours inside an *advisory* detection probe. For any direct caller passing a deadline-free ctx (the function is exported), it hangs forever. This contradicts the `versionDetectTimeout` doc's framing ("a hard ceiling, never merely a suggestion... an omitted or zero timeout must never produce an unbounded dial") in spirit: the connection is bounded, the probe is not. The bounded-unreachable test only exercises connection-refused, which never reaches the RPC.
**Fix:** Bound the RPC with the same budget as the dial:
```go
rpcCtx, rpcCancel := context.WithTimeout(ctx, effective)
defer rpcCancel()
resp, err := client.GetNodes(rpcCtx, &observerpb.GetNodesRequest{})
```

### WR-03: Runtime floor table cannot express the proxy-visibility ≤ 1.16 ceiling — enforcement is doc-only for the most common cluster class

**File:** `pkg/k8s/version.go:82-94` (README.md:79, 304-317)
**Issue:** The `featureFloors` comment claims "this table is the runtime-enforcement side of that documentation [README's Supported Cilium versions section]", but the table's shape (`floor` only, formatted as "requires >= X" by `belowFloorFeatures`) structurally cannot represent the one *ceiling* the README documents: `policy.cilium.io/proxy-visibility` removed at 1.17. Result: on a 1.17+ cluster — the majority of current installs — cpg *knows* the detected version, README still lists the annotation as "Recommended for ad-hoc bootstrap" (option 1), yet neither the version preflight nor the L7 preflight emits any signal that following that recommendation is a silent no-op. The only downstream mitigation is VIS-01's "no L7 records observed" warning, which fires after a wasted capture window rather than at connect time. The phase goal is "runtime version detection that never silently misbehaves off-floor"; an above-ceiling no-op is exactly that class of silent misbehavior, and the phase invariant table explicitly lists "proxy-visibility ≤ 1.16" alongside the floors.
**Fix:** Either (a) extend the table with an optional `ceiling` field and warn "proxy-visibility annotation is a no-op (removed at 1.17); detected <v>. Use the bootstrap L7 CNP instead" when `--l7` is set and detected ≥ 1.17; or, at minimum, (b) amend the `featureFloors` comment to state explicitly that the proxy-visibility ceiling is intentionally doc-only (COMPAT-01/COMPAT-03 + VIS-01) so the "keep in sync" instruction cannot be read as full parity.

## Info

### IN-01: `l7ClientFactory` reused by version preflight — misleading name and duplicated kubeconfig/client construction

**File:** `cmd/cpg/generate.go:27, 44, 50, 71, 77`
**Issue:** `maybeRunVersionPreflight` builds its client through `l7ClientFactory`, whose name and doc comment claim L7-preflight scope. On the explicit `--server --l7` path, both preflights independently call `k8s.LoadKubeConfig()` and construct separate clients — a kubeconfig with an `exec` credential plugin gets invoked twice per run (latency; possible double refresh).
**Fix:** Rename to `k8sClientFactory` (update the doc comment), and have `runGenerate` load kubeconfig once and pass it to both preflights on the explicit-server path.

### IN-02: `parseImageTag` doc-comment justifies its slash-free rejection with a field the code never reads

**File:** `pkg/k8s/version.go:100-122` (call site 191)
**Issue:** The comment cites "the shape Kubernetes reports in a pod's `.status.containerStatuses[].image` field" as the reason a slash-free colon string is "never a tag" — but `tallyPodImageVersions` reads `.Spec.Containers[].Image`, where bare `sha256:...` digests never occur and slash-free `repo:tag` references (e.g. `cilium:v1.16.0`, a locally-built image in a kind cluster) are valid and get silently skipped. The failure mode is safe (skip → possibly undetermined → warn-and-proceed, never a wrong minimum), but the justification is wrong and the conservatism undocumented as a trade-off.
**Fix:** Correct the comment to reflect the spec-field source and state the deliberate trade-off: slash-free `repo:tag` is rejected to keep the bare-digest defense simple, at the cost of skipping unqualified local images.

### IN-03: pods/list success with zero matches is completely silent — non-kube-system Cilium installs never learn why detection is undetermined

**File:** `pkg/k8s/version.go:142-153`
**Issue:** When the list succeeds but matches no pods (Cilium installed outside kube-system — e.g. the `cilium` namespace under OpenShift OLM — or a distribution relabeling the DaemonSet), detection returns `Source: "undetermined"` with zero log output (the test pins `wantWarns: 0`). The forbidden and list-failure paths both warn; the ran-but-empty path gives the operator nothing to grep.
**Fix:** Add a Debug (or Info) log on the empty-result path naming the searched namespace and label selector, e.g. `logger.Debug("version preflight: no cilium-agent pods matched", zap.String("namespace", ciliumNamespace), zap.String("selector", ciliumAgentLabelSelector))`.

### IN-04: Version tally + min-reduction loop duplicated between the two detection sources; the GetNodes copy has no test

**File:** `pkg/k8s/version.go:259-270` (duplicate of 195-204)
**Issue:** `DetectCiliumVersionViaGetNodes` re-implements the `versionsSeen[v.String()]++ / minVersion` reduction that `tallyPodImageVersions` already owns. Only the pod-images copy is exercised by tests (`parseAgentVersionString` is unit-tested, but the GetNodes loop — tally, min reduction, undetermined-on-empty — is not); a future edit to one copy (e.g. a changed skip rule) silently diverges the two sources' semantics.
**Fix:** Extract the reduction into a shared helper, e.g. `func reduceVersions(iter func(yield func(*apiversion.Version)))` or simpler: a `tallyVersion(versionsSeen map[string]int, min **apiversion.Version, v *apiversion.Version)` called from both loops; unit-test it once.

### IN-05: `belowFloorFeatures` nil-cluster branch is unreachable and contract-contradicting; a mistyped floor is silently dropped

**File:** `pkg/k8s/version.go:353-365` (also 357-359)
**Issue:** Two latent hazards in the floor evaluator: (1) the `cluster == nil` branch ("treated as failing every floor") is dead code — `finalizeCompat` returns early on empty/unparseable `ClusterVersion` — and if ever reached would contradict `CompatInfo`'s documented contract that `BelowFloorFeatures` is "Empty when ClusterVersion is undetermined". (2) `ParseGeneric(f.floor)` failure hits `continue`, so a typo in a future `featureFloors` entry silently disables that feature's enforcement with no signal; no test asserts every table entry parses.
**Fix:** Drop the `cluster == nil` arm (callers guarantee non-nil) or align its semantics with the contract, and add a one-liner table-validity test:
```go
func TestFeatureFloorsAllParse(t *testing.T) {
    for _, f := range featureFloors {
        if _, err := apiversion.ParseGeneric(f.floor); err != nil {
            t.Errorf("featureFloors[%q]: unparseable floor %q: %v", f.name, f.floor, err)
        }
    }
}
```

### IN-06: README compat-section intro claims "higher floors" but half the table rows are lower than the baseline

**File:** `README.md:70-80`
**Issue:** "Individual capabilities carry their own, higher floors:" is contradicted three rows later — `PolicyVerdictNotify` (≥ 1.8), `Verdict_AUDIT` (≥ 1.10), and `GetNodes` (≥ 1.10) are all *below* the 1.14 baseline (they are listed to show they're subsumed, which is useful, but the intro sentence misdescribes them).
**Fix:** Reword to "Individual capabilities carry their own floors (those below 1.14 are subsumed by the baseline):" or equivalent.

---

_Reviewed: 2026-07-22T13:23:51Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
