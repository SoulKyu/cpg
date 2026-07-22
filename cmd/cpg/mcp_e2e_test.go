package main

// This file is the real-transport counterpart to the in-memory MCP tests in
// mcp_harness_test.go / mcp_session_test.go / mcp_query_tools_test.go (which
// stay in place as the fast-feedback layer, D-11). It builds the actual
// `cpg` binary with `go build -race` and drives `cpg mcp` as a subprocess
// over real OS stdin/stdout pipes, against an in-process fake Hubble relay
// -- no cluster, no kubeconfig required (D-05/D-06).
//
// Shared infrastructure (this file, built once here so Plan 04's
// ungraceful-disconnect variant is a pure consumer with no infra edits):
//   - fakeRelay: an in-process observerpb.ObserverServer implementing only
//     GetFlows, with started/cancelled signaling for Pitfall 3's async race.
//   - buildE2EBinary: the one `go build -race` invocation this test suite
//     performs, guarded by sync.Once so every e2e test in this package
//     shares one compiled binary.
//   - e2eSession / startE2ESubprocess: the subprocess + pipe + tee +
//     mcp.IOTransport client harness (never mcp.CommandTransport -- see the
//     doc comment on startE2ESubprocess for why).
//   - buildPolicyDeniedFlow / buildInfraClassDropFlow: drop-flow fixtures
//     that set Verdict/DropReasonDesc so they actually flow through the real
//     classifier (Pitfall 4 -- the base testdata helpers deliberately don't
//     set these fields themselves).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	observerpb "github.com/cilium/cilium/api/v1/observer"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/SoulKyu/cpg/pkg/policy/testdata"
	"github.com/SoulKyu/cpg/pkg/session"
)

// fakeRelay is an in-process fake Hubble relay: a minimal
// observerpb.ObserverServer implementing ONLY GetFlows -- RESEARCH.md
// confirms pkg/hubble.Client's waitForConnReady is a pure gRPC channel-state
// check that makes no RPC, and the ONLY RPC subsequently invoked is
// GetFlows. UnimplementedObserverServer is embedded BY VALUE (never by
// pointer -- the SDK's own doc comment warns that embedding by pointer
// nil-panics if an unimplemented method is ever invoked).
//
// GetFlows signals startedCh (closed exactly once, sync.Once-guarded) the
// instant the handler fires -- BEFORE sending any fixture flow -- so a
// caller can synchronize on "the relay was actually reached" instead of
// racing the pipeline's asynchronous background goroutine (Pitfall 3: the
// pipeline's GetFlows call happens in a detached goroutine relative to
// start_session's synchronous response, so get_status reporting "capturing"
// does not by itself guarantee GetFlows has been invoked yet). This plan
// (19-02) builds and exposes the signal even though only Plan 04's
// ungraceful variant consumes it directly for its own pass/fail assertion --
// interface-first, so Plan 04 is a pure consumer with no infra edits.
//
// After sending its fixture flows, GetFlows blocks on stream.Context().Done()
// and records cancelled=true when it fires -- the fan-out proof Plan 04's
// ungraceful-disconnect variant asserts on.
type fakeRelay struct {
	observerpb.UnimplementedObserverServer

	flows []*flowpb.Flow

	mu        sync.Mutex
	started   bool
	cancelled bool

	startedCh chan struct{}
	startOnce sync.Once
}

// newFakeRelay constructs a fakeRelay that serves flows (in order, then
// blocks) to the GetFlows caller.
func newFakeRelay(flows []*flowpb.Flow) *fakeRelay {
	return &fakeRelay{
		flows:     flows,
		startedCh: make(chan struct{}),
	}
}

// GetFlows implements observerpb.ObserverServer. See the fakeRelay doc
// comment for the started/cancelled signaling contract.
func (f *fakeRelay) GetFlows(_ *observerpb.GetFlowsRequest, stream observerpb.Observer_GetFlowsServer) error {
	f.startOnce.Do(func() {
		f.mu.Lock()
		f.started = true
		f.mu.Unlock()
		close(f.startedCh)
	})

	for _, flow := range f.flows {
		if err := stream.Send(&observerpb.GetFlowsResponse{
			ResponseTypes: &observerpb.GetFlowsResponse_Flow{Flow: flow},
		}); err != nil {
			return err
		}
	}

	// Hold the stream open until the client disconnects (transport death or
	// context cancellation) -- exactly what keeps a real session "capturing"
	// until stop_session, and what a disconnect must cancel.
	<-stream.Context().Done()

	f.mu.Lock()
	f.cancelled = true
	f.mu.Unlock()

	return nil
}

// relaySnapshot is the {started, cancelled} pair fakeRelay.snapshot()
// returns -- a single mutex-guarded read of both flags at once.
type relaySnapshot struct {
	started   bool
	cancelled bool
}

// snapshot returns a consistent read of the started/cancelled flags.
func (f *fakeRelay) snapshot() relaySnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return relaySnapshot{started: f.started, cancelled: f.cancelled}
}

// waitStarted blocks until the relay's GetFlows handler has been reached (or
// timeout elapses), defeating Pitfall 3's async race: the pipeline's
// StreamDroppedFlows -> GetFlows call happens in a background goroutine
// detached from start_session's synchronous response, so get_status
// reporting "capturing" does NOT guarantee GetFlows has been invoked yet.
func (f *fakeRelay) waitStarted(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-f.startedCh:
	case <-time.After(timeout):
		t.Fatal("fake relay's GetFlows was never reached within the deadline")
	}
}

// startFakeRelay stands up an in-process fake Hubble relay (D-06) on an
// OS-assigned free port (127.0.0.1:0 avoids CI port collisions) serving
// flows via GetFlows, and returns its dialable address plus the relay itself
// so callers can inspect started/cancelled. The gRPC server is stopped via
// t.Cleanup.
func startFakeRelay(t *testing.T, flows []*flowpb.Flow) (addr string, relay *fakeRelay) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	relay = newFakeRelay(flows)
	grpcServer := grpc.NewServer()
	observerpb.RegisterObserverServer(grpcServer, relay)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	t.Cleanup(grpcServer.Stop)

	return lis.Addr().String(), relay
}

// buildPolicyDeniedFlow returns an ingress TCP flow fixture wired through the
// real classifier as a POLICY_DENIED (dropclass.DropClassPolicy) drop.
// testdata.IngressTCPFlow deliberately does NOT set Verdict/DropReasonDesc
// (Pitfall 4 -- it's built for tests that call policy.BuildPolicy directly,
// already past classification); without these two fields the real pipeline
// silently writes zero policy/evidence files for this flow.
func buildPolicyDeniedFlow() *flowpb.Flow {
	flow := testdata.IngressTCPFlow([]string{"k8s:app=client"}, []string{"k8s:app=api"}, "prod", 8080)
	flow.Verdict = flowpb.Verdict_DROPPED
	flow.DropReasonDesc = flowpb.DropReason_POLICY_DENIED
	return flow
}

// buildInfraClassDropFlow returns an egress UDP flow fixture wired through
// the real classifier as an infra-class drop (CT_MAP_INSERTION_FAILED ->
// dropclass.DropClassInfra, pkg/dropclass/classifier.go) so the session's
// cluster-health aggregates are non-empty. Same Pitfall 4 requirement as
// buildPolicyDeniedFlow.
func buildInfraClassDropFlow() *flowpb.Flow {
	flow := testdata.EgressUDPFlow([]string{"k8s:app=web"}, []string{"k8s:app=dns"}, "prod", 53)
	flow.Verdict = flowpb.Verdict_DROPPED
	flow.DropReasonDesc = flowpb.DropReason_CT_MAP_INSERTION_FAILED
	return flow
}

var (
	e2eBinaryOnce sync.Once
	e2eBinaryPath string
	e2eBinaryErr  error
)

// buildE2EBinary builds this package (".", i.e. ./cmd/cpg -- `go test` sets
// the test binary's working directory to the package's own source
// directory) into a temp dir via `go build -race`, once per test-binary
// invocation (D-05) -- the first `go build` invocation from within this
// test suite. Guarded by a package-level sync.Once (rather than TestMain,
// which would change semantics for every other test in this package) so
// Plan 04's ungraceful-disconnect variant -- added later, same package --
// reuses this exact helper with zero infra edits. The built binary is
// intentionally left in its own OS temp dir for the lifetime of the test
// process rather than t.TempDir()-scoped: a per-test TempDir would be
// removed at the end of whichever test first triggers the build, breaking
// any later test in the same binary run that also calls this helper.
func buildE2EBinary(t *testing.T) string {
	t.Helper()
	e2eBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "cpg-e2e-bin-")
		if err != nil {
			e2eBinaryErr = fmt.Errorf("creating build temp dir: %w", err)
			return
		}
		binPath := filepath.Join(dir, "cpg")

		buildCmd := exec.Command("go", "build", "-race", "-o", binPath, ".")
		var buildOutput bytes.Buffer
		buildCmd.Stdout = &buildOutput
		buildCmd.Stderr = &buildOutput
		if err := buildCmd.Run(); err != nil {
			e2eBinaryErr = fmt.Errorf("go build -race -o %s .: %w\n%s", binPath, err, buildOutput.String())
			return
		}
		e2eBinaryPath = binPath
	})
	require.NoError(t, e2eBinaryErr)
	return e2eBinaryPath
}

// syncBuffer is a mutex-guarded byte buffer. A plain bytes.Buffer is NOT
// safe here: exec.Cmd's internal stderr-copy goroutine writes into stderr
// for the entire lifetime of the subprocess, and the MCP client's internal
// stdout-read loop (driven via io.TeeReader below) writes into rawTee for as
// long as the transport is connected -- both are background goroutines that
// outlive any single CallTool round trip, so a diagnostic read (e.g. on a
// failed require.NoError, or the final byte-purity check) can race a
// still-in-flight write. Verified empirically: an earlier unguarded
// bytes.Buffer version of this file failed `go test -race` on exactly this
// read/write pair (Rule 1 auto-fix).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Bytes returns a snapshot copy -- never a slice aliasing the internal
// buffer, which a concurrent Write may still mutate/reallocate.
func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, b.buf.Len())
	copy(out, b.buf.Bytes())
	return out
}

// e2eSession bundles the pieces the graceful (Task 2) and ungraceful (Plan
// 04) e2e tests each need to drive a real `cpg mcp` subprocess and
// independently verify its raw stdout bytes.
type e2eSession struct {
	cs       *mcp.ClientSession
	cmd      *exec.Cmd
	stdinW   io.WriteCloser
	rawTee   *syncBuffer
	stderr   *syncBuffer
	exitedCh chan error
}

// startE2ESubprocess builds (once) and spawns the real cpg binary as `cpg
// mcp`, wires an mcp.IOTransport over its real stdin/stdout OS pipes --
// deliberately NOT mcp.CommandTransport, whose Close() cascade conflates a
// clean self-exit with a forced SIGTERM/SIGKILL escalation and which hands
// stdout straight to the JSON-RPC decoder with no independent byte-purity
// tee -- tees the raw stdout bytes into rawTee for the explicit purity
// re-validation, and connects an MCP client (the same client call shape
// every existing in-memory test already uses). The caller drives the
// session, then closes stdinW (gracefully, only AFTER stop_session; or
// abruptly, for the ungraceful variant) and waits on exitedCh via waitExit.
func startE2ESubprocess(t *testing.T, ctx context.Context) *e2eSession {
	t.Helper()
	binPath := buildE2EBinary(t)

	cmd := exec.Command(binPath, "mcp")
	stdinW, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdoutR, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var stderrBuf syncBuffer
	cmd.Stderr = &stderrBuf // subprocess zap/zapslog diagnostics on failure

	require.NoError(t, cmd.Start())
	// WR-04: guarantee termination even if a require.* below this point
	// fails before stdinW.Close() is reached (or client.Connect itself
	// fails, before the cmd.Wait reaper goroutine is even set up) — without
	// this, a mid-test failure orphans the -race subprocess for the
	// remainder of the test-binary run. Process.Kill() is idempotent
	// w.r.t. a process that already self-exited: once cmd.Wait() has
	// reaped it, Go's os.Process tracks that and turns any later Kill()
	// into a no-op (ErrProcessDone) instead of risking a signal to a
	// recycled PID.
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	var rawTee syncBuffer
	teed := io.TeeReader(stdoutR, &rawTee)

	transport := &mcp.IOTransport{Reader: io.NopCloser(teed), Writer: stdinW}
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client", Version: "0.0.0"}, nil)
	cs, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err, "connect failed; subprocess stderr:\n%s", stderrBuf.String())

	exitedCh := make(chan error, 1)
	go func() { exitedCh <- cmd.Wait() }()

	return &e2eSession{
		cs:       cs,
		cmd:      cmd,
		stdinW:   stdinW,
		rawTee:   &rawTee,
		stderr:   &stderrBuf,
		exitedCh: exitedCh,
	}
}

// waitExit blocks until the subprocess exits or timeout elapses, returning
// cmd.Wait()'s result -- T-19-04's DoS mitigation: a subprocess that never
// exits is a t.Fatal, never an infinite hang. Shared by the graceful (Task
// 2) and ungraceful (Plan 04) variants. Must be called from the test's own
// goroutine (never from inside a spawned `go func`) -- t.Fatal only unwinds
// correctly on the goroutine actually running the test.
func (s *e2eSession) waitExit(t *testing.T, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-s.exitedCh:
		return err
	case <-time.After(timeout):
		t.Fatalf("subprocess did not exit within %s; stderr:\n%s", timeout, s.stderr.String())
		return nil // unreachable: t.Fatalf stops this goroutine via runtime.Goexit
	}
}

// assertStdoutPurity independently re-validates that every non-empty line of
// raw stdout bytes parses as a JSON-RPC frame -- the redemption of
// 16-CONTEXT D-06's deliberately deferred assertion, now proven on real
// stdio (D-05) rather than the in-memory transport.
func assertStdoutPurity(t *testing.T, raw []byte) {
	t.Helper()
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var js json.RawMessage
		assert.NoError(t, json.Unmarshal(line, &js), "stdout purity violation: %q", line)
	}
}

// TestMCPE2EGracefulLifecycle drives the full D-07 graceful lifecycle over a
// real `cpg mcp` subprocess (built with -race) against the fake Hubble relay
// (Task 1's shared infra): initialize -> tools/list -> start_session ->
// get_status -> all 5 query tools mid-capture -> stop_session ->
// get_cluster_health post-stop -> close stdin -> exit 0. It folds in SRV-01's
// full handshake/schema/annotation proof (D-10) and the stdout byte-purity
// re-validation that redeems 16-CONTEXT D-06 on real stdio (D-05).
func TestMCPE2EGracefulLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-subprocess e2e test in -short mode (one-time -race build + subprocess overhead)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	relayAddr, relay := startFakeRelay(t, []*flowpb.Flow{buildPolicyDeniedFlow(), buildInfraClassDropFlow()})

	e2e := startE2ESubprocess(t, ctx)
	cs := e2e.cs

	// (1) initialize handshake (SRV-01).
	initResult := cs.InitializeResult()
	require.NotNil(t, initResult)
	require.NotNil(t, initResult.ServerInfo)
	assert.Equal(t, "cpg", initResult.ServerInfo.Name)
	assert.NotEmpty(t, initResult.ServerInfo.Version)

	// (2) tools/list: exactly 8 tools, schemas + annotations (D-10).
	toolsResult, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Len(t, toolsResult.Tools, 8, "3 session + 5 query tools")

	byName := make(map[string]*mcp.Tool, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		byName[tool.Name] = tool
	}

	allToolNames := []string{
		"start_session", "get_status", "stop_session",
		"list_dropped_flows", "list_policies", "get_policy", "get_evidence", "get_cluster_health",
	}
	for _, name := range allToolNames {
		tool, ok := byName[name]
		require.True(t, ok, "%s must be registered", name)
		assert.NotEmpty(t, tool.Description, "%s must have a non-empty description", name)
		assert.NotNil(t, tool.InputSchema, "%s must have an inputSchema", name)
	}

	queryToolNames := []string{"list_dropped_flows", "list_policies", "get_policy", "get_evidence", "get_cluster_health"}
	for _, name := range queryToolNames {
		tool := byName[name]
		assert.NotEmpty(t, tool.OutputSchema, "%s is data-returning and must expose a non-empty outputSchema", name)
		require.NotNil(t, tool.Annotations, "%s must carry annotations", name)
		assert.True(t, tool.Annotations.ReadOnlyHint, "%s must be ReadOnlyHint", name)
		assert.True(t, tool.Annotations.IdempotentHint, "%s must be IdempotentHint", name)
		require.NotNil(t, tool.Annotations.OpenWorldHint, "%s must set OpenWorldHint explicitly", name)
		assert.False(t, *tool.Annotations.OpenWorldHint, "%s must be OpenWorldHint=false", name)
	}

	// Session tools: their own annotation truth -- NEVER assert OpenWorldHint,
	// registration never sets it (D-10).
	startTool := byName["start_session"]
	require.NotNil(t, startTool.Annotations)
	assert.False(t, startTool.Annotations.ReadOnlyHint, "start_session must be ReadOnlyHint=false")
	assert.Nil(t, startTool.Annotations.OpenWorldHint, "start_session must never set OpenWorldHint")

	statusTool := byName["get_status"]
	require.NotNil(t, statusTool.Annotations)
	assert.True(t, statusTool.Annotations.ReadOnlyHint, "get_status must be ReadOnlyHint=true")
	assert.Nil(t, statusTool.Annotations.OpenWorldHint, "get_status must never set OpenWorldHint")

	stopTool := byName["stop_session"]
	require.NotNil(t, stopTool.Annotations)
	assert.False(t, stopTool.Annotations.ReadOnlyHint, "stop_session must be ReadOnlyHint=false")
	assert.True(t, stopTool.Annotations.IdempotentHint, "stop_session must be IdempotentHint=true")
	assert.Nil(t, stopTool.Annotations.OpenWorldHint, "stop_session must never set OpenWorldHint")

	// Dropclass enum: list_dropped_flows ONLY -- get_evidence's filter
	// surface deliberately excludes dropclass (18-CONTEXT D-10).
	dropSchema, ok := byName["list_dropped_flows"].InputSchema.(map[string]any)
	require.True(t, ok, "list_dropped_flows InputSchema must round-trip as map[string]any")
	dropProps, ok := dropSchema["properties"].(map[string]any)
	require.True(t, ok)
	dropProp, ok := dropProps["dropclass"].(map[string]any)
	require.True(t, ok, "list_dropped_flows must have a dropclass schema property")
	dropEnum, ok := dropProp["enum"].([]any)
	require.True(t, ok, "list_dropped_flows dropclass must carry an enum constraint")
	assert.ElementsMatch(t, []any{"policy", "infra", "transient", "noise", "unknown"}, dropEnum)

	evidenceSchema, ok := byName["get_evidence"].InputSchema.(map[string]any)
	require.True(t, ok)
	evidenceProps, ok := evidenceSchema["properties"].(map[string]any)
	require.True(t, ok)
	_, hasDropclass := evidenceProps["dropclass"]
	assert.False(t, hasDropclass, "get_evidence must NOT carry a dropclass property (18-CONTEXT D-10)")

	// (3) start_session against the real fake relay (D-06/D-07 bypass --
	// the real listener from Task 1, not the in-memory tests' unreachable
	// "127.0.0.1:1").
	startResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "start_session",
		Arguments: map[string]any{
			"server":         relayAddr,
			"tls":            false,
			"timeout":        "5s",
			"flush_interval": "1s",
		},
	})
	require.NoError(t, err)
	require.False(t, startResp.IsError, "start_session against the fake relay must not error")

	var startOut struct {
		SessionID string `json:"session_id"`
	}
	decodeStructured(t, startResp.StructuredContent, &startOut)
	require.NotEmpty(t, startOut.SessionID)

	// (4) get_status.
	statusResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_status",
		Arguments: map[string]any{"session_id": startOut.SessionID},
	})
	require.NoError(t, err)
	require.False(t, statusResp.IsError)

	var statusOut struct {
		TmpDir string `json:"tmp_dir"`
	}
	decodeStructured(t, statusResp.StructuredContent, &statusOut)
	require.NotEmpty(t, statusOut.TmpDir)
	require.DirExists(t, statusOut.TmpDir, "Manager.Start must have created the session tmpdir")

	// The pipeline's GetFlows call happens in a detached background
	// goroutine relative to start_session's synchronous response (Pitfall
	// 3) -- wait for the relay to actually be reached before relying on its
	// fixture flows having been sent.
	relay.waitStarted(t, 10*time.Second)
	midSnapshot := relay.snapshot()
	assert.True(t, midSnapshot.started, "fake relay must have been reached by now")
	assert.False(t, midSnapshot.cancelled, "fake relay's stream must not be cancelled while still capturing")

	// (5) all 5 query tools mid-capture. The aggregator flushes to disk on
	// its own flush_interval ticker (1s here), so the artifact-dependent
	// tools are polled with require.Eventually rather than asserted on the
	// very first call -- avoiding a flaky race against the flush tick while
	// still proving the fixtures flowed through mid-capture (not merely
	// after stop_session).
	type policyRow struct {
		Namespace string `json:"namespace"`
		Workload  string `json:"workload"`
	}
	var policyRows []policyRow
	require.Eventually(t, func() bool {
		listResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_policies",
			Arguments: map[string]any{"session_id": startOut.SessionID},
		})
		if err != nil || listResp.IsError {
			return false
		}
		var out struct {
			Policies []policyRow `json:"policies"`
		}
		decodeStructured(t, listResp.StructuredContent, &out)
		if len(out.Policies) == 0 {
			return false
		}
		policyRows = out.Policies
		return true
	}, 15*time.Second, 200*time.Millisecond, "expected list_policies to observe the POLICY_DENIED fixture mid-capture")
	require.Len(t, policyRows, 1)
	assert.Equal(t, "prod", policyRows[0].Namespace)
	assert.Equal(t, "api", policyRows[0].Workload)

	getPolicyResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_policy",
		Arguments: map[string]any{
			"session_id": startOut.SessionID,
			"namespace":  "prod",
			"workload":   "api",
		},
	})
	require.NoError(t, err)
	require.False(t, getPolicyResp.IsError)
	var getPolicyOut struct {
		YAML string `json:"yaml"`
	}
	decodeStructured(t, getPolicyResp.StructuredContent, &getPolicyOut)
	assert.NotEmpty(t, getPolicyOut.YAML, "get_policy must return the full CNP YAML")

	require.Eventually(t, func() bool {
		flowsResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_dropped_flows",
			Arguments: map[string]any{"session_id": startOut.SessionID},
		})
		if err != nil || flowsResp.IsError {
			return false
		}
		var out struct {
			Samples []struct {
				Namespace string `json:"namespace"`
			} `json:"samples"`
		}
		decodeStructured(t, flowsResp.StructuredContent, &out)
		return len(out.Samples) > 0
	}, 15*time.Second, 200*time.Millisecond, "expected list_dropped_flows to return live samples mid-capture")

	require.Eventually(t, func() bool {
		evidenceResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "get_evidence",
			Arguments: map[string]any{
				"session_id": startOut.SessionID,
				"namespace":  "prod",
				"workload":   "api",
			},
		})
		if err != nil || evidenceResp.IsError {
			return false
		}
		var out struct {
			TotalCount int `json:"total_count"`
		}
		decodeStructured(t, evidenceResp.StructuredContent, &out)
		return out.TotalCount > 0
	}, 15*time.Second, 200*time.Millisecond, "expected get_evidence to return live matched rules mid-capture")

	healthMidResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_cluster_health",
		Arguments: map[string]any{"session_id": startOut.SessionID},
	})
	require.NoError(t, err)
	require.False(t, healthMidResp.IsError, "get_cluster_health mid-capture must be a non-error available_after_stop marker")
	var healthMidOut struct {
		AvailableAfterStop bool `json:"available_after_stop"`
	}
	decodeStructured(t, healthMidResp.StructuredContent, &healthMidOut)
	assert.True(t, healthMidOut.AvailableAfterStop, "get_cluster_health must return the available_after_stop marker while capturing")

	// Pitfall 4 proof: a real policy file landed on disk under
	// DeriveSessionPaths(tmp_dir).
	paths := session.DeriveSessionPaths(statusOut.TmpDir)
	require.FileExists(t, filepath.Join(paths.OutputDir, "prod", "api.yaml"),
		"the POLICY_DENIED fixture must have produced a real policy file on disk")

	// (6) stop_session.
	stopResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "stop_session",
		Arguments: map[string]any{"session_id": startOut.SessionID},
	})
	require.NoError(t, err)
	require.False(t, stopResp.IsError)

	var stopOut struct {
		FlowsSeen       uint64 `json:"flows_seen"`
		PoliciesWritten uint64 `json:"policies_written"`
	}
	decodeStructured(t, stopResp.StructuredContent, &stopOut)
	assert.Greater(t, stopOut.FlowsSeen, uint64(0), "stop_session summary must report flows_seen > 0")
	assert.Greater(t, stopOut.PoliciesWritten, uint64(0), "stop_session summary must report policies_written > 0")

	// (7) get_cluster_health post-stop: full report + remediation URLs.
	healthPostResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_cluster_health",
		Arguments: map[string]any{"session_id": startOut.SessionID},
	})
	require.NoError(t, err)
	require.False(t, healthPostResp.IsError)

	var healthPostOut struct {
		Report *struct {
			Drops []struct {
				Reason      string `json:"reason"`
				Remediation string `json:"remediation"`
			} `json:"drops"`
		} `json:"report"`
	}
	decodeStructured(t, healthPostResp.StructuredContent, &healthPostOut)
	require.NotNil(t, healthPostOut.Report, "the infra-class fixture must produce a real cluster-health report post-stop")
	require.NotEmpty(t, healthPostOut.Report.Drops)
	assert.NotEmpty(t, healthPostOut.Report.Drops[0].Remediation, "post-stop report must include a per-reason remediation URL")

	// (8) graceful shutdown: close stdin ONLY AFTER stop_session, then
	// require a clean, bounded self-exit (jsonrpc2 treats a peer-initiated
	// clean io.EOF as not an error, so server.Run/RunE/main all return nil ->
	// exit 0).
	require.NoError(t, e2e.stdinW.Close())
	exitErr := e2e.waitExit(t, 10*time.Second)
	assert.NoError(t, exitErr, "a clean peer-EOF disconnect must exit 0")

	// (9) stdout byte-purity re-validation -- the redemption of 16-CONTEXT
	// D-06 on real stdio (D-05).
	assertStdoutPurity(t, e2e.rawTee.Bytes())
}

// TestMCPE2EUngracefulDisconnect proves the second half of SRV-04 (D-08):
// killing the transport (stdin EOF -- standing in for any real-world
// equivalent, e.g. a harness crash) while a session is still actively
// capturing, with NO stop_session call, triggers the SAME bounded
// session-cleanup fan-out Manager.Shutdown runs on every process-exit path
// (SESS-05, Phase 17): the session ctx is cancelled, the pipeline's Hubble
// stream is torn down, the process self-exits, and the session tmpdir is
// removed. Reuses Plan 02's fake relay + subprocess/tee harness + fixtures
// (Task 1's shared infra) with zero infrastructure edits.
//
// D-09: this e2e deliberately bypasses port-forward (D-06/D-07 -- no
// cluster, that IS the fake-relay bypass's whole point), so there is no real
// port-forward for this test to observe closing. The SAME Manager.Shutdown()
// fan-out that would close a real port-forward is what cancels the fake
// relay's GetFlows stream context here -- proving the fan-out fires on
// transport death via stream-cancel + tmpdir removal IS the honest,
// non-phantom proxy for "port-forward cleaned up" in a cluster-free e2e; it
// is not a gap this test fails to cover.
func TestMCPE2EUngracefulDisconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-subprocess e2e test in -short mode (one-time -race build + subprocess overhead)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	relayAddr, relay := startFakeRelay(t, []*flowpb.Flow{buildPolicyDeniedFlow(), buildInfraClassDropFlow()})

	e2e := startE2ESubprocess(t, ctx)
	cs := e2e.cs

	// Same setup as the graceful test, but only through get_status -- D-08
	// requires NO stop_session call before the disconnect below. A short
	// flush_interval (matching the graceful test) lets the aggregator flush
	// the POLICY_DENIED fixture to disk quickly, which the synchronization
	// step below depends on.
	startResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "start_session",
		Arguments: map[string]any{
			"server":         relayAddr,
			"tls":            false,
			"flush_interval": "1s",
		},
	})
	require.NoError(t, err)
	require.False(t, startResp.IsError, "start_session against the fake relay must not error")

	var startOut struct {
		SessionID string `json:"session_id"`
	}
	decodeStructured(t, startResp.StructuredContent, &startOut)
	require.NotEmpty(t, startOut.SessionID)

	statusResp, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_status",
		Arguments: map[string]any{"session_id": startOut.SessionID},
	})
	require.NoError(t, err)
	require.False(t, statusResp.IsError)

	var statusOut struct {
		TmpDir string `json:"tmp_dir"`
	}
	decodeStructured(t, statusResp.StructuredContent, &statusOut)
	require.NotEmpty(t, statusOut.TmpDir)
	require.DirExists(t, statusOut.TmpDir, "Manager.Start must have created the session tmpdir")

	// CRITICAL (Pitfall 3): Manager.Start returns as soon as its synchronous
	// setup completes -- the pipeline's actual GetFlows call happens later,
	// inside a detached background goroutine. get_status reporting
	// "capturing" does NOT by itself guarantee GetFlows has been invoked yet:
	// closing stdin before the relay is actually reached makes the
	// stream-cancel assertion below flaky (empirically reproduced in
	// research -- started=false cancelled=false despite a correct process
	// self-exit and tmpdir removal). Block on the relay's own started
	// signal, bounded, before disconnecting.
	relay.waitStarted(t, 5*time.Second)

	// A SECOND, stronger synchronization gate beyond waitStarted (Rule 1
	// auto-fix, found empirically running this exact test): waitStarted
	// only proves fakeRelay.GetFlows was INVOKED, not that its short
	// in-memory fixture-send loop (both flows.Send calls, before it blocks
	// on <-stream.Context().Done()) has FINISHED. Disconnecting immediately
	// after waitStarted races that loop -- if the transport tears down
	// mid-loop, stream.Send returns a non-nil error and GetFlows takes its
	// early `return err` path, NEVER reaching (and never setting)
	// `cancelled`. Reproduced empirically: closing stdin right after
	// waitStarted produced a subprocess session summary of "flows_seen": 0
	// and a 3ms session duration, and `cancelled` stayed false even 5s
	// after the process had already exited cleanly. Waiting for the
	// POLICY_DENIED fixture to actually land as a real policy file proves
	// the relay's full fixture-send loop already completed -- an artifact
	// reaching disk (network receive + classify + aggregate + flush-ticker
	// write) takes far longer than the relay's two back-to-back in-memory
	// Send() calls that precede its blocking wait, so disconnecting after
	// this point reliably lets GetFlows reach <-stream.Context().Done()
	// before any cancellation can race it again.
	paths := session.DeriveSessionPaths(statusOut.TmpDir)
	policyPath := filepath.Join(paths.OutputDir, "prod", "api.yaml")
	require.Eventually(t, func() bool {
		_, statErr := os.Stat(policyPath)
		return statErr == nil
	}, 15*time.Second, 200*time.Millisecond, "expected the POLICY_DENIED fixture to produce a real policy file before disconnecting")

	// Ungraceful disconnect (D-08): abruptly close stdin with NO preceding
	// stop_session call -- the session is still StateCapturing when the
	// transport dies.
	require.NoError(t, e2e.stdinW.Close())

	// (1) the process self-exits within a bounded cap -- comfortably above
	// the SESS-05 per-step deadlines (stopWait=5s + removeWait=2s in
	// pkg/session/manager.go's Shutdown); observed self-exit in research was
	// ~1s. waitExit's own t.Fatal covers the "never exits" DoS case
	// (T-19-04b). The transport-level EOF is clean either way (jsonrpc2
	// treats a peer-initiated io.EOF as not-an-error, exactly like the
	// graceful variant), so the process still exits cleanly even though no
	// stop_session preceded the disconnect.
	exitErr := e2e.waitExit(t, 10*time.Second)
	assert.NoError(t, exitErr, "an ungraceful peer-EOF disconnect must still exit cleanly (jsonrpc2 treats peer EOF as not-an-error regardless of in-flight session state)")

	// (2) the session tmpdir was removed by the cleanup fan-out.
	require.NoDirExists(t, statusOut.TmpDir, "Manager.Shutdown's bounded cleanup fan-out must remove the session tmpdir on transport death")

	// (3) the fake relay's GetFlows stream context was cancelled -- proving
	// the fan-out actually fired the session-ctx cancellation (T-19-06b),
	// not merely that the process happened to exit. Still polled, not read
	// instantaneously, even after the stronger synchronization above:
	// delivering the client's cancellation to the relay's server-side
	// stream is a genuine network event (an HTTP/2 stream-reset frame over
	// the loopback gRPC connection), so a short bounded wait is the
	// technically correct way to observe it rather than a same-instant
	// assertion. D-09: in this cluster-free e2e there is no real
	// port-forward to observe closing; this stream-cancel + tmpdir-removal
	// pair together are the honest, non-phantom proxy for "the same
	// Shutdown() fan-out that closes a real port-forward fired here".
	require.Eventually(t, func() bool {
		return relay.snapshot().cancelled
	}, 5*time.Second, 50*time.Millisecond, "the fake relay's GetFlows stream context must be cancelled by the session-cleanup fan-out on transport death")
	assert.True(t, relay.snapshot().started, "fake relay must have been reached before this assertion is meaningful")
}
