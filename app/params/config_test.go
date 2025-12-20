package params

import (
	"testing"

	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	"github.com/stretchr/testify/require"
)

func TestSetEVMConfigByMode(t *testing.T) {
	tests := []struct {
		name         string
		mode         NodeMode
		expectedHTTP bool
		expectedWS   bool
	}{
		{
			name:         "validator mode - EVM disabled",
			mode:         NodeModeValidator,
			expectedHTTP: false,
			expectedWS:   false,
		},
		{
			name:         "full mode - EVM enabled",
			mode:         NodeModeFull,
			expectedHTTP: true,
			expectedWS:   true,
		},
		{
			name:         "seed mode - EVM disabled",
			mode:         NodeModeSeed,
			expectedHTTP: false,
			expectedWS:   false,
		},
		{
			name:         "archive mode - EVM enabled",
			mode:         NodeModeArchive,
			expectedHTTP: true,
			expectedWS:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create default EVM config
			evmConfig := evmrpcconfig.DefaultConfig

			// Set EVM config based on mode
			SetEVMConfigByMode(&evmConfig, tt.mode)

			// Verify the config
			require.Equal(t, tt.expectedHTTP, evmConfig.HTTPEnabled, "EVM HTTP should match expected")
			require.Equal(t, tt.expectedWS, evmConfig.WSEnabled, "EVM WS should match expected")
		})
	}
}
