# Phase 19: Security Hardening & End-to-End Validation - Pattern Map

**Mapped:** 2026-07-21
**Files analyzed:** 4 (2 new Go test files [5 internal sub-components], 1 README edit, 1 go.mod mechanical edit)
**Analogs found:** 2 exact/role-match (README, go.mod) + 3 role-match sub-components (e2e schema/lifecycle/helpers) / 8 total sub-components; 2 sub-components (fake gRPC relay, subprocess+SSA infra) have **no in-repo analog** — substituted by this phase's own empirically-validated RESEARCH.md design (see "No Analog Found")

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|-----------------|---------------|
| `cmd/cpg/mcp_audit_test.go` [NEW] | test (static-analysis audit) | batch (one-shot SSA callgraph walk at `go test` time) | *(none in-repo)* — RESEARCH.md Pattern 1, empirically validated against this exact codebase | no-analog, but validated substitute design exists |
| `cmd/cpg/mcp_e2e_test.go` — graceful lifecycle test | test (integration, request-response) | request-response over real stdio | `cmd/cpg/mcp_session_test.go` `TestMCPSessionLifecycleWiringAndStdoutPurity` (lines 141-227) | role-match (same assertions, in-memory→real-subprocess transport swap) |
| `cmd/cpg/mcp_e2e_test.go` — tools/list + schema/annotation assertions | test (integration, request-response) | request-response | `cmd/cpg/mcp_query_tools_test.go` `TestMCPQueryToolsListed` + `TestMCPQueryToolsQRY05Contract` (lines 678-766) | role-match (identical assertion shape, needs subprocess client + full 8-tool set) |
| `cmd/cpg/mcp_e2e_test.go` — shared decode/schema helpers | utility (test helper) | n/a | `cmd/cpg/mcp_session_test.go` `requiredFields`/`decodeStructured` (lines 16-52) | exact — **reuse verbatim, same package, do not redefine** |
| `cmd/cpg/mcp_e2e_test.go` — fake Hubble relay (`observerpb.ObserverServer`) | test double / service (event-driven, streaming) | streaming (gRPC server stream) | *(none in-repo)* — `pkg/hubble/client_test.go` mocks at the `flowStream` Go-interface level, never a real `grpc.Server`; RESEARCH.md Pattern 2 / Architecture Diagram is the validated substitute | no-analog at the right layer |
| `cmd/cpg/mcp_e2e_test.go` — subprocess build+pipe+tee harness (incl. ungraceful variant) | test infra (process orchestration, file/stdio I/O) | request-response + process lifecycle | *(none in-repo)* — zero existing `exec.Command`/`os/exec`/`TestMain` usage anywhere in this module (verified via repo-wide grep) | no-analog — first of its kind in this repo |
| `README.md` — new `## MCP Server (cpg mcp)` section | documentation | n/a | `README.md` `## Explain policies` (lines 438-504) + `## L7 Prerequisites` (lines 246-374) — same file, established voice/structure | exact (same file, same author voice) |
| `go.mod` (`golang.org/x/tools` indirect→direct) | config | n/a | `go.mod`'s own existing require blocks (line 118: currently `// indirect`) | exact — mechanical `go mod tidy`, zero new hash |

## Pattern Assignments

### `cmd/cpg/mcp_audit_test.go` (test, static-analysis audit)

**Analog:** None in-repo (confirmed: `grep -rln "go/ssa\|go/callgraph\|go/packages" .` returns zero hits anywhere in the module). The substitute is this phase's own RESEARCH.md Pattern 1 — a design that was **written, executed, and validated against this exact repository** in the research session (not merely theoretical). Treat RESEARCH.md's code block as the closest available "analog" and copy it near-verbatim.

**Composition root to target** (`cmd/cpg/mcp.go:79-107`):
```go
func runMCPServer(ctx context.Context, transport mcp.Transport) error {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "cpg", Version: version},
		&mcp.ServerOptions{Logger: bridgedSlogLogger()},
	)
	mgr := session.NewManager(ctx, logger, mcpModeStdout(), version)
	registerSessionTools(server, mgr)
	registerQueryTools(server, mgr)

	err := server.Run(ctx, transport)
	mgr.Shutdown()
	return err
}
```
`mainPkg.Func("runMCPServer")` is the exact SSA lookup key — the audit's BFS root.

**Core pattern** — RESEARCH.md's validated 3-stage pipeline (RTA rooted at main+init, per RTA's own doc contract — NOT rooted directly at `runMCPServer`; then BFS-restrict to cpg-owned functions; then direct-call-instruction scan, never recursing into third-party callees):
```go
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

rtaRes := rta.Analyze([]*ssa.Function{mainFn, initFn}, true)
visited := bfsReachable(rtaRes.CallGraph, root) // plain BFS over Node.Out edges

cpgOwned := map[*ssa.Function]bool{}
for f := range visited {
    if f != nil && f.Pkg != nil && f.Pkg.Pkg != nil &&
        strings.HasPrefix(f.Pkg.Pkg.Path(), "github.com/SoulKyu/cpg/") {
        cpgOwned[f] = true
    }
}

for f := range cpgOwned {
    for _, b := range f.Blocks {
        for _, instr := range b.Instrs {
            call, ok := instr.(ssa.CallInstruction)
            if !ok { continue }
            common := call.Common()
            if callee := common.StaticCallee(); callee != nil {
                if disallowedFSWrite[callee.String()] {
                    assertAllowlisted(f.String(), callee.String())
                }
            } else if common.IsInvoke() && common.Method != nil {
                if k8sWriteVerbs[common.Method.Name()] {
                    assertAllowlisted(f.String(), common.Method.Name())
                }
            }
        }
    }
}
```
Full source: 19-RESEARCH.md lines 267-337 (design rationale + anti-patterns at lines 424-431).

**The exact allowlist this test must encode** (5 caller functions, independently re-verified in this pattern-mapping pass by reading each file directly — line numbers current as of this mapping):

| Caller (SSA name) | File:Lines | Calls | Why safe |
|---|---|---|---|
| `(*pkg/session.Manager).Start` | `pkg/session/manager.go:153` (`os.MkdirTemp("", "cpg-session-*")`), `:184`, `:208` (`os.RemoveAll(tmpDir)` cleanup-on-failure) | MkdirTemp, RemoveAll | Creates/removes only the session's own `os.MkdirTemp`-rooted tmpdir — never a caller-chosen path |
| `(*pkg/session.Manager).Shutdown$1` (anon closure) | `pkg/session/manager.go:484-487` | `os.RemoveAll(tmpDir)` | Same tmpDir, captured from the session struct, rooted in the same `os.MkdirTemp` call above |
| `(*pkg/output.Writer).Write` | `pkg/output/writer.go:40` (MkdirAll), `:81` (CreateTemp), `:88,92,96,100` (Remove), `:99` (Rename) | atomic temp+rename | Path is `filepath.Join(outputDir, ...)` where `outputDir` derives from `session.DeriveSessionPaths(tmpDir).OutputDir` — rooted in the session tmpdir |
| `(*pkg/evidence.Writer).Write` | `pkg/evidence/writer.go:66,70,77,81,84,85` | atomic temp+rename | Same shape, rooted in `DeriveSessionPaths(tmpDir).EvidenceDir` |
| `(*pkg/hubble.healthWriter).finalize` | `pkg/hubble/health_writer.go:140,145,152,156,159,160` | atomic temp+rename | Same shape, writes `cluster-health.json` under `DeriveSessionPaths(tmpDir).ClusterHealthPath`'s parent |

Reference atomic-write shape to cite in the allowlist's "why safe" comments (`pkg/output/writer.go:81-102`):
```go
tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
if err != nil { return fmt.Errorf("creating temp file: %w", err) }
tmpPath := tmp.Name()
if _, err := tmp.Write(data); err != nil {
    _ = tmp.Close(); _ = os.Remove(tmpPath)
    return fmt.Errorf("writing temp file: %w", err)
}
// ... Close/Chmod, each with its own os.Remove(tmpPath) rollback ...
if err := os.Rename(tmpPath, path); err != nil {
    _ = os.Remove(tmpPath)
    return fmt.Errorf("atomic rename: %w", err)
}
```

**go.mod integration point:** `go.mod:118` currently reads `golang.org/x/tools v0.44.0 // indirect`. Importing `go/packages`/`go/ssa`/`go/ssa/ssautil`/`go/callgraph`/`go/callgraph/rta` directly in `mcp_audit_test.go` requires no version bump (RESEARCH.md confirmed `git diff go.mod go.sum` empty after a real test run); run `go mod tidy` once the file exists to flip the comment to direct usage (cosmetic).

**Testing pattern:** same framework as every other file in `cmd/cpg` — stdlib `testing` + `testify` (`require`/`assert`), no golden files, no new test framework. Wall-clock budget per Pitfall 5: expect ~45-76s for this one test under `-race`; do not add a custom `-timeout` below ~120s.

---

### `cmd/cpg/mcp_e2e_test.go` (test, integration/e2e — real subprocess)

**Analogs:** `cmd/cpg/mcp_session_test.go` (lifecycle shape) + `cmd/cpg/mcp_query_tools_test.go` (tools/list schema-assertion shape) + `cmd/cpg/mcp_harness_test.go` (dual-scenario/stdout-purity shape). All three stay in place as fast in-memory feedback (D-11) — this file adds the same assertions over a **real subprocess + real stdio pipes + real fake gRPC relay** instead.

#### Sub-component A: shared decode/schema helpers — REUSE VERBATIM, DO NOT REDEFINE

`mcp_e2e_test.go` is `package main`, the same package as `mcp_session_test.go`. These existing unexported helpers are transport-agnostic (they operate on already-decoded Go values, never on the transport itself) and can be called directly with zero duplication:

```go
// cmd/cpg/mcp_session_test.go:24-41 — requiredFields
func requiredFields(t *testing.T, inputSchema any) []string { /* ... */ }

// cmd/cpg/mcp_session_test.go:47-52 — decodeStructured
func decodeStructured(t *testing.T, structuredContent any, out any) { /* ... */ }
```
RESEARCH.md's own Wave 0 Gaps note confirms this: "may reuse `decodeStructured`/`requiredFields` helpers already defined in `mcp_session_test.go` within the same package — no new shared-fixture file needed."

Do **NOT** reuse `startInMemoryMCPSession` (`mcp_harness_test.go:28-35`) or `connectQueryTestClient`/`startBypassSession` (`mcp_query_tools_test.go:33-88`) as-is — those wire an **in-memory** `mcp.InMemoryTransport` pair; the e2e needs its own subprocess-backed equivalent (Sub-component D below). Do **NOT** call `initLoggerForTesting`/`initObservedLoggerForTesting` (`cmd/cpg/testhelpers_test.go`) for the e2e — those swap the **in-process** package-level `logger` var, which has no effect on a separately-compiled subprocess; the e2e instead captures the subprocess's `cmd.Stderr` into a `bytes.Buffer` for failure diagnostics (see Sub-component D).

#### Sub-component B: graceful lifecycle assertions

**Analog:** `cmd/cpg/mcp_session_test.go:158-227` `TestMCPSessionLifecycleWiringAndStdoutPurity`.

**Core pattern to mirror** (adapt `clientT`/`cs` from in-memory to the real subprocess client — see Sub-component D):
```go
// mcp_session_test.go:183-212 (bypass args shape — reuse the arg names,
// point "server" at the fake relay's real address instead of the D-07
// unreachable "127.0.0.1:1" bypass):
startResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
	Name: "start_session",
	Arguments: map[string]any{
		"server":         fakeRelayAddr, // real listener this time, not "127.0.0.1:1"
		"timeout":        "5s",
		"flush_interval": "1s",
	},
})
require.NoError(t, err)
require.False(t, startResp.IsError)

var startOut struct{ SessionID string `json:"session_id"` }
decodeStructured(t, startResp.StructuredContent, &startOut)

statusResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
	Name: "get_status", Arguments: map[string]any{"session_id": startOut.SessionID},
})
var statusOut struct{ TmpDir string `json:"tmp_dir"` }
decodeStructured(t, statusResp.StructuredContent, &statusOut)
require.DirExists(t, statusOut.TmpDir)
```
Then extend with the 5 query-tool mid-capture calls, `stop_session`, `get_cluster_health` post-stop, `stdinW.Close()`, `cmd.Wait()` exit-0 assertion, and the byte-purity re-validation loop — full validated sequence in RESEARCH.md's "Architecture Patterns" diagram (lines 191-247) and Pattern 2 code (lines 361-407).

**Fixture that must reach the pipeline to produce real policy/evidence output** (Pitfall 4 — the existing `testdata` helpers alone are NOT sufficient):
```go
// pkg/policy/testdata/ingress_flow.go:9-28 (IngressTCPFlow) — base shape:
flow := testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=api"}, "prod", 8080)
// REQUIRED ADDITION (not set by the helper — see Pitfall 4):
flow.Verdict = flowpb.Verdict_DROPPED
flow.DropReasonDesc = flowpb.DropReason_POLICY_DENIED // -> dropclass.DropClassPolicy
```
`pkg/policy/testdata/ingress_flow.go:31-50` (`EgressUDPFlow`) is the second fixture shape available for the infra-class drop; RESEARCH's Pitfall 4 applies identically.

#### Sub-component C: tools/list + schema/annotation assertions (SRV-01/D-10)

**Analog:** `cmd/cpg/mcp_query_tools_test.go:678-766` (`TestMCPQueryToolsListed` + `TestMCPQueryToolsQRY05Contract`) — this is the exact assertion shape to copy, extended to the full 8-name set and to the 3 session tools' own (different) annotation truth.

```go
// mcp_query_tools_test.go:684-697 — tools/list + exact-name-set pattern:
toolsResult, err := cs.ListTools(ctx, nil)
require.NoError(t, err)
require.Len(t, toolsResult.Tools, 8, "3 session + 5 query tools")
byName := make(map[string]*mcp.Tool, len(toolsResult.Tools))
for _, tool := range toolsResult.Tools { byName[tool.Name] = tool }
for _, name := range []string{
	"start_session", "get_status", "stop_session",
	"list_dropped_flows", "list_policies", "get_policy", "get_evidence", "get_cluster_health",
} {
	assert.Contains(t, byName, name)
}
```
```go
// mcp_query_tools_test.go:734-743 — per-tool annotation/outputSchema contract
// (query tools only — session tools have DIFFERENT truth, see table below):
for _, name := range queryToolNames {
	tool := byName[name]
	require.NotNil(t, tool.Annotations)
	assert.True(t, tool.Annotations.ReadOnlyHint)
	require.NotNil(t, tool.Annotations.OpenWorldHint)
	assert.False(t, *tool.Annotations.OpenWorldHint)
	assert.NotEmpty(t, tool.OutputSchema)
}
```

**Exact annotation ground truth to assert against (read directly from registration code this pass, current line numbers):**

| Tool | File:Line | Annotations |
|---|---|---|
| `start_session` | `cmd/cpg/mcp_tools.go:105` | `ReadOnlyHint: false` only (no IdempotentHint, no OpenWorldHint) |
| `get_status` | `cmd/cpg/mcp_tools.go:149` | `ReadOnlyHint: true` only |
| `stop_session` | `cmd/cpg/mcp_tools.go:161` | `ReadOnlyHint: false, IdempotentHint: true` (no OpenWorldHint) |
| `list_policies` | `cmd/cpg/mcp_query.go:54-58` | `ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: jsonschema.Ptr(false)` |
| `get_policy` | `cmd/cpg/mcp_query.go:72-76` | same triple as list_policies |
| `get_cluster_health` | `cmd/cpg/mcp_query.go:96-100` | same triple |
| `get_evidence` | `cmd/cpg/mcp_query_evidence.go:84-87` | same triple |
| `list_dropped_flows` | `cmd/cpg/mcp_query_flows.go:130-133` | same triple |

A D-10 assertion that expects `OpenWorldHint` on any of the 3 session tools would be asserting something the registration code never sets — assert its presence/value **only** for the 5 query tools, exactly as the existing `TestMCPQueryToolsQRY05Contract` already scopes it.

**Dropclass enum assertion** (`mcp_query_tools_test.go:757-765`):
```go
dropclassSchema, _ := byName["list_dropped_flows"].InputSchema.(map[string]any)
dropclassProps, _ := dropclassSchema["properties"].(map[string]any)
dropclassProp, _ := dropclassProps["dropclass"].(map[string]any)
dropclassEnum, _ := dropclassProp["enum"].([]any)
assert.ElementsMatch(t, []any{"policy", "infra", "transient", "noise", "unknown"}, dropclassEnum)
```
This dropclass-enum assertion applies to `list_dropped_flows` ONLY — `get_evidence` has no dropclass input-schema property (its filter surface is namespace/workload/direction/port/peer/peer_cidr/http_method/http_path/dns_pattern per 18-CONTEXT D-10). *(Corrected during plan verification — an earlier draft wrongly extended the pattern to `get_evidence`.)*

#### Sub-component D: fake Hubble relay (`observerpb.ObserverServer`)

**Analog:** none in-repo at the right layer. `pkg/hubble/client_test.go` (`TestClient_StreamDroppedFlows` etc., lines 91-340) mocks the **Go interface** `flowStream` (`Recv()`/`Context()`) directly — useful only as a reminder of what shape `GetFlows`' response stream must satisfy, not as a gRPC-server analog. The real substitute is RESEARCH.md Pattern 2 / Architecture Diagram (validated by actually running it against this repo).

**The one RPC the fake must implement** (ground truth, read directly from `pkg/hubble/client.go` this pass):
```go
// pkg/hubble/client.go:109-123 — waitForConnReady: pure channel-state check,
// NEVER an RPC call:
func waitForConnReady(ctx context.Context, conn *grpc.ClientConn, timeout time.Duration) error {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn.Connect()
	for {
		if conn.GetState() == connectivity.Ready { return nil }
		if !conn.WaitForStateChange(dialCtx, conn.GetState()) {
			return fmt.Errorf("connecting to hubble relay %q: %w", conn.Target(), dialCtx.Err())
		}
	}
}
// pkg/hubble/client.go:77-88 — the ONLY RPC subsequently invoked:
client := observerpb.NewObserverClient(conn)
stream, err := client.GetFlows(ctx, req) // Follow:true, Whitelist from buildFilters
```
A fake embedding `observerpb.UnimplementedObserverServer` (by value) and implementing only `GetFlows` is sufficient — no other method is ever called by `pkg/hubble.Client`.

**Server-side plumbing pattern (net.Listen + grpc.NewServer):**
```go
lis, err := net.Listen("tcp", "127.0.0.1:0") // :0 = OS-assigned free port, avoids CI port collisions
srv := grpc.NewServer()
observerpb.RegisterObserverServer(srv, fake)
go srv.Serve(lis)
```
Full validated design: RESEARCH.md Architecture Patterns diagram (lines 191-247), Pitfall 3 (must signal "GetFlows reached" before the ungraceful test closes stdin — async race, empirically reproduced and fixed in research), Anti-Patterns (lines 424-431).

#### Sub-component E: subprocess build+pipe+tee harness (graceful exit-0 + ungraceful disconnect)

**Analog:** none in-repo — confirmed zero `os/exec`/`exec.Command`/`TestMain` usage anywhere in this module today. This sub-component is the first of its kind; use RESEARCH.md's validated Pattern 2 code near-verbatim:
```go
// RESEARCH.md lines 364-407 (validated: passed against a real -race binary):
cmd := exec.Command(binPath, "mcp")
stdinW, _ := cmd.StdinPipe()
stdoutR, _ := cmd.StdoutPipe()
var stderrBuf bytes.Buffer
cmd.Stderr = &stderrBuf // subprocess diagnostics on failure — the only "logger" substitute needed
cmd.Start()

var rawTee bytes.Buffer
teed := io.TeeReader(stdoutR, &rawTee)
transport := &mcp.IOTransport{Reader: io.NopCloser(teed), Writer: stdinW}
client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client", Version: "0.0.0"}, nil)
cs, err := client.Connect(ctx, transport, nil) // identical call shape to every in-memory test

exitedCh := make(chan error, 1)
go func() { exitedCh <- cmd.Wait() }()
// ... drive tool calls exactly like mcp_session_test.go/mcp_query_tools_test.go's
//     existing helpers (cs.ListTools, cs.CallTool) — zero new client-side API surface ...

stdinW.Close() // graceful: only AFTER stop_session
select {
case err := <-exitedCh: // err == nil expected — jsonrpc2 treats peer EOF as clean
case <-time.After(10 * time.Second): t.Fatal("did not exit in time")
}

for _, line := range bytes.Split(rawTee.Bytes(), []byte("\n")) {
	if len(bytes.TrimSpace(line)) == 0 { continue }
	var js json.RawMessage
	require.NoError(t, json.Unmarshal(line, &js), "stdout purity violation: %q", line)
}
```
**Build-once helper:** `go build -race -o <tmpdir>/cpg ./cmd/cpg` once per test binary run (TestMain or a `sync.Once`-guarded shared helper per D-05) — no existing analog, first `go build` invocation from within this test suite.

**Ungraceful variant (D-08):** same setup through `get_status`, then close stdin **without** `stop_session` — but only *after* synchronizing on the fake relay's `GetFlows` handler having actually fired (Pitfall 3 — an async race was empirically reproduced and fixed in research; do not skip this synchronization). Assert bounded self-exit (~10s cap), `require.NoDirExists(t, tmpDir)`, and `fake.snapshot().cancelled == true`.

**Imports this file needs that no other `cmd/cpg` test file currently imports** (flag explicitly for the planner — these are new, not copy-paste from an existing import block):
```go
import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os/exec"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	observerpb "github.com/cilium/cilium/api/v1/observer"
	"golang.org/x/tools/go/callgraph/rta" // mcp_audit_test.go only, not this file
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/SoulKyu/cpg/pkg/policy/testdata"
)
```
(Standard `mcp`/`testify` imports still apply — see any existing `cmd/cpg/mcp_*_test.go` file's own import block, e.g. `mcp_session_test.go:1-14`.)

---

### `README.md` — new `## MCP Server (cpg mcp)` section

**Analog:** same file's own `## Explain policies` (lines 438-504) and `## L7 Prerequisites` (lines 246-374) sections — both are the established voice/structure to match: practical, example-first, English, code blocks before prose caveats, a sub-heading per concern.

**Insertion point** (verified this pass by reading the full file's heading structure):
```
line 438: ## Explain policies
   ...
line 504: (evidence-dir paragraph, section ends)
line 506: ## Label selection        <-- insert the new section between these two
```

**Structural pattern to mirror** — a top-level `##` section with a short intro paragraph, a fenced example block, then `###` sub-headings for each distinct concern (mirrors `## L7 Prerequisites`'s own `### Two-step workflow` / `### Three ways to enable L7 visibility` / `### Starter L7-visibility CNP` / `### Capture-window guidance` / `### Known v1.2 limitations` structure at lines 259-374):
```markdown
## MCP Server (cpg mcp)

<intro paragraph — what it is, one sentence per D-13 item 1>

| Tool | Description |
|------|-------------|
| `start_session` | ... |
...

### Harness configuration

```json
{
  "mcpServers": {
    "cpg": {
      "command": "cpg",
      "args": ["mcp"],
      "env": {
        "KUBECONFIG": "/home/you/.kube/config",
        "PATH": "/usr/local/bin:/usr/bin:/bin",
        "TMPDIR": "/tmp"
      }
    }
  }
}
```

MCP hosts do not inherit your shell environment — ...

### Secrets posture

...

### Exec-credential-plugin caveat

...

### Session model
...
```

**Existing table pattern to copy exactly** — the repo already has one Markdown table this section's tool-list and skip-reasons tables should match stylistically (`README.md:412-419`, "Skip reasons"):
```markdown
| Reason | What it means |
|--------|---------------|
| `no_l4` | Flow has no L4 layer (no port/protocol info) |
```

**Cross-link target for the secrets-posture paragraph's `--l7` mention** (D-14): `## L7 Prerequisites <a id="l7-prerequisites"></a>` (line 246) — the existing anchor `#l7-prerequisites` is already used elsewhere in the file (e.g. line 626 `See [L7 Prerequisites](#l7-prerequisites)`); reuse the identical link syntax.

**Voice sample to match** (`README.md:11`, opening paragraph — direct, second person, no marketing fluff):
> "`cpg` connects to Hubble Relay, watches dropped flows in real time, and generates the CiliumNetworkPolicy YAML files that would allow them. You run it, wait for traffic to get denied, and it writes the fix."

---

### `go.mod` — `golang.org/x/tools` indirect → direct

**Analog:** the file's own existing structure. No behavior change — `go mod tidy` moves one line from the indirect block (currently `go.mod:118`) into the direct `require (...)` block (currently lines 7-25) once `mcp_audit_test.go` imports it directly. Zero new `go.sum` hash (already resolved).

## Shared Patterns

### MCP tool-call round trip (CallTool → decodeStructured)
**Source:** `cmd/cpg/mcp_session_test.go:47-52`, used identically by every existing `cmd/cpg/mcp_*_test.go` file and by both new e2e sub-tests.
```go
result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "...", Arguments: map[string]any{...}})
require.NoError(t, err) // transport-level error — should never fire for a well-formed call
require.False(t, result.IsError)
var out struct{ /* json-tagged subset */ }
decodeStructured(t, result.StructuredContent, &out)
```
**Apply to:** every tool call in `mcp_e2e_test.go`'s graceful and ungraceful sequences.

### Actionable-error-text contract ("not found or expired")
**Source:** `cmd/cpg/mcp_query_tools_test.go:768-799` (`TestMCPQueryToolsErrorTexts`), reusing `SESS-06`'s exact phrase from `pkg/session` untouched.
**Apply to:** not required by this phase's Wave 0 gaps directly, but if the e2e exercises any not-found path (e.g. a stray query after stop against a bogus id) it must assert the identical substring, never invent new wording.

### Atomic temp+rename filesystem write
**Source:** `pkg/output/writer.go:81-102`, mirrored byte-identically in `pkg/evidence/writer.go:66-85` and `pkg/hubble/health_writer.go:140-160`.
**Apply to:** the SEC-01 audit's allowlist rationale comments — every allowlisted call site is an instance of this exact same shape (CreateTemp → Write/Close/Chmod with Remove rollback → Rename), which is precisely why a single "why safe" comment style covers all 5 caller functions.

### `session.DeriveSessionPaths` as the single source of truth for tmpdir layout
**Source:** `pkg/session/paths.go:47-57`.
**Apply to:** both new test files — the audit's allowlist rationale ("paths rooted in `os.MkdirTemp`/`DeriveSessionPaths`") and the e2e's artifact-path assertions (deriving where policy/evidence/cluster-health files should land under the captured `tmp_dir`) both must call this function rather than re-deriving the `policies`/`evidence`/hash formula locally (Phase 18 WR-04's whole point).

### D-07 bypass argument shape for `start_session`
**Source:** `cmd/cpg/mcp_session_test.go:183-190`, reused by `cmd/cpg/mcp_query_tools_test.go:36-43` (`startBypassSession`).
```go
Arguments: map[string]any{
	"server":         addr,      // explicit address — skips kubeconfig/port-forward
	"timeout":        "...",
	"flush_interval": "...",
}
```
**Apply to:** the e2e's `start_session` call — same arg names, but `server` points at the fake relay's real `127.0.0.1:<port>` address (D-06) rather than the deliberately-unreachable `"127.0.0.1:1"` the in-memory tests use.

### CI race job — the e2e joins the existing job, does not add a new one
**Source:** `.github/workflows/ci.yml` step "Run tests with race detector": `go test -race -coverprofile=coverage.out -count=1 ./...`.
**Apply to:** both new test files run under this exact invocation with zero CI config changes; budget the audit's ~45-76s and the e2e's one-time `go build -race` cost into expected wall-clock, per RESEARCH.md Pitfall 5.

## No Analog Found

Sub-components with no close in-repo match — planner should build from RESEARCH.md's empirically-validated design instead of a codebase analog:

| Component | Role | Data Flow | Reason | Substitute |
|-----------|------|-----------|--------|------------|
| `cmd/cpg/mcp_audit_test.go` (whole file) | test (static-analysis) | batch | Zero existing `go/ssa`/`go/callgraph`/`go/packages` usage anywhere in this repo (verified: repo-wide grep, zero hits) | RESEARCH.md Pattern 1 (lines 267-337) — validated, produced exactly 5 callers / 0 k8s-write hits against this exact codebase |
| Fake `observerpb.ObserverServer` relay | test double / service | streaming (gRPC) | `pkg/hubble/client_test.go` only mocks the Go-level `flowStream` interface, never spins up a real `grpc.Server` | RESEARCH.md Pattern 2 + Architecture Diagram (lines 191-247, 355-407) — validated, both graceful and ungraceful runs passed against a real fake relay |
| Subprocess build+drive harness (`exec.Cmd`, `TestMain`) | test infra | process lifecycle + request-response | Zero existing `os/exec`/`TestMain` usage anywhere in this module | RESEARCH.md Pattern 2 (lines 355-407) — validated end to end, including the one real race found and fixed (Pitfall 3) |

## Metadata

**Analog search scope:** `cmd/cpg/` (all `mcp*.go` + `mcp*_test.go`), `pkg/session/` (`manager.go`, `session.go`, `paths.go`, `manager_test.go`), `pkg/hubble/` (`client.go`, `client_test.go`, `health_writer.go`), `pkg/output/writer.go`, `pkg/evidence/writer.go`, `pkg/policy/testdata/ingress_flow.go`, `README.md` (full file), `go.mod`, `.github/workflows/ci.yml`; repo-wide grep for `go/ssa`/`go/callgraph`/`go/packages`/`exec.Command`/`TestMain`.
**Files scanned:** 19 read directly (full or targeted ranges) + 2 repo-wide greps (zero-hit confirmations).
**Pattern extraction date:** 2026-07-21
**Note:** No project-level `./CLAUDE.md` exists at the repo root (only per-package `CLAUDE.md` auto-memory logs under `cmd/cpg/`, `pkg/hubble/`, `pkg/output/`, `pkg/policy/`, `pkg/policy/testdata/`, none of which impose conventions beyond what's already reflected in the code read above). `.claude/skills/` contains only an unrelated `desloppify/` tool directory — no applicable `SKILL.md` rules to load.
