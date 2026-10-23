package avail

import (
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func sampleCount(t *testing.T, h *prometheus.Histogram) uint64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, h.Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func TestLaneBlockLatencyRecordsEachStepOncePerBlock(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	state := utils.OrPanic1(NewState(
		keys[0],
		newTestDataState(&data.Config{Registry: registry}),
		utils.None[string](),
	))
	ep := registry.MustEpoch(0)
	lane := ep.Committee().Lane(keys[0].Public()).OrPanic("lane")

	var headers []*types.BlockHeader
	for range 2 {
		p, err := state.ProduceLocalBlock(lane, state.NextBlock(lane), types.GenPayload(rng))
		require.NoError(t, err)
		headers = append(headers, p.Msg().Block().Header())
	}

	votes, qcs := sampleCount(t, meters.LaneVoteReceive), sampleCount(t, meters.LaneQCFormed)
	for _, h := range headers {
		for _, vote := range makeLaneVotes(keys, h) {
			require.NoError(t, state.PushVote(ctx, vote))
		}
	}
	require.Equal(t, votes+2, sampleCount(t, meters.LaneVoteReceive))
	require.Equal(t, qcs+2, sampleCount(t, meters.LaneQCFormed))
}
