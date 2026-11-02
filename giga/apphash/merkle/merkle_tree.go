package merkle

import (
	"crypto/sha256"
	"fmt"
)

const (
	// merkleLeafPrefix is the domain prefix of a leaf hash.
	merkleLeafPrefix byte = 0x00

	// merkleInnerPrefix is the domain prefix of an inner node hash.
	merkleInnerPrefix byte = 0x01
)

// MerkleLeafHash returns the hash of the leaf holding preimage.
func MerkleLeafHash(
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

// MerkleInnerHash returns the hash of the inner node with children left and right.
func MerkleInnerHash(
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

// MerkleRoot returns the root of a tree, or SHA-256 of the empty string when the tree has no leaves.
func MerkleRoot(
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

// MerkleSiblings returns the sibling hashes on the path from one leaf of a tree to its root, leaf end first.
func MerkleSiblings(
	// The leaf hashes of the tree, in order.
	leafHashes [][32]byte,
	// The position of the leaf among leafHashes.
	leafIndex int,
) [][32]byte {
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
	return siblings
}

// MerkleSiblingCount returns the number of sibling hashes on the path from one leaf of a tree to its root.
func MerkleSiblingCount(
	// The number of leaves in the tree.
	leafCount uint64,
	// The position of the leaf in the tree. Must be less than leafCount.
	leafIndex uint64,
) int {
	count := 0
	position := leafIndex
	// size/2 + size%2 rounds up without overflowing when size is the largest uint64.
	for size := leafCount; size > 1; size = size/2 + size%2 {
		if position%2 == 1 || position+1 < size {
			count++
		}
		position /= 2
	}
	return count
}

// MerkleRootFromSiblings returns the root of the tree that siblings place leafHash in. It returns an error unless
// leafIndex is less than leafCount and siblings holds MerkleSiblingCount(leafCount, leafIndex) hashes.
func MerkleRootFromSiblings(
	// The number of leaves in the tree.
	leafCount uint64,
	// The position of the leaf in the tree.
	leafIndex uint64,
	// The hash of the leaf.
	leafHash [32]byte,
	// The sibling hashes on the path from the leaf to the root, leaf end first.
	siblings [][32]byte,
) ([32]byte, error) {
	if leafIndex >= leafCount {
		return [32]byte{}, fmt.Errorf("leaf index %d is not less than leaf count %d", leafIndex, leafCount)
	}
	if want := MerkleSiblingCount(leafCount, leafIndex); len(siblings) != want {
		return [32]byte{}, fmt.Errorf("%d siblings, want %d for leaf %d of %d", len(siblings), want, leafIndex,
			leafCount)
	}

	node := leafHash
	position := leafIndex
	// size/2 + size%2 rounds up without overflowing when size is the largest uint64.
	for size := leafCount; size > 1; size = size/2 + size%2 {
		switch {
		case position%2 == 1:
			node = MerkleInnerHash(siblings[0], node)
			siblings = siblings[1:]
		case position+1 < size:
			node = MerkleInnerHash(node, siblings[0])
			siblings = siblings[1:]
		default:
			// The node is carried up unpaired, so it has no sibling at this level.
		}
		position /= 2
	}
	return node, nil
}

// nextMerkleLevel returns the tree level above level: each adjacent pair hashed into its parent, and a trailing
// unpaired node carried up unchanged.
func nextMerkleLevel(
	// The hashes of one level of a tree, left to right. Must hold at least two.
	level [][32]byte,
) [][32]byte {
	next := make([][32]byte, 0, (len(level)+1)/2)
	for i := 0; i+1 < len(level); i += 2 {
		next = append(next, MerkleInnerHash(level[i], level[i+1]))
	}
	if len(level)%2 == 1 {
		next = append(next, level[len(level)-1])
	}
	return next
}
