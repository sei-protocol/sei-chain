package receipthash

import "crypto/sha256"

const (
	// merkleLeafPrefix is the domain prefix of a leaf hash in the receipts and logs trees.
	merkleLeafPrefix byte = 0x00

	// merkleInnerPrefix is the domain prefix of an inner node hash in the receipts and logs trees.
	merkleInnerPrefix byte = 0x01
)

// merklePath is the position of one leaf in a receipts or logs tree, and the sibling hashes on its path to the root.
// It does not hold the tree's leaf count, which root() takes.
type merklePath struct {
	// The position of the leaf among the tree's leaves.
	index uint64

	// The sibling hashes on the path from the leaf to the root, leaf end first.
	siblings [][32]byte
}

// newMerklePath returns the path from one leaf of a tree to its root.
func newMerklePath(
	// The leaf hashes of the tree, in order.
	leafHashes [][32]byte,
	// The position of the leaf among leafHashes.
	leafIndex int,
) merklePath {
	var siblings [][32]byte
	level := leafHashes
	position := leafIndex
	for len(level) > 1 {
		switch {
		case position%2 == 1:
			siblings = append(siblings, level[position-1])
		case position+1 < len(level):
			siblings = append(siblings, level[position+1])
		default:
			// The node is carried up unpaired, so it has no sibling at this level.
		}
		level = nextMerkleLevel(level)
		position /= 2
	}
	return merklePath{
		index:    uint64(leafIndex), //nolint:gosec // G115 - a slice index is never negative
		siblings: siblings,
	}
}

// root returns the root of the tree the path places leafHash in.
func (p *merklePath) root(
	// The number of leaves in the tree.
	leafCount uint64,
	// The hash of the leaf at the path's position.
	leafHash [32]byte,
) [32]byte {
	node := leafHash
	siblings := p.siblings
	position := p.index
	// size/2 + size%2 rounds up without overflowing when size is the largest uint64.
	for size := leafCount; size > 1; size = size/2 + size%2 {
		switch {
		case position%2 == 1:
			node = merkleInnerHash(siblings[0], node)
			siblings = siblings[1:]
		case position+1 < size:
			node = merkleInnerHash(node, siblings[0])
			siblings = siblings[1:]
		default:
			// The node is carried up unpaired, so it has no sibling at this level.
		}
		position /= 2
	}
	return node
}

// merkleLeafHash returns the hash of the leaf holding preimage.
func merkleLeafHash(
	// The leaf's preimage.
	preimage []byte,
) [32]byte {
	digest := sha256.New()
	digest.Write([]byte{merkleLeafPrefix})
	digest.Write(preimage)
	var leafHash [32]byte
	digest.Sum(leafHash[:0])
	return leafHash
}

// merkleInnerHash returns the hash of the inner node with children left and right.
func merkleInnerHash(
	// The hash of the left child.
	left [32]byte,
	// The hash of the right child.
	right [32]byte,
) [32]byte {
	var preimage [1 + 32 + 32]byte
	preimage[0] = merkleInnerPrefix
	copy(preimage[1:], left[:])
	copy(preimage[1+32:], right[:])
	return sha256.Sum256(preimage[:])
}

// nextMerkleLevel returns the tree level above level: each adjacent pair hashed into its parent, and a trailing
// unpaired node carried up unchanged.
func nextMerkleLevel(
	// The hashes of one level of a tree, left to right. Must hold at least two.
	level [][32]byte,
) [][32]byte {
	next := make([][32]byte, 0, (len(level)+1)/2)
	for i := 0; i+1 < len(level); i += 2 {
		next = append(next, merkleInnerHash(level[i], level[i+1]))
	}
	if len(level)%2 == 1 {
		next = append(next, level[len(level)-1])
	}
	return next
}

// merkleRoot returns the root of a tree, or SHA-256 of the empty string when the tree has no leaves.
func merkleRoot(
	// The leaf hashes of the tree, in order.
	leafHashes [][32]byte,
) [32]byte {
	if len(leafHashes) == 0 {
		return sha256.Sum256(nil)
	}
	level := leafHashes
	for len(level) > 1 {
		level = nextMerkleLevel(level)
	}
	return level[0]
}
