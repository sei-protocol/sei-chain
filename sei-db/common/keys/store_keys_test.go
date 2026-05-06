package keys

import (
	"testing"

	"github.com/stretchr/testify/require"
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
	got, err := AllModulesExcept(BankStoreKey)
	require.NoError(t, err)

	require.NotContains(t, got, BankStoreKey)
	require.Len(t, got, len(MemIAVLStoreKeys)-1)

	// Order of remaining keys is preserved.
	expected := make([]string, 0, len(MemIAVLStoreKeys)-1)
	for _, k := range MemIAVLStoreKeys {
		if k != BankStoreKey {
			expected = append(expected, k)
		}
	}
	require.Equal(t, expected, got)
}

func TestAllModulesExcept_MultipleExclusions(t *testing.T) {
	got, err := AllModulesExcept(BankStoreKey, EVMStoreKey, AuthStoreKey)
	require.NoError(t, err)

	require.NotContains(t, got, BankStoreKey)
	require.NotContains(t, got, EVMStoreKey)
	require.NotContains(t, got, AuthStoreKey)
	require.Len(t, got, len(MemIAVLStoreKeys)-3)
}

func TestAllModulesExcept_DuplicateExclusionsAreIdempotent(t *testing.T) {
	got, err := AllModulesExcept(BankStoreKey, BankStoreKey)
	require.NoError(t, err)

	require.NotContains(t, got, BankStoreKey)
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
	got, err := AllModulesExcept(BankStoreKey, "bogus")
	require.Error(t, err)
	require.Nil(t, got)
	require.Contains(t, err.Error(), "bogus")
}

func TestAllModulesExcept_DoesNotMutateMemIAVLStoreKeys(t *testing.T) {
	before := append([]string{}, MemIAVLStoreKeys...)
	_, err := AllModulesExcept(BankStoreKey, EVMStoreKey)
	require.NoError(t, err)
	require.Equal(t, before, MemIAVLStoreKeys)
}
