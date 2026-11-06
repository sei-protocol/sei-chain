package epoch

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestMarshalEpochRoundTrip(t *testing.T) {
	rng := utils.TestRng()
	committee, _, _ := genCommittee(rng, 3)
	epoch := testEpoch(7, 11, committee)

	raw := MarshalEpoch(epoch)
	decoded, err := types.EpochConv.Unmarshal(raw)
	require.NoError(t, err)
	require.Equal(t, epoch, decoded)
	require.Equal(t, raw, MarshalEpoch(decoded))
}
