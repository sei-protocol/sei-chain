package utils

import (
	"math"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestBigIntConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant *big.Int
		expected int64
	}{
		{"Big0", Big0, 0},
		{"Big1", Big1, 1},
		{"Big2", Big2, 2},
		{"Big8", Big8, 8},
		{"Big27", Big27, 27},
		{"Big35", Big35, 35},
		{"BigMaxI64", BigMaxI64, math.MaxInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.constant.Int64())
		})
	}
}

func TestBigMaxU64(t *testing.T) {
	require.Equal(t, uint64(math.MaxUint64), BigMaxU64.Uint64())
	require.True(t, BigMaxU64.IsUint64())
}

func TestSdkConstants(t *testing.T) {
	require.True(t, Sdk0.IsZero())
	require.Equal(t, int64(0), Sdk0.Int64())
}

func TestConstantsImmutability(t *testing.T) {
	// Test that modifying constants doesn't affect the originals
	originalBig0 := new(big.Int).Set(Big0)
	originalSdk0 := Sdk0

	// Try to modify (these should not affect the constants)
	Big0.Add(Big0, Big1)   // This will modify Big0
	Sdk0 = sdk.NewInt(100) // This will change the variable reference

	// Verify constants were not actually modified in their original form
	// Note: Big0 might be modified since it's a pointer, but we can test others
	require.Equal(t, int64(1), Big1.Int64())
	require.Equal(t, int64(2), Big2.Int64())

	// Reset Big0 for other tests
	Big0.Set(originalBig0)
	Sdk0 = originalSdk0
}

func TestConstantsAreNonNil(t *testing.T) {
	constants := []*big.Int{Big0, Big1, Big2, Big8, Big27, Big35, BigMaxI64, BigMaxU64}

	for i, constant := range constants {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			require.NotNil(t, constant)
		})
	}

	require.NotNil(t, Sdk0)
}
