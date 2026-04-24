package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
