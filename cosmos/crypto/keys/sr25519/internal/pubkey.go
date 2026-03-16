package internal

import (
	"bytes"
	"fmt"

	"github.com/oasisprotocol/curve25519-voi/primitives/sr25519"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
)

const (
	// PubKeySize is the size of a sr25519 public key in bytes.
	PubKeySize = sr25519.PublicKeySize

	// SignatureSize is the size of a sr25519 signature in bytes.
	SignatureSize = sr25519.SignatureSize
)

// PubKey implements crypto.PubKey.
type PubKey []byte

// Address is the SHA256-20 of the raw pubkey bytes.
func (pubKey PubKey) Address() crypto.Address {
	if len(pubKey) != PubKeySize {
		panic("pubkey is incorrect size")
	}
	return crypto.AddressHash(pubKey)
}

// Bytes returns the PubKey byte format.
func (pubKey PubKey) Bytes() []byte {
	return []byte(pubKey)
}

func (pubKey PubKey) Equals(other PubKey) bool {
	return bytes.Equal(pubKey[:], other[:])
}

func (pubKey PubKey) VerifySignature(msg []byte, sigBytes []byte) bool {
	var srpk sr25519.PublicKey
	if err := srpk.UnmarshalBinary(pubKey); err != nil {
		return false
	}

	var sig sr25519.Signature
	if err := sig.UnmarshalBinary(sigBytes); err != nil {
		return false
	}

	st := signingCtx.NewTranscriptBytes(msg)
	return srpk.Verify(st, &sig)
}

func (pubKey PubKey) String() string {
	return fmt.Sprintf("PubKeySr25519{%X}", []byte(pubKey))
}
