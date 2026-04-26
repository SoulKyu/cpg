package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestGenerateFlags_Validate(t *testing.T) {
	tests := []struct {
		name    string
		flags   generateFlags
		wantErr string
	}{
		{
			name:  "no namespace filters is valid",
			flags: generateFlags{},
		},
		{
			name:  "namespace filter only is valid",
			flags: generateFlags{commonFlags: commonFlags{namespaces: []string{"prod"}}},
		},
		{
			name:  "all-namespaces only is valid",
			flags: generateFlags{commonFlags: commonFlags{allNamespaces: true}},
		},
		{
			name:    "namespace + all-namespaces is rejected",
			flags:   generateFlags{commonFlags: commonFlags{namespaces: []string{"prod"}, allNamespaces: true}},
			wantErr: "mutually exclusive",
		},
		{
			name:    "multiple namespaces + all-namespaces is rejected",
			flags:   generateFlags{commonFlags: commonFlags{namespaces: []string{"prod", "staging"}, allNamespaces: true}},
			wantErr: "mutually exclusive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.flags.validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestGenerateFlags_ClusterDedupNamespaces(t *testing.T) {
	tests := []struct {
		name  string
		flags generateFlags
		want  []string
	}{
		{
			name:  "no filters yields cluster-wide listing",
			flags: generateFlags{},
			want:  []string{""},
		},
		{
			name:  "all-namespaces yields cluster-wide listing",
			flags: generateFlags{commonFlags: commonFlags{allNamespaces: true}},
			want:  []string{""},
		},
		{
			name:  "single namespace filter is passed through",
			flags: generateFlags{commonFlags: commonFlags{namespaces: []string{"prod"}}},
			want:  []string{"prod"},
		},
		{
			name:  "multiple namespace filters are passed through",
			flags: generateFlags{commonFlags: commonFlags{namespaces: []string{"prod", "staging"}}},
			want:  []string{"prod", "staging"},
		},
		{
			name:  "all-namespaces wins over namespace filter",
			flags: generateFlags{commonFlags: commonFlags{namespaces: []string{"prod"}, allNamespaces: true}},
			want:  []string{""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.flags.clusterDedupNamespaces()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseGenerateFlags_Defaults(t *testing.T) {
	cmd := newGenerateCmd()
	f := parseGenerateFlags(cmd)

	assert.Empty(t, f.server)
	assert.Empty(t, f.namespaces)
	assert.False(t, f.allNamespaces)
	assert.Equal(t, "./policies", f.outputDir)
	assert.False(t, f.tlsEnabled)
	assert.False(t, f.clusterDedup)
	assert.NotZero(t, f.flushInterval)
	assert.NotZero(t, f.timeout)
}

func TestParseGenerateFlags_Overrides(t *testing.T) {
	cmd := newGenerateCmd()
	require.NoError(t, cmd.Flags().Set("server", "relay.example.com:443"))
	require.NoError(t, cmd.Flags().Set("tls", "true"))
	require.NoError(t, cmd.Flags().Set("all-namespaces", "true"))
	require.NoError(t, cmd.Flags().Set("output-dir", "/tmp/out"))
	require.NoError(t, cmd.Flags().Set("cluster-dedup", "true"))
	require.NoError(t, cmd.Flags().Set("namespace", "prod"))
	require.NoError(t, cmd.Flags().Set("namespace", "staging"))

	f := parseGenerateFlags(cmd)
	assert.Equal(t, "relay.example.com:443", f.server)
	assert.True(t, f.tlsEnabled)
	assert.True(t, f.allNamespaces)
	assert.Equal(t, "/tmp/out", f.outputDir)
	assert.True(t, f.clusterDedup)
	assert.Equal(t, []string{"prod", "staging"}, f.namespaces)

	err := f.validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestParseGenerateFlags_L7 covers the L7CLI-01 + VIS-06 flag parsing surface
// on `cpg generate`: --l7 and --no-l7-preflight default to false, set to true
// independently, and both can be combined.
func TestParseGenerateFlags_L7(t *testing.T) {
	t.Run("defaults are false", func(t *testing.T) {
		cmd := newGenerateCmd()
		f := parseGenerateFlags(cmd)
		assert.False(t, f.l7)
		assert.False(t, f.noL7Preflight)
	})

	t.Run("--l7 alone", func(t *testing.T) {
		cmd := newGenerateCmd()
		require.NoError(t, cmd.Flags().Set("l7", "true"))
		f := parseGenerateFlags(cmd)
		assert.True(t, f.l7)
		assert.False(t, f.noL7Preflight)
	})

	t.Run("--no-l7-preflight alone", func(t *testing.T) {
		cmd := newGenerateCmd()
		require.NoError(t, cmd.Flags().Set("no-l7-preflight", "true"))
		f := parseGenerateFlags(cmd)
		assert.False(t, f.l7)
		assert.True(t, f.noL7Preflight)
	})

	t.Run("--l7 + --no-l7-preflight", func(t *testing.T) {
		cmd := newGenerateCmd()
		require.NoError(t, cmd.Flags().Set("l7", "true"))
		require.NoError(t, cmd.Flags().Set("no-l7-preflight", "true"))
		f := parseGenerateFlags(cmd)
		assert.True(t, f.l7)
		assert.True(t, f.noL7Preflight)
	})
}

// observedLoggerForTest builds a zap logger that captures entries at WarnLevel+
// so tests can assert on RunL7Preflight's emitted warnings.
func observedLoggerForTest() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.WarnLevel)
	return zap.New(core), logs
}

// withFakeL7ClientFactory swaps the package-level l7ClientFactory for the
// duration of a test. The substitute returns the supplied fake clientset
// regardless of the *rest.Config it is handed.
func withFakeL7ClientFactory(t *testing.T, client kubernetes.Interface) {
	t.Helper()
	prev := l7ClientFactory
	l7ClientFactory = func(*rest.Config) (kubernetes.Interface, error) {
		return client, nil
	}
	t.Cleanup(func() { l7ClientFactory = prev })
}

// TestMaybeRunL7Preflight_Gating asserts the gate matrix:
//   - --l7 unset                     → preflight NOT invoked
//   - --l7 set, --no-l7-preflight    → preflight NOT invoked (VIS-06 wins)
//   - --l7 set, no skip flag         → preflight INVOKED (VIS-04 warning visible)
func TestMaybeRunL7Preflight_Gating(t *testing.T) {
	// Misconfigured cilium-config: enable-l7-proxy is "false" → triggers VIS-04 warning.
	misconfigured := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cilium-config", Namespace: "kube-system"},
		Data:       map[string]string{"enable-l7-proxy": "false"},
	}

	cases := []struct {
		name           string
		l7Enabled      bool
		noPreflight    bool
		wantWarnSubstr string // empty → expect no warnings at all
	}{
		{name: "l7=false noPreflight=false → skipped", l7Enabled: false, noPreflight: false, wantWarnSubstr: ""},
		{name: "l7=false noPreflight=true  → skipped", l7Enabled: false, noPreflight: true, wantWarnSubstr: ""},
		{name: "l7=true  noPreflight=true  → skipped (VIS-06 wins)", l7Enabled: true, noPreflight: true, wantWarnSubstr: ""},
		{name: "l7=true  noPreflight=false → invoked (VIS-04 warning)", l7Enabled: true, noPreflight: false, wantWarnSubstr: "enable-l7-proxy"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(misconfigured)
			withFakeL7ClientFactory(t, client)

			lg, logs := observedLoggerForTest()
			// kubeConfig is never nil-loaded in this path because the factory
			// short-circuits — but the gate still calls LoadKubeConfig when
			// kubeConfig is nil. Pass an empty *rest.Config to avoid that.
			maybeRunL7Preflight(context.Background(), &rest.Config{}, tc.l7Enabled, tc.noPreflight, lg)

			if tc.wantWarnSubstr == "" {
				assert.Equal(t, 0, logs.Len(), "no warnings expected")
				return
			}
			require.Greater(t, logs.Len(), 0, "expected at least one warning")
			joined := ""
			for _, e := range logs.All() {
				joined += e.Message + "\n"
			}
			assert.Contains(t, joined, tc.wantWarnSubstr)
		})
	}
}

// TestParseCommonFlags_IgnoreProtocol_CaseInsensitiveAndCommaSep asserts the
// flag is repeatable + comma-separated and parseCommonFlags surfaces the raw
// input verbatim (normalization happens in validateIgnoreProtocols).
func TestParseCommonFlags_IgnoreProtocol_CaseInsensitiveAndCommaSep(t *testing.T) {
	cmd := newGenerateCmd()
	require.NoError(t, cmd.Flags().Set("ignore-protocol", "ICMPv4,icmpv6"))
	require.NoError(t, cmd.Flags().Set("ignore-protocol", "TCP"))

	f := parseCommonFlags(cmd)
	assert.Equal(t, []string{"ICMPv4", "icmpv6", "TCP"}, f.ignoreProtocols)
}

// TestValidateIgnoreProtocols_Normalization asserts mixed-case input is
// lower-cased and order is preserved.
func TestValidateIgnoreProtocols_Normalization(t *testing.T) {
	got, err := validateIgnoreProtocols([]string{"ICMPv4", "Tcp", "icmpv6"})
	require.NoError(t, err)
	assert.Equal(t, []string{"icmpv4", "tcp", "icmpv6"}, got)
}

// TestValidateIgnoreProtocols_UnknownReturnsError asserts the error message
// names the offending value AND the sorted allowlist.
func TestValidateIgnoreProtocols_UnknownReturnsError(t *testing.T) {
	_, err := validateIgnoreProtocols([]string{"foo"})
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "foo")
	assert.Contains(t, msg, "icmpv4, icmpv6, sctp, tcp, udp")
}

// TestValidateIgnoreProtocols_EmptyIsNoOp asserts nil input returns (nil, nil).
func TestValidateIgnoreProtocols_EmptyIsNoOp(t *testing.T) {
	got, err := validateIgnoreProtocols(nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestReplayCmd_RejectsNoL7PreflightFlag asserts that --no-l7-preflight is
// generate-only. Cobra surfaces unknown flags via SetArgs+Execute.
func TestReplayCmd_RejectsNoL7PreflightFlag(t *testing.T) {
	cmd := newReplayCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--no-l7-preflight", "../../testdata/flows/small.jsonl"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-l7-preflight")
}
