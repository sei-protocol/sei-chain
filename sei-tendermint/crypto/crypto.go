package crypto

import (
	"crypto/sha256"

	ed25519 "github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/libs/bytes"
)

const (
	// HashSize is the size in bytes of an AddressHash.
	HashSize = sha256.Size

	// AddressSize is the size of a pubkey address.
	AddressSize = 20
)

// An address is a []byte, but hex-encoded even in JSON.
// []byte leaves us the option to change the address length.
// Use an alias so Unmarshal methods (with ptr receivers) are available too.
type Address = bytes.HexBytes

// AddressHash computes a truncated SHA-256 hash of bz for use as
// a peer address.
//
// See: https://docs.tendermint.com/master/spec/core/data_structures.html#address
func AddressHash(bz []byte) Address {
	h := sha256.Sum256(bz)
	return Address(h[:AddressSize])
}

// Checksum returns the SHA256 of the bz.
func Checksum(bz []byte) []byte {
	h := sha256.Sum256(bz)
	return h[:]
}

type PubKey = ed25519.PublicKey
type PrivKey = ed25519.SecretKey
type Sig = ed25519.Signature
type BatchVerifier = ed25519.BatchVerifier
type ErrBadSig = ed25519.ErrBadSig

func SigFromBytes(raw []byte) (Sig, error) {
	return ed25519.SignatureFromBytes(raw)
}

func NewBatchVerifier() *BatchVerifier {
	return ed25519.NewBatchVerifier()
}
