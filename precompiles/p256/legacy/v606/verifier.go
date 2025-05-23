package v606

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"math/big"
)

func newPublicKey(x, y *big.Int) *ecdsa.PublicKey {
	// Check if the given coordinates are valid
	if x == nil || y == nil || !elliptic.P256().IsOnCurve(x, y) {
		return nil
	}

	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     x,
		Y:     y,
	}
}

// Verify verifies the given signature (r, s) for the given hash and public key (x, y).
// It returns true if the signature is valid, false otherwise.
func Verify(hash []byte, r, s, x, y *big.Int) bool {
	// Check if the signature is valid
	if r == nil || s == nil {
		return false
	}
	// Create the public key format
	publicKey := newPublicKey(x, y)

	// Check if they are invalid public key coordinates
	if publicKey == nil {
		return false
	}

	// Verify the signature with the public key,
	// then return true if it's valid, false otherwise
	return ecdsa.Verify(publicKey, hash, r, s)
}
