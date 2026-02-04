package evm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultEVMStoreConfig(t *testing.T) {
	cfg := DefaultEVMStoreConfig()

	// Default should be disabled
	require.False(t, cfg.Enable)
	require.Greater(t, cfg.KeepRecent, 0)
}

func TestAllEVMStoreTypes(t *testing.T) {
	types := AllEVMStoreTypes()

	require.Equal(t, NumEVMStoreTypes, len(types))

	// Verify expected types are present
	typeSet := make(map[EVMStoreType]bool)
	for _, st := range types {
		typeSet[st] = true
	}

	require.True(t, typeSet[StoreNonce], "StoreNonce should be in AllEVMStoreTypes")
	require.True(t, typeSet[StoreCodeHash], "StoreCodeHash should be in AllEVMStoreTypes")
	require.True(t, typeSet[StoreCode], "StoreCode should be in AllEVMStoreTypes")
	require.True(t, typeSet[StoreStorage], "StoreStorage should be in AllEVMStoreTypes")

	// Balance should NOT be present (reserved for future)
	require.False(t, typeSet[StoreBalance], "StoreBalance should not be in AllEVMStoreTypes yet")
}

func TestStoreTypeName(t *testing.T) {
	tests := []struct {
		storeType EVMStoreType
		expected  string
	}{
		{StoreNonce, "nonce"},
		{StoreCodeHash, "codehash"},
		{StoreCode, "code"},
		{StoreStorage, "storage"},
		{StoreBalance, "balance"},
		{StoreUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			require.Equal(t, tt.expected, StoreTypeName(tt.storeType))
		})
	}
}
