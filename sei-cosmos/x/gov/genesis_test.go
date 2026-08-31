package gov_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov"
	govkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/keeper"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

func TestImportExportQueues(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	addrs := seiapp.AddTestAddrs(app, ctx, 2, valTokens)

	SortAddresses(addrs)

	app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Header: &tmproto.Header{Height: app.LastBlockHeight() + 1}})

	ctx = app.BaseApp.NewContext(false, tmproto.Header{})

	// Create two proposals, put the second into the voting period
	proposal := TestProposal
	proposal1, err := app.GovKeeper.SubmitProposal(ctx, proposal)
	require.NoError(t, err)
	proposalID1 := proposal1.ProposalId

	proposal2, err := app.GovKeeper.SubmitProposal(ctx, proposal)
	require.NoError(t, err)
	proposalID2 := proposal2.ProposalId

	votingStarted, err := app.GovKeeper.AddDeposit(ctx, proposalID2, addrs[0], app.GovKeeper.GetDepositParams(ctx).MinDeposit)
	require.NoError(t, err)
	require.True(t, votingStarted)

	proposal1, ok := app.GovKeeper.GetProposal(ctx, proposalID1)
	require.True(t, ok)
	proposal2, ok = app.GovKeeper.GetProposal(ctx, proposalID2)
	require.True(t, ok)
	require.True(t, proposal1.Status == types.StatusDepositPeriod)
	require.True(t, proposal2.Status == types.StatusVotingPeriod)

	authGenState := auth.ExportGenesis(ctx, app.AccountKeeper)
	bankGenState := app.BankKeeper.ExportGenesis(ctx)

	// export the state and import it into a new app
	govGenState := gov.ExportGenesis(ctx, app.GovKeeper)
	genesisState := seiapp.NewDefaultGenesisState(app.AppCodec())

	genesisState[authtypes.ModuleName] = app.AppCodec().MustMarshalJSON(authGenState)
	genesisState[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenState)
	genesisState[types.ModuleName] = app.AppCodec().MustMarshalJSON(govGenState)

	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	if err != nil {
		panic(err)
	}

	db := dbm.NewMemDB()
	app2 := seiapp.SetupWithDB(t, db, false, false, false)

	app2.InitChain(&abci.RequestInitChain{
		ConsensusParams: seiapp.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	},
	)

	app2.Commit(context.Background())
	app2.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Header: &tmproto.Header{Height: app2.LastBlockHeight() + 1}})

	ctx2 := app2.BaseApp.NewContext(false, tmproto.Header{})

	// Jump the time forward past the DepositPeriod and VotingPeriod
	ctx2 = ctx2.WithBlockTime(ctx2.BlockHeader().Time.Add(app2.GovKeeper.GetDepositParams(ctx2).MaxDepositPeriod).Add(app2.GovKeeper.GetVotingParams(ctx2).VotingPeriod))

	// Make sure that they are still in the DepositPeriod and VotingPeriod respectively
	proposal1, ok = app2.GovKeeper.GetProposal(ctx2, proposalID1)
	require.True(t, ok)
	proposal2, ok = app2.GovKeeper.GetProposal(ctx2, proposalID2)
	require.True(t, ok)
	require.True(t, proposal1.Status == types.StatusDepositPeriod)
	require.True(t, proposal2.Status == types.StatusVotingPeriod)

	macc := app2.GovKeeper.GetGovernanceAccount(ctx2)
	require.Equal(t, app2.GovKeeper.GetDepositParams(ctx2).MinDeposit, app2.BankKeeper.GetAllBalances(ctx2, macc.GetAddress()))

	// Run the endblocker. Check to make sure that proposal1 is removed from state, and proposal2 is finished VotingPeriod.
	gov.EndBlocker(ctx2, app2.GovKeeper)

	proposal1, ok = app2.GovKeeper.GetProposal(ctx2, proposalID1)
	require.False(t, ok)

	proposal2, ok = app2.GovKeeper.GetProposal(ctx2, proposalID2)
	require.True(t, ok)
	require.True(t, proposal2.Status == types.StatusRejected)
}

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

func TestExportGenesisIncludesVotesFromUnfinishedTally(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	addrs := seiapp.AddTestAddrs(app, ctx, 3, valTokens)
	SortAddresses(addrs)
	stakingParams := app.StakingKeeper.GetParams(ctx)
	stakingParams.MinCommissionRate = sdk.ZeroDec()
	app.StakingKeeper.SetParams(ctx, stakingParams)
	createValidators(
		t,
		staking.NewHandler(app.StakingKeeper),
		ctx,
		seiapp.ConvertAddrsToValAddrs(addrs),
		[]int64{6, 3, 1},
	)
	staking.EndBlocker(ctx, app.StakingKeeper)

	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	app.GovKeeper.ActivateVotingPeriod(ctx, proposal)
	proposal, found := app.GovKeeper.GetProposal(ctx, proposal.ProposalId)
	require.True(t, found)

	for i, option := range []types.VoteOption{types.OptionYes, types.OptionNo, types.OptionAbstain} {
		require.NoError(t, app.GovKeeper.AddVote(
			ctx,
			proposal.ProposalId,
			addrs[i],
			types.NewNonSplitVoteOption(option),
		))
	}

	ctx = ctx.WithBlockTime(proposal.VotingEndTime)
	app.GovKeeper.CaptureExactTallyBoundary(ctx)
	complete, processed, _, _, _ := app.GovKeeper.TallyIncremental(ctx, proposal, 1)
	require.False(t, complete)
	require.Equal(t, 1, processed)

	validator, found := app.StakingKeeper.GetValidator(ctx, seiapp.ConvertAddrsToValAddrs(addrs)[0])
	require.True(t, found)
	_, err = app.StakingKeeper.Delegate(
		ctx,
		addrs[0],
		app.StakingKeeper.TokensFromConsensusPower(ctx, 20),
		stakingtypes.Unbonded,
		validator,
		true,
	)
	require.NoError(t, err)
	mutatedTallyParams := app.GovKeeper.GetTallyParams(ctx)
	mutatedTallyParams.Threshold = sdk.MustNewDecFromStr("0.90")
	mutatedTallyParams.ExpeditedThreshold = sdk.MustNewDecFromStr("0.95")
	app.GovKeeper.SetTallyParams(ctx, mutatedTallyParams)

	genesis := gov.ExportGenesis(ctx, app.GovKeeper)
	require.Len(t, genesis.Votes, 3)
	require.Len(t, genesis.VoteDelegationSnapshots, 3)
	require.Len(t, genesis.TallyElectorates, 1)
	genesisJSON := app.AppCodec().MustMarshalJSON(genesis)
	var decodedGenesis types.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesisJSON, &decodedGenesis)
	require.True(t, genesis.Equal(decodedGenesis))
	genesis = &decodedGenesis

	authGenesis := auth.ExportGenesis(ctx, app.AccountKeeper)
	bankGenesis := app.BankKeeper.ExportGenesis(ctx)
	stakingGenesis := staking.ExportGenesis(ctx, app.StakingKeeper)
	appGenesis := seiapp.NewDefaultGenesisState(app.AppCodec())
	appGenesis[authtypes.ModuleName] = app.AppCodec().MustMarshalJSON(authGenesis)
	appGenesis[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)
	appGenesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)
	appGenesis[types.ModuleName] = app.AppCodec().MustMarshalJSON(genesis)
	stateBytes, err := json.MarshalIndent(appGenesis, "", " ")
	require.NoError(t, err)

	complete, processed, sourcePasses, sourceBurnDeposits, sourceTallyResult := app.GovKeeper.TallyIncremental(ctx, proposal, 2)
	require.True(t, complete)
	require.Equal(t, 2, processed)
	require.True(t, sourcePasses)
	require.False(t, sourceBurnDeposits)
	require.False(t, sourceTallyResult.Equals(types.EmptyTallyResult()))

	db := dbm.NewMemDB()
	importedApp := seiapp.SetupWithDB(t, db, true, false, false)
	_, err = importedApp.InitChain(&abci.RequestInitChain{
		ConsensusParams: seiapp.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
		Time:            proposal.VotingEndTime,
	})
	require.NoError(t, err)
	importedApp.Commit(context.Background())
	importedCtx := importedApp.BaseApp.NewUncachedContext(false, tmproto.Header{Time: proposal.VotingEndTime})

	require.True(t, importedApp.GovKeeper.IsTallying(importedCtx, proposal.ProposalId))
	require.Len(t, importedApp.GovKeeper.GetVotes(importedCtx, proposal.ProposalId), 3)
	require.Len(t, importedApp.GovKeeper.GetVoteDelegationSnapshots(importedCtx, proposal), 3)
	newVoter := make(sdk.AccAddress, 20)
	binary.BigEndian.PutUint64(newVoter[12:], 4)
	err = importedApp.GovKeeper.AddVote(
		importedCtx,
		proposal.ProposalId,
		newVoter,
		types.NewNonSplitVoteOption(types.OptionNo),
	)
	require.ErrorIs(t, err, types.ErrInactiveProposal)

	complete, processed, importedPasses, importedBurnDeposits, importedTallyResult := importedApp.GovKeeper.TallyIncremental(
		importedCtx,
		proposal,
		3,
	)
	require.True(t, complete)
	require.Equal(t, 3, processed)
	require.Equal(t, sourcePasses, importedPasses)
	require.Equal(t, sourceBurnDeposits, importedBurnDeposits)
	require.True(t, sourceTallyResult.Equals(importedTallyResult))
}

func TestImportExportPreservesUnresolvedLegacyTally(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	voter := seiapp.AddTestAddrs(app, ctx, 1, valTokens)[0]

	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	app.GovKeeper.ActivateVotingPeriod(ctx, proposal)
	proposal, found := app.GovKeeper.GetProposal(ctx, proposal.ProposalId)
	require.True(t, found)
	require.NoError(t, app.GovKeeper.AddVote(
		ctx,
		proposal.ProposalId,
		voter,
		types.NewNonSplitVoteOption(types.OptionYes),
	))

	store := ctx.KVStore(app.GetKey(types.StoreKey))
	store.Delete(types.VoteDelegationsKey(proposal.ProposalId, voter))
	store.Delete(types.VoterProposalsKey(voter, proposal.ProposalId))
	require.NoError(t, govkeeper.NewMigrator(app.GovKeeper).Migrate3to4(ctx))

	ctx = ctx.WithBlockTime(proposal.VotingEndTime.Add(time.Second))
	genesis := gov.ExportGenesis(ctx, app.GovKeeper)
	require.Equal(t, uint64(2), genesis.VoteDelegationBackfillCutoff)
	require.Len(t, genesis.TallyElectorates, 1)
	genesisJSON := app.AppCodec().MustMarshalJSON(genesis)
	var decodedGenesis types.GenesisState
	app.AppCodec().MustUnmarshalJSON(genesisJSON, &decodedGenesis)
	require.Equal(t, genesis.VoteDelegationBackfillCutoff, decodedGenesis.VoteDelegationBackfillCutoff)

	authGenesis := auth.ExportGenesis(ctx, app.AccountKeeper)
	bankGenesis := app.BankKeeper.ExportGenesis(ctx)
	stakingGenesis := staking.ExportGenesis(ctx, app.StakingKeeper)
	appGenesis := seiapp.NewDefaultGenesisState(app.AppCodec())
	appGenesis[authtypes.ModuleName] = app.AppCodec().MustMarshalJSON(authGenesis)
	appGenesis[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)
	appGenesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)
	appGenesis[types.ModuleName] = genesisJSON
	stateBytes, err := json.Marshal(appGenesis)
	require.NoError(t, err)

	importedApp := seiapp.SetupWithDB(t, dbm.NewMemDB(), false, false, false)
	_, err = importedApp.InitChain(&abci.RequestInitChain{
		ConsensusParams: seiapp.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
		Time:            ctx.BlockTime(),
	})
	require.NoError(t, err)
	importedApp.Commit(context.Background())
	importedCtx := importedApp.BaseApp.NewUncachedContext(false, tmproto.Header{Time: ctx.BlockTime()})

	cutoff, found := importedApp.GovKeeper.GetVoteDelegationBackfillCutoff(importedCtx)
	require.True(t, found)
	require.Equal(t, uint64(2), cutoff)
	require.False(t, importedApp.GovKeeper.VoteDelegationBackfillRequired(importedCtx, proposal.ProposalId))
	require.True(t, importedApp.GovKeeper.IsTallying(importedCtx, proposal.ProposalId))
	require.False(t, importedCtx.KVStore(importedApp.GetKey(types.StoreKey)).Has(types.ProposalDeadlineKey(proposal.ProposalId, proposal.VotingEndTime)))
}

func TestImportExportPreservesModernTallyRound(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	proposal.Status = types.StatusVotingPeriod
	proposal.VotingEndTime = ctx.BlockTime().Add(time.Hour)
	app.GovKeeper.SetProposal(ctx, proposal)
	app.GovKeeper.SetVoteDelegationBackfillCutoff(ctx, proposal.ProposalId+1)
	app.GovKeeper.InsertActiveProposalQueueForModernTallyRound(ctx, proposal.ProposalId, proposal.VotingEndTime)

	genesis := gov.ExportGenesis(ctx, app.GovKeeper)
	require.Equal(t, []uint64{proposal.ProposalId}, genesis.ModernTallyRoundProposalIds)

	importedApp := seiapp.Setup(t, false, false, false)
	importedCtx := importedApp.BaseApp.NewContext(false, tmproto.Header{Time: ctx.BlockTime()})
	gov.InitGenesis(importedCtx, importedApp.AccountKeeper, importedApp.BankKeeper, importedApp.GovKeeper, genesis)

	store := importedCtx.KVStore(importedApp.GetKey(types.StoreKey))
	require.True(t, importedApp.GovKeeper.IsModernTallyRound(importedCtx, proposal.ProposalId))
	require.True(t, store.Has(types.ProposalDeadlineKey(proposal.ProposalId, proposal.VotingEndTime)))
	require.False(t, importedApp.GovKeeper.VoteDelegationBackfillRequired(importedCtx, proposal.ProposalId))
}

func TestImportExportCanonicalizesPartialLegacyVoteDelegationBackfill(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	addrs := seiapp.AddTestAddrs(app, ctx, 2, valTokens)
	SortAddresses(addrs)
	stakingParams := app.StakingKeeper.GetParams(ctx)
	stakingParams.MinCommissionRate = sdk.ZeroDec()
	app.StakingKeeper.SetParams(ctx, stakingParams)
	valAddrs := seiapp.ConvertAddrsToValAddrs(addrs)
	createValidators(t, staking.NewHandler(app.StakingKeeper), ctx, valAddrs[:1], []int64{5})
	staking.EndBlocker(ctx, app.StakingKeeper)

	voter := addrs[1]
	validator, found := app.StakingKeeper.GetValidator(ctx, valAddrs[0])
	require.True(t, found)
	delegatedTokens := app.StakingKeeper.TokensFromConsensusPower(ctx, 10)
	_, err := app.StakingKeeper.Delegate(ctx, voter, delegatedTokens, stakingtypes.Unbonded, validator, true)
	require.NoError(t, err)

	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	app.GovKeeper.ActivateVotingPeriod(ctx, proposal)
	proposal, found = app.GovKeeper.GetProposal(ctx, proposal.ProposalId)
	require.True(t, found)

	voters := make([]sdk.AccAddress, gov.MaxVotesProcessedPerBlock+1)
	voters[0] = voter
	for i := 1; i < len(voters); i++ {
		voters[i] = make(sdk.AccAddress, 20)
		binary.BigEndian.PutUint64(voters[i][12:], uint64(i))
	}
	for i, voteVoter := range voters {
		option := types.OptionNo
		if i == 0 {
			option = types.OptionYes
		}
		require.NoError(t, app.GovKeeper.AddVote(
			ctx,
			proposal.ProposalId,
			voteVoter,
			types.NewNonSplitVoteOption(option),
		))
	}

	store := ctx.KVStore(app.GetKey(types.StoreKey))
	for _, voteVoter := range voters {
		store.Delete(types.VoteDelegationsKey(proposal.ProposalId, voteVoter))
		store.Delete(types.VoterProposalsKey(voteVoter, proposal.ProposalId))
		store.Delete(types.VoteDelegationSnapshotRevisionKey(proposal.ProposalId, voteVoter))
	}
	require.NoError(t, govkeeper.NewMigrator(app.GovKeeper).Migrate3to4(ctx))

	ctx = ctx.WithBlockTime(proposal.VotingEndTime.Add(time.Second))
	complete, processed, _, _, _ := app.GovKeeper.TallyIncremental(
		ctx,
		proposal,
		gov.MaxVotesProcessedPerBlock,
	)
	require.False(t, complete)
	require.Equal(t, gov.MaxVotesProcessedPerBlock, processed)
	require.True(t, app.GovKeeper.IsVoteDelegationBackfillInProgress(ctx, proposal.ProposalId))

	expectedPasses, expectedBurnDeposits, expectedTallyResult := app.GovKeeper.Tally(ctx, proposal)
	genesis := gov.ExportGenesis(ctx, app.GovKeeper)
	require.Len(t, genesis.Votes, gov.MaxVotesProcessedPerBlock+1)
	require.Len(t, genesis.VoteDelegationSnapshots, gov.MaxVotesProcessedPerBlock+1)
	require.Len(t, genesis.TallyElectorates, 1)

	authGenesis := auth.ExportGenesis(ctx, app.AccountKeeper)
	bankGenesis := app.BankKeeper.ExportGenesis(ctx)
	stakingGenesis := staking.ExportGenesis(ctx, app.StakingKeeper)
	appGenesis := seiapp.NewDefaultGenesisState(app.AppCodec())
	appGenesis[authtypes.ModuleName] = app.AppCodec().MustMarshalJSON(authGenesis)
	appGenesis[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)
	appGenesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)
	appGenesis[types.ModuleName] = app.AppCodec().MustMarshalJSON(genesis)
	stateBytes, err := json.Marshal(appGenesis)
	require.NoError(t, err)

	importedApp := seiapp.SetupWithDB(t, dbm.NewMemDB(), false, false, false)
	_, err = importedApp.InitChain(&abci.RequestInitChain{
		ConsensusParams: seiapp.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
		Time:            ctx.BlockTime(),
	})
	require.NoError(t, err)
	importedApp.Commit(context.Background())
	importedCtx := importedApp.BaseApp.NewUncachedContext(false, tmproto.Header{Time: ctx.BlockTime()})
	importedProposal, found := importedApp.GovKeeper.GetProposal(importedCtx, proposal.ProposalId)
	require.True(t, found)
	require.False(t, importedApp.GovKeeper.VoteDelegationBackfillRequired(importedCtx, proposal.ProposalId))
	require.True(t, importedApp.GovKeeper.IsTallying(importedCtx, proposal.ProposalId))

	delegation, found := importedApp.StakingKeeper.GetDelegation(importedCtx, voter, valAddrs[0])
	require.True(t, found)
	delegation.Shares = delegation.Shares.Add(delegatedTokens.ToDec())
	importedApp.StakingKeeper.SetDelegation(importedCtx, delegation)
	importedApp.GovKeeper.StakingHooks().AfterDelegationModified(importedCtx, voter, valAddrs[0])
	importedPasses, importedBurnDeposits, importedTallyResult := importedApp.GovKeeper.Tally(importedCtx, importedProposal)
	require.Equal(t, expectedPasses, importedPasses)
	require.Equal(t, expectedBurnDeposits, importedBurnDeposits)
	require.True(t, expectedTallyResult.Equals(importedTallyResult))
}

func TestInitGenesisRejectsFutureTallyElectorate(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	app.GovKeeper.ActivateVotingPeriod(ctx, proposal)

	genesis := gov.ExportGenesis(ctx, app.GovKeeper)
	genesis.TallyElectorates = []types.TallyElectorate{{
		ProposalId:        proposal.ProposalId,
		TotalBondedTokens: sdk.ZeroInt(),
		TallyParams:       types.DefaultTallyParams(),
		TallyValidators:   []types.TallyValidator{},
	}}

	importedApp := seiapp.Setup(t, false, false, false)
	importedCtx := importedApp.BaseApp.NewContext(false, tmproto.Header{})
	require.Panics(t, func() {
		gov.InitGenesis(importedCtx, importedApp.AccountKeeper, importedApp.BankKeeper, importedApp.GovKeeper, genesis)
	})
}
