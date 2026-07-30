package config

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

func TestApplyWriteModeAuto(t *testing.T) {
	tests := []struct {
		name       string
		enableAuto bool
		mode       types.WriteMode
		want       types.WriteMode
	}{
		// When auto is enabled the explicit mode is always ignored in favor of
		// auto, regardless of what was configured.
		{"auto on + memiavl_only -> auto", true, types.MemiavlOnly, types.Auto},
		{"auto on + flatkv_only -> auto", true, types.FlatKVOnly, types.Auto},
		{"auto on + evm_migrated -> auto", true, types.EVMMigrated, types.Auto},
		{"auto on + test_only_dual_write -> auto", true, types.TestOnlyDualWrite, types.Auto},
		{"auto on + auto -> auto", true, types.Auto, types.Auto},
		// When auto is disabled the explicit mode is honored as a deliberate pin.
		{"auto off + memiavl_only -> pinned", false, types.MemiavlOnly, types.MemiavlOnly},
		{"auto off + flatkv_only -> pinned", false, types.FlatKVOnly, types.FlatKVOnly},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ApplyWriteModeAuto(tc.enableAuto, tc.mode))
		})
	}
}

func TestDefaultStateCommitConfigWriteMode(t *testing.T) {
	cfg := DefaultStateCommitConfig()
	// The raw default is the fixed fallback; auto comes from WriteModeEnableAuto
	// via ApplyWriteModeAuto at the config-parse boundary.
	require.Equal(t, types.MemiavlOnly, cfg.WriteMode)
	require.True(t, cfg.WriteModeEnableAuto)
}

func TestParseSCWriteMode(t *testing.T) {
	parsed, err := ParseSCWriteMode("cosmos_only")
	require.NoError(t, err)
	require.Equal(t, types.MemiavlOnly, parsed)

	parsed, err = ParseSCWriteMode("migrate_evm")
	require.NoError(t, err)
	require.Equal(t, types.MigrateEVM, parsed)

	_, err = ParseSCWriteMode("bogus")
	require.Error(t, err)
}
