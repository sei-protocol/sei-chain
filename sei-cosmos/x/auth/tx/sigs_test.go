package tx

import (
	"testing"

	"github.com/stretchr/testify/require"

	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types/multisig"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	txtypes "github.com/sei-protocol/sei-chain/sei-cosmos/types/tx"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
)

func TestDecodeMultisignatures(t *testing.T) {
	testSigs := [][]byte{
		[]byte("dummy1"),
		[]byte("dummy2"),
		[]byte("dummy3"),
	}

	badMultisig := testdata.BadMultiSignature{
		Signatures:     testSigs,
		MaliciousField: []byte("bad stuff..."),
	}
	bz, err := badMultisig.Marshal()
	require.NoError(t, err)

	_, err = decodeMultisignatures(bz)
	require.Error(t, err)

	goodMultisig := cryptotypes.MultiSignature{
		Signatures: testSigs,
	}
	bz, err = goodMultisig.Marshal()
	require.NoError(t, err)

	decodedSigs, err := decodeMultisignatures(bz)
	require.NoError(t, err)

	require.Equal(t, testSigs, decodedSigs)
}

func TestModeInfoAndSigToSignatureData(t *testing.T) {
	single := &signing.SingleSignatureData{
		SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
		Signature: []byte("a"),
	}

	// Nested Multi ModeInfo exercises the recursive decode path.
	inner := multisig.NewMultisig(2)
	multisig.AddSignature(inner, single, 0)
	outer := multisig.NewMultisig(2)
	multisig.AddSignature(outer, &signing.SingleSignatureData{
		SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
		Signature: []byte("b"),
	}, 0)
	multisig.AddSignature(outer, inner, 1)
	modeInfo, raw := SignatureDataToModeInfoAndSig(outer)
	got, err := ModeInfoAndSigToSignatureData(modeInfo, raw)
	require.NoError(t, err)
	require.Equal(t, outer, got)

	mi := &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Single_{
		Single: &txtypes.ModeInfo_Single{Mode: signing.SignMode_SIGN_MODE_DIRECT},
	}}
	bad := &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Multi_{
		Multi: &txtypes.ModeInfo_Multi{
			Bitarray:  cryptotypes.NewCompactBitArray(2),
			ModeInfos: []*txtypes.ModeInfo{mi, mi},
		},
	}}

	// fewer nested sigs than ModeInfos must error
	rawShort, err := (&cryptotypes.MultiSignature{Signatures: [][]byte{[]byte("a")}}).Marshal()
	require.NoError(t, err)
	_, err = ModeInfoAndSigToSignatureData(bad, rawShort)
	require.ErrorIs(t, err, sdkerrors.ErrTxDecode)

	// more nested sigs than ModeInfos must error
	rawLong, err := (&cryptotypes.MultiSignature{Signatures: [][]byte{[]byte("a"), []byte("b"), []byte("c")}}).Marshal()
	require.NoError(t, err)
	_, err = ModeInfoAndSigToSignatureData(bad, rawLong)
	require.ErrorIs(t, err, sdkerrors.ErrTxDecode)

	// mismatch inside nested Multi ModeInfo must error on the recursive call
	innerShort, err := (&cryptotypes.MultiSignature{Signatures: [][]byte{[]byte("a")}}).Marshal()
	require.NoError(t, err)
	rawNested, err := (&cryptotypes.MultiSignature{Signatures: [][]byte{[]byte("b"), innerShort}}).Marshal()
	require.NoError(t, err)
	nestedBad := &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Multi_{
		Multi: &txtypes.ModeInfo_Multi{
			Bitarray:  cryptotypes.NewCompactBitArray(2),
			ModeInfos: []*txtypes.ModeInfo{mi, bad},
		},
	}}
	_, err = ModeInfoAndSigToSignatureData(nestedBad, rawNested)
	require.ErrorIs(t, err, sdkerrors.ErrTxDecode)
}
