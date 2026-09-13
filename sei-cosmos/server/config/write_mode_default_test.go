//go:build !mock_chain_validation

package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// serverConfigRecord names this build's defaults record. The two builds declare
// different defaults, so they keep separate records: a shared one would be
// rewritten by whichever build regenerated it last, and the value it lost is the
// one the reviewer needed to see.
const serverConfigRecord = "server_config"

// TestGetConfigLegacyMemiavlOnlyResolvesToAuto guards the existing-fleet
// upgrade path: a config written by an older binary carries an explicit
// sc-write-mode = "memiavl_only" but no sc-write-mode-enable-auto key. The absent
// key must default to true so the node resolves to auto and can follow a
// governance-driven migration without any app.toml edit.
func TestGetConfigLegacyMemiavlOnlyResolvesToAuto(t *testing.T) {
	v := viper.New()

	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})
	v.Set("state-commit.sc-write-mode", "memiavl_only")

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.True(t, cfg.StateCommit.WriteModeEnableAuto)
	require.Equal(t, sctypes.Auto, cfg.StateCommit.WriteMode,
		"absent sc-write-mode-enable-auto must default to true and override an explicit memiavl_only")
}

func TestGetConfigLegacyCosmosOnlyResolvesToAuto(t *testing.T) {
	v := viper.New()

	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})
	v.Set("state-commit.sc-write-mode", "cosmos_only")

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.True(t, cfg.StateCommit.WriteModeEnableAuto)
	require.Equal(t, sctypes.Auto, cfg.StateCommit.WriteMode,
		"v6.4/v6.5 app.toml files with cosmos_only must parse before auto mode is applied")
}

// TestGetConfigPinnedModeRequiresAutoDisabled verifies that an explicit
// sc-write-mode is only honored when sc-write-mode-enable-auto = false. With auto
// enabled (the default), the explicit mode is ignored and the node runs in auto.
func TestGetConfigPinnedModeRequiresAutoDisabled(t *testing.T) {
	for _, mode := range []sctypes.WriteMode{
		sctypes.FlatKVOnly,
		sctypes.EVMMigrated,
		sctypes.TestOnlyDualWrite,
	} {
		t.Run(string(mode)+"/auto-disabled-pins", func(t *testing.T) {
			v := viper.New()
			v.Set("minimum-gas-prices", DefaultMinGasPrices)
			v.Set("telemetry.global-labels", []interface{}{})
			v.Set("state-commit.sc-write-mode-enable-auto", false)
			v.Set("state-commit.sc-write-mode", string(mode))

			cfg, err := GetConfig(v)
			require.NoError(t, err)
			require.False(t, cfg.StateCommit.WriteModeEnableAuto)
			require.Equal(t, mode, cfg.StateCommit.WriteMode,
				"with auto disabled the explicit mode must be honored as a pin")
		})

		t.Run(string(mode)+"/auto-enabled-overrides", func(t *testing.T) {
			v := viper.New()
			v.Set("minimum-gas-prices", DefaultMinGasPrices)
			v.Set("telemetry.global-labels", []interface{}{})
			v.Set("state-commit.sc-write-mode", string(mode))

			cfg, err := GetConfig(v)
			require.NoError(t, err)
			require.True(t, cfg.StateCommit.WriteModeEnableAuto)
			require.Equal(t, sctypes.Auto, cfg.StateCommit.WriteMode,
				"with auto enabled (default) the explicit mode must be ignored in favor of auto")
		})
	}
}

func TestGetConfigEmptyWriteModeUsesDefault(t *testing.T) {
	v := viper.New()

	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.Equal(t, sctypes.Auto, cfg.StateCommit.WriteMode,
		"unset sc-write-mode must fall back to the in-code default")
}

func TestDefaultStateCommitConfig(t *testing.T) {
	cfg := DefaultConfig()

	require.True(t, cfg.StateCommit.Enable)
	require.Empty(t, cfg.StateCommit.Directory)
	// WriteMode is the fixed fallback (memiavl_only); WriteModeEnableAuto
	// defaults true, so the effective default after resolution is auto.
	require.Equal(t, sctypes.MemiavlOnly, cfg.StateCommit.WriteMode)
	require.True(t, cfg.StateCommit.WriteModeEnableAuto)
}
