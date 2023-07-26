package memiavl

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"

	"github.com/cosmos/iavl"
	"github.com/cosmos/iavl/cache"
	dbm "github.com/tendermint/tm-db"
)

var emptyHash = sha256.New().Sum(nil)

// verify change sets by replay them to rebuild iavl tree and verify the root hashes
type Tree struct {
	version uint32
	// root node of empty tree is represented as `nil`
	root     Node
	snapshot *Snapshot

	// simple lru cache provided by iavl library
	cache cache.Cache

	initialVersion, cowVersion uint32

	// when true, the get and iterator methods could return a slice pointing to mmaped blob files.
	zeroCopy bool
}

type cacheNode struct {
	key, value []byte
}

func (n *cacheNode) GetCacheKey() []byte {
	return n.key
}

func (n *cacheNode) GetKey() []byte {
	return n.key
}

// NewEmptyTree creates an empty tree at an arbitrary version.
func NewEmptyTree(version uint64, initialVersion uint32, cacheSize int) *Tree {
	if version >= math.MaxUint32 {
		panic("version overflows uint32")
	}

	return &Tree{
		version:        uint32(version),
		initialVersion: initialVersion,
		// no need to copy if the tree is not backed by snapshot
		zeroCopy: true,
		cache:    cache.New(cacheSize),
	}
}

// New creates an empty tree at genesis version
func New(cacheSize int) *Tree {
	return NewEmptyTree(0, 0, cacheSize)
}

// New creates a empty tree with initial-version,
// it happens when a new store created at the middle of the chain.
func NewWithInitialVersion(initialVersion uint32, cacheSize int) *Tree {
	return NewEmptyTree(0, initialVersion, cacheSize)
}

// NewFromSnapshot mmap the blob files and create the root node.
func NewFromSnapshot(snapshot *Snapshot, zeroCopy bool, cacheSize int) *Tree {
	tree := &Tree{
		version:  snapshot.Version(),
		snapshot: snapshot,
		zeroCopy: zeroCopy,
		cache:    cache.New(cacheSize),
	}

	if !snapshot.IsEmpty() {
		tree.root = snapshot.RootNode()
	}

	return tree
}

func (t *Tree) SetZeroCopy(zeroCopy bool) {
	t.zeroCopy = zeroCopy
}

func (t *Tree) IsEmpty() bool {
	return t.root == nil
}

func (t *Tree) SetInitialVersion(initialVersion int64) error {
	if initialVersion >= math.MaxUint32 {
		return fmt.Errorf("version overflows uint32: %d", initialVersion)
	}
	t.initialVersion = uint32(initialVersion)
	return nil
}

// Copy returns a snapshot of the tree which won't be modified by further modifications on the main tree,
// the returned new tree can be accessed concurrently with the main tree.
func (t *Tree) Copy(cacheSize int) *Tree {
	if _, ok := t.root.(*MemNode); ok {
		// protect the existing `MemNode`s from get modified in-place
		t.cowVersion = t.version
	}
	newTree := *t
	// cache is not copied along because it's not thread-safe to access
	newTree.cache = cache.New(cacheSize)
	return &newTree
}

// ApplyChangeSet apply the change set of a whole version, and update hashes.
func (t *Tree) ApplyChangeSet(changeSet iavl.ChangeSet, updateHash bool) ([]byte, int64, error) {
	for _, pair := range changeSet.Pairs {
		if pair.Delete {
			t.remove(pair.Key)
		} else {
			t.set(pair.Key, pair.Value)
		}
	}

	return t.saveVersion(updateHash)
}

func (t *Tree) set(key, value []byte) {
	if value == nil {
		// the value could be nil when replaying changes from write-ahead-log because of protobuf decoding
		value = []byte{}
	}
	t.root, _ = setRecursive(t.root, key, value, t.version+1, t.cowVersion)
	t.cache.Add(&cacheNode{key, value})
}

func (t *Tree) remove(key []byte) {
	_, t.root, _ = removeRecursive(t.root, key, t.version+1, t.cowVersion)
	t.cache.Remove(key)
}

// saveVersion increases the version number and optionally updates the hashes
func (t *Tree) saveVersion(updateHash bool) ([]byte, int64, error) {
	var hash []byte
	if updateHash {
		hash = t.RootHash()
	}

	if t.version >= uint32(math.MaxUint32) {
		return nil, 0, errors.New("version overflows uint32")
	}
	t.version++

	// to be compatible with existing golang iavl implementation.
	// see: https://github.com/cosmos/iavl/pull/660
	if t.version == 1 && t.initialVersion > 0 {
		t.version = t.initialVersion
	}

	return hash, int64(t.version), nil
}

// Version returns the current tree version
func (t *Tree) Version() int64 {
	return int64(t.version)
}

// RootHash updates the hashes and return the current root hash
func (t *Tree) RootHash() []byte {
	if t.root == nil {
		return emptyHash
	}
	return t.root.Hash()
}

func (t *Tree) GetWithIndex(key []byte) (int64, []byte) {
	if t.root == nil {
		return 0, nil
	}

	value, index := t.root.Get(key)
	if !t.zeroCopy {
		value = bytes.Clone(value)
	}
	return int64(index), value
}

func (t *Tree) GetByIndex(index int64) ([]byte, []byte) {
	if index > math.MaxUint32 {
		return nil, nil
	}
	if t.root == nil {
		return nil, nil
	}

	key, value := t.root.GetByIndex(uint32(index))
	if !t.zeroCopy {
		key = bytes.Clone(key)
		value = bytes.Clone(value)
	}
	return key, value
}

func (t *Tree) Get(key []byte) []byte {
	if node := t.cache.Get(key); node != nil {
		return node.(*cacheNode).value
	}

	_, value := t.GetWithIndex(key)
	if value == nil {
		return nil
	}

	t.cache.Add(&cacheNode{key, value})
	return value
}

func (t *Tree) Has(key []byte) bool {
	return t.Get(key) != nil
}

func (t *Tree) Iterator(start, end []byte, ascending bool) dbm.Iterator {
	return NewIterator(start, end, ascending, t.root, t.zeroCopy)
}

// ScanPostOrder scans the tree in post-order, and call the callback function on each node.
// If the callback function returns false, the scan will be stopped.
func (t *Tree) ScanPostOrder(callback func(node Node) bool) {
	if t.root == nil {
		return
	}

	stack := []*stackEntry{{node: t.root}}

	for len(stack) > 0 {
		entry := stack[len(stack)-1]

		if entry.node.IsLeaf() || entry.expanded {
			callback(entry.node)
			stack = stack[:len(stack)-1]
			continue
		}

		entry.expanded = true
		stack = append(stack, &stackEntry{node: entry.node.Right()})
		stack = append(stack, &stackEntry{node: entry.node.Left()})
	}
}

type stackEntry struct {
	node     Node
	expanded bool
}

// Export returns a snapshot of the tree which won't be corrupted by further modifications on the main tree.
func (t *Tree) Export() *Exporter {
	if t.snapshot != nil && t.version == t.snapshot.Version() {
		// snapshot export algorithm is more efficient
		return t.snapshot.Export()
	}

	// do normal post-order traversal export
	return newExporter(func(callback func(node *iavl.ExportNode) bool) {
		t.ScanPostOrder(func(node Node) bool {
			return callback(&iavl.ExportNode{
				Key:     node.Key(),
				Value:   node.Value(),
				Version: int64(node.Version()),
				Height:  int8(node.Height()),
			})
		})
	})
}

func (t *Tree) Close() error {
	var err error
	if t.snapshot != nil {
		err = t.snapshot.Close()
		t.snapshot = nil
	}
	t.root = nil
	return err
}
