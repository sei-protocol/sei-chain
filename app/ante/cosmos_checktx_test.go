package ante_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	appante "github.com/sei-protocol/sei-chain/app/ante"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
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
