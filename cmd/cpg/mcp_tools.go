package main

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/SoulKyu/cpg/pkg/session"
)

// startSessionArgs is the LLM-facing input to start_session (D-05's
// argument surface — the same filters cmd/cpg/generate.go already exposes
// as CLI flags). Every field is optional: omitempty on each means an MCP
// client may call start_session with an empty object and still get sensible
// defaults (Manager.Start / buildPipelineConfig apply the 10s/5s Pitfall A
// fallbacks for Timeout/FlushInterval; the K8s-facing defaults are
// unchanged from the CLI). sessionRef.SessionID below is this file's one
// exception: it is always required.
type startSessionArgs struct {
	Namespace         []string `json:"namespace,omitempty" jsonschema:"namespace filter, repeatable"`
	AllNamespaces     bool     `json:"all_namespaces,omitempty" jsonschema:"observe all namespaces"`
	L7                bool     `json:"l7,omitempty" jsonschema:"enable L7 (HTTP/DNS) policy generation"`
	IgnoreDropReasons []string `json:"ignore_drop_reasons,omitempty" jsonschema:"exclude flows by drop reason name before classification"`
	IgnoreProtocols   []string `json:"ignore_protocols,omitempty" jsonschema:"drop flows whose L4 protocol matches: tcp, udp, icmpv4, icmpv6, sctp"`
	Server            string   `json:"server,omitempty" jsonschema:"explicit Hubble Relay address; bypasses auto port-forward when set"`
	TLS               bool     `json:"tls,omitempty" jsonschema:"enable TLS for the gRPC connection"`
	Timeout           string   `json:"timeout,omitempty" jsonschema:"Go duration string, e.g. \"30s\" (default: 10s; max 24h) — bounds kubeconfig+port-forward+dial setup"`
	ClusterDedup      bool     `json:"cluster_dedup,omitempty" jsonschema:"skip policies that already exist in cluster"`
	FlushInterval     string   `json:"flush_interval,omitempty" jsonschema:"Go duration string, e.g. \"5s\" (default: 5s; max 24h)"`
}

// sessionRef is the input to get_status/stop_session. Unlike every
// startSessionArgs field, SessionID carries NO omitempty: a struct field
// without omitempty/omitzero becomes schema-required (Pattern 0), and
// session_id is always required for these two tools.
type sessionRef struct {
	SessionID string `json:"session_id" jsonschema:"the opaque session_id returned by start_session"`
}

// maxSessionDuration is the upper bound (WR-03) on any MCP-supplied
// timeout/flush_interval value, enforced by parseOptionalDuration below.
// Prior to this bound, an MCP client could pass an arbitrarily large
// duration (e.g. "876000h") straight into setupCtx's deadline
// (pkg/session/manager.go's Start) — the sole remaining backstop on setup
// duration once WR-02's ctx-cancellation merge is in place for the
// ctx-observing path — or into the aggregator's flush ticker, leaving
// policy_file_count at 0 for the entire session. 24h is a
// product-appropriate ceiling: no real capture session is expected to run
// longer.
const maxSessionDuration = 24 * time.Hour

// parseOptionalDuration parses raw as a Go duration string for the named
// field. An empty string means the MCP arg was omitted: that is valid and
// returns the zero Duration, letting Manager.Start/buildPipelineConfig apply
// their own default via defaultDuration (Pitfall A) — an omitted
// timeout/flush_interval is never an error here. A non-empty value must
// parse via time.ParseDuration and be strictly positive: time.ParseDuration
// accepts a syntactically valid negative string ("-5s"), which must still be
// rejected explicitly rather than silently reaching PipelineConfig. A
// positive value above maxSessionDuration is also rejected (WR-03) — this
// bounds both timeout and flush_interval, since both flow through this one
// parser.
func parseOptionalDuration(raw, field string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q: %w", field, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive, got %q", field, raw)
	}
	if d > maxSessionDuration {
		return 0, fmt.Errorf("%s must be <= %s, got %q", field, maxSessionDuration, raw)
	}
	return d, nil
}

// registerSessionTools registers the 3 session-lifecycle MCP tools —
// start_session, get_status, stop_session — on server, wired to mgr. This is
// the composition-root entry point cmd/cpg/mcp.go's runMCPServer calls right
// after constructing the Manager; Phase 18 adds read-side query tools
// alongside these in the same composition-root style.
//
// Argument validation/normalization (namespace/all_namespaces mutual
// exclusivity, ignore_protocols/ignore_drop_reasons allowlisting) happens
// HERE, in package main, before calling into pkg/session — per Pitfall J,
// the existing CLI validators (commonflags.go) are unexported, package-main
// functions pkg/session cannot see; pkg/session.StartArgs receives only
// already-validated, already-normalized values.
func registerSessionTools(server *mcp.Server, mgr *session.Manager) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "start_session",
		Description: "Start a live Hubble capture session. Returns immediately with an " +
			"opaque session_id; the capture runs in the background. Only one session " +
			"may be active at a time — call stop_session before starting another. " +
			"Poll get_status to check progress.",
		// Not read-only: this starts a background capture and (usually) a
		// port-forward. Truthful either way — it never mutates the cluster
		// itself, but it does start real background work and side effects
		// under the session tmpdir, so ReadOnlyHint: true would be dishonest.
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args startSessionArgs) (*mcp.CallToolResult, session.StartResult, error) {
		if len(args.Namespace) > 0 && args.AllNamespaces {
			return nil, session.StartResult{}, fmt.Errorf("namespace and all_namespaces are mutually exclusive")
		}
		ignoreProtocols, err := validateIgnoreProtocols(args.IgnoreProtocols) // D-06, verbatim reuse
		if err != nil {
			return nil, session.StartResult{}, err
		}
		ignoreDropReasons, err := validateIgnoreDropReasons(args.IgnoreDropReasons, logger) // D-06, verbatim reuse
		if err != nil {
			return nil, session.StartResult{}, err
		}
		timeout, err := parseOptionalDuration(args.Timeout, "timeout")
		if err != nil {
			return nil, session.StartResult{}, err
		}
		flushInterval, err := parseOptionalDuration(args.FlushInterval, "flush_interval")
		if err != nil {
			return nil, session.StartResult{}, err
		}

		result, err := mgr.Start(ctx, session.StartArgs{
			Namespaces:        args.Namespace,
			AllNamespaces:     args.AllNamespaces,
			L7:                args.L7,
			IgnoreDropReasons: ignoreDropReasons,
			IgnoreProtocols:   ignoreProtocols,
			Server:            args.Server,
			TLS:               args.TLS,
			Timeout:           timeout,
			ClusterDedup:      args.ClusterDedup,
			FlushInterval:     flushInterval,
		})
		// Error path: return the Go error as-is — the go-sdk auto-converts a
		// non-nil error into a tool-error result, with the error text as
		// content (Pattern 0). Never hand-construct that result here.
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_status",
		Description: "Return coarse session state (capturing/stopped), elapsed time, and " +
			"on-disk artifact file counts. Works for a stopped-but-retained session too.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *mcp.CallToolRequest, args sessionRef) (*mcp.CallToolResult, session.StatusResult, error) {
		result, err := mgr.Status(args.SessionID)
		return nil, result, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "stop_session",
		Description: "Cancel the capture, finalize cluster-health.json and session stats, " +
			"and return the final summary. Idempotent — a second stop returns the same " +
			"summary with an already-stopped marker, never an error. Artifacts remain " +
			"queryable until a new session starts.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: true}, // D-03
	}, func(_ context.Context, _ *mcp.CallToolRequest, args sessionRef) (*mcp.CallToolResult, session.StopResult, error) {
		result, err := mgr.Stop(args.SessionID)
		return nil, result, err
	})
}
