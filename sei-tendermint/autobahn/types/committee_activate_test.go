package types

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestActivateCommittee_StayLeaveRejoin(t *testing.T) {
	rng := utils.TestRng()
	a := GenSecretKey(rng).Public()
	b := GenSecretKey(rng).Public()
	c := GenSecretKey(rng).Public()
	d := GenSecretKey(rng).Public()

	requireLanesSorted := func(t *testing.T, committee *Committee) {
		t.Helper()
		lanes := committee.Lanes()
		for i := 1; i < lanes.Len(); i++ {
			require.Less(t, lanes.At(i-1).Compare(lanes.At(i)), 0)
		}
	}

	// Epoch 0: A,B,D join.
	c0, err := NewCommittee(map[PublicKey]uint64{a: 1, b: 1, d: 1})
	require.NoError(t, err)
	require.Equal(t, NewLaneID(a, 0), c0.Lane(a).OrPanic("a"))
	require.Equal(t, NewLaneID(b, 0), c0.Lane(b).OrPanic("b"))
	require.Equal(t, NewLaneID(d, 0), c0.Lane(d).OrPanic("d"))
	require.False(t, c0.HasLane(NewLaneID(c, 0)))
	requireLanesSorted(t, c0)

	// Epoch 1: A,B,D stay → copy e_join=0.
	c1, err := ActivateCommittee(c0, map[PublicKey]uint64{a: 1, b: 1, d: 1}, 1)
	require.NoError(t, err)
	require.Equal(t, NewLaneID(a, 0), c1.Lane(a).OrPanic("a"))
	require.Equal(t, NewLaneID(b, 0), c1.Lane(b).OrPanic("b"))
	require.Equal(t, NewLaneID(d, 0), c1.Lane(d).OrPanic("d"))
	requireLanesSorted(t, c1)

	// Epoch 2: B,D leave; C joins. A stays.
	c2, err := ActivateCommittee(c1, map[PublicKey]uint64{a: 1, c: 1}, 2)
	require.NoError(t, err)
	require.Equal(t, NewLaneID(a, 0), c2.Lane(a).OrPanic("a"))
	require.Equal(t, NewLaneID(c, 2), c2.Lane(c).OrPanic("c"))
	require.False(t, c2.HasLane(NewLaneID(b, 0)))
	require.False(t, c2.HasLane(NewLaneID(d, 0)))
	require.False(t, c2.HasReplica(b))
	requireLanesSorted(t, c2)

	// Epoch 3: D rejoins; C and A stay.
	c3, err := ActivateCommittee(c2, map[PublicKey]uint64{a: 1, c: 1, d: 1}, 3)
	require.NoError(t, err)
	require.Equal(t, NewLaneID(a, 0), c3.Lane(a).OrPanic("a"))
	require.Equal(t, NewLaneID(c, 2), c3.Lane(c).OrPanic("c"))
	require.Equal(t, NewLaneID(d, 3), c3.Lane(d).OrPanic("d"))
	require.False(t, c3.HasLane(NewLaneID(d, 0)))
	requireLanesSorted(t, c3)
}

func TestFinalizeCommittee_RejectsDuplicatePubKeyDifferentEJoin(t *testing.T) {
	rng := utils.TestRng()
	v := GenSecretKey(rng).Public()
	_, err := finalizeCommittee(
		[]LaneID{NewLaneID(v, 0), NewLaneID(v, 1)},
		map[PublicKey]uint64{v: 1},
		1,
	)
	require.Error(t, err)
}
