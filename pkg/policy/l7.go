// Package policy — HTTP L7 extraction primitives.
//
// This file provides side-effect-free helpers used by the builder when L7
// HTTP visibility is enabled (Plan 08-02 wires them into BuildPolicy). The
// helpers convert Hubble Flow.L7.Http records into Cilium PortRuleHTTP
// entries with normalized method casing (HTTP-02) and anchored regex paths
// produced via regexp.QuoteMeta (HTTP-03).
//
// HTTP-05 anti-feature contract: Headers, Host, and HeaderMatches are
// intentionally NEVER populated by extractHTTPRules. See REQUIREMENTS.md
// HTTP-05 — emitting these would risk leaking Authorization/Cookie tokens
// into committed policy YAML. A unit test in l7_test.go enforces this
// invariant.
package policy

import (
	"net/url"
	"regexp"
	"strings"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/cilium/cilium/pkg/policy/api"
)

// extractHTTPRules returns the PortRuleHTTP entries derived from the L7 HTTP
// record on the supplied flow. It is nil-safe: a flow with no L7, no HTTP
// record, or an empty method returns nil. The path component is parsed out
// of the wire URL (stripping scheme/host/query/fragment), then escaped via
// regexp.QuoteMeta and anchored with ^…$ so the emitted regex matches only
// the observed literal path.
func extractHTTPRules(f *flowpb.Flow) []api.PortRuleHTTP {
	if f == nil {
		return nil
	}
	http := f.GetL7().GetHttp()
	if http == nil {
		return nil
	}
	method := normalizeHTTPMethod(http.GetMethod())
	if method == "" {
		return nil
	}
	return []api.PortRuleHTTP{{
		Method: method,
		Path:   anchorPath(http.GetUrl()),
		// HTTP-05: Headers, Host, HeaderMatches deliberately left zero.
	}}
}

// extractDNSQuery returns the DNS matchName literal derived from the L7 DNS
// record on the supplied flow. The wire-format query carries a canonical
// trailing dot ("api.example.com."); this helper strips that suffix and any
// surrounding whitespace so callers can drop the result directly into a Cilium
// FQDNSelector.MatchName (Cilium re-adds the trailing dot internally).
//
// Nil-safety: a nil flow, missing L7 wrapper, missing DNS record, or
// empty/whitespace-only query yields ("", false). Callers drop the entry
// without erroring (DNS-01: empty/malformed query → no DNS rule).
//
// DNS-03 invariant: only matchName literals are extracted — no glob/wildcard
// inference happens here. The companion injector (companion_dns.go) likewise
// emits only matchName.
func extractDNSQuery(f *flowpb.Flow) (string, bool) {
	if f == nil {
		return "", false
	}
	dns := f.GetL7().GetDns()
	if dns == nil {
		return "", false
	}
	q := strings.TrimSpace(dns.GetQuery())
	q = strings.TrimSuffix(q, ".")
	q = strings.TrimSpace(q)
	if q == "" {
		return "", false
	}
	return q, true
}

// normalizeHTTPMethod uppercases and trims surrounding whitespace from the
// supplied HTTP method. Empty input (after trim) yields the empty string;
// callers drop the corresponding L7 entry rather than emit a method-less
// rule.
func normalizeHTTPMethod(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// anchorPath extracts the path component from a Hubble HTTP record's Url
// field, escapes regex metacharacters via regexp.QuoteMeta, and anchors the
// result with ^…$. Both bare paths ("/api/v1/users") and full URLs
// ("http://host/api?x=1") are handled. Empty input yields "^/$" so a
// root-path observation produces a valid regex rather than an empty rule.
func anchorPath(rawURL string) string {
	path := pathFromURL(rawURL)
	if path == "" {
		path = "/"
	}
	return "^" + regexp.QuoteMeta(path) + "$"
}

// pathFromURL returns the path component of rawURL, stripping any scheme,
// host, query, and fragment. On parse failure it falls back to a manual
// strip of '?' and '#' from the original input to preserve a usable path.
func pathFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err == nil {
		// url.Parse populates Path even for bare paths; query and fragment
		// are returned via separate accessors and excluded from Path.
		return u.Path
	}
	// Defensive fallback: manually trim query/fragment.
	if i := strings.IndexByte(rawURL, '#'); i >= 0 {
		rawURL = rawURL[:i]
	}
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		rawURL = rawURL[:i]
	}
	return rawURL
}
