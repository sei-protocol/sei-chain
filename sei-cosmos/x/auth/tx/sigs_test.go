package tx

import (
	"testing"

	"github.com/stretchr/testify/require"

	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types/multisig"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
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
	msig := multisig.NewMultisig(2)
	multisig.AddSignature(msig, &signing.SingleSignatureData{
		SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
		Signature: []byte("a"),
	}, 0)
	modeInfo, raw := SignatureDataToModeInfoAndSig(msig)
	got, err := ModeInfoAndSigToSignatureData(modeInfo, raw)
	require.NoError(t, err)
	require.Equal(t, msig, got)

	single := &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Single_{
		Single: &txtypes.ModeInfo_Single{Mode: signing.SignMode_SIGN_MODE_DIRECT},
	}}
	mustMarshal := func(sigs [][]byte) []byte {
		bz, err := (&cryptotypes.MultiSignature{Signatures: sigs}).Marshal()
		require.NoError(t, err)
		return bz
	}
	mustErr := func(mi *txtypes.ModeInfo, sig []byte) {
		_, err := ModeInfoAndSigToSignatureData(mi, sig)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid multisig")
	}

	// fewer sigs than ModeInfos
	mustErr(&txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Multi_{
		Multi: &txtypes.ModeInfo_Multi{
			Bitarray:  cryptotypes.NewCompactBitArray(2),
			ModeInfos: []*txtypes.ModeInfo{single, single},
		},
	}}, mustMarshal([][]byte{[]byte("a")}))

	// more sigs than ModeInfos
	mustErr(&txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Multi_{
		Multi: &txtypes.ModeInfo_Multi{
			Bitarray:  cryptotypes.NewCompactBitArray(1),
			ModeInfos: []*txtypes.ModeInfo{single},
		},
	}}, mustMarshal([][]byte{[]byte("a"), []byte("b")}))

	// nested multi ModeInfo with mismatched child counts
	inner := &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Multi_{
		Multi: &txtypes.ModeInfo_Multi{
			Bitarray:  cryptotypes.NewCompactBitArray(2),
			ModeInfos: []*txtypes.ModeInfo{single, single},
		},
	}}
	mustErr(&txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Multi_{
		Multi: &txtypes.ModeInfo_Multi{
			Bitarray:  cryptotypes.NewCompactBitArray(1),
			ModeInfos: []*txtypes.ModeInfo{inner},
		},
	}}, mustMarshal([][]byte{mustMarshal([][]byte{[]byte("inner-only")})}))
}

func TestGetSignaturesV2_SignerInfoSigCountMismatch(t *testing.T) {
	_, pubKey, addr := testdata.KeyTestPubAddr()
	b := newBuilder()
	require.NoError(t, b.SetMsgs(testdata.NewTestMsg(addr)))
	require.NoError(t, b.SetSignatures(signing.SignatureV2{
		PubKey: pubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: []byte("sig"),
		},
	}))
	b.tx.Signatures = nil
	_, err := b.GetSignaturesV2()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid tx")
}
