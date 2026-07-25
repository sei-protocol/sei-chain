package multisig_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types/multisig"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
)

func TestValidateSignatureDataStructure(t *testing.T) {
	msg := []byte("validate")
	priv := secp256k1.GenPrivKey()
	sigBytes, err := priv.Sign(msg)
	require.NoError(t, err)
	single := &signing.SingleSignatureData{Signature: sigBytes}

	t.Run("single ok", func(t *testing.T) {
		require.NoError(t, multisig.ValidateSignatureDataStructure(single))
	})

	t.Run("nil", func(t *testing.T) {
		require.Error(t, multisig.ValidateSignatureDataStructure(nil))
	})

	t.Run("honest multisig", func(t *testing.T) {
		msig := multisig.NewMultisig(3)
		multisig.AddSignature(msig, single, 0)
		multisig.AddSignature(msig, single, 1)
		require.NoError(t, multisig.ValidateSignatureDataStructure(msig))
	})

	t.Run("trailing signature", func(t *testing.T) {
		msig := multisig.NewMultisig(3)
		multisig.AddSignature(msig, single, 0)
		multisig.AddSignature(msig, single, 1)
		msig.Signatures = append(msig.Signatures, single)
		require.Error(t, multisig.ValidateSignatureDataStructure(msig))
	})

	t.Run("nested trailing signature", func(t *testing.T) {
		inner := multisig.NewMultisig(2)
		multisig.AddSignature(inner, single, 0)
		inner.Signatures = append(inner.Signatures, single)

		outer := multisig.NewMultisig(2)
		multisig.AddSignature(outer, single, 0)
		multisig.AddSignature(outer, inner, 1)
		require.Error(t, multisig.ValidateSignatureDataStructure(outer))
	})

	t.Run("nil bit array", func(t *testing.T) {
		require.Error(t, multisig.ValidateSignatureDataStructure(&signing.MultiSignatureData{
			BitArray:   nil,
			Signatures: []signing.SignatureData{single},
		}))
	})

	t.Run("empty bit array", func(t *testing.T) {
		require.Error(t, multisig.ValidateSignatureDataStructure(&signing.MultiSignatureData{
			BitArray:   &cryptotypes.CompactBitArray{},
			Signatures: nil,
		}))
	})

	t.Run("size multiple of 8", func(t *testing.T) {
		msig := multisig.NewMultisig(8)
		multisig.AddSignature(msig, single, 0)
		multisig.AddSignature(msig, single, 7)
		require.NoError(t, multisig.ValidateSignatureDataStructure(msig))
	})
}

func TestValidateSignatureDataStructure_MoreTrueBitsThanSigs(t *testing.T) {
	ba := cryptotypes.NewCompactBitArray(3)
	ba.SetIndex(0, true)
	ba.SetIndex(1, true)
	require.Error(t, multisig.ValidateSignatureDataStructure(&signing.MultiSignatureData{
		BitArray:   ba,
		Signatures: []signing.SignatureData{&signing.SingleSignatureData{Signature: []byte{1}}},
	}))
}
