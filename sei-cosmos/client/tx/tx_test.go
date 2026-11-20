package tx_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keyring"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	signingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
)

func NewTestTxConfig() client.TxConfig {
	cfg := app.MakeEncodingConfig()
	return cfg.TxConfig
}

func TestBuildUnsignedTx(t *testing.T) {
	kb, err := keyring.New(t.Name(), "test", t.TempDir(), nil)
	require.NoError(t, err)

	path := hd.CreateHDPath(118, 0, 0).String()

	_, _, err = kb.NewMnemonic("test_key1", keyring.English, path, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
	require.NoError(t, err)

	txf := tx.Factory{}.
		WithTxConfig(NewTestTxConfig()).
		WithAccountNumber(50).
		WithSequence(23).
		WithFees("50usei").
		WithMemo("memo").
		WithChainID("test-chain")

	msg := banktypes.NewMsgSend(sdk.AccAddress("from"), sdk.AccAddress("to"), nil)
	tx, err := tx.BuildUnsignedTx(txf, msg)
	require.NoError(t, err)
	require.NotNil(t, tx)

	sigs, err := tx.GetTx().(signing.SigVerifiableTx).GetSignaturesV2()
	require.NoError(t, err)
	require.Empty(t, sigs)
}

func TestSign(t *testing.T) {
	requireT := require.New(t)
	path := hd.CreateHDPath(118, 0, 0).String()
	kr, err := keyring.New(t.Name(), "test", t.TempDir(), nil)
	requireT.NoError(err)

	from1 := "test_key1"
	from2 := "test_key2"

	// create a new key using a mnemonic generator and test if we can reuse seed to recreate that account
	_, seed, err := kr.NewMnemonic(from1, keyring.English, path, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
	requireT.NoError(err)
	requireT.NoError(kr.Delete(from1))
	info1, _, err := kr.NewMnemonic(from1, keyring.English, path, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
	requireT.NoError(err)

	info2, err := kr.NewAccount(from2, seed, "", path, hd.Secp256k1)
	requireT.NoError(err)

	pubKey1 := info1.GetPubKey()
	pubKey2 := info2.GetPubKey()
	requireT.NotEqual(pubKey1.Bytes(), pubKey2.Bytes())
	t.Log("Pub keys:", pubKey1, pubKey2)

	txfNoKeybase := tx.Factory{}.
		WithTxConfig(NewTestTxConfig()).
		WithAccountNumber(50).
		WithSequence(23).
		WithFees("50usei").
		WithMemo("memo").
		WithChainID("test-chain")
	txfDirect := txfNoKeybase.
		WithKeybase(kr).
		WithSignMode(signingtypes.SignMode_SIGN_MODE_DIRECT)
	txfAmino := txfDirect.
		WithSignMode(signingtypes.SignMode_SIGN_MODE_LEGACY_AMINO_JSON)
	msg1 := banktypes.NewMsgSend(info1.GetAddress(), sdk.AccAddress("to"), nil)
	msg2 := banktypes.NewMsgSend(info2.GetAddress(), sdk.AccAddress("to"), nil)

	txb, err := tx.BuildUnsignedTx(txfNoKeybase, msg1, msg2)
	requireT.NoError(err)
	txb2, err := tx.BuildUnsignedTx(txfNoKeybase, msg1, msg2)
	requireT.NoError(err)
	txbSimple, err := tx.BuildUnsignedTx(txfNoKeybase, msg2)
	requireT.NoError(err)

	testCases := []struct {
		name         string
		txf          tx.Factory
		txb          client.TxBuilder
		from         string
		overwrite    bool
		expectedPKs  []cryptotypes.PubKey
		matchingSigs []int // if not nil, check matching signature against old ones.
	}{
		{
			"should fail if txf without keyring",
			txfNoKeybase, txb, from1, true, nil, nil,
		},
		{
			"should fail for non existing key",
			txfAmino, txb, "unknown", true, nil, nil,
		},
		{
			"amino: should succeed with keyring",
			txfAmino, txbSimple, from1, true,
			[]cryptotypes.PubKey{pubKey1},
			nil,
		},
		{
			"direct: should succeed with keyring",
			txfDirect, txbSimple, from1, true,
			[]cryptotypes.PubKey{pubKey1},
			nil,
		},

		/**** test double sign Amino mode ****/
		{
			"amino: should sign multi-signers tx",
			txfAmino, txb, from1, true,
			[]cryptotypes.PubKey{pubKey1},
			nil,
		},
		{
			"amino: should append a second signature and not overwrite",
			txfAmino, txb, from2, false,
			[]cryptotypes.PubKey{pubKey1, pubKey2},
			[]int{0, 0},
		},
		{
			"amino: should overwrite a signature",
			txfAmino, txb, from2, true,
			[]cryptotypes.PubKey{pubKey2},
			[]int{1, 0},
		},

		/**** test double sign Direct mode
		  signing transaction with more than 2 signers should fail in DIRECT mode ****/
		{
			"direct: should fail to append a signature with different mode",
			txfDirect, txb, from1, false,
			[]cryptotypes.PubKey{},
			nil,
		},
		{
			"direct: should fail to sign multi-signers tx",
			txfDirect, txb2, from1, false,
			[]cryptotypes.PubKey{},
			nil,
		},
		{
			"direct: should fail to overwrite multi-signers tx",
			txfDirect, txb2, from1, true,
			[]cryptotypes.PubKey{},
			nil,
		},
	}
	var prevSigs []signingtypes.SignatureV2
	for _, tc := range testCases {
		t.Run(tc.name, func(_ *testing.T) {
			err = tx.Sign(tc.txf, tc.from, tc.txb, tc.overwrite)
			if len(tc.expectedPKs) == 0 {
				requireT.Error(err)
			} else {
				requireT.NoError(err)
				sigs := testSigners(requireT, tc.txb.GetTx(), tc.expectedPKs...)
				if tc.matchingSigs != nil {
					requireT.Equal(prevSigs[tc.matchingSigs[0]], sigs[tc.matchingSigs[1]])
				}
				prevSigs = sigs
			}
		})
	}
}

func testSigners(require *require.Assertions, tr signing.Tx, pks ...cryptotypes.PubKey) []signingtypes.SignatureV2 {
	sigs, err := tr.GetSignaturesV2()
	require.Len(sigs, len(pks))
	require.NoError(err)
	require.Len(sigs, len(pks))
	for i := range pks {
		require.True(sigs[i].PubKey.Equals(pks[i]), "Signature is signed with a wrong pubkey. Got: %s, expected: %s", sigs[i].PubKey, pks[i])
	}
	return sigs
}
