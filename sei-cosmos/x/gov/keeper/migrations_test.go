package keeper_test

import (
	"testing"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	govkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/keeper"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

func TestMigrate3to4BackfillsVoteDelegationTracking(t *testing.T) {
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
	for _, voter := range []int{0, 3} {
		store.Delete(govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[voter]))
		store.Delete(govtypes.VoterProposalsKey(addrs[voter], proposal.ProposalId))
	}

	migrator := govkeeper.NewMigrator(app.GovKeeper)
	require.NoError(t, migrator.Migrate3to4(ctx))
	for _, voter := range []int{0, 3} {
		require.True(t, store.Has(govtypes.VoteDelegationsKey(proposal.ProposalId, addrs[voter])))
		require.True(t, store.Has(govtypes.VoterProposalsKey(addrs[voter], proposal.ProposalId)))
	}

	app.GovKeeper.InitializeTally(ctx, proposal)
	validator, found := app.StakingKeeper.GetValidator(ctx, valAddrs[0])
	require.True(t, found)
	delegatedTokens := app.StakingKeeper.TokensFromConsensusPower(ctx, 20)
	_, err = app.StakingKeeper.Delegate(ctx, addrs[3], delegatedTokens, stakingtypes.Unbonded, validator, true)
	require.NoError(t, err)

	complete, processed, _, _, tallyResult := app.GovKeeper.TallyIncremental(ctx, proposal, 2)
	require.True(t, complete)
	require.Equal(t, 2, processed)
	require.True(t, tallyResult.Yes.Equal(app.StakingKeeper.TokensFromConsensusPower(ctx, 5)))
	require.True(t, tallyResult.No.IsZero())
}
