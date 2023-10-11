//go:build nativebyteorder
// +build nativebyteorder

package memiavl

import (
	"errors"
	"unsafe"
)

func init() {
	buf := [2]byte{}
	*(*uint16)(unsafe.Pointer(&buf[0])) = uint16(0xABCD)

	if buf != [2]byte{0xCD, 0xAB} {
		panic("native byte order is not little endian, please build without nativebyteorder")
	}
}

type NodeLayout = *nodeLayout

// Nodes is a continuously stored IAVL nodes
type Nodes struct {
	nodes []nodeLayout
}

func NewNodes(buf []byte) (Nodes, error) {
	// check alignment and size of the buffer
	p := unsafe.Pointer(unsafe.SliceData(buf))
	if uintptr(p)%unsafe.Alignof(nodeLayout{}) != 0 {
		return Nodes{}, errors.New("input buffer is not aligned")
	}
	size := int(unsafe.Sizeof(nodeLayout{}))
	if len(buf)%size != 0 {
		return Nodes{}, errors.New("input buffer length is not correct")
	}
	nodes := unsafe.Slice((*nodeLayout)(p), len(buf)/size)
	return Nodes{nodes}, nil
}

func (nodes Nodes) Node(i uint32) NodeLayout {
	return &nodes.nodes[i]
}

// see comment of `PersistedNode`
type nodeLayout struct {
	data [4]uint32
	hash [32]byte
}

func (node *nodeLayout) Height() uint8 {
	return uint8(node.data[0])
}

func (node NodeLayout) PreTrees() uint8 {
	return uint8(node.data[0] >> 8)
}

func (node *nodeLayout) Version() uint32 {
	return node.data[1]
}

func (node *nodeLayout) Size() uint32 {
	return node.data[2]
}

func (node *nodeLayout) KeyLeaf() uint32 {
	return node.data[3]
}

func (node *nodeLayout) KeyOffset() uint64 {
	return uint64(node.data[2]) | uint64(node.data[3])<<32
}

func (node *nodeLayout) Hash() []byte {
	return node.hash[:]
}

type LeafLayout = *leafLayout

// Nodes is a continuously stored IAVL nodes
type Leaves struct {
	leaves []leafLayout
}

func NewLeaves(buf []byte) (Leaves, error) {
	// check alignment and size of the buffer
	p := unsafe.Pointer(unsafe.SliceData(buf))
	if uintptr(p)%unsafe.Alignof(leafLayout{}) != 0 {
		return Leaves{}, errors.New("input buffer is not aligned")
	}
	size := int(unsafe.Sizeof(leafLayout{}))
	if len(buf)%size != 0 {
		return Leaves{}, errors.New("input buffer length is not correct")
	}
	leaves := unsafe.Slice((*leafLayout)(p), len(buf)/size)
	return Leaves{leaves}, nil
}

func (leaves Leaves) Leaf(i uint32) LeafLayout {
	return &leaves.leaves[i]
}

type leafLayout struct {
	version   uint32
	keyLen    uint32
	keyOffset uint64
	hash      [32]byte
}

func (leaf *leafLayout) Version() uint32 {
	return leaf.version
}

func (leaf *leafLayout) KeyLength() uint32 {
	return leaf.keyLen
}

func (leaf *leafLayout) KeyOffset() uint64 {
	return leaf.keyOffset
}

func (leaf *leafLayout) Hash() []byte {
	return leaf.hash[:]
}
