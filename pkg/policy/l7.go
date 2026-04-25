// Package policy — HTTP L7 extraction primitives.
//
// This file provides side-effect-free helpers used by the builder when L7
// HTTP visibility is enabled (Plan 08-02 wires them into BuildPolicy). The
// helpers convert Hubble Flow.L7.Http records into Cilium PortRuleHTTP
// entries with normalized method casing (HTTP-02) and anchored regex paths
// produced via regexp.QuoteMeta (HTTP-03).
//
// HTTP-05 anti-feature contract: Headers, Host, HostExact, and HeaderMatches
// are intentionally NEVER populated by extractHTTPRules. See REQUIREMENTS.md
// HTTP-05 — emitting these would risk leaking Authorization/Cookie tokens
// into committed policy YAML. A unit test in l7_test.go enforces this
// invariant.
package policy

import (
	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/cilium/cilium/pkg/policy/api"
)

// extractHTTPRules returns the PortRuleHTTP entries derived from the L7 HTTP
// record on the supplied flow. Returns nil-safe empty slice when the flow
// carries no HTTP record. (Stub — implemented in Task 2.)
func extractHTTPRules(f *flowpb.Flow) []api.PortRuleHTTP {
	return nil
}

// normalizeHTTPMethod returns the HTTP method in uppercase with surrounding
// whitespace trimmed. (Stub — implemented in Task 2.)
func normalizeHTTPMethod(s string) string {
	return s
}
