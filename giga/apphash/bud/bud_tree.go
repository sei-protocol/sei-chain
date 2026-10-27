package bud

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/sei-protocol/sei-chain/giga/apphash"
	"github.com/sei-protocol/sei-chain/giga/apphash/merkle"
)

const (
	// Byte offsets of the fixed fields in the serialized BUD tree format.
	budTreeVersionOffset = 0
	budTreeCountOffset   = budTreeVersionOffset + 1
	budTreeBudletsOffset = budTreeCountOffset + 8

	// minSerializedBudletSize is the length of the shortest serialized budlet: a 1 byte key and empty values.
	minSerializedBudletSize = 4 + 1 + 4 + 4 + 8
)

// BUDTree is the Merkle tree over one block's budlets, sorted by key.
type BUDTree struct {
	// The block's budlets, strictly sorted by key.
	budlets []*Budlet

	// The BUD hash committing to the tree.
	budHash apphash.BUDHash
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
		budHash: computeBUDHash(uint64(len(budlets)), merkle.MerkleRoot(budLeafHashes(budlets))),
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

// BUDHash returns the BUD hash committing to the tree.
func (tree *BUDTree) BUDHash() apphash.BUDHash {
	return tree.budHash
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
		siblings: merkle.MerkleSiblings(budLeafHashes(tree.budlets), index),
	}, true
}

// Serialize returns the tree in the serialized BUD tree format.
func (tree *BUDTree) Serialize() []byte {
	serialized := make([]byte, 0, budTreeBudletsOffset+len(tree.budlets)*minSerializedBudletSize)
	serialized = append(serialized, budVersion)
	serialized = binary.BigEndian.AppendUint64(serialized, uint64(len(tree.budlets)))
	for _, budlet := range tree.budlets {
		serialized = budlet.appendSerialized(serialized)
	}
	return serialized
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
