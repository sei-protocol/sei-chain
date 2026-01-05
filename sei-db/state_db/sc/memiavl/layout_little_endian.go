//go:build !nativebyteorder
// +build !nativebyteorder

package memiavl

import (
	"encoding/binary"
)

// Nodes is a continuously stored IAVL nodes
type Nodes struct {
	data []byte
}

func NewNodes(data []byte) (Nodes, error) {
	return Nodes{data}, nil
}

func (nodes Nodes) Node(i uint32) NodeLayout {
	offset := int(i) * SizeNode
	return NodeLayout{data: (*[SizeNode]byte)(nodes.data[offset : offset+SizeNode])}
}

// see comment of `PersistedNode`
type NodeLayout struct {
	data *[SizeNode]byte
}

func (node NodeLayout) Height() uint8 {
	return node.data[OffsetHeight]
}

func (node NodeLayout) PreTrees() uint8 {
	return node.data[OffsetPreTrees]
}

func (node NodeLayout) Version() uint32 {
	return binary.LittleEndian.Uint32(node.data[OffsetVersion : OffsetVersion+4])
}

func (node NodeLayout) Size() uint32 {
	return binary.LittleEndian.Uint32(node.data[OffsetSize : OffsetSize+4])
}

func (node NodeLayout) KeyLeaf() uint32 {
	return binary.LittleEndian.Uint32(node.data[OffsetKeyLeaf : OffsetKeyLeaf+4])
}

func (node NodeLayout) Hash() []byte {
	return node.data[OffsetHash : OffsetHash+SizeHash]
}

// Leaves is a continuously stored IAVL nodes
type Leaves struct {
	data []byte
}

func NewLeaves(data []byte) (Leaves, error) {
	return Leaves{data}, nil
}

func (leaves Leaves) Leaf(i uint32) LeafLayout {
	offset := int(i) * SizeLeaf
	return LeafLayout{data: (*[SizeLeaf]byte)(leaves.data[offset : offset+SizeLeaf])}
}

type LeafLayout struct {
	data *[SizeLeaf]byte
}

func (leaf LeafLayout) Version() uint32 {
	return binary.LittleEndian.Uint32(leaf.data[OffsetLeafVersion : OffsetLeafVersion+4])
}

func (leaf LeafLayout) KeyLength() uint32 {
	return binary.LittleEndian.Uint32(leaf.data[OffsetLeafKeyLen : OffsetLeafKeyLen+4])
}

func (leaf LeafLayout) KeyOffset() uint64 {
	return binary.LittleEndian.Uint64(leaf.data[OffsetLeafKeyOffset : OffsetLeafKeyOffset+8])
}

func (leaf LeafLayout) Hash() []byte {
	return leaf.data[OffsetLeafHash : OffsetLeafHash+32]
}
