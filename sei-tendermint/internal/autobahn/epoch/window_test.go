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
	decoded, err := windowConv.Decode(windowConv.Encode(withPrev))
	require.NoError(t, err)
	require.True(t, withPrev.Equal(decoded))

	withoutPrev := utils.OrPanic1(NewWindow(utils.None[*types.Epoch](), current, next))
	decoded, err = windowConv.Decode(windowConv.Encode(withoutPrev))
	require.NoError(t, err)
	require.True(t, withoutPrev.Equal(decoded))
	require.Equal(t, utils.None[*types.Epoch](), decoded.Prev())
}

func TestMarshalWindowRoundTrip(t *testing.T) {
	rng := utils.TestRng()
	genesis, keys, _ := genCommittee(rng, 3)
	nextCommittee := utils.OrPanic1(genesis.DeriveNext(map[types.PublicKey]uint64{keys[1].Public(): 7}, 2))
	current := testEpoch(1, 11, genesis)
	next := testEpoch(2, 11, nextCommittee)

	for _, prev := range []utils.Option[*types.Epoch]{utils.Some(testEpoch(0, 11, genesis)), utils.None[*types.Epoch]()} {
		window := utils.OrPanic1(NewWindow(prev, current, next))
		raw := MarshalWindow(window)
		decoded, err := UnmarshalWindow(raw)
		require.NoError(t, err)
		require.True(t, window.Equal(decoded))
		require.Equal(t, raw, MarshalWindow(decoded))
	}
}

func TestCompleteWindow(t *testing.T) {
	rng := utils.TestRng()
	genesis, keys, _ := genCommittee(rng, 3)
	weights2 := map[types.PublicKey]uint64{keys[1].Public(): 7}
	committee2 := utils.OrPanic1(genesis.DeriveNext(weights2, 2))
	weights3 := map[types.PublicKey]uint64{keys[0].Public(): 5, keys[1].Public(): 7}
	committee3 := utils.OrPanic1(committee2.DeriveNext(weights3, 3))
	e0 := testEpoch(0, 11, genesis)
	e1 := testEpoch(1, 11, genesis)
	e2 := testEpoch(2, 11, committee2)
	e3 := testEpoch(3, 11, committee3)
	committed := utils.OrPanic1(NewWindow(utils.Some(e0), e1, e2))
	full := utils.OrPanic1(NewWindow(utils.Some(e1), e2, e3))
	withoutPrev := utils.OrPanic1(NewWindow(utils.None[*types.Epoch](), e2, e3))

	completed, err := CompleteWindow(utils.Some(committed), withoutPrev)
	require.NoError(t, err)
	require.True(t, full.Equal(completed))

	completed, err = CompleteWindow(utils.Some(committed), full)
	require.NoError(t, err)
	require.True(t, full.Equal(completed))

	completed, err = CompleteWindow(utils.None[Window](), withoutPrev)
	require.NoError(t, err)
	require.True(t, withoutPrev.Equal(completed))
}

func TestCompleteWindowRejectsConflicts(t *testing.T) {
	rng := utils.TestRng()
	genesis, keys, _ := genCommittee(rng, 3)
	weights2 := map[types.PublicKey]uint64{keys[1].Public(): 7}
	committee2 := utils.OrPanic1(genesis.DeriveNext(weights2, 2))
	weights3 := map[types.PublicKey]uint64{keys[0].Public(): 5, keys[1].Public(): 7}
	committee3 := utils.OrPanic1(committee2.DeriveNext(weights3, 3))
	other, _, _ := genCommittee(rng, 2)
	e0 := testEpoch(0, 11, genesis)
	e1 := testEpoch(1, 11, genesis)
	e2 := testEpoch(2, 11, committee2)
	e3 := testEpoch(3, 11, committee3)
	committed := utils.OrPanic1(NewWindow(utils.Some(e0), e1, e2))

	_, err := CompleteWindow(
		utils.Some(committed),
		utils.OrPanic1(NewWindow(utils.None[*types.Epoch](), e1, e2)),
	)
	require.Error(t, err)

	_, err = CompleteWindow(
		utils.Some(committed),
		Window{prev: utils.Some(testEpoch(1, 11, other)), current: e2, next: e3},
	)
	require.Error(t, err)
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
