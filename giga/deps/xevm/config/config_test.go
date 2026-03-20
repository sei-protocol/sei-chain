package config

import (
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

func TestGetEVMChainID(t *testing.T) {
	tests := []struct {
		name            string
		cosmosChainID   string
		expectedChainID int64
	}{
		{
			name:            "pacific-1 chain ID",
			cosmosChainID:   "pacific-1",
			expectedChainID: 1329,
		},
		{
			name:            "atlantic-2 chain ID",
			cosmosChainID:   "atlantic-2",
			expectedChainID: 1328,
		},
		{
			name:            "arctic-1 chain ID",
			cosmosChainID:   "arctic-1",
			expectedChainID: 713715,
		},
		{
			name:            "unknown chain ID returns default",
			cosmosChainID:   "unknown-chain",
			expectedChainID: DefaultChainID,
		},
		{
			name:            "empty chain ID returns default",
			cosmosChainID:   "",
			expectedChainID: DefaultChainID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetEVMChainID(tt.cosmosChainID)
			require.NotNil(t, result)
			require.Equal(t, tt.expectedChainID, result.Int64())
		})
	}
}

func TestGetVersionWthDefault(t *testing.T) {
	tests := []struct {
		name           string
		chainID        string
		override       uint16
		defaultVersion uint16
		expected       uint16
	}{
		{
			name:           "live chain ID with override - override ignored",
			chainID:        "pacific-1",
			override:       100,
			defaultVersion: 50,
			expected:       50,
		},
		{
			name:           "live chain ID with zero override - uses default",
			chainID:        "atlantic-2",
			override:       0,
			defaultVersion: 50,
			expected:       50,
		},
		{
			name:           "non-live chain ID with override - uses override",
			chainID:        "test-chain",
			override:       100,
			defaultVersion: 50,
			expected:       100,
		},
		{
			name:           "non-live chain ID with zero override - uses default",
			chainID:        "test-chain",
			override:       0,
			defaultVersion: 50,
			expected:       50,
		},
		{
			name:           "arctic-1 (live) with override - override ignored",
			chainID:        "arctic-1",
			override:       200,
			defaultVersion: 75,
			expected:       75,
		},
		{
			name:           "non-live chain ID with same override and default",
			chainID:        "test-chain",
			override:       50,
			defaultVersion: 50,
			expected:       50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := sdk.Context{}.WithChainID(tt.chainID)
			result := GetVersionWthDefault(ctx, tt.override, tt.defaultVersion)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsLiveChainID(t *testing.T) {
	tests := []struct {
		name     string
		chainID  string
		expected bool
	}{
		{
			name:     "pacific-1 is live",
			chainID:  "pacific-1",
			expected: true,
		},
		{
			name:     "atlantic-2 is live",
			chainID:  "atlantic-2",
			expected: true,
		},
		{
			name:     "arctic-1 is live",
			chainID:  "arctic-1",
			expected: true,
		},
		{
			name:     "unknown chain ID is not live",
			chainID:  "unknown-chain",
			expected: false,
		},
		{
			name:     "empty chain ID is not live",
			chainID:  "",
			expected: false,
		},
		{
			name:     "test chain ID is not live",
			chainID:  "test-chain",
			expected: false,
		},
		{
			name:     "case sensitive - Pacific-1 is not live",
			chainID:  "Pacific-1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := sdk.Context{}.WithChainID(tt.chainID)
			result := IsLiveChainID(ctx)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsLiveEVMChainID(t *testing.T) {
	tests := []struct {
		name       string
		evmChainID int64
		expected   bool
	}{
		{
			name:       "pacific-1 EVM chain ID (1329) is live",
			evmChainID: 1329,
			expected:   true,
		},
		{
			name:       "atlantic-2 EVM chain ID (1328) is live",
			evmChainID: 1328,
			expected:   true,
		},
		{
			name:       "arctic-1 EVM chain ID (713715) is live",
			evmChainID: 713715,
			expected:   true,
		},
		{
			name:       "default chain ID (713714) is not live",
			evmChainID: DefaultChainID,
			expected:   false,
		},
		{
			name:       "random chain ID is not live",
			evmChainID: 12345,
			expected:   false,
		},
		{
			name:       "zero chain ID is not live",
			evmChainID: 0,
			expected:   false,
		},
		{
			name:       "negative chain ID is not live",
			evmChainID: -1,
			expected:   false,
		},
		{
			name:       "chain ID close to live ones is not live",
			evmChainID: 1327,
			expected:   false,
		},
		{
			name:       "chain ID close to live ones is not live",
			evmChainID: 1330,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsLiveEVMChainID(tt.evmChainID)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestChainIDMappingConsistency(t *testing.T) {
	// Test that ChainIDMapping and EVMChainIDMapping are consistent
	for cosmosChainID, evmChainID := range ChainIDMapping {
		t.Run("mapping consistency for "+cosmosChainID, func(t *testing.T) {
			// Check forward mapping
			result := GetEVMChainID(cosmosChainID)
			require.NotNil(t, result)
			require.Equal(t, evmChainID, result.Int64())

			// Check reverse mapping exists
			reverseCosmosChainID, ok := EVMChainIDMapping[evmChainID]
			require.True(t, ok, "EVMChainIDMapping should have reverse mapping for %d", evmChainID)
			require.Equal(t, cosmosChainID, reverseCosmosChainID)

			// Check IsLiveEVMChainID
			require.True(t, IsLiveEVMChainID(evmChainID))

			// Check IsLiveChainID
			ctx := sdk.Context{}.WithChainID(cosmosChainID)
			require.True(t, IsLiveChainID(ctx))
		})
	}

	// Test that EVMChainIDMapping has reverse entries for all ChainIDMapping entries
	for evmChainID, cosmosChainID := range EVMChainIDMapping {
		t.Run("reverse mapping consistency for "+cosmosChainID, func(t *testing.T) {
			// Check that forward mapping exists
			forwardEVMChainID, ok := ChainIDMapping[cosmosChainID]
			require.True(t, ok, "ChainIDMapping should have forward mapping for %s", cosmosChainID)
			require.Equal(t, evmChainID, forwardEVMChainID)
		})
	}
}

func TestGetEVMChainID_ReturnsNewInstance(t *testing.T) {
	// Test that GetEVMChainID returns a new instance each time
	result1 := GetEVMChainID("pacific-1")
	result2 := GetEVMChainID("pacific-1")

	require.NotSame(t, result1, result2, "GetEVMChainID should return a new instance each time")
	require.Equal(t, result1.Int64(), result2.Int64())
}

func TestGetVersionWthDefault_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		chainID        string
		override       uint16
		defaultVersion uint16
		expected       uint16
	}{
		{
			name:           "max uint16 override on non-live chain",
			chainID:        "test-chain",
			override:       65535,
			defaultVersion: 1,
			expected:       65535,
		},
		{
			name:           "max uint16 default on live chain",
			chainID:        "pacific-1",
			override:       100,
			defaultVersion: 65535,
			expected:       65535,
		},
		{
			name:           "min values",
			chainID:        "test-chain",
			override:       1,
			defaultVersion: 1,
			expected:       1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := sdk.Context{}.WithChainID(tt.chainID)
			result := GetVersionWthDefault(ctx, tt.override, tt.defaultVersion)
			require.Equal(t, tt.expected, result)
		})
	}
}
