package bud

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

const (
	// budHashDomain is the domain separation tag that leads the BUD hash preimage.
	budHashDomain = "sei-bud"

	// budVersion is the version of the BUD scheme produced by this package: the budlet serialization, the BUD
	// tree, and the BUD hash preimage.
	budVersion uint8 = 1
)

// ComputeBUDHash returns the BUD hash committing to a BUD tree.
func ComputeBUDHash(
	// The number of leaves in the BUD tree.
	leafCount uint64,
	// The root of the BUD tree.
	treeRoot [32]byte,
) apphash.BUDHash {
	preimage := make([]byte, 0, len(budHashDomain)+1+8+32)
	preimage = append(preimage, budHashDomain...)
	preimage = append(preimage, budVersion)
	preimage = binary.BigEndian.AppendUint64(preimage, leafCount)
	preimage = append(preimage, treeRoot[:]...)
	return sha256.Sum256(preimage)
}
