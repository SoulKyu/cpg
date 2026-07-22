---
phase: 22-bootstrap-artifact-generation
reviewed: 2026-07-22T15:20:04Z
depth: standard
files_reviewed: 15
files_reviewed_list:
  - pkg/policy/bootstrap_builder.go
  - pkg/policy/bootstrap_builder_test.go
  - cmd/cpg/bootstrap.go
  - cmd/cpg/bootstrap_test.go
  - cmd/cpg/mcp_bootstrap.go
  - cmd/cpg/mcp_bootstrap_test.go
  - cmd/cpg/mcp.go
  - cmd/cpg/main.go
  - cmd/cpg/mcp_audit_test.go
  - cmd/cpg/mcp_e2e_test.go
  - cmd/cpg/mcp_query_tools_test.go
  - cmd/cpg/runbook_test.go
  - cmd/cpg/readme_compat_test.go
  - README.md
  - docs/bootstrap-runbook.md
findings:
  critical: 0
  warning: 2
  info: 3
  total: 5
status: issues_found
---

# Phase 22: Code Review Report

**Reviewed:** 2026-07-22T15:20:04Z
**Depth:** standard
**Files Reviewed:** 15
**Status:** issues_found

## Summary

Phase 22 adds `cpg bootstrap` (CLI) and `get_bootstrap_policy` (MCP) surfaces that
emit a namespaced default-deny `CiliumNetworkPolicy`, plus the supporting builder,
version gate, runbook, and pinning tests. Build passes (`go build ./...`), and all
phase tests pass (`pkg/policy`, `cmd/cpg` bootstrap/runbook/readme/e2e/query pins).

All six primary phase invariants hold under verification:

1. **Artifact form** — `enableDefaultDeny: {ingress: true, egress: true}` +
   one-element empty-rule stanzas (`- {}`). Verified via `Sanitize()` assertion on
   the real vendored `api.Rule` (not a substring check) and empirically: the
   `ingress: []` / `egress: []` bug form (#35558) never appears.
2. **Version gate** — hard refusal fires on a determined below-floor version
   *before* artifact construction; warn-and-proceed on undetermined. CLI and MCP
   both call the single `bootstrapVersionGate` verbatim — no drift. The
   `strings.Contains(f, "enableDefaultDeny")` match is robust against the actual
   floor string `"enableDefaultDeny CNP field (requires >= 1.16.0)"`, and
   `finalizeCompat` (version.go:163,301) genuinely populates `BelowFloorFeatures`
   on the live path.
3. **MCP honesty** — `ReadOnlyHint: true` is honest (detection reads + marshal
   only, zero fs writes), YAML returned as tool-result content only,
   `fsWriteAllowlist` unchanged at its five pre-phase entries (the diff adds only a
   NOTE comment).
4. **stdout-only** — `runBootstrap` has a single `Fprintln` sink; no `-o` flag is
   registered on the command and none is inherited (root persistent flags are only
   `debug`/`log-level`/`json`; `-o`/`--output-dir` lives on `addCommonFlags`, which
   bootstrap deliberately skips).
5. **Runbook** — `policy-audit-mode` hyphenated token appears only in the leading
   warning block; capture step references `cpg generate --include-audit`; README
   compat row extended in place; all `readme_compat_test` pins intact.
6. **Zero new deps** — no `go.mod`/`go.sum` change in the diff.

**Security posture on hostile namespace input is sound**: output flows through a
typed-struct `sigs.k8s.io/yaml` marshal, so a newline-injection namespace
(`"a\ninjected: true"`) is escaped as a YAML block scalar (`|-`) and cannot inject
a sibling key. No YAML/path injection exists.

Two warnings concern doc/code drift and a missing-validation robustness gap; three
info items note minor comment drift and cosmetics.

## Warnings

### WR-01: Runbook documents a non-existent `-o`/`--output` flag for `cpg bootstrap`

**File:** `docs/bootstrap-runbook.md:48-49`
**Issue:** The "Bootstrap the Namespace" section says: *"Prefer to review before
applying? Use `-o`/`--output` to write the artifact to a file instead of piping
it..."* No such flag exists — `bootstrap.go` registers only `-n/--namespace`
(bootstrap.go:47), does not call `addCommonFlags`, and inherits no persistent
output flag. This directly contradicts phase invariant 4 (stdout-only, the SEC-01
simplification) and the command's own `Long` help, which uses shell redirection
(`> default-deny-production.yaml`, bootstrap.go:40). An operator following the
runbook runs `cpg bootstrap -n <ns> -o file.yaml` and gets
`unknown shorthand flag: 'o'`. The `-o/--output-dir` reference at runbook:119 is
correct because it applies to `cpg generate`, not bootstrap — only the bootstrap
section is wrong. `runbook_test.go` does not catch this (it pins only the
`policy-audit-mode` token and the `--include-audit` reference).
**Fix:** Replace the sentence with the redirection form the command actually
supports:
```markdown
Prefer to review before applying? Redirect to a file
(`cpg bootstrap -n <namespace> > default-deny-<namespace>.yaml`), inspect it,
then `kubectl apply -f` it yourself.
```

### WR-02: No namespace validation — invalid namespaces silently produce a malformed artifact

**File:** `pkg/policy/bootstrap_builder.go:35-36`, `cmd/cpg/bootstrap.go:114-116`, `cmd/cpg/mcp_bootstrap.go:64-66`
**Issue:** All three entry points guard only against the empty string; none
validates the namespace as a DNS-1123 label before it flows into
`metadata.name = "default-deny-" + namespace` and `metadata.namespace`. Hostile /
invalid inputs (`"Foo Bar"`, `"../etc"`, a value with a newline) are accepted and
emitted as syntactically-valid YAML with an invalid k8s name/namespace (verified
empirically). This is **not** a security hole — the typed-struct marshal escapes
newlines as a block scalar, so no key injection is possible — but because the
documented happy path pipes straight to `kubectl apply -f -` (bootstrap.go:43,
runbook:39), an invalid namespace surfaces only as a confusing server-side
rejection instead of a clear client-side error.
**Fix:** Validate before building the artifact (shared, to preserve CLI/MCP
parity), e.g. in `runBootstrap`/`handleGetBootstrapPolicy` after the empty check:
```go
if errs := validation.IsDNS1123Label(namespace); len(errs) > 0 {
    return fmt.Errorf("invalid namespace %q: %s", namespace, strings.Join(errs, "; "))
}
```
using `k8s.io/apimachinery/pkg/util/validation` (already in the module tree — no
new dep).

## Info

### IN-01: Stale `-n/-o` comment on the bootstrap constructor

**File:** `cmd/cpg/bootstrap.go:23`
**Issue:** The doc comment says *"bootstrap needs only -n/-o, none of
generate/replay's streaming-pipeline flag set"* — but the command registers only
`-n`. The `-o` reference is the same drift that produced WR-01 and should be
corrected in lockstep.
**Fix:** Change "-n/-o" to "-n".

### IN-02: `enableDefaultDeny` pointers share one backing variable

**File:** `pkg/policy/bootstrap_builder.go:28,46-47`
**Issue:** `t := true` is declared once and `&t` is assigned to both
`EnableDefaultDeny.Ingress` and `.Egress`. Harmless today (both are `true` and the
struct is never mutated after construction), but a future edit that dereferences
and flips one pointer would silently flip the other — a shared-backing-var
footgun.
**Fix:** Use two locals, or `api.DefaultDenyConfig{Ingress: ptr.To(true), Egress: ptr.To(true)}`.

### IN-03: `bootstrapResult.VersionSource` can be emitted empty despite its schema description

**File:** `cmd/cpg/mcp_bootstrap.go:29,84`
**Issue:** `VersionSource` has no `omitempty` and its jsonschema promises
`"pod-images, get-nodes, or undetermined"`, but on the undetermined path it echoes
`compat.Source` verbatim, which can be `""` when detection returns an
undetermined `CompatInfo` without setting `Source` (the CLI test stub sets it to
`"undetermined"`, but the live `bootstrapDetectVersion` early-return paths in
bootstrap.go:66,71 do set it, so this is edge-only cosmetics).
**Fix:** Default an empty source to `"undetermined"` before returning, so the
field always matches its documented enum.

---

_Reviewed: 2026-07-22T15:20:04Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
