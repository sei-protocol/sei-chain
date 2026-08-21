package ante_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	appante "github.com/sei-protocol/sei-chain/app/ante"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	authante "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/ante"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
)

func TestCheckAndChargeFeesRejectsFeeFreeAssociate(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{ChainID: "sei-test"}).WithIsCheckTx(true)
	testApp.ParamsKeeper.SetFeesParams(ctx, *paramtypes.DefaultFeesParams())

	sender, _ := testkeeper.MockAddressPair()
	txBuilder := testApp.GetTxConfig().NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(evmtypes.NewMsgAssociate(sender, "test")))
	txBuilder.SetGasLimit(100_000)

	_, err := appante.CheckAndChargeFees(
		ctx,
		txBuilder.GetTx(),
		testApp.AccountKeeper,
		testApp.BankKeeper,
		&testApp.FeeGrantKeeper,
		testApp.ParamsKeeper,
	)
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
}

func TestCheckAndChargeFeesUsesFeePriorityForAssociate(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{ChainID: "sei-test"}).WithIsCheckTx(true)
	testApp.ParamsKeeper.SetFeesParams(ctx, *paramtypes.DefaultFeesParams())

	sender, _ := testkeeper.MockAddressPair()
	funds := sdk.NewCoins(sdk.NewInt64Coin("usei", 1_000_000))
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, funds))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, sender, funds))

	const gasLimit = uint64(100_000)
	fees := sdk.NewCoins(sdk.NewInt64Coin("usei", 2_000))
	txBuilder := testApp.GetTxConfig().NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(evmtypes.NewMsgAssociate(sender, "test")))
	txBuilder.SetGasLimit(gasLimit)
	txBuilder.SetFeeAmount(fees)

	priority, err := appante.CheckAndChargeFees(
		ctx,
		txBuilder.GetTx(),
		testApp.AccountKeeper,
		testApp.BankKeeper,
		&testApp.FeeGrantKeeper,
		testApp.ParamsKeeper,
	)
	require.NoError(t, err)
	require.Equal(t, authante.GetTxPriority(fees, int64(gasLimit)), priority)
}
