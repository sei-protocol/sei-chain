package types

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestLaneID_Bytes(t *testing.T) {
	rng := utils.TestRng()
	want := GenLaneID(rng)
	got, err := LaneIDFromBytes(want.Bytes())
	require.NoError(t, err)
	require.Equal(t, want, got)

	raw := want.Bytes()
	_, err = LaneIDFromBytes(raw[:len(raw)-1])
	require.Error(t, err)
	_, err = LaneIDFromBytes(append(append([]byte{}, raw...), 0))
	require.Error(t, err)
	_, err = LaneIDFromBytes(nil)
	require.Error(t, err)
}
