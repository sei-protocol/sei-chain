package iavl

// NOTE: This file favors int64 as opposed to int for size/counts.
// The Tree on the other hand favors int.  This is intentional.

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/cosmos/iavl/cache"
	"github.com/pkg/errors"

	"github.com/cosmos/iavl/internal/encoding"
)

// Node represents a node in a Tree.
type Node struct {
	key       []byte
	value     []byte
	hash      []byte
	leftHash  []byte
	rightHash []byte
	version   int64
	size      int64
	leftNode  *Node
	rightNode *Node
	height    int8
	persisted bool

	mtx sync.RWMutex
}

var _ cache.Node = (*Node)(nil)

// NewNode returns a new node from a key, value and version.
func NewNode(key []byte, value []byte, version int64) *Node {
	return &Node{
		key:     key,
		value:   value,
		height:  0,
		size:    1,
		version: version,
	}
}

// MakeNode constructs an *Node from an encoded byte slice.
//
// The new node doesn't have its hash saved or set. The caller must set it
// afterwards.
func MakeNode(buf []byte) (*Node, error) {

	// Read node header (height, size, version, key).
	height, n, cause := encoding.DecodeVarint(buf)
	if cause != nil {
		return nil, errors.Wrap(cause, "decoding node.height")
	}
	buf = buf[n:]
	if height < int64(math.MinInt8) || height > int64(math.MaxInt8) {
		return nil, errors.New("invalid height, must be int8")
	}

	size, n, cause := encoding.DecodeVarint(buf)
	if cause != nil {
		return nil, errors.Wrap(cause, "decoding node.size")
	}
	buf = buf[n:]

	ver, n, cause := encoding.DecodeVarint(buf)
	if cause != nil {
		return nil, errors.Wrap(cause, "decoding node.version")
	}
	buf = buf[n:]

	key, n, cause := encoding.DecodeBytes(buf)
	if cause != nil {
		return nil, errors.Wrap(cause, "decoding node.key")
	}
	buf = buf[n:]

	node := &Node{
		height:  int8(height),
		size:    size,
		version: ver,
		key:     key,
	}

	// Read node body.

	if node.isLeaf() {
		val, _, cause := encoding.DecodeBytes(buf)
		if cause != nil {
			return nil, errors.Wrap(cause, "decoding node.value")
		}
		node.value = val
	} else { // Read children.
		leftHash, n, cause := encoding.DecodeBytes(buf)
		if cause != nil {
			return nil, errors.Wrap(cause, "deocding node.leftHash")
		}
		buf = buf[n:]

		rightHash, _, cause := encoding.DecodeBytes(buf)
		if cause != nil {
			return nil, errors.Wrap(cause, "decoding node.rightHash")
		}
		node.leftHash = leftHash
		node.rightHash = rightHash
	}
	return node, nil
}

// to conform with interface name
func (n *Node) GetCacheKey() []byte {
	n.mtx.RLock()
	defer n.mtx.RUnlock()
	return n.hash
}

func (n *Node) GetHash() []byte {
	n.mtx.RLock()
	defer n.mtx.RUnlock()
	return n.hash
}

func (node *Node) GetNodeKey() []byte {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return node.key
}

func (node *Node) GetValue() []byte {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return node.value
}

func (node *Node) GetSize() int64 {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return node.size
}

func (node *Node) GetHeight() int8 {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return node.height
}

func (node *Node) GetVersion() int64 {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return node.version
}

func (node *Node) GetLeftHash() []byte {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return node.leftHash
}

func (node *Node) GetRightHash() []byte {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return node.rightHash
}

func (node *Node) GetLeftNode() *Node {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return node.leftNode
}

func (node *Node) GetRightNode() *Node {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return node.rightNode
}

func (node *Node) GetPersisted() bool {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return node.persisted
}

func (node *Node) SetKey(k []byte) {
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.key = k
}

func (node *Node) SetLeftHash(h []byte) {
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.leftHash = h
}

func (node *Node) SetRightHash(h []byte) {
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.rightHash = h
}

func (node *Node) SetLeftNode(n *Node) {
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.leftNode = n
}

func (node *Node) SetRightNode(n *Node) {
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.rightNode = n
}

func (node *Node) SetHeight(h int8) {
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.height = h
}

func (node *Node) SetVersion(v int64) {
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.version = v
}

func (node *Node) SetSize(s int64) {
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.size = s
}

func (node *Node) SetHash(h []byte) {
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.hash = h
}

func (node *Node) SetPersisted(p bool) {
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.persisted = p
}

// String returns a string representation of the node.
func (node *Node) String() string {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	hashstr := "<no hash>"
	if len(node.hash) > 0 {
		hashstr = fmt.Sprintf("%X", node.hash)
	}
	return fmt.Sprintf("Node{%s:%s@%d %X;%X}#%s",
		ColoredBytes(node.key, Green, Blue),
		ColoredBytes(node.value, Cyan, Blue),
		node.version,
		node.leftHash, node.rightHash,
		hashstr)
}

// clone creates a shallow copy of a node with its hash set to nil.
func (node *Node) clone(version int64) (*Node, error) {
	if node.isLeaf() {
		return nil, ErrCloneLeafNode
	}
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return &Node{
		key:       node.key,
		height:    node.height,
		version:   version,
		size:      node.size,
		hash:      nil,
		leftHash:  node.leftHash,
		leftNode:  node.leftNode,
		rightHash: node.rightHash,
		rightNode: node.rightNode,
		persisted: false,
	}, nil
}

func (node *Node) isLeaf() bool {
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	return node.height == 0
}

// Check if the node has a descendant with the given key.
func (node *Node) has(t *ImmutableTree, key []byte) (has bool, err error) {
	if bytes.Equal(node.GetNodeKey(), key) {
		return true, nil
	}
	if node.isLeaf() {
		return false, nil
	}
	if bytes.Compare(key, node.GetNodeKey()) < 0 {
		leftNode, err := node.getLeftNode(t)
		if err != nil {
			return false, err
		}
		return leftNode.has(t, key)
	}

	rightNode, err := node.getRightNode(t)
	if err != nil {
		return false, err
	}

	return rightNode.has(t, key)
}

// Get a key under the node.
//
// The index is the index in the list of leaf nodes sorted lexicographically by key. The leftmost leaf has index 0.
// It's neighbor has index 1 and so on.
func (node *Node) get(t *ImmutableTree, key []byte) (index int64, value []byte, err error) {
	if node.isLeaf() {
		switch bytes.Compare(node.GetNodeKey(), key) {
		case -1:
			return 1, nil, nil
		case 1:
			return 0, nil, nil
		default:
			return 0, node.GetValue(), nil
		}
	}

	if bytes.Compare(key, node.GetNodeKey()) < 0 {
		leftNode, err := node.getLeftNode(t)
		if err != nil {
			return 0, nil, err
		}

		return leftNode.get(t, key)
	}

	rightNode, err := node.getRightNode(t)
	if err != nil {
		return 0, nil, err
	}

	index, value, err = rightNode.get(t, key)
	if err != nil {
		return 0, nil, err
	}

	index += node.GetSize() - rightNode.GetSize()
	return index, value, nil
}

func (node *Node) getByIndex(t *ImmutableTree, index int64) (key []byte, value []byte, err error) {
	if node.isLeaf() {
		if index == 0 {
			return node.GetNodeKey(), node.GetValue(), nil
		}
		return nil, nil, nil
	}
	// TODO: could improve this by storing the
	// sizes as well as left/right hash.
	leftNode, err := node.getLeftNode(t)
	if err != nil {
		return nil, nil, err
	}

	if index < leftNode.GetSize() {
		return leftNode.getByIndex(t, index)
	}

	rightNode, err := node.getRightNode(t)
	if err != nil {
		return nil, nil, err
	}

	return rightNode.getByIndex(t, index-leftNode.GetSize())
}

// Computes the hash of the node without computing its descendants. Must be
// called on nodes which have descendant node hashes already computed.
func (node *Node) _hash() ([]byte, error) {
	if node.GetHash() != nil {
		return node.GetHash(), nil
	}

	h := sha256.New()
	buf := new(bytes.Buffer)
	if err := node.writeHashBytes(buf); err != nil {
		return nil, err
	}
	_, err := h.Write(buf.Bytes())
	if err != nil {
		return nil, err
	}
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.hash = h.Sum(nil)

	return node.hash, nil
}

// Hash the node and its descendants recursively. This usually mutates all
// descendant nodes. Returns the node hash and number of nodes hashed.
// If the tree is empty (i.e. the node is nil), returns the hash of an empty input,
// to conform with RFC-6962.
func (node *Node) hashWithCount() ([]byte, int64, error) {
	if node == nil {
		return sha256.New().Sum(nil), 0, nil
	}
	if node.GetHash() != nil {
		return node.GetHash(), 0, nil
	}

	h := sha256.New()
	buf := new(bytes.Buffer)
	hashCount, err := node.writeHashBytesRecursively(buf)
	if err != nil {
		return nil, 0, err
	}
	_, err = h.Write(buf.Bytes())
	if err != nil {
		return nil, 0, err
	}
	node.mtx.Lock()
	defer node.mtx.Unlock()
	node.hash = h.Sum(nil)

	return node.hash, hashCount + 1, nil
}

// validate validates the node contents
func (node *Node) validate() error {
	if node == nil {
		return errors.New("node cannot be nil")
	}
	node.mtx.RLock()
	defer node.mtx.RUnlock()
	if node.key == nil {
		return errors.New("key cannot be nil")
	}
	if node.version <= 0 {
		return errors.New("version must be greater than 0")
	}
	if node.height < 0 {
		return errors.New("height cannot be less than 0")
	}
	if node.size < 1 {
		return errors.New("size must be at least 1")
	}

	if node.height == 0 {
		// Leaf nodes
		if node.value == nil {
			return errors.New("value cannot be nil for leaf node")
		}
		if node.leftHash != nil || node.leftNode != nil || node.rightHash != nil || node.rightNode != nil {
			return errors.New("leaf node cannot have children")
		}
		if node.size != 1 {
			return errors.New("leaf nodes must have size 1")
		}
	} else {
		// Inner nodes
		if node.value != nil {
			return errors.New("value must be nil for non-leaf node")
		}
		if node.leftHash == nil && node.rightHash == nil {
			return errors.New("inner node must have children")
		}
	}
	return nil
}

// Writes the node's hash to the given io.Writer. This function expects
// child hashes to be already set.
func (node *Node) writeHashBytes(w io.Writer) error {
	err := encoding.EncodeVarint(w, int64(node.GetHeight()))
	if err != nil {
		return errors.Wrap(err, "writing height")
	}
	err = encoding.EncodeVarint(w, node.GetSize())
	if err != nil {
		return errors.Wrap(err, "writing size")
	}
	err = encoding.EncodeVarint(w, node.GetVersion())
	if err != nil {
		return errors.Wrap(err, "writing version")
	}

	// Key is not written for inner nodes, unlike writeBytes.

	if node.isLeaf() {
		err = encoding.EncodeBytes(w, node.GetNodeKey())
		if err != nil {
			return errors.Wrap(err, "writing key")
		}

		// Indirection needed to provide proofs without values.
		// (e.g. ProofLeafNode.ValueHash)
		valueHash := sha256.Sum256(node.GetValue())

		err = encoding.EncodeBytes(w, valueHash[:])
		if err != nil {
			return errors.Wrap(err, "writing value")
		}
	} else {
		if node.GetLeftHash() == nil || node.GetRightHash() == nil {
			return ErrEmptyChildHash
		}
		err = encoding.EncodeBytes(w, node.GetLeftHash())
		if err != nil {
			return errors.Wrap(err, "writing left hash")
		}
		err = encoding.EncodeBytes(w, node.GetRightHash())
		if err != nil {
			return errors.Wrap(err, "writing right hash")
		}
	}

	return nil
}

// Writes the node's hash to the given io.Writer.
// This function has the side-effect of calling hashWithCount.
func (node *Node) writeHashBytesRecursively(w io.Writer) (hashCount int64, err error) {
	if node.GetLeftNode() != nil {
		leftHash, leftCount, err := node.GetLeftNode().hashWithCount()
		if err != nil {
			return 0, err
		}
		node.SetLeftHash(leftHash)
		hashCount += leftCount
	}
	if node.GetRightNode() != nil {
		rightHash, rightCount, err := node.GetRightNode().hashWithCount()
		if err != nil {
			return 0, err
		}
		node.SetRightHash(rightHash)
		hashCount += rightCount
	}
	err = node.writeHashBytes(w)

	return
}

func (node *Node) encodedSize() int {
	n := 1 +
		encoding.EncodeVarintSize(node.GetSize()) +
		encoding.EncodeVarintSize(node.GetVersion()) +
		encoding.EncodeBytesSize(node.GetNodeKey())
	if node.isLeaf() {
		n += encoding.EncodeBytesSize(node.GetValue())
	} else {
		n += encoding.EncodeBytesSize(node.GetLeftHash()) +
			encoding.EncodeBytesSize(node.GetRightHash())
	}
	return n
}

// Writes the node as a serialized byte slice to the supplied io.Writer.
func (node *Node) writeBytes(w io.Writer) error {
	if node == nil {
		return errors.New("cannot write nil node")
	}
	cause := encoding.EncodeVarint(w, int64(node.GetHeight()))
	if cause != nil {
		return errors.Wrap(cause, "writing height")
	}
	cause = encoding.EncodeVarint(w, node.GetSize())
	if cause != nil {
		return errors.Wrap(cause, "writing size")
	}
	cause = encoding.EncodeVarint(w, node.GetVersion())
	if cause != nil {
		return errors.Wrap(cause, "writing version")
	}

	// Unlike writeHashBytes, key is written for inner nodes.
	cause = encoding.EncodeBytes(w, node.GetNodeKey())
	if cause != nil {
		return errors.Wrap(cause, "writing key")
	}

	if node.isLeaf() {
		cause = encoding.EncodeBytes(w, node.GetValue())
		if cause != nil {
			return errors.Wrap(cause, "writing value")
		}
	} else {
		if node.GetLeftHash() == nil {
			return ErrLeftHashIsNil
		}
		cause = encoding.EncodeBytes(w, node.GetLeftHash())
		if cause != nil {
			return errors.Wrap(cause, "writing left hash")
		}

		if node.GetRightHash() == nil {
			return ErrRightHashIsNil
		}
		cause = encoding.EncodeBytes(w, node.GetRightHash())
		if cause != nil {
			return errors.Wrap(cause, "writing right hash")
		}
	}
	return nil
}

func (node *Node) getLeftNode(t *ImmutableTree) (*Node, error) {
	if node.GetLeftNode() != nil {
		return node.GetLeftNode(), nil
	}
	leftNode, err := t.ndb.GetNode(node.GetLeftHash())
	if err != nil {
		return nil, err
	}

	return leftNode, nil
}

func (node *Node) getRightNode(t *ImmutableTree) (*Node, error) {
	if node.GetRightNode() != nil {
		return node.GetRightNode(), nil
	}
	rightNode, err := t.ndb.GetNode(node.GetRightHash())
	if err != nil {
		return nil, err
	}

	return rightNode, nil
}

// NOTE: mutates height and size
func (node *Node) calcHeightAndSize(t *ImmutableTree) error {
	leftNode, err := node.getLeftNode(t)
	if err != nil {
		return err
	}

	rightNode, err := node.getRightNode(t)
	if err != nil {
		return err
	}

	height := maxInt8(leftNode.GetHeight(), rightNode.GetHeight()) + 1
	size := leftNode.GetSize() + rightNode.GetSize()
	node.SetHeight(height)
	node.SetSize(size)
	return nil
}

func (node *Node) calcBalance(t *ImmutableTree) (int, error) {
	leftNode, err := node.getLeftNode(t)
	if err != nil {
		return 0, err
	}

	rightNode, err := node.getRightNode(t)
	if err != nil {
		return 0, err
	}

	return int(leftNode.GetHeight()) - int(rightNode.GetHeight()), nil
}

// traverse is a wrapper over traverseInRange when we want the whole tree
// nolint: unparam
func (node *Node) traverse(t *ImmutableTree, ascending bool, cb func(*Node) bool) bool {
	return node.traverseInRange(t, nil, nil, ascending, false, false, func(node *Node) bool {
		return cb(node)
	})
}

// traversePost is a wrapper over traverseInRange when we want the whole tree post-order
func (node *Node) traversePost(t *ImmutableTree, ascending bool, cb func(*Node) bool) bool {
	return node.traverseInRange(t, nil, nil, ascending, false, true, func(node *Node) bool {
		return cb(node)
	})
}

func (node *Node) traverseInRange(tree *ImmutableTree, start, end []byte, ascending bool, inclusive bool, post bool, cb func(*Node) bool) bool {
	stop := false
	t := node.newTraversal(tree, start, end, ascending, inclusive, post, false)
	// TODO: figure out how to handle these errors
	for node2, err := t.next(); node2 != nil && err == nil; node2, err = t.next() {
		stop = cb(node2)
		if stop {
			return stop
		}
	}
	return stop
}

var (
	ErrCloneLeafNode  = fmt.Errorf("attempt to copy a leaf node")
	ErrEmptyChildHash = fmt.Errorf("found an empty child hash")
	ErrLeftHashIsNil  = fmt.Errorf("node.leftHash was nil in writeBytes")
	ErrRightHashIsNil = fmt.Errorf("node.rightHash was nil in writeBytes")
)
