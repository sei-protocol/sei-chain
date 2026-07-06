package types

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func TestRoundScaledIntMatchesDecRoundInt(t *testing.T) {
	half := new(big.Int).Div(decScaleFactor, big.NewInt(2)) // 0.5 * 10^Precision
	scale := func(n int64) *big.Int {
		return new(big.Int).Mul(big.NewInt(n), decScaleFactor)
	}
	cases := []*big.Int{
		big.NewInt(0),
		new(big.Int).Sub(half, big.NewInt(1)), // < 0.5 -> down
		new(big.Int).Set(half),                // 0.5, quo 0 (even) -> 0
		new(big.Int).Add(half, big.NewInt(1)), // > 0.5 -> up
		new(big.Int).Add(scale(1), half),      // 1.5, quo 1 (odd)  -> 2
		new(big.Int).Add(scale(2), half),      // 2.5, quo 2 (even) -> 2
		scale(1234567),
		scale(123456789),
	}
	for _, scaled := range cases {
		want := sdk.NewDecFromBigIntWithPrec(new(big.Int).Set(scaled), sdk.Precision).RoundInt()
		require.Equal(t, want, roundScaledInt(new(big.Int).Set(scaled)), "scaled=%s", scaled)
	}
}
