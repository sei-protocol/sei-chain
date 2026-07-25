package multisig

import (
	"fmt"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
)

// ValidateSignatureDataStructure checks SignatureData tree shape only (no crypto).
// For MultiSignatureData, len(Signatures) must equal the number of true bits in
// BitArray at every nesting level (the same invariant AddSignature maintains).
func ValidateSignatureDataStructure(data signing.SignatureData) error {
	if data == nil {
		return fmt.Errorf("signature data is required")
	}
	switch data := data.(type) {
	case *signing.SingleSignatureData:
		return nil
	case *signing.MultiSignatureData:
		return validateMultiSignatureDataStructure(data)
	default:
		return fmt.Errorf("unexpected signature data type %T", data)
	}
}

func validateMultiSignatureDataStructure(sig *signing.MultiSignatureData) error {
	if sig == nil {
		return fmt.Errorf("multi signature data is required")
	}
	if sig.BitArray == nil {
		return fmt.Errorf("bit array is required")
	}
	size := sig.BitArray.Count()
	nTrue := sig.BitArray.NumTrueBitsBefore(size)
	if len(sig.Signatures) != nTrue {
		return fmt.Errorf("signature size is incorrect %d", len(sig.Signatures))
	}
	for i, child := range sig.Signatures {
		if child == nil {
			return fmt.Errorf("signature data at index %d is nil", i)
		}
		if err := ValidateSignatureDataStructure(child); err != nil {
			return err
		}
	}
	return nil
}

// AminoMultisignature is used to represent amino multi-signatures for StdTx's.
// It is assumed that all signatures were made with SIGN_MODE_LEGACY_AMINO_JSON.
// Sigs is a list of signatures, sorted by corresponding index.
type AminoMultisignature struct {
	BitArray *types.CompactBitArray
	Sigs     [][]byte
}

// NewMultisig returns a new MultiSignatureData
func NewMultisig(n int) *signing.MultiSignatureData {
	return &signing.MultiSignatureData{
		BitArray:   types.NewCompactBitArray(n),
		Signatures: make([]signing.SignatureData, 0, n),
	}
}

// GetIndex returns the index of pk in keys. Returns -1 if not found
func getIndex(pk types.PubKey, keys []types.PubKey) int {
	for i := 0; i < len(keys); i++ {
		if pk.Equals(keys[i]) {
			return i
		}
	}
	return -1
}

// AddSignature adds a signature to the multisig, at the corresponding index. The index must
// represent the pubkey index in the LegacyAmingPubKey structure, which verifies this signature.
// If the signature already exists, replace it.
func AddSignature(mSig *signing.MultiSignatureData, sig signing.SignatureData, index int) {
	newSigIndex := mSig.BitArray.NumTrueBitsBefore(index)
	// Signature already exists, just replace the value there
	if mSig.BitArray.GetIndex(index) {
		mSig.Signatures[newSigIndex] = sig
		return
	}
	mSig.BitArray.SetIndex(index, true)
	// Optimization if the index is the greatest index
	if newSigIndex == len(mSig.Signatures) {
		mSig.Signatures = append(mSig.Signatures, sig)
		return
	}
	// Expand slice by one with a dummy element, move all elements after i
	// over by one, then place the new signature in that gap.
	mSig.Signatures = append(mSig.Signatures, &signing.SingleSignatureData{})
	copy(mSig.Signatures[newSigIndex+1:], mSig.Signatures[newSigIndex:])
	mSig.Signatures[newSigIndex] = sig
}

// AddSignatureFromPubKey adds a signature to the multisig, at the index in
// keys corresponding to the provided pubkey.
func AddSignatureFromPubKey(mSig *signing.MultiSignatureData, sig signing.SignatureData, pubkey types.PubKey, keys []types.PubKey) error {
	if mSig == nil {
		return fmt.Errorf("value of mSig is nil %v", mSig)
	}
	if sig == nil {
		return fmt.Errorf("value of sig is nil %v", sig)
	}

	if pubkey == nil || keys == nil {
		return fmt.Errorf("pubkey or keys can't be nil %v %v", pubkey, keys)
	}
	index := getIndex(pubkey, keys)
	if index == -1 {
		keysStr := make([]string, len(keys))
		for i, k := range keys {
			keysStr[i] = fmt.Sprintf("%X", k.Bytes())
		}

		return fmt.Errorf("provided key %X doesn't exist in pubkeys: \n%s", pubkey.Bytes(), strings.Join(keysStr, "\n"))
	}

	AddSignature(mSig, sig, index)
	return nil
}

func AddSignatureV2(mSig *signing.MultiSignatureData, sig signing.SignatureV2, keys []types.PubKey) error {
	return AddSignatureFromPubKey(mSig, sig.Data, sig.PubKey, keys)
}
