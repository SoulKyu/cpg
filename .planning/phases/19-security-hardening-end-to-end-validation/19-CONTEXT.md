# Phase 19: Security Hardening & End-to-End Validation - Context

**Gathered:** 2026-07-21
**Status:** Ready for planning

<domain>
## Phase Boundary

The readonly guarantee is structurally proven and documented, and the complete session lifecycle is verified end-to-end under race detection. Four deliverables, zero new product features: (1) SEC-01 structural readonly audit test over the MCP composition root, re-runnable for every future tool; (2) SRV-04 full-stdio e2e integration test (subprocess) driving `initialize → start_session → get_status → each query tool → stop_session → exit` under `-race`, plus an ungraceful-disconnect variant; (3) SRV-01 initialize-handshake verification listing all 8 tools with correct schemas (folded into the e2e); (4) SEC-03 README MCP section (harness `env` config, secrets posture, exec-credential caveat). Requirements: SRV-01, SRV-04, SEC-01, SEC-03. This is the closing phase of v1.5 — no pipeline change, no new tool, no new package.

**Pre-decided upstream (do not re-litigate):** readonly is structural, decided Phase 16, verified here (16-CONTEXT "Structural readonly rule"); `server` arg bypasses port-forward precisely so this phase's e2e can run against a local fake gRPC server without kubeconfig (17-CONTEXT D-07); stopped session retained and queryable (17-CONTEXT D-01/D-02); `session.DeriveSessionPaths` is the single source of truth for tmpdir layout (Phase 18 WR-04); stdout-purity byte-level assertion on real stdio belongs to THIS phase, not Phase 16 (16-CONTEXT D-06); no mutating tool, no multi-session, no redaction (REQUIREMENTS Out of Scope / v2).

</domain>

<decisions>
## Implementation Decisions

### SEC-01 — structural readonly audit mechanism
- **D-01:** The audit is a **static reachability test**: build the SSA callgraph (`golang.org/x/tools` — test-only dependency) rooted at `runMCPServer` (which transitively covers `registerSessionTools` + `registerQueryTools` and every handler), walk every reachable function, and assert two properties. Function-level reachability is required — `cmd/cpg` is one `package main` that also contains the CLI (`generate.go` writes to user-chosen output dirs), so a package/import-level scan cannot distinguish MCP-reachable code from CLI-only code without a mushy allowlist that would let a future MCP tool call `runGenerate` unnoticed.
- **D-02:** Property 1 — **no K8s write verb reachable**: no reachable function calls a client-go/dynamic-client write method (`Create`/`Update`/`Patch`/`Delete`/`Apply` selectors on K8s client types). Baseline verified during scouting: zero such verbs exist anywhere in the repo today (`pkg/k8s` is read + port-forward only) — the audit pins that fact against regression.
- **D-03:** Property 2 — **no filesystem write outside the session tmpdir**: every reachable call to a write-capable fs function (`os.WriteFile`, `os.Create`, `os.OpenFile` with write flags, `os.Mkdir*`, `os.MkdirTemp`, `os.Rename`, `os.Remove*`) must be in an **explicit allowlist of function symbols** — the atomic writers (`pkg/output`, `pkg/evidence`, `pkg/hubble` health writer) and `pkg/session` Manager tmpdir lifecycle ops — each documented in the test with WHY it is safe (paths rooted in `os.MkdirTemp`/`DeriveSessionPaths`). Any new unallowlisted write fails the test.
- **D-04:** Re-runnability contract: the callgraph is computed at test time from the composition root — a future `registerXTools` call or new tool handler is automatically swept in with zero test edits. The failure message names the offending function AND its call path from the root, so a future author immediately sees what leaked. Callgraph algorithm choice (RTA recommended; CHA acceptable fallback if RTA fights `package main`) = planner/researcher, but soundness direction must be over-approximation (never miss reachable code).

### SRV-04 — e2e harness shape
- **D-05:** Real subprocess, real stdio: the test builds the actual `cpg` binary **with `-race`** (once, e.g. TestMain or shared helper, into a test temp dir) and drives `cpg mcp` over its real stdin/stdout pipes. This — not the in-memory harness — is where 16-CONTEXT D-06's deferred assertion lands: every stdout byte parses as a JSON-RPC frame.
- **D-06:** No cluster, no kubeconfig: the test starts an **in-process fake Hubble relay** — a `grpc.Server` implementing `observerpb.ObserverServer` on `127.0.0.1:0` — and passes its address via `start_session{server: addr, tls: false}` (the D-07 bypass built for exactly this). The fake serves `GetFlows` with fixture dropped flows: at least one policy-actionable drop (POLICY_DENIED with workload labels → real policy + evidence files appear in the tmpdir) and one infra-class drop (→ non-empty cluster-health aggregates), then holds the stream open until context cancel so the session stays `capturing` until stopped. It must also implement whatever connectivity-check RPC `pkg/hubble.Client` performs at connect (client.go does an explicit check because `grpc.NewClient` dials lazily — researcher confirms which RPC).
- **D-07:** Graceful lifecycle assertions, in order: `initialize` handshake → `tools/list` (D-10) → `start_session` → `get_status` (state=capturing, `tmp_dir` captured for later) → each of the 5 query tools mid-capture (samples live; aggregates/health return the `available_after_stop` marker) → `stop_session` (final summary, counters > 0) → `get_cluster_health` post-stop (full report, remediation URLs) → close stdin → process exits 0. The whole test file runs under the suite's `-race` like the other 607 tests.
- **D-08:** Ungraceful-disconnect variant: same setup through `get_status` (tmpdir known), then **abruptly close stdin with no stop_session**. Assert within a bounded deadline (test-side cap ~10s, comfortably above the SESS-05 per-step deadlines): the process exits on its own, the session tmpdir is removed, and the fake relay's `GetFlows` stream context gets cancelled (proving the session-cleanup fan-out ran).
- **D-09:** Honest mapping of the roadmap's "port-forward … cleaned up" wording: the e2e deliberately bypasses port-forward (no cluster — that IS the D-07 design). The fan-out step that closes the port-forward is the same `Shutdown()` path whose per-step bounded cleanup is already unit-tested in `pkg/session` (SESS-05, Phase 17); the e2e proves the fan-out fires on transport death (stream cancel + tmpdir removal + bounded exit). State this mapping explicitly in the e2e test comment and in VERIFICATION so the verifier does not flag a phantom gap.

### SRV-01 — handshake/schema assertion depth
- **D-10:** Structural invariants, **no golden-file schema snapshots** (brittle against SDK serialization details, zero added safety). Assert: `initialize` succeeds with serverInfo name `cpg` + version; `tools/list` returns **exactly** the 8-name set {`start_session`, `get_status`, `stop_session`, `list_dropped_flows`, `list_policies`, `get_policy`, `get_evidence`, `get_cluster_health`}; every tool has a non-empty description and an inputSchema; every data-returning tool has an outputSchema; annotations match what the registration code declares (query tools `readOnlyHint: true` per 18 D-16 — read the actual registrations at plan time for the session tools' truth, don't guess); the dropclass enum (`policy|infra|transient|noise|unknown`) appears in the schema that carries it — `list_dropped_flows` only (`get_evidence`'s filter surface deliberately excludes dropclass per 18-CONTEXT D-10; the existing `TestMCPQueryToolsQRY05Contract` asserts exactly this scope). *(Corrected during plan verification — an earlier draft wrongly listed `get_evidence` here.)*
- **D-11:** SRV-01 is satisfied inside the e2e stdio test (the handshake over real stdio IS the requirement) — no separate harness registration doc-test. The existing in-memory all-8-tools test (Phase 18) stays as the fast-feedback layer; the e2e adds the real-transport proof.

### SEC-03 — README MCP section
- **D-12:** One new top-level section `## MCP Server (cpg mcp)` in README.md (README currently has zero MCP mentions — written from scratch), placed after the existing command documentation, in the README's existing voice (English, practical, example-first).
- **D-13:** Mandatory content blocks, in order: (1) what it is — readonly MCP server over stdio, single capture session, LLM reads dropped flows/policies/evidence/health; (2) the 8-tool table with one-line descriptions; (3) **harness configuration** — a concrete `mcpServers` JSON example with an explicit `env` block (`KUBECONFIG`, `PATH`, `TMPDIR`) and the WHY: MCP hosts spawn servers without your shell env — client-go needs `KUBECONFIG`, exec credential plugins need `PATH`, the session tmpdir honors `TMPDIR`; (4) **secrets posture** — with `--l7`-enabled sessions, HTTP paths/methods, FQDNs, and workload labels reach the LLM context via tool results; `Authorization`/`Cookie`/headers are NEVER captured (v1.2 anti-feature, structural); no redaction pass in v1.5 (REDACT-01 is v2) — operators on sensitive clusters should know what crosses the boundary; (5) **exec-credential-plugin caveat** — kubeconfigs using `exec` auth (aws eks get-token, gke-gcloud-auth-plugin, azure kubelogin) run non-interactively under an MCP host: an interactive login prompt hangs or fails `start_session`; verify headless auth works (`kubectl get pods` from a non-interactive shell) or use a static kubeconfig; the bounded `start_session` timeout turns this into an actionable error, not a silent hang; (6) session model note — one session at a time, stopped session retained and queryable until the next `start_session` or server exit.
- **D-14:** Scope guard: README section documents what EXISTS. No aspirational features, no HTTP transport, no multi-session. Cross-link the two-step L7 workflow docs where the secrets posture mentions `--l7`.

### Claude's Discretion
- Exact e2e client plumbing: go-sdk client over a command/pipe transport vs hand-rolled JSON-RPC over `exec.Cmd` pipes — whatever the SDK v1.6.1 actually offers (researcher confirms `CommandTransport` availability); the ungraceful variant likely needs hand-managed pipes regardless (must close stdin while watching the process).
- Audit test file/name layout in `cmd/cpg` (e.g. `mcp_audit_test.go`, `mcp_e2e_test.go`); `testing.Short()` guard on the e2e is planner's call.
- Fake relay fixture flow shapes (mirror `pkg/session/manager_test.go`'s fixtures — they're package-private, so `cmd/cpg` builds its own small ones).
- Exact bounded-deadline constants in the ungraceful test (must exceed SESS-05 step deadlines with margin).
- README prose and table formatting within D-13's block list.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` §MCP Server Core (SRV-01, SRV-04) + §Security & Hardening (SEC-01, SEC-03) — exact wording; §Out of Scope (no apply tool, no multi-session, no resources); §v2 (REDACT-01 deferred — shapes the SEC-03 secrets paragraph)
- `.planning/ROADMAP.md` — Phase 19 goal + 4 success criteria (the verification targets)

### Milestone research (v1.5 MCP)
- `.planning/research/PITFALLS.md` — Pitfall 1 (stdout is the wire — the e2e's byte-level frame assertion), Pitfall 7 (readonly is structural, not a hint — SEC-01's rationale), Pitfall 8 (kubeconfig bounded-timeout + env — feeds SEC-03), Pitfall 10 (protocol-level tests)
- `.planning/research/SUMMARY.md` — phase mapping (research phases 5+6 merged into this phase), four tensions recap
- `.planning/research/ARCHITECTURE.md` — composition-root pattern the audit roots at; no `pkg/mcp` naming rule

### Prior phase contracts (the decisions this phase verifies)
- `.planning/phases/16-mcp-server-foundation-write-safety/16-CONTEXT.md` — D-01 global stdout backstop, D-06 (byte-parse assertion deferred to THIS phase's real-stdio e2e), structural-readonly rule decided
- `.planning/phases/17-session-lifecycle/17-CONTEXT.md` — D-07 `server` bypass (the e2e's cluster-free lever), D-01..D-04 retention/purge semantics the e2e observes, SESS-05 bounded cleanup the ungraceful variant exercises
- `.planning/phases/18-query-tools/18-CONTEXT.md` — D-02 `available_after_stop` marker mid-capture, D-14 dropclass enum in schemas, D-16 annotations, QRY-05 contract the schema assertions pin
- `.planning/phases/18-query-tools/deferred-items.md` — pre-existing `pkg/session` shared-`/tmp` test flake (known noise — do NOT chase it as an e2e bug)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `cmd/cpg/mcp.go:79` `runMCPServer(ctx, transport)` — the SEC-01 callgraph root; already transport-injectable
- `cmd/cpg/mcp.go:40` `newMCPCmd` — the real stdio path the e2e subprocess exercises (IOTransport pre-swap capture + `os.Stdout = os.Stderr` backstop)
- `pkg/session/paths.go:47` `DeriveSessionPaths(tmpDir)` — layout single source of truth; audit allowlist rationale + e2e artifact-path assertions both use it
- `pkg/session/session.go:163` `StatusResult.TmpDir` (`tmp_dir`) — how the e2e learns the tmpdir to assert post-disconnect removal
- `pkg/session/session.go:134` `StartArgs.Server`/`TLS`/`Timeout` — the fake-relay injection surface (D-07)
- `pkg/hubble/client.go:54-66` — `grpc.NewClient` + insecure creds when TLS off + explicit connectivity check (fake relay must satisfy it)
- `observerpb "github.com/cilium/cilium/api/v1/observer"` (already imported by `pkg/hubble`) — `RegisterObserverServer` + `UnimplementedObserverServer` for the fake relay; no new go.mod product dependency
- `cmd/cpg/mcp_harness_test.go` + `mcp_query_tools_test.go` — the in-memory layer that stays as fast feedback (D-11); fixture patterns to mirror
- `pkg/session/manager_test.go:26` `closedFlowSource` + fixtures — flow-shape reference for the fake relay's `GetFlows` payloads

### Established Patterns
- All 607 tests run `-race`; the e2e follows (test process AND the subprocess binary built with `-race`)
- Zero K8s write verbs exist in the repo today (scout-verified: no `Create(`/`Update(`/`Delete(`/`Patch(` in `pkg/k8s` or `cmd/cpg`) — SEC-01 pins this baseline
- All three writers atomic temp+rename; `os.Rename`/`os.CreateTemp` calls are exactly the symbols the audit allowlists
- README has no MCP section (scout-verified: zero `mcp` mentions) — SEC-03 is a fresh section, not an edit

### Integration Points
- `golang.org/x/tools` (go/packages + go/ssa + go/callgraph) — new **test-only** dependency for the audit; go.mod impact is dev-scope, flag it in the plan
- The e2e builds the binary via `go build -race` at test time (TestMain) — requires the Go toolchain on PATH at test time (true locally + CI); sandbox note: run tests via `rtk proxy go test ./... -count=1 -race`, `make test` is sandbox-blocked
- CI (`.github/workflows`) already runs build + race tests — the e2e joins the existing race job; watch its wall-clock (subprocess build adds seconds, keep the e2e lean)

</code_context>

<specifics>
## Specific Ideas

- The e2e's stdout-purity assertion is the redemption of 16-CONTEXT D-06's deliberate deferral: "every stdout line parses as a JSON-RPC frame" belongs HERE, on real stdio, not on the in-memory transport.
- Audit failure output must teach: name the reachable offending function + call path from `runMCPServer`, so the fix (or a justified allowlist addition) is obvious.
- README `env` example should show realistic values and state the rule in one line: "MCP hosts do not inherit your shell environment."

</specifics>

<deferred>
## Deferred Ideas

None new — REDACT-01 (secrets redaction), LIVE-01 (live counters), FLOW-01 (flow-sample writer) remain v2; lint debt (LINT-01..03) and release hardening (RELSEC-01..02) remain tracked outside this phase's requirements.

</deferred>

---

*Phase: 19-Security Hardening & End-to-End Validation*
*Context gathered: 2026-07-21*
