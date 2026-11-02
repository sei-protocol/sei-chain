package bud

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash/merkle"
)

// ComputeBUDTreeHash returns the root of the BUD tree over one block's budlets. It returns an error unless every
// budlet is one NewBudlet() would accept and the budlets are strictly sorted by key.
func ComputeBUDTreeHash(
	// The block's budlets. Must be strictly sorted by key.
	budlets []*Budlet,
) ([32]byte, error) {
	if err := checkBudletsValid(budlets); err != nil {
		return [32]byte{}, fmt.Errorf("computing BUD tree hash: %w", err)
	}
	if err := checkBudletsSortedByKey(budlets); err != nil {
		return [32]byte{}, fmt.Errorf("computing BUD tree hash: %w", err)
	}
	return merkle.MerkleRoot(budLeafHashes(budlets)), nil
}

// budLeafHash returns the hash of the BUD tree leaf holding budlet, and scratch reused to hold the budlet's
// serialization, for the caller to pass to the next call.
func budLeafHash(
	// The budlet the leaf holds.
	budlet *Budlet,
	// A buffer whose contents are discarded and whose capacity is reused. May be nil.
	scratch []byte,
) ([32]byte, []byte) {
	scratch = budlet.appendSerialized(scratch[:0])
	return merkle.MerkleLeafHash(scratch), scratch
}

// budLeafHashes returns the leaf hash of each budlet, in order.
func budLeafHashes(
	// The budlets to hash, in key order.
	budlets []*Budlet,
) [][32]byte {
	hashes := make([][32]byte, len(budlets))
	var scratch []byte
	for i, budlet := range budlets {
		hashes[i], scratch = budLeafHash(budlet, scratch)
	}
	return hashes
}
