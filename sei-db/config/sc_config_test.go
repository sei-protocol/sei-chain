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

func TestAlignFlatKVWithMemIAVL(t *testing.T) {
	t.Run("mirrors memIAVL snapshot cadence onto FlatKV", func(t *testing.T) {
		cfg := DefaultStateCommitConfig()
		cfg.MemIAVLConfig.SnapshotInterval = 5000
		cfg.MemIAVLConfig.SnapshotKeepRecent = 3
		// Start FlatKV from divergent values to prove they get overwritten.
		cfg.FlatKVConfig.SnapshotInterval = 111
		cfg.FlatKVConfig.SnapshotKeepRecent = 222

		cfg.AlignFlatKVWithMemIAVL()

		require.Equal(t, uint32(5000), cfg.FlatKVConfig.SnapshotInterval)
		require.Equal(t, uint32(3), cfg.FlatKVConfig.SnapshotKeepRecent)
	})

	t.Run("floors keep-recent of 0 to 1 for both backends", func(t *testing.T) {
		cfg := DefaultStateCommitConfig()
		cfg.MemIAVLConfig.SnapshotKeepRecent = 0

		cfg.AlignFlatKVWithMemIAVL()

		require.Equal(t, uint32(1), cfg.MemIAVLConfig.SnapshotKeepRecent)
		require.Equal(t, uint32(1), cfg.FlatKVConfig.SnapshotKeepRecent)
	})
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
