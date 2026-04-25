package policy

import (
	"regexp"
	"testing"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/cilium/cilium/pkg/policy/api"
)

// httpFlow constructs a synthetic *flowpb.Flow whose L7 record carries the
// given HTTP method/url. Headers is intentionally exposed so the HTTP-05
// anti-feature lint test can prove header data is dropped.
func httpFlow(method, url string, headers []*flowpb.HTTPHeader) *flowpb.Flow {
	return &flowpb.Flow{
		L7: &flowpb.Layer7{
			Record: &flowpb.Layer7_Http{
				Http: &flowpb.HTTP{
					Method:  method,
					Url:     url,
					Headers: headers,
				},
			},
		},
	}
}

func TestExtractHTTPRules(t *testing.T) {
	tests := []struct {
		name string
		flow *flowpb.Flow
		want []api.PortRuleHTTP
	}{
		{
			name: "nil flow",
			flow: nil,
			want: nil,
		},
		{
			name: "flow with nil L7",
			flow: &flowpb.Flow{},
			want: nil,
		},
		{
			name: "flow with L7 but nil Http (DNS-only L7)",
			flow: &flowpb.Flow{L7: &flowpb.Layer7{}},
			want: nil,
		},
		{
			name: "GET root",
			flow: httpFlow("GET", "/", nil),
			want: []api.PortRuleHTTP{{Method: "GET", Path: "^/$"}},
		},
		{
			name: "GET /api/v1/users",
			flow: httpFlow("GET", "/api/v1/users", nil),
			want: []api.PortRuleHTTP{{Method: "GET", Path: "^/api/v1/users$"}},
		},
		{
			name: "POST /api/v1/users",
			flow: httpFlow("POST", "/api/v1/users", nil),
			want: []api.PortRuleHTTP{{Method: "POST", Path: "^/api/v1/users$"}},
		},
		{
			name: "lowercase method get",
			flow: httpFlow("get", "/api/v1/users", nil),
			want: []api.PortRuleHTTP{{Method: "GET", Path: "^/api/v1/users$"}},
		},
		{
			name: "mixed-case with whitespace",
			flow: httpFlow("  PoSt  ", "/api/v1/users", nil),
			want: []api.PortRuleHTTP{{Method: "POST", Path: "^/api/v1/users$"}},
		},
		{
			name: "empty method drops entry",
			flow: httpFlow("", "/api/v1/users", nil),
			want: nil,
		},
		{
			name: "empty url => root",
			flow: httpFlow("GET", "", nil),
			want: []api.PortRuleHTTP{{Method: "GET", Path: "^/$"}},
		},
		{
			name: "query string stripped",
			flow: httpFlow("GET", "/x?a=b", nil),
			want: []api.PortRuleHTTP{{Method: "GET", Path: "^/x$"}},
		},
		{
			name: "fragment stripped",
			flow: httpFlow("GET", "/x#frag", nil),
			want: []api.PortRuleHTTP{{Method: "GET", Path: "^/x$"}},
		},
		{
			name: "regex metachars escaped via QuoteMeta",
			flow: httpFlow("GET", "/api/v1.0/users", nil),
			want: []api.PortRuleHTTP{{Method: "GET", Path: `^/api/v1\.0/users$`}},
		},
		{
			name: "full URL strips scheme/host",
			flow: httpFlow("GET", "http://host/path?q=1", nil),
			want: []api.PortRuleHTTP{{Method: "GET", Path: "^/path$"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractHTTPRules(tc.flow)
			if len(got) != len(tc.want) {
				t.Fatalf("len(got)=%d want=%d; got=%+v", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i].Method != tc.want[i].Method || got[i].Path != tc.want[i].Path {
					t.Errorf("entry %d: got=%+v want=%+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestNormalizeHTTPMethod(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"GET", "GET"},
		{"get", "GET"},
		{"  PoSt  ", "POST"},
		{"", ""},
		{"   ", ""},
		{"Delete", "DELETE"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalizeHTTPMethod(tc.in); got != tc.want {
				t.Errorf("normalizeHTTPMethod(%q)=%q want=%q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExtractHTTPRules_NeverEmitsHeaders enforces HTTP-05: even when the
// inbound flow carries Headers, the emitted PortRuleHTTP must NEVER expose
// Headers, Host, HostExact, or HeaderMatches. This guards against accidental
// secret leakage (Authorization, Cookie) into committed YAML.
func TestExtractHTTPRules_NeverEmitsHeaders(t *testing.T) {
	headers := []*flowpb.HTTPHeader{
		{Key: "Authorization", Value: "Bearer secret-token"},
		{Key: "Cookie", Value: "session=deadbeef"},
		{Key: "User-Agent", Value: "curl/8.0"},
	}
	flow := httpFlow("GET", "/api/v1/users", headers)
	got := extractHTTPRules(flow)
	if len(got) == 0 {
		t.Fatalf("expected at least one entry, got 0")
	}
	for i, e := range got {
		if len(e.Headers) != 0 {
			t.Errorf("entry %d: Headers must be empty, got %+v", i, e.Headers)
		}
		if e.Host != "" {
			t.Errorf("entry %d: Host must be empty, got %q", i, e.Host)
		}
		if len(e.HeaderMatches) != 0 {
			t.Errorf("entry %d: HeaderMatches must be empty, got %+v", i, e.HeaderMatches)
		}
	}
}

// TestExtractHTTPRules_PathAnchored property test: for every emitted regex,
// it MUST match the literal observed path AND MUST NOT match the path with a
// prefix or suffix appended. This codifies HTTP-03 (regex anchoring).
func TestExtractHTTPRules_PathAnchored(t *testing.T) {
	cases := []struct {
		method, url, literal string
	}{
		{"GET", "/", "/"},
		{"GET", "/api/v1/users", "/api/v1/users"},
		{"POST", "/api/v1.0/users", "/api/v1.0/users"},
		{"GET", "/x?a=b", "/x"},
		{"GET", "/x#frag", "/x"},
		{"GET", "http://host/path?q=1", "/path"},
		{"GET", "", "/"},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			got := extractHTTPRules(httpFlow(tc.method, tc.url, nil))
			if len(got) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(got))
			}
			re, err := regexp.Compile(got[0].Path)
			if err != nil {
				t.Fatalf("invalid regex %q: %v", got[0].Path, err)
			}
			if !re.MatchString(tc.literal) {
				t.Errorf("regex %q must match literal %q", got[0].Path, tc.literal)
			}
			if re.MatchString("/evil" + tc.literal) {
				t.Errorf("regex %q must NOT match prefixed path %q", got[0].Path, "/evil"+tc.literal)
			}
			if re.MatchString(tc.literal + "/extra") {
				t.Errorf("regex %q must NOT match suffixed path %q", got[0].Path, tc.literal+"/extra")
			}
		})
	}
}
