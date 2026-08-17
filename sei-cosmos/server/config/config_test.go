package config

import (
	"math"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.True(t, cfg.GetMinGasPrices().IsZero())
}

func TestSetMinimumFees(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SetMinGasPrices(sdk.DecCoins{sdk.NewInt64DecCoin("foo", 5)})
	require.Equal(t, "5.000000000000000000foo", cfg.MinGasPrices)
}

func TestSetSnapshotDirectory(t *testing.T) {
	cfg := DefaultConfig()
	require.Equal(t, "", cfg.StateSync.SnapshotDirectory)
}

func TestSetConcurrencyWorkers(t *testing.T) {
	cfg := DefaultConfig()
	require.Equal(t, DefaultConcurrencyWorkers, cfg.ConcurrencyWorkers)
}

func TestOCCEnabled(t *testing.T) {
	cfg := DefaultConfig()
	require.False(t, cfg.OccEnabled)

	cfg.BaseConfig.OccEnabled = true
	require.True(t, cfg.OccEnabled)
}

func TestValidateFreeze(t *testing.T) {
	tests := []struct {
		name         string
		freezeHeight uint64
		haltHeight   uint64
		haltTime     uint64
		wantErr      string
	}{
		{name: "disabled"},
		{name: "enabled", freezeHeight: 100},
		{name: "height overflow", freezeHeight: uint64(math.MaxInt64) + 1, wantErr: "freeze-height must not exceed"},
		{name: "halt height", freezeHeight: 100, haltHeight: 100, wantErr: "cannot be combined"},
		{name: "halt time", freezeHeight: 100, haltTime: 100, wantErr: "cannot be combined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.FreezeHeight = tt.freezeHeight
			cfg.HaltHeight = tt.haltHeight
			cfg.HaltTime = tt.haltTime
			err := cfg.ValidateFreeze()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestGetConfigRejectsNegativeFreezeHeight(t *testing.T) {
	v := viper.New()
	v.Set("telemetry.global-labels", []interface{}{})
	v.Set("freeze-height", -1)

	_, err := GetConfig(v)
	require.Error(t, err)
}
