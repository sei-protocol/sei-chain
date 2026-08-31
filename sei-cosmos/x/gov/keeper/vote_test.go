package keeper_test

import (
	"encoding/hex"
	"testing"
	"time"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

func TestVotes(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	addrs := seiapp.AddTestAddrsIncremental(app, ctx, 5, sdk.NewInt(30000000))

	tp := TestProposal
	proposal, err := app.GovKeeper.SubmitProposal(ctx, tp)
	require.NoError(t, err)
	proposalID := proposal.ProposalId

	var invalidOption types.VoteOption = 0x10

	require.Error(t, app.GovKeeper.AddVote(ctx, proposalID, addrs[0], types.NewNonSplitVoteOption(types.OptionYes)), "proposal not on voting period")
	require.Error(t, app.GovKeeper.AddVote(ctx, 10, addrs[0], types.NewNonSplitVoteOption(types.OptionYes)), "invalid proposal ID")

	proposal.Status = types.StatusVotingPeriod
	app.GovKeeper.SetProposal(ctx, proposal)

	require.Error(t, app.GovKeeper.AddVote(ctx, proposalID, addrs[0], types.NewNonSplitVoteOption(invalidOption)), "invalid option")

	// Test first vote
	require.NoError(t, app.GovKeeper.AddVote(ctx, proposalID, addrs[0], types.NewNonSplitVoteOption(types.OptionAbstain)))
	vote, found := app.GovKeeper.GetVote(ctx, proposalID, addrs[0])
	require.True(t, found)
	require.Equal(t, addrs[0].String(), vote.Voter)
	require.Equal(t, proposalID, vote.ProposalId)
	require.True(t, len(vote.Options) == 1)
	require.Equal(t, types.OptionAbstain, vote.Options[0].Option)
	require.Equal(t, types.OptionAbstain, vote.Option)

	// Test change of vote
	require.NoError(t, app.GovKeeper.AddVote(ctx, proposalID, addrs[0], types.NewNonSplitVoteOption(types.OptionYes)))
	vote, found = app.GovKeeper.GetVote(ctx, proposalID, addrs[0])
	require.True(t, found)
	require.Equal(t, addrs[0].String(), vote.Voter)
	require.Equal(t, proposalID, vote.ProposalId)
	require.True(t, len(vote.Options) == 1)
	require.Equal(t, types.OptionYes, vote.Options[0].Option)
	require.Equal(t, types.OptionYes, vote.Option)

	// Test second vote
	require.NoError(t, app.GovKeeper.AddVote(ctx, proposalID, addrs[1], types.WeightedVoteOptions{
		types.WeightedVoteOption{Option: types.OptionYes, Weight: sdk.NewDecWithPrec(60, 2)},
		types.WeightedVoteOption{Option: types.OptionNo, Weight: sdk.NewDecWithPrec(30, 2)},
		types.WeightedVoteOption{Option: types.OptionAbstain, Weight: sdk.NewDecWithPrec(5, 2)},
		types.WeightedVoteOption{Option: types.OptionNoWithVeto, Weight: sdk.NewDecWithPrec(5, 2)},
	}))
	vote, found = app.GovKeeper.GetVote(ctx, proposalID, addrs[1])
	require.True(t, found)
	require.Equal(t, addrs[1].String(), vote.Voter)
	require.Equal(t, proposalID, vote.ProposalId)
	require.True(t, len(vote.Options) == 4)
	require.Equal(t, types.OptionYes, vote.Options[0].Option)
	require.Equal(t, types.OptionNo, vote.Options[1].Option)
	require.Equal(t, types.OptionAbstain, vote.Options[2].Option)
	require.Equal(t, types.OptionNoWithVeto, vote.Options[3].Option)
	require.True(t, vote.Options[0].Weight.Equal(sdk.NewDecWithPrec(60, 2)))
	require.True(t, vote.Options[1].Weight.Equal(sdk.NewDecWithPrec(30, 2)))
	require.True(t, vote.Options[2].Weight.Equal(sdk.NewDecWithPrec(5, 2)))
	require.True(t, vote.Options[3].Weight.Equal(sdk.NewDecWithPrec(5, 2)))
	require.Equal(t, types.OptionEmpty, vote.Option)

	// Test vote iterator
	// NOTE order of deposits is determined by the addresses
	votes := app.GovKeeper.GetAllVotes(ctx)
	require.Len(t, votes, 2)
	require.Equal(t, votes, app.GovKeeper.GetVotes(ctx, proposalID))
	require.Equal(t, addrs[0].String(), votes[0].Voter)
	require.Equal(t, proposalID, votes[0].ProposalId)
	require.True(t, len(votes[0].Options) == 1)
	require.Equal(t, types.OptionYes, votes[0].Options[0].Option)
	require.Equal(t, addrs[1].String(), votes[1].Voter)
	require.Equal(t, proposalID, votes[1].ProposalId)
	require.True(t, len(votes[1].Options) == 4)
	require.True(t, votes[1].Options[0].Weight.Equal(sdk.NewDecWithPrec(60, 2)))
	require.True(t, votes[1].Options[1].Weight.Equal(sdk.NewDecWithPrec(30, 2)))
	require.True(t, votes[1].Options[2].Weight.Equal(sdk.NewDecWithPrec(5, 2)))
	require.True(t, votes[1].Options[3].Weight.Equal(sdk.NewDecWithPrec(5, 2)))
	require.Equal(t, types.OptionEmpty, vote.Option)
}

func TestAddVoteRejectsBlocksAfterVotingEnd(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	addrs := seiapp.AddTestAddrsIncremental(app, ctx, 2, sdk.NewInt(30000000))

	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	proposal.Status = types.StatusVotingPeriod
	proposal.VotingEndTime = ctx.BlockTime().Add(time.Second)
	app.GovKeeper.SetProposal(ctx, proposal)
	app.GovKeeper.InsertActiveProposalQueue(ctx, proposal.ProposalId, proposal.VotingEndTime)

	atVotingEnd := ctx.WithBlockTime(proposal.VotingEndTime)
	require.NoError(t, app.GovKeeper.AddVote(
		atVotingEnd,
		proposal.ProposalId,
		addrs[0],
		types.NewNonSplitVoteOption(types.OptionYes),
	))
	app.GovKeeper.CaptureExactTallyBoundary(atVotingEnd)
	require.ErrorIs(t, app.GovKeeper.AddVote(
		atVotingEnd,
		proposal.ProposalId,
		addrs[1],
		types.NewNonSplitVoteOption(types.OptionYes),
	), types.ErrInactiveProposal)

	afterVotingEnd := ctx.WithBlockTime(proposal.VotingEndTime.Add(time.Second))
	require.ErrorIs(t, app.GovKeeper.AddVote(
		afterVotingEnd,
		proposal.ProposalId,
		addrs[1],
		types.NewNonSplitVoteOption(types.OptionYes),
	), types.ErrInactiveProposal)
}

func TestVoteDelegationTrackingPreservesHistoricalTraces(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{}).WithBlockTime(time.Unix(100, 0))
	addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})

	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	proposal.Status = types.StatusVotingPeriod
	proposal.VotingEndTime = ctx.BlockTime().Add(-time.Second)
	app.GovKeeper.SetProposal(ctx, proposal)

	store := ctx.KVStore(app.GetKey(types.StoreKey))
	store.Delete(types.IncrementalTallyEnabledKey)
	legacyCtx := ctx.WithIsTracing(true).WithClosestUpgradeName("v6.7")
	require.NoError(t, app.GovKeeper.AddVote(
		legacyCtx,
		proposal.ProposalId,
		addrs[0],
		types.NewNonSplitVoteOption(types.OptionYes),
	))
	require.False(t, store.Has(types.VoterProposalsKey(addrs[0], proposal.ProposalId)))
	require.False(t, store.Has(types.VoteDelegationsKey(proposal.ProposalId, addrs[0])))
	require.Len(t, app.GovKeeper.GetAllVotes(legacyCtx), 1)
	require.NotPanics(t, func() {
		_, _, _ = app.GovKeeper.Tally(legacyCtx, proposal)
	})

	gasBeforeHook := legacyCtx.GasMeter().GasConsumed()
	app.GovKeeper.StakingHooks().AfterDelegationModified(legacyCtx, addrs[0], valAddrs[0])
	require.Equal(t, gasBeforeHook, legacyCtx.GasMeter().GasConsumed())
	tracer, ok := legacyCtx.StoreTracer().(interface{ Dump() sdk.StoreTraceDump })
	require.True(t, ok)
	trace := tracer.Dump()
	require.NotContains(t, trace.Modules[types.ModuleName].Has, hex.EncodeToString(types.IncrementalTallyEnabledKey))

	proposal.VotingEndTime = ctx.BlockTime().Add(time.Second)
	app.GovKeeper.SetProposal(ctx, proposal)
	app.GovKeeper.EnableIncrementalTally(ctx)
	currentCtx := ctx.WithIsTracing(true).WithClosestUpgradeName("v6.6")
	require.NoError(t, app.GovKeeper.AddVote(
		currentCtx,
		proposal.ProposalId,
		addrs[0],
		types.NewNonSplitVoteOption(types.OptionYes),
	))
	require.True(t, store.Has(types.VoterProposalsKey(addrs[0], proposal.ProposalId)))
	require.True(t, store.Has(types.VoteDelegationsKey(proposal.ProposalId, addrs[0])))

	store.Delete(types.IncrementalTallyEnabledKey)
	emptyProposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	emptyProposal.Status = types.StatusVotingPeriod
	app.GovKeeper.SetProposal(ctx, emptyProposal)
	complete, _, _, _, _ := app.GovKeeper.TallyIncremental(legacyCtx, emptyProposal, 1)
	require.True(t, complete)
	require.False(t, store.Has(types.ProposalTallyBoundaryKey(emptyProposal.ProposalId)))

	initializedProposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	initializedProposal.Status = types.StatusVotingPeriod
	app.GovKeeper.SetProposal(ctx, initializedProposal)
	app.GovKeeper.InitializeTally(legacyCtx, initializedProposal)
	require.False(t, store.Has(types.ProposalTallyBoundaryKey(initializedProposal.ProposalId)))

	boundaryIterator := sdk.KVStorePrefixIterator(store, types.TallyBoundaryMetaKeyPrefix)
	require.False(t, boundaryIterator.Valid())
	require.NoError(t, boundaryIterator.Close())
}

func TestVoteDelegationSnapshotsFreezeAfterVotingEnd(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{}).WithBlockTime(time.Unix(100, 0))
	addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})

	expiredProposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	expiredProposal.Status = types.StatusVotingPeriod
	expiredProposal.VotingEndTime = ctx.BlockTime().Add(-time.Second)
	app.GovKeeper.SetProposal(ctx, expiredProposal)

	activeProposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	activeProposal.Status = types.StatusVotingPeriod
	activeProposal.VotingEndTime = ctx.BlockTime().Add(time.Second)
	app.GovKeeper.SetProposal(ctx, activeProposal)

	voteCtx := ctx.WithBlockTime(ctx.BlockTime().Add(-2 * time.Second))
	for _, proposal := range []types.Proposal{expiredProposal, activeProposal} {
		require.NoError(t, app.GovKeeper.AddVote(
			voteCtx,
			proposal.ProposalId,
			addrs[3],
			types.NewNonSplitVoteOption(types.OptionYes),
		))
		require.False(t, app.GovKeeper.IsTallying(ctx, proposal.ProposalId))
	}

	store := ctx.KVStore(app.GetKey(types.StoreKey))
	expiredSnapshotKey := types.VoteDelegationsKey(expiredProposal.ProposalId, addrs[3])
	activeSnapshotKey := types.VoteDelegationsKey(activeProposal.ProposalId, addrs[3])
	expiredSnapshot := append([]byte(nil), store.Get(expiredSnapshotKey)...)
	activeSnapshot := append([]byte(nil), store.Get(activeSnapshotKey)...)

	validator, found := app.StakingKeeper.GetValidator(ctx, valAddrs[0])
	require.True(t, found)
	delegatedTokens := app.StakingKeeper.TokensFromConsensusPower(ctx, 20)
	_, err = app.StakingKeeper.Delegate(ctx, addrs[3], delegatedTokens, stakingtypes.Unbonded, validator, true)
	require.NoError(t, err)

	require.Equal(t, expiredSnapshot, store.Get(expiredSnapshotKey))
	require.NotEqual(t, activeSnapshot, store.Get(activeSnapshotKey))
}
