package app_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app"
	anteante "github.com/sei-protocol/sei-chain/app/ante"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	kmultisig "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/multisig"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types/multisig"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	authsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

// TestReCheckTxSkipsSignatureVerification verifies that actual cryptographic
// signature verification is skipped when IsRecheckTx is true, so a tx signed
// with the wrong private key is accepted on recheck but rejected on DeliverTx.
func TestReCheckTxSkipsSignatureVerification(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: time.Now().UTC()})

	encodingConfig := app.MakeEncodingConfig()
	txConfig := encodingConfig.TxConfig

	// addr/pubKey are the legitimate account keys; wrongPrivKey belongs to a
	// different keypair — any signature it produces will fail verification
	// against pubKey.
	_, pubKey, addr := testdata.KeyTestPubAddr()
	wrongPrivKey, _, _ := testdata.KeyTestPubAddr()

	acc := authtypes.NewBaseAccount(addr, pubKey, 0, 0)
	signerAccounts := []authtypes.AccountI{acc}
	authParams := authtypes.DefaultParams()

	txBuilder := txConfig.NewTxBuilder()
	err := txBuilder.SetMsgs(testdata.NewTestMsg(addr))
	require.NoError(t, err)
	txBuilder.SetFeeAmount(testdata.NewTestFeeAmount())
	txBuilder.SetGasLimit(testdata.NewTestGasLimit())

	sigsV2 := []signing.SignatureV2{{
		PubKey: pubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  txConfig.SignModeHandler().DefaultMode(),
			Signature: nil,
		},
		Sequence: 0,
	}}
	require.NoError(t, txBuilder.SetSignatures(sigsV2...))

	signerData := authsigning.SignerData{ChainID: "sei-test", AccountNumber: 0, Sequence: 0}
	sigV2, err := tx.SignWithPrivKey(txConfig.SignModeHandler().DefaultMode(), signerData, txBuilder, wrongPrivKey, txConfig, 0)
	require.NoError(t, err)
	require.NoError(t, txBuilder.SetSignatures(sigV2))

	signedTx := txBuilder.GetTx()

	gasCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1))

	// ReCheckTx: VerifySignature is skipped entirely, so the wrong-key sig passes.
	recheckCtx := gasCtx.WithIsCheckTx(false).WithIsReCheckTx(true)
	_, err = anteante.CheckSignatures(recheckCtx, txConfig, signedTx, signerAccounts, authParams)
	require.NoError(t, err, "expected no error during ReCheckTx despite invalid signature")

	// DeliverTx: VerifySignature runs, so the wrong-key sig is rejected.
	deliverCtx := gasCtx.WithIsCheckTx(false)
	_, err = anteante.CheckSignatures(deliverCtx, txConfig, signedTx, signerAccounts, authParams)
	require.Error(t, err, "expected error during DeliverTx for invalid signature")
	require.Contains(t, err.Error(), "signature verification failed")
}

// TestReCheckTxChecksSequenceNumber verifies that the account sequence check
// still runs when IsRecheckTx is true.  A tx whose sequence does not match the
// account should be rejected even during recheck.
func TestReCheckTxChecksSequenceNumber(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: time.Now().UTC()})

	encodingConfig := app.MakeEncodingConfig()
	txConfig := encodingConfig.TxConfig

	privKey, pubKey, addr := testdata.KeyTestPubAddr()

	// Account is at sequence 0; tx is signed with sequence 1 — a mismatch.
	acc := authtypes.NewBaseAccount(addr, pubKey, 0, 0)
	signerAccounts := []authtypes.AccountI{acc}
	authParams := authtypes.DefaultParams()

	txBuilder := txConfig.NewTxBuilder()
	err := txBuilder.SetMsgs(testdata.NewTestMsg(addr))
	require.NoError(t, err)
	txBuilder.SetFeeAmount(testdata.NewTestFeeAmount())
	txBuilder.SetGasLimit(testdata.NewTestGasLimit())

	sigsV2 := []signing.SignatureV2{{
		PubKey: pubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  txConfig.SignModeHandler().DefaultMode(),
			Signature: nil,
		},
		Sequence: 1,
	}}
	require.NoError(t, txBuilder.SetSignatures(sigsV2...))

	signerData := authsigning.SignerData{ChainID: "sei-test", AccountNumber: 0, Sequence: 1}
	sigV2, err := tx.SignWithPrivKey(txConfig.SignModeHandler().DefaultMode(), signerData, txBuilder, privKey, txConfig, 1)
	require.NoError(t, err)
	require.NoError(t, txBuilder.SetSignatures(sigV2))

	signedTx := txBuilder.GetTx()

	gasCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeter(1, 1))

	// ReCheckTx: sequence check runs before the recheck short-circuit, so the
	// mismatch must still be rejected.
	recheckCtx := gasCtx.WithIsCheckTx(false).WithIsReCheckTx(true)
	_, err = anteante.CheckSignatures(recheckCtx, txConfig, signedTx, signerAccounts, authParams)
	require.Error(t, err, "expected sequence mismatch error during ReCheckTx")
	require.Contains(t, err.Error(), "account sequence mismatch")

	// Sanity check: correct sequence passes on recheck.
	accSeq0 := authtypes.NewBaseAccount(addr, pubKey, 0, 1) // account already at seq 1
	_, err = anteante.CheckSignatures(recheckCtx, txConfig, signedTx, []authtypes.AccountI{accSeq0}, authParams)
	require.NoError(t, err, "expected no error when sequence matches on ReCheckTx")
}

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

func TestCosmosStatelessChecksRejectsInvalidNestedMultisigKey(t *testing.T) {
	for _, tc := range []struct {
		name           string
		malformedChild cryptotypes.PubKey
	}{
		{
			name:           "wrong length secp256k1 child",
			malformedChild: &secp256k1.PubKey{Key: bytes.Repeat([]byte{1}, secp256k1.PubKeySize+1)},
		},
		{
			name:           "correct length invalid secp256k1 child",
			malformedChild: &secp256k1.PubKey{Key: append([]byte{0x02}, bytes.Repeat([]byte{0xff}, secp256k1.PubKeySize-1)...)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testApp := app.Setup(t, false, false, false)
			ctx := testApp.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: time.Now().UTC()})
			signedTx, _ := buildNestedMultisigTx(t, ctx, tc.malformedChild)

			_, err := anteante.CosmosStatelessChecks(signedTx, ctx.BlockHeight(), ctx.ConsensusParams())
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid secp256k1 public key")
		})
	}
}

func TestCheckPubKeysRejectsInvalidNestedMultisigKey(t *testing.T) {
	for _, tc := range []struct {
		name           string
		malformedChild cryptotypes.PubKey
	}{
		{
			name:           "wrong length secp256k1 child",
			malformedChild: &secp256k1.PubKey{Key: bytes.Repeat([]byte{1}, secp256k1.PubKeySize+1)},
		},
		{
			name:           "correct length invalid secp256k1 child",
			malformedChild: &secp256k1.PubKey{Key: append([]byte{0x02}, bytes.Repeat([]byte{0xff}, secp256k1.PubKeySize-1)...)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testApp := app.Setup(t, false, false, false)
			ctx := testApp.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: time.Now().UTC()})
			signedTx, addr := buildNestedMultisigTx(t, ctx, tc.malformedChild)

			acc := testApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
			require.NoError(t, acc.SetAccountNumber(0))
			testApp.AccountKeeper.SetAccount(ctx, acc)

			_, err := anteante.CheckPubKeys(ctx, signedTx, testApp.AccountKeeper, authtypes.DefaultParams())
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid secp256k1 public key")

			storedAcc := testApp.AccountKeeper.GetAccount(ctx, addr)
			require.NotNil(t, storedAcc)
			require.Nil(t, storedAcc.GetPubKey())
		})
	}
}

func buildNestedMultisigTx(t *testing.T, ctx sdk.Context, malformedPubKey cryptotypes.PubKey) (sdk.Tx, sdk.AccAddress) {
	t.Helper()

	txConfig := app.MakeEncodingConfig().TxConfig
	txBuilder := txConfig.NewTxBuilder()

	priv, pubKey, _ := testdata.KeyTestPubAddr()
	pubKeys := []cryptotypes.PubKey{pubKey, malformedPubKey}
	multisigPubKey := kmultisig.NewLegacyAminoPubKey(1, pubKeys)
	addr := sdk.AccAddress(multisigPubKey.Address())

	require.NoError(t, txBuilder.SetMsgs(testdata.NewTestMsg(addr)))
	txBuilder.SetFeeAmount(testdata.NewTestFeeAmount())
	txBuilder.SetGasLimit(testdata.NewTestGasLimit())
	require.NoError(t, txBuilder.SetSignatures(signing.SignatureV2{
		PubKey:   multisigPubKey,
		Data:     multisig.NewMultisig(len(pubKeys)),
		Sequence: 0,
	}))

	singleSig, err := tx.SignWithPrivKey(
		txConfig.SignModeHandler().DefaultMode(),
		authsigning.SignerData{
			ChainID:       ctx.ChainID(),
			AccountNumber: 0,
			Sequence:      0,
		},
		txBuilder,
		priv,
		txConfig,
		0,
	)
	require.NoError(t, err)

	multisigSig := multisig.NewMultisig(len(pubKeys))
	require.NoError(t, multisig.AddSignatureV2(multisigSig, singleSig, pubKeys))
	require.NoError(t, txBuilder.SetSignatures(signing.SignatureV2{
		PubKey:   multisigPubKey,
		Data:     multisigSig,
		Sequence: 0,
	}))

	return txBuilder.GetTx(), addr
}
