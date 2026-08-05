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

	// Epoch 0: A,B,D join.
	c0, err := NewCommittee(map[PublicKey]uint64{a: 1, b: 1, d: 1})
	require.NoError(t, err)
	require.Equal(t, NewLaneID(a, 0), c0.Lane(a).OrPanic("a"))
	require.Equal(t, NewLaneID(b, 0), c0.Lane(b).OrPanic("b"))
	require.Equal(t, NewLaneID(d, 0), c0.Lane(d).OrPanic("d"))
	require.False(t, c0.HasLane(NewLaneID(c, 0)))

	// Epoch 10: A,B,D stay → copy e_join=0.
	c10, err := ActivateCommittee(c0, map[PublicKey]uint64{a: 1, b: 1, d: 1}, 10)
	require.NoError(t, err)
	require.Equal(t, NewLaneID(a, 0), c10.Lane(a).OrPanic("a"))
	require.Equal(t, NewLaneID(b, 0), c10.Lane(b).OrPanic("b"))
	require.Equal(t, NewLaneID(d, 0), c10.Lane(d).OrPanic("d"))

	// Epoch 11: B,D leave; C joins. A stays.
	c11, err := ActivateCommittee(c10, map[PublicKey]uint64{a: 1, c: 1}, 11)
	require.NoError(t, err)
	require.Equal(t, NewLaneID(a, 0), c11.Lane(a).OrPanic("a"))
	require.Equal(t, NewLaneID(c, 11), c11.Lane(c).OrPanic("c"))
	require.False(t, c11.HasLane(NewLaneID(b, 0)))
	require.False(t, c11.HasLane(NewLaneID(d, 0)))
	require.False(t, c11.HasReplica(b))

	// Epoch 12: D rejoins; C and A stay.
	c12, err := ActivateCommittee(c11, map[PublicKey]uint64{a: 1, c: 1, d: 1}, 12)
	require.NoError(t, err)
	require.Equal(t, NewLaneID(a, 0), c12.Lane(a).OrPanic("a"))
	require.Equal(t, NewLaneID(c, 11), c12.Lane(c).OrPanic("c"))
	require.Equal(t, NewLaneID(d, 12), c12.Lane(d).OrPanic("d"))
	require.False(t, c12.HasLane(NewLaneID(d, 0)))
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
