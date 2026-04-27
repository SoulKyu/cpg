package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// newWarnObserver returns a zap.Logger wired to an observer that captures
// messages at WarnLevel and above. Used to assert FILTER-03 WARN emission.
func newWarnObserver() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.WarnLevel)
	return zap.New(core), logs
}

// TestValidateIgnoreDropReasonsEmpty confirms nil/empty input is a no-op.
func TestValidateIgnoreDropReasonsEmpty(t *testing.T) {
	logger, _ := newWarnObserver()

	got, err := validateIgnoreDropReasons(nil, logger)
	require.NoError(t, err)
	assert.Nil(t, got)

	got, err = validateIgnoreDropReasons([]string{}, logger)
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestValidateIgnoreDropReasonsValid confirms a known uppercase reason name
// passes validation and is returned unchanged (already canonical).
func TestValidateIgnoreDropReasonsValid(t *testing.T) {
	logger, logs := newWarnObserver()

	got, err := validateIgnoreDropReasons([]string{"CT_MAP_INSERTION_FAILED"}, logger)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "CT_MAP_INSERTION_FAILED", got[0])
	// CT_MAP_INSERTION_FAILED is Infra — FILTER-03 WARN expected (tested separately)
	_ = logs
}

// TestValidateIgnoreDropReasonsCaseInsensitive confirms lowercase input is
// normalized to the canonical uppercase enum name.
func TestValidateIgnoreDropReasonsCaseInsensitive(t *testing.T) {
	logger, _ := newWarnObserver()

	got, err := validateIgnoreDropReasons([]string{"ct_map_insertion_failed"}, logger)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "CT_MAP_INSERTION_FAILED", got[0])
}

// TestValidateIgnoreDropReasonsCommaSeparated confirms that two valid reasons
// (already split by StringSlice) are both normalized and returned.
func TestValidateIgnoreDropReasonsCommaSeparated(t *testing.T) {
	logger, _ := newWarnObserver()

	got, err := validateIgnoreDropReasons([]string{"POLICY_DENIED", "policy_deny"}, logger)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "POLICY_DENIED", got[0])
	assert.Equal(t, "POLICY_DENY", got[1])
}

// TestValidateIgnoreDropReasonsUnknown confirms that an unrecognized reason
// name returns an error containing "unknown drop reason".
func TestValidateIgnoreDropReasonsUnknown(t *testing.T) {
	logger, _ := newWarnObserver()

	got, err := validateIgnoreDropReasons([]string{"TOTALLY_MADE_UP"}, logger)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "unknown drop reason")
}

// TestValidateIgnoreDropReasonsRedundantWarn confirms that passing an Infra-
// classified reason emits a WARN containing "redundant" but no error.
// CT_MAP_INSERTION_FAILED is Infra (dropclass.DropClassInfra).
func TestValidateIgnoreDropReasonsRedundantWarn(t *testing.T) {
	logger, logs := newWarnObserver()

	got, err := validateIgnoreDropReasons([]string{"CT_MAP_INSERTION_FAILED"}, logger)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "CT_MAP_INSERTION_FAILED", got[0])

	require.Equal(t, 1, logs.Len(), "expected exactly one WARN log")
	assert.Contains(t, logs.All()[0].Message, "redundant")
}

// TestValidateIgnoreDropReasonsRedundantTransient confirms that a Transient-
// classified reason also triggers FILTER-03 WARN.
// TTL_EXCEEDED is Transient (dropclass.DropClassTransient).
func TestValidateIgnoreDropReasonsRedundantTransient(t *testing.T) {
	logger, logs := newWarnObserver()

	got, err := validateIgnoreDropReasons([]string{"TTL_EXCEEDED"}, logger)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "TTL_EXCEEDED", got[0])

	require.Equal(t, 1, logs.Len(), "expected exactly one WARN log")
	assert.Contains(t, logs.All()[0].Message, "redundant")
}

// TestValidateIgnoreDropReasonsPolicyNoWarn confirms that a Policy-classified
// reason does NOT emit a WARN (it's non-redundant — user wants to ignore even
// policy drops, which is a valid advanced use case).
// POLICY_DENIED is Policy (dropclass.DropClassPolicy).
func TestValidateIgnoreDropReasonsPolicyNoWarn(t *testing.T) {
	logger, logs := newWarnObserver()

	got, err := validateIgnoreDropReasons([]string{"POLICY_DENIED"}, logger)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "POLICY_DENIED", got[0])

	assert.Equal(t, 0, logs.Len(), "expected no WARN for Policy-classified reason")
}

// TestIgnoreDropReasonFlagParses confirms the --ignore-drop-reason flag is
// registered on both generate and replay subcommands.
func TestIgnoreDropReasonFlagParses(t *testing.T) {
	t.Run("generate", func(t *testing.T) {
		cmd := newGenerateCmd()
		require.NoError(t, cmd.Flags().Set("ignore-drop-reason", "POLICY_DENIED"))
		f := parseCommonFlags(cmd)
		assert.Equal(t, []string{"POLICY_DENIED"}, f.ignoreDropReasons)
	})

	t.Run("replay", func(t *testing.T) {
		cmd := newReplayCmd()
		require.NoError(t, cmd.Flags().Set("ignore-drop-reason", "POLICY_DENIED"))
		f := parseCommonFlags(cmd)
		assert.Equal(t, []string{"POLICY_DENIED"}, f.ignoreDropReasons)
	})
}

// TestLevenshtein verifies the edit distance helper with known pairs.
func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"CT_MAP_INSERT_FAIL", "CT_MAP_INSERTION_FAILED", 5},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, levenshtein(tc.a, tc.b), "levenshtein(%q, %q)", tc.a, tc.b)
	}
}

// TestSuggestClosest verifies that suggestClosest returns at most n candidates
// in ascending distance order, with ties broken lexicographically.
func TestSuggestClosest(t *testing.T) {
	candidates := []string{"CT_MAP_INSERTION_FAILED", "POLICY_DENIED", "STALE_OR_UNROUTABLE_IP", "CT_NO_MAP_FOUND", "SERVICE_BACKEND_NOT_FOUND"}

	// CT_MAP_INSERT_FAIL is closest to CT_MAP_INSERTION_FAILED
	got := suggestClosest("CT_MAP_INSERT_FAIL", candidates, 5)
	require.LessOrEqual(t, len(got), 5, "must return at most n suggestions")
	require.Greater(t, len(got), 0, "must return at least 1 suggestion")
	assert.Equal(t, "CT_MAP_INSERTION_FAILED", got[0], "closest match must be first")

	// With the I-4 threshold (min(10, len/2+2)), only CT_MAP_INSERTION_FAILED
	// (dist=5) passes for this candidate set; requesting n=2 returns at most 1.
	got2 := suggestClosest("CT_MAP_INSERT_FAIL", candidates, 2)
	assert.LessOrEqual(t, len(got2), 2, "must return at most n suggestions")
}

// TestValidateIgnoreDropReasonsLevenshtein verifies I3: error message for an
// unknown reason name lists up to 5 fuzzy-matched suggestions (not all 76+).
func TestValidateIgnoreDropReasonsLevenshtein(t *testing.T) {
	logger, _ := newWarnObserver()

	_, err := validateIgnoreDropReasons([]string{"CT_MAP_INSERT_FAIL"}, logger)
	require.Error(t, err)
	errMsg := err.Error()

	// Must contain Levenshtein suggestion text.
	assert.Contains(t, errMsg, "did you mean any of:", "error must list suggestions")
	assert.Contains(t, errMsg, "CT_MAP_INSERTION_FAILED", "closest match must appear in suggestions")

	// Error message must be bounded (not listing all 76+ reasons).
	assert.Less(t, len(errMsg), 500, "error message must be bounded (<500 chars)")

	// Extract suggestions: text between "did you mean any of: " and "?"
	start := strings.Index(errMsg, "did you mean any of: ")
	if start >= 0 {
		rest := errMsg[start+len("did you mean any of: "):]
		end := strings.Index(rest, "?")
		if end >= 0 {
			suggList := strings.Split(rest[:end], ", ")
			assert.LessOrEqual(t, len(suggList), 5, "at most 5 suggestions must be listed")
		}
	}
}

// TestPreRunE_RejectsInvalidDropReason verifies I2: PreRunE on generate rejects
// an invalid --ignore-drop-reason before any pipeline construction.
func TestPreRunE_RejectsInvalidDropReason(t *testing.T) {
	cmd := newGenerateCmd()
	require.NoError(t, cmd.Flags().Set("ignore-drop-reason", "TYPO_REASON"))
	// Call PreRunE directly (RunE is not invoked).
	require.NotNil(t, cmd.PreRunE, "generate cmd must have PreRunE wired")
	err := cmd.PreRunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown drop reason")
}

// TestPreRunE_RejectsInvalidProtocol verifies I2: PreRunE rejects an invalid
// --ignore-protocol before any pipeline construction.
func TestPreRunE_RejectsInvalidProtocol(t *testing.T) {
	cmd := newReplayCmd()
	require.NoError(t, cmd.Flags().Set("ignore-protocol", "tcpp"))
	require.NotNil(t, cmd.PreRunE, "replay cmd must have PreRunE wired")
	err := cmd.PreRunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown protocol")
}

// TestPreRunE_ValidFlagsPass verifies I2: PreRunE returns nil for valid flags.
func TestPreRunE_ValidFlagsPass(t *testing.T) {
	cmd := newGenerateCmd()
	require.NoError(t, cmd.Flags().Set("ignore-drop-reason", "POLICY_DENIED"))
	require.NotNil(t, cmd.PreRunE, "generate cmd must have PreRunE wired")
	err := cmd.PreRunE(cmd, nil)
	require.NoError(t, err)
}

// TestValidateIgnoreDropReasons_NoSuggestionsForGarbage verifies I-4/I-5: when
// the input is so far from all known DropReason names that no candidate passes
// the distance threshold, the error omits the "did you mean" clause entirely.
// "ZZZZZ" has length 5 → threshold = min(10, 5/2+2) = min(10, 4) = 4.
// All real DropReason names (e.g. CT_MAP_INSERTION_FAILED) are far more than 4
// edits away from "ZZZZZ", so suggestions is empty.
func TestValidateIgnoreDropReasons_NoSuggestionsForGarbage(t *testing.T) {
	logger, _ := newWarnObserver()

	_, err := validateIgnoreDropReasons([]string{"ZZZZZ"}, logger)
	require.Error(t, err)
	errMsg := err.Error()

	assert.Contains(t, errMsg, "unknown drop reason", "error must contain 'unknown drop reason'")
	assert.Contains(t, errMsg, "https://docs.cilium.io", "error must contain the docs URL")
	assert.NotContains(t, errMsg, "did you mean", "garbage input must produce no suggestions clause")
}

// TestLevenshtein_Unicode verifies I-3: the levenshtein helper counts runes, not
// bytes, so a single multi-byte character substitution costs 1 (not 2+).
func TestLevenshtein_Unicode(t *testing.T) {
	// "é" is 2 bytes (U+00E9) — byte-based impl would return ≥ 2.
	assert.Equal(t, 1, levenshtein("café", "cafe"), "single-rune substitution must cost 1")
	assert.Equal(t, 0, levenshtein("café", "café"), "identity must cost 0")
	assert.Equal(t, 1, levenshtein("naïve", "naive"), "single-rune substitution (ï→i) must cost 1")
}

// TestFailOnInfraDropsFlagParses confirms the --fail-on-infra-drops flag is
// registered on both generate and replay subcommands.
func TestFailOnInfraDropsFlagParses(t *testing.T) {
	t.Run("generate", func(t *testing.T) {
		cmd := newGenerateCmd()
		require.NoError(t, cmd.Flags().Set("fail-on-infra-drops", "true"))
		f := parseCommonFlags(cmd)
		assert.True(t, f.failOnInfraDrops)
	})

	t.Run("replay", func(t *testing.T) {
		cmd := newReplayCmd()
		require.NoError(t, cmd.Flags().Set("fail-on-infra-drops", "true"))
		f := parseCommonFlags(cmd)
		assert.True(t, f.failOnInfraDrops)
	})
}
