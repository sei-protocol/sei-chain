package evidence_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/tmhash"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func rewindOver(height int64) []types.Rewind {
	return []types.Rewind{{
		ChainID:    evidenceChainID,
		SafeHeight: height - 1,
		Discarded:  []types.DiscardedBlock{{Height: height, Hash: bytes.Repeat([]byte{1}, tmhash.Size)}},
	}}
}

func TestAddEvidenceRejectsRewoundHeights(t *testing.T) {
	const height int64 = 10
	ctx := t.Context()
	pool, pv, _ := defaultTestPool(ctx, t, height)

	blockTime := defaultEvidenceTime.Add(time.Duration(height) * time.Minute)
	ev, err := types.NewMockDuplicateVoteEvidenceWithValidator(ctx, height, blockTime, pv, evidenceChainID)
	require.NoError(t, err)

	restore := types.ReplaceRewinds(rewindOver(height))
	require.Error(t, pool.AddEvidence(ctx, ev))
	require.Error(t, pool.CheckEvidence(ctx, types.EvidenceList{ev}))
	restore()

	require.NoError(t, pool.AddEvidence(ctx, ev))
}

func TestReportConflictingVotesSkipsRewoundHeights(t *testing.T) {
	const height int64 = 10
	ctx := t.Context()
	pool, pv, _ := defaultTestPool(ctx, t, height)
	t.Cleanup(types.ReplaceRewinds(rewindOver(height + 1)))

	ev, err := types.NewMockDuplicateVoteEvidenceWithValidator(ctx, height+1, defaultEvidenceTime, pv, evidenceChainID)
	require.NoError(t, err)
	pool.ReportConflictingVotes(ev.VoteA.Vote, ev.VoteB.Vote)

	state := pool.State()
	state.LastBlockHeight++
	state.LastBlockTime = ev.Time()
	state.LastValidators = types.NewValidatorSet([]*types.Validator{types.NewValidator(pv.PrivKey.Public(), 10)})
	pool.Update(ctx, state, []types.Evidence{})

	evList, _ := pool.PendingEvidence(defaultEvidenceMaxBytes)
	require.Empty(t, evList)
}
