package bud

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

const (
	// budDomain is the domain separation tag that leads the BUD preimage.
	budDomain = "sei-bud"

	// budVersion is the version of the BUD scheme produced by this package: the budlet serialization, the BUD
	// tree and its serialization, and the BUD preimage.
	budVersion uint8 = 1
)

// hashBUD returns the BUD committing to a BUD tree.
func hashBUD(
	// The number of leaves in the BUD tree.
	leafCount uint64,
	// The root of the BUD tree.
	treeRoot [32]byte,
) apphash.BUD {
	preimage := make([]byte, 0, len(budDomain)+1+8+32)
	preimage = append(preimage, budDomain...)
	preimage = append(preimage, budVersion)
	preimage = binary.BigEndian.AppendUint64(preimage, leafCount)
	preimage = append(preimage, treeRoot[:]...)
	return apphash.BUD(crypto.Keccak256(preimage))
}
