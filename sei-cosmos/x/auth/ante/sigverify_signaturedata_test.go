package ante_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	kmultisig "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/multisig"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types/multisig"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/ante"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

func TestSignatureDataToBz_FlatMultisigMatchesWireAggregate(t *testing.T) {
	msg := []byte("flat-msig")
	privs := []*secp256k1.PrivKey{secp256k1.GenPrivKey(), secp256k1.GenPrivKey(), secp256k1.GenPrivKey()}
	msig := multisig.NewMultisig(3)
	var leafSigs [][]byte
	for i := 0; i < 2; i++ {
		sigBz, err := privs[i].Sign(msg)
		require.NoError(t, err)
		leafSigs = append(leafSigs, sigBz)
		multisig.AddSignature(msig, &signing.SingleSignatureData{Signature: sigBz}, i)
	}

	got, err := ante.SignatureDataToBz(msig)
	require.NoError(t, err)
	require.Len(t, got, 3) // two leaves + root aggregate
	require.Equal(t, leafSigs[0], got[0])
	require.Equal(t, leafSigs[1], got[1])

	_, wireSig := tx.SignatureDataToModeInfoAndSig(msig)
	require.Equal(t, wireSig, got[2], "root aggregate must match Tx.signatures encoding")
}

func TestSignatureDataToBz_NestedLeavesPlusRootWireOnly(t *testing.T) {
	msg := []byte("nested-msig")
	alice := secp256k1.GenPrivKey()
	bob := secp256k1.GenPrivKey()
	carol := secp256k1.GenPrivKey()

	sigAlice, err := alice.Sign(msg)
	require.NoError(t, err)
	sigBob, err := bob.Sign(msg)
	require.NoError(t, err)
	sigCarol, err := carol.Sign(msg)
	require.NoError(t, err)

	inner := multisig.NewMultisig(2)
	multisig.AddSignature(inner, &signing.SingleSignatureData{Signature: sigBob}, 0)

	outer := multisig.NewMultisig(2)
	multisig.AddSignature(outer, &signing.SingleSignatureData{Signature: sigAlice}, 0)
	multisig.AddSignature(outer, inner, 1)

	got, err := ante.SignatureDataToBz(outer)
	require.NoError(t, err)
	// leaves + one root wire aggregate; no separate inner aggregate event
	require.Len(t, got, 3)
	require.Equal(t, sigAlice, got[0])
	require.Equal(t, sigBob, got[1])

	_, wireOuter := tx.SignatureDataToModeInfoAndSig(outer)
	require.Equal(t, wireOuter, got[2])

	_, wireInner := tx.SignatureDataToModeInfoAndSig(inner)
	for _, bz := range got {
		require.False(t, bytes.Equal(bz, wireInner), "inner-only aggregate must not be emitted as its own event")
	}
	for _, bz := range got {
		require.False(t, bytes.Equal(bz, sigCarol))
	}
}

func TestSignatureDataToBz_RejectsTrailingSignatures(t *testing.T) {
	msg := []byte("trailing")
	priv := secp256k1.GenPrivKey()
	sigBz, err := priv.Sign(msg)
	require.NoError(t, err)
	single := &signing.SingleSignatureData{Signature: sigBz}

	msig := multisig.NewMultisig(3)
	multisig.AddSignature(msig, single, 0)
	multisig.AddSignature(msig, single, 1)
	msig.Signatures = append(msig.Signatures, &signing.SingleSignatureData{
		Signature: bytes.Repeat([]byte{0xab}, 1024),
	})

	_, err = ante.SignatureDataToBz(msig)
	require.Error(t, err)
	require.True(t, sdkerrors.ErrInvalidType.Is(err))
}

func TestConsumeMultisignatureVerificationGas_BitArrayExceedsKeySet(t *testing.T) {
	params := types.DefaultParams()
	pkSet := []cryptotypes.PubKey{secp256k1.GenPrivKey().PubKey(), secp256k1.GenPrivKey().PubKey()}
	pubkey := kmultisig.NewLegacyAminoPubKey(2, pkSet)

	sig := multisig.NewMultisig(4)
	sig.BitArray.SetIndex(0, true)
	sig.BitArray.SetIndex(1, true)
	sig.Signatures = []signing.SignatureData{
		&signing.SingleSignatureData{Signature: []byte{1}},
		&signing.SingleSignatureData{Signature: []byte{2}},
	}

	meter := sdk.NewInfiniteGasMeter(1, 1)
	err := ante.ConsumeMultisignatureVerificationGas(meter, sig, pubkey, params, 0)
	require.Error(t, err)
	require.True(t, sdkerrors.ErrInvalidType.Is(err))
}

func TestConsumeMultisignatureVerificationGas_TrailingSignatures(t *testing.T) {
	params := types.DefaultParams()
	pkSet := []cryptotypes.PubKey{
		secp256k1.GenPrivKey().PubKey(),
		secp256k1.GenPrivKey().PubKey(),
		secp256k1.GenPrivKey().PubKey(),
	}
	pubkey := kmultisig.NewLegacyAminoPubKey(2, pkSet)

	sig := multisig.NewMultisig(3)
	multisig.AddSignature(sig, &signing.SingleSignatureData{Signature: []byte{1}}, 0)
	multisig.AddSignature(sig, &signing.SingleSignatureData{Signature: []byte{2}}, 1)
	sig.Signatures = append(sig.Signatures, &signing.SingleSignatureData{Signature: []byte{3}})

	meter := sdk.NewInfiniteGasMeter(1, 1)
	err := ante.ConsumeMultisignatureVerificationGas(meter, sig, pubkey, params, 0)
	require.Error(t, err)
	require.True(t, sdkerrors.ErrInvalidType.Is(err))
}

func TestConsumeMultisignatureVerificationGas_NestedExactGas(t *testing.T) {
	params := types.DefaultParams()
	secpCost := params.GetSigVerifyCostSecp256k1()

	alice := secp256k1.GenPrivKey()
	bob := secp256k1.GenPrivKey()
	carol := secp256k1.GenPrivKey()

	innerPk := kmultisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{bob.PubKey(), carol.PubKey()})
	outerPk := kmultisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{alice.PubKey(), innerPk})

	// outer: alice + nested(bob, carol) — three secp256k1 leaf verifications.
	innerSig := multisig.NewMultisig(2)
	multisig.AddSignature(innerSig, &signing.SingleSignatureData{Signature: []byte{1}}, 0)
	multisig.AddSignature(innerSig, &signing.SingleSignatureData{Signature: []byte{2}}, 1)

	outerSig := multisig.NewMultisig(2)
	multisig.AddSignature(outerSig, &signing.SingleSignatureData{Signature: []byte{3}}, 0)
	multisig.AddSignature(outerSig, innerSig, 1)

	meter := sdk.NewInfiniteGasMeter(1, 1)
	require.NoError(t, ante.ConsumeMultisignatureVerificationGas(meter, outerSig, outerPk, params, 0))
	require.Equal(t, 3*secpCost, meter.GasConsumed())
}
