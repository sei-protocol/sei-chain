package staking

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/dec"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

func TestUnbondingEntriesRejectAnImpossibleLength(t *testing.T) {
	s := newStore(newMemState())
	s.SetWord(unbondingBase(alice, val1), solstorage.Slot(1<<32))
	_, err := s.unbondingEntries(alice, val1)
	require.ErrorIs(t, err, errInvalidStoredEntries)
}

func TestQueueClampsToItsLastEntry(t *testing.T) {
	q := newStore(newMemState()).unbondingQueue()
	require.Equal(t, uint64(5), q.clamp(5), "an empty queue takes any time")
	_, err := q.push(10)
	require.NoError(t, err)
	require.Equal(t, uint64(10), q.clamp(5))
	require.Equal(t, uint64(11), q.clamp(11))
	q.pop()
	require.Equal(t, q.head(), q.tail())
	require.Equal(t, uint64(5), q.clamp(5), "a drained queue takes any time")
	_, ok := q.due(100)
	require.False(t, ok)
	require.Equal(t, common.Hash{}, q.s.Word(q.entry(0)), "pop zeroes the entry")
}

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
		require.Equal(t, wantTokens.TruncateInt().BigInt().String(), dec.Truncate(decTokens).String())
	}
}
