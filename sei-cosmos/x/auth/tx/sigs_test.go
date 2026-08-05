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

	// fewer nested sigs than ModeInfos must error
	rawShort, err := (&cryptotypes.MultiSignature{Signatures: [][]byte{[]byte("a")}}).Marshal()
	require.NoError(t, err)
	mi := &txtypes.ModeInfo_Single_{Single: &txtypes.ModeInfo_Single{Mode: signing.SignMode_SIGN_MODE_DIRECT}}
	bad := &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Multi_{
		Multi: &txtypes.ModeInfo_Multi{
			Bitarray:  cryptotypes.NewCompactBitArray(2),
			ModeInfos: []*txtypes.ModeInfo{{Sum: mi}, {Sum: mi}},
		},
	}}
	_, err = ModeInfoAndSigToSignatureData(bad, rawShort)
	require.Error(t, err)
}

func TestGetSignaturesV2_SignerInfoSigCountMismatch(t *testing.T) {
	_, pk, addr := testdata.KeyTestPubAddr()
	b := newBuilder()
	require.NoError(t, b.SetMsgs(testdata.NewTestMsg(addr)))
	require.NoError(t, b.SetSignatures(signing.SignatureV2{
		PubKey: pk,
		Data:   &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: []byte("sig")},
	}))
	b.tx.Signatures = nil
	_, err := b.GetSignaturesV2()
	require.Error(t, err)
}
