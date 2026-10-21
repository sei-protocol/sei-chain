package avail

import (
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func committedSamples(t *testing.T) uint64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, meters.LaneBlockCommitted.Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func TestCommitQCObservesEachHeldBlockItFinalizes(t *testing.T) {
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

	var tip *types.BlockHeader
	for range 2 {
		p, err := state.ProduceLocalBlock(lane, state.NextBlock(lane), types.GenPayload(rng))
		require.NoError(t, err)
		tip = p.Msg().Block().Header()
	}
	qc := types.BuildCommitQC(ep, keys, utils.None[*types.CommitQC](), map[types.LaneID]*types.LaneQC{
		lane: types.NewLaneQC(makeLaneVotes(keys, tip)),
	})

	before := committedSamples(t)
	require.NoError(t, state.PushCommitQC(ctx, qc))
	require.Equal(t, before+2, committedSamples(t))

	// A stale CommitQC is a no-op and observes nothing.
	require.NoError(t, state.PushCommitQC(ctx, qc))
	require.Equal(t, before+2, committedSamples(t))
}
