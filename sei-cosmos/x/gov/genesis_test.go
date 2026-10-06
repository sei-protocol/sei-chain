package gov_test

import (
	"context"
	"testing"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
)

func TestImportExportQueues_ErrorUnconsistentState(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	require.Panics(t, func() {
		gov.InitGenesis(ctx, app.AccountKeeper, app.BankKeeper, app.GovKeeper, &types.GenesisState{
			Deposits: types.Deposits{
				{
					ProposalId: 1234,
					Depositor:  "me",
					Amount: sdk.Coins{
						sdk.NewCoin(
							"usei",
							sdk.NewInt(1234),
						),
					},
				},
			},
		})
	})
}

func TestEqualProposals(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	addrs := seiapp.AddTestAddrs(app, ctx, 2, valTokens)

	SortAddresses(addrs)

	app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Header: &tmproto.Header{Height: app.LastBlockHeight() + 1}})

	// Submit two proposals
	proposal := TestProposal
	proposal1, err := app.GovKeeper.SubmitProposal(ctx, proposal)
	require.NoError(t, err)

	proposal2, err := app.GovKeeper.SubmitProposal(ctx, proposal)
	require.NoError(t, err)

	// They are similar but their IDs should be different
	require.NotEqual(t, proposal1, proposal2)
	require.NotEqual(t, proposal1, proposal2)

	// Now create two genesis blocks
	state1 := types.GenesisState{Proposals: []types.Proposal{proposal1}}
	state2 := types.GenesisState{Proposals: []types.Proposal{proposal2}}
	require.NotEqual(t, state1, state2)
	require.False(t, state1.Equal(state2))

	// Now make proposals identical by setting both IDs to 55
	proposal1.ProposalId = 55
	proposal2.ProposalId = 55
	require.Equal(t, proposal1, proposal1)
	require.Equal(t, proposal1, proposal2)

	// Reassign proposals into state
	state1.Proposals[0] = proposal1
	state2.Proposals[0] = proposal2

	// State should be identical now..
	require.Equal(t, state1, state2)
	require.True(t, state1.Equal(state2))
}

func TestInitGenesisRestoresModernTallyRound(t *testing.T) {
	genesisTime := time.Now().UTC()
	proposal := votingPeriodProposal(t, genesisTime)
	genesis := types.DefaultGenesisState()
	genesis.StartingProposalId = proposal.ProposalId + 1
	genesis.Proposals = types.Proposals{proposal}
	genesis.VoteDelegationBackfillCutoff = proposal.ProposalId + 1
	genesis.ModernTallyRoundProposalIds = []uint64{proposal.ProposalId}

	importedApp := seiapp.Setup(t, false, false, false)
	importedCtx := importedApp.BaseApp.NewContext(false, tmproto.Header{Time: genesisTime})
	gov.InitGenesis(importedCtx, importedApp.AccountKeeper, importedApp.BankKeeper, importedApp.GovKeeper, genesis)

	store := importedCtx.KVStore(importedApp.GetKey(types.StoreKey))
	require.True(t, importedApp.GovKeeper.IsModernTallyRound(importedCtx, proposal.ProposalId))
	require.True(t, store.Has(types.ProposalDeadlineKey(proposal.ProposalId, proposal.VotingEndTime)))
	require.False(t, importedApp.GovKeeper.VoteDelegationBackfillRequired(importedCtx, proposal.ProposalId))
}

func TestInitGenesisRejectsFutureTallyElectorate(t *testing.T) {
	genesisTime := time.Now().UTC()
	proposal := votingPeriodProposal(t, genesisTime)
	genesis := types.DefaultGenesisState()
	genesis.StartingProposalId = proposal.ProposalId + 1
	genesis.Proposals = types.Proposals{proposal}
	genesis.TallyElectorates = []types.TallyElectorate{{
		ProposalId:        proposal.ProposalId,
		TotalBondedTokens: sdk.ZeroInt(),
		TallyParams:       types.DefaultTallyParams(),
		TallyValidators:   []types.TallyValidator{},
	}}

	importedApp := seiapp.Setup(t, false, false, false)
	importedCtx := importedApp.BaseApp.NewContext(false, tmproto.Header{Time: genesisTime})
	require.Panics(t, func() {
		gov.InitGenesis(importedCtx, importedApp.AccountKeeper, importedApp.BankKeeper, importedApp.GovKeeper, genesis)
	})
}

func TestInitGenesisResumesFrozenTally(t *testing.T) {
	proposal := votingPeriodProposal(t, time.Now().UTC())
	genesisTime := proposal.VotingEndTime.Add(time.Second)
	voter := sdk.AccAddress(make([]byte, 20))
	validator := sdk.ValAddress(append(make([]byte, 19), 1))
	power := sdk.NewInt(100)
	genesis := types.DefaultGenesisState()
	genesis.StartingProposalId = proposal.ProposalId + 1
	genesis.Proposals = types.Proposals{proposal}
	genesis.VoteDelegationBackfillCutoff = proposal.ProposalId + 1
	genesis.Votes = types.Votes{types.NewVote(proposal.ProposalId, voter, types.NewNonSplitVoteOption(types.OptionYes))}
	genesis.VoteDelegationSnapshots = []types.VoteDelegationSnapshot{{
		ProposalId:  proposal.ProposalId,
		Voter:       voter.String(),
		Delegations: []types.VoteDelegation{{Validator: validator.String(), Shares: power.ToDec()}},
	}}
	genesis.TallyElectorates = []types.TallyElectorate{{
		ProposalId:        proposal.ProposalId,
		TotalBondedTokens: power,
		TallyParams:       types.DefaultTallyParams(),
		TallyValidators: []types.TallyValidator{{
			Address:         validator.String(),
			BondedTokens:    power,
			DelegatorShares: power.ToDec(),
		}},
	}}

	importedApp := seiapp.Setup(t, false, false, false)
	importedCtx := importedApp.BaseApp.NewContext(false, tmproto.Header{Time: genesisTime})
	gov.InitGenesis(importedCtx, importedApp.AccountKeeper, importedApp.BankKeeper, importedApp.GovKeeper, genesis)

	require.False(t, importedApp.GovKeeper.VoteDelegationBackfillRequired(importedCtx, proposal.ProposalId))
	require.True(t, importedApp.GovKeeper.IsTallying(importedCtx, proposal.ProposalId))
	require.Len(t, importedApp.GovKeeper.GetVotes(importedCtx, proposal.ProposalId), 1)
	require.Len(t, importedApp.GovKeeper.GetVoteDelegationSnapshots(importedCtx, proposal), 1)
	require.ErrorIs(t, importedApp.GovKeeper.AddVote(
		importedCtx,
		proposal.ProposalId,
		sdk.AccAddress(append(make([]byte, 19), 2)),
		types.NewNonSplitVoteOption(types.OptionNo),
	), types.ErrInactiveProposal)

	complete, _, passes, burnDeposits, result := importedApp.GovKeeper.TallyIncremental(importedCtx, proposal, 10)
	require.True(t, complete)
	require.True(t, passes)
	require.False(t, burnDeposits)
	require.Equal(t, power, result.Yes)
}

// votingPeriodProposal returns proposal 1, submitted at submitTime and in its
// voting period for the following hour.
func votingPeriodProposal(t *testing.T, submitTime time.Time) types.Proposal {
	t.Helper()
	proposal, err := types.NewProposal(TestProposal, 1, submitTime, submitTime, false)
	require.NoError(t, err)
	proposal.Status = types.StatusVotingPeriod
	proposal.VotingStartTime = submitTime
	proposal.VotingEndTime = submitTime.Add(time.Hour)
	return proposal
}
