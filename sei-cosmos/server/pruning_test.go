package server

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

func TestGetPruningOptionsFromFlags(t *testing.T) {
	tests := []struct {
		name            string
		initParams      func() *viper.Viper
		expectedOptions types.PruningOptions
		wantErr         bool
	}{
		{
			name: FlagPruning,
			initParams: func() *viper.Viper {
				v := viper.New()
				v.Set(FlagPruning, types.PruningOptionNothing)
				return v
			},
			expectedOptions: types.PruneNothing,
		},
		{
			name: FlagIAVLPruning,
			initParams: func() *viper.Viper {
				v := viper.New()
				v.Set(FlagIAVLPruning, types.PruningOptionNothing)
				return v
			},
			expectedOptions: types.PruneNothing,
		},
		{
			name: "custom pruning options",
			initParams: func() *viper.Viper {
				v := viper.New()
				v.Set(FlagPruning, types.PruningOptionCustom)
				v.Set(FlagPruningKeepRecent, 1234)
				v.Set(FlagPruningKeepEvery, 4321)
				v.Set(FlagPruningInterval, 10)

				return v
			},
			expectedOptions: types.PruningOptions{
				KeepRecent: 1234,
				KeepEvery:  4321,
				Interval:   10,
			},
		},
		{
			name: "custom pruning options iavl",
			initParams: func() *viper.Viper {
				v := viper.New()
				v.Set(FlagIAVLPruning, types.PruningOptionCustom)
				v.Set(FlagIAVLPruningKeepRecent, 1234)
				v.Set(FlagIAVLPruningKeepEvery, 4321)
				v.Set(FlagIAVLPruningInterval, 10)

				return v
			},
			expectedOptions: types.PruningOptions{
				KeepRecent: 1234,
				KeepEvery:  4321,
				Interval:   10,
			},
		},
		{
			name: types.PruningOptionDefault,
			initParams: func() *viper.Viper {
				v := viper.New()
				v.Set(FlagPruning, types.PruningOptionDefault)
				return v
			},
			expectedOptions: types.PruneDefault,
		},
		{
			name: "new format takes priority over legacy",
			initParams: func() *viper.Viper {
				v := viper.New()
				// Both old and new format present - new format should win
				v.Set(FlagPruning, types.PruningOptionNothing)
				v.Set(FlagIAVLPruning, types.PruningOptionEverything)
				return v
			},
			expectedOptions: types.PruneEverything,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(j *testing.T) {
			viper.Reset()
			viper.SetDefault(FlagPruning, types.PruningOptionDefault)
			v := tt.initParams()

			opts, err := GetPruningOptionsFromFlags(v)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.Equal(t, tt.expectedOptions, opts)
		})
	}
}
