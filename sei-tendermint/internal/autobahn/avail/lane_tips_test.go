package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestLaneTipsTrackHeldWithLaneQCAndCommittedBlocks(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	state := utils.OrPanic1(NewState(
		keys[0],
		newTestDataState(&data.Config{Registry: registry}),
		utils.None[string](),
	))
	lane := registry.MustEpoch(0).Committee().Lane(keys[0].Public()).OrPanic("lane")

	var headers []*types.BlockHeader
	for range 3 {
		p, err := state.ProduceLocalBlock(lane, state.NextBlock(lane), types.GenPayload(rng))
		require.NoError(t, err)
		headers = append(headers, p.Msg().Block().Header())
	}
	require.Equal(t, LaneTip{WithCommitQC: 0, WithLaneQC: 0, Held: 3}, state.LaneTips()[lane])

	for _, h := range headers[:2] {
		for _, vote := range makeLaneVotes(keys, h) {
			require.NoError(t, state.PushVote(ctx, vote))
		}
	}
	require.Equal(t, LaneTip{WithCommitQC: 0, WithLaneQC: 2, Held: 3}, state.LaneTips()[lane])

	pruneToHeader(state, keys, headers[0])
	tips := state.LaneTips()
	require.Equal(t, LaneTip{WithCommitQC: 1, WithLaneQC: 2, Held: 3}, tips[lane])
	for other, tip := range tips {
		if other != lane {
			require.Equal(t, LaneTip{}, tip)
		}
	}
}
