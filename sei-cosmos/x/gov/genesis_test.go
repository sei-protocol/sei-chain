package gov_test

import (
	"context"
	"testing"

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
