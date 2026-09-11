package keeper_test

import (
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	gov "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov"
	govkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/keeper"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

func TestMigrate3to4SchedulesBoundedVoteDelegationBackfill(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})

	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	proposal.Status = govtypes.StatusVotingPeriod
	app.GovKeeper.SetProposal(ctx, proposal)
	require.NoError(t, app.GovKeeper.AddVote(
		ctx,
		proposal.ProposalId,
		addrs[0],
		govtypes.NewNonSplitVoteOption(govtypes.OptionYes),
	))
	require.NoError(t, app.GovKeeper.AddVote(
		ctx,
		proposal.ProposalId,
		addrs[3],
		govtypes.NewNonSplitVoteOption(govtypes.OptionNo),
	))

	store := ctx.KVStore(app.GetKey(govtypes.StoreKey))
	store.Delete(govtypes.IncrementalTallyEnabledKey)
	store.Delete(govtypes.DeadlineBoundaryBlockTimeKey)
	for _, voter := range []int{0, 3} {
		store.Delete(govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[voter]))
		store.Delete(govtypes.VoterProposalsKey(addrs[voter], proposal.ProposalId))
	}

	migrator := govkeeper.NewMigrator(app.GovKeeper)
	require.NoError(t, migrator.Migrate3to4(ctx))
	require.True(t, store.Has(govtypes.IncrementalTallyEnabledKey))
	require.Equal(t, sdk.FormatTimeBytes(ctx.BlockTime()), store.Get(govtypes.DeadlineBoundaryBlockTimeKey))
	cutoff, found := app.GovKeeper.GetVoteDelegationBackfillCutoff(ctx)
	require.True(t, found)
	require.Equal(t, uint64(2), cutoff)
	require.False(t, app.GovKeeper.IsVoteDelegationBackfillInProgress(ctx, proposal.ProposalId))
	for _, voter := range []int{0, 3} {
		require.False(t, store.Has(govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[voter])))
		require.False(t, store.Has(govtypes.VoterProposalsKey(addrs[voter], proposal.ProposalId)))
	}

	newProposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	newProposal.Status = govtypes.StatusVotingPeriod
	app.GovKeeper.SetProposal(ctx, newProposal)
	require.NoError(t, app.GovKeeper.AddVote(
		ctx,
		newProposal.ProposalId,
		addrs[1],
		govtypes.NewNonSplitVoteOption(govtypes.OptionAbstain),
	))
	backfillComplete, backfilled := app.GovKeeper.BackfillVoteDelegationTracking(ctx, newProposal.ProposalId, 1)
	require.True(t, backfillComplete)
	require.Zero(t, backfilled)

	backfillComplete, backfilled = app.GovKeeper.BackfillVoteDelegationTracking(ctx, proposal.ProposalId, 0)
	require.False(t, backfillComplete)
	require.Zero(t, backfilled)
	require.True(t, app.GovKeeper.IsVoteDelegationBackfillInProgress(ctx, proposal.ProposalId))

	complete, processed, _, _, _ := app.GovKeeper.TallyIncremental(ctx, proposal, 1)
	require.False(t, complete)
	require.Equal(t, 1, processed)
	require.False(t, app.GovKeeper.IsTallying(ctx, proposal.ProposalId))
	require.True(t, app.GovKeeper.IsVoteDelegationBackfillInProgress(ctx, proposal.ProposalId))
	tracked := 0
	for _, voter := range []int{0, 3} {
		if store.Has(govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[voter])) {
			tracked++
			require.True(t, store.Has(govtypes.VoterProposalsKey(addrs[voter], proposal.ProposalId)))
		}
	}
	require.Equal(t, 1, tracked)
	require.True(t, store.Has(govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[0])))
	require.ErrorIs(t, app.GovKeeper.AddVote(
		ctx,
		proposal.ProposalId,
		addrs[2],
		govtypes.NewNonSplitVoteOption(govtypes.OptionYes),
	), govtypes.ErrInactiveProposal)

	validator, found := app.StakingKeeper.GetValidator(ctx, valAddrs[0])
	require.True(t, found)
	snapshotKey := govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[0])
	snapshotBeforeDelegation := append([]byte(nil), store.Get(snapshotKey)...)
	delegatedTokens := app.StakingKeeper.TokensFromConsensusPower(ctx, 20)
	_, err = app.StakingKeeper.Delegate(ctx, addrs[0], delegatedTokens, stakingtypes.Unbonded, validator, true)
	require.NoError(t, err)
	require.NotEqual(t, snapshotBeforeDelegation, store.Get(snapshotKey))

	require.NotPanics(t, func() {
		_, _, _ = app.GovKeeper.Tally(ctx, proposal)
	})
	genesis := gov.ExportGenesis(ctx, app.GovKeeper)
	require.Len(t, genesis.Votes, 3)
	require.Len(t, genesis.VoteDelegationSnapshots, 3)
	require.Equal(t, cutoff, genesis.VoteDelegationBackfillCutoff)

	lateDelegatedTokens := app.StakingKeeper.TokensFromConsensusPower(ctx, 10)
	validator, found = app.StakingKeeper.GetValidator(ctx, valAddrs[0])
	require.True(t, found)
	_, err = app.StakingKeeper.Delegate(ctx, addrs[3], lateDelegatedTokens, stakingtypes.Unbonded, validator, true)
	require.NoError(t, err)

	complete, processed, _, _, _ = app.GovKeeper.TallyIncremental(ctx, proposal, 1)
	require.False(t, complete)
	require.Equal(t, 1, processed)
	require.True(t, app.GovKeeper.IsTallying(ctx, proposal.ProposalId))
	require.False(t, app.GovKeeper.IsVoteDelegationBackfillInProgress(ctx, proposal.ProposalId))
	for _, voter := range []int{0, 3} {
		require.True(t, store.Has(govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[voter])))
		require.True(t, store.Has(govtypes.VoterProposalsKey(addrs[voter], proposal.ProposalId)))
	}

	validator, found = app.StakingKeeper.GetValidator(ctx, valAddrs[0])
	require.True(t, found)
	_, err = app.StakingKeeper.Delegate(ctx, addrs[3], delegatedTokens, stakingtypes.Unbonded, validator, true)
	require.NoError(t, err)

	complete, processed, _, _, tallyResult := app.GovKeeper.TallyIncremental(ctx, proposal, 2)
	require.True(t, complete)
	require.Equal(t, 2, processed)
	require.Equal(t, app.StakingKeeper.TokensFromConsensusPower(ctx, 25).String(), tallyResult.Yes.String())
	require.Equal(t, lateDelegatedTokens.String(), tallyResult.No.String())
}
