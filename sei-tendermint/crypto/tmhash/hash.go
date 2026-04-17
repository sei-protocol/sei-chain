package tmhash

import (
	"crypto/sha256"
	"hash"
)

const (
	Size = sha256.Size
)

// New returns a new hash.Hash.
func New() hash.Hash {
	return sha256.New()
}

// Sum returns the SHA256 of the bz.
func Sum(bz []byte) []byte {
	h := sha256.Sum256(bz)
	return h[:]
}
