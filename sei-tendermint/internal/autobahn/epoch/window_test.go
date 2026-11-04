package epoch

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestWindowConvRoundTripAndOptionalPrev(t *testing.T) {
	rng := utils.TestRng()
	genesis, keys, _ := genCommittee(rng, 3)
	weights := map[types.PublicKey]uint64{keys[1].Public(): 7}
	nextCommittee := utils.OrPanic1(genesis.DeriveNext(weights, 2))
	prev := testEpoch(0, 11, genesis)
	current := testEpoch(1, 11, genesis)
	next := testEpoch(2, 11, nextCommittee)

	withPrev := utils.OrPanic1(NewWindow(utils.Some(prev), current, next))
	decoded, err := WindowConv.Decode(WindowConv.Encode(withPrev))
	require.NoError(t, err)
	require.True(t, withPrev.Equal(decoded))

	withoutPrev := utils.OrPanic1(NewWindow(utils.None[*types.Epoch](), current, next))
	decoded, err = WindowConv.Decode(WindowConv.Encode(withoutPrev))
	require.NoError(t, err)
	require.True(t, withoutPrev.Equal(decoded))
	require.Equal(t, utils.None[*types.Epoch](), decoded.Prev())
}

func TestWindowRejectsNonConsecutiveEpochs(t *testing.T) {
	rng := utils.TestRng()
	genesis, keys, _ := genCommittee(rng, 3)
	weights := map[types.PublicKey]uint64{keys[1].Public(): 7}
	nextCommittee := utils.OrPanic1(genesis.DeriveNext(weights, 2))

	_, err := NewWindow(
		utils.Some(testEpoch(0, 0, genesis)),
		testEpoch(1, 0, genesis),
		testEpoch(3, 0, nextCommittee),
	)
	require.Error(t, err)
}
