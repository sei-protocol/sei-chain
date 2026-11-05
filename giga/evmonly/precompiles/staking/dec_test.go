package staking

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

// TestShareMathMatchesSDK pins the exchange between tokens and shares to
// x/staking's Validator, rounding included.
func TestShareMathMatchesSDK(t *testing.T) {
	rng := rand.New(rand.NewSource(7)) //nolint:gosec // deterministic test data
	random := func(bits int) *big.Int {
		v := new(big.Int).Rand(rng, new(big.Int).Lsh(big.NewInt(1), uint(rng.Intn(bits)+1))) //nolint:gosec // bounded
		return v.Add(v, big.NewInt(1))
	}
	for range 2_000 {
		v := validator{tokens: random(100), delegatorShares: random(160)}
		sv := stakingtypes.Validator{
			Tokens:          sdk.NewIntFromBigInt(v.tokens),
			DelegatorShares: sdk.NewDecFromBigIntWithPrec(v.delegatorShares, sdk.Precision),
		}

		amount := random(100)
		got, err := v.sharesFromTokens(amount)
		require.NoError(t, err)
		want, err := sv.SharesFromTokens(sdk.NewIntFromBigInt(amount))
		require.NoError(t, err)
		require.Equal(t, want.BigInt().String(), got.String(), "shares for %s tokens", amount)
		truncated, err := sv.SharesFromTokensTruncated(sdk.NewIntFromBigInt(amount))
		require.NoError(t, err)
		require.Equal(t, truncated.BigInt().String(), got.String(), "x/staking's truncated shares are the same")

		shares := new(big.Int).Rand(rng, v.delegatorShares)
		decTokens := v.tokensFromShares(shares)
		wantTokens := sv.TokensFromShares(sdk.NewDecFromBigIntWithPrec(shares, sdk.Precision))
		require.Equal(t, wantTokens.BigInt().String(), decTokens.String(), "tokens for %s shares", shares)
		require.Equal(t, wantTokens.TruncateInt().BigInt().String(), truncateDec(decTokens).String())
	}
}

func TestVotingPowerRatioMatchesSDK(t *testing.T) {
	rng := rand.New(rand.NewSource(11)) //nolint:gosec // deterministic test data
	for range 2_000 {
		total := big.NewInt(rng.Int63n(1<<40) + 1)
		power := new(big.Int).Rand(rng, total)
		want := sdk.NewIntFromBigInt(power).ToDec().Quo(sdk.NewIntFromBigInt(total).ToDec())
		require.Equal(t, want.BigInt().String(), quoDec(intToDec(power), intToDec(total)).String(), "%s / %s", power, total)
	}
}
