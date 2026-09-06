package walrus

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"

	"github.com/cespare/xxhash/v2"
)

// The longest key hashed without touching the heap. Keys above it take the streaming path, which is slower
// but unbounded.
const maxInlineHashKey = 96

// newPodSalt draws the salt one pod's hashes are derived from.
//
// The salt is drawn when the pod is built, after the keys it will hold are already fixed, and every pod draws
// its own. That ordering is what defeats a key chosen to collide with others: the function it would have to
// collide under does not exist until the keys are immutable, and a pod that seals is never rewritten.
func newPodSalt() (uint64, error) {
	var buffer [8]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return 0, fmt.Errorf("failed to draw a pod salt: %w", err)
	}
	return binary.BigEndian.Uint64(buffer[:]), nil
}

// podKeyHash returns the value a pod's index is ordered by and its bloom filter derives positions from.
//
// The salt is hashed with the key rather than mixed into the result, so two keys collide only when they
// collide under that pod's salt. Keys that collide under one pod's salt are unrelated to those that collide
// under another's.
func podKeyHash(salt uint64, key []byte) uint64 {
	if len(key) <= maxInlineHashKey {
		var buffer [8 + maxInlineHashKey]byte
		binary.BigEndian.PutUint64(buffer[0:8], salt)
		copy(buffer[8:], key)
		return xxhash.Sum64(buffer[:8+len(key)])
	}

	var digest xxhash.Digest
	digest.Reset()
	var prefix [8]byte
	binary.BigEndian.PutUint64(prefix[:], salt)
	_, _ = digest.Write(prefix[:])
	_, _ = digest.Write(key)
	return digest.Sum64()
}

// indexHash returns the 32 bits of a pod hash that the hash index is ordered by.
func indexHash(hash uint64) uint32 {
	return uint32(hash >> 32) //nolint:gosec // G115 - the shift leaves exactly the 32 bits being kept
}
