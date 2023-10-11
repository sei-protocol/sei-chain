package memiavl

import (
	"bytes"
	"encoding/binary"
	"io"
)

type MemNode struct {
	height  uint8
	size    int64
	version uint32
	key     []byte
	value   []byte
	left    Node
	right   Node

	hash []byte
}

var _ Node = (*MemNode)(nil)

func newLeafNode(key, value []byte, version uint32) *MemNode {
	return &MemNode{
		key: key, value: value, version: version, size: 1,
	}
}

func (node *MemNode) Height() uint8 {
	return node.height
}

func (node *MemNode) IsLeaf() bool {
	return node.height == 0
}

func (node *MemNode) Size() int64 {
	return node.size
}

func (node *MemNode) Version() uint32 {
	return node.version
}

func (node *MemNode) Key() []byte {
	return node.key
}

func (node *MemNode) Value() []byte {
	return node.value
}

func (node *MemNode) Left() Node {
	return node.left
}

func (node *MemNode) Right() Node {
	return node.right
}

// Mutate clones the node if it's version is smaller than or equal to cowVersion, otherwise modify in-place
func (node *MemNode) Mutate(version, cowVersion uint32) *MemNode {
	n := node
	if node.version <= cowVersion {
		cloned := *node
		n = &cloned
	}
	n.version = version
	n.hash = nil
	return n
}

func (node *MemNode) SafeHash() []byte {
	return node.Hash()
}

// Computes the hash of the node without computing its descendants. Must be
// called on nodes which have descendant node hashes already computed.
func (node *MemNode) Hash() []byte {
	if node == nil {
		return nil
	}
	if node.hash != nil {
		return node.hash
	}
	node.hash = HashNode(node)
	return node.hash
}

func (node *MemNode) updateHeightSize() {
	node.height = maxUInt8(node.left.Height(), node.right.Height()) + 1
	node.size = node.left.Size() + node.right.Size()
}

func (node *MemNode) calcBalance() int {
	return int(node.left.Height()) - int(node.right.Height())
}

func calcBalance(node Node) int {
	return int(node.Left().Height()) - int(node.Right().Height())
}

// Invariant: node is returned by `Mutate(version)`.
//
//	   S               L
//	  / \      =>     / \
//	 L                   S
//	/ \                 / \
//	  LR               LR
func (node *MemNode) rotateRight(version, cowVersion uint32) *MemNode {
	newSelf := node.left.Mutate(version, cowVersion)
	node.left = node.left.Right()
	newSelf.right = node
	node.updateHeightSize()
	newSelf.updateHeightSize()
	return newSelf
}

// Invariant: node is returned by `Mutate(version, cowVersion)`.
//
//	 S              R
//	/ \     =>     / \
//	    R         S
//	   / \       / \
//	 RL             RL
func (node *MemNode) rotateLeft(version, cowVersion uint32) *MemNode {
	newSelf := node.right.Mutate(version, cowVersion)
	node.right = node.right.Left()
	newSelf.left = node
	node.updateHeightSize()
	newSelf.updateHeightSize()
	return newSelf
}

// Invariant: node is returned by `Mutate(version, cowVersion)`.
func (node *MemNode) reBalance(version, cowVersion uint32) *MemNode {
	balance := node.calcBalance()
	switch {
	case balance > 1:
		leftBalance := calcBalance(node.left)
		if leftBalance >= 0 {
			// left left
			return node.rotateRight(version, cowVersion)
		}
		// left right
		node.left = node.left.Mutate(version, cowVersion).rotateLeft(version, cowVersion)
		return node.rotateRight(version, cowVersion)
	case balance < -1:
		rightBalance := calcBalance(node.right)
		if rightBalance <= 0 {
			// right right
			return node.rotateLeft(version, cowVersion)
		}
		// right left
		node.right = node.right.Mutate(version, cowVersion).rotateRight(version, cowVersion)
		return node.rotateLeft(version, cowVersion)
	default:
		// nothing changed
		return node
	}
}

func (node *MemNode) Get(key []byte) ([]byte, uint32) {
	if node.IsLeaf() {
		switch bytes.Compare(node.key, key) {
		case -1:
			return nil, 1
		case 1:
			return nil, 0
		default:
			return node.value, 0
		}
	}

	if bytes.Compare(key, node.key) == -1 {
		return node.Left().Get(key)
	}
	right := node.Right()
	value, index := right.Get(key)
	return value, index + uint32(node.Size()) - uint32(right.Size())
}

func (node *MemNode) GetByIndex(index uint32) ([]byte, []byte) {
	if node.IsLeaf() {
		if index == 0 {
			return node.key, node.value
		}
		return nil, nil
	}

	left := node.Left()
	leftSize := uint32(left.Size())
	if index < leftSize {
		return left.GetByIndex(index)
	}

	right := node.Right()
	return right.GetByIndex(index - leftSize)
}

// EncodeBytes writes a varint length-prefixed byte slice to the writer,
// it's used for hash computation, must be compactible with the official IAVL implementation.
func EncodeBytes(w io.Writer, bz []byte) error {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], uint64(len(bz)))
	if _, err := w.Write(buf[0:n]); err != nil {
		return err
	}
	_, err := w.Write(bz)
	return err
}

func maxUInt8(a, b uint8) uint8 {
	if a > b {
		return a
	}
	return b
}
