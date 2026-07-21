# Phase 19: Security Hardening & End-to-End Validation - Research

**Researched:** 2026-07-21
**Domain:** Static reachability analysis (Go SSA/callgraph) for a structural readonly audit; real-subprocess MCP stdio integration testing
**Confidence:** HIGH — the two hardest questions (SEC-01 audit feasibility, SRV-04 e2e mechanics) were validated by **running real, throwaway probe code against this exact repository** (built with the actual toolchain, executing the actual `cpg` binary), not just by reading source. All probe files were deleted before this document was written; `git status` is clean except a pre-existing, unrelated `.planning/config.json` toggle from the GSD tooling itself.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**SEC-01 — structural readonly audit mechanism**
- **D-01:** The audit is a **static reachability test**: build the SSA callgraph (`golang.org/x/tools` — test-only dependency) rooted at `runMCPServer` (which transitively covers `registerSessionTools` + `registerQueryTools` and every handler), walk every reachable function, and assert two properties. Function-level reachability is required — `cmd/cpg` is one `package main` that also contains the CLI (`generate.go` writes to user-chosen output dirs), so a package/import-level scan cannot distinguish MCP-reachable code from CLI-only code without a mushy allowlist that would let a future MCP tool call `runGenerate` unnoticed.
- **D-02:** Property 1 — **no K8s write verb reachable**: no reachable function calls a client-go/dynamic-client write method (`Create`/`Update`/`Patch`/`Delete`/`Apply` selectors on K8s client types). Baseline verified during scouting: zero such verbs exist anywhere in the repo today (`pkg/k8s` is read + port-forward only) — the audit pins that fact against regression.
- **D-03:** Property 2 — **no filesystem write outside the session tmpdir**: every reachable call to a write-capable fs function (`os.WriteFile`, `os.Create`, `os.OpenFile` with write flags, `os.Mkdir*`, `os.MkdirTemp`, `os.Rename`, `os.Remove*`) must be in an **explicit allowlist of function symbols** — the atomic writers (`pkg/output`, `pkg/evidence`, `pkg/hubble` health writer) and `pkg/session` Manager tmpdir lifecycle ops — each documented in the test with WHY it is safe (paths rooted in `os.MkdirTemp`/`DeriveSessionPaths`). Any new unallowlisted write fails the test.
- **D-04:** Re-runnability contract: the callgraph is computed at test time from the composition root — a future `registerXTools` call or new tool handler is automatically swept in with zero test edits. The failure message names the offending function AND its call path from the root, so a future author immediately sees what leaked. Callgraph algorithm choice (RTA recommended; CHA acceptable fallback if RTA fights `package main`) = planner/researcher, but soundness direction must be over-approximation (never miss reachable code).

**SRV-04 — e2e harness shape**
- **D-05:** Real subprocess, real stdio: the test builds the actual `cpg` binary **with `-race`** (once, e.g. TestMain or shared helper, into a test temp dir) and drives `cpg mcp` over its real stdin/stdout pipes. This — not the in-memory harness — is where 16-CONTEXT D-06's deferred assertion lands: every stdout byte parses as a JSON-RPC frame.
- **D-06:** No cluster, no kubeconfig: the test starts an **in-process fake Hubble relay** — a `grpc.Server` implementing `observerpb.ObserverServer` on `127.0.0.1:0` — and passes its address via `start_session{server: addr, tls: false}` (the D-07 bypass built for exactly this). The fake serves `GetFlows` with fixture dropped flows: at least one policy-actionable drop (POLICY_DENIED with workload labels → real policy + evidence files appear in the tmpdir) and one infra-class drop (→ non-empty cluster-health aggregates), then holds the stream open until context cancel so the session stays `capturing` until stopped. It must also implement whatever connectivity-check RPC `pkg/hubble.Client` performs at connect (client.go does an explicit check because `grpc.NewClient` dials lazily — researcher confirms which RPC).
- **D-07:** Graceful lifecycle assertions, in order: `initialize` handshake → `tools/list` (D-10) → `start_session` → `get_status` (state=capturing, `tmp_dir` captured for later) → each of the 5 query tools mid-capture (samples live; aggregates/health return the `available_after_stop` marker) → `stop_session` (final summary, counters > 0) → `get_cluster_health` post-stop (full report, remediation URLs) → close stdin → process exits 0. The whole test file runs under the suite's `-race` like the other 607 tests.
- **D-08:** Ungraceful-disconnect variant: same setup through `get_status` (tmpdir known), then **abruptly close stdin with no stop_session**. Assert within a bounded deadline (test-side cap ~10s, comfortably above the SESS-05 per-step deadlines): the process exits on its own, the session tmpdir is removed, and the fake relay's `GetFlows` stream context gets cancelled (proving the session-cleanup fan-out ran).
- **D-09:** Honest mapping of the roadmap's "port-forward … cleaned up" wording: the e2e deliberately bypasses port-forward (no cluster — that IS the D-07 design). The fan-out step that closes the port-forward is the same `Shutdown()` path whose per-step bounded cleanup is already unit-tested in `pkg/session` (SESS-05, Phase 17); the e2e proves the fan-out fires on transport death (stream cancel + tmpdir removal + bounded exit). State this mapping explicitly in the e2e test comment and in VERIFICATION so the verifier does not flag a phantom gap.

**SRV-01 — handshake/schema assertion depth**
- **D-10:** Structural invariants, **no golden-file schema snapshots** (brittle against SDK serialization details, zero added safety). Assert: `initialize` succeeds with serverInfo name `cpg` + version; `tools/list` returns **exactly** the 8-name set {`start_session`, `get_status`, `stop_session`, `list_dropped_flows`, `list_policies`, `get_policy`, `get_evidence`, `get_cluster_health`}; every tool has a non-empty description and an inputSchema; every data-returning tool has an outputSchema; annotations match what the registration code declares (query tools `readOnlyHint: true` per 18 D-16 — read the actual registrations at plan time for the session tools' truth, don't guess); the dropclass enum (`policy|infra|transient|noise|unknown`) appears in the schemas that carry it (`list_dropped_flows`, `get_evidence`).
- **D-11:** SRV-01 is satisfied inside the e2e stdio test (the handshake over real stdio IS the requirement) — no separate harness registration doc-test. The existing in-memory all-8-tools test (Phase 18) stays as the fast-feedback layer; the e2e adds the real-transport proof.

**SEC-03 — README MCP section**
- **D-12:** One new top-level section `## MCP Server (cpg mcp)` in README.md (README currently has zero MCP mentions — written from scratch), placed after the existing command documentation, in the README's existing voice (English, practical, example-first).
- **D-13:** Mandatory content blocks, in order: (1) what it is — readonly MCP server over stdio, single capture session, LLM reads dropped flows/policies/evidence/health; (2) the 8-tool table with one-line descriptions; (3) **harness configuration** — a concrete `mcpServers` JSON example with an explicit `env` block (`KUBECONFIG`, `PATH`, `TMPDIR`) and the WHY: MCP hosts spawn servers without your shell env — client-go needs `KUBECONFIG`, exec credential plugins need `PATH`, the session tmpdir honors `TMPDIR`; (4) **secrets posture** — with `--l7`-enabled sessions, HTTP paths/methods, FQDNs, and workload labels reach the LLM context via tool results; `Authorization`/`Cookie`/headers are NEVER captured (v1.2 anti-feature, structural); no redaction pass in v1.5 (REDACT-01 is v2) — operators on sensitive clusters should know what crosses the boundary; (5) **exec-credential-plugin caveat** — kubeconfigs using `exec` auth (aws eks get-token, gke-gcloud-auth-plugin, azure kubelogin) run non-interactively under an MCP host: an interactive login prompt hangs or fails `start_session`; verify headless auth works (`kubectl get pods` from a non-interactive shell) or use a static kubeconfig; the bounded `start_session` timeout turns this into an actionable error, not a silent hang; (6) session model note — one session at a time, stopped session retained and queryable until the next `start_session` or server exit.
- **D-14:** Scope guard: README section documents what EXISTS. No aspirational features, no HTTP transport, no multi-session. Cross-link the two-step L7 workflow docs where the secrets posture mentions `--l7`.

### Claude's Discretion
- Exact e2e client plumbing: go-sdk client over a command/pipe transport vs hand-rolled JSON-RPC over `exec.Cmd` pipes — whatever the SDK v1.6.1 actually offers (researcher confirms `CommandTransport` availability); the ungraceful variant likely needs hand-managed pipes regardless (must close stdin while watching the process).
- Audit test file/name layout in `cmd/cpg` (e.g. `mcp_audit_test.go`, `mcp_e2e_test.go`); `testing.Short()` guard on the e2e is planner's call.
- Fake relay fixture flow shapes (mirror `pkg/session/manager_test.go`'s fixtures — they're package-private, so `cmd/cpg` builds its own small ones).
- Exact bounded-deadline constants in the ungraceful test (must exceed SESS-05 step deadlines with margin).
- README prose and table formatting within D-13's block list.

### Deferred Ideas (OUT OF SCOPE)
None new — REDACT-01 (secrets redaction), LIVE-01 (live counters), FLOW-01 (flow-sample writer) remain v2; lint debt (LINT-01..03) and release hardening (RELSEC-01..02) remain tracked outside this phase's requirements.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SRV-01 | SRE can register cpg in an MCP harness (`cpg mcp`, stdio) and initialize handshake succeeds with all tools listed | Empirically proven end-to-end against the real compiled `-race` binary (see "Validated e2e findings"): `initialize` returns `ProtocolVersion:2025-11-25`, `ServerInfo` populated, `tools/list` returns exactly 8 tools. Exact session-tool annotation ground truth extracted from `cmd/cpg/mcp_tools.go` for D-10's assertion depth (see Code Examples). |
| SRV-04 | End-to-end stdio integration test drives full lifecycle under `-race`, plus ungraceful-disconnect variant | Fully validated by executing two throwaway probe tests against the real `-race`-built binary + a real in-process fake gRPC relay: graceful lifecycle (init→start→status→list_policies→stop→clean exit 0, byte-pure stdout) and ungraceful disconnect (abrupt stdin close→bounded self-exit→tmpdir removed→relay stream cancelled) both passed. See "Architecture Patterns" and "Code Examples" for the exact reusable shape, and "Common Pitfalls" for the one real race discovered (relay-not-yet-reached). |
| SEC-01 | Readonly guarantee is structural, verified by a re-runnable audit test | Fully validated: naive whole-program CHA/RTA reachability from `runMCPServer` is **empirically infeasible** (70–102 spurious callers via third-party interface-dispatch noise). A restricted design — RTA-computed reachability filtered to cpg-owned functions, then a **direct SSA call-instruction scan** of only those functions (no further third-party recursion) — was built and run against this exact codebase and produced **exactly 5 caller functions**, matching D-03's prediction precisely, with 0 k8s-write-verb hits. See "Architecture Patterns" Pattern 1 and "Code Examples" for the validated design. |
| SEC-03 | README MCP section documents harness env, secrets posture, exec-credential caveat | README's exact heading structure read in full; a specific, evidence-based insertion point identified (after `## Explain policies`, before `## Label selection`) with rationale. See "Architecture Patterns" → "Recommended Project Structure" analog (README section placement) and Code Examples for the `env` block content already implied by prior-phase code (`KUBECONFIG`/`PATH`/`TMPDIR`). |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

No project-level `./CLAUDE.md` exists in this repository (confirmed: `ls CLAUDE.md` → not found). No project-specific skills directory with applicable rules was found either (`.claude/skills/` contains only an unrelated `desloppify/` tool directory; no `SKILL.md` files were present to load). The user's **global** CLAUDE.md cost-management and workflow rules apply to the *orchestrating* session, not to phase content, and are out of scope for this document.

## Summary

This phase closes v1.5 with zero new product features: a structural readonly audit test (SEC-01), a real-subprocess stdio e2e test covering the full session lifecycle under `-race` plus an ungraceful-disconnect variant (SRV-04, folding in SRV-01's handshake proof), and a new README section (SEC-03). Both of the phase's hard technical risks were **not just researched but empirically executed** against this exact repository in this session, using throwaway test files that were written, run, and deleted (repo confirmed clean afterward).

**The SEC-01 finding is the most consequential discovery of this research.** A naive interpretation of "build the SSA callgraph rooted at `runMCPServer`, walk every reachable function, assert no write verb" — using either CHA or RTA's ordinary whole-program reachability query — is not viable for this codebase. Because `cmd/cpg` imports the full Cilium codebase, `client-go`, and `cilium/ebpf`, both algorithms' interface-dispatch modeling considers common interfaces (`io.Writer`, `io.Closer`, arbitrary `Command`-shaped interfaces in `cilium/hive/script`) reachable from `Manager.Shutdown()`'s dependencies as potentially resolving to **any** implementation anywhere in the whole program — surfacing 70–102 entirely spurious "reachable" filesystem-write call paths through code cpg never actually calls (crypto FIPS self-tests, `klog` log-file rotation, `mime/multipart` upload handling, kubeconfig OIDC token-cache persistence, a Cilium shell-scripting DSL's `Mv`/`Cp`/`Sed`/`Mkdir` commands). An allowlist built to accommodate this noise would be 15–20x larger than necessary and would defeat the audit's purpose (a human cannot meaningfully review 100 "this is safe because CHA is imprecise" entries).

The validated fix: use **RTA** (not CHA — CHA produces false positives even among cpg's own functions, see below) rooted at the program's real `main`+`init` per RTA's documented API contract, BFS the resulting graph from the `runMCPServer` node to get the set of **cpg-owned** reachable functions (`github.com/SoulKyu/cpg/...` package prefix), and then perform a **direct SSA call-instruction scan** of only those functions' own bodies for disallowed callees — never further expanding into third-party call graphs. Run against this exact repository, this design produced **exactly 5 caller functions** — `(*pkg/session.Manager).Start`, `(*pkg/session.Manager).Shutdown$1`, `(*pkg/output.Writer).Write`, `(*pkg/evidence.Writer).Write`, `(*pkg/hubble.healthWriter).finalize` — precisely matching the CONTEXT's own D-03 prediction ("the atomic writers ... and pkg/session Manager tmpdir lifecycle ops"), with zero K8s write-verb hits (confirming D-02's baseline independently, via a different method than the original grep-based scouting).

For SRV-04, the full graceful and ungraceful e2e sequences were run for real: a `-race`-built `cpg` binary, spawned as a real subprocess, driven over real stdin/stdout pipes, against a real in-process fake `observerpb.ObserverServer` gRPC relay. Both passed. One genuine race was discovered and fixed during validation: the pipeline's background goroutine reaches the relay's `GetFlows` RPC **asynchronously** relative to `start_session`'s tool response — an ungraceful-disconnect test that closes stdin immediately after `get_status` can race ahead of the relay ever being dialed, making the "stream cancelled" assertion flaky unless the test synchronizes on the relay having actually been reached first.

**Primary recommendation:** Build the SEC-01 audit as RTA-rooted-at-main+init → BFS filtered to `github.com/SoulKyu/cpg/` functions → direct-call-instruction scan (not naive whole-program reachability); build the SRV-04 e2e as a hand-rolled `exec.Cmd` + pipes + `mcp.IOTransport` (not `mcp.CommandTransport`) so the raw stdout bytes can be teed for an explicit purity assertion and the ungraceful disconnect can be driven with precise, unassisted control; synchronize the ungraceful variant on the fake relay's `GetFlows` handler having actually started before closing stdin.

## Architectural Responsibility Map

cpg is a CLI + MCP stdio server, not a multi-tier web app — the standard browser/SSR/API/CDN/DB tiers don't map directly. The table below adapts the closest analogous boundaries for this phase's two new test artifacts and one doc change.

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Structural readonly audit (SEC-01) | Test/CI tooling (build-time static analysis) | — | Runs entirely at `go test` time against the SSA form of the compiled program; never executes at runtime, never touches a real cluster or filesystem beyond the analyzer's own bookkeeping |
| E2E stdio lifecycle test (SRV-04/SRV-01) | Test/CI tooling (drives the real MCP Server process) | MCP Server Process (the subject under test) | The test process is the "client tier"; the spawned `cpg mcp` subprocess is the "server tier" being exercised exactly as a real MCP host would exercise it |
| Fake Hubble relay (D-06) | Test/CI tooling (test double for an external service) | — | Stands in for the real Hubble Relay gRPC service; lives only inside the test process, never a shared/long-lived component |
| README MCP section (SEC-03) | Documentation | — | Static content, no runtime behavior; consumed by a human (SRE) configuring an external MCP host, not by cpg itself |

**Why this matters here:** the one genuine cross-tier risk this phase must get right is *not* accidentally testing at the wrong layer — e.g., asserting readonly safety by grepping source text (would miss indirection) instead of SSA-level reachability, or asserting e2e behavior only against the in-memory transport (already done in Phase 16-18) instead of the real stdio subprocess (this phase's actual, undone obligation per D-05/D-11).

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `golang.org/x/tools` (`go/packages`, `go/ssa`, `go/ssa/ssautil`, `go/callgraph`, `go/callgraph/rta`) | v0.44.0 | SSA construction + Rapid Type Analysis callgraph for the SEC-01 audit | `[VERIFIED: local module cache + go list -m]` Official Go team module (`golang.org/x/` namespace). Already resolved as an **indirect** transitive dependency in this exact repo's `go.mod`/`go.sum` at v0.44.0 — confirmed via `go list -m golang.org/x/tools` → `v0.44.0`, and via a real `go test` run that imported it directly with **zero go.mod/go.sum diff** (`git status --short go.mod go.sum` empty after). Its own `go.mod` declares `go 1.25.0`, compatible with this repo's `go1.25.12` toolchain — confirmed by successfully building and running against it. `golang.org/x/tools/cmd/callgraph` (shipped inside the same v0.44.0 module) is the exact upstream reference implementation this audit's design was validated against. |
| `github.com/modelcontextprotocol/go-sdk/mcp` (client-side: `NewClient`, `IOTransport`, `ClientSession`) | v1.6.1 (already a project dependency) | Drives the real-stdio e2e as an MCP client | `[VERIFIED: vendored source read + empirical run]` Already used server-side since Phase 16; this phase is the first to use its **client** API. `mcp.NewClient(&mcp.Implementation{...}, nil).Connect(ctx, transport, nil)` plus `ClientSession.ListTools`/`CallTool` were exercised for real against the compiled subprocess binary in this session and worked identically to the existing in-memory-transport tests (`cmd/cpg/mcp_harness_test.go`, `mcp_session_test.go`, `mcp_query_tools_test.go`) — same call shapes, different `Transport` implementation. |
| `google.golang.org/grpc` + `github.com/cilium/cilium/api/v1/observer` (`observerpb`) | v1.79.3 / v1.19.4 (already project dependencies) | The in-process fake Hubble relay (D-06) | `[VERIFIED: vendored source read + empirical run]` `observerpb.ObserverServer` interface has 6 methods (`GetFlows`, `GetAgentEvents`, `GetDebugEvents`, `GetNodes`, `GetNamespaces`, `ServerStatus`); `UnimplementedObserverServer` may be embedded by value for forward-compat stubs. Confirmed empirically that **only `GetFlows` is ever invoked** by `pkg/hubble.Client` — see Common Pitfalls / Code Examples. No new go.mod dependency: both packages are already direct dependencies used by production code. |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/SoulKyu/cpg/pkg/policy/testdata` (`IngressTCPFlow`, `EgressUDPFlow`) | in-repo | Base flow fixture builder for the fake relay's `GetFlows` payloads | Reuse for L4/labels/namespace shape, but see Common Pitfalls — **must additionally set `Verdict`/`DropReasonDesc`**, which these helpers deliberately don't set (they're designed for direct `policy.BuildPolicy` calls, not for flowing through the real classifier). |
| `net` (`net.Listen("tcp", "127.0.0.1:0")`) | stdlib | Ephemeral local port for the fake relay | Standard Go idiom; port 0 lets the OS assign a free port, avoiding flaky port collisions in CI. |
| `os/exec` | stdlib | Hand-rolled subprocess plumbing for the e2e (see recommendation below) | Preferred over `mcp.CommandTransport` for this phase's specific needs (byte-level stdout tee + precise ungraceful-disconnect timing control) — see Architecture Patterns Pattern 2 and Common Pitfalls. |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| RTA rooted at `main`+`init`, then BFS-restricted to cpg-owned functions, then direct-call scan | CHA whole-program reachability from `runMCPServer` | **Empirically rejected.** CHA produced 8 **false-positive** "reachable" cpg-owned functions not present in RTA's set, including `main`, `newGenerateCmd`, `newReplayCmd`, `newExplainCmd` — none of which `runMCPServer` can actually call. Using CHA for even the first-stage reachability query would force reviewing/allowlisting genuinely-unreachable CLI entry points, undermining trust in the audit. |
| RTA/CHA reachability + direct-call-instruction scan restricted to cpg-owned functions | RTA/CHA whole-program reachability query for "is `os.WriteFile` reachable from `runMCPServer`" (the literal, naive reading of D-01–D-03) | **Empirically rejected as the sole mechanism.** Produces 70 (RTA) to 102 (CHA) distinct "caller" functions, nearly all spurious (third-party interface-dispatch artifacts unrelated to cpg's actual behavior). Not humanly reviewable as a security control. |
| Hand-rolled `exec.Cmd` + pipes + `mcp.IOTransport` (client side) | `mcp.CommandTransport{Command: exec.Command(...)}` | `CommandTransport` **does work** (confirmed via its own test suite, `cmd_test.go`) and is simpler to wire. It's a legitimate simpler alternative for the *graceful* variant alone. Rejected as the uniform choice because: (1) it doesn't expose the raw stdout stream for an independent byte-purity tee — its `Connect` internally wraps `StdoutPipe()` and hands it straight to the JSON-RPC decoder, so "every stdout byte parses as JSON-RPC" would only be provable *implicitly* (any corruption breaks the whole session with a generic decode error) rather than as an explicit, diagnostic assertion; (2) its `Close()` cascade (close stdin → wait → SIGTERM → SIGKILL) conflates "the server exited on its own" with "we had to escalate," making the ungraceful-disconnect test's core claim ("the process exits on its own") harder to assert cleanly. |
| Rooting RTA directly at `runMCPServer` (as CONTEXT.md's open question literally asks) | Rooting RTA at `main`+`init` (per its documented contract), then BFS-restricting the resulting graph | RTA's own package doc (`golang.org/x/tools/go/callgraph/rta`, `Analyze`'s doc comment) states: *"The root functions must be one or more entrypoints (main and init functions) of a complete SSA program."* Rooting at an arbitrary interior function is off-label — it ran without crashing in this session's probe, but nothing in RTA's contract guarantees its soundness reasoning holds for a non-main/init root. Building the whole-program graph the documented way, then querying reachability-from-`runMCPServer` as a downstream BFS over that graph, respects the contract and is exactly what the exact same set of `cmd/callgraph`'s own commands do when asked for a subgraph. |

**Installation:**
```bash
# golang.org/x/tools is already resolved (indirect, v0.44.0) — importing it directly in a
# _test.go file requires NO go.mod edit to build/test successfully (verified: git diff
# go.mod/go.sum was empty after a real `go test` run that imported go/packages, go/ssa,
# go/ssa/ssautil, go/callgraph, and go/callgraph/rta directly). Run this once the test
# file exists, to flip the go.mod comment from `// indirect` to direct and keep go.mod
# accurate (cosmetic, not required for tests to pass):
go mod tidy
```

**Version verification:** `go list -m golang.org/x/tools` → `v0.44.0` (already resolved). `go list -m -versions golang.org/x/tools` shows newer releases exist up to `v0.48.0` on the proxy; recommend **staying on the already-resolved v0.44.0** rather than deliberately bumping, to keep this phase's dependency footprint to the documented "test-only, dev-scope" line CONTEXT.md already calls out — a version bump, if it happens, should arrive from an unrelated `go mod tidy` after some other dependency change, not from this phase.

## Package Legitimacy Audit

> Required whenever a phase installs external packages. This phase adds exactly one new **direct** (test-only) `go.mod` line: `golang.org/x/tools`. It is not a new *download* — the module is already present, hashed, and verified in `go.sum` today as an indirect dependency (pulled in transitively, most likely via `k8s.io/*` tooling); this phase only promotes an existing, already-trusted module to direct usage.

`slopcheck` targets npm/PyPI package-name hallucination and has no meaningful applicability to the Go module ecosystem (Go modules are namespaced by VCS-hosted import path, not a flat registry name, which structurally prevents the "sound-alike package name" attack slopcheck screens for). It was attempted per protocol (`pip install slopcheck --break-system-packages`) and did not produce a usable `slopcheck` binary on `PATH` in this environment. Per the graceful-degradation clause, the package below is tagged `[ASSUMED]` — but every fact used to reach that judgment was independently, empirically verified in this session (not merely asserted), which the table documents explicitly.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `golang.org/x/tools` | Go module proxy (proxy.golang.org) | Official Go team module since ~2011; this specific `v0.44.0` release resolvable via `go list -m -versions` alongside 14 newer releases up to v0.48.0 | N/A (Go proxy doesn't expose download counts) | `github.com/golang/tools` (official Go org, code review gated via Gerrit, GitHub is a read-only mirror) | not run (unsupported ecosystem) | **Approved** — already a hashed, verified transitive dependency of this exact repo (`go.sum` contains it today); promoting to direct added zero new lines to `go.sum`; empirically built and executed successfully against this repo's toolchain in this session |

**Packages removed due to slopcheck `[SLOP]` verdict:** none
**Packages flagged as suspicious `[SUS]`:** none

*Package tagged `[ASSUMED]` per protocol (slopcheck unavailable for this ecosystem) — however, unlike a typical unverified `[ASSUMED]` package, this one was independently confirmed via `go list -m`, was already present in `go.sum` before this research session began, and was successfully built/executed against the real repository multiple times in this session. The planner may treat this with higher-than-default confidence for a `[ASSUMED]` tag, but should still gate the `go.mod` change behind a normal code-review step (not a `checkpoint:human-verify`, given it's a zero-new-hash addition) per standard practice.*

## Architecture Patterns

### System Architecture Diagram — SEC-01 audit test

```
                    go test ./cmd/cpg/...  (this test, at test time only)
                                  │
                                  ▼
                     packages.Load(cfg, ".")            cfg.Mode = LoadAllSyntax
                     [loads cmd/cpg + full dep graph]    cfg.Tests = false
                                  │
                                  ▼
                ssautil.AllPackages(initial, InstantiateGenerics)
                     prog.Build()          [builds SSA IR for everything]
                                  │
                                  ▼
        rta.Analyze([]*ssa.Function{mainPkg.Func("main"), mainPkg.Func("init")}, true)
        [RTA's documented contract: root at main+init, NOT at runMCPServer directly]
                                  │
                                  ▼
              BFS the resulting *callgraph.Graph starting at the
              runMCPServer node  →  visited map[*ssa.Function]bool
                                  │
                                  ▼
         FILTER: keep only visited functions whose package path has
         prefix "github.com/SoulKyu/cpg/"   (drop all third-party noise)
                                  │
                                  ▼
      For EACH remaining cpg-owned function (NOT recursing further):
      walk its own ssa.Function.Blocks[].Instrs for *ssa.Call instructions
                                  │
                    ┌─────────────┴─────────────┐
                    ▼                             ▼
        StaticCallee() name matches         IsInvoke() (dynamic/interface
        a disallowed fs-write func          call) whose Method.Name() is a
        (os.WriteFile/Create/...)           k8s write verb (Create/Update/
                    │                        Patch/Delete/Apply) AND the
                    ▼                        interface/receiver type looks
        Is this exact (caller,callee)        k8s-client-shaped
        pair in the hand-written                    │
        allowlist with a documented                 ▼
        safety rationale?                   FAIL — no allowlist exists for
             │           │                   this property (D-02: baseline
            YES          NO                   is zero, any hit is new)
             │            │
             ▼            ▼
           PASS      FAIL, print caller
                      name + one call path
                      from runMCPServer
```

### System Architecture Diagram — SRV-04 e2e test (both variants)

```
   go test process (this test)                    fake Hubble relay (in-process)
   ────────────────────────────                    ────────────────────────────
   1. go build -race -o tmp/cpg ./cmd/cpg          net.Listen("tcp","127.0.0.1:0")
      (once per test binary run)                   grpc.NewServer()
                                                    RegisterObserverServer(srv, fake)
   2. exec.Command(tmp/cpg, "mcp")                 go grpcServer.Serve(lis)
      cmd.StdinPipe()  → stdinW
      cmd.StdoutPipe() → stdoutR  ──┐
      cmd.Start()                   │ io.TeeReader
                                     ▼
                          rawTee (bytes.Buffer, for the
                          explicit purity re-validation)
                                     │
                          mcp.IOTransport{Reader: teed, Writer: stdinW}
                          mcp.NewClient(...).Connect(ctx, transport, nil)
                                     │
              ┌──────────────────────┴──────────────────────┐
              ▼                                              ▼
     initialize / tools-list / CallTool(...)         [real cpg mcp subprocess]
     over newline-delimited JSON-RPC                 runMCPServer → Manager.Start
     on the real stdin/stdout pipes                       │ (background goroutine,
              │                                            │  detached from tool-call ctx)
              ▼                                            ▼
     start_session{server:relayAddr,tls:false} ──────► hubble.Client.StreamDroppedFlows
     (D-07 bypass: no kubeconfig, no port-forward)         │  waitForConnReady (channel-
              │                                            │  state check, NO RPC call)
              ▼                                            ▼
     get_status (poll) ◄───────────────────────── observerpb.ObserverClient.GetFlows(ctx,req)
     (tmp_dir captured here)                              │  ── ONLY this RPC is ever
              │                                           │     invoked by the real client
              ▼                                           ▼
     [GRACEFUL]                                   fake relay's GetFlows sends fixture
     list_policies / stop_session                 flows, then blocks on
     → assert real files exist in tmp_dir         <-stream.Context().Done()
              │                                           │
              ▼                                           │
     stdinW.Close()  ──────────────────────────►  server.Run(ctx,transport) returns nil
     (graceful: AFTER stop_session)                (peer EOF ⇒ err==nil, confirmed via
              │                                     jsonrpc2/conn.go's wait() logic)
              ▼                                           │
     cmd.Wait() returns nil, exit 0                        ▼
     assertStdoutPurity(rawTee.Bytes())            mgr.Shutdown() already ran synchronously
                                                    BEFORE runMCPServer returned (bounded
     [UNGRACEFUL]                                  fan-out: cancel session ctx, wait ≤5s,
     stdinW.Close() WITHOUT stop_session            os.RemoveAll(tmpDir) wait ≤2s)
     (ONLY after confirming fake.started==true             │
      — see Common Pitfalls for why this                   ▼
      synchronization is required)              stream.Context().Done() fires in the fake
              │                                  relay's GetFlows handler → fake.cancelled=true
              ▼
     assert: process exits within ~1s (well
     under the 10s test cap); tmp_dir removed;
     fake.snapshot() shows started=true,cancelled=true
```

### Recommended Project Structure

No new packages. Two new test files in the existing `cmd/cpg` package (matching the "Claude's Discretion" naming latitude), plus a README edit:

```
cmd/cpg/
├── mcp_audit_test.go     # NEW — SEC-01: RTA reachability + direct-call scan + allowlist
├── mcp_e2e_test.go       # NEW — SRV-04/SRV-01: real-subprocess graceful + ungraceful lifecycle
├── mcp.go                # unchanged — runMCPServer is the audit's root and the e2e's target
├── mcp_tools.go           # unchanged — session tools (read here for D-10's exact annotations)
├── mcp_query.go           # unchanged — query tools
├── mcp_harness_test.go   # unchanged — stays as the fast in-memory-transport layer (D-11)
├── mcp_session_test.go   # unchanged
└── mcp_query_tools_test.go  # unchanged — the existing all-8-tools in-memory assertion

README.md                 # EDIT — new "## MCP Server (cpg mcp)" section after "## Explain policies"
```

### Pattern 1: RTA-rooted-at-main + BFS-filter-to-cpg-owned + direct-call scan (SEC-01)

**What:** A three-stage pipeline that avoids whole-program interface-dispatch noise: (1) compute a sound, precise whole-program callgraph via RTA rooted the way its API contract requires; (2) reduce that graph to "which of **cpg's own** functions are reachable from `runMCPServer`" via a plain BFS; (3) for each such function, scan **only its own SSA instructions** (never recursing into third-party callees) for a disallowed call.

**When to use:** Any "is dangerous capability X reachable from safe entry point Y" audit in a Go codebase that imports large third-party dependency trees with heavy interface usage. The naive "is X reachable via any path" formulation breaks down exactly when the transitive dependency graph is large enough that common interfaces (`io.Writer`, `io.Closer`) have dozens of unrelated implementations.

**Example (validated: produced exactly 5 expected callers, 0 k8s-write hits, when run against this repository):**
```go
// Source: this research session's throwaway probe, run against
// github.com/SoulKyu/cpg @ 2026-07-21 (deleted before this doc was written;
// git status confirmed clean). Reproduced here as the validated pattern.

cfg := &packages.Config{Mode: packages.LoadAllSyntax, Tests: false, Dir: "."}
initial, err := packages.Load(cfg, ".")
// ... err handling, packages.PrintErrors(initial) check ...

mode := ssa.InstantiateGenerics // required for soundness (matches x/tools/cmd/callgraph)
prog, pkgs := ssautil.AllPackages(initial, mode)
prog.Build()

var mainPkg *ssa.Package
for _, p := range pkgs {
    if p != nil && p.Pkg.Name() == "main" {
        mainPkg = p
    }
}
root := mainPkg.Func("runMCPServer")
mainFn, initFn := mainPkg.Func("main"), mainPkg.Func("init")

// RTA's own doc: "The root functions must be one or more entrypoints (main
// and init functions) of a complete SSA program." Root there, not at
// runMCPServer directly.
rtaRes := rta.Analyze([]*ssa.Function{mainFn, initFn}, true)

// BFS from the runMCPServer node within RTA's whole-program graph.
visited := bfsReachable(rtaRes.CallGraph, root) // plain BFS over Node.Out edges

// Restrict to cpg's own package tree -- this is what keeps the result small
// and precise; third-party functions are never individually re-scanned.
cpgOwned := map[*ssa.Function]bool{}
for f := range visited {
    if f != nil && f.Pkg != nil && f.Pkg.Pkg != nil &&
        strings.HasPrefix(f.Pkg.Pkg.Path(), "github.com/SoulKyu/cpg/") {
        cpgOwned[f] = true
    }
}

// Direct call-site scan: for each cpg-owned reachable function, look at ITS
// OWN instructions only. Do not expand into third-party callees.
for f := range cpgOwned {
    for _, b := range f.Blocks {
        for _, instr := range b.Instrs {
            call, ok := instr.(ssa.CallInstruction)
            if !ok {
                continue
            }
            common := call.Common()
            if callee := common.StaticCallee(); callee != nil {
                if disallowedFSWrite[callee.String()] {
                    assertAllowlisted(f.String(), callee.String()) // fails the test if not allowlisted
                }
            } else if common.IsInvoke() && common.Method != nil {
                if k8sWriteVerbs[common.Method.Name()] {
                    // check common.Value.Type().String() looks k8s-client-shaped
                    assertAllowlisted(f.String(), common.Method.Name())
                }
            }
        }
    }
}
```

**Validated result on this exact repository** (RTA, `-race` off, ~16s wall clock):
```
caller: (*github.com/SoulKyu/cpg/pkg/session.Manager).Start
    -> STATIC:os.MkdirTemp, STATIC:os.RemoveAll (cleanup-on-failure paths)
caller: (*github.com/SoulKyu/cpg/pkg/output.Writer).Write
    -> STATIC:os.MkdirAll, os.CreateTemp, os.Remove, os.Rename
caller: (*github.com/SoulKyu/cpg/pkg/session.Manager).Shutdown$1
    -> STATIC:os.RemoveAll   (the anonymous closure inside Shutdown's bounded-remove goroutine)
caller: (*github.com/SoulKyu/cpg/pkg/evidence.Writer).Write
    -> STATIC:os.MkdirAll, os.CreateTemp, os.Remove, os.Rename
caller: (*github.com/SoulKyu/cpg/pkg/hubble.healthWriter).finalize
    -> STATIC:os.MkdirAll, os.CreateTemp, os.Remove, os.Rename
k8s write-verb hits: 0
```
This is precisely the "atomic writers + Manager tmpdir lifecycle ops" allowlist CONTEXT.md's D-03 predicted, with no extraneous entries.

### Pattern 2: Hand-rolled subprocess + byte-tee + `mcp.IOTransport` client (SRV-04)

**What:** Instead of `mcp.CommandTransport`, manually create `exec.Cmd`, get `StdinPipe()`/`StdoutPipe()`, wrap the stdout reader in `io.TeeReader` for an independent byte-purity buffer, then hand the (wrapped) reader/writer pair to `mcp.IOTransport` — the same `Transport` implementation already proven safe by the existing in-memory-transport test suite, just backed by real OS pipes instead of `net.Pipe()`.

**When to use:** Whenever a test needs both (a) the SDK's full protocol handling (framing, initialize, dispatch) and (b) independent, diagnostic visibility into the raw bytes crossing the wire, or (c) precise control over exactly when/how the transport is torn down (as opposed to `CommandTransport`'s bundled close-then-escalate cascade).

**Example (validated: full graceful lifecycle passed, 24996 raw bytes / 8 JSON-RPC lines, zero corruption):**
```go
// Source: this research session's throwaway probe (deleted; git clean after).
cmd := exec.Command(binPath, "mcp")
stdinW, _ := cmd.StdinPipe()
stdoutR, _ := cmd.StdoutPipe()
var stderrBuf bytes.Buffer
cmd.Stderr = &stderrBuf // capture server-side zap/zapslog logs for failure diagnostics
cmd.Start()

var rawTee bytes.Buffer
teed := io.TeeReader(stdoutR, &rawTee) // independent copy for the explicit purity check

transport := &mcp.IOTransport{Reader: io.NopCloser(teed), Writer: stdinW}
client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client", Version: "0.0.0"}, nil)
cs, err := client.Connect(ctx, transport, nil) // same call shape as every existing in-memory test

exitedCh := make(chan error, 1)
go func() { exitedCh <- cmd.Wait() }()

// ... drive initialize/tools-list/CallTool exactly like mcp_session_test.go's
//     existing helpers (cs.ListTools, cs.CallTool) -- zero new client-side API surface ...

// Graceful: AFTER stop_session, close stdin only, then wait for self-exit.
stdinW.Close()
select {
case err := <-exitedCh:
    // err == nil confirmed empirically: go-sdk's jsonrpc2 Connection.wait()
    // treats a peer-initiated clean io.EOF as NOT an error (internal/jsonrpc2/
    // conn.go:448 `if !errors.Is(s.readErr, io.EOF) { err = s.readErr }`), so
    // server.Run returns nil, RunE returns nil, main() exits 0.
case <-time.After(10 * time.Second):
    t.Fatal("did not exit in time")
}

// Explicit byte-purity re-validation, independent of whatever the SDK
// already tolerated:
for _, line := range bytes.Split(rawTee.Bytes(), []byte("\n")) {
    if len(bytes.TrimSpace(line)) == 0 {
        continue
    }
    var js json.RawMessage
    if err := json.Unmarshal(line, &js); err != nil {
        t.Fatalf("stdout purity violation: %q: %v", line, err)
    }
}
```

**Validated server-side log sequence** (from real subprocess stderr, confirming SRV-03's bridge works end-to-end too):
```
INFO  server run start
INFO  server connecting
INFO  server session connected {"session_id": ""}
INFO  session initialized
INFO  session/manager.go:194  session started {"session_id": "sess_...", "evidence_session_id": "..."}
INFO  hubble/pipeline.go:176  connected to Hubble Relay, streaming dropped flows {"server": "127.0.0.1:PORT", ...}
INFO  output/writer.go:75  policy written {"path": "/tmp/cpg-session-.../policies/prod/api.yaml"}
INFO  hubble/health_writer.go:93  health writer: no infra/transient drops observed — skipping cluster-health.json
INFO  hubble/pipeline.go:140  session summary {"duration": "609ms", "flows_seen": 1, "policies_written": 1, ...}
INFO  server session disconnected {"session_id": ""}
INFO  server session ended
```

### Anti-Patterns to Avoid

- **Whole-program "is X reachable from the root" as the SEC-01 mechanism:** produces 70–102 spurious callers on this codebase (empirically measured). Always restrict the "does it call something dangerous" check to a direct scan of cpg-owned function bodies, using callgraph reachability only to determine which cpg functions are in-scope.
- **Using CHA for the reachability stage:** CHA falsely includes `main`, `newGenerateCmd`, `newReplayCmd`, `newExplainCmd` as "reachable from `runMCPServer`" in this exact codebase (empirically measured) — these are not reachable by any real call path. RTA (rooted per its documented contract) does not have this problem.
- **Rooting RTA directly at `runMCPServer`:** works mechanically but is off-label per RTA's own doc comment. Root at `main`+`init`, then BFS/query the resulting graph for the `runMCPServer` subgraph instead.
- **`mcp.CommandTransport` for the ungraceful-disconnect assertion:** its `Close()` bundles "close stdin" with a timed SIGTERM/SIGKILL escalation, making "the process exited on its own" harder to assert unambiguously than with a hand-rolled `stdinW.Close()` + independent `cmd.Wait()` race.
- **Reusing `pkg/policy/testdata` flow builders unmodified for the fake relay's fixtures:** they don't set `Verdict`/`DropReasonDesc` (by design, for a different call path) — a flow built this way will silently produce **zero** policy/evidence output when fed through the real pipeline, with no error, just an empty result that looks like "nothing happened yet" rather than "the fixture was wrong."
- **Closing stdin for the ungraceful variant immediately after `get_status`, without synchronizing on the relay having been reached:** the pipeline's `GetFlows` call happens asynchronously in a detached goroutine; disconnecting too early makes the "relay stream was cancelled" assertion flaky/false (empirically reproduced — see Common Pitfalls).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| JSON-RPC framing/parsing for the e2e client | A hand-rolled newline-delimited-JSON reader/writer | `mcp.IOTransport` + `mcp.NewClient`/`ClientSession` (already proven by 3 existing test files in this package) | The SDK's `ioConn` already handles batching, trailing-data validation, and the exact framing (`ndjson`) correctly; reimplementing it would duplicate tested code and risk subtly different edge-case behavior from the real MCP hosts this server must interoperate with |
| Whole-program call reachability | A custom AST walker following function calls textually | `golang.org/x/tools/go/ssa` + `go/callgraph/rta` | A textual/AST-level walk cannot account for interface dispatch, generics instantiation, or closures — exactly the constructs D-01 calls out as needing "function-level reachability," not a package/import scan |
| The fake Hubble relay's gRPC framing | A hand-rolled gRPC-wire-format server | `google.golang.org/grpc.NewServer()` + generated `observerpb.RegisterObserverServer` | Already a direct dependency; the generated stubs are what the real `pkg/hubble.Client` expects to talk to — anything else risks testing a fake protocol, not the real one |
| Subprocess lifecycle (build once, run many, terminate) | Custom process-group/signal plumbing | `os/exec.Cmd` (`StdinPipe`/`StdoutPipe`/`Start`/`Wait`) — stdlib | Already the exact mechanism `mcp.CommandTransport` itself uses internally; no reason to go lower-level than the stdlib primitives it's built on |

**Key insight:** every "don't hand-roll" item above is already a proven, tested library in this exact dependency tree — Phase 19 adds zero new hand-rolled infrastructure beyond the two novel analysis/filter steps (the cpg-ownership package-prefix filter and the direct-call-instruction scan), both of which are irreducibly project-specific and have no library to delegate to.

## Common Pitfalls

### Pitfall 1: Whole-program reachability explodes into third-party noise
**What goes wrong:** An audit test asking "is `os.WriteFile` reachable from `runMCPServer`" via ordinary CHA or RTA whole-program queries returns dozens of unrelated call paths through library internals cpg never actually exercises.
**Why it happens:** Both algorithms model interface/dynamic dispatch conservatively. CHA assumes any interface method call may resolve to any type in the whole program implementing that method; RTA is more precise (only types actually observed as "runtime types" via `MakeInterface`) but is still whole-program by construction. A codebase importing Cilium + client-go + `cilium/ebpf` has thousands of types implementing common interfaces (`io.Writer`, `io.Closer`), and cpg's own code (e.g. `Manager.Shutdown()`, which touches goroutines, `errgroup`, contexts) touches enough of those interfaces that the "reachable" set balloons.
**How to avoid:** Restrict the "does this call a disallowed function" check to cpg-owned functions' **own** direct SSA call instructions (never recursing the check into third-party callees). Use callgraph reachability only to determine the cpg-owned in-scope set.
**Warning signs:** An allowlist growing past ~10-15 entries, or containing packages the audit author doesn't recognize as part of cpg's own logic (e.g. `crypto/internal/fips140`, `mime/multipart`, `k8s.io/client-go/tools/clientcmd`, `github.com/cilium/hive/script`) — all empirically observed as CHA/RTA false-positive paths in this exact codebase.

### Pitfall 2: CHA produces false positives even for the reachability-scoping stage
**What goes wrong:** Using CHA (rather than RTA) even just to determine "which cpg-owned functions are reachable from `runMCPServer`" (Pattern 1's stage 2) falsely includes CLI-only entry points.
**Why it happens:** CHA's coarse "any interface-typed call could dispatch anywhere" reasoning also affects ordinary reachability queries when a function value is stored/passed around (e.g. cobra's `Command.RunE` field assignment can look, to CHA, like "this function might be called from anywhere a `RunE`-shaped value is invoked").
**How to avoid:** Use RTA (rooted per its documented main+init contract), not CHA, for the reachability-scoping stage. Empirically verified on this codebase: RTA's cpg-owned reachable set (320 functions) is a strict subset of CHA's (328 functions) — the 8-function difference is exactly `main`, `newExplainCmd`, `newReplayCmd`, `newMCPCmd`, `dropclass.init#1`, `addCommonFlags`, `isKubectlPlugin`, `newGenerateCmd`, none of which `runMCPServer` genuinely calls.
**Warning signs:** The audit flags a CLI-only function (`runGenerate`, `newReplayCmd`, etc.) as "reachable from the MCP composition root" — this is the algorithm's imprecision, not a real leak, but treating it as real would be actively misleading.

### Pitfall 3: The pipeline's relay connection is asynchronous relative to `start_session`'s response
**What goes wrong:** An ungraceful-disconnect test that calls `start_session`, then immediately `get_status`, then immediately closes stdin, can race ahead of the background pipeline goroutine ever calling `GetFlows` on the fake relay — making the "the relay's stream context was cancelled" assertion fail non-deterministically (reproduced empirically in this session: first attempt showed `fake relay: started=false cancelled=false` even though the process itself exited correctly and the tmpdir was removed correctly).
**Why it happens:** `Manager.Start` returns as soon as its **synchronous** setup (kubeconfig/port-forward-or-bypass resolution) completes; the actual `hubble.Client.StreamDroppedFlows` → `GetFlows` call happens later, inside the detached background goroutine (`go func() { err := m.runPipeline(sessionCtx, cfg) ... }()` in `pkg/session/manager.go`). `get_status` can return "capturing" before that goroutine has made its first RPC.
**How to avoid:** Have the fake relay's `GetFlows` handler set a flag/close a channel the moment it's invoked, and have the ungraceful-disconnect test poll/wait on that signal (bounded, e.g. 5s) **before** closing stdin. Fixed and re-verified in this session: with the synchronization added, the test passed reliably (`fake relay: started=true cancelled=true`, process self-exit in ~1.02s).
**Warning signs:** The ungraceful test passes most of the time but occasionally reports the relay was never reached — a classic async-race signature, not a real product bug.

### Pitfall 4: Existing flow-fixture helpers don't set `Verdict`/`DropReasonDesc`
**What goes wrong:** Reusing `pkg/policy/testdata.IngressTCPFlow`/`EgressUDPFlow` verbatim as the fake relay's `GetFlows` payload produces a flow that the real classifier never treats as a drop at all — `Manager.Start`'s real pipeline runs to completion having "seen" the flow but written zero policy/evidence files, silently.
**Why it happens:** Those helpers were built for tests that call `policy.BuildPolicy` **directly** (already past the classification stage) — they set `TrafficDirection`/`Source`/`Destination`/`L4` but never `Verdict` or `DropReasonDesc`, because that test path never re-derives drop-classification from the flow.
**How to avoid:** After building the base flow, explicitly set `flow.Verdict = flowpb.Verdict_DROPPED` and `flow.DropReasonDesc = flowpb.DropReason_POLICY_DENIED` (value 133; maps to `dropclass.DropClassPolicy`, confirmed via `pkg/dropclass/classifier.go:75`) before handing it to the fake relay. Verified empirically: with these two fields set, a real `-race`-built subprocess against a real fake relay produced a genuine `policies/prod/api.yaml` file and a `policies_written: 1` summary.
**Warning signs:** `get_status`'s `policy_file_count` stays at 0 through the whole test despite the relay reporting flows sent; `list_policies` returns an empty page.

### Pitfall 5: The audit test's wall-clock cost is real and non-trivial, especially under `-race`
**What goes wrong:** A plan or CI run treats a ~45-76 second single test as a hang or a regression.
**Why it happens:** `packages.LoadAllSyntax` type-checks the **entire** transitive dependency graph (Cilium + client-go + ebpf — a large codebase) in-process; RTA/CHA callgraph construction is itself CPU-bound. Measured on this exact repository (identical hardware, this session): non-`-race` — `packages.Load` ~5.3s, SSA build ~3.5s, RTA graph build ~5.0s (total single-algorithm run ≈13-14s). Under `-race` — `packages.Load` ~18.3s, SSA build ~9.7-10.0s, RTA graph build ~27-28s (total single-algorithm run ≈55-56s; a run doing both CHA+RTA for comparison measured 70-76s).
**How to avoid:** Budget for it explicitly in the plan and in VERIFICATION — this is one test among ~600+ others, and its cost is dominated by analyzing dependencies, not by anything this phase's code does. Do not add a custom `-timeout` below ~120s for this specific test; Go's default 10-minute per-package timeout comfortably covers it either way. Consider (planner's call, not required) a `testing.Short()` skip if CI wall-clock becomes a concern later — not needed today given CI's `go test -race -count=1 ./...` has no explicit timeout and already accommodates a large build+test matrix.
**Warning signs:** A CI run for `cmd/cpg` that used to take single-digit seconds now takes ~1 extra minute — expected, not a regression, as long as it's this one test.

### Pitfall 6: `client-go`'s blank-imported OIDC auth plugin has a token-cache persistence path that writes to disk
**What goes wrong:** RTA's reachable-set (before restricting to cpg-owned functions) includes a path from `runMCPServer` through `k8s.io/client-go/plugin/pkg/client/auth/oidc` → `clientcmd.ModifyConfig`/`WriteToFile` → `os.WriteFile` — i.e., a THIRD-PARTY capability (not cpg's own code) that, if actually exercised, would write to the user's kubeconfig file to cache a refreshed OIDC token.
**Why it happens:** `pkg/k8s/client.go` blank-imports `k8s.io/client-go/plugin/pkg/client/auth` for OIDC/GCP/Azure/exec support (documented, intentional, needed for real clusters using those auth methods). This registers the OIDC auth provider's round-tripper into client-go's transport stack; if a user's kubeconfig actually specifies OIDC auth with a refreshable token, client-go's own library code (not cpg's) may write the refreshed token back to the kubeconfig file on disk.
**How to avoid:** This finding is correctly **excluded** by Pattern 1's design (it's third-party code, not a cpg-owned function, so the direct-call-scan never examines it) — the audit is right not to flag it as an SEC-01 violation the way a naive whole-program query would. However, it is a legitimate, separate question worth an explicit written decision (not blocking this phase): does cpg's MCP mode ever plausibly encounter a kubeconfig with OIDC auth in practice, and if so, is "client-go may rewrite the user's kubeconfig file" an acceptable, documented behavior, or should it be defended against (e.g., loading a read-only copy of the kubeconfig)? Flagged as an Open Question below rather than folded into the audit's pass/fail — it is a pre-existing client-go behavior, not something this phase introduces or regresses.
**Warning signs:** None currently observed in practice — this is a "what does client-go do in a scenario cpg's tests don't exercise" question, not an observed bug.

## Code Examples

### Exact session-tool annotation ground truth (for D-10's schema assertions)

```go
// Source: github.com/SoulKyu/cpg/cmd/cpg/mcp_tools.go (read directly this session)
// start_session:
Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false}
// get_status:
Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}
// stop_session:
Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: true}
```
None of the 3 session tools set `OpenWorldHint` (only the 5 query tools do, per Phase 18's D-16: `ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: jsonschema.Ptr(false)` — confirmed in `mcp_query.go` and cross-checked by the existing `TestMCPQueryToolsQRY05Contract` test). A D-10 schema assertion that expects `OpenWorldHint` on the session tools would be asserting something the registration code never sets — assert its presence/value only for the 5 query tools.

### The one gRPC method the fake relay must implement

```go
// Source: github.com/SoulKyu/cpg/pkg/hubble/client.go (read directly this session)
// waitForConnReady (client.go:109) does NOT make any RPC call -- it is purely
// a gRPC channel-state check:
func waitForConnReady(ctx context.Context, conn *grpc.ClientConn, timeout time.Duration) error {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready { return nil }
		if !conn.WaitForStateChange(dialCtx, state) {
			return fmt.Errorf("connecting to hubble relay %q: %w", conn.Target(), dialCtx.Err())
		}
	}
}
// The ONLY RPC actually invoked afterward is:
client := observerpb.NewObserverClient(conn)
stream, err := client.GetFlows(ctx, req)
```
A fake relay embedding `observerpb.UnimplementedObserverServer` (by value) and implementing only `GetFlows` is sufficient — `ServerStatus`/`GetNodes`/etc. are never called by `pkg/hubble.Client` and do not need implementations.

### Fixture flow that actually produces a policy through the real pipeline

```go
// Source: this session's validated probe (deleted; pattern reproduced here).
flow := testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=api"}, "prod", 8080)
flow.Verdict = flowpb.Verdict_DROPPED                    // required -- not set by testdata helpers
flow.DropReasonDesc = flowpb.DropReason_POLICY_DENIED    // required -- maps to dropclass.DropClassPolicy
// Fed through a real fake-relay -> real cpg-race subprocess -> real pipeline, this
// produced: policies/prod/api.yaml on disk, list_policies returning it, and
// stop_session reporting policies_written: 1, flows_seen: 1.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| In-memory-transport-only MCP testing (Phases 16-18) | Real-subprocess stdio testing added alongside (this phase) | This phase (SRV-04) | The in-memory layer stays as fast-feedback (D-11); it cannot, by construction, prove real newline-delimited-JSON framing over actual OS pipes — that gap is exactly what this phase closes |
| Grep-based "no `.Create(`/`.Update(`" scouting (used to establish the D-02 baseline in prior research) | SSA-level, re-runnable structural audit (this phase) | This phase (SEC-01) | Grep cannot catch indirection (a future tool calling a helper that calls a write verb) or survive refactors; the SSA audit is automatically re-swept for every future `registerXTools` call, per D-04 |

**Deprecated/outdated:** None specific to this phase's tooling — `golang.org/x/tools`'s `go/ssa`/`go/callgraph` APIs used here are the current, actively maintained ones (same APIs `golang.org/x/tools/cmd/callgraph` itself uses in this exact v0.44.0 release).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | README section placement recommendation (insert after `## Explain policies`, before `## Label selection`) is the correct interpretation of D-12's "after the existing command documentation" | Architecture Patterns / SEC-03 | Low — this is a documentation-ordering judgment call, not a technical fact; if wrong, it's a one-line diff to move the section, no functional impact |
| A2 | `golang.org/x/tools` needs no `checkpoint:human-verify` gate given it's already a hashed transitive dependency of this repo | Package Legitimacy Audit | Low — worst case the planner adds a gate anyway; being over-cautious here costs a few minutes of human review, not a real risk |

**Note on the rest of this document:** unlike most research documents, the large majority of claims here are tagged `[VERIFIED: ...]` because they were independently confirmed by executing real code against this exact repository in this session (not merely read from documentation or inferred from training knowledge) — see the empirically-run probes referenced throughout Architecture Patterns and Common Pitfalls.

## Open Questions

1. **Does cpg's MCP mode ever plausibly encounter a kubeconfig using OIDC `exec` auth with token-cache persistence, and if so, is client-go's potential rewrite of the kubeconfig file (Pitfall 6) an acceptable, already-documented behavior?**
   - What we know: `pkg/k8s/client.go` blank-imports the auth-plugin package (needed for real-world exec/OIDC/GCP/Azure auth support); RTA's whole-program reachability shows a real (if third-party) code path from client-go's OIDC round-tripper to `clientcmd.WriteToFile`.
   - What's unclear: whether this is purely a theoretical library capability never actually exercised by cpg's own configuration, or a latent, currently-undocumented side effect for OIDC-auth clusters.
   - Recommendation: Out of scope for SEC-01's pass/fail (it's third-party code, correctly excluded by the direct-call-scan design), but worth a one-line note in SEC-03's exec-credential-plugin caveat (D-13 item 5) acknowlodging that some auth plugins may refresh/persist credentials back to the kubeconfig file, alongside the existing interactive-hang caveat.

2. **Should the SEC-01 audit's fs-write allowlist match on the exact call-site (caller, callee) pair, or on the caller function alone?**
   - What we know: the 5 validated caller functions each make 1-6 distinct fs-write calls (e.g. `Writer.Write` calls `os.MkdirAll`, `os.CreateTemp`, `os.Remove` three times across different error paths, and `os.Rename`).
   - What's unclear: whether the allowlist should be keyed per-function (any fs-write from `(*pkg/output.Writer).Write` is fine) or per-(function,callee) pair (only these specific 4 stdlib functions from this function are fine, a 5th would still fail).
   - Recommendation: Per-function is simpler and matches D-03's own phrasing ("explicit allowlist of function symbols... each documented in the test with WHY it is safe") — the function-level safety rationale (paths rooted in `os.MkdirTemp`/`DeriveSessionPaths`) already covers every fs call that function makes internally. Per-(function,callee) is stricter but adds maintenance friction for zero realistic safety gain given these are the atomic-writer functions' own well-understood internals. Planner's call within D-03's stated bound.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Both new tests (build + `go test`) | ✓ | go1.25.12 (matches `go.mod`'s `toolchain go1.25.12`) | — |
| cgo + C compiler | `-race` builds (both the audit test's own compilation and the e2e's `go build -race` subprocess step) | ✓ | gcc 13.3.0 (Ubuntu 13.3.0-6ubuntu2~24.04.1), `CGO_ENABLED=1` | — |
| `golang.org/x/tools` v0.44.0 | SEC-01 audit | ✓ | Already resolved, hashed in `go.sum` | — |
| golangci-lint | CI lint job (new test files must stay lint-clean; `only-new-issues: true` enforces this for genuinely new lines) | ✓ (local: v2.1.0; **CI pins v2.12.2** — a version mismatch exists between this dev environment and CI, not introduced by this phase, informational only) | local v2.1.0 / CI v2.12.2 | CI's pinned version is authoritative; local mismatch doesn't block anything |
| Kubernetes cluster / kubeconfig | **Not required** for either new test — D-06/D-07's bypass (`server`+`tls:false`) is specifically designed to avoid this | N/A (deliberately unused) | — | — |
| `kubectl` / `docker` | Not required by this phase's tests | present in this environment but irrelevant here | — | — |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** none — everything this phase's tests need is already present and was empirically exercised successfully in this session.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `github.com/stretchr/testify` (assert/require) — already the project-wide convention, no new framework |
| Config file | none — no `.golangci.yml` change needed (lint config already enables `govet`/`errcheck`/`staticcheck`/`unused`/`errorlint`; no `gosec`, so `exec.Command(binPath, "mcp")` in the e2e test triggers no lint finding) |
| Quick run command | `rtk proxy go test ./cmd/cpg/... -run TestMCPAudit -v` (audit only, ~13-56s depending on `-race`) / `rtk proxy go test ./cmd/cpg/... -run TestMCPE2E -v` (e2e only, ~1-3s per sub-test plus the one-time binary build, ~6s warm-cache) |
| Full suite command | `rtk proxy go test ./... -count=1 -race` (per the sandbox-blocked-`make-test` constraint already recorded in project memory) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SEC-01 | No K8s write verb / no fs write outside tmpdir reachable from `runMCPServer`, re-runnable | unit (static analysis) | `go test ./cmd/cpg/... -run TestMCPAudit -v` | ❌ Wave 0 — new file `mcp_audit_test.go` |
| SRV-01 | `initialize` handshake succeeds, all 8 tools listed with correct schemas | integration (real subprocess) | `go test ./cmd/cpg/... -run TestMCPE2E -v` (folded into the graceful e2e test per D-11) | ❌ Wave 0 — new file `mcp_e2e_test.go` |
| SRV-04 | Full lifecycle under `-race` + ungraceful-disconnect variant | integration (real subprocess, `-race`) | `go test ./cmd/cpg/... -run TestMCPE2E -race -v` | ❌ Wave 0 — same new file, two test functions |
| SEC-03 | README documents harness env / secrets posture / exec-credential caveat | manual-only (documentation) | n/a — reviewed by the plan-checker/verifier reading the rendered README, no automated test possible for prose content | ❌ Wave 0 — README.md edit |

### Sampling Rate
- **Per task commit:** `go test ./cmd/cpg/... -run '<new test name>' -v` (fast, targeted)
- **Per wave merge:** `go test ./cmd/cpg/... -race -count=1` (this package only — the audit test's ~45-76s `-race` cost is localized to `cmd/cpg`, not the whole module)
- **Phase gate:** `go test ./... -count=1 -race` full suite green before `/gsd-verify-work`, matching CI's exact invocation

### Wave 0 Gaps
- [ ] `cmd/cpg/mcp_audit_test.go` — covers SEC-01 (new file, no existing scaffolding)
- [ ] `cmd/cpg/mcp_e2e_test.go` — covers SRV-01 + SRV-04 (new file; may reuse `decodeStructured`/`requiredFields` helpers already defined in `mcp_session_test.go` within the same package — no new shared-fixture file needed)
- [ ] No new framework install needed — `testing` + `testify` already present and already imported throughout `cmd/cpg`

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture, Design and Threat Modeling | yes | Composition-root pattern (already decided Phase 16, this phase verifies it structurally) — the MCP command's tool table is the sole trust boundary between "LLM-reachable" and "not LLM-reachable" code |
| V4 Access Control | yes | This phase's entire purpose: proving the "readonly" access-control boundary is structural (no reachable K8s write verb, no reachable fs write outside the session tmpdir), not merely a `readOnlyHint` annotation (Pitfall 7 from prior research: annotations are advisory, not enforcement) |
| V5 Input Validation | yes (verifies, doesn't add) | Already implemented in Phases 17-18: `evidence.ValidatePolicyRef` (path-traversal guard on `namespace`/`workload`), `parseOptionalDuration`'s bounds checking. This phase's e2e exercises these paths for real over the wire but adds no new validation logic |
| V7 Error Handling and Logging | yes (verifies) | stdout/stderr separation (SRV-02/SRV-03, Phase 16) — this phase's e2e is the first real-subprocess proof that zero non-JSON-RPC bytes reach stdout across a full session, including error paths |
| V12 Files and Resources | yes | Session tmpdir confinement (`os.MkdirTemp`, `DeriveSessionPaths`) — SEC-01's fs-write property directly audits this boundary |
| V14 Configuration | yes (documentation) | SEC-03's README section documents the required MCP host `env` block (`KUBECONFIG`/`PATH`/`TMPDIR`) — a configuration-security concern (silent fallback to the wrong cluster/credentials if env isn't forwarded, per prior research Pitfall 8) |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Confused deputy / excessive tool permissions (a future MCP tool addition accidentally reaching a write path) | Elevation of Privilege | SEC-01's re-runnable structural audit — this is the exact mitigation this phase builds; every future `registerXTools` call is automatically swept into the same check with zero test edits (D-04) |
| Trusting `readOnlyHint`/other tool annotations as the actual enforcement mechanism | Elevation of Privilege | Already documented as a non-mitigation in prior research (Pitfall 7) — annotations are advisory per the MCP spec's own tool-annotations documentation; SEC-01 enforces at the composition-root/reachability level instead |
| Path traversal via `namespace`/`workload` MCP tool arguments | Tampering | Already implemented (`evidence.ValidatePolicyRef`, Phase 18) — this phase's e2e exercises it live but doesn't add new logic; the audit's fs-write allowlist independently confirms the writers involved are the atomic, tmpdir-rooted ones |
| Information disclosure via LLM context (HTTP paths/labels reaching the LLM, no redaction in v1.5) | Information Disclosure | Explicit, documented accepted risk — SEC-03's secrets-posture paragraph (D-13 item 4) is the mitigation for THIS phase: informing the operator, not technically redacting (REDACT-01 is v2) |
| Supply-chain risk from the new test-only `golang.org/x/tools` dependency | Tampering | Already a hashed, verified transitive dependency before this phase (see Package Legitimacy Audit) — promoting to direct usage adds no new trust surface |
| Silent kubeconfig/env misconfiguration under an MCP host (host doesn't forward `KUBECONFIG`/`PATH`/`TMPDIR`) | Denial of Service / Information Disclosure (wrong-cluster session) | SEC-03's harness-configuration paragraph (D-13 item 3) — documentation mitigation; the bounded `start_session` timeout (already implemented, Phase 17) turns a resulting hang into an actionable error rather than a silent wrong-cluster capture |

## Sources

### Primary (HIGH confidence — read directly or executed in this session)
- `golang.org/x/tools@v0.44.0` — local module cache, read directly: `go/callgraph/rta/rta.go` (Analyze's doc comment: root-at-main+init contract), `cmd/callgraph/main.go` (the reference implementation this design mirrors), `go/callgraph/cha/`, `go/packages/` — all executed against this repository in this session
- `github.com/modelcontextprotocol/go-sdk@v1.6.1` — local module cache, read directly: `mcp/cmd.go` (`CommandTransport`, `pipeRWC.Close()` cascade), `mcp/transport.go` (`ioConn`, `IOTransport`, `StdioTransport`, newline-delimited-JSON framing, `nopCloserWriter`), `mcp/server.go` (`Server.Run`'s exact ctx.Done()-vs-ssClosed race and return-value semantics), `mcp/client.go` (`ClientSession.Close`), `internal/jsonrpc2/conn.go` (`Connection.wait()`'s `io.EOF`-is-not-an-error logic) — all cross-checked against `mcp/cmd_test.go`'s own test suite
- `github.com/cilium/cilium@v1.19.4/api/v1/observer` — local module cache, read directly: `observer_grpc.pb.go` (`ObserverServer` interface, `RegisterObserverServer`, `UnimplementedObserverServer`), `observer.pb.go` (`GetFlowsResponse` oneof shape)
- This repository's own source, read directly this session: `cmd/cpg/mcp.go`, `mcp_tools.go`, `mcp_query.go`, `mcp_harness_test.go`, `mcp_session_test.go`, `mcp_query_tools_test.go`, `mcp_test.go`, `pkg/session/manager.go`, `session.go`, `paths.go`, `pipeline_config.go`, `manager_test.go`, `pkg/hubble/client.go`, `pkg/dropclass/classifier.go`, `pkg/labels/selector.go`, `pkg/policy/testdata/ingress_flow.go`, `README.md` (full heading structure), `.github/workflows/ci.yml`, `.golangci.yml`, `go.mod`
- **This session's own empirical test runs** (probe files written, executed, then deleted; `git status` confirmed clean afterward): SSA/callgraph feasibility probe (CHA vs RTA timing + reachable-set sizes), fs-write name-enumeration probe (exposed the CHA/RTA noise problem), direct-call-scan probe (validated the 5-caller/0-k8s-hit design), full graceful e2e probe (real `-race` binary + real fake relay, passed), full ungraceful-disconnect e2e probe (initially raced, fixed with relay-synchronization, then passed)

### Secondary (MEDIUM confidence)
- `.planning/research/PITFALLS.md` (Pitfalls 1, 7, 8, 10 — cited per CONTEXT.md's canonical refs) — prior-phase research, not re-verified line-by-line this session but consistent with everything independently confirmed here
- `.planning/phases/16-18` CONTEXT.md files — prior locked decisions, treated as authoritative per the phase's "pre-decided upstream, do not re-litigate" framing

### Tertiary (LOW confidence)
- None — every non-trivial claim in this document was either read from source in this exact repository/dependency tree, or executed and observed directly in this session.

## Metadata

**Confidence breakdown:**
- SEC-01 audit design: HIGH — validated by running the actual proposed design against this exact codebase and confirming it produces the exact predicted allowlist
- SRV-04/SRV-01 e2e mechanics: HIGH — validated by running the actual graceful and ungraceful lifecycles against a real `-race`-built subprocess and a real fake relay
- SEC-03 README placement: MEDIUM — the technical content (env block, secrets posture, exec-credential caveat) is fully specified by CONTEXT.md's D-13; the exact insertion point is a reasoned judgment call (A1 in Assumptions Log), not independently verifiable
- Package legitimacy (`golang.org/x/tools`): MEDIUM-HIGH — slopcheck itself unavailable/inapplicable, but independently cross-verified via `go list -m`, pre-existing `go.sum` presence, and successful real execution

**Research date:** 2026-07-21
**Valid until:** Stable for the remainder of this milestone (v1.5) — the empirical findings are tied to this exact `go.mod`/dependency-tree snapshot; a significant Cilium/client-go version bump could change the specific noise functions CHA/RTA surface (though the Pattern 1 *design* — restrict to cpg-owned direct calls — would remain correct regardless). Recommend re-validating the exact allowlist contents (not the design) if `github.com/cilium/cilium` or `k8s.io/client-go` are bumped in a future milestone.
