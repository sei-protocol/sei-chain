package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func TestRoadRange_Has(t *testing.T) {
	r := RoadRange{First: 10, Next: 13}
	require.False(t, r.Has(9))
	require.True(t, r.Has(10))
	require.True(t, r.Has(12))
	require.False(t, r.Has(13))
}

func TestRoadRange_IsLastRoad(t *testing.T) {
	r := RoadRange{First: 10, Next: 13} // covers 10, 11, 12
	require.False(t, r.IsLastRoad(10))
	require.False(t, r.IsLastRoad(11))
	require.True(t, r.IsLastRoad(12))
	require.False(t, r.IsLastRoad(13))
}

func TestEpoch_AcceptsAppEpoch(t *testing.T) {
	rng := utils.TestRng()
	weights := map[PublicKey]uint64{GenSecretKey(rng).Public(): 1}
	committee := utils.OrPanic1(NewCommittee(weights))
	cases := []struct {
		cur, app EpochIndex
		ok       bool
	}{
		{0, 0, true},
		{0, 1, false},
		{0, ^EpochIndex(0), false}, // MaxUint64 must not count as cur-1
		{1, 1, true},
		{1, 0, true},
		{1, 2, false},
		{2, 1, true},
		{2, 0, false},
	}
	for _, tc := range cases {
		ep := NewEpoch(tc.cur, RoadRange{First: 0, Next: 1}, committee)
		require.Equal(t, tc.ok, ep.AcceptsAppEpoch(tc.app), "Epoch(%d).AcceptsAppEpoch(%d)", tc.cur, tc.app)
	}
}
