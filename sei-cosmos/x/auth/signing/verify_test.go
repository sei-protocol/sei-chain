package signing_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	kmultisig "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/multisig"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types/multisig"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/ante"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/legacy/legacytx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

func TestVerifySignature(t *testing.T) {
	priv, pubKey, addr := testdata.KeyTestPubAddr()
	priv1, pubKey1, addr1 := testdata.KeyTestPubAddr()

	const (
		memo    = "testmemo"
		chainId = "test-chain"
	)

	app, ctx := createTestApp(t, false)
	ctx = ctx.WithBlockHeight(1)

	cdc := codec.NewLegacyAmino()
	sdk.RegisterLegacyAminoCodec(cdc)
	types.RegisterLegacyAminoCodec(cdc)
	cdc.RegisterConcrete(testdata.TestMsg{}, "cosmos-sdk/Test", nil)

	acc1 := app.AccountKeeper.NewAccountWithAddress(ctx, addr)
	_ = app.AccountKeeper.NewAccountWithAddress(ctx, addr1)
	app.AccountKeeper.SetAccount(ctx, acc1)
	balances := sdk.NewCoins(sdk.NewInt64Coin("atom", 200))
	require.NoError(t, apptesting.FundAccount(app.BankKeeper, ctx, addr, balances))
	acc, err := ante.GetSignerAcc(ctx, app.AccountKeeper, addr)
	require.NoError(t, apptesting.FundAccount(app.BankKeeper, ctx, addr, balances))

	msgs := []sdk.Msg{testdata.NewTestMsg(addr)}
	fee := legacytx.NewStdFee(50000, sdk.Coins{sdk.NewInt64Coin("atom", 150)})
	signerData := signing.SignerData{
		ChainID:       chainId,
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
	}
	signBytes := legacytx.StdSignBytes(signerData.ChainID, signerData.AccountNumber, signerData.Sequence, 10, fee, msgs, memo)
	signature, err := priv.Sign(signBytes)
	require.NoError(t, err)

	stdSig := legacytx.StdSignature{PubKey: pubKey, Signature: signature}
	sigV2, err := legacytx.StdSignatureToSignatureV2(cdc, stdSig)
	require.NoError(t, err)

	handler := MakeTestHandlerMap()
	stdTx := legacytx.NewStdTx(msgs, fee, []legacytx.StdSignature{stdSig}, memo)
	stdTx.TimeoutHeight = 10
	err = signing.VerifySignature(pubKey, signerData, sigV2.Data, handler, stdTx)
	require.NoError(t, err)

	pkSet := []cryptotypes.PubKey{pubKey, pubKey1}
	multisigKey := kmultisig.NewLegacyAminoPubKey(2, pkSet)
	multisignature := multisig.NewMultisig(2)
	msgs = []sdk.Msg{testdata.NewTestMsg(addr, addr1)}
	multiSignBytes := legacytx.StdSignBytes(signerData.ChainID, signerData.AccountNumber, signerData.Sequence, 10, fee, msgs, memo)

	sig1, err := priv.Sign(multiSignBytes)
	require.NoError(t, err)
	stdSig1 := legacytx.StdSignature{PubKey: pubKey, Signature: sig1}
	sig1V2, err := legacytx.StdSignatureToSignatureV2(cdc, stdSig1)
	require.NoError(t, err)

	sig2, err := priv1.Sign(multiSignBytes)
	require.NoError(t, err)
	stdSig2 := legacytx.StdSignature{PubKey: pubKey, Signature: sig2}
	sig2V2, err := legacytx.StdSignatureToSignatureV2(cdc, stdSig2)
	require.NoError(t, err)

	err = multisig.AddSignatureFromPubKey(multisignature, sig1V2.Data, pkSet[0], pkSet)
	require.NoError(t, err)
	err = multisig.AddSignatureFromPubKey(multisignature, sig2V2.Data, pkSet[1], pkSet)
	require.NoError(t, err)

	stdTx = legacytx.NewStdTx(msgs, fee, []legacytx.StdSignature{stdSig1, stdSig2}, memo)
	stdTx.TimeoutHeight = 10

	err = signing.VerifySignature(multisigKey, signerData, multisignature, handler, stdTx)
	require.NoError(t, err)
}

// returns context and app with params set on account keeper
func createTestApp(t *testing.T, isCheckTx bool) (*app.App, sdk.Context) {
	app := app.Setup(t, isCheckTx, false, false)
	ctx := app.BaseApp.NewContext(isCheckTx, tmproto.Header{})
	app.AccountKeeper.SetParams(ctx, types.DefaultParams())

	return app, ctx
}
