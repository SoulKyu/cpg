package main

import (
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
