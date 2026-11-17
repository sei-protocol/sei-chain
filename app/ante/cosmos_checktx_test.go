package ante_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/app"
	appante "github.com/sei-protocol/sei-chain/app/ante"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types/multisig"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	authante "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/ante"
	authkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	bankkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
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
		testApp.ParamsKeeper,
	)
	require.NoError(t, err)
	require.Equal(t, authante.GetTxPriority(fees, int64(gasLimit)), priority)
}

func TestCosmosAnteModeInfoNestingLimit(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	priv := secp256k1.GenPrivKey()
	signer := sdk.AccAddress(priv.PubKey().Address())
	type anteFunc func(sdk.Context, client.TxConfig, sdk.Tx, paramskeeper.Keeper, *evmkeeper.Keeper, authkeeper.AccountKeeper, bankkeeper.Keeper) (sdk.Context, error)

	for _, ante := range []struct {
		name    string
		checkTx bool
		run     anteFunc
	}{
		{name: "checktx", checkTx: true, run: appante.CosmosCheckTxAnte},
		{name: "delivertx", run: appante.CosmosDeliverTxAnte},
	} {
		ctx := testApp.NewContext(false, tmproto.Header{ChainID: "sei-test"}).WithIsCheckTx(ante.checkTx)
		for _, tc := range []struct {
			levels  int
			wantErr error
		}{
			{levels: 5, wantErr: sdkerrors.ErrUnknownAddress},
			{levels: 6, wantErr: sdkerrors.ErrTxDecode},
		} {
			t.Run(fmt.Sprintf("%s/levels=%d", ante.name, tc.levels), func(t *testing.T) {
				var data signing.SignatureData = &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: []byte("sig")}
				for range tc.levels {
					m := multisig.NewMultisig(1)
					multisig.AddSignature(m, data, 0)
					data = m
				}
				txBuilder := testApp.GetTxConfig().NewTxBuilder()
				require.NoError(t, txBuilder.SetMsgs(banktypes.NewMsgSend(signer, signer, sdk.NewCoins(sdk.NewInt64Coin("usei", 1)))))
				txBuilder.SetGasLimit(100_000)
				require.NoError(t, txBuilder.SetSignatures(signing.SignatureV2{PubKey: priv.PubKey(), Data: data}))

				_, err := ante.run(ctx, testApp.GetTxConfig(), txBuilder.GetTx(), testApp.ParamsKeeper, &testApp.EvmKeeper, testApp.AccountKeeper, testApp.BankKeeper)
				require.ErrorIs(t, err, tc.wantErr)
			})
		}
	}
}
