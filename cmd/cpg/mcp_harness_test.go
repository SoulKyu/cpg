package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startInMemoryMCPSession creates one independent in-memory transport pair
// (mcp.NewInMemoryTransports), launches runMCPServer against the server
// half in a goroutine, and returns the client half plus a drain closure
// that blocks until that goroutine's error has been consumed.
//
// Each InMemoryTransport half may be Connect-ed AT MOST ONCE (go-sdk
// transport.go: Transport.Connect "is called exactly once by
// [Server.Connect] or [Client.Connect]"), so every scenario that needs its
// own Connect call gets its own pair via this helper rather than reusing
// one across scenarios. Phases 17-19 extend this file by adding new
// protocol scenarios with one more startInMemoryMCPSession call, not by
// reinventing this setup.
func startInMemoryMCPSession(ctx context.Context) (client *mcp.InMemoryTransport, drain func()) {
	serverT, clientT := mcp.NewInMemoryTransports()

	errCh := make(chan error, 1)
	go func() { errCh <- runMCPServer(ctx, serverT) }()

	return clientT, func() { <-errCh }
}

// TestMCPStdoutPurity proves SRV-02 end-to-end on in-memory transports: two
// independent simulated sessions — Session A (initialize handshake +
// tools/list) and Session B (unknown method) — each drive their own
// single-Connect transport pair and server goroutine, while a SINGLE
// os.Stdout os.Pipe capture spans both sessions. The final assertion proves
// zero bytes leaked (D-06) across every protocol scenario at once. Session
// A's tools/list intentionally does not assert an exact/empty tool count:
// Phase 16 registered zero tools, Phase 17+ register session/query tools —
// this call's only job here is exercising a normal protocol round-trip
// without leaking to stdout. The precise tool surface (names, required
// schema fields) is asserted in mcp_session_test.go's
// TestMCPSessionToolsListed.
func TestMCPStdoutPurity(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	realStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = realStdout })

	initLoggerForTesting(t)

	// --- Session A: initialize handshake + tools/list ---
	//
	// clientA is Connect-ed exactly once, via mcp.NewClient(...).Connect
	// below. Context cancellation is the shutdown mechanism, mirroring
	// go-sdk's own TestServerRunContextCancel (mcp/cmd_test.go): server.Run
	// selects on ctx.Done() and returns cleanly once cancelled, so the same
	// ctx is used for both the server goroutine and the client Connect
	// call.
	ctxA, cancelA := context.WithCancel(context.Background())
	clientA, drainA := startInMemoryMCPSession(ctxA)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	csA, err := client.Connect(ctxA, clientA, nil) // performs initialize automatically
	require.NoError(t, err)
	require.NotNil(t, csA.InitializeResult())

	toolsResult, err := csA.ListTools(ctxA, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, toolsResult.Tools, "Phase 17 registers session tools; TestMCPSessionToolsListed asserts the exact surface")

	cancelA()
	drainA()
	_ = csA.Close()

	// --- Session B: unknown method ---
	//
	// A SEPARATE, independent transport pair and server goroutine — never
	// touches Session A's pair. clientB is Connect-ed exactly once, via the
	// raw Connection obtained below; it never goes through mcp.NewClient,
	// so no automatic initialize call is made. go-sdk's jsonrpc2 dispatch
	// always writes a Response for any call-shaped Request (one with an
	// ID), converting the handler's returned error (here: "invalid during
	// session initialization", since Session B skips initialize on purpose)
	// into a well-formed JSON-RPC error frame — never a Go transport error
	// from Read.
	ctxB, cancelB := context.WithCancel(context.Background())
	clientB, drainB := startInMemoryMCPSession(ctxB)

	rawConn, err := clientB.Connect(ctxB)
	require.NoError(t, err)

	// MakeID only accepts the default JSON marshaling types of a Request ID:
	// nil, float64, or string — an int is rejected ("invalid ID type int").
	id, err := jsonrpc.MakeID(float64(1))
	require.NoError(t, err)
	require.NoError(t, rawConn.Write(ctxB, &jsonrpc.Request{
		ID:     id,
		Method: "totally/unknown",
		Params: json.RawMessage("{}"),
	}))

	msg, err := rawConn.Read(ctxB)
	require.NoError(t, err, "a JSON-RPC error response is still a well-formed frame, not a Go error")
	resp, ok := msg.(*jsonrpc.Response)
	require.True(t, ok, "expected a JSON-RPC Response frame for the unknown-method request")
	assert.NotNil(t, resp.Error, "unknown method must resolve to a JSON-RPC error, not a success result")

	cancelB()
	drainB()
	_ = rawConn.Close()

	// --- Zero-leak assertion (D-06), across BOTH sessions ---
	require.NoError(t, w.Close())
	leaked, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Empty(t, leaked, "D-06: zero bytes on the real os.Stdout across both sessions")
}
