//go:build !libsecp256k1_sdk
// +build !libsecp256k1_sdk

package secp256k1

import (
	secp256k1 "github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	cosmoscrypto "github.com/cosmos/cosmos-sdk/crypto/utils"
)

// Sign creates an ECDSA signature on curve Secp256k1, using SHA256 on the msg.
// The returned signature will be of the form R || S (in lower-S form).
func (pk *PrivKey) Sign(msg []byte) ([]byte, error) {
	priv, _ := secp256k1.PrivKeyFromBytes(pk.Key)
	hash := cosmoscrypto.Sha256(msg)
	sig, err := ecdsa.SignCompact(priv, hash, true) // true=compressed pubkey
	if err != nil {
		return nil, err
	}
	// SignCompact struct: [recovery_id][R][S]
	// we need to remove the recovery id and return the R||S bytes
	return sig[1:], nil
}

// VerifySignature verifies a signature of the form R || S.
// It uses the standard btcec/v2 signature verification approach.
func (pubKey *PubKey) VerifySignature(msg []byte, sigStr []byte) bool {
	if len(sigStr) != 64 {
		return false
	}
	p, err := secp256k1.ParsePubKey(pubKey.Key)
	if err != nil {
		return false
	}

	var rScalar, sScalar secp256k1.ModNScalar
	// Check for overflow: SetByteSlice returns true if the value >= curve order N
	if rScalar.SetByteSlice(sigStr[:32]) || sScalar.SetByteSlice(sigStr[32:]) {
		return false
	}
	// Enforce low-S: reject S > N/2 to prevent signature malleability
	if sScalar.IsOverHalfOrder() {
		return false
	}

	signature := ecdsa.NewSignature(&rScalar, &sScalar)

	// Use single hash to match the signing process
	hash := cosmoscrypto.Sha256(msg)
	return signature.Verify(hash, p)
}
