package config

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateFreezeBackport(t *testing.T) {
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
