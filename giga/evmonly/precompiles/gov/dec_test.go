package gov

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// TestDecMatchesSDK pins mulDec and quoDec to sdk.Dec, which x/gov tallies with.
func TestDecMatchesSDK(t *testing.T) {
	rng := rand.New(rand.NewSource(7)) //nolint:gosec // deterministic test data
	random := func() *big.Int {
		v := new(big.Int).Rand(rng, new(big.Int).Lsh(big.NewInt(1), uint(rng.Intn(120)+1))) //nolint:gosec // bounded
		return v.Add(v, big.NewInt(1))
	}
	for range 2_000 {
		a, b := random(), random()
		da, db := sdk.NewDecFromBigIntWithPrec(a, sdk.Precision), sdk.NewDecFromBigIntWithPrec(b, sdk.Precision)
		require.Equal(t, da.Mul(db).BigInt().String(), mulDec(a, b).String(), "%s * %s", a, b)
		require.Equal(t, da.Quo(db).BigInt().String(), quoDec(a, b).String(), "%s / %s", a, b)
	}
}
