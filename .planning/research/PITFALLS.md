# Pitfalls Research

**Domain:** Adding a readonly MCP stdio server to an existing Go streaming CLI (cpg — Hubble flow capture + CiliumNetworkPolicy generator, cobra + zap + cilium gRPC + client-go/SPDY port-forward)
**Researched:** 2026-07-20
**Confidence:** HIGH — grounded in the official MCP spec (current draft + stable 2025-06-18, cross-checked), current official Claude Code MCP docs, vendored library source read directly (cobra, zap), and the live cpg codebase (read/grepped this session). MEDIUM/LOW flagged inline wherever a claim rests on community sources only.

## Critical Pitfalls

### Pitfall 1: stdout is the wire — and cpg already has writers aimed at it

**What goes wrong:**
Over stdio transport, `stdout` carries JSON-RPC exclusively. One stray byte that isn't a valid MCP message corrupts the stream; the client either drops the connection or fails in confusing, hard-to-diagnose ways. This is normative, not a style preference — verified identically in both the current draft and the stable 2025-06-18 MCP spec: *"The server **MUST NOT** write anything to its `stdout` that is not a valid MCP message"* and *"The server **MAY** write UTF-8 strings to its standard error (`stderr`) for logging purposes."*

**Why it happens:**
`cpg mcp` is going to reuse `pkg/hubble.RunPipelineWithSource` — code written for a CLI where stdout is a human's terminal. Grepping that code path today turns up concrete, already-existing stdout writers that a naive MCP integration will trip over on day one:

- `PipelineConfig.Stdout` (`pkg/hubble/pipeline.go`, field doc: *"Nil defaults to os.Stdout. Use bytes.Buffer in tests."*) — at the end of every `RunPipelineWithSource` call, `PrintClusterHealthSummary(stdout, ...)` prints the HEALTH-03 cluster-health summary block. If the MCP session path leaves this field nil (the default), the very first session that completes (or is stopped) writes a multi-line human-readable summary straight onto the JSON-RPC channel.
- `policyWriter.diffOut` (`pkg/hubble/writer.go`) — same nil-defaults-to-`os.Stdout` pattern, active whenever `--dry-run --dry-run-diff` renders a unified YAML diff. Relevant if any MCP "preview" tool reuses dry-run mode.
- `pkg/k8s/portforward.go` — already does the *right* thing: `portforward.New(dialer, ports, stopCh, readyCh, io.Discard, io.Discard)`. client-go's port-forwarder, when given a real writer, prints a `Forwarding from 127.0.0.1:PORT -> 4245` line per forwarded port; cpg already discards it. The risk here isn't an existing gap — it's regression: a future debugging session wiring `os.Stdout` in here "just to see the port" and forgetting to revert before merging the MCP path.
- zap (`cmd/cpg/main.go` `buildLogger`) — verified via `pkg.go.dev/go.uber.org/zap`: `NewProductionConfig()` and `NewDevelopmentConfig()` both default `OutputPaths`/`ErrorOutputPaths` to **stderr**, and `NewDevelopment()` follows the same rule. All three of cpg's existing logger-construction branches are already MCP-safe. Stays safe only as long as nobody adds an `OutputPaths` override, a `--log-file`-style flag, or points `--json` output at stdout for the `mcp` subcommand.
- cobra — verified by reading the vendored source (`spf13/cobra@v1.9.1/command.go`) directly: `Print`/`Println`/`Printf` (used for the "Error:" line and, on failure, the auto-printed usage string) route through `OutOrStderr()`, which falls back to `os.Stderr` unless `SetOut`/`SetOutput` was called. Grepping cpg's production code confirms it calls neither `SetOut`/`SetOutput`/`SetErr` nor any raw `fmt.Print*` anywhere — so cobra's default error path is stderr-safe today. Two residual risks remain: (a) this safety is incidental, enforced by nobody having added those calls yet, not by a test; (b) cobra's `--help` / `flag.ErrHelp` path explicitly targets `OutOrStdout()` (real stdout) — never trigger cobra's help rendering from inside the MCP code path.
- Transitive dependencies: grpc-go's default logger writes to stderr regardless of `GRPC_GO_LOG_SEVERITY_LEVEL`/`GRPC_GO_LOG_VERBOSITY_LEVEL`; client-go's `klog`, if triggered before `flag.Parse()` (plausible, since cpg uses cobra/pflag not stdlib `flag`), still lands its "logging before flag.Parse" warning on stderr, not stdout. Low residual risk — noted for completeness, not a live gap.

**How to avoid:**
Wire every one of these `io.Writer` seams explicitly for the `cpg mcp` path instead of relying on their nil-defaults-to-stdout fallback — `io.Discard`, a captured buffer surfaced through a query tool, or a `zap.Debug` call, depending on the field. Add `SilenceUsage`/`SilenceErrors` to the `mcp` subcommand as defense-in-depth even though it's not strictly required today. Never call `SetOut(os.Stdout)`. Back this with an automated test (see Pitfall 10) that treats "anything non-JSON-RPC reaches stdout" as a hard failure — don't rely on code review to catch it forever.

**Warning signs:**
A manual `cpg mcp` smoke test under a human eyeballing a terminal looks completely fine — the TTY masks corruption because a human is reading text, not parsing NDJSON. The failure only shows up once a real LLM harness reports "invalid JSON" or silently drops the connection after the first completed session.

**Phase to address:**
MCP server skeleton / stdio wiring — first phase, before any tool logic. The stdout-purity test should exist before the first real tool is added.

---

### Pitfall 2: blocking a tool handler on the capture pipeline

**What goes wrong:**
`RunPipeline`/`RunPipelineWithSource(ctx, cfg, source)` is the only existing entry point into the streaming pipeline, and it's built to block until `ctx.Done()` or a stream error/EOF — exactly the shape `generate`/`replay` need. Calling it directly inside a `start_session` tool handler blocks that tool call for the entire capture duration, which for a live session is intentionally open-ended.

**Why it happens — and a correction worth making explicitly:**
The common assumption is "MCP tool calls time out around 60 seconds, so anything long must be async." That number is real, but it's not universal — verified against the current official Claude Code docs (`code.claude.com/docs/en/mcp`, fetched 2026-07-20): the ~60s figure is a **per-request timer that applies to HTTP, SSE, and connector transports only** — *"Stdio and WebSocket servers have no per-request timer."* For stdio (what `cpg mcp` uses), the actual ceilings are different: `MCP_TOOL_TIMEOUT` defaults to roughly **28 hours** when unset, and a separate **idle timeout** aborts a call that sends *no response and no progress notification* for **30 minutes** on stdio servers specifically (5 minutes for HTTP/SSE/WebSocket/connector; this idle check requires Claude Code ≥v2.1.187, and only applies to stdio servers from ≥v2.1.203). So a synchronous design won't necessarily die at 60 seconds under Claude Code. It's still wrong, for three independent reasons: (a) any session run synchronously for longer than 30 minutes trips the idle timeout unless progress notifications are implemented (nontrivial, and not what PROJECT.md's `start_session`/`status`/`stop_session` design calls for); (b) even comfortably inside 30 minutes, a fully-blocked tool call means the LLM cannot check status, cannot read partial results, and cannot stop early — a bad architecture independent of the exact numeric ceiling; (c) not every MCP host is Claude Code, and other stdio hosts' timeout behavior is far less documented — designing as if a synchronous call could be killed at any moment is the only safe default.

Separately — and this is the part that actually breaks a synchronous design even inside the timeout budget — the `context.Context` passed into a tool handler is request-scoped in every mainstream MCP SDK, Go included, and is typically cancelled once the handler returns. Spawning `go RunPipelineWithSource(ctx, cfg, source)` using that handler's `ctx` directly means the pipeline goroutine gets cancelled the instant the tool call returns, which defeats "background" before it starts.

**How to avoid:**
`start_session` spawns the pipeline in a goroutine against a **detached** context — `context.WithoutCancel(context.Background())` (stdlib, Go 1.21+; cpg is on 1.25.1) wrapped in its own `context.WithCancel` so `stop_session` has something to call — stores the cancel func in a session registry keyed by session ID, and returns the session ID + tmpdir path immediately. `status`/query tools read tmpdir artifacts and/or an in-memory `SessionStats` snapshot. `stop_session` calls the stored `cancel()`, then waits — bounded — for the goroutine to actually exit before returning, so the caller knows port-forward and tmpdir cleanup have genuinely happened, not just been requested.

**Warning signs:**
`start_session` taking more than a second or two in testing (it should be near-instant — connect, dial, spawn, return); any test that has to wait out a whole capture window before it gets a session ID back.

**Phase to address:**
Session lifecycle phase (start/status/stop tools).

---

### Pitfall 3: client/harness death orphans the session

**What goes wrong:**
The MCP client process crashes, the user force-quits, or the harness restarts without cleanly closing stdin. This is not hypothetical — it's documented across the MCP ecosystem, including against Claude Code itself: [anthropics/claude-code#22612](https://github.com/anthropics/claude-code/issues/22612) ("Orphaned MCP server processes not cleaned up when sessions end") and [#39170](https://github.com/anthropics/claude-code/issues/39170) ("MCP plugin bun processes orphaned on unclean exit, peg CPU at 100% each"). For cpg specifically, an orphaned `cpg mcp` process means: the background pipeline goroutine keeps streaming Hubble flows and writing into the session tmpdir indefinitely; the SPDY port-forward to hubble-relay stays open (client-go's SPDY implementation has a documented history of goroutine/stream leaks even on the *correct* shutdown path — [kubernetes/kubernetes#105830](https://github.com/kubernetes/kubernetes/issues/105830), [#96339](https://github.com/kubernetes/kubernetes/issues/96339)); and the ephemeral session tmpdir is never removed, silently consuming `$TMPDIR` across repeated sessions.

**Why it happens:**
The spec's shutdown contract is unambiguous and identical in both the current draft and the stable 2025-06-18 spec: *"Servers **SHOULD** exit promptly when their standard input is closed or reads return end-of-file. This is the primary graceful-shutdown signal and the only portable one."* Whichever Go MCP SDK is chosen correctly detects that and stops its own read/write loop — but that only stops the *SDK's transport loop*. It has no knowledge of cpg's session registry, port-forward `stopCh`s, or tmpdirs; those need an explicit hook. A concrete illustration of how easy this class of bug is to ship even inside an SDK's own contract: [modelcontextprotocol/go-sdk#224](https://github.com/modelcontextprotocol/go-sdk/issues/224), *"Server.Run does not stop when the context is cancelled"* — client disconnect worked correctly, caller-initiated context cancellation did not, until fixed via PR #234 (a v0.3.0 release blocker). Pin above the fix, or explicitly verify the shutdown behavior of whichever SDK/version is chosen.

**How to avoid:**
Treat "the transport's `Run()`/`Serve()` returned" (for any reason — clean stdin EOF, transport error, or context cancellation) as the single root shutdown trigger, and fan it out explicitly: walk the live session registry and, per session, cancel its context, close its port-forward `stopCh`, and `os.RemoveAll` its tmpdir — each with its own bounded deadline so one wedged cleanup can't block process exit forever (log and move on past the deadline). Don't rely solely on a top-of-`main()` `defer` if sessions live in a mutex-guarded map; make shutdown a real function that both the SDK's return path and a signal handler call. On Linux, `golang.org/x/sys/unix.Prctl(unix.PR_SET_PDEATHSIG, ...)` is a reasonable defense-in-depth addition, but it's Linux-only and not a substitute for honoring stdin EOF — the spec calls that "the only portable" mechanism for a reason.

**Warning signs:**
`ps aux | grep cpg` still shows a live `cpg mcp` process after killing the harness; `$TMPDIR` accumulating `cpg-session-*` directories across repeated dev-loop restarts; hubble-relay's connection count creeping upward over a dev session that restarts the harness repeatedly.

**Phase to address:**
Session lifecycle phase (shutdown path) — verify with an integration test that kills the transport mid-session and asserts the port-forward and tmpdir are both gone within a bounded deadline.

---

### Pitfall 4: unbounded query results blow the LLM's context window

**What goes wrong:**
A "list dropped flows" or "list generated policies" tool dumps everything the session has accumulated. Verified against the current official Claude Code docs: Claude Code warns above **10,000 tokens** of MCP tool output and hard-caps at **25,000 tokens by default** (`MAX_MCP_OUTPUT_TOKENS`, configurable); a tool can declare its own limit via the `anthropic/maxResultSizeChars` annotation, which applies independently of the environment variable for text content.

**Why it happens:**
cpg's existing FIFO caps (`--evidence-samples`, `--evidence-sessions`) bound the *on-disk evidence file size* — a different budget from what a single MCP tool call returns. A session spanning `--all-namespaces` for even a modest window can realistically accumulate hundreds of generated policies and thousands of evidence samples across cpg's own 76-value drop-reason taxonomy; dumping that wholesale into one tool result either gets silently truncated by the client (worst case: truncated mid-JSON) or blows the conversation's usable context.

**How to avoid:**
Split every "many-of-X" query tool into **list** (cheap: names/IDs/counts/timestamps only, paginated, default page size 10–20, always return `has_more` and `total_count`) and **get** (targeted: full YAML/evidence body for one named resource). Never ship a "give me everything" tool. Use `anthropic/maxResultSizeChars` on tools known to return large bodies (e.g., a single rule's full evidence with many samples).

**Warning signs:**
A query tool's response size scales with session duration or namespace count instead of being bounded by an explicit page-size parameter.

**Phase to address:**
Query tools phase — build pagination into the first version of each list tool, not as a retrofit after a real session produces an oversized response.

---

### Pitfall 5: session-state races between the writing pipeline and reading tools

**What goes wrong:**
An MCP query tool reads a policy YAML file from the session tmpdir at the same moment the background pipeline goroutine is writing/updating it, and observes a truncated or zero-length file.

**Why it happens — verified via source read, and it's a real, pre-existing asymmetry:**
Two of cpg's three artifact writers already use the safe pattern: `pkg/evidence/writer.go` and `pkg/hubble/health_writer.go` both write to a temp file, then `os.Rename` into place — atomic on the same filesystem, so a concurrent reader always sees either the complete old file or the complete new one, never a partial write. The third — `pkg/output/writer.go`, the CNP policy YAML writer, which MCP query tools will read most often — uses a direct `os.WriteFile(path, data, 0644)`: open, truncate, write, close, with no atomicity guarantee. This isn't a latent MCP-specific bug; it's an existing gap that has simply never mattered before, because `generate`/`replay` are single-consumer, run-to-completion CLI invocations — nothing has ever polled the output directory *while* it was being written. MCP query tools are the first concurrent reader this code will ever have.

**How to avoid:**
Bring `pkg/output/writer.go` in line with the temp+rename pattern already established (twice) by the evidence and health writers, before wiring any MCP query tool to read from it. This is a small, mechanical, low-risk fix that removes an entire class of flaky-read bugs and is internally consistent with cpg's own prior art.

**Warning signs:**
Intermittent YAML parse errors from a "list policies"/"get policy" tool that don't reproduce on retry (the classic torn-read signature); failures correlating with pipeline flush activity rather than any particular policy's content.

**Phase to address:**
Can be fixed standalone, ahead of the MCP milestone, since it's a legitimate small fix to existing v1.0 behavior on its own merits. At the latest, must land in the query tools phase before any query tool reads policy YAML from an active session — verify with a `-race` test that polls while a writer goroutine actively appends.

---

### Pitfall 6: tool schemas that fight cpg's own data model

**What goes wrong:**
Two concrete shapes already present in cpg's types will misuse an LLM if exposed to a tool schema naively.

**Why it happens:**

1. `pkg/dropclass` has two enum-shaped types at very different scales. `DropClass` is a small, stable taxonomy — `DropClassInfra`, `DropClassTransient`, `DropClassNoise`, `DropClassPolicy`, plus an `Unknown` fallback (~5 values) — an ideal JSON Schema `enum`. `DropReason` (`flowpb.DropReason_name`) is the raw, 76-value, Cilium-version-dependent protobuf enum (PROJECT.md tracks this precisely via a `ClassifierVersion` semver, because the taxonomy shifts across Cilium releases). Baking all 76 raw values into a tool's schema as an `enum` bloats every tool call's context and silently goes stale on a Cilium upgrade — a hardcoded enum either rejects newly valid values or accepts removed ones. Expose `DropClass` as the schema-level enum for filtering; treat raw `DropReason` names as documented free text with a few examples, or serve them from a small dedicated "list drop reasons observed this session" tool instead of baking them into a schema.
2. cpg's existing `cpg explain <NS/workload | policy.yaml>` CLI command has a union input shape. Verified against the current official Claude Code docs: *"Some MCP servers declare a tool's input schema as a JSON Schema union, with `anyOf`, `oneOf`, or `allOf` at the top level of the schema. The Claude API doesn't accept those keywords at the schema root."* Depending on the Claude Code version, a root-level `oneOf` either gets the whole tool skipped, or gets its branches merged with `required` demoted from schema enforcement into description prose — meaning the LLM can send an invalid combination and the schema won't stop it. Don't mirror the CLI's `<A | B>` ergonomic as a root-level schema union in an "explain" MCP tool: either split into two tools (`explain_workload(namespace, workload)` / `explain_policy(policy_ref)`), or keep one tool with all params optional-but-documented and validate the "exactly one of" invariant in the handler body, returning a clear tool-error result rather than relying on schema validation.

General guidance beyond these two concrete cases (WebSearch-corroborated, MEDIUM confidence, standard MCP practice): keep schemas flat — deep nesting increases token cost and LLM cognitive load; write descriptions as usage guidance ("call this when...", "not this when...") rather than only field documentation; keep per-tool parameter counts low, splitting into more tools rather than accumulating optional flags.

**How to avoid:**
See above — enum the small stable taxonomy, document (don't enum) the large/volatile one; avoid root-level schema unions, push "exactly one of" validation into handler logic with clear error messages.

**Warning signs:**
An LLM repeatedly guessing at drop-reason spelling/casing across turns; a schema union tool accepting a call with both a namespace/workload pair and a policy path set simultaneously without error.

**Phase to address:**
Query tools phase — review schemas before implementation. Schema mistakes are expensive to fix later: once an LLM harness has "learned" a tool's quirks across a long session, changing the contract becomes a breaking mid-session change (see Pitfall 6 recovery cost below).

---

### Pitfall 7: "readonly" is a hint, not an enforcement mechanism

**What goes wrong:**
Treating the MCP `readOnlyHint` tool annotation, or a README sentence, as the actual safety boundary.

**Why it happens:**
Verified via the MCP project's own tool-annotations documentation (corroborated across multiple independent write-ups): annotations including `readOnlyHint`/`destructiveHint`/`idempotentHint`/`openWorldHint` are explicitly advisory — *"annotations are not guaranteed to faithfully describe tool behavior, and clients must treat them as untrusted unless they come from a trusted server."* A client — or a confused LLM turn — is not required to respect `readOnlyHint: true`; at most it affects whether a host auto-approves a call without a confirmation prompt.

This is a live risk for cpg specifically, not a hypothetical one. Verified via source grep: cpg today has **zero** write verbs anywhere against the Kubernetes API — no `.Create`/`.Update`/`.Patch`/`.Delete`/`.Apply` in `pkg/k8s` or `cmd/cpg`, only `List`/`Get` plus the port-forward SubResource tunnel. Today's "readonly" is structural fact, not a policy statement. But `cpg apply` (dry-run by default, `--force` to apply) is already sitting in PROJECT.md's Planned list as a carried-over v1.3 candidate. The moment that command exists in the same binary, the MCP readonly guarantee becomes a question of "did someone remember to exclude this tool/code path from the mcp command's wiring" rather than "this binary cannot do it" — exactly the class of mistake an annotation cannot protect against, because enforcement has to be structural.

**How to avoid:**
Enforce readonly at the composition root, not with a runtime flag check inside a shared handler: the `cpg mcp` command should only ever construct/register tool handlers that call read-only functions (list/get/status/explain-style readers over the tmpdir and the K8s API). A mutating command like `apply`, if and when it ships, must not be reachable from the MCP tool table even though it lives in the same binary. Make "does this tool's handler chain reach any Kubernetes write verb, or any filesystem write outside the session tmpdir" an explicit review question for *every* new MCP tool, not a one-time audit. Still set `readOnlyHint: true` on every cpg tool — correct MCP citizenship, helps well-behaved hosts skip confirmation prompts — just don't mistake it for the control.

**Warning signs:**
Any new MCP tool handler that imports `pkg/k8s` functions beyond the existing List/Get/port-forward set, or writes to any path outside the session tmpdir.

**Phase to address:**
Security/readonly-hardening phase for the audit process, but the structural decision — which packages/functions the `mcp` command is allowed to call — should be made when the `cpg mcp` command skeleton is first laid out, not bolted on later.

---

### Pitfall 8: kubeconfig access — MCP host env stripping + interactive exec-credential plugins

**What goes wrong:**
Two distinct, both verified, failure modes around cluster auth.

**Why it happens:**

1. **Env stripping.** Verified via Claude Code's own documentation and issue tracker (e.g. [anthropics/claude-code#1254](https://github.com/anthropics/claude-code/issues/1254), [#10955](https://github.com/anthropics/claude-code/issues/10955)): *"Environment variables are per-server and not inherited from your shell... you must pass environment variables explicitly in the config block under `env`."* cpg's `LoadKubeConfig()` (`pkg/k8s/client.go`) resolves via `clientcmd`'s standard rules: `KUBECONFIG` env, then `~/.kube/config` (needs `HOME`), then in-cluster config. If the MCP host's server entry for `cpg mcp` doesn't explicitly forward `KUBECONFIG`/`HOME`, resolution silently falls through to a default that may not exist or may point at the wrong cluster. If the resolved kubeconfig's `exec:` auth provider shells out to a cloud CLI (`aws`, `gke-gcloud-auth-plugin`, `kubelogin`), that binary must also be resolvable — `PATH` needs the same explicit treatment. `$TMPDIR` needs it too, for the session tmpdir to land somewhere sane.
2. **Interactive auth hang.** cpg already registers exec-based auth providers — `pkg/k8s/client.go` blank-imports `k8s.io/client-go/plugin/pkg/client/auth` (OIDC, GCP, Azure, and by extension any `exec:`-configured provider such as `aws eks get-token`). Verified via client-go's exec-plugin source and [kubernetes/kubernetes#98451](https://github.com/kubernetes/kubernetes/issues/98451): the exec-credential flow's stdin/stdout handling is tuned for a human at a terminal — it inherits stderr directly from the parent process and checks stdout's TTY-ness to decide whether to also forward stdin (for an interactive 2FA/browser-based re-auth prompt). Under `cpg mcp`, the real stdin is the JSON-RPC channel from the harness, not a keyboard. If the user's kubeconfig needs an interactive re-auth step (expired SSO session, first device-code flow) at the moment cpg builds a client, the exec plugin can block waiting for input that will never arrive in the shape it expects — hanging whatever tool call triggered it, instead of failing fast. Note precisely what cpg's existing `clientcmd.NewNonInteractiveDeferredLoadingClientConfig` call does and doesn't buy here: it disables clientcmd's own prompt-for-missing-value flow (a different, older mechanism) — it does not control what a configured `exec:` provider decides to do on its own.

**How to avoid:**
Document, in the MCP server setup instructions, that `KUBECONFIG`, `HOME` (or an explicit `--kubeconfig`-equivalent parameter threaded through session start), `PATH`, and `TMPDIR` must be set explicitly in the host's `env` block — nothing is inherited by default. For the interactive-auth-hang risk, state pre-authenticated credentials as a precondition (e.g., "run `kubectl get pods` once in a real shell before starting an MCP session if your cluster uses SSO/exec auth") and wrap the initial `LoadKubeConfig`/client-build call inside `start_session` in its own short bounded timeout, so a hang surfaces as a clear tool error ("kubeconfig auth did not complete within Ns — re-authenticate outside the MCP session and retry") instead of hanging the tool call indefinitely.

**Warning signs:**
`start_session` succeeding on the developer's own machine but failing or hanging once wired into an actual MCP host config; auth failures with no clear indication of *which* layer failed (missing env vs. hung exec plugin vs. genuinely no cluster access).

**Phase to address:**
Security/readonly-hardening phase for the documentation; the bounded-timeout wrapper belongs in the session-start work in the session lifecycle phase, alongside Pitfall 2 — it's the same "don't let `start_session` block forever" concern with a different root cause.

---

### Pitfall 9: secrets travel differently through an LLM than through committed YAML

**What goes wrong:**
Data that's acceptably low-risk when it lands in a git-reviewed YAML file becomes materially higher-risk when it's read directly into an LLM's context and plausibly persisted in a harness's conversation logs/telemetry with no human review gate in between.

**Why it happens:**
cpg already has prior art defending against exactly this class of risk in generated policy YAML — PROJECT.md's own Key Decisions record: *"HTTP `headerMatches`/`host`/`hostExact` NEVER emitted (anti-feature) — Risk of leaking `Authorization`/`Cookie`/session tokens into committed YAML."* The evidence schema (`pkg/evidence/schema.go`, `L7Ref`) mirrors that discipline — it persists only `HTTPMethod`/`HTTPPath`/`DNSMatchName`, never headers. But `HTTPPath` itself isn't risk-free: applications routinely embed tokens/session IDs/reset codes directly in URL paths (`/reset-password/eyJhbG...`, `/api/v1/sessions/<opaque-id>`) — RE2-anchored per cpg's existing v1.2 discipline, but stored verbatim in both the generated policy and the evidence file today. That existing exposure is currently mitigated by a human review gate: generated YAML is meant to be read before a git commit/apply. An MCP query tool removes that gate — the same `HTTPPath` string flows directly into an LLM's context and is summarized/repeated by the model with nobody in the loop first. Raw Hubble flow data, surfaced via a "list dropped flows" tool, carries a related risk one layer upstream: Kubernetes labels/annotations are conventionally not supposed to hold secrets, but that convention isn't enforced by the API server, and cpg has never filtered label/annotation values because nothing has previously consumed them outside a human skimming CLI/log output.

**How to avoid:**
Treat MCP tool output as a stricter trust boundary than "will be code-reviewed before commit." For any field carrying arbitrary operator-controlled string data (HTTP path, labels, annotations), make an explicit decision rather than shipping by omission: document the residual risk clearly (e.g., "HTTP paths are shown verbatim in tool output; avoid running capture sessions against workloads with tokens embedded in URLs"), or add a best-effort redaction pass (flag high-entropy path segments) before these fields ship in a query tool. This is exactly the kind of silent-scope-creep risk cpg's own AI-feature shelving note (PROJECT.md, 2026-04-25 — hallucination risk, label-hygiene dependency) already flagged as a live category for this codebase; treat it with the same explicitness here.

**Warning signs:**
A "list flows" or "get policy evidence" tool response containing a URL path or label value that looks like a token/opaque ID, discovered only by someone reading a transcript after the fact.

**Phase to address:**
Security/readonly-hardening phase — needs an explicit ship-as-is-with-docs vs. redact decision before any query tool exposing `HTTPPath` or labels goes out, not discovered after the fact.

---

### Pitfall 10: no protocol-level test harness — regressions caught only by manual harness pokes

**What goes wrong:**
MCP tools get the same strong unit coverage cpg's readers/writers already have, but the MCP framing itself — tool registration, JSON schema validity, request/response shape, multi-call session lifecycle — only gets exercised by manually running `cpg mcp` under an actual harness and eyeballing behavior. That doesn't run in CI and doesn't catch regressions like Pitfall 1's stdout leak before they ship.

**Why it happens / how to avoid:**
Both mainstream Go MCP SDKs support exactly the kind of test cpg needs without a real subprocess. The official `modelcontextprotocol/go-sdk` exposes `NewInMemoryTransports()` — a bidirectional in-memory client/server transport pair that exercises the full JSON-RPC flow (initialize, capability negotiation, tool discovery, tool invocation) with no subprocess, no port binding, no timing sensitivity, fast enough to run hundreds of times per second. `mark3labs/mcp-go` provides equivalent in-process/test-server helpers. cpg's existing test suite already leans heavily on structured assertions over string-matching — `zaptest/observer` is used pervasively across `pkg/hubble`, `pkg/output`, and elsewhere to assert on decoded log-entry structs rather than substrings — the same philosophy applies directly here: assert on decoded JSON-RPC/tool-result structs, not raw string `Contains` checks, and keep a small golden-sequence corpus (`start_session → status → list_policies → stop_session`) run against the in-memory transport in CI.

**Warning signs:**
MCP-specific behavior (schema shape, session cleanup, pagination) verified only by hand; CI shows zero coverage under any `cmd/cpg/mcp*.go`-shaped path while the rest of `pkg/` stays at its existing high bar.

**Phase to address:**
MCP server skeleton phase — stand up the in-memory-transport harness first, and write the stdout-purity assertion (Pitfall 1) as its first test. Every subsequent phase adds to this harness instead of inventing its own manual test ritual.

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|-----------------|------------------|
| Reuse `RunPipelineWithSource` unmodified, patch `Stdout`/`diffOut` with a one-off nil-check at the MCP call site | Fast, zero changes to pipeline.go | Every future contributor touching `pipeline.go` has to remember the MCP caller depends on that writer being non-nil; easy to regress silently | Only alongside the stdout-corruption test (Pitfall 1/10) — the test enforces it, not developer memory |
| Skip pagination on `list_policies`/`list_flows` "because sessions are small in practice" | Ships a query tool sooner | First all-namespaces or long-running session produces a response the client silently truncates or that blows the context budget | Never for the initial ship — add page params with a generous default; cheap now, expensive to retrofit once a harness has learned the unpaginated shape |
| Leave `pkg/output/writer.go`'s non-atomic write as-is ("it's been fine for a CLI") | Avoids touching stable v1.0 code during a feature milestone | First "list/get policy" bug report is a torn-read race — hard to reproduce, easy to misdiagnose as an MCP bug rather than a pre-existing writer gap | Never, once query tools read that directory concurrently with an active session — fix before wiring the reader |
| Ship `cpg mcp` without explicit `SilenceUsage`/`SilenceErrors`, relying on "we don't call `fmt.Print*` today" | No code change required | A future one-line debug `fmt.Println` anywhere in a shared code path the session pipeline touches ships silently and corrupts every session until caught | Acceptable only alongside the automated stdout-purity test doing the enforcement instead of review vigilance |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|-----------------|-------------------|
| MCP host (Claude Code / stdio clients generally) | Assuming the ~60s tool-call timeout commonly cited for MCP applies to stdio | Verified: stdio has no per-request timer under Claude Code; real ceilings are `MCP_TOOL_TIMEOUT` (default ~28h) and the stdio idle timeout (30 min, no response *and* no progress notification). Design async regardless (Pitfall 2) — but don't cite the wrong number in docs or tests |
| MCP host env passing | Assuming `cpg mcp` inherits the shell's `KUBECONFIG`/`PATH`/`TMPDIR` | Claude Code (and stdio hosts generally) give the spawned server a clean/minimal env by default; document the required `env` block explicitly (Pitfall 8) |
| Hubble Relay gRPC (existing `--timeout`) | Reusing the CLI's `--timeout` flag as if it bounds session duration | Verified (`pkg/hubble/client.go`): `--timeout` only wraps the gRPC dial (`context.WithTimeout(ctx, timeout)` at connection time). Session duration must be controlled by the MCP session's own cancellable context (`stop_session`), kept separate from the dial timeout |
| client-go SPDY port-forward (existing) | Treating `PortForwardToRelay`'s returned `cleanup func()` as fire-and-forget-and-done | client-go SPDY has a documented history of goroutine/stream leaks even on the *correct* shutdown path (k8s/k8s#105830, #96339) — after calling `cleanup()`, don't immediately assume underlying goroutines are gone; don't start a fresh port-forward for a new session before the previous one's teardown has actually settled, or concurrent sessions can race for the dynamically-assigned local port |
| Kubernetes exec-credential plugins | Assuming kubeconfig auth either succeeds or fails fast | An interactive exec plugin can hang indefinitely under non-interactive stdio (Pitfall 8) — wrap the initial client-build call in its own bounded timeout |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|-----------------|
| Unbounded `list_dropped_flows`/`list_policies` responses | Tool result silently truncated by the client, or exceeds `MAX_MCP_OUTPUT_TOKENS` (25,000 by default under Claude Code) | Pagination with `has_more`/`total_count` from the first version of the tool (Pitfall 4) | Any `--all-namespaces` session, or any session left running more than a few minutes in a chatty namespace |
| Many small files in the session tmpdir (one YAML per policy, one JSON per evidence rule) | `list`/`status` tool calls do a full directory walk + stat on every poll | Cache directory-listing metadata between polls, invalidate on mtime/count change, instead of re-walking on every call | Sessions spanning hundreds of workloads under `--all-namespaces` |
| Relay-pod lookup + fresh port-forward per session start | Slower `start_session`, extra `List` load on the K8s API server per session | Fine at cpg's expected scale (interactive SRE sessions) — don't over-engineer pooling for this milestone | Only matters if something starts many short-lived sessions back-to-back, which isn't the documented v1.5 use case |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Trusting `readOnlyHint` as the enforcement mechanism | A future code change (or a confused/adversarial client) reaches a mutating path while the annotation still claims safety | Structural exclusion — the `mcp` command only ever imports/calls read-only functions (Pitfall 7) |
| `cpg apply` (Planned/v1.3-carried) sharing any code path or binary wiring with `cpg mcp`'s tool table | The single feature most likely to violate the readonly guarantee is already on the roadmap | Explicit review gate on every new command/tool: does it reach a K8s write verb or a filesystem write outside the session tmpdir, before it's anywhere near the MCP wiring |
| Forwarding raw `HTTPPath` / label / annotation strings into tool results unfiltered | Tokens/session IDs embedded in URLs or annotations land in an LLM's context, and plausibly in harness telemetry, without the human-review gate that protects committed YAML today | Explicit decision + documentation (Pitfall 9); consider redaction for high-entropy path segments |
| Silent kubeconfig fallback to the wrong cluster because `KUBECONFIG`/`HOME` weren't forwarded by the MCP host | A session silently captures/reports on the wrong cluster, or fails opaquely | Document the required `env` block; have `start_session` echo which cluster/context it resolved in its result, so both the LLM and the human watching can catch a wrong-cluster session immediately |
| Interactive exec-credential plugin hangs a tool call indefinitely | Looks identical to a hung/broken MCP server from the harness's side, with no diagnostic pointing at the real cause | Bounded timeout around the initial client-build call (Pitfall 8) |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|--------------|-------------------|
| `start_session` returns before confirming the port-forward + relay dial actually succeeded | LLM believes a session is running; the first `status`/`list` call reveals nothing was ever captured, several turns later | Do the port-forward + initial relay dial synchronously inside `start_session` (already fast — dial-only timeout, not the full session); only the streaming/capture loop itself goes async |
| Vague tool descriptions ("get flows", "get policies") | LLM can't decide which tool answers "why was this pod's traffic dropped," calls the wrong one or asks the user to disambiguate | Write descriptions as usage guidance, not just field docs (Pitfall 6) — state explicitly when to use each tool relative to the others |
| Silent truncation of a large tool result | LLM reasons over an incomplete flow/policy list without knowing it's incomplete, draws confident but wrong conclusions | Always surface `has_more`/`total_count` explicitly in the payload rather than relying solely on client-side truncation warnings |
| Generic error message on kubeconfig/auth failure | Neither the LLM nor the human can tell "no cluster access" from "no hubble-relay pod found" from "auth is hung waiting on interactive input" | Distinct, specific error strings per failure mode (the "no relay pod found" case already has one in `pkg/k8s/portforward.go` — extend the same discipline to the new failure modes from Pitfall 8) |

## "Looks Done But Isn't" Checklist

- [ ] **stdout purity:** looks done when `cpg mcp` runs fine under a human watching a terminal — verify with an automated test asserting every line on the transport's stdout parses as JSON-RPC across a full session (start/status/list/stop), not a manual smoke test (Pitfall 1)
- [ ] **Session cleanup:** looks done when `stop_session` returns success — verify the port-forward's underlying goroutines and the session tmpdir are actually gone afterward, and separately verify the same happens on an *ungraceful* disconnect (stdin closed without ever calling `stop_session`) (Pitfall 3)
- [ ] **Pagination:** looks done when `list_policies` works against a handful of fixture policies — verify against a synthetic session with hundreds of policies/flows before calling it done (Pitfall 4)
- [ ] **Atomic reads:** looks done when query tools pass tests against a static, already-finished tmpdir — verify against a tmpdir being actively written by a live pipeline goroutine concurrently, under `-race` (Pitfall 5)
- [ ] **Readonly guarantee:** looks done when the README says "readonly" — verify by grepping the actual `cpg mcp` tool registration for any reachable K8s write verb or filesystem write outside the session tmpdir, and re-run that check every time a tool is added (Pitfall 7)
- [ ] **kubeconfig portability:** looks done when it works on the developer's own machine with a fully populated shell env — verify against the MCP host's actual spawn environment (explicit `env` block only, nothing inherited) (Pitfall 8)

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|----------------|------------------|
| stdout corruption shipped | LOW | Capture stdin/stdout to files instead of a live pipe to see the raw transcript; bisect to the offending writer/print call; add the missing redirect; add the Pitfall 1/10 regression test so it can't recur silently |
| Orphaned sessions accumulating on a dev machine | LOW | `pkill -f 'cpg mcp'`, manually clear stale `$TMPDIR/cpg-session-*` dirs; treat the discovery as the forcing function to fix the shutdown fan-out (Pitfall 3) before it happens in a teammate's environment |
| Non-atomic policy writer race surfaces in production | LOW | Isolated fix (temp+rename, matching the pattern the evidence/health writers already use) — no data model or API change required |
| Tool schema union (`oneOf` at root) silently dropped/weakened by a client | MEDIUM | Split into two explicit tools, or move "exactly one of" validation into the handler; if a long-running harness session has already "learned" the old shape, this is a breaking contract change mid-session |
| Secrets already surfaced through a shipped tool | MEDIUM-HIGH | Add redaction/filtering going forward, but treat any exposure that already happened in a captured conversation transcript as an incident on the harness/telemetry side, not just a code fix |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|-------------------|----------------|
| 1. stdout pollution | MCP server skeleton (stdio wiring) — first phase | Automated test: full session transcript, every stdout line parses as JSON-RPC |
| 2. Blocking tool handler on the pipeline | Session lifecycle (start/status/stop) | `start_session` returns in low-hundreds-of-ms in tests; pipeline context is `context.WithoutCancel`-derived, not the handler's request context |
| 3. Orphaned sessions on disconnect | Session lifecycle (shutdown path) | Integration test: kill the transport mid-session, assert port-forward + tmpdir are gone within a bounded deadline |
| 4. Unbounded results | Query tools | Synthetic large-session test asserts a paginated response stays under a fixed token/size budget |
| 5. Writer/reader races | Query tools (fix can land standalone, earlier) | `-race` test: reader polling `pkg/output` while a writer goroutine actively appends |
| 6. Tool schema mistakes | Query tools (schema designed before implementation) | Schema review checklist: no root-level `oneOf`/`anyOf`/`allOf`, enums only for small stable sets (`DropClass`, not `DropReason`), descriptions state "when to use" |
| 7. Readonly enforcement | Security/readonly hardening (structural decision made at skeleton time) | Import-graph check: the `mcp` command's dependency tree contains no K8s write verb and no filesystem write outside the session tmpdir |
| 8. kubeconfig access (env + interactive hang) | Security hardening (docs) + session lifecycle (bounded timeout) | `env` block documented in README/setup; `start_session` returns a clear timeout error when auth hangs, tested with a stub exec plugin that blocks on stdin |
| 9. Secrets in flow/policy data | Security hardening | Explicit written decision (ship documented risk vs. redact) reviewed before query tools expose `HTTPPath`/labels |
| 10. No protocol-level tests | MCP server skeleton (harness first) | In-memory transport test harness exists and is exercised by every subsequent phase's tools |

## Sources

**Official / primary (HIGH confidence):**
- MCP spec, stdio transport, current draft: https://modelcontextprotocol.io/specification/draft/basic/transports/stdio
- MCP spec, stdio transport, stable 2025-06-18: https://modelcontextprotocol.io/specification/2025-06-18/basic/transports
- MCP spec, versioning/lifecycle, current draft: https://modelcontextprotocol.io/specification/draft/basic/versioning
- MCP getting-started overview: https://modelcontextprotocol.io/docs/getting-started/intro
- Claude Code MCP reference docs (timeouts, output-token limits, schema-union handling), fetched 2026-07-20: https://code.claude.com/docs/en/mcp
- MCP tool annotations as an untrusted risk vocabulary: https://blog.modelcontextprotocol.io/posts/2026-03-16-tool-annotations/
- cobra v1.9.1 source, read directly (`OutOrStdout`/`OutOrStderr`/`Println` default-writer resolution, `Execute()` error/usage path)
- zap `NewProductionConfig`/`NewDevelopmentConfig` default output paths: https://pkg.go.dev/go.uber.org/zap
- modelcontextprotocol/go-sdk issue #224 (`Server.Run` context-cancellation bug, closed via PR #234): https://github.com/modelcontextprotocol/go-sdk/issues/224
- go-sdk in-memory transport / testing: https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp
- client-go exec auth plugin source and stdin/TTY behavior: https://github.com/kubernetes/client-go/blob/master/plugin/pkg/client/auth/exec/exec.go, https://github.com/kubernetes/kubernetes/issues/98451
- client-go SPDY goroutine leak history: https://github.com/kubernetes/kubernetes/issues/105830, https://github.com/kubernetes/kubernetes/issues/96339
- OWASP MCP Top 10 2025 (tool poisoning, excessive permissions, confused deputy): https://owasp.org/www-project-mcp-top-10/

**Community / cross-ecosystem (MEDIUM confidence, corroborated across multiple independent reports):**
- Claude Code orphaned MCP process issues: https://github.com/anthropics/claude-code/issues/22612, https://github.com/anthropics/claude-code/issues/39170
- Claude Code MCP env-variable-not-inherited issues: https://github.com/anthropics/claude-code/issues/1254, https://github.com/anthropics/claude-code/issues/10955
- MCP stdout-pollution write-ups: https://chatforest.com/guides/mcp-debugging-guide/, https://github.com/dirmacs/daedra/issues/4, https://github.com/ruvnet/claude-flow/issues/835
- MCP tool-result pagination guidance: https://chatforest.com/guides/mcp-pagination-patterns/, https://www.morphllm.com/mcp-output-too-large
- MCP tool schema design guidance: https://www.arcade.dev/blog/mcp-tool-definitions-guide/, https://aws.amazon.com/blogs/machine-learning/mcp-tool-design-practical-approaches-and-tradeoffs/
- mark3labs/mcp-go (Go SDK alternative, stdio transport, testing helpers): https://github.com/mark3labs/mcp-go

**cpg codebase (read/grepped directly this research session):**
- `pkg/hubble/pipeline.go` — `PipelineConfig.Stdout`, `RunPipeline`/`RunPipelineWithSource` signatures, session-summary print path
- `pkg/hubble/writer.go` — `policyWriter.diffOut`, dry-run diff emission
- `pkg/hubble/summary.go` — `PrintClusterHealthSummary`
- `pkg/output/writer.go` — non-atomic `os.WriteFile` policy writer
- `pkg/evidence/writer.go`, `pkg/hubble/health_writer.go` — atomic temp+rename writers
- `pkg/k8s/portforward.go`, `pkg/k8s/client.go` — port-forward `io.Discard` wiring, `LoadKubeConfig`, auth-plugin blank import
- `pkg/dropclass/classifier.go` — `DropClass`/`DropReason` enum shapes
- `pkg/evidence/schema.go` — `L7Ref` fields (no headers persisted)
- `cmd/cpg/main.go` — `buildLogger`, zap config branches
- `go.mod` — confirms no MCP SDK dependency exists yet; Go 1.25.1/toolchain 1.25.12
- `.planning/PROJECT.md` — v1.5 milestone scope, existing Key Decisions (header-leak anti-feature, evidence atomic writes, AI-feature shelving rationale)

---
*Pitfalls research for: MCP stdio server integration into an existing Go streaming CLI (cpg)*
*Researched: 2026-07-20*
