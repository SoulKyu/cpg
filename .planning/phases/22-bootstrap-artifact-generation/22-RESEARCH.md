# Phase 22: Bootstrap Artifact Generation - Research

**Researched:** 2026-07-22
**Domain:** Cilium `CiliumNetworkPolicy` default-deny semantics (Go API types, Sanitize() validation, CRD structural-schema pruning) + cpg's existing CLI/MCP composition patterns
**Confidence:** HIGH (the load-bearing claim — the exact artifact shape that enforces default-deny — is empirically verified against the actual vendored `github.com/cilium/cilium v1.19.4` dependency, not inferred from memory)

## Summary

This phase has one fact that determines whether success criterion 1 is even achievable as literally worded in CONTEXT.md/ROADMAP.md, so it leads this report: **`ingress: []` / `egress: []` (literal empty arrays) is not the fix for cilium/cilium#35558 — it is the bug.** I built and ran a standalone Go program against cpg's actual vendored `github.com/cilium/cilium@v1.19.4` (`pkg/policy/api`, `pkg/k8s/apis/cilium.io/v2`) and confirmed, empirically:

1. `api.Rule{EndpointSelector: ..., Ingress: []api.IngressRule{}, Egress: []api.EgressRule{}, EnableDefaultDeny: {true, true}}.Sanitize()` returns the error `"rule must have at least one of Ingress, IngressDeny, Egress, EgressDeny"`. Empty-but-present slices have `len() == 0`, identical to omitted fields — Sanitize() cannot distinguish them.
2. Both `Ingress []IngressRule` and `Egress []EgressRule` carry `json:"ingress,omitempty"` / `json:"egress,omitempty"` tags (`pkg/policy/api/rule.go:89,103`), and cpg's own marshal path (`sigs.k8s.io/yaml.Marshal`, same call `pkg/output/writer.go` already uses) drops empty slices entirely under `omitempty`. **A literal `ingress: []` token can never survive this marshal path even if Sanitize() didn't reject it first** — the key disappears, it does not marshal as `[]`.
3. The combination that actually passes `Sanitize()`, actually marshals with the field present, and is Cilium's own **officially documented** default-deny pattern (`docs.cilium.io` "Layer 3 Examples" / "Default Deny Ingress Policy") is a **one-element list containing a single empty rule object**: `Ingress: []api.IngressRule{{}}` / `Egress: []api.EgressRule{{}}`, i.e. YAML `ingress:\n- {}` / `egress:\n- {}`, NOT `ingress: []`. I verified this combined with an explicit `EnableDefaultDeny{Ingress: &true, Egress: &true}` marshals cleanly via `sigs.k8s.io/yaml.Marshal` on the exact `ciliumv2.CiliumNetworkPolicy` type cpg uses, and `Sanitize()` returns no error. Full verified output is in Code Examples below.

This directly corrects the literal wording of the CONTEXT.md locked decision ("explicit empty `ingress: []`/`egress: []` stanzas") and the code sketch already sitting in `.planning/phases/22-bootstrap-artifact-generation/22-PATTERNS.md` (`Ingress: []*api.IngressRule{}` — a compile error too, since `api.Rule.Ingress` is `[]IngressRule`, not `[]*IngressRule`, and `EnableDefaultDeny` is typed `DefaultDenyConfig` with `*bool` fields, not a `*DefaultDeny{bool,bool}` type that does not exist in the vendored package). **The spirit of the locked decision — both `enableDefaultDeny` AND explicit non-nil ingress/egress presence, tested as a named acceptance criterion guarding the #35558 failure mode — is fully achievable; only the literal YAML token `[]` vs `- {}` needs correcting in the plan.** See Pitfall 1 for the full detail and Code Examples for copy-pasteable verified Go.

Second load-bearing fact: `cilium/cilium#35558`'s "silent no-op" framing describes Cilium **before** PR #35904 (merged, backported to the 1.17 release line). Before that fix, `enableDefaultDeny` alone with zero rule stanzas was accepted but non-enforcing (an accepted, do-nothing CRD object — the true silent no-op). After the fix (1.17+), the *same* input is now a hard `Sanitize()` rejection (visible only in cilium-agent logs, not to `kubectl apply`, since the CRD's OpenAPI schema places no constraint on this — apiserver admission succeeds either way). Neither behavior is what cpg's artifact should rely on: the `ingress: [{}]`/`egress: [{}]` pattern sidesteps the whole ambiguity because it has always been valid, on every version back to Cilium 1.9 (deny-policies GA), independent of #35558's resolution.

Third fact, closing Key Research Question 2: Cilium's CRD YAML (`ciliumnetworkpolicies.yaml`, checked in the vendored v1.19.4 module) sets no `x-kubernetes-preserve-unknown-fields: true` anywhere. Structural-schema CRDs (mandatory since Kubernetes 1.16, `apiextensions.k8s.io/v1`) prune unrecognized properties by default. A Cilium <1.16 cluster's CRD schema does not declare `enableDefaultDeny` at all, so the Kubernetes apiserver will silently strip that field from the stored object on `kubectl apply` — success looks identical to a correctly-applied policy, but the field is gone. This is exactly the "looks like it worked" failure mode ROADMAP.md's success criterion 2 warns against, and it justifies (independent of any Sanitize()-side behavior) the CONTEXT.md hard-refusal-below-1.16 decision.

**Primary recommendation:** Build the artifact with `EndpointSelector: labels.BuildEndpointSelector(nil)` (reuses cpg's own proven "select all" fallback, `pkg/labels/selector.go:149-155`), `Ingress: []api.IngressRule{{}}`, `Egress: []api.EgressRule{{}}`, and explicit `EnableDefaultDeny: api.DefaultDenyConfig{Ingress: &t, Egress: &t}`; marshal via `sigs.k8s.io/yaml.Marshal` on the standard `*ciliumv2.CiliumNetworkPolicy` type; gate on the existing `featureFloors` "enableDefaultDeny CNP field" >= 1.16.0 entry (`pkg/k8s/version.go:103`) already wired by Phase 21; keep the MCP tool's write surface at zero by never calling any writer function from the tool handler (`yaml.Marshal` alone, in memory, is enough).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Bootstrap CNP construction (Go struct → typed `api.Rule`) | Backend / CLI process (`pkg/policy`) | — | Pure in-memory transform, no I/O; mirrors existing `pkg/policy/builder.go` |
| YAML marshaling | Backend / CLI process (`pkg/output` marshal path) | — | Reuses `sigs.k8s.io/yaml.Marshal`, the same codepath `generate` already uses for byte-consistent style |
| Version gate (Cilium >= 1.16 floor) | Backend / CLI process (`pkg/k8s`) | — | Pure computation over an already-detected `CompatInfo`; no new K8s verb |
| Cilium version detection | K8s API (read: `pods/list` in kube-system) | Hubble Relay observer gRPC (`GetNodes`, MCP-only secondary) | Existing Phase 21 `DetectCiliumVersion`/`DetectCiliumVersionViaGetNodes`, reused verbatim |
| `-o` file output | CLI process (local filesystem) | — | CLI-only; never reachable from the MCP composition root, so SEC-01's audit does not scan it |
| MCP tool response | MCP / Backend process (in-memory, stdout JSON-RPC wire) | — | `get_bootstrap_policy` returns YAML as a string field in its typed result; zero filesystem writes |
| Runbook | Documentation (repo markdown) | — | Static content, no runtime tier |

## Standard Stack

### Core
No new external dependencies. Every type this phase needs is already vendored via `go.mod`'s `github.com/cilium/cilium v1.19.4` and already imported elsewhere in cpg.

| Library | Version (verified) | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/cilium/cilium/pkg/policy/api` | v1.19.4 (pinned, `go.mod:8`) | `api.Rule`, `api.IngressRule`, `api.EgressRule`, `api.DefaultDenyConfig`, `api.EndpointSelector` | Same package `pkg/policy/builder.go` already uses |
| `github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2` | v1.19.4 | `ciliumv2.CiliumNetworkPolicy` wrapper type | Same package `pkg/output/writer.go` already uses |
| `sigs.k8s.io/yaml` | already in `go.mod` (indirect via cilium) | `yaml.Marshal`/`yaml.Unmarshal` on the CNP struct | Same marshal call `pkg/output/writer.go:56,71` already uses — required for "byte-consistent style" per CONTEXT.md |
| `github.com/spf13/cobra` | already in `go.mod` | `newBootstrapCmd()` construction | Same pattern every other `cmd/cpg/*.go` command uses |
| `github.com/modelcontextprotocol/go-sdk/mcp` | already in `go.mod` | `mcp.AddTool` registration for `get_bootstrap_policy` | Same pattern `cmd/cpg/mcp_query.go` already uses |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `ingress: [{}]` / `egress: [{}]` legacy-empty-rule pattern | `enableDefaultDeny` alone, zero rule stanzas | Verified REJECTED by `Sanitize()` in the vendored v1.19.4 (post-PR#35904 behavior); do not use |
| `sigs.k8s.io/yaml.Marshal` on typed struct | hand-built `map[string]any` / raw YAML string template | Typed struct guarantees schema correctness and matches "same marshal path as generate" instruction; a hand-built map risks drifting from the real CRD shape |
| Reusing `pkg/output.Writer.Write` for `-o` | New minimal atomic-write helper | `Writer.Write` is namespace/workload-directory-shaped and does existing-file merge — wrong semantics for a single arbitrary `-o <path>`; every existing atomic writer (`pkg/output/writer.go`, `pkg/evidence/writer.go`, `pkg/hubble/health_writer.go`) is already a bespoke ~15-line CreateTemp→Write→Chmod→Rename block: a 4th one for bootstrap matches convention, not a deviation |

## Package Legitimacy Audit

Not applicable — this phase introduces **zero new external packages**. All types/functions used are already present in `go.mod` and already imported by existing cpg code (`pkg/policy/builder.go`, `pkg/output/writer.go`, `cmd/cpg/mcp_query.go`). No `pip`/`npm`/`cargo`/`go get` install step is required. Skip the Package Legitimacy Gate.

## Architecture Patterns

### System Architecture Diagram

```
CLI path:                                MCP path:
cpg bootstrap -n <ns>                    get_bootstrap_policy(namespace)
        |                                        |
   PreRunE: -n required, refuse if empty          same validation inline in handler
        |                                        |
   k8s.LoadKubeConfig() (best-effort)      k8s.LoadKubeConfig() (best-effort)
        |                                        |
   kubernetes.NewForConfig (best-effort)   kubernetes.NewForConfig (best-effort)
        |                                        |
   k8s.DetectCiliumVersion(ctx, client)    k8s.DetectCiliumVersion(ctx, client)
        |                                        |
   belowFloorFeatures contains                belowFloorFeatures contains
   "enableDefaultDeny CNP field"?           "enableDefaultDeny CNP field"?
     |escaped: determined & below -> hard error, exit nonzero
     |undetermined -> stderr warning, proceed        (same branch, returned as
     |>= floor -> proceed silently                    tool result field, never
        |                                              a Go error for undetermined)
   pkg/policy.BuildBootstrapPolicy(ns)     pkg/policy.BuildBootstrapPolicy(ns)
     -> *ciliumv2.CiliumNetworkPolicy         (identical builder call)
        |                                        |
   sigs.k8s.io/yaml.Marshal(cnp)            sigs.k8s.io/yaml.Marshal(cnp)
        |                                        |
   -o set? --------+                        return YAML string + compat info
     | no           | yes                        as bootstrapResult (structuredContent)
   stdout       atomic CreateTemp+                   |
                Write+Chmod+Rename              MCP JSON-RPC response (stdout wire)
                (pkg/output-style, CLI-only,
                 never reachable from
                 runMCPServer -> no SEC-01
                 allowlist entry needed)
```

### Recommended Project Structure
```
cmd/cpg/
├── bootstrap.go            # newBootstrapCmd(), runBootstrap, flag parsing/validation
├── bootstrap_test.go        # command-level tests (flag validation, stdout/-o, version gate)
├── mcp_bootstrap.go         # registerBootstrapTool(), bootstrapArgs/bootstrapResult, handler
├── mcp_bootstrap_test.go
└── main.go                  # +1 line: rootCmd.AddCommand(newBootstrapCmd())

pkg/policy/
├── bootstrap_builder.go     # BuildBootstrapPolicy(namespace string) *ciliumv2.CiliumNetworkPolicy
└── bootstrap_builder_test.go  # THE named acceptance-criterion test (Sanitize() + marshal roundtrip
                                # asserting BOTH enableDefaultDeny AND literal "- {}" survive)

pkg/k8s/
└── version.go                # unchanged code; featureFloors already has the 1.16 entry (line 103)

docs/
└── bootstrap-runbook.md      # phase-ordered 1:1 with docs.cilium.io "Creating Policies from Verdicts"

README.md                     # + compat table row (already has the enableDefaultDeny/1.16/PR#30572
                               # row at line 78 from Phase 21 — verify wording covers bootstrap use,
                               # extend if the phrasing is generate-specific)
```

### Pattern 1: Bootstrap CNP builder (verified against vendored v1.19.4)
**What:** Construct a `*ciliumv2.CiliumNetworkPolicy` whose `Spec` passes `Sanitize()` and enforces default-deny on both directions.
**When to use:** `pkg/policy/bootstrap_builder.go`'s single exported function, called identically from both the CLI command and the MCP handler.
**Example:** see Code Examples below (full runnable, empirically verified).

### Pattern 2: CLI command (newBootstrapCmd)
**What:** A single-purpose, non-streaming cobra command — closest existing analog is `cmd/cpg/replay.go`'s simplicity, not `generate.go`'s full `commonFlags` set (bootstrap needs only `-n`/`-o`, not `--all-namespaces`, `--flush-interval`, `--cluster-dedup`, etc. — do **not** call `addCommonFlags(cmd)`).
**Example:**
```go
// Source: pattern mirrors cmd/cpg/replay.go (constructor shape) + cmd/cpg/generate.go
// (version-preflight call site), verified against actual files in this repo.
func newBootstrapCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "bootstrap",
        Short: "Generate a namespaced default-deny bootstrap CiliumNetworkPolicy",
        Args:  cobra.NoArgs,
        RunE:  runBootstrap,
    }
    cmd.Flags().StringP("namespace", "n", "", "target namespace (required)")
    cmd.Flags().StringP("output", "o", "", "write to file instead of stdout")
    _ = cmd.MarkFlagRequired("namespace") // cobra-level required, PreRunE not needed for this alone
    return cmd
}
```

### Pattern 3: MCP readonly tool registration
**What:** `get_bootstrap_policy` follows `cmd/cpg/mcp_query.go`'s `get_policy` shape exactly — a YAML string field inside a typed result struct, `ReadOnlyHint: true`, registered in its own `registerBootstrapTool` function and wired into `runMCPServer` (`cmd/cpg/mcp.go:94-96`) right after `registerQueryTools`.
**Critical constraint (SEC-01):** the handler must call **only** `k8s.LoadKubeConfig`, `kubernetes.NewForConfig`, `k8s.DetectCiliumVersion` (all already reachable from `runMCPServer` today via `start_session` → `resolveSetup` → `detectVersion`, and `pods.List` is not in `k8sWriteVerbs`, `cmd/cpg/mcp_audit_test.go:66-77`) and `pkg/policy.BuildBootstrapPolicy` + `sigs.k8s.io/yaml.Marshal` (pure in-memory). It must **never** call `pkg/output.Writer.Write` or any function in `disallowedFSWrite` (`cmd/cpg/mcp_audit_test.go:29-53`) — doing so would fail `TestMCPAuditReadonlyReachability` and require a new `fsWriteAllowlist` entry, which CONTEXT.md's locked decision explicitly forbids ("zero SEC-01 allowlist entries").
**Example:**
```go
// Source: pattern verified against cmd/cpg/mcp_query.go:63-79 (get_policy registration)
// and cmd/cpg/mcp_audit_test.go (SEC-01 constraints on what the handler may call).
mcp.AddTool(server, &mcp.Tool{
    Name: "get_bootstrap_policy",
    Description: "Returns a namespaced default-deny bootstrap CiliumNetworkPolicy as YAML " +
        "(enableDefaultDeny + explicit ingress/egress presence, cilium/cilium#35558-safe). " +
        "Detects the cluster's Cilium version; refuses below the 1.16 floor when the version " +
        "is determined, proceeds with a warning when undetermined.",
    Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
}, func(ctx context.Context, _ *mcp.CallToolRequest, args bootstrapArgs) (*mcp.CallToolResult, bootstrapResult, error) {
    return handleGetBootstrapPolicy(ctx, args)
})
```

### Anti-Patterns to Avoid
- **`Ingress: []api.IngressRule{}` (empty slice) as "the empty stanza":** fails `Sanitize()` (verified) and is dropped by `omitempty` before it even reaches Sanitize on a real apply — see Pitfall 1.
- **`&api.DefaultDeny{Ingress: true, Egress: true}`** (from `22-PATTERNS.md`'s existing sketch): this type does not exist. The real type is `api.DefaultDenyConfig{Ingress *bool, Egress *bool}` as a **non-pointer** field on `Rule` (`EnableDefaultDeny DefaultDenyConfig`, not `*DefaultDenyConfig`) — assigning `&api.DefaultDeny{...}` will not compile.
- **`api.NewESFromMatchRequirements(nil)`** (also from `22-PATTERNS.md`): this function takes two required parameters (`matchLabels map[string]string, reqs []slim_metav1.LabelSelectorRequirement`) — a single-arg call will not compile. Use `labels.BuildEndpointSelector(nil)` instead (already proven, already in this repo).
- **Calling `pkg/output.Writer.Write` from the MCP tool handler** "for consistency" — it does directory creation + existing-file merge semantics wrong for this artifact, and any filesystem write reachable from `runMCPServer` fails SEC-01's structural audit unless allowlisted (which the locked decision forbids).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| "Select all endpoints in namespace" selector | A raw `api.EndpointSelector{}` zero value or a new helper | `labels.BuildEndpointSelector(nil)` (`pkg/labels/selector.go:149-155`) | Zero-value `api.EndpointSelector{}` has `LabelSelector == nil`, which fails `Sanitize()`'s `"rule must have one of EndpointSelector or NodeSelector"` check (verified); the existing helper already returns the correct `&slim_metav1.LabelSelector{}` non-nil-but-empty form and is already unit-tested elsewhere in this repo |
| Atomic single-file write for `-o` | A generic "write any file atomically" package abstraction | A 4th bespoke CreateTemp→Write→Chmod→Rename block, matching the shape already duplicated 3x (`pkg/output/writer.go`, `pkg/evidence/writer.go`, `pkg/hubble/health_writer.go`) | The codebase's own convention is per-call-site duplication of this exact ~15-line shape, not a shared abstraction — matching it keeps the pattern audit-able and consistent with `cmd/cpg/mcp_audit_test.go`'s per-function allowlist design |
| Version-floor computation | New parsing/comparison logic for "is 1.15 < 1.16" | `pkg/k8s.belowFloorFeatures` + the existing `featureFloors` table (`pkg/k8s/version.go:97-104`), which already has the `"enableDefaultDeny CNP field", "1.16.0"` entry from Phase 21 | Zero new code needed — Phase 21 built exactly this gate; Phase 22 only needs to read `CompatInfo.BelowFloorFeatures` and branch on whether it contains that string |

**Key insight:** every piece of infrastructure this phase needs (version gate, atomic-write shape, "select all" selector, YAML marshal path, MCP readonly-tool registration shape) already exists in this exact repository, already tested, already audited. This phase's only genuinely new code is the ~20-line `BuildBootstrapPolicy` function and its acceptance test — everything else is composition of Phase 17-21's work.

## Common Pitfalls

### Pitfall 1: Empty-array `ingress: []`/`egress: []` does not enforce default-deny (this is the #35558 bug, not its fix)
**What goes wrong:** A CNP built with `Ingress: []api.IngressRule{}` / `Egress: []api.EgressRule{}` either (a) fails `Sanitize()` outright on post-PR#35904 agents (1.17+, silently logged agent-side, `kubectl apply` still reports success) or (b) is accepted-but-non-enforcing on pre-fix agents (exactly at the 1.16 floor this phase targets) — the textbook #35558 silent no-op. Either way, the policy never actually enforces default-deny, which is precisely what success criterion 1 requires it NOT do.
**Why it happens:** `Sanitize()` requires `len(Ingress) > 0 || len(IngressDeny) > 0 || len(Egress) > 0 || len(EgressDeny) > 0` — a zero-length slice is indistinguishable from an omitted field in Go, and `omitempty` on the JSON tags drops it from the marshaled YAML entirely before the field can even be inspected by a human reviewing `kubectl get -o yaml`.
**How to avoid:** Use `Ingress: []api.IngressRule{{}}` / `Egress: []api.EgressRule{{}}` — a one-element list containing a single empty rule object — Cilium's own documented default-deny idiom (YAML `ingress:\n- {}`), confirmed via direct probe to (1) pass `Sanitize()` with no error, (2) marshal with the literal `- {}` token present, (3) combine cleanly with an explicit `enableDefaultDeny`.
**Warning signs:** if the acceptance test only checks "does the YAML contain the substring `enableDefaultDeny`" without ALSO asserting `Sanitize()` succeeds against the real vendored `api.Rule` type, it will pass on the broken construction. The test must call `.Sanitize()` (or an equivalent roundtrip through the real Cilium validation) — not just assert on marshaled string content.

### Pitfall 2: CRD structural-schema pruning on <1.16 clusters (silent field loss, not a Sanitize()-side error)
**What goes wrong:** On a cluster whose CiliumNetworkPolicy CRD predates 1.16, `enableDefaultDeny` is not in the CRD's OpenAPI schema. `kubectl apply` succeeds; the apiserver silently prunes the unknown field before it's even stored (standard `apiextensions.k8s.io/v1` structural-schema behavior — no `x-kubernetes-preserve-unknown-fields: true` override exists anywhere in Cilium's CRD, verified against the vendored CRD YAML). The cilium-agent then applies its own pre-1.16 default-deny heuristic (posture derived purely from rule presence, no `enableDefaultDeny` concept) — which, given the `[{}]` element pattern from Pitfall 1, still happens to work, but the operator's `kubectl get -o yaml` will never show `enableDefaultDeny` on that cluster even though the applied YAML had it.
**Why it happens:** This is standard, intentional Kubernetes API machinery (structural schema pruning), not a Cilium bug — but it looks alarming/confusing to an operator who doesn't expect it.
**How to avoid:** This is exactly why CONTEXT.md's hard-refusal-below-1.16 decision is correct and should not be relitigated — refuse before generating an artifact whose most-explicit field will silently vanish on the target cluster, rather than emit it and let the operator discover the pruning later.

### Pitfall 3: `22-PATTERNS.md`'s existing code sketch does not compile and encodes the broken pattern
**What goes wrong:** `.planning/phases/22-bootstrap-artifact-generation/22-PATTERNS.md` (already present before this research ran) contains a `BuildBootstrapPolicy` sketch using `[]*api.IngressRule{}` (wrong pointer-slice type), `&api.DefaultDeny{...}` (nonexistent type), `api.NewESFromMatchRequirements(nil)` (wrong arity), and empty-array stanzas (Pitfall 1's bug). If the planner or an implementing task pulls code directly from that file, none of it compiles, and the parts that do compile don't enforce default-deny.
**Why it happens:** `22-PATTERNS.md` appears to have been generated from structural/file-role analogy matching (which analog file to imitate) without checking the actual vendored API types — a reasonable division of labor, but its Go snippets are illustrative sketches, not verified code.
**How to avoid:** Use this RESEARCH.md's Code Examples section (empirically run against the real dependency) as the source of truth for exact type names/shapes; treat `22-PATTERNS.md`'s file/pattern *classification* (which existing file each new file most resembles) as still useful, but its inline code as unverified.

### Pitfall 4: Runbook drifting from the real "Creating Policies from Verdicts" phase order
**What goes wrong:** A runbook written from memory of "roughly what Cilium's audit-mode docs say" easily reorders or drops steps, and — given the explicit out-of-scope constraint against ever suggesting daemon-wide `policy-audit-mode` except in the warning — is easy to accidentally under- or over-warn.
**Why it happens:** The upstream doc (`docs.cilium.io/en/stable/security/policy-creation/`) interleaves daemon-wide AND per-endpoint audit-mode instructions across multiple numbered sections; naively summarizing risks blending them.
**How to avoid:** Mirror the exact 11-section order captured in State of the Art below; put the anti-daemon-wide warning in the runbook's first lines (before section 1), and when documenting the runbook's own "enable audit mode" step, present the **per-endpoint** form as the only actionable instruction while explicitly and prominently marking the daemon-wide form as "documented here only to warn against it."

### Pitfall 5: Reusing `PolicyName`/`policyNamePrefix` (`pkg/policy/builder.go:26-32`) for the bootstrap artifact name
**What goes wrong:** `PolicyName(workload)` returns `"cpg-" + workload` — reusing it for bootstrap would produce e.g. `cpg-<namespace>`, not CONTEXT.md's locked `default-deny-<ns>` naming, and would collide with the dedup/merge logic in `pkg/policy/merge.go` (`readExistingPolicy`/`MergePolicy` keyed by that prefix convention) that assumes `cpg-*`-prefixed names are per-workload generate output.
**Why it happens:** `pkg/policy/builder.go` is the closest analog file, tempting reuse of its exported naming helper.
**How to avoid:** `BuildBootstrapPolicy` should construct `"default-deny-" + namespace` directly, independent of `PolicyName`/`policyNamePrefix` — bootstrap artifacts are not generate's per-workload output and must never be merge-target candidates.

## Code Examples

Verified patterns — the following was run directly against `github.com/cilium/cilium v1.19.4` (this repo's exact pinned version) via a standalone Go program in a scratch module; output is pasted verbatim.

### Verified: the correct, Sanitize()-passing, marshal-surviving bootstrap Rule
```go
// Source: empirically verified against github.com/cilium/cilium@v1.19.4
// (pkg/policy/api/rule.go, rule_validation.go) — NOT from training-data memory.
package policy

import (
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/policy/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/SoulKyu/cpg/pkg/labels"
)

// BuildBootstrapPolicy constructs a namespaced default-deny CiliumNetworkPolicy.
// The Ingress/Egress one-element-empty-rule-object pattern ([]api.IngressRule{{}},
// not []api.IngressRule{} — see 22-RESEARCH.md Pitfall 1) is load-bearing: it is
// what makes the policy pass Cilium's own Sanitize() AND actually enforce
// default-deny, independent of the enableDefaultDeny field's own version-gated
// behavior.
func BuildBootstrapPolicy(namespace string) *ciliumv2.CiliumNetworkPolicy {
	t := true
	return &ciliumv2.CiliumNetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cilium.io/v2",
			Kind:       "CiliumNetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-deny-" + namespace,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "cpg",
			},
		},
		Spec: &api.Rule{
			EndpointSelector: labels.BuildEndpointSelector(nil), // "select all" — proven fallback shape
			Ingress:          []api.IngressRule{{}},
			Egress:           []api.EgressRule{{}},
			EnableDefaultDeny: api.DefaultDenyConfig{
				Ingress: &t,
				Egress:  &t,
			},
		},
	}
}
```

### Verified: actual marshaled YAML (sigs.k8s.io/yaml.Marshal, same call cpg's generate path uses)
```yaml
# This is the VERBATIM output of yaml.Marshal(cnp) for the construction above,
# captured by running it against the vendored v1.19.4 dependency.
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: default-deny-demo
  namespace: demo
spec:
  egress:
  - {}
  enableDefaultDeny:
    egress: true
    ingress: true
  endpointSelector: {}
  ingress:
  - {}
status: {}
```
Both success-criterion-1 tokens are present: `enableDefaultDeny` (both directions true) AND non-empty `ingress`/`egress` stanzas (each a one-element list, not omitted, not `[]`). `cnp.Spec.Sanitize()` returns `nil` for this exact struct (verified).

### Verified: what `Sanitize()` actually rejects (do not build this)
```go
// This construction was ALSO run against v1.19.4 and confirmed to fail:
//   err: rule must have at least one of Ingress, IngressDeny, Egress, EgressDeny
// even with EnableDefaultDeny explicitly set to {true, true}. This is the
// #35558 failure mode itself, not a fix for it.
Spec: &api.Rule{
    EndpointSelector:  labels.BuildEndpointSelector(nil),
    Ingress:           []api.IngressRule{},   // empty slice — WRONG
    Egress:            []api.EgressRule{},    // empty slice — WRONG
    EnableDefaultDeny: api.DefaultDenyConfig{Ingress: &t, Egress: &t},
}
```

### Existing version-gate reuse (no new code needed beyond a branch on the string)
```go
// Source: pkg/k8s/version.go:97-104 (Phase 21, already present, unmodified by this phase)
var featureFloors = []struct {
	name  string
	floor string
}{
	{"baseline cpg operation", "1.14.0"},
	{"cilium-dbg binary naming", "1.15.0"},
	{"enableDefaultDeny CNP field", "1.16.0"}, // <- this phase's gate, already declared
}

// Bootstrap-side usage (new code, ~5 lines):
compat := k8s.DetectCiliumVersion(ctx, client, logger) // existing function, unmodified
for _, f := range compat.BelowFloorFeatures {
    if strings.Contains(f, "enableDefaultDeny") {
        if compat.ClusterVersion != "" { // determined AND below floor -> hard refusal
            return fmt.Errorf("cluster Cilium version %s is below the enableDefaultDeny floor "+
                "(>= 1.16.0 required); refusing to generate a bootstrap artifact that would be "+
                "silently pruned by the CRD schema on this cluster", compat.ClusterVersion)
        }
        // else: compat.ClusterVersion == "" means undetermined -> warn-and-proceed (CONTEXT.md)
    }
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| Default-deny via presence of ANY non-empty `ingress`/`egress`/`ingressDeny`/`egressDeny` rule list (implicit posture, no dedicated field) | `enableDefaultDeny: {ingress, egress}` explicit field, still requiring at least one non-empty rule list per `Sanitize()` | 1.16 introduced the field (PR [#30572](https://github.com/cilium/cilium/pull/30572), already cited in this repo's README); PR [#35904](https://github.com/cilium/cilium/pull/35904) (merged, backported to the 1.17 release line) closed the "accepted-but-non-enforcing with zero rules" ambiguity by turning it into a hard `Sanitize()` rejection instead of a silent no-op | On exactly-1.16 clusters (this phase's floor), `enableDefaultDeny` alone with zero rules is still the pre-fix silent-no-op behavior; the `[{}]`-element pattern sidesteps the ambiguity entirely on every version |
| `--policy-audit-mode=true` on the whole cilium-agent DaemonSet (via ConfigMap) | Per-endpoint `cilium-dbg endpoint config PolicyAuditMode=Enabled` for a single `CiliumEndpoint` | Long-standing per-endpoint capability; the runbook's own doc (`docs.cilium.io/en/stable/security/policy-creation/`) documents BOTH forms side by side, with an explicit "not recommended for production" warning on the daemon-wide form | Deferred to Phase 23 (AUD-03), but Phase 22's runbook and its first-lines warning set the framing this phase must not undercut |

**Deprecated/outdated:**
- Treating cilium/cilium#35558 as "still open, still a silent no-op today" — the underlying validation-ambiguity issue was resolved by PR #35904 (closed, backported to 1.17); what remains true on every version, pre- and post-fix, is that `enableDefaultDeny` alone with zero rule stanzas never reliably enforces anything, which is why the `[{}]`-element pattern (not reliance on the fix's rejection behavior) is the correct implementation choice regardless of target-cluster version.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | PR #35904 was backported to the Cilium 1.17 release line specifically (not 1.18+) | State of the Art | Low — this claim only informs framing/prose in the runbook and is not load-bearing for the artifact's correctness; the `[{}]`-element construction works regardless of exactly which version the fix landed in. Sourced from WebFetch of the PR page, not Context7/official-docs-verified. |
| A2 | Cilium's own "Creating Policies from Verdicts" doc's exact 11-section order (Setup / Deploy demo / Scale down / Enable audit daemon-wide / Enable audit per-endpoint / Observe verdicts / Create policy / Disable audit daemon-wide / Disable audit per-endpoint / Verify / Clean-up) reflects the CURRENT `docs.cilium.io/en/stable/` page, not a stale cached/older-version mirror | Common Pitfalls (Pitfall 4), runbook-modeling requirement | Medium — the runbook is a named success criterion (#3) requiring 1:1 phase-order mirroring; if the section order has shifted since this research ran, the runbook-writing task should re-fetch `https://docs.cilium.io/en/stable/security/policy-creation/` directly rather than trust this summary verbatim |

**If this table is empty:** N/A — see above; both entries are low/medium risk and neither affects the artifact-shape finding (which is fully verified, not assumed).

## Open Questions (RESOLVED)

1. **Does the README's existing `enableDefaultDeny` compat row (line 78, from Phase 21) already cover bootstrap's use, or does CONTEXT.md's "gains the bootstrap/enableDefaultDeny floor row" requirement mean a SECOND row / cross-reference is expected?**
   - RESOLVED: extend the existing row's Notes with the `cpg bootstrap` cross-reference + runbook link, no duplicate row — implemented by 22-03-PLAN.md Task 2, keeping all `readme_compat_test.go` pins.
   - What we know: `README.md:78` already reads `| \`enableDefaultDeny\` CNP field | >= 1.16 | PR #30572 |` — generically worded, not tied to any specific command.
   - What's unclear: whether CONTEXT.md's phrasing implies the planner should add prose linking this existing row to `cpg bootstrap` specifically (e.g., "used by `cpg bootstrap`, see docs/bootstrap-runbook.md"), or whether the existing row already satisfies the requirement and no README diff is needed beyond the runbook link.
   - Recommendation: treat this as a small planner discretion item — the safe interpretation is to extend the existing row's prose/cross-reference rather than add a duplicate row, since `readme_compat_test.go` pins the existing row's tokens/PR citations and a naive second row risks satisfying CONTEXT.md's letter while fragmenting the single source of truth COMPAT-01 established.

2. **Should the CLI hard-refusal (determined version < 1.16) exit before or after constructing the artifact in memory?**
   - RESOLVED: gate strictly before `BuildBootstrapPolicy`, mirroring `generate.go`'s preflight-then-pipeline order — implemented by 22-02-PLAN.md Task 1(d).
   - What we know: CONTEXT.md specifies "hard refusal with an actionable error naming the detected version and the floor. No legacy-form emission."
   - What's unclear: whether the version preflight should run strictly before calling `BuildBootstrapPolicy` (cheaper, no wasted work) or whether building-then-discarding is acceptable for code simplicity.
   - Recommendation: gate before building — mirrors `generate.go`'s existing pattern of running `maybeRunVersionPreflight` before the pipeline does any real work, and avoids ever holding a known-broken-for-this-cluster artifact in memory even transiently.

## Environment Availability

Skipped — this phase's only external dependency (a reachable Kubernetes cluster + kubeconfig for version detection) is identical to every existing command (`generate`, `mcp`) and already has warn-and-proceed handling proven in Phase 21; no new tool/runtime/service dependency is introduced.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go's built-in `testing` package + `github.com/stretchr/testify` (already used throughout `cmd/cpg/*_test.go`, `pkg/policy/*_test.go`) |
| Config file | none — standard `go test ./...` |
| Quick run command | `go test ./pkg/policy/... ./cmd/cpg/... -run Bootstrap -count=1` |
| Full suite command | `go test ./... -count=1 -race` (per `MEMORY.md`: sandbox blocks `go` under `make`; run `rtk proxy go test ./... -count=1 -race` directly) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| AUD-02 (criterion 1) | `BuildBootstrapPolicy` output carries `enableDefaultDeny` AND non-empty `ingress`/`egress`, and `Spec.Sanitize()` returns nil (the actual #35558 regression guard — assert on Sanitize(), not just substring match) | unit | `go test ./pkg/policy/... -run TestBuildBootstrapPolicy -v` | ❌ Wave 0 |
| AUD-02 (criterion 2, determined+below-floor) | `cpg bootstrap -n ns` against a mocked/injected `CompatInfo{ClusterVersion:"1.15.0", BelowFloorFeatures:[...]}` exits nonzero with an actionable message naming both the detected version and 1.16 | unit/CLI | `go test ./cmd/cpg/... -run TestBootstrapVersionGate -v` | ❌ Wave 0 |
| AUD-02 (criterion 2, undetermined) | Same command with `CompatInfo{Source:"undetermined"}` proceeds, emits a stderr warning, still produces artifact | unit/CLI | `go test ./cmd/cpg/... -run TestBootstrapUndeterminedVersion -v` | ❌ Wave 0 |
| AUD-02 (criterion 3) | `docs/bootstrap-runbook.md` never mentions daemon-wide `policy-audit-mode` outside its first-lines warning block | golden/text | `go test ./cmd/cpg/... -run TestRunbookNeverSuggestsDaemonWideAudit -v` (mirrors `readme_compat_test.go`'s `strings.Contains`-based pinning approach) | ❌ Wave 0 |
| AUD-02 (criterion 4) | `TestMCPAuditReadonlyReachability` (existing SEC-01 audit) still passes with zero new `fsWriteAllowlist`/`k8sWriteVerbs` entries after `get_bootstrap_policy` is wired in | integration (SSA whole-program) | `go test ./cmd/cpg/... -run TestMCPAuditReadonlyReachability -v` (existing test, ~45-76s under `-race` per its own doc comment) | ✅ (`cmd/cpg/mcp_audit_test.go`) — reused unmodified, no new test needed, just must keep passing |

### Sampling Rate
- **Per task commit:** `go test ./pkg/policy/... ./cmd/cpg/... -run Bootstrap -count=1`
- **Per wave merge:** `go test ./... -count=1 -race`
- **Phase gate:** Full suite green (including the existing `TestMCPAuditReadonlyReachability` and `TestReadmeCompatSection`) before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `pkg/policy/bootstrap_builder_test.go` — covers AUD-02 criterion 1 (the named #35558 regression test, asserting `Sanitize()` succeeds AND marshaled YAML contains both `enableDefaultDeny` and the `- {}` rule-element tokens)
- [ ] `cmd/cpg/bootstrap_test.go` — covers AUD-02 criterion 2 (both branches: hard refusal + undetermined warn-and-proceed), needs a test seam for `k8s.DetectCiliumVersion` similar to how `generate_test.go`/`mcp_test.go` substitute `l7ClientFactory`/`detectVersionFn`
- [ ] `docs/bootstrap-runbook.md` golden test — no file exists yet; mirror `cmd/cpg/readme_compat_test.go`'s `strings.Contains` pinning style

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | Bootstrap reuses the operator's existing kubeconfig; no new auth surface |
| V3 Session Management | no | Not session-scoped (unlike `start_session`/query tools) — bootstrap is stateless per call |
| V4 Access Control | yes | Version detection reuses the existing `pods/list` RBAC verb already required by every other command; bootstrap itself requires **no new RBAC verb** — it never calls the K8s API for anything beyond the existing read |
| V5 Input Validation | yes | `-n`/`namespace` must be validated as a legal Kubernetes namespace name before use in `metadata.namespace` — reuse `evidence.ValidatePolicyRef`-style validation or Kubernetes' own namespace-name regex; an unvalidated namespace string flowing into YAML risks malformed output, not injection (YAML is generated via typed struct marshal, not string concatenation, so YAML-injection is structurally prevented) |
| V6 Cryptography | no | Not applicable — no cryptographic operation in this phase |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Silently-non-enforcing "default-deny" artifact applied in production, operator believes traffic is blocked when it is not | Repudiation / false sense of security (not a classic STRIDE-Tampering case, but the phase's core threat model) | The `[{}]`-element construction (Pitfall 1) + the named Sanitize()-asserting acceptance test are the mitigation; this is the single most important control in this entire phase |
| CRD field silently pruned on a version-mismatched cluster, operator unaware their policy is weaker than intended | Tampering (unintended, structural) | Hard refusal below the determined 1.16 floor (already a locked CONTEXT.md decision); this research confirms the mechanism (structural-schema pruning) that makes the refusal necessary |
| MCP tool handler accidentally introduces a filesystem write, weakening the "readonly by default" guarantee other tools rely on | Elevation of Privilege (structural) | `TestMCPAuditReadonlyReachability` (existing SEC-01 SSA audit) — must be re-run and must show zero new allowlist entries after this phase's tool is added |

## Sources

### Primary (HIGH confidence)
- Direct execution against `github.com/cilium/cilium@v1.19.4` (`pkg/policy/api/rule.go`, `rule_validation.go`, `rule_validation_test.go`, `rules_test.go`) — the exact dependency version pinned in this repo's `go.mod`, run via a standalone Go program in this session (see Code Examples)
- `/home/gule/Workspace/team-infrastructure/cpg/pkg/k8s/version.go` (Phase 21, this repo) — `featureFloors`, `DetectCiliumVersion`, `CompatInfo`
- `/home/gule/Workspace/team-infrastructure/cpg/pkg/policy/builder.go`, `pkg/output/writer.go`, `pkg/labels/selector.go`, `cmd/cpg/mcp_query.go`, `cmd/cpg/mcp_audit_test.go`, `cmd/cpg/mcp.go`, `cmd/cpg/generate.go`, `cmd/cpg/main.go`, `cmd/cpg/readme_compat_test.go`, `README.md` — all read directly this session
- Cilium CRD YAML, `github.com/cilium/cilium@v1.19.4/pkg/k8s/apis/cilium.io/client/crds/v2/ciliumnetworkpolicies.yaml` — checked directly for `preserveUnknownFields`/`x-kubernetes-preserve-unknown-fields` (absent, confirming default pruning behavior applies)

### Secondary (MEDIUM confidence)
- [Default Deny Ingress Policy — Cilium docs](https://docs.cilium.io/en/latest/network/servicemesh/default-deny-ingress-policy/) — WebFetch, official docs, current stable
- [cilium/cilium#35558](https://github.com/cilium/cilium/issues/35558) — WebSearch + WebFetch summary, official GitHub issue, cross-checked against the vendored source's actual `Sanitize()` test cases
- [cilium/cilium#35904](https://github.com/cilium/cilium/pull/35904) — WebFetch summary of the merged PR that resolved #35558's ambiguity
- [Creating Policies from Verdicts — Cilium docs](https://docs.cilium.io/en/stable/security/policy-creation/) — WebFetch, official docs, the runbook's structural model
- [Layer 3 Examples — Cilium docs](https://docs.cilium.io/en/stable/security/policy/language/) — WebSearch summary confirming the `- {}` legacy default-deny pattern's official documentation status

### Tertiary (LOW confidence)
- None — every claim above was either verified against the vendored source directly or cross-checked against official Cilium documentation.

## Metadata

**Confidence breakdown:**
- Artifact shape / Sanitize() behavior: HIGH — empirically verified by running code against the exact pinned dependency version, not inferred
- CRD pruning behavior on <1.16: MEDIUM-HIGH — verified the absence of `preserveUnknownFields` override (a direct fact), the pruning consequence itself is standard, well-documented Kubernetes API machinery behavior (not Cilium-specific), not independently re-verified against a live <1.16 cluster in this session
- Runbook phase order: MEDIUM — WebFetch-summarized from the current official docs page, not independently cross-checked against a second source or a version-pinned archive
- MCP/SEC-01 integration pattern: HIGH — read directly from this repo's existing, passing test (`cmd/cpg/mcp_audit_test.go`) and existing tool registrations

**Research date:** 2026-07-22
**Valid until:** 30 days (stable domain — the vendored Cilium version is pinned in `go.mod` and will not silently drift; re-verify only if `go.mod`'s cilium version bumps before this phase is planned/implemented)
