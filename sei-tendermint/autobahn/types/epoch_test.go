package types

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEpochIsClosed(t *testing.T) {
	rng := utils.TestRng()
	a := GenSecretKey(rng).Public()
	b := GenSecretKey(rng).Public()
	c := GenSecretKey(rng).Public()

	ep1 := NewEpoch(1, OpenRoadRange(), time.Time{},
		utils.OrPanic1(NewCommittee(map[PublicKey]uint64{a: 1, c: 1})), 0)

	stay := LaneID{Validator: a, Joined: 0}
	leave := LaneID{Validator: b, Joined: 0}
	joiner := LaneID{Validator: c, Joined: 1}
	// Joined == this epoch and absent from committee: not closed
	// (IsClosed requires Joined < epoch index).
	sameEpochAbsent := LaneID{Validator: GenSecretKey(rng).Public(), Joined: 1}

	require.False(t, ep1.IsClosed(stay))
	require.True(t, ep1.IsClosed(leave))
	require.False(t, ep1.IsClosed(joiner))
	require.False(t, ep1.IsClosed(sameEpochAbsent))
}
