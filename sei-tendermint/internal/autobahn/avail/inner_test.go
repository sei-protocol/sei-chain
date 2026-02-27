package avail

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/stretchr/testify/require"
)

func TestPruneMismatchedIndices(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := types.GenCommittee(rng, 4)
	ds := data.NewState(&data.Config{
		Committee: committee,
	}, utils.None[data.BlockStore]())
	state := NewState(keys[0], ds)

	// Helper to create a CommitQC for a specific index
	makeQC := func(index types.RoadIndex, prev utils.Option[*types.CommitQC]) *types.CommitQC {
		vs := types.ViewSpec{CommitQC: prev}
		fullProposal := utils.OrPanic1(types.NewProposal(
			leaderKey(committee, keys, vs.View()),
			committee,
			vs,
			time.Now(),
			nil,
			utils.None[*types.AppQC](),
		))
		vote := types.NewCommitVote(fullProposal.Proposal().Msg())
		var votes []*types.Signed[*types.CommitVote]
		for _, k := range keys {
			votes = append(votes, types.Sign(k, vote))
		}
		return types.NewCommitQC(votes)
	}

	qc0 := makeQC(0, utils.None[*types.CommitQC]())
	_ = makeQC(1, utils.Some(qc0)) // show we can generate index 1

	// Create an AppQC for index 1 (matching qc1)
	appProposal1 := types.NewAppProposal(0, 1, types.GenAppHash(rng))
	appQC1 := types.NewAppQC(makeAppVotes(keys, appProposal1))

	// Now call PushAppQC with appQC1 (index 1) and qc0 (index 0)
	err := state.PushAppQC(appQC1, qc0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mismatched QCs")

	// Get the inner state
	for inner := range state.inner.Lock() {
		// Now call prune with mismatched QCs directly to test the safety check
		updated, err := inner.prune(appQC1, qc0)

		require.Error(t, err)
		require.Contains(t, err.Error(), "mismatched QCs")
		require.False(t, updated, "prune should return false for mismatched indices")
		require.False(t, inner.latestAppQC.IsPresent(), "latestAppQC should not have been updated")
	}
}
