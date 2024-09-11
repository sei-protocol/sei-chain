package iavl

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"

	"github.com/pkg/errors"
	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/iavl/internal/logger"
)

// commitGap after upgrade/delete commitGap FastNodes when commit the batch
var commitGap uint64 = 5000000

// ErrVersionDoesNotExist is returned if a requested version does not exist.
var ErrVersionDoesNotExist = errors.New("version does not exist")

// MutableTree is a persistent tree which keeps track of versions. It is not safe for concurrent
// use, and should be guarded by a Mutex or RWLock as appropriate. An immutable tree at a given
// version can be returned via GetImmutable, which is safe for concurrent access.
//
// Given and returned key/value byte slices must not be modified, since they may point to data
// located inside IAVL which would also be modified.
//
// The inner ImmutableTree should not be used directly by callers.
type MutableTree struct {
	ITree                        *ImmutableTree // The current, working tree.
	Mtx                          *sync.RWMutex
	lastSaved                    *ImmutableTree         // The most recently saved tree.
	orphans                      map[string]int64       // Nodes removed by changes to working tree.
	unsavedFastNodeAdditions     map[string]*FastNode   // FastNodes that have not yet been saved to disk
	unsavedFastNodeRemovals      map[string]interface{} // FastNodes that have not yet been removed from disk
	ndb                          *nodeDB
	skipFastStorageUpgrade       bool // If true, the tree will work like no fast storage and always not upgrade fast storage
	separateOrphanStorage        bool
	separateOrphanVersionsToKeep int64
	orphandb                     *orphanDB
}

// NewMutableTree returns a new tree with the specified cache size and datastore.
func NewMutableTree(db dbm.DB, cacheSize int, skipFastStorageUpgrade bool) (*MutableTree, error) {
	return NewMutableTreeWithOpts(db, cacheSize, nil, skipFastStorageUpgrade)
}

// NewMutableTreeWithOpts returns a new tree with the specified options.
func NewMutableTreeWithOpts(db dbm.DB, cacheSize int, opts *Options, skipFastStorageUpgrade bool) (*MutableTree, error) {
	ndb := newNodeDB(db, cacheSize, opts)
	head := &ImmutableTree{ndb: ndb, skipFastStorageUpgrade: skipFastStorageUpgrade}
	if opts == nil {
		defaultOpts := DefaultOptions()
		opts = &defaultOpts
	}
	var orphandb *orphanDB
	if opts.SeparateOrphanStorage {
		orphandb = NewOrphanDB(opts)
	}

	return &MutableTree{
		ITree:                        head,
		lastSaved:                    head.clone(),
		orphans:                      map[string]int64{},
		unsavedFastNodeAdditions:     make(map[string]*FastNode),
		unsavedFastNodeRemovals:      make(map[string]interface{}),
		ndb:                          ndb,
		skipFastStorageUpgrade:       skipFastStorageUpgrade,
		separateOrphanStorage:        opts.SeparateOrphanStorage,
		separateOrphanVersionsToKeep: opts.SeparateOphanVersionsToKeep,
		orphandb:                     orphandb,
		Mtx:                          &sync.RWMutex{},
	}, nil
}

func (tree *MutableTree) ImmutableTree() *ImmutableTree {
	tree.Mtx.RLock()
	defer tree.Mtx.RUnlock()
	return tree.ITree
}

func (tree *MutableTree) LastSaved() *ImmutableTree {
	tree.Mtx.RLock()
	defer tree.Mtx.RUnlock()
	return tree.lastSaved
}

func (tree *MutableTree) Has(key []byte) (bool, error) {
	return tree.ImmutableTree().Has(key)
}

// IsEmpty returns whether or not the tree has any keys. Only trees that are
// not empty can be saved.
func (tree *MutableTree) IsEmpty() bool {
	return tree.ImmutableTree().Size() == 0
}

// VersionExists returns whether or not a version exists.
func (tree *MutableTree) VersionExists(version int64) bool {
	tree.Mtx.Lock()
	defer tree.Mtx.Unlock()
	latestVersion, err := tree.ndb.getLatestVersion()
	if err != nil {
		return false
	}
	if version <= latestVersion {
		has, err := tree.ndb.hasVersion(version)
		return err == nil && has
	}
	return false
}

// AvailableVersions returns all available versions in ascending order
func (tree *MutableTree) AvailableVersions() []int {
	tree.Mtx.Lock()
	defer tree.Mtx.Unlock()
	firstVersion, err := tree.ndb.getFirstVersion()
	if err != nil {
		return nil
	}
	latestVersion, err := tree.ndb.getLatestVersion()
	if err != nil {
		return nil
	}
	res := make([]int, 0)
	for version := firstVersion; version <= latestVersion; version++ {
		res = append(res, int(version))
	}
	return res
}

// Hash returns the hash of the latest saved version of the tree, as returned
// by SaveVersion. If no versions have been saved, Hash returns nil.
func (tree *MutableTree) Hash() ([]byte, error) {
	return tree.LastSaved().Hash()
}

// WorkingHash returns the hash of the current working tree.
func (tree *MutableTree) WorkingHash() ([]byte, error) {
	return tree.ImmutableTree().Hash()
}

// String returns a string representation of the tree.
func (tree *MutableTree) String() (string, error) {
	return tree.ndb.String()
}

// Set/Remove will orphan at most tree.Height nodes,
// balancing the tree after a Set/Remove will orphan at most 3 nodes.
func (tree *MutableTree) prepareOrphansSlice() []*Node {
	return make([]*Node, 0, tree.ITree.Height()+3)
}

// Set sets a key in the working tree. Nil values are invalid. The given
// key/value byte slices must not be modified after this call, since they point
// to slices stored within IAVL. It returns true when an existing value was
// updated, while false means it was a new key.
func (tree *MutableTree) Set(key, value []byte) (updated bool, err error) {
	tree.Mtx.Lock()
	defer tree.Mtx.Unlock()
	var orphaned []*Node
	orphaned, updated, err = tree.set(key, value)
	if err != nil {
		return false, err
	}
	err = tree.addOrphans(orphaned)
	if err != nil {
		return updated, err
	}
	return updated, nil
}

// Get returns the value of the specified key if it exists, or nil otherwise.
// The returned value must not be modified, since it may point to data stored within IAVL.
func (tree *MutableTree) Get(key []byte) ([]byte, error) {
	if tree.ImmutableTree().root == nil {
		return nil, nil
	}

	tree.Mtx.RLock()
	defer tree.Mtx.RUnlock()
	if !tree.skipFastStorageUpgrade {
		if fastNode, ok := tree.unsavedFastNodeAdditions[unsafeToStr(key)]; ok {
			return fastNode.value, nil
		}
		// check if node was deleted
		if _, ok := tree.unsavedFastNodeRemovals[string(key)]; ok {
			return nil, nil
		}
	}

	// Mtx is already acquired
	return tree.ITree.Get(key)
}

// Import returns an importer for tree nodes previously exported by ImmutableTree.Export(),
// producing an identical IAVL tree. The caller must call Close() on the importer when done.
//
// version should correspond to the version that was initially exported. It must be greater than
// or equal to the highest ExportNode version number given.
//
// Import can only be called on an empty tree. It is the callers responsibility that no other
// modifications are made to the tree while importing.
func (tree *MutableTree) Import(version int64) (*Importer, error) {
	return newImporter(tree, version)
}

// Iterate iterates over all keys of the tree. The keys and values must not be modified,
// since they may point to data stored within IAVL. Returns true if stopped by callnack, false otherwise
func (tree *MutableTree) Iterate(fn func(key []byte, value []byte) bool) (stopped bool, err error) {
	if tree.ImmutableTree().root == nil {
		return false, nil
	}

	if tree.skipFastStorageUpgrade {
		return tree.ImmutableTree().Iterate(fn)
	}

	isFastCacheEnabled, err := tree.ImmutableTree().IsFastCacheEnabled()
	if err != nil {
		return false, err
	}
	if !isFastCacheEnabled {
		return tree.ImmutableTree().Iterate(fn)
	}

	itr := NewUnsavedFastIterator(nil, nil, true, tree.ndb, tree.unsavedFastNodeAdditions, tree.unsavedFastNodeRemovals)
	defer itr.Close()
	for ; itr.Valid(); itr.Next() {
		if fn(itr.Key(), itr.Value()) {
			return true, nil
		}
	}
	return false, nil
}

// Iterator returns an iterator over the mutable tree.
// CONTRACT: no updates are made to the tree while an iterator is active.
func (tree *MutableTree) Iterator(start, end []byte, ascending bool) (dbm.Iterator, error) {
	if !tree.skipFastStorageUpgrade {
		isFastCacheEnabled, err := tree.ImmutableTree().IsFastCacheEnabled()
		if err != nil {
			return nil, err
		}

		if isFastCacheEnabled {
			return NewUnsavedFastIterator(start, end, ascending, tree.ndb, tree.unsavedFastNodeAdditions, tree.unsavedFastNodeRemovals), nil
		}
	}

	return tree.ImmutableTree().Iterator(start, end, ascending)
}

// no need to acquire Mtx since it's only called by `Set` which already holds the Mtx
func (tree *MutableTree) set(key []byte, value []byte) (orphans []*Node, updated bool, err error) {
	if value == nil {
		return nil, updated, fmt.Errorf("attempt to store nil value at key '%s'", key)
	}

	if tree.ITree.root == nil {
		if !tree.skipFastStorageUpgrade {
			tree.addUnsavedAddition(key, NewFastNode(key, value, tree.ITree.version+1))
		}
		tree.ITree.root = NewNode(key, value, tree.ITree.version+1)
		return nil, updated, nil
	}

	orphans = tree.prepareOrphansSlice()
	tree.ITree.root, updated, err = tree.recursiveSet(tree.ITree.root, key, value, &orphans)
	return orphans, updated, err
}

// no need to acquire Mtx since it's only called by `set` which already holds the Mtx
func (tree *MutableTree) recursiveSet(node *Node, key []byte, value []byte, orphans *[]*Node) (
	newSelf *Node, updated bool, err error,
) {
	version := tree.ITree.version + 1

	if node.isLeaf() {
		if !tree.skipFastStorageUpgrade {
			tree.addUnsavedAddition(key, NewFastNode(key, value, version))
		}

		switch bytes.Compare(key, node.GetNodeKey()) {
		case -1:
			return &Node{
				key:       node.GetNodeKey(),
				height:    1,
				size:      2,
				leftNode:  NewNode(key, value, version),
				rightNode: node,
				version:   version,
			}, false, nil
		case 1:
			return &Node{
				key:       key,
				height:    1,
				size:      2,
				leftNode:  node,
				rightNode: NewNode(key, value, version),
				version:   version,
			}, false, nil
		default:
			*orphans = append(*orphans, node)
			return NewNode(key, value, version), true, nil
		}
	} else {
		*orphans = append(*orphans, node)
		node, err = node.clone(version)
		if err != nil {
			return nil, false, err
		}

		if bytes.Compare(key, node.GetNodeKey()) < 0 {
			leftNode, err := node.getLeftNode(tree.ITree)
			if err != nil {
				return nil, false, err
			}
			lNode, u, e := tree.recursiveSet(leftNode, key, value, orphans)
			if e != nil {
				return nil, u, e
			}
			updated = u
			node.SetLeftNode(lNode)
			node.SetLeftHash(nil) // leftHash is yet unknown
		} else {
			rightNode, err := node.getRightNode(tree.ITree)
			if err != nil {
				return nil, false, err
			}
			rNode, u, e := tree.recursiveSet(rightNode, key, value, orphans)
			if e != nil {
				return nil, u, e
			}
			updated = u
			node.SetRightNode(rNode)
			node.SetRightHash(nil) // rightHash is yet unknown
		}

		if updated {
			return node, updated, nil
		}
		err = node.calcHeightAndSize(tree.ITree)
		if err != nil {
			return nil, false, err
		}

		newNode, err := tree.balance(node, orphans)
		if err != nil {
			return nil, false, err
		}
		return newNode, updated, err
	}
}

// Remove removes a key from the working tree. The given key byte slice should not be modified
// after this call, since it may point to data stored inside IAVL.
func (tree *MutableTree) Remove(key []byte) ([]byte, bool, error) {
	tree.Mtx.Lock()
	defer tree.Mtx.Unlock()
	val, orphaned, removed, err := tree.remove(key)
	if err != nil {
		return nil, false, err
	}

	err = tree.addOrphans(orphaned)
	if err != nil {
		return val, removed, err
	}
	return val, removed, nil
}

// remove tries to remove a key from the tree and if removed, returns its
// value, nodes orphaned and 'true'.
// no need to acquire Mtx since it's only called by `Remove` which already holds the Mtx.
func (tree *MutableTree) remove(key []byte) (value []byte, orphaned []*Node, removed bool, err error) {
	if tree.ITree.root == nil {
		return nil, nil, false, nil
	}
	orphaned = tree.prepareOrphansSlice()
	newRootHash, newRoot, _, value, err := tree.recursiveRemove(tree.ITree.root, key, &orphaned)
	if err != nil {
		return nil, nil, false, err
	}
	if len(orphaned) == 0 {
		return nil, nil, false, nil
	}

	if !tree.skipFastStorageUpgrade {
		tree.addUnsavedRemoval(key)
	}

	if newRoot == nil && newRootHash != nil {
		tree.ITree.root, err = tree.ndb.GetNode(newRootHash)
		if err != nil {
			return nil, nil, false, err
		}
	} else {
		tree.ITree.root = newRoot
	}
	return value, orphaned, true, nil
}

// removes the node corresponding to the passed key and balances the tree.
// It returns:
// - the hash of the new node (or nil if the node is the one removed)
// - the node that replaces the orig. node after remove
// - new leftmost leaf key for tree after successfully removing 'key' if changed.
// - the removed value
// - the orphaned nodes.
// no need to acquire Mtx since it's only called by `remove` which already holds the Mtx
func (tree *MutableTree) recursiveRemove(node *Node, key []byte, orphans *[]*Node) (newHash []byte, newSelf *Node, newKey []byte, newValue []byte, err error) {
	version := tree.ITree.version + 1

	if node.isLeaf() {
		if bytes.Equal(key, node.GetNodeKey()) {
			*orphans = append(*orphans, node)
			return nil, nil, nil, node.GetValue(), nil
		}
		return node.GetHash(), node, nil, nil, nil
	}

	// node.key < key; we go to the left to find the key:
	if bytes.Compare(key, node.GetNodeKey()) < 0 {
		leftNode, err := node.getLeftNode(tree.ITree)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		newLeftHash, newLeftNode, newKey, value, err := tree.recursiveRemove(leftNode, key, orphans)
		if err != nil {
			return nil, nil, nil, nil, err
		}

		if len(*orphans) == 0 {
			return node.GetHash(), node, nil, value, nil
		}
		*orphans = append(*orphans, node)
		if newLeftHash == nil && newLeftNode == nil { // left node held value, was removed
			return node.GetRightHash(), node.GetRightNode(), node.GetNodeKey(), value, nil
		}

		newNode, err := node.clone(version)
		if err != nil {
			return nil, nil, nil, nil, err
		}

		newNode.SetLeftHash(newLeftHash)
		newNode.SetLeftNode(newLeftNode)
		err = newNode.calcHeightAndSize(tree.ITree)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		newNode, err = tree.balance(newNode, orphans)
		if err != nil {
			return nil, nil, nil, nil, err
		}

		return newNode.GetHash(), newNode, newKey, value, nil
	}
	// node.key >= key; either found or look to the right:
	rightNode, err := node.getRightNode(tree.ITree)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	newRightHash, newRightNode, newKey, value, err := tree.recursiveRemove(rightNode, key, orphans)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if len(*orphans) == 0 {
		return node.GetHash(), node, nil, value, nil
	}
	*orphans = append(*orphans, node)
	if newRightHash == nil && newRightNode == nil { // right node held value, was removed
		return node.GetLeftHash(), node.GetLeftNode(), nil, value, nil
	}

	newNode, err := node.clone(version)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	newNode.SetRightHash(newRightHash)
	newNode.SetRightNode(newRightNode)
	if newKey != nil {
		newNode.SetKey(newKey)
	}
	err = newNode.calcHeightAndSize(tree.ITree)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	newNode, err = tree.balance(newNode, orphans)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	return newNode.GetHash(), newNode, nil, value, nil
}

// Load the latest versioned tree from disk.
func (tree *MutableTree) Load() (int64, error) {
	return tree.LoadVersion(int64(0))
}

// LazyLoadVersion attempts to lazy load only the specified target version
// without loading previous roots/versions. Lazy loading should be used in cases
// where only reads are expected. Any writes to a lazy loaded tree may result in
// unexpected behavior. If the targetVersion is non-positive, the latest version
// will be loaded by default. If the latest version is non-positive, this method
// performs a no-op. Otherwise, if the root does not exist, an error will be
// returned.
func (tree *MutableTree) LazyLoadVersion(targetVersion int64) (toReturn int64, toErr error) {
	latestVersion, err := tree.ndb.getLatestVersion()
	if err != nil {
		return 0, err
	}
	if latestVersion < targetVersion {
		return latestVersion, fmt.Errorf("wanted to load target %d but only found up to %d", targetVersion, latestVersion)
	}

	// no versions have been saved if the latest version is non-positive
	if latestVersion <= 0 {
		if targetVersion <= 0 {
			if !tree.skipFastStorageUpgrade {
				_, err := tree.enableFastStorageAndCommitIfNotEnabled()
				return 0, err
			}
			return 0, nil
		}
		return 0, fmt.Errorf("no versions found while trying to load %v", targetVersion)
	}

	// default to the latest version if the targeted version is non-positive
	if targetVersion <= 0 {
		targetVersion = latestVersion
	}

	rootHash, err := tree.ndb.getRoot(targetVersion)
	if err != nil {
		return 0, err
	}
	if rootHash == nil {
		return latestVersion, ErrVersionDoesNotExist
	}

	tree.Mtx.Lock()
	defer func() {
		tree.Mtx.Unlock()
		if !tree.skipFastStorageUpgrade {
			// Attempt to upgrade
			if _, err := tree.enableFastStorageAndCommitIfNotEnabled(); err != nil {
				toReturn = 0
				toErr = err
			}
		}
	}()

	iTree := &ImmutableTree{
		ndb:                    tree.ndb,
		version:                targetVersion,
		skipFastStorageUpgrade: tree.skipFastStorageUpgrade,
	}
	if len(rootHash) > 0 {
		// If rootHash is empty then root of tree should be nil
		// This makes `LazyLoadVersion` to do the same thing as `LoadVersion`
		iTree.root, err = tree.ndb.GetNode(rootHash)
		if err != nil {
			return 0, err
		}
	}

	tree.orphans = map[string]int64{}
	tree.ITree = iTree // Mtx is already held
	tree.lastSaved = iTree.clone()

	return targetVersion, nil
}

// Returns the version number of the latest version found
func (tree *MutableTree) LoadVersion(targetVersion int64) (toReturn int64, toErr error) {
	firstVersion, err := tree.ndb.getFirstVersion()
	if err != nil {
		return 0, err
	}
	if firstVersion > 0 && firstVersion < int64(tree.ndb.opts.InitialVersion) {
		return firstVersion, fmt.Errorf("initial version set to %v, but found earlier version %v",
			tree.ndb.opts.InitialVersion, firstVersion)
	}

	latestVersion, err := tree.ndb.getLatestVersion()
	if err != nil {
		return 0, err
	}

	if latestVersion < targetVersion {
		return latestVersion, fmt.Errorf("wanted to load target %d but only found up to %d", targetVersion, latestVersion)
	}

	if firstVersion == 0 {
		if targetVersion <= 0 {
			if !tree.skipFastStorageUpgrade {
				tree.Mtx.Lock()
				defer tree.Mtx.Unlock()
				_, err := tree.enableFastStorageAndCommitIfNotEnabled()
				return 0, err
			}
			return 0, nil
		}
		return 0, fmt.Errorf("no versions found while trying to load %v", targetVersion)
	}

	if targetVersion <= 0 {
		targetVersion = latestVersion
	}

	fmt.Printf("[Debug] This tree has first version %d and latest version %d\n", firstVersion, latestVersion)
	if !tree.VersionExists(targetVersion) {
		return 0, ErrVersionDoesNotExist
	}

	rootNodeKey, err := tree.ndb.getRoot(targetVersion)
	if err != nil {
		return 0, err
	}

	t := &ImmutableTree{
		ndb:                    tree.ndb,
		version:                latestVersion,
		skipFastStorageUpgrade: tree.skipFastStorageUpgrade,
	}

	if rootNodeKey != nil {
		t.root, err = tree.ndb.GetNode(rootNodeKey)
		if err != nil {
			return tree.LegacyLoadVersion(targetVersion)
		}
	}

	tree.orphans = map[string]int64{}
	tree.ITree = t // Mtx is already held
	tree.lastSaved = t.clone()

	return latestVersion, nil
}

// Returns the version number of the latest version found
func (tree *MutableTree) LegacyLoadVersion(targetVersion int64) (toReturn int64, toErr error) {
	roots, err := tree.ndb.getRoots()
	fmt.Printf("[Debug] Found %d roots]\n", len(roots))
	if err != nil {
		return 0, err
	}

	if len(roots) == 0 {
		if targetVersion <= 0 {
			if !tree.skipFastStorageUpgrade {
				_, err := tree.enableFastStorageAndCommitIfNotEnabled()
				return 0, err
			}
			return 0, nil
		}
		return 0, fmt.Errorf("no versions found while trying to load %v", targetVersion)
	}

	firstVersion := int64(0)
	latestVersion := int64(0)

	var latestRoot []byte
	for version, r := range roots {
		if version > latestVersion && (targetVersion == 0 || version <= targetVersion) {
			latestVersion = version
			latestRoot = r
		}
		if firstVersion == 0 || version < firstVersion {
			firstVersion = version
		}
	}

	if !(targetVersion == 0 || latestVersion == targetVersion) {
		return latestVersion, fmt.Errorf("wanted to load target %v but only found up to %v",
			targetVersion, latestVersion)
	}

	if firstVersion > 0 && firstVersion < int64(tree.ndb.opts.InitialVersion) {
		return latestVersion, fmt.Errorf("initial version set to %v, but found earlier version %v",
			tree.ndb.opts.InitialVersion, firstVersion)
	}

	t := &ImmutableTree{
		ndb:                    tree.ndb,
		version:                latestVersion,
		skipFastStorageUpgrade: tree.skipFastStorageUpgrade,
	}

	if len(latestRoot) != 0 {
		t.root, err = tree.ndb.GetNode(latestRoot)
		if err != nil {
			return 0, err
		}
	}

	tree.orphans = map[string]int64{}
	tree.ITree = t // Mtx is already held
	tree.lastSaved = t.clone()
	return latestVersion, nil
}

// LoadVersionForOverwriting attempts to load a tree at a previously committed
// version, or the latest version below it. Any versions greater than targetVersion will be deleted.
func (tree *MutableTree) LoadVersionForOverwriting(targetVersion int64) (int64, error) {
	latestVersion, err := tree.LoadVersion(targetVersion)
	if err != nil {
		return latestVersion, err
	}

	if err = tree.ndb.DeleteVersionsFrom(targetVersion + 1); err != nil {
		return latestVersion, err
	}

	if !tree.skipFastStorageUpgrade {
		if err := tree.enableFastStorageAndCommit(); err != nil {
			return latestVersion, err
		}
	}

	tree.ndb.resetLatestVersion(latestVersion)

	tree.Mtx.Lock()
	defer tree.Mtx.Unlock()

	return latestVersion, nil
}

// Returns true if the tree may be auto-upgraded, false otherwise
// An example of when an upgrade may be performed is when we are enaling fast storage for the first time or
// need to overwrite fast nodes due to mismatch with live state.
func (tree *MutableTree) IsUpgradeable() (bool, error) {
	shouldForce, err := tree.ndb.shouldForceFastStorageUpgrade()
	if err != nil {
		return false, err
	}
	return !tree.skipFastStorageUpgrade && (!tree.ndb.hasUpgradedToFastStorage() || shouldForce), nil
}

// enableFastStorageAndCommitIfNotEnabled if nodeDB doesn't mark fast storage as enabled, enable it, and commit the update.
// Checks whether the fast cache on disk matches latest live state. If not, deletes all existing fast nodes and repopulates them
// from latest tree.
// nolint: unparam
func (tree *MutableTree) enableFastStorageAndCommitIfNotEnabled() (bool, error) {
	isUpgradeable, err := tree.IsUpgradeable()
	if err != nil {
		return false, err
	}

	if !isUpgradeable {
		return false, nil
	}

	// If there is a mismatch between which fast nodes are on disk and the live state due to temporary
	// downgrade and subsequent re-upgrade, we cannot know for sure which fast nodes have been removed while downgraded,
	// Therefore, there might exist stale fast nodes on disk. As a result, to avoid persisting the stale state, it might
	// be worth to delete the fast nodes from disk.
	fastItr := NewFastIterator(nil, nil, true, tree.ndb)
	defer fastItr.Close()
	var deletedFastNodes uint64
	for ; fastItr.Valid(); fastItr.Next() {
		deletedFastNodes++
		if err := tree.ndb.DeleteFastNode(fastItr.Key()); err != nil {
			return false, err
		}
		if deletedFastNodes%commitGap == 0 {
			if err := tree.ndb.Commit(); err != nil {
				return false, err
			}
		}
	}
	if deletedFastNodes%commitGap != 0 {
		if err := tree.ndb.Commit(); err != nil {
			return false, err
		}
	}

	if err := tree.enableFastStorageAndCommit(); err != nil {
		tree.ndb.storageVersion = defaultStorageVersionValue
		return false, err
	}
	return true, nil
}

func (tree *MutableTree) enableFastStorageAndCommit() error {
	var err error

	itr := NewIteratorUnlocked(nil, nil, true, tree.ImmutableTree())
	defer itr.Close()
	version := tree.ImmutableTree().version
	var upgradedFastNodes uint64
	for ; itr.Valid(); itr.Next() {
		upgradedFastNodes++
		if err = tree.ndb.SaveFastNodeNoCache(NewFastNode(itr.Key(), itr.Value(), version)); err != nil {
			return err
		}
		if upgradedFastNodes%commitGap == 0 {
			tree.ndb.Commit()
		}
	}

	if err = itr.Error(); err != nil {
		return err
	}

	if err = tree.ndb.setFastStorageVersionToBatch(); err != nil {
		return err
	}

	return tree.ndb.Commit()
}

// GetImmutable loads an ImmutableTree at a given version for querying. The returned tree is
// safe for concurrent access, provided the version is not deleted, e.g. via `DeleteVersion()`.
func (tree *MutableTree) GetImmutable(version int64) (*ImmutableTree, error) {
	rootHash, err := tree.ndb.getRoot(version)
	if err != nil {
		return nil, err
	}
	if rootHash == nil {
		return nil, ErrVersionDoesNotExist
	}

	tree.Mtx.Lock()
	defer tree.Mtx.Unlock()

	if len(rootHash) == 0 {
		return &ImmutableTree{
			ndb:                    tree.ndb,
			version:                version,
			skipFastStorageUpgrade: tree.skipFastStorageUpgrade,
		}, nil
	}

	root, err := tree.ndb.GetNode(rootHash)
	if err != nil {
		return nil, err
	}
	return &ImmutableTree{
		root:                   root,
		ndb:                    tree.ndb,
		version:                version,
		skipFastStorageUpgrade: tree.skipFastStorageUpgrade,
	}, nil
}

// Rollback resets the working tree to the latest saved version, discarding
// any unsaved modifications.
func (tree *MutableTree) Rollback() {
	tree.Mtx.Lock()
	defer tree.Mtx.Unlock()
	if tree.ITree.version > 0 {
		tree.ITree = tree.lastSaved.clone()
	} else {
		tree.ITree = &ImmutableTree{
			ndb:                    tree.ndb,
			version:                0,
			skipFastStorageUpgrade: tree.skipFastStorageUpgrade,
		}
	}
	tree.orphans = map[string]int64{}
	if !tree.skipFastStorageUpgrade {
		tree.unsavedFastNodeAdditions = map[string]*FastNode{}
		tree.unsavedFastNodeRemovals = map[string]interface{}{}
	}
}

// GetVersioned gets the value at the specified key and version. The returned value must not be
// modified, since it may point to data stored within IAVL.
func (tree *MutableTree) GetVersioned(key []byte, version int64) ([]byte, error) {
	if tree.VersionExists(version) {
		if !tree.skipFastStorageUpgrade {
			isFastCacheEnabled, err := tree.ImmutableTree().IsFastCacheEnabled()
			if err != nil {
				return nil, err
			}

			if isFastCacheEnabled {
				fastNode, _ := tree.ndb.GetFastNode(key)
				if fastNode == nil && version == tree.ndb.latestVersion {
					return nil, nil
				}

				if fastNode != nil && fastNode.versionLastUpdatedAt <= version {
					return fastNode.value, nil
				}
			}
		}
		t, err := tree.GetImmutable(version)
		if err != nil {
			return nil, nil
		}
		value, err := t.Get(key)
		if err != nil {
			return nil, err
		}
		return value, nil
	}
	return nil, nil
}

// SaveCurrentVersion overwrites the current version without bumping.
// It will return an error if the version does not exist in tree, or if
// the hash being saved is different. In
// other words, only SaveVersion can insert new node into the tree.
func (tree *MutableTree) SaveCurrentVersion() ([]byte, int64, error) {
	version := tree.ImmutableTree().version
	if version == 1 && tree.ndb.opts.InitialVersion > 0 {
		version = int64(tree.ndb.opts.InitialVersion)
	}

	if !tree.VersionExists(version) {
		return nil, version, errors.New(fmt.Sprintf("attempting to overwrite non-existent version %d", version))
	}

	existingHash, err := tree.ndb.getRoot(version)
	if err != nil {
		return nil, version, err
	}

	// If the existing root hash is empty (because the tree is empty), then we need to
	// compare with the hash of an empty input which is what `WorkingHash()` returns.
	if len(existingHash) == 0 {
		existingHash = sha256.New().Sum(nil)
	}

	newHash, err := tree.WorkingHash()
	if err != nil {
		return nil, version, err
	}

	if bytes.Equal(existingHash, newHash) {
		tree.Mtx.Lock()
		defer tree.Mtx.Unlock()
		if v, err := tree.commitVersion(version, true); err != nil {
			return nil, v, err
		}
		clone := tree.ITree.clone()
		clone.version = version
		tree.ITree = clone
		tree.lastSaved = clone.clone()
		tree.orphans = map[string]int64{}
		return existingHash, version, nil
	}

	return nil, version, fmt.Errorf("version %d was already saved to different hash %X (existing hash %X)", version, newHash, existingHash)
}

// SaveVersion saves a new tree version to disk, based on the current state of
// the tree. Returns the hash and new version number.
func (tree *MutableTree) SaveVersion() ([]byte, int64, error) {
	version := tree.ImmutableTree().version + 1
	if version == 1 && tree.ndb.opts.InitialVersion > 0 {
		version = int64(tree.ndb.opts.InitialVersion)
	}

	if tree.VersionExists(version) {
		// If the version already exists, return an error as we're attempting to overwrite.
		// However, the same hash means idempotent (i.e. no-op).
		existingHash, err := tree.ndb.getRoot(version)
		if err != nil {
			return nil, version, err
		}

		// If the existing root hash is empty (because the tree is empty), then we need to
		// compare with the hash of an empty input which is what `WorkingHash()` returns.
		if len(existingHash) == 0 {
			existingHash = sha256.New().Sum(nil)
		}

		newHash, err := tree.WorkingHash()
		if err != nil {
			return nil, version, err
		}

		if bytes.Equal(existingHash, newHash) {
			tree.Mtx.Lock()
			defer tree.Mtx.Unlock()
			clone := tree.ITree.clone()
			clone.version = version
			tree.ITree = clone
			tree.lastSaved = clone.clone()
			tree.orphans = map[string]int64{}
			return existingHash, version, nil
		}

		return nil, version, fmt.Errorf("version %d was already saved to different hash %X (existing hash %X)", version, newHash, existingHash)
	}

	tree.Mtx.Lock()
	defer tree.Mtx.Unlock()

	if v, err := tree.commitVersion(version, false); err != nil {
		return nil, v, err
	}

	// Mtx is already held at this point
	clone := tree.ITree.clone()
	clone.version = version
	tree.ndb.resetLatestVersion(version)

	// set new working tree
	tree.ITree = clone
	tree.lastSaved = clone.clone()
	tree.orphans = map[string]int64{}
	if !tree.skipFastStorageUpgrade {
		tree.unsavedFastNodeAdditions = make(map[string]*FastNode)
		tree.unsavedFastNodeRemovals = make(map[string]interface{})
	}

	hash, err := tree.lastSaved.Hash()
	if err != nil {
		return nil, version, err
	}

	return hash, version, nil
}

func (tree *MutableTree) saveFastNodeVersion() error {
	if err := tree.saveFastNodeAdditions(); err != nil {
		return err
	}
	if err := tree.saveFastNodeRemovals(); err != nil {
		return err
	}
	return tree.ndb.setFastStorageVersionToBatch()
}

func (tree *MutableTree) handleOrphans(version int64) error {
	if !tree.separateOrphanStorage {
		// store orphan in the same levelDB as application data
		return tree.ndb.SaveOrphans(version, tree.orphans)
	}

	if tree.separateOrphanVersionsToKeep == 0 {
		panic("must keep at least one version")
	}

	// optimization for the 1 version case so that we don't have to save and immediately delete the same version
	if tree.separateOrphanVersionsToKeep == 1 {
		for orphan := range tree.orphans {
			if err := tree.ndb.deleteOrphanedData([]byte(orphan)); err != nil {
				return err
			}
		}
		return nil
	}

	if err := tree.orphandb.SaveOrphans(version, tree.orphans); err != nil {
		return err
	}
	oldOrphans := tree.orphandb.GetOrphans(version - tree.separateOrphanVersionsToKeep + 1)
	for orphan := range oldOrphans {
		if err := tree.ndb.deleteOrphanedData([]byte(orphan)); err != nil {
			return err
		}
	}
	return tree.orphandb.DeleteOrphans(version - tree.separateOrphanVersionsToKeep + 1)
}

func (tree *MutableTree) commitVersion(version int64, silentSaveRootError bool) (int64, error) {
	if tree.ITree.root == nil {
		// There can still be orphans, for example if the root is the node being
		// removed.
		logger.Debug("SAVE EMPTY TREE %v\n", version)
		if err := tree.handleOrphans(version); err != nil {
			return 0, err
		}
		if err := tree.ndb.SaveEmptyRoot(version); !silentSaveRootError && err != nil {
			return 0, err
		}
	} else {
		logger.Debug("SAVE TREE %v\n", version)
		if _, err := tree.ndb.SaveBranch(tree.ITree.root); err != nil {
			return 0, err
		}
		if err := tree.handleOrphans(version); err != nil {
			return 0, err
		}
		if err := tree.ndb.SaveRoot(tree.ITree.root, version); !silentSaveRootError && err != nil {
			return 0, err
		}
	}

	if !tree.skipFastStorageUpgrade {
		if err := tree.saveFastNodeVersion(); err != nil {
			return version, err
		}
	}

	if err := tree.ndb.Commit(); err != nil {
		return version, err
	}

	return version, nil
}

// nolint: unused
func (tree *MutableTree) getUnsavedFastNodeAdditions() map[string]*FastNode {
	return tree.unsavedFastNodeAdditions
}

// getUnsavedFastNodeRemovals returns unsaved FastNodes to remove
// nolint: unused
func (tree *MutableTree) getUnsavedFastNodeRemovals() map[string]interface{} {
	return tree.unsavedFastNodeRemovals
}

func (tree *MutableTree) addUnsavedAddition(key []byte, node *FastNode) {
	skey := unsafeToStr(key)
	delete(tree.unsavedFastNodeRemovals, skey)
	tree.unsavedFastNodeAdditions[skey] = node
}

func (tree *MutableTree) saveFastNodeAdditions() error {
	keysToSort := make([]string, 0, len(tree.unsavedFastNodeAdditions))
	for key := range tree.unsavedFastNodeAdditions {
		keysToSort = append(keysToSort, key)
	}
	sort.Strings(keysToSort)

	for _, key := range keysToSort {
		if err := tree.ndb.SaveFastNode(tree.unsavedFastNodeAdditions[key]); err != nil {
			return err
		}
	}
	return nil
}

func (tree *MutableTree) addUnsavedRemoval(key []byte) {
	skey := unsafeToStr(key)
	delete(tree.unsavedFastNodeAdditions, skey)
	tree.unsavedFastNodeRemovals[skey] = true
}

func (tree *MutableTree) saveFastNodeRemovals() error {
	keysToSort := make([]string, 0, len(tree.unsavedFastNodeRemovals))
	for key := range tree.unsavedFastNodeRemovals {
		keysToSort = append(keysToSort, key)
	}
	sort.Strings(keysToSort)

	for _, key := range keysToSort {
		if err := tree.ndb.DeleteFastNode(unsafeToBz(key)); err != nil {
			return err
		}
	}
	return nil
}

// unlocked
func (tree *MutableTree) deleteVersion(version int64) error {
	if version <= 0 {
		return errors.New("version must be greater than 0")
	}
	if version == tree.ImmutableTree().version {
		return errors.Errorf("cannot delete latest saved version (%d)", version)
	}
	if !tree.VersionExists(version) {
		return errors.Wrap(ErrVersionDoesNotExist, "")
	}
	if err := tree.ndb.DeleteVersion(version, true); err != nil {
		return err
	}

	return nil
}

// SetInitialVersion sets the initial version of the tree, replacing Options.InitialVersion.
// It is only used during the initial SaveVersion() call for a tree with no other versions,
// and is otherwise ignored.
func (tree *MutableTree) SetInitialVersion(version uint64) {
	tree.Mtx.Lock()
	defer tree.Mtx.Unlock()
	tree.ndb.opts.InitialVersion = version
}

// DeleteVersions deletes a series of versions from the MutableTree.
// Deprecated: please use DeleteVersionsRange instead.
func (tree *MutableTree) DeleteVersions(versions ...int64) error {
	logger.Debug("DELETING VERSIONS: %v\n", versions)

	if tree.separateOrphanStorage {
		// no need to delete versions if we are keeping orphans separately
		return nil
	}

	if len(versions) == 0 {
		return nil
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i] < versions[j]
	})

	// Find ordered data and delete by interval
	intervals := map[int64]int64{}
	var fromVersion int64
	for _, version := range versions {
		if version-fromVersion != intervals[fromVersion] {
			fromVersion = version
		}
		intervals[fromVersion]++
	}

	for fromVersion, sortedBatchSize := range intervals {
		if err := tree.DeleteVersionsRange(fromVersion, fromVersion+sortedBatchSize); err != nil {
			return err
		}
	}

	return nil
}

// DeleteVersionsRange removes versions from an interval from the MutableTree (not inclusive).
// An error is returned if any single version has active readers.
// All writes happen in a single batch with a single commit.
func (tree *MutableTree) DeleteVersionsRange(fromVersion, toVersion int64) error {
	tree.Mtx.Lock()
	defer tree.Mtx.Unlock()

	if err := tree.ndb.DeleteVersionsRange(fromVersion, toVersion); err != nil {
		return err
	}

	if err := tree.ndb.Commit(); err != nil {
		return err
	}

	return nil
}

// DeleteVersion deletes a tree version from disk. The version can then no
// longer be accessed.
func (tree *MutableTree) DeleteVersion(version int64) error {
	logger.Debug("DELETE VERSION: %d\n", version)

	if err := tree.deleteVersion(version); err != nil {
		return err
	}

	tree.Mtx.Lock()
	defer tree.Mtx.Unlock()

	if err := tree.ndb.Commit(); err != nil {
		return err
	}

	return nil
}

// Rotate right and return the new node and orphan.
// Mtx already held
func (tree *MutableTree) rotateRight(node *Node) (*Node, *Node, error) {
	version := tree.ITree.version + 1

	var err error
	// TODO: optimize balance & rotate.
	node, err = node.clone(version)
	if err != nil {
		return nil, nil, err
	}

	orphaned, err := node.getLeftNode(tree.ITree)
	if err != nil {
		return nil, nil, err
	}
	newNode, err := orphaned.clone(version)
	if err != nil {
		return nil, nil, err
	}

	newNoderHash, newNoderCached := newNode.GetRightHash(), newNode.GetRightNode()
	newNode.SetRightHash(node.GetHash())
	newNode.SetRightNode(node)
	node.SetLeftHash(newNoderHash)
	node.SetLeftNode(newNoderCached)

	err = node.calcHeightAndSize(tree.ITree)
	if err != nil {
		return nil, nil, err
	}

	err = newNode.calcHeightAndSize(tree.ITree)
	if err != nil {
		return nil, nil, err
	}

	return newNode, orphaned, nil
}

// Rotate left and return the new node and orphan.
func (tree *MutableTree) rotateLeft(node *Node) (*Node, *Node, error) {
	version := tree.ITree.version + 1

	var err error
	// TODO: optimize balance & rotate.
	node, err = node.clone(version)
	if err != nil {
		return nil, nil, err
	}

	orphaned, err := node.getRightNode(tree.ITree)
	if err != nil {
		return nil, nil, err
	}
	newNode, err := orphaned.clone(version)
	if err != nil {
		return nil, nil, err
	}

	newNodelHash, newNodelCached := newNode.GetLeftHash(), newNode.GetLeftNode()
	newNode.SetLeftHash(node.GetHash())
	newNode.SetLeftNode(node)
	node.SetRightHash(newNodelHash)
	node.SetRightNode(newNodelCached)

	err = node.calcHeightAndSize(tree.ITree)
	if err != nil {
		return nil, nil, err
	}

	err = newNode.calcHeightAndSize(tree.ITree)
	if err != nil {
		return nil, nil, err
	}

	return newNode, orphaned, nil
}

// NOTE: assumes that node can be modified
// TODO: optimize balance & rotate
func (tree *MutableTree) balance(node *Node, orphans *[]*Node) (newSelf *Node, err error) {
	if node.GetPersisted() {
		return nil, fmt.Errorf("unexpected balance() call on persisted node")
	}
	balance, err := node.calcBalance(tree.ITree)
	if err != nil {
		return nil, err
	}

	if balance > 1 {
		leftNode, err := node.getLeftNode(tree.ITree)
		if err != nil {
			return nil, err
		}

		lftBalance, err := leftNode.calcBalance(tree.ITree)
		if err != nil {
			return nil, err
		}

		if lftBalance >= 0 {
			// Left Left Case
			newNode, orphaned, err := tree.rotateRight(node)
			if err != nil {
				return nil, err
			}
			*orphans = append(*orphans, orphaned)
			return newNode, nil
		}
		// Left Right Case
		var leftOrphaned *Node

		left, err := node.getLeftNode(tree.ITree)
		if err != nil {
			return nil, err
		}
		node.SetLeftHash(nil)
		lNode, lOrphaned, err := tree.rotateLeft(left)
		if err != nil {
			return nil, err
		}
		leftOrphaned = lOrphaned
		node.SetLeftNode(lNode)

		newNode, rightOrphaned, err := tree.rotateRight(node)
		if err != nil {
			return nil, err
		}
		*orphans = append(*orphans, left, leftOrphaned, rightOrphaned)
		return newNode, nil
	}
	if balance < -1 {
		rightNode, err := node.getRightNode(tree.ITree)
		if err != nil {
			return nil, err
		}

		rightBalance, err := rightNode.calcBalance(tree.ITree)
		if err != nil {
			return nil, err
		}
		if rightBalance <= 0 {
			// Right Right Case
			newNode, orphaned, err := tree.rotateLeft(node)
			if err != nil {
				return nil, err
			}
			*orphans = append(*orphans, orphaned)
			return newNode, nil
		}
		// Right Left Case
		var rightOrphaned *Node

		right, err := node.getRightNode(tree.ITree)
		if err != nil {
			return nil, err
		}
		node.SetRightHash(nil)
		rNode, rOrphaned, err := tree.rotateRight(right)
		if err != nil {
			return nil, err
		}
		rightOrphaned = rOrphaned
		node.SetRightNode(rNode)
		newNode, leftOrphaned, err := tree.rotateLeft(node)
		if err != nil {
			return nil, err
		}

		*orphans = append(*orphans, right, leftOrphaned, rightOrphaned)
		return newNode, nil
	}
	// Nothing changed
	return node, nil
}

func (tree *MutableTree) addOrphans(orphans []*Node) error {
	for _, node := range orphans {
		if !node.GetPersisted() {
			// We don't need to orphan nodes that were never persisted.
			continue
		}
		if len(node.GetHash()) == 0 {
			return fmt.Errorf("expected to find node hash, but was empty")
		}
		tree.orphans[unsafeToStr(node.GetHash())] = node.GetVersion()
	}
	return nil
}

func (tree *MutableTree) Version() int64 {
	return tree.ImmutableTree().Version()
}
