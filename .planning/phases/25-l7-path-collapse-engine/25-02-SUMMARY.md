---
phase: 25-l7-path-collapse-engine
plan: 02
subsystem: testing
tags: [l7, http, golden-regression, byte-identity, mcp, session-args, fail-closed, parity, cilium]

requires:
  - phase: 25-l7-path-collapse-engine (plan 01)
    provides: collapse engine behind PipelineConfig fields + CLI flags, ReplaceL7HTTP merge, DefaultMinFlowsPerL7Rule/ResolveMinFlowsPerL7Rule
  - phase: 17-session-lifecycle
    provides: startSessionArgs → session.StartArgs → buildPipelineConfig plumbing (include_audit mirror surface)
provides:
  - Byte-identity golden regression: TestL7NoCollapseByteIdentity proves default-path emission is byte-identical to the pre-phase-25 build (ROADMAP SC2, L7GEN-01 criterion 2), with committed per-fixture golden DIRECTORIES (l7_http = 1 policy; mixed_tcp_icmp = server + client)
  - MCP session args collapse_l7_paths / min_flows_per_l7_rule with fail-closed validation (negative rejected before Manager.Start, argument-naming error) and 0/omitted → DefaultMinFlowsPerL7Rule(5) via policy.ResolveMinFlowsPerL7Rule (T-25-05, L7GEN-03)
  - CLI-vs-MCP parity proof: TestMCPCLICollapseParity drives the same shared fixture through fakeRelay+MCP subprocess and RunPipelineWithSource; identical collapsed (Method, Path) sets (ROADMAP SC5)
affects: [verify-work (phase 25), docs, explain/evidence consumers untouched by design]

actuals:
  tokens: 7400
  tasks: 3
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Per-fixture golden DIRECTORIES mirroring the emitted <ns>/<workload>.yaml tree, with pinned emission-count asymmetry (1 vs 2 policies)"
    - "Parity comparison as parsed sets of (Method, Path) pairs extracted from CNP YAML (spec.ingress/egress[].toPorts[].rules.http[]), not raw bytes"

key-files:
  created:
    - pkg/hubble/pipeline_l7_golden_test.go
    - testdata/golden/l7-collapse-regression/l7_http/production/api-server.yaml
    - testdata/golden/l7-collapse-regression/mixed_tcp_icmp/production/server.yaml
    - testdata/golden/l7-collapse-regression/mixed_tcp_icmp/production/client.yaml
  modified:
    - cmd/cpg/mcp_tools.go
    - cmd/cpg/mcp_session_test.go
    - cmd/cpg/mcp_e2e_test.go
    - pkg/session/session.go
    - pkg/session/pipeline_config.go
    - pkg/session/pipeline_config_test.go

key-decisions:
  - "Golden source = the PRE-PHASE-25 commit e4159a8 (plan's documented fallback), not the v1.6.0 tag: executed verification showed tag v1.6.0 is the old product 1.6.0 release from the v1.1-milestone era (no --l7 flag, no l7 fixtures), so the pinned tag procedure was impossible; the fallback matches ROADMAP SC2's own 'pre-phase golden files' language and T-25-04's intent exactly. Provenance recorded in the test header"
  - "Parity test gate = 2, not the plan text's 5: the mixed-classes fixture carries exactly 2 distinct paths per segment-class group (the 25-01 fixture-design precedent), so gate 5 would emit only literals and fail the plan's own acceptance criterion requiring numeric/0x-hex/hex-run/UUID templates in the parity set. Both surfaces use the identical value; the parity must-have (same collapsed rules on both surfaces) is strengthened, not weakened"
  - "Task 3 required NO production change — CLI-vs-MCP parity held out of the box once Task 2's args landed; the only RED-phase fix was to the parity test's own CNP-YAML parser helper (http rules live under spec.ingress[].toPorts[].rules.http[], one http level), so Task 3 lands as a single test commit"

patterns-established:
  - "Pattern: golden regression with provenance comment — the test header records the exact worktree+replay+copy commands so a reviewer can re-derive goldens; a diff is always fixed in code, never in the golden"
  - "Pattern: MCP arg mirror — startSessionArgs fields (omitempty + jsonschema descriptions mirroring CLI help) → package-main validation before mgr.Start → StartArgs → buildPipelineConfig with single-site default resolution (policy.ResolveMinFlowsPerL7Rule)"

requirements-completed: [L7GEN-01, L7GEN-03]

coverage:
  - id: D1
    description: "Default-path byte-identity: with L7Enabled and all collapse fields zero, emitted YAML is byte-identical to the pre-phase-25 build on l7_http (exactly 1 policy) and mixed_tcp_icmp (exactly 2 policies: server INGRESS→destination, client EGRESS→source)"
    requirement: L7GEN-01
    verification:
      - kind: integration
        ref: "pkg/hubble/pipeline_l7_golden_test.go#TestL7NoCollapseByteIdentity"
        status: pass
    human_judgment: false
  - id: D2
    description: "MCP min_flows_per_l7_rule is fail-closed: negative rejected before Manager.Start with an argument-naming error; omitted/0 resolves to 5 via ResolveMinFlowsPerL7Rule in exactly one place; tools/list advertises both new start_session properties"
    requirement: L7GEN-03
    verification:
      - kind: integration
        ref: "cmd/cpg/mcp_session_test.go#TestMCPCollapseArgs"
        status: pass
      - kind: unit
        ref: "pkg/session/pipeline_config_test.go#TestBuildPipelineConfig_CollapseDefaults"
        status: pass
    human_judgment: false
  - id: D3
    description: "CLI-vs-MCP parity: same mixed-classes fixture through fakeRelay+MCP subprocess and RunPipelineWithSource with identical PipelineConfig values yields exactly the same collapsed (Method, Path) set — 4 class templates + version/mixed/encoded literals"
    requirement: L7GEN-03
    verification:
      - kind: e2e
        ref: "cmd/cpg/mcp_e2e_test.go#TestMCPCLICollapseParity"
        status: pass
    human_judgment: false

duration: ~95min
completed: 2026-10-04
status: complete
---

# Phase 25, Plan 02: MCP Parity + Byte-Identity Golden Regression Summary

**Default-path emission is regression-pinned byte-identical to the pre-phase-25 build, and the collapse flags are fully mirrored through the MCP session surface with fail-closed gate validation and proven CLI-vs-MCP rule parity.**

## Performance

- **Duration:** ~95 min
- **Started:** 2026-10-04T19:00Z
- **Completed:** 2026-10-04T20:35Z
- **Tasks:** 3 of 3
- **Files modified:** 10 (4 created, 6 modified)

## Accomplishments

- SC2 (L7GEN-01 criterion 2) closed: `TestL7NoCollapseByteIdentity` compares the emitted `<ns>/<workload>.yaml` tree file-for-file, raw-byte, against committed goldens generated from the pre-phase-25 build; emission-count asymmetry pinned (l7_http = 1 policy; mixed_tcp_icmp = 2 — aggregator `policyTargetEndpoint` routes INGRESS→destination/server, EGRESS→source/client)
- T-25-05 closed on the MCP half: `min_flows_per_l7_rule: -1` is rejected before `Manager.Start` with an argument-naming error; 0/omitted resolves to `DefaultMinFlowsPerL7Rule` (5) via `policy.ResolveMinFlowsPerL7Rule` — one defaulting site, no second
- SC5 (L7GEN-03) proven: `TestMCPCLICollapseParity` feeds the identical fixture file to both surfaces; the collapsed (Method, Path) sets match exactly and contain the full class inventory (numeric, 0x-hex, hex-run, UUID templates + version/mixed/encoded literals)
- Full `go test ./... -count=1 -race` green (13 packages)

## Task Commits

1. **Task 1: byte-identity regression (TDD)** — `6cc171b` (test, RED verified: golden dirs absent → FAIL) + `40cfe09` (feat: pre-phase-25 goldens, GREEN)
2. **Task 2: MCP collapse args (TDD)** — `15e6de0` (test, RED: fields unknown → compile fail) + `3eb7229` (feat: mcp_tools.go/session.go/pipeline_config.go wiring)
3. **Task 3: CLI-vs-MCP parity** — `fbdcbed` (test; single commit — no production change was required, see Deviations)

**Plan metadata:** `docs(25-02): add execution summary` (this commit, includes STATE.md/ROADMAP.md progress updates)

## Files Created/Modified

- `pkg/hubble/pipeline_l7_golden_test.go` (new) — `TestL7NoCollapseByteIdentity` + provenance comment with the exact generation commands
- `testdata/golden/l7-collapse-regression/{l7_http,mixed_tcp_icmp}/production/*.yaml` (new) — pre-phase-25 goldens mirroring the emitted tree
- `cmd/cpg/mcp_tools.go` — `startSessionArgs.CollapseL7Paths`/`.MinFlowsPerL7Rule` (omitempty + jsonschema descriptions mirroring the CLI help), fail-closed `< 0` rejection in the `registerSessionTools` handler before `mgr.Start`, both forwarded into `session.StartArgs`
- `pkg/session/session.go` — `StartArgs.CollapseL7Paths`/`.MinFlowsPerL7Rule` with IncludeAudit-style constraint comments
- `pkg/session/pipeline_config.go` — mapping via `policy.ResolveMinFlowsPerL7Rule` (single defaulting site)
- `cmd/cpg/mcp_session_test.go` — `TestMCPCollapseArgs` (negative rejected + naming, omitted-gate start succeeds, schema advertises both properties)
- `pkg/session/pipeline_config_test.go` — `TestBuildPipelineConfig_CollapseDefaults` (0→5, 7→7, all-zero harmless)
- `cmd/cpg/mcp_e2e_test.go` — `TestMCPCLICollapseParity` + `parseFixtureFlows` (protojson fixture → fakeRelay) + `parsePolicyHTTPRules`/`parityRuleSet` helpers

## Decisions Made

See `key-decisions` above (golden source fallback; parity gate 2; Task 3 single commit). All other decisions followed the plan verbatim; the MCP arg surface mirrors the include_audit pattern exactly (PATTERNS.md Pattern 3).

## Deviations from Plan

1. **Golden source: pre-phase-25 commit `e4159a8` instead of the v1.6.0 tag.** The plan's pinned procedure (`git worktree add /tmp/cpg-v1.6.0 v1.6.0`) was verified impossible: tag v1.6.0 (commit 76665a5, product release 1.6.0 from the v1.1-milestone era) predates the L7 work entirely — its `cpg replay` has no `--l7` flag and its testdata has no `l7_http.jsonl`/`mixed_tcp_icmp.jsonl`. The plan's documented fallback ("else the pre-phase commit") was applied: goldens generated from a worktree at `e4159a8` (parent of the first phase-25 commit `9ef0028`), which is exactly the "pre-phase golden files" source ROADMAP SC2 pins and the build T-25-04 protects. The provenance comment in `pipeline_l7_golden_test.go` records the executed commands. Does not contradict any must_have or threat control — it is the same build the threat model targets.
2. **Parity test gate value 2 instead of 5.** The plan's Task 3 behavior text says "min_flows 5 on both surfaces", but its own acceptance criterion requires the parity set to contain numeric/0x-hex/hex-run/UUID templates — impossible at gate 5 on the mixed-classes fixture (2 distinct paths per class group, per 25-01's documented fixture design). Gate 2 is used identically on both surfaces; parity semantics unchanged and strengthened (actual templates compared, not literals-only).
3. **Task 3 landed as one test commit.** RED was verified (the test initially failed on its own CNP-YAML parser helper — rules are nested at `spec.ingress[].toPorts[].rules.http[]`, not `spec.rules.http`); after the harness fix the test passed with zero production changes, so there was no feat half to commit. Forcing an empty production commit was rejected.

## Environment Note

- This sandbox OOM-kills the memory-heavy `TestMCPAuditReadonlyReachability` SSA whole-program audit under default GC settings (documented 45-76s / multi-GB); the full suite was run as `GOMEMLIMIT=4GiB GOGC=50 go test ./... -count=1 -race -timeout 900s` → exit 0, all 13 packages ok. No `make` exists in this repo; no `--no-verify` used.

## Self-Check: PASSED

- Golden dirs exist on disk with the provenance comment naming the pre-phase-25 commit and exact commands; `mixed_tcp_icmp/` carries both server + client policies
- `go test ./pkg/hubble/ -run TestL7NoCollapseByteIdentity -race -count=1` → ok
- `go test ./cmd/cpg/ ./pkg/session/ -run 'TestMCPCollapseArgs|TestBuildPipelineConfig_CollapseDefaults' -race -count=1` → ok
- `go test ./cmd/cpg/ -run 'TestMCPCollapseArgs|TestMCPCLICollapseParity' -race -count=1 -timeout 300s` → ok
- `go build ./...` → ok
- `GOMEMLIMIT=4GiB GOGC=50 go test ./... -count=1 -race -timeout 900s` → exit 0 (13 packages ok)
- `grep` proofs: `collapse_l7_paths,omitempty` + `min_flows_per_l7_rule,omitempty` in mcp_tools.go; `CollapseL7Paths`/`MinFlowsPerL7Rule` in session.go StartArgs and pipeline_config.go
