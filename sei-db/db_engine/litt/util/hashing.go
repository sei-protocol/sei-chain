package util

import (
	"encoding/binary"

	"github.com/dchest/siphash"
)

// Perm64 computes A permutation (invertible function) on 64 bits.
// The constants were found by automated search, to
// optimize avalanche. Avalanche means that for a
// random number x, flipping bit i of x has about a
// 50 percent chance of flipping bit j of perm64(x).
// For each possible pair (i,j), this function achieves
// a probability between 49.8 and 50.2 percent.
//
// Warning: this is not a cryptographic hash function. This hash function may be suitable for hash tables, but not for
// cryptographic purposes. It is trivially easy to reverse this function.
//
// Algorithm borrowed from https://github.com/hiero-ledger/hiero-consensus-node/blob/main/platform-sdk/swirlds-common/src/main/java/com/swirlds/common/utility/NonCryptographicHashing.java
// (original implementation is under Apache 2.0 license, algorithm designed by Leemon Baird)
func Perm64(x uint64) uint64 {
	// This is necessary so that 0 does not hash to 0.
	// As a side effect this constant will hash to 0.
	x ^= 0x5e8a016a5eb99c18

	x += x << 30
	x ^= x >> 27
	x += x << 16
	x ^= x >> 20
	x += x << 5
	x ^= x >> 18
	x += x << 10
	x ^= x >> 24
	x += x << 30
	return x
}

// Perm64Bytes hashes a byte slice using perm64.
func Perm64Bytes(b []byte) uint64 {
	x := uint64(0)

	for i := 0; i < len(b); i += 8 {
		var next uint64
		if i+8 <= len(b) {
			// grab the next 8 bytes
			next = binary.BigEndian.Uint64(b[i:])
		} else {
			// insufficient bytes, pad with zeros
			nextBytes := make([]byte, 8)
			copy(nextBytes, b[i:])
			next = binary.BigEndian.Uint64(nextBytes)
		}
		x = Perm64(next ^ x)
	}

	return x
}

// LegacyHashKey hash a key using the original littDB hash function. Once all data stored using the original
// hash function is deleted, this function can be removed.
func LegacyHashKey(key []byte, salt uint32) uint32 {
	return uint32(Perm64(Perm64Bytes(key) ^ uint64(salt)))
}

// HashKey hashes a key using perm64 and a salt.
func HashKey(key []byte, salt [16]byte) uint32 {
	leftSalt := binary.BigEndian.Uint64(salt[:8])
	rightSalt := binary.BigEndian.Uint64(salt[8:])
	hash := siphash.Hash(leftSalt, rightSalt, key)
	return uint32(hash)
}
