//go:build mock_chain_validation

package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// These records mirror write_mode_default_test.go for the reserve build, where
// sc-write-mode-enable-auto defaults to false rather than true. The rule is
// unchanged — an absent key takes the in-code default, and the default decides
// whether an explicit sc-write-mode is honored — so every resolution below is
// the opposite of the stock one for the same input.

// serverConfigRecord names this build's defaults record, separate from the stock
// build's so both declared values stay readable side by side.
const serverConfigRecord = "server_config.reserve"

func TestGetConfigLegacyMemiavlOnlyIsPinnedOnAReserveBuild(t *testing.T) {
	v := viper.New()

	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})
	v.Set("state-commit.sc-write-mode", "memiavl_only")

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.False(t, cfg.StateCommit.WriteModeEnableAuto)
	require.Equal(t, sctypes.MemiavlOnly, cfg.StateCommit.WriteMode,
		"absent sc-write-mode-enable-auto must default to false and honor the explicit memiavl_only")
}

func TestGetConfigLegacyCosmosOnlyIsPinnedOnAReserveBuild(t *testing.T) {
	v := viper.New()

	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})
	v.Set("state-commit.sc-write-mode", "cosmos_only")

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.False(t, cfg.StateCommit.WriteModeEnableAuto)
	require.Equal(t, sctypes.MemiavlOnly, cfg.StateCommit.WriteMode,
		"v6.4/v6.5 app.toml files with cosmos_only must parse to memiavl_only and then be honored")
}

// TestGetConfigPinnedModeNeedsNoAutoKeyOnAReserveBuild is the inverse of the
// stock TestGetConfigPinnedModeRequiresAutoDisabled: pinning needs no key here,
// and setting the key to true is what un-pins the node. assertReserveNodeAllowed
// refuses to start in that state.
func TestGetConfigPinnedModeNeedsNoAutoKeyOnAReserveBuild(t *testing.T) {
	for _, mode := range []sctypes.WriteMode{
		sctypes.FlatKVOnly,
		sctypes.EVMMigrated,
		sctypes.TestOnlyDualWrite,
	} {
		t.Run(string(mode)+"/auto-key-absent-pins", func(t *testing.T) {
			v := viper.New()
			v.Set("minimum-gas-prices", DefaultMinGasPrices)
			v.Set("telemetry.global-labels", []interface{}{})
			v.Set("state-commit.sc-write-mode", string(mode))

			cfg, err := GetConfig(v)
			require.NoError(t, err)
			require.False(t, cfg.StateCommit.WriteModeEnableAuto)
			require.Equal(t, mode, cfg.StateCommit.WriteMode,
				"with auto defaulted off the explicit mode must be honored as a pin")
		})

		t.Run(string(mode)+"/auto-enabled-overrides", func(t *testing.T) {
			v := viper.New()
			v.Set("minimum-gas-prices", DefaultMinGasPrices)
			v.Set("telemetry.global-labels", []interface{}{})
			v.Set("state-commit.sc-write-mode-enable-auto", true)
			v.Set("state-commit.sc-write-mode", string(mode))

			cfg, err := GetConfig(v)
			require.NoError(t, err)
			require.True(t, cfg.StateCommit.WriteModeEnableAuto)
			require.Equal(t, sctypes.Auto, cfg.StateCommit.WriteMode,
				"an explicit sc-write-mode-enable-auto = true must still win over the default")
		})
	}
}

func TestGetConfigEmptyWriteModeUsesDefaultOnAReserveBuild(t *testing.T) {
	v := viper.New()

	v.Set("minimum-gas-prices", DefaultMinGasPrices)
	v.Set("telemetry.global-labels", []interface{}{})

	cfg, err := GetConfig(v)
	require.NoError(t, err)
	require.Equal(t, sctypes.MemiavlOnly, cfg.StateCommit.WriteMode,
		"an app.toml carrying neither key must resolve to a pinned memiavl_only, "+
			"which is what lets a reserve start with no configuration at all")
}

func TestDefaultStateCommitConfigOnAReserveBuild(t *testing.T) {
	cfg := DefaultConfig()

	require.True(t, cfg.StateCommit.Enable)
	require.Empty(t, cfg.StateCommit.Directory)
	// WriteMode is the same fixed fallback as the stock build; only
	// WriteModeEnableAuto moves, and it is what makes that fallback effective.
	require.Equal(t, sctypes.MemiavlOnly, cfg.StateCommit.WriteMode)
	require.False(t, cfg.StateCommit.WriteModeEnableAuto)
}
