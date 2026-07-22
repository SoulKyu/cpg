# Phase 24: cpg-Dedicated Skills & Agent Tooling - Research

**Researched:** 2026-07-22
**Domain:** Claude Code skill/agent authoring (repo-local `.claude/skills/*`, `.claude/agents/*`) + a Go consistency tripwire over the existing cpg MCP tool registry
**Confidence:** HIGH

## Summary

This phase produces pure markdown (5 `SKILL.md` files + 1 agent `.md` file) plus one new Go test file. There is no new runtime code path in `cmd/cpg`'s MCP server — the phase's only Go change is a test that reads the existing tool registry (already fully built by Phases 17/18/22) via the in-memory harness already living in `mcp_harness_test.go`, and cross-checks it against the new markdown. Frontmatter conventions were verified directly from this machine's own `~/.claude/skills/*` (template-skill, skill-creator) and this repo's own pre-existing `.claude/skills/desloppify/SKILL.md`: a skill needs only `name:` and `description:` in YAML frontmatter — no `tools:` field (that's agent-only). Agent frontmatter was verified from `~/.claude/agents/*.md` (gsd-executor.md, gsd-domain-researcher.md): `name:`, `description:`, `tools:` (comma-separated allowlist), optional `color:`.

The MCP tool registry is fixed at exactly 9 tools, verified three independent ways in this session: (1) direct read of `cmd/cpg/mcp.go`'s `runMCPServer` composition root (`registerSessionTools` + `registerQueryTools` + `registerBootstrapTool`), (2) grep of every `Name: "..."` literal across `mcp_tools.go`/`mcp_query.go`/`mcp_query_evidence.go`/`mcp_query_flows.go`/`mcp_bootstrap.go`, (3) the existing `TestMCPE2EGracefulLifecycle` in `mcp_e2e_test.go` (line 412) which already asserts `require.Len(t, toolsResult.Tools, 9, "3 session + 5 query tools + get_bootstrap_policy")`. The 9 names: `start_session`, `get_status`, `stop_session`, `list_dropped_flows`, `list_policies`, `get_policy`, `get_evidence`, `get_cluster_health`, `get_bootstrap_policy`.

**Primary recommendation:** Build the tripwire (`cmd/cpg/skills_test.go`) on the exact same `startInMemoryMCPSession` + `mcp.NewClient(...).Connect(...)` + `cs.ListTools(ctx, nil)` pattern `mcp_harness_test.go`/`mcp_e2e_test.go` already use — zero new test infrastructure — and gate phantom/coverage checks on the fact that every one of the 9 current tool names begins with `start_`/`get_`/`stop_`/`list_` (a real, load-bearing naming convention, not a coincidence: session tools are verb-first lifecycle calls, query/bootstrap tools are all read accessors).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Tool semantics (names, args, descriptions) | cpg MCP Server (Go, `cmd/cpg/mcp_*.go`) | — | Single source of truth (locked by CONTEXT.md's Router Principle); skills/agent must never restate this |
| Workflow routing / step ordering | Claude Code Skill Layer (`.claude/skills/cpg-*/SKILL.md`) | Claude Code Agent Layer (`.claude/agents/cpg-operator.md`) | Skills are pure routers; the operator agent is the one place that actually drives multi-step MCP sessions |
| Live tool discovery at invocation time | Claude Code runtime (`tools/list` MCP call, `--help` for CLI) | — | Explicitly locked by CONTEXT.md — skills never hardcode a second copy of tool semantics |
| Consistency enforcement (no phantom tools, coverage floor, count pin) | Go Test / CI (`cmd/cpg/skills_test.go`) | — | Only a compiled, CI-run test can fail a build; markdown alone cannot self-enforce |
| Human-run CLI onboarding steps (`cpg bootstrap`, `cpg audit-window`) | Operator (human, via CLI) | Claude Code Skill Layer (guides, never runs) | Locked "Variant B" decision in CONTEXT.md — the skill GUIDES, never executes these |
| HTML health report rendering | Claude Code Skill Layer (script/asset bundled in `cpg-health-report`) | pkg/hubble (`cluster-health.json` as data source) | Skill consumes an existing Go-produced artifact; no new Go code needed |

## User Constraints (from CONTEXT.md)

<user_constraints>
### Locked Decisions

**Deliverable Set (all six SKL items)**
- Five skills: `cpg-triage` (SKL-01, live MCP session end-to-end), `cpg-audit-onboard` (SKL-02, full onboarding workflow), `cpg-policy-review` (SKL-03, offline CNP audit), `cpg-health-report` (SKL-04, cluster-health.json → HTML report), `cpg-mcp-smoke` (SKL-05, post-release smoke vs e2e fake relay).
- Build the `cpg-operator` subagent (SKL-06): single repo-local agent driving MCP sessions, referenced by `cpg-triage` and `cpg-audit-onboard` instead of per-skill agents.
- Layout: `.claude/skills/cpg-<name>/SKILL.md` with standard Claude Code skill frontmatter (`name`, `description`); `.claude/agents/cpg-operator.md` with agent frontmatter. Nothing outside the repo.

**Router Principle (anti-drift, locked by REQUIREMENTS)**
- Skills NEVER restate tool argument schemas or result shapes. They name tools and route workflow steps; the harness discovers semantics live via `tools/list` (MCP) and `--help` (CLI). Tool names may appear; parameter-level detail may not.
- CLI-only surfaces from the gate decision are respected: `cpg-audit-onboard` GUIDES the operator through `cpg bootstrap` and `cpg audit-window` (human-run CLI commands, matching the Variant B decision) and DRIVES the capture via MCP `start_session` with `include_audit: true`.

**Consistency Tripwire (success criterion 5)**
- One Go test (cmd/cpg, alongside the existing golden-pin tests) that: (1) enumerates the registered MCP tool names from the same registration path the server uses; (2) asserts every tool name referenced in any `.claude/skills/cpg-*/SKILL.md` and `.claude/agents/cpg-operator.md` exists in that registry (no phantom tools); (3) asserts every registered tool name is mentioned by at least the smoke skill (coverage floor); (4) pins the tool COUNT (now 9, including Phase 22's `get_bootstrap_policy`) so a future tool addition forces a skill sweep. Grep-based prose checks stay shallow (names only) per the router principle.
- README gets a short "Agent tooling" section listing the skills; pinned by the same test class.

**Smoke Scope (SKL-05)**
- `cpg-mcp-smoke` asserts the real handshake tool count as it exists NOW (9 tools — the ROADMAP's "8-tool" text predates Phase 22; the criterion's intent is "the full real tool surface") plus a full session lifecycle against the existing e2e fake relay harness (reuse `mcp_e2e_test.go` infrastructure paths/commands, don't rebuild it).

### Claude's Discretion
- Exact skill prose, workflow step ordering inside each skill, HTML report structure for `cpg-health-report`, agent frontmatter details (tools list), test file naming.

### Deferred Ideas (OUT OF SCOPE)
- Global/user-level skills — hard operator constraint, repo-local only.
- Per-skill dedicated agents — SKL-06's single `cpg-operator` replaces them.
</user_constraints>

## Phase Requirements

<phase_requirements>
| ID | Description | Research Support |
|----|-------------|------------------|
| SKL-01 | `cpg-triage` skill drives a live MCP session end-to-end: start → classify drops (policy vs infra) → present each CNP with its evidence → recommend what to apply | Tool names/order verified from `mcp_tools.go`/`mcp_query.go` (§ MCP Tool Reference below); routes through `cpg-operator` per SKL-06 |
| SKL-02 | `cpg-audit-onboard` skill guides/drives the full onboarding workflow: bootstrap → audit window → `include_audit` capture → enforce checklist | `docs/bootstrap-runbook.md` full 10-step flow read verbatim (§ Existing Workflow Content); CLI-only steps identified (`cpg bootstrap`, `cpg audit-window`) |
| SKL-03 | `cpg-policy-review` skill audits generated CNPs offline (over-broad rules, L7 anchoring, missing DNS-53 companions, dedup sanity) via `cpg explain` + evidence | `cpg explain` flag surface read from `cmd/cpg/explain.go`; README §Explain policies (line 489) and §L7 Prerequisites (line 296) identified as routing targets |
| SKL-04 | `cpg-health-report` skill turns `cluster-health.json` into an HTML report of infra drops by node/workload with Cilium remediation links | `pkg/hubble.ClusterHealthReport`/`HealthDropJSON` schema extracted verbatim from `health_writer.go` (§ cluster-health.json Schema below) |
| SKL-05 | `cpg-mcp-smoke` skill runs a post-release smoke of the tagged binary against the e2e fake relay: 8-tool handshake + full session lifecycle | Exact e2e test/build/run commands identified from `mcp_e2e_test.go` (§ Fake Relay E2E Harness below); tool count corrected to 9 per CONTEXT.md |
| SKL-06 | `cpg-operator` subagent (single repo-local agent driving MCP sessions) is used by `cpg-triage`/`cpg-audit-onboard` instead of per-skill agents | Agent frontmatter format verified from `~/.claude/agents/gsd-executor.md`/`gsd-domain-researcher.md` (§ Agent Frontmatter Format below) |
</phase_requirements>

## Standard Stack

### Core
No new libraries this phase. Everything is markdown authoring plus one Go test file using already-vendored dependencies.

| Library | Version (go.mod) | Purpose | Why Standard |
|---------|-------------------|---------|--------------|
| `github.com/modelcontextprotocol/go-sdk` | v1.6.1 `[VERIFIED: go.mod]` | `mcp.NewClient`/`mcp.NewInMemoryTransports`/`cs.ListTools` — the exact client-side surface the tripwire test uses to enumerate tools | Already the project's one MCP SDK; every existing MCP test uses it |
| `github.com/stretchr/testify` | v1.11.1 `[VERIFIED: go.mod]` | `require`/`assert` in the new test file | Already the project's one assertion library |

### Supporting
None. No HTML templating library is needed for `cpg-health-report`: Claude's Discretion covers "HTML report structure" — a self-contained static HTML string (inline `<style>`, no external JS/CSS CDN dependency) is consistent with cpg's own zero-extra-runtime-dependency posture (`pkg/output`'s YAML writer, `pkg/diff`'s unified diff — no template engine anywhere in the existing Go codebase either).

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Regex-based tool-name extraction from skill markdown | A structured metadata block per skill (e.g. YAML frontmatter `tools: [...]`) | Frontmatter list would be more robust to parse but duplicates the same drift risk the tripwire exists to prevent — a skill author could list a tool in frontmatter without actually routing to it in prose, or vice versa. Regex over rendered prose keeps the check honest: it only passes if the tool name is actually *written* somewhere a human/LLM would read it. |
| Hand-rolled backtick-token regex | A markdown AST parser (e.g. `goldmark`) | Massive overkill for "does this file mention this string" — no new dependency justified; `strings.Contains`/`regexp` is the established convention in this exact test class (`readme_compat_test.go`, `runbook_test.go`, `audit_docs_test.go` all use `strings.Contains`, never an AST parser) |

**Installation:** None — no `go get`/`npm install` needed this phase.

## Package Legitimacy Audit

**N/A — no external packages installed this phase.** The new Go test file (`cmd/cpg/skills_test.go`) imports only stdlib (`os`, `path/filepath`, `regexp`, `strings`, `testing`) plus the two already-vendored, already-in-go.sum dependencies listed above (`go-sdk`, `testify`). No `go.mod` change, no `npm`/`pip`/`cargo` install. The slopcheck/registry-verification protocol is skipped per its own trigger condition ("whenever this phase installs external packages" — it does not).

## Architecture Patterns

### System Architecture Diagram

```
 Operator / LLM harness
        │
        │ invokes via natural-language request
        ▼
 Claude Code skill discovery (.claude/skills/cpg-*/SKILL.md)
        │
        │ SKILL.md names workflow steps + tool names ONLY
        │ (never argument schemas / result shapes — Router Principle)
        ▼
 ┌─────────────────────────────┬───────────────────────────────┐
 │ cpg-triage, cpg-audit-onboard│ cpg-policy-review, cpg-health- │
 │  → delegate session driving  │ report, cpg-mcp-smoke          │
 │    to cpg-operator agent     │  → route directly to CLI/MCP   │
 └──────────────┬───────────────┴───────────────┬───────────────┘
                │                                │
                ▼                                ▼
     .claude/agents/cpg-operator.md      cpg explain / cpg mcp (direct)
                │
                │ drives MCP session lifecycle
                ▼
        `cpg mcp` subprocess (stdio JSON-RPC)
                │
                │ tools/list discovers LIVE semantics
                │ (never a skill's stale copy)
                ▼
   cmd/cpg/mcp.go runMCPServer → registerSessionTools +
   registerQueryTools + registerBootstrapTool  (9 tools,
   Description: strings = single source of truth)
                │
                ▼
        pkg/session, pkg/hubble, pkg/output, pkg/evidence
        (existing Phase 17-23 implementation, unchanged)

 Parallel, offline (CI):
   cmd/cpg/skills_test.go
        │  reads .claude/skills/cpg-*/SKILL.md + cpg-operator.md
        │  enumerates the SAME 9-tool registry via
        │  startInMemoryMCPSession (mcp_harness_test.go)
        ▼
   assert: no phantom names, smoke skill covers all 9, count == 9
```

A reader can trace SKL-01 end to end: operator request → `cpg-triage` SKILL.md → delegates to `cpg-operator` agent → `cpg mcp` subprocess → `tools/list` → session tools → pkg/session/pkg/hubble/pkg/output. The tripwire runs orthogonally in CI, never in the runtime path.

### Recommended Project Structure
```
.claude/
├── skills/
│   ├── cpg-triage/SKILL.md            # SKL-01
│   ├── cpg-audit-onboard/SKILL.md     # SKL-02
│   ├── cpg-policy-review/SKILL.md     # SKL-03
│   ├── cpg-health-report/SKILL.md     # SKL-04 (may bundle scripts/ or assets/ for HTML template)
│   └── cpg-mcp-smoke/SKILL.md         # SKL-05
└── agents/
    └── cpg-operator.md                # SKL-06
cmd/cpg/
└── skills_test.go                     # new tripwire test (alongside readme_compat_test.go et al.)
README.md                              # new "## Agent tooling" section
```

### Pattern 1: SKILL.md Frontmatter (repo-local skill)
**What:** Minimal two-field YAML frontmatter — `name`, `description` — followed by markdown body.
**When to use:** Every one of the 5 skills in this phase.
**Example (verified from this repo's own pre-existing skill + `~/.claude/skills/skill-creator/SKILL.md`):**
```yaml
---
name: cpg-triage
description: >
  Drive a live cpg MCP session end-to-end: start capture, classify dropped
  flows (policy vs infra), present each generated CiliumNetworkPolicy with
  its evidence, and recommend what to apply. Use when an operator wants to
  turn live cluster traffic into reviewable network policy right now. Do
  NOT use for offline review of already-generated policies (see
  cpg-policy-review) or for onboarding a brand-new namespace (see
  cpg-audit-onboard).
---
```
**Discoverability mechanics `[VERIFIED: ~/.claude/skills/skill-creator/SKILL.md]`:** `description` is the ONLY field the harness uses to decide when to invoke the skill — it must state both what the skill does AND when to use it (positive triggers) and, per skill-creator's own guidance, benefits from explicit negative examples ("Do NOT use for...") to disambiguate between this phase's 5 sibling skills, which is exactly the disambiguation problem this phase has (5 skills covering overlapping cpg surface area). `name` must match the directory name (`cpg-triage/` ↔ `name: cpg-triage`) — confirmed by every example on this machine (`template-skill/`↔`template-skill`, `desloppify/`↔`desloppify`).

### Pattern 2: Agent Frontmatter (repo-local subagent)
**What:** `name`, `description`, `tools` (comma-separated allowlist string), optional `color`.
**When to use:** The single `cpg-operator` agent (SKL-06).
**Example (verified from `~/.claude/agents/gsd-executor.md` and `gsd-domain-researcher.md`):**
```yaml
---
name: cpg-operator
description: Drives a live cpg MCP session (start_session/get_status/stop_session
  plus the 5 query tools and get_bootstrap_policy) on behalf of cpg-triage and
  cpg-audit-onboard. Never invoked directly by the operator — spawned by those
  two skills when a live session needs driving.
tools: Bash, Read
---
```
**Tools list (Claude's Discretion, but constrained by the readonly/router principles):** `Bash` is required to spawn `cpg mcp` and drive JSON-RPC over stdio (or, more realistically, to shell out to a small MCP client script/CLI); `Read` for inspecting generated policy YAML/evidence files the session writes to its tmpdir. No `Write`/`Edit` — the agent's job is to drive and report, never to author repo files (SEC-01's readonly discipline extends naturally to the operator's own tool allowlist: nothing here should ever gain a write verb the underlying `cpg mcp` server itself doesn't have).

### Pattern 3: Router-only tool references (anti-drift)
**What:** A skill names a tool (`start_session`, `get_evidence`, etc.) as a step label but never restates its JSON argument shape or its `structuredContent` fields.
**When to use:** Every workflow step in every skill this phase produces.
**Example — CORRECT (router-only):**
```markdown
## Step 2: Start the capture

Call `start_session`. Discover its exact argument surface via `tools/list`
before calling — do not assume argument names from this document.
```
**Anti-pattern — WRONG (restates schema, will drift):**
```markdown
## Step 2: Start the capture

Call `start_session` with `{namespace: [...], all_namespaces: bool, l7: bool,
include_audit: bool, ...}` — see the 10-field schema below.
```
The wrong version is exactly what the CONTEXT.md Router Principle forbids, and it is exactly what would silently drift the next time `startSessionArgs` (`cmd/cpg/mcp_tools.go:21-33`) gains or loses a field.

### Anti-Patterns to Avoid
- **Restating tool argument schemas in skill prose:** locked out by CONTEXT.md's Router Principle; also the literal drift vector SKL success criterion 5 exists to catch.
- **Per-skill dedicated agents:** explicitly deferred (CONTEXT.md Deferred Ideas) — SKL-06 is the ONE agent, referenced by `cpg-triage`/`cpg-audit-onboard`.
- **Global/user-level `~/.claude/skills/cpg-*`:** explicitly out of scope — repo-local only, `.claude/skills/` inside this repo.
- **`cpg-audit-onboard` suggesting daemon-wide `policy-audit-mode`:** the runbook it routes to (`docs/bootstrap-runbook.md`) already has a golden-pin test (`TestRunbookNeverSuggestsDaemonWideAudit`) confining that token to the runbook's leading warning block only — the new skill must not reintroduce a bare mention outside a similarly-scoped warning (see Pitfall 1 below).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| MCP tool discovery inside a skill | A hardcoded table of tool args/results in SKILL.md prose | Live `tools/list` call at invocation time (Router Principle) | Any hardcoded copy drifts the moment `cmd/cpg/mcp_*.go` changes; this is the exact failure mode the tripwire exists to catch |
| Fake-relay e2e harness for `cpg-mcp-smoke` | A new standalone smoke-test script/binary | `mcp_e2e_test.go`'s existing `fakeRelay`/`buildE2EBinary`/`startE2ESubprocess`/`TestMCPE2EGracefulLifecycle` machinery, invoked via `go test -run TestMCPE2E` | CONTEXT.md explicitly locks "reuse `mcp_e2e_test.go` infrastructure paths/commands, don't rebuild it" — a second harness would be a second thing to keep in sync |
| Skill/agent markdown parsing in the tripwire | A YAML frontmatter parser + full markdown AST | `os.ReadFile` + `regexp`/`strings.Contains` over raw file bytes | Matches the established golden-pin convention in this exact package (`readme_compat_test.go`, `runbook_test.go`, `audit_docs_test.go` — none use a markdown library) |

**Key insight:** every "don't hand-roll" here is really the same instruction restated three ways: this phase's only genuinely new logic is the tripwire's phantom/coverage/count assertions — everything else (tool semantics, e2e harness, markdown reading style) is deliberate reuse of what Phases 17-23 and the existing test suite already built.

## MCP Tool Reference (verified from source, 2026-07-22)

The complete, current 9-tool registry — the single source of truth every skill routes to and the tripwire enumerates live rather than hardcoding:

| Tool | Registered in | ReadOnlyHint | One-line purpose (paraphrased from the Go `Description:`) |
|------|---------------|--------------|-------------------------------------------------------------|
| `start_session` | `mcp_tools.go:96` | false | Start a live Hubble capture session in the background; returns `session_id` immediately |
| `get_status` | `mcp_tools.go:147` | true | Coarse session state, elapsed time, on-disk artifact counts |
| `stop_session` | `mcp_tools.go:157` | false (IdempotentHint: true) | Cancel capture, finalize `cluster-health.json`, return final summary |
| `list_dropped_flows` | `mcp_query_flows.go:107` | true | Two-section view: policy-actionable samples + infra/transient/noise aggregates |
| `list_policies` | `mcp_query.go:42` | true | Paginated metadata for every generated CNP: namespace, workload, name, rule counts, YAML path |
| `get_policy` | `mcp_query.go:63` | true | Full CNP YAML + metadata for one namespace/workload pair |
| `get_evidence` | `mcp_query_evidence.go:70` | true | Paginated per-rule flow evidence, shape-identical to `cpg explain --output json` |
| `get_cluster_health` | `mcp_query.go:81` | true | Finalized cluster-health report: per-drop-reason counts + remediation URLs |
| `get_bootstrap_policy` | `mcp_bootstrap.go:41` | true (IdempotentHint: true) | Namespaced default-deny CNP as YAML, version-gated on Cilium ≥ 1.16 |

Composition root: `cmd/cpg/mcp.go:94-97` — `registerSessionTools` (3) + `registerQueryTools` (3 inline + `registerGetEvidenceTool` + `registerListDroppedFlowsTool` = 5) + `registerBootstrapTool` (1) = **9**, matching `TestMCPE2EGracefulLifecycle`'s own `require.Len(..., 9, "3 session + 5 query tools + get_bootstrap_policy")` at `mcp_e2e_test.go:412`. All 9 names begin with one of exactly four verb prefixes: `start_`, `get_`, `stop_`, `list_` — load-bearing for the tripwire's phantom-check regex design (§ Tripwire Design below).

**Naming-convention risk flagged explicitly:** if a future tool violates this 4-prefix convention (e.g. a hypothetical `cnp_diff` or `explain`-style tool with no verb prefix), the phantom-check regex as designed would silently fail to catch a reference to it. This is documented as Open Question 1 below, RESOLVED with a mitigation.

## Fake Relay E2E Harness (SKL-05 routing target)

`cpg-mcp-smoke` must route to the existing infrastructure in `cmd/cpg/mcp_e2e_test.go` — do not rebuild:

- **Test to invoke:** `TestMCPE2EGracefulLifecycle` (`mcp_e2e_test.go:389`) — asserts the full 9-tool handshake (initialize → tools/list with schema+annotation checks → start_session → get_status → all 5 query tools mid-capture via `require.Eventually` → stop_session → get_cluster_health post-stop → graceful stdin-close exit).
- **How it's invoked:** `go test ./cmd/cpg/... -run TestMCPE2EGracefulLifecycle -v` (NOT `-short` — the test explicitly `t.Skip`s under `testing.Short()` at line 391, since it does a one-time `go build -race` of the real binary plus a real subprocess).
- **What it builds/runs:** `buildE2EBinary` (`mcp_e2e_test.go:218`) does `go build -race -o <tmp>/cpg .` once (sync.Once-guarded across the whole test binary run), then `startE2ESubprocess` spawns `<tmp>/cpg mcp` as a real OS subprocess wired to an in-process fake Hubble relay (`startFakeRelay`, `mcp_e2e_test.go:157` — a real gRPC server on `127.0.0.1:0` implementing only `GetFlows`, no cluster/kubeconfig required).
- **Companion test:** `TestMCPE2EUngracefulDisconnect` (`mcp_e2e_test.go:695`) proves the same fan-out on a killed transport — worth mentioning in the smoke skill as the "abrupt disconnect" scenario, though the graceful lifecycle test is the primary smoke target per CONTEXT.md ("full session lifecycle").
- **The "8-tool" ROADMAP text is stale:** CONTEXT.md already resolves this — the smoke skill must assert 9, matching current reality (`get_bootstrap_policy` shipped in Phase 22, after the ROADMAP text was written).

## Consistency Tripwire Design

### Enumerating the live registry in Go
Reuse `startInMemoryMCPSession` from `mcp_harness_test.go:28` verbatim — it is already exported at package scope (same `package main`, no export needed since the test lives in the same package):

```go
// Source: cmd/cpg/mcp_harness_test.go:16-35 (existing helper, reused verbatim)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
clientTransport, drain := startInMemoryMCPSession(ctx)
defer drain()

client := mcp.NewClient(&mcp.Implementation{Name: "skills-tripwire", Version: "0.0.0"}, nil)
cs, err := client.Connect(ctx, clientTransport, nil)
require.NoError(t, err)
defer cs.Close()

toolsResult, err := cs.ListTools(ctx, nil)
require.NoError(t, err)
```

This is the exact same round-trip `TestMCPStdoutPurity` (`mcp_harness_test.go:49`) and `TestMCPE2EGracefulLifecycle` (`mcp_e2e_test.go:410`) already perform — zero new harness code.

### Test sketch (`cmd/cpg/skills_test.go`, new file)
```go
package main

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wantToolCount pins SKL success criterion 5's tool-count tripwire (locked
// by CONTEXT.md). Bump this ONLY alongside a full .claude/skills/ +
// .claude/agents/cpg-operator.md sweep for the new tool.
const wantToolCount = 9

// toolNameToken matches backtick-quoted, verb-prefixed snake_case
// identifiers — every one of the 9 current MCP tool names starts with
// start_/get_/stop_/list_ (see 24-RESEARCH.md "MCP Tool Reference"). Skill
// authors are required (by this test) to always backtick-quote tool names
// in prose, mirroring the convention already visible in README.md's MCP
// tool table.
var toolNameToken = regexp.MustCompile("`((?:start|get|stop|list)_[a-z_]+)`")

func TestSkillsConsistencyTripwire(t *testing.T) {
	registry := liveToolRegistry(t)
	require.Len(t, registry, wantToolCount,
		"MCP tool count changed — sweep .claude/skills/cpg-*/SKILL.md and "+
			".claude/agents/cpg-operator.md before bumping this pin")

	skillPaths, err := filepath.Glob("../../.claude/skills/cpg-*/SKILL.md")
	require.NoError(t, err)
	require.NotEmpty(t, skillPaths, "expected at least one cpg-* skill")
	agentPath := "../../.claude/agents/cpg-operator.md"

	allPaths := append(skillPaths, agentPath)
	coveredBySmoke := map[string]bool{}

	for _, path := range allPaths {
		data, err := osReadFile(t, path)
		tokens := toolNameToken.FindAllStringSubmatch(data, -1)
		for _, m := range tokens {
			name := m[1]
			assert.True(t, registry[name],
				"%s references phantom tool %q — not in the live 9-tool registry", path, name)
			if strings.Contains(path, "cpg-mcp-smoke") {
				coveredBySmoke[name] = true
			}
		}
	}

	for name := range registry {
		assert.True(t, coveredBySmoke[name],
			"cpg-mcp-smoke/SKILL.md must mention every registered tool (coverage floor); missing %q", name)
	}
}

// liveToolRegistry enumerates the exact tool set runMCPServer registers,
// via the same in-memory harness mcp_harness_test.go/mcp_e2e_test.go
// already use — never a hardcoded literal list.
func liveToolRegistry(t *testing.T) map[string]bool {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientTransport, drain := startInMemoryMCPSession(ctx)
	defer drain()

	client := mcp.NewClient(&mcp.Implementation{Name: "skills-tripwire", Version: "0.0.0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer cs.Close()

	toolsResult, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)

	registry := make(map[string]bool, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		registry[tool.Name] = true
	}
	return registry
}
```
(`osReadFile` is a two-line `os.ReadFile` + `require.NoError` wrapper — omitted for brevity; every sibling golden-pin test in this package does the equivalent inline.)

### Repo-root path resolution
Confirmed pattern from `readme_compat_test.go:32` (`os.ReadFile("../../README.md")`) and `runbook_test.go:33` (`os.ReadFile("../../docs/bootstrap-runbook.md")`): Go test binaries run with cwd set to the package's own source directory (`cmd/cpg/`), so `../../` reaches the repo root. The new tripwire test uses the identical `../../.claude/skills/...` / `../../.claude/agents/...` relative paths — no new root-detection logic needed.

### README pin
Add `assert.Contains(t, readme, "## Agent tooling")` plus one `assert.Contains` per skill name (`cpg-triage`, `cpg-audit-onboard`, `cpg-policy-review`, `cpg-health-report`, `cpg-mcp-smoke`) to the same test file — CONTEXT.md requires this section be "pinned by the same test class," i.e. this new file, not a new addition to `readme_compat_test.go` (keeps that file scoped to COMPAT-01/03 per its own doc comment).

## cluster-health.json Schema (SKL-04 routing target)

Verified verbatim from `pkg/hubble/health_writer.go:242-270` `[VERIFIED: source]`:

```go
type ClusterHealthReport struct {
	SchemaVersion     int              `json:"schema_version"`
	ClassifierVersion string           `json:"classifier_version"`
	Session           HealthSession    `json:"session"`
	Drops             []HealthDropJSON `json:"drops"`
}

type HealthSession struct {
	Started        time.Time `json:"started"`
	Ended          time.Time `json:"ended"`
	FlowsSeen      uint64    `json:"flows_seen"`
	InfraDropTotal uint64    `json:"infra_drops_total"`
}

type HealthDropJSON struct {
	Reason      string            `json:"reason"`
	Class       string            `json:"class"`
	Count       uint64            `json:"count"`
	Remediation string            `json:"remediation,omitempty"` // omitted when no Cilium-docs deep link exists
	ByNode      map[string]uint64 `json:"by_node"`
	ByWorkload  map[string]uint64 `json:"by_workload"`
}
```

`cpg-health-report`'s HTML output should render: one table row per `Drops[]` entry (reason, class, count), an expandable/nested breakdown of `ByNode`/`ByWorkload` (note: server-side capped at 100 entries each via `capHealthCountMap` when read through the `get_cluster_health` MCP tool — `mcp_query.go:377-426` — though a file read directly off disk via `cpg`'s own output is uncapped; the skill should state which path it used), and a clickable link wherever `Remediation` is non-empty. `Session.FlowsSeen`/`InfraDropTotal` make a natural report header/summary line. This same JSON is available two ways: (1) directly on disk at `<session_tmpdir>/cluster-health.json` (post-`stop_session`), or (2) via the `get_cluster_health` MCP tool's `structuredContent.report` field — the skill should route to whichever is more natural for its invocation context (an already-running session → tool; a saved/archived session directory → direct file read), and state that choice explicitly rather than silently picking one (Router Principle: this is workflow routing, not schema restatement, so documenting "read the file at `<tmpdir>/cluster-health.json`" is fine — it is not re-describing the JSON shape, just where to find it).

## Existing Workflow Content to Route To (not duplicate)

### `cpg-audit-onboard` → `docs/bootstrap-runbook.md`
Full 10-section flow, read verbatim this session: Prerequisites → Bootstrap the Namespace (`cpg bootstrap -n <ns> | kubectl apply -f -`) → Deploy/Scale Considerations → Enable Per-Endpoint Audit Mode (`cpg audit-window -n <ns> --ttl 30m`, foreground/supervised) → Observe Policy Verdicts (`hubble observe flows -t policy-verdict`) → Capture with `cpg generate --include-audit` → Create and Apply Generated Policies (`kubectl apply -f ./policies/<namespace>/`) → Disable Per-Endpoint Audit Mode (automatic on `cpg audit-window` exit — no separate step) → Verify Enforcement → Clean-up. CONTEXT.md's "enforce checklist ends with the human applying policies (never an apply tool)" maps directly onto the runbook's own "Create and Apply Generated Policies" section, which is explicitly a `kubectl apply` step, never an MCP tool call — no tool exists for this and none should be invented.

### `cpg-triage` → live MCP session tools, in this order
`start_session` → poll `get_status` → `list_dropped_flows` (classify: samples[] = policy-actionable, aggregates[] = infra/transient/noise, per the tool's own Description field, `mcp_query_flows.go:107-125`) → for each policy-actionable workload, `list_policies` then `get_policy` (full CNP YAML) paired with `get_evidence` (per-rule flow evidence, "byte-identical in shape to `cpg explain --output json`") → `stop_session` → `get_cluster_health` (post-stop, for the infra/transient side the policy tools never cover). This is the literal tool-call sequence `TestMCPE2EGracefulLifecycle` already exercises end to end (`mcp_e2e_test.go:389-675`), giving the skill a proven, tested reference sequence to route through.

### `cpg-policy-review` → `cpg explain` + evidence, offline
CLI surface verified from `cmd/cpg/explain.go:18-49`: `cpg explain <NAMESPACE/WORKLOAD | path/to/policy.yaml>` with filters `--ingress`/`--egress`/`--port`/`--peer`/`--peer-cidr`/`--http-method`/`--http-path`/`--dns-pattern`/`--since`/`--samples-limit`, output via `--json`/`--format text|json|yaml`. README §"Explain policies" (line 489) and §"L7 Prerequisites" (line 296, anchor `#l7-prerequisites`) are the two sections to route the skill's "over-broad rules / L7 anchoring / missing DNS-53 companions / dedup sanity" checklist items to — this is prose the skill should point at (`README.md#l7-prerequisites`), not re-explain.

### `cpg-triage`/`cpg-audit-onboard` → `cpg-operator` agent
Both skills delegate live-session driving to the one `cpg-operator` agent per SKL-06 (never invoke `cpg mcp` directly themselves) — this is the phase's one explicit indirection layer, and it is the reason `cpg-operator`'s `description` must clearly state "spawned by cpg-triage and cpg-audit-onboard" (Pattern 2 example above) so Claude Code's own agent-selection logic never invokes it standalone in a context those two skills didn't set up.

## Common Pitfalls

### Pitfall 1: Skill prose accidentally tripping the existing `policy-audit-mode` runbook pin
**What goes wrong:** `TestRunbookNeverSuggestsDaemonWideAudit` (`runbook_test.go:32`) and `TestRunbookAuditWindowStep` (`audit_docs_test.go:56`) both read `docs/bootstrap-runbook.md` directly by path — they do NOT grep the repo. Similarly `TestReadmeCompatSection`/`TestReadmeAuditWindowSection` read `README.md` directly by path.
**Why it happens:** A cursory read of the pitfall description in this task's prompt ("if any test greps repo-wide") suggests a risk that doesn't actually exist here.
**How to avoid:** `grep -rn "policy-audit-mode" --include="*.go" --include="*.md" .` was run this session — the ONLY occurrences outside `.planning/` planning artifacts are inside `docs/bootstrap-runbook.md`'s own leading warning block (which the golden pin already scopes correctly). No existing test greps `.claude/skills/*` or any new file this phase creates; nothing in this phase's output is at risk of tripping an existing pin.
**Warning signs:** N/A — verified absent. `RESOLVED` (see Open Question 2 below).
**Caution for skill authors regardless:** `cpg-audit-onboard`'s SKILL.md should still avoid casually mentioning the bare daemon-wide `policy-audit-mode` string outside a clear warning, as a matter of the same operational-safety discipline the runbook itself enforces — but this is a style recommendation, not a test-failure risk.

### Pitfall 2: `get_cluster_health`'s mid-capture non-error marker being mistaken for a failure
**What goes wrong:** `get_cluster_health` returns a non-error `available_after_stop: true` marker while a session is still capturing (`mcp_query.go:449-457`) — not an error, not empty data.
**Why it happens:** An LLM skill author unfamiliar with the tool's exact semantics might write triage/health-report prose that treats any "no report yet" response as a fault to surface to the operator.
**How to avoid:** `cpg-triage` and `cpg-health-report` should explicitly note the workflow order requirement: `stop_session` before `get_cluster_health` for a full report — mirroring the note already in `get_cluster_health`'s own Description field, without restating the field's JSON shape (that stays router-only per the Router Principle — "call stop_session first before expecting a report" is workflow guidance, not schema restatement).
**Warning signs:** A skill or agent that calls `get_cluster_health` before `stop_session` and treats the response as an error.

### Pitfall 3: `cpg-mcp-smoke` skipping the e2e test in short mode
**What goes wrong:** `TestMCPE2EGracefulLifecycle`/`TestMCPE2EUngracefulDisconnect` both `t.Skip` under `testing.Short()` (`mcp_e2e_test.go:390-392`, `696-698`).
**Why it happens:** A skill author running `go test ./cmd/cpg/... -short` (a common fast-feedback default) will get a silent skip, not a real smoke result.
**How to avoid:** `cpg-mcp-smoke`'s SKILL.md must explicitly instruct running WITHOUT `-short` — e.g. `go test ./cmd/cpg/... -run TestMCPE2E -v` — and should mention the one-time `-race` build cost so an operator isn't surprised by the longer runtime.
**Warning signs:** A "smoke passed" result that took under a second — the real e2e build+subprocess round trip takes several seconds minimum.

### Pitfall 4: Tripwire regex missing a tool name that doesn't follow the 4-prefix convention
**What goes wrong:** The phantom/coverage regex (`(?:start|get|stop|list)_[a-z_]+`) only matches tool names beginning with one of those four verbs. A hypothetical future tool with a different naming style (e.g. `explain`, `cnp_diff`) would silently bypass both the phantom check and the coverage floor.
**Why it happens:** The regex is deliberately narrow to avoid false-positive matches against unrelated snake_case prose tokens (arg names like `all_namespaces`, `flush_interval` — none of which start with those 4 verbs, so they're safely excluded too).
**How to avoid:** RESOLVED as an accepted, documented tradeoff (see Open Question 1) — all 9 current tools comply, and the count-pin assertion (`wantToolCount = 9`) independently forces a human to look at `mcp.go`'s composition root the moment a 10th tool is added, at which point the regex's prefix list should be revisited if the new tool breaks the convention.
**Warning signs:** `wantToolCount` assertion fails on a `go test` run after a tool-registration change — that failure IS the intended tripwire firing; the fix is to update the skills first, then bump the constant, per the comment in the test sketch above.

## Code Examples

### Verified SKILL.md frontmatter (this repo's own existing skill)
```yaml
# Source: .claude/skills/desloppify/SKILL.md:1-8 (pre-existing in this repo)
---
name: desloppify
description: >
  Multi-language codebase health scanner. Use when the user explicitly asks
  to run desloppify, scan for technical debt, get a health score, or create
  a cleanup plan. Do NOT trigger for general code review, renaming, or
  fixing individual bugs.
---
```

### Verified agent frontmatter (global example on this machine)
```yaml
# Source: ~/.claude/agents/gsd-executor.md:1-8
---
name: gsd-executor
description: Executes GSD plans with atomic commits, deviation handling, checkpoint protocols, and state management. Spawned by execute-phase orchestrator or execute-plan command.
tools: Read, Write, Edit, Bash, Grep, Glob, mcp__context7__*
color: yellow
---
```

### README MCP tool table style to extend for "Agent tooling" section
```markdown
# Source: README.md:561-570 (existing style to mirror for the new section)
| Tool | Description |
|------|-------------|
| `start_session` | Start a live Hubble capture session in the background; ... |
```
The new "## Agent tooling" section (placed after "## MCP Server (cpg mcp)", before "## Label selection" — README.md line 608 — is a natural insertion point since it's the next `##` boundary after the MCP section) should use an equivalent `| Skill | Purpose |` table for the 5 skills plus one line for `cpg-operator`, keeping the same terse, no-schema-restatement style already established by the MCP tool table itself.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| ROADMAP text describing an "8-tool" MCP handshake | 9-tool registry (adds `get_bootstrap_policy`) | Phase 22 (this repo, prior to this research) | `cpg-mcp-smoke` must assert 9, not 8 — already resolved by CONTEXT.md, reconfirmed here from source |
| Manual `kubectl exec` + `cilium-dbg endpoint config` for per-endpoint audit mode | `cpg audit-window -n <ns> --ttl <dur>` (Phase 23) | Phase 23 (this repo) | `docs/bootstrap-runbook.md`'s "Enable Per-Endpoint Audit Mode" section already reflects the new command; `cpg-audit-onboard` routes to the current runbook text, not the old manual steps |

**Deprecated/outdated:** None specific to this phase's own domain (Claude Code skill/agent frontmatter has been stable across the examples checked on this machine — `template-skill`, `skill-creator`, and every `gsd-*` agent use the identical two/three-field frontmatter shape).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Claude Code's skill-discovery mechanism reads ONLY `name`/`description` from frontmatter to decide invocation (no other field affects triggering) | Pattern 1 | If a hidden trigger mechanism exists (e.g. keyword lists, embeddings over the body), the 5 skills might under/over-trigger relative to expectations — but this is a Claude Code platform behavior, not something the phase's own tests can control either way, so the risk is contained to UX quality, not correctness |
| A2 | `Bash, Read` is a sufficient/appropriate `tools:` allowlist for `cpg-operator` | Pattern 2 | If the agent needs to parse MCP JSON-RPC responses in a more structured way than shell text processing allows, a `Write` (for a temp script) or additional tool may be needed — flagged as Claude's Discretion in CONTEXT.md, so this is explicitly not locked and the planner/executor may adjust |
| A3 | The `## Agent tooling` README section belongs after `## MCP Server (cpg mcp)` (before `## Label selection`) | Code Examples | Low risk — purely a documentation placement choice with no functional impact; the tripwire's README assertions (§ Consistency Tripwire Design) only check `strings.Contains`, not section position |

**All three assumptions are low-risk / UX-only** — none affect the phase's correctness-bearing deliverables (the tripwire test, the tool registry, the runbook/explain routing targets), which are all `[VERIFIED: source]`.

## Open Questions

1. **RESOLVED: Does the tripwire's verb-prefix regex (`start_`/`get_`/`stop_`/`list_`) risk missing a future non-conforming tool name?**
   - What we know: all 9 current tools comply with this convention (verified by direct enumeration above).
   - What's unclear: whether every future tool will continue to comply.
   - Resolution: Accept the constraint explicitly, documented in Pitfall 4. The independent `wantToolCount` pin (constant `9`) provides a structural backstop — any tool-count change, compliant naming or not, fails the tripwire and forces a human to look at the composition root, at which point the regex can be widened if needed. This is a one-line follow-up, not a design gap.

2. **RESOLVED: Does any existing test grep the repo broadly enough that new skill files referencing `policy-audit-mode` (or other pinned tokens) could accidentally fail?**
   - What we know: `TestRunbookNeverSuggestsDaemonWideAudit`, `TestReadmeCompatSection`, `TestReadmeAuditWindowSection`, `TestRunbookAuditWindowStep` all read one specific file each (`README.md` or `docs/bootstrap-runbook.md`) by explicit relative path — none glob or grep `.claude/`.
   - What's unclear: nothing remaining — verified directly via `grep -rn "policy-audit-mode" --include="*.go" --include="*.md" .` this session; all non-`.planning/` hits are inside the runbook's own already-correctly-scoped warning block.
   - Resolution: No risk. New skill files are free to mention `cpg audit-window`/`PolicyAuditMode` (the per-endpoint form, already the runbook's own preferred terminology) without any collision.

3. **RESOLVED: What frontmatter fields does a repo-local SKILL.md actually need?**
   - What we know: `name`, `description` — confirmed identical across `~/.claude/skills/template-skill/SKILL.md`, `~/.claude/skills/skill-creator/SKILL.md`, and this repo's own `.claude/skills/desloppify/SKILL.md`. No `tools:` field on any skill example — that field only appears on agent frontmatter.
   - What's unclear: nothing remaining.
   - Resolution: Use the two-field format shown in Pattern 1 for all 5 skills.

4. **RESOLVED: What frontmatter fields does `.claude/agents/cpg-operator.md` need?**
   - What we know: `name`, `description`, `tools` (comma-separated) — confirmed from `~/.claude/agents/gsd-executor.md` and `~/.claude/agents/gsd-domain-researcher.md`; `color` is optional/cosmetic (both examples set it, but it has no functional bearing on invocation).
   - What's unclear: nothing remaining.
   - Resolution: Use the three-field format shown in Pattern 2 (`color` optional, Claude's Discretion per CONTEXT.md).

5. **RESOLVED: Does `cpg-health-report` need a new HTML templating dependency?**
   - What we know: the existing Go codebase has zero templating dependencies anywhere (`pkg/output`, `pkg/diff` both hand-build their output formats); the skill itself is markdown/prose, not Go code, so there's no Go dependency question at all — any HTML generation this skill performs happens as part of the skill's own instructed workflow (e.g., instructing the LLM to write a self-contained HTML file), not via a new library import.
   - What's unclear: nothing remaining — this was conflated with "does the phase need a new dependency," which it does not, since no Go code is being added to produce HTML.
   - Resolution: No new dependency. Self-contained inline-styled HTML, generated by the skill's own instructed workflow at invocation time.

**No unresolved open questions remain.**

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | `cmd/cpg/skills_test.go` (new tripwire test), existing `go test` suite | ✓ `[VERIFIED: go.mod]` | go 1.25.1 (toolchain go1.25.12) | — |
| `github.com/modelcontextprotocol/go-sdk` | In-memory tool-registry enumeration | ✓ `[VERIFIED: go.mod]` | v1.6.1, already vendored | — |
| `github.com/stretchr/testify` | Test assertions | ✓ `[VERIFIED: go.mod]` | v1.11.1, already vendored | — |
| Claude Code skill/agent runtime | Discovering/invoking the 5 skills + `cpg-operator` | ✓ (this session ran inside it) | — | — |

**Missing dependencies with no fallback:** None.
**Missing dependencies with fallback:** None — this phase introduces no new runtime dependency of any kind.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `stretchr/testify` (already the project's one test stack; no new framework) |
| Config file | none — plain `go test`, no config file in this repo (`go.mod` toolchain pin only) |
| Quick run command | `go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` |
| Full suite command | `go test ./cmd/cpg/... -count=1 -race` (per this repo's `make test`; e2e tests additionally require dropping `-short`, which `make test` already does not pass) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SKL-01 | `cpg-triage` routes through the correct live-session tool sequence, referencing only real tool names | unit (mechanical: phantom-check only) | `go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` | ❌ Wave 0 |
| SKL-01 | `cpg-triage`'s actual workflow correctness (does it classify/present/recommend sensibly) | manual-only | N/A — LLM-authored prose quality is not unit-testable; verify by human read-through | N/A |
| SKL-02 | `cpg-audit-onboard` references only real tool names + guides (never runs) `cpg bootstrap`/`cpg audit-window` | unit (mechanical: phantom-check only) | `go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` | ❌ Wave 0 |
| SKL-02 | `cpg-audit-onboard` never suggests an apply tool for the enforce checklist | manual-only | N/A — a "never mentions an apply tool" claim over free-form prose is not reliably regex-checkable without excessive false positives; verify by human read-through | N/A |
| SKL-03 | `cpg-policy-review` references only real `cpg explain` flags / MCP tool names | unit (mechanical: phantom-check only, tool-name half) | `go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` | ❌ Wave 0 |
| SKL-04 | `cpg-health-report` produces HTML reflecting the real `ClusterHealthReport` field set | manual-only | N/A — HTML generation happens at LLM-invocation time, not build time; verify by running the skill once against a real/fixture `cluster-health.json` and inspecting output | N/A |
| SKL-05 | `cpg-mcp-smoke` routes to `TestMCPE2EGracefulLifecycle` and asserts the current 9-tool count | unit (mechanical: phantom-check + count pin) + integration (the routed-to test itself) | `go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` (routing correctness) and `go test ./cmd/cpg/... -run TestMCPE2EGracefulLifecycle -v` (the actual smoke) | ❌ Wave 0 (tripwire) / ✓ (e2e test, pre-existing) |
| SKL-06 | `cpg-operator` referenced by `cpg-triage`/`cpg-audit-onboard`, references only real tool names | unit (mechanical: phantom-check + coverage-floor half) | `go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` | ❌ Wave 0 |
| (all) | No phantom tool names anywhere in `.claude/skills/cpg-*/SKILL.md` or `.claude/agents/cpg-operator.md` | unit | `go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` | ❌ Wave 0 |
| (all) | `cpg-mcp-smoke` mentions every registered tool (coverage floor) | unit | `go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` | ❌ Wave 0 |
| (all) | Tool count pinned at 9 | unit | `go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` | ❌ Wave 0 |
| (all) | README `## Agent tooling` section exists and lists all 5 skills | unit | `go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./cmd/cpg/... -run TestSkillsConsistencyTripwire -v` (sub-second — no `-race` build, in-memory transport only)
- **Per wave merge:** `go test ./cmd/cpg/... -count=1 -race` (full package, includes the e2e tests this phase's smoke skill routes to — several seconds due to the one-time `-race` binary build)
- **Phase gate:** Full suite green before `/gsd-verify-work`, PLUS one manual read-through pass per skill (SKL-01/02/04 above) since workflow-prose quality is not mechanically testable — this is expected and inherent to a documentation/routing phase, not a gap in the test design.

### Wave 0 Gaps
- [ ] `cmd/cpg/skills_test.go` — new file, covers the mechanical half of SKL-01 through SKL-06 (phantom-check, coverage-floor, count-pin, README section)
- [ ] `.claude/skills/cpg-triage/SKILL.md`, `cpg-audit-onboard/SKILL.md`, `cpg-policy-review/SKILL.md`, `cpg-health-report/SKILL.md`, `cpg-mcp-smoke/SKILL.md`, `.claude/agents/cpg-operator.md` — none exist yet; the tripwire test cannot pass until all six exist with correctly backtick-quoted tool-name references
- [ ] README.md `## Agent tooling` section — does not exist yet
- Framework install: none — `go test` and `testify` are already fully set up repo-wide

## Security Domain

`security_enforcement` is absent from `.planning/config.json` (treated as enabled per protocol), but this phase's tech stack has essentially no ASVS-relevant surface: no new authentication, session, network, or cryptographic code. The deliverables are (1) static markdown consumed by the Claude Code runtime, not by cpg's own Go binary, and (2) one Go test that only reads local files and talks to an in-memory MCP transport (no network, no untrusted input).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | No auth surface introduced |
| V3 Session Management | No | The MCP "session" concept here is `pkg/session.Manager`, unchanged by this phase — no new session code |
| V4 Access Control | No | No new RBAC/permission surface; `cpg-operator`'s `tools:` allowlist (`Bash, Read`, no `Write`/`Edit`) is the closest analog — see below |
| V5 Input Validation | Marginal | The tripwire test's own regex parses local, developer-authored markdown files (not untrusted external input) — no injection surface. If `cpg-health-report`'s generated HTML ever embeds untrusted `cluster-health.json` field values (drop reasons, remediation URLs — both are cpg-generated, closed-vocabulary strings, not free-form user input) directly into HTML without escaping, that would be a latent XSS-shaped risk in a *rendered file an operator opens locally* — low severity, but worth a one-line escaping note in the skill's own instructions |
| V6 Cryptography | No | No cryptographic code |

### Known Threat Patterns for this domain

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Skill/agent prose drifting from the real MCP tool surface (stale tool names, phantom tools, uncovered tools) — an LLM-facing "prompt injection via stale documentation" shaped risk, where an agent could be misled into calling a nonexistent tool or missing a real one | Tampering (of the LLM's own operating context) | The Go consistency tripwire (`cmd/cpg/skills_test.go`) — this IS the mitigation this phase exists to build, not an external control |
| `cpg-operator` agent's `tools:` allowlist being broader than necessary (scope creep toward `Write`/`Edit`) | Elevation of Privilege | Keep the allowlist minimal (`Bash, Read` per Pattern 2) — no write verb the underlying `cpg mcp` server itself doesn't already have (SEC-01's readonly discipline extended by convention, not by new enforcement code) |
| `cpg-health-report`'s generated HTML echoing `cluster-health.json` string fields (`Reason`, `Remediation`) without escaping, into a file an operator later opens in a browser | Tampering (self-XSS in a locally-generated, locally-opened file — low severity since the data source is cpg's own closed-vocabulary drop-reason taxonomy, not attacker-controlled input) | Skill instructions should note "HTML-escape any cluster-health.json string field before embedding" as a one-line workflow guardrail; no new Go code needed since this happens at LLM-generation time, not in `pkg/hubble` |

## Sources

### Primary (HIGH confidence)
- `cmd/cpg/mcp.go`, `mcp_tools.go`, `mcp_query.go`, `mcp_query_evidence.go`, `mcp_query_flows.go`, `mcp_bootstrap.go` — direct source read, tool registry composition root and all 9 `Description:` strings `[VERIFIED: source]`
- `cmd/cpg/mcp_harness_test.go`, `mcp_e2e_test.go`, `mcp_query_tools_test.go` — direct source read, in-memory + e2e test harness patterns `[VERIFIED: source]`
- `cmd/cpg/readme_compat_test.go`, `runbook_test.go`, `audit_docs_test.go` — direct source read, golden-pin test conventions `[VERIFIED: source]`
- `docs/bootstrap-runbook.md` — direct source read, full onboarding workflow `[VERIFIED: source]`
- `pkg/hubble/health_writer.go` — direct source read, `ClusterHealthReport` schema `[VERIFIED: source]`
- `cmd/cpg/explain.go` — direct source read, `cpg explain` flag surface `[VERIFIED: source]`
- `README.md` — direct source read, existing MCP tool table, section structure, `[VERIFIED: source]`
- `go.mod` — direct read, `go-sdk`/`testify` pinned versions `[VERIFIED: go.mod]`
- `~/.claude/skills/template-skill/SKILL.md`, `~/.claude/skills/skill-creator/SKILL.md` — direct file read on this machine, SKILL.md frontmatter format `[VERIFIED: local filesystem]`
- `~/.claude/agents/gsd-executor.md`, `~/.claude/agents/gsd-domain-researcher.md` — direct file read on this machine, agent frontmatter format `[VERIFIED: local filesystem]`
- `.claude/skills/desloppify/SKILL.md` (this repo) — direct file read, confirms repo-local skill layout already works in this environment `[VERIFIED: source]`
- `.planning/phases/24-cpg-dedicated-skills-agent-tooling/24-CONTEXT.md`, `.planning/REQUIREMENTS.md` — direct source read, locked decisions and SKL-01..06 requirement text `[VERIFIED: source]`
- `grep -rn "policy-audit-mode" --include="*.go" --include="*.md" .` — direct command run this session, confirms scope of existing golden pin `[VERIFIED: command output]`

### Secondary (MEDIUM confidence)
None used — every claim in this document traces to a direct source/file read or command run in this session.

### Tertiary (LOW confidence)
None.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new dependencies; existing versions read directly from `go.mod`
- Architecture: HIGH — every pattern verified against real source in this repo or real frontmatter examples on this machine
- Pitfalls: HIGH — all four verified against direct source reads or a direct repo-wide grep run this session

**Research date:** 2026-07-22
**Valid until:** 30 days (stable domain — the MCP tool registry only changes with a new phase, and Claude Code's skill/agent frontmatter format has been stable across every example checked)
