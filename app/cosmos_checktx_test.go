package app_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app"
	anteante "github.com/sei-protocol/sei-chain/app/ante"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	authsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

// TestCheckSignaturesSkipsEventsOnCheckTx verifies that signature events
// (account sequence + signature bytes) are not built during CheckTx or
// ReCheckTx, but are built during DeliverTx.
func TestCheckSignaturesSkipsEventsOnCheckTx(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: time.Now().UTC()})

	encodingConfig := app.MakeEncodingConfig()
	txConfig := encodingConfig.TxConfig

	privKey, pubKey, addr := testdata.KeyTestPubAddr()

	acc := authtypes.NewBaseAccount(addr, pubKey, 0, 0)
	signerAccounts := []authtypes.AccountI{acc}
	authParams := authtypes.DefaultParams()

	// Build and sign a tx.
	txBuilder := txConfig.NewTxBuilder()
	err := txBuilder.SetMsgs(testdata.NewTestMsg(addr))
	require.NoError(t, err)
	txBuilder.SetFeeAmount(testdata.NewTestFeeAmount())
	txBuilder.SetGasLimit(testdata.NewTestGasLimit())

	// First pass: set empty sigs so signer info is populated.
	sigsV2 := []signing.SignatureV2{{
		PubKey: pubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  txConfig.SignModeHandler().DefaultMode(),
			Signature: nil,
		},
		Sequence: 0,
	}}
	require.NoError(t, txBuilder.SetSignatures(sigsV2...))

	// Second pass: real signature.
	signerData := authsigning.SignerData{ChainID: "sei-test", AccountNumber: 0, Sequence: 0}
	sigV2, err := tx.SignWithPrivKey(txConfig.SignModeHandler().DefaultMode(), signerData, txBuilder, privKey, txConfig, 0)
	require.NoError(t, err)
	require.NoError(t, txBuilder.SetSignatures(sigV2))

	signedTx := txBuilder.GetTx()

	// Use an infinite gas meter so signature verification gas consumption doesn't
	// cause a panic when the context has no real store behind it.
	gasCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1))

	// DeliverTx context: both flags false → events must be populated.
	deliverCtx := gasCtx.WithIsCheckTx(false)
	events, err := anteante.CheckSignatures(deliverCtx, txConfig, signedTx, signerAccounts, authParams)
	require.NoError(t, err)
	require.NotEmpty(t, events, "expected signature events in DeliverTx context")

	// CheckTx context → events must be empty.
	checkCtx := gasCtx.WithIsCheckTx(true)
	events, err = anteante.CheckSignatures(checkCtx, txConfig, signedTx, signerAccounts, authParams)
	require.NoError(t, err)
	require.Empty(t, events, "expected no signature events in CheckTx context")

	// ReCheckTx context → events must be empty.
	recheckCtx := gasCtx.WithIsCheckTx(false).WithIsReCheckTx(true)
	events, err = anteante.CheckSignatures(recheckCtx, txConfig, signedTx, signerAccounts, authParams)
	require.NoError(t, err)
	require.Empty(t, events, "expected no signature events in ReCheckTx context")
}
