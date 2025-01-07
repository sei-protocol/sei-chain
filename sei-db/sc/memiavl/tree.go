package memiavl

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sync"

	ics23 "github.com/confio/ics23/go"
	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/sc/types"
	dbm "github.com/tendermint/tm-db"
)

var _ types.Tree = (*Tree)(nil)
var emptyHash = sha256.New().Sum(nil)

// Tree verify change sets by replay them to rebuild iavl tree and verify the root hashes
type Tree struct {
	version uint32
	// root node of empty tree is represented as `nil`
	root     Node
	snapshot *Snapshot

	initialVersion, cowVersion uint32

	// when true, the get and iterator methods could return a slice pointing to mmaped blob files.
	zeroCopy bool

	// sync.RWMutex is used to protect the tree for thread safety during snapshot reload
	mtx *sync.RWMutex

	pendingChanges chan iavl.ChangeSet
	pendingWg      sync.WaitGroup
}

// NewEmptyTree creates an empty tree at an arbitrary version.
func NewEmptyTree(version uint64, initialVersion uint32) *Tree {
	if version >= math.MaxUint32 {
		panic("version overflows uint32")
	}

	return &Tree{
		version:        uint32(version),
		initialVersion: initialVersion,
		// no need to copy if the tree is not backed by snapshot
		zeroCopy:  true,
		mtx:       &sync.RWMutex{},
		pendingWg: sync.WaitGroup{},
	}
}

// New creates an empty tree at genesis version
func New(_ int) *Tree {
	return NewEmptyTree(0, 0)
}

// NewWithInitialVersion creates an empty tree with initial-version,
// it happens when a new store created at the middle of the chain.
func NewWithInitialVersion(initialVersion uint32) *Tree {
	return NewEmptyTree(0, initialVersion)
}

// NewFromSnapshot mmap the blob files and create the root node.
func NewFromSnapshot(snapshot *Snapshot, zeroCopy bool, _ int) *Tree {
	tree := &Tree{
		version:   snapshot.Version(),
		snapshot:  snapshot,
		zeroCopy:  zeroCopy,
		mtx:       &sync.RWMutex{},
		pendingWg: sync.WaitGroup{},
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
func (t *Tree) Copy(_ int) *Tree {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	if _, ok := t.root.(*MemNode); ok {
		// protect the existing `MemNode`s from get modified in-place
		t.cowVersion = t.version
	}
	newTree := *t
	newTree.mtx = &sync.RWMutex{}
	return &newTree
}

// ApplyChangeSet apply the change set of a whole version, and update hashes.
func (t *Tree) ApplyChangeSet(changeSet iavl.ChangeSet) {
	for _, pair := range changeSet.Pairs {
		if pair.Delete {
			t.Remove(pair.Key)
		} else {
			t.Set(pair.Key, pair.Value)
		}
	}
}

func (t *Tree) ApplyChangeSetAsync(changeSet iavl.ChangeSet) {
	t.pendingChanges <- changeSet
}

func (t *Tree) StartBackgroundWrite() {
	t.pendingWg.Add(1)
	t.pendingChanges = make(chan iavl.ChangeSet, 1000)
	go func() {
		defer t.pendingWg.Done()
		for nextChange := range t.pendingChanges {
			t.ApplyChangeSet(nextChange)
			t.SaveVersion(false)
		}
	}()
}

func (t *Tree) WaitToCompleteAsyncWrite() {
	close(t.pendingChanges)
	t.pendingWg.Wait()
	t.pendingChanges = nil
}

func (t *Tree) Set(key, value []byte) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if value == nil {
		// the value could be nil when replaying changes from write-ahead-log because of protobuf decoding
		value = []byte{}
	}
	t.root, _ = setRecursive(t.root, key, value, t.version+1, t.cowVersion)
}

func (t *Tree) Remove(key []byte) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	_, t.root, _ = removeRecursive(t.root, key, t.version+1, t.cowVersion)
}

// SaveVersion increases the version number and optionally updates the hashes
func (t *Tree) SaveVersion(updateHash bool) ([]byte, int64, error) {
	if t.version >= uint32(math.MaxUint32) {
		return nil, 0, errors.New("version overflows uint32")
	}

	var hash []byte
	if updateHash {
		hash = t.RootHash()
	}

	t.version = nextVersionU32(t.version, t.initialVersion)
	return hash, int64(t.version), nil
}

// Version returns the current tree version
func (t *Tree) Version() int64 {
	return int64(t.version)
}

// RootHash updates the hashes and return the current root hash,
// it clones the persisted node's bytes, so the returned bytes is safe to retain.
func (t *Tree) RootHash() []byte {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	if t.root == nil {
		return emptyHash
	}
	return t.root.SafeHash()
}

func (t *Tree) GetWithIndex(key []byte) (int64, []byte) {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	if t.root == nil {
		return 0, nil
	}

	value, index := t.root.Get(key)
	if !t.zeroCopy {
		value = utils.Clone(value)
	}
	return int64(index), value
}

func (t *Tree) GetByIndex(index int64) ([]byte, []byte) {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	if index > math.MaxUint32 {
		return nil, nil
	}
	if t.root == nil {
		return nil, nil
	}

	key, value := t.root.GetByIndex(uint32(index))
	if !t.zeroCopy {
		key = utils.Clone(key)
		value = utils.Clone(value)
	}
	return key, value
}

func (t *Tree) Get(key []byte) []byte {
	_, value := t.GetWithIndex(key)

	return value
}

func (t *Tree) Has(key []byte) bool {
	return t.Get(key) != nil
}

func (t *Tree) Iterator(start, end []byte, ascending bool) dbm.Iterator {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
	return NewIterator(start, end, ascending, t.root, t.zeroCopy)
}

// ScanPostOrder scans the tree in post-order, and call the callback function on each node.
// If the callback function returns false, the scan will be stopped.
func (t *Tree) ScanPostOrder(callback func(node Node) bool) {
	t.mtx.RLock()
	defer t.mtx.RUnlock()
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
	return newExporter(func(callback func(node *types.SnapshotNode) bool) {
		t.ScanPostOrder(func(node Node) bool {
			return callback(&types.SnapshotNode{
				Key:     node.Key(),
				Value:   node.Value(),
				Version: int64(node.Version()),
				Height:  int8(node.Height()),
			})
		})
	})
}

func (t *Tree) Close() error {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	var err error
	if t.snapshot != nil {
		err = t.snapshot.Close()
		t.snapshot = nil
	}
	if t.pendingChanges != nil {
		close(t.pendingChanges)
	}
	t.root = nil
	return err
}

// ReplaceWith is used during reload to replace the current tree with the newly loaded snapshot
func (t *Tree) ReplaceWith(other *Tree) error {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	snapshot := t.snapshot
	t.version = other.version
	t.root = other.root
	t.snapshot = other.snapshot
	t.initialVersion = other.initialVersion
	t.cowVersion = other.cowVersion
	t.zeroCopy = other.zeroCopy
	if snapshot != nil {
		return snapshot.Close()
	}
	return nil
}

// nextVersionU32 is compatible with existing golang iavl implementation.
// see: https://github.com/cosmos/iavl/pull/660
func nextVersionU32(v uint32, initialVersion uint32) uint32 {
	if v == 0 && initialVersion > 1 {
		return initialVersion
	}
	return v + 1
}

// GetProof takes a key for creating existence or absence proof and returns the
// appropriate merkle.Proof. Since this must be called after querying for the value, this function should never error
// Thus, it will panic on error rather than returning it
func (t *Tree) GetProof(key []byte) *ics23.CommitmentProof {
	var (
		commitmentProof *ics23.CommitmentProof
		err             error
	)
	exists := t.Has(key)

	if exists {
		// value was found
		commitmentProof, err = t.GetMembershipProof(key)
		if err != nil {
			// sanity check: If value was found, membership proof must be creatable
			panic(fmt.Sprintf("unexpected value for empty proof: %s", err.Error()))
		}
	} else {
		// value wasn't found
		commitmentProof, err = t.GetNonMembershipProof(key)
		if err != nil {
			// sanity check: If value wasn't found, nonmembership proof must be creatable
			panic(fmt.Sprintf("unexpected error for nonexistence proof: %s", err.Error()))
		}
	}

	return commitmentProof

}
