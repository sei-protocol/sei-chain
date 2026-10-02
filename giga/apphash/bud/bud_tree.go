package bud

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

const (
	// budLeafPrefix is the domain prefix of a BUD tree leaf hash.
	budLeafPrefix byte = 0x00

	// budInnerPrefix is the domain prefix of a BUD tree inner node hash.
	budInnerPrefix byte = 0x01

	// Byte offsets of the fixed fields in the serialized BUD tree format.
	budTreeVersionOffset = 0
	budTreeCountOffset   = budTreeVersionOffset + 1
	budTreeBudletsOffset = budTreeCountOffset + 8

	// minSerializedBudletSize is the length of the shortest serialized budlet: a 1 byte key and an empty value.
	minSerializedBudletSize = 4 + 1 + 1 + 4 + 8
)

// BUDTree is the Merkle tree over one block's budlets, sorted by key.
type BUDTree struct {
	// The block's budlets, strictly sorted by key.
	budlets []*Budlet

	// The BUD committing to the tree.
	bud apphash.BUD
}

// NewBUDTree returns the BUD tree over one block's budlets. It returns an error unless every budlet is one
// NewBudlet() would accept and the budlets are strictly sorted by key.
func NewBUDTree(
	// The block's budlets. Must be strictly sorted by key. Retained, so it must not be mutated afterward.
	budlets []*Budlet,
) (*BUDTree, error) {
	if err := checkBudletsValid(budlets); err != nil {
		return nil, fmt.Errorf("creating BUD tree: %w", err)
	}
	if err := checkBudletsSortedByKey(budlets); err != nil {
		return nil, fmt.Errorf("creating BUD tree: %w", err)
	}
	return &BUDTree{
		budlets: budlets,
		bud:     hashBUD(uint64(len(budlets)), budTreeRoot(budLeafHashes(budlets))),
	}, nil
}

// DeserializeBUDTree parses a BUD tree from the serialized BUD tree format. It returns an error unless data starts
// with a supported BUD version, holds exactly the number of budlets it declares, and those budlets are ones
// NewBUDTree() would accept.
func DeserializeBUDTree(
	// The serialized BUD tree.
	data []byte,
) (*BUDTree, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("serialized BUD tree is empty")
	}
	if version := data[budTreeVersionOffset]; version != budVersion {
		return nil, fmt.Errorf("unsupported BUD version %d, want %d", version, budVersion)
	}
	if len(data) < budTreeBudletsOffset {
		return nil, fmt.Errorf("serialized BUD tree is %d bytes, want at least %d",
			len(data), budTreeBudletsOffset)
	}

	count := binary.BigEndian.Uint64(data[budTreeCountOffset:budTreeBudletsOffset])
	rest := data[budTreeBudletsOffset:]
	budlets := make([]*Budlet, 0, min(count, uint64(len(rest)/minSerializedBudletSize)))
	for i := uint64(0); i < count; i++ {
		budlet, after, err := readBudlet(rest)
		if err != nil {
			return nil, fmt.Errorf("deserializing BUD tree budlet %d of %d: %w", i, count, err)
		}
		budlets = append(budlets, budlet)
		rest = after
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("serialized BUD tree has %d bytes after its %d budlets", len(rest), count)
	}

	tree, err := NewBUDTree(budlets)
	if err != nil {
		return nil, fmt.Errorf("deserializing BUD tree: %w", err)
	}
	return tree, nil
}

// BUD returns the BUD committing to the tree.
func (tree *BUDTree) BUD() apphash.BUD {
	return tree.bud
}

// Budlets returns the tree's budlets, sorted by key. The caller must not mutate them.
func (tree *BUDTree) Budlets() []*Budlet {
	return tree.budlets
}

// BuildBUDProof returns the BUD proof of the budlet with key, or false when the tree holds no budlet with that
// key. Each call rehashes the whole tree, so it costs as much as NewBUDTree().
func (tree *BUDTree) BuildBUDProof(
	// The key of the budlet to prove.
	key []byte,
) (*BUDProof, bool) {
	index, found := slices.BinarySearchFunc(tree.budlets, key, func(budlet *Budlet, target []byte) int {
		return bytes.Compare(budlet.key, target)
	})
	if !found {
		return nil, false
	}
	return &BUDProof{
		budlet:   *tree.budlets[index],
		count:    uint64(len(tree.budlets)),
		index:    uint64(index), //nolint:gosec // G115 - a slice index is never negative
		siblings: budTreeSiblings(budLeafHashes(tree.budlets), index),
	}, true
}

// Serialize returns the tree in the serialized BUD tree format.
func (tree *BUDTree) Serialize() []byte {
	serialized := make([]byte, 0, budTreeBudletsOffset+len(tree.budlets)*minSerializedBudletSize)
	serialized = append(serialized, budVersion)
	serialized = binary.BigEndian.AppendUint64(serialized, uint64(len(tree.budlets)))
	for _, budlet := range tree.budlets {
		serialized = append(serialized, budlet.Serialize()...)
	}
	return serialized
}

// budLeafHash returns the hash of the BUD tree leaf holding budlet.
func budLeafHash(
	// The budlet the leaf holds.
	budlet *Budlet,
) [32]byte {
	return sha256.Sum256(append([]byte{budLeafPrefix}, budlet.Serialize()...))
}

// budInnerHash returns the hash of the BUD tree inner node with children left and right.
func budInnerHash(
	// The hash of the left child.
	left [32]byte,
	// The hash of the right child.
	right [32]byte,
) [32]byte {
	var preimage [1 + 32 + 32]byte
	preimage[0] = budInnerPrefix
	copy(preimage[1:], left[:])
	copy(preimage[1+32:], right[:])
	return sha256.Sum256(preimage[:])
}

// budLeafHashes returns the leaf hash of each budlet, in order.
func budLeafHashes(
	// The budlets to hash, in key order.
	budlets []*Budlet,
) [][32]byte {
	hashes := make([][32]byte, len(budlets))
	for i, budlet := range budlets {
		hashes[i] = budLeafHash(budlet)
	}
	return hashes
}

// nextBUDTreeLevel returns the BUD tree level above level: each adjacent pair hashed into its parent, and a
// trailing unpaired node carried up unchanged.
func nextBUDTreeLevel(
	// The hashes of one level of a BUD tree, left to right. Must hold at least two.
	level [][32]byte,
) [][32]byte {
	next := make([][32]byte, 0, (len(level)+1)/2)
	for i := 0; i+1 < len(level); i += 2 {
		next = append(next, budInnerHash(level[i], level[i+1]))
	}
	if len(level)%2 == 1 {
		next = append(next, level[len(level)-1])
	}
	return next
}

// budTreeRoot returns the root of a BUD tree, or 32 zero bytes when the tree has no leaves.
func budTreeRoot(
	// The leaf hashes of the BUD tree, in key order.
	leafHashes [][32]byte,
) [32]byte {
	if len(leafHashes) == 0 {
		return [32]byte{}
	}
	level := leafHashes
	for len(level) > 1 {
		level = nextBUDTreeLevel(level)
	}
	return level[0]
}

// budTreeSiblings returns the sibling hashes on the path from one leaf of a BUD tree to its root, leaf end first.
func budTreeSiblings(
	// The leaf hashes of the BUD tree, in key order.
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
		level = nextBUDTreeLevel(level)
		position /= 2
	}
	return siblings
}

// budTreeSiblingCount returns the number of sibling hashes on the path from one leaf of a BUD tree to its root.
func budTreeSiblingCount(
	// The number of leaves in the BUD tree.
	leafCount uint64,
	// The position of the leaf in the BUD tree. Must be less than leafCount.
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
