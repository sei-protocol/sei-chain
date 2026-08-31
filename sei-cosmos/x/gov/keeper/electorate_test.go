package keeper_test

import (
	"testing"
	"time"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

func TestGapTallyBoundaryFreezesElectorateBeforeNextBlockMutations(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	initialTime := time.Unix(100, 0)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Time: initialTime})
	addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})
	proposal := createVotingProposalEndingAt(t, ctx, app, initialTime.Add(5*time.Second))
	delegateAndVoteYes(t, ctx, app, proposal.ProposalId, addrs[3], valAddrs[0], 2)
	_, _, expected := app.GovKeeper.Tally(ctx, proposal)

	app.GovKeeper.InitializeDeadlineBoundaryClock(ctx)
	nextCtx := ctx.WithBlockTime(initialTime.Add(10 * time.Second))
	app.GovKeeper.CaptureGapTallyBoundary(nextCtx)
	delegateToValidator(t, nextCtx, app, addrs[3], valAddrs[0], 20)

	complete, processed, _, _, result := app.GovKeeper.TallyIncremental(nextCtx, proposal, 1)
	require.True(t, complete)
	require.Equal(t, 1, processed)
	require.True(t, expected.Equals(result))
}

func TestExactTallyBoundaryFreezesBeforeLaterEndBlockMutations(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	blockTime := time.Unix(100, 0)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Time: blockTime})
	addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})

	first := createVotingProposalEndingAt(t, ctx, app, blockTime)
	second := createVotingProposalEndingAt(t, ctx, app, blockTime)
	delegateAndVoteYes(t, ctx, app, first.ProposalId, addrs[3], valAddrs[0], 2)
	require.NoError(t, app.GovKeeper.AddVote(
		ctx,
		second.ProposalId,
		addrs[3],
		govtypes.NewNonSplitVoteOption(govtypes.OptionYes),
	))
	_, _, expected := app.GovKeeper.Tally(ctx, second)

	app.GovKeeper.CaptureExactTallyBoundary(ctx)
	require.Equal(t, 1, countStorePrefix(ctx, app, govtypes.TallyBoundaryMetaKeyPrefix))
	delegateToValidator(t, ctx, app, addrs[3], valAddrs[0], 20)

	complete, processed, _, _, result := app.GovKeeper.TallyIncremental(ctx, first, 1)
	require.True(t, complete)
	require.Equal(t, 1, processed)
	require.True(t, expected.Equals(result))
	app.GovKeeper.RemoveFromActiveProposalQueue(ctx, first.ProposalId, first.VotingEndTime)
	require.Equal(t, 1, countStorePrefix(ctx, app, govtypes.TallyBoundaryMetaKeyPrefix))

	complete, processed, _, _, result = app.GovKeeper.TallyIncremental(ctx, second, 1)
	require.True(t, complete)
	require.Equal(t, 1, processed)
	require.True(t, expected.Equals(result))
	app.GovKeeper.RemoveFromActiveProposalQueue(ctx, second.ProposalId, second.VotingEndTime)
	require.Zero(t, countStorePrefix(ctx, app, govtypes.TallyBoundaryMetaKeyPrefix))
}

func TestTallyOnlyWaitsForDelegationUpdatesThroughItsBoundary(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	blockTime := time.Unix(100, 0)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Time: blockTime})
	addrs, valAddrs := createValidators(t, ctx, app, []int64{5, 5, 5})
	proposal := createVotingProposalEndingAt(t, ctx, app, blockTime)
	delegateAndVoteYes(t, ctx, app, proposal.ProposalId, addrs[3], valAddrs[0], 2)

	firstShares := queueDelegationShareUpdate(t, ctx, app, addrs[3], valAddrs[0], sdk.OneDec())
	app.GovKeeper.CaptureExactTallyBoundary(ctx)
	queueDelegationShareUpdate(t, ctx, app, addrs[3], valAddrs[0], sdk.OneDec())

	complete, processed, _, _, _ := app.GovKeeper.TallyIncremental(ctx, proposal, 2)
	require.True(t, complete)
	require.Equal(t, 2, processed)
	require.True(t, app.GovKeeper.HasPendingVoteDelegationUpdates(ctx))
	archivedSnapshot := decodeVoteDelegationSnapshot(
		t,
		ctx.KVStore(app.GetKey(govtypes.StoreKey)).Get(
			govtypes.TallyVoteDelegationsKey(proposal.ProposalId, false, addrs[3]),
		),
	)
	requireVoteDelegationShares(
		t,
		[]govtypes.VoteDelegationSnapshot{archivedSnapshot},
		valAddrs[0],
		firstShares,
	)
}

func createVotingProposalEndingAt(
	t *testing.T,
	ctx sdk.Context,
	app *seiapp.App,
	endTime time.Time,
) govtypes.Proposal {
	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	app.GovKeeper.ActivateVotingPeriod(ctx, proposal)
	proposal, found := app.GovKeeper.GetProposal(ctx, proposal.ProposalId)
	require.True(t, found)
	app.GovKeeper.RemoveFromActiveProposalQueue(ctx, proposal.ProposalId, proposal.VotingEndTime)
	proposal.VotingEndTime = endTime
	app.GovKeeper.SetProposal(ctx, proposal)
	app.GovKeeper.InsertActiveProposalQueue(ctx, proposal.ProposalId, endTime)
	return proposal
}

func delegateAndVoteYes(
	t *testing.T,
	ctx sdk.Context,
	app *seiapp.App,
	proposalID uint64,
	voter sdk.AccAddress,
	validator sdk.ValAddress,
	power int64,
) {
	delegateToValidator(t, ctx, app, voter, validator, power)
	require.NoError(t, app.GovKeeper.AddVote(
		ctx,
		proposalID,
		voter,
		govtypes.NewNonSplitVoteOption(govtypes.OptionYes),
	))
}

func delegateToValidator(
	t *testing.T,
	ctx sdk.Context,
	app *seiapp.App,
	delegator sdk.AccAddress,
	validatorAddress sdk.ValAddress,
	power int64,
) {
	validator, found := app.StakingKeeper.GetValidator(ctx, validatorAddress)
	require.True(t, found)
	_, err := app.StakingKeeper.Delegate(
		ctx,
		delegator,
		app.StakingKeeper.TokensFromConsensusPower(ctx, power),
		stakingtypes.Unbonded,
		validator,
		true,
	)
	require.NoError(t, err)
}

func queueDelegationShareUpdate(
	t *testing.T,
	ctx sdk.Context,
	app *seiapp.App,
	delegator sdk.AccAddress,
	validator sdk.ValAddress,
	delta sdk.Dec,
) sdk.Dec {
	delegation, found := app.StakingKeeper.GetDelegation(ctx, delegator, validator)
	require.True(t, found)
	delegation.Shares = delegation.Shares.Sub(delta)
	app.StakingKeeper.SetDelegation(ctx, delegation)
	app.GovKeeper.StakingHooks().AfterDelegationModified(
		stakingtypes.WithSlashDelegationModification(ctx),
		delegator,
		validator,
	)
	return delegation.Shares
}

func countStorePrefix(tctx sdk.Context, app *seiapp.App, prefix []byte) int {
	iterator := sdk.KVStorePrefixIterator(tctx.KVStore(app.GetKey(govtypes.StoreKey)), prefix)
	defer func() { _ = iterator.Close() }()
	count := 0
	for ; iterator.Valid(); iterator.Next() {
		count++
	}
	return count
}
