package migration

import (
	"testing"

	"github.com/stretchr/testify/require"

	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

func TestAllModulesExcept_NoExclusions(t *testing.T) {
	got, err := AllModulesExcept()
	require.NoError(t, err)
	require.Equal(t, MemIAVLStoreKeys, got)
}

func TestAllModulesExcept_ReturnsCopy(t *testing.T) {
	got, err := AllModulesExcept()
	require.NoError(t, err)

	// Mutating the result must not affect the package-level slice.
	got[0] = "mutated"
	require.NotEqual(t, "mutated", MemIAVLStoreKeys[0])
}

func TestAllModulesExcept_SingleExclusion(t *testing.T) {
	got, err := AllModulesExcept(banktypes.StoreKey)
	require.NoError(t, err)

	require.NotContains(t, got, banktypes.StoreKey)
	require.Len(t, got, len(MemIAVLStoreKeys)-1)

	// Order of remaining keys is preserved.
	expected := make([]string, 0, len(MemIAVLStoreKeys)-1)
	for _, k := range MemIAVLStoreKeys {
		if k != banktypes.StoreKey {
			expected = append(expected, k)
		}
	}
	require.Equal(t, expected, got)
}

func TestAllModulesExcept_MultipleExclusions(t *testing.T) {
	got, err := AllModulesExcept(banktypes.StoreKey, evmtypes.StoreKey, authtypes.StoreKey)
	require.NoError(t, err)

	require.NotContains(t, got, banktypes.StoreKey)
	require.NotContains(t, got, evmtypes.StoreKey)
	require.NotContains(t, got, authtypes.StoreKey)
	require.Len(t, got, len(MemIAVLStoreKeys)-3)
}

func TestAllModulesExcept_DuplicateExclusionsAreIdempotent(t *testing.T) {
	got, err := AllModulesExcept(banktypes.StoreKey, banktypes.StoreKey)
	require.NoError(t, err)

	require.NotContains(t, got, banktypes.StoreKey)
	require.Len(t, got, len(MemIAVLStoreKeys)-1)
}

func TestAllModulesExcept_ExcludeAll(t *testing.T) {
	all := append([]string{}, MemIAVLStoreKeys...)
	got, err := AllModulesExcept(all...)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestAllModulesExcept_UnknownModuleReturnsError(t *testing.T) {
	got, err := AllModulesExcept("not-a-real-module")
	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "not-a-real-module")
}

func TestAllModulesExcept_UnknownModuleAmongValidOnesReturnsError(t *testing.T) {
	got, err := AllModulesExcept(banktypes.StoreKey, "bogus")
	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "bogus")
}

func TestAllModulesExcept_DoesNotMutateMemIAVLStoreKeys(t *testing.T) {
	before := append([]string{}, MemIAVLStoreKeys...)
	_, err := AllModulesExcept(banktypes.StoreKey, evmtypes.StoreKey)
	require.NoError(t, err)
	require.Equal(t, before, MemIAVLStoreKeys)
}

// TestMigrationStore_NotInMemIAVLStoreKeys asserts that the reserved
// MigrationStore name does not collide with any production module's
// store key. The migration package routes traffic by store name and
// reserves MigrationStore for its own bookkeeping (boundary, version);
// a future module added to MemIAVLStoreKeys whose StoreKey happened to
// be "migration" would silently shadow that bookkeeping.
func TestMigrationStore_NotInMemIAVLStoreKeys(t *testing.T) {
	require.NotContains(t, MemIAVLStoreKeys, MigrationStore,
		"MemIAVLStoreKeys must not contain the reserved MigrationStore name %q", MigrationStore)
}
