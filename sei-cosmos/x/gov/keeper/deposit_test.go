package keeper_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"

	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	bankkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	govkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/keeper"
)

func TestDeposits(t *testing.T) {
	testcases := map[string]struct {
		isExpedited bool
	}{
		"regular": {
			isExpedited: false,
		},
		"expedited": {
			isExpedited: true,
		},
	}

	for _, tc := range testcases {
		app := seiapp.Setup(t, false, false, false)
		ctx := app.BaseApp.NewContext(false, tmproto.Header{})

		// With expedited proposals the minimum deposit is higer, so we must
		// initialize and deposit an amount depositMultiplier times larger
		// than the regular min deposit amount.
		depositMultiplier := int64(1)
		if tc.isExpedited {
			depositMultiplier = types.DefaultMinExpeditedDepositTokens.Quo(types.DefaultMinDepositTokens).Int64()
		}

		TestAddrs := seiapp.AddTestAddrsIncremental(app, ctx, 2, sdk.NewInt(10000000*depositMultiplier))

		tp := TestProposal
		proposal, err := app.GovKeeper.SubmitProposalWithExpedite(ctx, tp, tc.isExpedited)
		require.NoError(t, err)
		proposalID := proposal.ProposalId

		firstDepositValue := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, app.StakingKeeper.TokensFromConsensusPower(ctx, 4*depositMultiplier)))
		secondDepositValue := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, app.StakingKeeper.TokensFromConsensusPower(ctx, 5*depositMultiplier)))

		addr0Initial := app.BankKeeper.GetAllBalances(ctx, TestAddrs[0])
		addr1Initial := app.BankKeeper.GetAllBalances(ctx, TestAddrs[1])

		require.True(t, proposal.TotalDeposit.IsEqual(sdk.NewCoins()))

		// Check no deposits at beginning
		deposit, found := app.GovKeeper.GetDeposit(ctx, proposalID, TestAddrs[1])
		require.False(t, found)
		proposal, ok := app.GovKeeper.GetProposal(ctx, proposalID)
		require.True(t, ok)
		require.True(t, proposal.VotingStartTime.Equal(time.Time{}))

		// Check first deposit
		votingStarted, err := app.GovKeeper.AddDeposit(ctx, proposalID, TestAddrs[0], firstDepositValue)
		require.NoError(t, err)
		require.False(t, votingStarted)
		deposit, found = app.GovKeeper.GetDeposit(ctx, proposalID, TestAddrs[0])
		require.True(t, found)
		require.Equal(t, firstDepositValue, deposit.Amount)
		require.Equal(t, TestAddrs[0].String(), deposit.Depositor)
		proposal, ok = app.GovKeeper.GetProposal(ctx, proposalID)
		require.True(t, ok)
		require.Equal(t, firstDepositValue, proposal.TotalDeposit)
		require.Equal(t, addr0Initial.Sub(firstDepositValue), app.BankKeeper.GetAllBalances(ctx, TestAddrs[0]))

		// Check a second deposit from same address
		votingStarted, err = app.GovKeeper.AddDeposit(ctx, proposalID, TestAddrs[0], secondDepositValue)
		require.NoError(t, err)
		require.False(t, votingStarted)
		deposit, found = app.GovKeeper.GetDeposit(ctx, proposalID, TestAddrs[0])
		require.True(t, found)
		require.Equal(t, firstDepositValue.Add(secondDepositValue...), deposit.Amount)
		require.Equal(t, TestAddrs[0].String(), deposit.Depositor)
		proposal, ok = app.GovKeeper.GetProposal(ctx, proposalID)
		require.True(t, ok)
		require.Equal(t, firstDepositValue.Add(secondDepositValue...), proposal.TotalDeposit)
		require.Equal(t, addr0Initial.Sub(firstDepositValue).Sub(secondDepositValue), app.BankKeeper.GetAllBalances(ctx, TestAddrs[0]))

		// Check third deposit from a new address
		votingStarted, err = app.GovKeeper.AddDeposit(ctx, proposalID, TestAddrs[1], firstDepositValue)
		require.NoError(t, err)
		require.True(t, votingStarted)
		deposit, found = app.GovKeeper.GetDeposit(ctx, proposalID, TestAddrs[1])
		require.True(t, found)
		require.Equal(t, TestAddrs[1].String(), deposit.Depositor)
		require.Equal(t, firstDepositValue, deposit.Amount)
		proposal, ok = app.GovKeeper.GetProposal(ctx, proposalID)
		require.True(t, ok)
		require.Equal(t, firstDepositValue.Add(secondDepositValue...).Add(firstDepositValue...), proposal.TotalDeposit)
		require.Equal(t, addr1Initial.Sub(firstDepositValue), app.BankKeeper.GetAllBalances(ctx, TestAddrs[1]))

		// Check that proposal moved to voting period
		proposal, ok = app.GovKeeper.GetProposal(ctx, proposalID)
		require.True(t, ok)
		require.True(t, proposal.VotingStartTime.Equal(ctx.BlockHeader().Time))

		// Test deposit iterator
		// NOTE order of deposits is determined by the addresses
		deposits := app.GovKeeper.GetAllDeposits(ctx)
		require.Len(t, deposits, 2)
		require.Equal(t, deposits, app.GovKeeper.GetDeposits(ctx, proposalID))
		require.Equal(t, TestAddrs[0].String(), deposits[0].Depositor)
		require.Equal(t, firstDepositValue.Add(secondDepositValue...), deposits[0].Amount)
		require.Equal(t, TestAddrs[1].String(), deposits[1].Depositor)
		require.Equal(t, firstDepositValue, deposits[1].Amount)

		// Test Refund Deposits
		deposit, found = app.GovKeeper.GetDeposit(ctx, proposalID, TestAddrs[1])
		require.True(t, found)
		require.Equal(t, firstDepositValue, deposit.Amount)
		app.GovKeeper.RefundDeposits(ctx, proposalID)
		deposit, found = app.GovKeeper.GetDeposit(ctx, proposalID, TestAddrs[1])
		require.False(t, found)
		require.Equal(t, addr0Initial, app.BankKeeper.GetAllBalances(ctx, TestAddrs[0]))
		require.Equal(t, addr1Initial, app.BankKeeper.GetAllBalances(ctx, TestAddrs[1]))

		// Test delete deposits
		_, err = app.GovKeeper.AddDeposit(ctx, proposalID, TestAddrs[0], firstDepositValue)
		require.NoError(t, err)
		app.GovKeeper.DeleteDeposits(ctx, proposalID)
		deposits = app.GovKeeper.GetDeposits(ctx, proposalID)
		require.Len(t, deposits, 0)
	}
}

func TestAddDepositRejectsBlocksAfterVotingEnd(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{}).WithBlockTime(time.Unix(100, 0))
	depositor := seiapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(100))[0]
	depositAmount := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1))

	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	proposal.Status = types.StatusVotingPeriod
	proposal.VotingEndTime = ctx.BlockTime().Add(time.Second)
	app.GovKeeper.SetProposal(ctx, proposal)
	app.GovKeeper.InsertActiveProposalQueue(ctx, proposal.ProposalId, proposal.VotingEndTime)

	atVotingEnd := ctx.WithBlockTime(proposal.VotingEndTime)
	_, err = app.GovKeeper.AddDeposit(atVotingEnd, proposal.ProposalId, depositor, depositAmount)
	require.NoError(t, err)

	proposalAtVotingEnd, found := app.GovKeeper.GetProposal(ctx, proposal.ProposalId)
	require.True(t, found)
	depositorBalanceAtVotingEnd := app.BankKeeper.GetAllBalances(ctx, depositor)
	moduleBalanceAtVotingEnd := app.BankKeeper.GetAllBalances(ctx, app.AccountKeeper.GetModuleAddress(types.ModuleName))
	app.GovKeeper.CaptureExactTallyBoundary(atVotingEnd)
	_, err = app.GovKeeper.AddDeposit(atVotingEnd, proposal.ProposalId, depositor, depositAmount)
	require.ErrorIs(t, err, types.ErrInactiveProposal)

	afterVotingEnd := ctx.WithBlockTime(proposal.VotingEndTime.Add(time.Second))
	_, err = app.GovKeeper.AddDeposit(afterVotingEnd, proposal.ProposalId, depositor, depositAmount)
	require.ErrorIs(t, err, types.ErrInactiveProposal)

	proposalAfterVotingEnd, found := app.GovKeeper.GetProposal(ctx, proposal.ProposalId)
	require.True(t, found)
	require.Equal(t, proposalAtVotingEnd.TotalDeposit, proposalAfterVotingEnd.TotalDeposit)
	require.Equal(t, depositorBalanceAtVotingEnd, app.BankKeeper.GetAllBalances(ctx, depositor))
	require.Equal(t, moduleBalanceAtVotingEnd, app.BankKeeper.GetAllBalances(ctx, app.AccountKeeper.GetModuleAddress(types.ModuleName)))

	store := afterVotingEnd.KVStore(app.GetKey(types.StoreKey))
	store.Delete(types.IncrementalTallyEnabledKey)
	legacyCtx := afterVotingEnd.WithIsTracing(true).WithClosestUpgradeName("v6.7")
	_, err = app.GovKeeper.AddDeposit(legacyCtx, proposal.ProposalId, depositor, depositAmount)
	require.NoError(t, err)
}

func TestRefundDepositsLeavesInvalidRecipientPending(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	validDepositor := seiapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(100))[0]

	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)

	evmAddr := common.HexToAddress("0x0000000000000000000000000000000000000001")
	castDepositor := sdk.AccAddress(evmAddr[:])
	app.EvmKeeper.SetAddressMapping(ctx, castDepositor, evmAddr)
	require.NoError(t, app.BankKeeper.AddCoins(ctx, castDepositor, sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 100)), true))

	castDeposit := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 40))
	validDeposit := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 30))
	_, err = app.GovKeeper.AddDeposit(ctx, proposal.ProposalId, castDepositor, castDeposit)
	require.NoError(t, err)
	_, err = app.GovKeeper.AddDeposit(ctx, proposal.ProposalId, validDepositor, validDeposit)
	require.NoError(t, err)

	app.EvmKeeper.SetAddressMapping(ctx, sdk.AccAddress(bytes.Repeat([]byte{2}, common.AddressLength)), evmAddr)
	require.False(t, app.BankKeeper.CanSendTo(ctx, castDepositor))

	app.GovKeeper.RefundDeposits(ctx, proposal.ProposalId)

	deposit, found := app.GovKeeper.GetDeposit(ctx, proposal.ProposalId, castDepositor)
	require.True(t, found)
	require.Equal(t, castDeposit, deposit.Amount)
	_, found = app.GovKeeper.GetDeposit(ctx, proposal.ProposalId, validDepositor)
	require.False(t, found)
	require.Equal(t, sdk.NewInt(100), app.BankKeeper.GetBalance(ctx, validDepositor, sdk.DefaultBondDenom).Amount)
	require.Equal(t, castDeposit, app.BankKeeper.GetAllBalances(ctx, app.AccountKeeper.GetModuleAddress(types.ModuleName)))
}

func TestRefundDepositsLeavesBlockedRecipientPending(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)

	blockedDepositor := sdk.AccAddress(append(append([]byte{}, bankkeeper.CoinbaseAddressPrefix...), make([]byte, 8)...))
	require.True(t, app.BankKeeper.CanSendTo(ctx, blockedDepositor))
	require.True(t, app.BankKeeper.BlockedAddr(blockedDepositor))

	initialBalance := sdk.NewInt(100)
	depositAmount := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 40))
	require.NoError(t, app.BankKeeper.AddCoins(
		ctx,
		blockedDepositor,
		sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, initialBalance)),
		true,
	))
	_, err = app.GovKeeper.AddDeposit(ctx, proposal.ProposalId, blockedDepositor, depositAmount)
	require.NoError(t, err)

	app.GovKeeper.RefundDeposits(ctx, proposal.ProposalId)

	deposit, found := app.GovKeeper.GetDeposit(ctx, proposal.ProposalId, blockedDepositor)
	require.True(t, found)
	require.Equal(t, depositAmount, deposit.Amount)
	expectedBalance := initialBalance.Sub(depositAmount.AmountOf(sdk.DefaultBondDenom))
	require.Equal(t, expectedBalance, app.BankKeeper.GetBalance(ctx, blockedDepositor, sdk.DefaultBondDenom).Amount)
	require.Equal(t, depositAmount, app.BankKeeper.GetAllBalances(ctx, app.AccountKeeper.GetModuleAddress(types.ModuleName)))
}

func TestAddDepositIgnoresEmptyAndRejectsOtherDenoms(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	require.True(t, app.GovKeeper.IncrementalTallyEnabled(ctx))
	depositor := seiapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(100))[0]
	require.NoError(t, app.BankKeeper.AddCoins(ctx, depositor, sdk.NewCoins(sdk.NewInt64Coin("other", 100)), true))
	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)

	_, err = app.GovKeeper.AddDeposit(ctx, proposal.ProposalId, depositor, sdk.NewCoins())
	require.NoError(t, err)
	_, found := app.GovKeeper.GetDeposit(ctx, proposal.ProposalId, depositor)
	require.False(t, found)

	for _, amount := range []sdk.Coins{
		sdk.NewCoins(sdk.NewInt64Coin("other", 1)),
		sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1), sdk.NewInt64Coin("other", 1)),
	} {
		_, err = app.GovKeeper.AddDeposit(ctx, proposal.ProposalId, depositor, amount)
		require.ErrorIs(t, err, sdkerrors.ErrInvalidCoins)
	}
	_, found = app.GovKeeper.GetDeposit(ctx, proposal.ProposalId, depositor)
	require.False(t, found)

	_, err = app.GovKeeper.AddDeposit(ctx, proposal.ProposalId, depositor, sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)))
	require.NoError(t, err)
	_, found = app.GovKeeper.GetDeposit(ctx, proposal.ProposalId, depositor)
	require.True(t, found)
}

func TestSettleDepositsResumesWithinBudget(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	depositors := seiapp.AddTestAddrsIncremental(app, ctx, 3, sdk.NewInt(100))
	blockedDepositor := sdk.AccAddress(append(append([]byte{}, bankkeeper.CoinbaseAddressPrefix...), make([]byte, 8)...))
	require.NoError(t, app.BankKeeper.AddCoins(ctx, blockedDepositor, sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 100)), true))

	burned, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	refunded, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)
	amount := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 10))
	for _, depositor := range depositors {
		_, err = app.GovKeeper.AddDeposit(ctx, burned.ProposalId, depositor, amount)
		require.NoError(t, err)
		_, err = app.GovKeeper.AddDeposit(ctx, refunded.ProposalId, depositor, amount)
		require.NoError(t, err)
	}
	_, err = app.GovKeeper.AddDeposit(ctx, refunded.ProposalId, blockedDepositor, amount)
	require.NoError(t, err)
	supply := app.BankKeeper.GetSupply(ctx, sdk.DefaultBondDenom).Amount
	requireDepositsBacked := func() {
		msg, broken := govkeeper.ModuleAccountInvariant(app.GovKeeper, app.BankKeeper)(ctx)
		require.False(t, broken, msg)
	}

	app.GovKeeper.ScheduleDepositSettlement(ctx, refunded.ProposalId, false)
	app.GovKeeper.ScheduleDepositSettlement(ctx, burned.ProposalId, true)

	require.Equal(t, 2, app.GovKeeper.SettleDeposits(ctx, 2))
	require.Len(t, app.GovKeeper.GetDeposits(ctx, burned.ProposalId), 1)
	require.Len(t, app.GovKeeper.GetDeposits(ctx, refunded.ProposalId), 4)
	requireDepositsBacked()

	require.Equal(t, 2, app.GovKeeper.SettleDeposits(ctx, 2))
	require.Empty(t, app.GovKeeper.GetDeposits(ctx, burned.ProposalId))
	requireDepositsBacked()

	require.Equal(t, 3, app.GovKeeper.SettleDeposits(ctx, 10))
	remaining := app.GovKeeper.GetDeposits(ctx, refunded.ProposalId)
	require.Len(t, remaining, 1)
	require.Equal(t, blockedDepositor.String(), remaining[0].Depositor)
	require.Zero(t, app.GovKeeper.SettleDeposits(ctx, 10))
	requireDepositsBacked()

	require.Equal(t, supply.SubRaw(30), app.BankKeeper.GetSupply(ctx, sdk.DefaultBondDenom).Amount)
	for _, depositor := range depositors {
		require.Equal(t, sdk.NewInt(90), app.BankKeeper.GetBalance(ctx, depositor, sdk.DefaultBondDenom).Amount)
	}
}

func TestScheduleDepositSettlementSkipsProposalsWithoutDeposits(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
	require.NoError(t, err)

	app.GovKeeper.ScheduleDepositSettlement(ctx, proposal.ProposalId, true)
	require.Zero(t, queuedDepositSettlements(ctx, app))
}

func TestSettleDepositsCountsProposalsWithoutRecords(t *testing.T) {
	app := seiapp.Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	depositor := seiapp.AddTestAddrsIncremental(app, ctx, 1, sdk.NewInt(100))[0]
	for range 3 {
		proposal, err := app.GovKeeper.SubmitProposal(ctx, TestProposal)
		require.NoError(t, err)
		_, err = app.GovKeeper.AddDeposit(ctx, proposal.ProposalId, depositor, sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)))
		require.NoError(t, err)
		app.GovKeeper.ScheduleDepositSettlement(ctx, proposal.ProposalId, true)
		app.GovKeeper.DeleteDeposits(ctx, proposal.ProposalId)
	}

	require.Equal(t, 2, app.GovKeeper.SettleDeposits(ctx, 2))
	require.Equal(t, 1, queuedDepositSettlements(ctx, app))
}

func queuedDepositSettlements(ctx sdk.Context, app *seiapp.App) (queued int) {
	iterator := sdk.KVStorePrefixIterator(ctx.KVStore(app.GetKey(types.StoreKey)), types.DepositSettlementKeyPrefix)
	defer func() { _ = iterator.Close() }()
	for ; iterator.Valid(); iterator.Next() {
		queued++
	}
	return queued
}
