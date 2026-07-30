package types

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
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
