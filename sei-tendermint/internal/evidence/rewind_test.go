package evidence_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/tmhash"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/evidence"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func rewindOver(height int64) []types.Rewind {
	return []types.Rewind{{
		ChainID:    evidenceChainID,
		SafeHeight: height - 1,
		Discarded:  []types.DiscardedBlock{{Height: height, Hash: bytes.Repeat([]byte{1}, tmhash.Size)}},
	}}
}

func requireInvalidEvidence(t *testing.T, err error) {
	t.Helper()
	var invalid *types.ErrInvalidEvidence
	require.True(t, errors.As(err, &invalid), "want ErrInvalidEvidence, got %v", err)
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

func TestPoolStartRemovesRewoundPendingEvidence(t *testing.T) {
	const height int64 = 10
	ctx := t.Context()
	val := types.NewMockPV()
	evidenceDB := dbm.NewMemDB()
	stateStore := initializeValidatorState(ctx, t, val, height)
	state, err := stateStore.Load()
	require.NoError(t, err)
	blockStore, err := initializeBlockStore(dbm.NewMemDB(), state, val.PrivKey.Public().Address())
	require.NoError(t, err)
	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(ctx))

	pool := evidence.NewPool(evidenceDB, stateStore, blockStore, eventBus)
	startPool(t, pool, stateStore)
	kept, err := types.NewMockDuplicateVoteEvidenceWithValidator(
		ctx, height-1, defaultEvidenceTime.Add(time.Duration(height-1)*time.Minute), val, evidenceChainID)
	require.NoError(t, err)
	rewound, err := types.NewMockDuplicateVoteEvidenceWithValidator(
		ctx, height, defaultEvidenceTime.Add(time.Duration(height)*time.Minute), val, evidenceChainID)
	require.NoError(t, err)
	require.NoError(t, pool.AddEvidence(ctx, kept))
	require.NoError(t, pool.AddEvidence(ctx, rewound))

	t.Cleanup(types.ReplaceRewinds(rewindOver(height)))
	restarted := evidence.NewPool(evidenceDB, stateStore, blockStore, eventBus)
	startPool(t, restarted, stateStore)

	evList, _ := restarted.PendingEvidence(defaultEvidenceMaxBytes)
	require.Equal(t, []types.Evidence{kept}, evList)
	require.Equal(t, uint32(1), restarted.Size())
	requireInvalidEvidence(t, restarted.CheckEvidence(ctx, types.EvidenceList{rewound}))
}

func TestCheckEvidenceRefusesPendingRewoundEvidence(t *testing.T) {
	const height int64 = 10
	ctx := t.Context()
	pool, pv, _ := defaultTestPool(ctx, t, height)

	ev, err := types.NewMockDuplicateVoteEvidenceWithValidator(
		ctx, height, defaultEvidenceTime.Add(time.Duration(height)*time.Minute), pv, evidenceChainID)
	require.NoError(t, err)
	require.NoError(t, pool.AddEvidence(ctx, ev))

	t.Cleanup(types.ReplaceRewinds(rewindOver(height)))
	requireInvalidEvidence(t, pool.CheckEvidence(ctx, types.EvidenceList{ev}))
	requireInvalidEvidence(t, pool.AddEvidence(ctx, ev))
}
