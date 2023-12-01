package memiavl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
)

// Node interface encapsulate the interface of both PersistedNode and MemNode.
type Node interface {
	Height() uint8
	IsLeaf() bool
	Size() int64
	Version() uint32
	Key() []byte
	Value() []byte
	Left() Node
	Right() Node
	Hash() []byte

	// SafeHash returns byte slice that's safe to retain
	SafeHash() []byte

	// PersistedNode clone a new node, MemNode modify in place
	Mutate(version, cowVersion uint32) *MemNode

	// Get query the value for a key, it's put into interface because a specialized implementation is more efficient.
	Get(key []byte) ([]byte, uint32)
	GetByIndex(uint32) ([]byte, []byte)
}

// setRecursive do set operation.
// it always do modification and return new `MemNode`, even if the value is the same.
// also returns if it's an update or insertion, if update, the tree height and balance is not changed.
func setRecursive(node Node, key, value []byte, version, cowVersion uint32) (*MemNode, bool) {
	if node == nil {
		return newLeafNode(key, value, version), true
	}

	nodeKey := node.Key()
	if node.IsLeaf() {
		switch bytes.Compare(key, nodeKey) {
		case -1:
			return &MemNode{
				height:  1,
				size:    2,
				version: version,
				key:     nodeKey,
				left:    newLeafNode(key, value, version),
				right:   node,
			}, false
		case 1:
			return &MemNode{
				height:  1,
				size:    2,
				version: version,
				key:     key,
				left:    node,
				right:   newLeafNode(key, value, version),
			}, false
		default:
			newNode := node.Mutate(version, cowVersion)
			newNode.value = value
			return newNode, true
		}
	} else {
		var (
			newChild, newNode *MemNode
			updated           bool
		)
		if bytes.Compare(key, nodeKey) == -1 {
			newChild, updated = setRecursive(node.Left(), key, value, version, cowVersion)
			newNode = node.Mutate(version, cowVersion)
			newNode.left = newChild
		} else {
			newChild, updated = setRecursive(node.Right(), key, value, version, cowVersion)
			newNode = node.Mutate(version, cowVersion)
			newNode.right = newChild
		}

		if !updated {
			newNode.updateHeightSize()
			newNode = newNode.reBalance(version, cowVersion)
		}

		return newNode, updated
	}
}

// removeRecursive returns:
// - (nil, origNode, nil) -> nothing changed in subtree
// - (value, nil, newKey) -> leaf node is removed
// - (value, new node, newKey) -> subtree changed
func removeRecursive(node Node, key []byte, version, cowVersion uint32) ([]byte, Node, []byte) {
	if node == nil {
		return nil, nil, nil
	}

	if node.IsLeaf() {
		if bytes.Equal(node.Key(), key) {
			return node.Value(), nil, nil
		}
		return nil, node, nil
	}

	if bytes.Compare(key, node.Key()) == -1 {
		value, newLeft, newKey := removeRecursive(node.Left(), key, version, cowVersion)
		if value == nil {
			return nil, node, nil
		}
		if newLeft == nil {
			return value, node.Right(), node.Key()
		}
		newNode := node.Mutate(version, cowVersion)
		newNode.left = newLeft
		newNode.updateHeightSize()
		return value, newNode.reBalance(version, cowVersion), newKey
	}

	value, newRight, newKey := removeRecursive(node.Right(), key, version, cowVersion)
	if value == nil {
		return nil, node, nil
	}
	if newRight == nil {
		return value, node.Left(), nil
	}

	newNode := node.Mutate(version, cowVersion)
	newNode.right = newRight
	if newKey != nil {
		newNode.key = newKey
	}
	newNode.updateHeightSize()
	return value, newNode.reBalance(version, cowVersion), nil
}

// Writes the node's hash to the given `io.Writer`. This function recursively calls
// children to update hashes.
func writeHashBytes(node Node, w io.Writer) error {
	var (
		n   int
		buf [binary.MaxVarintLen64]byte
	)

	n = binary.PutVarint(buf[:], int64(node.Height()))
	if _, err := w.Write(buf[0:n]); err != nil {
		return fmt.Errorf("writing height, %w", err)
	}
	n = binary.PutVarint(buf[:], node.Size())
	if _, err := w.Write(buf[0:n]); err != nil {
		return fmt.Errorf("writing size, %w", err)
	}
	n = binary.PutVarint(buf[:], int64(node.Version()))
	if _, err := w.Write(buf[0:n]); err != nil {
		return fmt.Errorf("writing version, %w", err)
	}

	// Key is not written for inner nodes, unlike writeBytes.

	if node.IsLeaf() {
		if err := EncodeBytes(w, node.Key()); err != nil {
			return fmt.Errorf("writing key, %w", err)
		}

		// Indirection needed to provide proofs without values.
		// (e.g. ProofLeafNode.ValueHash)
		valueHash := sha256.Sum256(node.Value())

		if err := EncodeBytes(w, valueHash[:]); err != nil {
			return fmt.Errorf("writing value, %w", err)
		}
	} else {
		if err := EncodeBytes(w, node.Left().Hash()); err != nil {
			return fmt.Errorf("writing left hash, %w", err)
		}
		if err := EncodeBytes(w, node.Right().Hash()); err != nil {
			return fmt.Errorf("writing right hash, %w", err)
		}
	}

	return nil
}

// HashNode computes the hash of the node.
func HashNode(node Node) []byte {
	if node == nil {
		return nil
	}
	h := sha256.New()
	if err := writeHashBytes(node, h); err != nil {
		panic(err)
	}
	return h.Sum(nil)
}

// VerifyHash compare node's cached hash with computed one
func VerifyHash(node Node) bool {
	return bytes.Equal(HashNode(node), node.Hash())
}
