package types

import (
	"encoding/hex"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestLaneIDConv(t *testing.T) {
	rng := utils.TestRng()
	for range 10 {
		require.NoError(t, LaneIDConv.Test(GenLaneID(rng)))
		require.NoError(t, LaneIDConv.Test(NewLaneID(GenSecretKey(rng).Public(), EpochIndex(rng.Uint64()))))
	}
}

func TestLaneID_BytesRoundtrip(t *testing.T) {
	rng := utils.TestRng()
	for range 20 {
		want := NewLaneID(GenSecretKey(rng).Public(), EpochIndex(rng.Uint64()))
		got, err := LaneIDFromBytes(want.Bytes())
		require.NoError(t, err)
		require.Equal(t, want, got)

		raw, err := hex.DecodeString(want.HexString())
		require.NoError(t, err)
		gotHex, err := LaneIDFromBytes(raw)
		require.NoError(t, err)
		require.Equal(t, want, gotHex)
	}
}

func TestLaneIDFromBytes_ExactLength(t *testing.T) {
	rng := utils.TestRng()
	good := NewLaneID(GenSecretKey(rng).Public(), 1).Bytes()
	_, err := LaneIDFromBytes(good[:len(good)-1])
	require.Error(t, err)
	_, err = LaneIDFromBytes(append(append([]byte{}, good...), 0))
	require.Error(t, err)
	_, err = LaneIDFromBytes(nil)
	require.Error(t, err)
}
