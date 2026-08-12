package types

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestDeriveNext_StayLeaveRejoin(t *testing.T) {
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
	require.Equal(t, LaneID{Validator: a, Joined: 0}, c0.Lane(a).OrPanic("a"))
	require.Equal(t, LaneID{Validator: b, Joined: 0}, c0.Lane(b).OrPanic("b"))
	require.Equal(t, LaneID{Validator: d, Joined: 0}, c0.Lane(d).OrPanic("d"))
	require.False(t, c0.HasLane(LaneID{Validator: c, Joined: 0}))
	requireLanesSorted(t, c0)

	// Epoch 1: A, B, D remain; Joined stays 0.
	c1, err := c0.DeriveNext(map[PublicKey]uint64{a: 1, b: 1, d: 1}, 1)
	require.NoError(t, err)
	require.Equal(t, LaneID{Validator: a, Joined: 0}, c1.Lane(a).OrPanic("a"))
	require.Equal(t, LaneID{Validator: b, Joined: 0}, c1.Lane(b).OrPanic("b"))
	require.Equal(t, LaneID{Validator: d, Joined: 0}, c1.Lane(d).OrPanic("d"))
	requireLanesSorted(t, c1)

	// Epoch 2: B and D leave; C joins with Joined=2; A remains.
	c2, err := c1.DeriveNext(map[PublicKey]uint64{a: 1, c: 1}, 2)
	require.NoError(t, err)
	require.Equal(t, LaneID{Validator: a, Joined: 0}, c2.Lane(a).OrPanic("a"))
	require.Equal(t, LaneID{Validator: c, Joined: 2}, c2.Lane(c).OrPanic("c"))
	require.False(t, c2.HasLane(LaneID{Validator: b, Joined: 0}))
	require.False(t, c2.HasLane(LaneID{Validator: d, Joined: 0}))
	require.False(t, c2.HasReplica(b))
	requireLanesSorted(t, c2)

	// Epoch 3: D rejoins with Joined=3; C and A remain.
	c3, err := c2.DeriveNext(map[PublicKey]uint64{a: 1, c: 1, d: 1}, 3)
	require.NoError(t, err)
	require.Equal(t, LaneID{Validator: a, Joined: 0}, c3.Lane(a).OrPanic("a"))
	require.Equal(t, LaneID{Validator: c, Joined: 2}, c3.Lane(c).OrPanic("c"))
	require.Equal(t, LaneID{Validator: d, Joined: 3}, c3.Lane(d).OrPanic("d"))
	require.False(t, c3.HasLane(LaneID{Validator: d, Joined: 0}))
	requireLanesSorted(t, c3)
}
